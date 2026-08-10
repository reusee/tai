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

// discardScreen renders and discards: it shows that Render accepts any
// number of screens. A real application might present to several terminals.
type discardScreen struct{}

func (discardScreen) Width() int          { return 1 }
func (discardScreen) Height() int         { return 1 }
func (discardScreen) Present(taiui.Frame) {}

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

	// The framebuffer is data state: the demo mutates it on each animation
	// tick, and the next render reads the updated content purely.
	fb := taiui.NewFrameBufferContent(fbWidth, fbHeight)

	state := State{Width: width, Height: height, Scroll: 0, Toggle: true, Time: time.Now()}
	base := dscope.New(
		func() State { return state },
		func(s State) taiui.Root { return taiui.Root{Element: buildUI(s, fb)} },
	)
	scope := base

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

	for {
		taiui.Render(scope, screen, discardScreen{})
		select {
		case key := <-keyCh:
			switch key {
			case "up":
				state.Scroll--
			case "down":
				state.Scroll++
			case "space":
				state.Toggle = !state.Toggle
			case "quit":
				return
			}
		case <-tick.C:
			state.Frame++
			drawBall(fb, state.Frame)
		case <-clock.C:
			state.Time = time.Now()
		case <-resizeCh:
			if ws, err := t.WindowSize(); err == nil && ws.Width > 0 && ws.Height > 0 {
				width, height = ws.Width, ws.Height
				screen.resize(width, height)
				state.Width, state.Height = width, height
			}
		case <-sigCh:
			return
		}
		// Forking the base scope with the new State re-evaluates the Root
		// provider; the old scope is discarded, so the fork chain stays
		// shallow.
		scope = base.Fork(func() State { return state })
	}
}

func readKeys(r io.Reader, ch chan<- string) {
	var buf [1]byte
	for {
		n, err := r.Read(buf[:])
		if err != nil {
			return
		}
		if n == 0 {
			// The tty is in non-blocking raw mode; avoid a busy loop.
			time.Sleep(2 * time.Millisecond)
			continue
		}
		b := buf[0]
		if b == 0x1b {
			// Collect the two bytes that follow ESC. Arrow keys arrive as
			// ESC [ A/B/C/D; other sequences are ignored.
			seq := []byte{b}
			for len(seq) < 3 {
				n, err := r.Read(buf[:])
				if err != nil || n == 0 {
					break
				}
				seq = append(seq, buf[0])
			}
			if len(seq) == 3 && seq[1] == '[' {
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
			continue
		}
		switch b {
		case 'q', 'Q', 0x03:
			ch <- "quit"
		case ' ':
			ch <- "space"
		}
	}
}
