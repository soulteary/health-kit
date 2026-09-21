// Package health provides a unified health check toolkit for Go services:
// a common Checker interface, built-in probes for Redis, HTTP, SQL databases
// and arbitrary functions, parallel multi-probe aggregation, and ready-made
// net/http endpoint handlers.
//
// # Layout
//
// The root package has no web-framework dependency. It provides the net/http
// handlers ([Handler], [LivenessHandler], [ReadinessHandler], [SimpleHandler])
// and the framework-agnostic core they are built on.
//
// Fiber support lives in the subpackage
// github.com/soulteary/health-kit/v3/fiberadapter, so only importing that
// links Fiber -- and with it fasthttp -- into your binary. A service on
// net/http, Echo, Gin or chi pays nothing for Fiber support existing.
//
// # Getting started
//
//	aggregator := health.NewAggregator(health.DefaultConfig().WithServiceName("myservice"))
//	aggregator.AddChecker(health.NewRedisChecker(redisClient))
//
//	http.Handle("/healthz", health.Handler(aggregator))
//	http.Handle("/livez", health.LivenessHandler("myservice"))
//
// [DefaultConfig] is safe to expose publicly: it reports only an overall
// status. [DefaultInternalConfig] adds per-probe detail, including each
// probe's error verbatim -- database DSNs, internal hostnames, filesystem
// paths -- so an aggregator built from it belongs behind an IP whitelist or
// cluster-internal networking. See [Config.WithIPWhitelist].
//
// # Writing an adapter for another framework
//
// The endpoint's whole decision -- apply the IP whitelist, run the checks,
// reduce to a status code and a body -- is [Decide], which knows nothing
// about any web framework. An adapter implements [ClientIPSource], the
// minimal view of a request needed to resolve a client IP, and writes out the
// returned [Decision].
//
// The trusted-proxy rule itself lives in [Config.ClientIP] and is shared by
// every adapter. That is deliberate: a rule that disagrees with itself across
// frameworks is an IP-whitelist bypass, not a cosmetic difference. Do not
// reimplement it. See the fiberadapter subpackage for a worked example, and
// [RequestSource] for the net/http implementation.
package health
