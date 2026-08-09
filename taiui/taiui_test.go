package taiui

import (
	"fmt"
	"testing"

	"github.com/clipperhouse/displaywidth"
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
