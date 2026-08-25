# health-kit

[![Go Reference](https://pkg.go.dev/badge/github.com/soulteary/health-kit/v2.svg)](https://pkg.go.dev/github.com/soulteary/health-kit/v2)
[![Go Report Card](https://goreportcard.com/badge/github.com/soulteary/health-kit/v2)](https://goreportcard.com/report/github.com/soulteary/health-kit/v2)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![codecov](https://codecov.io/gh/soulteary/health-kit/graph/badge.svg)](https://codecov.io/gh/soulteary/health-kit)

[English](README.md)

统一的 Go 服务健康检查工具包。提供健康检查接口、探针实现、多探针聚合以及兼容 Fiber 和 net/http 的 HTTP 处理器。

## 特性

- **检查器接口**：所有探针的统一健康检查接口
- **内置探针**：Redis、HTTP、数据库和自定义探针
- **并行聚合**：并行运行多个健康检查并聚合结果
- **HTTP 处理器**：标准库和 Fiber 兼容的 `/health` 端点处理器
- **Kubernetes 支持**：专用的存活和就绪探针处理器
- **IP 白名单**：限制特定 IP/CIDR 访问健康检查端点
- **关键检查**：区分关键和非关键依赖
- **延迟追踪**：测量和报告健康检查延迟

## 安装

```bash
go get github.com/soulteary/health-kit/v2
```

v2 的所有 Fiber 专用 Handler 均基于 Fiber v3。仍使用 Fiber v2 的应用应继续使用 health-kit v1；net/http Handler 与探针 API 的行为保持不变。

## 使用

### 基础健康检查

```go
import (
    health "github.com/soulteary/health-kit/v2"
)

// 创建配置
config := health.DefaultConfig().
    WithServiceName("myservice").
    WithTimeout(5 * time.Second)

// 创建聚合器
aggregator := health.NewAggregator(config)

// 添加检查器
aggregator.AddCheckers(
    health.NewRedisChecker(redisClient),
    health.NewHTTPChecker("herald", "http://herald:8080/healthz"),
)

// 执行健康检查
result := aggregator.Check(context.Background())
fmt.Printf("状态: %s\n", result.Status)
```

### Redis 健康检查

```go
// 基础 Redis 检查器
redisChecker := health.NewRedisChecker(redisClient)

// 自定义名称和超时
redisChecker := health.NewRedisCheckerWithName("session-redis", redisClient).
    WithTimeout(2 * time.Second)
```

### HTTP 依赖检查

```go
// 检查外部服务健康状态
httpChecker := health.NewHTTPChecker("herald", "http://herald:8080/healthz").
    WithTimeout(3 * time.Second).
    WithExpectedCode(http.StatusOK)

// 使用自定义 HTTP 方法
httpChecker := health.NewHTTPChecker("api", "http://api/status").
    WithMethod(http.MethodHead)
```

### 数据库健康检查

```go
// 基础数据库检查器
dbChecker := health.NewDBChecker(db)

// 自定义名称
dbChecker := health.NewDBCheckerWithName("postgres", db).
    WithTimeout(5 * time.Second)
```

### 自定义健康检查

```go
// 创建自定义检查器
customChecker := health.NewCustomChecker("cache", func(ctx context.Context) error {
    if cache.Size() == 0 {
        return errors.New("缓存为空")
    }
    return nil
}).WithTimeout(1 * time.Second)
```

### 禁用检查器

```go
// 用于未配置的可选依赖
disabledChecker := health.NewDisabledChecker("optional-redis").
    WithMessage("Redis 未配置")
```

### HTTP 处理器（标准库）

```go
import (
    "net/http"
    health "github.com/soulteary/health-kit/v2"
)

// 完整健康检查，包含所有探针
http.HandleFunc("/health", health.Handler(aggregator))

// Kubernetes 存活探针（服务运行时始终返回 OK）
http.HandleFunc("/livez", health.LivenessHandler("myservice"))

// Kubernetes 就绪探针（检查所有依赖）
http.HandleFunc("/readyz", health.ReadinessHandler(aggregator))

// 简单健康检查，不含探针
http.HandleFunc("/health", health.SimpleHandler("myservice"))
```

### Fiber 处理器

```go
import (
    "github.com/gofiber/fiber/v3"
    health "github.com/soulteary/health-kit/v2"
)

app := fiber.New()

// 完整健康检查
app.Get("/health", health.FiberHandler(aggregator))
app.Get("/healthz", health.FiberHandler(aggregator))

// Kubernetes 存活探针
app.Get("/livez", health.FiberLivenessHandler("myservice"))

// Kubernetes 就绪探针
app.Get("/readyz", health.FiberReadinessHandler(aggregator))

// 简单健康检查
app.Get("/health", health.SimpleFiberHandler("myservice"))
```

### 配置选项

```go
config := health.DefaultConfig().
    WithServiceName("herald").
    WithTimeout(5 * time.Second).
    WithIPWhitelist([]string{"10.0.0.0/8", "192.168.1.1"}).
    WithTrustedProxies([]string{"10.0.0.0/8"}). // 仅信任反向代理
    WithDetails(true).          // 包含详细响应
    WithChecks(true).           // 包含单个检查结果
    WithCriticalChecks([]string{"redis", "database"})  // 关键依赖
```

### 关键与非关键检查

```go
// 定义哪些检查是关键的
config := health.DefaultConfig().
    WithCriticalChecks([]string{"redis", "database"})

aggregator := health.NewAggregator(config)
aggregator.AddCheckers(
    health.NewRedisChecker(redisClient),          // 关键
    health.NewDBChecker(db),                       // 关键
    health.NewHTTPChecker("cache", cacheURL),      // 非关键
)

result := aggregator.Check(ctx)
// 如果 Redis 或 DB 失败: Status = "unhealthy"
// 如果只有 cache 失败: Status = "degraded"
// 如果全部通过: Status = "ok"
```

### IP 白名单

```go
config := health.DefaultConfig().
    WithIPWhitelist([]string{
        "127.0.0.1",        // 本地主机
        "10.0.0.0/8",       // 私有网络 CIDR
        "192.168.1.100",    // 特定 IP
    })

// 非白名单 IP 的请求将收到 403 Forbidden
```

### 可信代理（转发头）

当服务部署在反向代理或负载均衡后面时，请配置可信代理 IP/CIDR，只有来自
可信代理的 `X-Forwarded-For` / `X-Real-IP` 才会被采纳，避免被伪造头绕过。

```go
config := health.DefaultConfig().
    WithIPWhitelist([]string{"192.168.1.100"}).
    WithTrustedProxies([]string{"10.0.0.0/8"}) // 仅信任代理 IP
```

### 生产环境隐私

健康检查默认返回详细依赖信息。生产环境建议关闭详细信息与单项检查结果，
避免泄露内部状态。

## 项目结构

```
health-kit/
├── checker.go         # 检查器接口、结果类型及 JSON 序列化
├── config.go          # 配置，支持 IP 白名单
├── probes.go          # 内置探针（Redis、HTTP、DB、自定义、禁用）
├── aggregator.go      # 多探针聚合，支持并行执行
├── handler.go         # Fiber 和 net/http 的 HTTP 处理器
└── *_test.go          # 完整测试
```

## 集成示例

### Herald（OTP 服务）

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

### Stargate（认证网关）

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

### Warden（用户服务）

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
    
    // Warden 在 ONLY_LOCAL 模式下 Redis 是可选的
    if redisEnabled {
        aggregator.AddChecker(health.NewRedisChecker(redisClient))
    } else {
        aggregator.AddChecker(health.NewDisabledChecker("redis"))
    }
    
    // 检查数据是否已加载
    aggregator.AddChecker(health.NewCustomChecker("data_loaded", func(ctx context.Context) error {
        if userCache.Len() == 0 {
            return errors.New("没有加载用户")
        }
        return nil
    }))
    
    http.HandleFunc("/health", health.Handler(aggregator))
    http.HandleFunc("/healthcheck", health.Handler(aggregator))
    
    http.ListenAndServe(":8080", nil)
}
```

## 响应格式

### 详细响应（默认）

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

### 简单响应（WithDetails(false)）

```json
{
  "status": "ok",
  "service": "myservice"
}
```

### 降级响应

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

## HTTP 状态码

| 健康状态 | HTTP 状态码 |
|---------|------------|
| ok | 200 OK |
| degraded | 200 OK |
| unhealthy | 503 Service Unavailable |
| disabled | 不适用（聚合时跳过） |

## 要求

- Go 1.26 或更高版本
- github.com/gofiber/fiber/v3 v3.4.0+（用于 Fiber 处理器）
- github.com/redis/go-redis/v9 v9.7.3+（用于 Redis 探针）

## 测试覆盖率

运行测试：

```bash
go test ./... -v

# 带覆盖率
go test ./... -coverprofile=coverage.out -covermode=atomic
go tool cover -html=coverage.out -o coverage.html
go tool cover -func=coverage.out
```

## 贡献

1. Fork 本仓库
2. 创建功能分支 (`git checkout -b feature/amazing-feature`)
3. 提交更改 (`git commit -m 'Add some amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing-feature`)
5. 提交 Pull Request

## 许可证

详见 [LICENSE](LICENSE) 文件。
