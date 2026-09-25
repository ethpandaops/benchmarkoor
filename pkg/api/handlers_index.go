package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
	"github.com/ethpandaops/benchmarkoor/pkg/executor"
	"github.com/go-chi/chi/v5"
	"github.com/sirupsen/logrus"
)

// emptyJSONObject is spliced in for runs that have no (or malformed) steps
// JSON, preserving the historical behaviour where "steps" is always present.
var emptyJSONObject = json.RawMessage("{}")

// indexResponse is the /index payload. Field order mirrors the historical
// shape (discovery_path first, then the executor.IndexEntry fields).
type indexResponse struct {
	Generated int64        `json:"generated"`
	Entries   []indexEntry `json:"entries"`
}

type indexEntry struct {
	DiscoveryPath     string                  `json:"discovery_path"`
	RunID             string                  `json:"run_id"`
	Timestamp         int64                   `json:"timestamp"`
	TimestampEnd      int64                   `json:"timestamp_end,omitempty"`
	SuiteHash         string                  `json:"suite_hash,omitempty"`
	Instance          *executor.IndexInstance `json:"instance"`
	Tests             indexTestStats          `json:"tests"`
	Status            string                  `json:"status,omitempty"`
	TerminationReason string                  `json:"termination_reason,omitempty"`
	Metadata          json.RawMessage         `json:"metadata,omitempty"`

	// DeletionRequestedAt (unix seconds) is set while the run sits in the
	// deletion queue. DeletionError carries the last failed attempt.
	DeletionRequestedAt int64  `json:"deletion_requested_at,omitempty"`
	DeletionError       string `json:"deletion_error,omitempty"`
}

type indexTestStats struct {
	TestsTotal  int             `json:"tests_total"`
	TestsPassed int             `json:"tests_passed"`
	TestsFailed int             `json:"tests_failed"`
	Steps       json.RawMessage `json:"steps"`
}

// indexGzipLevel is the gzip level used for the cached /index body. Encoding
// happens once per runs generation rather than once per request, but a rebuild
// still lands on a live request, so this stays at the default. On a body of
// this shape BestCompression buys a couple of percent for several times the
// CPU, which is the wrong trade on a rebuild path that already reads the whole
// runs table.
const indexGzipLevel = gzip.DefaultCompression

// indexCacheEntry is a fully rendered /index response for one runs-table
// generation. The body, its gzip-encoded copy and the ETag they share are all
// built together and then served verbatim. Entries are immutable once stored,
// so readers may hold one without copying.
type indexCacheEntry struct {
	gen uint64
	// body is the marshaled JSON. gzipped is the same bytes gzip-encoded, or
	// nil if encoding failed; callers then fall back to body.
	body    []byte
	gzipped []byte
	etag    string
}

// handleIndex returns the aggregated index of all benchmark runs from all
// discovery paths. The response shape matches executor.Index with an
// additional "discovery_path" field on each entry.
//
// The full payload is O(number of runs) and the UI polls it periodically, so
// the response is cached and keyed by the store's runs generation: while no run
// is upserted or deleted nothing touches the database, nothing is re-encoded,
// and an unchanged client gets a 304 instead of the body.
func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	gen := s.indexStore.RunsGeneration()

	entry := s.cachedIndex(gen)
	if entry == nil {
		built, err := s.buildIndex(r.Context(), gen)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError,
				errorResponse{err.Error()})

			return
		}

		s.storeCachedIndex(built)

		entry = built
	}

	writeIndexResponse(w, r, entry)
}

// buildIndex renders the whole /index response for the given generation.
func (s *server) buildIndex(
	ctx context.Context, gen uint64,
) (*indexCacheEntry, error) {
	runs, err := s.indexStore.ListAllRuns(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing runs: %w", err)
	}

	// ListAllRuns already orders by timestamp descending, so no re-sort here.
	entries := make([]indexEntry, 0, len(runs))

	for i := range runs {
		run := &runs[i]

		// Splice the stored steps/metadata JSON straight through instead of
		// unmarshaling then re-marshaling. Malformed blobs fall back to the
		// historical defaults ("{}" for steps, omitted for metadata).
		steps := json.RawMessage(run.StepsJSON)
		if len(steps) == 0 || !json.Valid(steps) {
			steps = emptyJSONObject
		}

		var metadata json.RawMessage
		if run.MetadataJSON != "" && json.Valid([]byte(run.MetadataJSON)) {
			metadata = json.RawMessage(run.MetadataJSON)
		}

		var deletionRequestedAt int64
		if run.DeletionRequestedAt != nil {
			deletionRequestedAt = run.DeletionRequestedAt.Unix()
		}

		entries = append(entries, indexEntry{
			DiscoveryPath:     run.DiscoveryPath,
			RunID:             run.RunID,
			Timestamp:         run.Timestamp,
			TimestampEnd:      run.TimestampEnd,
			SuiteHash:         run.SuiteHash,
			Status:            run.Status,
			TerminationReason: run.TerminationReason,
			Instance: &executor.IndexInstance{
				ID:               run.InstanceID,
				Client:           run.Client,
				Image:            run.Image,
				RollbackStrategy: run.RollbackStrategy,
			},
			Tests: indexTestStats{
				TestsTotal:  run.TestsTotal,
				TestsPassed: run.TestsPassed,
				TestsFailed: run.TestsFailed,
				Steps:       steps,
			},
			Metadata:            metadata,
			DeletionRequestedAt: deletionRequestedAt,
			DeletionError:       run.DeletionError,
		})
	}

	body, err := json.Marshal(indexResponse{
		Generated: time.Now().Unix(),
		Entries:   entries,
	})
	if err != nil {
		return nil, fmt.Errorf("encoding index: %w", err)
	}

	return &indexCacheEntry{
		gen:     gen,
		body:    body,
		gzipped: gzipIndexBody(s.log, body),
		etag:    indexETag(body),
	}, nil
}

// writeIndexResponse serves a cached entry. It answers a matching
// If-None-Match with a 304 and otherwise writes the pre-encoded copy the
// client accepts. Setting Content-Encoding makes the compression middleware
// pass the bytes straight through instead of gzipping them a second time.
func writeIndexResponse(
	w http.ResponseWriter, r *http.Request, entry *indexCacheEntry,
) {
	header := w.Header()
	header.Set("ETag", entry.etag)
	header.Add("Vary", "Accept-Encoding")
	// The index changes whenever a run is indexed, so the client must
	// revalidate on every poll. Revalidation is a 304 rather than a
	// multi-megabyte transfer, which is the point.
	header.Set("Cache-Control", "no-cache")

	if etagMatches(r.Header.Get("If-None-Match"), entry.etag) {
		w.WriteHeader(http.StatusNotModified)

		return
	}

	body := entry.body

	if entry.gzipped != nil && acceptsGzip(r) {
		body = entry.gzipped

		header.Set("Content-Encoding", "gzip")
	}

	header.Set("Content-Type", "application/json")
	header.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)

	_, _ = w.Write(body)
}

// gzipIndexBody gzip-encodes the rendered body. It returns nil when encoding
// fails, which is not fatal: the caller then serves the plain body and lets
// the compression middleware handle it as before.
func gzipIndexBody(log logrus.FieldLogger, body []byte) []byte {
	// Real index bodies compress to well under a tenth of their size, so this
	// hint saves the buffer several doublings of a multi-megabyte allocation.
	buf := bytes.NewBuffer(make([]byte, 0, len(body)/8))

	writer, err := gzip.NewWriterLevel(buf, indexGzipLevel)
	if err != nil {
		log.WithError(err).Warn("Creating gzip writer for index response")

		return nil
	}

	if _, err := writer.Write(body); err != nil {
		log.WithError(err).Warn("Compressing index response")

		return nil
	}

	if err := writer.Close(); err != nil {
		log.WithError(err).Warn("Flushing compressed index response")

		return nil
	}

	return buf.Bytes()
}

// indexETag derives a strong validator from the rendered body. The body embeds
// its own "generated" timestamp, so hashing the content (rather than keying off
// the generation counter, which restarts with the process) is what keeps a 304
// honest across restarts.
func indexETag(body []byte) string {
	sum := sha256.Sum256(body)

	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

// etagMatches reports whether an If-None-Match header covers the given ETag.
// It handles the "*" wildcard, comma-separated lists and the weak prefix,
// since an intermediary may weaken a validator on the way back.
func etagMatches(ifNoneMatch, etag string) bool {
	if ifNoneMatch == "" {
		return false
	}

	for _, candidate := range strings.Split(ifNoneMatch, ",") {
		candidate = strings.TrimSpace(candidate)

		if candidate == "*" || strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}

	return false
}

// acceptsGzip reports whether the request allows a gzip-encoded response.
// "gzip;q=0" is an explicit refusal, so the quality value decides rather than
// the bare presence of the token.
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
			continue
		}

		_, quality, ok := strings.Cut(params, "q=")
		if !ok {
			return true
		}

		value, err := strconv.ParseFloat(strings.TrimSpace(quality), 64)

		return err != nil || value > 0
	}

	return false
}

// cachedIndex returns the cached response if it was built for the given runs
// generation, otherwise nil.
func (s *server) cachedIndex(gen uint64) *indexCacheEntry {
	s.indexCacheMu.Lock()
	defer s.indexCacheMu.Unlock()

	if s.indexCache != nil && s.indexCache.gen == gen {
		return s.indexCache
	}

	return nil
}

// storeCachedIndex records a freshly built response. A build is only allowed
// to replace the cache if its generation is at least as fresh, so a slow build
// for an older generation can't clobber a newer cached entry.
func (s *server) storeCachedIndex(entry *indexCacheEntry) {
	s.indexCacheMu.Lock()
	defer s.indexCacheMu.Unlock()

	if s.indexCache == nil || entry.gen >= s.indexCache.gen {
		s.indexCache = entry
	}
}

// handleSuiteStats returns suite statistics for a given suite hash.
// The response shape matches executor.SuiteStats (map[string]*TestDurations).
func (s *server) handleSuiteStats(w http.ResponseWriter, r *http.Request) {
	suiteHash := chi.URLParam(r, "hash")
	if suiteHash == "" {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"suite hash is required"})

		return
	}

	// Parse max_runs_per_client: default 30, clamp to [1, 200].
	maxRuns := 30
	if v := r.URL.Query().Get("max_runs_per_client"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			maxRuns = n
		}
	}

	maxRuns = max(1, min(200, maxRuns))

	durations, err := s.indexStore.ListTestStatsBySuiteRecent(
		r.Context(), suiteHash, maxRuns,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"listing test stats: " + err.Error()})

		return
	}

	// Group by test name and build the SuiteStats shape.
	stats := make(executor.SuiteStats, len(durations))

	for i := range durations {
		d := &durations[i]

		steps := &executor.RunDurationStepsStats{
			Setup: &executor.RunDurationStepStats{
				GasUsed:       d.SetupGasUsed,
				Time:          d.SetupTimeNs,
				RPCCallsCount: d.SetupRPCCallsCount,
				ResourceTotals: &executor.ResourceTotals{
					CPUUsec:        d.SetupResourceCPUUsec,
					MemoryDelta:    d.SetupResourceMemDelta,
					MemoryBytes:    d.SetupResourceMemBytes,
					DiskReadBytes:  d.SetupResourceDiskReadB,
					DiskWriteBytes: d.SetupResourceDiskWriteB,
					DiskReadIOPS:   d.SetupResourceDiskReadOps,
					DiskWriteIOPS:  d.SetupResourceDiskWriteOps,
				},
			},
			Test: &executor.RunDurationStepStats{
				GasUsed:       d.TestGasUsed,
				Time:          d.TestTimeNs,
				RPCCallsCount: d.TestRPCCallsCount,
				ResourceTotals: &executor.ResourceTotals{
					CPUUsec:        d.TestResourceCPUUsec,
					MemoryDelta:    d.TestResourceMemDelta,
					MemoryBytes:    d.TestResourceMemBytes,
					DiskReadBytes:  d.TestResourceDiskReadB,
					DiskWriteBytes: d.TestResourceDiskWriteB,
					DiskReadIOPS:   d.TestResourceDiskReadOps,
					DiskWriteIOPS:  d.TestResourceDiskWriteOps,
				},
			},
		}

		rd := &executor.RunDuration{
			ID:       d.RunID,
			Client:   d.Client,
			GasUsed:  d.TotalGasUsed,
			Time:     d.TotalTimeNs,
			RunStart: d.RunStart,
			RunEnd:   d.RunEnd,
			Steps:    steps,
		}

		if stats[d.TestName] == nil {
			stats[d.TestName] = &executor.TestDurations{
				Durations: make([]*executor.RunDuration, 0, 4),
			}
		}

		stats[d.TestName].Durations = append(
			stats[d.TestName].Durations, rd,
		)
	}

	// Sort durations within each test by time_ns descending.
	for _, td := range stats {
		sort.Slice(td.Durations, func(i, j int) bool {
			return td.Durations[i].Time > td.Durations[j].Time
		})
	}

	writeJSON(w, http.StatusOK, stats)
}

// handleQueryRuns handles PostgREST-style queries against the runs table.
func (s *server) handleQueryRuns(w http.ResponseWriter, r *http.Request) {
	params, err := indexstore.ParseQueryParams(
		r.URL.Query(), indexstore.AllowedRunColumns(),
	)
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{err.Error()})

		return
	}

	params.CountExact = strings.Contains(
		r.Header.Get("Prefer"), "count=exact",
	)

	result, err := s.indexStore.QueryRuns(r.Context(), params)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"querying runs: " + err.Error()})

		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleQueryTestStats handles PostgREST-style queries against the
// test_stats table.
func (s *server) handleQueryTestStats(
	w http.ResponseWriter, r *http.Request,
) {
	params, err := indexstore.ParseQueryParams(
		r.URL.Query(), indexstore.AllowedTestStatColumns(),
	)
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{err.Error()})

		return
	}

	params.CountExact = strings.Contains(
		r.Header.Get("Prefer"), "count=exact",
	)

	result, err := s.indexStore.QueryTestStats(r.Context(), params)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"querying test stats: " + err.Error()})

		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleQuerySuites handles PostgREST-style queries against the suites
// table.
func (s *server) handleQuerySuites(
	w http.ResponseWriter, r *http.Request,
) {
	params, err := indexstore.ParseQueryParams(
		r.URL.Query(), indexstore.AllowedSuiteColumns(),
	)
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{err.Error()})

		return
	}

	params.CountExact = strings.Contains(
		r.Header.Get("Prefer"), "count=exact",
	)

	result, err := s.indexStore.QuerySuites(r.Context(), params)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"querying suites: " + err.Error()})

		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleQueryTestStatsBlockLogs handles PostgREST-style queries against
// the test_stats_block_logs table.
func (s *server) handleQueryTestStatsBlockLogs(
	w http.ResponseWriter, r *http.Request,
) {
	params, err := indexstore.ParseQueryParams(
		r.URL.Query(), indexstore.AllowedTestStatsBlockLogColumns(),
	)
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{err.Error()})

		return
	}

	params.CountExact = strings.Contains(
		r.Header.Get("Prefer"), "count=exact",
	)

	result, err := s.indexStore.QueryTestStatsBlockLogs(
		r.Context(), params,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{
				"querying test stats block logs: " + err.Error(),
			})

		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleRunIndexer triggers an immediate indexing pass. It returns 409 if
// an indexing pass is already in progress.
func (s *server) handleRunIndexer(w http.ResponseWriter, r *http.Request) {
	if s.indexer == nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"indexing is not enabled"})

		return
	}

	if started := s.indexer.RunNow(); !started {
		writeJSON(w, http.StatusConflict, map[string]string{
			"status":  "already_running",
			"message": "Indexing pass already in progress",
		})

		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status":  "started",
		"message": "Indexing pass started",
	})
}
