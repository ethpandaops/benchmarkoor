package api

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/sirupsen/logrus"
)

// Live log-stream broker between benchmarkoor runners (one persistent
// WS per run, pushes log bytes when asked) and browser clients (one WS
// per open log panel, receives the buffered snapshot + live messages).
//
// Design:
//   - Every run has a runHub with an in-memory ring buffer (so late UIs
//     see recent history), a single runner conn, and zero or more UI
//     conns.
//   - Subscriber count flips from 0→1 (first UI connects) cause the
//     hub to send {type:"stream_on"} to the runner. 1→0 sends
//     {type:"stream_off"}. No TTL — we rely on the OS detecting the
//     closed socket.
//   - Each UI conn has a buffered send channel + a dedicated writer
//     goroutine. Fan-out is non-blocking: if the channel is full, the
//     conn is kicked immediately. This prevents a single slow
//     subscriber from stalling the runner's read loop.

const (
	maxLogBytesPerRun    = 512 * 1024
	wsWriteDeadline      = 5 * time.Second
	wsMaxReadFrame       = 128 * 1024 // 64 KiB chunks + headroom
	wsServerAcceptClose  = "server shutting down"
	wsServerRunEndClose  = "run ended"
	wsServerRunnerChange = "runner replaced"
	uiSendChCapacity     = 64 // buffered messages per UI conn
)

// wsMessage is the common envelope; not all fields are used in every
// direction. JSON tags match the schema documented on the endpoints.
type wsMessage struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

type wsHub struct {
	mu   sync.Mutex
	runs map[string]*runHub
	log  logrus.FieldLogger
}

type runHub struct {
	mu         sync.Mutex
	runnerConn *runnerConn // nil between connects
	uis        []*uiConn
	buf        []byte // ring-style byte slice, capped at maxLogBytesPerRun
	seq        int64  // total bytes ever received; monotonic across truncations
	truncated  bool   // sticky once buffer has dropped bytes
	streaming  bool   // last command sent to runner
	updated    time.Time
}

// runnerConn wraps the runner-side WS. Commands (stream_on/off) are
// rare and sent directly under a mutex — no need for a channel.
type runnerConn struct {
	ws  *websocket.Conn
	mu  sync.Mutex
	log logrus.FieldLogger
}

// uiConn wraps a UI-side WS with a buffered send channel + a writer
// goroutine so fan-out never blocks the runner's read loop. If the
// channel fills (backpressure from a slow client), the conn is kicked.
type uiConn struct {
	ws        *websocket.Conn
	sendCh    chan []byte   // pre-serialized JSON messages
	done      chan struct{} // closed when writer goroutine exits
	closeOnce sync.Once
	log       logrus.FieldLogger
}

func newWsHub(log logrus.FieldLogger) *wsHub {
	return &wsHub{
		runs: make(map[string]*runHub),
		log:  log.WithField("component", "wshub"),
	}
}

// ── Runner registration ──────────────────────────────────────────

// RegisterRunner attaches a newly-connected runner WS to the hub's
// run. Replaces any existing runner conn (a reconnect supersedes the
// stale one). Runs the read loop until the WS closes.
func (h *wsHub) RegisterRunner(ctx context.Context, runID string, ws *websocket.Conn) {
	conn := &runnerConn{ws: ws, log: h.log.WithFields(logrus.Fields{"run_id": runID, "side": "runner"})}

	rh := h.getOrCreate(runID)

	rh.mu.Lock()
	var old *runnerConn
	if rh.runnerConn != nil {
		old = rh.runnerConn
	}
	rh.runnerConn = conn
	rh.updated = time.Now().UTC()
	want := len(rh.uis) > 0
	rh.streaming = want
	rh.mu.Unlock()

	if old != nil {
		_ = old.ws.Close(websocket.StatusNormalClosure, wsServerRunnerChange)
		conn.log.Info("Replaced stale runner WS connection")
	} else {
		conn.log.Info("Runner WS connected")
	}

	if want {
		sendRunnerCommand(conn, true)
	}

	h.readRunnerLoop(ctx, runID, conn)

	rh.mu.Lock()
	if rh.runnerConn == conn {
		rh.runnerConn = nil
		rh.streaming = false
	}
	rh.mu.Unlock()

	conn.log.Info("Runner WS disconnected")
}

// ── UI registration ──────────────────────────────────────────────

// RegisterUI attaches a newly-connected UI WS. Sends the current
// buffer as a snapshot, starts a writer goroutine for non-blocking
// fan-out, then blocks on a read loop (just to detect close). On
// exit, the conn is unregistered and the writer goroutine drained.
func (h *wsHub) RegisterUI(ctx context.Context, runID string, ws *websocket.Conn) {
	conn := newUIConn(ws, h.log.WithFields(logrus.Fields{"run_id": runID, "side": "ui"}))

	rh := h.getOrCreate(runID)

	var (
		snapshot     []byte
		wasTruncated bool
		needStreamOn bool
		runnerForCmd *runnerConn
	)

	rh.mu.Lock()
	rh.uis = append(rh.uis, conn)
	rh.updated = time.Now().UTC()
	if len(rh.buf) > 0 {
		snapshot = append(make([]byte, 0, len(rh.buf)), rh.buf...)
		wasTruncated = rh.truncated
	}
	if len(rh.uis) == 1 && rh.runnerConn != nil && !rh.streaming {
		needStreamOn = true
		rh.streaming = true
		runnerForCmd = rh.runnerConn
	}
	rh.mu.Unlock()

	conn.log.Info("UI WS connected")

	// Send snapshot through the channel (buffered, will not block).
	snapMsg, _ := json.Marshal(wsMessage{Type: "snapshot", Text: string(snapshot), Truncated: wasTruncated})
	if !conn.send(snapMsg) {
		conn.kick()
		conn.awaitWriter()
		h.unregisterUI(runID, conn)

		return
	}

	if needStreamOn {
		sendRunnerCommand(runnerForCmd, true)
	}

	// Read loop — we just wait for the close; UI clients don't send
	// messages today. Ping/pong is handled by the library.
	for {
		_, _, err := ws.Read(ctx)
		if err != nil {
			break
		}
	}

	conn.kick()
	conn.awaitWriter()
	h.unregisterUI(runID, conn)
	conn.log.Info("UI WS disconnected")
}

// ── Lifecycle ────────────────────────────────────────────────────

// DropRun closes all WSes for the run with a run_ended notice and
// frees the buffer. Called by the indexer right after DeleteLiveRun.
func (h *wsHub) DropRun(runID string) {
	h.mu.Lock()
	rh, ok := h.runs[runID]
	if ok {
		delete(h.runs, runID)
	}
	h.mu.Unlock()

	if !ok {
		return
	}

	rh.mu.Lock()
	runner := rh.runnerConn
	uis := rh.uis
	rh.runnerConn = nil
	rh.uis = nil
	rh.mu.Unlock()

	endedMsg, _ := json.Marshal(wsMessage{Type: "run_ended"})
	for _, ui := range uis {
		ui.send(endedMsg) // buffered; drainAndClose flushes it
		ui.drainAndClose()
	}

	if runner != nil {
		_ = runner.ws.Close(websocket.StatusNormalClosure, wsServerRunEndClose)
	}
}

// EvictIdle drops runHubs with no runner + no UI that have been idle
// past `cutoff`. Called from the stalewatcher on its existing tick.
func (h *wsHub) EvictIdle(cutoff time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for id, rh := range h.runs {
		rh.mu.Lock()
		idle := rh.runnerConn == nil && len(rh.uis) == 0 && rh.updated.Before(cutoff)
		rh.mu.Unlock()

		if idle {
			delete(h.runs, id)
		}
	}
}

// Stop closes every open WS on server shutdown.
func (h *wsHub) Stop() {
	h.mu.Lock()
	runs := h.runs
	h.runs = make(map[string]*runHub)
	h.mu.Unlock()

	for _, rh := range runs {
		rh.mu.Lock()
		runner := rh.runnerConn
		uis := rh.uis
		rh.runnerConn = nil
		rh.uis = nil
		rh.mu.Unlock()

		if runner != nil {
			_ = runner.ws.Close(websocket.StatusGoingAway, wsServerAcceptClose)
		}
		for _, ui := range uis {
			ui.kick()
		}
	}
}

// ── Internals ────────────────────────────────────────────────────

func (h *wsHub) getOrCreate(runID string) *runHub {
	h.mu.Lock()
	defer h.mu.Unlock()

	if rh, ok := h.runs[runID]; ok {
		return rh
	}

	rh := &runHub{updated: time.Now().UTC()}
	h.runs[runID] = rh

	return rh
}

// readRunnerLoop handles incoming runner messages (log, bye). Returns
// when the WS closes or ctx is cancelled.
func (h *wsHub) readRunnerLoop(ctx context.Context, runID string, conn *runnerConn) {
	conn.ws.SetReadLimit(wsMaxReadFrame)

	for {
		_, data, err := conn.ws.Read(ctx)
		if err != nil {
			return
		}

		var msg wsMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			conn.log.WithError(err).Debug("Ignoring malformed runner message")

			continue
		}

		switch msg.Type {
		case "log":
			h.appendAndFanOut(runID, []byte(msg.Text))
		case "bye":
			return
		default:
			conn.log.WithField("type", msg.Type).Debug("Ignoring unknown runner message")
		}
	}
}

// appendAndFanOut writes new bytes into the run's ring buffer and
// pushes a pre-serialized message to every subscribed UI conn's
// channel. The push is non-blocking: if a UI's channel is full (slow
// client), that conn is kicked immediately without affecting others.
func (h *wsHub) appendAndFanOut(runID string, data []byte) {
	if len(data) == 0 {
		return
	}

	h.mu.Lock()
	rh, ok := h.runs[runID]
	h.mu.Unlock()

	if !ok {
		return
	}

	rh.mu.Lock()
	// Append + trim to cap (drop oldest bytes but keep seq monotonic).
	rh.buf = append(rh.buf, data...)
	if len(rh.buf) > maxLogBytesPerRun {
		overflow := len(rh.buf) - maxLogBytesPerRun
		rh.buf = rh.buf[overflow:]

		if !rh.truncated {
			rh.truncated = true
		}
	}
	rh.seq += int64(len(data))
	rh.updated = time.Now().UTC()

	uis := make([]*uiConn, len(rh.uis))
	copy(uis, rh.uis)
	rh.mu.Unlock()

	// Pre-serialize once, push to every UI channel.
	b, err := json.Marshal(wsMessage{Type: "log", Text: string(data)})
	if err != nil {
		return
	}

	for _, ui := range uis {
		if !ui.send(b) {
			// Channel full → kick this slow subscriber. The read loop
			// in RegisterUI will notice the close and call unregisterUI.
			ui.log.Debug("Kicking slow UI subscriber")
			ui.kick()
		}
	}
}

// unregisterUI removes a UI conn from its run. If that drops the
// subscriber count to 0, send stream_off to the runner.
func (h *wsHub) unregisterUI(runID string, conn *uiConn) {
	h.mu.Lock()
	rh, ok := h.runs[runID]
	h.mu.Unlock()

	if !ok {
		return
	}

	rh.mu.Lock()
	for i, ui := range rh.uis {
		if ui == conn {
			rh.uis = append(rh.uis[:i], rh.uis[i+1:]...)

			break
		}
	}

	var runnerForCmd *runnerConn

	if len(rh.uis) == 0 && rh.runnerConn != nil && rh.streaming {
		rh.streaming = false
		runnerForCmd = rh.runnerConn
	}

	rh.updated = time.Now().UTC()
	rh.mu.Unlock()

	if runnerForCmd != nil {
		sendRunnerCommand(runnerForCmd, false)
	}
}

// ── Runner conn helpers ──────────────────────────────────────────

// sendRunnerCommand emits stream_on / stream_off to a runner conn.
// Errors are logged and ignored; the read loop will notice if the
// conn is truly dead.
func sendRunnerCommand(conn *runnerConn, on bool) {
	if conn == nil {
		return
	}

	msg := wsMessage{Type: "stream_off"}
	if on {
		msg.Type = "stream_on"
	}

	b, err := json.Marshal(msg)
	if err != nil {
		return
	}

	conn.mu.Lock()
	defer conn.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), wsWriteDeadline)
	defer cancel()

	if err := conn.ws.Write(ctx, websocket.MessageText, b); err != nil {
		conn.log.WithError(err).Debug("Failed to send stream command")
	}
}

// ── UI conn helpers ──────────────────────────────────────────────

func newUIConn(ws *websocket.Conn, log logrus.FieldLogger) *uiConn {
	c := &uiConn{
		ws:     ws,
		sendCh: make(chan []byte, uiSendChCapacity),
		done:   make(chan struct{}),
		log:    log,
	}

	go c.writeLoop()

	return c
}

// writeLoop drains the send channel and writes to the WS. Exits on
// channel close or write error.
func (c *uiConn) writeLoop() {
	defer close(c.done)

	for msg := range c.sendCh {
		ctx, cancel := context.WithTimeout(context.Background(), wsWriteDeadline)
		err := c.ws.Write(ctx, websocket.MessageText, msg)
		cancel()

		if err != nil {
			c.log.WithError(err).Debug("UI write failed, exiting writer")

			return
		}
	}
}

// send pushes a pre-serialized message onto the channel. Returns false
// if the channel is full (backpressure → caller should kick this conn).
func (c *uiConn) send(msg []byte) bool {
	select {
	case c.sendCh <- msg:
		return true
	default:
		return false
	}
}

// kick force-closes the WS + channel without waiting for the writer
// goroutine to drain. Used by appendAndFanOut to kick slow subscribers
// without blocking the runner's read loop.
func (c *uiConn) kick() {
	c.closeOnce.Do(func() {
		_ = c.ws.Close(websocket.StatusInternalError, "slow client")
		close(c.sendCh)
		// Don't wait — the writer goroutine exits on its own after the
		// WS close makes its current write fail.
	})
}

// drainAndClose lets the writer goroutine flush any buffered messages
// (e.g. a run_ended frame), waits for it to exit, then closes the WS.
// Used by DropRun where we want the client to see the final message
// before the connection drops.
func (c *uiConn) drainAndClose() {
	c.closeOnce.Do(func() {
		close(c.sendCh)
		<-c.done
		_ = c.ws.Close(websocket.StatusNormalClosure, "closed")
	})
}

// awaitWriter blocks until the writer goroutine has exited. Call after
// kick() in cleanup paths (e.g. RegisterUI return) to prevent
// goroutine leaks.
func (c *uiConn) awaitWriter() {
	<-c.done
}
