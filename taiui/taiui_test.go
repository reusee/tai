package taiui

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3/color"
	"github.com/gdamore/tcell/v3/vt"
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
	Render(Rect(Fill(true), Text("Hello")), screen)

	if r := screen.cell(0, 0); r != 'H' {
		t.Fatalf("expected 'H' at cell 0, got %v", r)
	}
}

func TestNestedRect(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Rect(
		Fill(true),
		Rect(
			Margin(2),
			Padding(1),
			Text("Nested"),
		),
	), screen)

	// "Nested" should appear at row 3 (margin 2 + padding 1), col 3.
	if r := screen.cell(3, 3); r != 'N' {
		t.Fatalf("expected 'N' at row 3 col 3, got %v", r)
	}
}

func TestCanvas(t *testing.T) {
	screen := newFakeScreen(80, 25)
	content := NewCanvasContent(10, 10)
	content.SetContent(0, 0, 'X', nil, vt.BaseStyle)
	element := Canvas(content)
	Render(element, screen)

	if r := screen.cell(0, 0); r != 'X' {
		t.Fatalf("expected 'X' at cell 0, got %v", r)
	}

	// Canvas content is state: mutating it and re-rendering yields the
	// updated UI without any element-update call.
	content.SetContent(0, 0, 'Y', nil, vt.BaseStyle)
	Render(element, screen)

	if r := screen.cell(0, 0); r != 'Y' {
		t.Fatalf("expected 'Y' at cell 0 after content change, got %v", r)
	}
}

func TestCanvasContentBounds(t *testing.T) {
	content := NewCanvasContent(10, 10)
	// Writes outside the content bounds are ignored and must not corrupt
	// in-bounds cells.
	content.SetContent(-1, 0, 'X', nil, vt.BaseStyle)
	content.SetContent(0, -1, 'X', nil, vt.BaseStyle)
	content.SetContent(10, 0, 'X', nil, vt.BaseStyle)
	content.SetContent(0, 10, 'X', nil, vt.BaseStyle)

	screen := newFakeScreen(80, 25)
	Render(Canvas(content), screen)

	// Every write was out of bounds, so all cells are blank.
	if r := screen.cell(0, 0); r != 0 {
		t.Fatalf("expected blank cell 0, got %v", r)
	}
	if r := screen.cell(0, 1); r != 0 {
		t.Fatalf("expected blank cell at row 1, got %v", r)
	}
}

func TestCanvasClear(t *testing.T) {
	content := NewCanvasContent(10, 10)
	content.SetContent(0, 0, 'X', nil, vt.BaseStyle)
	content.Clear(vt.BaseStyle.WithBg(HexColor(0x101010)))
	screen := newFakeScreen(80, 25)
	Render(Canvas(content), screen)
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
	Render(Rect(namedBox, Fill(true), Text("n")), screen)

	if r := screen.cell(0, 0); r != 'n' {
		t.Fatalf("expected 'n' at cell 0, got %v", r)
	}
}

func TestRenderToMultipleScreens(t *testing.T) {
	screen1 := newFakeScreen(80, 25)
	screen2 := newFakeScreen(80, 25)
	element := Text("m")
	Render(element, screen1, screen2)

	for i, screen := range []*fakeScreen{screen1, screen2} {
		if r := screen.cell(0, 0); r != 'm' {
			t.Fatalf("expected 'm' at cell 0 on screen %d, got %v", i, r)
		}
	}
}

func TestVerticalScroll(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 2, Right: 80},
		VerticalScroll(Text("a", "b", "c"), 0),
	), screen)

	if r := screen.cell(0, 0); r != 'a' {
		t.Fatalf("expected 'a' at cell 0, got %v", r)
	}
	// The window is two rows high and shows the first two content rows;
	// the third row is cropped without an indicator.
	if r := screen.cell(0, 1); r != 'b' {
		t.Fatalf("expected 'b' at row 1, got %v", r)
	}
}

func TestVerticalScrollOffsetBeyondShortContent(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 4, Right: 80},
		VerticalScroll(Text("a", "b", "c"), 1000),
	), screen)
	// The offset is far beyond the content: the first collection range
	// misses the window, and a second pass re-collects the window cells.
	// The view clamps to the content start, showing "a" at row 0.
	if r := screen.cell(0, 0); r != 'a' {
		t.Fatalf("expected 'a' at (0,0), got %v", r)
	}
	if r := screen.cell(0, 2); r != 'c' {
		t.Fatalf("expected 'c' at (0,2), got %v", r)
	}
}

func TestVerticalScrollOffsetIsFirstVisibleRow(t *testing.T) {
	// The offset is the first visible content row: offset 10 in a
	// 6-row window shows "line 11" at the window's first row, not
	// "line 08" as under the old center-based semantics. The line-number
	// digit at column 6 identifies the visible rows: '1' for line 11
	// rather than '8' for line 08.
	screen := newFakeScreen(80, 25)
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, fmt.Sprintf("line %02d", i))
	}
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 6, Right: 80},
		VerticalScroll(Text(lines), 10),
	), screen)
	// The first visible content row is line 11; its '1' digit is at
	// column 6.
	if r := screen.cell(6, 0); r != '1' {
		t.Fatalf("expected line 11 at (6,0), got %v", r)
	}
	// The last visible row is line 16; its '6' digit is at column 6.
	if r := screen.cell(6, 5); r != '6' {
		t.Fatalf("expected line 16 at (6,5), got %v", r)
	}
}

func TestVerticalScrollWrapWithScrollbar(t *testing.T) {
	// When the scrollbar is shown, the child is rendered at the visible
	// width (the window width minus the scrollbar column), so wrapped
	// text wraps within the visible area instead of hiding behind the
	// scrollbar.
	screen := newFakeScreen(80, 25)
	lines := []string{"abcdefghij", "abcdefghij", "abcdefghij"}
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 4, Right: 5},
		VerticalScroll(
			Text(lines, Wrap(true)),
			0,
			Scrollbar(true),
		),
	), screen)
	// Each source line wraps at the visible width (4 columns: 5 minus
	// the scrollbar column): "abcd", "efgh", "ij". The second visible
	// line starts with 'e'. Without visible-width rendering, the line
	// would wrap at 5 columns and the second visible line would start
	// with 'f', hiding the 'e' behind the scrollbar.
	if r := screen.cell(0, 1); r != 'e' {
		t.Fatalf("expected 'e' at (0,1), got %v", r)
	}
	if r := screen.cell(3, 1); r != 'h' {
		t.Fatalf("expected 'h' at (3,1), got %v", r)
	}
	// The scrollbar column is reserved: no text appears at column 4.
	if r := screen.cell(4, 1); r != 0 && r != '█' {
		t.Fatalf("expected no text in the scrollbar column at (4,1), got %v", r)
	}
}

func TestTextCombiningCluster(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Text("e\u0301x"), screen)

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
	Render(Text("x\U0001F469\u200d\U0001F4BBy"), screen)

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
	element := Text("\u00A1x")

	t.Setenv("RUNEWIDTH_EASTASIAN", "")
	s1 := newFakeScreen(80, 25)
	Render(element, s1)
	if r := s1.cell(1, 0); r != 'x' {
		t.Fatalf("expected ambiguous rune narrow by default, got %v at col 1", r)
	}

	t.Setenv("RUNEWIDTH_EASTASIAN", "1")
	s2 := newFakeScreen(80, 25)
	Render(element, s2)
	if r := s2.cell(1, 0); r != 0 {
		t.Fatalf("expected wide ambiguous rune to skip col 1, got %v", r)
	}
	if r := s2.cell(2, 0); r != 'x' {
		t.Fatalf("expected 'x' at col 2 with wide ambiguous rune, got %v", r)
	}
}

func TestCanvasCombc(t *testing.T) {
	content := NewCanvasContent(5, 5)
	content.SetContent(0, 0, 'e', []rune{'\u0301'}, vt.BaseStyle)
	screen := newFakeScreen(80, 25)
	Render(Canvas(content), screen)

	cell := screen.lastCell(0, 0)
	if cell.Rune != 'e' || !sameCombc(cell.Combc, []rune{'\u0301'}) {
		t.Fatalf("canvas dropped combining runes: %+v", cell)
	}
}

func TestCanvasClipWideCluster(t *testing.T) {
	screen := newFakeScreen(80, 25)
	content := NewCanvasContent(4, 1)
	content.SetContent(2, 0, '\U0001F469', []rune{'\u200d', '\U0001F4BB'}, vt.BaseStyle)
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 1, Right: 3},
		Canvas(content),
	), screen)
	// The wide cluster at the box's right edge would extend past it; it
	// must be clipped, so neither its base column nor the spill column
	// is drawn.
	if r := screen.cell(2, 0); r != 0 {
		t.Fatalf("expected clipped wide cluster, got %v", r)
	}
}

func TestCanvasWideClusterFits(t *testing.T) {
	screen := newFakeScreen(80, 25)
	content := NewCanvasContent(4, 1)
	content.SetContent(2, 0, '\U0001F469', []rune{'\u200d', '\U0001F4BB'}, vt.BaseStyle)
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 1, Right: 5},
		Canvas(content),
	), screen)
	// The wide cluster fits within the box: it is drawn at its base
	// column, and the trailing column is blank.
	if r := screen.cell(2, 0); r != '\U0001F469' {
		t.Fatalf("expected woman emoji at (2,0), got %v", r)
	}
	if r := screen.cell(3, 0); r != 0 {
		t.Fatalf("expected trailing column blank, got %v", r)
	}
}

func TestCanvasWideClusterTrailingCell(t *testing.T) {
	screen := newFakeScreen(80, 25)
	content := NewCanvasContent(4, 1)
	content.SetContent(2, 0, '\U0001F469', []rune{'\u200d', '\U0001F4BB'}, vt.BaseStyle)
	content.SetContent(3, 0, 'x', nil, vt.BaseStyle)
	Render(Canvas(content), screen)
	// The wide cluster at (2,0) covers its trailing column (3,0); the
	// cell at (3,0) is part of the cluster's visual space and must not
	// be drawn over it.
	if r := screen.cell(2, 0); r != '\U0001F469' {
		t.Fatalf("expected woman emoji at (2,0), got %v", r)
	}
	if r := screen.cell(3, 0); r != 0 {
		t.Fatalf("expected trailing column blank, got %v", r)
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
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 4, Right: 80},
		VerticalScroll(Text("a", "e\u0301"), 0),
	), screen)

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

func TestWrapLineLimit(t *testing.T) {
	options := displaywidth.Options{}
	// The limit bounds the produced lines: a long line in a small box
	// never wraps beyond the rows the box can show.
	if got := wrapLineLimited("hello world", 8, 1, options); !sameStrings(got, []string{"hello"}) {
		t.Fatalf("limit 1: got %q", got)
	}
	if got := wrapLineLimited("hello world", 8, 2, options); !sameStrings(got, []string{"hello", "world"}) {
		t.Fatalf("limit 2: got %q", got)
	}
	// A limit of 0 produces no lines.
	if got := wrapLineLimited("hello", 10, 0, options); len(got) != 0 {
		t.Fatalf("limit 0: got %q", got)
	}
	// A negative limit is unlimited, matching wrapLine.
	if got := wrapLineLimited("hello world", 8, -1, options); !sameStrings(got, []string{"hello", "world"}) {
		t.Fatalf("unlimited: got %q", got)
	}
	// A long line in a small box stops at the limit.
	long := strings.Repeat("word ", 100)
	if got := wrapLineLimited(long, 5, 3, options); len(got) != 3 {
		t.Fatalf("long line limit 3: got %d lines", len(got))
	}
}

func TestWrapLinePreservesIndentation(t *testing.T) {
	options := displaywidth.Options{}
	// An indented code line that fits the box is returned unchanged:
	// the wrap fast path never re-flows text that fits, so indentation
	// survives in terminal output.
	if got := wrapLine("    func main() {", 40, options); !sameStrings(got, []string{"    func main() {"}) {
		t.Fatalf("indented fitting line: got %q", got)
	}
	// When a line wraps, the indentation stays on the first line and
	// the whitespace at the wrap boundary is dropped; the continuation
	// line starts at the left column.
	if got := wrapLine("    hello world", 12, options); !sameStrings(got, []string{"    hello", "world"}) {
		t.Fatalf("indented wrapped line: got %q", got)
	}
	// Runs of consecutive spaces between words survive in wrapped lines.
	if got := wrapLine("ab  cd ef", 8, options); !sameStrings(got, []string{"ab  cd", "ef"}) {
		t.Fatalf("double space in wrapped line: got %q", got)
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

func TestWrapLineWordWiderThanBox(t *testing.T) {
	options := displaywidth.Options{}
	// A word wider than the box after a short word: the short word
	// flushes, then the wide word hard-breaks.
	if got := wrapLine("ab abcdef", 4, options); !sameStrings(got, []string{"ab", "abcd", "ef"}) {
		t.Fatalf("wide word after short word: got %q", got)
	}
}

func TestWrapLineTab(t *testing.T) {
	options := displaywidth.Options{}
	// "a\tb" in an 8-column box: 'a' at column 0, the tab advances to
	// column 8 (outside the box), so 'b' wraps to the next line. The
	// tab itself is dropped at the wrap boundary, matching the
	// whitespace-at-boundary rule.
	if got := wrapLine("a\tb", 8, options); !sameStrings(got, []string{"a", "b"}) {
		t.Fatalf("tab break: got %q", got)
	}
	// In a wider box the tab is preserved verbatim and expands to a tab
	// stop at render time.
	if got := wrapLine("a\tb", 80, options); !sameStrings(got, []string{"a\tb"}) {
		t.Fatalf("fitting tab line: got %q", got)
	}
}

func TestTextTabWidth(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Text("a\tb", TabWidth(4)), screen)
	// TabWidth(4) places 'b' at col 4.
	if r := screen.cell(4, 0); r != 'b' {
		t.Fatalf("expected 'b' at (4,0), got %v", r)
	}
}

func TestTextTabExpansionFill(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Text("a\tb", Fill(true)), screen)
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
	Render(Text("a\tb"), screen)
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

func TestTextFastPathMatchesGeneralPath(t *testing.T) {
	// The fast path must produce the same output as the general path for
	// the conditions it handles. A no-op offset style forces the general
	// path without changing the output.
	for _, text := range []string{"hello", "a\tb", "e\u0301x", "\U0001F469\u200d\U0001F4BB"} {
		fast := newFakeScreen(80, 25)
		Render(Text(text), fast)

		general := newFakeScreen(80, 25)
		Render(Text(text, OffsetStyleFunc(func(int) StyleFunc { return SameStyle })), general)

		if !fast.frames[len(fast.frames)-1].Equal(general.frames[len(general.frames)-1]) {
			t.Fatalf("fast path output differs from general path for %q", text)
		}
	}
}

func TestTextFastPathEmptyBoxCursor(t *testing.T) {
	// An empty box override with a cursor: the fast path must not set a
	// cursor, matching the general path's early return.
	fast := newFakeScreen(80, 25)
	Render(Text("a", Cursor(true), Box{Top: 5, Left: 0, Bottom: 5, Right: 10}), fast)

	general := newFakeScreen(80, 25)
	Render(Text("a", Cursor(true), Box{Top: 5, Left: 0, Bottom: 5, Right: 10}, OffsetStyleFunc(func(int) StyleFunc { return SameStyle })), general)

	if !fast.frames[len(fast.frames)-1].Equal(general.frames[len(general.frames)-1]) {
		t.Fatal("fast path empty-box cursor output differs from general path")
	}
}

func TestTextFastPathCursorMatchesGeneralPath(t *testing.T) {
	// The fast path must produce the same cursor position as the general
	// path for the conditions it handles. A no-op offset style forces the
	// general path without changing the output.
	for _, text := range []string{"hello", "a\tb", "e\u0301x", "\U0001F469\u200d\U0001F4BB", ""} {
		fast := newFakeScreen(80, 25)
		Render(Text(text, Cursor(true)), fast)

		general := newFakeScreen(80, 25)
		Render(Text(text, Cursor(true), OffsetStyleFunc(func(int) StyleFunc { return SameStyle })), general)

		if !fast.frames[len(fast.frames)-1].Equal(general.frames[len(general.frames)-1]) {
			t.Fatalf("fast path cursor output differs from general path for %q", text)
		}
	}
}

func TestTextFastPathCursorAtMatchesGeneralPath(t *testing.T) {
	// The fast path must produce the same cursor-at-offset position as
	// the general path for the conditions it handles. A no-op offset
	// style forces the general path without changing the output.
	for _, text := range []string{"hello", "a\tb", "e\u0301x", "\U0001F469\u200d\U0001F4BB", ""} {
		for _, offset := range []int{0, 1, 2, 10} {
			fast := newFakeScreen(80, 25)
			Render(Text(text, CursorAt(offset)), fast)

			general := newFakeScreen(80, 25)
			Render(Text(text, CursorAt(offset), OffsetStyleFunc(func(int) StyleFunc { return SameStyle })), general)

			if !fast.frames[len(fast.frames)-1].Equal(general.frames[len(general.frames)-1]) {
				t.Fatalf("fast path cursor-at output differs from general path for %q at offset %d", text, offset)
			}
		}
	}
}

func TestTextFastPathFillMatchesGeneralPath(t *testing.T) {
	// The fast path must produce the same output as the general path for
	// the conditions it handles, including fill. A no-op offset style
	// forces the general path without changing the output.
	for _, text := range []string{"hello", "a\tb", "e\u0301x", "\U0001F469\u200d\U0001F4BB", ""} {
		fast := newFakeScreen(80, 25)
		Render(Text(text, Fill(true)), fast)

		general := newFakeScreen(80, 25)
		Render(Text(text, Fill(true), OffsetStyleFunc(func(int) StyleFunc { return SameStyle })), general)

		if !fast.frames[len(fast.frames)-1].Equal(general.frames[len(general.frames)-1]) {
			t.Fatalf("fast path fill output differs from general path for %q", text)
		}
	}
}

func TestTextFastPathFillCursorMatchesGeneralPath(t *testing.T) {
	// The fast path must produce the same cursor position as the general
	// path for the conditions it handles, including fill. A no-op offset
	// style forces the general path without changing the output.
	for _, text := range []string{"hello", "a\tb", "e\u0301x", "\U0001F469\u200d\U0001F4BB", ""} {
		fast := newFakeScreen(80, 25)
		Render(Text(text, Fill(true), Cursor(true)), fast)

		general := newFakeScreen(80, 25)
		Render(Text(text, Fill(true), Cursor(true), OffsetStyleFunc(func(int) StyleFunc { return SameStyle })), general)

		if !fast.frames[len(fast.frames)-1].Equal(general.frames[len(general.frames)-1]) {
			t.Fatalf("fast path fill cursor output differs from general path for %q", text)
		}
	}
}

func TestTextFastPathFillCursorAtMatchesGeneralPath(t *testing.T) {
	// The fast path must produce the same cursor-at-offset position as
	// the general path for the conditions it handles, including fill. A
	// no-op offset style forces the general path without changing the
	// output.
	for _, text := range []string{"hello", "a\tb", "e\u0301x", "\U0001F469\u200d\U0001F4BB", ""} {
		for _, offset := range []int{0, 1, 2, 10} {
			fast := newFakeScreen(80, 25)
			Render(Text(text, Fill(true), CursorAt(offset)), fast)

			general := newFakeScreen(80, 25)
			Render(Text(text, Fill(true), CursorAt(offset), OffsetStyleFunc(func(int) StyleFunc { return SameStyle })), general)

			if !fast.frames[len(fast.frames)-1].Equal(general.frames[len(general.frames)-1]) {
				t.Fatalf("fast path fill cursor-at output differs from general path for %q at offset %d", text, offset)
			}
		}
	}
}

func TestTextWrapRender(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 4, Right: 8},
		Text("one two three", Wrap(true)),
	), screen)
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
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 4, Right: 4},
		Text("abcdefgh", Wrap(true)),
	), screen)
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

func TestTextWrapPreservesIndentation(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 3, Right: 80},
		Text("    func main() {", Wrap(true)),
	), screen)
	// The indented code line keeps its indentation through the
	// render-time wrap: a fitting indented line is returned unchanged
	// by the wrap function, so its leading spaces reach the frame.
	for x, want := range []rune{' ', ' ', ' ', ' ', 'f'} {
		if r := screen.cell(x, 0); r != want {
			t.Fatalf("expected %q at (%d,0), got %q", want, x, r)
		}
	}
}

func TestTextFillAligned(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Text("ab", AlignRight, Fill(true)), screen)
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
	Render(Text("ab", AlignCenter, Fill(true)), screen2)
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
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 2, Right: 2},
		Text("a\U0001F469\u200d\U0001F4BB"),
	), screen)
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
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 2, Right: 2},
		Text("\U0001F469\u200d\U0001F4BBa", AlignRight, Fill(true)),
	), screen)
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
	if w := ClusterWidth(options, 'e', []rune{'\u0301'}); w != 1 {
		t.Fatalf("expected combining cluster width 1, got %d", w)
	}
	if w := ClusterWidth(options, '\U0001F469', []rune{'\u200d', '\U0001F4BB'}); w != 2 {
		t.Fatalf("expected ZWJ cluster width 2, got %d", w)
	}
	if w := ClusterWidth(options, 'x', nil); w != 1 {
		t.Fatalf("expected plain rune width 1, got %d", w)
	}
}

func TestTextAlignCenterRounding(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Text("a", AlignCenter), screen)
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
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 2, Right: 80},
		Padding(0, 10, 0, 0),
		Text("a", AlignCenter),
	), screen)
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
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 10, Right: 80},
		Text("a", "b", "c", "d", VAlignMiddle),
	), screen)
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
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 10, Right: 80},
		Text("a", "b", "c", "d", VAlignBottom),
	), screen)
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
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 10, Right: 80},
		Padding(1, 0, 1, 0),
		Text("a", "b", VAlignMiddle),
	), screen)
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
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 10, Right: 8},
		Text("one two three four", Wrap(true), VAlignMiddle),
	), screen)
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
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 2, Right: 2},
		Border(true),
		Padding(10),
		Fill(true),
		Text("x"),
	), screen)
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

func TestBorderStyleOverridesChain(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Rect(
		Border(true),
		FGColor(HexColor(0x0000ff)),
		BorderStyle(SameStyle.SetFG(HexColor(0xff0000))),
		Text("a"),
	), screen)
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
	Render(Column(
		Rect(Fill(true), Text("a")),
		Rect(Fill(true), Text("b")),
	), screen)
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
	Render(Row(
		Rect(Fill(true), Text("a")),
		Rect(Fill(true), Text("b")),
	), screen)
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
	Render(Row(
		Rect(Fill(true), Text("a")),
		Weighted(2, Rect(Fill(true), Text("b"))),
	), screen)
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
	Render(Row(
		Rect(Fill(true), Text("a")),
		Rect(Fill(true), Text("b")),
		Rect(Fill(true), Text("c")),
	), screen)
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
	Render(Row(
		Margin(1),
		Padding(1),
		Fill(true),
		Rect(Fill(true), Text("a")),
		Rect(Fill(true), Text("b")),
	), screen)
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
	Render(Rect(
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
	), screen)
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
	Render(Text("a", Italic(true), Reverse(true)), screen)
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
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 4, Right: 80},
		VerticalScroll(Text(lines), 1000),
	), screen)
	// The view clamps to the content end: rows show lines 16..19.
	if r := screen.cell(0, 0); r != 'l' {
		t.Fatalf("expected line 16 at (0,0), got %v", r)
	}
	if r := screen.cell(6, 0); r != '6' {
		t.Fatalf("expected line 16 at (6,0), got %v", r)
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
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 10, Right: 80},
		VerticalScroll(Text(lines), 0, Scrollbar(true)),
	), screen)
	if r := screen.cell(79, 0); r != '█' {
		t.Fatalf("expected scrollbar thumb at (79,0), got %v", r)
	}
}

func TestVerticalScrollNoScrollbarWhenFits(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 10, Right: 80},
		VerticalScroll(Text("a", "b", "c"), 0, Scrollbar(true)),
	), screen)
	if r := screen.cell(79, 0); r != 0 {
		t.Fatalf("expected no scrollbar thumb when content fits, got %v", r)
	}
}

func TestVerticalScrollBoxSpec(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(VerticalScroll(
		Text("a", "b", "c"),
		0,
		Box{Top: 0, Left: 0, Bottom: 3, Right: 80},
	), screen)
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
	Render(VerticalScroll(
		Text(lines),
		1000,
		Box{Top: 0, Left: 0, Bottom: 4, Right: 80},
		FGColor(HexColor(0xff0000)),
	), screen)
	// The style chain applies to the scroll content.
	if r, g, b := screen.lastCell(0, 0).Style.Fg().RGB(); !(r == 0xff && g == 0 && b == 0) {
		t.Fatalf("expected red content, got %#x %#x %#x", r, g, b)
	}
}

func TestVerticalScrollFill(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(VerticalScroll(
		Text("a", "b", "c"),
		0,
		Box{Top: 0, Left: 0, Bottom: 10, Right: 80},
		Fill(true),
	), screen)
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
	Render(VerticalScroll(
		Text("\U0001F469\u200d\U0001F4BB"),
		0,
		Box{Top: 0, Left: 0, Bottom: 4, Right: 80},
		Fill(true),
	), screen)
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
	Render(Rect(Fill(true), Text("\U0001F469\u200d\U0001F4BB")), screen)
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
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 3, Right: 4},
		Fill(true),
	), screen)
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
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 2, Right: 1},
		Fill(true),
		Text("\U0001F469\u200d\U0001F4BB"),
	), screen)
	// The wide cluster's trailing columns must not spill the marks into
	// the next row; row 1 stays fillable.
	if cell := screen.lastCell(0, 1); !cell.Set || cell.Rune != ' ' {
		t.Fatalf("expected filled ' ' at (0,1), got %+v", cell)
	}
}

func TestRectFillNegativeMargin(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 2, Right: 2},
		Margin(-1),
		Fill(true),
	), screen)
	// A negative margin pushes the outer box outside the element box; the
	// fill loop must clip to the element box and not index out of range.
	if cell := screen.lastCell(0, 0); !cell.Set || cell.Rune != ' ' {
		t.Fatalf("expected filled ' ' at (0,0), got %+v", cell)
	}
}

func TestRectFillNegativeMarginChild(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 2, Right: 2},
		Margin(-1),
		Fill(true),
		Text("a"),
	), screen)
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
	Render(Text("u", Underline(true), UnderlineColor(HexColor(0xff0000))), screen)
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
	Render(Text("u", UnderlineStyle(DoubleUnderline)), screen)
	cell := screen.lastCell(0, 0)
	if cell.Style.Attr()&vt.DoubleUnderline != vt.DoubleUnderline {
		t.Fatalf("expected double underline attr, got %v", cell.Style.Attr())
	}
}

func TestBorder(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Rect(
		Border(true),
		Padding(1),
		Text("a"),
	), screen)
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
	Render(Rect(
		Border(true),
		Padding(1),
		Fill(true),
		Text("a"),
	), screen)
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
	Render(Row(
		Border(true),
		Rect(Fill(true), Text("a")),
		Rect(Fill(true), Text("b")),
	), screen)
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
	Render(Rect(
		Border(true),
		BorderStyle(SameStyle.SetFG(HexColor(0xff0000))),
		Text("a"),
	), screen)
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
	Render(Rect(
		Border(true),
		BorderType(BorderRounded),
		Text("a"),
	), screen)
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
	Render(Rect(
		Border(true),
		BorderType(BorderDouble),
		Text("a"),
	), screen)
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
	Render(Rect(
		Border(true),
		BorderType(BorderThick),
		Text("a"),
	), screen)
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

func TestRenderNilElement(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(nil, screen)
	// A nil element renders an empty frame: the screen is cleared to
	// blank cells, never showing stale content.
	if cell := screen.lastCell(0, 0); cell.Set {
		t.Fatal("expected blank cell for nil element")
	}
	if len(screen.frames) != 1 {
		t.Fatalf("expected one frame presented, got %d", len(screen.frames))
	}
}

func TestFlexFillChildOverflow(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Rect(
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
	), screen)
	// The child Rect's Box override covers the whole element box; the
	// Row's fill must not paint over the child's cells in the padding ring.
	if r, g, b := screen.lastCell(1, 0).Style.Bg().RGB(); !(r == 0 && g == 0xff && b == 0) {
		t.Fatalf("expected child fill to survive in the Row padding ring, got %#x %#x %#x", r, g, b)
	}
}

func TestFlexFillNoChildren(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Row(
		Box{Top: 0, Left: 0, Bottom: 3, Right: 4},
		Fill(true),
	), screen)
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

func TestFlexFillRingEmpty(t *testing.T) {
	// A Row with fill and no box model has an empty ring: the fill is a
	// no-op, so the marks tracking and the fill loop are skipped. The
	// children tile the content area; the Row's fill does not paint the
	// content area, which is the children's responsibility.
	screen := newFakeScreen(80, 25)
	Render(Row(
		Fill(true),
		Text("a"),
		Text("b"),
	), screen)
	if r := screen.cell(0, 0); r != 'a' {
		t.Fatalf("expected 'a' at (0,0), got %v", r)
	}
	if r := screen.cell(40, 0); r != 'b' {
		t.Fatalf("expected 'b' at (40,0), got %v", r)
	}
	// The Row's fill does not paint the content area: the cells after the
	// text are unset, not filled.
	if cell := screen.lastCell(1, 0); cell.Set {
		t.Fatal("expected no fill from the Row in the content area")
	}
}

func TestFlexFillRingNonEmpty(t *testing.T) {
	// A Row with fill and padding has a non-empty ring: the fill paints
	// the padding ring cells the children did not occupy.
	screen := newFakeScreen(80, 25)
	Render(Row(
		Fill(true),
		Padding(1),
		Text("a"),
		Text("b"),
	), screen)
	// The padding ring is filled.
	if cell := screen.lastCell(0, 0); !cell.Set || cell.Rune != ' ' {
		t.Fatalf("expected filled padding at (0,0), got %+v", cell)
	}
	// The content starts after the padding.
	if r := screen.cell(1, 1); r != 'a' {
		t.Fatalf("expected 'a' at (1,1), got %v", r)
	}
	// The Row's fill does not paint the content area.
	if cell := screen.lastCell(2, 1); cell.Set {
		t.Fatal("expected no fill from the Row in the content area")
	}
}

func TestVerticalScrollClipLeft(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Rect(
		Box{Top: 0, Left: 5, Bottom: 4, Right: 10},
		VerticalScroll(
			Rect(Box{Top: 0, Left: 0, Bottom: 2, Right: 10}, Fill(true)),
			0,
		),
	), screen)
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
	content := NewCanvasContent(4, 2)
	content.SetContent(2, 0, '\U0001F469', []rune{'\u200d', '\U0001F4BB'}, vt.BaseStyle)
	Render(VerticalScroll(
		Canvas(content),
		0,
		Box{Top: 0, Left: 0, Bottom: 2, Right: 3},
	), screen)
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
	Render(Text("a\nb"), screen)
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
	Render(Text("a\r\nb"), screen)
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
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 2, Right: 80},
		Text(lines, Wrap(true)),
	), screen)
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
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 3, Right: 3},
		Border(true),
		Margin(-1, 0, 0, 0),
	), screen)
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
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 4, Right: 4},
		Border(true),
		Margin(-10),
		Fill(true),
	), screen)
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
	Render(Text("ab", Cursor(true)), screen)
	frame := screen.frames[len(screen.frames)-1]
	if !frame.CursorSet {
		t.Fatal("expected cursor set")
	}
	if frame.CursorX != 2 || frame.CursorY != 0 {
		t.Fatalf("expected cursor at (2,0), got (%d,%d)", frame.CursorX, frame.CursorY)
	}

	// Without the Cursor spec, no cursor is recorded.
	screen2 := newFakeScreen(80, 25)
	Render(Text("ab"), screen2)
	if frame := screen2.frames[len(screen2.frames)-1]; frame.CursorSet {
		t.Fatal("expected no cursor without Cursor spec")
	}
}

func TestTextCursorEmpty(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Text("", Cursor(true)), screen)
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
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 2, Right: 3},
		Text("abcdef", Cursor(true)),
	), screen)
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

func TestFrameDirtyRowsInto(t *testing.T) {
	a := newFrame(4, 3)
	b := newFrame(4, 3)
	var buf []int
	if rows := a.DirtyRowsInto(b, buf); len(rows) != 0 {
		t.Fatalf("expected no dirty rows for identical frames, got %v", rows)
	}
	b.setCell(1, 0, 'x', nil, vt.BaseStyle)
	b.setCell(2, 2, 'y', nil, vt.BaseStyle)
	buf = a.DirtyRowsInto(b, buf)
	if len(buf) != 2 || buf[0] != 0 || buf[1] != 2 {
		t.Fatalf("expected rows [0 2], got %v", buf)
	}
	// The buffer is reused: the next call resets it.
	buf = a.DirtyRowsInto(b, buf)
	if len(buf) != 2 || buf[0] != 0 || buf[1] != 2 {
		t.Fatalf("expected reused buffer rows [0 2], got %v", buf)
	}
	// A pre-populated buffer is reset.
	buf = []int{99, 100}
	buf = a.DirtyRowsInto(b, buf)
	if len(buf) != 2 || buf[0] != 0 || buf[1] != 2 {
		t.Fatalf("expected reset buffer rows [0 2], got %v", buf)
	}
	// A size mismatch returns all rows.
	a = newFrame(3, 4)
	buf = a.DirtyRowsInto(b, buf)
	if len(buf) != 4 {
		t.Fatalf("expected all rows on size mismatch, got %v", buf)
	}
}

func TestFrameCellPool(t *testing.T) {
	f := newFrame(4, 3)
	if len(f.Cells) != 12 {
		t.Fatalf("expected 12 cells, got %d", len(f.Cells))
	}
	f.setCell(0, 0, 'x', nil, vt.BaseStyle)
	ReleaseFrame(f)
	// The next frame reuses the pooled cells, cleared: a stale set cell
	// from the previous frame must not survive.
	g := newFrame(4, 3)
	if g.Cells[0].Set {
		t.Fatal("expected reused cells cleared")
	}
}

func TestMarksPool(t *testing.T) {
	marks := getMarks(10)
	if len(marks) != 10 {
		t.Fatalf("expected 10 marks, got %d", len(marks))
	}
	for i := range marks {
		if marks[i] {
			t.Fatalf("expected cleared marks, got true at %d", i)
		}
	}
	marks[3] = true
	putMarks(marks)
	marks = getMarks(10)
	if marks[3] {
		t.Fatal("expected marks cleared on reuse")
	}
}

// releasingScreen is a Screen that does not retain presented frames and
// returns their cells to the pool, for testing FrameReleaser.
type releasingScreen struct {
	fakeScreen
	released int
}

func (s *releasingScreen) Present(Frame) {}

func (s *releasingScreen) ReleaseFrame(frame Frame) {
	ReleaseFrame(frame)
	s.released++
}

func TestRenderFrameReleaser(t *testing.T) {
	screen := &releasingScreen{fakeScreen: *newFakeScreen(80, 25)}
	element := Text("a")
	Render(element, screen)
	if screen.released != 1 {
		t.Fatalf("expected one release, got %d", screen.released)
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
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 4, Right: 80},
		VerticalScroll(Text(lines, Cursor(true)), 1000),
	), screen)
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

func TestVerticalScrollCursorLastWins(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 4, Right: 80},
		VerticalScroll(
			Overlay(
				Text("aaa", Cursor(true)),
				Text("bbbbb", Cursor(true)),
			),
			0,
		),
	), screen)
	frame := screen.frames[len(screen.frames)-1]
	if !frame.CursorSet {
		t.Fatal("expected cursor set")
	}
	// The overlay's later child draws over the earlier one; the last
	// cursor request wins, so the cursor is at the end of "bbbbb".
	if frame.CursorX != 5 || frame.CursorY != 0 {
		t.Fatalf("expected cursor at (5,0), got (%d,%d)", frame.CursorX, frame.CursorY)
	}
}

func TestBorderTitle(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Rect(
		Border(true),
		Title("T"),
		Text("a"),
	), screen)
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
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 2, Right: 4},
		Border(true),
		Title("ABCDE"),
	), screen)
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
	Render(Rect(
		Border(true),
		BorderStyle(SameStyle.SetFG(HexColor(0xff0000))),
		Title("T"),
		Text("a"),
	), screen)
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

func TestWrapLineFastPathSpaces(t *testing.T) {
	options := displaywidth.Options{}
	// A line with internal spaces that fits the box is returned as-is:
	// the fast path skips word splitting.
	if got := wrapLine("hello world", 11, options); !sameStrings(got, []string{"hello world"}) {
		t.Fatalf("fast path with spaces: got %q", got)
	}
	// A line with a tab that fits the box is returned unchanged: the
	// tab is preserved and expands to a tab stop at render time.
	if got := wrapLine("a\tb", 80, options); !sameStrings(got, []string{"a\tb"}) {
		t.Fatalf("fitting tab line: got %q", got)
	}
	// A line with a tab that does not fit wraps at the tab boundary.
	if got := wrapLine("a\tb", 8, options); !sameStrings(got, []string{"a", "b"}) {
		t.Fatalf("tab line: got %q", got)
	}
	// Fitting lines keep their leading and trailing spaces: indentation
	// must survive wrapping, so an indented code line is returned
	// unchanged.
	if got := wrapLine(" hello", 10, options); !sameStrings(got, []string{" hello"}) {
		t.Fatalf("leading space: got %q", got)
	}
	if got := wrapLine("hello ", 10, options); !sameStrings(got, []string{"hello "}) {
		t.Fatalf("trailing space: got %q", got)
	}
	// A line with consecutive spaces keeps them when it fits.
	if got := wrapLine("a  b", 10, options); !sameStrings(got, []string{"a  b"}) {
		t.Fatalf("double space: got %q", got)
	}
}

func TestWrapCJKBreakAtAnyCluster(t *testing.T) {
	options := displaywidth.Options{}
	// Han characters have display width 2. A Han sequence is breakable
	// at any cluster boundary: when the current line cannot fit the
	// whole sequence but can fit part of it, the part stays on the line
	// and the rest wraps. In a 4-column box, "ab" leaves two columns
	// that "汉" fills (the boundary space is dropped), so "汉字" splits
	// as "ab汉" + "字" instead of "ab" + "汉字".
	got := wrapLine("ab 汉字", 4, options)
	want := []string{"ab汉", "字"}
	if !sameStrings(got, want) {
		t.Fatalf("wrapLine(\"ab 汉字\", 4) = %q, want %q", got, want)
	}

	// In a 3-column box neither "ab 汉" (5 columns) nor "ab汉" (4) fits
	// with the leading text, so each Han character gets its own line.
	got = wrapLine("ab 汉字", 3, options)
	want = []string{"ab", "汉", "字"}
	if !sameStrings(got, want) {
		t.Fatalf("wrapLine(\"ab 汉字\", 3) = %q, want %q", got, want)
	}

	// A pure Han line wraps at any cluster boundary when it exceeds the
	// box width: two Han characters fill a 4-column line.
	got = wrapLine("汉字测试", 4, options)
	want = []string{"汉字", "测试"}
	if !sameStrings(got, want) {
		t.Fatalf("wrapLine(\"汉字测试\", 4) = %q, want %q", got, want)
	}
	got = wrapLine("汉字测试", 3, options)
	want = []string{"汉", "字", "测", "试"}
	if !sameStrings(got, want) {
		t.Fatalf("wrapLine(\"汉字测试\", 3) = %q, want %q", got, want)
	}

	// Non-Han words remain unbreakable: a long word still hard-breaks
	// only when it exceeds the box width.
	got = wrapLine("ab cd", 3, options)
	want = []string{"ab", "cd"}
	if !sameStrings(got, want) {
		t.Fatalf("wrapLine(\"ab cd\", 3) = %q, want %q", got, want)
	}
}

func TestTextWrapCJKBreak(t *testing.T) {
	// End-to-end rendering check: Han characters are two columns wide,
	// so a 4-column box holds "ab汉" on the first row (the boundary
	// space is dropped so the cluster fills the line), "字测" on the
	// second, and "试" on the third. Without CJK break support, "汉字"
	// would move as a whole to the next row, wasting the two columns
	// after "ab".
	screen := newFakeScreen(80, 25)
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 3, Right: 4},
		Text("ab 汉字测试", Wrap(true)),
	), screen)

	if r := screen.cell(0, 0); r != 'a' {
		t.Fatalf("expected 'a' at (0,0), got %v", r)
	}
	if r := screen.cell(1, 0); r != 'b' {
		t.Fatalf("expected 'b' at (1,0), got %v", r)
	}
	if r := screen.cell(2, 0); r != '汉' {
		t.Fatalf("expected '汉' at (2,0), got %v", r)
	}
	if r := screen.cell(0, 1); r != '字' {
		t.Fatalf("expected '字' at (0,1), got %v", r)
	}
	if r := screen.cell(2, 1); r != '测' {
		t.Fatalf("expected '测' at (2,1), got %v", r)
	}
	if r := screen.cell(0, 2); r != '试' {
		t.Fatalf("expected '试' at (0,2), got %v", r)
	}
	// The trailing column of the wide '汉' cluster is blank.
	if cell := screen.lastCell(3, 0); cell.Set {
		t.Fatalf("expected the trailing column of '汉' blank, got %+v", cell)
	}
}

func TestWrapLinePreservesTabs(t *testing.T) {
	options := displaywidth.Options{}
	// Fitting lines with leading tabs (Go indentation) are preserved
	// verbatim: the wrap fast path returns a fitting line unchanged, so
	// indentation never collapses to spaces.
	if got := wrapLine("\t\tfoo", 80, options); !sameStrings(got, []string{"\t\tfoo"}) {
		t.Fatalf("wrapLine(\"\\t\\tfoo\", 80) = %q", got)
	}
	// A fitting line with tabs anywhere keeps them verbatim.
	if got := wrapLine("\t\tfoo bar", 80, options); !sameStrings(got, []string{"\t\tfoo bar"}) {
		t.Fatalf("wrapLine(\"\\t\\tfoo bar\", 80) = %q", got)
	}
	// When the indentation would push the first word past the box, the
	// leading whitespace is dropped and the word hard-breaks from the
	// left column: the tabs cannot be preserved within the box, and the
	// word itself fits.
	if got := wrapLine("\t\tfoo bar", 8, options); !sameStrings(got, []string{"foo bar"}) {
		t.Fatalf("wrapLine(\"\\t\\tfoo bar\", 8) = %q", got)
	}
}

func TestWrapLineLimitedIterAppends(t *testing.T) {
	options := displaywidth.Options{}
	iter := getGraphemeIter()
	defer putGraphemeIter(iter)
	// The helper appends to the caller-provided slice, so a render pass
	// reuses the pooled line slice across lines.
	out := []string{"existing"}
	got := wrapLineLimitedIter("hello world", 8, 2, options, iter, defaultTabWidth, out)
	if !sameStrings(got, []string{"existing", "hello", "world"}) {
		t.Fatalf("expected appended lines, got %q", got)
	}
	// The limit bounds the appended lines, not the total.
	out = []string{"existing"}
	got = wrapLineLimitedIter("hello world", 8, 1, options, iter, defaultTabWidth, out)
	if !sameStrings(got, []string{"existing", "hello"}) {
		t.Fatalf("expected limit-bounded append, got %q", got)
	}
}

func TestWrapLines(t *testing.T) {
	// WrapLines wraps each source line to the given width, applying the
	// same cluster-aware word wrapping Text uses internally, so callers
	// that pre-wrap content (e.g., a TUI computing scroll extents) stay
	// consistent with Text's rendering.
	got := WrapLines([]string{"one two three", "four five six"}, 7)
	want := []string{"one two", "three", "four", "five", "six"}
	if !sameStrings(got, want) {
		t.Fatalf("WrapLines = %q, want %q", got, want)
	}
}

func TestVerticalScrollEmptyContent(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 4, Right: 80},
		VerticalScroll(Text(""), 0, Fill(true)),
	), screen)
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

func TestVerticalScrollOverlayLastDrawWins(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Rect(
		Box{Top: 0, Left: 0, Bottom: 4, Right: 80},
		VerticalScroll(
			Overlay(
				Text("aaaa"),
				Text("bbbb"),
			),
			0,
		),
	), screen)
	// The overlay's later child draws over the earlier one inside the
	// scroll: the flat cell collection replays in draw order, so the
	// later draw wins.
	if r := screen.cell(0, 0); r != 'b' {
		t.Fatalf("expected 'b' at (0,0), got %v", r)
	}
	if r := screen.cell(3, 0); r != 'b' {
		t.Fatalf("expected 'b' at (3,0), got %v", r)
	}
}

func TestOverlayStacking(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Overlay(
		Text("a"),
		Text("b"),
	), screen)
	// Later children draw over earlier ones: 'b' wins at (0,0).
	if r := screen.cell(0, 0); r != 'b' {
		t.Fatalf("expected 'b' at (0,0), got %v", r)
	}
}

func TestOverlayFill(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Overlay(
		Fill(true),
		BGColor(HexColor(0x101010)),
		Text("a"),
	), screen)
	// Fill paints the background in the cells the text does not occupy.
	if cell := screen.lastCell(1, 0); !cell.Set || cell.Rune != ' ' {
		t.Fatalf("expected filled ' ' at (1,0), got %+v", cell)
	}
	if r := screen.cell(0, 0); r != 'a' {
		t.Fatalf("expected 'a' at (0,0), got %v", r)
	}
}

func TestOverlayFillWideCluster(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Overlay(
		Fill(true),
		Text("\U0001F469\u200d\U0001F4BB"),
	), screen)
	// The wide cluster occupies two columns; fill must not paint the
	// trailing column, matching Rect's fill semantics.
	cell := screen.lastCell(1, 0)
	if cell.Set {
		t.Fatal("fill painted over the wide cluster's trailing column")
	}
	if cell := screen.lastCell(0, 0); cell.Rune != '\U0001F469' {
		t.Fatalf("expected woman emoji as cluster base, got %v", cell.Rune)
	}
}

func TestOverlayBoxAndStyle(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Overlay(
		Box{Top: 0, Left: 0, Bottom: 2, Right: 2},
		FGColor(HexColor(0xff0000)),
		Text("a"),
	), screen)
	// The Box override constrains the overlay, and the style chain
	// applies to the content.
	if r := screen.cell(0, 0); r != 'a' {
		t.Fatalf("expected 'a' at (0,0), got %v", r)
	}
	if r := screen.cell(0, 2); r != 0 {
		t.Fatalf("expected nothing at (0,2), got %v", r)
	}
	if r, g, b := screen.lastCell(0, 0).Style.Fg().RGB(); !(r == 0xff && g == 0 && b == 0) {
		t.Fatalf("expected red content, got %#x %#x %#x", r, g, b)
	}
}

func TestCursorAt(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Text("abc", CursorAt(1)), screen)
	frame := screen.frames[len(screen.frames)-1]
	if !frame.CursorSet {
		t.Fatal("expected cursor set")
	}
	if frame.CursorX != 1 || frame.CursorY != 0 {
		t.Fatalf("expected cursor at (1,0), got (%d,%d)", frame.CursorX, frame.CursorY)
	}

	// Offset 0 places the cursor at the start.
	screen2 := newFakeScreen(80, 25)
	Render(Text("abc", CursorAt(0)), screen2)
	frame = screen2.frames[len(screen2.frames)-1]
	if frame.CursorX != 0 || frame.CursorY != 0 {
		t.Fatalf("expected cursor at (0,0), got (%d,%d)", frame.CursorX, frame.CursorY)
	}

	// An offset beyond the line length is clamped to the end.
	screen3 := newFakeScreen(80, 25)
	Render(Text("abc", CursorAt(10)), screen3)
	frame = screen3.frames[len(screen3.frames)-1]
	if frame.CursorX != 3 || frame.CursorY != 0 {
		t.Fatalf("expected cursor clamped at (3,0), got (%d,%d)", frame.CursorX, frame.CursorY)
	}
}

func TestCursorAtAlignRight(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Text("abc", AlignRight, CursorAt(1)), screen)
	frame := screen.frames[len(screen.frames)-1]
	// "abc" is right-aligned: it starts at col 77. The cursor at offset 1
	// is at col 78.
	if frame.CursorX != 78 || frame.CursorY != 0 {
		t.Fatalf("expected cursor at (78,0), got (%d,%d)", frame.CursorX, frame.CursorY)
	}
}

func TestCursorAtTab(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Text("a\tb", CursorAt(2)), screen)
	frame := screen.frames[len(screen.frames)-1]
	// "a\tb" renders 'a' at col 0, 'b' at col 8. The cursor at offset 2
	// (after the tab) is at col 8, where 'b' is drawn.
	if frame.CursorX != 8 || frame.CursorY != 0 {
		t.Fatalf("expected cursor at (8,0), got (%d,%d)", frame.CursorX, frame.CursorY)
	}
}

func TestInput(t *testing.T) {
	screen := newFakeScreen(80, 25)
	Render(Input("hello", 2), screen)
	// Input renders the text with the cursor at the given offset.
	if r := screen.cell(0, 0); r != 'h' {
		t.Fatalf("expected 'h' at (0,0), got %v", r)
	}
	frame := screen.frames[len(screen.frames)-1]
	if frame.CursorX != 2 || frame.CursorY != 0 {
		t.Fatalf("expected cursor at (2,0), got (%d,%d)", frame.CursorX, frame.CursorY)
	}
}

func TestList(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		screen := newFakeScreen(80, 25)
		Render(List([]string{"a", "b", "c"}, 0), screen)
		if r := screen.cell(0, 0); r != 'a' {
			t.Fatalf("expected 'a' at (0,0), got %v", r)
		}
		if r := screen.cell(0, 1); r != 'b' {
			t.Fatalf("expected 'b' at (0,1), got %v", r)
		}
		if r := screen.cell(0, 2); r != 'c' {
			t.Fatalf("expected 'c' at (0,2), got %v", r)
		}
	})

	t.Run("SelectedStyle", func(t *testing.T) {
		screen := newFakeScreen(80, 25)
		Render(List(
			[]string{"a", "b", "c"},
			1,
			ListStyle(SameStyle.SetBG(HexColor(0xff0000))),
		), screen)
		// The selected item is highlighted with the ListStyle.
		cell := screen.lastCell(0, 1)
		if cell.Rune != 'b' {
			t.Fatalf("expected 'b' at (0,1), got %v", cell.Rune)
		}
		if r, g, b := cell.Style.Bg().RGB(); !(r == 0xff && g == 0 && b == 0) {
			t.Fatalf("expected red background on selected item, got %#x %#x %#x", r, g, b)
		}
		// The selected row is fully painted with the selected style.
		cell = screen.lastCell(1, 1)
		if !cell.Set || cell.Rune != ' ' {
			t.Fatalf("expected filled ' ' at (1,1), got %+v", cell)
		}
		if r, g, b := cell.Style.Bg().RGB(); !(r == 0xff && g == 0 && b == 0) {
			t.Fatalf("expected red background on selected row, got %#x %#x %#x", r, g, b)
		}
		// The non-selected items keep the list style.
		cell = screen.lastCell(0, 0)
		if r, g, b := cell.Style.Bg().RGB(); r >= 0 || g >= 0 || b >= 0 {
			t.Fatalf("expected no background on non-selected item, got %#x %#x %#x", r, g, b)
		}
	})

	t.Run("ScrollFollow", func(t *testing.T) {
		screen := newFakeScreen(80, 25)
		var items []string
		for i := 0; i < 100; i++ {
			items = append(items, fmt.Sprintf("item %02d", i))
		}
		Render(Rect(
			Box{Top: 0, Left: 0, Bottom: 5, Right: 80},
			List(items, 50),
		), screen)
		// The view is centered on the selected item (50) in a 5-row box:
		// rows show items 48..52.
		if r := screen.cell(0, 0); r != 'i' {
			t.Fatalf("expected item 48 at (0,0), got %v", r)
		}
		if r := screen.cell(6, 0); r != '8' {
			t.Fatalf("expected item 48 at (6,0), got %v", r)
		}
		if r := screen.cell(0, 4); r != 'i' {
			t.Fatalf("expected item 52 at (0,4), got %v", r)
		}
	})

	t.Run("ScrollClamp", func(t *testing.T) {
		screen := newFakeScreen(80, 25)
		var items []string
		for i := 0; i < 10; i++ {
			items = append(items, fmt.Sprintf("item %02d", i))
		}
		Render(Rect(
			Box{Top: 0, Left: 0, Bottom: 5, Right: 80},
			List(items, 9),
		), screen)
		// The view clamps to the content end: rows show items 5..9.
		if r := screen.cell(0, 0); r != 'i' {
			t.Fatalf("expected item 5 at (0,0), got %v", r)
		}
		if r := screen.cell(0, 4); r != 'i' {
			t.Fatalf("expected item 9 at (0,4), got %v", r)
		}
	})

	t.Run("SelectedClamp", func(t *testing.T) {
		screen := newFakeScreen(80, 25)
		Render(Rect(
			Box{Top: 0, Left: 0, Bottom: 5, Right: 80},
			List([]string{"a", "b", "c"}, 10),
		), screen)
		// The selected index is clamped to the last item; the view shows
		// the last items.
		if r := screen.cell(0, 0); r != 'a' {
			t.Fatalf("expected 'a' at (0,0), got %v", r)
		}
		if r := screen.cell(0, 2); r != 'c' {
			t.Fatalf("expected 'c' at (0,2), got %v", r)
		}
	})

	t.Run("Fill", func(t *testing.T) {
		screen := newFakeScreen(80, 25)
		Render(Rect(
			Box{Top: 0, Left: 0, Bottom: 5, Right: 80},
			List([]string{"a", "b"}, 0, Fill(true)),
		), screen)
		// The rows below the last item are filled.
		if cell := screen.lastCell(0, 2); !cell.Set || cell.Rune != ' ' {
			t.Fatalf("expected filled ' ' at (0,2), got %+v", cell)
		}
		if cell := screen.lastCell(0, 4); !cell.Set || cell.Rune != ' ' {
			t.Fatalf("expected filled ' ' at (0,4), got %+v", cell)
		}
	})

	t.Run("Empty", func(t *testing.T) {
		screen := newFakeScreen(80, 25)
		Render(Rect(
			Box{Top: 0, Left: 0, Bottom: 5, Right: 80},
			List(nil, 0, Fill(true)),
		), screen)
		// An empty list renders no items; fill paints the whole box.
		for y := 0; y < 5; y++ {
			for x := 0; x < 80; x++ {
				cell := screen.lastCell(x, y)
				if !cell.Set || cell.Rune != ' ' {
					t.Fatalf("expected filled ' ' at (%d,%d), got %+v", x, y, cell)
				}
			}
		}
	})
}

func TestListItemRendering(t *testing.T) {
	// A tab in a list item advances to the next tab stop (default 8);
	// the skipped cells are painted because the selected row is filled.
	screen := newFakeScreen(80, 25)
	Render(List([]string{"a\tb"}, 0), screen)
	if r := screen.cell(0, 0); r != 'a' {
		t.Fatalf("expected 'a' at (0,0), got %v", r)
	}
	if cell := screen.lastCell(1, 0); !cell.Set || cell.Rune != ' ' {
		t.Fatalf("expected filled tab gap at (1,0), got %+v", cell)
	}
	if r := screen.cell(8, 0); r != 'b' {
		t.Fatalf("expected 'b' at (8,0), got %v", r)
	}

	// A wide cluster occupies two columns: the trailing column is
	// blank and the next rune follows it.
	screen2 := newFakeScreen(80, 25)
	Render(List([]string{"x\U0001F469\u200d\U0001F4BBy"}, 0), screen2)
	if r := screen2.cell(0, 0); r != 'x' {
		t.Fatalf("expected 'x' at (0,0), got %v", r)
	}
	if r := screen2.cell(1, 0); r != '\U0001F469' {
		t.Fatalf("expected woman emoji at (1,0), got %v", r)
	}
	if r := screen2.cell(2, 0); r != 0 {
		t.Fatalf("expected trailing column blank, got %v", r)
	}
	if r := screen2.cell(3, 0); r != 'y' {
		t.Fatalf("expected 'y' at (3,0), got %v", r)
	}
}

func TestReadKeysImprovements(t *testing.T) {
	t.Run("Arrows", func(t *testing.T) {
		ch := make(chan string, 8)
		go ReadKeys(strings.NewReader("\x1b[C\x1b[D\x1bOC\x1bOD"), ch)
		var got []string
		for len(got) < 4 {
			select {
			case k := <-ch:
				got = append(got, k)
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for arrow keys")
			}
		}
		want := []string{"right", "left", "right", "left"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("SplitSequenceAcrossZeroReads", func(t *testing.T) {
		// A zero-byte read is not a completion signal: an escape
		// sequence split across reads is held until it completes, so
		// the arrow key is not lost.
		ch := make(chan string, 8)
		go ReadKeys(&chunkReader{chunks: [][]byte{[]byte("\x1b["), {}, []byte("A")}}, ch)
		select {
		case k := <-ch:
			if k != "up" {
				t.Fatalf("expected up, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for up")
		}
	})

	t.Run("IncompleteSequenceConsumedByFinalByte", func(t *testing.T) {
		// ESC [ 1 q is a complete CSI sequence: the final byte q is
		// consumed as part of the sequence, so it must not leak into
		// the key stream as a printable character.
		ch := make(chan string, 8)
		go ReadKeys(&chunkReader{chunks: [][]byte{[]byte("\x1b[1"), {}, []byte("q")}}, ch)
		select {
		case k := <-ch:
			t.Fatalf("expected no key, got %q", k)
		case <-time.After(100 * time.Millisecond):
		}
	})

	t.Run("IncompleteSequenceDiscardedAfterTimeout", func(t *testing.T) {
		// A partial sequence that receives no further bytes within the
		// grace period is discarded, so a later byte is processed
		// normally.
		ch := make(chan string, 8)
		go ReadKeys(&delayedChunkReader{
			chunks: [][]byte{[]byte("\x1b[1"), {}, []byte("q")},
			delay:  150 * time.Millisecond,
		}, ch)
		select {
		case k := <-ch:
			if k != "q" {
				t.Fatalf("expected q, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for q")
		}
	})

	t.Run("LoneEsc", func(t *testing.T) {
		// A lone ESC at end of input is a real key press, not a partial
		// sequence: it is emitted as "esc".
		ch := make(chan string, 8)
		go ReadKeys(strings.NewReader("\x1b"), ch)
		select {
		case k := <-ch:
			if k != "esc" {
				t.Fatalf("expected esc, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for esc")
		}
	})

	t.Run("LoneEscTimeout", func(t *testing.T) {
		// A lone ESC that never grows is emitted as "esc" when the grace
		// period expires.
		ch := make(chan string, 8)
		go ReadKeys(&delayedChunkReader{
			chunks: [][]byte{[]byte("\x1b"), {}},
			delay:  150 * time.Millisecond,
		}, ch)
		select {
		case k := <-ch:
			if k != "esc" {
				t.Fatalf("expected esc, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for esc")
		}
	})

	t.Run("UnknownCSIDiscarded", func(t *testing.T) {
		ch := make(chan string, 8)
		go ReadKeys(strings.NewReader("\x1b[?25lq"), ch)
		select {
		case k := <-ch:
			if k != "q" {
				t.Fatalf("expected q, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for q")
		}
	})
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if r.idx >= len(r.chunks) {
		return 0, io.EOF
	}
	chunk := r.chunks[r.idx]
	r.idx++
	if len(chunk) == 0 {
		return 0, nil
	}
	return copy(p, chunk), nil
}

// delayedChunkReader returns its chunks in order; an empty chunk yields
// zero-byte reads for at least delay before advancing, simulating a
// non-blocking tty poll with a pause between chunks.
type delayedChunkReader struct {
	chunks [][]byte
	idx    int
	delay  time.Duration
}

func (r *delayedChunkReader) Read(p []byte) (int, error) {
	if r.idx >= len(r.chunks) {
		return 0, io.EOF
	}
	chunk := r.chunks[r.idx]
	if len(chunk) == 0 {
		time.Sleep(r.delay)
		r.idx++
		return 0, nil
	}
	r.idx++
	return copy(p, chunk), nil
}

// chunkReader returns its chunks in order; an empty chunk yields a
// zero-byte read, simulating a non-blocking tty poll.
type chunkReader struct {
	chunks [][]byte
	idx    int
}

func TestReadKeysComprehensive(t *testing.T) {
	t.Run("FunctionKeysSS3", func(t *testing.T) {
		ch := make(chan string, 8)
		go ReadKeys(strings.NewReader("\x1bOP\x1bOQ\x1bOR\x1bOS"), ch)
		var got []string
		for len(got) < 4 {
			select {
			case k := <-ch:
				got = append(got, k)
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for function keys")
			}
		}
		want := []string{"f1", "f2", "f3", "f4"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("FunctionKeysTilde", func(t *testing.T) {
		ch := make(chan string, 16)
		input := "\x1b[11~\x1b[12~\x1b[13~\x1b[14~\x1b[15~\x1b[17~\x1b[18~\x1b[19~\x1b[20~\x1b[21~\x1b[23~\x1b[24~"
		go ReadKeys(strings.NewReader(input), ch)
		var got []string
		for len(got) < 12 {
			select {
			case k := <-ch:
				got = append(got, k)
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for function keys")
			}
		}
		want := []string{"f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8", "f9", "f10", "f11", "f12"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("InsertDelete", func(t *testing.T) {
		ch := make(chan string, 8)
		go ReadKeys(strings.NewReader("\x1b[2~\x1b[3~"), ch)
		var got []string
		for len(got) < 2 {
			select {
			case k := <-ch:
				got = append(got, k)
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for keys")
			}
		}
		want := []string{"insert", "delete"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("BackspaceEnterSpace", func(t *testing.T) {
		ch := make(chan string, 8)
		go ReadKeys(strings.NewReader("\x7f\x0d\x0a\x20\x08"), ch)
		var got []string
		for len(got) < 5 {
			select {
			case k := <-ch:
				got = append(got, k)
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for keys")
			}
		}
		want := []string{"backspace", "enter", "enter", "space", "backspace"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("ShiftTab", func(t *testing.T) {
		ch := make(chan string, 8)
		go ReadKeys(strings.NewReader("\x1b[Z"), ch)
		select {
		case k := <-ch:
			if k != "shift-tab" {
				t.Fatalf("expected shift-tab, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for shift-tab")
		}
	})

	t.Run("CtrlLetters", func(t *testing.T) {
		ch := make(chan string, 8)
		go ReadKeys(strings.NewReader("\x01\x02\x05\x0c\x1a"), ch)
		var got []string
		for len(got) < 5 {
			select {
			case k := <-ch:
				got = append(got, k)
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for ctrl keys")
			}
		}
		want := []string{"ctrl-a", "ctrl-b", "ctrl-e", "ctrl-l", "ctrl-z"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("CtrlC", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x03"), ch)
		select {
		case k := <-ch:
			if k != "ctrl-c" {
				t.Fatalf("expected ctrl-c, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for ctrl-c")
		}
	})

	t.Run("PrintableChars", func(t *testing.T) {
		ch := make(chan string, 8)
		go ReadKeys(strings.NewReader("abcXYZ"), ch)
		var got []string
		for len(got) < 6 {
			select {
			case k := <-ch:
				got = append(got, k)
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for printable chars")
			}
		}
		want := []string{"a", "b", "c", "X", "Y", "Z"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("ModifiedArrows", func(t *testing.T) {
		ch := make(chan string, 8)
		go ReadKeys(strings.NewReader("\x1b[1;2A\x1b[1;5C\x1b[1;3B\x1b[1;6D"), ch)
		var got []string
		for len(got) < 4 {
			select {
			case k := <-ch:
				got = append(got, k)
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for modified arrows")
			}
		}
		want := []string{"shift-up", "ctrl-right", "alt-down", "shift-ctrl-left"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("AltKey", func(t *testing.T) {
		ch := make(chan string, 8)
		go ReadKeys(strings.NewReader("\x1ba\x1bx"), ch)
		var got []string
		for len(got) < 2 {
			select {
			case k := <-ch:
				got = append(got, k)
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for alt keys")
			}
		}
		want := []string{"alt-a", "alt-x"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("ModifiedTilde", func(t *testing.T) {
		ch := make(chan string, 8)
		go ReadKeys(strings.NewReader("\x1b[5;2~\x1b[2;5~"), ch)
		var got []string
		for len(got) < 2 {
			select {
			case k := <-ch:
				got = append(got, k)
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for modified tilde keys")
			}
		}
		want := []string{"shift-pageup", "ctrl-insert"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})
}

func TestReadKeysExtended(t *testing.T) {
	t.Run("F13-F24", func(t *testing.T) {
		ch := make(chan string, 16)
		input := "\x1b[28~\x1b[29~\x1b[31~\x1b[32~\x1b[33~\x1b[34~\x1b[35~\x1b[36~\x1b[37~\x1b[38~\x1b[39~\x1b[40~"
		go ReadKeys(strings.NewReader(input), ch)
		var got []string
		for len(got) < 12 {
			select {
			case k := <-ch:
				got = append(got, k)
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for F13-F24")
			}
		}
		want := []string{"f13", "f14", "f15", "f16", "f17", "f18", "f19", "f20", "f21", "f22", "f23", "f24"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("BracketedPaste", func(t *testing.T) {
		ch := make(chan string, 8)
		go ReadKeys(strings.NewReader("\x1b[200~hello\x1b[201~"), ch)
		var got []string
		for len(got) < 7 {
			select {
			case k := <-ch:
				got = append(got, k)
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for bracketed paste")
			}
		}
		if got[0] != "paste-start" || got[6] != "paste-end" {
			t.Fatalf("expected paste-start and paste-end, got %v", got)
		}
	})

	t.Run("FocusEvents", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b[I\x1b[O"), ch)
		var got []string
		for len(got) < 2 {
			select {
			case k := <-ch:
				got = append(got, k)
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for focus events")
			}
		}
		if got[0] != "focus-in" || got[1] != "focus-out" {
			t.Fatalf("expected focus-in and focus-out, got %v", got)
		}
	})

	t.Run("ExtendedControlChars", func(t *testing.T) {
		ch := make(chan string, 8)
		go ReadKeys(strings.NewReader("\x1c\x1d\x1e\x1f"), ch)
		var got []string
		for len(got) < 4 {
			select {
			case k := <-ch:
				got = append(got, k)
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for control chars")
			}
		}
		want := []string{"ctrl-\\", "ctrl-]", "ctrl-^", "ctrl-_"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("KeypadSS3", func(t *testing.T) {
		ch := make(chan string, 16)
		go ReadKeys(strings.NewReader("\x1bOM\x1bOj\x1bOk\x1bOm\x1bOn\x1bOo\x1bOX"), ch)
		var got []string
		for len(got) < 7 {
			select {
			case k := <-ch:
				got = append(got, k)
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for keypad keys")
			}
		}
		want := []string{"kp-enter", "kp-multiply", "kp-add", "kp-subtract", "kp-decimal", "kp-divide", "kp-equal"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("BeginKey", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b[E\x1bOE"), ch)
		var got []string
		for len(got) < 2 {
			select {
			case k := <-ch:
				got = append(got, k)
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for begin key")
			}
		}
		if got[0] != "begin" || got[1] != "begin" {
			t.Fatalf("expected begin begin, got %v", got)
		}
	})

	t.Run("KittyKeyboard", func(t *testing.T) {
		ch := make(chan string, 8)
		go ReadKeys(strings.NewReader("\x1b[57358;1u\x1b[57396;1u\x1b[97;1u\x1b[57358;5u"), ch)
		var got []string
		for len(got) < 4 {
			select {
			case k := <-ch:
				got = append(got, k)
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for kitty keys")
			}
		}
		want := []string{"f1", "up", "a", "ctrl-f1"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("KittyKeyboardReleaseIgnored", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b[57358;1;1u\x1b[57358;1;0u\x1b[97;1;1u"), ch)
		var got []string
		for len(got) < 2 {
			select {
			case k := <-ch:
				got = append(got, k)
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for kitty keys")
			}
		}
		want := []string{"f1", "a"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("KittyModePopNotMouse", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b[<0uq"), ch)
		select {
		case k := <-ch:
			if k != "q" {
				t.Fatalf("expected q after mode pop, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for q after mode pop")
		}
	})

	t.Run("ModOneNoPrefix", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b[1;1A"), ch)
		select {
		case k := <-ch:
			if k != "up" {
				t.Fatalf("expected up, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for up")
		}
	})
}

func TestReadKeysUTF8(t *testing.T) {
	t.Run("MultiByteChars", func(t *testing.T) {
		ch := make(chan string, 8)
		// "é" (U+00E9 = 0xC3 0xA9), "中" (U+4E2D = 0xE4 0xB8 0xAD), "a"
		go ReadKeys(strings.NewReader("\xc3\xa9\xe4\xb8\xada"), ch)
		var got []string
		for len(got) < 3 {
			select {
			case k := <-ch:
				got = append(got, k)
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for UTF-8 keys")
			}
		}
		want := []string{"é", "中", "a"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("Emoji", func(t *testing.T) {
		ch := make(chan string, 4)
		// "🎉" (U+1F389 = 0xF0 0x9F 0x8E 0x89)
		go ReadKeys(strings.NewReader("\xf0\x9f\x8e\x89"), ch)
		select {
		case k := <-ch:
			if k != "🎉" {
				t.Fatalf("expected 🎉, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for emoji")
		}
	})

	t.Run("AltMultiByte", func(t *testing.T) {
		ch := make(chan string, 4)
		// ESC + "é" = 0x1b 0xC3 0xA9
		go ReadKeys(strings.NewReader("\x1b\xc3\xa9"), ch)
		select {
		case k := <-ch:
			if k != "alt-é" {
				t.Fatalf("expected alt-é, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for alt-é")
		}
	})

	t.Run("SplitAcrossReads", func(t *testing.T) {
		// A UTF-8 sequence split across reads is held until it
		// completes, matching the escape-sequence split behavior.
		ch := make(chan string, 4)
		go ReadKeys(&chunkReader{chunks: [][]byte{[]byte("\xe4\xb8"), {}, []byte("\xad")}}, ch)
		select {
		case k := <-ch:
			if k != "中" {
				t.Fatalf("expected 中, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for 中")
		}
	})

	t.Run("IncompleteDroppedAfterTimeout", func(t *testing.T) {
		// An incomplete 3-byte sequence (only 2 bytes) that never
		// completes is dropped after the grace period.
		ch := make(chan string, 4)
		go ReadKeys(&delayedChunkReader{
			chunks: [][]byte{[]byte("\xe4\xb8"), {}},
			delay:  150 * time.Millisecond,
		}, ch)
		select {
		case k := <-ch:
			t.Fatalf("expected no key, got %q", k)
		case <-time.After(200 * time.Millisecond):
		}
	})
}

func TestReadKeysSS3ModifiedAndModifyOtherKeys(t *testing.T) {
	t.Run("SS3ShiftUp", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1bO1;2A"), ch)
		select {
		case k := <-ch:
			if k != "shift-up" {
				t.Fatalf("expected shift-up, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for shift-up")
		}
	})

	t.Run("SS3CtrlRight", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1bO1;5C"), ch)
		select {
		case k := <-ch:
			if k != "ctrl-right" {
				t.Fatalf("expected ctrl-right, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for ctrl-right")
		}
	})

	t.Run("SS3ShiftCtrlUp", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1bO1;6A"), ch)
		select {
		case k := <-ch:
			if k != "shift-ctrl-up" {
				t.Fatalf("expected shift-ctrl-up, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for shift-ctrl-up")
		}
	})

	t.Run("SS3PlainStillWorks", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1bOA"), ch)
		select {
		case k := <-ch:
			if k != "up" {
				t.Fatalf("expected up, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for up")
		}
	})

	t.Run("SS3KeypadStillWorks", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1bOM"), ch)
		select {
		case k := <-ch:
			if k != "kp-enter" {
				t.Fatalf("expected kp-enter, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for kp-enter")
		}
	})

	t.Run("ModifyOtherKeysEnter", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b[27;1;13~"), ch)
		select {
		case k := <-ch:
			if k != "enter" {
				t.Fatalf("expected enter, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for enter")
		}
	})

	t.Run("ModifyOtherKeysTab", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b[27;1;9~"), ch)
		select {
		case k := <-ch:
			if k != "tab" {
				t.Fatalf("expected tab, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for tab")
		}
	})

	t.Run("ModifyOtherKeysBackspace", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b[27;1;127~"), ch)
		select {
		case k := <-ch:
			if k != "backspace" {
				t.Fatalf("expected backspace, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for backspace")
		}
	})

	t.Run("ModifyOtherKeysCtrlA", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b[27;5;97~"), ch)
		select {
		case k := <-ch:
			if k != "ctrl-a" {
				t.Fatalf("expected ctrl-a, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for ctrl-a")
		}
	})

	t.Run("ModifyOtherKeysShiftA", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b[27;2;97~"), ch)
		select {
		case k := <-ch:
			if k != "shift-a" {
				t.Fatalf("expected shift-a, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for shift-a")
		}
	})

	t.Run("ModifyOtherKeysEsc", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b[27;1;27~"), ch)
		select {
		case k := <-ch:
			if k != "esc" {
				t.Fatalf("expected esc, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for esc")
		}
	})

	t.Run("ModifyOtherKeysUnicode", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b[27;1;20013~"), ch)
		select {
		case k := <-ch:
			if k != "中" {
				t.Fatalf("expected 中, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for 中")
		}
	})
}

func TestReadKeysControlStrings(t *testing.T) {
	t.Run("OSCBelTerminator", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b]0;title\x07q"), ch)
		select {
		case k := <-ch:
			if k != "q" {
				t.Fatalf("expected q after OSC, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for q after OSC")
		}
	})

	t.Run("OSCSTTerminator", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b]0;title\x1b\\q"), ch)
		select {
		case k := <-ch:
			if k != "q" {
				t.Fatalf("expected q after OSC ST, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for q after OSC ST")
		}
	})

	t.Run("DCSConsumed", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1bP1q\x1b\\q"), ch)
		select {
		case k := <-ch:
			if k != "q" {
				t.Fatalf("expected q after DCS, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for q after DCS")
		}
	})

	t.Run("SOSConsumed", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1bXdata\x1b\\q"), ch)
		select {
		case k := <-ch:
			if k != "q" {
				t.Fatalf("expected q after SOS, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for q after SOS")
		}
	})

	t.Run("PMConsumed", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b^data\x1b\\q"), ch)
		select {
		case k := <-ch:
			if k != "q" {
				t.Fatalf("expected q after PM, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for q after PM")
		}
	})

	t.Run("APCConsumed", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b_data\x1b\\q"), ch)
		select {
		case k := <-ch:
			if k != "q" {
				t.Fatalf("expected q after APC, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for q after APC")
		}
	})

	t.Run("OSCConsumedNoKeyAfter", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b]0;title\x07"), ch)
		select {
		case k := <-ch:
			t.Fatalf("expected no key from OSC alone, got %q", k)
		case <-time.After(100 * time.Millisecond):
		}
	})

	t.Run("OSCIncompleteThenTimeout", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(&delayedChunkReader{
			chunks: [][]byte{[]byte("\x1b]0;title"), {}},
			delay:  150 * time.Millisecond,
		}, ch)
		select {
		case k := <-ch:
			t.Fatalf("expected no key from incomplete OSC, got %q", k)
		case <-time.After(200 * time.Millisecond):
		}
	})
}

func TestReadKeysMouseFormats(t *testing.T) {
	t.Run("X10LeftClick", func(t *testing.T) {
		ch := make(chan string, 4)
		// \x1b[M followed by 3 bytes: button+32, x+32, y+32
		// Left click at (1,1): button=0+32=32, x=1+32=33, y=1+32=33
		go ReadKeys(strings.NewReader("\x1b[M\x20\x21\x21"), ch)
		select {
		case k := <-ch:
			if k != "mouse-left@0,0" {
				t.Fatalf("expected mouse-left@0,0, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for X10 mouse")
		}
	})

	t.Run("X10WheelUp", func(t *testing.T) {
		ch := make(chan string, 4)
		// Wheel up at (5,3): button=64+32=96, x=5+32=37, y=3+32=35
		go ReadKeys(strings.NewReader("\x1b[M\x60\x25\x23"), ch)
		select {
		case k := <-ch:
			if k != "mouse-wheel-up@4,2" {
				t.Fatalf("expected mouse-wheel-up@4,2, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for X10 wheel")
		}
	})

	t.Run("URXVTLeftClick", func(t *testing.T) {
		ch := make(chan string, 4)
		// Left click at (1,1): Cb=0+32=32, Cx=1, Cy=1
		go ReadKeys(strings.NewReader("\x1b[32;1;1M"), ch)
		select {
		case k := <-ch:
			if k != "mouse-left@0,0" {
				t.Fatalf("expected mouse-left@0,0, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for URXVT mouse")
		}
	})

	t.Run("URXVTWheelDown", func(t *testing.T) {
		ch := make(chan string, 4)
		// Wheel down at (10,5): Cb=65+32=97, Cx=10, Cy=5
		go ReadKeys(strings.NewReader("\x1b[97;10;5M"), ch)
		select {
		case k := <-ch:
			if k != "mouse-wheel-down@9,4" {
				t.Fatalf("expected mouse-wheel-down@9,4, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for URXVT wheel")
		}
	})

	t.Run("URXVTRelease", func(t *testing.T) {
		ch := make(chan string, 4)
		// Release at (1,1): Cb=3+32=35, Cx=1, Cy=1
		go ReadKeys(strings.NewReader("\x1b[35;1;1M"), ch)
		select {
		case k := <-ch:
			if k != "mouse-release@0,0" {
				t.Fatalf("expected mouse-release@0,0, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for URXVT release")
		}
	})
}

func TestReadKeysExtendedModifiers(t *testing.T) {
	t.Run("MetaUp", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b[1;9A"), ch)
		select {
		case k := <-ch:
			if k != "meta-up" {
				t.Fatalf("expected meta-up, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for meta-up")
		}
	})

	t.Run("SuperUp", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b[1;17A"), ch)
		select {
		case k := <-ch:
			if k != "super-up" {
				t.Fatalf("expected super-up, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for super-up")
		}
	})

	t.Run("HyperUp", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b[1;33A"), ch)
		select {
		case k := <-ch:
			if k != "hyper-up" {
				t.Fatalf("expected hyper-up, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for hyper-up")
		}
	})

	t.Run("CtrlMetaUp", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b[1;13A"), ch)
		select {
		case k := <-ch:
			if k != "ctrl-meta-up" {
				t.Fatalf("expected ctrl-meta-up, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for ctrl-meta-up")
		}
	})

	t.Run("KittyPrintScreen", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b[57388;1u"), ch)
		select {
		case k := <-ch:
			if k != "print-screen" {
				t.Fatalf("expected print-screen, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for print-screen")
		}
	})

	t.Run("KittyScrollLock", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b[57389;1u"), ch)
		select {
		case k := <-ch:
			if k != "scroll-lock" {
				t.Fatalf("expected scroll-lock, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for scroll-lock")
		}
	})

	t.Run("KittyPause", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b[57390;1u"), ch)
		select {
		case k := <-ch:
			if k != "pause" {
				t.Fatalf("expected pause, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for pause")
		}
	})

	t.Run("KittyMenu", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x1b[57391;1u"), ch)
		select {
		case k := <-ch:
			if k != "menu" {
				t.Fatalf("expected menu, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for menu")
		}
	})

	t.Run("CtrlSpace", func(t *testing.T) {
		ch := make(chan string, 4)
		go ReadKeys(strings.NewReader("\x00"), ch)
		select {
		case k := <-ch:
			if k != "ctrl-space" {
				t.Fatalf("expected ctrl-space, got %q", k)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for ctrl-space")
		}
	})
}

func TestReadKeysKittyAndGeneric(t *testing.T) {
	t.Run("KittyKeypad", func(t *testing.T) {
		ch := make(chan string, 16)
		go ReadKeys(strings.NewReader(
			"\x1b[57345;1u\x1b[57348;1u\x1b[57349;1u\x1b[57350;1u"+
				"\x1b[57351;1u\x1b[57352;1u\x1b[57353;1u\x1b[57354;1u"+
				"\x1b[57355;1u\x1b[57356;1u\x1b[57357;1u\x1b[57344;1u",
		), ch)
		var got []string
		for len(got) < 12 {
			select {
			case k := <-ch:
				got = append(got, k)
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for kitty keypad keys")
			}
		}
		want := []string{
			"kp-f1", "kp-f4", "kp-home", "kp-end",
			"kp-pageup", "kp-pagedown", "kp-left", "kp-right",
			"kp-up", "kp-down", "kp-begin", "kp-enter",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("GenericPrintable", func(t *testing.T) {
		ch := make(chan string, 8)
		go ReadKeys(strings.NewReader("qs[]S?"), ch)
		var got []string
		for len(got) < 6 {
			select {
			case k := <-ch:
				got = append(got, k)
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for printable keys")
			}
		}
		want := []string{"q", "s", "[", "]", "S", "?"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})
}
