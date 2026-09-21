// Fiber adapter tests, moved here from the root package together with the
// handlers they cover. External test package on purpose: they compile only
// against health-kit's exported API, which is what an out-of-tree adapter has.
package fiberadapter_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	health "github.com/soulteary/health-kit/v2"
	"github.com/soulteary/health-kit/v2/fiberadapter"
)

// mockChecker mirrors the root package's test double; the adapter tests live
// in an external package and cannot reach that one.
type mockChecker struct {
	name   string
	status health.Status
	err    string
}

func (m *mockChecker) Name() string { return m.name }

func (m *mockChecker) Check(ctx context.Context) health.CheckResult {
	return health.CheckResult{
		Name:    m.name,
		Status:  m.status,
		Error:   m.err,
		Latency: 0,
	}
}
func TestFiberHandler(t *testing.T) {
	t.Run("healthy response", func(t *testing.T) {
		app := fiber.New()
		aggregator := health.NewAggregator(health.DefaultConfig().WithServiceName("test"))
		aggregator.AddChecker(&mockChecker{name: "redis", status: health.StatusHealthy})

		app.Get("/health", fiberadapter.Handler(aggregator))

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result health.AggregatedResult
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)
		assert.Equal(t, health.StatusHealthy, result.Status)
	})

	t.Run("unhealthy response", func(t *testing.T) {
		app := fiber.New()
		aggregator := health.NewAggregator(health.DefaultConfig().WithServiceName("test"))
		aggregator.AddChecker(&mockChecker{name: "redis", status: health.StatusUnhealthy})

		app.Get("/health", fiberadapter.Handler(aggregator))

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	})

	t.Run("without details", func(t *testing.T) {
		app := fiber.New()
		config := health.DefaultConfig().WithServiceName("test").WithDetails(false)
		aggregator := health.NewAggregator(config)
		aggregator.AddChecker(&mockChecker{name: "redis", status: health.StatusHealthy})

		app.Get("/health", fiberadapter.Handler(aggregator))

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		var result health.SimpleResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)
		assert.Equal(t, health.StatusHealthy, result.Status)
	})

	t.Run("without checks", func(t *testing.T) {
		app := fiber.New()
		config := health.DefaultConfig().WithServiceName("test").WithChecks(false)
		aggregator := health.NewAggregator(config)
		aggregator.AddChecker(&mockChecker{name: "redis", status: health.StatusHealthy})

		app.Get("/health", fiberadapter.Handler(aggregator))

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, _ := io.ReadAll(resp.Body)
		assert.NotContains(t, string(body), `"checks"`)
	})

	t.Run("IP whitelist denied", func(t *testing.T) {
		app := fiber.New()
		config := health.DefaultConfig().WithServiceName("test").WithIPWhitelist([]string{"192.168.1.1"})
		aggregator := health.NewAggregator(config)

		app.Get("/health", fiberadapter.Handler(aggregator))

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})

	t.Run("trusted proxy X-Forwarded-For invalid first IP falls back", func(t *testing.T) {
		app := fiber.New()
		config := health.DefaultConfig().
			WithServiceName("test").
			WithIPWhitelist([]string{"192.168.1.1"}).
			WithTrustedProxies([]string{"10.0.0.0/8"})
		aggregator := health.NewAggregator(config)
		aggregator.AddChecker(&mockChecker{name: "redis", status: health.StatusHealthy})

		app.Get("/health", fiberadapter.Handler(aggregator))

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		req.Header.Set("X-Forwarded-For", "invalid-ip, 192.168.1.1")
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		// parseForwardedIP 第一个 IP 无效返回 ""，随后可能用 X-Real-IP 或 remoteIP
		// 此处无 X-Real-IP，会回退到 10.0.0.1，不在白名单，403
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})
}

func TestFiberLivenessHandler(t *testing.T) {
	app := fiber.New()
	app.Get("/livez", fiberadapter.LivenessHandler("test-service"))

	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result health.SimpleResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, health.StatusHealthy, result.Status)
	assert.Equal(t, "test-service", result.Service)
}

func TestFiberReadinessHandler(t *testing.T) {
	app := fiber.New()
	aggregator := health.NewAggregator(health.DefaultConfig().WithServiceName("test"))
	aggregator.AddChecker(&mockChecker{name: "redis", status: health.StatusHealthy})

	app.Get("/readyz", fiberadapter.ReadinessHandler(aggregator))

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestSimpleFiberHandler(t *testing.T) {
	app := fiber.New()
	app.Get("/health", fiberadapter.SimpleHandler("simple-service"))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result health.SimpleResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, health.StatusHealthy, result.Status)
	assert.Equal(t, "simple-service", result.Service)
}
