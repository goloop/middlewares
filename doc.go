// Package middlewares is a set of production-ready HTTP middleware for the
// standard net/http. Every middleware has the ordinary shape
// func(http.Handler) http.Handler, so it works with any router: the standard
// http.ServeMux, the goloop/mux router, or hand-written handlers.
//
// It is not a framework. It closes the common cross-cutting needs that the
// standard library leaves out - request identifiers, real client IP, panic
// recovery, request logging, timeouts, response compression, rate limiting,
// concurrency throttling, CORS and security headers - with no third-party
// dependencies.
//
// Compose middleware with Chain, or apply them one-shot with Handler:
//
//	h := middlewares.Chain(
//	    middlewares.RequestID(),
//	    middlewares.RealIP(),
//	    middlewares.Recoverer(),
//	    middlewares.Logger(),
//	    middlewares.Compress(),
//	)(mux)
//	http.ListenAndServe(":8080", h)
//
// Logging uses the standard log/slog. Pass any *slog.Logger through the
// relevant option; the package never imports a concrete logging backend.
//
// Security notes worth reading before production use:
//
// # Rate limiting and throttling are different tools
//
// RateLimit answers "how often may this client come back"; Throttle answers
// "how much of this work fits in the process at once". Ten per minute still
// admits ten in the same instant, which is what exhausts memory in front of an
// expensive handler; ten concurrent slots do not, and neither stops a client
// from returning every second forever. In front of sign-in both belong.
//
//	middlewares.RateLimit(middlewares.RateLimitConfig{
//	    Limit: 20, Window: 15 * time.Minute,
//	})
//
// The store decides, and the default one counts only what this process saw.
// A limit that has to hold across instances needs a RateLimitStore they share;
// the interface is one method so that a Redis or database implementation is a
// short piece of application code rather than a dependency of this package.
//
// When a store cannot answer, the request is refused with 503 rather than
// served. A limiter usually sits in front of sign-in, password reset and
// one-time codes, and a store outage that quietly removed the limit from
// exactly those routes would turn an availability problem into a security one.
// WithFailOpen chooses the other trade for routes where it is the right one.
//
// Anything keyed by client address inherits the caveat on RealIP: behind a
// reverse proxy with no trusted-proxy policy, every client resolves to the
// proxy and the limit becomes one budget shared by everyone. Configure
// WithTrustedProxies before relying on KeyByIP.
//
// # Correlated logs
//
// Logger writes one line per request. WithContextLogger additionally puts a
// logger into the request context, already carrying the request identifier,
// method and path, so that handler logs join up with it:
//
//	h := middlewares.Logger(middlewares.WithContextLogger())
//	// in a handler:
//	middlewares.LoggerFrom(r.Context()).Warn("upstream refused", "status", code)
//
// The alternative is remembering to add the identifier at every call site,
// which fails quietly and selectively - the lines that are missing it are the
// ones written in a hurry, which are the ones written during an incident.
//
//   - RealIP trusts no proxy headers by default; configure trusted proxies
//     explicitly, or a client can spoof its address.
//   - Timeout cancels the request context but cannot stop a handler goroutine
//     that ignores it.
//   - Recoverer cannot change a response that was already committed; it only
//     logs the panic in that case.
//   - Compress never touches text/event-stream, already-encoded responses, or
//     bodies below the minimum size.
//   - CORS allows nothing until you name the origins; wildcard origin together
//     with credentials is rejected at construction.
package middlewares
