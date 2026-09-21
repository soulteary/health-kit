# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
Because Go encodes the major version in the import path, every major release
also changes the module path — see [Unreleased](#unreleased) for the current one.

## [Unreleased]

### Added

- `CHANGELOG.md` and `SECURITY.md`.
- Runnable examples (`Example`, `ExampleDefaultConfig`,
  `ExampleDefaultInternalConfig`, `ExampleDecide`, `ExampleConfig_ClientIP`)
  that `go test` verifies, so they cannot drift from the API.
- A package doc in `doc.go` describing the layout, the
  `DefaultConfig`/`DefaultInternalConfig` privacy distinction, and how to write
  an adapter.
- `.github/workflows/release.yml`. Eleven tags exist with nothing having
  checked any of them, and the one mistake this repository has actually made —
  `v1.4.0` tagged on a commit declaring `/v2`, leaving the tag unfetchable and
  the proxy's v1 list stopping at v1.3.0 — is the kind that is caught at tag
  time or not at all. It runs on a `v*` tag (and on demand): the module path
  must carry the tag's major version, with v0 and v1 taking no suffix, and both
  READMEs' `go get` line must name that same path. Then the CI gate against the
  tagged commit — gofmt, `go mod tidy` cleanliness, vet, golangci-lint, `go
  test -race` with coverage, and govulncheck. The guard was exercised against
  eleven tag/module pairs, among them the real `v1.4.0`/`/v2` pair and a
  `v4.0.0` tag on a `/v3` module. Verification only: it publishes nothing and
  takes no write permissions.
- `.github/dependabot.yml`. Weekly gomod and github-actions updates, minor and
  patch grouped into one PR, majors left separate — for this module a
  dependency major is a judgement call. The release gate is an action too, so
  a silently stale action would be a stale release check.

### Changed

- CI builds `golangci-lint` with the toolchain `setup-go` installs, instead of
  taking a release binary from `golangci-lint-action`. Those binaries are
  compiled with Go 1.26 and refuse a module targeting a newer language
  version, so with `go 1.27.0` in `go.mod` the lint job exited with "the Go
  language version (go1.26) used to build golangci-lint is lower than the
  targeted Go version (1.27.0)" before reading a line of code. Compiled with
  1.27 the same linter runs clean on all three packages. The `security-scan`
  job already installed `govulncheck` this way and was never affected.

### Fixed

- `(*Aggregator).Check` was over gocyclo's complexity threshold (17 vs 15). The
  overall-status reduction it shared with `CheckSequential` — written out
  verbatim in both — is now the single `overallStatus` method. No behaviour
  change; `Check` drops to 11 and `CheckSequential` to 5.
- The package doc still claimed the root package provided "HTTP handlers
  compatible with both Fiber and net/http". It has had no Fiber handlers since
  v3.0.0. Both READMEs opened with the same claim and were missed at the time;
  they now say what the root package actually ships, and where Fiber lives.
- The `[3.0.0]` heading still read "unreleased" although the tag exists and the
  proxy has served it since 2026-09-21.
- Both compare links at the foot of this file pointed at `HEAD`, so
  `[Unreleased]` covered everything back to v2.3.0 and `[3.0.0]` grew with every
  commit instead of ending at its tag.

## [3.0.0] — 2026-09-21

### Changed — BREAKING

- **The module path is now `github.com/soulteary/health-kit/v3`.** Required by
  Go's import compatibility rule, because this release removes exported
  symbols. Every user must update the import path, including net/http-only
  users who are otherwise unaffected.
- **Fiber support moved to the `fiberadapter` subpackage.** The root package no
  longer imports Fiber, so a binary that never serves a Fiber route no longer
  links it. Measured against v2.3.0 for a net/http-only program: 25 fewer
  linked packages, 8 fewer modules, 14% smaller binary.

  | Removed from the root package | Replacement |
  |---|---|
  | `health.FiberHandler` | `fiberadapter.Handler` |
  | `health.FiberLivenessHandler` | `fiberadapter.LivenessHandler` |
  | `health.FiberReadinessHandler` | `fiberadapter.ReadinessHandler` |
  | `health.SimpleFiberHandler` | `fiberadapter.SimpleHandler` |

  Keeping these as deprecated shims was not an option: a shim has to import
  Fiber, which would relink fasthttp and give back the entire benefit.

- Apart from the import path, no net/http API changed. `Handler`,
  `LivenessHandler`, `ReadinessHandler` and `SimpleHandler` keep their
  signatures and their behaviour.

### Added

- `Decide`, `Decision`, `ClientIPSource` and `RequestSource` — the endpoint
  decision, factored out so an adapter for Echo, Gin or chi is roughly twenty
  lines. `SimpleResponse` is the former unexported `simpleResponse`.
- `Decision.StatusCode` is set even on a forbidden decision, so an adapter that
  writes it unconditionally cannot send `0`.
- Both READMEs document the adapter API, with a worked Echo adapter.

### Fixed

- The trusted-proxy rule had two implementations, `getClientIPFromRequest` and
  `getClientIPFromFiber`, differing only in where the peer address and headers
  came from. They are now the single `Config.ClientIP`. A rule that can
  disagree with itself across frameworks is an IP-whitelist bypass, so this is
  a hardening fix rather than tidying.
- `fiberadapter.Source.Header` had no test coverage at all: Fiber's `app.Test`
  does not carry `http.Request.RemoteAddr` into the fasthttp context, so the
  one test that claimed to exercise the trusted-proxy path never entered it.
  Breaking that method outright left the suite green. Covered now, including a
  cross-framework parity table asserting both `ClientIPSource` implementations
  resolve the same client IP.
- All four Fiber examples in the READMEs called `fiberadapter.*` without
  importing the package, so none of them compiled.
- `parseForwardedIP` had an unreachable `len(parts) == 0` branch.

### Unchanged, deliberately

- The two 403 bodies still differ — `health.Handler` answers `text/plain`,
  `fiberadapter.Handler` answers JSON, as both always have. Unifying them would
  be a silent behaviour change; both are now pinned by tests instead.

### Dependencies

- `modernc.org/sqlite` 1.59.0, `modernc.org/libc` 1.77.0,
  `dustin/go-humanize` 1.1.0, `gofiber/schema` 1.8.7, `gofiber/utils/v2` 2.5.2,
  `molecule-man/go-brrr` 1.1.1, `go.uber.org/atomic` 1.12.0.
- CI actions: `actions/checkout`, `actions/setup-go` and
  `actions/upload-artifact` to v7, `codecov/codecov-action` to v7,
  `soulteary/goreportcard-action` to v1.1.2.

## [2.3.0] — 2026-09-14

### Dependencies

Dependency refresh only. No API removed, no call site needs rewriting.

- `miniredis` 2.39.0 (was 2.36.1), `modernc.org/sqlite` 1.58.0 (was 1.44.3).

## [2.2.0] — 2026-09-12

A correctness release. **The default response shape changed.**

### Changed — BREAKING

- **`IncludeDetails` and `IncludeChecks` now default to `false`.** They were on
  by default, with no IP whitelist, while the built-in probes put `err.Error()`
  verbatim into the result — database DSNs, internal hostnames, filesystem
  paths — on an endpoint that is usually unauthenticated. Callers that parse
  the `checks` map should switch to `DefaultInternalConfig()`, and put the
  endpoint behind authentication or cluster-internal networking.

### Fixed

- **`Config.Timeout` is now enforced.** `Check` built a timeout context, handed
  it to every checker, then called `wg.Wait()` — nothing consulted the context,
  so aggregation blocked until the slowest checker returned, however long that
  took. A slow dependency that used to hang the endpoint now reports
  `unhealthy` on time.
- **A panicking checker is recovered rather than fatal.** A panic in a
  goroutine is not recovered by HTTP middleware on another stack, so a probe
  could kill the service it reports on.
- **Two checkers registered under one name both run.** The second silently
  replaced the first in the results map, so a healthy namesake could hide a
  failing check. Results are combined, keeping the least healthy status.
- **A completed healthy check is no longer reported as a timeout.** Results
  were keyed by `Checker.Name()` but cleared by `CheckResult.Name`, so a
  checker returning a differently-named — or unnamed — result left its
  registered name pending and the endpoint answered 503 for a check that had
  succeeded.
- **Synthesized results carry a real timestamp.** Timeout and recovered-panic
  results left `Timestamp` at its zero value, putting `0001-01-01T00:00:00Z` in
  the output for precisely the failures an operator is reading.
- Requirements said Go 1.26 while `go.mod` required 1.27.0.

## [2.1.0] — 2026-08-27

### Changed

- `go.mod` now declares `go 1.27.0`.

## [2.0.0] — 2026-08-26

### Changed — BREAKING

- **Module path is `github.com/soulteary/health-kit/v2`.**
- **Fiber handlers target Fiber v3.** Applications still on Fiber v2 should
  remain on the v1 line.

> Note: the `v1.4.0` tag points at this same commit, which already carries the
> `/v2` module path. `github.com/soulteary/health-kit@v1.4.0` therefore does not
> resolve; use `v1.3.0` for the last v1 release, or `v2.0.0` and later.

## [1.3.0] — 2026-08-12

### Added

- Trusted-proxy handling for the health endpoint's IP checks: `X-Forwarded-For`
  and `X-Real-IP` are honoured only when the peer is a configured trusted proxy.
- Go Report Card in CI.

## [1.2.0] — 2026-03-06

### Changed

- `go.mod` now declares `go 1.26`.

## [1.1.0] — 2026-02-04

### Changed

- Dependency updates.

## [1.0.0] — 2026-01-26

Initial release: the `Checker` interface, built-in Redis / HTTP / database /
custom / disabled probes, parallel multi-probe aggregation, net/http and Fiber
handlers, Kubernetes liveness and readiness probes, and IP whitelisting.

[Unreleased]: https://github.com/soulteary/health-kit/compare/v3.0.0...HEAD
[3.0.0]: https://github.com/soulteary/health-kit/compare/v2.3.0...v3.0.0
[2.3.0]: https://github.com/soulteary/health-kit/compare/v2.2.0...v2.3.0
[2.2.0]: https://github.com/soulteary/health-kit/compare/v2.1.0...v2.2.0
[2.1.0]: https://github.com/soulteary/health-kit/compare/v2.0.0...v2.1.0
[2.0.0]: https://github.com/soulteary/health-kit/compare/v1.3.0...v2.0.0
[1.3.0]: https://github.com/soulteary/health-kit/compare/v1.2.0...v1.3.0
[1.2.0]: https://github.com/soulteary/health-kit/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/soulteary/health-kit/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/soulteary/health-kit/releases/tag/v1.0.0
