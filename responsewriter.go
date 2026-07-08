package middlewares

import (
	"bufio"
	"io"
	"net"
	"net/http"
)

// responseWriter wraps an http.ResponseWriter to record the status code and the
// number of body bytes written, which Logger and Recoverer both need. It
// forwards Flush, Hijack and ReadFrom to the underlying writer through
// http.ResponseController, so streaming, WebSocket upgrades and efficient file
// serving keep working.
type responseWriter struct {
	http.ResponseWriter
	status      int
	written     int64
	wroteHeader bool
}

func wrapWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w}
}

func (w *responseWriter) WriteHeader(code int) {
	if code >= 100 && code < 200 {
		// Informational (1xx) responses, e.g. 103 Early Hints, may be sent
		// several times before the final status; forward them without latching.
		w.ResponseWriter.WriteHeader(code)
		return
	}
	if w.wroteHeader {
		return
	}
	w.status = code
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap exposes the wrapped ResponseWriter so http.ResponseController can reach
// the underlying deadline/flush methods through this wrapper (Go 1.20+ idiom).
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.written += int64(n)
	return n, err
}

// Status returns the status code sent to the client, defaulting to 200 when the
// handler wrote a body without an explicit WriteHeader.
func (w *responseWriter) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

// Written returns the number of body bytes written.
func (w *responseWriter) Written() int64 { return w.written }

// Flush forwards to the underlying writer so streaming handlers are not broken
// by the wrapper.
func (w *responseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	//nolint:errcheck // best-effort flush, mirrors http.ResponseController usage
	http.NewResponseController(w.ResponseWriter).Flush()
}

// Hijack forwards to the underlying writer so WebSocket and other upgrades work.
func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

// ReadFrom lets the underlying writer use its efficient copy path when
// available, while still counting the bytes.
func (w *responseWriter) ReadFrom(src io.Reader) (int64, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		n, err := rf.ReadFrom(src)
		w.written += n
		return n, err
	}
	n, err := io.Copy(w.ResponseWriter, src)
	w.written += n
	return n, err
}
