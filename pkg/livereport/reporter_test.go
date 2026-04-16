package livereport

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReporter_PostsOnStartAndStop(t *testing.T) {
	var (
		count    atomic.Int64
		lastBody atomic.Value
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		require.Equal(t, "gzip", r.Header.Get("Content-Encoding"))

		gz, err := gzip.NewReader(r.Body)
		require.NoError(t, err)
		defer func() { _ = gz.Close() }()

		body, err := io.ReadAll(gz)
		require.NoError(t, err)
		lastBody.Store(body)
		count.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	cfg := &config.LiveReportingConfig{
		Enabled:        true,
		Endpoint:       srv.URL,
		Token:          "secret",
		DiscoveryPath:  "dp/test",
		Interval:       "1m",
		JitterFraction: 0,
	}

	log := logrus.New()
	log.SetLevel(logrus.WarnLevel)

	var status atomic.Value
	status.Store("running")

	snap := func() indexstore.LiveRunReport {
		return indexstore.LiveRunReport{
			RunID:  "run-1",
			Status: status.Load().(string),
		}
	}

	r := New(log, cfg, snap)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r.Start(ctx)

	// Wait for the initial report.
	require.Eventually(t, func() bool { return count.Load() >= 1 }, 2*time.Second, 10*time.Millisecond)

	status.Store("completed")
	r.Stop()

	// Stop must trigger a final synchronous report carrying the latest status.
	require.GreaterOrEqual(t, count.Load(), int64(2))

	var got indexstore.LiveRunReport
	require.NoError(t, json.Unmarshal(lastBody.Load().([]byte), &got))
	assert.Equal(t, "completed", got.Status)
	assert.Equal(t, "dp/test", got.DiscoveryPath)
}

func TestReporter_ContinuesOnHTTPError(t *testing.T) {
	var count atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := &config.LiveReportingConfig{
		Enabled:       true,
		Endpoint:      srv.URL,
		Token:         "x",
		DiscoveryPath: "dp",
		Interval:      "1m",
	}

	r := New(logrus.New(), cfg, func() indexstore.LiveRunReport {
		return indexstore.LiveRunReport{RunID: "x", Status: "running"}
	})
	r.Start(context.Background())

	require.Eventually(t, func() bool { return count.Load() >= 1 }, 2*time.Second, 10*time.Millisecond)

	// Stop should still succeed (and trigger its own final report attempt).
	r.Stop()

	assert.GreaterOrEqual(t, count.Load(), int64(2))
}

func TestReporter_NextDelayJitter(t *testing.T) {
	cfg := &config.LiveReportingConfig{
		Interval:       "1m",
		JitterFraction: 0.2,
	}
	r := &reporter{cfg: cfg}

	for i := 0; i < 50; i++ {
		d := r.nextDelay()
		assert.GreaterOrEqual(t, d, time.Duration(float64(time.Minute)*0.8))
		assert.LessOrEqual(t, d, time.Duration(float64(time.Minute)*1.2))
	}
}

func TestReporter_NextDelayNoJitter(t *testing.T) {
	// Negative jitter fraction = disabled.
	cfg := &config.LiveReportingConfig{Interval: "30s", JitterFraction: -1}
	r := &reporter{cfg: cfg}
	assert.Equal(t, 30*time.Second, r.nextDelay())
}
