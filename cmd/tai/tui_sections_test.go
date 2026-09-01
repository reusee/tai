package main

import (
	"strings"
	"testing"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/taiui"
)

func TestTUIOutputSectionsRecordAndMap(t *testing.T) {
	tu := &TUI{
		output: taiui.NewLineBuffer(0),
		tabs:   taiui.NewTabs(3),
	}
	tu.writeOutputPart(generators.RoleUser, taiui.NoColor, false, "hi\n")
	tu.mu.Lock()
	tu.pendingOwner = &outputSectionOwner{run: 1, seq: 2}
	tu.mu.Unlock()
	tu.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "model one\n")
	tu.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "more\n")
	tu.mu.Lock()
	tu.pendingOwner = &outputSectionOwner{run: 1, seq: 5}
	tu.mu.Unlock()
	tu.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "model two\n")

	if len(tu.outputSections) != 3 {
		t.Fatalf("sections %d, want 3", len(tu.outputSections))
	}
	if got := tu.eventSections[outputSectionOwner{run: 1, seq: 2}]; got != 1 {
		t.Fatalf("event (1,2) section %d, want 1", got)
	}
	if got := tu.eventSections[outputSectionOwner{run: 1, seq: 5}]; got != 2 {
		t.Fatalf("event (1,5) section %d, want 2", got)
	}
}

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
	tu.pendingOwner = &outputSectionOwner{run: 1, seq: 2}
	tu.mu.Unlock()
	tu.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "attempt one\n")
	for i := 0; i < 40; i++ {
		tu.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "filler line\n")
	}
	tu.events.Add(taiui.EventNode{Run: 1, Seq: 1, Lines: []taiui.Line{{Text: "loop"}}})
	tu.events.Add(taiui.EventNode{Run: 1, Seq: 2, ParentSeq: 1, Lines: []taiui.Line{{Text: "attempt start"}}})
	tu.events.Add(taiui.EventNode{Run: 1, Seq: 3, ParentSeq: 2, Lines: []taiui.Line{{Text: "usage-unique"}}})
	tu.eventSections[outputSectionOwner{run: 1, seq: 2}] = 1

	boxes := tu.tabs.Boxes(tu.width, tu.height)
	display := tu.events.Display(max(boxes[1].Width()-1, 1), panelStyle.BaseBG)
	row := -1
	for i, line := range display {
		if strings.Contains(line.Text, "usage-unique") {
			row = i
			break
		}
	}
	if row < 0 {
		t.Fatal("usage row not found")
	}
	box := boxes[1]
	y := box.Top + 1 + row
	if y >= box.Bottom {
		t.Fatalf("usage row %d beyond the pane", y)
	}
	tu.jumpToEventAtClick(box.Left+2, y)

	want := tu.outputSectionDisplayTop(tu.outputSections[1].startLine, boxes[0])
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
}
