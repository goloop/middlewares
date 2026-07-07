package middlewares

import (
	"net/http"
	"time"
)

type timeoutConfig struct {
	message string
}

// TimeoutOption configures the Timeout middleware.
type TimeoutOption func(*timeoutConfig)

// WithTimeoutMessage sets the body sent when the deadline is exceeded.
func WithTimeoutMessage(msg string) TimeoutOption {
	return func(c *timeoutConfig) { c.message = msg }
}

// Timeout enforces a per-request deadline. It cancels the request context after
// d and, if the handler has not finished, replies with 503 Service Unavailable.
// It is built on http.TimeoutHandler, so the timeout reply and the handler's
// own writes are synchronized and cannot race.
//
// Timeout cannot stop a handler goroutine that ignores its context. Long
// handlers must observe r.Context().Done() themselves to actually stop working.
func Timeout(d time.Duration, opts ...TimeoutOption) Middleware {
	var cfg timeoutConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, d, cfg.message)
	}
}
