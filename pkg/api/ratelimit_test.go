package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

func TestExtractIP_IgnoresXFFWithoutTrustedProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.RemoteAddr = "203.0.113.5:54321"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")

	got := extractIP(req, nil)

	assert.Equal(t, "203.0.113.5", got,
		"with no trusted proxies configured, a client-supplied XFF header must never override RemoteAddr")
}

func TestExtractIP_HonorsXFFFromTrustedProxy(t *testing.T) {
	trusted := parseTrustedProxies([]string{"10.0.0.0/8"})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.RemoteAddr = "10.0.0.1:54321"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")

	got := extractIP(req, trusted)

	assert.Equal(t, "198.51.100.7", got,
		"when RemoteAddr is a trusted proxy, the XFF value it appended should be used")
}

func TestExtractIP_UsesRightmostHop(t *testing.T) {
	trusted := parseTrustedProxies([]string{"10.0.0.1"})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.RemoteAddr = "10.0.0.1:54321"
	// A client claiming an arbitrary left-most IP, followed by the hop our
	// trusted proxy actually observed and appended.
	req.Header.Set("X-Forwarded-For", "9.9.9.9, 198.51.100.7")

	got := extractIP(req, trusted)

	assert.Equal(t, "198.51.100.7", got,
		"only the right-most hop (appended by our trusted proxy) should be trusted, not attacker-supplied left-most entries")
}

func TestExtractIP_IgnoresXFFFromUntrustedDirectConnection(t *testing.T) {
	// trusted_proxies is configured, but this specific request bypassed the
	// proxy and connected directly - the XFF header must still be ignored.
	trusted := parseTrustedProxies([]string{"10.0.0.0/8"})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.RemoteAddr = "203.0.113.5:54321"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")

	got := extractIP(req, trusted)

	assert.Equal(t, "203.0.113.5", got,
		"a direct connection from outside the trusted proxy ranges must not be able to spoof its IP via XFF")
}

func TestParseTrustedProxies(t *testing.T) {
	networks := parseTrustedProxies([]string{
		"10.0.0.0/8",
		"192.168.1.1",
		"not-an-ip",
		"",
		"::1",
	})

	require.Len(t, networks, 3, "malformed/empty entries should be skipped, not error out")

	assert.True(t, isTrustedProxy("10.1.2.3", networks))
	assert.True(t, isTrustedProxy("192.168.1.1", networks))
	assert.False(t, isTrustedProxy("192.168.1.2", networks), "bare IP entries should match only that exact address")
	assert.True(t, isTrustedProxy("::1", networks))
	assert.False(t, isTrustedProxy("8.8.8.8", networks))
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
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

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

func spoofedIPFor(i int) string {
	return fmt.Sprintf("10.0.%d.1", i)
}
