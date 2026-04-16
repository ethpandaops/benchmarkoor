package livereport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/sirupsen/logrus"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

// LogStreamer owns a persistent WebSocket to the API for the lifetime
// of a run and streams tail-bytes of benchmarkoor.log on demand. The
// API tells it when to start / stop streaming based on whether any UI
// client is watching; when nobody is watching, the streamer holds the
// socket open but sends zero bytes.
type LogStreamer interface {
	Start(ctx context.Context)
	Stop()
}

// NewLogStreamer creates a streamer. logFilePath is the benchmarkoor.log
// file for the run; the streamer tails it by byte offset (not
// line-count) and handles rotation / truncation by resetting its
// offset when the file shrinks.
func NewLogStreamer(log logrus.FieldLogger, cfg *config.LiveReportingConfig, runID, logFilePath string) LogStreamer {
	return &logStreamer{
		log:         log.WithFields(logrus.Fields{"component": "livereport.log", "run_id": runID}),
		cfg:         cfg,
		runID:       runID,
		logFilePath: logFilePath,
		done:        make(chan struct{}),
	}
}

type logStreamer struct {
	log         logrus.FieldLogger
	cfg         *config.LiveReportingConfig
	runID       string
	logFilePath string

	done   chan struct{}
	wg     sync.WaitGroup
	closed bool
	mu     sync.Mutex
}

// Compile-time interface check.
var _ LogStreamer = (*logStreamer)(nil)

// wsMessage must match the envelope used by pkg/api/wshub.go. Kept in
// sync by hand — there are only four message types and the surface is
// intentionally tiny.
type wsMessage struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Start launches the outer goroutine that owns the WS lifecycle with
// backoff reconnect. Safe to call once. No-op when LogsEnabled is
// false.
func (s *logStreamer) Start(ctx context.Context) {
	if !s.cfg.GetLogsEnabled() {
		s.log.Debug("Log streamer disabled by config")

		return
	}

	s.wg.Add(1)

	go s.runConnectLoop(ctx)
}

// Stop signals the outer goroutine to exit and waits for it. Sends a
// bye frame on the way out if there's still an open WS. Idempotent.
func (s *logStreamer) Stop() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()

		return
	}

	s.closed = true
	s.mu.Unlock()

	close(s.done)
	s.wg.Wait()
}

// runConnectLoop is the outer lifecycle: dial, run the read/write
// loop, reconnect with exponential backoff on unexpected close.
func (s *logStreamer) runConnectLoop(ctx context.Context) {
	defer s.wg.Done()

	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		default:
		}

		if err := s.connectAndServe(ctx); err != nil {
			// Unexpected disconnect — log at debug and retry.
			s.log.WithError(err).Debug("Log WS disconnected; will retry")
		}

		// Bail immediately if we're shutting down.
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		case <-time.After(jitterBackoff(backoff)):
		}

		if backoff < maxBackoff {
			backoff = min(backoff*2, maxBackoff)
		}
	}
}

// connectAndServe dials the WS and runs its read loop until the
// connection closes. Returns the disconnect error (nil on graceful
// close from our side).
func (s *logStreamer) connectAndServe(ctx context.Context) error {
	dialURL, err := s.wsURL()
	if err != nil {
		return fmt.Errorf("building ws url: %w", err)
	}

	// Dial context with a short handshake timeout — the runner must
	// not block on a dead API forever.
	dialCtx, cancel := context.WithTimeout(ctx, s.cfg.GetTimeout())
	defer cancel()

	hdr := http.Header{}
	hdr.Set("Authorization", "Bearer "+s.cfg.Token)

	ws, _, err := websocket.Dial(dialCtx, dialURL, &websocket.DialOptions{
		HTTPHeader: hdr,
	})
	if err != nil {
		return fmt.Errorf("dialing %s: %w", dialURL, err)
	}

	// Use a generous read limit to accommodate the 64 KiB-per-message
	// convention on the server side.
	ws.SetReadLimit(128 * 1024)

	s.log.Info("Log WS connected")

	// Inner tail state — only live when stream_on is active. Uses a
	// done-channel (not context.WithCancel) so the static analyzer
	// doesn't chase a lost-cancel warning across the read-loop exit
	// paths.
	var (
		tailDone   chan struct{}
		tailWG     sync.WaitGroup
		tailActive bool
	)

	stopTail := func() {
		if !tailActive {
			return
		}

		close(tailDone)
		tailWG.Wait()
		tailDone = nil
		tailActive = false
	}

	// One write-serializing mutex per connection.
	writer := &wsWriter{ws: ws, timeout: s.cfg.GetTimeout()}

	defer func() {
		stopTail()
		_ = ws.Close(websocket.StatusNormalClosure, "runner stopping")
	}()

	// If the outer loop was cancelled while connected, send a bye
	// frame and close the WS so the server cleans up immediately and
	// the read loop unblocks. Use a fresh background context so a
	// cancelled outer ctx doesn't also stop the write.
	go func() {
		select {
		case <-ctx.Done():
		case <-s.done:
		}

		byeCtx, cancel := context.WithTimeout(context.Background(), s.cfg.GetTimeout())
		_ = writer.writeMessage(byeCtx, wsMessage{Type: "bye"})
		cancel()

		_ = ws.Close(websocket.StatusNormalClosure, "runner stopping")
	}()

	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			stopTail()

			return err
		}

		var msg wsMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			s.log.WithError(err).Debug("Ignoring malformed API message")

			continue
		}

		switch msg.Type {
		case "stream_on":
			if tailActive {
				continue // already running
			}

			tailDone = make(chan struct{})
			tailActive = true
			done := tailDone

			tailWG.Add(1)

			go func() {
				defer tailWG.Done()
				s.runTailLoop(ctx, done, writer)
			}()

			s.log.Debug("Log tail started")
		case "stream_off":
			stopTail()
			s.log.Debug("Log tail stopped")
		default:
			s.log.WithField("type", msg.Type).Debug("Ignoring unknown API message")
		}
	}
}

// runTailLoop periodically reads new bytes from the log file and
// sends them as wsMessages. Exits when done is closed (stream_off from
// the outer loop) or ctx is cancelled (outer shutdown).
func (s *logStreamer) runTailLoop(ctx context.Context, done <-chan struct{}, writer *wsWriter) {
	interval := s.cfg.GetLogsInterval()
	if interval <= 0 {
		interval = config.DefaultLiveReportingLogsInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var offset int64

	readAndShip := func() {
		newBytes, newOffset, err := readNewBytes(s.logFilePath, offset)
		if err != nil {
			s.log.WithError(err).Debug("log file read failed")

			return
		}

		if len(newBytes) == 0 {
			offset = newOffset

			return
		}

		// Split large chunks into <=64 KiB messages so the server's
		// read limit is never exceeded.
		const maxMsgBytes = 64 * 1024
		for len(newBytes) > 0 {
			n := min(len(newBytes), maxMsgBytes)

			if err := writer.writeMessage(ctx, wsMessage{Type: "log", Text: string(newBytes[:n])}); err != nil {
				s.log.WithError(err).Debug("Failed to send log chunk")

				return // leave offset unchanged; next connect will retry from here
			}

			newBytes = newBytes[n:]
		}

		offset = newOffset
	}

	// Fire once immediately so the first log chunk doesn't wait a
	// full tick after stream_on.
	readAndShip()

	for {
		select {
		case <-ctx.Done():
			// Final flush so the last bytes reach the API before we go.
			readAndShip()

			return
		case <-done:
			// stream_off — flush and exit.
			readAndShip()

			return
		case <-ticker.C:
			readAndShip()
		}
	}
}

// wsURL builds the ws(s) URL the runner dials. Converts the configured
// HTTP(S) endpoint to ws(s) and appends the ingest path with the
// run_id query param.
func (s *logStreamer) wsURL() (string, error) {
	raw := strings.TrimRight(s.cfg.Endpoint, "/") + "/api/v1/ingest/ws"

	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}

	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
		// Already a WS URL; leave it alone.
	default:
		return "", fmt.Errorf("unsupported endpoint scheme %q", u.Scheme)
	}

	q := u.Query()
	q.Set("run_id", s.runID)
	u.RawQuery = q.Encode()

	return u.String(), nil
}

// wsWriter serializes writes to a WS connection. nhooyr/coder's Write
// must not be called concurrently; this wrapper enforces that and
// applies a deadline.
type wsWriter struct {
	ws      *websocket.Conn
	mu      sync.Mutex
	timeout time.Duration
}

func (w *wsWriter) writeMessage(ctx context.Context, msg wsMessage) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	writeCtx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return w.ws.Write(writeCtx, websocket.MessageText, b)
}

// readNewBytes opens the log file and reads from `offset` to EOF. On
// truncation / rotation (size < offset), starts from 0. Returns the
// bytes read and the new offset.
func readNewBytes(path string, offset int64) ([]byte, int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Not yet created — nothing to do.
			return nil, offset, nil
		}

		return nil, offset, err
	}

	size := fi.Size()
	if size < offset {
		offset = 0
	}

	if size == offset {
		return nil, offset, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}

	// Cap per-tick read so a single surge can't ship unbounded bytes.
	const maxRead = 64 * 1024

	n := min(size-offset, maxRead)

	buf := make([]byte, n)

	read, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, offset, err
	}

	return buf[:read], offset + int64(read), nil
}

// jitterBackoff returns d with ±25% jitter. Prevents thundering-herd
// reconnects when the API bounces.
func jitterBackoff(d time.Duration) time.Duration {
	jitter := 1 + (rand.Float64()*0.5 - 0.25) // [0.75, 1.25)
	out := time.Duration(float64(d) * jitter)
	if out <= 0 {
		return d
	}

	return out
}
