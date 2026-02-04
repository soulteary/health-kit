package health

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPStatusCode(t *testing.T) {
	tests := []struct {
		status   Status
		expected int
	}{
		{StatusHealthy, http.StatusOK},
		{StatusDegraded, http.StatusOK},
		{StatusUnhealthy, http.StatusServiceUnavailable},
		{StatusDisabled, http.StatusServiceUnavailable},
		{StatusUnknown, http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			assert.Equal(t, tt.expected, HTTPStatusCode(tt.status))
		})
	}
}

func TestHandler(t *testing.T) {
	t.Run("healthy response", func(t *testing.T) {
		aggregator := NewAggregator(DefaultConfig().WithServiceName("test"))
		aggregator.AddChecker(&mockChecker{name: "redis", status: StatusHealthy})

		handler := Handler(aggregator)
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()

		handler(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var result AggregatedResult
		err := json.NewDecoder(rec.Body).Decode(&result)
		require.NoError(t, err)
		assert.Equal(t, StatusHealthy, result.Status)
		assert.Equal(t, "test", result.Service)
	})

	t.Run("unhealthy response", func(t *testing.T) {
		aggregator := NewAggregator(DefaultConfig().WithServiceName("test"))
		aggregator.AddChecker(&mockChecker{name: "redis", status: StatusUnhealthy, err: "connection failed"})

		handler := Handler(aggregator)
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()

		handler(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})

	t.Run("without details", func(t *testing.T) {
		config := DefaultConfig().WithServiceName("test").WithDetails(false)
		aggregator := NewAggregator(config)
		aggregator.AddChecker(&mockChecker{name: "redis", status: StatusHealthy})

		handler := Handler(aggregator)
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()

		handler(rec, req)

		var result simpleResponse
		err := json.NewDecoder(rec.Body).Decode(&result)
		require.NoError(t, err)
		assert.Equal(t, StatusHealthy, result.Status)
		assert.Equal(t, "test", result.Service)
	})

	t.Run("without checks", func(t *testing.T) {
		config := DefaultConfig().WithServiceName("test").WithChecks(false)
		aggregator := NewAggregator(config)
		aggregator.AddChecker(&mockChecker{name: "redis", status: StatusHealthy})

		handler := Handler(aggregator)
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()

		handler(rec, req)

		body, _ := io.ReadAll(rec.Body)
		assert.NotContains(t, string(body), `"checks"`)
	})

	t.Run("IP whitelist allowed", func(t *testing.T) {
		config := DefaultConfig().WithServiceName("test").WithIPWhitelist([]string{"192.168.1.1"})
		aggregator := NewAggregator(config)
		aggregator.AddChecker(&mockChecker{name: "redis", status: StatusHealthy})

		handler := Handler(aggregator)
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rec := httptest.NewRecorder()

		handler(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("IP whitelist denied", func(t *testing.T) {
		config := DefaultConfig().WithServiceName("test").WithIPWhitelist([]string{"192.168.1.1"})
		aggregator := NewAggregator(config)
		aggregator.AddChecker(&mockChecker{name: "redis", status: StatusHealthy})

		handler := Handler(aggregator)
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		rec := httptest.NewRecorder()

		handler(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("X-Forwarded-For header", func(t *testing.T) {
		config := DefaultConfig().
			WithServiceName("test").
			WithIPWhitelist([]string{"192.168.1.1"}).
			WithTrustedProxies([]string{"10.0.0.0/8"})
		aggregator := NewAggregator(config)

		handler := Handler(aggregator)
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.Header.Set("X-Forwarded-For", "192.168.1.1, 10.0.0.1")
		req.RemoteAddr = "10.0.0.1:12345"
		rec := httptest.NewRecorder()

		handler(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("X-Real-IP header", func(t *testing.T) {
		config := DefaultConfig().
			WithServiceName("test").
			WithIPWhitelist([]string{"192.168.1.1"}).
			WithTrustedProxies([]string{"10.0.0.0/8"})
		aggregator := NewAggregator(config)

		handler := Handler(aggregator)
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.Header.Set("X-Real-IP", "192.168.1.1")
		req.RemoteAddr = "10.0.0.1:12345"
		rec := httptest.NewRecorder()

		handler(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("untrusted proxy ignores forwarded headers", func(t *testing.T) {
		config := DefaultConfig().
			WithServiceName("test").
			WithIPWhitelist([]string{"192.168.1.1"}).
			WithTrustedProxies([]string{"10.0.0.0/8"})
		aggregator := NewAggregator(config)

		handler := Handler(aggregator)
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.Header.Set("X-Forwarded-For", "192.168.1.1")
		req.RemoteAddr = "203.0.113.10:12345"
		rec := httptest.NewRecorder()

		handler(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
}

func TestLivenessHandler(t *testing.T) {
	handler := LivenessHandler("test-service")
	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var result simpleResponse
	err := json.NewDecoder(rec.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, StatusHealthy, result.Status)
	assert.Equal(t, "test-service", result.Service)
}

func TestReadinessHandler(t *testing.T) {
	aggregator := NewAggregator(DefaultConfig().WithServiceName("test"))
	aggregator.AddChecker(&mockChecker{name: "redis", status: StatusHealthy})

	handler := ReadinessHandler(aggregator)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestSimpleHandler(t *testing.T) {
	handler := SimpleHandler("simple-service")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var result simpleResponse
	err := json.NewDecoder(rec.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, StatusHealthy, result.Status)
	assert.Equal(t, "simple-service", result.Service)
}

func TestFiberHandler(t *testing.T) {
	t.Run("healthy response", func(t *testing.T) {
		app := fiber.New()
		aggregator := NewAggregator(DefaultConfig().WithServiceName("test"))
		aggregator.AddChecker(&mockChecker{name: "redis", status: StatusHealthy})

		app.Get("/health", FiberHandler(aggregator))

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result AggregatedResult
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)
		assert.Equal(t, StatusHealthy, result.Status)
	})

	t.Run("unhealthy response", func(t *testing.T) {
		app := fiber.New()
		aggregator := NewAggregator(DefaultConfig().WithServiceName("test"))
		aggregator.AddChecker(&mockChecker{name: "redis", status: StatusUnhealthy})

		app.Get("/health", FiberHandler(aggregator))

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	})

	t.Run("without details", func(t *testing.T) {
		app := fiber.New()
		config := DefaultConfig().WithServiceName("test").WithDetails(false)
		aggregator := NewAggregator(config)
		aggregator.AddChecker(&mockChecker{name: "redis", status: StatusHealthy})

		app.Get("/health", FiberHandler(aggregator))

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		var result simpleResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)
		assert.Equal(t, StatusHealthy, result.Status)
	})

	t.Run("without checks", func(t *testing.T) {
		app := fiber.New()
		config := DefaultConfig().WithServiceName("test").WithChecks(false)
		aggregator := NewAggregator(config)
		aggregator.AddChecker(&mockChecker{name: "redis", status: StatusHealthy})

		app.Get("/health", FiberHandler(aggregator))

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
		config := DefaultConfig().WithServiceName("test").WithIPWhitelist([]string{"192.168.1.1"})
		aggregator := NewAggregator(config)

		app.Get("/health", FiberHandler(aggregator))

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})
}

func TestFiberLivenessHandler(t *testing.T) {
	app := fiber.New()
	app.Get("/livez", FiberLivenessHandler("test-service"))

	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result simpleResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, StatusHealthy, result.Status)
	assert.Equal(t, "test-service", result.Service)
}

func TestFiberReadinessHandler(t *testing.T) {
	app := fiber.New()
	aggregator := NewAggregator(DefaultConfig().WithServiceName("test"))
	aggregator.AddChecker(&mockChecker{name: "redis", status: StatusHealthy})

	app.Get("/readyz", FiberReadinessHandler(aggregator))

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestSimpleFiberHandler(t *testing.T) {
	app := fiber.New()
	app.Get("/health", SimpleFiberHandler("simple-service"))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result simpleResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, StatusHealthy, result.Status)
	assert.Equal(t, "simple-service", result.Service)
}

func TestGetClientIPFromRequest(t *testing.T) {
	tests := []struct {
		name       string
		xff        string
		xri        string
		remoteAddr string
		trusted    []string
		expected   string
	}{
		{
			name:       "X-Forwarded-For single IP",
			xff:        "192.168.1.1",
			trusted:    []string{"10.0.0.0/8"},
			remoteAddr: "10.0.0.1:12345",
			expected:   "192.168.1.1",
		},
		{
			name:       "X-Forwarded-For multiple IPs",
			xff:        "192.168.1.1, 10.0.0.1, 172.16.0.1",
			trusted:    []string{"10.0.0.0/8"},
			remoteAddr: "10.0.0.1:12345",
			expected:   "192.168.1.1",
		},
		{
			name:       "X-Real-IP",
			xri:        "192.168.1.1",
			remoteAddr: "10.0.0.1:12345",
			trusted:    []string{"10.0.0.0/8"},
			expected:   "192.168.1.1",
		},
		{
			name:       "RemoteAddr fallback",
			remoteAddr: "192.168.1.1:12345",
			expected:   "192.168.1.1",
		},
		{
			name:       "Untrusted proxy ignores X-Forwarded-For",
			xff:        "192.168.1.1",
			remoteAddr: "203.0.113.10:12345",
			trusted:    []string{"10.0.0.0/8"},
			expected:   "203.0.113.10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.xri != "" {
				req.Header.Set("X-Real-IP", tt.xri)
			}
			if tt.remoteAddr != "" {
				req.RemoteAddr = tt.remoteAddr
			}

			config := DefaultConfig().WithTrustedProxies(tt.trusted)
			result := getClientIPFromRequest(req, config)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Benchmark tests
func BenchmarkAggregator_Check(b *testing.B) {
	aggregator := NewAggregator(DefaultConfig().WithServiceName("bench"))
	aggregator.AddCheckers(
		&mockChecker{name: "redis", status: StatusHealthy},
		&mockChecker{name: "database", status: StatusHealthy},
		&mockChecker{name: "cache", status: StatusHealthy},
	)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		aggregator.Check(ctx)
	}
}

func BenchmarkHandler(b *testing.B) {
	aggregator := NewAggregator(DefaultConfig().WithServiceName("bench"))
	aggregator.AddCheckers(
		&mockChecker{name: "redis", status: StatusHealthy},
		&mockChecker{name: "database", status: StatusHealthy},
	)

	handler := Handler(aggregator)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler(rec, req)
	}
}
