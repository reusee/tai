package taiui

import (
	"errors"
	"io"
	"testing"

	"github.com/gdamore/tcell/v3/tty"
)

// openerTty is a fake terminal for OpenTty tests: Start returns
// startErr once, then succeeds, modeling a transient start failure
// such as an interrupted ioctl.
type openerTty struct {
	startErr error
	starts   int
}

func (f *openerTty) Start() error {
	f.starts++
	err := f.startErr
	f.startErr = nil
	return err
}

func (f *openerTty) Stop() error              { return nil }
func (f *openerTty) Drain() error             { return nil }
func (f *openerTty) NotifyResize(chan<- bool) {}

func (f *openerTty) WindowSize() (tty.WindowSize, error) {
	return tty.WindowSize{Width: 80, Height: 25}, nil
}

func (f *openerTty) Read([]byte) (int, error) { return 0, io.EOF }

func (f *openerTty) Write(p []byte) (int, error) { return len(p), nil }

func (f *openerTty) Close() error { return nil }

// TestOpenTtyFallsBackAfterStartFailure pins the fall-through: a
// backend whose every start fails does not disable the session, so the
// next backend still wins. See TheoryOfTerminalAcquisition.
func TestOpenTtyFallsBackAfterStartFailure(t *testing.T) {
	std, dev := &openerTty{}, &openerTty{}
	// Re-arm the failure before every open, so both starts of the first
	// backend fail and OpenTty must fall through to the second.
	armFailure := func() (tty.Tty, error) {
		std.startErr = errors.New("not raw-able")
		return std, nil
	}
	acquired, err := OpenTty(armFailure, func() (tty.Tty, error) { return dev, nil })
	if err != nil {
		t.Fatal(err)
	}
	if acquired != tty.Tty(dev) {
		t.Fatal("expected the fallback backend to win")
	}
	if std.starts != 2 {
		t.Fatalf("unexpected first-backend starts: %d", std.starts)
	}
}

// TestOpenTtyRetriesTransientStartFailure pins the single retry of one
// backend: a transient start failure recovers without falling through.
// See TheoryOfTerminalAcquisition.
func TestOpenTtyRetriesTransientStartFailure(t *testing.T) {
	fake := &openerTty{}
	fake.startErr = errors.New("transient")
	acquired, err := OpenTty(func() (tty.Tty, error) { return fake, nil })
	if err != nil {
		t.Fatal(err)
	}
	if acquired != tty.Tty(fake) || fake.starts != 2 {
		t.Fatalf("expected one retry, starts=%d", fake.starts)
	}
}

// TestOpenTtyAllAttemptsFail pins the error path: when every attempt
// fails, OpenTty reports an error so the caller falls back to plain
// output. See TheoryOfTerminalAcquisition.
func TestOpenTtyAllAttemptsFail(t *testing.T) {
	_, err := OpenTty(
		func() (tty.Tty, error) { return nil, errors.New("construct fail") },
		func() (tty.Tty, error) { return &openerTty{startErr: errors.New("start fail")}, nil },
	)
	if err == nil {
		t.Fatal("expected an error when every attempt fails")
	}
}
