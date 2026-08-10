package taiui

import (
	"fmt"

	"github.com/gdamore/tcell/v3/color"
	"github.com/gdamore/tcell/v3/vt"
)

type Style = vt.Style

// StyleFunc transforms a Style into another Style. It is the unit of the
// style chain: elements hold an ordered list of StyleFuncs applied at
// render time, and the StyleFunc methods compose new functions onto an
// existing chain.
type StyleFunc func(Style) Style

// SameStyle is the identity style function: it returns its input
// unchanged. It is the zero point for composing style chains.
var SameStyle = StyleFunc(func(s Style) Style { return s })

func (fn StyleFunc) SetFG(c Color) StyleFunc {
	return func(s Style) Style { return fn(s).WithFg(c) }
}
func (fn StyleFunc) SetBG(c Color) StyleFunc {
	return func(s Style) Style { return fn(s).WithBg(c) }
}

// SetUnderlineColor sets the color of the underline. It is visible only
// when the underline is on.
func (fn StyleFunc) SetUnderlineColor(c Color) StyleFunc {
	return func(s Style) Style { return fn(s).WithUc(c) }
}
func (fn StyleFunc) SetBold(b bool) StyleFunc {
	return func(s Style) Style { return withAttrOn(fn(s), b, vt.Bold) }
}
func (fn StyleFunc) SetUnderline(b bool) StyleFunc {
	return func(s Style) Style { return withAttrOn(fn(s), b, vt.Underline) }
}

// SetUnderlineStyle selects the underline variant. Selecting a style also
// turns the underline on; the VT underline mask holds the variant.
func (fn StyleFunc) SetUnderlineStyle(style UnderlineStyle) StyleFunc {
	if style < 0 || int(style) >= len(underlineStyles) {
		panic(fmt.Errorf("taiui: bad underline style %d", style))
	}
	return func(s Style) Style {
		s = fn(s)
		attr := s.Attr() &^ vt.UnderlineMask
		attr |= underlineStyles[style]
		return s.WithAttr(attr)
	}
}
func (fn StyleFunc) SetItalic(b bool) StyleFunc {
	return func(s Style) Style { return withAttrOn(fn(s), b, vt.Italic) }
}
func (fn StyleFunc) SetStrikeThrough(b bool) StyleFunc {
	return func(s Style) Style { return withAttrOn(fn(s), b, vt.StrikeThrough) }
}
func (fn StyleFunc) SetDim(b bool) StyleFunc {
	return func(s Style) Style { return withAttrOn(fn(s), b, vt.Dim) }
}
func (fn StyleFunc) SetReverse(b bool) StyleFunc {
	return func(s Style) Style { return withAttrOn(fn(s), b, vt.Reverse) }
}
func (fn StyleFunc) SetBlink(b bool) StyleFunc {
	return func(s Style) Style { return withAttrOn(fn(s), b, vt.Blink) }
}
func (fn StyleFunc) SetOverline(b bool) StyleFunc {
	return func(s Style) Style { return withAttrOn(fn(s), b, vt.Overline) }
}
func (fn StyleFunc) And(f2 StyleFunc) StyleFunc {
	return func(s Style) Style { return f2(fn(s)) }
}

// underlineStyles maps the UnderlineStyle values to their VT underline
// attributes.
var underlineStyles = [...]vt.Attr{
	vt.PlainUnderline,
	vt.DoubleUnderline,
	vt.CurlyUnderline,
	vt.DottedUnderline,
	vt.DashedUnderline,
}

type Color = color.Color

// UnderlineStyle selects the variant drawn by the underline. Selecting a
// style also turns the underline on; the variant alone has no effect.
type UnderlineStyle int

// The underline variants, in the same order as the VT underline style
// bits (vt.UnderlineMask).
const (
	PlainUnderline UnderlineStyle = iota
	DoubleUnderline
	CurlyUnderline
	DottedUnderline
	DashedUnderline
)

// HexColor and RGBColor build colors from their components: HexColor takes
// a 24-bit RGB value, and RGBColor takes separate red, green, and blue
// bytes.

var (
	HexColor = color.NewHexColor
	RGBColor = color.NewRGBColor
)

// Toggle specs for the remaining VT attributes. Like Bold and Underline,
// each is a bool spec accepted by every element.
type (
	Blink         bool
	Dim           bool
	Italic        bool
	Overline      bool
	Reverse       bool
	StrikeThrough bool
)

func (Blink) spec()         {}
func (Dim) spec()           {}
func (Italic) spec()        {}
func (Overline) spec()      {}
func (Reverse) spec()       {}
func (StrikeThrough) spec() {}

func towards128(x, n int32) (int32, int32) {
	if x > 128 {
		return x - n, -n
	}
	return x + n, n
}

func withAttrOn(style Style, on bool, attr vt.Attr) Style {
	if on {
		return style.WithAttr(style.Attr() | attr)
	}
	return style.WithAttr(style.Attr() &^ attr)
}

// DarkerOrLighterStyle returns a style whose background, and any
// monochrome foreground, are shifted by n toward the mid-gray 128: on a
// dark theme both become lighter, on a light theme both darker. Colored
// foregrounds and unset colors are preserved.
func DarkerOrLighterStyle(style Style, n int32) Style {
	fg := style.Fg()
	bg := style.Bg()
	r, g, b := fg.RGB()
	r2, g2, b2 := bg.RGB()
	if r2 >= 0 {
		r2, _ = towards128(r2, n)
		g2, _ = towards128(g2, n)
		b2, _ = towards128(b2, n)
		bg = color.NewRGBColor(r2, g2, b2)
	}
	if r >= 0 && r == g && g == b {
		r, _ = towards128(r, n)
		g, _ = towards128(g, n)
		b, _ = towards128(b, n)
		fg = color.NewRGBColor(r, g, b)
	}
	return style.WithFg(fg).WithBg(bg)
}
