package health

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatus(t *testing.T) {
	t.Run("IsHealthy", func(t *testing.T) {
		tests := []struct {
			status   Status
			expected bool
		}{
			{StatusHealthy, true},
			{StatusUnhealthy, false},
			{StatusDegraded, false},
			{StatusDisabled, false},
			{StatusUnknown, false},
		}

		for _, tt := range tests {
			t.Run(string(tt.status), func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.status.IsHealthy())
			})
		}
	})

	t.Run("String", func(t *testing.T) {
		assert.Equal(t, "ok", StatusHealthy.String())
		assert.Equal(t, "unhealthy", StatusUnhealthy.String())
		assert.Equal(t, "degraded", StatusDegraded.String())
		assert.Equal(t, "disabled", StatusDisabled.String())
		assert.Equal(t, "unknown", StatusUnknown.String())
	})
}

func TestCheckResult(t *testing.T) {
	t.Run("MarshalJSON", func(t *testing.T) {
		result := CheckResult{
			Name:      "test",
			Status:    StatusHealthy,
			Latency:   150 * time.Millisecond,
			Timestamp: time.Now(),
		}

		data, err := result.MarshalJSON()
		require.NoError(t, err)
		assert.Contains(t, string(data), `"latency_ms":150`)
		assert.Contains(t, string(data), `"status":"ok"`)
	})

	t.Run("MarshalJSON with error", func(t *testing.T) {
		result := CheckResult{
			Name:      "test",
			Status:    StatusUnhealthy,
			Latency:   50 * time.Millisecond,
			Error:     "connection failed",
			Timestamp: time.Now(),
		}

		data, err := result.MarshalJSON()
		require.NoError(t, err)
		assert.Contains(t, string(data), `"error":"connection failed"`)
	})

	t.Run("MarshalJSON with metadata", func(t *testing.T) {
		result := CheckResult{
			Name:      "test",
			Status:    StatusHealthy,
			Latency:   100 * time.Millisecond,
			Timestamp: time.Now(),
			Metadata: map[string]any{
				"version": "1.0.0",
			},
		}

		data, err := result.MarshalJSON()
		require.NoError(t, err)
		assert.Contains(t, string(data), `"metadata"`)
		assert.Contains(t, string(data), `"version":"1.0.0"`)
	})
}

func TestCheckerFunc(t *testing.T) {
	t.Run("basic usage", func(t *testing.T) {
		checker := NewCheckerFunc("test-func", func(ctx context.Context) CheckResult {
			return CheckResult{
				Name:      "test-func",
				Status:    StatusHealthy,
				Timestamp: time.Now(),
			}
		})

		assert.Equal(t, "test-func", checker.Name())

		result := checker.Check(context.Background())
		assert.Equal(t, StatusHealthy, result.Status)
		assert.Equal(t, "test-func", result.Name)
	})

	t.Run("nil function", func(t *testing.T) {
		checker := NewCheckerFunc("nil-func", nil)

		result := checker.Check(context.Background())
		assert.Equal(t, StatusUnknown, result.Status)
		assert.Contains(t, result.Error, "nil")
	})
}

func TestAggregatedResult(t *testing.T) {
	t.Run("IsHealthy", func(t *testing.T) {
		result := AggregatedResult{Status: StatusHealthy}
		assert.True(t, result.IsHealthy())

		result.Status = StatusUnhealthy
		assert.False(t, result.IsHealthy())
	})

	t.Run("IsDegraded", func(t *testing.T) {
		result := AggregatedResult{Status: StatusDegraded}
		assert.True(t, result.IsDegraded())

		result.Status = StatusHealthy
		assert.False(t, result.IsDegraded())
	})

	t.Run("MarshalJSON", func(t *testing.T) {
		result := AggregatedResult{
			Status:       StatusHealthy,
			Service:      "test-service",
			Timestamp:    time.Now(),
			TotalLatency: 200 * time.Millisecond,
			Checks: map[string]CheckResult{
				"redis": {
					Name:      "redis",
					Status:    StatusHealthy,
					Latency:   50 * time.Millisecond,
					Timestamp: time.Now(),
				},
			},
		}

		data, err := result.MarshalJSON()
		require.NoError(t, err)
		assert.Contains(t, string(data), `"total_latency_ms":200`)
		assert.Contains(t, string(data), `"service":"test-service"`)
		assert.Contains(t, string(data), `"checks"`)
	})

	t.Run("MarshalJSON without checks", func(t *testing.T) {
		result := AggregatedResult{
			Status:       StatusHealthy,
			Service:      "test-service",
			Timestamp:    time.Now(),
			TotalLatency: 100 * time.Millisecond,
		}

		data, err := result.MarshalJSON()
		require.NoError(t, err)
		assert.Contains(t, string(data), `"status":"ok"`)
	})
}
