package apps

import (
	"errors"
	"testing"

	"github.com/reusee/dscope"
)

// mustRaiseMainErr runs fn and verifies that it raises want as a panic:
// the app's error path turns a non-nil MainErr into a panic carrying
// the same error. See TheoryOfApps.
func mustRaiseMainErr(t *testing.T, want error, fn func()) {
	t.Helper()
	defer func() {
		v := recover()
		err, ok := v.(error)
		if !ok || !errors.Is(err, want) {
			t.Fatalf("expected the raised MainErr, got %v", v)
		}
	}()
	fn()
}

func TestAppRunMainErr(t *testing.T) {
	// A nil MainErr is a normal outcome: the run ends without a panic.
	New("test_run_nil", "test app", func() MainErr { return nil }).Run()

	sentinel := errors.New("boom")
	mustRaiseMainErr(t, sentinel, func() {
		New("test_run_err", "test app", func() MainErr { return sentinel }).Run()
	})
}

func TestAppCallMainErr(t *testing.T) {
	New("test_call_nil", "test app", func() MainErr { return nil }).Call(dscope.New())

	sentinel := errors.New("boom")
	mustRaiseMainErr(t, sentinel, func() {
		New("test_call_err", "test app", func() MainErr { return sentinel }).Call(dscope.New())
	})
}
