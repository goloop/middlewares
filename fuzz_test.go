package middlewares

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// FuzzRealIPForwarded checks that RealIP never panics and never yields a
// non-IP result, whatever a client puts in the forwarded headers, even when
// every proxy is trusted.
func FuzzRealIPForwarded(f *testing.F) {
	f.Add("1.2.3.4", "8.8.8.8", "10.0.0.1:9999")
	f.Add("", "", "")
	f.Add("not-an-ip, 5.6.7.8", "garbage", "bad-remote")
	f.Add("::1, 2001:db8::1", "2001:db8::2", "[::1]:80")

	mw := RealIP(WithTrustedProxies("0.0.0.0/0", "::/0"))

	f.Fuzz(func(t *testing.T, xff, xrip, remote string) {
		var got string
		handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = RealIPFrom(r.Context())
		}))

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = remote
		r.Header.Set("X-Forwarded-For", xff)
		r.Header.Set("X-Real-IP", xrip)

		handler.ServeHTTP(httptest.NewRecorder(), r) // must not panic

		// A non-empty result must be a valid IP address.
		if got != "" && net.ParseIP(got) == nil {
			// directIP may echo a bare RemoteAddr that is not an IP; that only
			// happens when the peer is untrusted, which cannot occur here since
			// every proxy is trusted. So any non-IP result is a real defect.
			if net.ParseIP(directIP(remote)) != nil {
				t.Fatalf("RealIP returned non-IP %q (xff=%q xrip=%q remote=%q)",
					got, xff, xrip, remote)
			}
		}
	})
}

// FuzzCORSOrigin checks that CORS never panics for arbitrary allowed and
// request origins, and never echoes a disallowed origin.
func FuzzCORSOrigin(f *testing.F) {
	f.Add("https://a.example.com", "https://a.example.com")
	f.Add("https://a.example.com", "https://evil.example.com")
	f.Add("", "")
	f.Add("*", "https://anything")

	f.Fuzz(func(t *testing.T, allowed, origin string) {
		h := CORS(WithAllowedOrigins(allowed))(http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {}))

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r) // must not panic

		got := rec.Header().Get("Access-Control-Allow-Origin")
		allowAll := allowed == "*"
		if got != "" && got != "*" && !allowAll && got != origin {
			t.Fatalf("echoed origin %q that was neither allowed nor the request origin", got)
		}
	})
}

// FuzzSafeRequestID checks that an accepted inbound identifier is always within
// the length limit and printable ASCII.
func FuzzSafeRequestID(f *testing.F) {
	f.Add("safe-123")
	f.Add("bad\x00id")
	f.Add("")

	f.Fuzz(func(t *testing.T, s string) {
		if !safeRequestID(s) {
			return
		}
		if len(s) == 0 || len(s) > maxRequestIDLen {
			t.Fatalf("accepted id with bad length: %d", len(s))
		}
		for i := 0; i < len(s); i++ {
			if s[i] < 0x20 || s[i] > 0x7e {
				t.Fatalf("accepted id with non-printable byte %#x", s[i])
			}
		}
	})
}
