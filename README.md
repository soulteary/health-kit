# health-kit

[![Go Reference](https://pkg.go.dev/badge/github.com/soulteary/health-kit/v2.svg)](https://pkg.go.dev/github.com/soulteary/health-kit/v2)
[![Go Report Card](.github/goreportcard.svg)](.github/goreportcard-report.md)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![codecov](https://codecov.io/gh/soulteary/health-kit/graph/badge.svg)](https://codecov.io/gh/soulteary/health-kit)

[中文文档](README_CN.md)

A unified health check toolkit for Go services. This package provides health check interfaces, probe implementations, multi-probe aggregation, and HTTP handlers compatible with both Fiber and net/http.

## Features

- **Checker Interface**: Unified health check interface for all probes
- **Built-in Probes**: Redis, HTTP, Database, and Custom probes
- **Parallel Aggregation**: Run multiple health checks in parallel with aggregated results
- **HTTP Handlers**: Standard library and Fiber-compatible `/health` endpoint handlers
- **Kubernetes Support**: Dedicated liveness and readiness probe handlers
- **IP Whitelisting**: Restrict health endpoint access to specific IPs/CIDRs
- **Critical Checks**: Distinguish between critical and non-critical dependencies
- **Latency Tracking**: Measure and report health check latency

## Installation

```bash
go get github.com/soulteary/health-kit/v2
```

Version 2 uses Fiber v3 for all Fiber-specific handlers. Applications that still use Fiber v2 should remain on health-kit v1. The net/http handlers and probe APIs keep the same behavior.

## Usage

### Basic Health Check

```go
import (
    health "github.com/soulteary/health-kit/v2"
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
    health "github.com/soulteary/health-kit/v2"
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
    health "github.com/soulteary/health-kit/v2"
)

app := fiber.New()

// Full health check
app.Get("/health", health.FiberHandler(aggregator))
app.Get("/healthz", health.FiberHandler(aggregator))

// Kubernetes liveness probe
app.Get("/livez", health.FiberLivenessHandler("myservice"))

// Kubernetes readiness probe
app.Get("/readyz", health.FiberReadinessHandler(aggregator))

// Simple health check
app.Get("/health", health.SimpleFiberHandler("myservice"))
```

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
├── handler.go         # HTTP handlers for Fiber and net/http
└── *_test.go          # Comprehensive tests
```

## Integration Examples

### Herald (OTP Service)

```go
package main

import (
    "github.com/gofiber/fiber/v3"
    health "github.com/soulteary/health-kit/v2"
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
    app.Get("/healthz", health.FiberHandler(aggregator))
    
    app.Listen(":8080")
}
```

### Stargate (Auth Gateway)

```go
package main

import (
    "net/http"
    health "github.com/soulteary/health-kit/v2"
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
    health "github.com/soulteary/health-kit/v2"
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

| Health Status | HTTP Status Code |
|---------------|------------------|
| ok            | 200 OK           |
| degraded      | 200 OK           |
| unhealthy     | 503 Service Unavailable |
| disabled      | N/A (skipped in aggregation) |

## Requirements

- **Go 1.27+** (`go.mod` declares `go 1.27.0`)
- github.com/gofiber/fiber/v3 v3.4.0+ (for Fiber handlers)
- github.com/redis/go-redis/v9 v9.7.3+ (for Redis probe)

## Test Coverage

Run tests:

```bash
go test ./... -v

# With coverage
go test ./... -coverprofile=coverage.out -covermode=atomic
go tool cover -html=coverage.out -o coverage.html
go tool cover -func=coverage.out
```

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

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

See [LICENSE](LICENSE) file for details.
