package taiui

import (
	"fmt"
	"testing"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3/color"
	"github.com/gdamore/tcell/v3/vt"
	"github.com/reusee/dscope"
)

type fakeScreen struct {
	width, height int
	frames        []Frame
}

func newFakeScreen(width, height int) *fakeScreen {
	return &fakeScreen{width: width, height: height}
}

func (s *fakeScreen) Width() int  { return s.width }
func (s *fakeScreen) Height() int { return s.height }

func (s *fakeScreen) Present(frame Frame) {
	s.frames = append(s.frames, frame)
}

func (s *fakeScreen) cell(x, y int) rune {
	frame := s.frames[len(s.frames)-1]
	return frame.Cells[y*frame.Width+x].Rune
}

func (s *fakeScreen) lastCell(x, y int) FrameCell {
	frame := s.frames[len(s.frames)-1]
	return frame.Cells[y*frame.Width+x]
}

func newRootScope(root Root) Scope {
	return dscope.New(func() Root { return root })
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRender(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(Fill(true), Text("Hello"))})
	Render(scope, screen)

	if r := screen.cell(0, 0); r != 'H' {
		t.Fatalf("expected 'H' at cell 0, got %v", r)
	}
}

func TestNestedRect(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Fill(true),
		Rect(
			Margin(2),
			Padding(1),
			Text("Nested"),
		),
	)})
	Render(scope, screen)

	// "Nested" should appear at row 3 (margin 2 + padding 1), col 3.
	if r := screen.cell(3, 3); r != 'N' {
		t.Fatalf("expected 'N' at row 3 col 3, got %v", r)
	}
}

func TestFrameBuffer(t *testing.T) {
	screen := newFakeScreen(80, 25)
	content := NewFrameBufferContent(10, 10)
	content.SetContent(0, 0, 'X', nil, vt.BaseStyle)
	scope := newRootScope(Root{Element: FrameBuffer(content)})
	Render(scope, screen)

	if r := screen.cell(0, 0); r != 'X' {
		t.Fatalf("expected 'X' at cell 0, got %v", r)
	}

	// Framebuffer content is state: mutating it and re-rendering yields the
	// updated UI without any element-update call.
	content.SetContent(0, 0, 'Y', nil, vt.BaseStyle)
	Render(scope, screen)

	if r := screen.cell(0, 0); r != 'Y' {
		t.Fatalf("expected 'Y' at cell 0 after content change, got %v", r)
	}
}

func TestFrameBufferContentBounds(t *testing.T) {
	content := NewFrameBufferContent(10, 10)
	// Writes outside the content bounds are ignored and must not corrupt
	// in-bounds cells.
	content.SetContent(-1, 0, 'X', nil, vt.BaseStyle)
	content.SetContent(0, -1, 'X', nil, vt.BaseStyle)
	content.SetContent(10, 0, 'X', nil, vt.BaseStyle)
	content.SetContent(0, 10, 'X', nil, vt.BaseStyle)

	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: FrameBuffer(content)})
	Render(scope, screen)

	// Every write was out of bounds, so all cells are blank.
	if r := screen.cell(0, 0); r != 0 {
		t.Fatalf("expected blank cell 0, got %v", r)
	}
	if r := screen.cell(0, 1); r != 0 {
		t.Fatalf("expected blank cell at row 1, got %v", r)
	}
}

func namedBox() Box { return Box{Top: 0, Left: 0, Bottom: 25, Right: 80} }

func TestNamedFunctionSpec(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(namedBox, Fill(true), Text("n"))})
	Render(scope, screen)

	if r := screen.cell(0, 0); r != 'n' {
		t.Fatalf("expected 'n' at cell 0, got %v", r)
	}
}

func TestStateUpdateViaFork(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := dscope.New(
		func() string { return "a" },
		func(s string) Root { return Root{Element: Text(s)} },
	)
	Render(scope, screen)

	if r := screen.cell(0, 0); r != 'a' {
		t.Fatalf("expected 'a' at cell 0, got %v", r)
	}

	// A state change is a scope fork: the root element re-evaluates and the
	// next render reflects the new state.
	scope = scope.Fork(func() string { return "b" })
	Render(scope, screen)

	if r := screen.cell(0, 0); r != 'b' {
		t.Fatalf("expected 'b' at cell 0 after fork, got %v", r)
	}
}

func TestRenderToMultipleScreens(t *testing.T) {
	screen1 := newFakeScreen(80, 25)
	screen2 := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Text("m")})
	Render(scope, screen1, screen2)

	for i, screen := range []*fakeScreen{screen1, screen2} {
		if r := screen.cell(0, 0); r != 'm' {
			t.Fatalf("expected 'm' at cell 0 on screen %d, got %v", i, r)
		}
	}
}

func TestVerticalScroll(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 2, Right: 80},
		VerticalScroll(Text("a", "b", "c"), 0),
	)})
	Render(scope, screen)

	if r := screen.cell(0, 0); r != 'a' {
		t.Fatalf("expected 'a' at cell 0, got %v", r)
	}
	if r := screen.cell(0, 1); r != ' ' {
		t.Fatalf("expected bottom crop indicator at row 1, got %v", r)
	}
}

func TestTextCombiningCluster(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Text("e\u0301x")})
	Render(scope, screen)

	// e + combining acute is one grapheme cluster: one cell carrying the
	// base rune and its combining rune, advancing by one column.
	cell := screen.lastCell(0, 0)
	if cell.Rune != 'e' {
		t.Fatalf("expected 'e' as cluster base, got %v", cell.Rune)
	}
	if !sameCombc(cell.Combc, []rune{'\u0301'}) {
		t.Fatalf("expected combining acute, got %v", cell.Combc)
	}
	if r := screen.cell(1, 0); r != 'x' {
		t.Fatalf("expected 'x' one column after the cluster, got %v", r)
	}
}

func TestTextZWJEmojiCluster(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Text("x\U0001F469\u200d\U0001F4BBy")})
	Render(scope, screen)

	// Woman + ZWJ + laptop is one grapheme cluster; the ZWJ and laptop are
	// combining runes of the cluster, and the cluster spans two columns.
	cell := screen.lastCell(1, 0)
	if cell.Rune != '\U0001F469' {
		t.Fatalf("expected woman emoji as cluster base, got %v", cell.Rune)
	}
	if !sameCombc(cell.Combc, []rune{'\u200d', '\U0001F4BB'}) {
		t.Fatalf("expected ZWJ and laptop combining runes, got %v", cell.Combc)
	}
	if r := screen.cell(2, 0); r != 0 {
		t.Fatalf("expected the wide cluster's second column blank, got %v", r)
	}
	if r := screen.cell(3, 0); r != 'y' {
		t.Fatalf("expected 'y' after the wide cluster, got %v", r)
	}
}

func TestAmbiguousRunewidthEnv(t *testing.T) {
	scope := newRootScope(Root{Element: Text("\u00A1x")})

	t.Setenv("RUNEWIDTH_EASTASIAN", "")
	s1 := newFakeScreen(80, 25)
	Render(scope, s1)
	if r := s1.cell(1, 0); r != 'x' {
		t.Fatalf("expected ambiguous rune narrow by default, got %v at col 1", r)
	}

	t.Setenv("RUNEWIDTH_EASTASIAN", "1")
	s2 := newFakeScreen(80, 25)
	Render(scope, s2)
	if r := s2.cell(1, 0); r != 0 {
		t.Fatalf("expected wide ambiguous rune to skip col 1, got %v", r)
	}
	if r := s2.cell(2, 0); r != 'x' {
		t.Fatalf("expected 'x' at col 2 with wide ambiguous rune, got %v", r)
	}
}

func TestFrameBufferCombc(t *testing.T) {
	content := NewFrameBufferContent(5, 5)
	content.SetContent(0, 0, 'e', []rune{'\u0301'}, vt.BaseStyle)
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: FrameBuffer(content)})
	Render(scope, screen)

	cell := screen.lastCell(0, 0)
	if cell.Rune != 'e' || !sameCombc(cell.Combc, []rune{'\u0301'}) {
		t.Fatalf("framebuffer dropped combining runes: %+v", cell)
	}
}

func TestFrameEqual(t *testing.T) {
	a := newFrame(2, 2)
	b := newFrame(2, 2)
	if !a.Equal(b) {
		t.Fatal("empty frames should be equal")
	}

	a.setCell(0, 0, 'x', nil, vt.BaseStyle)
	if a.Equal(b) {
		t.Fatal("frames with different set cells should differ")
	}
	b.setCell(0, 0, 'x', nil, vt.BaseStyle)
	if !a.Equal(b) {
		t.Fatal("frames with same set cells should be equal")
	}

	b.setCell(0, 0, 'x', []rune{'\u0301'}, vt.BaseStyle)
	if a.Equal(b) {
		t.Fatal("frames with different combining runes should differ")
	}
	b.setCell(0, 0, 'x', nil, vt.BaseStyle.WithAttr(vt.Bold))
	if a.Equal(b) {
		t.Fatal("frames with different styles should differ")
	}
}

func TestFrameDirty(t *testing.T) {
	a := newFrame(4, 3)
	b := newFrame(4, 3)
	if dirty := a.Dirty(b); len(dirty) != 0 {
		t.Fatalf("expected no dirty runs for identical frames, got %v", dirty)
	}

	b.setCell(1, 0, 'x', nil, vt.BaseStyle)
	dirty := a.Dirty(b)
	if len(dirty) != 1 || dirty[0] != (Box{Top: 0, Left: 1, Bottom: 1, Right: 2}) {
		t.Fatalf("expected one run at (1,0), got %v", dirty)
	}

	b.setCell(2, 0, 'y', nil, vt.BaseStyle)
	dirty = a.Dirty(b)
	if len(dirty) != 1 || dirty[0] != (Box{Top: 0, Left: 1, Bottom: 1, Right: 3}) {
		t.Fatalf("expected adjacent runs merged, got %v", dirty)
	}

	b.setCell(3, 1, 'z', nil, vt.BaseStyle)
	dirty = a.Dirty(b)
	if len(dirty) != 2 {
		t.Fatalf("expected two runs, got %v", dirty)
	}

	a = newFrame(3, 4)
	if dirty := a.Dirty(b); len(dirty) != 1 || dirty[0] != (Box{Top: 0, Left: 0, Bottom: 4, Right: 3}) {
		t.Fatalf("expected whole-frame run on size mismatch, got %v", dirty)
	}
}

func TestVerticalScrollCombc(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 4, Right: 80},
		VerticalScroll(Text("a", "e\u0301"), 0),
	)})
	Render(scope, screen)

	cell := screen.lastCell(0, 1)
	if cell.Rune != 'e' || !sameCombc(cell.Combc, []rune{'\u0301'}) {
		t.Fatalf("scroll dropped combining runes: %+v", cell)
	}
}

func TestWrapLine(t *testing.T) {
	options := displaywidth.Options{}
	if got := wrapLine("hello", 10, options); !sameStrings(got, []string{"hello"}) {
		t.Fatalf("short line: got %q", got)
	}
	if got := wrapLine("hello", 5, options); !sameStrings(got, []string{"hello"}) {
		t.Fatalf("exact fit: got %q", got)
	}
	if got := wrapLine("hello world", 8, options); !sameStrings(got, []string{"hello", "world"}) {
		t.Fatalf("space break: got %q", got)
	}
	if got := wrapLine("", 10, options); !sameStrings(got, []string{""}) {
		t.Fatalf("empty line: got %q", got)
	}
	if got := wrapLine("hello", 0, options); len(got) != 0 {
		t.Fatalf("zero width: got %q", got)
	}
}

func TestWrapLineCluster(t *testing.T) {
	options := displaywidth.Options{}
	// Wide clusters hard-break at cluster boundaries: cluster(2) + 'x'(1)
	// fits in 3 columns, then 'y' overflows to the next line.
	got := wrapLine("\U0001F469\u200d\U0001F4BBxy", 3, options)
	if !sameStrings(got, []string{"\U0001F469\u200d\U0001F4BBx", "y"}) {
		t.Fatalf("cluster break: got %q", got)
	}
	// A cluster wider than the box occupies its own line.
	got = wrapLine("\U0001F469\u200d\U0001F4BB", 1, options)
	if !sameStrings(got, []string{"\U0001F469\u200d\U0001F4BB"}) {
		t.Fatalf("wide cluster alone: got %q", got)
	}
}

func TestTextWrapRender(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 4, Right: 8},
		Text("one two three", Wrap(true)),
	)})
	Render(scope, screen)
	// "one two three" (13 wide) wraps in an 8-wide box: "one two" on row 0,
	// "three" on row 1.
	if r := screen.cell(0, 0); r != 'o' {
		t.Fatalf("expected 'o' at (0,0), got %v", r)
	}
	if r := screen.cell(4, 0); r != 't' {
		t.Fatalf("expected 't' at (4,0), got %v", r)
	}
	if r := screen.cell(0, 1); r != 't' {
		t.Fatalf("expected 't' at (0,1), got %v", r)
	}
}

func TestTextWrapHardBreak(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 4, Right: 4},
		Text("abcdefgh", Wrap(true)),
	)})
	Render(scope, screen)
	// "abcdefgh" is one 8-wide word in a 4-wide box: it hard-breaks into
	// "abcd" and "efgh".
	if r := screen.cell(0, 0); r != 'a' {
		t.Fatalf("expected 'a' at (0,0), got %v", r)
	}
	if r := screen.cell(3, 0); r != 'd' {
		t.Fatalf("expected 'd' at (3,0), got %v", r)
	}
	if r := screen.cell(0, 1); r != 'e' {
		t.Fatalf("expected 'e' at (0,1), got %v", r)
	}
	if r := screen.cell(3, 1); r != 'h' {
		t.Fatalf("expected 'h' at (3,1), got %v", r)
	}
}

func TestTextFillAligned(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Text("ab", AlignRight, Fill(true))})
	Render(scope, screen)
	// Right-aligned "ab" sits at cols 78-79; fill paints the whole line,
	// including the leading gap.
	if cell := screen.lastCell(0, 0); !cell.Set || cell.Rune != ' ' {
		t.Fatalf("expected filled leading gap at (0,0), got %+v", cell)
	}
	if r := screen.cell(78, 0); r != 'a' {
		t.Fatalf("expected 'a' at (78,0), got %v", r)
	}
	if r := screen.cell(79, 0); r != 'b' {
		t.Fatalf("expected 'b' at (79,0), got %v", r)
	}

	screen2 := newFakeScreen(80, 25)
	scope2 := newRootScope(Root{Element: Text("ab", AlignCenter, Fill(true))})
	Render(scope2, screen2)
	// Center-aligned "ab" sits at cols 39-40; both gaps are filled.
	if cell := screen2.lastCell(38, 0); !cell.Set || cell.Rune != ' ' {
		t.Fatalf("expected filled leading gap at (38,0), got %+v", cell)
	}
	if r := screen2.cell(39, 0); r != 'a' {
		t.Fatalf("expected 'a' at (39,0), got %v", r)
	}
	if r := screen2.cell(40, 0); r != 'b' {
		t.Fatalf("expected 'b' at (40,0), got %v", r)
	}
	if cell := screen2.lastCell(41, 0); !cell.Set || cell.Rune != ' ' {
		t.Fatalf("expected filled trailing gap at (41,0), got %+v", cell)
	}
}

func TestTextClusterClip(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 2, Right: 2},
		Text("a\U0001F469\u200d\U0001F4BB"),
	)})
	Render(scope, screen)
	// 'a' fits at col 0; the wide cluster needs cols 1-2 but only col 1
	// remains, so it is clipped: text never spills past its box.
	if r := screen.cell(0, 0); r != 'a' {
		t.Fatalf("expected 'a' at (0,0), got %v", r)
	}
	if r := screen.cell(1, 0); r != 0 {
		t.Fatalf("expected clipped wide cluster, got %v", r)
	}
}

func TestFlexColumn(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Column(
		Rect(Fill(true), Text("a")),
		Rect(Fill(true), Text("b")),
	)})
	Render(scope, screen)
	// Two equal children split the 25-row box: the first occupies rows
	// 0..11, the second rows 12..24.
	if r := screen.cell(0, 0); r != 'a' {
		t.Fatalf("expected 'a' at (0,0), got %v", r)
	}
	if r := screen.cell(0, 11); r != ' ' {
		t.Fatalf("expected filled ' ' at (0,11), got %v", r)
	}
	if r := screen.cell(0, 12); r != 'b' {
		t.Fatalf("expected 'b' at (0,12), got %v", r)
	}
}

func TestFlexRow(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Row(
		Rect(Fill(true), Text("a")),
		Rect(Fill(true), Text("b")),
	)})
	Render(scope, screen)
	if r := screen.cell(0, 0); r != 'a' {
		t.Fatalf("expected 'a' at (0,0), got %v", r)
	}
	if r := screen.cell(39, 0); r != ' ' {
		t.Fatalf("expected filled ' ' at (39,0), got %v", r)
	}
	if r := screen.cell(40, 0); r != 'b' {
		t.Fatalf("expected 'b' at (40,0), got %v", r)
	}
}

func TestFlexWeighted(t *testing.T) {
	screen := newFakeScreen(90, 25)
	scope := newRootScope(Root{Element: Row(
		Rect(Fill(true), Text("a")),
		Weighted(2, Rect(Fill(true), Text("b"))),
	)})
	Render(scope, screen)
	// Weights 1 and 2 divide the 90 columns into 30 and 60.
	if r := screen.cell(0, 0); r != 'a' {
		t.Fatalf("expected 'a' at (0,0), got %v", r)
	}
	if r := screen.cell(29, 0); r != ' ' {
		t.Fatalf("expected filled ' ' at (29,0), got %v", r)
	}
	if r := screen.cell(30, 0); r != 'b' {
		t.Fatalf("expected 'b' at (30,0), got %v", r)
	}
}

func TestFlexRounding(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Row(
		Rect(Fill(true), Text("a")),
		Rect(Fill(true), Text("b")),
		Rect(Fill(true), Text("c")),
	)})
	Render(scope, screen)
	// 80 / 3 = 26, so the first and second children get 26 columns and
	// the last child absorbs the remaining 28.
	if r := screen.cell(0, 0); r != 'a' {
		t.Fatalf("expected 'a' at (0,0), got %v", r)
	}
	if r := screen.cell(25, 0); r != ' ' {
		t.Fatalf("expected filled ' ' at (25,0), got %v", r)
	}
	if r := screen.cell(26, 0); r != 'b' {
		t.Fatalf("expected 'b' at (26,0), got %v", r)
	}
	if r := screen.cell(52, 0); r != 'c' {
		t.Fatalf("expected 'c' at (52,0), got %v", r)
	}
}

func TestFlexBoxModel(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Row(
		Margin(1),
		Padding(1),
		Fill(true),
		Rect(Fill(true), Text("a")),
		Rect(Fill(true), Text("b")),
	)})
	Render(scope, screen)
	// The Row's margin and padding shrink the content area to x 2..77.
	// The children split it evenly: first covers x 2..39, second x 40..77;
	// the padding ring is filled, the outer margin stays unset.
	if r := screen.cell(2, 2); r != 'a' {
		t.Fatalf("expected 'a' at (2,2), got %v", r)
	}
	if r := screen.cell(40, 2); r != 'b' {
		t.Fatalf("expected 'b' at (40,2), got %v", r)
	}
	if r := screen.cell(1, 1); r != ' ' {
		t.Fatalf("expected filled padding ring at (1,1), got %v", r)
	}
	if r := screen.cell(0, 0); r != 0 {
		t.Fatalf("expected unset outer margin at (0,0), got %v", r)
	}
}

func TestFlexFillNegativeMargin(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 6, Right: 6},
		Margin(1),
		Padding(1),
		Fill(true),
		Row(
			Margin(-1),
			Padding(1),
			Fill(true),
			FGColor(HexColor(0xff0000)),
			Rect(Fill(true), Text("a")),
		),
	)})
	Render(scope, screen)
	// The Row's negative margin pushes its outer box past its own box;
	// its fill must clip to its box, so the Rect's padding ring keeps the
	// Rect's fill style instead of the Row's red background.
	if r := screen.cell(2, 2); r != 'a' {
		t.Fatalf("expected 'a' at (2,2), got %v", r)
	}
	if r, _, _ := screen.lastCell(1, 1).Style.Fg().RGB(); r == 0xff {
		t.Fatal("row fill leaked past its box")
	}
}

func TestStyleHelpers(t *testing.T) {
	if style := SameStyle.SetItalic(true)(vt.BaseStyle); style.Attr()&vt.Italic == 0 {
		t.Fatal("SetItalic(true) did not set italic")
	}
	if style := SameStyle.SetStrikeThrough(true)(vt.BaseStyle); style.Attr()&vt.StrikeThrough == 0 {
		t.Fatal("SetStrikeThrough(true) did not set strike-through")
	}
	if style := SameStyle.SetDim(true)(vt.BaseStyle); style.Attr()&vt.Dim == 0 {
		t.Fatal("SetDim(true) did not set dim")
	}
	if style := SameStyle.SetReverse(true)(vt.BaseStyle); style.Attr()&vt.Reverse == 0 {
		t.Fatal("SetReverse(true) did not set reverse")
	}
	if style := SameStyle.SetBlink(true)(vt.BaseStyle); style.Attr()&vt.Blink == 0 {
		t.Fatal("SetBlink(true) did not set blink")
	}
	if style := SameStyle.SetOverline(true)(vt.BaseStyle); style.Attr()&vt.Overline == 0 {
		t.Fatal("SetOverline(true) did not set overline")
	}
	if style := SameStyle.SetBold(true).SetBold(false)(vt.BaseStyle); style.Attr()&vt.Bold != 0 {
		t.Fatal("SetBold(false) did not clear bold")
	}
}

func TestStyleSpecsRender(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Text("a", Italic(true), Reverse(true))})
	Render(scope, screen)
	cell := screen.lastCell(0, 0)
	if cell.Style.Attr()&vt.Italic == 0 {
		t.Fatal("Italic(true) spec had no effect")
	}
	if cell.Style.Attr()&vt.Reverse == 0 {
		t.Fatal("Reverse(true) spec had no effect")
	}
}

func TestVerticalScrollEndClamp(t *testing.T) {
	screen := newFakeScreen(80, 25)
	var lines []string
	for i := 1; i <= 19; i++ {
		lines = append(lines, fmt.Sprintf("line %02d", i))
	}
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 4, Right: 80},
		VerticalScroll(Text(lines), 1000),
	)})
	Render(scope, screen)
	// The view clamps to the content end: rows show lines 16..19. The top
	// crop indicator " 15.. " covers the first columns of row 0.
	if r := screen.cell(0, 0); r != ' ' {
		t.Fatalf("expected top crop indicator at (0,0), got %v", r)
	}
	if r := screen.cell(6, 0); r != '6' {
		t.Fatalf("expected line 16 visible after the crop indicator, got %v", r)
	}
	if r := screen.cell(0, 3); r != 'l' {
		t.Fatalf("expected line 19 at row 3, got %v", r)
	}
	if r := screen.cell(6, 3); r != '9' {
		t.Fatalf("expected line 19 at (6,3), got %v", r)
	}
}

func TestVerticalScrollScrollbar(t *testing.T) {
	screen := newFakeScreen(80, 25)
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, fmt.Sprintf("s%02d", i))
	}
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 10, Right: 80},
		VerticalScroll(Text(lines), 0, Scrollbar(true)),
	)})
	Render(scope, screen)
	if r := screen.cell(79, 0); r != '█' {
		t.Fatalf("expected scrollbar thumb at (79,0), got %v", r)
	}
}

func TestVerticalScrollNoScrollbarWhenFits(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 10, Right: 80},
		VerticalScroll(Text("a", "b", "c"), 0, Scrollbar(true)),
	)})
	Render(scope, screen)
	if r := screen.cell(79, 0); r != 0 {
		t.Fatalf("expected no scrollbar thumb when content fits, got %v", r)
	}
}

func TestVerticalScrollBoxSpec(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: VerticalScroll(
		Text("a", "b", "c"),
		0,
		Box{Top: 0, Left: 0, Bottom: 3, Right: 80},
	)})
	Render(scope, screen)
	// The Box override constrains the scroll view to 3 rows.
	if r := screen.cell(0, 0); r != 'a' {
		t.Fatalf("expected 'a' at (0,0), got %v", r)
	}
	if r := screen.cell(0, 2); r != 'c' {
		t.Fatalf("expected 'c' at (0,2), got %v", r)
	}
	if r := screen.cell(0, 3); r != 0 {
		t.Fatalf("expected no content at (0,3), got %v", r)
	}
}

func TestVerticalScrollStyleSpec(t *testing.T) {
	screen := newFakeScreen(80, 25)
	var lines []string
	for i := 1; i <= 19; i++ {
		lines = append(lines, fmt.Sprintf("line %02d", i))
	}
	scope := newRootScope(Root{Element: VerticalScroll(
		Text(lines),
		1000,
		Box{Top: 0, Left: 0, Bottom: 4, Right: 80},
		FGColor(HexColor(0xff0000)),
	)})
	Render(scope, screen)
	// The style chain applies to the scroll content.
	if r, g, b := screen.lastCell(0, 0).Style.Fg().RGB(); !(r == 0xff && g == 0 && b == 0) {
		t.Fatalf("expected red content, got %#x %#x %#x", r, g, b)
	}
}

func TestVerticalScrollFill(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: VerticalScroll(
		Text("a", "b", "c"),
		0,
		Box{Top: 0, Left: 0, Bottom: 10, Right: 80},
		Fill(true),
	)})
	Render(scope, screen)
	// Fill paints the visible window cells the content does not occupy.
	if r := screen.cell(0, 0); r != 'a' {
		t.Fatalf("expected 'a' at (0,0), got %v", r)
	}
	if cell := screen.lastCell(1, 0); !cell.Set || cell.Rune != ' ' {
		t.Fatalf("expected filled ' ' at (1,0), got %+v", cell)
	}
	if cell := screen.lastCell(0, 9); !cell.Set || cell.Rune != ' ' {
		t.Fatalf("expected filled ' ' at (0,9), got %+v", cell)
	}
}

func TestVerticalScrollFillWideCluster(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: VerticalScroll(
		Text("\U0001F469\u200d\U0001F4BB"),
		0,
		Box{Top: 0, Left: 0, Bottom: 4, Right: 80},
		Fill(true),
	)})
	Render(scope, screen)
	// The wide cluster occupies two columns; fill must not paint the
	// trailing column.
	if cell := screen.lastCell(1, 0); cell.Set {
		t.Fatal("fill painted over the wide cluster's trailing column")
	}
	if cell := screen.lastCell(0, 0); cell.Rune != '\U0001F469' {
		t.Fatalf("expected woman emoji as cluster base, got %v", cell.Rune)
	}
}

func TestRectFillWideCluster(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(Fill(true), Text("\U0001F469\u200d\U0001F4BB"))})
	Render(scope, screen)
	// The wide cluster occupies two columns; fill must not paint the
	// trailing column.
	cell := screen.lastCell(1, 0)
	if cell.Set {
		t.Fatal("fill painted over the wide cluster's trailing column")
	}
	if cell := screen.lastCell(0, 0); cell.Rune != '\U0001F469' {
		t.Fatalf("expected woman emoji as cluster base, got %v", cell.Rune)
	}
}

func TestRectFillNoChildren(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 3, Right: 4},
		Fill(true),
	)})
	Render(scope, screen)
	// A fill-only Rect paints every box cell without marks tracking.
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			cell := screen.lastCell(x, y)
			if !cell.Set || cell.Rune != ' ' {
				t.Fatalf("expected filled ' ' at (%d,%d), got %+v", x, y, cell)
			}
		}
	}
}

func TestRectFillWideClusterTrailingRow(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 2, Right: 1},
		Fill(true),
		Text("\U0001F469\u200d\U0001F4BB"),
	)})
	Render(scope, screen)
	// The wide cluster's trailing columns must not spill the marks into
	// the next row; row 1 stays fillable.
	if cell := screen.lastCell(0, 1); !cell.Set || cell.Rune != ' ' {
		t.Fatalf("expected filled ' ' at (0,1), got %+v", cell)
	}
}

func TestRectFillNegativeMargin(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 2, Right: 2},
		Margin(-1),
		Fill(true),
	)})
	Render(scope, screen)
	// A negative margin pushes the outer box outside the element box; the
	// fill loop must clip to the element box and not index out of range.
	if cell := screen.lastCell(0, 0); !cell.Set || cell.Rune != ' ' {
		t.Fatalf("expected filled ' ' at (0,0), got %+v", cell)
	}
}

func TestRectFillNegativeMarginChild(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 2, Right: 2},
		Margin(-1),
		Fill(true),
		Text("a"),
	)})
	Render(scope, screen)
	// With children, the marks path must likewise clip its fill loop.
	if cell := screen.lastCell(0, 0); !cell.Set || cell.Rune != ' ' {
		t.Fatalf("expected filled ' ' at (0,0), got %+v", cell)
	}
}

func TestDarkerOrLighterStyle(t *testing.T) {
	// A monochrome foreground shifts with the background toward the
	// mid-gray, without wrapping around the color byte boundary.
	style := vt.BaseStyle.WithFg(HexColor(0xffffff)).WithBg(HexColor(0x000000))
	got := DarkerOrLighterStyle(style, 15)
	r, g, b := got.Fg().RGB()
	if !(r == 0xf0 && g == 0xf0 && b == 0xf0) {
		t.Fatalf("expected monochrome fg shifted to 0xf0, got %#x %#x %#x", r, g, b)
	}
	if r2, g2, b2 := got.Bg().RGB(); !(r2 == 15 && g2 == 15 && b2 == 15) {
		t.Fatalf("expected bg shifted to 15, got %#x %#x %#x", r2, g2, b2)
	}

	// A monochrome foreground on a colored background stays monochrome;
	// the colored background shifts per channel.
	style = vt.BaseStyle.WithFg(HexColor(0x808080)).WithBg(HexColor(0x440000))
	got = DarkerOrLighterStyle(style, 15)
	if r, g, b := got.Fg().RGB(); !(r == 0x8f && g == 0x8f && b == 0x8f) {
		t.Fatalf("expected monochrome fg to stay monochrome, got %#x %#x %#x", r, g, b)
	}
	if r2, g2, b2 := got.Bg().RGB(); !(r2 == 0x53 && g2 == 15 && b2 == 15) {
		t.Fatalf("expected colored bg shifted per channel, got %#x %#x %#x", r2, g2, b2)
	}

	// A colored foreground is left untouched.
	style = vt.BaseStyle.WithFg(HexColor(0xff0000)).WithBg(HexColor(0x000000))
	got = DarkerOrLighterStyle(style, 15)
	if r, _, _ := got.Fg().RGB(); r != 0xff {
		t.Fatalf("expected colored fg untouched, got %#x", r)
	}

	// An unset foreground is preserved: no concrete RGB color is assigned
	// to it. (tcell's vt package represents an unset color as a
	// valid-but-colorless sentinel, so Valid() alone cannot detect it.)
	style = vt.BaseStyle.WithFg(color.Default).WithBg(HexColor(0x000000))
	got = DarkerOrLighterStyle(style, 15)
	if r, g, b := got.Fg().RGB(); r >= 0 || g >= 0 || b >= 0 {
		t.Fatalf("expected default fg preserved, got %#x %#x %#x", r, g, b)
	}
	if r2, g2, b2 := got.Bg().RGB(); !(r2 == 15 && g2 == 15 && b2 == 15) {
		t.Fatalf("expected bg shifted, got %#x %#x %#x", r2, g2, b2)
	}
}

func TestUnderlineColorSpec(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Text("u", Underline(true), UnderlineColor(HexColor(0xff0000)))})
	Render(scope, screen)
	cell := screen.lastCell(0, 0)
	if cell.Style.Attr()&vt.Underline == 0 {
		t.Fatal("Underline(true) spec had no effect")
	}
	r, g, b := cell.Style.Uc().RGB()
	if !(r == 0xff && g == 0 && b == 0) {
		t.Fatalf("expected red underline color, got %#x %#x %#x", r, g, b)
	}
}

func TestUnderlineStyleSpec(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Text("u", UnderlineStyle(DoubleUnderline))})
	Render(scope, screen)
	cell := screen.lastCell(0, 0)
	if cell.Style.Attr()&vt.DoubleUnderline != vt.DoubleUnderline {
		t.Fatalf("expected double underline attr, got %v", cell.Style.Attr())
	}
}

func TestBorder(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Border(true),
		Padding(1),
		Text("a"),
	)})
	Render(scope, screen)
	// The border ring sits at the box edges; padding pushes the content
	// to (2,2).
	if r := screen.cell(0, 0); r != '┌' {
		t.Fatalf("expected top-left corner at (0,0), got %v", r)
	}
	if r := screen.cell(79, 0); r != '┐' {
		t.Fatalf("expected top-right corner at (79,0), got %v", r)
	}
	if r := screen.cell(0, 24); r != '└' {
		t.Fatalf("expected bottom-left corner at (0,24), got %v", r)
	}
	if r := screen.cell(79, 24); r != '┘' {
		t.Fatalf("expected bottom-right corner at (79,24), got %v", r)
	}
	if r := screen.cell(1, 0); r != '─' {
		t.Fatalf("expected top edge at (1,0), got %v", r)
	}
	if r := screen.cell(0, 1); r != '│' {
		t.Fatalf("expected left edge at (0,1), got %v", r)
	}
	if r := screen.cell(2, 2); r != 'a' {
		t.Fatalf("expected 'a' at content (2,2), got %v", r)
	}
}

func TestBorderFill(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Border(true),
		Padding(1),
		Fill(true),
		Text("a"),
	)})
	Render(scope, screen)
	// Fill paints the padding ring between the border and the content;
	// the border ring carries the glyphs.
	if cell := screen.lastCell(1, 1); !cell.Set || cell.Rune != ' ' {
		t.Fatalf("expected filled padding at (1,1), got %+v", cell)
	}
	if r := screen.cell(2, 2); r != 'a' {
		t.Fatalf("expected 'a' at content (2,2), got %v", r)
	}
}

func TestFlexBorder(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Row(
		Border(true),
		Rect(Fill(true), Text("a")),
		Rect(Fill(true), Text("b")),
	)})
	Render(scope, screen)
	// The border shrinks the tiling area to x 1..78; the two equal
	// children split it into 39 columns each.
	if r := screen.cell(0, 0); r != '┌' {
		t.Fatalf("expected top-left corner at (0,0), got %v", r)
	}
	if r := screen.cell(1, 1); r != 'a' {
		t.Fatalf("expected 'a' at (1,1), got %v", r)
	}
	if r := screen.cell(40, 1); r != 'b' {
		t.Fatalf("expected 'b' at (40,1), got %v", r)
	}
}

func TestBorderStyle(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Border(true),
		BorderStyle(SameStyle.SetFG(HexColor(0xff0000))),
		Text("a"),
	)})
	Render(scope, screen)
	cell := screen.lastCell(0, 0)
	if cell.Rune != '┌' {
		t.Fatalf("expected top-left corner, got %v", cell.Rune)
	}
	if r, _, _ := cell.Style.Fg().RGB(); r != 0xff {
		t.Fatalf("expected red border, got %#x", r)
	}
}

func TestRenderNilRoot(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: nil})
	Render(scope, screen)
	// A nil root element renders an empty frame: the screen is cleared to
	// blank cells, never showing stale content.
	if cell := screen.lastCell(0, 0); cell.Set {
		t.Fatal("expected blank cell for nil root")
	}
	if len(screen.frames) != 1 {
		t.Fatalf("expected one frame presented, got %d", len(screen.frames))
	}
}

func TestFlexFillChildOverflow(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 4, Right: 4},
		Fill(true),
		BGColor(HexColor(0x00ff00)),
		Row(
			Padding(1),
			Fill(true),
			BGColor(HexColor(0xff0000)),
			Rect(
				Box{Top: 0, Left: 0, Bottom: 4, Right: 4},
				BGColor(HexColor(0x00ff00)),
				Fill(true),
			),
		),
	)})
	Render(scope, screen)
	// The child Rect's Box override covers the whole element box; the
	// Row's fill must not paint over the child's cells in the padding ring.
	if r, g, b := screen.lastCell(1, 0).Style.Bg().RGB(); !(r == 0 && g == 0xff && b == 0) {
		t.Fatalf("expected child fill to survive in the Row padding ring, got %#x %#x %#x", r, g, b)
	}
}

func TestFlexFillNoChildren(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Row(
		Box{Top: 0, Left: 0, Bottom: 3, Right: 4},
		Fill(true),
	)})
	Render(scope, screen)
	// A fill-only Row with no children paints the whole box, matching
	// Rect's no-children behavior: the empty content area is not left
	// blank.
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			cell := screen.lastCell(x, y)
			if !cell.Set || cell.Rune != ' ' {
				t.Fatalf("expected filled ' ' at (%d,%d), got %+v", x, y, cell)
			}
		}
	}
}
