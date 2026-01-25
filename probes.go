package health

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisChecker checks Redis connectivity
type RedisChecker struct {
	name    string
	client  *redis.Client
	timeout time.Duration
}

// NewRedisChecker creates a new Redis health checker
func NewRedisChecker(client *redis.Client) *RedisChecker {
	return &RedisChecker{
		name:    "redis",
		client:  client,
		timeout: 2 * time.Second,
	}
}

// NewRedisCheckerWithName creates a new Redis health checker with custom name
func NewRedisCheckerWithName(name string, client *redis.Client) *RedisChecker {
	return &RedisChecker{
		name:    name,
		client:  client,
		timeout: 2 * time.Second,
	}
}

// WithTimeout sets the timeout for Redis checks
func (c *RedisChecker) WithTimeout(timeout time.Duration) *RedisChecker {
	c.timeout = timeout
	return c
}

// Name returns the checker name
func (c *RedisChecker) Name() string {
	return c.name
}

// Check performs the Redis health check
func (c *RedisChecker) Check(ctx context.Context) CheckResult {
	result := CheckResult{
		Name:      c.name,
		Timestamp: time.Now(),
	}

	if c.client == nil {
		result.Status = StatusUnhealthy
		result.Error = "redis client is nil"
		return result
	}

	// Create timeout context
	checkCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	start := time.Now()
	err := c.client.Ping(checkCtx).Err()
	result.Latency = time.Since(start)

	if err != nil {
		result.Status = StatusUnhealthy
		result.Error = err.Error()
	} else {
		result.Status = StatusHealthy
	}

	return result
}

// HTTPChecker checks HTTP endpoint availability
type HTTPChecker struct {
	name       string
	url        string
	method     string
	timeout    time.Duration
	client     *http.Client
	expectCode int
}

// NewHTTPChecker creates a new HTTP health checker
func NewHTTPChecker(name, url string) *HTTPChecker {
	return &HTTPChecker{
		name:       name,
		url:        url,
		method:     http.MethodGet,
		timeout:    5 * time.Second,
		client:     &http.Client{Timeout: 5 * time.Second},
		expectCode: http.StatusOK,
	}
}

// WithTimeout sets the timeout for HTTP checks
func (c *HTTPChecker) WithTimeout(timeout time.Duration) *HTTPChecker {
	c.timeout = timeout
	c.client.Timeout = timeout
	return c
}

// WithMethod sets the HTTP method
func (c *HTTPChecker) WithMethod(method string) *HTTPChecker {
	c.method = method
	return c
}

// WithExpectedCode sets the expected HTTP status code
func (c *HTTPChecker) WithExpectedCode(code int) *HTTPChecker {
	c.expectCode = code
	return c
}

// WithClient sets a custom HTTP client
func (c *HTTPChecker) WithClient(client *http.Client) *HTTPChecker {
	c.client = client
	return c
}

// Name returns the checker name
func (c *HTTPChecker) Name() string {
	return c.name
}

// Check performs the HTTP health check
func (c *HTTPChecker) Check(ctx context.Context) CheckResult {
	result := CheckResult{
		Name:      c.name,
		Timestamp: time.Now(),
	}

	// Create request with timeout
	checkCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(checkCtx, c.method, c.url, nil)
	if err != nil {
		result.Status = StatusUnhealthy
		result.Error = fmt.Sprintf("failed to create request: %v", err)
		return result
	}

	start := time.Now()
	resp, err := c.client.Do(req)
	result.Latency = time.Since(start)

	if err != nil {
		result.Status = StatusUnhealthy
		result.Error = err.Error()
		return result
	}
	defer func() { _ = resp.Body.Close() }()

	// Drain and close body
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != c.expectCode {
		result.Status = StatusUnhealthy
		result.Error = fmt.Sprintf("unexpected status code: %d (expected %d)", resp.StatusCode, c.expectCode)
	} else {
		result.Status = StatusHealthy
	}

	result.Metadata = map[string]any{
		"status_code": resp.StatusCode,
	}

	return result
}

// DBChecker checks database connectivity
type DBChecker struct {
	name    string
	db      *sql.DB
	timeout time.Duration
}

// NewDBChecker creates a new database health checker
func NewDBChecker(db *sql.DB) *DBChecker {
	return &DBChecker{
		name:    "database",
		db:      db,
		timeout: 5 * time.Second,
	}
}

// NewDBCheckerWithName creates a new database health checker with custom name
func NewDBCheckerWithName(name string, db *sql.DB) *DBChecker {
	return &DBChecker{
		name:    name,
		db:      db,
		timeout: 5 * time.Second,
	}
}

// WithTimeout sets the timeout for database checks
func (c *DBChecker) WithTimeout(timeout time.Duration) *DBChecker {
	c.timeout = timeout
	return c
}

// Name returns the checker name
func (c *DBChecker) Name() string {
	return c.name
}

// Check performs the database health check
func (c *DBChecker) Check(ctx context.Context) CheckResult {
	result := CheckResult{
		Name:      c.name,
		Timestamp: time.Now(),
	}

	if c.db == nil {
		result.Status = StatusUnhealthy
		result.Error = "database connection is nil"
		return result
	}

	// Create timeout context
	checkCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	start := time.Now()
	err := c.db.PingContext(checkCtx)
	result.Latency = time.Since(start)

	if err != nil {
		result.Status = StatusUnhealthy
		result.Error = err.Error()
	} else {
		result.Status = StatusHealthy

		// Add connection pool stats if available
		stats := c.db.Stats()
		result.Metadata = map[string]any{
			"open_connections": stats.OpenConnections,
			"in_use":           stats.InUse,
			"idle":             stats.Idle,
		}
	}

	return result
}

// CustomChecker allows creating health checkers from functions
type CustomChecker struct {
	name     string
	checkFn  func(ctx context.Context) error
	timeout  time.Duration
	metadata map[string]any
}

// NewCustomChecker creates a new custom health checker
func NewCustomChecker(name string, checkFn func(ctx context.Context) error) *CustomChecker {
	return &CustomChecker{
		name:    name,
		checkFn: checkFn,
		timeout: 5 * time.Second,
	}
}

// WithTimeout sets the timeout for custom checks
func (c *CustomChecker) WithTimeout(timeout time.Duration) *CustomChecker {
	c.timeout = timeout
	return c
}

// WithMetadata sets static metadata for the check result
func (c *CustomChecker) WithMetadata(metadata map[string]any) *CustomChecker {
	c.metadata = metadata
	return c
}

// Name returns the checker name
func (c *CustomChecker) Name() string {
	return c.name
}

// Check performs the custom health check
func (c *CustomChecker) Check(ctx context.Context) CheckResult {
	result := CheckResult{
		Name:      c.name,
		Timestamp: time.Now(),
		Metadata:  c.metadata,
	}

	if c.checkFn == nil {
		result.Status = StatusUnknown
		result.Error = "check function is nil"
		return result
	}

	// Create timeout context
	checkCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	start := time.Now()
	err := c.checkFn(checkCtx)
	result.Latency = time.Since(start)

	if err != nil {
		result.Status = StatusUnhealthy
		result.Error = err.Error()
	} else {
		result.Status = StatusHealthy
	}

	return result
}

// DisabledChecker always returns disabled status
// Useful for optional dependencies that are not configured
type DisabledChecker struct {
	name    string
	message string
}

// NewDisabledChecker creates a new disabled checker
func NewDisabledChecker(name string) *DisabledChecker {
	return &DisabledChecker{
		name:    name,
		message: "component is disabled",
	}
}

// WithMessage sets the message for the disabled status
func (c *DisabledChecker) WithMessage(message string) *DisabledChecker {
	c.message = message
	return c
}

// Name returns the checker name
func (c *DisabledChecker) Name() string {
	return c.name
}

// Check returns a disabled status
func (c *DisabledChecker) Check(_ context.Context) CheckResult {
	return CheckResult{
		Name:      c.name,
		Status:    StatusDisabled,
		Message:   c.message,
		Timestamp: time.Now(),
	}
}
