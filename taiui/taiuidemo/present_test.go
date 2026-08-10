package main

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3/vt"
	"github.com/reusee/tai/taiui"
)

func TestSgrResetsState(t *testing.T) {
	// A style carrying only a background must clear attributes a previous
	// cell may have set: the SGR sequence starts with the reset parameter.
	// Without it, an underlined or overlined run would bleed into every
	// following cell that shares the same background.
	style := vt.BaseStyle.WithBg(taiui.HexColor(0x141414))
	seq := sgr(style)
	if !strings.HasPrefix(seq, "\x1b[0;") {
		t.Fatalf("expected reset prefix, got %q", seq)
	}
	if !strings.Contains(seq, "48") {
		t.Fatalf("expected background parameter, got %q", seq)
	}
}

func TestSgrUnderlineColor(t *testing.T) {
	// The demo styles a dashed underline with an underline color; the
	// presenter must emit it so the terminal renders the specified color.
	style := vt.BaseStyle.
		WithAttr(vt.DashedUnderline).
		WithUc(taiui.HexColor(0x00ffff))
	seq := sgr(style)
	if !strings.Contains(seq, "4:5") {
		t.Fatalf("expected dashed underline parameter, got %q", seq)
	}
	if !strings.Contains(seq, "58") {
		t.Fatalf("expected underline color parameter, got %q", seq)
	}
}

func TestSgrCache(t *testing.T) {
	// The SGR cache stores the computed sequence by its key, so a later
	// call with the same style returns the cached string without
	// rebuilding it.
	style := vt.BaseStyle.WithBg(taiui.HexColor(0x123456))
	seq1 := sgr(style)
	seq2 := sgr(style)
	if seq1 != seq2 {
		t.Fatalf("expected identical SGR for identical style, got %q and %q", seq1, seq2)
	}
	key := sgrKey{attr: style.Attr(), fg: style.Fg(), bg: style.Bg(), uc: style.Uc()}
	if cached, ok := sgrCache[key]; !ok || cached != seq1 {
		t.Fatalf("expected SGR cached for style, got %q", seq1)
	}
}

func TestHandleKeyScrollClamp(t *testing.T) {
	scroll := 0
	toggle := true
	w1Weight := 1
	modal := false
	rotation := 0
	changed, quit := handleKey(&scroll, &toggle, &w1Weight, &modal, &rotation, "up")
	if quit {
		t.Fatal("up must not quit the demo")
	}
	if scroll != 0 {
		t.Fatalf("expected scroll clamped at 0, got %d", scroll)
	}
	if len(changed) != 0 {
		t.Fatalf("expected no provider for clamped up, got %d", len(changed))
	}
	changed, quit = handleKey(&scroll, &toggle, &w1Weight, &modal, &rotation, "down")
	if quit {
		t.Fatal("down must not quit the demo")
	}
	if scroll != 1 {
		t.Fatalf("expected scroll 1 after down, got %d", scroll)
	}
	if len(changed) != 1 {
		t.Fatalf("expected one provider after down, got %d", len(changed))
	}
	changed, quit = handleKey(&scroll, &toggle, &w1Weight, &modal, &rotation, "space")
	if quit {
		t.Fatal("space must not quit the demo")
	}
	if toggle {
		t.Fatal("expected toggle flipped by space")
	}
	if len(changed) != 1 {
		t.Fatalf("expected one provider after space, got %d", len(changed))
	}
	_, quit = handleKey(&scroll, &toggle, &w1Weight, &modal, &rotation, "quit")
	if !quit {
		t.Fatal("quit must stop the demo")
	}
}

func TestHandleKeyW1Weight(t *testing.T) {
	scroll := 0
	toggle := true
	w1Weight := 1
	modal := false
	rotation := 0
	changed, quit := handleKey(&scroll, &toggle, &w1Weight, &modal, &rotation, "left")
	if quit {
		t.Fatal("left must not quit the demo")
	}
	if w1Weight != 1 {
		t.Fatalf("expected w1 weight clamped at 1, got %d", w1Weight)
	}
	if len(changed) != 0 {
		t.Fatalf("expected no provider for clamped left, got %d", len(changed))
	}
	changed, quit = handleKey(&scroll, &toggle, &w1Weight, &modal, &rotation, "right")
	if quit {
		t.Fatal("right must not quit the demo")
	}
	if w1Weight != 2 {
		t.Fatalf("expected w1 weight 2 after right, got %d", w1Weight)
	}
	if len(changed) != 1 {
		t.Fatalf("expected one provider after right, got %d", len(changed))
	}
	for i := 0; i < maxW1Weight; i++ {
		handleKey(&scroll, &toggle, &w1Weight, &modal, &rotation, "right")
	}
	if w1Weight != maxW1Weight {
		t.Fatalf("expected w1 weight clamped at %d, got %d", maxW1Weight, w1Weight)
	}
	changed, quit = handleKey(&scroll, &toggle, &w1Weight, &modal, &rotation, "right")
	if quit {
		t.Fatal("right must not quit the demo")
	}
	if len(changed) != 0 {
		t.Fatalf("expected no provider at upper clamp, got %d", len(changed))
	}
}

func TestHandleKeyModal(t *testing.T) {
	scroll := 0
	toggle := true
	w1Weight := 1
	modal := false
	rotation := 0
	changed, quit := handleKey(&scroll, &toggle, &w1Weight, &modal, &rotation, "modal")
	if quit {
		t.Fatal("modal must not quit the demo")
	}
	if !modal {
		t.Fatal("expected modal toggled by m")
	}
	if len(changed) != 1 {
		t.Fatalf("expected one provider after modal, got %d", len(changed))
	}
	changed, quit = handleKey(&scroll, &toggle, &w1Weight, &modal, &rotation, "modal")
	if quit {
		t.Fatal("modal must not quit the demo")
	}
	if modal {
		t.Fatal("expected modal toggled back by m")
	}
	if len(changed) != 1 {
		t.Fatalf("expected one provider after modal, got %d", len(changed))
	}
}

func TestHandleKeyRotation(t *testing.T) {
	scroll := 0
	toggle := true
	w1Weight := 1
	modal := false
	rotation := 0
	changed, quit := handleKey(&scroll, &toggle, &w1Weight, &modal, &rotation, "tab")
	if quit {
		t.Fatal("tab must not quit the demo")
	}
	if rotation != 1 {
		t.Fatalf("expected rotation 1 after tab, got %d", rotation)
	}
	if len(changed) != 1 {
		t.Fatalf("expected one provider after tab, got %d", len(changed))
	}
	for i := 0; i < 3; i++ {
		handleKey(&scroll, &toggle, &w1Weight, &modal, &rotation, "tab")
	}
	if rotation != 0 {
		t.Fatalf("expected rotation wrapped to 0, got %d", rotation)
	}
}

func TestRotatedPanelIndex(t *testing.T) {
	// Position p shows the panel originally at (p - rotation) mod 4, in
	// clockwise order: 0 top-left, 1 top-right, 2 bottom-right,
	// 3 bottom-left.
	cases := []struct{ p, rotation, want int }{
		{0, 0, 0}, {1, 0, 1}, {2, 0, 2}, {3, 0, 3},
		{0, 1, 3}, {1, 1, 0}, {2, 1, 1}, {3, 1, 2},
		{0, 2, 2}, {1, 2, 3}, {2, 2, 0}, {3, 2, 1},
		{0, 3, 1}, {1, 3, 2}, {2, 3, 3}, {3, 3, 0},
		{0, 4, 0}, // four presses return to the original arrangement
	}
	for _, c := range cases {
		if got := rotatedPanelIndex(c.p, c.rotation); got != c.want {
			t.Fatalf("rotatedPanelIndex(%d, %d) = %d, want %d", c.p, c.rotation, got, c.want)
		}
	}
}

func TestPaintRowBatchesMiddleUnsetRun(t *testing.T) {
	frame := &taiui.Frame{
		Width:  5,
		Height: 1,
		Cells:  make([]taiui.FrameCell, 5),
	}
	frame.Cells[0] = taiui.FrameCell{Rune: 'a', Style: vt.BaseStyle, Set: true}
	frame.Cells[4] = taiui.FrameCell{Rune: 'b', Style: vt.BaseStyle, Set: true}
	var sb strings.Builder
	paintRow(&sb, frame, 0, displaywidth.Options{})
	// The row is "a   b": the middle unset run (cells 1-3) is batched
	// into a single write of three spaces.
	if !strings.Contains(sb.String(), "   ") {
		t.Fatalf("expected batched spaces, got %q", sb.String())
	}
}

func TestPaintRowTrailingUnsetErase(t *testing.T) {
	frame := &taiui.Frame{
		Width:  4,
		Height: 1,
		Cells:  make([]taiui.FrameCell, 4),
	}
	frame.Cells[0] = taiui.FrameCell{Rune: 'a', Style: vt.BaseStyle, Set: true}
	var sb strings.Builder
	paintRow(&sb, frame, 0, displaywidth.Options{})
	// The row is "a   ": the trailing unset run is erased to the line
	// end in a single write, so the cursor never wraps at the last
	// column.
	if !strings.Contains(sb.String(), "\x1b[K") {
		t.Fatalf("expected erase-to-end for trailing unset run, got %q", sb.String())
	}
}

func TestPaintRowEmptyRowEraseLine(t *testing.T) {
	frame := &taiui.Frame{
		Width:  4,
		Height: 1,
		Cells:  make([]taiui.FrameCell, 4),
	}
	var sb strings.Builder
	paintRow(&sb, frame, 0, displaywidth.Options{})
	// An entirely unset row is cleared with the erase-line sequence in a
	// single write instead of one space per cell.
	if sb.String() != "\x1b[0m\x1b[2K" {
		t.Fatalf("expected erase-line for empty row, got %q", sb.String())
	}
}

func TestPresentSkipsUnchangedFrame(t *testing.T) {
	var sb strings.Builder
	s := &ansiScreen{w: &sb, width: 80, height: 24}
	frame := taiui.Frame{Width: 80, Height: 24, Cells: make([]taiui.FrameCell, 80*24)}
	s.Present(frame)
	if sb.Len() == 0 {
		t.Fatal("expected first present to write")
	}
	sb.Reset()
	s.Present(frame)
	if sb.Len() != 0 {
		t.Fatalf("expected unchanged frame skipped, got %q", sb.String())
	}
}

func TestPresentRepositionsCursorOnly(t *testing.T) {
	var sb strings.Builder
	s := &ansiScreen{w: &sb, width: 80, height: 24}
	frame := taiui.Frame{Width: 80, Height: 24, Cells: make([]taiui.FrameCell, 80*24)}
	s.Present(frame)
	sb.Reset()
	frame.CursorSet = true
	frame.CursorX = 5
	frame.CursorY = 3
	s.Present(frame)
	if !strings.Contains(sb.String(), "\x1b[4;6H") {
		t.Fatalf("expected cursor reposition, got %q", sb.String())
	}
}

func TestWriteCursorPos(t *testing.T) {
	var sb strings.Builder
	writeCursorPos(&sb, 5, 3)
	if sb.String() != "\x1b[4;6H" {
		t.Fatalf("expected cursor sequence, got %q", sb.String())
	}
}

func TestReadKeys(t *testing.T) {
	ch := make(chan string, 8)
	go readKeys(strings.NewReader("\x1b[Aqm \t\x1b[B"), ch)
	var got []string
	for len(got) < 6 {
		select {
		case k := <-ch:
			got = append(got, k)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for keys")
		}
	}
	want := []string{"up", "quit", "modal", "space", "tab", "down"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestReadKeysLoneEsc(t *testing.T) {
	ch := make(chan string, 8)
	go readKeys(strings.NewReader("\x1bq"), ch)
	// A lone ESC followed by a non-sequence byte is discarded: the
	// incomplete sequence never resolves to a key.
	select {
	case k := <-ch:
		t.Fatalf("expected no key, got %q", k)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestRuneWidthEnv(t *testing.T) {
	t.Setenv("RUNEWIDTH_EASTASIAN", "")
	if got := runeWidthEnv(); got != "EA=narrow" {
		t.Fatalf("expected EA=narrow, got %q", got)
	}
	t.Setenv("RUNEWIDTH_EASTASIAN", "1")
	if got := runeWidthEnv(); got != "EA=wide" {
		t.Fatalf("expected EA=wide, got %q", got)
	}
}

func TestBounce(t *testing.T) {
	// bounce maps a counter onto a 0..span sawtooth, so the ball appears
	// to bounce between the canvas edges.
	cases := []struct {
		v, want int
	}{
		{0, 0}, {1, 1}, {2, 2}, {3, 3}, {4, 2}, {5, 1}, {6, 0},
	}
	for _, c := range cases {
		if got := bounce(c.v, 3); got != c.want {
			t.Fatalf("bounce(%d, 3) = %d, want %d", c.v, got, c.want)
		}
	}
}

func TestAnsiScreenRetainsFrame(t *testing.T) {
	var sb strings.Builder
	s := &ansiScreen{w: &sb, width: 80, height: 24}
	frame := taiui.Frame{Width: 80, Height: 24, Cells: make([]taiui.FrameCell, 80*24)}
	frame.Cells[0] = taiui.FrameCell{Rune: 'a', Style: vt.BaseStyle, Set: true}
	s.Present(frame)
	// The screen retains the presented frame for the next damage
	// comparison. It does not implement FrameReleaser, so the renderer
	// allocates a fresh frame per pass and never reuses the retained
	// frame's cells.
	if !s.last.Equal(frame) {
		t.Fatal("expected screen to retain the presented frame")
	}
	if _, ok := any(s).(taiui.FrameReleaser); ok {
		t.Fatal("expected ansiScreen not to implement FrameReleaser")
	}
}

func TestDiscardScreenReleasesFrames(t *testing.T) {
	var _ taiui.FrameReleaser = discardScreen{}
}
