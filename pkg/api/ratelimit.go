package api

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
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
	trustedProxies := parseTrustedProxies(s.cfg.Server.TrustedProxies)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractIP(r, trustedProxies)
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

// extractIP returns the client's IP address for rate-limiting purposes.
//
// X-Forwarded-For is only honored when the direct connection (RemoteAddr)
// comes from a configured trusted proxy - otherwise it's an arbitrary,
// attacker-controlled header that would let every request claim a fresh
// identity and bypass the limiter entirely. When trusted, the right-most
// entry in the header is used, since that's the hop our trusted proxy
// itself observed and appended - anything to its left could have been
// forged by the client.
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

	clientIP := strings.TrimSpace(hops[len(hops)-1])
	if clientIP == "" {
		return remoteIP
	}

	return clientIP
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
// granting broader trust than configured.
func parseTrustedProxies(proxies []string) []*net.IPNet {
	networks := make([]*net.IPNet, 0, len(proxies))

	for _, p := range proxies {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		if !strings.Contains(p, "/") {
			ip := net.ParseIP(p)
			if ip == nil {
				continue
			}

			bits := 32
			if ip.To4() == nil {
				bits = 128
			}

			p = fmt.Sprintf("%s/%d", p, bits)
		}

		_, network, err := net.ParseCIDR(p)
		if err != nil {
			continue
		}

		networks = append(networks, network)
	}

	return networks
}
