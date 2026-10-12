package podman

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestConnWithCtx_CancelsOnManagerDone is a regression test for the
// goroutine leak in every Podman call site that uses connWithCtx
// (including WaitForContainerExit, the long-lived one): the manager
// stopping (m.done closing) must cancel the derived connection context,
// not just leave any in-flight call running until the caller's own ctx
// eventually cancels.
func TestConnWithCtx_CancelsOnManagerDone(t *testing.T) {
	m := &manager{conn: context.Background(), done: make(chan struct{})}

	derived, cancel := m.connWithCtx(context.Background())
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

func TestConnWithCtx_CancelsOnParentContext(t *testing.T) {
	m := &manager{conn: context.Background(), done: make(chan struct{})}
	parentCtx, parentCancel := context.WithCancel(context.Background())

	derived, cancel := m.connWithCtx(parentCtx)
	defer cancel()

	parentCancel()

	select {
	case <-derived.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("derived context was not canceled after the parent context was canceled")
	}
}

func TestConnWithCtx_ExplicitCancelDoesNotLeakWatcher(t *testing.T) {
	m := &manager{conn: context.Background(), done: make(chan struct{})}

	_, cancel := m.connWithCtx(context.Background())
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

// TestManagerWgWait_ReturnsAfterDoneClosed mirrors the docker package's
// equivalent test: closing m.done (what Stop() does first) must cause
// every connWithCtx watcher to release so wg.Wait() returns promptly.
func TestManagerWgWait_ReturnsAfterDoneClosed(t *testing.T) {
	m := &manager{conn: context.Background(), done: make(chan struct{})}

	_, cancel := m.connWithCtx(context.Background())
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
