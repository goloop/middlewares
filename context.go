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
func RealIPFrom(ctx context.Context) string {
	ip, _ := ctx.Value(realIPKey).(string)
	return ip
}
