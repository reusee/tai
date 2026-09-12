package taiui

import (
	"errors"

	"github.com/gdamore/tcell/v3/tty"
)

const TheoryOfTerminalAcquisition = `
taiui terminal acquisition theory:
- OpenTty acquires a terminal for a TUI session from an ordered list
  of backends, each retried once. The raw-mode ioctls of a session
  start can fail transiently — an interleaved signal delivering EINTR,
  or a previous session's teardown racing this one's setup — and a
  backend whose terminal is not usable must still fall through to the
  next one. A single failure therefore never disables the TUI for the
  whole invocation; only when every attempt fails does OpenTty return
  an error, and the caller falls back to plain output.
- The default backends open stdin/stdout first and the controlling
  terminal second, so a non-terminal stdin — a pipe from another
  program — still reaches the controlling terminal when one exists.
`

// TtyOpener opens one terminal backend for a TUI session. It is the
// unit OpenTty iterates. See TheoryOfTerminalAcquisition.
type TtyOpener func() (tty.Tty, error)

// OpenTty acquires a terminal for a TUI session. Each backend is
// retried once, so a transient start failure does not disable the
// session; the first terminal that starts is returned. With no openers
// given, the default backends open stdin/stdout first and the
// controlling terminal second. An error means no backend produced a
// usable terminal. See TheoryOfTerminalAcquisition.
func OpenTty(openers ...TtyOpener) (tty.Tty, error) {
	if len(openers) == 0 {
		openers = []TtyOpener{tty.NewStdIoTty, tty.NewDevTty}
	}
	for _, open := range openers {
		for retry := 0; retry < 2; retry++ {
			t, err := open()
			if err != nil {
				continue
			}
			if err := t.Start(); err != nil {
				continue
			}
			return t, nil
		}
	}
	return nil, errors.New("taiui: no usable terminal")
}
