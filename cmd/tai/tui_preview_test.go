package main

import (
	"fmt"
	"testing"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/taiui"
)

// previewDisplayTexts extracts the texts of a wrapped display for
// assertions.
func previewDisplayTexts(display []taiui.Line) []string {
	texts := make([]string, len(display))
	for i, line := range display {
		texts[i] = line.Text
	}
	return texts
}

// assertPreviewTexts asserts the display's row count and texts.
func assertPreviewTexts(t *testing.T, display []taiui.Line, want ...string) {
	t.Helper()
	if len(display) != len(want) {
		t.Fatalf("expected %d display rows, got %d: %q", len(want), len(display), previewDisplayTexts(display))
	}
	for i, line := range display {
		if line.Text != want[i] {
			t.Fatalf("row %d: expected %q, got %q", i, want[i], line.Text)
		}
	}
}

// TestOutputPreviewCollapsesAllSections verifies the global preview:
// every section projects to its first source line, so the whole output
// structure fits the visible area, and leaving the preview restores
// every line. toggleOutputPreview locks the state itself, so the test
// calls it without holding mu. See TheoryOfOutputControls.
func TestOutputPreviewCollapsesAllSections(t *testing.T) {
	tui := newTUIForTest()
	box := taiui.Box{Top: 0, Left: 0, Bottom: 20, Right: 40}

	tui.writeOutputPart(generators.RoleUser, outputColorUserLine, false, "question\n")
	tui.writeOutputPart(generators.RoleModel, outputColorThoughtLine, true,
		"thought line one\nthought line two\nthought line three\n")
	tui.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "answer\n")

	tui.toggleOutputPreview()
	tui.mu.Lock()
	display := wrappedDisplay(tui, 0, box)
	tui.mu.Unlock()
	assertPreviewTexts(t, display, "question", "thought line one", "answer")
	if display[1].Color != outputColorThoughtLine {
		t.Fatalf("expected the thought color on the preview row, got %#x", display[1].Color)
	}

	// Leaving the preview restores every line of every section.
	tui.toggleOutputPreview()
	tui.mu.Lock()
	display = wrappedDisplay(tui, 0, box)
	tui.mu.Unlock()
	assertPreviewTexts(t, display,
		"question", "", "thought line one", "thought line two", "thought line three", "", "answer")
}

// TestOutputPreviewClickJumpsToSection verifies the preview's
// click-to-jump: a press on any preview row leaves the preview with
// the Output tab scrolled to the pressed section's full view, and the
// fold control is inert there. toggleOutputPreview locks the state
// itself; previewClickToSection and toggleControlAtClick expect the
// caller to hold mu. See TheoryOfOutputControls.
func TestOutputPreviewClickJumpsToSection(t *testing.T) {
	tui := newTUIForTest()
	tui.width, tui.height = 40, 10

	tui.writeOutputPart(generators.RoleUser, outputColorUserLine, false, "q\n")
	tui.writeOutputPart(generators.RoleModel, outputColorThoughtLine, true, "t1\nt2\nt3\nt4\nt5\nt6\n")
	tui.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "answer one\nanswer two\nanswer three\n")

	box := tui.tabs.Boxes(40, 10)[0]
	tui.toggleOutputPreview()
	tui.mu.Lock()
	if tui.toggleControlAtClick(box.Left, box.Top+1) {
		t.Fatal("the fold control must be inert in the preview")
	}
	tui.mu.Unlock()

	// A press on the second preview row jumps to section 1's full view:
	// pane rows start at box.Top+1, one preview row per section.
	tui.mu.Lock()
	ok := tui.previewClickToSection(box.Left+5, box.Top+2)
	inPreview := tui.outputPreview
	display := wrappedDisplay(tui, 0, box)
	expected := tui.outputSectionOffset(1)
	tui.mu.Unlock()
	if !ok || inPreview {
		t.Fatalf("expected the press to leave the preview, ok=%v preview=%v", ok, inPreview)
	}
	if tui.scrolls[0].Offset != expected {
		t.Fatalf("expected the scroll offset to anchor section 1 at %d, got %d", expected, tui.scrolls[0].Offset)
	}
	if display[expected].Text != "t1" {
		t.Fatalf("expected section 1's first row at the anchor, got %q", display[expected].Text)
	}
	if tui.scrolls[0].Follow {
		t.Fatal("expected following to stop after the jump")
	}
}

// TestOutputPreviewExitLandsOnTopSection verifies that leaving the
// preview lands the restored full view on the section at the top of
// the preview viewport, so the reading position carries over.
// toggleOutputPreview locks the state itself, so the test calls it
// without holding mu. See TheoryOfOutputControls.
func TestOutputPreviewExitLandsOnTopSection(t *testing.T) {
	tui := newTUIForTest()
	tui.width, tui.height = 40, 10
	box := tui.tabs.Boxes(40, 10)[0]

	for i := 0; i < 12; i++ {
		role, line := generators.RoleUser, outputColorUserLine
		if i%2 == 1 {
			role, line = generators.RoleModel, taiui.NoColor
		}
		tui.writeOutputPart(role, line, false, fmt.Sprintf("section %d header\nbody a\nbody b\n", i))
	}

	tui.toggleOutputPreview()
	tui.mu.Lock()
	// Build the preview projection, then scroll so section 4's row sits
	// at the pane top: every preview row is one section, so the offset
	// is the section index.
	wrappedDisplay(tui, 0, box)
	tui.scrolls[0].Offset = tui.outputSectionOffset(4)
	tui.mu.Unlock()
	tui.toggleOutputPreview()
	tui.mu.Lock()
	display := wrappedDisplay(tui, 0, box)
	expected := tui.outputSectionOffset(4)
	tui.mu.Unlock()
	if tui.scrolls[0].Offset != expected {
		t.Fatalf("expected the restored view to anchor section 4 at %d, got %d", expected, tui.scrolls[0].Offset)
	}
	if display[expected].Text != "section 4 header" {
		t.Fatalf("expected section 4's header at the anchor, got %q", display[expected].Text)
	}
}

// TestOutputPreviewKeyMapping verifies the p key binding.
func TestOutputPreviewKeyMapping(t *testing.T) {
	if got := mapTUIKey("p"); got != "preview" {
		t.Fatalf("expected p to map to preview, got %q", got)
	}
	if got := mapTUIKey("P"); got != "preview" {
		t.Fatalf("expected P to map to preview, got %q", got)
	}
}
