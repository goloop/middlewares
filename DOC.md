# middlewares - reference

Production-ready HTTP middleware for `net/http`, with no third-party
dependencies. Every middleware is a `func(http.Handler) http.Handler`.

## Table of contents

- [Composition](#composition)
- [RequestID](#requestid)
- [RealIP](#realip)
- [Recoverer](#recoverer)
- [Logger](#logger)
- [Timeout](#timeout)
- [Compress](#compress)
- [Throttle](#throttle)
- [RateLimit](#ratelimit)
- [CORS](#cors)
- [SecurityHeaders](#securityheaders)
- [MaxBytes](#maxbytes)
- [Design notes](#design-notes)

## Composition

```go
h := middlewares.Chain(a, b, c)(handler) // a -> b -> c -> handler
h := middlewares.Handler(handler, a, b, c) // same, one call
```

The first middleware is the outermost wrapper. Any router that accepts
`func(http.Handler) http.Handler` (including `goloop/mux` via `Use`) accepts
these directly.

## RequestID

Assigns each request an identifier, stores it in the context and echoes it in a
response header.

```go
middlewares.RequestID(
    middlewares.WithRequestIDHeader("X-Request-ID"),        // default
    middlewares.WithRequestIDGenerator(func() string {...}), // default: 128-bit random
    middlewares.WithTrustedRequestIDHeader(),               // accept safe inbound ids
)

id := middlewares.RequestIDFrom(r.Context())
```

Inbound identifiers are ignored by default. With `WithTrustedRequestIDHeader`,
an inbound value is accepted only if it is printable ASCII and at most 128 bytes;
otherwise a fresh one is generated.

## RealIP

Resolves the client IP into the context, read with `middlewares.RealIPFrom`.

```go
middlewares.RealIP(
    middlewares.WithTrustedProxies("10.0.0.0/8", "192.168.0.0/16"),
    middlewares.TrustLoopbackProxies(),
)
```

**Proxy trust model.** By default the direct peer address (`RemoteAddr`) is
used and forwarded headers are ignored - otherwise any client could spoof
`X-Forwarded-For`. Forwarded headers are honored only when `RemoteAddr` is
inside a trusted proxy range: then `X-Real-IP` is preferred, else the left-most
entry of `X-Forwarded-For`.

## Recoverer

Catches a panic, logs it with a stack trace, and turns it into a response.

```go
middlewares.Recoverer(
    middlewares.WithRecoverLogger(logger),  // default: slog.Default
    middlewares.WithRecoverHandler(func(w http.ResponseWriter, r *http.Request, v any) {
        // render your own error page/JSON
    }),
)
```

**Recoverer cannot change a committed response.** If the handler already wrote a
status and body before panicking, Recoverer only logs the panic. On an
uncommitted response it runs the configured handler, or sends a bare `500`.
`http.ErrAbortHandler` is re-panicked so the server aborts as usual.

## Logger

One structured line per request via `log/slog`.

```go
middlewares.Logger(
    middlewares.WithLogger(logger),          // default: slog.Default
    middlewares.WithLogLevel(slog.LevelInfo), // default
    middlewares.WithLogMessage("http request"),
    middlewares.WithSkip(func(r *http.Request) bool { return r.URL.Path == "/health" }),
)
```

Attributes: `method`, `path`, `status`, `bytes`, `duration`, and `request_id`
when `RequestID` ran earlier. No work is done when the request is skipped or the
logger is not enabled at the level.

`WithContextLogger()` additionally puts a request-scoped logger into the
context, already carrying `request_id`, `method`, `path` (and `remote_ip` when
`RealIP` ran), so handler logs correlate with the request line without anyone
remembering to add identifiers:

```go
h := middlewares.Logger(middlewares.WithContextLogger())
// in a handler:
middlewares.LoggerFrom(r.Context()).Warn("upstream refused", "status", code)
```

`LoggerFrom` falls back to `slog.Default()` outside a request or without the
option, so a call site never has to check. The option is opt-in because it
allocates per request.

## Timeout

A per-request deadline built on `http.TimeoutHandler`.

```go
middlewares.Timeout(5*time.Second, middlewares.WithTimeoutMessage("timeout"))
```

On expiry it cancels the context and replies `503`. **It does not kill your
handler goroutine** - a long handler must observe `r.Context().Done()` to
actually stop. The timeout reply and the handler's writes are synchronized by
`http.TimeoutHandler`, so they cannot race.

## Compress

Transparent gzip for clients that accept it.

```go
middlewares.Compress(
    middlewares.WithCompressLevel(gzip.DefaultCompression),
    middlewares.WithCompressMinBytes(1024), // default
    middlewares.WithCompressTypes("text/html", "application/json", ...),
)
```

**Compression skip rules.** A response is not compressed when: the client does
not send `Accept-Encoding: gzip`; the method is `HEAD`; the status is `204` or
`304`; a `Content-Encoding` is already set; the media type is not in the
compressible set; the media type is `text/event-stream`; or the body is smaller
than the minimum. A compressed response sets `Content-Encoding: gzip`, drops
`Content-Length` and adds `Vary: Accept-Encoding`. gzip writers are pooled.

## Throttle

An in-process concurrency limiter.

```go
middlewares.Throttle(100,
    middlewares.WithThrottleStatus(http.StatusServiceUnavailable), // default 503
    middlewares.WithThrottleRetryAfter(1),                         // seconds; 0 omits
)
```

At most `limit` requests run at once; further requests are rejected immediately
rather than queued. This is per process, not a distributed rate limiter.
`Throttle` panics if `limit` is not positive.

It answers a different question from `RateLimit`: how much of this work fits in
the process at once, versus how often one client may come back. In front of
expensive work such as password hashing, both belong.

## RateLimit

How often one caller may return, keyed however you choose.

```go
middlewares.RateLimit(middlewares.RateLimitConfig{
    Key:    middlewares.KeyByIP(),               // default; or KeyByHeader("X-API-Key")
    Limit:  20,
    Window: 15 * time.Minute,
    Store:  middlewares.NewMemoryRateLimitStore(), // default; per process
},
    middlewares.WithRateLimitStatus(429),          // default
    // middlewares.WithFailOpen(),                 // see below before using
)
```

A refused request gets the configured status, `Retry-After` and the
`X-RateLimit-Limit`/`-Remaining`/`-Reset` headers; an allowed one gets the same
headers minus `Retry-After`, so a client can slow down before it is refused.

The memory store is a token bucket, which is why there is no double burst at a
window boundary the way a fixed window allows. It counts only what this
process saw: behind a load balancer each instance keeps its own buckets, and a
limit that must hold across them needs a `RateLimitStore` the instances share.
The interface is one method, so a Redis or database implementation is a short
piece of application code rather than a dependency of this package.

When the store cannot answer, the request is refused with 503. A limiter
usually guards sign-in, password reset and one-time codes, and a store outage
that silently removed the limit from exactly those routes would turn an
availability problem into a security one. `WithFailOpen` chooses the other
trade, explicitly, for routes where reachability matters more.

Anything keyed by client address inherits the `RealIP` caveat: behind a proxy
with no trusted-proxy policy, every client resolves to the proxy and the limit
becomes one bucket shared by everyone. `RateLimit` panics if `Limit` or
`Window` is not positive.

## CORS

Cross-Origin Resource Sharing, safe by default.

```go
middlewares.CORS(
    middlewares.WithAllowedOrigins("https://app.example.com"), // or "*"
    middlewares.WithAllowedMethods("GET", "POST"),
    middlewares.WithAllowedHeaders("Authorization", "Content-Type"),
    middlewares.WithExposedHeaders("X-Total-Count"),
    middlewares.WithAllowCredentials(),
    middlewares.WithMaxAge(600),
)
```

Nothing is allowed until you name an origin. A preflight `OPTIONS` from an
allowed origin is answered with `204`. With credentials the specific origin is
echoed (never `*`). A wildcard origin together with credentials is an invalid
combination and **panics at construction**.

## SecurityHeaders

A conservative set of response headers, without overwriting values the handler
already set.

```go
middlewares.SecurityHeaders(
    middlewares.WithFrameOptions("DENY"),          // default
    middlewares.WithReferrerPolicy("no-referrer"), // default
    middlewares.WithHSTS(31536000),                // opt-in, HTTPS only
    middlewares.WithCSP("default-src 'self'"),     // opt-in
)
```

Defaults: `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`,
`Referrer-Policy: no-referrer`. HSTS and CSP are opt-in because they depend on
the deployment.

### Full HSTS policy

`WithHSTS(seconds)` emits `max-age` only. A real policy almost always wants
more, and `WithHSTSPolicy` carries it:

```go
middlewares.SecurityHeaders(middlewares.WithHSTSPolicy(middlewares.HSTS{
    MaxAge:            365 * 24 * time.Hour,
    IncludeSubDomains: true,
    Preload:           true,
}))
```

`preload` is emitted only alongside `includeSubDomains`, because the browser
preload lists reject it otherwise and a header that can never be honoured is
worse than an honest one. A zero `MaxAge` omits the header entirely.

## MaxBytes

```go
middlewares.MaxBytes(1 << 20) // cap request bodies at 1 MiB
```

Wraps the request body with `http.MaxBytesReader`.

## Design notes

- The internal response-writer wrapper tracks status and byte count and forwards
  `Flush`, `Hijack` and `ReadFrom` through `http.ResponseController`, so
  streaming, upgrades and efficient file serving keep working.
- Runtime middleware never return errors; configuration errors (a wildcard CORS
  origin with credentials, a non-positive throttle limit) panic at construction,
  surfacing programming mistakes immediately.
