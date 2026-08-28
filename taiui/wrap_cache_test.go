package taiui

import (
	"strings"
	"testing"
)

func TestWrapCacheColored(t *testing.T) {
	var c WrapCache
	buf := NewLineBuffer(0)

	buf.Append(NoColor, "hello\n")
	display := c.Colored(buf, 40)
	if len(display) != 1 || display[0].Text != "hello" {
		t.Fatalf("unexpected display: %v", display)
	}

	buf.Append(NoColor, "partial text")
	display = c.Colored(buf, 40)
	if len(display) != 2 || display[1].Text != "partial text" {
		t.Fatalf("unexpected display with partial: %v", display)
	}
	if c.count != 1 {
		t.Fatalf("the partial line must not be cached, got count %d", c.count)
	}
	if c.stable != 1 {
		t.Fatalf("expected one stable display line, got %d", c.stable)
	}
	if c.buf != nil {
		t.Fatal("the partial line must not copy the stable lines into scratch")
	}

	buf.Append(NoColor, "\nworld\n")
	display = c.Colored(buf, 40)
	if len(display) != 3 || display[1].Text != "partial text" || display[2].Text != "world" {
		t.Fatalf("unexpected display after completion: %v", display)
	}
	if c.count != 3 {
		t.Fatalf("expected cached count 3, got %d", c.count)
	}
	if c.stable != 3 {
		t.Fatalf("expected three stable display lines after completion, got %d", c.stable)
	}
}

func TestWrapCacheColoredResizes(t *testing.T) {
	var c WrapCache
	buf := NewLineBuffer(0)
	buf.Append(NoColor, strings.Repeat("x", 30)+"\n")

	display := c.Colored(buf, 40)
	if len(display) != 1 {
		t.Fatalf("expected 1 display line at width 40, got %d", len(display))
	}
	if c.count != 1 {
		t.Fatalf("expected cached count 1, got %d", c.count)
	}

	// The width change resets the cache and re-wraps the same source
	// line; the cached source count stays at one wrapped-once line.
	display = c.Colored(buf, 10)
	if len(display) != 3 {
		t.Fatalf("expected 3 display lines at width 10 (hard break), got %d", len(display))
	}
	if c.count != 1 {
		t.Fatalf("expected cached count 1 after resize, got %d", c.count)
	}
}

func TestWrapCachePlain(t *testing.T) {
	var c WrapCache
	buf := NewStringBuffer(0)
	buf.Append([]byte("a\nb\nc\n"))

	base := HexColor(0x0a1428)
	alt := AltBG(base)
	display := c.Plain(buf, 40, base)
	if len(display) != 3 {
		t.Fatalf("expected 3 display lines, got %d", len(display))
	}
	for i, want := range []Color{base, alt, base} {
		if display[i].BGColor != want {
			t.Fatalf("line %d: expected background %#x, got %#x", i, want, display[i].BGColor)
		}
	}

	// The alternation continues across calls: the cached count is the
	// source's logical line index, so the 4th line is odd-shaded.
	buf.Append([]byte("d\n"))
	display = c.Plain(buf, 40, base)
	if len(display) != 4 || display[3].BGColor != alt {
		t.Fatalf("expected the alternation to continue across calls, got %v", display)
	}

	// The partial line carries the shade it will keep once completed:
	// after 4 completed lines it is the 5th (even index), so base.
	buf.Append([]byte("partial"))
	display = c.Plain(buf, 40, base)
	if len(display) != 5 || display[4].Text != "partial" || display[4].BGColor != base {
		t.Fatalf("expected the partial line to carry its future shade, got %v", display)
	}
	if c.stable != 4 {
		t.Fatalf("expected four stable display lines, got %d", c.stable)
	}
	if c.buf != nil {
		t.Fatal("the partial line must not copy the stable lines into scratch")
	}

	// A base change resets the cache and re-derives every shade.
	base2 := HexColor(0x2e2e2e)
	alt2 := AltBG(base2)
	display = c.Plain(buf, 40, base2)
	if len(display) != 5 {
		t.Fatalf("expected 5 display lines after the base change, got %d", len(display))
	}
	for i, want := range []Color{base2, alt2, base2, alt2, base2} {
		if display[i].BGColor != want {
			t.Fatalf("line %d: expected shade %#x re-derived from the new base, got %#x", i, want, display[i].BGColor)
		}
	}
}

func TestWrapCacheLines(t *testing.T) {
	var c WrapCache
	lines := []Line{{Text: "0123456789"}}

	display := c.Lines(lines, 40)
	if len(display) != 1 || display[0].Text != "0123456789" {
		t.Fatalf("unexpected display: %v", display)
	}

	lines = append(lines, Line{Text: "b"})
	display = c.Lines(lines, 40)
	if len(display) != 2 {
		t.Fatalf("expected 2 display lines, got %d", len(display))
	}

	lines = append(lines, Line{Text: "c"})
	display = c.Lines(lines, 40)
	if len(display) != 3 {
		t.Fatalf("expected 3 display lines, got %d", len(display))
	}

	// The width change resets the cache and re-wraps: the long line
	// hard-breaks into two at width 5.
	display = c.Lines(lines, 5)
	if len(display) != 4 {
		t.Fatalf("expected 4 display lines after resize, got %d", len(display))
	}
}

func TestWrapCacheGroups(t *testing.T) {
	var c WrapCache
	base := HexColor(0x0a1428)
	alt := AltBG(base)
	fg := RGBColor(1, 2, 3)

	groups := [][]Line{{{Text: "first", Color: fg}}}
	display := c.Groups(groups, 8, base)
	if len(display) != 1 || display[0].Text != "first" {
		t.Fatalf("unexpected display: %v", display)
	}
	if display[0].BGColor != base || display[0].Color != fg {
		t.Fatalf("expected the group shade with the foreground preserved, got %+v", display[0])
	}

	// The second group flips the shade, and both display lines of the
	// wrapped group carry the group's shade.
	groups = append(groups, []Line{{Text: "0123456789", Color: fg}})
	display = c.Groups(groups, 8, base)
	if len(display) != 3 {
		t.Fatalf("expected 3 display lines, got %d", len(display))
	}
	for i, want := range []Color{base, alt, alt} {
		if display[i].BGColor != want {
			t.Fatalf("line %d: expected shade %#x, got %#x", i, want, display[i].BGColor)
		}
	}

	// A width change resets the cache and re-derives every shade.
	display = c.Groups(groups, 4, base)
	if len(display) != 5 {
		t.Fatalf("expected 5 display lines after resize, got %d", len(display))
	}
	for i, want := range []Color{base, base, alt, alt, alt} {
		if display[i].BGColor != want {
			t.Fatalf("line %d: expected shade %#x after resize, got %#x", i, want, display[i].BGColor)
		}
	}

	// A base change resets the cache and re-derives every shade.
	base2 := HexColor(0x2e2e2e)
	alt2 := AltBG(base2)
	display = c.Groups(groups, 4, base2)
	for i, want := range []Color{base2, base2, alt2, alt2, alt2} {
		if display[i].BGColor != want {
			t.Fatalf("line %d: expected shade %#x after the base change, got %#x", i, want, display[i].BGColor)
		}
	}

	// The caller's group lines are never mutated: the source keeps no
	// background, so a reset re-derives shades instead of stacking them.
	if got := groups[1][0].BGColor; got != NoColor {
		t.Fatalf("expected the source line untouched, got background %#x", got)
	}
}
