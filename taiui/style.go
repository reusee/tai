package taiui

import (
	"github.com/gdamore/tcell/v3/color"
	"github.com/gdamore/tcell/v3/vt"
)

type Style = vt.Style

type StyleFunc func(Style) Style

var SameStyle = StyleFunc(func(s Style) Style { return s })

func (fn StyleFunc) SetFG(c Color) StyleFunc {
	return func(s Style) Style { return fn(s).WithFg(c) }
}
func (fn StyleFunc) SetBG(c Color) StyleFunc {
	return func(s Style) Style { return fn(s).WithBg(c) }
}
func (fn StyleFunc) SetBold(b bool) StyleFunc {
	return func(s Style) Style { return withAttrOn(fn(s), b, vt.Bold) }
}
func (fn StyleFunc) SetUnderline(b bool) StyleFunc {
	return func(s Style) Style { return withAttrOn(fn(s), b, vt.Underline) }
}
func (fn StyleFunc) And(f2 StyleFunc) StyleFunc {
	return func(s Style) Style { return f2(fn(s)) }
}

type Color = color.Color

var (
	HexColor = color.NewHexColor
	RGBColor = color.NewRGBColor
)

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

func DarkerOrLighterStyle(style Style, n int32) Style {
	fg := style.Fg()
	bg := style.Bg()
	r, g, b := fg.RGB()
	mono := r == g && g == b
	r2, g2, b2 := bg.RGB()
	r2, d := towards128(r2, n)
	if mono {
		r += d
	}
	g2, d = towards128(g2, n)
	if mono {
		g += d
	}
	b2, d = towards128(b2, n)
	if mono {
		b += d
	}
	return style.
		WithFg(color.NewRGBColor(r, g, b)).
		WithBg(color.NewRGBColor(r2, g2, b2))
}

func applyStyleSpec(style Style, v any) (Style, bool) {
	switch v := v.(type) {
	case FGColor:
		return style.WithFg(Color(v)), true
	case BGColor:
		return style.WithBg(Color(v)), true
	case Style:
		return v, true
	case StyleFunc:
		return v(style), true
	case Bold:
		return withAttrOn(style, bool(v), vt.Bold), true
	case Underline:
		return withAttrOn(style, bool(v), vt.Underline), true
	}
	return style, false
}
