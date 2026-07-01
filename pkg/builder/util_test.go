package builder

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTailBufferConcurrent exercises the case the mutex guards: the container
// log-streaming goroutine keeps Write()-ing while the caller reads String() on
// the error path (RunInitContainer doesn't join the streaming goroutine). Run
// with -race to catch a regression.
func TestTailBufferConcurrent(t *testing.T) {
	const maxBytes = 64

	tb := newTailBuffer(maxBytes)

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()

		for i := 0; i < 2000; i++ {
			_, _ = tb.Write([]byte("a streamed container log line\n"))
		}
	}()

	for i := 0; i < 2000; i++ {
		_ = tb.String()
	}

	wg.Wait()

	// The retained tail is bounded by max regardless of how much was written.
	assert.LessOrEqual(t, len(tb.String()), maxBytes)
}
