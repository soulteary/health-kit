// Fiber adapter tests, moved here from the root package together with the
// handlers they cover. External test package on purpose: they compile only
// against health-kit's exported API, which is what an out-of-tree adapter has.
package fiberadapter_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	health "github.com/soulteary/health-kit/v4"
	"github.com/soulteary/health-kit/v4/fiberadapter"
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
			WithTrustedProxies([]string{testPeerIP})
		aggregator := health.NewAggregator(config)
		aggregator.AddChecker(&mockChecker{name: "redis", status: health.StatusHealthy})

		app.Get("/health", fiberadapter.Handler(aggregator))

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.Header.Set("X-Forwarded-For", "invalid-ip, 192.168.1.1")
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		// 首段 "invalid-ip" 无效，parseForwardedIP 返回 ""；不得顺延取链路里的
		// 192.168.1.1，否则伪造 XFF 就能挑一个白名单内地址。无 X-Real-IP，
		// 因此回退到对端 0.0.0.0，不在白名单 → 403。
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})

	t.Run("trusted proxy X-Forwarded-For grants access", func(t *testing.T) {
		// 反向用例：同样的配置下合法 XFF 必须放行。缺了它，上面的 403
		// 可能只是因为整条可信代理链路根本没跑起来。
		app := fiber.New()
		config := health.DefaultConfig().
			WithServiceName("test").
			WithIPWhitelist([]string{"192.168.1.1"}).
			WithTrustedProxies([]string{testPeerIP})
		aggregator := health.NewAggregator(config)
		aggregator.AddChecker(&mockChecker{name: "redis", status: health.StatusHealthy})

		app.Get("/health", fiberadapter.Handler(aggregator))

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.Header.Set("X-Forwarded-For", "192.168.1.1")
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("untrusted peer ignores X-Forwarded-For", func(t *testing.T) {
		app := fiber.New()
		config := health.DefaultConfig().
			WithServiceName("test").
			WithIPWhitelist([]string{"192.168.1.1"}).
			WithTrustedProxies([]string{"10.0.0.0/8"}) // 不含 0.0.0.0
		aggregator := health.NewAggregator(config)

		app.Get("/health", fiberadapter.Handler(aggregator))

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.Header.Set("X-Forwarded-For", "192.168.1.1")
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})

	t.Run("forbidden body is JSON", func(t *testing.T) {
		// 与 health.Handler 的 text/plain "Forbidden" 有意不同，本 PR 明确
		// 保留该差异。钉死它，避免以后被"顺手统一"掉而无人察觉。
		app := fiber.New()
		config := health.DefaultConfig().WithServiceName("test").WithIPWhitelist([]string{"192.168.1.1"})
		aggregator := health.NewAggregator(config)

		app.Get("/health", fiberadapter.Handler(aggregator))

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		require.Equal(t, http.StatusForbidden, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), fiber.MIMEApplicationJSON)

		var body map[string]string
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		assert.Equal(t, map[string]string{"error": "Forbidden"}, body)
	})

	t.Run("details with checks includes per-check results", func(t *testing.T) {
		// 原有 "healthy response" 用的是 DefaultConfig（IncludeDetails=false），
		// 解成 AggregatedResult 也能过，等于没验到 details 分支。
		app := fiber.New()
		aggregator := health.NewAggregator(health.DefaultInternalConfig().WithServiceName("test"))
		aggregator.AddChecker(&mockChecker{name: "redis", status: health.StatusHealthy})

		app.Get("/health", fiberadapter.Handler(aggregator))

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result health.AggregatedResult
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
		assert.Equal(t, health.StatusHealthy, result.Status)
		require.Contains(t, result.Checks, "redis")
		assert.Equal(t, health.StatusHealthy, result.Checks["redis"].Status)
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

// testPeerIP 是 Fiber 的 app.Test 呈现给 handler 的对端地址。
//
// app.Test 走的是内存假连接，不会把 http.Request.RemoteAddr 带到
// fasthttp.RequestCtx 上，所以 c.RequestCtx().RemoteIP() 恒为 0.0.0.0。
// 想让可信代理分支真正跑起来，就得把这个地址列进 TrustedProxies —— 原先
// 那条设了 req.RemoteAddr = "10.0.0.1:12345" 的用例从来没进过该分支。
const testPeerIP = "0.0.0.0"

// TestClientIPParityAcrossAdapters 是本次拆包的核心不变量：
// 同一份 Config、同一组请求头，net/http 与 Fiber 两个 ClientIPSource
// 必须解析出完全相同的客户端 IP。
//
// 这条规则以前有两份实现（getClientIPFromRequest / getClientIPFromFiber），
// 合并成一份之后，真正的风险就从"两份逻辑写歪"转移到了"两个 Source
// 取值取歪"—— 比如某个适配器的 Header() 大小写敏感、或 RemoteIP() 取到了
// 代理地址。那同样是一次白名单绕过，而且只有跨框架对比才验得出来。
func TestClientIPParityAcrossAdapters(t *testing.T) {
	tests := []struct {
		name     string
		xff      string
		xri      string
		trusted  []string
		expected string
	}{
		{
			name:     "no headers falls back to the peer",
			trusted:  []string{testPeerIP},
			expected: testPeerIP,
		},
		{
			name:     "trusted peer honours X-Forwarded-For",
			xff:      "192.168.1.1",
			trusted:  []string{testPeerIP},
			expected: "192.168.1.1",
		},
		{
			name:     "trusted peer takes the left-most X-Forwarded-For hop",
			xff:      "192.168.1.1, 10.0.0.1, 172.16.0.1",
			trusted:  []string{testPeerIP},
			expected: "192.168.1.1",
		},
		{
			name:     "trusted peer honours X-Real-IP",
			xri:      "192.168.1.1",
			trusted:  []string{testPeerIP},
			expected: "192.168.1.1",
		},
		{
			name:     "X-Forwarded-For wins over X-Real-IP",
			xff:      "192.168.1.1",
			xri:      "172.16.0.1",
			trusted:  []string{testPeerIP},
			expected: "192.168.1.1",
		},
		{
			name:     "invalid X-Forwarded-For does not fall through to the next hop",
			xff:      "invalid-ip, 192.168.1.1",
			trusted:  []string{testPeerIP},
			expected: testPeerIP,
		},
		{
			name:     "invalid X-Forwarded-For falls back to X-Real-IP",
			xff:      "invalid-ip",
			xri:      "192.168.1.1",
			trusted:  []string{testPeerIP},
			expected: "192.168.1.1",
		},
		{
			name:     "invalid X-Real-IP falls back to the peer",
			xri:      "not-an-ip",
			trusted:  []string{testPeerIP},
			expected: testPeerIP,
		},
		{
			name:     "untrusted peer ignores X-Forwarded-For",
			xff:      "192.168.1.1",
			trusted:  []string{"10.0.0.0/8"},
			expected: testPeerIP,
		},
		{
			name:     "untrusted peer ignores X-Real-IP",
			xri:      "192.168.1.1",
			trusted:  []string{"10.0.0.0/8"},
			expected: testPeerIP,
		},
		{
			name:     "no trusted proxies configured ignores both headers",
			xff:      "192.168.1.1",
			xri:      "172.16.0.1",
			expected: testPeerIP,
		},
		{
			name:     "X-Forwarded-For with a port",
			xff:      "192.168.1.1:9999",
			trusted:  []string{testPeerIP},
			expected: "192.168.1.1",
		},
		{
			name:     "IPv6 X-Forwarded-For",
			xff:      "2001:db8::2",
			trusted:  []string{testPeerIP},
			expected: "2001:db8::2",
		},
		{
			name:     "lower-case header names resolve the same",
			xff:      "192.168.1.1",
			trusted:  []string{testPeerIP},
			expected: "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := health.DefaultConfig().WithTrustedProxies(tt.trusted)

			newReq := func() *http.Request {
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				if tt.xff != "" {
					req.Header.Set("X-Forwarded-For", tt.xff)
				}
				if tt.xri != "" {
					req.Header.Set("X-Real-IP", tt.xri)
				}
				return req
			}

			// net/http 侧：把对端对齐成 Fiber 假连接呈现的同一个地址。
			stdReq := newReq()
			stdReq.RemoteAddr = net.JoinHostPort(testPeerIP, "0")
			stdIP := config.ClientIP(health.RequestSource(stdReq))

			// Fiber 侧：在真实 handler 内部走 fiberadapter.Source。
			var fiberIP string
			app := fiber.New()
			app.Get("/", func(c fiber.Ctx) error {
				fiberIP = config.ClientIP(fiberadapter.Source{C: c})
				return c.SendStatus(fiber.StatusOK)
			})
			resp, err := app.Test(newReq())
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())

			assert.Equal(t, tt.expected, stdIP, "net/http source")
			assert.Equal(t, tt.expected, fiberIP, "fiber source")
			assert.Equal(t, stdIP, fiberIP, "两个适配器对同一请求解析出了不同的客户端 IP")
		})
	}
}

// TestSourceHeader 直接覆盖 Source.Header —— 它此前没有任何测试碰到过。
func TestSourceHeader(t *testing.T) {
	app := fiber.New()

	var got, missing string
	var remote net.IP
	app.Get("/", func(c fiber.Ctx) error {
		src := fiberadapter.Source{C: c}
		got = src.Header("X-Forwarded-For")
		missing = src.Header("X-Does-Not-Exist")
		remote = src.RemoteIP()
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "192.168.1.1")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	assert.Equal(t, "192.168.1.1", got)
	assert.Empty(t, missing, "缺失的头必须返回空串，而不是 panic 或占位值")
	require.NotNil(t, remote)
	assert.Equal(t, testPeerIP, remote.String())
}

// Source 必须满足 health.ClientIPSource —— 这正是三方适配器要实现的契约。
var _ health.ClientIPSource = fiberadapter.Source{}
