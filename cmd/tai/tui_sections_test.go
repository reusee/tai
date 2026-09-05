package main

import (
	"strings"
	"testing"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/taiui"
	"github.com/reusee/tai/tree"
)

func TestTUIOutputSectionsRecordAndMap(t *testing.T) {
	tu := &TUI{
		output: taiui.NewLineBuffer(0),
		tabs:   taiui.NewTabs(3),
	}
	tu.writeOutputPart(generators.RoleUser, taiui.NoColor, false, "hi\n")
	tu.mu.Lock()
	tu.pendingOwner = &outputSectionOwner{attempt: 2}
	tu.mu.Unlock()
	tu.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "model one\n")
	tu.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "more\n")
	tu.mu.Lock()
	tu.pendingOwner = &outputSectionOwner{attempt: 5}
	tu.mu.Unlock()
	tu.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "model two\n")

	if len(tu.outputSections) != 3 {
		t.Fatalf("sections %d, want 3", len(tu.outputSections))
	}
	if got := tu.eventSections[outputSectionOwner{attempt: 2}]; got != 1 {
		t.Fatalf("attempt 2 section %d, want 1", got)
	}
	if got := tu.eventSections[outputSectionOwner{attempt: 5}]; got != 2 {
		t.Fatalf("attempt 5 section %d, want 2", got)
	}
}

// TestTUIEventClickJumpsToOutputSection verifies that a press on the
// attempt-start node's jump marker jumps the Output tab to the section
// the attempt wrote, while presses off the marker and on other nodes
// never jump. See TheoryOfTUIOutputSections and TheoryOfTreeTab.
func TestTUIEventClickJumpsToOutputSection(t *testing.T) {
	tu := &TUI{
		output: taiui.NewLineBuffer(0),
		tabs:   taiui.NewTabs(3),
		width:  100,
		height: 40,
	}
	tu.tabs.FocusTab(0)
	tu.tabs.Toggle(1)
	tu.writeOutputPart(generators.RoleUser, taiui.NoColor, false, "hi\n")
	tu.mu.Lock()
	tu.pendingOwner = &outputSectionOwner{attempt: 1}
	tu.mu.Unlock()
	tu.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "attempt one\n")
	for i := 0; i < 40; i++ {
		tu.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "filler line\n")
	}
	// Build the tree the Tree tab renders: a loop branch carrying an
	// attempt-start node whose content names attempt 1, a usage node,
	// and a finish node. See TheoryOfTreeTab.
	tr, err := tree.New().WriteAll(
		tree.WriteOp{Parent: "root", Name: "loop-1", Type: tree.TypeLoop, Author: tree.AuthorProgram},
		tree.WriteOp{Parent: "loop-1", Name: "attempt-start-1", Type: tree.TypeEvent, Author: tree.AuthorProgram, Content: "attempt 1 start (1/3)"},
		tree.WriteOp{Parent: "loop-1", Name: "usage-1", Type: tree.TypeEvent, Author: tree.AuthorProgram, Content: "usage-unique"},
		tree.WriteOp{Parent: "loop-1", Name: "finish-1", Type: tree.TypeEvent, Author: tree.AuthorProgram, Content: "finish: stop"},
	)
	if err != nil {
		t.Fatal(err)
	}
	tu.setTree(tr)
	tu.eventSections = map[outputSectionOwner]int{{attempt: 1}: 1}

	boxes := tu.tabs.Boxes(tu.width, tu.height)
	box := boxes[1]
	// The content rows are indented past the status column, so the
	// display width and the press columns share the content origin.
	// See TheoryOfTreeTab.
	contentLeft := box.Left
	if box.Width() > controlColumnWidth {
		contentLeft += controlColumnWidth
	}
	display := tu.treeDisplay(treeContentWidth(tu.tabs.Expanded[1], box.Width()), panelStyle.BaseBG)
	row := -1
	for i, line := range display {
		if strings.Contains(line.Text, eventJumpMarker) {
			row = i
			break
		}
	}
	if row < 0 {
		t.Fatal("attempt-start row with the jump marker not found")
	}
	start, end, ok := markerColumnRange(display[row].Text, taiui.DisplayWidthOptions())
	if !ok || end <= start {
		t.Fatalf("marker column range not found in %q", display[row].Text)
	}
	y := box.Top + 1 + row
	if y >= box.Bottom {
		t.Fatalf("attempt-start row %d beyond the pane", y)
	}

	tu.mu.Lock()
	defer tu.mu.Unlock()

	// A press on the jump marker jumps the Output tab to the section
	// the attempt wrote.
	tu.treeAtClick(contentLeft+start, y)
	want := tu.outputSectionOffset(1)
	if want <= 0 {
		t.Fatal("section top must be below the content start")
	}
	if tu.scrolls[0].Offset != want {
		t.Fatalf("output offset %d, want %d", tu.scrolls[0].Offset, want)
	}
	if tu.scrolls[0].Follow {
		t.Fatal("jump must stop following the tail")
	}
	if tu.tabs.Focus != 0 || !tu.tabs.Expanded[0] {
		t.Fatal("jump must focus the expanded Output tab")
	}

	// A press off the marker and a press on another node never jump.
	tu.scrolls[0].Offset = 0
	tu.scrolls[0].Follow = true
	tu.treeAtClick(contentLeft+2, y)
	if tu.scrolls[0].Offset != 0 || !tu.scrolls[0].Follow {
		t.Fatal("a press off the marker must not jump")
	}
	usageRow := -1
	for i, line := range display {
		if strings.Contains(line.Text, "usage-unique") {
			usageRow = i
			break
		}
	}
	if usageRow < 0 {
		t.Fatal("usage row not found")
	}
	tu.treeAtClick(contentLeft+2, box.Top+1+usageRow)
	if tu.scrolls[0].Offset != 0 || !tu.scrolls[0].Follow {
		t.Fatal("a press on another node must not jump")
	}
}
