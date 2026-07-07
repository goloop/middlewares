package middlewares

import "net/http"

// Middleware is the standard net/http middleware shape: it wraps a handler and
// returns a new handler.
type Middleware func(http.Handler) http.Handler

// Chain composes middleware into a single Middleware. The first middleware in
// the list is the outermost wrapper, so it runs first on the way in and last on
// the way out:
//
//	mw := middlewares.Chain(a, b, c) // request flows a -> b -> c -> handler
func Chain(m ...Middleware) Middleware {
	return func(next http.Handler) http.Handler {
		for i := len(m) - 1; i >= 0; i-- {
			next = m[i](next)
		}
		return next
	}
}

// Handler applies middleware to next in one call. It is shorthand for
// Chain(m...)(next).
func Handler(next http.Handler, m ...Middleware) http.Handler {
	return Chain(m...)(next)
}
