package livereport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

// testServer spins up an httptest WS server whose handler runs the
// provided function once per connection. Callers drive the streamer's
// behavior by writing commands to the server-side conn and asserting
// on what the streamer sends back.
type testServer struct {
	srv        *httptest.Server
	onConnect  func(ctx context.Context, ws *websocket.Conn)
	connectCnt atomic.Int64
}

func newTestServer(t *testing.T, onConnect func(ctx context.Context, ws *websocket.Conn)) *testServer {
	t.Helper()

	ts := &testServer{onConnect: onConnect}
	ts.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}

		ts.connectCnt.Add(1)
		ts.onConnect(r.Context(), ws)
	}))
	t.Cleanup(ts.srv.Close)

	return ts
}

func (ts *testServer) endpoint() string {
	// The streamer appends /api/v1/ingest/ws itself; our handler is
	// catch-all so it doesn't matter what path we use.
	return ts.srv.URL
}

// writeJSON sends a wsMessage to the streamer from the test server.
func writeJSON(t *testing.T, ws *websocket.Conn, msg wsMessage) {
	t.Helper()

	b, err := json.Marshal(msg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	require.NoError(t, ws.Write(ctx, websocket.MessageText, b))
}

// readJSON reads one wsMessage from a client conn with a short deadline.
func readJSON(t *testing.T, ws *websocket.Conn) (wsMessage, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, data, err := ws.Read(ctx)
	if err != nil {
		return wsMessage{}, err
	}

	var msg wsMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return wsMessage{}, err
	}

	return msg, nil
}

// writeFile is a convenience for overwriting the log tail test file.
func writeFile(t *testing.T, path, text string) {
	t.Helper()

	require.NoError(t, os.WriteFile(path, []byte(text), 0o644))
}

// appendFile appends text atomically.
func appendFile(t *testing.T, path, text string) {
	t.Helper()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)

	_, err = f.WriteString(text)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

func streamerCfg(endpoint string) *config.LiveReportingConfig {
	return &config.LiveReportingConfig{
		Enabled:       true,
		Endpoint:      endpoint,
		Token:         "test-token",
		DiscoveryPath: "dp",
		Timeout:       "2s",
		LogsInterval:  "50ms",
	}
}

func silentLogger() logrus.FieldLogger {
	l := logrus.New()
	l.SetLevel(logrus.PanicLevel)

	return l
}

func TestLogStreamer_IdleUntilStreamOn(t *testing.T) {
	// Collect all messages the streamer sends. No commands are sent
	// from the server side, so nothing should arrive.
	var (
		mu  sync.Mutex
		got []wsMessage
	)

	ts := newTestServer(t, func(ctx context.Context, ws *websocket.Conn) {
		defer func() { _ = ws.Close(websocket.StatusNormalClosure, "") }()

		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				return
			}

			var msg wsMessage
			if err := json.Unmarshal(data, &msg); err == nil {
				mu.Lock()
				got = append(got, msg)
				mu.Unlock()
			}
		}
	})

	dir := t.TempDir()
	logPath := filepath.Join(dir, "benchmarkoor.log")

	writeFile(t, logPath, "hello there\n")

	s := NewLogStreamer(silentLogger(), streamerCfg(ts.endpoint()), "run-1", logPath)
	ctx := context.Background()
	s.Start(ctx)

	// Give the streamer time to connect and for any tail to fire if
	// it were going to (it shouldn't — no stream_on).
	time.Sleep(300 * time.Millisecond)

	s.Stop()

	// The server may receive a bye frame on Stop; anything else (like
	// log messages) would be a bug.
	mu.Lock()
	defer mu.Unlock()

	for _, m := range got {
		assert.NotEqual(t, "log", m.Type, "streamer must not send log messages without stream_on")
	}
}

func TestLogStreamer_TailsOnStreamOn(t *testing.T) {
	logCh := make(chan string, 16)

	ts := newTestServer(t, func(ctx context.Context, ws *websocket.Conn) {
		defer func() { _ = ws.Close(websocket.StatusNormalClosure, "") }()

		// Ask the streamer to start tailing.
		writeJSON(t, ws, wsMessage{Type: "stream_on"})

		// Drain whatever the streamer sends; deliver log chunks on
		// logCh so the test can assert on them.
		for {
			msg, err := readJSON(t, ws)
			if err != nil {
				return
			}

			if msg.Type == "log" {
				select {
				case logCh <- msg.Text:
				default:
				}
			}
		}
	})

	dir := t.TempDir()
	logPath := filepath.Join(dir, "benchmarkoor.log")
	writeFile(t, logPath, "initial bytes\n")

	s := NewLogStreamer(silentLogger(), streamerCfg(ts.endpoint()), "run-1", logPath)

	ctx := context.Background()
	s.Start(ctx)

	// First chunk should be the initial file contents.
	select {
	case got := <-logCh:
		assert.Equal(t, "initial bytes\n", got)
	case <-time.After(2 * time.Second):
		t.Fatal("expected initial log chunk")
	}

	// Append more — should show up on the next tick.
	appendFile(t, logPath, "more stuff\n")

	select {
	case got := <-logCh:
		assert.Equal(t, "more stuff\n", got)
	case <-time.After(2 * time.Second):
		t.Fatal("expected appended log chunk")
	}

	s.Stop()
}

func TestLogStreamer_HandlesTruncation(t *testing.T) {
	logCh := make(chan string, 16)

	ts := newTestServer(t, func(ctx context.Context, ws *websocket.Conn) {
		defer func() { _ = ws.Close(websocket.StatusNormalClosure, "") }()

		writeJSON(t, ws, wsMessage{Type: "stream_on"})

		for {
			msg, err := readJSON(t, ws)
			if err != nil {
				return
			}

			if msg.Type == "log" {
				select {
				case logCh <- msg.Text:
				default:
				}
			}
		}
	})

	dir := t.TempDir()
	logPath := filepath.Join(dir, "benchmarkoor.log")
	writeFile(t, logPath, strings.Repeat("a", 100))

	s := NewLogStreamer(silentLogger(), streamerCfg(ts.endpoint()), "run-1", logPath)
	ctx := context.Background()
	s.Start(ctx)

	// Consume the initial chunk.
	select {
	case got := <-logCh:
		assert.Len(t, got, 100)
	case <-time.After(2 * time.Second):
		t.Fatal("initial chunk missing")
	}

	// Truncate by rewriting smaller. Next tick should reset offset to
	// 0 and ship the new contents from the start.
	writeFile(t, logPath, "tiny\n")

	select {
	case got := <-logCh:
		assert.Equal(t, "tiny\n", got)
	case <-time.After(2 * time.Second):
		t.Fatal("expected post-truncation chunk")
	}

	s.Stop()
}

func TestLogStreamer_StopSendsBye(t *testing.T) {
	var (
		mu  sync.Mutex
		got []wsMessage
	)

	done := make(chan struct{})

	ts := newTestServer(t, func(ctx context.Context, ws *websocket.Conn) {
		defer close(done)
		defer func() { _ = ws.Close(websocket.StatusNormalClosure, "") }()

		for {
			msg, err := readJSON(t, ws)
			if err != nil {
				return
			}

			mu.Lock()
			got = append(got, msg)
			mu.Unlock()
		}
	})

	dir := t.TempDir()
	logPath := filepath.Join(dir, "benchmarkoor.log")
	writeFile(t, logPath, "")

	s := NewLogStreamer(silentLogger(), streamerCfg(ts.endpoint()), "run-1", logPath)
	s.Start(context.Background())

	time.Sleep(150 * time.Millisecond)

	s.Stop()

	// Server should see the connection close (triggering done).
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("server never saw close")
	}

	mu.Lock()
	defer mu.Unlock()

	found := false
	for _, m := range got {
		if m.Type == "bye" {
			found = true
			break
		}
	}
	assert.True(t, found, "streamer should send a bye before closing")
}

func TestLogStreamer_DisabledByConfig(t *testing.T) {
	disabled := false

	cfg := &config.LiveReportingConfig{
		Enabled:      true,
		Endpoint:     "http://127.0.0.1:1", // unreachable; would fail if dialed
		Token:        "x",
		Timeout:      "1s",
		LogsEnabled:  &disabled,
		LogsInterval: "50ms",
	}

	dir := t.TempDir()
	logPath := filepath.Join(dir, "benchmarkoor.log")
	writeFile(t, logPath, "")

	s := NewLogStreamer(silentLogger(), cfg, "run-1", logPath)
	s.Start(context.Background())

	// Give it a beat to NOT dial anything.
	time.Sleep(150 * time.Millisecond)

	s.Stop()
}
