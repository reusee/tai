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
	// cursor while rendering and restore it on exit.
	width, height := 80, 24
	io.WriteString(t, "\x1b[?25l")
	defer func() {
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

	keyCh := make(chan string, 8)
	go readKeys(t, keyCh)

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
			changed, quit := state.HandleKey(key)
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

func readKeys(r io.Reader, ch chan<- string) {
	var buf [64]byte
	var pending []byte
	for {
		n, err := r.Read(buf[:])
		if err != nil {
			return
		}
		if n == 0 {
			// The tty is in non-blocking raw mode; avoid a busy loop.
			// An incomplete ESC sequence that never grew is discarded.
			if len(pending) > 0 && pending[0] == 0x1b && len(pending) < 3 {
				pending = pending[:0]
			}
			time.Sleep(2 * time.Millisecond)
			continue
		}
		pending = append(pending, buf[:n]...)
		for len(pending) > 0 {
			if pending[0] == 0x1b {
				// Arrow keys arrive as ESC [ A/B/C/D; other sequences
				// are ignored. Wait for the full three-byte sequence.
				if len(pending) < 3 {
					if len(pending) == 2 && pending[1] != '[' {
						// ESC followed by a non-sequence byte: the ESC
						// is not part of an arrow sequence, so discard
						// it and process the rest.
						pending = pending[1:]
						continue
					}
					break
				}
				seq := pending[:3]
				if seq[1] == '[' {
					switch seq[2] {
					case 'A':
						ch <- "up"
					case 'B':
						ch <- "down"
					case 'C':
						ch <- "right"
					case 'D':
						ch <- "left"
					}
				}
				pending = pending[3:]
				continue
			}
			switch pending[0] {
			case 'q', 'Q', 0x03:
				ch <- "quit"
			case ' ':
				ch <- "space"
			case 'm', 'M':
				ch <- "modal"
			case '\t':
				ch <- "tab"
			}
			pending = pending[1:]
		}
	}
}
