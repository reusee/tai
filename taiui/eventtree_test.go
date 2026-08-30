package taiui

import (
	"strings"
	"testing"
	"time"
)

// TestEventTreeRendering verifies the tree rendering: a node that
// arrives before its parent heals under it, the depth-first display
// order is independent of arrival order, every display line carries two
// Han-character widths of indent per depth, consecutive nodes alternate
// the two background shades, and every node's first display line
// right-aligns its elapsed-time timer at the pane's right edge. See
// TheoryOfEventTree.
func TestEventTreeRendering(t *testing.T) {
	var tree EventTree
	// The pane is wide enough that every node line fits its
	// depth-adjusted wrap width even with the timer zone reserved, so
	// each node renders exactly one display line.
	contentWidth := 99
	base := HexColor(0x202020)

	// Arrival order: the finish node (seq 4) precedes its parent
	// (seq 2), which precedes the run root (seq 1); the usage node
	// (seq 3) arrives last. The healed forest must render in tree
	// order.
	tree.Add(EventNode{Run: 1, Seq: 4, ParentSeq: 2, Lines: []Line{{Text: "finish"}}})
	tree.Add(EventNode{Run: 1, Seq: 2, ParentSeq: 1, Lines: []Line{{Text: "attempt start"}}})
	tree.Add(EventNode{Run: 1, Seq: 1, Lines: []Line{{Text: "loop start"}}})
	tree.Add(EventNode{Run: 1, Seq: 3, ParentSeq: 2, Lines: []Line{{Text: "usage"}}})

	if len(tree.Roots) != 1 {
		t.Fatalf("expected one root after healing, got %d", len(tree.Roots))
	}
	root := tree.Roots[0]
	if root.Seq != 1 {
		t.Fatalf("expected the run root, got seq %d", root.Seq)
	}
	if len(root.Children) != 1 || root.Children[0].Seq != 2 {
		t.Fatalf("expected the attempt under the run root, got %+v", root.Children)
	}
	attempt := root.Children[0]
	if len(attempt.Children) != 2 || attempt.Children[0].Seq != 3 || attempt.Children[1].Seq != 4 {
		t.Fatalf("expected the attempt's children sorted by sequence, got %+v", attempt.Children)
	}

	display := tree.Display(contentWidth, base)
	// Every display line carries two Han-character widths of indent per
	// depth: the run root at depth 0, the attempt at depth 1, its
	// lifecycle nodes at depth 2.
	depths := []int{0, 1, 2, 2}
	wantOrder := []string{
		"loop start",
		"　　attempt start",
		"　　　　usage",
		"　　　　finish",
	}
	if len(display) != len(wantOrder) {
		t.Fatalf("expected %d display lines, got %d", len(wantOrder), len(display))
	}
	alt := AltBG(base)
	options := DisplayWidthOptions()
	for i, want := range wantOrder {
		if !strings.HasPrefix(display[i].Text, want) {
			t.Fatalf("line %d: expected prefix %q, got %q", i, want, display[i].Text)
		}
		// Every node carries a zero elapsed, so every timer reads +0:00
		// and right-aligns at the pane's right edge. See
		// TheoryOfEventTree.
		if !strings.HasSuffix(display[i].Text, "+0:00") {
			t.Fatalf("line %d: expected the +0:00 timer suffix, got %q", i, display[i].Text)
		}
		if width := options.String(display[i].Text); width != contentWidth {
			t.Fatalf("line %d: expected the timer right-aligned at width %d, got %d", i, contentWidth, width)
		}
		indent := strings.Repeat("　", eventIndentHanWidth*depths[i])
		if !strings.HasPrefix(display[i].Text, indent) || strings.HasPrefix(display[i].Text, indent+"　") {
			t.Fatalf("line %d: expected indent depth %d, got %q", i, depths[i], display[i].Text)
		}
		wantShade := base
		if i%2 == 1 {
			wantShade = alt
		}
		if display[i].BGColor != wantShade {
			t.Fatalf("line %d: expected shade %v, got %v", i, wantShade, display[i].BGColor)
		}
	}

	// A long body at depth 2 wraps within the width left by the
	// indent, and every wrapped line keeps the depth-2 indent.
	tree.Add(EventNode{Run: 1, Seq: 5, ParentSeq: 2, Lines: []Line{{Text: strings.Repeat("word ", 40)}}})
	display = tree.Display(contentWidth, base)
	if len(display) <= len(wantOrder)+1 {
		t.Fatalf("expected wrapped body lines, got %d", len(display))
	}
	for _, line := range display[len(wantOrder)+1:] {
		if !strings.HasPrefix(line.Text, strings.Repeat("　", 2*eventIndentHanWidth)) {
			t.Fatalf("expected the depth-2 indent on %q", line.Text)
		}
	}
}

// TestEventTreeElapsedTimer verifies the elapsed-time timer: each node
// right-aligns its recorded duration at the pane's right edge of its
// first display line, and a pane too narrow for the timer omits it.
// See TheoryOfEventTree.
func TestEventTreeElapsedTimer(t *testing.T) {
	var tree EventTree
	base := HexColor(0x202020)
	contentWidth := 79
	tree.Add(EventNode{Lines: []Line{{Text: "finish"}}, Elapsed: time.Minute})

	display := tree.Display(contentWidth, base)
	if len(display) != 1 {
		t.Fatalf("expected 1 display line, got %d", len(display))
	}
	if !strings.HasSuffix(display[0].Text, "+1:00") {
		t.Fatalf("expected the +1:00 timer suffix, got %q", display[0].Text)
	}
	if width := DisplayWidthOptions().String(display[0].Text); width != contentWidth {
		t.Fatalf("expected the timer right-aligned at width %d, got %d", contentWidth, width)
	}

	// A later node records a larger elapsed time, so the timer orders
	// nodes by their arrival.
	tree.Add(EventNode{Lines: []Line{{Text: "start"}}, Elapsed: 2 * time.Minute})
	display = tree.Display(contentWidth, base)
	if !strings.HasSuffix(display[len(display)-1].Text, "+2:00") {
		t.Fatalf("expected the +2:00 timer suffix on the later node, got %q", display[len(display)-1].Text)
	}

	// A pane too narrow for the timer zone omits the timer instead of
	// colliding with the text.
	for _, line := range tree.Display(6, base) {
		if strings.HasSuffix(line.Text, "+2:00") {
			t.Fatalf("expected no timer on a narrow pane, got %q", line.Text)
		}
	}

	// The stopwatch format carries hours beyond one.
	if got := formatElapsed(3723 * time.Second); got != "+1:02:03" {
		t.Fatalf("expected +1:02:03, got %q", got)
	}
}

// TestEventTreeAlternateBackgrounds verifies that consecutive nodes
// alternate the two log background shades by display order, with every
// display line of one node sharing one shade. See TheoryOfEventTree.
func TestEventTreeAlternateBackgrounds(t *testing.T) {
	var tree EventTree
	tree.Add(EventNode{Lines: []Line{{Text: "first"}}})
	tree.Add(EventNode{Lines: []Line{{Text: "second"}}})

	base := HexColor(0x202020)
	alt := AltBG(base)
	display := tree.Display(20, base)
	if len(display) != 2 {
		t.Fatalf("expected 2 display lines, got %d", len(display))
	}
	for i, want := range []Color{base, alt} {
		if display[i].BGColor != want {
			t.Fatalf("line %d: expected shade %#v, got %#v", i, want, display[i].BGColor)
		}
	}
}

// TestEventTreeCollapse verifies the collapse behavior: an expandable
// node collapses to its header plus an expand hint by default,
// ToggleLastExpanded toggles the last expandable node, and ToggleAtRow
// expands on any row of a collapsed node while collapsing only on the
// header row of an expanded node. See TheoryOfEventTree.
func TestEventTreeCollapse(t *testing.T) {
	var tree EventTree
	base := HexColor(0x202020)
	node := tree.Add(EventNode{
		Lines: []Line{
			{Text: "header"},
			{Text: "line1"},
			{Text: "line2"},
		},
		Expandable: true,
	})
	if node.Expanded {
		t.Fatal("expected a collapsed node by default")
	}
	collapsed := tree.Display(70, base)
	if len(collapsed) != 2 || !strings.Contains(collapsed[1].Text, "expand") {
		t.Fatalf("expected the collapsed header and hint, got %+v", collapsed)
	}

	// ToggleLastExpanded toggles the last expandable node.
	tree.ToggleLastExpanded()
	if !node.Expanded {
		t.Fatal("expected the node expanded after ToggleLastExpanded")
	}
	expanded := tree.Display(70, base)
	if len(expanded) != 3 || expanded[1].Text != "line1" {
		t.Fatalf("expected the full body after expanding, got %+v", expanded)
	}
	tree.ToggleLastExpanded()
	if node.Expanded {
		t.Fatal("expected the node collapsed after the second toggle")
	}

	// A press on any row of a collapsed node expands it; a press on the
	// header row of an expanded node collapses it; a press inside an
	// expanded body keeps it expanded; a press outside every recorded
	// range is a no-op.
	tree.Display(70, base)
	tree.ToggleAtRow(1)
	if !node.Expanded {
		t.Fatal("expected the press on the collapsed node to expand it")
	}
	tree.Display(70, base)
	tree.ToggleAtRow(2)
	if !node.Expanded {
		t.Fatal("expected the press inside the expanded body to keep it expanded")
	}
	tree.ToggleAtRow(0)
	if node.Expanded {
		t.Fatal("expected the press on the expanded header to collapse it")
	}
	tree.Display(70, base)
	tree.ToggleAtRow(99)
	if node.Expanded {
		t.Fatal("expected the node to stay collapsed on an outside press")
	}

	// Non-expandable nodes are never toggled.
	plain := tree.Add(EventNode{Lines: []Line{{Text: "plain"}}})
	if plain.Expandable {
		t.Fatal("expected the plain node to be non-expandable")
	}
}
