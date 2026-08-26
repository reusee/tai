package taiui

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3/tty"
)

// sessionTty is a minimal tty.Tty for Session tests: reads block on an
// input channel like a terminal with no pending input, and writes are
// recorded for sequence assertions.
type sessionTty struct {
	in      chan string
	mu      sync.Mutex
	written strings.Builder
	stopped bool
}

func (s *sessionTty) Start() error { return nil }

func (s *sessionTty) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	return nil
}

func (s *sessionTty) Drain() error             { return nil }
func (s *sessionTty) NotifyResize(chan<- bool) {}
func (s *sessionTty) Close() error             { return nil }

func (s *sessionTty) WindowSize() (tty.WindowSize, error) {
	return tty.WindowSize{Width: 40, Height: 10}, nil
}

func (s *sessionTty) Read(p []byte) (int, error) {
	str, ok := <-s.in
	if !ok {
		return 0, io.EOF
	}
	return copy(p, str), nil
}

func (s *sessionTty) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.written.Write(p)
}

func (s *sessionTty) Written() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.written.String()
}

func (s *sessionTty) Stopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

func TestSessionLifecycleAndQuit(t *testing.T) {
	ttyDev := &sessionTty{in: make(chan string, 4)}
	renderCount := 0
	sess := &Session{
		Tty:    ttyDev,
		Screen: NewTerminalScreen(ttyDev, 40, 10),
		Update: make(chan struct{}, 1),
		Mouse:  true,
		Render: func() { renderCount++ },
		Key:    func(key string) bool { return key == "q" },
		Gen:    func() {},
		GenEnd: func(error) {},
	}

	done := make(chan error, 1)
	go func() { done <- sess.Run() }()
	ttyDev.in <- "q"

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected a clean quit, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for the session to quit")
	}

	if renderCount == 0 {
		t.Fatal("expected at least one render before the quit")
	}
	if !ttyDev.Stopped() {
		t.Fatal("expected the tty stopped on exit")
	}
	out := ttyDev.Written()
	hide := strings.Index(out, CursorHideSequence)
	enable := strings.Index(out, MouseEnableSequence)
	disable := strings.Index(out, MouseDisableSequence)
	restore := strings.Index(out, CursorRestoreSequence)
	if hide < 0 || enable < 0 || disable < 0 || restore < 0 {
		t.Fatalf("missing a lifecycle sequence in %q", out)
	}
	if !(hide < enable && enable < disable && disable < restore) {
		t.Fatalf("unexpected sequence order: hide %d enable %d disable %d restore %d",
			hide, enable, disable, restore)
	}
}

func TestSessionSetMouse(t *testing.T) {
	ttyDev := &sessionTty{in: make(chan string, 4)}
	sess := &Session{Tty: ttyDev}

	sess.SetMouse(true)
	sess.SetMouse(false)

	out := ttyDev.Written()
	enable := strings.Index(out, MouseEnableSequence)
	disable := strings.Index(out, MouseDisableSequence)
	if enable < 0 || disable < 0 {
		t.Fatalf("missing a mouse sequence in %q", out)
	}
	if !(enable < disable) {
		t.Fatalf("unexpected mouse sequence order: enable %d disable %d", enable, disable)
	}
}

func TestSessionRecoversGenPanic(t *testing.T) {
	ttyDev := &sessionTty{in: make(chan string, 4)}
	genEnded := make(chan struct{})
	var genEndErr error
	update := make(chan struct{}, 1)
	sess := &Session{
		Tty:    ttyDev,
		Screen: NewTerminalScreen(ttyDev, 40, 10),
		Update: update,
		Render: func() {},
		Key: func(key string) bool {
			select {
			case <-genEnded:
				return key == "q"
			default:
				return false
			}
		},
		Gen: func() { panic("boom") },
		GenEnd: func(err error) {
			genEndErr = err
			close(genEnded)
			select {
			case update <- struct{}{}:
			default:
			}
		},
	}

	done := make(chan error, 1)
	go func() { done <- sess.Run() }()
	<-genEnded
	ttyDev.in <- "q"

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("expected the recovered panic error, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for the session to quit")
	}
	if genEndErr == nil || !strings.Contains(genEndErr.Error(), "panic: boom") {
		t.Fatalf("expected GenEnd to receive the panic error, got %v", genEndErr)
	}
}
