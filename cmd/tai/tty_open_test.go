package main

import (
	"errors"
	"io"
	"testing"

	"github.com/gdamore/tcell/v3/tty"
)

type fakeTty struct {
	startErr error
	starts   int
}

// Start returns startErr once, then succeeds — modeling a transient
// start failure such as an interrupted ioctl.
func (f *fakeTty) Start() error {
	f.starts++
	err := f.startErr
	f.startErr = nil
	return err
}

func (f *fakeTty) Stop() error              { return nil }
func (f *fakeTty) Drain() error             { return nil }
func (f *fakeTty) NotifyResize(chan<- bool) {}

func (f *fakeTty) WindowSize() (tty.WindowSize, error) {
	return tty.WindowSize{Width: 80, Height: 25}, nil
}

func (f *fakeTty) Read([]byte) (int, error) { return 0, io.EOF }

func (f *fakeTty) Write(p []byte) (int, error) { return len(p), nil }

func (f *fakeTty) Close() error { return nil }

func TestTryOpenTtyFallsBackAfterStartFailure(t *testing.T) {
	std, dev := &fakeTty{}, &fakeTty{}
	// Re-arm the failure before every open, so both starts of the first
	// backend fail and tryOpenTty must fall through to /dev/tty.
	armFailure := func() (tty.Tty, error) {
		std.startErr = errors.New("not raw-able")
		return std, nil
	}
	acquired, err := tryOpenTty([]ttyOpener{
		armFailure,
		func() (tty.Tty, error) { return dev, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if acquired != tty.Tty(dev) {
		t.Fatal("expected the dev tty fallback to win")
	}
	if std.starts != 2 {
		t.Fatalf("unexpected std starts: %d", std.starts)
	}
}

func TestTryOpenTtyRetriesTransientStartFailure(t *testing.T) {
	fake := &fakeTty{}
	fake.startErr = errors.New("transient")
	acquired, err := tryOpenTty([]ttyOpener{
		func() (tty.Tty, error) { return fake, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if acquired != tty.Tty(fake) || fake.starts != 2 {
		t.Fatalf("expected one retry, starts=%d", fake.starts)
	}
}

func TestTryOpenTtyAllAttemptsFail(t *testing.T) {
	_, err := tryOpenTty([]ttyOpener{
		func() (tty.Tty, error) { return nil, errors.New("construct fail") },
		func() (tty.Tty, error) { return &fakeTty{startErr: errors.New("start fail")}, nil },
	})
	if err == nil {
		t.Fatal("expected an error when every attempt fails")
	}
}
