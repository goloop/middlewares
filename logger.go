package middlewares

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

type loggerConfig struct {
	logger  *slog.Logger
	level   slog.Level
	message string
	skip    func(*http.Request) bool
	inject  bool
}

// loggerKey is the context key under which a request-scoped logger is stored.
// It is an unexported type so that nothing outside this package can collide
// with it or overwrite it.
type loggerKey struct{}

// LoggerFrom returns the logger [WithContextLogger] put in the context,
// carrying the request identifier, method and path. Without that option, or
// outside a request, it returns slog.Default so a caller never has to check:
//
//	middlewares.LoggerFrom(r.Context()).Info("upstream refused", "status", code)
//
// The line then carries the identifier of the request that caused it, which is
// what makes the two halves of an incident join up.
func LoggerFrom(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}

// LoggerOption configures the Logger middleware.
type LoggerOption func(*loggerConfig)

// WithLogger sets the slog.Logger used for request logs. When nil (the
// default), slog.Default is used.
func WithLogger(l *slog.Logger) LoggerOption {
	return func(c *loggerConfig) { c.logger = l }
}

// WithLogLevel sets the level at which requests are logged (default Info).
func WithLogLevel(level slog.Level) LoggerOption {
	return func(c *loggerConfig) { c.level = level }
}

// WithLogMessage sets the log message (default "http request").
func WithLogMessage(msg string) LoggerOption {
	return func(c *loggerConfig) {
		if msg != "" {
			c.message = msg
		}
	}
}

// WithSkip skips logging for requests where fn returns true, for example health
// checks.
func WithSkip(fn func(*http.Request) bool) LoggerOption {
	return func(c *loggerConfig) { c.skip = fn }
}

// WithContextLogger puts a logger for this request into its context, already
// carrying the request identifier, method and path, so that handlers can log
// with [LoggerFrom] and have their lines correlate without threading anything
// through.
//
// It is worth the option because the alternative fails quietly. Logger writes
// one line per request with the identifier on it, and every line a handler
// writes is separate and unrelated; joining them afterwards means matching on
// timestamps. The usual fix is to remember to add the identifier at each call
// site, and what actually happens is that someone does not - including people
// who have just written in a design document that the logs correlate.
//
// It is opt-in rather than automatic because it allocates a context and a
// logger per request, and a service that never reads it should not pay for it.
func WithContextLogger() LoggerOption {
	return func(c *loggerConfig) { c.inject = true }
}

// Logger logs one line per request with method, path, status, response size and
// duration, using log/slog. It does no work when the request is skipped or when
// the logger is not enabled at the configured level. The request identifier
// from RequestID, if present, is included.
func Logger(opts ...LoggerOption) Middleware {
	cfg := loggerConfig{
		level:   slog.LevelInfo,
		message: "http request",
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			logger := cfg.logger
			if logger == nil {
				logger = slog.Default()
			}

			if cfg.inject {
				r = r.WithContext(context.WithValue(r.Context(),
					loggerKey{}, requestLogger(logger, r)))
			}

			if (cfg.skip != nil && cfg.skip(r)) || !logger.Enabled(r.Context(), cfg.level) {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			rw := wrapWriter(w)
			next.ServeHTTP(rw, r)

			attrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rw.Status()),
				slog.Int64("bytes", rw.Written()),
				slog.Duration("duration", time.Since(start)),
			}
			if id := RequestIDFrom(r.Context()); id != "" {
				attrs = append(attrs, slog.String("request_id", id))
			}
			logger.LogAttrs(r.Context(), cfg.level, cfg.message, attrs...)
		})
	}
}

// requestLogger returns the logger handlers get from the context: the
// configured one with the identifiers of this request already attached, so a
// handler writes an ordinary line and gets a correlated one.
func requestLogger(base *slog.Logger, r *http.Request) *slog.Logger {
	attrs := []any{
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	}
	if id := RequestIDFrom(r.Context()); id != "" {
		attrs = append(attrs, slog.String("request_id", id))
	}
	if ip := RealIPFrom(r.Context()); ip != "" {
		attrs = append(attrs, slog.String("remote_ip", ip))
	}
	return base.With(attrs...)
}
