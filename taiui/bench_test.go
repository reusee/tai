package taiui

import (
	"fmt"
	"testing"

	"github.com/gdamore/tcell/v3/vt"
	"github.com/reusee/dscope"
)

// Benchmarks for the render hot path. The screen discards presented
// frames and returns their cells to the pool, so the reported
// allocations are the per-pass costs of the renderer and the element
// tree, not the frame cells.
type benchmarkReleasingScreen struct{}

func (benchmarkReleasingScreen) Width() int  { return 80 }
func (benchmarkReleasingScreen) Height() int { return 25 }

func (benchmarkReleasingScreen) Present(Frame) {}

func (benchmarkReleasingScreen) ReleaseFrame(frame Frame) {
	ReleaseFrame(frame)
}

func BenchmarkRenderText(b *testing.B) {
	scope := dscope.New(func() Root {
		return Root{Element: Text("hello world")}
	})
	screen := benchmarkReleasingScreen{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Render(scope, screen)
	}
}

func BenchmarkRenderTextFill(b *testing.B) {
	scope := dscope.New(func() Root {
		return Root{Element: Text("hello world", Fill(true))}
	})
	screen := benchmarkReleasingScreen{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Render(scope, screen)
	}
}

func BenchmarkRenderTextWrapped(b *testing.B) {
	scope := dscope.New(func() Root {
		return Root{Element: Rect(
			Box{Top: 0, Left: 0, Bottom: 25, Right: 40},
			Text("one two three four five six seven eight nine ten eleven twelve", Wrap(true)),
		)}
	})
	screen := benchmarkReleasingScreen{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Render(scope, screen)
	}
}

func BenchmarkRenderRectFill(b *testing.B) {
	scope := dscope.New(func() Root {
		return Root{Element: Rect(
			Fill(true),
			BGColor(HexColor(0x141414)),
			Text("content"),
		)}
	})
	screen := benchmarkReleasingScreen{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Render(scope, screen)
	}
}

func BenchmarkRenderInput(b *testing.B) {
	scope := dscope.New(func() Root {
		return Root{Element: Input("hello world", 5)}
	})
	screen := benchmarkReleasingScreen{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Render(scope, screen)
	}
}

func BenchmarkRenderList(b *testing.B) {
	items := make([]string, 100)
	for i := range items {
		items[i] = fmt.Sprintf("item %02d", i)
	}
	scope := dscope.New(func() Root {
		return Root{Element: Rect(
			Box{Top: 0, Left: 0, Bottom: 25, Right: 40},
			List(items, 50),
		)}
	})
	screen := benchmarkReleasingScreen{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Render(scope, screen)
	}
}

func BenchmarkRenderBorderTitle(b *testing.B) {
	scope := dscope.New(func() Root {
		return Root{Element: Rect(
			Border(true),
			Title("Title"),
			Text("content"),
		)}
	})
	screen := benchmarkReleasingScreen{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Render(scope, screen)
	}
}

func BenchmarkFrameDirtyRowsInto(b *testing.B) {
	a := newFrame(80, 24)
	c := newFrame(80, 24)
	// A few cells differ, as in a typical partial update.
	for i := 0; i < 10; i++ {
		c.setCell(i*7%80, i%24, 'x', nil, vt.BaseStyle)
	}
	var buf []int
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf = a.DirtyRowsInto(c, buf[:0])
	}
}
