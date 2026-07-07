package builder

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// datadirProgressInterval is how often the datadir-size progress line is logged
// during a build.
const datadirProgressInterval = 5 * time.Minute

// logDatadirProgress periodically logs the size of dir until the returned stop
// func is called, giving visible progress during a long build even when the
// underlying tool (e.g. state-actor) logs nothing itself. The stop func cancels
// the ticker and blocks until the logging goroutine has exited.
func logDatadirProgress(ctx context.Context, log logrus.FieldLogger, dir string, interval time.Duration) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				log.WithField("datadir_size", humanizeBytes(dirSize(dir))).
					Info("Building datadir (size growing)")
			}
		}
	}()

	return func() {
		cancel()
		wg.Wait()
	}
}

// humanizeBytes renders a byte count as a human-readable KiB/MiB/GiB string.
func humanizeBytes(b int64) string {
	const (
		kib = 1024
		mib = 1024 * kib
		gib = 1024 * mib
	)

	switch {
	case b >= gib:
		return fmt.Sprintf("%.1f GiB", float64(b)/float64(gib))
	case b >= mib:
		return fmt.Sprintf("%.1f MiB", float64(b)/float64(mib))
	case b >= kib:
		return fmt.Sprintf("%.1f KiB", float64(b)/float64(kib))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
