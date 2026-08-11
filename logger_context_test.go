package middlewares

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Correlation must be the default once asked for, not a thing every author of
// every log line has to remember.
func TestContextLoggerCarriesTheRequestID(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(slog.NewJSONHandler(&buf, nil))

	h := Chain(
		RequestID(),
		Logger(WithLogger(base), WithContextLogger()),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		LoggerFrom(r.Context()).Warn("upstream refused")
	}))

	r := httptest.NewRequest(http.MethodGet, "/geo/mosaic", nil)
	h.ServeHTTP(httptest.NewRecorder(), r)

	var handlerLine map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) != nil {
			continue
		}
		if m["msg"] == "upstream refused" {
			handlerLine = m
		}
	}
	if handlerLine == nil {
		t.Fatalf("the handler's line is missing:\n%s", buf.String())
	}

	for _, key := range []string{"request_id", "method", "path"} {
		if handlerLine[key] == nil || handlerLine[key] == "" {
			t.Errorf("the handler's line carries no %q: %v", key, handlerLine)
		}
	}
	if handlerLine["path"] != "/geo/mosaic" {
		t.Errorf("path = %v, want the request's own", handlerLine["path"])
	}
}

// Without the option nothing is put in the context, and a handler that asks
// anyway gets something usable rather than nil.
func TestLoggerFromWithoutTheOption(t *testing.T) {
	var got *slog.Logger
	h := Logger()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = LoggerFrom(r.Context())
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if got == nil {
		t.Fatal("LoggerFrom returned nil")
	}
	if got != slog.Default() {
		t.Error("LoggerFrom did not fall back to slog.Default")
	}

	if l := LoggerFrom(context.Background()); l != slog.Default() {
		t.Error("LoggerFrom outside a request did not fall back to slog.Default")
	}
}
