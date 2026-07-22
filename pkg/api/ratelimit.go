package api

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

const (
	rateLimitCleanupInterval = 5 * time.Minute
	rateLimitEntryTTL        = 10 * time.Minute
)

type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type rateLimiterMap struct {
	mu       sync.Mutex
	limiters map[string]*ipLimiter
	rps      rate.Limit
	burst    int
}

func newRateLimiterMap(requestsPerMinute int) *rateLimiterMap {
	rps := rate.Limit(float64(requestsPerMinute) / 60.0)

	rl := &rateLimiterMap{
		limiters: make(map[string]*ipLimiter, 64),
		rps:      rps,
		burst:    requestsPerMinute, // Allow burst up to the per-minute limit.
	}

	go rl.cleanup()

	return rl
}

func (rl *rateLimiterMap) getLimiter(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	entry, exists := rl.limiters[ip]
	if !exists {
		limiter := rate.NewLimiter(rl.rps, rl.burst)
		rl.limiters[ip] = &ipLimiter{
			limiter:  limiter,
			lastSeen: time.Now(),
		}

		return limiter
	}

	entry.lastSeen = time.Now()

	return entry.limiter
}

func (rl *rateLimiterMap) cleanup() {
	ticker := time.NewTicker(rateLimitCleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()

		for ip, entry := range rl.limiters {
			if time.Since(entry.lastSeen) > rateLimitEntryTTL {
				delete(rl.limiters, ip)
			}
		}

		rl.mu.Unlock()
	}
}

// rateLimitMiddleware returns a per-IP rate limiting middleware for
// the given tier configuration.
func (s *server) rateLimitMiddleware(
	tier config.RateLimitTier,
) func(http.Handler) http.Handler {
	limiterMap := newRateLimiterMap(tier.RequestsPerMinute)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.warnOnUntrustedForwardedFor(r)

			ip := extractIP(r, s.trustedProxies)
			limiter := limiterMap.getLimiter(ip)

			if !limiter.Allow() {
				writeJSON(w, http.StatusTooManyRequests,
					errorResponse{"rate limit exceeded"})

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// warnOnUntrustedForwardedFor logs once if requests arrive carrying an
// X-Forwarded-For header while no trusted proxies are configured. That
// combination means a reverse proxy is almost certainly in front of the API
// but isn't declared, so every client behind it is rate limited as a single
// address - a silent lockout that is otherwise very hard to diagnose.
func (s *server) warnOnUntrustedForwardedFor(r *http.Request) {
	if len(s.trustedProxies) > 0 || r.Header.Get("X-Forwarded-For") == "" {
		return
	}

	s.xffWarnOnce.Do(func() {
		s.log.Warn(
			"Requests carry X-Forwarded-For but api.server.trusted_proxies " +
				"is empty, so rate limiting keys on the direct connection " +
				"address. If a reverse proxy fronts this API, list every " +
				"proxy in the chain under trusted_proxies - otherwise all " +
				"clients behind it share a single rate limit bucket.",
		)
	})
}

// extractIP returns the client's IP address for rate-limiting purposes.
//
// X-Forwarded-For is only honored when the direct connection (RemoteAddr)
// comes from a configured trusted proxy - otherwise it's an arbitrary,
// attacker-controlled header that would let every request claim a fresh
// identity and bypass the limiter entirely.
//
// When trusted, the header is walked right-to-left and the first entry that
// is not itself a trusted proxy is returned: with a chain of proxies (say a
// CDN in front of a load balancer) the right-most entries are the hops our
// own infrastructure appended, and only the first untrusted address below
// them is attributable. Anything further left could have been forged by the
// client, so a malformed or exhausted chain falls back to RemoteAddr rather
// than trusting a value we can't vouch for. Every candidate must parse as an
// IP, so arbitrary strings never become limiter keys.
func extractIP(r *http.Request, trustedProxies []*net.IPNet) string {
	remoteIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteIP = r.RemoteAddr
	}

	if !isTrustedProxy(remoteIP, trustedProxies) {
		return remoteIP
	}

	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return remoteIP
	}

	hops := strings.Split(xff, ",")

	for i := len(hops) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(hops[i])

		if net.ParseIP(hop) == nil {
			// A hop we can't parse breaks the chain: everything further
			// left is unverifiable, so fall back to the connection address.
			break
		}

		if !isTrustedProxy(hop, trustedProxies) {
			return hop
		}
	}

	return remoteIP
}

// isTrustedProxy reports whether ip falls within any of the configured
// trusted proxy networks.
func isTrustedProxy(ip string, trustedProxies []*net.IPNet) bool {
	if len(trustedProxies) == 0 {
		return false
	}

	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}

	for _, network := range trustedProxies {
		if network.Contains(parsed) {
			return true
		}
	}

	return false
}

// parseTrustedProxies parses a list of bare IPs or CIDR ranges into
// networks suitable for membership checks. Bare IPs are treated as
// single-address networks. Entries that fail to parse are skipped, which
// fails closed: a malformed entry simply never matches, rather than
// granting broader trust than configured. Skipped entries are logged at
// warn level, since a typo silently demotes a real proxy to untrusted and
// collapses everyone behind it into one rate limit bucket.
func parseTrustedProxies(log logrus.FieldLogger, proxies []string) []*net.IPNet {
	networks := make([]*net.IPNet, 0, len(proxies))

	for _, p := range proxies {
		entry := strings.TrimSpace(p)
		if entry == "" {
			continue
		}

		cidr := entry

		if !strings.Contains(cidr, "/") {
			ip := net.ParseIP(cidr)
			if ip == nil {
				log.WithField("entry", entry).Warn(
					"Ignoring api.server.trusted_proxies entry: not a valid IP or CIDR range",
				)

				continue
			}

			bits := 32
			if ip.To4() == nil {
				bits = 128
			}

			cidr = fmt.Sprintf("%s/%d", cidr, bits)
		}

		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			log.WithField("entry", entry).WithError(err).Warn(
				"Ignoring api.server.trusted_proxies entry: not a valid IP or CIDR range",
			)

			continue
		}

		networks = append(networks, network)
	}

	return networks
}
