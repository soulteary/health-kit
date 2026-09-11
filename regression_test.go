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
// second checker registered under an existing name could hide the first.
// Replacing the first checker had the same effect -- it never ran at all --
// so the failing check has to survive whichever order the two are added in,
// and the aggregate has to report it.
func TestDuplicateCheckerNamesDoNotMaskFailures(t *testing.T) {
	for _, tc := range []struct{ name, first, second string }{
		{"failing first", "unhealthy", "healthy"},
		{"healthy first", "healthy", "unhealthy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ranFailing, ranHealthy bool
			mk := func(kind string) Checker {
				return NewCheckerFunc("db", func(context.Context) CheckResult {
					if kind == "unhealthy" {
						ranFailing = true
						return CheckResult{Name: "db", Status: StatusUnhealthy, Error: "down"}
					}
					ranHealthy = true
					return CheckResult{Name: "db", Status: StatusHealthy}
				})
			}

			a := NewAggregator(DefaultConfig()).AddChecker(mk(tc.first)).AddChecker(mk(tc.second))

			if n := len(a.GetCheckerNames()); n != 1 {
				t.Errorf("GetCheckerNames() reports %d names, want the single distinct \"db\"", n)
			}

			result := a.Check(context.Background())

			if !ranFailing {
				t.Error("the failing checker never ran -- it was dropped at registration")
			}
			if !ranHealthy {
				t.Error("the healthy checker never ran -- it was dropped at registration")
			}
			if len(result.Checks) != 1 {
				t.Errorf("result holds %d checks, want the single \"db\" entry", len(result.Checks))
			}

			db, ok := result.Checks["db"]
			if !ok {
				t.Fatal(`no "db" entry in the result`)
			}
			if db.Status != StatusUnhealthy {
				t.Errorf("db status = %q, want unhealthy: a healthy namesake must not mask a failing check", db.Status)
			}
			if !strings.Contains(db.Error, "down") {
				t.Errorf("db error = %q, want it to carry the failure", db.Error)
			}
			if result.Status == StatusHealthy {
				t.Error("aggregate status is healthy despite a failing check registered under a duplicate name")
			}
		})
	}
}

// --- Codex review follow-ups (PR #4) ---

// TestResultNameMismatchIsNotReportedAsTimeout is the regression test for
// pending being keyed by the result's Name rather than the registered one. A
// checker returning a CheckResult with a different -- or empty -- Name left its
// registered name pending, so a completed healthy check had a timeout
// fabricated for it and the endpoint answered 503.
func TestResultNameMismatchIsNotReportedAsTimeout(t *testing.T) {
	for _, tc := range []struct {
		name       string
		resultName string
	}{
		{"empty result name", ""},
		{"different result name", "postgres"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := NewAggregator(DefaultConfig()).
				AddChecker(NewCheckerFunc("db", func(context.Context) CheckResult {
					return CheckResult{Name: tc.resultName, Status: StatusHealthy}
				}))

			result := a.Check(context.Background())

			if result.Status != StatusHealthy {
				t.Errorf("aggregate status = %q, want healthy", result.Status)
			}
			db, ok := result.Checks["db"]
			if !ok {
				t.Fatalf(`result is keyed %v, want the registered name "db"`, keysOf(result.Checks))
			}
			if db.Status != StatusHealthy {
				t.Errorf("db status = %q (error %q), want healthy -- a timeout was fabricated", db.Status, db.Error)
			}
			if db.Name != "db" {
				t.Errorf("db result Name = %q, want the registered name", db.Name)
			}
		})
	}

	// The sequential path has the same identity rule.
	a := NewAggregator(DefaultConfig()).
		AddChecker(NewCheckerFunc("db", func(context.Context) CheckResult {
			return CheckResult{Name: "postgres", Status: StatusHealthy}
		}))
	if _, ok := a.CheckSequential(context.Background()).Checks["db"]; !ok {
		t.Error(`CheckSequential keyed the result by the returned name, not the registered "db"`)
	}
}

// TestIncompleteChecksReportTheRealCause is the regression test for every
// pending check being reported as having exceeded Config.Timeout. An
// already-cancelled caller context returns in microseconds, so a five-second
// configuration claimed a five-second timeout for a call that took no time.
func TestIncompleteChecksReportTheRealCause(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Timeout = 5 * time.Second

	a := NewAggregator(cfg).
		AddChecker(NewCheckerFunc("slow", func(ctx context.Context) CheckResult {
			<-ctx.Done()
			return CheckResult{Name: "slow", Status: StatusHealthy}
		}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	start := time.Now()
	result := a.Check(ctx)
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("Check took %s with an already-cancelled context", elapsed)
	}

	slow, ok := result.Checks["slow"]
	if !ok {
		t.Fatal("no result for the pending check")
	}
	if strings.Contains(slow.Error, cfg.Timeout.String()) {
		t.Errorf("error = %q, but the call lasted %s -- a cancelled context is not a %s timeout", slow.Error, elapsed, cfg.Timeout)
	}
	if !strings.Contains(slow.Error, "canceled") {
		t.Errorf("error = %q, want it to name the cancellation", slow.Error)
	}
}

// TestSynthesizedResultsCarryTimestamps: Timestamp is always serialised, so a
// zero value put 0001-01-01T00:00:00Z in the health output for precisely the
// failures an operator is looking at.
func TestSynthesizedResultsCarryTimestamps(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Timeout = 50 * time.Millisecond

	before := time.Now()
	a := NewAggregator(cfg).
		AddChecker(NewCheckerFunc("slow", func(ctx context.Context) CheckResult {
			<-ctx.Done()
			return CheckResult{Name: "slow", Status: StatusHealthy}
		})).
		AddChecker(NewCheckerFunc("boom", func(context.Context) CheckResult {
			panic("boom")
		}))

	result := a.Check(context.Background())

	for _, name := range []string{"slow", "boom"} {
		r, ok := result.Checks[name]
		if !ok {
			t.Fatalf("no result for %q", name)
		}
		if r.Timestamp.IsZero() {
			t.Errorf("%s Timestamp is the zero value; it serialises as 0001-01-01T00:00:00Z", name)
		}
		if r.Timestamp.Before(before) {
			t.Errorf("%s Timestamp = %s, want a time from this check", name, r.Timestamp)
		}
	}
}

func keysOf(m map[string]CheckResult) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// --- Codex review round 2 (PR #4) ---

// TestIncompleteReasonComparesDeadlines is the regression test for the
// elapsed-vs-timeout heuristic. A caller deadline merely CLOSE to
// Config.Timeout -- 99ms against 100ms -- is misread as the configured
// timeout once ordinary scheduling delay pushes elapsed past it, because
// elapsed time does not say which deadline actually fired.
func TestIncompleteReasonComparesDeadlines(t *testing.T) {
	const timeout = 100 * time.Millisecond
	own := time.Now().Add(timeout)

	// A caller deadline just inside the configured timeout, observed late.
	callerCtx, cancel := context.WithDeadline(context.Background(), own.Add(-2*time.Millisecond))
	defer cancel()
	<-callerCtx.Done()

	got := incompleteReason(callerCtx, own, timeout, 150*time.Millisecond)
	if !strings.Contains(got, "caller deadline") {
		t.Errorf("reason = %q, want it to name the caller deadline even when observed after the configured timeout", got)
	}

	// The aggregator's own deadline is reported as the configured timeout.
	ownCtx, cancel2 := context.WithDeadline(context.Background(), time.Now().Add(-time.Millisecond))
	defer cancel2()
	<-ownCtx.Done()

	got = incompleteReason(ownCtx, time.Now().Add(-time.Millisecond), timeout, timeout)
	if !strings.Contains(got, timeout.String()) {
		t.Errorf("reason = %q, want it to name the configured timeout", got)
	}

	// Cancellation is still reported as cancellation.
	cancelled, cancel3 := context.WithCancel(context.Background())
	cancel3()
	if got := incompleteReason(cancelled, own, timeout, time.Millisecond); !strings.Contains(got, "canceled") {
		t.Errorf("reason = %q, want it to name the cancellation", got)
	}
}

// --- Codex review round 3 (PR #4) ---

// TestIncompleteReasonKeepsSubMillisecondDeadlines is the regression test for
// the millisecond of slack the round-2 fix used. A caller deadline less than a
// millisecond earlier than the aggregator's -- 99.5ms against 100ms -- fell
// inside that tolerance and was reported as the configured timeout, which is
// exactly the misattribution the comparison exists to avoid.
func TestIncompleteReasonKeepsSubMillisecondDeadlines(t *testing.T) {
	const timeout = 100 * time.Millisecond
	own := time.Now().Add(timeout)

	for _, earlier := range []time.Duration{
		500 * time.Microsecond,
		time.Microsecond,
		time.Nanosecond,
	} {
		t.Run(earlier.String(), func(t *testing.T) {
			callerCtx, cancel := context.WithDeadline(context.Background(), own.Add(-earlier))
			defer cancel()
			<-callerCtx.Done()

			got := incompleteReason(callerCtx, own, timeout, timeout)
			if !strings.Contains(got, "caller deadline") {
				t.Errorf("reason = %q, want it to name the caller deadline %s before ours", got, earlier)
			}
		})
	}
}

// TestCheckContextUsesTheRecordedDeadline pins the property the comparison
// above rests on: checkCtx is derived from ownDeadline itself, so when the
// caller's deadline is later, checkCtx.Deadline() is EXACTLY ownDeadline
// rather than a few nanoseconds past it.
func TestCheckContextUsesTheRecordedDeadline(t *testing.T) {
	const timeout = 50 * time.Millisecond

	var seen time.Time
	var ok bool
	agg := NewAggregator(Config{Timeout: timeout})
	agg.AddChecker(NewCheckerFunc("probe", func(ctx context.Context) CheckResult {
		seen, ok = ctx.Deadline()
		return CheckResult{Name: "probe", Status: StatusHealthy}
	}))

	before := time.Now().Add(timeout)
	agg.Check(context.Background())
	after := time.Now().Add(timeout)

	if !ok {
		t.Fatal("the check context carried no deadline")
	}
	if seen.Before(before) || seen.After(after) {
		t.Errorf("check deadline %s is outside [%s, %s]; it must come from the recorded ownDeadline", seen, before, after)
	}
}
