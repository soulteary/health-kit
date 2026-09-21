package health_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"

	health "github.com/soulteary/health-kit/v3"
)

// The common case: aggregate a few probes behind a net/http endpoint.
func Example() {
	aggregator := health.NewAggregator(health.DefaultConfig().WithServiceName("myservice"))
	aggregator.AddChecker(health.NewCustomChecker("cache", func(context.Context) error {
		return nil
	}))

	rec := httptest.NewRecorder()
	health.Handler(aggregator)(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	fmt.Println(rec.Code)
	fmt.Print(rec.Body.String())
	// Output:
	// 200
	// {"status":"ok","service":"myservice"}
}

// DefaultConfig is deliberately terse. A failing probe changes the status code
// but does not leak why it failed -- the error text of a real probe carries
// DSNs, internal hostnames and filesystem paths.
func ExampleDefaultConfig() {
	aggregator := health.NewAggregator(health.DefaultConfig().WithServiceName("myservice"))
	aggregator.AddChecker(health.NewCustomChecker("db", func(context.Context) error {
		return errors.New("dial tcp 10.0.3.14:5432: connection refused")
	}))

	rec := httptest.NewRecorder()
	health.Handler(aggregator)(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	fmt.Println(rec.Code)
	fmt.Print(rec.Body.String())
	// Output:
	// 503
	// {"status":"unhealthy","service":"myservice"}
}

// DefaultInternalConfig adds per-probe detail, errors included. Put it behind
// an IP whitelist or cluster-internal networking.
func ExampleDefaultInternalConfig() {
	config := health.DefaultInternalConfig().
		WithServiceName("myservice").
		WithIPWhitelist([]string{"10.0.0.0/8"})

	aggregator := health.NewAggregator(config)
	aggregator.AddChecker(health.NewCustomChecker("db", func(context.Context) error {
		return errors.New("dial tcp 10.0.3.14:5432: connection refused")
	}))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "10.1.2.3:54321"
	rec := httptest.NewRecorder()
	health.Handler(aggregator)(rec, req)

	result := aggregator.Check(context.Background())
	fmt.Println(rec.Code)
	fmt.Println(result.Checks["db"].Error)

	// The same endpoint from outside the whitelist:
	outside := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	outside.RemoteAddr = "203.0.113.10:54321"
	denied := httptest.NewRecorder()
	health.Handler(aggregator)(denied, outside)
	fmt.Println(denied.Code)

	// Output:
	// 503
	// dial tcp 10.0.3.14:5432: connection refused
	// 403
}

// Decide is the whole endpoint decision without any web framework, so an
// adapter for Echo, Gin or chi only has to implement ClientIPSource and write
// the Decision out. This is what fiberadapter does.
func ExampleDecide() {
	config := health.DefaultConfig().
		WithServiceName("myservice").
		WithIPWhitelist([]string{"192.168.1.1"}).
		WithTrustedProxies([]string{"10.0.0.0/8"})

	aggregator := health.NewAggregator(config)
	aggregator.AddChecker(health.NewCustomChecker("cache", func(context.Context) error {
		return nil
	}))

	// A request arriving through a trusted proxy, forwarded for a
	// whitelisted client.
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "192.168.1.1")

	decision := health.Decide(context.Background(), aggregator, health.RequestSource(req))
	fmt.Println(decision.Forbidden, decision.StatusCode)

	// The same client, but the peer is not a trusted proxy, so the header is
	// ignored and the real peer is judged instead.
	req.RemoteAddr = "203.0.113.10:12345"
	decision = health.Decide(context.Background(), aggregator, health.RequestSource(req))
	fmt.Println(decision.Forbidden, decision.StatusCode)

	// Output:
	// false 200
	// true 403
}

// headerSource is the twenty lines an out-of-tree adapter has to write: where
// the peer address and the request headers come from. Everything about *when*
// a forwarded header may be believed stays in Config.ClientIP.
type headerSource struct {
	peer    net.IP
	headers map[string]string
}

func (s headerSource) RemoteIP() net.IP          { return s.peer }
func (s headerSource) Header(name string) string { return s.headers[name] }

func ExampleConfig_ClientIP() {
	config := health.DefaultConfig().WithTrustedProxies([]string{"10.0.0.0/8"})

	trusted := headerSource{
		peer:    net.ParseIP("10.0.0.1"),
		headers: map[string]string{"X-Forwarded-For": "192.168.1.1, 172.16.0.1"},
	}
	fmt.Println(config.ClientIP(trusted))

	untrusted := headerSource{
		peer:    net.ParseIP("203.0.113.10"),
		headers: map[string]string{"X-Forwarded-For": "192.168.1.1"},
	}
	fmt.Println(config.ClientIP(untrusted))

	// Output:
	// 192.168.1.1
	// 203.0.113.10
}
