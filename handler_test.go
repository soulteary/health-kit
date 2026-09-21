package health

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

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

		var result SimpleResponse
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

	t.Run("empty RemoteAddr with whitelist returns 403", func(t *testing.T) {
		config := DefaultConfig().WithServiceName("test").WithIPWhitelist([]string{"192.168.1.1"})
		aggregator := NewAggregator(config)
		aggregator.AddChecker(&mockChecker{name: "redis", status: StatusHealthy})

		handler := Handler(aggregator)
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		// httptest.NewRequest 默认 RemoteAddr 为 192.0.2.1:1234，必须显式清空，
		// 否则走不到 Config.ClientIP 里 remoteIP == nil 返回 "" 的分支。
		req.RemoteAddr = ""
		rec := httptest.NewRecorder()

		handler(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("forbidden body is text/plain", func(t *testing.T) {
		// health.Handler 与 fiberadapter.Handler 的 403 响应体一直不一致，
		// 本 PR 有意保留该差异。两侧都钉死，避免以后被"顺手统一"掉。
		config := DefaultConfig().WithServiceName("test").WithIPWhitelist([]string{"192.168.1.1"})
		aggregator := NewAggregator(config)

		handler := Handler(aggregator)
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.RemoteAddr = "203.0.113.10:12345"
		rec := httptest.NewRecorder()

		handler(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.Equal(t, "Forbidden\n", rec.Body.String())
		assert.Contains(t, rec.Header().Get("Content-Type"), "text/plain")
	})

	t.Run("details with checks includes per-check results", func(t *testing.T) {
		aggregator := NewAggregator(DefaultInternalConfig().WithServiceName("test"))
		aggregator.AddChecker(&mockChecker{name: "redis", status: StatusHealthy})

		handler := Handler(aggregator)
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()

		handler(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var result AggregatedResult
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&result))
		assert.Equal(t, StatusHealthy, result.Status)
		assert.Equal(t, "test", result.Service)
		require.Contains(t, result.Checks, "redis")
		assert.Equal(t, StatusHealthy, result.Checks["redis"].Status)
	})

	t.Run("details without checks omits the checks key", func(t *testing.T) {
		config := DefaultInternalConfig().WithServiceName("test").WithChecks(false)
		aggregator := NewAggregator(config)
		aggregator.AddChecker(&mockChecker{name: "redis", status: StatusHealthy})

		handler := Handler(aggregator)
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()

		handler(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		body := rec.Body.String()
		assert.NotContains(t, body, `"checks"`)
		// 仍是完整的 AggregatedResult，而不是降级成 SimpleResponse。
		assert.Contains(t, body, `"total_latency_ms"`)
	})
}

// TestDecide 直接覆盖框架无关的核心判定，不经过任何 HTTP 栈。
// 这是三方适配器（Echo/Gin/chi）唯一需要依赖的契约。
func TestDecide(t *testing.T) {
	newAgg := func(config Config, status Status) *Aggregator {
		aggregator := NewAggregator(config)
		aggregator.AddChecker(&mockChecker{name: "redis", status: status})
		return aggregator
	}

	t.Run("forbidden carries a usable status code", func(t *testing.T) {
		config := DefaultConfig().WithServiceName("test").WithIPWhitelist([]string{"192.168.1.1"})
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.RemoteAddr = "203.0.113.10:12345"

		decision := Decide(context.Background(), newAgg(config, StatusHealthy), RequestSource(req))

		assert.True(t, decision.Forbidden)
		// 适配器若无条件写 StatusCode，也不能写出 0。
		assert.Equal(t, http.StatusForbidden, decision.StatusCode)
		assert.Nil(t, decision.Body)
	})

	t.Run("whitelisted client is not forbidden", func(t *testing.T) {
		config := DefaultConfig().WithServiceName("test").WithIPWhitelist([]string{"192.168.1.1"})
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.RemoteAddr = "192.168.1.1:12345"

		decision := Decide(context.Background(), newAgg(config, StatusHealthy), RequestSource(req))

		assert.False(t, decision.Forbidden)
		assert.Equal(t, http.StatusOK, decision.StatusCode)
	})

	t.Run("no details yields SimpleResponse", func(t *testing.T) {
		config := DefaultConfig().WithServiceName("test")
		req := httptest.NewRequest(http.MethodGet, "/health", nil)

		decision := Decide(context.Background(), newAgg(config, StatusHealthy), RequestSource(req))

		require.IsType(t, SimpleResponse{}, decision.Body)
		body := decision.Body.(SimpleResponse)
		assert.Equal(t, StatusHealthy, body.Status)
		assert.Equal(t, "test", body.Service)
	})

	t.Run("details yields AggregatedResult with checks", func(t *testing.T) {
		config := DefaultInternalConfig().WithServiceName("test")
		req := httptest.NewRequest(http.MethodGet, "/health", nil)

		decision := Decide(context.Background(), newAgg(config, StatusHealthy), RequestSource(req))

		require.IsType(t, AggregatedResult{}, decision.Body)
		body := decision.Body.(AggregatedResult)
		assert.Equal(t, StatusHealthy, body.Status)
		assert.Contains(t, body.Checks, "redis")
	})

	t.Run("details without checks strips the checks map", func(t *testing.T) {
		config := DefaultInternalConfig().WithServiceName("test").WithChecks(false)
		req := httptest.NewRequest(http.MethodGet, "/health", nil)

		decision := Decide(context.Background(), newAgg(config, StatusHealthy), RequestSource(req))

		require.IsType(t, AggregatedResult{}, decision.Body)
		assert.Nil(t, decision.Body.(AggregatedResult).Checks)
	})

	t.Run("unhealthy maps to 503", func(t *testing.T) {
		config := DefaultInternalConfig().WithServiceName("test")
		req := httptest.NewRequest(http.MethodGet, "/health", nil)

		decision := Decide(context.Background(), newAgg(config, StatusUnhealthy), RequestSource(req))

		assert.Equal(t, http.StatusServiceUnavailable, decision.StatusCode)
	})
}

func TestLivenessHandler(t *testing.T) {
	handler := LivenessHandler("test-service")
	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var result SimpleResponse
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

	var result SimpleResponse
	err := json.NewDecoder(rec.Body).Decode(&result)
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
			name:     "empty RemoteAddr yields an empty client IP",
			xff:      "192.168.1.1",
			trusted:  []string{"10.0.0.0/8"},
			expected: "",
		},
		{
			name:       "Untrusted proxy ignores X-Forwarded-For",
			xff:        "192.168.1.1",
			remoteAddr: "203.0.113.10:12345",
			trusted:    []string{"10.0.0.0/8"},
			expected:   "203.0.113.10",
		},
		{
			name:       "X-Real-IP invalid IP falls back to RemoteAddr",
			xri:        "not-an-ip",
			remoteAddr: "10.0.0.1:12345",
			trusted:    []string{"10.0.0.0/8"},
			expected:   "10.0.0.1",
		},
		{
			name:       "X-Forwarded-For empty string uses RemoteAddr",
			xff:        "",
			remoteAddr: "10.0.0.1:12345",
			trusted:    []string{"10.0.0.0/8"},
			expected:   "10.0.0.1",
		},
		{
			name:       "X-Forwarded-For first part whitespace only falls back to RemoteAddr",
			xff:        "  , 192.168.1.1",
			remoteAddr: "10.0.0.1:12345",
			trusted:    []string{"10.0.0.0/8"},
			expected:   "10.0.0.1",
		},
		{
			// 首段是非空垃圾串：parseForwardedIP 返回 ""，不能顺延取
			// 链路里下一个 IP，否则伪造 XFF 就能挑一个白名单内的地址。
			name:       "X-Forwarded-For invalid first IP does not fall through to the next hop",
			xff:        "invalid-ip, 192.168.1.1",
			remoteAddr: "10.0.0.1:12345",
			trusted:    []string{"10.0.0.0/8"},
			expected:   "10.0.0.1",
		},
		{
			name:       "X-Forwarded-For invalid first IP falls back to X-Real-IP",
			xff:        "invalid-ip",
			xri:        "192.168.1.1",
			remoteAddr: "10.0.0.1:12345",
			trusted:    []string{"10.0.0.0/8"},
			expected:   "192.168.1.1",
		},
		{
			name:       "X-Forwarded-For takes precedence over X-Real-IP",
			xff:        "192.168.1.1",
			xri:        "172.16.0.1",
			remoteAddr: "10.0.0.1:12345",
			trusted:    []string{"10.0.0.0/8"},
			expected:   "192.168.1.1",
		},
		{
			name:       "Untrusted proxy ignores X-Real-IP",
			xri:        "192.168.1.1",
			remoteAddr: "203.0.113.10:12345",
			trusted:    []string{"10.0.0.0/8"},
			expected:   "203.0.113.10",
		},
		{
			name:       "no trusted proxies configured ignores both headers",
			xff:        "192.168.1.1",
			xri:        "172.16.0.1",
			remoteAddr: "10.0.0.1:12345",
			expected:   "10.0.0.1",
		},
		{
			name:       "trusted proxy by exact IP rather than CIDR",
			xff:        "192.168.1.1",
			remoteAddr: "10.0.0.1:12345",
			trusted:    []string{"10.0.0.1"},
			expected:   "192.168.1.1",
		},
		{
			name:       "IPv6 RemoteAddr is unwrapped from its brackets",
			remoteAddr: "[2001:db8::1]:443",
			expected:   "2001:db8::1",
		},
		{
			name:       "IPv6 X-Forwarded-For from a trusted proxy",
			xff:        "2001:db8::2",
			remoteAddr: "[2001:db8::1]:443",
			trusted:    []string{"2001:db8::/32"},
			expected:   "2001:db8::2",
		},
		{
			name:       "X-Forwarded-For with a port is accepted",
			xff:        "192.168.1.1:9999",
			remoteAddr: "10.0.0.1:12345",
			trusted:    []string{"10.0.0.0/8"},
			expected:   "192.168.1.1",
		},
		{
			name:       "X-Real-IP whitespace only falls back to RemoteAddr",
			xri:        "   ",
			remoteAddr: "10.0.0.1:12345",
			trusted:    []string{"10.0.0.0/8"},
			expected:   "10.0.0.1",
		},
		{
			// RemoteAddr 不可解析时 ClientIP 返回 ""，IsIPAllowed("") 为 false，
			// 即"解析不出来就拒绝"，不能退化成放行。
			name:       "unparseable RemoteAddr yields an empty client IP",
			remoteAddr: "not-an-address",
			trusted:    []string{"10.0.0.0/8"},
			expected:   "",
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
			// 显式赋值（含空串）：httptest.NewRequest 默认填 192.0.2.1:1234。
			req.RemoteAddr = tt.remoteAddr

			config := DefaultConfig().WithTrustedProxies(tt.trusted)
			result := config.ClientIP(RequestSource(req))
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
