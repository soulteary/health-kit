package health

import (
	"context"
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
func (a *Aggregator) AddChecker(checker Checker) *Aggregator {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.checkers = append(a.checkers, checker)
	return a
}

// AddCheckers adds multiple health checkers to the aggregator
func (a *Aggregator) AddCheckers(checkers ...Checker) *Aggregator {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.checkers = append(a.checkers, checkers...)
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

	// Run all checks in parallel
	var wg sync.WaitGroup
	resultChan := make(chan CheckResult, len(checkers))

	for _, checker := range checkers {
		wg.Add(1)
		go func(c Checker) {
			defer wg.Done()
			resultChan <- c.Check(checkCtx)
		}(checker)
	}

	// Wait for all checks to complete
	wg.Wait()
	close(resultChan)

	// Collect results
	hasCriticalFailure := false
	hasNonCriticalFailure := false

	for checkResult := range resultChan {
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
