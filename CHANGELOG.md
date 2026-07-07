# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Initial v0 development branch: a dependency-free set of HTTP middleware for
`net/http`.

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
