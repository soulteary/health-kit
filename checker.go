// Package health provides a unified health check toolkit for Go services.
// It includes health check interfaces, probe implementations, multi-probe aggregation,
// and HTTP handlers compatible with both Fiber and net/http.
package health

import (
	"context"
	"encoding/json"
	"time"
)

// Status represents the health status of a component
type Status string

const (
	// StatusHealthy indicates the component is healthy
	StatusHealthy Status = "ok"
	// StatusUnhealthy indicates the component is unhealthy
	StatusUnhealthy Status = "unhealthy"
	// StatusDegraded indicates the component is partially healthy
	StatusDegraded Status = "degraded"
	// StatusDisabled indicates the component is disabled
	StatusDisabled Status = "disabled"
	// StatusUnknown indicates the component status cannot be determined
	StatusUnknown Status = "unknown"
)

// IsHealthy returns true if the status indicates healthy state
func (s Status) IsHealthy() bool {
	return s == StatusHealthy
}

// String returns the string representation of the status
func (s Status) String() string {
	return string(s)
}

// CheckResult represents the result of a single health check
type CheckResult struct {
	// Name is the identifier of the checked component
	Name string `json:"name"`
	// Status is the health status
	Status Status `json:"status"`
	// Latency is the time taken to perform the check
	Latency time.Duration `json:"-"`
	// Error contains error details if the check failed
	Error string `json:"error,omitempty"`
	// Message contains additional information
	Message string `json:"message,omitempty"`
	// Timestamp is when the check was performed
	Timestamp time.Time `json:"timestamp"`
	// Metadata contains additional check-specific data
	Metadata map[string]any `json:"metadata,omitempty"`
}

// MarshalJSON customizes JSON marshaling for CheckResult
func (r CheckResult) MarshalJSON() ([]byte, error) {
	type jsonResult struct {
		Name      string         `json:"name"`
		Status    Status         `json:"status"`
		LatencyMs int64          `json:"latency_ms"`
		Error     string         `json:"error,omitempty"`
		Message   string         `json:"message,omitempty"`
		Timestamp time.Time      `json:"timestamp"`
		Metadata  map[string]any `json:"metadata,omitempty"`
	}

	jr := jsonResult{
		Name:      r.Name,
		Status:    r.Status,
		LatencyMs: r.Latency.Milliseconds(),
		Error:     r.Error,
		Message:   r.Message,
		Timestamp: r.Timestamp,
		Metadata:  r.Metadata,
	}

	return json.Marshal(jr)
}

// Checker is the interface that health check probes must implement
type Checker interface {
	// Name returns the name of the checker
	Name() string
	// Check performs the health check and returns the result
	Check(ctx context.Context) CheckResult
}

// CheckerFunc is a function adapter for Checker interface
type CheckerFunc struct {
	name string
	fn   func(ctx context.Context) CheckResult
}

// NewCheckerFunc creates a new CheckerFunc with the given name and function
func NewCheckerFunc(name string, fn func(ctx context.Context) CheckResult) *CheckerFunc {
	return &CheckerFunc{
		name: name,
		fn:   fn,
	}
}

// Name returns the name of the checker
func (c *CheckerFunc) Name() string {
	return c.name
}

// Check performs the health check
func (c *CheckerFunc) Check(ctx context.Context) CheckResult {
	if c.fn == nil {
		return CheckResult{
			Name:      c.name,
			Status:    StatusUnknown,
			Timestamp: time.Now(),
			Error:     "check function is nil",
		}
	}
	return c.fn(ctx)
}

// AggregatedResult represents the combined result of multiple health checks
type AggregatedResult struct {
	// Status is the overall health status
	Status Status `json:"status"`
	// Service is the name of the service
	Service string `json:"service"`
	// Checks contains individual check results
	Checks map[string]CheckResult `json:"checks,omitempty"`
	// Timestamp is when the aggregation was performed
	Timestamp time.Time `json:"timestamp"`
	// TotalLatency is the total time taken for all checks
	TotalLatency time.Duration `json:"-"`
}

// MarshalJSON customizes JSON marshaling for AggregatedResult
func (r AggregatedResult) MarshalJSON() ([]byte, error) {
	type jsonCheckRes struct {
		Name      string         `json:"name"`
		Status    Status         `json:"status"`
		LatencyMs int64          `json:"latency_ms"`
		Error     string         `json:"error,omitempty"`
		Message   string         `json:"message,omitempty"`
		Timestamp time.Time      `json:"timestamp"`
		Metadata  map[string]any `json:"metadata,omitempty"`
	}

	type jsonResult struct {
		Status         Status                  `json:"status"`
		Service        string                  `json:"service"`
		Checks         map[string]jsonCheckRes `json:"checks,omitempty"`
		Timestamp      time.Time               `json:"timestamp"`
		TotalLatencyMs int64                   `json:"total_latency_ms"`
	}

	jr := jsonResult{
		Status:         r.Status,
		Service:        r.Service,
		Timestamp:      r.Timestamp,
		TotalLatencyMs: r.TotalLatency.Milliseconds(),
	}

	if len(r.Checks) > 0 {
		jr.Checks = make(map[string]jsonCheckRes)
		for k, v := range r.Checks {
			jr.Checks[k] = jsonCheckRes{
				Name:      v.Name,
				Status:    v.Status,
				LatencyMs: v.Latency.Milliseconds(),
				Error:     v.Error,
				Message:   v.Message,
				Timestamp: v.Timestamp,
				Metadata:  v.Metadata,
			}
		}
	}

	return json.Marshal(jr)
}

// IsHealthy returns true if the overall status is healthy
func (r AggregatedResult) IsHealthy() bool {
	return r.Status == StatusHealthy
}

// IsDegraded returns true if any check failed but service is still functional
func (r AggregatedResult) IsDegraded() bool {
	return r.Status == StatusDegraded
}
