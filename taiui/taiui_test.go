package taiui

import (
	"testing"

	"github.com/gdamore/tcell/v3/vt"
	"github.com/reusee/dscope"
)

type fakeScreen struct {
	cells map[[2]int]rune
}

func newFakeScreen() *fakeScreen {
	return &fakeScreen{cells: make(map[[2]int]rune)}
}

func (s *fakeScreen) SetContent(x, y int, mainc rune, combc []rune, st Style) {
	s.cells[[2]int{x, y}] = mainc
}

func newTestScope(screen *fakeScreen) Scope {
	return dscope.New(
		func() Box { return Box{0, 0, 25, 80} },
		func() Style { return vt.BaseStyle },
		func() SetContent { return screen.SetContent },
	)
}

func newTestScreen() (Scope, *fakeScreen) {
	screen := newFakeScreen()
	return newTestScope(screen), screen
}

func TestRender(t *testing.T) {
	scope, screen := newTestScreen()

	root := Rect(
		Fill(true),
		Text("Hello"),
	)
	RenderAll(scope, root)

	if r, ok := screen.cells[[2]int{0, 0}]; !ok || r != 'H' {
		t.Fatalf("expected 'H' at cell 0, got %v", r)
	}
}

func TestNestedRect(t *testing.T) {
	scope, screen := newTestScreen()

	root := Rect(
		Fill(true),
		Rect(
			Margin(2),
			Padding(1),
			Text("Nested"),
		),
	)
	RenderAll(scope, root)

	// "Nested" should appear at row 3 (margin 2 + padding 1), col 3.
	if r, ok := screen.cells[[2]int{3, 3}]; !ok || r != 'N' {
		t.Fatalf("expected 'N' at row 3 col 3, got %v", r)
	}
}

func TestFrameBuffer(t *testing.T) {
	scope, screen := newTestScreen()

	fb := NewFrameBuffer(Box{0, 0, 10, 10})
	fb.SetContent(0, 0, 'X', nil, vt.BaseStyle)

	RenderAll(scope, fb)

	if r, ok := screen.cells[[2]int{0, 0}]; !ok || r != 'X' {
		t.Fatalf("expected 'X' at cell 0, got %v", r)
	}
}

func TestElementFuncRendersReturns(t *testing.T) {
	scope, screen := newTestScreen()
	RenderAll(scope, ElementFunc(func() Element {
		return Text("f")
	}))

	if r, ok := screen.cells[[2]int{0, 0}]; !ok || r != 'f' {
		t.Fatalf("expected 'f' at cell 0, got %v", r)
	}
}

func namedBox() Box { return Box{0, 0, 25, 80} }

func TestNamedFunctionSpec(t *testing.T) {
	scope, screen := newTestScreen()
	RenderAll(scope, Rect(namedBox, Fill(true), Text("n")))

	if r, ok := screen.cells[[2]int{0, 0}]; !ok || r != 'n' {
		t.Fatalf("expected 'n' at cell 0, got %v", r)
	}
}
