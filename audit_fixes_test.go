package middlewares

import (
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCompressDoesNotNeutralizeRecoverer guards BUG-01: a panic under
// Recoverer(Compress()) must yield a 500, not a 200 with the partial body.
func TestCompressDoesNotNeutralizeRecoverer(t *testing.T) {
	h := Chain(Recoverer(WithRecoverLogger(silentLogger())), Compress())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "partial small body")
			panic("boom")
		}))

	r := req(http.MethodGet, "/")
	r.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "partial small body") {
		t.Error("partial body was committed instead of being discarded")
	}
}

// TestCompressInvalidLevelNoPanic guards BUG-03: an out-of-range gzip level is
// ignored at construction and never panics on a request.
func TestCompressInvalidLevelNoPanic(t *testing.T) {
	h := Compress(WithCompressLevel(99))(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, strings.Repeat("x", 4096))
		}))

	r := req(http.MethodGet, "/")
	r.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r) // must not panic
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// TestWrappersUnwrap guards BUG-04: the wrappers expose Unwrap so
// ResponseController can reach the underlying writer.
func TestWrappersUnwrap(t *testing.T) {
	base := httptest.NewRecorder()
	rw := wrapWriter(base)
	if got := rw.Unwrap(); got != http.ResponseWriter(base) {
		t.Error("responseWriter.Unwrap did not return the underlying writer")
	}
	cw := &compressWriter{ResponseWriter: base}
	if got := cw.Unwrap(); got != http.ResponseWriter(base) {
		t.Error("compressWriter.Unwrap did not return the underlying writer")
	}
}

// TestInformationalStatusNotLatched guards BUG-05: a 1xx response is forwarded
// and the final status is preserved.
func TestInformationalStatusNotLatched(t *testing.T) {
	base := httptest.NewRecorder()
	rw := wrapWriter(base)
	rw.WriteHeader(http.StatusEarlyHints) // 103
	rw.WriteHeader(http.StatusNotFound)   // 404 final
	if rw.Status() != http.StatusNotFound {
		t.Errorf("Status() = %d, want 404", rw.Status())
	}
}

// TestRealIPRightToLeft guards BUG-02: a spoofed left-most X-Forwarded-For entry
// is not returned; the right-most untrusted address is the client.
func TestRealIPRightToLeft(t *testing.T) {
	var got string
	h := RealIP(WithTrustedProxies("10.0.0.0/8"))(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			got = RealIPFrom(r.Context())
		}))

	r := req(http.MethodGet, "/")
	r.RemoteAddr = "10.0.0.1:1234" // trusted direct peer
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.50")
	h.ServeHTTP(httptest.NewRecorder(), r)

	if got != "203.0.113.50" {
		t.Errorf("RealIPFrom = %q, want 203.0.113.50 (not the spoofed 6.6.6.6)", got)
	}
}

// TestCompressLevelValidRange checks a valid level is still applied.
func TestCompressLevelValidRange(t *testing.T) {
	c := compressConfig{level: gzip.DefaultCompression}
	WithCompressLevel(gzip.BestSpeed)(&c)
	if c.level != gzip.BestSpeed {
		t.Errorf("level = %d, want %d", c.level, gzip.BestSpeed)
	}
	WithCompressLevel(99)(&c) // ignored
	if c.level != gzip.BestSpeed {
		t.Errorf("invalid level changed config to %d", c.level)
	}
}
