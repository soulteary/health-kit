# health-kit

[![Go Reference](https://pkg.go.dev/badge/github.com/soulteary/health-kit/v3.svg)](https://pkg.go.dev/github.com/soulteary/health-kit/v3)
[![Go Report Card](.github/goreportcard.svg)](.github/goreportcard-report.md)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![codecov](https://codecov.io/gh/soulteary/health-kit/graph/badge.svg)](https://codecov.io/gh/soulteary/health-kit)

[English](README.md)

统一的 Go 服务健康检查工具包：健康检查接口、探针实现、多探针聚合，以及 net/http 处理器。Fiber v3 支持位于 `fiberadapter` 子包 —— 自 v3.0.0 起根包就不再提供 Fiber 处理器。


> **v3.0.0 破坏性变更 —— 模块路径变更，且 Fiber 支持移入子包。**
>
> **第一步 —— 所有人，包括只用 net/http 的用户。** 模块路径现为
> `github.com/soulteary/health-kit/v3`：
>
> ```bash
> go get github.com/soulteary/health-kit/v3
> go mod edit -droprequire github.com/soulteary/health-kit/v2
> ```
>
> 然后改掉源码里的 import 路径。升大版本号是 Go 的 import compatibility rule
> 要求的：v3 删除了导出符号。留兼容 shim 这条路走不通 —— shim 会把 Fiber
> 重新导入回来，下面那些收益也就一并没了。
>
> **第二步 —— 仅 Fiber 用户。** Fiber handler 移至
> `github.com/soulteary/health-kit/v3/fiberadapter`，于是导入根包不再把
> Fiber（以及 fasthttp）链接进用不到它的二进制。对一个 net/http 服务来说，
> 这意味着**少链接 25 个包、少 8 个模块、二进制小 14%**。
>
> | 原来 | 现在 |
> |---|---|
> | `health.FiberHandler(agg)` | `fiberadapter.Handler(agg)` |
> | `health.FiberLivenessHandler(name)` | `fiberadapter.LivenessHandler(name)` |
> | `health.FiberReadinessHandler(agg)` | `fiberadapter.ReadinessHandler(agg)` |
> | `health.SimpleFiberHandler(name)` | `fiberadapter.SimpleHandler(name)` |
>
> 除 import 路径外，net/http 一侧的 API 没有变化：`health.Handler`、
> `LivenessHandler`、`ReadinessHandler`、`SimpleHandler` 的签名和行为都不变。

## 特性

- **检查器接口**：所有探针的统一健康检查接口
- **内置探针**：Redis、HTTP、数据库和自定义探针
- **并行聚合**：并行运行多个健康检查并聚合结果
- **HTTP 处理器**：标准库处理器，以及位于独立子包中的 Fiber 适配器
- **框架无关内核**：`Decide` 与 `ClientIPSource` 均已导出，为 Echo、Gin、chi
  写适配器约二十行，且共用同一份可信代理规则
- **Kubernetes 支持**：专用的存活和就绪探针处理器
- **IP 白名单**：限制特定 IP/CIDR 访问健康检查端点
- **关键检查**：区分关键和非关键依赖
- **延迟追踪**：测量和报告健康检查延迟

## 安装

```bash
go get github.com/soulteary/health-kit/v3
```

根包不依赖任何 Web 框架。Fiber 支持位于子包中，只有导入它才会链接 Fiber（以及 fasthttp）：

```bash
go get github.com/soulteary/health-kit/v3/fiberadapter
```

Fiber Handler 基于 Fiber v3。仍使用 Fiber v2 的应用应继续使用 health-kit v1；net/http Handler 与探针 API 的行为保持不变。

## 使用

### 基础健康检查

```go
import (
    health "github.com/soulteary/health-kit/v3"
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
    health "github.com/soulteary/health-kit/v3"
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
    health "github.com/soulteary/health-kit/v3"
    "github.com/soulteary/health-kit/v3/fiberadapter"
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

### 为其他框架编写适配器（Echo、Gin、chi……）

本仓库没有 Echo 或 Gin 适配器，你也不需要等：整个端点判定逻辑与框架无关，
并且已经导出。写一个适配器大约二十行。

核心是两样东西。`health.Decide` 负责应用 IP 白名单、执行检查，并把结果归约
成一个状态码和一个响应体：

```go
type Decision struct {
    Forbidden  bool // 客户端 IP 不在白名单内
    StatusCode int  // 恒有值，被拒绝时也是
    Body       any  // 按 JSON 序列化
}

func Decide(ctx context.Context, aggregator *Aggregator, src ClientIPSource) Decision
```

`health.ClientIPSource` 是你唯一需要实现的东西 —— 解析客户端 IP 所需的最小
请求视图：

```go
type ClientIPSource interface {
    RemoteIP() net.IP            // 对端地址，取不到时为 nil
    Header(name string) string   // 请求头，缺失时为 ""
}
```

可信代理规则本身 —— 什么时候可以相信 `X-Forwarded-For` 和 `X-Real-IP`、以及
两者的优先级 —— 位于 `Config.ClientIP`，由所有适配器共用。这是有意为之：
一条在不同框架间各说各话的规则就是一次 IP 白名单绕过，而不是无关紧要的差异。
**不要重新实现它。**

一个完整的 Echo 适配器：

```go
type echoSource struct{ c echo.Context }

func (s echoSource) RemoteIP() net.IP {
    host, _, err := net.SplitHostPort(s.c.Request().RemoteAddr)
    if err != nil {
        return net.ParseIP(s.c.Request().RemoteAddr)
    }
    return net.ParseIP(host)
}

func (s echoSource) Header(name string) string { return s.c.Request().Header.Get(name) }

func Handler(aggregator *health.Aggregator) echo.HandlerFunc {
    return func(c echo.Context) error {
        d := health.Decide(c.Request().Context(), aggregator, echoSource{c: c})
        if d.Forbidden {
            return c.JSON(http.StatusForbidden, map[string]string{"error": "Forbidden"})
        }
        return c.JSON(d.StatusCode, d.Body)
    }
}
```

注意 `Decide` **不**负责渲染 403 响应体,这由各适配器自己决定。内置的两个适配器
在这一点上历史遗留地不一致(`health.Handler` 返回 `text/plain`,
`fiberadapter.Handler` 返回 JSON),强行统一会让某一边的行为悄悄改变。
按你自己 API 的风格选一种即可。

如果你要写适配器,最值得从 `fiberadapter` 抄的一个测试是跨框架 parity 表:
把同一组请求头分别喂给你的 `ClientIPSource` 和 `health.RequestSource`,
断言两者解析出相同的客户端 IP。

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
    health "github.com/soulteary/health-kit/v3"
    "github.com/soulteary/health-kit/v3/fiberadapter"
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
    health "github.com/soulteary/health-kit/v3"
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
    health "github.com/soulteary/health-kit/v3"
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

此处为节选：顶层的 `timestamp`、`total_latency_ms` 以及每个探针的 `timestamp`
始终存在。

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

`HTTPStatusCode` 把状态映射为状态码：

| 健康状态 | `HTTPStatusCode` | 端点会报告吗？ |
|---------|------------------|----------------|
| `ok` | 200 OK | 会 |
| `degraded` | 200 OK | 会 |
| `unhealthy` | 503 Service Unavailable | 会 |
| `disabled` | 503 Service Unavailable | 不会 |
| `unknown` | 503 Service Unavailable | 不会 |

`disabled` 和 `unknown` 是**单个探针**的状态。`Aggregator` 只会归约出前三种，
所以端点不会因为它们返回 503：被禁用的探针仍然出现在 `checks` 里，只是在归约
整体状态时被跳过。后两行只在你自己拿单个探针的状态去调 `HTTPStatusCode` 时才
有意义 —— 前三种之外的一切都映射为 503。

## 要求

- **Go 1.27+**（`go.mod` 声明 `go 1.27.0`）
- github.com/gofiber/fiber/v3 v3.5.0+ —— **仅当**你导入 `fiberadapter` 时需要，
  根包不会引入它
- github.com/redis/go-redis/v9 v9.22.0+（用于 Redis 探针）
- modernc.org/sqlite v1.59.0+ 与 github.com/alicebob/miniredis/v2 v2.39.0+ 为
  仅测试依赖

## 测试覆盖率

运行测试：

```bash
go test ./... -v

# 带覆盖率 —— CI 实际执行的命令
go test -race -coverprofile=coverage.out -covermode=atomic ./...
go tool cover -html=coverage.out -o coverage.html
go tool cover -func=coverage.out
```

两个包均为 **100% 语句覆盖**。注意 `fiberadapter` 的测试是*外部*测试包
（`package fiberadapter_test`）：它只针对导出 API 编译，以此保证这套 API
确实够外部适配器使用。

## 升级说明（v3.0.0）

**破坏性变更：模块路径现为 `github.com/soulteary/health-kit/v3`，且 Fiber
Handler 移入 `fiberadapter` 子包。** 两步迁移方式见本文件顶部的说明。其余改动
均为增量。

- **为适配器作者新导出**：`Decide`、`Decision`、`ClientIPSource`、
  `RequestSource` 以及 `SimpleResponse`（原为未导出的 `simpleResponse`）。
  详见上文*为其他框架编写适配器*。
- **可信代理规则只剩一份实现。** `getClientIPFromRequest` 与
  `getClientIPFromFiber` 原本是同一条规则的两份拷贝，差别只在于对端地址和
  请求头从哪里取。现已合并为 `Config.ClientIP`，藏在一个两方法接口之后。
  行为没有变化，但一条能自相矛盾的规则就是一次白名单绕过，所以值得知道
  现在只有一份了。
- **`Decision.StatusCode` 在 `Forbidden` 为 true 时也有值。** 只有你自己写
  适配器、并且无条件写入状态码时才会受影响。
- **两个 403 响应体依然没有统一** —— `health.Handler` 返回 `text/plain`，
  `fiberadapter.Handler` 返回 JSON，一直如此。现在两者都有测试钉住，
  不会再悄悄漂移。

同一版本内的依赖升级：

- `modernc.org/sqlite` v1.59.0（此前 v1.58.0）、`modernc.org/libc` v1.77.0、
  `github.com/dustin/go-humanize` v1.1.0、`github.com/gofiber/schema` v1.8.7、
  `github.com/gofiber/utils/v2` v2.5.2、`github.com/molecule-man/go-brrr`
  v1.1.1、`go.uber.org/atomic` v1.12.0。
- CI actions：`actions/checkout`、`actions/setup-go`、
  `actions/upload-artifact` 升至 v7，`codecov/codecov-action` 升至 v7，
  `soulteary/goreportcard-action` 升至 v1.1.2。

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

## 变更日志

完整发布历史见 [CHANGELOG.md](CHANGELOG.md)。上文的*升级说明*各节对破坏性
版本有更详细的描述。

## 安全

`DefaultConfig()` 有意不输出探针细节，转发头也只在对端是已配置的可信代理时
才被采信。[SECURITY.md](SECURITY.md) 解释了这两点，以及如何上报安全问题 ——
请不要为安全问题开公开 issue。

## 贡献

1. Fork 本仓库
2. 创建功能分支 (`git checkout -b feature/amazing-feature`)
3. 提交更改 (`git commit -m 'Add some amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing-feature`)
5. 提交 Pull Request

## 许可证

详见 [LICENSE](LICENSE) 文件。
