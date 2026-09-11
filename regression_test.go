package health

import (
	"context"
	"strings"
	"testing"
	"time"
)

// blockingChecker ignores its context, which is exactly the case the timeout
// has to survive: a driver call that does not take a context, or takes one and
// ignores it.
type blockingChecker struct {
	name string
	done chan struct{}
}

func (c *blockingChecker) Name() string { return c.name }
func (c *blockingChecker) Check(context.Context) CheckResult {
	<-c.done
	return CheckResult{Name: c.name, Status: StatusHealthy}
}

type panickingChecker struct{ name string }

func (c *panickingChecker) Name() string { return c.name }
func (c *panickingChecker) Check(context.Context) CheckResult {
	panic("probe blew up")
}

// TestTimeoutIsEnforcedAgainstUncooperativeCheckers is the regression test for
// the aggregator blocking on wg.Wait(): the timeout context was handed to the
// checkers and then ignored, so a checker that did not honour it hung the
// health endpoint indefinitely.
func TestTimeoutIsEnforcedAgainstUncooperativeCheckers(t *testing.T) {
	blocked := &blockingChecker{name: "stuck", done: make(chan struct{})}
	defer close(blocked.done)

	cfg := DefaultConfig()
	cfg.Timeout = 100 * time.Millisecond
	a := NewAggregator(cfg).
		AddChecker(blocked).
		AddChecker(NewCheckerFunc("fast", func(context.Context) CheckResult {
			return CheckResult{Name: "fast", Status: StatusHealthy}
		}))

	done := make(chan AggregatedResult, 1)
	go func() { done <- a.Check(context.Background()) }()

	select {
	case result := <-done:
		stuck, ok := result.Checks["stuck"]
		if !ok {
			t.Fatal("the timed-out check was omitted from the result entirely")
		}
		if stuck.Status.IsHealthy() {
			t.Errorf("timed-out check reported %q, want unhealthy", stuck.Status)
		}
		if !strings.Contains(stuck.Error, "did not complete") {
			t.Errorf("timed-out check error = %q, want it to say so", stuck.Error)
		}
		if fast, ok := result.Checks["fast"]; !ok || !fast.Status.IsHealthy() {
			t.Error("the cooperative check's result was lost")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Check() never returned; the timeout is not enforced")
	}
}

// TestPanickingCheckerDoesNotCrashTheProcess: a panic in a goroutine is fatal
// and is not recovered by an HTTP framework's middleware, so a health probe
// could kill the service it reports on.
func TestPanickingCheckerDoesNotCrashTheProcess(t *testing.T) {
	a := NewAggregator(DefaultConfig()).
		AddChecker(&panickingChecker{name: "boom"}).
		AddChecker(NewCheckerFunc("ok", func(context.Context) CheckResult {
			return CheckResult{Name: "ok", Status: StatusHealthy}
		}))

	result := a.Check(context.Background())

	boom, ok := result.Checks["boom"]
	if !ok {
		t.Fatal("the panicking check produced no result")
	}
	if boom.Status != StatusUnhealthy {
		t.Errorf("panicking check status = %q, want unhealthy", boom.Status)
	}
	if !strings.Contains(boom.Error, "panicked") {
		t.Errorf("panicking check error = %q, want it to mention the panic", boom.Error)
	}
	if okResult, ok := result.Checks["ok"]; !ok || !okResult.Status.IsHealthy() {
		t.Error("a sibling check was lost because of the panic")
	}
}

// TestDuplicateCheckerNamesDoNotMaskFailures: results are keyed by name, so a
// second checker registered under an existing name silently replaced the first
// in the output -- a healthy namesake could hide a failing check.
func TestDuplicateCheckerNamesDoNotMaskFailures(t *testing.T) {
	a := NewAggregator(DefaultConfig()).
		AddChecker(NewCheckerFunc("db", func(context.Context) CheckResult {
			return CheckResult{Name: "db", Status: StatusUnhealthy, Error: "down"}
		})).
		AddChecker(NewCheckerFunc("db", func(context.Context) CheckResult {
			return CheckResult{Name: "db", Status: StatusHealthy}
		}))

	if n := len(a.GetCheckerNames()); n != 1 {
		t.Errorf("aggregator holds %d checkers named \"db\", want 1", n)
	}

	result := a.Check(context.Background())
	if len(result.Checks) != 1 {
		t.Errorf("result holds %d checks, want 1", len(result.Checks))
	}
}
