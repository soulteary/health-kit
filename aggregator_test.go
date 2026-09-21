package health

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockChecker is a simple mock for testing
type mockChecker struct {
	name   string
	status Status
	delay  time.Duration
	err    string
}

func (m *mockChecker) Name() string {
	return m.name
}

func (m *mockChecker) Check(ctx context.Context) CheckResult {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return CheckResult{
				Name:      m.name,
				Status:    StatusUnhealthy,
				Error:     "context cancelled",
				Timestamp: time.Now(),
			}
		}
	}

	return CheckResult{
		Name:      m.name,
		Status:    m.status,
		Error:     m.err,
		Latency:   m.delay,
		Timestamp: time.Now(),
	}
}

func TestNewAggregator(t *testing.T) {
	config := DefaultConfig().WithServiceName("test-service")
	aggregator := NewAggregator(config)

	assert.NotNil(t, aggregator)
	assert.Equal(t, "test-service", aggregator.Config().ServiceName)
	assert.Empty(t, aggregator.GetCheckerNames())
}

func TestAggregator_AddChecker(t *testing.T) {
	aggregator := NewAggregator(DefaultConfig())

	checker1 := &mockChecker{name: "redis", status: StatusHealthy}
	checker2 := &mockChecker{name: "database", status: StatusHealthy}

	aggregator.AddChecker(checker1)
	assert.Len(t, aggregator.GetCheckerNames(), 1)

	aggregator.AddChecker(checker2)
	assert.Len(t, aggregator.GetCheckerNames(), 2)
}

func TestAggregator_AddCheckers(t *testing.T) {
	aggregator := NewAggregator(DefaultConfig())

	checker1 := &mockChecker{name: "redis", status: StatusHealthy}
	checker2 := &mockChecker{name: "database", status: StatusHealthy}
	checker3 := &mockChecker{name: "cache", status: StatusHealthy}

	aggregator.AddCheckers(checker1, checker2, checker3)
	assert.Len(t, aggregator.GetCheckerNames(), 3)
}

func TestAggregator_RemoveChecker(t *testing.T) {
	aggregator := NewAggregator(DefaultConfig())

	checker1 := &mockChecker{name: "redis", status: StatusHealthy}
	checker2 := &mockChecker{name: "database", status: StatusHealthy}

	aggregator.AddCheckers(checker1, checker2)
	assert.Len(t, aggregator.GetCheckerNames(), 2)

	aggregator.RemoveChecker("redis")
	names := aggregator.GetCheckerNames()
	assert.Len(t, names, 1)
	assert.Equal(t, "database", names[0])

	// 移除不存在的名称，列表不变
	aggregator.RemoveChecker("nonexistent")
	names = aggregator.GetCheckerNames()
	assert.Len(t, names, 1)
	assert.Equal(t, "database", names[0])
}

func TestAggregator_Check(t *testing.T) {
	t.Run("all healthy", func(t *testing.T) {
		aggregator := NewAggregator(DefaultConfig().WithServiceName("test"))
		aggregator.AddCheckers(
			&mockChecker{name: "redis", status: StatusHealthy},
			&mockChecker{name: "database", status: StatusHealthy},
		)

		result := aggregator.Check(context.Background())

		assert.Equal(t, StatusHealthy, result.Status)
		assert.Equal(t, "test", result.Service)
		assert.Len(t, result.Checks, 2)
	})

	t.Run("one critical unhealthy", func(t *testing.T) {
		aggregator := NewAggregator(DefaultConfig().WithServiceName("test"))
		aggregator.AddCheckers(
			&mockChecker{name: "redis", status: StatusUnhealthy, err: "connection failed"},
			&mockChecker{name: "database", status: StatusHealthy},
		)

		result := aggregator.Check(context.Background())

		assert.Equal(t, StatusUnhealthy, result.Status)
		assert.Len(t, result.Checks, 2)
	})

	t.Run("non-critical unhealthy results in degraded", func(t *testing.T) {
		config := DefaultConfig().
			WithServiceName("test").
			WithCriticalChecks([]string{"database"})

		aggregator := NewAggregator(config)
		aggregator.AddCheckers(
			&mockChecker{name: "redis", status: StatusUnhealthy, err: "connection failed"},
			&mockChecker{name: "database", status: StatusHealthy},
		)

		result := aggregator.Check(context.Background())

		assert.Equal(t, StatusDegraded, result.Status)
	})

	t.Run("disabled checks are skipped", func(t *testing.T) {
		aggregator := NewAggregator(DefaultConfig().WithServiceName("test"))
		aggregator.AddCheckers(
			&mockChecker{name: "redis", status: StatusHealthy},
			NewDisabledChecker("optional-service"),
		)

		result := aggregator.Check(context.Background())

		assert.Equal(t, StatusHealthy, result.Status)
		assert.Len(t, result.Checks, 2)
		assert.Equal(t, StatusDisabled, result.Checks["optional-service"].Status)
	})

	t.Run("no checkers returns healthy", func(t *testing.T) {
		aggregator := NewAggregator(DefaultConfig().WithServiceName("test"))

		result := aggregator.Check(context.Background())

		assert.Equal(t, StatusHealthy, result.Status)
		assert.Empty(t, result.Checks)
	})

	t.Run("parallel execution", func(t *testing.T) {
		aggregator := NewAggregator(DefaultConfig().WithServiceName("test").WithTimeout(5 * time.Second))
		aggregator.AddCheckers(
			&mockChecker{name: "slow1", status: StatusHealthy, delay: 100 * time.Millisecond},
			&mockChecker{name: "slow2", status: StatusHealthy, delay: 100 * time.Millisecond},
			&mockChecker{name: "slow3", status: StatusHealthy, delay: 100 * time.Millisecond},
		)

		start := time.Now()
		result := aggregator.Check(context.Background())
		elapsed := time.Since(start)

		assert.Equal(t, StatusHealthy, result.Status)
		// Parallel execution should complete in ~100ms, not 300ms
		assert.Less(t, elapsed, 200*time.Millisecond)
	})

	t.Run("context cancelled during Check", func(t *testing.T) {
		aggregator := NewAggregator(DefaultConfig().WithServiceName("test").WithTimeout(5 * time.Second))
		aggregator.AddCheckers(
			&mockChecker{name: "slow", status: StatusHealthy, delay: 200 * time.Millisecond},
		)

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()

		result := aggregator.Check(ctx)
		// mockChecker 在 ctx.Done() 时返回 StatusUnhealthy
		assert.Equal(t, StatusUnhealthy, result.Status)
		assert.Len(t, result.Checks, 1)
	})
}

func TestAggregator_CheckSequential(t *testing.T) {
	t.Run("all healthy", func(t *testing.T) {
		aggregator := NewAggregator(DefaultConfig().WithServiceName("test"))
		aggregator.AddCheckers(
			&mockChecker{name: "redis", status: StatusHealthy},
			&mockChecker{name: "database", status: StatusHealthy},
		)

		result := aggregator.CheckSequential(context.Background())

		assert.Equal(t, StatusHealthy, result.Status)
		assert.Len(t, result.Checks, 2)
	})

	t.Run("sequential execution", func(t *testing.T) {
		aggregator := NewAggregator(DefaultConfig().WithServiceName("test").WithTimeout(5 * time.Second))
		aggregator.AddCheckers(
			&mockChecker{name: "slow1", status: StatusHealthy, delay: 50 * time.Millisecond},
			&mockChecker{name: "slow2", status: StatusHealthy, delay: 50 * time.Millisecond},
		)

		start := time.Now()
		result := aggregator.CheckSequential(context.Background())
		elapsed := time.Since(start)

		assert.Equal(t, StatusHealthy, result.Status)
		// Sequential execution should take ~100ms
		assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond)
	})

	t.Run("context cancellation", func(t *testing.T) {
		aggregator := NewAggregator(DefaultConfig().WithServiceName("test").WithTimeout(50 * time.Millisecond))
		aggregator.AddCheckers(
			&mockChecker{name: "slow", status: StatusHealthy, delay: 200 * time.Millisecond},
		)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		result := aggregator.CheckSequential(ctx)

		assert.Equal(t, StatusUnhealthy, result.Status)
	})

	t.Run("context cancelled before second check", func(t *testing.T) {
		aggregator := NewAggregator(DefaultConfig().WithServiceName("test").WithTimeout(500 * time.Millisecond))
		aggregator.AddCheckers(
			&mockChecker{name: "fast", status: StatusHealthy, delay: 10 * time.Millisecond},
			&mockChecker{name: "slow", status: StatusHealthy, delay: 200 * time.Millisecond},
		)

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
		defer cancel()

		result := aggregator.CheckSequential(ctx)

		// First check should complete, but second should be skipped due to context cancellation
		assert.Equal(t, StatusUnhealthy, result.Status)
	})

	t.Run("no checkers returns healthy", func(t *testing.T) {
		aggregator := NewAggregator(DefaultConfig().WithServiceName("test"))

		result := aggregator.CheckSequential(context.Background())

		assert.Equal(t, StatusHealthy, result.Status)
	})

	t.Run("disabled checks are skipped", func(t *testing.T) {
		aggregator := NewAggregator(DefaultConfig().WithServiceName("test"))
		aggregator.AddCheckers(
			&mockChecker{name: "redis", status: StatusHealthy},
			NewDisabledChecker("optional-service"),
		)

		result := aggregator.CheckSequential(context.Background())

		assert.Equal(t, StatusHealthy, result.Status)
		assert.Len(t, result.Checks, 2)
		assert.Equal(t, StatusDisabled, result.Checks["optional-service"].Status)
	})

	t.Run("non-critical unhealthy results in degraded", func(t *testing.T) {
		config := DefaultConfig().
			WithServiceName("test").
			WithCriticalChecks([]string{"database"})

		aggregator := NewAggregator(config)
		aggregator.AddCheckers(
			&mockChecker{name: "redis", status: StatusUnhealthy, err: "connection failed"},
			&mockChecker{name: "database", status: StatusHealthy},
		)

		result := aggregator.CheckSequential(context.Background())

		assert.Equal(t, StatusDegraded, result.Status)
	})

	t.Run("critical unhealthy results in unhealthy", func(t *testing.T) {
		config := DefaultConfig().
			WithServiceName("test").
			WithCriticalChecks([]string{"database"})

		aggregator := NewAggregator(config)
		aggregator.AddCheckers(
			&mockChecker{name: "redis", status: StatusHealthy},
			&mockChecker{name: "database", status: StatusUnhealthy, err: "connection failed"},
		)

		result := aggregator.CheckSequential(context.Background())

		assert.Equal(t, StatusUnhealthy, result.Status)
	})
}

func TestAggregator_SetConfig(t *testing.T) {
	aggregator := NewAggregator(DefaultConfig().WithServiceName("old"))
	require.Equal(t, "old", aggregator.Config().ServiceName)

	aggregator.SetConfig(DefaultConfig().WithServiceName("new"))
	assert.Equal(t, "new", aggregator.Config().ServiceName)
}

func TestAggregator_GetCheckerNames(t *testing.T) {
	aggregator := NewAggregator(DefaultConfig())
	aggregator.AddCheckers(
		&mockChecker{name: "redis", status: StatusHealthy},
		&mockChecker{name: "database", status: StatusHealthy},
		&mockChecker{name: "cache", status: StatusHealthy},
	)

	names := aggregator.GetCheckerNames()
	assert.Len(t, names, 3)
	assert.Contains(t, names, "redis")
	assert.Contains(t, names, "database")
	assert.Contains(t, names, "cache")
}

// statusSeverity 与 leastHealthy 是"同名 checker 合并"的核心：一个健康的
// checker 不能把同名的失败 checker 盖掉。此前只覆盖到 Healthy/Unhealthy
// 两个分支，错误合并与耗时取大完全没测。
func TestStatusSeverity(t *testing.T) {
	// 顺序即语义：Disabled 最轻，Unhealthy 最重，未知状态按 Degraded 处理。
	assert.Less(t, statusSeverity(StatusDisabled), statusSeverity(StatusHealthy))
	assert.Less(t, statusSeverity(StatusHealthy), statusSeverity(StatusDegraded))
	assert.Less(t, statusSeverity(StatusDegraded), statusSeverity(StatusUnhealthy))
	assert.Equal(t, statusSeverity(StatusDegraded), statusSeverity(StatusUnknown))
	assert.Equal(t, statusSeverity(StatusDegraded), statusSeverity(Status("something-new")))
}

func TestLeastHealthy(t *testing.T) {
	t.Run("keeps the worse status regardless of argument order", func(t *testing.T) {
		healthy := CheckResult{Name: "redis", Status: StatusHealthy}
		unhealthy := CheckResult{Name: "redis", Status: StatusUnhealthy, Error: "down"}

		assert.Equal(t, StatusUnhealthy, leastHealthy(healthy, unhealthy).Status)
		assert.Equal(t, StatusUnhealthy, leastHealthy(unhealthy, healthy).Status)
	})

	t.Run("adopts the other error when the worse one has none", func(t *testing.T) {
		worse := CheckResult{Name: "redis", Status: StatusUnhealthy}
		other := CheckResult{Name: "redis", Status: StatusDegraded, Error: "slow"}

		assert.Equal(t, "slow", leastHealthy(worse, other).Error)
	})

	t.Run("joins two distinct errors so neither disappears", func(t *testing.T) {
		worse := CheckResult{Name: "redis", Status: StatusUnhealthy, Error: "down"}
		other := CheckResult{Name: "redis", Status: StatusDegraded, Error: "slow"}

		assert.Equal(t, "down; slow", leastHealthy(worse, other).Error)
	})

	t.Run("does not duplicate an identical error", func(t *testing.T) {
		worse := CheckResult{Name: "redis", Status: StatusUnhealthy, Error: "down"}
		other := CheckResult{Name: "redis", Status: StatusDegraded, Error: "down"}

		assert.Equal(t, "down", leastHealthy(worse, other).Error)
	})

	t.Run("reports the larger latency", func(t *testing.T) {
		worse := CheckResult{Name: "redis", Status: StatusUnhealthy, Latency: 10 * time.Millisecond}
		other := CheckResult{Name: "redis", Status: StatusHealthy, Latency: 50 * time.Millisecond}

		assert.Equal(t, 50*time.Millisecond, leastHealthy(worse, other).Latency)
		assert.Equal(t, 50*time.Millisecond, leastHealthy(other, worse).Latency)
	})
}

func TestCheckSequentialNamesakes(t *testing.T) {
	t.Run("a healthy namesake cannot mask a failing one", func(t *testing.T) {
		aggregator := NewAggregator(DefaultInternalConfig().WithServiceName("test"))
		aggregator.AddChecker(&mockChecker{name: "redis", status: StatusHealthy})
		aggregator.AddChecker(&mockChecker{name: "redis", status: StatusUnhealthy, err: "down"})

		result := aggregator.CheckSequential(context.Background())

		require.Len(t, result.Checks, 1)
		assert.Equal(t, StatusUnhealthy, result.Checks["redis"].Status)
		assert.Contains(t, result.Checks["redis"].Error, "down")
		assert.Equal(t, StatusUnhealthy, result.Status)
	})

	t.Run("cancelled context short-circuits as unhealthy", func(t *testing.T) {
		aggregator := NewAggregator(DefaultInternalConfig().WithServiceName("test"))
		aggregator.AddChecker(&mockChecker{name: "redis", status: StatusHealthy})

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		result := aggregator.CheckSequential(ctx)

		assert.Equal(t, StatusUnhealthy, result.Status)
		assert.Empty(t, result.Checks, "取消后不应该再跑任何 checker")
	})
}
