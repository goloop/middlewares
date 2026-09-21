package middlewares

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// okHandler counts how many requests actually reached the handler.
func okHandler(served *int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*served++
		w.WriteHeader(http.StatusOK)
	})
}

func doGet(h http.Handler, addr string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = addr
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestRateLimitRefusesOverTheLimit(t *testing.T) {
	served := 0
	h := RateLimit(RateLimitConfig{
		Limit:  3,
		Window: time.Minute,
	})(okHandler(&served))

	for i := range 3 {
		if got := doGet(h, "10.0.0.1:1").Code; got != http.StatusOK {
			t.Fatalf("request %d = %d, want 200", i+1, got)
		}
	}

	w := doGet(h, "10.0.0.1:1")
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("the fourth request = %d, want 429", w.Code)
	}
	if served != 3 {
		t.Errorf("the handler ran %d times, want 3", served)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("a refused request carries no Retry-After")
	}
	if got := w.Header().Get("X-RateLimit-Remaining"); got != "0" {
		t.Errorf("X-RateLimit-Remaining = %q, want 0", got)
	}
}

// The budget is per key, or it is not a limit on anyone in particular.
func TestRateLimitKeysSeparately(t *testing.T) {
	served := 0
	h := RateLimit(RateLimitConfig{Limit: 1, Window: time.Minute})(okHandler(&served))

	doGet(h, "10.0.0.1:1")
	if got := doGet(h, "10.0.0.2:1").Code; got != http.StatusOK {
		t.Errorf("a second client = %d, want 200 - the budget is not shared", got)
	}
	if got := doGet(h, "10.0.0.1:1").Code; got != http.StatusTooManyRequests {
		t.Errorf("the first client = %d, want 429", got)
	}
}

// An allowed request reports the budget too, so a client can slow down before
// it is refused rather than after.
func TestRateLimitHeadersOnAllowedRequests(t *testing.T) {
	h := RateLimit(RateLimitConfig{Limit: 5, Window: time.Minute})(
		okHandler(new(int)))

	w := doGet(h, "10.0.0.1:1")
	if got := w.Header().Get("X-RateLimit-Limit"); got != "5" {
		t.Errorf("X-RateLimit-Limit = %q, want 5", got)
	}
	if got := w.Header().Get("X-RateLimit-Remaining"); got != "4" {
		t.Errorf("X-RateLimit-Remaining = %q, want 4", got)
	}
	if w.Header().Get("X-RateLimit-Reset") == "" {
		t.Error("no X-RateLimit-Reset on an allowed request")
	}
}

// brokenStore stands in for a shared store that cannot be reached.
type brokenStore struct{}

func (brokenStore) Take(context.Context, string, int, time.Duration) (RateLimitResult, error) {
	return RateLimitResult{}, errors.New("dial tcp: connection refused")
}

// The default when the store is down is to refuse, because a limiter usually
// sits in front of sign-in and a store outage must not quietly take the limit
// off exactly those routes.
func TestRateLimitFailsClosedByDefault(t *testing.T) {
	served := 0
	h := RateLimit(RateLimitConfig{
		Limit: 1, Window: time.Minute, Store: brokenStore{},
	})(okHandler(&served))

	w := doGet(h, "10.0.0.1:1")
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("a broken store = %d, want 503", w.Code)
	}
	if w.Code == http.StatusTooManyRequests {
		t.Error("a store outage was reported as the caller's fault")
	}
	if served != 0 {
		t.Error("the handler ran while the limiter could not decide")
	}
}

// Failing open is available and has to be asked for.
func TestRateLimitFailOpenIsOptIn(t *testing.T) {
	served := 0
	h := RateLimit(RateLimitConfig{
		Limit: 1, Window: time.Minute, Store: brokenStore{},
	}, WithFailOpen())(okHandler(&served))

	if got := doGet(h, "10.0.0.1:1").Code; got != http.StatusOK {
		t.Errorf("with WithFailOpen the request = %d, want 200", got)
	}
	if served != 1 {
		t.Error("the handler did not run under WithFailOpen")
	}
}

// A bucket refills continuously, which is the reason to prefer it: a fixed
// window would let a full budget through on each side of a boundary.
func TestMemoryStoreRefillsGradually(t *testing.T) {
	now := time.Now()
	s := NewMemoryRateLimitStore()
	s.now = func() time.Time { return now }

	ctx := context.Background()
	for range 4 {
		if _, err := s.Take(ctx, "k", 4, time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	res, _ := s.Take(ctx, "k", 4, time.Minute)
	if res.Allowed {
		t.Fatal("the budget was not spent")
	}

	// A quarter of the window buys back exactly one request, not the lot.
	now = now.Add(15 * time.Second)
	if res, _ := s.Take(ctx, "k", 4, time.Minute); !res.Allowed {
		t.Error("a quarter window bought back nothing")
	}
	if res, _ := s.Take(ctx, "k", 4, time.Minute); res.Allowed {
		t.Error("a quarter window bought back more than one request")
	}
}

// The boundary case a fixed window gets wrong: two full budgets back to back.
func TestMemoryStoreHasNoWindowBoundaryBurst(t *testing.T) {
	now := time.Now()
	s := NewMemoryRateLimitStore()
	s.now = func() time.Time { return now }

	ctx := context.Background()
	allowed := 0
	spend := func() {
		for range 10 {
			if res, _ := s.Take(ctx, "k", 10, time.Minute); res.Allowed {
				allowed++
			}
		}
	}

	spend()                    // full budget at the end of a window
	now = now.Add(time.Second) // a moment later, as a reset would land
	spend()                    // a fixed window would grant another 10

	if allowed > 11 {
		t.Errorf("%d requests went through around a boundary, want about the "+
			"limit - a burst of two budgets is what a fixed window allows",
			allowed)
	}
}

// RetryAfter has to be the time until a retry can actually succeed, or a
// client that obeys it is refused again.
func TestMemoryStoreRetryAfterIsHonest(t *testing.T) {
	now := time.Now()
	s := NewMemoryRateLimitStore()
	s.now = func() time.Time { return now }

	ctx := context.Background()
	if _, err := s.Take(ctx, "k", 1, time.Minute); err != nil {
		t.Fatal(err)
	}
	res, _ := s.Take(ctx, "k", 1, time.Minute)
	if res.Allowed || res.RetryAfter <= 0 {
		t.Fatalf("expected a refusal with a retry hint, got %+v", res)
	}

	now = now.Add(res.RetryAfter)
	if res, _ := s.Take(ctx, "k", 1, time.Minute); !res.Allowed {
		t.Error("a client that waited exactly RetryAfter was refused again")
	}
}

// A limit that is not a limit is a programming error, and finding it at
// startup beats finding it in production.
func TestRateLimitRejectsNonsenseConfig(t *testing.T) {
	for _, cfg := range []RateLimitConfig{
		{Limit: 0, Window: time.Minute},
		{Limit: 1, Window: 0},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("RateLimit(%+v) did not panic", cfg)
				}
			}()
			RateLimit(cfg)
		}()
	}
}

func TestKeyByHeaderFallsBackToAddress(t *testing.T) {
	key := KeyByHeader("X-API-Key")

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1"
	if got := key(r); got != "10.0.0.1" {
		t.Errorf("without the header the key = %q, want the address", got)
	}

	r.Header.Set("X-API-Key", "abc")
	if got := key(r); got != "X-API-Key:abc" {
		t.Errorf("with the header the key = %q", got)
	}
}

// RealIP and the limiter have to agree, or the limit is keyed by something
// nobody configured.
func TestKeyByIPUsesRealIP(t *testing.T) {
	var key string
	h := RealIP(TrustLoopbackProxies())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key = KeyByIP()(r)
		}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	h.ServeHTTP(httptest.NewRecorder(), r)

	if key != "203.0.113.9" {
		t.Errorf("KeyByIP() = %q, want the address RealIP resolved", key)
	}
}

// Every key ever seen must not stay in memory for the life of the process.
func TestMemoryStoreForgetsIdleKeys(t *testing.T) {
	now := time.Now()
	s := NewMemoryRateLimitStore()
	s.now = func() time.Time { return now }

	ctx := context.Background()
	for i := range 50 {
		if _, err := s.Take(ctx, strconv.Itoa(i), 5, time.Second); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.buckets) != 50 {
		t.Fatalf("buckets = %d, want 50", len(s.buckets))
	}

	now = now.Add(time.Minute)
	if _, err := s.Take(ctx, "fresh", 5, time.Second); err != nil {
		t.Fatal(err)
	}
	if len(s.buckets) > 2 {
		t.Errorf("buckets = %d after a long idle period, want the idle ones "+
			"dropped", len(s.buckets))
	}
}

// A store shared by several limits must not let a route with a short window
// hand a fresh budget to a key that a long window is still holding: each
// bucket is swept by its own refill time, not by the window of the request
// that happens to trigger the sweep.
func TestMemoryStoreSharedAcrossWindowsKeepsLongBudgets(t *testing.T) {
	now := time.Now()
	s := NewMemoryRateLimitStore()
	s.now = func() time.Time { return now }

	ctx := context.Background()
	first, _ := s.Take(ctx, "login:victim", 1, time.Hour)
	second, _ := s.Take(ctx, "login:victim", 1, time.Hour)
	if !first.Allowed || second.Allowed {
		t.Fatalf("got %v, %v; want the single hourly attempt spent",
			first.Allowed, second.Allowed)
	}

	// Traffic on an unrelated route with a short window, long enough after
	// the previous sweep to trigger another one.
	now = now.Add(5 * time.Second)
	if _, err := s.Take(ctx, "other", 100, time.Second); err != nil {
		t.Fatal(err)
	}

	third, _ := s.Take(ctx, "login:victim", 1, time.Hour)
	if third.Allowed {
		t.Error("a short-window route reset a budget an hourly limit was still holding")
	}

	// Once the hour has passed the key is back to full and may be dropped.
	now = now.Add(time.Hour)
	if _, err := s.Take(ctx, "other", 100, time.Second); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.buckets["login:victim"]; ok {
		t.Error("a bucket that is back to full was kept")
	}
}
