// Package middlewares is a set of production-ready HTTP middleware for the
// standard net/http. Every middleware has the ordinary shape
// func(http.Handler) http.Handler, so it works with any router: the standard
// http.ServeMux, the goloop/mux router, or hand-written handlers.
//
// It is not a framework. It closes the common cross-cutting needs that the
// standard library leaves out - request identifiers, real client IP, panic
// recovery, request logging, timeouts, response compression, concurrency
// throttling, CORS and security headers - with no third-party dependencies.
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
