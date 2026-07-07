package middlewares

import (
	"log/slog"
	"net/http"
	"runtime/debug"
)

type recovererConfig struct {
	logger  *slog.Logger
	handler func(http.ResponseWriter, *http.Request, any)
}

// RecovererOption configures the Recoverer middleware.
type RecovererOption func(*recovererConfig)

// WithRecoverLogger sets the logger used to report a recovered panic. When nil
// (the default), slog.Default is used.
func WithRecoverLogger(l *slog.Logger) RecovererOption {
	return func(c *recovererConfig) { c.logger = l }
}

// WithRecoverHandler sets a handler that renders the response after a panic. It
// runs only when nothing has been written to the client yet; the recovered
// value is passed as the third argument. Without it, a bare 500 is sent on an
// uncommitted response.
func WithRecoverHandler(fn func(http.ResponseWriter, *http.Request, any)) RecovererOption {
	return func(c *recovererConfig) { c.handler = fn }
}

// Recoverer catches a panic from a downstream handler, logs it with a stack
// trace and turns it into a response. If the handler had already committed a
// response (status and some body written), Recoverer cannot change it and only
// logs the panic. http.ErrAbortHandler is re-panicked so the server can abort
// the connection as usual.
func Recoverer(opts ...RecovererOption) Middleware {
	var cfg recovererConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := wrapWriter(w)
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if rec == http.ErrAbortHandler {
					panic(rec)
				}

				logger := cfg.logger
				if logger == nil {
					logger = slog.Default()
				}
				logger.ErrorContext(r.Context(), "panic recovered",
					slog.Any("panic", rec),
					slog.String("stack", string(debug.Stack())))

				if rw.wroteHeader {
					// The response is already committed; nothing safe to write.
					return
				}
				if cfg.handler != nil {
					cfg.handler(rw, r, rec)
					return
				}
				rw.WriteHeader(http.StatusInternalServerError)
			}()

			next.ServeHTTP(rw, r)
		})
	}
}
