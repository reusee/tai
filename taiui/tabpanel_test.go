package taiui

import (
	"strings"
	"testing"
)

func TestTabPanel(t *testing.T) {
	if TabPanel(Box{Top: 0, Left: 0, Bottom: 0, Right: 20}, 1, "Output", "Output", false, true, false, nil, ScrollState{}, PanelStyle{}) != nil {
		t.Fatal("a degenerate box must render no element")
	}

	var sb strings.Builder
	collapsed := TabPanel(
		Box{Top: 0, Left: 0, Bottom: 1, Right: 20},
		2, "Summary", "Summary",
		false, false, true,
		nil, ScrollState{}, PanelStyle{},
	)
	Render(collapsed, NewTerminalScreen(&sb, 20, 1))
	if !strings.Contains(sb.String(), "2 Summary") {
		t.Fatalf("expected the collapsed strip label, got %q", sb.String())
	}

	sb.Reset()
	expanded := TabPanel(
		Box{Top: 0, Left: 0, Bottom: 4, Right: 40},
		1, "Output", "Output (generating...)",
		true, true, false,
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
