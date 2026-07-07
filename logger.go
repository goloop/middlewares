package middlewares

import (
	"log/slog"
	"net/http"
	"time"
)

type loggerConfig struct {
	logger  *slog.Logger
	level   slog.Level
	message string
	skip    func(*http.Request) bool
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
