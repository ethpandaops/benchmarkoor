package api

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

// newTestLogger returns a logger that discards output but keeps a hook so
// tests can assert on what was logged.
func newTestLogger() (*logrus.Logger, *logtest.Hook) {
	log, hook := logtest.NewNullLogger()
	log.SetLevel(logrus.DebugLevel)

	return log, hook
}

func testTrustedProxies(t *testing.T, entries ...string) []*net.IPNet {
	t.Helper()

	log, _ := newTestLogger()

	return parseTrustedProxies(log, entries)
}

func TestExtractIP_IgnoresXFFWithoutTrustedProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.RemoteAddr = "203.0.113.5:54321"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")

	got := extractIP(req, nil)

	assert.Equal(t, "203.0.113.5", got,
		"with no trusted proxies configured, a client-supplied XFF header must never override RemoteAddr")
}

func TestExtractIP_HonorsXFFFromTrustedProxy(t *testing.T) {
	trusted := testTrustedProxies(t, "10.0.0.0/8")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.RemoteAddr = "10.0.0.1:54321"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")

	got := extractIP(req, trusted)

	assert.Equal(t, "198.51.100.7", got,
		"when RemoteAddr is a trusted proxy, the XFF value it appended should be used")
}

func TestExtractIP_UsesRightmostUntrustedHop(t *testing.T) {
	trusted := testTrustedProxies(t, "10.0.0.1")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.RemoteAddr = "10.0.0.1:54321"
	// A client claiming an arbitrary left-most IP, followed by the hop our
	// trusted proxy actually observed and appended.
	req.Header.Set("X-Forwarded-For", "9.9.9.9, 198.51.100.7")

	got := extractIP(req, trusted)

	assert.Equal(t, "198.51.100.7", got,
		"only the right-most hop (appended by our trusted proxy) should be trusted, not attacker-supplied left-most entries")
}

// TestExtractIP_SkipsTrustedHopsInChain covers a chain of proxies (a CDN in
// front of a load balancer). Taking the right-most entry unconditionally
// would key every request on the CDN's egress address, collapsing the whole
// user base into a single rate limit bucket.
func TestExtractIP_SkipsTrustedHopsInChain(t *testing.T) {
	trusted := testTrustedProxies(t, "10.0.0.0/8", "192.0.2.0/24")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.RemoteAddr = "10.0.0.1:54321"
	// Client, then the CDN egress our load balancer observed and appended.
	req.Header.Set("X-Forwarded-For", "198.51.100.7, 192.0.2.5")

	got := extractIP(req, trusted)

	assert.Equal(t, "198.51.100.7", got,
		"hops that are themselves trusted proxies must be skipped in favour of the first untrusted address")
}

func TestExtractIP_AllHopsTrustedFallsBackToRemoteAddr(t *testing.T) {
	trusted := testTrustedProxies(t, "10.0.0.0/8")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.RemoteAddr = "10.0.0.1:54321"
	req.Header.Set("X-Forwarded-For", "10.0.0.9, 10.0.0.8")

	got := extractIP(req, trusted)

	assert.Equal(t, "10.0.0.1", got,
		"with no untrusted hop to attribute, the connection address is the only honest key")
}

func TestExtractIP_MalformedHopsAreNeverUsedAsKeys(t *testing.T) {
	trusted := testTrustedProxies(t, "10.0.0.0/8")

	for _, xff := range []string{"unknown", "", "   ", "1.2.3.4, not-an-ip", "<script>"} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		req.RemoteAddr = "10.0.0.1:54321"
		req.Header.Set("X-Forwarded-For", xff)

		got := extractIP(req, trusted)

		assert.Equal(t, "10.0.0.1", got,
			"an unparseable hop (%q) breaks the chain and must fall back to RemoteAddr, never become a limiter key", xff)
	}
}

// TestExtractIP_PassthroughProxyCannotMintBuckets covers a trusted proxy
// that forwards the client's header verbatim instead of appending to it.
// Only parseable IPs are ever returned, so a caller can't mint a fresh
// limiter key per request out of arbitrary strings.
func TestExtractIP_PassthroughProxyCannotMintBuckets(t *testing.T) {
	trusted := testTrustedProxies(t, "10.0.0.0/8")

	seen := make(map[string]struct{}, 3)

	for _, spoof := range []string{"bucket-a", "bucket-b", "bucket-c"} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		req.RemoteAddr = "10.0.0.1:54321"
		req.Header.Set("X-Forwarded-For", spoof)

		seen[extractIP(req, trusted)] = struct{}{}
	}

	assert.Len(t, seen, 1,
		"non-IP X-Forwarded-For values must all collapse onto the connection address, not one bucket each")
}

func TestExtractIP_IgnoresXFFFromUntrustedDirectConnection(t *testing.T) {
	// trusted_proxies is configured, but this specific request bypassed the
	// proxy and connected directly - the XFF header must still be ignored.
	trusted := testTrustedProxies(t, "10.0.0.0/8")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.RemoteAddr = "203.0.113.5:54321"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")

	got := extractIP(req, trusted)

	assert.Equal(t, "203.0.113.5", got,
		"a direct connection from outside the trusted proxy ranges must not be able to spoof its IP via XFF")
}

func TestParseTrustedProxies(t *testing.T) {
	networks := testTrustedProxies(t,
		"10.0.0.0/8",
		"192.168.1.1",
		"not-an-ip",
		"",
		"::1",
	)

	require.Len(t, networks, 3, "malformed/empty entries should be skipped, not error out")

	assert.True(t, isTrustedProxy("10.1.2.3", networks))
	assert.True(t, isTrustedProxy("192.168.1.1", networks))
	assert.False(t, isTrustedProxy("192.168.1.2", networks), "bare IP entries should match only that exact address")
	assert.True(t, isTrustedProxy("::1", networks))
	assert.False(t, isTrustedProxy("8.8.8.8", networks))
}

// TestParseTrustedProxies_WarnsOnSkippedEntries guards the diagnosability of
// a typo: a skipped entry silently demotes a real proxy to untrusted, which
// puts every client behind it into one bucket.
func TestParseTrustedProxies_WarnsOnSkippedEntries(t *testing.T) {
	log, hook := newTestLogger()

	networks := parseTrustedProxies(log, []string{"10.0.0/8", "not-an-ip", "10.0.0.0/8"})

	require.Len(t, networks, 1)

	var warnings []string

	for _, e := range hook.AllEntries() {
		if e.Level == logrus.WarnLevel {
			warnings = append(warnings, e.Message)
		}
	}

	assert.Len(t, warnings, 2,
		"every skipped trusted_proxies entry must be logged, or a typo is undiagnosable")
}

func TestIsTrustedProxy_EmptyList(t *testing.T) {
	assert.False(t, isTrustedProxy("10.0.0.1", nil),
		"an empty trusted proxy list must never treat any address as trusted")
}

// TestRateLimitMiddleware_SpoofedXFFCannotBypassLimit is a regression test
// for the exploit verified live against a running server: with no
// trusted_proxies configured, sending a unique X-Forwarded-For value on
// every request must not grant a fresh rate-limit bucket per request.
func TestRateLimitMiddleware_SpoofedXFFCannotBypassLimit(t *testing.T) {
	log, _ := newTestLogger()

	s := &server{
		log: log,
		cfg: &config.APIConfig{
			Server: config.APIServerConfig{
				RateLimit: config.RateLimitConfig{Enabled: true},
			},
		},
	}

	limit := config.RateLimitTier{RequestsPerMinute: 5}
	handler := s.rateLimitMiddleware(limit)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) },
	))

	tripped := false

	for i := 0; i < 20; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		req.RemoteAddr = "203.0.113.5:54321"
		req.Header.Set("X-Forwarded-For", spoofedIPFor(i))

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code == http.StatusTooManyRequests {
			tripped = true

			break
		}
	}

	assert.True(t, tripped,
		"the limiter must trip within a small multiple of the configured cap even when every request carries a unique spoofed X-Forwarded-For value")
}

// TestRateLimitMiddleware_WarnsOnceOnUntrustedXFF covers the deployment that
// silently regressed: an API behind a reverse proxy with no trusted_proxies
// set now keys every client on the proxy address, so operators need a signal
// rather than unexplained 429s.
func TestRateLimitMiddleware_WarnsOnceOnUntrustedXFF(t *testing.T) {
	log, hook := newTestLogger()

	s := &server{
		log: log,
		cfg: &config.APIConfig{
			Server: config.APIServerConfig{
				RateLimit: config.RateLimitConfig{Enabled: true},
			},
		},
	}

	handler := s.rateLimitMiddleware(config.RateLimitTier{RequestsPerMinute: 100})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		req.RemoteAddr = "203.0.113.5:54321"
		req.Header.Set("X-Forwarded-For", "1.2.3.4")

		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	// A request without the header must not warn on its own.
	direct := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	direct.RemoteAddr = "203.0.113.6:54321"
	handler.ServeHTTP(httptest.NewRecorder(), direct)

	warnings := 0

	for _, e := range hook.AllEntries() {
		if e.Level == logrus.WarnLevel {
			warnings++
		}
	}

	assert.Equal(t, 1, warnings,
		"the untrusted-XFF warning must fire once, not once per request")
}

func spoofedIPFor(i int) string {
	return fmt.Sprintf("10.0.%d.1", i)
}
