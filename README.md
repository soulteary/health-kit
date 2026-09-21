# health-kit

[![Go Reference](https://pkg.go.dev/badge/github.com/soulteary/health-kit/v3.svg)](https://pkg.go.dev/github.com/soulteary/health-kit/v3)
[![Go Report Card](.github/goreportcard.svg)](.github/goreportcard-report.md)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![codecov](https://codecov.io/gh/soulteary/health-kit/graph/badge.svg)](https://codecov.io/gh/soulteary/health-kit)

[中文文档](README_CN.md)

A unified health check toolkit for Go services: health check interfaces, probe implementations, multi-probe aggregation, and net/http handlers. Fiber v3 support lives in the `fiberadapter` subpackage — the root package has not carried Fiber handlers since v3.0.0.


> **Breaking in v3.0.0 — new module path, and Fiber support moved to a subpackage.**
>
> **Step 1 — everyone, including net/http-only users.** The module path is now
> `github.com/soulteary/health-kit/v3`:
>
> ```bash
> go get github.com/soulteary/health-kit/v3
> go mod edit -droprequire github.com/soulteary/health-kit/v2
> ```
>
> Then update the import path in your source. The major-version bump is
> required by Go's import compatibility rule, because v3 removes exported
> symbols; keeping them as shims was not an option, since a shim would import
> Fiber again and give back the whole benefit below.
>
> **Step 2 — Fiber users only.** The Fiber handlers moved to
> `github.com/soulteary/health-kit/v3/fiberadapter`, so importing the root
> package no longer links Fiber (and fasthttp) into binaries that never use
> it. In a net/http service that means **25 fewer linked packages, 8 fewer
> modules and a 14% smaller binary**.
>
> | Before | After |
> |---|---|
> | `health.FiberHandler(agg)` | `fiberadapter.Handler(agg)` |
> | `health.FiberLivenessHandler(name)` | `fiberadapter.LivenessHandler(name)` |
> | `health.FiberReadinessHandler(agg)` | `fiberadapter.ReadinessHandler(agg)` |
> | `health.SimpleFiberHandler(name)` | `fiberadapter.SimpleHandler(name)` |
>
> Apart from the import path, no net/http API changed: `health.Handler`,
> `LivenessHandler`, `ReadinessHandler` and `SimpleHandler` keep their
> signatures and their behaviour.

## Features

- **Checker Interface**: Unified health check interface for all probes
- **Built-in Probes**: Redis, HTTP, Database, and Custom probes
- **Parallel Aggregation**: Run multiple health checks in parallel with aggregated results
- **HTTP Handlers**: Standard library handlers, plus a Fiber adapter in its own subpackage
- **Framework-Agnostic Core**: `Decide` and `ClientIPSource` are exported, so an
  adapter for Echo, Gin or chi is ~20 lines and shares one trusted-proxy rule
- **Kubernetes Support**: Dedicated liveness and readiness probe handlers
- **IP Whitelisting**: Restrict health endpoint access to specific IPs/CIDRs
- **Critical Checks**: Distinguish between critical and non-critical dependencies
- **Latency Tracking**: Measure and report health check latency

## Installation

```bash
go get github.com/soulteary/health-kit/v3
```

The root package has no web-framework dependency. Fiber support lives in a
subpackage, so only importing it links Fiber (and fasthttp):

```bash
go get github.com/soulteary/health-kit/v3/fiberadapter
```

Fiber handlers target Fiber v3. Applications still on Fiber v2 should remain on
health-kit v1. The net/http handlers and probe APIs keep the same behavior.

## Usage

### Basic Health Check

```go
import (
    health "github.com/soulteary/health-kit/v3"
)

// Create a configuration
config := health.DefaultConfig().
    WithServiceName("myservice").
    WithTimeout(5 * time.Second)

// Create an aggregator
aggregator := health.NewAggregator(config)

// Add checkers
aggregator.AddCheckers(
    health.NewRedisChecker(redisClient),
    health.NewHTTPChecker("herald", "http://herald:8080/healthz"),
)

// Perform health check
result := aggregator.Check(context.Background())
fmt.Printf("Status: %s\n", result.Status)
```

### Redis Health Check

```go
// Basic Redis checker
redisChecker := health.NewRedisChecker(redisClient)

// With custom name and timeout
redisChecker := health.NewRedisCheckerWithName("session-redis", redisClient).
    WithTimeout(2 * time.Second)
```

### HTTP Dependency Check

```go
// Check external service health
httpChecker := health.NewHTTPChecker("herald", "http://herald:8080/healthz").
    WithTimeout(3 * time.Second).
    WithExpectedCode(http.StatusOK)

// Check with custom HTTP method
httpChecker := health.NewHTTPChecker("api", "http://api/status").
    WithMethod(http.MethodHead)
```

### Database Health Check

```go
// Basic database checker
dbChecker := health.NewDBChecker(db)

// With custom name
dbChecker := health.NewDBCheckerWithName("postgres", db).
    WithTimeout(5 * time.Second)
```

### Custom Health Check

```go
// Create a custom checker
customChecker := health.NewCustomChecker("cache", func(ctx context.Context) error {
    if cache.Size() == 0 {
        return errors.New("cache is empty")
    }
    return nil
}).WithTimeout(1 * time.Second)
```

### Disabled Checker

```go
// For optional dependencies that are not configured
disabledChecker := health.NewDisabledChecker("optional-redis").
    WithMessage("Redis is not configured")
```

### HTTP Handlers (Standard Library)

```go
import (
    "net/http"
    health "github.com/soulteary/health-kit/v3"
)

// Full health check with all probes
http.HandleFunc("/health", health.Handler(aggregator))

// Kubernetes liveness probe (always returns OK if service is running)
http.HandleFunc("/livez", health.LivenessHandler("myservice"))

// Kubernetes readiness probe (checks all dependencies)
http.HandleFunc("/readyz", health.ReadinessHandler(aggregator))

// Simple health check without probes
http.HandleFunc("/health", health.SimpleHandler("myservice"))
```

### Fiber Handlers

```go
import (
    "github.com/gofiber/fiber/v3"
    health "github.com/soulteary/health-kit/v3"
    "github.com/soulteary/health-kit/v3/fiberadapter"
)

app := fiber.New()

// Full health check
app.Get("/health", fiberadapter.Handler(aggregator))
app.Get("/healthz", fiberadapter.Handler(aggregator))

// Kubernetes liveness probe
app.Get("/livez", fiberadapter.LivenessHandler("myservice"))

// Kubernetes readiness probe
app.Get("/readyz", fiberadapter.ReadinessHandler(aggregator))

// Simple health check
app.Get("/health", fiberadapter.SimpleHandler("myservice"))
```

### Adapters for Other Frameworks (Echo, Gin, chi…)

There is no Echo or Gin adapter in this repository, and you do not need one:
the whole endpoint decision is framework-agnostic and exported. An adapter is a
translation layer of about twenty lines.

Two pieces do the work. `health.Decide` applies the IP whitelist, runs the
checks and reduces the outcome to a status code and a body:

```go
type Decision struct {
    Forbidden  bool // client IP is not on the whitelist
    StatusCode int  // always set, including on a forbidden decision
    Body       any  // serialize as JSON
}

func Decide(ctx context.Context, aggregator *Aggregator, src ClientIPSource) Decision
```

`health.ClientIPSource` is the only thing you implement — the minimal view of a
request needed to resolve a client IP:

```go
type ClientIPSource interface {
    RemoteIP() net.IP            // peer address, nil when unavailable
    Header(name string) string   // request header, "" when absent
}
```

The trusted-proxy rule itself — when `X-Forwarded-For` and `X-Real-IP` may be
believed, and in what order — lives in `Config.ClientIP` and is shared by every
adapter. That is deliberate: a rule that disagrees with itself across
frameworks is an IP-whitelist bypass, not a cosmetic difference. Do not
reimplement it.

A complete Echo adapter:

```go
type echoSource struct{ c echo.Context }

func (s echoSource) RemoteIP() net.IP {
    host, _, err := net.SplitHostPort(s.c.Request().RemoteAddr)
    if err != nil {
        return net.ParseIP(s.c.Request().RemoteAddr)
    }
    return net.ParseIP(host)
}

func (s echoSource) Header(name string) string { return s.c.Request().Header.Get(name) }

func Handler(aggregator *health.Aggregator) echo.HandlerFunc {
    return func(c echo.Context) error {
        d := health.Decide(c.Request().Context(), aggregator, echoSource{c: c})
        if d.Forbidden {
            return c.JSON(http.StatusForbidden, map[string]string{"error": "Forbidden"})
        }
        return c.JSON(d.StatusCode, d.Body)
    }
}
```

Note that `Decide` does **not** render the 403 body — each adapter does. The
two in-tree adapters disagree on it for historical reasons (`health.Handler`
answers `text/plain`, `fiberadapter.Handler` answers JSON), so unifying them
would be a silent behaviour change for somebody. Pick whichever matches the
rest of your API.

If you write an adapter, the one test worth copying from `fiberadapter` is the
cross-framework parity table: run the same headers through your
`ClientIPSource` and through `health.RequestSource`, and assert they resolve
the same client IP.

### Configuration Options

```go
config := health.DefaultConfig().
    WithServiceName("herald").
    WithTimeout(5 * time.Second).
    WithIPWhitelist([]string{"10.0.0.0/8", "192.168.1.1"}).
    WithTrustedProxies([]string{"10.0.0.0/8"}). // trust only your reverse proxies
    WithDetails(true).          // opt in to per-check detail
    WithChecks(true).           // opt in to the per-check results map
    WithCriticalChecks([]string{"redis", "database"})
```

| Option | `DefaultConfig()` | Notes |
|--------|-------------------|-------|
| `ServiceName` | `"service"` | echoed in the response |
| `Timeout` | `5s` | enforced: a checker that overruns is recorded unhealthy |
| `IncludeDetails` | `false` | per-check messages, errors and metadata |
| `IncludeChecks` | `false` | the `checks` map itself |
| `IPWhitelist` | `nil` | empty means no IP restriction |
| `TrustedProxies` | `nil` | which peers' forwarded headers to believe |
| `CriticalChecks` | `nil` | names whose failure makes the whole result unhealthy |

`DefaultInternalConfig()` is `DefaultConfig()` with `IncludeDetails` and
`IncludeChecks` turned on.

Read the current values back with `aggregator.Config()`, and replace them with
`SetConfig`.

### Timeouts, Panics and Duplicate Names

**The timeout is enforced, not advisory.** Any checker that has not reported
within `Config.Timeout` is recorded as `unhealthy` with a "did not complete
within ..." message, and aggregation returns. A checker that ignores its
context — a driver call that takes none, or takes one and ignores it — can no
longer hang the endpoint.

The reason distinguishes *whose* deadline fired: an already-cancelled caller
context, or a caller deadline earlier than the aggregator's, is reported as
such rather than as the configured timeout.

**A panicking checker cannot take the process down.** Panics inside a check are
recovered and reported as an unhealthy result for that check. This matters
because a panic in a goroutine is fatal and is *not* recovered by an HTTP
framework's middleware, which runs on a different stack — so a probe that
dereferenced a nil database handle used to kill the service it was reporting on.

**Registering two checkers under one name runs both.** Their results are
combined into that name's single entry, keeping the least healthy status and
merging the error text, so a healthy namesake cannot mask a failing check.
`GetCheckerNames()` deduplicates to match the output keys.

Synthesized results — a timeout or a recovered panic — carry a real
`Timestamp`.

### Critical vs Non-Critical Checks

```go
// Define which checks are critical
config := health.DefaultConfig().
    WithCriticalChecks([]string{"redis", "database"})

aggregator := health.NewAggregator(config)
aggregator.AddCheckers(
    health.NewRedisChecker(redisClient),          // Critical
    health.NewDBChecker(db),                       // Critical  
    health.NewHTTPChecker("cache", cacheURL),      // Non-critical
)

result := aggregator.Check(ctx)
// If Redis or DB fails: Status = "unhealthy"
// If only cache fails: Status = "degraded"
// If all pass: Status = "ok"
```

### IP Whitelisting

```go
config := health.DefaultConfig().
    WithIPWhitelist([]string{
        "127.0.0.1",        // Localhost
        "10.0.0.0/8",       // Private network CIDR
        "192.168.1.100",    // Specific IP
    })

// Requests from non-whitelisted IPs will receive 403 Forbidden
```

### Trusted Proxies (Forwarded Headers)

If your service sits behind a reverse proxy or load balancer, configure trusted
proxy IPs/CIDRs before relying on `X-Forwarded-For` or `X-Real-IP` headers.
Untrusted sources will be ignored to prevent header spoofing.

```go
config := health.DefaultConfig().
    WithIPWhitelist([]string{"192.168.1.100"}).
    WithTrustedProxies([]string{"10.0.0.0/8"}) // Only proxy IPs are trusted
```

### Production Privacy

`DefaultConfig()` returns the **public** shape: `IncludeDetails` and
`IncludeChecks` are both false, so the response carries the overall status and
nothing about individual dependencies. A health endpoint is usually reachable
without authentication, and per-check detail names your internal hosts,
database versions and error strings to anyone who asks.

```go
health.NewAggregator(health.DefaultConfig())        // status only
health.NewAggregator(health.DefaultInternalConfig()) // details + per-check results
```

Use `DefaultInternalConfig()` for an endpoint that is already behind
authentication or only reachable from inside the cluster, or set
`IncludeDetails` / `IncludeChecks` individually.

## Project Structure

```
health-kit/
├── checker.go         # Checker interface, result types, and JSON marshaling
├── config.go          # Configuration with IP whitelist support
├── probes.go          # Built-in probes (Redis, HTTP, DB, Custom, Disabled)
├── aggregator.go      # Multi-probe aggregation with parallel execution
├── handler.go         # net/http handlers + the framework-agnostic Decide core
├── fiberadapter/      # Fiber v3 adapter (only importers of this link Fiber)
└── *_test.go          # Comprehensive tests
```

## Integration Examples

### Herald (OTP Service)

```go
package main

import (
    "github.com/gofiber/fiber/v3"
    health "github.com/soulteary/health-kit/v3"
    "github.com/soulteary/health-kit/v3/fiberadapter"
    "github.com/redis/go-redis/v9"
)

func main() {
    redisClient := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
    
    config := health.DefaultConfig().
        WithServiceName("herald").
        WithTimeout(2 * time.Second)
    
    aggregator := health.NewAggregator(config)
    aggregator.AddChecker(health.NewRedisChecker(redisClient))
    
    app := fiber.New()
    app.Get("/healthz", fiberadapter.Handler(aggregator))
    
    app.Listen(":8080")
}
```

### Stargate (Auth Gateway)

```go
package main

import (
    "net/http"
    health "github.com/soulteary/health-kit/v3"
)

func main() {
    config := health.DefaultConfig().
        WithServiceName("stargate").
        WithTimeout(5 * time.Second).
        WithCriticalChecks([]string{"redis"})
    
    aggregator := health.NewAggregator(config)
    aggregator.AddCheckers(
        health.NewRedisChecker(redisClient),
        health.NewHTTPChecker("herald", "http://herald:8080/healthz").WithTimeout(2*time.Second),
        health.NewHTTPChecker("warden", "http://warden:8080/health").WithTimeout(2*time.Second),
    )
    
    http.HandleFunc("/health", health.Handler(aggregator))
    http.HandleFunc("/livez", health.LivenessHandler("stargate"))
    http.HandleFunc("/readyz", health.ReadinessHandler(aggregator))
    
    http.ListenAndServe(":8080", nil)
}
```

### Warden (User Service)

```go
package main

import (
    "net/http"
    health "github.com/soulteary/health-kit/v3"
)

func main() {
    config := health.DefaultConfig().
        WithServiceName("warden").
        WithIPWhitelist([]string{"10.0.0.0/8", "127.0.0.1"}).
        WithTimeout(5 * time.Second)
    
    aggregator := health.NewAggregator(config)
    
    // Redis is optional for Warden in ONLY_LOCAL mode
    if redisEnabled {
        aggregator.AddChecker(health.NewRedisChecker(redisClient))
    } else {
        aggregator.AddChecker(health.NewDisabledChecker("redis"))
    }
    
    // Check data loaded
    aggregator.AddChecker(health.NewCustomChecker("data_loaded", func(ctx context.Context) error {
        if userCache.Len() == 0 {
            return errors.New("no users loaded")
        }
        return nil
    }))
    
    http.HandleFunc("/health", health.Handler(aggregator))
    http.HandleFunc("/healthcheck", health.Handler(aggregator))
    
    http.ListenAndServe(":8080", nil)
}
```

## Response Format

### Detailed Response (`DefaultInternalConfig()`, or `WithDetails(true).WithChecks(true)`)

```json
{
  "status": "ok",
  "service": "myservice",
  "checks": {
    "redis": {
      "name": "redis",
      "status": "ok",
      "latency_ms": 5,
      "timestamp": "2024-01-25T10:30:00Z"
    },
    "database": {
      "name": "database",
      "status": "ok",
      "latency_ms": 12,
      "timestamp": "2024-01-25T10:30:00Z",
      "metadata": {
        "open_connections": 10,
        "in_use": 5,
        "idle": 5
      }
    }
  },
  "timestamp": "2024-01-25T10:30:00Z",
  "total_latency_ms": 15
}
```

### Simple Response (`DefaultConfig()` — the default)

```json
{
  "status": "ok",
  "service": "myservice"
}
```

### Degraded Response

Abbreviated: the top-level `timestamp` and `total_latency_ms`, and each check's
`timestamp`, are always present.

```json
{
  "status": "degraded",
  "service": "myservice",
  "checks": {
    "redis": {
      "name": "redis",
      "status": "ok",
      "latency_ms": 5
    },
    "cache": {
      "name": "cache",
      "status": "unhealthy",
      "error": "connection refused",
      "latency_ms": 100
    }
  }
}
```

## HTTP Status Codes

`HTTPStatusCode` maps a status to a code:

| Health status | `HTTPStatusCode` | Can an endpoint report it? |
|---------------|------------------|----------------------------|
| `ok`          | 200 OK           | yes |
| `degraded`    | 200 OK           | yes |
| `unhealthy`   | 503 Service Unavailable | yes |
| `disabled`    | 503 Service Unavailable | no |
| `unknown`     | 503 Service Unavailable | no |

`disabled` and `unknown` are per-check statuses. `Aggregator` reduces to one of
the first three, so no endpoint answers 503 on their account: a disabled check
still appears under `checks`, it is just skipped when reducing to the overall
status. The last two rows matter only if you call `HTTPStatusCode` yourself
with a single check's status — anything outside the first three maps to 503.

## Requirements

- **Go 1.27+** (`go.mod` declares `go 1.27.0`)
- github.com/gofiber/fiber/v3 v3.5.0+ — **only** if you import `fiberadapter`;
  the root package does not pull it in
- github.com/redis/go-redis/v9 v9.22.0+ (for the Redis probe)
- modernc.org/sqlite v1.59.0+ and github.com/alicebob/miniredis/v2 v2.39.0+ are
  test-only dependencies

## Test Coverage

Run tests:

```bash
go test ./... -v

# With coverage — what CI runs
go test -race -coverprofile=coverage.out -covermode=atomic ./...
go tool cover -html=coverage.out -o coverage.html
go tool cover -func=coverage.out
```

Both packages are at **100% statement coverage**. Note that `fiberadapter`'s
tests are an *external* test package (`package fiberadapter_test`): they
compile only against the exported API, which keeps that API honest about being
sufficient for an out-of-tree adapter.

## Upgrade Notes (v3.0.0)

**Breaking: the module path is now `github.com/soulteary/health-kit/v3`, and
the Fiber handlers moved to the `fiberadapter` subpackage.** See the note at
the top of this file for the two migration steps. Everything else is additive.

- **Newly exported for adapter authors**: `Decide`, `Decision`,
  `ClientIPSource`, `RequestSource` and `SimpleResponse` (was the unexported
  `simpleResponse`). See *Adapters for Other Frameworks* above.
- **One trusted-proxy implementation.** `getClientIPFromRequest` and
  `getClientIPFromFiber` were two copies of the same rule, differing only in
  where the peer address and headers came from. They are now `Config.ClientIP`
  behind a two-method interface. No behaviour changed, but a rule that can
  disagree with itself across frameworks is a whitelist bypass, so it is worth
  knowing there is only one of it now.
- **`Decision.StatusCode` is set even when `Forbidden` is true.** Only matters
  if you write your own adapter and set the status unconditionally.
- **The 403 bodies are still not unified** — `health.Handler` answers
  `text/plain`, `fiberadapter.Handler` answers JSON, as both always have. Both
  are now pinned by tests so the difference cannot drift silently.

Dependency refresh in the same release:

- `modernc.org/sqlite` v1.59.0 (was v1.58.0), `modernc.org/libc` v1.77.0,
  `github.com/dustin/go-humanize` v1.1.0, `github.com/gofiber/schema` v1.8.7,
  `github.com/gofiber/utils/v2` v2.5.2, `github.com/molecule-man/go-brrr`
  v1.1.1, `go.uber.org/atomic` v1.12.0.
- CI actions: `actions/checkout`, `actions/setup-go` and
  `actions/upload-artifact` to v7, `codecov/codecov-action` to v7,
  `soulteary/goreportcard-action` to v1.1.2.

## Upgrade Notes (v2.3.0)

Dependency refresh only. No API was removed and no call needs rewriting.

- Test Redis is `miniredis` v2.39.0 (was v2.36.1).
- The SQLite driver is `modernc.org/sqlite` v1.58.0 (was v1.44.3).

## Upgrade Notes (v2.2.0)

**The default response shape changed.** `DefaultConfig()` no longer includes
per-check detail.

- **`IncludeDetails` and `IncludeChecks` now default to `false`.** They were on,
  with no `IPWhitelist`, while the built-in probes put `err.Error()` verbatim into
  the result — database DSNs, internal hostnames, filesystem paths — on an
  endpoint that is usually unauthenticated. **If your monitoring parses the
  `checks` map, switch to `DefaultInternalConfig()`** (or call
  `.WithDetails(true).WithChecks(true)`) and put the endpoint behind
  authentication or cluster-internal networking.
- **`Config.Timeout` is enforced.** `Check` built a timeout context, handed it to
  every checker, and then called `wg.Wait()` — nothing consulted the context, so
  aggregation blocked until the slowest checker returned, however long that took.
  A checker that overruns is now recorded as unhealthy and aggregation returns on
  time. A slow dependency that previously made the endpoint hang will now make it
  report `unhealthy` instead.
- **A panicking checker is recovered**, not fatal. If you relied on a probe panic
  to crash and restart a pod, that no longer happens — the check reports
  unhealthy and `ReadinessHandler` answers 503.
- **Two checkers registered under one name both run.** The second used to
  silently replace the first in the results map, so a healthy namesake could hide
  a failing check. Their results are now combined, keeping the least healthy
  status.
- **A completed healthy check is no longer reported as a timeout.** Results were
  keyed by `Checker.Name()` but cleared by `CheckResult.Name`, so a checker
  returning a result with a different — or empty — `Name` left its registered name
  pending, and the endpoint answered 503 for a check that had succeeded. Results
  now travel with the name they were registered under, which is also what
  `CriticalChecks` matches and what keys the output.
- **Synthesized results carry a real timestamp.** Timeout and recovered-panic
  results left `Timestamp` at its zero value, putting `0001-01-01T00:00:00Z` in
  the output for precisely the failures an operator is reading.
- **Requirements said Go 1.26**; `go.mod` requires `1.27.0`.

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for the full release history. The
*Upgrade Notes* sections above cover the breaking releases in more detail.

## Security

`DefaultConfig()` withholds probe detail on purpose, and forwarded headers are
believed only from a configured trusted proxy. [SECURITY.md](SECURITY.md)
explains both, and how to report a vulnerability — please do not open a public
issue for one.

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

See [LICENSE](LICENSE) file for details.
