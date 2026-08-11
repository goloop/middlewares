package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A third project assumed RealIP normalized the forwarded address into
// RemoteAddr, keyed its rate limit on RemoteAddr, and got one shared bucket
// for its whole userbase behind nginx. The assumption is easy to make and the
// failure is silent, so the promise that RemoteAddr is untouched is pinned
// here rather than only stated in the documentation.
func TestRealIPLeavesRemoteAddrAlone(t *testing.T) {
	const peer = "127.0.0.1:5555"

	var seenRemoteAddr, seenContext string
	h := RealIP(TrustLoopbackProxies())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seenRemoteAddr = r.RemoteAddr
			seenContext = RealIPFrom(r.Context())
		}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = peer
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	h.ServeHTTP(httptest.NewRecorder(), r)

	if seenContext != "203.0.113.7" {
		t.Errorf("RealIPFrom() = %q, want the forwarded address", seenContext)
	}
	if seenRemoteAddr != peer {
		t.Errorf("RemoteAddr = %q, want it untouched at %q - code that reads "+
			"it must keep getting the peer, so the difference stays visible",
			seenRemoteAddr, peer)
	}
}
