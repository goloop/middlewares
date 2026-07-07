package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSPreflightAllowed(t *testing.T) {
	h := CORS(
		WithAllowedOrigins("https://app.example.com"),
		WithAllowedMethods("GET", "POST"),
		WithMaxAge(600),
	)(ok200("should not run for preflight"))

	r := httptest.NewRequest(http.MethodOptions, "/", nil)
	r.Header.Set("Origin", "https://app.example.com")
	r.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("Allow-Origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST" {
		t.Fatalf("Allow-Methods = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Max-Age"); got != "600" {
		t.Fatalf("Max-Age = %q", got)
	}
}

func TestCORSDisallowedOrigin(t *testing.T) {
	h := CORS(WithAllowedOrigins("https://good.example.com"))(ok200("ok"))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Allow-Origin = %q, want empty for disallowed origin", got)
	}
	// Non-preflight requests still reach the handler.
	if rec.Body.String() != "ok" {
		t.Fatalf("handler should still run, body = %q", rec.Body.String())
	}
}

func TestCORSNoOriginPassesThrough(t *testing.T) {
	h := CORS(WithAllowedOrigins("https://good.example.com"))(ok200("ok"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Body.String() != "ok" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestCORSWildcard(t *testing.T) {
	h := CORS(WithAllowedOrigins("*"))(ok200("ok"))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Origin", "https://anything.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Allow-Origin = %q, want *", got)
	}
}

func TestCORSCredentialsEchoesOrigin(t *testing.T) {
	h := CORS(
		WithAllowedOrigins("https://app.example.com"),
		WithAllowCredentials(),
	)(ok200("ok"))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("Allow-Origin = %q, want the specific origin (never * with creds)", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("Allow-Credentials = %q, want true", got)
	}
}

func TestCORSWildcardCredentialsPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for wildcard origin with credentials")
		}
	}()
	CORS(WithAllowedOrigins("*"), WithAllowCredentials())
}
