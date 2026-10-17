[![deps.dev](https://img.shields.io/badge/deps.dev-insights-4c8dbc)](https://deps.dev/go/github.com%2Fgoloop%2Fmiddlewares) [![License](https://img.shields.io/badge/license-MIT-brightgreen)](https://github.com/goloop/middlewares/blob/v1/LICENSE) [![License](https://img.shields.io/badge/godoc-YES-green)](https://pkg.go.dev/github.com/goloop/middlewares) [![Stay with Ukraine](https://img.shields.io/static/v1?label=Stay%20with&message=Ukraine%20♥&color=ffD700&labelColor=0057B8&style=flat)](https://u24.gov.ua/)

# middlewares

`middlewares` is a set of production-ready HTTP middleware for Go's standard
`net/http`. Every middleware has the ordinary shape
`func(http.Handler) http.Handler`, so it works with any router: the standard
`http.ServeMux`, the [goloop/mux](https://github.com/goloop/mux) router, or
hand-written handlers. It has **no third-party dependencies** and logs through
the standard `log/slog`.

It is not a framework - it closes the common cross-cutting needs the standard
library leaves out, so a service can stay on the standard toolbox and still get
request IDs, real client IP, panic recovery, request logging, timeouts,
compression, throttling, CORS and security headers.

## Features

- `RequestID` - a random identifier per request, in the context and response
  header; inbound values are ignored unless explicitly trusted.
- `RealIP` - the client IP, from `RemoteAddr` by default and from forwarded
  headers only behind trusted proxies.
- `Recoverer` - turns a panic into a `500` and a logged stack trace, without
  corrupting an already-committed response.
- `Logger` - one structured `log/slog` line per request with status, size and
  duration.
- `Timeout` - a per-request deadline built on `http.TimeoutHandler`.
- `Compress` - gzip with skip rules for small bodies, server-sent events and
  already-encoded responses.
- `RateLimit` - how often one client may come back, with a pluggable store
  and an in-process token bucket by default.
- `Throttle` - an in-process concurrency limiter: a semaphore with load
  shedding, for work that pays its cost per call (password hashing, image
  processing).
- `CORS` - safe by default; nothing is allowed until you name the origins.
- `SecurityHeaders`, `MaxBytes` - conservative headers and a request-body cap.
- `Chain` and `Handler` - compose middleware.

## Installation

```bash
go get -u github.com/goloop/middlewares
```

```go
import "github.com/goloop/middlewares"
```

Requires Go 1.25 or newer.

## Quick start

```go
mux := http.NewServeMux()
mux.HandleFunc("GET /health", health)

h := middlewares.Chain(
    middlewares.RequestID(),
    middlewares.RealIP(middlewares.TrustLoopbackProxies()),
    middlewares.Recoverer(),
    middlewares.Logger(),
    middlewares.Compress(),
    middlewares.SecurityHeaders(),
)(mux)

http.ListenAndServe(":8080", h)
```

With the `goloop/mux` router (same type, no adapter needed):

```go
r := mux.New()
r.Use(
    middlewares.RequestID(),
    middlewares.RealIP(),
    middlewares.Recoverer(),
    middlewares.Logger(),
)
r.Get("/users/{id}", usersShow)
```

## Security notes

Read these before production use:

- **RealIP** trusts no proxy headers by default. Configure `WithTrustedProxies`
  (or `TrustLoopbackProxies`) or a client can spoof its address.
- **Timeout** cancels the request context but cannot stop a handler goroutine
  that ignores it - long handlers must watch `r.Context().Done()`.
- **Recoverer** cannot change a response that was already committed; it only
  logs the panic in that case.
- **Compress** never touches `text/event-stream`, already-encoded responses, or
  bodies below the minimum size.
- **CORS** allows nothing until you name the origins; a wildcard origin together
  with credentials is rejected at construction.
- **Throttle** is an in-process concurrency limiter, not a distributed rate
  limiter - and not a substitute for one. A rate limit answers "how often may
  this client come back"; Throttle answers "how much of this work fits in the
  process at once". Ten-per-minute still admits ten in the same second; ten
  slots do not. Pair them in front of memory-bound endpoints such as login.

## Logging

Logging uses the standard `log/slog`. Pass any `*slog.Logger` through
`WithLogger`; the package never imports a concrete logging backend, so you can
plug in whatever handler you already use.

## Documentation

- Full reference and recipes: [DOC.md](DOC.md) · [DOC.UK.md](DOC.UK.md)
- Package API: [pkg.go.dev/github.com/goloop/middlewares](https://pkg.go.dev/github.com/goloop/middlewares)
- Changes between versions: [CHANGELOG.md](CHANGELOG.md)

## Contributing

Contributions are welcome. Please run `go test ./...`, `go test -race ./...`,
`go vet ./...` and `gofmt -l .` before submitting a pull request.

## License

`middlewares` is released under the MIT License. See [LICENSE](LICENSE).

## Release v1.0.0

Respect gzip quality values and preserve response status and cache variation. The v1 release keeps the existing import path and public API. Future incompatible changes require a new major version.
