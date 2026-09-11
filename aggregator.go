package health

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Aggregator manages multiple health checkers and aggregates their results
type Aggregator struct {
	config   Config
	checkers []Checker
	mu       sync.RWMutex
}

// NewAggregator creates a new health check aggregator
func NewAggregator(config Config) *Aggregator {
	return &Aggregator{
		config:   config,
		checkers: make([]Checker, 0),
	}
}

// AddChecker adds a health checker to the aggregator
// Adding a second checker with a name already in use replaces the first: the
// results are keyed by name, so the earlier one would otherwise be silently
// overwritten and a failing check could be masked by a healthy namesake.
func (a *Aggregator) AddChecker(checker Checker) *Aggregator {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.checkers = a.replaceByName(a.checkers, checker)
	return a
}

// replaceByName appends checker, replacing any existing entry with the same
// name in place so ordering stays stable.
func (a *Aggregator) replaceByName(list []Checker, checker Checker) []Checker {
	for i, existing := range list {
		if existing.Name() == checker.Name() {
			list[i] = checker
			return list
		}
	}
	return append(list, checker)
}

// AddCheckers adds multiple health checkers to the aggregator
func (a *Aggregator) AddCheckers(checkers ...Checker) *Aggregator {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, checker := range checkers {
		a.checkers = a.replaceByName(a.checkers, checker)
	}
	return a
}

// RemoveChecker removes a checker by name
func (a *Aggregator) RemoveChecker(name string) *Aggregator {
	a.mu.Lock()
	defer a.mu.Unlock()

	filtered := make([]Checker, 0, len(a.checkers))
	for _, c := range a.checkers {
		if c.Name() != name {
			filtered = append(filtered, c)
		}
	}
	a.checkers = filtered
	return a
}

// Check performs all health checks in parallel and aggregates the results
func (a *Aggregator) Check(ctx context.Context) AggregatedResult {
	a.mu.RLock()
	checkers := make([]Checker, len(a.checkers))
	copy(checkers, a.checkers)
	a.mu.RUnlock()

	start := time.Now()
	result := AggregatedResult{
		Service:   a.config.ServiceName,
		Timestamp: start,
		Checks:    make(map[string]CheckResult),
	}

	if len(checkers) == 0 {
		result.Status = StatusHealthy
		result.TotalLatency = time.Since(start)
		return result
	}

	// Create timeout context
	checkCtx, cancel := context.WithTimeout(ctx, a.config.Timeout)
	defer cancel()

	// Run all checks in parallel.
	//
	// The channel is buffered for every checker, so a goroutine whose check
	// outlives the timeout can still finish and exit rather than blocking
	// forever on the send.
	resultChan := make(chan CheckResult, len(checkers))

	for _, checker := range checkers {
		go func(c Checker) {
			// A checker that panics used to take the whole process with it: a
			// panic in a goroutine is fatal and is not recovered by the HTTP
			// framework's middleware, which is on a different stack. A health
			// endpoint must not be able to kill the service it reports on.
			defer func() {
				if r := recover(); r != nil {
					resultChan <- CheckResult{
						Name:    c.Name(),
						Status:  StatusUnhealthy,
						Error:   fmt.Sprintf("check panicked: %v", r),
						Latency: time.Since(start),
					}
				}
			}()
			resultChan <- c.Check(checkCtx)
		}(checker)
	}

	// Collect results, honouring the timeout.
	//
	// This used to be a plain wg.Wait(): the timeout context was handed to the
	// checkers and then the aggregator blocked until every one of them
	// returned. Any checker that ignores its context -- a blocking driver call,
	// say -- hung the health endpoint indefinitely, which is exactly the
	// situation the endpoint exists to report.
	pending := make(map[string]bool, len(checkers))
	for _, c := range checkers {
		pending[c.Name()] = true
	}

	hasCriticalFailure := false
	hasNonCriticalFailure := false

	record := func(checkResult CheckResult) {
		delete(pending, checkResult.Name)
		result.Checks[checkResult.Name] = checkResult

		// Skip disabled checks
		if checkResult.Status == StatusDisabled {
			return
		}

		// Check if this is a failure
		if !checkResult.Status.IsHealthy() {
			if a.config.IsCritical(checkResult.Name) {
				hasCriticalFailure = true
			} else {
				hasNonCriticalFailure = true
			}
		}
	}

collect:
	for i := 0; i < len(checkers); i++ {
		select {
		case checkResult := <-resultChan:
			record(checkResult)
		case <-checkCtx.Done():
			break collect
		}
	}

	// Anything that did not report within the budget is reported as timed out
	// rather than silently omitted. Its goroutine is left to finish on its own.
	for name := range pending {
		record(CheckResult{
			Name:    name,
			Status:  StatusUnhealthy,
			Error:   fmt.Sprintf("check did not complete within %s", a.config.Timeout),
			Latency: time.Since(start),
		})
	}

	// Determine overall status
	if hasCriticalFailure {
		result.Status = StatusUnhealthy
	} else if hasNonCriticalFailure {
		result.Status = StatusDegraded
	} else {
		result.Status = StatusHealthy
	}

	result.TotalLatency = time.Since(start)
	return result
}

// CheckSequential performs all health checks sequentially
// Useful when parallel execution might cause issues
func (a *Aggregator) CheckSequential(ctx context.Context) AggregatedResult {
	a.mu.RLock()
	checkers := make([]Checker, len(a.checkers))
	copy(checkers, a.checkers)
	a.mu.RUnlock()

	start := time.Now()
	result := AggregatedResult{
		Service:   a.config.ServiceName,
		Timestamp: start,
		Checks:    make(map[string]CheckResult),
	}

	if len(checkers) == 0 {
		result.Status = StatusHealthy
		result.TotalLatency = time.Since(start)
		return result
	}

	// Create timeout context
	checkCtx, cancel := context.WithTimeout(ctx, a.config.Timeout)
	defer cancel()

	hasCriticalFailure := false
	hasNonCriticalFailure := false

	for _, checker := range checkers {
		// Check if context is cancelled
		select {
		case <-checkCtx.Done():
			result.Status = StatusUnhealthy
			result.TotalLatency = time.Since(start)
			return result
		default:
		}

		checkResult := checker.Check(checkCtx)
		result.Checks[checkResult.Name] = checkResult

		// Skip disabled checks
		if checkResult.Status == StatusDisabled {
			continue
		}

		// Check if this is a failure
		if !checkResult.Status.IsHealthy() {
			if a.config.IsCritical(checkResult.Name) {
				hasCriticalFailure = true
			} else {
				hasNonCriticalFailure = true
			}
		}
	}

	// Determine overall status
	if hasCriticalFailure {
		result.Status = StatusUnhealthy
	} else if hasNonCriticalFailure {
		result.Status = StatusDegraded
	} else {
		result.Status = StatusHealthy
	}

	result.TotalLatency = time.Since(start)
	return result
}

// GetCheckerNames returns the names of all registered checkers
func (a *Aggregator) GetCheckerNames() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	names := make([]string, len(a.checkers))
	for i, c := range a.checkers {
		names[i] = c.Name()
	}
	return names
}

// Config returns the current configuration
func (a *Aggregator) Config() Config {
	return a.config
}

// SetConfig updates the configuration
func (a *Aggregator) SetConfig(config Config) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.config = config
}
