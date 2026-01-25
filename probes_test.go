package health

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

func TestRedisChecker(t *testing.T) {
	t.Run("nil client", func(t *testing.T) {
		checker := NewRedisChecker(nil)
		result := checker.Check(context.Background())

		assert.Equal(t, "redis", result.Name)
		assert.Equal(t, StatusUnhealthy, result.Status)
		assert.Contains(t, result.Error, "nil")
	})

	t.Run("custom name", func(t *testing.T) {
		checker := NewRedisCheckerWithName("session-redis", nil)
		assert.Equal(t, "session-redis", checker.Name())
	})

	t.Run("with timeout", func(t *testing.T) {
		checker := NewRedisChecker(nil).WithTimeout(10 * time.Second)
		assert.Equal(t, 10*time.Second, checker.timeout)
	})

	t.Run("Name method", func(t *testing.T) {
		checker := NewRedisChecker(nil)
		assert.Equal(t, "redis", checker.Name())
	})

	t.Run("successful check with miniredis", func(t *testing.T) {
		mr, err := miniredis.Run()
		require.NoError(t, err)
		defer mr.Close()

		client := redis.NewClient(&redis.Options{
			Addr: mr.Addr(),
		})
		defer func() { _ = client.Close() }()

		checker := NewRedisChecker(client)
		result := checker.Check(context.Background())

		assert.Equal(t, StatusHealthy, result.Status)
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

		checker := NewRedisChecker(client).WithTimeout(100 * time.Millisecond)
		result := checker.Check(context.Background())

		assert.Equal(t, StatusUnhealthy, result.Status)
		assert.NotEmpty(t, result.Error)
	})
}

func TestHTTPChecker(t *testing.T) {
	t.Run("successful check", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		checker := NewHTTPChecker("external-api", server.URL)
		result := checker.Check(context.Background())

		assert.Equal(t, "external-api", result.Name)
		assert.Equal(t, StatusHealthy, result.Status)
		assert.Empty(t, result.Error)
		assert.NotNil(t, result.Metadata)
		assert.Equal(t, 200, result.Metadata["status_code"])
	})

	t.Run("Name method", func(t *testing.T) {
		checker := NewHTTPChecker("my-api", "http://example.com")
		assert.Equal(t, "my-api", checker.Name())
	})

	t.Run("unexpected status code", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()

		checker := NewHTTPChecker("external-api", server.URL)
		result := checker.Check(context.Background())

		assert.Equal(t, StatusUnhealthy, result.Status)
		assert.Contains(t, result.Error, "unexpected status code")
	})

	t.Run("custom expected code", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
		}))
		defer server.Close()

		checker := NewHTTPChecker("external-api", server.URL).WithExpectedCode(http.StatusCreated)
		result := checker.Check(context.Background())

		assert.Equal(t, StatusHealthy, result.Status)
	})

	t.Run("custom method", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
		}))
		defer server.Close()

		checker := NewHTTPChecker("external-api", server.URL).WithMethod(http.MethodHead)
		result := checker.Check(context.Background())

		assert.Equal(t, StatusHealthy, result.Status)
	})

	t.Run("connection error", func(t *testing.T) {
		checker := NewHTTPChecker("external-api", "http://localhost:59999").WithTimeout(100 * time.Millisecond)
		result := checker.Check(context.Background())

		assert.Equal(t, StatusUnhealthy, result.Status)
		assert.NotEmpty(t, result.Error)
	})

	t.Run("invalid URL", func(t *testing.T) {
		checker := NewHTTPChecker("external-api", "://invalid")
		result := checker.Check(context.Background())

		assert.Equal(t, StatusUnhealthy, result.Status)
		assert.Contains(t, result.Error, "failed to create request")
	})

	t.Run("with timeout", func(t *testing.T) {
		checker := NewHTTPChecker("external-api", "http://example.com").WithTimeout(1 * time.Second)
		assert.Equal(t, 1*time.Second, checker.timeout)
	})

	t.Run("with custom client", func(t *testing.T) {
		client := &http.Client{Timeout: 30 * time.Second}
		checker := NewHTTPChecker("external-api", "http://example.com").WithClient(client)
		assert.Equal(t, client, checker.client)
	})
}

func TestDBChecker(t *testing.T) {
	t.Run("nil database", func(t *testing.T) {
		checker := NewDBChecker(nil)
		result := checker.Check(context.Background())

		assert.Equal(t, "database", result.Name)
		assert.Equal(t, StatusUnhealthy, result.Status)
		assert.Contains(t, result.Error, "nil")
	})

	t.Run("custom name", func(t *testing.T) {
		checker := NewDBCheckerWithName("postgres", nil)
		assert.Equal(t, "postgres", checker.Name())
	})

	t.Run("with timeout", func(t *testing.T) {
		checker := NewDBChecker(nil).WithTimeout(10 * time.Second)
		assert.Equal(t, 10*time.Second, checker.timeout)
	})

	t.Run("Name method", func(t *testing.T) {
		checker := NewDBChecker(nil)
		assert.Equal(t, "database", checker.Name())
	})

	t.Run("successful check with sqlite", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		checker := NewDBChecker(db)
		result := checker.Check(context.Background())

		assert.Equal(t, StatusHealthy, result.Status)
		assert.Empty(t, result.Error)
		assert.Greater(t, result.Latency, time.Duration(0))
		assert.NotNil(t, result.Metadata)
		assert.Contains(t, result.Metadata, "open_connections")
	})

	t.Run("failed check with closed db", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		require.NoError(t, err)

		// Close the database to simulate failure
		_ = db.Close()

		checker := NewDBChecker(db).WithTimeout(100 * time.Millisecond)
		result := checker.Check(context.Background())

		assert.Equal(t, StatusUnhealthy, result.Status)
		assert.NotEmpty(t, result.Error)
	})
}

func TestCustomChecker(t *testing.T) {
	t.Run("successful check", func(t *testing.T) {
		checker := NewCustomChecker("custom", func(ctx context.Context) error {
			return nil
		})
		result := checker.Check(context.Background())

		assert.Equal(t, "custom", result.Name)
		assert.Equal(t, StatusHealthy, result.Status)
	})

	t.Run("Name method", func(t *testing.T) {
		checker := NewCustomChecker("my-custom-check", nil)
		assert.Equal(t, "my-custom-check", checker.Name())
	})

	t.Run("failed check", func(t *testing.T) {
		checker := NewCustomChecker("custom", func(ctx context.Context) error {
			return errors.New("custom error")
		})
		result := checker.Check(context.Background())

		assert.Equal(t, StatusUnhealthy, result.Status)
		assert.Equal(t, "custom error", result.Error)
	})

	t.Run("nil function", func(t *testing.T) {
		checker := NewCustomChecker("custom", nil)
		result := checker.Check(context.Background())

		assert.Equal(t, StatusUnknown, result.Status)
		assert.Contains(t, result.Error, "nil")
	})

	t.Run("with timeout", func(t *testing.T) {
		checker := NewCustomChecker("custom", nil).WithTimeout(10 * time.Second)
		assert.Equal(t, 10*time.Second, checker.timeout)
	})

	t.Run("with metadata", func(t *testing.T) {
		metadata := map[string]any{"version": "1.0.0"}
		checker := NewCustomChecker("custom", func(ctx context.Context) error {
			return nil
		}).WithMetadata(metadata)

		result := checker.Check(context.Background())
		assert.Equal(t, "1.0.0", result.Metadata["version"])
	})
}

func TestDisabledChecker(t *testing.T) {
	t.Run("basic usage", func(t *testing.T) {
		checker := NewDisabledChecker("redis")
		result := checker.Check(context.Background())

		assert.Equal(t, "redis", result.Name)
		assert.Equal(t, StatusDisabled, result.Status)
		assert.Equal(t, "component is disabled", result.Message)
	})

	t.Run("custom message", func(t *testing.T) {
		checker := NewDisabledChecker("redis").WithMessage("Redis is not configured")
		result := checker.Check(context.Background())

		assert.Equal(t, "Redis is not configured", result.Message)
	})

	t.Run("Name method", func(t *testing.T) {
		checker := NewDisabledChecker("optional-cache")
		assert.Equal(t, "optional-cache", checker.Name())
	})
}
