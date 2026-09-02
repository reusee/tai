package main

import (
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

// TestThoughtsCollapseClosedShowsFirstLine verifies the collapsed
// projection: non-thought sections render in full and a closed thought
// section renders as one row showing its first line. See
// TheoryOfTUIThoughtsCollapse.
func TestThoughtsCollapseClosedShowsFirstLine(t *testing.T) {
	tui := newTUIForTest()
	box := taiui.Box{Top: 0, Left: 0, Bottom: 20, Right: 40}

	tui.writeOutputPart(generators.RoleUser, outputColorUserLine, false, "question\n")
	tui.writeOutputPart(generators.RoleModel, outputColorThoughtLine, true,
		"thought line one\nthought line two\nthought line three\n")
	tui.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "answer\n")

	tui.thoughtsCollapsed = true
	display := wrappedDisplay(tui, 0, box)
	assertTexts(t, displayTexts(display),
		"question", "", "thought line one", "answer")
	if display[2].Color != outputColorThoughtLine {
		t.Fatalf("expected the thought color on the collapsed row, got %#x", display[2].Color)
	}
}

// TestThoughtsCollapseStreamingShowsNewestLine verifies that the
// collapsed row of a still-streaming thought section tracks the newest
// output line, including a trailing partial line, and freezes to the
// section's first line when the request ends. See
// TheoryOfTUIThoughtsCollapse.
func TestThoughtsCollapseStreamingShowsNewestLine(t *testing.T) {
	tui := newTUIForTest()
	box := taiui.Box{Top: 0, Left: 0, Bottom: 20, Right: 40}
	tui.generating = true
	tui.thoughtsCollapsed = true

	tui.writeOutputPart(generators.RoleModel, outputColorThoughtLine, true, "thinking first\n")
	assertTexts(t, displayTexts(wrappedDisplay(tui, 0, box)), "thinking first")

	tui.writeOutputPart(generators.RoleModel, outputColorThoughtLine, true, "thinking second\n")
	assertTexts(t, displayTexts(wrappedDisplay(tui, 0, box)), "thinking second")

	// A partial trailing line is the newest output line.
	tui.writeOutputPart(generators.RoleModel, outputColorThoughtLine, true, "partial thi")
	assertTexts(t, displayTexts(wrappedDisplay(tui, 0, box)), "partial thi")

	// When the request ends, the row freezes to the section's first line.
	tui.generating = false
	assertTexts(t, displayTexts(wrappedDisplay(tui, 0, box)), "thinking first")
}

// TestThoughtsCollapseFinalizesToFirstLine verifies that a thought
// section frozen by a newer section shows its first line even while the
// request is still in flight. See TheoryOfTUIThoughtsCollapse.
func TestThoughtsCollapseFinalizesToFirstLine(t *testing.T) {
	tui := newTUIForTest()
	box := taiui.Box{Top: 0, Left: 0, Bottom: 20, Right: 40}
	tui.generating = true
	tui.thoughtsCollapsed = true

	tui.writeOutputPart(generators.RoleModel, outputColorThoughtLine, true,
		"thinking first\nthinking second\n")
	assertTexts(t, displayTexts(wrappedDisplay(tui, 0, box)), "thinking second")

	tui.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "answer\n")
	assertTexts(t, displayTexts(wrappedDisplay(tui, 0, box)), "thinking first", "answer")
}

// TestThoughtsCollapseKeepsUnsectionedOutput verifies that lines
// appended outside any section — command output, stderr — render in
// full after a collapsed thought section. See
// TheoryOfTUIThoughtsCollapse.
func TestThoughtsCollapseKeepsUnsectionedOutput(t *testing.T) {
	tui := newTUIForTest()
	box := taiui.Box{Top: 0, Left: 0, Bottom: 20, Right: 40}
	tui.thoughtsCollapsed = true

	tui.writeOutputPart(generators.RoleModel, outputColorThoughtLine, true, "thoughts here\n")
	tui.generating = false
	tui.write([]byte("command output\n"))

	assertTexts(t, displayTexts(wrappedDisplay(tui, 0, box)), "thoughts here", "command output")
}

// TestThoughtsCollapseTogglePreservesContent verifies that collapsing
// is a display projection: toggling back to the expanded display shows
// every line, and collapsing again reproduces the same projection. See
// TheoryOfTUIThoughtsCollapse.
func TestThoughtsCollapseTogglePreservesContent(t *testing.T) {
	tui := newTUIForTest()
	box := taiui.Box{Top: 0, Left: 0, Bottom: 20, Right: 40}

	tui.writeOutputPart(generators.RoleUser, outputColorUserLine, false, "question\n")
	tui.writeOutputPart(generators.RoleModel, outputColorThoughtLine, true,
		"thought one\nthought two\n")
	tui.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "answer\n")

	tui.thoughtsCollapsed = true
	collapsed := displayTexts(wrappedDisplay(tui, 0, box))
	assertTexts(t, collapsed, "question", "", "thought one", "answer")

	tui.thoughtsCollapsed = false
	expanded := displayTexts(wrappedDisplay(tui, 0, box))
	assertTexts(t, expanded, "question", "", "thought one", "thought two", "", "answer")

	tui.thoughtsCollapsed = true
	again := displayTexts(wrappedDisplay(tui, 0, box))
	if !slices.Equal(again, collapsed) {
		t.Fatalf("re-collapsed display changed: got %q, want %q", again, collapsed)
	}
}

// TestThoughtsCollapseWidthChangeRewraps verifies that a content-width
// change resets the collapsed caches and rewraps the projection. See
// TheoryOfTUIThoughtsCollapse.
func TestThoughtsCollapseWidthChangeRewraps(t *testing.T) {
	tui := newTUIForTest()
	box := taiui.Box{Top: 0, Left: 0, Bottom: 20, Right: 40}

	tui.writeOutputPart(generators.RoleModel, outputColorThoughtLine, true, "thought\n")
	tui.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "answer\n")
	tui.thoughtsCollapsed = true

	narrow := displayTexts(wrappedDisplay(tui, 0, box))
	wide := displayTexts(wrappedDisplay(tui, 0, taiui.Box{Top: 0, Left: 0, Bottom: 20, Right: 80}))
	if !slices.Equal(wide, narrow) {
		t.Fatalf("expected the same rows after the width change, got %q vs %q", wide, narrow)
	}
}

// TestThoughtsCollapseRowTruncatedToWidth verifies that a collapsed row
// never exceeds the content width. See TheoryOfTUIThoughtsCollapse.
func TestThoughtsCollapseRowTruncatedToWidth(t *testing.T) {
	tui := newTUIForTest()
	box := taiui.Box{Top: 0, Left: 0, Bottom: 20, Right: 40}
	tui.thoughtsCollapsed = true

	tui.writeOutputPart(generators.RoleModel, outputColorThoughtLine, true,
		strings.Repeat("x", 100)+"\n")
	display := wrappedDisplay(tui, 0, box)
	if len(display) != 1 {
		t.Fatalf("expected one collapsed row, got %d: %v", len(display), display)
	}
	if width := displaywidth.String(display[0].Text); width > 39 {
		t.Fatalf("collapsed row too wide: %d columns", width)
	}
	if !strings.HasSuffix(display[0].Text, "…") {
		t.Fatalf("expected the truncation marker, got %q", display[0].Text)
	}
}

// TestThoughtsCollapseSectionJump verifies that showOutputSection lands
// on the section's collapsed row. See TheoryOfTUIThoughtsCollapse.
func TestThoughtsCollapseSectionJump(t *testing.T) {
	tui := newTUIForTest()
	// showOutputSection derives its box from the TUI's width and
	// height; set them so the boxes tile a real screen, and derive the
	// wrappedDisplay box from the same Boxes so both paths share one
	// content width.
	tui.width, tui.height = 40, 3
	box := tui.tabs.Boxes(40, 3)[0]

	tui.writeOutputPart(generators.RoleUser, outputColorUserLine, false, "question\n")
	tui.writeOutputPart(generators.RoleModel, outputColorThoughtLine, true,
		"thought one\nthought two\nthought three\n")
	tui.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "answer\n")
	tui.thoughtsCollapsed = true
	wrappedDisplay(tui, 0, box)

	tui.showOutputSection(1)
	if tui.scrolls[0].Offset != 2 {
		t.Fatalf("expected the thought row offset 2, got %d", tui.scrolls[0].Offset)
	}
	tui.showOutputSection(2)
	if tui.scrolls[0].Offset != 3 {
		t.Fatalf("expected the answer row offset 3, got %d", tui.scrolls[0].Offset)
	}
}
