package api

import (
	"net"
	"net/http"
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

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractIP(r)
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

// extractIP returns the client's IP address from the request.
func extractIP(r *http.Request) string {
	// Check X-Forwarded-For first (common with reverse proxies).
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the chain.
		if idx := len(xff); idx > 0 {
			for i, c := range xff {
				if c == ',' {
					return xff[:i]
				}
			}

			return xff
		}
	}

	// Fall back to RemoteAddr.
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return ip
}
