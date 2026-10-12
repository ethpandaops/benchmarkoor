package docker

import (
	"context"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCtxWithDone_CancelsOnManagerDone is a regression test for the
// WaitForContainerExit goroutine leak: the manager stopping (m.done
// closing) must cancel any in-flight call derived via ctxWithDone, not
// just leave it running until the caller's own ctx eventually cancels.
func TestCtxWithDone_CancelsOnManagerDone(t *testing.T) {
	m := &manager{done: make(chan struct{})}

	derived, cancel := m.ctxWithDone(context.Background())
	defer cancel()

	select {
	case <-derived.Done():
		t.Fatal("derived context should not be done before m.done closes")
	default:
	}

	close(m.done)

	select {
	case <-derived.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("derived context was not canceled after m.done closed")
	}

	assert.ErrorIs(t, derived.Err(), context.Canceled)
}

func TestCtxWithDone_CancelsOnParentContext(t *testing.T) {
	m := &manager{done: make(chan struct{})}
	parentCtx, parentCancel := context.WithCancel(context.Background())

	derived, cancel := m.ctxWithDone(parentCtx)
	defer cancel()

	parentCancel()

	select {
	case <-derived.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("derived context was not canceled after the parent context was canceled")
	}
}

func TestCtxWithDone_ExplicitCancelDoesNotLeakWatcher(t *testing.T) {
	m := &manager{done: make(chan struct{})}

	_, cancel := m.ctxWithDone(context.Background())
	cancel()

	waited := make(chan struct{})

	go func() {
		m.wg.Wait()
		close(waited)
	}()

	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher goroutine did not exit after its own cancel func was called")
	}
}

// TestManagerWgWait_ReturnsAfterDoneClosed is a regression test for the
// core NM-08 claim: previously, closing m.done (what Stop() does first)
// did not cause any tracked goroutine to release, so m.wg.Wait() could
// block past what a caller would reasonably expect from Stop().
func TestManagerWgWait_ReturnsAfterDoneClosed(t *testing.T) {
	m := &manager{done: make(chan struct{})}

	_, cancel := m.ctxWithDone(context.Background())
	defer cancel()

	stopped := make(chan struct{})

	go func() {
		close(m.done)
		m.wg.Wait()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("wg.Wait() did not return promptly after m.done closed")
	}
}

func TestNewManager_WgAndDoneAreWired(t *testing.T) {
	// Sanity check that the fields ctxWithDone depends on are actually
	// initialized by the constructor used in production.
	mgr, err := NewManager(logrus.New())
	require.NoError(t, err)

	m, ok := mgr.(*manager)
	require.True(t, ok)

	require.NotNil(t, m.done)
}
