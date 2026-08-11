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

// Throttle bounds how many requests run at once: a semaphore over the handler,
// shedding load instead of queueing it. It is an in-process concurrency
// limiter, not a rate limiter, and the two answer different questions: a rate
// limit says how often a client may come back, a concurrency limit says how
// much of this work the process can afford at the same moment. A rate limit of
// ten per minute still admits ten requests in the same second; ten concurrent
// slots do not, which is what matters in front of work that pays its cost per
// call - password hashing at tens of megabytes per verification is the classic
// case, where the limit turns a spike into a bounded memory ceiling.
//
// When every slot is busy the request is rejected immediately with the
// configured status rather than queued. The waiters themselves would be cheap
// to hold; what a queue actually does is pin each caller's connection while
// its deadline runs out, so the process ends up doing expensive work for
// clients that have already given up. An immediate rejection with Retry-After
// tells them honestly to come back later.
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
