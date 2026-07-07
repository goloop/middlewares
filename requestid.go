package middlewares

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// DefaultRequestIDHeader is the header read and written by RequestID unless
// overridden with WithRequestIDHeader.
const DefaultRequestIDHeader = "X-Request-ID"

// maxRequestIDLen bounds an accepted inbound identifier, so a hostile client
// cannot push arbitrarily long values into logs and headers.
const maxRequestIDLen = 128

type requestIDConfig struct {
	header       string
	generate     func() string
	trustInbound bool
}

// RequestIDOption configures the RequestID middleware.
type RequestIDOption func(*requestIDConfig)

// WithRequestIDHeader sets the header used to read and write the identifier.
func WithRequestIDHeader(name string) RequestIDOption {
	return func(c *requestIDConfig) {
		if name != "" {
			c.header = name
		}
	}
}

// WithRequestIDGenerator sets the function that produces a new identifier.
func WithRequestIDGenerator(fn func() string) RequestIDOption {
	return func(c *requestIDConfig) {
		if fn != nil {
			c.generate = fn
		}
	}
}

// WithTrustedRequestIDHeader lets RequestID reuse a client-supplied identifier
// when it is safe (ASCII, no control characters, within the length limit).
// Without this option a fresh identifier is always generated.
func WithTrustedRequestIDHeader() RequestIDOption {
	return func(c *requestIDConfig) { c.trustInbound = true }
}

// RequestID assigns each request an identifier, stores it in the request
// context (read with RequestIDFrom) and echoes it in the response header. By
// default it generates a new 128-bit random identifier and ignores any inbound
// value; use WithTrustedRequestIDHeader to accept a safe client-supplied one.
func RequestID(opts ...RequestIDOption) Middleware {
	cfg := requestIDConfig{
		header:   DefaultRequestIDHeader,
		generate: newRequestID,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := ""
			if cfg.trustInbound {
				if v := r.Header.Get(cfg.header); safeRequestID(v) {
					id = v
				}
			}
			if id == "" {
				id = cfg.generate()
			}

			w.Header().Set(cfg.header, id)
			ctx := context.WithValue(r.Context(), requestIDKey, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// newRequestID returns a random 128-bit identifier as 32 hex characters.
func newRequestID() string {
	var b [16]byte
	//nolint:errcheck // crypto/rand.Read never returns an error on supported platforms
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// safeRequestID reports whether an inbound identifier is acceptable: non-empty,
// within the length limit and made only of printable ASCII.
func safeRequestID(v string) bool {
	if v == "" || len(v) > maxRequestIDLen {
		return false
	}
	for i := 0; i < len(v); i++ {
		if v[i] < 0x20 || v[i] > 0x7e {
			return false
		}
	}
	return true
}
