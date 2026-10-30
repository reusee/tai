package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v3/tty"
	"github.com/reusee/tai/taiui"
)

const (
	fbWidth  = 30
	fbHeight = 3
)

func main() {
	t, err := tty.NewStdIoTty()
	if err != nil {
		t, err = tty.NewDevTty()
		if err != nil {
			fmt.Fprintln(os.Stderr, "taiuidemo: no terminal available:", err)
			os.Exit(1)
		}
	}
	if err := t.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "taiuidemo: cannot start terminal:", err)
		os.Exit(1)
	}
	defer t.Stop()

	// The demo drives the terminal directly with ANSI escapes: hide the
	// cursor while rendering and restore it on exit. Mouse reporting is
	// enabled for the session, so a tap arrives as a press and release
	// and a swipe as a wheel event or a button-held drag; the demo maps
	// them onto its own actions. See TheoryOfDemoArchitecture.
	width, height := 80, 24
	io.WriteString(t, "\x1b[?25l")
	io.WriteString(t, taiui.MouseEnableSequence)
	defer func() {
		io.WriteString(t, taiui.MouseDisableSequence)
		fmt.Fprintf(t, "\x1b[%d;1H", height)
		io.WriteString(t, "\x1b[0m\x1b[?25h")
	}()

	if ws, err := t.WindowSize(); err == nil && ws.Width > 0 && ws.Height > 0 {
		width, height = ws.Width, ws.Height
	}

	screen := taiui.NewTerminalScreen(t, width, height)

	// The demo state lives in one struct. The key-handled state (scroll,
	// toggle, w1 weight, modal, rotation) is mutated by HandleKey; the
	// dynamic state (terminal size, frame counter, clock) is updated by
	// the event loop below.
	state := State{
		Width:    width,
		Height:   height,
		Toggle:   true,
		W1Weight: 1,
		Now:      time.Now(),
	}

	// Render builds the element tree from the current state and presents
	// it. The initial render presents the first frame; subsequent renders
	// happen only when a case below changes state, so a key press that
	// changes nothing skips the render entirely.
	render := func() {
		taiui.Render(BuildRoot(state), screen, taiui.DiscardScreen{})
	}

	resizeCh := make(chan bool, 4)
	t.NotifyResize(resizeCh)

	// The library's decoder turns the terminal into generic key names —
	// arrows, printable characters, and pointer events — and mapDemoKey
	// maps the demo's bindings onto its action vocabulary. The demo
	// carries no decoder of its own. See TheoryOfDemoArchitecture.
	keyCh := make(chan string, 8)
	go taiui.ReadKeys(t, keyCh)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	clock := time.NewTicker(time.Second)
	defer clock.Stop()

	render()
	for {
		select {
		case key := <-keyCh:
			// HandleKey mutates the state and reports whether anything
			// changed, so a key that had no effect (e.g., up at the
			// scroll clamp) skips the render entirely.
			changed, quit := state.HandleKey(mapDemoKey(key))
			if quit {
				return
			}
			if changed {
				render()
			}
		case <-tick.C:
			// The ball is derived from the frame counter: bumping the
			// frame and rebuilding the tree moves the ball declaratively.
			state.Frame++
			render()
		case <-clock.C:
			state.Now = time.Now()
			render()
		case <-resizeCh:
			if ws, err := t.WindowSize(); err == nil && ws.Width > 0 && ws.Height > 0 {
				width, height = ws.Width, ws.Height
				screen.Resize(width, height)
				state.Width, state.Height = width, height
				render()
			}
		case <-sigCh:
			return
		}
	}
}
