package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

// cacheMeta stores the HTTP validators of a cached remote file so subsequent
// runs can decide, via a cheap HEAD request, whether the cache is still
// current or the origin has published a different artifact.
type cacheMeta struct {
	URL           string `json:"url"`
	ETag          string `json:"etag,omitempty"`
	LastModified  string `json:"last_modified,omitempty"`
	ContentLength int64  `json:"content_length,omitempty"`
}

// fetchResult reports the outcome of a validated fetch.
type fetchResult struct {
	Path    string // local path to the validated cache copy
	Changed bool   // true if the file was (re-)downloaded during this call
}

// fetchCached resolves a URL to a cached local path, validating any
// existing cached copy against the origin via a HEAD request before
// reusing it. If the cached copy is still current (HTTP validators
// match) it is reused; otherwise it is re-downloaded.
//
// When the HEAD request succeeds with no validators (no ETag / no
// Last-Modified and no Content-Length), the cache is reused with a
// warning — we have nothing to compare. When the HEAD request fails
// outright we also fall back to the cache with a warning so a transient
// network blip doesn't force a multi-hundred-MiB re-download.
//
// The cache path layout is:
//
//	<cacheDir>/<prefix>-<sha256(url)[:16]>      the file
//	<cacheDir>/<prefix>-<sha256(url)[:16]>.meta the validators sidecar
//
// `rawURL` is the URL written into the sidecar and used for logging.
// `downloadURL` is what we actually hit (may differ when a GitHub
// browser URL has been rewritten to an API URL).
func fetchCached(
	ctx context.Context,
	log logrus.FieldLogger,
	rawURL, downloadURL, bearerToken, cacheDir, prefix string,
) (*fetchResult, error) {
	if cacheDir == "" {
		cacheDir = os.TempDir()
	}

	cachedPath := cachePath(cacheDir, prefix, rawURL)
	metaPath := cachedPath + ".meta"

	fileExists := false
	if _, err := os.Stat(cachedPath); err == nil {
		fileExists = true
	}

	// Probe the origin for current validators. Failure is non-fatal when a
	// cache copy exists — better to serve slightly stale than to fail the
	// run on a network blip.
	remoteMeta, headErr := headMeta(ctx, downloadURL, bearerToken)

	if fileExists && headErr == nil {
		cachedMeta, readErr := readCacheMeta(metaPath)
		if readErr == nil && metaMatches(cachedMeta, remoteMeta) {
			log.WithFields(logrus.Fields{
				"url":  rawURL,
				"path": cachedPath,
			}).Info("Using cached file (validators match)")

			return &fetchResult{Path: cachedPath}, nil
		}

		if readErr == nil && !hasValidators(remoteMeta) {
			log.WithFields(logrus.Fields{
				"url":  rawURL,
				"path": cachedPath,
			}).Warn("Origin did not return ETag/Last-Modified; using cached file without revalidation")

			return &fetchResult{Path: cachedPath}, nil
		}

		if readErr != nil {
			log.WithFields(logrus.Fields{
				"url":  rawURL,
				"path": cachedPath,
			}).Info("Cache sidecar missing; re-downloading to refresh validators")
		} else {
			log.WithFields(logrus.Fields{
				"url":     rawURL,
				"path":    cachedPath,
				"changed": describeChange(cachedMeta, remoteMeta),
			}).Info("Origin changed; re-downloading")
		}
	} else if fileExists && headErr != nil {
		log.WithFields(logrus.Fields{
			"url":   rawURL,
			"path":  cachedPath,
			"error": headErr.Error(),
		}).Warn("HEAD failed; using cached file without revalidation")

		return &fetchResult{Path: cachedPath}, nil
	}

	// Either the cache is absent, stale, or unusable. Download a fresh copy.
	if headErr != nil {
		// We'll do our best without validators.
		log.WithFields(logrus.Fields{
			"url":   rawURL,
			"error": headErr.Error(),
		}).Warn("Could not probe origin before download")
	}

	log.WithField("url", rawURL).Info("Downloading file")

	tmpPath := cachedPath + ".tmp"

	if err := os.MkdirAll(filepath.Dir(cachedPath), 0755); err != nil {
		return nil, fmt.Errorf("creating cache directory: %w", err)
	}

	if err := downloadToFile(ctx, downloadURL, tmpPath, bearerToken, log); err != nil {
		_ = os.Remove(tmpPath)

		return nil, err
	}

	if err := os.Rename(tmpPath, cachedPath); err != nil {
		_ = os.Remove(tmpPath)

		return nil, fmt.Errorf("caching file: %w", err)
	}

	// Persist the validators we just observed (if any). If the HEAD
	// failed, still write a minimal sidecar with the URL so subsequent
	// runs treat the cache as present-but-unvalidated.
	meta := cacheMeta{URL: rawURL}
	if remoteMeta != nil {
		meta.ETag = remoteMeta.ETag
		meta.LastModified = remoteMeta.LastModified
		meta.ContentLength = remoteMeta.ContentLength
	}

	if err := writeCacheMeta(metaPath, &meta); err != nil {
		log.WithError(err).Warn("Failed to write cache sidecar")
	}

	return &fetchResult{Path: cachedPath, Changed: true}, nil
}

// cachePath returns the stable on-disk path for a given URL + prefix.
func cachePath(cacheDir, prefix, key string) string {
	hash := sha256.Sum256([]byte(key))
	return filepath.Join(cacheDir, prefix+"-"+hex.EncodeToString(hash[:8]))
}

// headMeta sends a HEAD request and extracts the validators we care about.
func headMeta(ctx context.Context, url, bearerToken string) (*cacheMeta, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating HEAD request: %w", err)
	}

	applyAuth(req, bearerToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HEAD %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HEAD %s: HTTP %d", url, resp.StatusCode)
	}

	return &cacheMeta{
		URL:           url,
		ETag:          strings.TrimSpace(resp.Header.Get("ETag")),
		LastModified:  strings.TrimSpace(resp.Header.Get("Last-Modified")),
		ContentLength: resp.ContentLength,
	}, nil
}

// metaMatches returns true when the cached validators match the origin
// response. At least one validator must be present and equal for a match;
// a bare Content-Length match is not sufficient (it's too common for
// unrelated rebuilds to produce identical sizes).
func metaMatches(cached, remote *cacheMeta) bool {
	if cached == nil || remote == nil {
		return false
	}

	if cached.ETag != "" && remote.ETag != "" {
		return cached.ETag == remote.ETag
	}

	if cached.LastModified != "" && remote.LastModified != "" {
		if cached.LastModified == remote.LastModified {
			// Back up with Content-Length if both sides have it — some CDNs
			// reuse Last-Modified across content revisions.
			if cached.ContentLength > 0 && remote.ContentLength > 0 {
				return cached.ContentLength == remote.ContentLength
			}

			return true
		}

		return false
	}

	return false
}

// hasValidators reports whether the remote gave us anything we can use to
// detect content changes beyond Content-Length (which is too weak alone).
func hasValidators(m *cacheMeta) bool {
	return m != nil && (m.ETag != "" || m.LastModified != "")
}

// describeChange produces a compact log field value describing which
// validator differs between the cached and remote metadata.
func describeChange(cached, remote *cacheMeta) string {
	if cached == nil || remote == nil {
		return "unknown"
	}

	switch {
	case cached.ETag != remote.ETag:
		return fmt.Sprintf("etag %q→%q", cached.ETag, remote.ETag)
	case cached.LastModified != remote.LastModified:
		return fmt.Sprintf("last_modified %q→%q", cached.LastModified, remote.LastModified)
	case cached.ContentLength != remote.ContentLength:
		return fmt.Sprintf("content_length %d→%d", cached.ContentLength, remote.ContentLength)
	default:
		return "no-validators-match"
	}
}

// readCacheMeta loads a sidecar JSON file.
func readCacheMeta(path string) (*cacheMeta, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is under our cache dir
	if err != nil {
		return nil, err
	}

	var m cacheMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing cache sidecar %q: %w", path, err)
	}

	return &m, nil
}

// writeCacheMeta atomically writes the sidecar JSON next to the cached file.
func writeCacheMeta(path string, meta *cacheMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling cache sidecar: %w", err)
	}

	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("writing cache sidecar: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("renaming cache sidecar: %w", err)
	}

	return nil
}
