package redisprobe

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	health "github.com/soulteary/health-kit/v4"
)

// The probe accepts every go-redis client shape, not just a standalone one --
// a cluster or Sentinel deployment is exactly where a health probe earns its
// keep. Compile-time only; nothing here dials.
var (
	_ Pinger = (*redis.Client)(nil)
	_ Pinger = (*redis.ClusterClient)(nil)
	_ Pinger = (*redis.Ring)(nil)
	_ Pinger = (redis.UniversalClient)(nil)
)

// A Checker is usable wherever the root package wants a Checker.
var _ health.Checker = (*Checker)(nil)

// stubPinger is a value, not a pointer, so it also covers the branch of isNil
// that has nothing that could be nil. Being an interface, Pinger admits it.
type stubPinger struct{ err error }

func (p stubPinger) Ping(ctx context.Context) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(ctx, "ping")
	if p.err != nil {
		cmd.SetErr(p.err)
	} else {
		cmd.SetVal("PONG")
	}
	return cmd
}

func TestChecker(t *testing.T) {
	t.Run("nil client", func(t *testing.T) {
		checker := New(nil)
		result := checker.Check(context.Background())

		assert.Equal(t, "redis", result.Name)
		assert.Equal(t, health.StatusUnhealthy, result.Status)
		assert.Contains(t, result.Error, "nil")
	})

	t.Run("typed nil client", func(t *testing.T) {
		// Pinger is an interface, so this is a non-nil interface holding a
		// nil pointer. Pinging it panics; the probe must report instead.
		var client *redis.Client
		checker := New(client)

		require.NotPanics(t, func() {
			result := checker.Check(context.Background())
			assert.Equal(t, health.StatusUnhealthy, result.Status)
			assert.Contains(t, result.Error, "nil")
		})
	})

	t.Run("custom name", func(t *testing.T) {
		checker := NewWithName("session-redis", nil)
		assert.Equal(t, "session-redis", checker.Name())
	})

	t.Run("with timeout", func(t *testing.T) {
		checker := New(nil).WithTimeout(10 * time.Second)
		assert.Equal(t, 10*time.Second, checker.timeout)
	})

	t.Run("default timeout", func(t *testing.T) {
		assert.Equal(t, DefaultTimeout, New(nil).timeout)
	})

	t.Run("Name method", func(t *testing.T) {
		checker := New(nil)
		assert.Equal(t, "redis", checker.Name())
	})

	t.Run("non-pointer client", func(t *testing.T) {
		result := New(stubPinger{}).Check(context.Background())

		assert.Equal(t, health.StatusHealthy, result.Status)
		assert.Empty(t, result.Error)
	})

	t.Run("non-pointer client reporting a failure", func(t *testing.T) {
		result := New(stubPinger{err: errors.New("connection refused")}).
			Check(context.Background())

		assert.Equal(t, health.StatusUnhealthy, result.Status)
		assert.Contains(t, result.Error, "connection refused")
	})

	t.Run("successful check with miniredis", func(t *testing.T) {
		mr, err := miniredis.Run()
		require.NoError(t, err)
		defer mr.Close()

		client := redis.NewClient(&redis.Options{
			Addr: mr.Addr(),
		})
		defer func() { _ = client.Close() }()

		checker := New(client)
		result := checker.Check(context.Background())

		assert.Equal(t, health.StatusHealthy, result.Status)
		assert.Empty(t, result.Error)
		assert.Greater(t, result.Latency, time.Duration(0))
	})

	t.Run("failed check with closed redis", func(t *testing.T) {
		mr, err := miniredis.Run()
		require.NoError(t, err)

		client := redis.NewClient(&redis.Options{
			Addr: mr.Addr(),
		})
		defer func() { _ = client.Close() }()

		// Close miniredis to simulate failure
		mr.Close()

		checker := New(client).WithTimeout(100 * time.Millisecond)
		result := checker.Check(context.Background())

		assert.Equal(t, health.StatusUnhealthy, result.Status)
		assert.NotEmpty(t, result.Error)
	})
}

// A probe is only useful once an aggregator runs it, so cover that seam too.
func TestCheckerInAggregator(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	aggregator := health.NewAggregator(health.DefaultInternalConfig().WithServiceName("svc"))
	aggregator.AddChecker(NewWithName("session-redis", client))

	result := aggregator.Check(context.Background())

	assert.Equal(t, health.StatusHealthy, result.Status)
	require.Contains(t, result.Checks, "session-redis")
	assert.Equal(t, health.StatusHealthy, result.Checks["session-redis"].Status)
}
