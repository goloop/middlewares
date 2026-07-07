package middlewares

import (
	"net/http"
	"strconv"
)

// Default values for the security headers. They are conservative and safe for
// APIs and typical web apps; a Content-Security-Policy is intentionally not set
// by default, since a useful policy is application-specific.
const (
	defaultContentTypeOptions = "nosniff"
	defaultFrameOptions       = "DENY"
	defaultReferrerPolicy     = "no-referrer"
)

type securityConfig struct {
	contentTypeOptions string
	frameOptions       string
	referrerPolicy     string
	hstsSeconds        int
	csp                string
}

// SecurityOption configures the SecurityHeaders middleware.
type SecurityOption func(*securityConfig)

// WithFrameOptions sets the X-Frame-Options value (default "DENY"). An empty
// string omits the header.
func WithFrameOptions(v string) SecurityOption {
	return func(c *securityConfig) { c.frameOptions = v }
}

// WithReferrerPolicy sets the Referrer-Policy value (default "no-referrer"). An
// empty string omits the header.
func WithReferrerPolicy(v string) SecurityOption {
	return func(c *securityConfig) { c.referrerPolicy = v }
}

// WithHSTS enables Strict-Transport-Security with the given max-age in seconds.
// It should only be set for sites served exclusively over HTTPS. Zero, the
// default, omits the header.
func WithHSTS(seconds int) SecurityOption {
	return func(c *securityConfig) { c.hstsSeconds = seconds }
}

// WithCSP sets a Content-Security-Policy. It is opt-in because a good policy
// depends on the application.
func WithCSP(policy string) SecurityOption {
	return func(c *securityConfig) { c.csp = policy }
}

// SecurityHeaders adds a conservative set of security response headers:
// X-Content-Type-Options, X-Frame-Options and Referrer-Policy by default, with
// optional HSTS and Content-Security-Policy. Existing header values set by the
// handler are not overwritten.
func SecurityHeaders(opts ...SecurityOption) Middleware {
	cfg := securityConfig{
		contentTypeOptions: defaultContentTypeOptions,
		frameOptions:       defaultFrameOptions,
		referrerPolicy:     defaultReferrerPolicy,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			setIfAbsent(h, "X-Content-Type-Options", cfg.contentTypeOptions)
			setIfAbsent(h, "X-Frame-Options", cfg.frameOptions)
			setIfAbsent(h, "Referrer-Policy", cfg.referrerPolicy)
			if cfg.csp != "" {
				setIfAbsent(h, "Content-Security-Policy", cfg.csp)
			}
			if cfg.hstsSeconds > 0 {
				setIfAbsent(h, "Strict-Transport-Security",
					"max-age="+strconv.Itoa(cfg.hstsSeconds))
			}
			next.ServeHTTP(w, r)
		})
	}
}

func setIfAbsent(h http.Header, key, value string) {
	if value == "" {
		return
	}
	if h.Get(key) == "" {
		h.Set(key, value)
	}
}
