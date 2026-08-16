package generators

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/modes"
)

// discardLogger returns a logger that drops every record, for tests that
// only assert on retry behavior.
func discardLogger() logs.Logger {
	return logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestRetrierExhaustionStripsErrRetryable(t *testing.T) {
	calls := 0
	fn := func() (int, error) {
		calls++
		return 0, errors.Join(errors.New("transient api failure"), ErrRetryable)
	}

	retrier := Retrier{logger: discardLogger()}
	result, err := retrier.Do(context.Background(), fn, 0)

	if err == nil {
		t.Fatal("expected error after retry exhaustion")
	}
	if calls != 10 {
		t.Fatalf("expected 10 calls (maxRetries), got %d", calls)
	}
	if errors.Is(err, ErrRetryable) {
		t.Fatal("ErrRetryable must be stripped after exhaustion to prevent outer retry loops")
	}
	if !strings.Contains(err.Error(), "retry exhausted") {
		t.Fatalf("expected 'retry exhausted' in error message, got: %v", err)
	}
	if result != 0 {
		t.Fatalf("expected zero result, got %d", result)
	}
}

func TestRetrierSuccessOnRetry(t *testing.T) {
	calls := 0
	fn := func() (int, error) {
		calls++
		if calls < 3 {
			return 0, errors.Join(errors.New("transient"), ErrRetryable)
		}
		return 42, nil
	}

	retrier := Retrier{logger: discardLogger()}
	result, err := retrier.Do(context.Background(), fn, 0)

	if err != nil {
		t.Fatalf("expected success on third attempt, got: %v", err)
	}
	if result != 42 {
		t.Fatalf("expected result 42, got %d", result)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestRetrierNonRetryableImmediateReturn(t *testing.T) {
	calls := 0
	testErr := errors.New("fatal error")
	fn := func() (int, error) {
		calls++
		return 0, testErr
	}

	retrier := Retrier{logger: discardLogger()}
	_, err := retrier.Do(context.Background(), fn, 0)

	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, testErr) {
		t.Fatalf("expected original error to be preserved, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call (no retry for non-retryable), got %d", calls)
	}
}

func TestRetrierRecordsAPIErrors(t *testing.T) {
	// Each retryable API error is recorded as an api_error event so the
	// interaction transcript shows the raw API errors even when a later
	// attempt succeeds. The recorder is bound on the Retrier value. See
	// generators.TheoryOfEventRecorder.
	rec := &fakeEventRecorder{enabled: true}
	calls := 0
	fn := func() (int, error) {
		calls++
		if calls < 3 {
			return 0, errors.Join(errors.New("transient"), ErrRetryable)
		}
		return 42, nil
	}

	retrier := Retrier{logger: discardLogger(), eventRecorder: rec}
	result, err := retrier.Do(context.Background(), fn, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result != 42 {
		t.Fatalf("expected 42, got %d", result)
	}
	// Two retryable errors, two recorded events.
	if len(rec.events) != 2 {
		t.Fatalf("expected 2 api_error events, got %d: %v", len(rec.events), rec.events)
	}
	if !strings.Contains(rec.events[0], "retryable API error (attempt 1/10)") {
		t.Fatalf("unexpected event: %s", rec.events[0])
	}
	if !strings.Contains(rec.events[1], "retryable API error (attempt 2/10)") {
		t.Fatalf("unexpected event: %s", rec.events[1])
	}
}

func TestRetrierProvider(t *testing.T) {
	// The Retrier provider binds the logger and event recorder from the
	// dscope scope, so the caller passes only the runtime values
	// (context, the function, and the optional backoff). The generic Do
	// method infers T from the function's result type, so the same
	// dscope-provided value serves State-returning and other callers
	// without a monomorphic wrapper. See TheoryOfRetry.
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Call(func(
		retrier Retrier,
	) {
		calls := 0
		result, err := retrier.Do(context.Background(), func() (State, error) {
			calls++
			if calls < 2 {
				return nil, errors.Join(errors.New("transient"), ErrRetryable)
			}
			return NewPrompts("", nil), nil
		}, 0)
		if err != nil {
			t.Fatalf("expected success on second attempt, got: %v", err)
		}
		if result == nil {
			t.Fatal("expected a non-nil state")
		}
		if calls != 2 {
			t.Fatalf("expected 2 calls, got %d", calls)
		}
	})
}
