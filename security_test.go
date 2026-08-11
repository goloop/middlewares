package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHSTSPolicy(t *testing.T) {
	tests := []struct {
		name string
		opt  SecurityOption
		want string
	}{
		{
			"max-age only",
			WithHSTSPolicy(HSTS{MaxAge: 365 * 24 * time.Hour}),
			"max-age=31536000",
		},
		{
			"with subdomains",
			WithHSTSPolicy(HSTS{
				MaxAge:            365 * 24 * time.Hour,
				IncludeSubDomains: true,
			}),
			"max-age=31536000; includeSubDomains",
		},
		{
			"the full policy",
			WithHSTSPolicy(HSTS{
				MaxAge:            365 * 24 * time.Hour,
				IncludeSubDomains: true,
				Preload:           true,
			}),
			"max-age=31536000; includeSubDomains; preload",
		},
		{
			// The browser lists reject preload without includeSubDomains, so
			// emitting it alone would be a header that can never be honoured.
			"preload without subdomains is not emitted",
			WithHSTSPolicy(HSTS{MaxAge: time.Hour, Preload: true}),
			"max-age=3600",
		},
		{
			"a zero policy omits the header",
			WithHSTSPolicy(HSTS{IncludeSubDomains: true}),
			"",
		},
		{
			"the seconds form still works",
			WithHSTS(600),
			"max-age=600",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := SecurityHeaders(tt.opt)(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {}))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

			if got := w.Header().Get("Strict-Transport-Security"); got != tt.want {
				t.Errorf("Strict-Transport-Security = %q, want %q", got, tt.want)
			}
		})
	}
}
