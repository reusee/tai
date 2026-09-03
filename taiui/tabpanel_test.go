package taiui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3/vt"
)

func TestTabPanel(t *testing.T) {
	if TabPanel(Box{Top: 0, Left: 0, Bottom: 0, Right: 20}, "Output", "Output", false, true, false, false, nil, ScrollState{}, PanelStyle{}) != nil {
		t.Fatal("a degenerate box must render no element")
	}

	var sb strings.Builder
	collapsed := TabPanel(
		Box{Top: 0, Left: 0, Bottom: 1, Right: 20},
		"Summary", "Summary",
		false, false, true, false,
		nil, ScrollState{}, PanelStyle{},
	)
	Render(collapsed, NewTerminalScreen(&sb, 20, 1))
	if !strings.Contains(sb.String(), "Summary") {
		t.Fatalf("expected the collapsed strip label, got %q", sb.String())
	}

	sb.Reset()
	expanded := TabPanel(
		Box{Top: 0, Left: 0, Bottom: 4, Right: 40},
		"Output", "Output (generating...)",
		true, true, false, false,
		[]Line{{Text: "body"}}, ScrollState{}, PanelStyle{},
	)
	Render(expanded, NewTerminalScreen(&sb, 40, 4))
	out := sb.String()
	if !strings.Contains(out, "generating") {
		t.Fatalf("expected the expanded panel label, got %q", out)
	}
	if !strings.Contains(out, "body") {
		t.Fatalf("expected the panel content, got %q", out)
	}
}

// TestPanelTitleBlankStrikeThrough verifies the title rule: the blank
// part of an expanded title row and of a collapsed strip carries the
// strike-through attribute, and the label cells stay plain.
func TestPanelTitleBlankStrikeThrough(t *testing.T) {
	style := testPanelStyle()

	t.Run("ExpandedTitleRow", func(t *testing.T) {
		element := Panel(
			Box{Top: 0, Left: 0, Bottom: 2, Right: 20},
			"Tab", false, nil, 0, false, true, style,
		)
		screen := newFakeScreen(20, 2)
		Render(element, screen)
		if len(screen.frames) == 0 {
			t.Fatal("expected a rendered frame")
		}
		frame := screen.frames[len(screen.frames)-1]
		// "Tab" (3 cells) centers at column 8 in the 20-wide title row.
		for _, x := range []int{0, 5, 15} {
			if frame.Cells[x].Style.Attr()&vt.StrikeThrough == 0 {
				t.Fatalf("expected strike-through on blank title cell %d", x)
			}
		}
		for _, x := range []int{8, 9, 10} {
			if frame.Cells[x].Style.Attr()&vt.StrikeThrough != 0 {
				t.Fatalf("expected no strike-through on label cell %d", x)
			}
		}
	})

	t.Run("CollapsedStrip", func(t *testing.T) {
		element := CollapsedPanel(Box{Top: 0, Left: 0, Bottom: 1, Right: 12}, "Output", false, false, style)
		screen := newFakeScreen(12, 1)
		Render(element, screen)
		if len(screen.frames) == 0 {
			t.Fatal("expected a rendered frame")
		}
		frame := screen.frames[len(screen.frames)-1]
		// The 6-cell label centers at column 3 in the 12-wide strip.
		if frame.Cells[0].Style.Attr()&vt.StrikeThrough == 0 {
			t.Fatal("expected strike-through on the blank strip cell 0")
		}
		if frame.Cells[3].Style.Attr()&vt.StrikeThrough != 0 {
			t.Fatal("expected no strike-through on the strip label cell 3")
		}
	})
}

func TestPaneHeight(t *testing.T) {
	if got := PaneHeight(Box{Top: 0, Left: 0, Bottom: 8, Right: 10}); got != 7 {
		t.Fatalf("expected 7, got %d", got)
	}
	if got := PaneHeight(Box{Top: 0, Left: 0, Bottom: 1, Right: 10}); got != 1 {
		t.Fatalf("expected 1 for a one-row box, got %d", got)
	}
	if got := PaneHeight(Box{Top: 0, Left: 0, Bottom: 0, Right: 10}); got != 1 {
		t.Fatalf("expected 1 for a degenerate box, got %d", got)
	}
}
