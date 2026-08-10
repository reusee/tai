package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gdamore/tcell/v3/vt"
	"github.com/reusee/tai/taiui"
)

const (
	minWidth  = 80
	minHeight = 24
)

// State is the demo's mutable state. A state change is a scope fork: the
// Root provider re-evaluates and the next render reflects the change.
type State struct {
	Width  int
	Height int
	Scroll int
	Toggle bool
	Frame  int64
	Time   time.Time
}

func buildUI(s State, fb *taiui.FrameBufferContent) taiui.Element {
	if s.Width < minWidth || s.Height < minHeight {
		return taiui.Rect(
			// A Box override pins the banner to the middle third of the
			// screen regardless of the box the parent would assign.
			taiui.Box{Top: s.Height / 3, Left: 0, Bottom: 2 * s.Height / 3, Right: s.Width},
			taiui.Border(true),
			taiui.Fill(true),
			taiui.BGColor(taiui.HexColor(0x300000)),
			taiui.Text(
				fmt.Sprintf("too small: %dx%d, need %dx%d", s.Width, s.Height, minWidth, minHeight),
				taiui.AlignCenter,
			),
		)
	}
	return taiui.Column(
		taiui.Weighted(1, header(s)),
		taiui.Weighted(22, taiui.Row(
			taiui.Weighted(1, taiui.Column(
				taiui.Weighted(1, panelText()),
				taiui.Weighted(1, panelScroll(s)),
			)),
			taiui.Weighted(1, taiui.Column(
				taiui.Weighted(1, panelBox(s)),
				taiui.Weighted(1, panelDynamic(s, fb)),
			)),
		)),
		taiui.Weighted(1, footer()),
	)
}

func header(s State) taiui.Element {
	return taiui.Row(
		taiui.Weighted(3, taiui.Text(
			" taiui demo ",
			taiui.Bold(true),
			taiui.Alt(s.Toggle, taiui.BGColor(taiui.HexColor(0x303030)), taiui.BGColor(taiui.HexColor(0x202020))),
			taiui.Fill(true),
		)),
		taiui.Weighted(2, taiui.Text(
			fmt.Sprintf("%s \u00b7 %s", runeWidthEnv(), s.Time.Format("15:04:05")),
			taiui.AlignRight,
			taiui.Fill(true),
			taiui.BGColor(taiui.HexColor(0x202020)),
		)),
	)
}

func footer() taiui.Element {
	return taiui.Text(
		" \u2191/\u2193 scroll \u00b7 space toggle \u00b7 q quit ",
		taiui.Dim(true),
		taiui.Fill(true),
		taiui.BGColor(taiui.HexColor(0x181818)),
	)
}

func panelTitle(title string) taiui.Element {
	return taiui.Text(title, taiui.Bold(true), taiui.FGColor(taiui.HexColor(0xffcc00)))
}

func panelText() taiui.Element {
	return taiui.Rect(
		taiui.Border(true),
		taiui.Padding(1),
		taiui.Fill(true),
		taiui.BGColor(taiui.HexColor(0x141414)),
		taiui.Column(
			taiui.Weighted(1, panelTitle("Text \u00b7 Style")),
			taiui.Weighted(1, taiui.Row(
				taiui.Weighted(1, taiui.Text("left", taiui.AlignLeft, taiui.Fill(true))),
				taiui.Weighted(1, taiui.Text("center", taiui.AlignCenter, taiui.Fill(true))),
				taiui.Weighted(1, taiui.Text("right", taiui.AlignRight, taiui.Fill(true))),
			)),
			taiui.Weighted(1, taiui.Text("e\u0301 cluster \u00b7 \U0001F469\u200d\U0001F4BB wide \u00b7 \u00a1 ambig", taiui.Fill(true))),
			taiui.Weighted(1, taiui.Text("rainbow", taiui.OffsetStyleFunc(rainbowStyle), taiui.Bold(true), taiui.Fill(true))),
			taiui.Weighted(1, taiui.Text("Dim Blink Strike Overline", taiui.Dim(true), taiui.Blink(true), taiui.StrikeThrough(true), taiui.Overline(true), taiui.Fill(true))),
			taiui.Weighted(1, taiui.Row(
				taiui.Weighted(1, taiui.Text("Double", taiui.DoubleUnderline, taiui.Fill(true))),
				taiui.Weighted(1, taiui.Text("Curly", taiui.CurlyUnderline, taiui.Fill(true))),
				taiui.Weighted(1, taiui.Text("Dotted", taiui.DottedUnderline, taiui.Fill(true))),
				taiui.Weighted(1, taiui.Text("Dashed", taiui.DashedUnderline, taiui.UnderlineColor(taiui.HexColor(0x00ffff)), taiui.Fill(true))),
			)),
		),
	)
}

var rainbowColors = [...]int32{0xff0000, 0xff8800, 0xffff00, 0x00ff00, 0x00ffff, 0x0000ff, 0xff00ff}

func rainbowStyle(offset int) taiui.StyleFunc {
	return taiui.SameStyle.SetFG(taiui.HexColor(rainbowColors[offset%len(rainbowColors)]))
}

func panelBox(s State) taiui.Element {
	return taiui.Rect(
		taiui.Border(true),
		taiui.Padding(1),
		taiui.Fill(true),
		taiui.BGColor(taiui.HexColor(0x141414)),
		taiui.Column(
			taiui.Weighted(1, panelTitle("Box \u00b7 Flex")),
			taiui.Weighted(5, taiui.Rect(
				taiui.Margin(1),
				taiui.Border(true),
				bigBoxBorder(s), // zero-arg function spec
				taiui.Fill(true),
				taiui.BGColor(taiui.HexColor(0x181818)),
				taiui.Row(
					taiui.Weighted(1, fillRect("w1", 0x800000)),
					taiui.Weighted(2, fillRect("w2", 0x008000)),
				),
			)),
		),
	)
}

// bigBoxBorder is a zero-argument function spec: element constructors
// evaluate such specs eagerly at construction, so the border color follows
// the toggle state on every rebuild.
func bigBoxBorder(s State) func() taiui.Spec {
	return func() taiui.Spec {
		if s.Toggle {
			return taiui.BorderStyle(taiui.SameStyle.SetFG(taiui.HexColor(0xff8800)))
		}
		return taiui.BorderStyle(taiui.SameStyle.SetFG(taiui.HexColor(0x0088ff)))
	}
}

func fillRect(label string, bg int32) taiui.Element {
	return taiui.Rect(
		taiui.Fill(true),
		taiui.BGColor(taiui.HexColor(bg)),
		taiui.Text(label, taiui.AlignCenter),
	)
}

func panelScroll(s State) taiui.Element {
	return taiui.Rect(
		taiui.Border(true),
		taiui.Padding(1),
		taiui.Fill(true),
		taiui.BGColor(taiui.HexColor(0x141414)),
		taiui.Column(
			taiui.Weighted(1, panelTitle(fmt.Sprintf("Scroll (\u2191\u2193) \u00b7 offset %d", s.Scroll))),
			taiui.Weighted(5, taiui.VerticalScroll(
				taiui.Rect(
					// The right padding keeps wrapped lines clear of the
					// scrollbar column reserved at the window edge.
					taiui.Padding(0, 1, 0, 0),
					taiui.Text(scrollLines(), taiui.Wrap(true)),
				),
				s.Scroll,
				taiui.Scrollbar(true),
				taiui.Fill(true),
			)),
		),
	)
}

func scrollLines() []string {
	lines := []string{
		"\u2191/\u2193 scrolls \u00b7 scrollbar on the right",
		"the view clamps at the content extent",
		"\U0001F469\u200d\U0001F4BB is one wide cluster",
		"",
	}
	for i := 0; i < 60; i++ {
		lines = append(lines, fmt.Sprintf("row %02d", i))
	}
	lines = append(lines, "",
		"the end - a wrapped paragraph: one two three four five six seven eight nine ten eleven twelve thirteen",
	)
	return lines
}

func panelDynamic(s State, fb *taiui.FrameBufferContent) taiui.Element {
	return taiui.Rect(
		taiui.Border(true),
		taiui.Padding(1),
		taiui.Fill(true),
		taiui.BGColor(taiui.HexColor(0x141414)),
		taiui.Column(
			taiui.Weighted(1, panelTitle("State \u00b7 FrameBuffer")),
			taiui.Weighted(3, taiui.FrameBuffer(fb)),
			taiui.Weighted(2, taiui.Text(
				fmt.Sprintf("frame %d \u00b7 toggle %v", s.Frame, s.Toggle),
				taiui.Fill(true),
				taiui.If(s.Toggle, taiui.BGColor(taiui.HexColor(0x103010))),
			)),
		),
	)
}

// drawBall redraws the framebuffer canvas: each animation tick clears the
// canvas and places the ball at a position derived from the frame counter.
func drawBall(fb *taiui.FrameBufferContent, frame int64) {
	clear := vt.BaseStyle.WithBg(taiui.HexColor(0x101010))
	for y := 0; y < fbHeight; y++ {
		for x := 0; x < fbWidth; x++ {
			fb.SetContent(x, y, ' ', nil, clear)
		}
	}
	bx := bounce(int(frame*2), fbWidth-1)
	by := bounce(int(frame), fbHeight-1)
	fb.SetContent(bx, by, '\u25CF', nil, vt.BaseStyle.WithFg(taiui.HexColor(0xff8800)))
}

// bounce maps a counter onto a 0..span sawtooth, so the ball appears to
// bounce between the canvas edges.
func bounce(v, span int) int {
	p := v % (2 * span)
	if p > span {
		p = 2*span - p
	}
	return p
}

func runeWidthEnv() string {
	if rw := strings.ToLower(os.Getenv("RUNEWIDTH_EASTASIAN")); rw == "1" || rw == "true" || rw == "yes" {
		return "EA=wide"
	}
	return "EA=narrow"
}
