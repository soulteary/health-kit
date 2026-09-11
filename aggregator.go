package health

import (
	"context"
	"errors"
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

// AddChecker adds a health checker to the aggregator.
//
// Registering a second checker under a name already in use keeps BOTH: every
// registered checker runs, and their results are combined into the single
// entry that name has in the output, keeping the least healthy status.
// Replacing the first checker instead -- or letting the map key silently
// overwrite it -- means a failing check can be masked by a healthy namesake,
// which is the one thing a health endpoint must never do.
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

	// Create timeout context from ownDeadline rather than from the duration,
	// so checkCtx.Deadline() is EXACTLY min(caller deadline, ownDeadline) and
	// incompleteReason can tell the two apart by comparing them directly.
	//
	// WithTimeout calls time.Now() again, a few nanoseconds after the line
	// above, which left the two deadlines differing by an amount no exact
	// comparison could account for -- and the millisecond of slack that
	// papered over it discarded any caller deadline less than a millisecond
	// earlier than this one.
	ownDeadline := time.Now().Add(a.config.Timeout)
	checkCtx, cancel := context.WithDeadline(ctx, ownDeadline)
	defer cancel()

	// Run all checks in parallel.
	//
	// The channel is buffered for every checker, so a goroutine whose check
	// outlives the timeout can still finish and exit rather than blocking
	// forever on the send.
	resultChan := make(chan checkOutcome, len(checkers))

	for _, checker := range checkers {
		go func(c Checker) {
			// A checker that panics used to take the whole process with it: a
			// panic in a goroutine is fatal and is not recovered by the HTTP
			// framework's middleware, which is on a different stack. A health
			// endpoint must not be able to kill the service it reports on.
			defer func() {
				if r := recover(); r != nil {
					resultChan <- checkOutcome{
						name: c.Name(),
						result: CheckResult{
							Status:    StatusUnhealthy,
							Error:     fmt.Sprintf("check panicked: %v", r),
							Latency:   time.Since(start),
							Timestamp: time.Now(),
						},
					}
				}
			}()
			resultChan <- checkOutcome{name: c.Name(), result: c.Check(checkCtx)}
		}(checker)
	}

	// Collect results, honouring the timeout.
	//
	// This used to be a plain wg.Wait(): the timeout context was handed to the
	// checkers and then the aggregator blocked until every one of them
	// returned. Any checker that ignores its context -- a blocking driver call,
	// say -- hung the health endpoint indefinitely, which is exactly the
	// situation the endpoint exists to report.
	// pending counts outstanding checks PER REGISTERED NAME. It is keyed by
	// Checker.Name(), never by the name the result carries: a checker whose
	// CheckResult.Name differs from -- or simply omits -- its Name() used to
	// leave the registered name pending, so a successfully completed healthy
	// check had a timeout fabricated for it and turned the aggregate
	// unhealthy. The count also lets duplicate registrations of one name both
	// be waited for.
	pending := make(map[string]int, len(checkers))
	for _, c := range checkers {
		pending[c.Name()]++
	}

	record := func(out checkOutcome) {
		if n := pending[out.name]; n > 1 {
			pending[out.name] = n - 1
		} else {
			delete(pending, out.name)
		}

		// The registered name is the identity: it is what the caller
		// configured, what CriticalChecks is matched against, and what keys
		// the output.
		checkResult := out.result
		checkResult.Name = out.name
		if existing, ok := result.Checks[out.name]; ok {
			checkResult = leastHealthy(existing, checkResult)
		}
		result.Checks[out.name] = checkResult
	}

collect:
	for i := 0; i < len(checkers); i++ {
		select {
		case out := <-resultChan:
			record(out)
		case <-checkCtx.Done():
			break collect
		}
	}

	// Anything that did not report within the budget is reported as incomplete
	// rather than silently omitted. Its goroutine is left to finish on its own.
	incomplete := incompleteReason(checkCtx, ownDeadline, a.config.Timeout, time.Since(start))
	for name := range pending {
		record(checkOutcome{
			name: name,
			result: CheckResult{
				Status:    StatusUnhealthy,
				Error:     incomplete,
				Latency:   time.Since(start),
				Timestamp: time.Now(),
			},
		})
	}

	// Determine overall status from the combined results, so a name recorded
	// more than once is counted once.
	hasCriticalFailure := false
	hasNonCriticalFailure := false
	for name, checkResult := range result.Checks {
		if checkResult.Status == StatusDisabled || checkResult.Status.IsHealthy() {
			continue
		}
		if a.config.IsCritical(name) {
			hasCriticalFailure = true
		} else {
			hasNonCriticalFailure = true
		}
	}

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

	for _, checker := range checkers {
		// Check if context is cancelled
		select {
		case <-checkCtx.Done():
			result.Status = StatusUnhealthy
			result.TotalLatency = time.Since(start)
			return result
		default:
		}

		// Key by the REGISTERED name, and combine namesakes, for the same
		// reasons as Check: a result carrying a different (or empty) Name must
		// not land under the wrong key, and a healthy checker must not be able
		// to mask a failing one registered under the same name.
		name := checker.Name()
		checkResult := checker.Check(checkCtx)
		checkResult.Name = name
		if existing, ok := result.Checks[name]; ok {
			checkResult = leastHealthy(existing, checkResult)
		}
		result.Checks[name] = checkResult
	}

	hasCriticalFailure := false
	hasNonCriticalFailure := false
	for name, checkResult := range result.Checks {
		if checkResult.Status == StatusDisabled || checkResult.Status.IsHealthy() {
			continue
		}
		if a.config.IsCritical(name) {
			hasCriticalFailure = true
		} else {
			hasNonCriticalFailure = true
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

// GetCheckerNames returns the distinct names of all registered checkers, in
// registration order.
//
// Several checkers may share a name -- they all run, and their results are
// combined into that name's single entry -- so the names are deduplicated here
// to match the keys a check result actually has.
func (a *Aggregator) GetCheckerNames() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	names := make([]string, 0, len(a.checkers))
	seen := make(map[string]bool, len(a.checkers))
	for _, c := range a.checkers {
		if seen[c.Name()] {
			continue
		}
		seen[c.Name()] = true
		names = append(names, c.Name())
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

// checkOutcome pairs a check's result with the name it was REGISTERED under.
//
// A Checker is free to return a CheckResult whose Name differs from its
// Name(), or to leave it empty, so the result alone cannot say which
// registered check reported.
type checkOutcome struct {
	name   string
	result CheckResult
}

// statusSeverity orders statuses from healthiest to least healthy, so two
// results recorded under one name can be combined without losing a failure.
func statusSeverity(s Status) int {
	switch s {
	case StatusDisabled:
		return 0
	case StatusHealthy:
		return 1
	case StatusDegraded:
		return 2
	case StatusUnhealthy:
		return 3
	default:
		return 2
	}
}

// leastHealthy returns whichever of two results for the same registered name
// reports the worse status, merging the other's error text in so neither check
// disappears from the output.
func leastHealthy(a, b CheckResult) CheckResult {
	worse, other := a, b
	if statusSeverity(b.Status) > statusSeverity(a.Status) {
		worse, other = b, a
	}

	if other.Error != "" && other.Error != worse.Error {
		if worse.Error == "" {
			worse.Error = other.Error
		} else {
			worse.Error = worse.Error + "; " + other.Error
		}
	}
	if worse.Latency < other.Latency {
		worse.Latency = other.Latency
	}
	return worse
}

// incompleteReason describes why a check has no result, distinguishing the
// caller's context ending from the configured timeout elapsing.
//
// Every pending check used to be reported as having exceeded Config.Timeout.
// An already-cancelled caller context returns in microseconds, so a five-second
// configuration produced "check did not complete within 5s" for a call that
// lasted no time at all -- misleading to both operators and monitoring.
func incompleteReason(ctx context.Context, ownDeadline time.Time, timeout, elapsed time.Duration) string {
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return fmt.Sprintf("check did not complete: context canceled after %s", elapsed.Round(time.Millisecond))
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		// Which deadline fired is decided by comparing the DEADLINES, not by
		// comparing elapsed time against the timeout. An elapsed-time
		// heuristic misreads a caller deadline that is merely close to
		// Config.Timeout -- 99ms against 100ms -- because ordinary scheduling
		// delay pushes elapsed past the timeout before the result is
		// observed.
		//
		// Exactly, with no tolerance: checkCtx was derived from ownDeadline
		// itself, so its deadline is min(caller, ownDeadline) and any earlier
		// value is the caller's, however small the difference. Allowing a
		// millisecond of slack reported a caller deadline 99.5ms before a
		// 100ms one as the configured timeout.
		if actual, ok := ctx.Deadline(); ok && actual.Before(ownDeadline) {
			return fmt.Sprintf("check did not complete: caller deadline exceeded after %s", elapsed.Round(time.Millisecond))
		}
		return fmt.Sprintf("check did not complete within %s", timeout)
	default:
		return fmt.Sprintf("check did not complete within %s", timeout)
	}
}
