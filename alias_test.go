package middlewares

import (
	"net/http"
	"testing"
)

// routerMiddleware stands in for any router's defined middleware type. What
// this package returns must be assignable to it with no conversion; that is
// what the Middleware alias exists for.
type routerMiddleware func(http.Handler) http.Handler

func TestReturnedMiddlewareFitsForeignNamedTypes(t *testing.T) {
	var use func(routerMiddleware) = func(routerMiddleware) {}

	// Each call below compiles only while Middleware stays an alias.
	use(Throttle(1))
	use(MaxBytes(1))
	use(RequestID())
	use(Chain(Recoverer(), Throttle(1)))
}
