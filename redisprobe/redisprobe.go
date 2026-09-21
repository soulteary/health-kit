// Package redisprobe checks Redis connectivity for health-kit.
//
// It lives in its own package so that importing the root package does not drag
// go-redis -- and with it cespare/xxhash and go.uber.org/atomic -- into
// binaries that never talk to Redis. A service backed by Postgres, MySQL or
// nothing at all pays nothing for Redis support existing; only importing this
// package links it in.
//
// The probe is a translation layer and nothing more: what a failure means,
// which checks are critical and how a status maps to an HTTP code all live in
// the root package.
//
//	agg := health.NewAggregator(health.DefaultConfig().WithServiceName("myservice"))
//	agg.AddChecker(redisprobe.New(redisClient))
package redisprobe

import (
	"context"
	"reflect"
	"time"

	"github.com/redis/go-redis/v9"

	health "github.com/soulteary/health-kit/v4"
)

// DefaultTimeout bounds a single PING when [Checker.WithTimeout] is not used.
const DefaultTimeout = 2 * time.Second

// Pinger is the part of a go-redis client this probe uses. *redis.Client,
// *redis.ClusterClient, *redis.Ring and redis.UniversalClient all satisfy it,
// so a probe written against it works the same on a standalone server, a
// cluster and a Sentinel failover setup.
type Pinger interface {
	Ping(ctx context.Context) *redis.StatusCmd
}

// Checker reports whether a Redis server answers PING. It implements
// health.Checker.
type Checker struct {
	name    string
	client  Pinger
	timeout time.Duration
}

// New creates a Redis health checker named "redis".
func New(client Pinger) *Checker {
	return NewWithName("redis", client)
}

// NewWithName creates a Redis health checker with a custom name. Use it when a
// service talks to more than one Redis, so each shows up under its own key in
// the aggregated result.
func NewWithName(name string, client Pinger) *Checker {
	return &Checker{
		name:    name,
		client:  client,
		timeout: DefaultTimeout,
	}
}

// WithTimeout sets the timeout for Redis checks.
func (c *Checker) WithTimeout(timeout time.Duration) *Checker {
	c.timeout = timeout
	return c
}

// Name returns the checker name.
func (c *Checker) Name() string {
	return c.name
}

// Check performs the Redis health check.
func (c *Checker) Check(ctx context.Context) health.CheckResult {
	result := health.CheckResult{
		Name:      c.name,
		Timestamp: time.Now(),
	}

	if isNil(c.client) {
		result.Status = health.StatusUnhealthy
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
		result.Status = health.StatusUnhealthy
		result.Error = err.Error()
	} else {
		result.Status = health.StatusHealthy
	}

	return result
}

// isNil reports whether there is no client to ping. Pinger is an interface, so
// a plain p == nil misses the case that actually reaches here -- a nil
// *redis.Client stored in it, which a caller gets from an unassigned field or
// a constructor that returned early. Pinging that panics, and a health probe
// that takes the process down is worse than the outage it was meant to report.
func isNil(p Pinger) bool {
	if p == nil {
		return true
	}
	v := reflect.ValueOf(p)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
