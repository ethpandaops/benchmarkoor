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
//   - Fan-out writes are serialized per connection via the conn's own
//     mutex + a 5 s deadline; slow / dead subscribers are kicked.

const (
	maxLogBytesPerRun    = 512 * 1024
	wsWriteDeadline      = 5 * time.Second
	wsMaxReadFrame       = 128 * 1024 // 64 KiB chunks + headroom
	wsServerAcceptClose  = "server shutting down"
	wsServerRunEndClose  = "run ended"
	wsServerRunnerChange = "runner replaced"
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
	runnerConn *wsConn // nil between connects
	uis        []*wsConn
	buf        []byte // ring-style byte slice, capped at maxLogBytesPerRun
	seq        int64  // total bytes ever received; monotonic across truncations
	truncated  bool   // sticky once buffer has dropped bytes
	streaming  bool   // last command sent to runner
	updated    time.Time
}

// wsConn wraps a single WebSocket with its own write serialization.
// nhooyr's Write must not be called concurrently, so every outbound
// message goes through Write() which takes the mutex + a short
// deadline.
type wsConn struct {
	ws    *websocket.Conn
	mu    sync.Mutex
	log   logrus.FieldLogger
	label string // "runner" / "ui" — for log context
}

func newWsHub(log logrus.FieldLogger) *wsHub {
	return &wsHub{
		runs: make(map[string]*runHub),
		log:  log.WithField("component", "wshub"),
	}
}

// RegisterRunner attaches a newly-connected runner WS to the hub's
// run. Replaces any existing runner conn (a reconnect supersedes the
// stale one). Evaluates current subscriber state and pushes an initial
// stream_on/off command synchronously before returning. Runs the read
// loop until the WS closes.
func (h *wsHub) RegisterRunner(ctx context.Context, runID string, ws *websocket.Conn) {
	conn := &wsConn{ws: ws, log: h.log.WithFields(logrus.Fields{"run_id": runID, "side": "runner"}), label: "runner"}

	rh := h.getOrCreate(runID)

	rh.mu.Lock()
	var old *wsConn
	if rh.runnerConn != nil {
		old = rh.runnerConn
	}
	rh.runnerConn = conn
	rh.updated = time.Now().UTC()

	// Runners default to "not streaming" — only send stream_on if we
	// already have UIs waiting. stream_off is never sent on connect
	// because the runner starts in the off state anyway.
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
		sendCommand(conn, true)
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

// RegisterUI attaches a newly-connected UI WS to the hub's run. Sends
// the current buffer as a snapshot immediately, then fan-out forwards
// live log messages to this conn. Runs the read loop until the UI
// closes. The read loop is trivial — we only need it to notice the
// close; UI clients don't send messages today.
func (h *wsHub) RegisterUI(ctx context.Context, runID string, ws *websocket.Conn) {
	conn := &wsConn{ws: ws, log: h.log.WithFields(logrus.Fields{"run_id": runID, "side": "ui"}), label: "ui"}

	rh := h.getOrCreate(runID)

	var (
		snapshot     []byte
		wasTruncated bool
		needStreamOn bool
		runnerForCmd *wsConn
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

	if err := writeJSONMsg(conn, wsMessage{Type: "snapshot", Text: string(snapshot), Truncated: wasTruncated}); err != nil {
		conn.log.WithError(err).Debug("Failed to send initial snapshot")
		_ = ws.Close(websocket.StatusInternalError, "snapshot write failed")

		h.unregisterUI(runID, conn)

		return
	}

	if needStreamOn {
		sendCommand(runnerForCmd, true)
	}

	// Read loop — we just wait for the close; UI clients don't send
	// messages today. Still honor ping/pong via nhooyr's defaults.
	for {
		_, _, err := ws.Read(ctx)
		if err != nil {
			break
		}
	}

	h.unregisterUI(runID, conn)
	conn.log.Info("UI WS disconnected")
}

// DropRun closes all WSes for the run with a run_ended notice and
// frees the buffer. Called by the indexer right after DeleteLiveRun so
// the takeover to a canonical Run is clean.
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

	for _, ui := range uis {
		_ = writeJSONMsg(ui, wsMessage{Type: "run_ended"})
		_ = ui.ws.Close(websocket.StatusNormalClosure, wsServerRunEndClose)
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
			_ = ui.ws.Close(websocket.StatusGoingAway, wsServerAcceptClose)
		}
	}
}

// getOrCreate returns the runHub for runID, creating it lazily. Used
// by Register* — we intentionally don't require a live_runs row to
// exist before accepting WS registrations; callers (handlers) gate
// that check earlier in the request flow.
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
func (h *wsHub) readRunnerLoop(ctx context.Context, runID string, conn *wsConn) {
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
// forwards them to every subscribed UI conn. Called on every log
// message from the runner.
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

	uis := make([]*wsConn, len(rh.uis))
	copy(uis, rh.uis)
	rh.mu.Unlock()

	msg := wsMessage{Type: "log", Text: string(data)}
	for _, ui := range uis {
		if err := writeJSONMsg(ui, msg); err != nil {
			// Close the conn; the UI's read loop will notice and
			// unregister itself.
			_ = ui.ws.Close(websocket.StatusInternalError, "slow ui write")
		}
	}
}

// unregisterUI removes a UI conn from its run. If that drops the
// subscriber count to 0, send stream_off to the runner.
func (h *wsHub) unregisterUI(runID string, conn *wsConn) {
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

	var runnerForCmd *wsConn

	if len(rh.uis) == 0 && rh.runnerConn != nil && rh.streaming {
		rh.streaming = false
		runnerForCmd = rh.runnerConn
	}

	rh.updated = time.Now().UTC()
	rh.mu.Unlock()

	if runnerForCmd != nil {
		sendCommand(runnerForCmd, false)
	}
}

// sendCommand emits stream_on / stream_off to a runner conn. Errors
// are logged and ignored; the read loop will unregister if the conn
// is truly dead.
func sendCommand(conn *wsConn, on bool) {
	if conn == nil {
		return
	}

	msg := wsMessage{Type: "stream_off"}
	if on {
		msg.Type = "stream_on"
	}

	if err := writeJSONMsg(conn, msg); err != nil {
		conn.log.WithError(err).Debug("Failed to send stream command")
	}
}

// writeJSONMsg serializes and sends a message with the write deadline
// applied. Caller-safe across goroutines via the per-conn mutex.
func writeJSONMsg(conn *wsConn, msg wsMessage) error {
	conn.mu.Lock()
	defer conn.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), wsWriteDeadline)
	defer cancel()

	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return conn.ws.Write(ctx, websocket.MessageText, b)
}
