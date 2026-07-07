package middlewares

import (
	"net/http"
	"strconv"
	"strings"
)

type corsConfig struct {
	origins     map[string]bool
	allowAll    bool
	methods     string
	headers     string
	exposed     string
	credentials bool
	maxAge      int
}

// CORSOption configures the CORS middleware.
type CORSOption func(*corsConfig)

// WithAllowedOrigins sets the origins permitted to make cross-origin requests.
// The special value "*" allows any origin. Nothing is allowed until this is
// set.
func WithAllowedOrigins(origins ...string) CORSOption {
	return func(c *corsConfig) {
		for _, o := range origins {
			if o == "*" {
				c.allowAll = true
				continue
			}
			c.origins[o] = true
		}
	}
}

// WithAllowedMethods sets the methods advertised in preflight responses.
func WithAllowedMethods(methods ...string) CORSOption {
	return func(c *corsConfig) { c.methods = strings.Join(methods, ", ") }
}

// WithAllowedHeaders sets the request headers advertised in preflight
// responses. When unset, the requested headers are reflected back.
func WithAllowedHeaders(headers ...string) CORSOption {
	return func(c *corsConfig) { c.headers = strings.Join(headers, ", ") }
}

// WithExposedHeaders sets the response headers browsers may expose to script.
func WithExposedHeaders(headers ...string) CORSOption {
	return func(c *corsConfig) { c.exposed = strings.Join(headers, ", ") }
}

// WithAllowCredentials allows credentialed requests (cookies, HTTP auth). It
// cannot be combined with a wildcard origin.
func WithAllowCredentials() CORSOption {
	return func(c *corsConfig) { c.credentials = true }
}

// WithMaxAge sets how long, in seconds, a preflight result may be cached.
func WithMaxAge(seconds int) CORSOption {
	return func(c *corsConfig) { c.maxAge = seconds }
}

// CORS handles Cross-Origin Resource Sharing. Its defaults are safe: no origin
// is allowed until you name one with WithAllowedOrigins. A preflight OPTIONS
// request from an allowed origin is answered with 204 and the appropriate
// headers; other requests are annotated and passed through.
//
// CORS panics if credentials are allowed together with a wildcard origin, which
// is an invalid and unsafe combination.
func CORS(opts ...CORSOption) Middleware {
	cfg := corsConfig{
		origins: make(map[string]bool),
		methods: "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS",
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.allowAll && cfg.credentials {
		panic("middlewares: CORS cannot allow credentials with a wildcard origin")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				// Not a cross-origin request.
				next.ServeHTTP(w, r)
				return
			}

			allowed := cfg.allowAll || cfg.origins[origin]
			h := w.Header()
			if allowed {
				if cfg.allowAll && !cfg.credentials {
					h.Set("Access-Control-Allow-Origin", "*")
				} else {
					h.Set("Access-Control-Allow-Origin", origin)
					h.Add("Vary", "Origin")
				}
				if cfg.credentials {
					h.Set("Access-Control-Allow-Credentials", "true")
				}
			}

			if r.Method == http.MethodOptions &&
				r.Header.Get("Access-Control-Request-Method") != "" {
				if allowed {
					if cfg.methods != "" {
						h.Set("Access-Control-Allow-Methods", cfg.methods)
					}
					if cfg.headers != "" {
						h.Set("Access-Control-Allow-Headers", cfg.headers)
					} else if req := r.Header.Get("Access-Control-Request-Headers"); req != "" {
						h.Set("Access-Control-Allow-Headers", req)
					}
					if cfg.maxAge > 0 {
						h.Set("Access-Control-Max-Age", strconv.Itoa(cfg.maxAge))
					}
					h.Add("Vary", "Access-Control-Request-Method")
					h.Add("Vary", "Access-Control-Request-Headers")
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			if allowed && cfg.exposed != "" {
				h.Set("Access-Control-Expose-Headers", cfg.exposed)
			}
			next.ServeHTTP(w, r)
		})
	}
}
