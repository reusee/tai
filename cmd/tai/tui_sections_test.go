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
	tu.events.Add(taiui.EventNode{Run: 1, Seq: 2, ParentSeq: 1,
		Lines: []taiui.Line{{Text: "🚀 [loop 1 attempt 1 start] " + eventJumpMarker}}})
	tu.events.Add(taiui.EventNode{Run: 1, Seq: 3, ParentSeq: 2, Lines: []taiui.Line{{Text: "usage-unique"}}})
	tu.events.Add(taiui.EventNode{Run: 1, Seq: 4, ParentSeq: 2,
		Lines: []taiui.Line{{Text: "🏁 [Finish: stop]"}}})
	tu.eventSections[outputSectionOwner{run: 1, seq: 2}] = 1

	boxes := tu.tabs.Boxes(tu.width, tu.height)
	display := tu.events.Display(max(boxes[1].Width()-1, 1), panelStyle.BaseBG)
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
	box := boxes[1]
	y := box.Top + 1 + row
	if y >= box.Bottom {
		t.Fatalf("attempt-start row %d beyond the pane", y)
	}

	// A press on the jump marker jumps the Output tab to the section
	// the attempt wrote.
	tu.jumpToEventAtClick(box.Left+start, y)
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

	// A press off the marker, a press on a row without the marker, and
	// a press on the finish line never jump.
	tu.scrolls[0].Offset = 0
	tu.scrolls[0].Follow = true
	tu.jumpToEventAtClick(box.Left+2, y)
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
	tu.jumpToEventAtClick(box.Left+2, box.Top+1+usageRow)
	if tu.scrolls[0].Offset != 0 || !tu.scrolls[0].Follow {
		t.Fatal("a press on a row without the marker must not jump")
	}
	finishRow := -1
	for i, line := range display {
		if strings.Contains(line.Text, "[Finish: stop]") {
			finishRow = i
			break
		}
	}
	if finishRow < 0 {
		t.Fatal("finish row not found")
	}
	tu.jumpToEventAtClick(box.Left+2, box.Top+1+finishRow)
	if tu.scrolls[0].Offset != 0 || !tu.scrolls[0].Follow {
		t.Fatal("a press on the finish line must not jump")
	}
}
