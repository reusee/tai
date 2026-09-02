package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/clipperhouse/displaywidth"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/taiui"
)

// displayTexts extracts the text of each display line.
func displayTexts(lines []taiui.Line) []string {
	texts := make([]string, len(lines))
	for i, line := range lines {
		texts[i] = line.Text
	}
	return texts
}

// assertTexts fails the test when the display lines differ from want.
func assertTexts(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected display lines: got %q, want %q", got, want)
	}
}

// TestOutputSectionCollapseShowsFirstLine verifies the per-section
// projection: collapsing a section reduces it to its first source line
// while every other section renders in full. See TheoryOfOutputControls.
func TestOutputSectionCollapseShowsFirstLine(t *testing.T) {
	tui := newTUIForTest()
	box := taiui.Box{Top: 0, Left: 0, Bottom: 20, Right: 40}

	tui.writeOutputPart(generators.RoleUser, outputColorUserLine, false, "question\n")
	tui.writeOutputPart(generators.RoleModel, outputColorThoughtLine, true,
		"thought line one\nthought line two\nthought line three\n")
	tui.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "answer\n")

	tui.mu.Lock()
	tui.toggleOutputSectionLocked(1)
	display := wrappedDisplay(tui, 0, box)
	tui.mu.Unlock()
	assertTexts(t, displayTexts(display),
		"question", "", "thought line one", "answer")
	if display[2].Color != outputColorThoughtLine {
		t.Fatalf("expected the thought color on the collapsed row, got %#x", display[2].Color)
	}
}

// TestOutputSectionCollapseStreaming verifies that a collapsed section
// keeps exactly one row while output streams into it, hiding the
// trailing partial line under its row. See TheoryOfOutputControls.
func TestOutputSectionCollapseStreaming(t *testing.T) {
	tui := newTUIForTest()
	box := taiui.Box{Top: 0, Left: 0, Bottom: 20, Right: 40}
	tui.generating = true

	tui.writeOutputPart(generators.RoleModel, outputColorThoughtLine, true, "thinking first\n")
	tui.mu.Lock()
	tui.toggleOutputSectionLocked(0)
	tui.mu.Unlock()
	assertTexts(t, displayTexts(wrappedDisplay(tui, 0, box)), "thinking first")

	tui.writeOutputPart(generators.RoleModel, outputColorThoughtLine, true, "thinking second\n")
	assertTexts(t, displayTexts(wrappedDisplay(tui, 0, box)), "thinking first")

	// The trailing partial line stays hidden under the collapsed row.
	tui.writeOutputPart(generators.RoleModel, outputColorThoughtLine, true, "partial thi")
	assertTexts(t, displayTexts(wrappedDisplay(tui, 0, box)), "thinking first")
}

// TestOutputSectionTogglePreservesContent verifies that collapsing is a
// display projection: toggling back re-reveals every line and
// re-collapsing reproduces the same projection. See
// TheoryOfOutputControls.
func TestOutputSectionTogglePreservesContent(t *testing.T) {
	tui := newTUIForTest()
	box := taiui.Box{Top: 0, Left: 0, Bottom: 20, Right: 40}

	tui.writeOutputPart(generators.RoleUser, outputColorUserLine, false, "question\n")
	tui.writeOutputPart(generators.RoleModel, outputColorThoughtLine, true,
		"thought one\nthought two\n")
	tui.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "answer\n")

	tui.mu.Lock()
	tui.toggleOutputSectionLocked(1)
	collapsed := displayTexts(wrappedDisplay(tui, 0, box))
	tui.toggleOutputSectionLocked(1)
	expanded := displayTexts(wrappedDisplay(tui, 0, box))
	tui.toggleOutputSectionLocked(1)
	again := displayTexts(wrappedDisplay(tui, 0, box))
	tui.mu.Unlock()
	assertTexts(t, collapsed, "question", "", "thought one", "answer")
	assertTexts(t, expanded, "question", "", "thought one", "thought two", "", "answer")
	if !slices.Equal(again, collapsed) {
		t.Fatalf("re-collapsed display changed: got %q, want %q", again, collapsed)
	}
}

// TestOutputSectionWidthChangeRewraps verifies that a content-width
// change resets the projection and rewraps every row. See
// TheoryOfOutputControls.
func TestOutputSectionWidthChangeRewraps(t *testing.T) {
	tui := newTUIForTest()

	tui.writeOutputPart(generators.RoleModel, outputColorThoughtLine, true, "thought\n")
	tui.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "answer\n")
	tui.mu.Lock()
	tui.toggleOutputSectionLocked(0)
	narrow := displayTexts(wrappedDisplay(tui, 0, taiui.Box{Top: 0, Left: 0, Bottom: 20, Right: 40}))
	wide := displayTexts(wrappedDisplay(tui, 0, taiui.Box{Top: 0, Left: 0, Bottom: 20, Right: 80}))
	tui.mu.Unlock()
	if !slices.Equal(wide, narrow) {
		t.Fatalf("expected the same rows after the width change, got %q vs %q", wide, narrow)
	}
}

// TestOutputSectionCollapsedRowTruncatedToWidth verifies that a
// collapsed row never exceeds the content width. See
// TheoryOfOutputControls.
func TestOutputSectionCollapsedRowTruncatedToWidth(t *testing.T) {
	tui := newTUIForTest()
	box := taiui.Box{Top: 0, Left: 0, Bottom: 20, Right: 40}

	tui.writeOutputPart(generators.RoleModel, outputColorThoughtLine, true,
		strings.Repeat("x", 100)+"\n")
	tui.mu.Lock()
	tui.toggleOutputSectionLocked(0)
	display := wrappedDisplay(tui, 0, box)
	tui.mu.Unlock()
	if len(display) != 1 {
		t.Fatalf("expected one collapsed row, got %d: %v", len(display), display)
	}
	// The content width is the box minus the control column and the
	// scrollbar column: 40 - 2 - 1 = 37. See TheoryOfOutputControls.
	if width := displaywidth.String(display[0].Text); width > 37 {
		t.Fatalf("collapsed row too wide: %d columns", width)
	}
	if !strings.HasSuffix(display[0].Text, "…") {
		t.Fatalf("expected the truncation marker, got %q", display[0].Text)
	}
}

// TestSectionControlsGlyph verifies that the fold control's glyph
// follows the section's collapsed state. See TheoryOfOutputControls.
func TestSectionControlsGlyph(t *testing.T) {
	tui := newTUIForTest()
	tui.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "x\n")
	tui.mu.Lock()
	defer tui.mu.Unlock()
	if got := tui.sectionControls(0)[0].Glyph; got != sectionGlyphExpanded {
		t.Fatalf("expanded section glyph %q, want %q", got, sectionGlyphExpanded)
	}
	tui.toggleOutputSectionLocked(0)
	if got := tui.sectionControls(0)[0].Glyph; got != sectionGlyphCollapsed {
		t.Fatalf("collapsed section glyph %q, want %q", got, sectionGlyphCollapsed)
	}
}

func TestOutputControlRowsPinned(t *testing.T) {
	tui := newTUIForTest()
	box := taiui.Box{Top: 0, Left: 0, Bottom: 10, Right: 40}

	tui.writeOutputPart(generators.RoleUser, outputColorUserLine, false, "q\n")
	var b strings.Builder
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, "thought %02d\n", i)
	}
	tui.writeOutputPart(generators.RoleModel, outputColorThoughtLine, true, b.String())
	tui.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "answer\n")

	tui.mu.Lock()
	defer tui.mu.Unlock()
	display := wrappedDisplay(tui, 0, box)
	// The pane is 8 rows (the box minus the label strip and the input
	// bar row), so the viewport at offset 0 covers display rows 0..7.
	// The projection holds s0 (2 rows), s1 (30 rows), s2 (1 row);
	// s2's control row sits below the viewport.
	rows := tui.outputControlRows(box, display, 0)
	if len(rows) != 2 || rows[0].section != 0 || rows[0].row != box.Top+1 ||
		rows[1].section != 1 || rows[1].row != box.Top+3 {
		t.Fatalf("unexpected control rows at offset 0: %+v", rows)
	}

	// Scrolled past the first section: its control disappears and the
	// thought section's control pins to the pane top.
	rows = tui.outputControlRows(box, display, 5)
	if len(rows) != 1 || rows[0].section != 1 || rows[0].row != box.Top+1 {
		t.Fatalf("unexpected control rows at offset 5: %+v", rows)
	}

	// Near the tail both the thought section's last row and the answer
	// are visible; the two controls never share a row.
	rows = tui.outputControlRows(box, display, 31)
	if len(rows) != 2 || rows[0].section != 1 || rows[1].section != 2 ||
		rows[0].row == rows[1].row {
		t.Fatalf("unexpected control rows at offset 31: %+v", rows)
	}
}

func TestToggleControlAtClick(t *testing.T) {
	tui := newTUIForTest()
	tui.width, tui.height = 40, 10

	tui.writeOutputPart(generators.RoleUser, outputColorUserLine, false, "q\n")
	tui.writeOutputPart(generators.RoleModel, outputColorThoughtLine, true, "t1\nt2\n")
	tui.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "answer\n")

	box := tui.tabs.Boxes(40, 10)[0]
	tui.mu.Lock()
	display := wrappedDisplay(tui, 0, box)
	rows := tui.outputControlRows(box, display, 0)
	tui.mu.Unlock()
	// The pane is 6 rows (the box minus the label strip and the input
	// bar row); the projection holds 6 display rows — s0 2 rows, s1 3
	// rows, s2 1 row — with controls at pane rows 1, 3, and 6.
	if len(rows) != 3 {
		t.Fatalf("expected 3 control rows in the pane, got %+v", rows)
	}
	if rows[1].section != 1 || rows[1].row != 3 {
		t.Fatalf("unexpected second control row: %+v", rows[1])
	}

	// A press on section 1's control collapses it.
	tui.mu.Lock()
	ok := tui.toggleControlAtClick(box.Left, rows[1].row)
	collapsed := tui.outputSections[1].collapsed
	tui.mu.Unlock()
	if !ok || !collapsed {
		t.Fatalf("expected the press to collapse section 1, ok=%v collapsed=%v", ok, collapsed)
	}

	// A second press expands it again: the collapsed section still
	// keeps its control row, so the same press hits it again.
	tui.mu.Lock()
	ok = tui.toggleControlAtClick(box.Left, rows[1].row)
	collapsed = tui.outputSections[1].collapsed
	tui.mu.Unlock()
	if !ok || collapsed {
		t.Fatal("expected the second press to expand section 1")
	}

	// A press off the control column and a press on a row without a
	// control are no-ops.
	tui.mu.Lock()
	if tui.toggleControlAtClick(box.Left+5, rows[1].row) {
		t.Fatal("a press off the control column must not toggle")
	}
	if tui.toggleControlAtClick(box.Left, rows[1].row+1) {
		t.Fatal("a press on a row without a control must not toggle")
	}
	tui.mu.Unlock()
}

// TestOutputControlColumnBesideContent pins the control column's place
// in the layout: the column paints only the content rows, the title
// row spans the full tab width with a centered label, and the content
// is indented past the column. See TheoryOfOutputControls.
func TestOutputControlColumnBesideContent(t *testing.T) {
	tui := newTUIForTest()
	tui.tabs.Expanded = []bool{true, false, false}
	tui.tabs.HasContent = []bool{true, false, false}
	tui.tabs.Focus = 0
	tui.interactive = false
	tui.width, tui.height = 40, 10

	tui.writeOutputPart(generators.RoleUser, outputColorUserLine, false, "q\n")
	tui.writeOutputPart(generators.RoleModel, outputColorThoughtLine, true, "t1\nt2\n")
	tui.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "answer\n")

	box := tui.tabs.Boxes(40, 10)[0]
	tui.mu.Lock()
	display := wrappedDisplay(tui, 0, box)
	rows := tui.outputControlRows(box, display, 0)
	tui.mu.Unlock()

	screen := &panelTestScreen{width: 40, height: 10}
	taiui.Render(buildRoot(tui, 40, 10, [3][]taiui.Line{display, nil, nil}), screen)
	frame := screen.frames[len(screen.frames)-1]

	// The title row spans the full tab width: the 6-wide label centers
	// at column 17 in the 40-wide box, not inside a column-shifted
	// panel.
	if cell := frame.Cells[17]; cell.Rune != 'O' {
		t.Fatalf("expected the centered title 'O' at (17,0), got %q", string(cell.Rune))
	}
	// The control column covers the content rows only: the first
	// section's fold glyph sits in the column on its control row.
	if len(rows) == 0 {
		t.Fatal("expected control rows")
	}
	first := rows[0]
	want := []rune(sectionGlyphExpanded)[0]
	if cell := frame.Cells[first.row*frame.Width]; cell.Rune != want {
		t.Fatalf("expected the fold glyph at (%d,0), got %q", first.row, string(cell.Rune))
	}
	// The content is indented past the column: the first display line
	// starts at column 2 on the row below the title.
	if cell := frame.Cells[1*frame.Width+controlColumnWidth]; cell.Rune != 'q' {
		t.Fatalf("expected indented content at (2,1), got %q", string(cell.Rune))
	}
}

// TestControlStripText verifies the horizontal hover strip's layout:
// one Han-width slot per control. See TheoryOfOutputControls.
func TestControlStripText(t *testing.T) {
	strip := controlStripText([]outputControl{{Glyph: "▾"}, {Glyph: "✕"}})
	if strip != "▾ ✕ " {
		t.Fatalf("unexpected strip %q", strip)
	}
}
