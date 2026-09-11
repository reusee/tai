package taiui

import (
	"bytes"
	"testing"

	"github.com/gdamore/tcell/v3/vt"
)

var testMenuEntries = []MenuEntry{
	{Title: "File", Items: []MenuItem{
		{Label: "New", Action: "new"},
		{Label: "Open file", Action: "open"},
	}},
	{Title: "View", Items: []MenuItem{
		{Label: "Zoom in", Action: "zoom-in"},
	}},
	{Title: "Quit", Action: "quit"},
}

// menuTestScreen records the frames Render presents, so tests inspect
// rendered cell styles.
type menuTestScreen struct {
	w, h   int
	frames []Frame
}

func (s *menuTestScreen) Width() int  { return s.w }
func (s *menuTestScreen) Height() int { return s.h }
func (s *menuTestScreen) Present(f Frame) {
	s.frames = append(s.frames, f)
}

// menuTestReversedRunes returns row y's runes whose cell style carries
// the reverse attribute.
func menuTestReversedRunes(f *Frame, y int) string {
	var out []rune
	for x := 0; x < f.Width; x++ {
		cell := f.Cells[y*f.Width+x]
		if cell.Set && cell.Style != nil && cell.Style.Attr()&vt.Reverse != 0 {
			out = append(out, cell.Rune)
		}
	}
	return string(out)
}

// TestMenuBarLayout pins the pure layout: ordered titles, hit-test
// agreement, gap and off-row inertness, narrow-width truncation, and
// the rendered titles. See TheoryOfMenu.
func TestMenuBarLayout(t *testing.T) {
	bar := NewMenuBar(testMenuEntries)
	slots := bar.Layout(80)
	want := []string{"File", "View", "Quit"}
	if len(slots) != len(want) {
		t.Fatalf("expected %d slots, got %d", len(want), len(slots))
	}
	for i, slot := range slots {
		if bar.Entries[slot.Index].Title != want[i] {
			t.Fatalf("slot %d is %q, want %q", i, bar.Entries[slot.Index].Title, want[i])
		}
		if slot.X1-slot.X0 != len(want[i]) {
			t.Fatalf("slot %d width is %d, want %d", i, slot.X1-slot.X0, len(want[i]))
		}
		index, ok := bar.Hit(1, 80, slot.X0, 0)
		if !ok || index != slot.Index {
			t.Fatalf("hit at %d resolved %d ok=%v, want %d", slot.X0, index, ok, slot.Index)
		}
		index, _ = bar.Hit(1, 80, slot.X1-1, 0)
		if index != slot.Index {
			t.Fatalf("hit at %d resolved %d, want %d", slot.X1-1, index, slot.Index)
		}
	}
	if _, ok := bar.Hit(1, 80, slots[0].X1, 0); ok {
		t.Fatal("the gap between titles is inert")
	}
	if _, ok := bar.Hit(1, 80, 0, 1); ok {
		t.Fatal("row 1 is not the menu bar")
	}
	if _, ok := bar.Hit(0, 80, 0, 0); ok {
		t.Fatal("no inset means no menu bar")
	}
	if len(bar.Layout(3)) != 0 {
		t.Fatal("a width narrower than the first title keeps no slot")
	}
	var buf bytes.Buffer
	Render(bar.Element(80, -1), NewTerminalScreen(&buf, 80, 10))
	for _, title := range want {
		if !bytes.Contains(buf.Bytes(), []byte(title)) {
			t.Fatalf("the menu bar must render %q, got: %q", title, buf.String())
		}
	}
}

// TestMenuBarPress pins the press state machine: a title press opens,
// switches, or closes the dropdown and arms or disarms; an item press
// runs its action and closes and disarms; a top-level entry press runs
// its action; a press on the side padding closes without an action; a
// press outside closes, disarms, and is not consumed. See TheoryOfMenu.
func TestMenuBarPress(t *testing.T) {
	bar := NewMenuBar(testMenuEntries)
	if _, consumed := bar.Press(1, 80, 24, 0, 0); !consumed || bar.Open != 0 || !bar.Armed {
		t.Fatalf("the File title press must open its menu and arm, got open=%d armed=%v consumed=%v", bar.Open, bar.Armed, consumed)
	}
	box, ok := bar.DropdownBox(80, 24)
	if !ok {
		t.Fatal("the File dropdown must have a box")
	}
	if box.Top != 1 || box.Height() != len(testMenuEntries[0].Items) {
		t.Fatalf("the dropdown must hold exactly the item rows below the bar, got top %d height %d", box.Top, box.Height())
	}
	if box.Width() != len("Open file")+MenuDropdownPadding*2 {
		t.Fatalf("the dropdown width is %d, want the item width plus two padding cells", box.Width())
	}
	if action, consumed := bar.Press(1, 80, 24, box.Left+MenuDropdownPadding, box.Top); !consumed || action != "new" {
		t.Fatalf("the first item press must run its action, got %q consumed=%v", action, consumed)
	}
	if bar.Open != -1 || bar.Armed {
		t.Fatal("an item press must close the menu and disarm")
	}

	bar.Press(1, 80, 24, 0, 0)
	bar.Press(1, 80, 24, 7, 0)
	if bar.Open != 1 {
		t.Fatalf("another title press must switch menus, got %d", bar.Open)
	}
	bar.Press(1, 80, 24, 7, 0)
	if bar.Open != -1 || bar.Armed {
		t.Fatal("pressing the open title again must close the menu and disarm")
	}

	bar.Press(1, 80, 24, 0, 0)
	if action, consumed := bar.Press(1, 80, 24, box.Left, box.Top); !consumed || action != "" {
		t.Fatalf("a press on the side padding must close the menu without an action, got %q consumed=%v", action, consumed)
	}

	bar.Press(1, 80, 24, 0, 0)
	if _, consumed := bar.Press(1, 80, 24, 5, box.Bottom); consumed {
		t.Fatal("a press below the dropdown is not consumed")
	}
	if bar.Open != -1 || bar.Armed {
		t.Fatal("a press outside must close the menu and disarm")
	}
}

// TestMenuBarTopLevelEntry pins the top-level entry: a press runs its
// action, closes, and disarms. See TheoryOfMenu.
func TestMenuBarTopLevelEntry(t *testing.T) {
	bar := NewMenuBar(testMenuEntries)
	slot := bar.Layout(80)[2]
	if action, consumed := bar.Press(1, 80, 24, slot.X0, 0); !consumed || action != "quit" {
		t.Fatalf("a top-level entry press must run its action, got %q consumed=%v", action, consumed)
	}
	if bar.Open != -1 || bar.Armed {
		t.Fatal("a top-level entry press must leave the bar closed and disarmed")
	}
}

// TestMenuBarMotion pins the armed hover mode: motion does nothing while
// unarmed, switches the open dropdown between category titles while
// armed, keeps it elsewhere, hides it over a top-level entry without
// disarming, and re-pops it when the pointer returns. Hover runs no
// action. See TheoryOfMenu.
func TestMenuBarMotion(t *testing.T) {
	bar := NewMenuBar(testMenuEntries)
	bar.Motion(1, 80, 7, 0)
	if bar.Open != -1 {
		t.Fatal("motion must not open a menu while the bar is unarmed")
	}
	bar.Press(1, 80, 24, 0, 0)
	bar.Motion(1, 80, 7, 0)
	if bar.Open != 1 {
		t.Fatalf("motion over another title must pop up that title's menu, got %d", bar.Open)
	}
	bar.Motion(1, 80, 5, 10)
	if bar.Open != 1 {
		t.Fatal("motion off the menu bar must keep the open menu")
	}
	bar.Motion(1, 80, 14, 0)
	if bar.Open != -1 {
		t.Fatal("motion over a top-level entry must hide the open menu")
	}
	if !bar.Armed {
		t.Fatal("hiding by hover must not disarm the bar")
	}
	// Hover only highlights: the top-level entry resolves as a pointer
	// target, and an action is reported only by Press. See
	// TheoryOfMenu.
	if got := bar.TitleAt(1, 80, 14, 0); got != 2 {
		t.Fatalf("the pointer on the top-level entry must resolve to it, got %d", got)
	}
	bar.Motion(1, 80, 7, 0)
	if bar.Open != 1 {
		t.Fatal("motion back to a category title must re-pop its menu")
	}
}

// TestMenuBarPointerTargets pins the hover targets: the title under the
// pointer, and the item under the pointer inside the dropdown's text
// columns only. See TheoryOfMenu.
func TestMenuBarPointerTargets(t *testing.T) {
	bar := NewMenuBar(testMenuEntries)
	if got := bar.TitleAt(1, 80, 0, 0); got != 0 {
		t.Fatalf("the pointer on the File title must resolve to it, got %d", got)
	}
	if got := bar.TitleAt(1, 80, 5, 0); got != -1 {
		t.Fatalf("the pointer in the gap must resolve to no title, got %d", got)
	}
	bar.Press(1, 80, 24, 0, 0)
	box, _ := bar.DropdownBox(80, 24)
	if got := bar.ItemAt(80, 24, box.Left+MenuDropdownPadding, box.Top); got != 0 {
		t.Fatalf("the pointer on the first item must resolve to row 0, got %d", got)
	}
	if got := bar.ItemAt(80, 24, box.Left, box.Top); got != -1 {
		t.Fatalf("the pointer on the side padding must resolve to no item, got %d", got)
	}
	if got := bar.ItemAt(80, 24, box.Left+MenuDropdownPadding, box.Bottom); got != -1 {
		t.Fatalf("the pointer below the items must resolve to no item, got %d", got)
	}
}

// TestMenuBarHighlight pins the rendered highlight: the hovered title
// and the hovered dropdown item render reversed, and no pointer
// position highlights nothing. See TheoryOfMenu.
func TestMenuBarHighlight(t *testing.T) {
	lastFrame := func(e Element) *Frame {
		screen := &menuTestScreen{w: 80, h: 24}
		Render(e, screen)
		return &screen.frames[len(screen.frames)-1]
	}

	bar := NewMenuBar(testMenuEntries)
	if got := menuTestReversedRunes(lastFrame(bar.Element(80, 0)), 0); got != "File" {
		t.Fatalf("the hovered title must render reversed, got %q", got)
	}
	if got := menuTestReversedRunes(lastFrame(bar.Element(80, -1)), 0); got != "" {
		t.Fatalf("no hover must highlight nothing, got %q", got)
	}
	bar.Press(1, 80, 24, 0, 0)
	if got := menuTestReversedRunes(lastFrame(bar.Dropdown(80, 24, 1, NoColor)), 2); got != "Open file" {
		t.Fatalf("the hovered item must render reversed, got %q", got)
	}
	if got := menuTestReversedRunes(lastFrame(bar.Dropdown(80, 24, -1, NoColor)), 1); got != "" {
		t.Fatalf("the unhovered item must stay plain, got %q", got)
	}
}

// TestMenuDropdownRendering pins the borderless dropdown element: the
// item labels render and no border glyph or title row appears. See
// TheoryOfMenu.
func TestMenuDropdownRendering(t *testing.T) {
	bar := NewMenuBar(testMenuEntries)
	bar.Press(1, 80, 24, 0, 0)
	var buf bytes.Buffer
	Render(bar.Dropdown(80, 24, -1, NoColor), NewTerminalScreen(&buf, 80, 24))
	out := buf.String()
	for _, want := range []string{"New", "Open file"} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Fatalf("the dropdown must render %q, got: %q", want, out)
		}
	}
	for _, glyph := range "┌┐└┘─│" {
		if bytes.ContainsRune(buf.Bytes(), glyph) {
			t.Fatalf("the dropdown must not draw the border glyph %q, got: %q", glyph, out)
		}
	}
	if bytes.Contains(buf.Bytes(), []byte("File")) {
		t.Fatal("the borderless dropdown carries no title row")
	}
}
