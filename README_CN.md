# health-kit

[![Go Reference](https://pkg.go.dev/badge/github.com/soulteary/health-kit/v2.svg)](https://pkg.go.dev/github.com/soulteary/health-kit/v2)
[![Go Report Card](.github/goreportcard.svg)](.github/goreportcard-report.md)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![codecov](https://codecov.io/gh/soulteary/health-kit/graph/badge.svg)](https://codecov.io/gh/soulteary/health-kit)

[English](README.md)

统一的 Go 服务健康检查工具包。提供健康检查接口、探针实现、多探针聚合以及兼容 Fiber 和 net/http 的 HTTP 处理器。


> **v2.4.0 破坏性变更 —— Fiber 支持移入子包。**
> Fiber handler 现位于 `github.com/soulteary/health-kit/v2/fiberadapter`，
> 于是导入根包不再把 Fiber（以及 fasthttp）链接进用不到它的二进制。
> 对一个 net/http 服务来说，这意味着**少链接 25 个包、少 8 个模块、二进制小 14%**。
>
> | 原来 | 现在 |
> |---|---|
> | `health.FiberHandler(agg)` | `fiberadapter.Handler(agg)` |
> | `health.FiberLivenessHandler(name)` | `fiberadapter.LivenessHandler(name)` |
> | `health.FiberReadinessHandler(agg)` | `fiberadapter.ReadinessHandler(agg)` |
> | `health.SimpleFiberHandler(name)` | `fiberadapter.SimpleHandler(name)` |
>
> net/http 一侧没有任何变化。

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
    "github.com/soulteary/health-kit/v2/fiberadapter"
)

app := fiber.New()

// 完整健康检查
app.Get("/health", fiberadapter.Handler(aggregator))
app.Get("/healthz", fiberadapter.Handler(aggregator))

// Kubernetes 存活探针
app.Get("/livez", fiberadapter.LivenessHandler("myservice"))

// Kubernetes 就绪探针
app.Get("/readyz", fiberadapter.ReadinessHandler(aggregator))

// 简单健康检查
app.Get("/health", fiberadapter.SimpleHandler("myservice"))
```

### 配置选项

```go
config := health.DefaultConfig().
    WithServiceName("herald").
    WithTimeout(5 * time.Second).
    WithIPWhitelist([]string{"10.0.0.0/8", "192.168.1.1"}).
    WithTrustedProxies([]string{"10.0.0.0/8"}). // 只信任你自己的反向代理
    WithDetails(true).          // 主动开启逐项检查细节
    WithChecks(true).           // 主动开启 checks 结果映射
    WithCriticalChecks([]string{"redis", "database"})
```

| 配置项 | `DefaultConfig()` | 说明 |
|--------|-------------------|------|
| `ServiceName` | `"service"` | 会回显在响应里 |
| `Timeout` | `5s` | 强制生效：超时的检查器被记为 unhealthy |
| `IncludeDetails` | `false` | 逐项检查的 message、error 和 metadata |
| `IncludeChecks` | `false` | `checks` 映射本身 |
| `IPWhitelist` | `nil` | 为空表示不做 IP 限制 |
| `TrustedProxies` | `nil` | 哪些对端的转发头可以信 |
| `CriticalChecks` | `nil` | 这些名字失败会让整体结果变成 unhealthy |

`DefaultInternalConfig()` 就是在 `DefaultConfig()` 基础上把 `IncludeDetails` 和
`IncludeChecks` 打开。

用 `aggregator.Config()` 读回当前配置，用 `SetConfig` 整体替换。

### 超时、panic 与重名检查器

**超时是强制的，不是建议性的。** 任何在 `Config.Timeout` 内没有汇报的检查器都会被
记为 `unhealthy`，message 为"did not complete within ..."，然后聚合返回。忽略
context 的检查器——驱动调用不接收 context，或接收了却忽略它——再也不能把端点挂住。

失败原因会区分是*谁的*截止时间先到：调用方 context 已被取消，或调用方的截止时间早于
聚合器的，都会照实报告，而不是一律报成配置的超时。

**panic 的检查器不会拖垮进程。** 检查内部的 panic 会被 recover，并作为该项的
unhealthy 结果上报。这一点很重要：goroutine 里的 panic 是致命的，且*不会*被 HTTP
框架的中间件 recover（它在另一个调用栈上）——所以一个解引用了 nil 数据库句柄的探针，
此前会把它正在报告的那个服务杀掉。

**用同一个名字注册两个检查器，两个都会跑。** 它们的结果会合并进该名字的同一个条目，
保留最不健康的状态并拼接错误文本，因此一个健康的同名项无法掩盖一个失败的检查。
`GetCheckerNames()` 会去重，以匹配输出的键。

合成出来的结果——超时或 recover 的 panic——都带有真实的 `Timestamp`。

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

`DefaultConfig()` 返回的是**对外**形态：`IncludeDetails` 与 `IncludeChecks`
均为 false，响应只包含整体状态，不含任何单项依赖信息。健康检查接口通常无需认证即可访问，
而单项检查详情会把内部主机名、数据库版本与错误信息暴露给任何调用方。

```go
health.NewAggregator(health.DefaultConfig())         // 仅状态
health.NewAggregator(health.DefaultInternalConfig()) // 详情 + 单项结果
```

已置于认证之后、或仅集群内可达的接口，可使用 `DefaultInternalConfig()`，
也可单独设置 `IncludeDetails` / `IncludeChecks`。

## 项目结构

```
health-kit/
├── checker.go         # 检查器接口、结果类型及 JSON 序列化
├── config.go          # 配置，支持 IP 白名单
├── probes.go          # 内置探针（Redis、HTTP、DB、自定义、禁用）
├── aggregator.go      # 多探针聚合，支持并行执行
├── handler.go         # net/http 处理器 + 框架无关的 Decide 核心
├── fiberadapter/      # Fiber v3 适配器（只有导入它的二进制才会链接 Fiber）
└── *_test.go          # 完整测试
```

## 集成示例

### Herald（OTP 服务）

```go
package main

import (
    "github.com/gofiber/fiber/v3"
    health "github.com/soulteary/health-kit/v2"
    "github.com/soulteary/health-kit/v2/fiberadapter"
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

### 详细响应（`DefaultInternalConfig()`，或 `WithDetails(true).WithChecks(true)`）

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

### 简单响应（`DefaultConfig()` —— 默认形态）

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

- **Go 1.27+**（`go.mod` 声明 `go 1.27.0`）
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

## 升级说明（v2.3.0）

仅升级依赖。没有删除任何 API，调用方无需改代码。

- 测试用 Redis 为 `miniredis` v2.39.0（此前 v2.36.1）。
- SQLite 驱动为 `modernc.org/sqlite` v1.58.0（此前 v1.44.3）。

## 升级说明（v2.2.0）

**默认响应形态变了。** `DefaultConfig()` 不再包含逐项检查细节。

- **`IncludeDetails` 和 `IncludeChecks` 现在默认为 `false`。** 它们此前是开启的，
  且没有 `IPWhitelist`，而内置探针会把 `err.Error()` 原样塞进结果——数据库 DSN、
  内网主机名、文件系统路径——放在一个通常没有认证的端点上。**如果你的监控在解析
  `checks` 映射，请改用 `DefaultInternalConfig()`**（或调用
  `.WithDetails(true).WithChecks(true)`），并把该端点放到认证之后或仅集群内可达。
- **`Config.Timeout` 现在强制生效。** `Check` 此前构造了一个带超时的 context、
  交给每个检查器，然后就 `wg.Wait()`——没有任何地方查看这个 context，于是聚合一直
  阻塞到最慢的检查器返回，不管那要多久。现在超时的检查器被记为 unhealthy，聚合按时
  返回。此前会让端点挂住的慢依赖，现在会让它报告 `unhealthy`。
- **panic 的检查器会被 recover**，不再致命。如果你原本依赖探针 panic 来让 Pod 崩溃
  重启，这个行为没有了——该检查报告 unhealthy，`ReadinessHandler` 返回 503。
- **同名注册的两个检查器都会运行。** 第二个此前会在结果映射里静默替换第一个，于是
  健康的同名项可以掩盖失败的检查。现在结果会合并，保留最不健康的状态。
- **已完成的健康检查不再被报成超时。** 结果此前按 `Checker.Name()` 建键、却按
  `CheckResult.Name` 清除，于是返回了不同——或空——`Name` 的检查器会让它注册的名字
  一直处于 pending，端点就为一个其实已成功的检查返回 503。现在结果随它注册时的名字
  一起传递，这也是 `CriticalChecks` 匹配的对象和输出的键。
- **合成结果带有真实时间戳。** 超时和 recover 的 panic 结果此前把 `Timestamp` 留在
  零值，于是输出里出现 `0001-01-01T00:00:00Z`——而这正是运维最需要看的那些失败。
- **要求里写的是 Go 1.26**；`go.mod` 需要 `1.27.0`。

## 贡献

1. Fork 本仓库
2. 创建功能分支 (`git checkout -b feature/amazing-feature`)
3. 提交更改 (`git commit -m 'Add some amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing-feature`)
5. 提交 Pull Request

## 许可证

详见 [LICENSE](LICENSE) 文件。
