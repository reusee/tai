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

func TestFrameBufferClear(t *testing.T) {
	content := NewFrameBufferContent(10, 10)
	content.SetContent(0, 0, 'X', nil, vt.BaseStyle)
	content.Clear(vt.BaseStyle.WithBg(HexColor(0x101010)))
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: FrameBuffer(content)})
	Render(scope, screen)
	// Clear resets every cell to a blank cell with the given style.
	cell := screen.lastCell(0, 0)
	if !cell.Set || cell.Rune != ' ' {
		t.Fatalf("expected cleared cell at (0,0), got %+v", cell)
	}
	if r, g, b := cell.Style.Bg().RGB(); !(r == 0x10 && g == 0x10 && b == 0x10) {
		t.Fatalf("expected clear style background, got %#x %#x %#x", r, g, b)
	}
	// A cleared cell far from the origin is also blank with the style.
	cell = screen.lastCell(9, 9)
	if !cell.Set || cell.Rune != ' ' {
		t.Fatalf("expected cleared cell at (9,9), got %+v", cell)
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

func TestFrameEqualSizeMismatch(t *testing.T) {
	a := newFrame(2, 3) // 6 cells
	b := newFrame(3, 2) // also 6 cells
	if a.Equal(b) {
		t.Fatal("frames of different dimensions must not be equal")
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

func TestWrapLineTab(t *testing.T) {
	options := displaywidth.Options{}
	// A tab is whitespace: it acts as a break point and is dropped.
	if got := wrapLine("a\tb", 8, options); !sameStrings(got, []string{"a b"}) {
		t.Fatalf("tab break: got %q", got)
	}
}

func TestTextTabWidth(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Text("a\tb", TabWidth(4))})
	Render(scope, screen)
	// TabWidth(4) places 'b' at col 4.
	if r := screen.cell(4, 0); r != 'b' {
		t.Fatalf("expected 'b' at (4,0), got %v", r)
	}
}

func TestTextTabExpansionFill(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Text("a\tb", Fill(true))})
	Render(scope, screen)
	// With fill, the tab's skipped cells are painted with the background.
	if cell := screen.lastCell(1, 0); !cell.Set || cell.Rune != ' ' {
		t.Fatalf("expected filled tab gap at (1,0), got %+v", cell)
	}
	if r := screen.cell(8, 0); r != 'b' {
		t.Fatalf("expected 'b' at (8,0), got %v", r)
	}
}

func TestTextTabExpansion(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Text("a\tb")})
	Render(scope, screen)
	// A tab advances to the next tab stop (default 8): 'a' at col 0,
	// 'b' at col 8. The skipped cells are unset without fill.
	if r := screen.cell(0, 0); r != 'a' {
		t.Fatalf("expected 'a' at (0,0), got %v", r)
	}
	if r := screen.cell(8, 0); r != 'b' {
		t.Fatalf("expected 'b' at (8,0), got %v", r)
	}
	if cell := screen.lastCell(1, 0); cell.Set {
		t.Fatalf("expected unset tab gap at (1,0), got %+v", cell)
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

func TestTextFillAlignRightClippedGap(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 2, Right: 2},
		Text("\U0001F469\u200d\U0001F4BBa", AlignRight, Fill(true)),
	)})
	Render(scope, screen)
	// Right-aligned text wider than the box: the leading wide cluster is
	// clipped at the left edge, and fill paints the residual gap it
	// leaves in the content area.
	if cell := screen.lastCell(0, 0); !cell.Set || cell.Rune != ' ' {
		t.Fatalf("expected filled gap at (0,0), got %+v", cell)
	}
	if r := screen.cell(1, 0); r != 'a' {
		t.Fatalf("expected 'a' at (1,0), got %v", r)
	}
}

func TestClusterWidth(t *testing.T) {
	options := displaywidth.Options{}
	if w := clusterWidth(options, 'e', []rune{'\u0301'}); w != 1 {
		t.Fatalf("expected combining cluster width 1, got %d", w)
	}
	if w := clusterWidth(options, '\U0001F469', []rune{'\u200d', '\U0001F4BB'}); w != 2 {
		t.Fatalf("expected ZWJ cluster width 2, got %d", w)
	}
	if w := clusterWidth(options, 'x', nil); w != 1 {
		t.Fatalf("expected plain rune width 1, got %d", w)
	}
}

func TestTextAlignCenterRounding(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Text("a", AlignCenter)})
	Render(scope, screen)
	// An odd-width line centers with the extra column on the right,
	// matching the conventional (width-len)/2 rule: col 39 of 80.
	if r := screen.cell(39, 0); r != 'a' {
		t.Fatalf("expected 'a' at (39,0), got %v", r)
	}
	if r := screen.cell(40, 0); r != 0 {
		t.Fatalf("expected col 40 blank, got %v", r)
	}
}

func TestTextAlignCenterPadding(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 2, Right: 80},
		Padding(0, 10, 0, 0),
		Text("a", AlignCenter),
	)})
	Render(scope, screen)
	// Center alignment is relative to the padded content area, which the
	// right padding shrinks to [0, 70): the 'a' sits at (0+70-1)/2 = 34.
	if r := screen.cell(34, 0); r != 'a' {
		t.Fatalf("expected 'a' at (34,0), got %v", r)
	}
	if r := screen.cell(35, 0); r != 0 {
		t.Fatalf("expected col 35 blank, got %v", r)
	}
}

func TestTextVAlignMiddle(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 10, Right: 80},
		Text("a", "b", "c", "d", VAlignMiddle),
	)})
	Render(scope, screen)
	// 4 lines in a 10-row box: middle alignment places them at rows 3..6,
	// with 3 blank rows above and below.
	if r := screen.cell(0, 2); r != 0 {
		t.Fatalf("expected row 2 blank, got %v", r)
	}
	if r := screen.cell(0, 3); r != 'a' {
		t.Fatalf("expected 'a' at (0,3), got %v", r)
	}
	if r := screen.cell(0, 6); r != 'd' {
		t.Fatalf("expected 'd' at (0,6), got %v", r)
	}
	if r := screen.cell(0, 7); r != 0 {
		t.Fatalf("expected row 7 blank, got %v", r)
	}
}

func TestTextVAlignBottom(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 10, Right: 80},
		Text("a", "b", "c", "d", VAlignBottom),
	)})
	Render(scope, screen)
	// 4 lines in a 10-row box: bottom alignment places them at rows 6..9.
	if r := screen.cell(0, 5); r != 0 {
		t.Fatalf("expected row 5 blank, got %v", r)
	}
	if r := screen.cell(0, 6); r != 'a' {
		t.Fatalf("expected 'a' at (0,6), got %v", r)
	}
	if r := screen.cell(0, 9); r != 'd' {
		t.Fatalf("expected 'd' at (0,9), got %v", r)
	}
}

func TestTextVAlignMiddlePadding(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 10, Right: 80},
		Padding(1, 0, 1, 0),
		Text("a", "b", VAlignMiddle),
	)})
	Render(scope, screen)
	// The padded content area is rows 1..8 (8 rows); 2 lines center at
	// rows 4..5, with 3 blank rows above and below.
	if r := screen.cell(0, 3); r != 0 {
		t.Fatalf("expected row 3 blank, got %v", r)
	}
	if r := screen.cell(0, 4); r != 'a' {
		t.Fatalf("expected 'a' at (0,4), got %v", r)
	}
	if r := screen.cell(0, 5); r != 'b' {
		t.Fatalf("expected 'b' at (0,5), got %v", r)
	}
	if r := screen.cell(0, 6); r != 0 {
		t.Fatalf("expected row 6 blank, got %v", r)
	}
}

func TestTextVAlignMiddleWrap(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 10, Right: 8},
		Text("one two three four", Wrap(true), VAlignMiddle),
	)})
	Render(scope, screen)
	// "one two three four" wraps to 3 lines in an 8-wide box; middle
	// alignment places them at rows 3..5 of the 10-row box.
	if r := screen.cell(0, 2); r != 0 {
		t.Fatalf("expected row 2 blank, got %v", r)
	}
	if r := screen.cell(0, 3); r != 'o' {
		t.Fatalf("expected 'o' at (0,3), got %v", r)
	}
	if r := screen.cell(0, 5); r != 'f' {
		t.Fatalf("expected 'f' at (0,5), got %v", r)
	}
	if r := screen.cell(0, 6); r != 0 {
		t.Fatalf("expected row 6 blank, got %v", r)
	}
}

func TestBoxModelDegenerate(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 2, Right: 2},
		Border(true),
		Padding(10),
		Fill(true),
		Text("x"),
	)})
	Render(scope, screen)
	// Border plus padding exceeds the box: the content box has negative
	// dimensions. Rendering degenerates safely: the border ring paints
	// and no content escapes the box.
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			if cell := screen.lastCell(x, y); !cell.Set {
				t.Fatalf("expected border or fill at (%d,%d), got unset", x, y)
			}
		}
	}
	if r := screen.cell(0, 0); r != '┌' {
		t.Fatalf("expected top-left corner at (0,0), got %v", r)
	}
	if r := screen.cell(1, 1); r != '┘' {
		t.Fatalf("expected bottom-right corner at (1,1), got %v", r)
	}
}

func TestVerticalScrollCropClippedToScrollbar(t *testing.T) {
	screen := newFakeScreen(80, 25)
	lines := make([]string, 2000)
	for i := range lines {
		lines[i] = "x"
	}
	scope := newRootScope(Root{Element: VerticalScroll(
		Text(lines),
		2000,
		Box{Top: 0, Left: 0, Bottom: 4, Right: 3},
		Scrollbar(true),
		Fill(true),
	)})
	Render(scope, screen)
	// The top crop indicator " 1996.. " is wider than the window's
	// content area; it must be clipped at the scrollbar column, so the
	// scrollbar column keeps the fill background instead of a digit.
	if r := screen.cell(2, 0); r != ' ' {
		t.Fatalf("expected crop clipped at the scrollbar column, got %v", r)
	}
	if r := screen.cell(2, 3); r != '█' {
		t.Fatalf("expected scrollbar thumb at (2,3), got %v", r)
	}
}

func TestBorderStyleOverridesChain(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Border(true),
		FGColor(HexColor(0x0000ff)),
		BorderStyle(SameStyle.SetFG(HexColor(0xff0000))),
		Text("a"),
	)})
	Render(scope, screen)
	// The border style applies after the element's style chain, so the
	// border overrides the chain's foreground while the content keeps it.
	cell := screen.lastCell(0, 0)
	if cell.Rune != '┌' {
		t.Fatalf("expected top-left corner at (0,0), got %v", cell.Rune)
	}
	if r, _, _ := cell.Style.Fg().RGB(); r != 0xff {
		t.Fatalf("expected red border, got %#x", r)
	}
	cell = screen.lastCell(1, 1)
	if cell.Rune != 'a' {
		t.Fatalf("expected 'a' at (1,1), got %v", cell.Rune)
	}
	if r, g, b := cell.Style.Fg().RGB(); !(r == 0 && g == 0 && b == 0xff) {
		t.Fatalf("expected blue content, got %#x %#x %#x", r, g, b)
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

func TestBorderRounded(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Border(true),
		BorderType(BorderRounded),
		Text("a"),
	)})
	Render(scope, screen)
	if r := screen.cell(0, 0); r != '╭' {
		t.Fatalf("expected rounded top-left corner, got %v", r)
	}
	if r := screen.cell(79, 0); r != '╮' {
		t.Fatalf("expected rounded top-right corner, got %v", r)
	}
	if r := screen.cell(0, 24); r != '╰' {
		t.Fatalf("expected rounded bottom-left corner, got %v", r)
	}
	if r := screen.cell(79, 24); r != '╯' {
		t.Fatalf("expected rounded bottom-right corner, got %v", r)
	}
}

func TestBorderDouble(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Border(true),
		BorderType(BorderDouble),
		Text("a"),
	)})
	Render(scope, screen)
	if r := screen.cell(0, 0); r != '╔' {
		t.Fatalf("expected double top-left corner, got %v", r)
	}
	if r := screen.cell(1, 0); r != '═' {
		t.Fatalf("expected double top edge, got %v", r)
	}
	if r := screen.cell(0, 1); r != '║' {
		t.Fatalf("expected double left edge, got %v", r)
	}
}

func TestBorderThick(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Border(true),
		BorderType(BorderThick),
		Text("a"),
	)})
	Render(scope, screen)
	if r := screen.cell(0, 0); r != '┏' {
		t.Fatalf("expected thick top-left corner, got %v", r)
	}
	if r := screen.cell(1, 0); r != '━' {
		t.Fatalf("expected thick top edge, got %v", r)
	}
	if r := screen.cell(0, 1); r != '┃' {
		t.Fatalf("expected thick left edge, got %v", r)
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

func TestNewBaseScope(t *testing.T) {
	// A base scope always provides Root: without a Root definition,
	// Render renders an empty frame instead of panicking.
	screen := newFakeScreen(80, 25)
	scope := NewBaseScope()
	Render(scope, screen)
	if cell := screen.lastCell(0, 0); cell.Set {
		t.Fatal("expected blank cell for default root")
	}

	// A Root definition overrides the default.
	screen2 := newFakeScreen(80, 25)
	scope2 := NewBaseScope(func() Root { return Root{Element: Text("a")} })
	Render(scope2, screen2)
	if r := screen2.cell(0, 0); r != 'a' {
		t.Fatalf("expected 'a' at cell 0, got %v", r)
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

func TestVerticalScrollClipLeft(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 5, Bottom: 4, Right: 10},
		VerticalScroll(
			Rect(Box{Top: 0, Left: 0, Bottom: 2, Right: 10}, Fill(true)),
			0,
		),
	)})
	Render(scope, screen)
	// The child's Box override paints beyond the window's left edge;
	// the scroll must clip those cells so they never bleed onto the
	// screen outside the window.
	if r := screen.cell(4, 0); r != 0 {
		t.Fatalf("expected no bleed left of the window, got %v", r)
	}
	if r := screen.cell(5, 0); r != ' ' {
		t.Fatalf("expected content inside the window, got %v", r)
	}
	if r := screen.cell(4, 1); r != 0 {
		t.Fatalf("expected no bleed left of the window on row 1, got %v", r)
	}
	if r := screen.cell(5, 1); r != ' ' {
		t.Fatalf("expected content inside the window on row 1, got %v", r)
	}
}

func TestVerticalScrollClipRightWideCluster(t *testing.T) {
	screen := newFakeScreen(80, 25)
	content := NewFrameBufferContent(4, 2)
	content.SetContent(2, 0, '\U0001F469', []rune{'\u200d', '\U0001F4BB'}, vt.BaseStyle)
	scope := newRootScope(Root{Element: VerticalScroll(
		FrameBuffer(content),
		0,
		Box{Top: 0, Left: 0, Bottom: 2, Right: 3},
	)})
	Render(scope, screen)
	// The wide cluster at the window's right edge would extend past it;
	// it must be clipped, so neither its base column nor the spill
	// column is drawn.
	if r := screen.cell(2, 0); r != 0 {
		t.Fatalf("expected clipped wide cluster, got %v", r)
	}
	if r := screen.cell(3, 0); r != 0 {
		t.Fatalf("expected no spill past the window, got %v", r)
	}
}

func TestTextNewlineSplit(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Text("a\nb")})
	Render(scope, screen)
	// A bare string with embedded newlines is split into lines at
	// construction, so each line renders on its own row.
	if r := screen.cell(0, 0); r != 'a' {
		t.Fatalf("expected 'a' at (0,0), got %v", r)
	}
	if r := screen.cell(0, 1); r != 'b' {
		t.Fatalf("expected 'b' at (0,1), got %v", r)
	}
}

func TestTextCRLFNormalized(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Text("a\r\nb")})
	Render(scope, screen)
	// CRLF is normalized to LF: the carriage return never reaches a
	// cell, and the second line starts on its own row.
	if cell := screen.lastCell(1, 0); cell.Set {
		t.Fatalf("expected no carriage return cell at (1,0), got %+v", cell)
	}
	if r := screen.cell(0, 1); r != 'b' {
		t.Fatalf("expected 'b' at (0,1), got %v", r)
	}
}

func TestTextBoxFullStopsEarly(t *testing.T) {
	screen := newFakeScreen(80, 25)
	var lines []string
	for i := 0; i < 1000; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 2, Right: 80},
		Text(lines, Wrap(true)),
	)})
	Render(scope, screen)
	// The box holds two rows; rendering stops there and the remaining
	// lines are never processed.
	if r := screen.cell(0, 0); r != 'l' {
		t.Fatalf("expected first line at (0,0), got %v", r)
	}
	if r := screen.cell(0, 1); r != 'l' {
		t.Fatalf("expected second line at (0,1), got %v", r)
	}
	if r := screen.cell(0, 2); r != 0 {
		t.Fatalf("expected nothing below the box, got %v", r)
	}
}

func TestBorderNegativeMarginClipped(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 3, Right: 3},
		Border(true),
		Margin(-1, 0, 0, 0),
	)})
	Render(scope, screen)
	// The negative top margin pushes the top ring outside the element
	// box. The remaining ring is clipped to the box: the left and right
	// edges start at the box top, and the top edge and its corners are
	// gone.
	if r := screen.cell(0, 0); r != '│' {
		t.Fatalf("expected clipped left edge at (0,0), got %v", r)
	}
	if r := screen.cell(1, 0); r != 0 {
		t.Fatalf("expected clipped top edge at (1,0), got %v", r)
	}
	if r := screen.cell(2, 0); r != '│' {
		t.Fatalf("expected clipped right edge at (2,0), got %v", r)
	}
	if r := screen.cell(0, 2); r != '└' {
		t.Fatalf("expected bottom-left corner at (0,2), got %v", r)
	}
	if r := screen.cell(1, 2); r != '─' {
		t.Fatalf("expected bottom edge at (1,2), got %v", r)
	}
	if r := screen.cell(2, 2); r != '┘' {
		t.Fatalf("expected bottom-right corner at (2,2), got %v", r)
	}
}

func TestBorderNegativeMarginFullyClipped(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 4, Right: 4},
		Border(true),
		Margin(-10),
		Fill(true),
	)})
	Render(scope, screen)
	// The negative margin pushes the whole ring outside the element box:
	// no border glyph is drawn, and fill covers the box.
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			cell := screen.lastCell(x, y)
			if !cell.Set || cell.Rune != ' ' {
				t.Fatalf("expected filled ' ' at (%d,%d), got %+v", x, y, cell)
			}
		}
	}
}

func TestTextCursor(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Text("ab", Cursor(true))})
	Render(scope, screen)
	frame := screen.frames[len(screen.frames)-1]
	if !frame.CursorSet {
		t.Fatal("expected cursor set")
	}
	if frame.CursorX != 2 || frame.CursorY != 0 {
		t.Fatalf("expected cursor at (2,0), got (%d,%d)", frame.CursorX, frame.CursorY)
	}

	// Without the Cursor spec, no cursor is recorded.
	screen2 := newFakeScreen(80, 25)
	scope2 := newRootScope(Root{Element: Text("ab")})
	Render(scope2, screen2)
	if frame := screen2.frames[len(screen2.frames)-1]; frame.CursorSet {
		t.Fatal("expected no cursor without Cursor spec")
	}
}

func TestTextCursorEmpty(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Text("", Cursor(true))})
	Render(scope, screen)
	frame := screen.frames[len(screen.frames)-1]
	if !frame.CursorSet {
		t.Fatal("expected cursor set")
	}
	if frame.CursorX != 0 || frame.CursorY != 0 {
		t.Fatalf("expected cursor at (0,0), got (%d,%d)", frame.CursorX, frame.CursorY)
	}
}

func TestTextCursorClipped(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 2, Right: 3},
		Text("abcdef", Cursor(true)),
	)})
	Render(scope, screen)
	frame := screen.frames[len(screen.frames)-1]
	if !frame.CursorSet {
		t.Fatal("expected cursor set")
	}
	// The text is clipped at the right edge: the cursor is at the clip
	// position, the column after the last visible character.
	if frame.CursorX != 3 || frame.CursorY != 0 {
		t.Fatalf("expected cursor at (3,0), got (%d,%d)", frame.CursorX, frame.CursorY)
	}
}

func TestFrameEqualCursor(t *testing.T) {
	a := newFrame(2, 2)
	b := newFrame(2, 2)
	a.setCursor(1, 1)
	if a.Equal(b) {
		t.Fatal("frames with different cursor states should differ")
	}
	b.setCursor(1, 1)
	if !a.Equal(b) {
		t.Fatal("frames with same cursor should be equal")
	}
	b.setCursor(0, 0)
	if a.Equal(b) {
		t.Fatal("frames with different cursor positions should differ")
	}
}

func TestFrameDirtyRows(t *testing.T) {
	a := newFrame(4, 3)
	b := newFrame(4, 3)
	if rows := a.DirtyRows(b); len(rows) != 0 {
		t.Fatalf("expected no dirty rows for identical frames, got %v", rows)
	}
	b.setCell(1, 0, 'x', nil, vt.BaseStyle)
	b.setCell(2, 2, 'y', nil, vt.BaseStyle)
	rows := a.DirtyRows(b)
	if len(rows) != 2 || rows[0] != 0 || rows[1] != 2 {
		t.Fatalf("expected rows [0 2], got %v", rows)
	}
	a = newFrame(3, 4)
	rows = a.DirtyRows(b)
	if len(rows) != 4 {
		t.Fatalf("expected all rows on size mismatch, got %v", rows)
	}
}

func TestBoxIntersect(t *testing.T) {
	a := Box{Top: 0, Left: 0, Bottom: 10, Right: 10}
	b := Box{Top: 5, Left: 5, Bottom: 15, Right: 15}
	got := a.Intersect(b)
	want := Box{Top: 5, Left: 5, Bottom: 10, Right: 10}
	if got != want {
		t.Fatalf("expected %+v, got %+v", want, got)
	}
	// Non-overlapping boxes produce an empty box.
	c := Box{Top: 20, Left: 20, Bottom: 30, Right: 30}
	got = a.Intersect(c)
	if got.Width() != 0 || got.Height() != 0 {
		t.Fatalf("expected empty intersection, got %+v", got)
	}
}

func TestVerticalScrollCursor(t *testing.T) {
	screen := newFakeScreen(80, 25)
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, fmt.Sprintf("line %02d", i))
	}
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 4, Right: 80},
		VerticalScroll(Text(lines, Cursor(true)), 1000),
	)})
	Render(scope, screen)
	frame := screen.frames[len(screen.frames)-1]
	if !frame.CursorSet {
		t.Fatal("expected cursor set in scroll")
	}
	// The view clamps to the content end: the last line (line 19) is at
	// window row 3, and the cursor is at the end of that line.
	if frame.CursorY != 3 {
		t.Fatalf("expected cursor at row 3, got %d", frame.CursorY)
	}
	if frame.CursorX != 7 {
		t.Fatalf("expected cursor at col 7, got %d", frame.CursorX)
	}
}

func TestBorderTitle(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Border(true),
		Title("T"),
		Text("a"),
	)})
	Render(scope, screen)
	// The title is centered on the top border, replacing the edge glyph
	// it covers: 'T' at col 39, with the edge glyphs on both sides.
	if r := screen.cell(39, 0); r != 'T' {
		t.Fatalf("expected title 'T' at (39,0), got %v", r)
	}
	if r := screen.cell(38, 0); r != '─' {
		t.Fatalf("expected edge glyph left of the title, got %v", r)
	}
	if r := screen.cell(40, 0); r != '─' {
		t.Fatalf("expected edge glyph right of the title, got %v", r)
	}
}

func TestBorderTitleClipped(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 2, Right: 4},
		Border(true),
		Title("ABCDE"),
	)})
	Render(scope, screen)
	// The title is wider than the top edge: it is clipped to the visible
	// edge range, so the corners keep their glyphs and no title rune
	// spills past the box.
	if r := screen.cell(0, 0); r != '┌' {
		t.Fatalf("expected top-left corner at (0,0), got %v", r)
	}
	if r := screen.cell(1, 0); r != 'C' {
		t.Fatalf("expected title 'C' at (1,0), got %v", r)
	}
	if r := screen.cell(2, 0); r != 'D' {
		t.Fatalf("expected title 'D' at (2,0), got %v", r)
	}
	if r := screen.cell(3, 0); r != '┐' {
		t.Fatalf("expected top-right corner at (3,0), got %v", r)
	}
}

func TestBorderTitleStyle(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Border(true),
		BorderStyle(SameStyle.SetFG(HexColor(0xff0000))),
		Title("T"),
		Text("a"),
	)})
	Render(scope, screen)
	// The title uses the border style, not the element's style chain.
	cell := screen.lastCell(39, 0)
	if cell.Rune != 'T' {
		t.Fatalf("expected title 'T' at (39,0), got %v", cell.Rune)
	}
	if r, _, _ := cell.Style.Fg().RGB(); r != 0xff {
		t.Fatalf("expected red title, got %#x", r)
	}
}

func TestWrapLineFastPath(t *testing.T) {
	options := displaywidth.Options{}
	// A line with no whitespace that fits the box is returned as-is:
	// the fast path skips word splitting entirely.
	if got := wrapLine("hello", 10, options); !sameStrings(got, []string{"hello"}) {
		t.Fatalf("fast path: got %q", got)
	}
	// A line with no whitespace that exactly fits is also returned as-is.
	if got := wrapLine("hello", 5, options); !sameStrings(got, []string{"hello"}) {
		t.Fatalf("exact fit fast path: got %q", got)
	}
	// A line with no whitespace wider than the box still hard-breaks.
	if got := wrapLine("hello", 3, options); !sameStrings(got, []string{"hel", "lo"}) {
		t.Fatalf("hard break: got %q", got)
	}
	// A line with whitespace still wraps at the space.
	if got := wrapLine("hello world", 8, options); !sameStrings(got, []string{"hello", "world"}) {
		t.Fatalf("space break: got %q", got)
	}
}

func TestVerticalScrollEmptyContent(t *testing.T) {
	screen := newFakeScreen(80, 25)
	scope := newRootScope(Root{Element: Rect(
		Box{Top: 0, Left: 0, Bottom: 4, Right: 80},
		VerticalScroll(Text(""), 0, Fill(true)),
	)})
	Render(scope, screen)
	// An empty child renders no content: the window is filled with the
	// background, and no crop indicators or scrollbar appear.
	for y := 0; y < 4; y++ {
		for x := 0; x < 80; x++ {
			cell := screen.lastCell(x, y)
			if !cell.Set || cell.Rune != ' ' {
				t.Fatalf("expected filled ' ' at (%d,%d), got %+v", x, y, cell)
			}
		}
	}
}
