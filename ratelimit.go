package middlewares

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RateLimitStore decides whether one more request may be served for a key.
//
// It is an interface because the useful implementations are not the library's
// to write: a limit that holds across several instances of a service needs
// something both instances can see, and that means a dependency this package
// will not take. The shape here is what a shared store can answer efficiently -
// one call that both decides and reports what is left.
type RateLimitStore interface {
	// Take records an attempt against key and reports whether it is allowed.
	// limit and window describe the budget; a store keyed by more than one
	// budget can use them to size the bucket it creates.
	Take(ctx context.Context, key string, limit int, window time.Duration) (RateLimitResult, error)
}

// RateLimitResult is one store decision.
type RateLimitResult struct {
	// Allowed says whether to serve the request.
	Allowed bool

	// Remaining is how much budget is left after this attempt.
	Remaining int

	// RetryAfter is how long until the caller may try again. It is only
	// meaningful when Allowed is false.
	RetryAfter time.Duration

	// Reset is when the budget returns to full. Zero when the store does not
	// track it.
	Reset time.Time
}

// RateLimitConfig describes a limit.
type RateLimitConfig struct {
	// Key groups requests that share a budget. Nil means [KeyByIP].
	Key func(*http.Request) string

	// Limit is how many requests one key may make per Window. A limit of
	// zero or less is a programming error and panics at construction.
	Limit  int
	Window time.Duration

	// Store decides. Nil means a fresh [NewMemoryRateLimitStore], which is
	// per-process: with more than one instance running, each gets its own
	// budget and the effective limit multiplies by the instance count.
	Store RateLimitStore
}

type rateLimitConfig struct {
	RateLimitConfig
	failOpen bool
	status   int
}

// RateLimitOption configures the RateLimit middleware.
type RateLimitOption func(*rateLimitConfig)

// WithFailOpen serves requests when the store cannot answer, instead of
// refusing them.
//
// The default is the other way round, and deliberately: a limiter is usually
// in front of sign-in, password reset or one-time codes, and a store outage
// that silently removed the limit from exactly those routes would be an
// availability problem turning itself into a security one. Choose this for
// routes where being reachable matters more than being limited - and know that
// while the store is down, they are not limited at all.
func WithFailOpen() RateLimitOption {
	return func(c *rateLimitConfig) { c.failOpen = true }
}

// WithRateLimitStatus sets the status returned when the limit is reached
// (default 429 Too Many Requests).
func WithRateLimitStatus(code int) RateLimitOption {
	return func(c *rateLimitConfig) {
		if code != 0 {
			c.status = code
		}
	}
}

// RateLimit limits how often one caller may make a request.
//
// It is not [Throttle] and does not replace it. A rate limit answers "how
// often may this client come back"; Throttle answers "how much of this work
// fits in the process at once". Ten per minute still admits ten in the same
// instant, which is what exhausts memory in front of an expensive handler;
// ten concurrent slots do not, and neither stops a client from returning every
// second forever. In front of sign-in both belong.
//
// A rejected request gets the configured status, a Retry-After header, and the
// X-RateLimit-Limit, X-RateLimit-Remaining and X-RateLimit-Reset headers. So
// does an allowed one, minus Retry-After, so a well-behaved client can slow
// down before it is refused.
//
// RateLimit panics if Limit is not positive or Window is not positive, since
// either is a programming error rather than a runtime condition.
func RateLimit(cfg RateLimitConfig, opts ...RateLimitOption) Middleware {
	if cfg.Limit <= 0 {
		panic("middlewares: RateLimit limit must be positive")
	}
	if cfg.Window <= 0 {
		panic("middlewares: RateLimit window must be positive")
	}

	c := rateLimitConfig{
		RateLimitConfig: cfg,
		status:          http.StatusTooManyRequests,
	}
	for _, opt := range opts {
		opt(&c)
	}
	if c.Key == nil {
		c.Key = KeyByIP()
	}
	if c.Store == nil {
		c.Store = NewMemoryRateLimitStore()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			res, err := c.Store.Take(r.Context(), c.Key(r), c.Limit, c.Window)
			if err != nil {
				if c.failOpen {
					next.ServeHTTP(w, r)
					return
				}
				// Not the limit status: nothing was counted, and telling a
				// caller to slow down would be a lie about what happened.
				// This is the service being unable to serve safely.
				http.Error(w,
					http.StatusText(http.StatusServiceUnavailable),
					http.StatusServiceUnavailable)
				return
			}

			setRateLimitHeaders(w, c.Limit, res)
			if !res.Allowed {
				if res.RetryAfter > 0 {
					w.Header().Set("Retry-After",
						strconv.Itoa(int(math.Ceil(res.RetryAfter.Seconds()))))
				}
				http.Error(w, http.StatusText(c.status), c.status)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// setRateLimitHeaders reports the budget on every reply, allowed or not.
func setRateLimitHeaders(w http.ResponseWriter, limit int, res RateLimitResult) {
	h := w.Header()
	h.Set("X-RateLimit-Limit", strconv.Itoa(limit))
	h.Set("X-RateLimit-Remaining", strconv.Itoa(max(res.Remaining, 0)))
	if !res.Reset.IsZero() {
		h.Set("X-RateLimit-Reset", strconv.FormatInt(res.Reset.Unix(), 10))
	}
}

// KeyByIP groups requests by client address, using the address [RealIP]
// resolved when it ran.
//
// Read the note on RealIP before relying on this. Without WithTrustedProxies,
// every client behind a reverse proxy resolves to the proxy's own address, so
// this returns one key for all of them and the limit becomes a budget the
// whole userbase shares. Nothing errors; the limiter simply stops being
// per-client.
func KeyByIP() func(*http.Request) string {
	return func(r *http.Request) string {
		if ip := RealIPFrom(r.Context()); ip != "" {
			return ip
		}
		// Without RealIP in front, the peer address is all there is - and
		// it has to lose its port first. Every connection from one client
		// carries a different port, so keying on the pair would give each
		// request a budget of its own and limit nobody.
		return directIP(r.RemoteAddr)
	}
}

// KeyByHeader groups requests by a header value, for limits keyed by an API
// key or a tenant. Requests without the header fall back to the client
// address, so a missing header cannot buy an unlimited budget.
func KeyByHeader(name string) func(*http.Request) string {
	byIP := KeyByIP()
	return func(r *http.Request) string {
		if v := r.Header.Get(name); v != "" {
			return name + ":" + v
		}
		return byIP(r)
	}
}

// MemoryRateLimitStore is a token bucket per key, held in this process.
//
// A bucket refills continuously rather than resetting on a schedule, which is
// what keeps a burst at a window boundary from being twice the limit: a fixed
// window that resets at noon will happily serve a full budget at 11:59:59 and
// another at 12:00:00. The bucket also lets a caller who has been quiet spend
// a little faster, up to the limit, which is usually what "ten per minute"
// was meant to allow.
//
// It counts only what this process saw. Behind a load balancer with several
// instances, each keeps its own buckets and the effective limit is multiplied
// by the number of instances; a limit that has to hold across them needs a
// store they share.
type MemoryRateLimitStore struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	now     func() time.Time

	// lastSweep is when idle buckets were last dropped. Without it the map
	// grows with every key ever seen, and the keys are usually addresses.
	lastSweep time.Time
}

type bucket struct {
	tokens float64
	seen   time.Time

	// full is when the bucket is back to its own limit, worked out from the
	// limit and window it was last taken with. A store shared by several
	// limits sweeps by this and not by the window of whichever request
	// happens to trigger the sweep, so a route with a short window cannot
	// hand a fresh budget to a key still being held by a long one.
	full time.Time
}

// NewMemoryRateLimitStore returns an empty in-process token-bucket store.
func NewMemoryRateLimitStore() *MemoryRateLimitStore {
	return &MemoryRateLimitStore{
		buckets: map[string]*bucket{},
		now:     time.Now,
	}
}

// Take records an attempt and reports whether it is allowed.
func (s *MemoryRateLimitStore) Take(
	_ context.Context,
	key string,
	limit int,
	window time.Duration,
) (RateLimitResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.sweep(now, window)

	rate := float64(limit) / window.Seconds() // tokens per second

	b, ok := s.buckets[key]
	if !ok {
		b = &bucket{tokens: float64(limit), seen: now}
		s.buckets[key] = b
	} else {
		b.tokens = min(float64(limit),
			b.tokens+now.Sub(b.seen).Seconds()*rate)
		b.seen = now
	}

	var res RateLimitResult
	if b.tokens < 1 {
		// Time until one token is back, which is when a retry can succeed.
		res.RetryAfter = time.Duration((1 - b.tokens) / rate * float64(time.Second))
		res.Remaining = 0
	} else {
		b.tokens--
		res.Allowed = true
		res.Remaining = int(b.tokens)
	}

	b.full = now.Add(time.Duration((float64(limit) - b.tokens) / rate * float64(time.Second)))
	res.Reset = b.full
	return res, nil
}

// sweep drops buckets that are back to full, since a full bucket says nothing
// a fresh one would not, so the map does not grow for the life of the process.
// Each bucket is judged by its own refill time; the window of the request that
// triggers the sweep only decides how often the map is walked.
func (s *MemoryRateLimitStore) sweep(now time.Time, window time.Duration) {
	if now.Sub(s.lastSweep) < window {
		return
	}
	s.lastSweep = now

	for key, b := range s.buckets {
		if !now.Before(b.full) {
			delete(s.buckets, key)
		}
	}
}
