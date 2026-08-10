package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v3/tty"
	"github.com/reusee/dscope"
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

	// The key-handled state (scroll, toggle, w1 weight, modal, rotation)
	// lives inside the HandleKey provider: it injects the current values
	// and returns the providers that carry the new values, so forking one
	// piece recomputes only the components that depend on it. The frame
	// counter and the clock are local variables updated by the event loop
	// below.
	frame := int64(0)
	now := time.Now()

	// The App provides every provider as a method, so dscope.New(new(App))
	// creates a scope with all of them. The dynamic state (terminal size,
	// frame counter, clock) is external: the event loop forks it as
	// closures over its local variables.
	scope := dscope.New(new(App))

	// forkState forks the current scope with the given providers.
	forkState := func(defs ...any) taiui.Scope {
		scope = scope.Fork(defs...)
		return scope
	}

	scope = forkState(
		func() Width { return Width(width) },
		func() Height { return Height(height) },
		func() Frame { return Frame(frame) },
		func() Now { return Now(now) },
	)

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

	// The initial render presents the first frame; subsequent renders
	// happen only when a case below changes state, so a key press that
	// changes nothing skips the render entirely.
	taiui.Render(scope, screen, taiui.DiscardScreen{})
	for {
		select {
		case key := <-keyCh:
			// The key handler is a provider in the scope: it injects the
			// current state and returns the providers that carry the new
			// state.
			handleKey := dscope.Get[HandleKey](scope)
			changed, quit := handleKey(key)
			if quit {
				return
			}
			// Fork only the providers whose state changed: dscope keeps the
			// cached results of the unchanged providers, so the next render
			// recomputes only the components that depend on the change.
			// All changed providers are forked in one layer, so the scope
			// stack stays flat.
			if len(changed) > 0 {
				scope = forkState(changed...)
				taiui.Render(scope, screen, taiui.DiscardScreen{})
			}
		case <-tick.C:
			// The ball is derived from the frame counter: forking the frame
			// state rebuilds the framebuffer content declaratively.
			frame++
			scope = forkState(func() Frame { return Frame(frame) })
			taiui.Render(scope, screen, taiui.DiscardScreen{})
		case <-clock.C:
			now = time.Now()
			scope = forkState(func() Now { return Now(now) })
			taiui.Render(scope, screen, taiui.DiscardScreen{})
		case <-resizeCh:
			if ws, err := t.WindowSize(); err == nil && ws.Width > 0 && ws.Height > 0 {
				width, height = ws.Width, ws.Height
				screen.Resize(width, height)
				// Both changed providers are forked in one layer, so the
				// scope stack stays flat.
				scope = forkState(
					func() Width { return Width(width) },
					func() Height { return Height(height) },
				)
				taiui.Render(scope, screen, taiui.DiscardScreen{})
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
