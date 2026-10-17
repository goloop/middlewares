package middlewares

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompressRespectsQuality(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  bool
	}{{"gzip;q=0", false}, {"gzip;q=0.0", false}, {"gzip;q=0.5", true}, {"br, gzip", true}, {"gzip;q=invalid", false}, {"gzip;q=NaN", false}} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Encoding", tt.value)
		if got := acceptsGzip(r); got != tt.want {
			t.Errorf("%s: %v", tt.value, got)
		}
	}
}

func TestCompressPreservesImplicitStatusAndVary(t *testing.T) {
	h := Compress()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("small"))
		w.WriteHeader(http.StatusTeapot)
	}))
	for _, encoding := range []string{"", "gzip"} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Encoding", encoding)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d", w.Code)
		}
		if !strings.Contains(strings.Join(w.Header().Values("Vary"), ","), "Accept-Encoding") {
			t.Fatal("missing Vary")
		}
	}
}
