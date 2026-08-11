package middlewares

import (
	"net/http"
	"strconv"
	"time"
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
	hstsSubDomains     bool
	hstsPreload        bool
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
//
// Use [WithHSTSPolicy] for the directives a full policy usually carries.
func WithHSTS(seconds int) SecurityOption {
	return func(c *securityConfig) { c.hstsSeconds = seconds }
}

// HSTS is a complete Strict-Transport-Security policy.
type HSTS struct {
	// MaxAge is how long a browser should refuse plain HTTP for this host.
	// It is a duration rather than a count of seconds because a year is
	// easier to read as 365*24*time.Hour than as 31536000. Values below a
	// second round down to zero, which omits the header.
	MaxAge time.Duration

	// IncludeSubDomains extends the policy to every subdomain. Almost every
	// real policy wants it, and it is worth meaning: a subdomain that cannot
	// serve HTTPS becomes unreachable for as long as MaxAge lasts.
	IncludeSubDomains bool

	// Preload asks for inclusion in the browsers' built-in list. It requires
	// IncludeSubDomains and a long MaxAge, and removal from that list takes
	// months, so set it only for a host that will be HTTPS-only indefinitely.
	Preload bool
}

// WithHSTSPolicy sets a full Strict-Transport-Security policy, including
// includeSubDomains and preload.
//
// It replaces rather than extends [WithHSTS]; the last of the two applied
// wins. A zero MaxAge omits the header, so a policy can be turned off in one
// place without unpicking the rest of it.
func WithHSTSPolicy(p HSTS) SecurityOption {
	return func(c *securityConfig) {
		c.hstsSeconds = int(p.MaxAge / time.Second)
		c.hstsSubDomains = p.IncludeSubDomains
		c.hstsPreload = p.Preload
	}
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
				setIfAbsent(h, "Strict-Transport-Security", hstsValue(cfg))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// hstsValue renders the policy in the order the header is conventionally
// written. preload is emitted only alongside includeSubDomains, because the
// browser lists reject a policy without it and a header that can never be
// honoured is worse than an honest one.
func hstsValue(cfg securityConfig) string {
	v := "max-age=" + strconv.Itoa(cfg.hstsSeconds)
	if cfg.hstsSubDomains {
		v += "; includeSubDomains"
		if cfg.hstsPreload {
			v += "; preload"
		}
	}
	return v
}

func setIfAbsent(h http.Header, key, value string) {
	if value == "" {
		return
	}
	if h.Get(key) == "" {
		h.Set(key, value)
	}
}
