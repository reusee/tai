package taiui

import (
	"fmt"
	"io"
	"sync"

	"github.com/gdamore/tcell/v3/tty"
)

// Cursor sequences for a TUI session: the cursor is hidden while the
// session runs and restored, with SGR attributes reset, on exit.
const (
	CursorHideSequence    = "\x1b[?25l"
	CursorRestoreSequence = "\x1b[0m\x1b[?25h"
)

const TheoryOfSession = `
taiui session theory:
- Session owns the terminal plumbing of a TUI event loop: the raw-mode
  lifecycle (Tty.Stop on exit), cursor hiding, optional mouse reporting,
  key decoding (ReadKeys), resize notification, and a coalesced update
  channel. The application supplies behavior through callbacks set
  before Run and never mutated during it: Render draws the current
  state, Key handles one key (returning true quits), OnResize observes
  size changes, and Gen runs the session's work on its own goroutine.
  Every wake — key, update, resize — triggers a render. A key wake
  drains the keys already queued in order and applies them before that
  render, so wheel and auto-repeat bursts do not produce one frame per
  queued key.
- A panic in Gen is recovered into the session error and reported
  through GenEnd (which is also called with nil on normal return), so
  the display stays up and the user can read the failure; the error is
  returned when the session quits.
- The loop performs no locking: the application synchronizes its state
  (typically one mutex), and the callbacks acquire it as needed.
`

// Session drives a TUI event loop over a terminal: it hides the cursor,
// optionally enables mouse reporting, decodes keys, listens for resizes
// and update notifications, renders on every event, and restores the
// terminal on every exit path. Fill the exported fields and call Run.
// See TheoryOfSession.
type Session struct {
	// Tty is the terminal, already started in raw mode. Run stops it on
	// return.
	Tty tty.Tty
	// Screen presents rendered frames to Tty.
	Screen *TerminalScreen
	// Update receives change notifications; each send wakes the loop and
	// triggers a render. Use a buffered channel of capacity 1 so
	// repeated notifications coalesce.
	Update chan struct{}
	// Mouse enables mouse reporting, and disables it on exit, when true.
	Mouse bool
	// Render draws the current state; it is called at the top of every
	// loop iteration. May be nil.
	Render func()
	// Key handles one decoded key name; returning true quits the session.
	// May be nil, in which case keys are consumed and ignored.
	Key func(key string) bool
	// OnResize observes terminal size changes with the new dimensions.
	// May be nil.
	OnResize func(width, height int)
	// Gen runs the session's work on its own goroutine. A panic in Gen
	// is recovered into the session error. May be nil.
	Gen func()
	// GenEnd is called after Gen returns, with the recovered panic error
	// or nil. May be nil.
	GenEnd func(err error)

	mu  sync.Mutex
	err error
}

// SetMouse enables or disables mouse reporting at runtime. Call it from
// the Key callback: the callback and rendering execute serially on the
// loop goroutine, and TerminalScreen.Present flushes its buffer before
// returning, so the sequence never interleaves with rendered output.
// The exit path unconditionally disables mouse reporting, so a disable
// here is harmless when repeated at exit. Disabling hands text
// selection back to the terminal; enabling restores pointer
// interaction. See TheoryOfSession.
func (s *Session) SetMouse(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if enabled {
		io.WriteString(s.Tty, MouseEnableSequence)
	} else {
		io.WriteString(s.Tty, MouseDisableSequence)
	}
}

// Err returns the session's recorded error: the panic recovered from
// Gen, if any.
func (s *Session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Run drives the session until the Key callback quits it, restoring the
// terminal on every exit path. It returns the session error (see Err).
// See TheoryOfSession.
func (s *Session) Run() error {
	io.WriteString(s.Tty, CursorHideSequence)
	defer func() {
		io.WriteString(s.Tty, CursorRestoreSequence)
		s.Tty.Stop()
	}()
	if s.Mouse {
		io.WriteString(s.Tty, MouseEnableSequence)
		defer io.WriteString(s.Tty, MouseDisableSequence)
	}

	resizeCh := make(chan bool, 4)
	s.Tty.NotifyResize(resizeCh)
	keyCh := make(chan string, 16)
	go ReadKeys(s.Tty, keyCh)

	if s.Gen != nil {
		go func() {
			var runErr error
			func() {
				defer func() {
					if r := recover(); r != nil {
						runErr = fmt.Errorf("panic: %v", r)
					}
				}()
				s.Gen()
			}()
			if runErr != nil {
				s.mu.Lock()
				s.err = runErr
				s.mu.Unlock()
			}
			if s.GenEnd != nil {
				s.GenEnd(runErr)
			}
		}()
	}

	for {
		if s.Render != nil {
			s.Render()
		}
		select {
		case key := <-keyCh:
			if s.handleKeyBatch(keyCh, key) {
				return s.Err()
			}
		case <-s.Update:
		case <-resizeCh:
			if ws, err := s.Tty.WindowSize(); err == nil && ws.Width > 0 && ws.Height > 0 {
				if s.OnResize != nil {
					s.OnResize(ws.Width, ws.Height)
				}
				s.Screen.Resize(ws.Width, ws.Height)
			}
		}
	}
}

// handleKeyBatch handles first and then the keys already queued on keyCh,
// preserving order. It stops at a quit key and reports whether the session
// should exit, so a burst of wheel or auto-repeat events is applied once
// per render wake instead of once per frame.
func (s *Session) handleKeyBatch(keyCh <-chan string, first string) bool {
	key := first
	for {
		if s.Key != nil && s.Key(key) {
			return true
		}
		select {
		case next, ok := <-keyCh:
			if !ok {
				return false
			}
			key = next
		default:
			return false
		}
	}
}
