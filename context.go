package middlewares

import "context"

// ctxKey is a private type for context keys defined in this package, so they can
// never collide with keys from other packages.
type ctxKey int

const (
	requestIDKey ctxKey = iota
	realIPKey
)

// RequestIDFrom returns the request identifier stored by the RequestID
// middleware, or an empty string if none is present.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// RealIPFrom returns the client IP resolved by the RealIP middleware, or an
// empty string if none is present.
//
// This is the only way to read that address: RealIP writes it here and leaves
// Request.RemoteAddr untouched. An empty result means the middleware did not
// run, and falling back to RemoteAddr is then the caller's decision to make
// knowingly - behind a proxy it is the proxy's address.
func RealIPFrom(ctx context.Context) string {
	ip, _ := ctx.Value(realIPKey).(string)
	return ip
}
