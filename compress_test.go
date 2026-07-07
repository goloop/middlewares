package middlewares

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func gzipRequest(method, target string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	r.Header.Set("Accept-Encoding", "gzip")
	return r
}

func gunzip(t *testing.T, data []byte) string {
	t.Helper()
	zr, err := gzip.NewReader(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	return string(out)
}

func TestCompressGzipLargeJSON(t *testing.T) {
	payload := `{"data":"` + strings.Repeat("x", 4000) + `"}`
	h := Compress()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, payload)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, gzipRequest(http.MethodGet, "/"))

	if enc := rec.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", enc)
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatalf("Vary = %q, want Accept-Encoding", rec.Header().Get("Vary"))
	}
	if got := gunzip(t, rec.Body.Bytes()); got != payload {
		t.Fatalf("decompressed body mismatch")
	}
}

func TestCompressSkipsSmall(t *testing.T) {
	h := Compress()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`) // below 1024 bytes
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, gzipRequest(http.MethodGet, "/"))

	if enc := rec.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("small body should not be compressed, got %q", enc)
	}
	if rec.Body.String() != `{"ok":true}` {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestCompressSkipsSSE(t *testing.T) {
	big := strings.Repeat("data: hello\n\n", 500) // well over the threshold
	h := Compress()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, big)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, gzipRequest(http.MethodGet, "/"))

	if enc := rec.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("SSE must not be compressed, got %q", enc)
	}
}

func TestCompressSkipsAlreadyEncoded(t *testing.T) {
	big := strings.Repeat("a", 4000)
	h := Compress()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "br")
		fmt.Fprint(w, big)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, gzipRequest(http.MethodGet, "/"))

	if enc := rec.Header().Get("Content-Encoding"); enc != "br" {
		t.Fatalf("Content-Encoding = %q, want br preserved", enc)
	}
}

func TestCompressSkipsHead(t *testing.T) {
	h := Compress()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, gzipRequest(http.MethodHead, "/"))
	if enc := rec.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("HEAD must not be compressed, got %q", enc)
	}
}

func TestCompressSkipsWhenClientDoesNotAccept(t *testing.T) {
	big := strings.Repeat("a", 4000)
	h := Compress()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, big)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil)) // no Accept-Encoding
	if enc := rec.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("must not compress without Accept-Encoding, got %q", enc)
	}
	if rec.Body.String() != big {
		t.Fatalf("body altered")
	}
}

func TestCompressSkipsNonCompressibleType(t *testing.T) {
	big := strings.Repeat("\x00", 4000)
	h := Compress()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte(big))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, gzipRequest(http.MethodGet, "/"))
	if enc := rec.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("image/png must not be compressed, got %q", enc)
	}
}
