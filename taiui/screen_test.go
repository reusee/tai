// taiui/screen_test.go
package taiui

import (
	"strings"
	"testing"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3/vt"
)

func TestTerminalScreenSgrResetsState(t *testing.T) {
	// A style carrying only a background must clear attributes a previous
	// cell may have set: the SGR sequence starts with the reset parameter.
	// Without it, an underlined or overlined run would bleed into every
	// following cell that shares the same background.
	s := NewTerminalScreen(&strings.Builder{}, 80, 24)
	style := vt.BaseStyle.WithBg(HexColor(0x141414))
	seq := s.sgr(style)
	if !strings.HasPrefix(seq, "\x1b[0;") {
		t.Fatalf("expected reset prefix, got %q", seq)
	}
	if !strings.Contains(seq, "48") {
		t.Fatalf("expected background parameter, got %q", seq)
	}
}

func TestTerminalScreenSgrUnderlineColor(t *testing.T) {
	// A dashed underline with an underline color must emit both the
	// dashed parameter and the underline color parameter.
	s := NewTerminalScreen(&strings.Builder{}, 80, 24)
	style := vt.BaseStyle.
		WithAttr(vt.DashedUnderline).
		WithUc(HexColor(0x00ffff))
	seq := s.sgr(style)
	if !strings.Contains(seq, "4:5") {
		t.Fatalf("expected dashed underline parameter, got %q", seq)
	}
	if !strings.Contains(seq, "58") {
		t.Fatalf("expected underline color parameter, got %q", seq)
	}
}

func TestTerminalScreenSgrCache(t *testing.T) {
	// The SGR cache stores the computed sequence by its key, so a later
	// call with the same style returns the cached string without
	// rebuilding it.
	s := NewTerminalScreen(&strings.Builder{}, 80, 24)
	style := vt.BaseStyle.WithBg(HexColor(0x123456))
	seq1 := s.sgr(style)
	seq2 := s.sgr(style)
	if seq1 != seq2 {
		t.Fatalf("expected identical SGR for identical style, got %q and %q", seq1, seq2)
	}
	key := sgrKey{attr: style.Attr(), fg: style.Fg(), bg: style.Bg(), uc: style.Uc()}
	if cached, ok := s.sgrCache[key]; !ok || cached != seq1 {
		t.Fatalf("expected SGR cached for style, got %q", seq1)
	}
}

func TestTerminalScreenPaintRowBatchesMiddleUnsetRun(t *testing.T) {
	s := NewTerminalScreen(&strings.Builder{}, 80, 24)
	frame := &Frame{
		Width:  5,
		Height: 1,
		Cells:  make([]FrameCell, 5),
	}
	frame.Cells[0] = FrameCell{Rune: 'a', Style: vt.BaseStyle, Set: true}
	frame.Cells[4] = FrameCell{Rune: 'b', Style: vt.BaseStyle, Set: true}
	var sb strings.Builder
	s.paintRow(&sb, frame, 0, displaywidth.Options{})
	// The row is "a   b": the middle unset run (cells 1-3) is batched
	// into a single write of three spaces.
	if !strings.Contains(sb.String(), "   ") {
		t.Fatalf("expected batched spaces, got %q", sb.String())
	}
}

func TestTerminalScreenPaintRowTrailingUnsetErase(t *testing.T) {
	s := NewTerminalScreen(&strings.Builder{}, 80, 24)
	frame := &Frame{
		Width:  4,
		Height: 1,
		Cells:  make([]FrameCell, 4),
	}
	frame.Cells[0] = FrameCell{Rune: 'a', Style: vt.BaseStyle, Set: true}
	var sb strings.Builder
	s.paintRow(&sb, frame, 0, displaywidth.Options{})
	// The row is "a   ": the trailing unset run is erased to the line
	// end in a single write, so the cursor never wraps at the last
	// column.
	if !strings.Contains(sb.String(), "\x1b[K") {
		t.Fatalf("expected erase-to-end for trailing unset run, got %q", sb.String())
	}
}

func TestTerminalScreenPaintRowEmptyRowEraseLine(t *testing.T) {
	s := NewTerminalScreen(&strings.Builder{}, 80, 24)
	frame := &Frame{
		Width:  4,
		Height: 1,
		Cells:  make([]FrameCell, 4),
	}
	var sb strings.Builder
	s.paintRow(&sb, frame, 0, displaywidth.Options{})
	// An entirely unset row is cleared with the erase-line sequence in a
	// single write instead of one space per cell.
	if sb.String() != "\x1b[0m\x1b[2K" {
		t.Fatalf("expected erase-line for empty row, got %q", sb.String())
	}
}

func TestTerminalScreenPresentSkipsUnchangedFrame(t *testing.T) {
	var sb strings.Builder
	s := NewTerminalScreen(&sb, 80, 24)
	frame := Frame{Width: 80, Height: 24, Cells: make([]FrameCell, 80*24)}
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

func TestTerminalScreenPresentRepositionsCursorOnly(t *testing.T) {
	var sb strings.Builder
	s := NewTerminalScreen(&sb, 80, 24)
	frame := Frame{Width: 80, Height: 24, Cells: make([]FrameCell, 80*24)}
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

func TestTerminalScreenResize(t *testing.T) {
	var sb strings.Builder
	s := NewTerminalScreen(&sb, 80, 24)
	s.Resize(100, 30)
	if s.Width() != 100 || s.Height() != 30 {
		t.Fatalf("expected 100x30, got %dx%d", s.Width(), s.Height())
	}
	if !strings.Contains(sb.String(), "\x1b[2J") {
		t.Fatalf("expected clear on resize, got %q", sb.String())
	}
}

func TestTerminalScreenWriteCursorPos(t *testing.T) {
	var sb strings.Builder
	writeCursorPos(&sb, 5, 3)
	if sb.String() != "\x1b[4;6H" {
		t.Fatalf("expected cursor sequence, got %q", sb.String())
	}
}

func TestTerminalScreenRetainsFrame(t *testing.T) {
	var sb strings.Builder
	s := NewTerminalScreen(&sb, 80, 24)
	frame := Frame{Width: 80, Height: 24, Cells: make([]FrameCell, 80*24)}
	frame.Cells[0] = FrameCell{Rune: 'a', Style: vt.BaseStyle, Set: true}
	s.Present(frame)
	// The screen retains the presented frame for the next damage
	// comparison. It does not implement FrameReleaser, so the renderer
	// allocates a fresh frame per pass and never reuses the retained
	// frame's cells.
	if !s.last.Equal(frame) {
		t.Fatal("expected screen to retain the presented frame")
	}
	if _, ok := any(s).(FrameReleaser); ok {
		t.Fatal("expected TerminalScreen not to implement FrameReleaser")
	}
}

func TestDiscardScreenReleasesFrames(t *testing.T) {
	var _ FrameReleaser = DiscardScreen{}
}
