package middlewares

import (
	"bufio"
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

// DefaultCompressMinBytes is the smallest response, in bytes, that Compress will
// gzip. Smaller bodies are sent as-is, since compression would not help.
const DefaultCompressMinBytes = 1024

type compressConfig struct {
	level    int
	minBytes int
	types    map[string]bool
}

// CompressOption configures the Compress middleware.
type CompressOption func(*compressConfig)

// WithCompressLevel sets the gzip level (gzip.BestSpeed to gzip.BestCompression,
// plus gzip.DefaultCompression and gzip.HuffmanOnly). An out-of-range level is
// ignored and the default is kept, so a configuration mistake cannot turn into
// a deferred runtime nil-pointer panic on the first large request.
func WithCompressLevel(level int) CompressOption {
	return func(c *compressConfig) {
		if level == gzip.DefaultCompression || level == gzip.HuffmanOnly ||
			(level >= gzip.NoCompression && level <= gzip.BestCompression) {
			c.level = level
		}
	}
}

// WithCompressMinBytes sets the minimum response size to compress.
func WithCompressMinBytes(n int) CompressOption {
	return func(c *compressConfig) {
		if n >= 0 {
			c.minBytes = n
		}
	}
}

// WithCompressTypes replaces the set of compressible content types (matched on
// the media type, without parameters).
func WithCompressTypes(types ...string) CompressOption {
	return func(c *compressConfig) {
		c.types = make(map[string]bool, len(types))
		for _, t := range types {
			c.types[strings.ToLower(t)] = true
		}
	}
}

func defaultCompressTypes() map[string]bool {
	return map[string]bool{
		"text/html":                 true,
		"text/css":                  true,
		"text/plain":                true,
		"text/xml":                  true,
		"text/javascript":           true,
		"application/javascript":    true,
		"application/json":          true,
		"application/xml":           true,
		"application/xhtml+xml":     true,
		"image/svg+xml":             true,
		"application/rss+xml":       true,
		"application/atom+xml":      true,
		"application/manifest+json": true,
	}
}

// Compress gzips responses for clients that accept it. It buffers the response
// until it reaches the minimum size, then decides whether to compress based on
// the status, existing Content-Encoding and Content-Type. It never compresses
// HEAD responses, 204/304 replies, already-encoded bodies, text/event-stream
// (server-sent events) or bodies below the minimum size. The Vary:
// Accept-Encoding header is always set on a compressed response.
func Compress(opts ...CompressOption) Middleware {
	cfg := compressConfig{
		level:    gzip.DefaultCompression,
		minBytes: DefaultCompressMinBytes,
		types:    defaultCompressTypes(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	pool := &sync.Pool{
		New: func() any {
			// The level is validated by WithCompressLevel, but fall back to the
			// default rather than returning a typed-nil writer if it is not.
			w, err := gzip.NewWriterLevel(io.Discard, cfg.level)
			if err != nil {
				w, _ = gzip.NewWriterLevel(io.Discard, gzip.DefaultCompression)
			}
			return w
		},
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip HEAD, clients that do not accept gzip, and connection
			// upgrades (WebSocket and similar), which must not be buffered.
			if r.Method == http.MethodHead || !acceptsGzip(r) ||
				r.Header.Get("Upgrade") != "" {
				next.ServeHTTP(w, r)
				return
			}
			cw := &compressWriter{ResponseWriter: w, cfg: &cfg, pool: pool}
			defer func() {
				if p := recover(); p != nil {
					// Do not flush the buffered partial response during panic
					// unwinding: that would commit a 200 and stop an outer
					// Recoverer from sending a 500. Discard and re-panic.
					cw.discard()
					panic(p)
				}
				cw.close()
			}()
			next.ServeHTTP(cw, r)
		})
	}
}

// acceptsGzip reports whether the request advertises gzip support.
func acceptsGzip(r *http.Request) bool {
	for _, enc := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		if strings.EqualFold(strings.TrimSpace(strings.SplitN(enc, ";", 2)[0]), "gzip") {
			return true
		}
	}
	return false
}

// compressWriter buffers the response until it can decide whether gzip is worth
// it, then either compresses the stream or passes it through unchanged.
type compressWriter struct {
	http.ResponseWriter
	cfg  *compressConfig
	pool *sync.Pool

	status      int
	buf         []byte
	decided     bool
	passthrough bool
	gz          *gzip.Writer
}

func (c *compressWriter) WriteHeader(code int) {
	if code >= 100 && code < 200 {
		// Informational (1xx) responses, e.g. 103 Early Hints, are forwarded
		// immediately and must not latch the final status.
		c.ResponseWriter.WriteHeader(code)
		return
	}
	if c.status == 0 {
		c.status = code
	}
}

// Unwrap exposes the wrapped ResponseWriter so http.ResponseController can reach
// the underlying deadline/flush methods through this wrapper (Go 1.20+ idiom).
func (c *compressWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }

// discard drops the buffered response without writing anything, returning any
// gzip writer to the pool. It is used on the panic path so a partial buffered
// body is not committed as a successful response.
func (c *compressWriter) discard() {
	c.buf = nil
	if c.gz != nil {
		c.gz.Reset(io.Discard)
		c.pool.Put(c.gz)
		c.gz = nil
	}
}

func (c *compressWriter) Write(b []byte) (int, error) {
	if c.decided {
		return c.writeDecided(b)
	}
	c.buf = append(c.buf, b...)
	if len(c.buf) < c.cfg.minBytes {
		return len(b), nil
	}
	if err := c.start(); err != nil {
		return 0, err
	}
	return len(b), nil
}

// start commits to compressing or not, using the headers and the buffered bytes
// so far, then flushes the buffer through the chosen path.
func (c *compressWriter) start() error {
	c.decided = true
	if c.status == 0 {
		c.status = http.StatusOK
	}

	h := c.ResponseWriter.Header()
	if c.compressible(h) {
		if h.Get("Content-Type") == "" {
			h.Set("Content-Type", http.DetectContentType(c.buf))
		}
		h.Del("Content-Length")
		h.Set("Content-Encoding", "gzip")
		addVary(h, "Accept-Encoding")
		c.ResponseWriter.WriteHeader(c.status)

		gz := c.pool.Get().(*gzip.Writer)
		gz.Reset(c.ResponseWriter)
		c.gz = gz
	} else {
		c.passthrough = true
		c.ResponseWriter.WriteHeader(c.status)
	}

	buf := c.buf
	c.buf = nil
	_, err := c.writeDecided(buf)
	return err
}

func (c *compressWriter) writeDecided(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	if c.passthrough {
		return c.ResponseWriter.Write(b)
	}
	return c.gz.Write(b)
}

// compressible reports whether a response with these headers should be gzipped.
func (c *compressWriter) compressible(h http.Header) bool {
	if c.status == http.StatusNoContent || c.status == http.StatusNotModified {
		return false
	}
	if h.Get("Content-Encoding") != "" {
		return false
	}
	ct := h.Get("Content-Type")
	if ct == "" {
		// Unknown yet: sniff from the buffered bytes.
		ct = http.DetectContentType(c.buf)
	}
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	ct = strings.ToLower(strings.TrimSpace(ct))
	if ct == "text/event-stream" {
		return false
	}
	return c.cfg.types[ct]
}

// Flush forces a decision (streaming handlers cannot wait for the buffer to
// fill) and flushes whatever is pending.
func (c *compressWriter) Flush() {
	if !c.decided {
		//nolint:errcheck // flush is best-effort
		c.start()
	}
	if c.gz != nil {
		//nolint:errcheck // flush is best-effort
		c.gz.Flush()
	}
	//nolint:errcheck // flush is best-effort
	http.NewResponseController(c.ResponseWriter).Flush()
}

// Hijack lets a handler take over the connection (for example a WebSocket
// upgrade). A hijacked connection is used raw and cannot be gzipped, so the
// writer stops managing the response and forwards to the underlying one.
func (c *compressWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c.decided = true
	c.passthrough = true
	c.buf = nil
	return http.NewResponseController(c.ResponseWriter).Hijack()
}

// close finalizes the response: it emits a small buffered body uncompressed, or
// closes the gzip stream and returns the writer to the pool.
func (c *compressWriter) close() {
	if !c.decided {
		if c.status == 0 {
			c.status = http.StatusOK
		}
		c.ResponseWriter.WriteHeader(c.status)
		if len(c.buf) > 0 {
			//nolint:errcheck // nothing useful to do with a late write error
			c.ResponseWriter.Write(c.buf)
		}
		return
	}
	if c.gz != nil {
		//nolint:errcheck // best-effort close
		c.gz.Close()
		c.pool.Put(c.gz)
		c.gz = nil
	}
}

// addVary appends value to the Vary header if it is not already present.
func addVary(h http.Header, value string) {
	for _, v := range h.Values("Vary") {
		if strings.EqualFold(v, value) {
			return
		}
	}
	h.Add("Vary", value)
}
