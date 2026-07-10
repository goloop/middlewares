# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
