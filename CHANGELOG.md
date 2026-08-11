# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.3.1] - 2026-08-11

Patch release: documentation, prompted by a defect it went on to find.

### Documentation
- `RealIP` and `RealIPFrom` now say explicitly that the context is the only
  place the resolved address is written, and that `Request.RemoteAddr` is left
  untouched. The previous wording was true and easy to read past: a project
  keyed its rate limit on `RemoteAddr` believing the middleware had normalized
  it, and behind nginx that gave the whole userbase one shared bucket - the
  failure is silent, and only shows up in production because a developer
  hitting the API directly never goes through the proxy.
- A test pins the promise, so the sentence cannot quietly become untrue.

## [0.3.0] - 2026-08-11

Minor release: rate limiting, correlated logs and a full HSTS policy. All
additive.

### Added
- `RateLimit` limits how often one caller may return, with `RateLimitStore` as
  the decision point, `KeyByIP`/`KeyByHeader` for grouping, and 429 plus
  `Retry-After` and the `X-RateLimit-*` headers on the way out. Three projects
  wrote this for themselves and two found races in their own versions, which is
  the argument for it living here.
- When the store cannot answer, the request is refused with 503 by default
  rather than served. A limiter usually sits in front of sign-in, password
  reset and one-time codes, and a store outage that quietly removed the limit
  from exactly those routes would turn an availability problem into a security
  one. `WithFailOpen` chooses the other trade explicitly.
- `NewMemoryRateLimitStore` is a token bucket, not a fixed window: a window
  that resets on a schedule serves a full budget on each side of the boundary,
  which is twice the limit in an instant. It counts only what this process saw,
  and says so.
- `WithContextLogger` and `LoggerFrom` put a logger carrying the request
  identifier, method and path into the request context, so handler logs
  correlate with the request line by default instead of by everyone
  remembering. It is opt-in because it allocates per request.
- `WithHSTSPolicy` and `HSTS` carry `includeSubDomains` and `preload`, which a
  real policy almost always wants. `preload` is emitted only alongside
  `includeSubDomains`, since the browser lists reject it otherwise and a header
  that cannot be honoured is worse than an honest one. `WithHSTS(seconds)` is
  unchanged.

### Documentation
- `RealIP` now states what its safe default costs: behind a reverse proxy with
  no trusted-proxy policy, every client resolves to the proxy, so anything
  keyed by address - rate limits, audit trails, ban lists - silently becomes
  one budget shared by everyone. The default is still right; it is only correct
  once `WithTrustedProxies` names the proxy.
- The package documentation contrasts `RateLimit` with `Throttle` directly,
  since the two answer different questions and a service in front of expensive
  work usually wants both.

## [0.1.1] - 2026-07-10

### Documentation
- Documented the `compressWriter` and `responseWriter` `Write`/`WriteHeader`
  methods (status latching and buffering behaviour).

## [0.1.0]

Initial v0 release: a dependency-free set of HTTP middleware for `net/http`.

### Added

- `Middleware` type, `Chain` and `Handler` composition helpers.
- `RequestID` with a random generator, context accessor `RequestIDFrom`, and
  opt-in trusted inbound identifiers.
- `RealIP` with a trusted-proxy policy and context accessor `RealIPFrom`.
- `Recoverer` with a committed-response guard and configurable handler/logger.
- `Logger` built on `log/slog`, with skip and level options.
- `Timeout` built on `http.TimeoutHandler`.
- `Compress` (gzip) with size, type and level options and safe skip rules.
- `Throttle` in-process concurrency limiter.
- `CORS` with safe defaults and construction-time validation.
- `SecurityHeaders` and `MaxBytes`.
- Internal response-writer wrapper that preserves `Flush`, `Hijack` and
  `ReadFrom`.

### Fixed

- `Compress` no longer neutralizes an outer `Recoverer`: on a handler panic it
  discards the buffered partial response and re-panics, so `Recoverer` still
  sends a 500 instead of a 200 with a truncated body.
- `WithCompressLevel` validates the gzip level and ignores an out-of-range
  value, so a configuration mistake can no longer become a deferred nil-pointer
  panic on the first large request.
- The response-writer and compress wrappers implement `Unwrap`, so
  `http.ResponseController` (e.g. `SetWriteDeadline`) works through `Logger`,
  `Recoverer` and `Compress`.
- Informational 1xx responses (e.g. `103 Early Hints`) are forwarded without
  latching the status, so the final status is preserved.
- `RealIP` reads `X-Forwarded-For` from right to left, skipping trusted
  proxies, so a client-supplied left-most entry can no longer spoof the
  resolved address.
