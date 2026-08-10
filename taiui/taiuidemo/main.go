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

type discardScreen struct{}

func (discardScreen) Width() int          { return 1 }
func (discardScreen) Height() int         { return 1 }
func (discardScreen) Present(taiui.Frame) {}

// ReleaseFrame returns the discarded frame's cells to the pool: the
// screen renders and discards, so the cells are never retained.
func (discardScreen) ReleaseFrame(frame taiui.Frame) {
	taiui.ReleaseFrame(frame)
}

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

	screen := &ansiScreen{w: t, width: width, height: height}

	// Each piece of state is an independent variable and an independent
	// provider, so forking one piece recomputes only the components that
	// depend on it.
	scroll := 0
	toggle := true
	w1Weight := 1
	modal := false
	frame := int64(0)
	now := time.Now()

	scope := dscope.New(
		func() Width { return Width(width) },
		func() Height { return Height(height) },
		func() Scroll { return Scroll(scroll) },
		func() Toggle { return Toggle(toggle) },
		func() W1Weight { return W1Weight(w1Weight) },
		func() Modal { return Modal(modal) },
		func() Frame { return Frame(frame) },
		func() Now { return Now(now) },
		provideFrameBufferContent,
		provideHeader,
		provideFooter,
		providePanelText,
		providePanelScroll,
		providePanelBox,
		providePanelDynamic,
		rootProvider,
	)

	// forkState forks the current scope with the given providers.
	forkState := func(defs ...any) taiui.Scope {
		scope = scope.Fork(defs...)
		return scope
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

	// The initial render presents the first frame; subsequent renders
	// happen only when a case below changes state, so a key press that
	// changes nothing skips the render entirely.
	taiui.Render(scope, screen, discardScreen{})
	for {
		select {
		case key := <-keyCh:
			changed, quit := handleKey(&scroll, &toggle, &w1Weight, &modal, key)
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
				taiui.Render(scope, screen, discardScreen{})
			}
		case <-tick.C:
			// The ball is derived from the frame counter: forking the frame
			// state rebuilds the framebuffer content declaratively.
			frame++
			scope = forkState(func() Frame { return Frame(frame) })
			taiui.Render(scope, screen, discardScreen{})
		case <-clock.C:
			now = time.Now()
			scope = forkState(func() Now { return Now(now) })
			taiui.Render(scope, screen, discardScreen{})
		case <-resizeCh:
			if ws, err := t.WindowSize(); err == nil && ws.Width > 0 && ws.Height > 0 {
				width, height = ws.Width, ws.Height
				screen.resize(width, height)
				// Both changed providers are forked in one layer, so the
				// scope stack stays flat.
				scope = forkState(
					func() Width { return Width(width) },
					func() Height { return Height(height) },
				)
				taiui.Render(scope, screen, discardScreen{})
			}
		case <-sigCh:
			return
		}
	}
}

// maxW1Weight bounds the w1 flex weight adjustable with the left and
// right arrow keys; the weight must stay positive for Weighted.
const maxW1Weight = 10

func handleKey(scroll *int, toggle *bool, w1Weight *int, modal *bool, key string) (changed []any, quit bool) {
	switch key {
	case "up":
		// The scroll offset never goes negative: the view clamps at the
		// content start.
		if *scroll > 0 {
			*scroll--
			changed = append(changed, func() Scroll { return Scroll(*scroll) })
		}
	case "down":
		*scroll++
		changed = append(changed, func() Scroll { return Scroll(*scroll) })
	case "left":
		// The w1 weight never drops below 1: Weighted requires a positive
		// weight, so the w1 box always keeps a share of the row.
		if *w1Weight > 1 {
			*w1Weight--
			changed = append(changed, func() W1Weight { return W1Weight(*w1Weight) })
		}
	case "right":
		if *w1Weight < maxW1Weight {
			*w1Weight++
			changed = append(changed, func() W1Weight { return W1Weight(*w1Weight) })
		}
	case "space":
		*toggle = !*toggle
		changed = append(changed, func() Toggle { return Toggle(*toggle) })
	case "modal":
		// The modal is part of the element tree, derived from state: an
		// Overlay stacks it over the main UI.
		*modal = !*modal
		changed = append(changed, func() Modal { return Modal(*modal) })
	case "quit":
		return nil, true
	}
	return changed, false
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
			}
			pending = pending[1:]
		}
	}
}
