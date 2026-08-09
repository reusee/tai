package taiui

import (
	"testing"

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
