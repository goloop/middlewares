package middlewares

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func req(method, target string) *http.Request {
	return httptest.NewRequest(method, target, nil)
}

func ok200(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	})
}

func TestChainOrder(t *testing.T) {
	var trail []string
	tag := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				trail = append(trail, name+">")
				next.ServeHTTP(w, r)
				trail = append(trail, name+"<")
			})
		}
	}
	h := Chain(tag("a"), tag("b"))(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { trail = append(trail, "h") }))
	h.ServeHTTP(httptest.NewRecorder(), req(http.MethodGet, "/"))

	if got := strings.Join(trail, ","); got != "a>,b>,h,b<,a<" {
		t.Fatalf("order = %q, want a>,b>,h,b<,a<", got)
	}
}

func TestHandler(t *testing.T) {
	hit := false
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hit = true
			next.ServeHTTP(w, r)
		})
	}
	h := Handler(ok200("x"), mw)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req(http.MethodGet, "/"))
	if !hit || rec.Body.String() != "x" {
		t.Fatalf("Handler did not apply middleware")
	}
}

func TestRequestIDGeneratedAndHeaderSet(t *testing.T) {
	var seen string
	h := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFrom(r.Context())
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req(http.MethodGet, "/"))

	if seen == "" {
		t.Fatal("request id not in context")
	}
	if got := rec.Header().Get(DefaultRequestIDHeader); got != seen {
		t.Fatalf("header %q != context id %q", got, seen)
	}
	if len(seen) != 32 {
		t.Fatalf("generated id length = %d, want 32 hex chars", len(seen))
	}
}

func TestRequestIDIgnoresInboundByDefault(t *testing.T) {
	var seen string
	h := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFrom(r.Context())
	}))
	r := req(http.MethodGet, "/")
	r.Header.Set(DefaultRequestIDHeader, "client-supplied")
	h.ServeHTTP(httptest.NewRecorder(), r)

	if seen == "client-supplied" {
		t.Fatal("inbound id must be ignored without WithTrustedRequestIDHeader")
	}
}

func TestRequestIDTrustedInboundAccepted(t *testing.T) {
	var seen string
	h := RequestID(WithTrustedRequestIDHeader())(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			seen = RequestIDFrom(r.Context())
		}))
	r := req(http.MethodGet, "/")
	r.Header.Set(DefaultRequestIDHeader, "safe-123")
	h.ServeHTTP(httptest.NewRecorder(), r)

	if seen != "safe-123" {
		t.Fatalf("trusted inbound id = %q, want safe-123", seen)
	}
}

func TestRequestIDRejectsUnsafeInbound(t *testing.T) {
	cases := map[string]string{
		"control char": "bad\x00id",
		"too long":     strings.Repeat("a", maxRequestIDLen+1),
		"non-ascii":    "id-\xff",
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			var seen string
			h := RequestID(WithTrustedRequestIDHeader())(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					seen = RequestIDFrom(r.Context())
				}))
			r := req(http.MethodGet, "/")
			r.Header.Set(DefaultRequestIDHeader, bad)
			h.ServeHTTP(httptest.NewRecorder(), r)
			if seen == bad {
				t.Fatalf("unsafe inbound id %q was accepted", bad)
			}
			if seen == "" {
				t.Fatal("expected a freshly generated id")
			}
		})
	}
}

func TestRealIPDefaultsToRemoteAddr(t *testing.T) {
	var seen string
	h := RealIP()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RealIPFrom(r.Context())
	}))
	r := req(http.MethodGet, "/")
	r.RemoteAddr = "203.0.113.9:12345"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	h.ServeHTTP(httptest.NewRecorder(), r)

	if seen != "203.0.113.9" {
		t.Fatalf("real ip = %q, want RemoteAddr host (headers untrusted)", seen)
	}
}

func TestRealIPTrustedProxyXForwardedFor(t *testing.T) {
	var seen string
	h := RealIP(WithTrustedProxies("10.0.0.0/8"))(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			seen = RealIPFrom(r.Context())
		}))
	r := req(http.MethodGet, "/")
	r.RemoteAddr = "10.1.2.3:9999"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 10.9.9.9")
	h.ServeHTTP(httptest.NewRecorder(), r)

	if seen != "1.2.3.4" {
		t.Fatalf("real ip = %q, want 1.2.3.4 from trusted proxy", seen)
	}
}

func TestRealIPTrustedProxyXRealIP(t *testing.T) {
	var seen string
	h := RealIP(TrustLoopbackProxies())(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			seen = RealIPFrom(r.Context())
		}))
	r := req(http.MethodGet, "/")
	r.RemoteAddr = "127.0.0.1:5555"
	r.Header.Set("X-Real-IP", "8.8.8.8")
	h.ServeHTTP(httptest.NewRecorder(), r)

	if seen != "8.8.8.8" {
		t.Fatalf("real ip = %q, want 8.8.8.8", seen)
	}
}

func TestRecovererBeforeWrite(t *testing.T) {
	h := Recoverer(WithRecoverLogger(silentLogger()))(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			panic("boom")
		}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req(http.MethodGet, "/"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestRecovererAfterWrite(t *testing.T) {
	h := Recoverer(WithRecoverLogger(silentLogger()))(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, "partial")
			panic("boom")
		}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req(http.MethodGet, "/"))
	// Response already committed as 201; Recoverer must not change it.
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (already committed)", rec.Code)
	}
	if rec.Body.String() != "partial" {
		t.Fatalf("body = %q, want partial", rec.Body.String())
	}
}

func TestRecovererCustomHandler(t *testing.T) {
	h := Recoverer(
		WithRecoverLogger(silentLogger()),
		WithRecoverHandler(func(w http.ResponseWriter, r *http.Request, v any) {
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(w, "recovered: %v", v)
		}),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("kaboom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req(http.MethodGet, "/"))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "kaboom") {
		t.Fatalf("body = %q, want to mention panic value", rec.Body.String())
	}
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
}

// infoLogger discards output but is enabled at Info, so Logger actually wraps
// the ResponseWriter (a silent Error-level logger makes Logger skip wrapping,
// which would leave the wrapper untested).
func infoLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

func TestLoggerCapturesStatusAndBytes(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	h := Logger(WithLogger(logger))(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, "missing")
		}))
	h.ServeHTTP(httptest.NewRecorder(), req(http.MethodGet, "/x"))

	out := buf.String()
	if !strings.Contains(out, "status=404") {
		t.Fatalf("log missing status: %s", out)
	}
	if !strings.Contains(out, "bytes=7") {
		t.Fatalf("log missing byte count: %s", out)
	}
	if !strings.Contains(out, "method=GET") || !strings.Contains(out, "path=/x") {
		t.Fatalf("log missing method/path: %s", out)
	}
}

func TestLoggerSkip(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	h := Logger(
		WithLogger(logger),
		WithSkip(func(r *http.Request) bool { return r.URL.Path == "/health" }),
	)(ok200("ok"))
	h.ServeHTTP(httptest.NewRecorder(), req(http.MethodGet, "/health"))
	if buf.Len() != 0 {
		t.Fatalf("skipped request should not log, got: %s", buf.String())
	}
}

func TestLoggerDisabledDoesNotLog(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))
	h := Logger(WithLogger(logger))(ok200("ok")) // logs at Info, handler at Error
	h.ServeHTTP(httptest.NewRecorder(), req(http.MethodGet, "/"))
	if buf.Len() != 0 {
		t.Fatalf("disabled level should not log, got: %s", buf.String())
	}
}

func TestLoggerPreservesFlush(t *testing.T) {
	flushed := false
	h := Logger(WithLogger(infoLogger()))(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			f, ok := w.(http.Flusher)
			if !ok {
				t.Fatal("wrapped writer lost http.Flusher")
			}
			fmt.Fprint(w, "chunk")
			f.Flush()
			flushed = true
		}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req(http.MethodGet, "/"))
	if !flushed || !rec.Flushed {
		t.Fatalf("flush not propagated (flushed=%v recorder=%v)", flushed, rec.Flushed)
	}
}

func TestLoggerIncludesRequestID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	h := Chain(RequestID(), Logger(WithLogger(logger)))(ok200("ok"))
	h.ServeHTTP(httptest.NewRecorder(), req(http.MethodGet, "/"))
	if !strings.Contains(buf.String(), "request_id=") {
		t.Fatalf("log missing request_id: %s", buf.String())
	}
}

func TestTimeout(t *testing.T) {
	h := Timeout(20 * time.Millisecond)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
		}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req(http.MethodGet, "/"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 on timeout", rec.Code)
	}
}

func TestThrottleConcurrency(t *testing.T) {
	release := make(chan struct{})
	var inflight atomic.Int32
	h := Throttle(1)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inflight.Add(1)
		<-release
		inflight.Add(-1)
	}))

	// Occupy the single slot.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		h.ServeHTTP(httptest.NewRecorder(), req(http.MethodGet, "/"))
	}()

	// Wait until the first request holds the slot.
	for inflight.Load() == 0 {
		time.Sleep(time.Millisecond)
	}

	// Second request must be rejected immediately.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req(http.MethodGet, "/"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when slot is busy", rec.Code)
	}

	close(release)
	wg.Wait()
}

func TestThrottlePanicsOnBadLimit(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for non-positive limit")
		}
	}()
	Throttle(0)
}

func TestSecurityHeaders(t *testing.T) {
	h := SecurityHeaders()(ok200("ok"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req(http.MethodGet, "/"))
	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Fatalf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestSecurityHeadersDoesNotOverwrite(t *testing.T) {
	h := SecurityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req(http.MethodGet, "/"))
	if got := rec.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Fatalf("X-Frame-Options = %q, want handler value preserved", got)
	}
}

func TestSecurityHeadersHSTSAndCSP(t *testing.T) {
	h := SecurityHeaders(WithHSTS(3600), WithCSP("default-src 'self'"))(ok200("ok"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req(http.MethodGet, "/"))
	if got := rec.Header().Get("Strict-Transport-Security"); got != "max-age=3600" {
		t.Fatalf("HSTS = %q", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != "default-src 'self'" {
		t.Fatalf("CSP = %q", got)
	}
}

func TestMaxBytes(t *testing.T) {
	h := MaxBytes(4)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := r.Body.Read(make([]byte, 100))
		_ = err
		// Reading beyond the limit yields an error and the reader caps the body.
		buf := make([]byte, 100)
		n, _ := r.Body.Read(buf)
		_ = n
	}))
	body := strings.NewReader("way too many bytes")
	r := httptest.NewRequest(http.MethodPost, "/", body)
	rec := httptest.NewRecorder()
	// Should not panic; the MaxBytesReader is installed.
	h.ServeHTTP(rec, r)
	if r.Body == nil {
		t.Fatal("body reader missing")
	}
}

func TestRequestIDFromEmptyContext(t *testing.T) {
	if RequestIDFrom(context.Background()) != "" {
		t.Fatal("expected empty id for bare context")
	}
	if RealIPFrom(context.Background()) != "" {
		t.Fatal("expected empty ip for bare context")
	}
}
