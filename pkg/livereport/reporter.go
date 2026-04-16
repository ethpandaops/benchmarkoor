// Package livereport implements the runner-side reporter that streams
// status snapshots to a benchmarkoor API instance during a run.
package livereport

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/sirupsen/logrus"
)

// Snapshotter returns the current state of a run as a LiveRunReport.
// Called by the reporter on every tick (and on demand via ReportNow).
// The DiscoveryPath / Token / etc. are filled in by the reporter, so the
// snapshot only needs to populate the fields that change over time.
type Snapshotter func() indexstore.LiveRunReport

// Reporter periodically posts run status snapshots to the API.
type Reporter interface {
	Start(ctx context.Context)
	Stop()
	ReportNow(ctx context.Context)
}

// New creates a Reporter. The snapshotter is invoked on every tick to
// produce the report payload. The reporter does not own the snapshotter's
// lifecycle.
func New(log logrus.FieldLogger, cfg *config.LiveReportingConfig, snap Snapshotter) Reporter {
	return &reporter{
		log:  log.WithField("component", "livereport"),
		cfg:  cfg,
		snap: snap,
		http: &http.Client{Timeout: cfg.GetTimeout()},
		done: make(chan struct{}),
	}
}

type reporter struct {
	log  logrus.FieldLogger
	cfg  *config.LiveReportingConfig
	snap Snapshotter
	http *http.Client

	done   chan struct{}
	wg     sync.WaitGroup
	closed bool
	mu     sync.Mutex
}

// Start launches the background reporting loop. Safe to call only once.
func (r *reporter) Start(ctx context.Context) {
	r.wg.Add(1)

	go func() {
		defer r.wg.Done()

		// Send an initial report immediately so the row appears in the API
		// quickly rather than waiting a full interval.
		r.report(ctx)

		for {
			delay := r.nextDelay()

			timer := time.NewTimer(delay)

			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-r.done:
				timer.Stop()
				return
			case <-timer.C:
				r.report(ctx)
			}
		}
	}()
}

// Stop sends one final report synchronously (with a short timeout) and
// then signals the background loop to exit. Idempotent.
func (r *reporter) Stop() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}

	r.closed = true
	r.mu.Unlock()

	// Final synchronous report so the terminal status reaches the API.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	r.report(ctx)
	cancel()

	close(r.done)
	r.wg.Wait()
}

// ReportNow triggers an immediate (out-of-band) report. Called by the
// runner at key transitions like run start and run end.
func (r *reporter) ReportNow(ctx context.Context) {
	r.report(ctx)
}

// report builds a report from the snapshotter and posts it. Errors are
// logged at WARN and swallowed — the reporter is best-effort.
func (r *reporter) report(ctx context.Context) {
	report := r.snap()
	report.DiscoveryPath = r.cfg.DiscoveryPath

	body, err := json.Marshal(report)
	if err != nil {
		r.log.WithError(err).Warn("Failed to marshal live report")
		return
	}

	// Gzip the body. The per-test gas map carried in the snapshot scales
	// with suite size, so this matters for large suites; the overhead is
	// negligible for small ones. The API has matching middleware to
	// inflate the request body transparently.
	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(body); err != nil {
		r.log.WithError(err).Warn("Failed to gzip live report")
		return
	}

	if err := gz.Close(); err != nil {
		r.log.WithError(err).Warn("Failed to finalize gzipped live report")
		return
	}

	url := strings.TrimRight(r.cfg.Endpoint, "/") + "/api/v1/ingest/runs"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		r.log.WithError(err).Warn("Failed to build live report request")
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Authorization", "Bearer "+r.cfg.Token)

	resp, err := r.http.Do(req)
	if err != nil {
		r.log.WithError(err).WithField("url", url).
			Warn("Failed to post live report")
		return
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode >= 300 {
		r.log.WithFields(logrus.Fields{
			"status": resp.StatusCode,
			"url":    url,
		}).Warn("Live report API returned non-success status")
		return
	}

	r.log.WithFields(logrus.Fields{
		"run_id": report.RunID,
		"status": report.Status,
	}).Debug("Live report sent")
}

// nextDelay returns the wait duration before the next report, applying
// jitter so multiple runners don't all report simultaneously. Result is
// always in the (0, 2*base) range.
func (r *reporter) nextDelay() time.Duration {
	base := r.cfg.GetInterval()
	frac := r.cfg.GetJitterFraction()

	if frac <= 0 {
		return base
	}

	// Multiplier in [1-frac, 1+frac).
	mul := 1 + (rand.Float64()*2-1)*frac
	if mul <= 0 {
		mul = 0.1
	}

	d := time.Duration(float64(base) * mul)
	if d <= 0 {
		return base
	}

	return d
}

// ensure Reporter compile-time check
var _ Reporter = (*reporter)(nil)
