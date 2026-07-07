package middlewares

import (
	"net/http"
	"strconv"
)

type throttleConfig struct {
	status     int
	retryAfter int // seconds; 0 omits the Retry-After header
}

// ThrottleOption configures the Throttle middleware.
type ThrottleOption func(*throttleConfig)

// WithThrottleStatus sets the status returned when the limit is reached
// (default 503 Service Unavailable).
func WithThrottleStatus(code int) ThrottleOption {
	return func(c *throttleConfig) {
		if code != 0 {
			c.status = code
		}
	}
}

// WithThrottleRetryAfter sets the Retry-After header (in seconds) on a rejected
// request. Zero, the default, omits the header.
func WithThrottleRetryAfter(seconds int) ThrottleOption {
	return func(c *throttleConfig) { c.retryAfter = seconds }
}

// Throttle limits the number of requests processed concurrently to limit. When
// all slots are busy, further requests are rejected immediately with the
// configured status rather than being queued. This is an in-process concurrency
// limiter, not a distributed rate limiter.
//
// Throttle panics if limit is not positive, since that is a programming error.
func Throttle(limit int, opts ...ThrottleOption) Middleware {
	if limit <= 0 {
		panic("middlewares: Throttle limit must be positive")
	}
	cfg := throttleConfig{status: http.StatusServiceUnavailable}
	for _, opt := range opts {
		opt(&cfg)
	}

	tokens := make(chan struct{}, limit)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case tokens <- struct{}{}:
				defer func() { <-tokens }()
				next.ServeHTTP(w, r)
			default:
				if cfg.retryAfter > 0 {
					w.Header().Set("Retry-After", strconv.Itoa(cfg.retryAfter))
				}
				http.Error(w, http.StatusText(cfg.status), cfg.status)
			}
		})
	}
}
