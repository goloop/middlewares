package middlewares

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequestIDCustomHeaderAndGenerator(t *testing.T) {
	h := RequestID(
		WithRequestIDHeader("X-Trace"),
		WithRequestIDGenerator(func() string { return "fixed-id" }),
	)(ok200("ok"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req(http.MethodGet, "/"))
	if got := rec.Header().Get("X-Trace"); got != "fixed-id" {
		t.Fatalf("X-Trace = %q, want fixed-id", got)
	}
}

func TestLoggerLevelAndMessage(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h := Logger(
		WithLogger(logger),
		WithLogLevel(slog.LevelWarn),
		WithLogMessage("served"),
	)(ok200("ok"))
	h.ServeHTTP(httptest.NewRecorder(), req(http.MethodGet, "/"))
	out := buf.String()
	if !strings.Contains(out, `level=WARN`) || !strings.Contains(out, `msg=served`) {
		t.Fatalf("log = %q, want WARN/served", out)
	}
}

func TestThrottleStatusAndRetryAfter(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	h := Throttle(1,
		WithThrottleStatus(http.StatusTooManyRequests),
		WithThrottleRetryAfter(5),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-release
	}))

	go h.ServeHTTP(httptest.NewRecorder(), req(http.MethodGet, "/"))
	<-started

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req(http.MethodGet, "/"))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "5" {
		t.Fatalf("Retry-After = %q, want 5", got)
	}
	close(release)
}

func TestTimeoutMessage(t *testing.T) {
	h := Timeout(10*time.Millisecond, WithTimeoutMessage("too slow"))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req(http.MethodGet, "/"))
	if !strings.Contains(rec.Body.String(), "too slow") {
		t.Fatalf("body = %q, want custom timeout message", rec.Body.String())
	}
}

func TestSecurityHeadersCustomOptions(t *testing.T) {
	h := SecurityHeaders(
		WithFrameOptions("SAMEORIGIN"),
		WithReferrerPolicy("strict-origin"),
	)(ok200("ok"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req(http.MethodGet, "/"))
	if got := rec.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Fatalf("X-Frame-Options = %q", got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "strict-origin" {
		t.Fatalf("Referrer-Policy = %q", got)
	}
}

func TestCORSPreflightExplicitHeaders(t *testing.T) {
	h := CORS(
		WithAllowedOrigins("https://a.example.com"),
		WithAllowedHeaders("Authorization", "Content-Type"),
		WithExposedHeaders("X-Total-Count"),
	)(ok200("ok"))

	// Preflight advertises the configured allowed headers.
	r := httptest.NewRequest(http.MethodOptions, "/", nil)
	r.Header.Set("Origin", "https://a.example.com")
	r.Header.Set("Access-Control-Request-Method", "GET")
	r.Header.Set("Access-Control-Request-Headers", "authorization")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "Authorization, Content-Type" {
		t.Fatalf("Allow-Headers = %q", got)
	}

	// A real request exposes the configured response headers.
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.Header.Set("Origin", "https://a.example.com")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, r2)
	if got := rec2.Header().Get("Access-Control-Expose-Headers"); got != "X-Total-Count" {
		t.Fatalf("Expose-Headers = %q", got)
	}
}

func TestCORSPreflightReflectsRequestHeaders(t *testing.T) {
	h := CORS(WithAllowedOrigins("https://a.example.com"))(ok200("ok"))
	r := httptest.NewRequest(http.MethodOptions, "/", nil)
	r.Header.Set("Origin", "https://a.example.com")
	r.Header.Set("Access-Control-Request-Method", "GET")
	r.Header.Set("Access-Control-Request-Headers", "X-Custom")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "X-Custom" {
		t.Fatalf("Allow-Headers = %q, want reflected X-Custom", got)
	}
}

func TestCompressStreamingFlush(t *testing.T) {
	h := Compress(WithCompressMinBytes(1), WithCompressLevel(gzip.BestSpeed))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, strings.Repeat("a", 10))
			w.(http.Flusher).Flush()
			fmt.Fprint(w, strings.Repeat("b", 10))
		}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, gzipRequest(http.MethodGet, "/"))

	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected gzip, got %q", rec.Header().Get("Content-Encoding"))
	}
	body := gunzip(t, rec.Body.Bytes())
	if body != strings.Repeat("a", 10)+strings.Repeat("b", 10) {
		t.Fatalf("streamed body mismatch: %q", body)
	}
}

func TestCompressTypesRestriction(t *testing.T) {
	big := strings.Repeat("a", 4000)
	h := Compress(WithCompressTypes("text/plain"))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json") // not in the allow-list
			fmt.Fprint(w, big)
		}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, gzipRequest(http.MethodGet, "/"))
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("application/json should not compress when types are restricted")
	}
}

func TestResponseWriterReadFrom(t *testing.T) {
	// io.Copy into the Logger-wrapped writer exercises the ReadFrom path.
	h := Logger(WithLogger(silentLogger()))(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			io.Copy(w, strings.NewReader("copied-bytes"))
		}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req(http.MethodGet, "/"))
	if rec.Body.String() != "copied-bytes" {
		t.Fatalf("body = %q, want copied-bytes", rec.Body.String())
	}
}

func TestResponseWriterHijackUnsupported(t *testing.T) {
	// The wrapper always exposes Hijacker, but on a non-hijackable writer it
	// must return an error rather than panic.
	h := Recoverer(WithRecoverLogger(silentLogger()))(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("wrapper should expose http.Hijacker")
			}
			if _, _, err := hj.Hijack(); err == nil {
				t.Fatal("expected an error hijacking a recorder")
			}
		}))
	h.ServeHTTP(httptest.NewRecorder(), req(http.MethodGet, "/"))
}
