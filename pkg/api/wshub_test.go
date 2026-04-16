package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wsTestServer spins up an httptest server that upgrades either side
// via the given handler. Used by the hub tests so we can simulate
// real runner and UI connections with actual WebSocket frames.
func wsTestServer(t *testing.T, upgrade func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(upgrade))
	t.Cleanup(srv.Close)

	return srv
}

// dialWS opens a client WS to the given httptest URL. The caller is
// responsible for closing the returned conn.
func dialWS(t *testing.T, baseURL string) *websocket.Conn {
	t.Helper()

	wsURL := strings.Replace(baseURL, "http://", "ws://", 1)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ws, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err)
	// Snapshots can approach maxLogBytesPerRun in size; give the
	// client side a generous read limit.
	ws.SetReadLimit(2 * int64(maxLogBytesPerRun))

	return ws
}

// readWSMessage reads one JSON message from the client side, with a
// short deadline so tests don't hang forever.
func readWSMessage(t *testing.T, ws *websocket.Conn) wsMessage {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, data, err := ws.Read(ctx)
	require.NoError(t, err)

	var msg wsMessage
	require.NoError(t, json.Unmarshal(data, &msg))

	return msg
}

// drainMessagesUntil reads until a message of the given type is seen
// (or deadline) and returns that message. Useful when multiple
// snapshot/log events can interleave.
func drainMessagesUntil(t *testing.T, ws *websocket.Conn, wantType string, timeout time.Duration) wsMessage {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for {
		_, data, err := ws.Read(ctx)
		require.NoError(t, err)

		var msg wsMessage
		require.NoError(t, json.Unmarshal(data, &msg))

		if msg.Type == wantType {
			return msg
		}
	}
}

// hubTestSetup wires a wsHub to an httptest server that routes /runner
// and /ui paths to RegisterRunner / RegisterUI respectively.
func hubTestSetup(t *testing.T, runID string) (*wsHub, string) {
	t.Helper()

	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	hub := newWsHub(log)
	t.Cleanup(hub.Stop)

	srv := wsTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}

		switch r.URL.Path {
		case "/runner":
			hub.RegisterRunner(r.Context(), runID, ws)
		case "/ui":
			hub.RegisterUI(r.Context(), runID, ws)
		}
	})

	return hub, srv.URL
}

func TestWsHub_RunnerAloneNoStreamOn(t *testing.T) {
	_, base := hubTestSetup(t, "r1")

	runner := dialWS(t, base+"/runner")
	defer func() { _ = runner.Close(websocket.StatusNormalClosure, "") }()

	// No UI subscribers — runner must not receive any stream_on.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, _, err := runner.Read(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded, "runner should not receive any message")
}

func TestWsHub_UIConnectSendsStreamOnAndSnapshot(t *testing.T) {
	_, base := hubTestSetup(t, "r1")

	runner := dialWS(t, base+"/runner")
	defer func() { _ = runner.Close(websocket.StatusNormalClosure, "") }()

	ui := dialWS(t, base+"/ui")
	defer func() { _ = ui.Close(websocket.StatusNormalClosure, "") }()

	// UI should receive an initial snapshot (empty text, empty truncated).
	snap := readWSMessage(t, ui)
	assert.Equal(t, "snapshot", snap.Type)
	assert.Equal(t, "", snap.Text)
	assert.False(t, snap.Truncated)

	// Runner should receive stream_on.
	cmd := readWSMessage(t, runner)
	assert.Equal(t, "stream_on", cmd.Type)
}

func TestWsHub_SecondUIGetsSnapshotNoExtraStreamOn(t *testing.T) {
	_, base := hubTestSetup(t, "r1")

	runner := dialWS(t, base+"/runner")
	defer func() { _ = runner.Close(websocket.StatusNormalClosure, "") }()

	ui1 := dialWS(t, base+"/ui")
	defer func() { _ = ui1.Close(websocket.StatusNormalClosure, "") }()

	// Consume the initial messages so we can assert the second UI
	// connect doesn't cause another stream_on.
	_ = readWSMessage(t, ui1) // snapshot
	streamOn := readWSMessage(t, runner)
	require.Equal(t, "stream_on", streamOn.Type)

	// Runner sends some log text through, which should fan out to ui1.
	require.NoError(t, runner.Write(context.Background(), websocket.MessageText, []byte(`{"type":"log","text":"hello "}`)))
	logMsg := readWSMessage(t, ui1)
	require.Equal(t, "log", logMsg.Type)
	require.Equal(t, "hello ", logMsg.Text)

	// Second UI connects. Should get a snapshot with whatever's in the
	// buffer ("hello ") and the runner should NOT receive a second
	// stream_on.
	ui2 := dialWS(t, base+"/ui")
	defer func() { _ = ui2.Close(websocket.StatusNormalClosure, "") }()

	snap := readWSMessage(t, ui2)
	assert.Equal(t, "snapshot", snap.Type)
	assert.Equal(t, "hello ", snap.Text)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, _, err := runner.Read(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded, "runner should not see another stream_on")
}

func TestWsHub_LastUIDisconnectSendsStreamOff(t *testing.T) {
	_, base := hubTestSetup(t, "r1")

	runner := dialWS(t, base+"/runner")
	defer func() { _ = runner.Close(websocket.StatusNormalClosure, "") }()

	ui := dialWS(t, base+"/ui")

	_ = readWSMessage(t, ui) // snapshot
	require.Equal(t, "stream_on", readWSMessage(t, runner).Type)

	// UI disconnects; runner should receive stream_off.
	require.NoError(t, ui.Close(websocket.StatusNormalClosure, "done"))

	cmd := drainMessagesUntil(t, runner, "stream_off", time.Second)
	assert.Equal(t, "stream_off", cmd.Type)
}

func TestWsHub_FanOutToMultipleUIs(t *testing.T) {
	_, base := hubTestSetup(t, "r1")

	runner := dialWS(t, base+"/runner")
	defer func() { _ = runner.Close(websocket.StatusNormalClosure, "") }()

	ui1 := dialWS(t, base+"/ui")
	defer func() { _ = ui1.Close(websocket.StatusNormalClosure, "") }()
	ui2 := dialWS(t, base+"/ui")
	defer func() { _ = ui2.Close(websocket.StatusNormalClosure, "") }()

	// Consume snapshots + stream_on.
	_ = readWSMessage(t, ui1)
	_ = readWSMessage(t, ui2)
	require.Equal(t, "stream_on", readWSMessage(t, runner).Type)

	// Runner sends a log chunk; both UIs should see it.
	require.NoError(t, runner.Write(context.Background(), websocket.MessageText, []byte(`{"type":"log","text":"fanout"}`)))

	var wg sync.WaitGroup
	wg.Add(2)

	check := func(ws *websocket.Conn) {
		defer wg.Done()

		msg := readWSMessage(t, ws)
		assert.Equal(t, "log", msg.Type)
		assert.Equal(t, "fanout", msg.Text)
	}

	go check(ui1)
	go check(ui2)
	wg.Wait()
}

func TestWsHub_DropRunClosesAllAndNotifiesUIs(t *testing.T) {
	hub, base := hubTestSetup(t, "r1")

	runner := dialWS(t, base+"/runner")
	ui := dialWS(t, base+"/ui")

	_ = readWSMessage(t, ui)     // snapshot
	_ = readWSMessage(t, runner) // stream_on

	hub.DropRun("r1")

	msg := readWSMessage(t, ui)
	assert.Equal(t, "run_ended", msg.Type)

	// Both conns should now close from the server side.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, _, err := ui.Read(ctx)
	assert.Error(t, err, "UI read should fail after DropRun")

	_, _, err = runner.Read(ctx)
	assert.Error(t, err, "runner read should fail after DropRun")
}

func TestWsHub_BufferTruncationFlagsSnapshot(t *testing.T) {
	hub, base := hubTestSetup(t, "r1")

	runner := dialWS(t, base+"/runner")
	defer func() { _ = runner.Close(websocket.StatusNormalClosure, "") }()

	// First UI comes and goes so we can populate the buffer without
	// the test having to drain a zillion log messages.
	ui := dialWS(t, base+"/ui")
	_ = readWSMessage(t, ui)     // snapshot
	_ = readWSMessage(t, runner) // stream_on
	_ = ui.Close(websocket.StatusNormalClosure, "")
	_ = drainMessagesUntil(t, runner, "stream_off", time.Second)

	// Push more bytes than the buffer cap straight through the hub.
	overflow := maxLogBytesPerRun + 1024
	big := strings.Repeat("x", overflow)
	hub.appendAndFanOut("r1", []byte(big))

	// New UI should get a truncated snapshot.
	ui2 := dialWS(t, base+"/ui")
	defer func() { _ = ui2.Close(websocket.StatusNormalClosure, "") }()

	snap := readWSMessage(t, ui2)
	assert.Equal(t, "snapshot", snap.Type)
	assert.True(t, snap.Truncated, "snapshot should flag truncation after overflow")
	assert.Len(t, snap.Text, maxLogBytesPerRun, "snapshot text should be capped at the buffer size")
}
