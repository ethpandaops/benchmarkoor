package runner

import (
	"io"
	"sync"
	"testing"

	"github.com/sirupsen/logrus"
)

// TestRemoveHookConcurrentWithLogging guards against a data race in removeHook.
//
// In production, removeHook runs from a deferred call at the end of RunInstance
// while the container death monitor and log-streaming goroutines may still be
// logging on the same logger. Those goroutines run under the shared r.wg, which
// is only waited on in Stop(), so they are not guaranteed to have stopped when
// the defer fires.
//
// If removeHook mutates logger.Hooks in place without the logger's lock, that
// write races the locked map read in logrus's hook-firing path. Go turns a
// concurrent map read and write into an unrecoverable fatal error that takes
// the whole process down.
//
// Run with "go test -race": this test reports a race against an in-place
// implementation and passes against the ReplaceHooks-based one. It re-adds the
// hook on every iteration so a regression keeps writing the map under load and
// is caught reliably.
func TestRemoveHookConcurrentWithLogging(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	logger.SetLevel(logrus.DebugLevel)

	r := &runner{logger: logger}

	hook := &fileHook{writer: io.Discard, formatter: logger.Formatter}
	r.logger.AddHook(hook)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Stand in for the death monitor and log-streaming goroutines that are still
	// emitting log entries when removeHook runs. Each Warn makes logrus read
	// logger.Hooks under the logger's lock.
	for i := 0; i < 4; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for {
				select {
				case <-stop:
					return
				default:
					logger.WithField("k", "v").Warn("container exited unexpectedly")
				}
			}
		}()
	}

	// In production removeHook is called once via defer. Calling it many times
	// here, re-adding the hook each cycle, keeps an in-place implementation
	// writing the map at the same time as the readers above.
	for i := 0; i < 2000; i++ {
		r.removeHook(hook)
		r.logger.AddHook(hook)
	}

	close(stop)
	wg.Wait()

	// With no goroutines left logging, a final removeHook should leave the hook
	// gone. Reading logger.Hooks directly is safe here because every logger has
	// returned, so there is no concurrent access.
	r.removeHook(hook)

	for level, hooks := range logger.Hooks {
		for _, h := range hooks {
			if h == hook {
				t.Fatalf("hook still present at level %v after removeHook", level)
			}
		}
	}
}
