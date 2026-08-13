package taiui

import (
	"fmt"
	"testing"

	"github.com/gdamore/tcell/v3/color"
)

func TestLineBufferSplitsLines(t *testing.T) {
	b := NewLineBuffer(0)
	b.Append(0, "hello\nworld\n")
	lines := b.Lines()
	if len(lines) != 2 || lines[0].Text != "hello" || lines[1].Text != "world" {
		t.Fatalf("unexpected lines: %v", lines)
	}
	if b.HasPartial() {
		t.Fatal("expected no partial line")
	}
}

func TestLineBufferPartial(t *testing.T) {
	b := NewLineBuffer(0)
	b.Append(0, "foo")
	if !b.HasPartial() {
		t.Fatal("expected partial line")
	}
	b.Append(0, "bar\n")
	lines := b.Lines()
	if len(lines) != 1 || lines[0].Text != "foobar" {
		t.Fatalf("unexpected lines: %v", lines)
	}
	if b.HasPartial() {
		t.Fatal("expected no partial line")
	}
	b.Append(0, "baz")
	lines = b.Lines()
	if len(lines) != 2 || lines[1].Text != "baz" {
		t.Fatalf("unexpected lines: %v", lines)
	}
}

func TestLineBufferMaxLines(t *testing.T) {
	b := NewLineBuffer(2)
	for i := 0; i < 5; i++ {
		b.Append(0, fmt.Sprintf("line %d\n", i))
	}
	lines := b.Lines()
	if len(lines) != 2 || lines[0].Text != "line 3" || lines[1].Text != "line 4" {
		t.Fatalf("unexpected lines: %v", lines)
	}
}

func TestStringBufferAppendReturnsCompletedLines(t *testing.T) {
	b := NewStringBuffer(0)
	if got := b.Append([]byte("foo")); len(got) != 0 {
		t.Fatalf("expected no completed lines, got %v", got)
	}
	got := b.Append([]byte("bar\nbaz\n"))
	if len(got) != 2 || got[0] != "foobar" || got[1] != "baz" {
		t.Fatalf("unexpected completed lines: %v", got)
	}
	if b.HasPartial() {
		t.Fatal("expected no partial line")
	}
}

func TestWrapLinesColoredCarriesColors(t *testing.T) {
	lines := []Line{
		{Text: "aaa bbb", Color: color.PaletteColor(12)},
		{Text: "ccc", Color: 0},
	}
	wrapped := WrapLinesColored(lines, 5)
	want := []Line{
		{Text: "aaa", Color: color.PaletteColor(12)},
		{Text: "bbb", Color: color.PaletteColor(12)},
		{Text: "ccc", Color: 0},
	}
	if len(wrapped) != len(want) {
		t.Fatalf("expected %d lines, got %d: %v", len(want), len(wrapped), wrapped)
	}
	for i := range want {
		if wrapped[i] != want[i] {
			t.Fatalf("line %d: got %+v, want %+v", i, wrapped[i], want[i])
		}
	}
}

func TestPlainLinesAlternatesBackgrounds(t *testing.T) {
	base := HexColor(0x0a1428)
	lines := PlainLines([]string{"a", "b", "c"}, base)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	for i, line := range lines {
		want := base
		if i%2 == 1 {
			want = AltBG(base)
		}
		if line.BGColor != want {
			t.Fatalf("line %d: expected background %#x, got %#x", i, want, line.BGColor)
		}
		if line.Color != 0 {
			t.Fatalf("line %d: expected no foreground color, got %#x", i, line.Color)
		}
	}
	if AltBG(base) == base {
		t.Fatal("alternate background must differ from the base")
	}
}

func TestAltBG(t *testing.T) {
	for _, base := range []Color{HexColor(0x0a1428), HexColor(0x2e2e2e)} {
		r1, g1, b1 := base.RGB()
		r2, g2, b2 := AltBG(base).RGB()
		if !(r2 > r1 && g2 > g1 && b2 > b1) {
			t.Fatalf("expected alternate lighter than base %#x, got %#x %#x %#x -> %#x %#x %#x",
				base, r1, g1, b1, r2, g2, b2)
		}
	}
}

func TestLinesElementGroupsColors(t *testing.T) {
	base := HexColor(0x0a1428)
	alt := AltBG(base)
	lines := []Line{
		{Text: "first", BGColor: base},
		{Text: "second", BGColor: alt},
	}
	element := LinesElement(lines, Box{Top: 0, Left: 0, Bottom: 2, Right: 10})
	screen := newFakeScreen(10, 2)
	Render(NewBaseScope(func() Root { return Root{Element: element} }), screen)
	if len(screen.frames) == 0 {
		t.Fatal("expected a rendered frame")
	}
	frame := screen.frames[len(screen.frames)-1]

	wantR, wantG, wantB := base.RGB()
	cell := frame.Cells[9] // row 0, rightmost column: a fill cell
	if !cell.Set {
		t.Fatal("expected the first row painted with its background")
	}
	if r, g, b := cell.Style.Bg().RGB(); r != wantR || g != wantG || b != wantB {
		t.Fatalf("expected base background %#x, got %#x %#x %#x", base, r, g, b)
	}

	wantR, wantG, wantB = alt.RGB()
	cell = frame.Cells[19] // row 1, rightmost column: a fill cell
	if !cell.Set {
		t.Fatal("expected the second row painted with its background")
	}
	if r, g, b := cell.Style.Bg().RGB(); r != wantR || g != wantG || b != wantB {
		t.Fatalf("expected alternate background %#x, got %#x %#x %#x", alt, r, g, b)
	}
}
