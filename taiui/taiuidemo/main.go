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

	// Each piece of state is an independent variable and an independent
	// provider, so forking one piece recomputes only the components that
	// depend on it.
	scroll := 0
	toggle := true
	w1Weight := 1
	frame := int64(0)
	now := time.Now()

	base := dscope.New(
		func() Width { return Width(width) },
		func() Height { return Height(height) },
		func() Scroll { return Scroll(scroll) },
		func() Toggle { return Toggle(toggle) },
		func() W1Weight { return W1Weight(w1Weight) },
		func() Frame { return Frame(frame) },
		func() Now { return Now(now) },
		func() *taiui.FrameBufferContent { return fb },
		provideHeader,
		provideFooter,
		providePanelText,
		providePanelScroll,
		providePanelBox,
		providePanelDynamic,
		rootProvider,
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
			changed, quit := handleKey(&scroll, &toggle, &w1Weight, key)
			if quit {
				return
			}
			// Fork only the providers whose state changed: dscope keeps the
			// cached results of the unchanged providers, so the next render
			// recomputes only the components that depend on the change.
			for _, provider := range changed {
				scope = scope.Fork(provider)
			}
		case <-tick.C:
			frame++
			drawBall(fb, frame)
			scope = scope.Fork(func() Frame { return Frame(frame) })
		case <-clock.C:
			now = time.Now()
			scope = scope.Fork(func() Now { return Now(now) })
		case <-resizeCh:
			if ws, err := t.WindowSize(); err == nil && ws.Width > 0 && ws.Height > 0 {
				width, height = ws.Width, ws.Height
				screen.resize(width, height)
				scope = scope.Fork(func() Width { return Width(width) })
				scope = scope.Fork(func() Height { return Height(height) })
			}
		case <-sigCh:
			return
		}
	}
}

// maxW1Weight bounds the w1 flex weight adjustable with the left and
// right arrow keys; the weight must stay positive for Weighted.
const maxW1Weight = 10

// handleKey applies one key event to the demo state. It returns the
// providers for the state pieces that changed, and whether the key
// requests quitting the demo.
func handleKey(scroll *int, toggle *bool, w1Weight *int, key string) (changed []any, quit bool) {
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
	case "quit":
		return nil, true
	}
	return changed, false
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
