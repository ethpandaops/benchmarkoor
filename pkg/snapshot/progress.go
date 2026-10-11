package snapshot

import (
	"time"

	"github.com/sirupsen/logrus"
)

// ProgressEvery is how much of the archive streams between progress lines: a
// 1 TB export runs for hours and would otherwise log nothing after its start.
const ProgressEvery int64 = 32 << 30

// progress is an io.Writer that reports every `every` bytes written through it.
type progress struct {
	every  int64
	report func(bytes int64, elapsed time.Duration)
	start  time.Time
	total  int64
	next   int64
}

func newProgress(log logrus.FieldLogger, every int64) *progress {
	return &progress{every: every, next: every, start: time.Now(), report: func(bytes int64, elapsed time.Duration) {
		log.WithFields(logrus.Fields{
			"streamed_bytes": bytes,
			"elapsed":        elapsed.Round(time.Second).String(),
			"mb_per_s":       int64(float64(bytes) / elapsed.Seconds() / 1e6),
		}).Info("Streaming datadir")
	}}
}

func (p *progress) Write(b []byte) (int, error) {
	p.total += int64(len(b))
	for p.total >= p.next {
		p.report(p.total, time.Since(p.start))
		p.next += p.every
	}

	return len(b), nil
}
