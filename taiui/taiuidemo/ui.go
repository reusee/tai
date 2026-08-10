package main

import (
	"fmt"
	"time"

	"github.com/gdamore/tcell/v3/vt"
	"github.com/reusee/tai/taiui"
)

const (
	minWidth  = 80
	minHeight = 24
)

const TheoryOfProviders = `
taiuidemo provider theory:
- State and UI are split into providers: each piece of state is its own
  provider type, and each component is its own provider type. dscope caches
  each provider result and recomputes only the providers whose dependencies
  changed, so a state change recomputes exactly the components that depend
  on it.
- The event loop forks the current scope with only the providers that
  changed. Forking from the current scope preserves the cached results of
  unchanged providers; forking from the base scope would discard them and
  recompute everything. dscope compacts the definition chain internally,
  so the scope stack stays flat no matter how many forks the event loop
  performs, and resolutions stay O(1). The event loop renders only when
  state changed, so a key press that changes nothing skips the render
  entirely.
- The framebuffer content is derived state: provideFrameBufferContent
  builds it from the frame counter, so the ball is a pure function of
  state and the event loop never mutates the content in place.
- The root provider composes the component providers; it is the only
  provider that depends on all of them, so it is recomputed on every state
  change, but the components themselves are recomputed only when their own
  dependencies change.
`

// State providers: each piece of demo state is its own provider type, so
// forking one piece recomputes only the components that depend on it.
type Width int
type Height int
type Scroll int
type Toggle bool
type Frame int64
type Now time.Time

// W1Weight is the flex weight of the w1 box in the Box panel. The left
// and right arrow keys adjust it, changing the w1:w2 ratio.
type W1Weight int

// Modal is the demo's modal state: when true, an Overlay stacks a modal
// over the main UI. The m key toggles it.
type Modal bool

// Rotation is the clockwise rotation of the four panel contents: the tab
// key advances it, moving each panel one position clockwise.
type Rotation int

// Component providers: each panel is its own provider type, so dscope can
// cache it independently and recompute it only when its dependencies change.
type Header taiui.Element
type Footer taiui.Element
type PanelText taiui.Element
type PanelScroll taiui.Element
type PanelBox taiui.Element
type PanelDynamic taiui.Element

func rootProvider(
	w Width,
	h Height,
	modal Modal,
	rotation Rotation,
	hdr Header,
	ftr Footer,
	pt PanelText,
	ps PanelScroll,
	pb PanelBox,
	pd PanelDynamic,
) taiui.Root {
	if int(w) < minWidth || int(h) < minHeight {
		return taiui.Root{Element: taiui.Rect(
			// A Box override pins the banner to the middle third of the
			// screen regardless of the box the parent would assign.
			taiui.Box{Top: int(h) / 3, Left: 0, Bottom: 2 * int(h) / 3, Right: int(w)},
			taiui.Border(true),
			taiui.Fill(true),
			taiui.BGColor(taiui.HexColor(0x300000)),
			taiui.Text(
				fmt.Sprintf("too small: %dx%d, need %dx%d", int(w), int(h), minWidth, minHeight),
				taiui.AlignCenter,
			),
		)}
	}
	// The four panels are arranged in clockwise order (top-left,
	// top-right, bottom-right, bottom-left) and rotated by the rotation
	// state, so the tab key moves each panel one position clockwise.
	rotated := rotatePanels(pt, pb, pd, ps, rotation)
	root := taiui.Root{Element: taiui.Column(
		taiui.Weighted(1, taiui.Element(hdr)),
		taiui.Weighted(22, taiui.Row(
			taiui.Weighted(1, taiui.Column(
				taiui.Weighted(1, rotated[0]),
				taiui.Weighted(1, rotated[3]),
			)),
			taiui.Weighted(1, taiui.Column(
				taiui.Weighted(1, rotated[1]),
				taiui.Weighted(1, rotated[2]),
			)),
		)),
		taiui.Weighted(1, taiui.Element(ftr)),
	)}
	if bool(modal) {
		// The modal is part of the element tree, derived from state: an
		// Overlay stacks it over the main UI, so toggling the modal state
		// re-renders the overlay without any imperative layer management.
		root.Element = taiui.Overlay(
			root.Element,
			taiui.Rect(
				taiui.Box{Top: int(h) / 4, Left: int(w) / 4, Bottom: 3 * int(h) / 4, Right: 3 * int(w) / 4},
				taiui.Border(true),
				taiui.Fill(true),
				taiui.BGColor(taiui.HexColor(0x202020)),
				taiui.Padding(1),
				taiui.Column(
					taiui.Weighted(1, taiui.Text("Modal", taiui.Bold(true), taiui.AlignCenter)),
					taiui.Weighted(1, taiui.Text("m closes", taiui.AlignCenter)),
				),
			),
		)
	}
	return root
}

// rotatePanels arranges the four panels for the given clockwise rotation.
// The panels are given in clockwise order (top-left, top-right,
// bottom-right, bottom-left); position p shows the panel originally at
// (p - rotation) mod 4.
func rotatePanels(pt PanelText, pb PanelBox, pd PanelDynamic, ps PanelScroll, rotation Rotation) [4]taiui.Element {
	panels := [...]taiui.Element{
		taiui.Element(pt),
		taiui.Element(pb),
		taiui.Element(pd),
		taiui.Element(ps),
	}
	var rotated [4]taiui.Element
	for p := 0; p < 4; p++ {
		rotated[p] = panels[rotatedPanelIndex(p, int(rotation))]
	}
	return rotated
}

// rotatedPanelIndex returns the index (in clockwise order) of the panel
// shown at position p under the given clockwise rotation.
func rotatedPanelIndex(p, rotation int) int {
	return (p - rotation%4 + 4) % 4
}

func provideHeader(t Toggle, now Now) Header {
	return Header(header(t, now))
}

func provideFooter() Footer {
	return Footer(footer())
}

func providePanelText() PanelText {
	return PanelText(panelText())
}

func providePanelScroll(scroll Scroll) PanelScroll {
	return PanelScroll(panelScroll(scroll))
}

func providePanelBox(t Toggle, w1 W1Weight) PanelBox {
	return PanelBox(panelBox(t, w1))
}

func providePanelDynamic(frame Frame, toggle Toggle, fb *taiui.FrameBufferContent) PanelDynamic {
	return PanelDynamic(panelDynamic(frame, toggle, fb))
}

// provideFrameBufferContent derives the framebuffer content from the
// frame counter: the ball position is a pure function of state, so the
// content is rebuilt by dscope when the frame changes, and the event
// loop never mutates it in place.
func provideFrameBufferContent(frame Frame) *taiui.FrameBufferContent {
	fb := taiui.NewFrameBufferContent(fbWidth, fbHeight)
	fb.Clear(vt.BaseStyle.WithBg(taiui.HexColor(0x101010)))
	bx := bounce(int(frame*2), fbWidth-1)
	by := bounce(int(frame), fbHeight-1)
	fb.SetContent(bx, by, '\u25CF', nil, vt.BaseStyle.WithFg(taiui.HexColor(0xff8800)))
	return fb
}

func header(t Toggle, now Now) taiui.Element {
	return taiui.Row(
		taiui.Weighted(3, taiui.Text(
			" taiui demo ",
			taiui.Bold(true),
			taiui.Alt(bool(t), taiui.BGColor(taiui.HexColor(0x303030)), taiui.BGColor(taiui.HexColor(0x202020))),
			taiui.Fill(true),
		)),
		taiui.Weighted(2, taiui.Text(
			fmt.Sprintf("%s \u00b7 %s", runeWidthEnv(), time.Time(now).Format("15:04:05")),
			taiui.AlignRight,
			taiui.Fill(true),
			taiui.BGColor(taiui.HexColor(0x202020)),
		)),
	)
}

func footer() taiui.Element {
	return taiui.Text(
		" \u2191/\u2193 scroll \u00b7 \u2190/\u2192 w1:w2 \u00b7 space toggle \u00b7 m modal \u00b7 tab rotate \u00b7 q quit ",
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

func panelBox(t Toggle, w1 W1Weight) taiui.Element {
	return taiui.Rect(
		taiui.Border(true),
		taiui.Padding(1),
		taiui.Fill(true),
		taiui.BGColor(taiui.HexColor(0x141414)),
		taiui.Column(
			taiui.Weighted(1, panelTitle(fmt.Sprintf("Box \u00b7 Flex \u00b7 w1:w2 = %d:2", int(w1)))),
			taiui.Weighted(5, taiui.Rect(
				taiui.Margin(1),
				taiui.Border(true),
				taiui.BorderType(taiui.BorderRounded),
				bigBoxBorder(t), // zero-arg function spec
				taiui.Fill(true),
				taiui.BGColor(taiui.HexColor(0x181818)),
				taiui.Row(
					taiui.Weighted(int(w1), fillRect("w1", 0x800000)),
					taiui.Weighted(2, fillRect("w2", 0x008000)),
				),
			)),
		),
	)
}

// bigBoxBorder is a zero-argument function spec: element constructors
// evaluate such specs eagerly at construction, so the border color follows
// the toggle state on every rebuild.
func bigBoxBorder(t Toggle) func() taiui.Spec {
	return func() taiui.Spec {
		if bool(t) {
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

func panelScroll(scroll Scroll) taiui.Element {
	return taiui.Rect(
		taiui.Border(true),
		taiui.Padding(1),
		taiui.Fill(true),
		taiui.BGColor(taiui.HexColor(0x141414)),
		taiui.Column(
			taiui.Weighted(1, panelTitle(fmt.Sprintf("Scroll (\u2191\u2193) \u00b7 offset %d", int(scroll)))),
			taiui.Weighted(5, taiui.VerticalScroll(
				taiui.Rect(
					// The right padding keeps wrapped lines clear of the
					// scrollbar column reserved at the window edge.
					taiui.Padding(0, 1, 0, 0),
					taiui.Text(scrollLines(), taiui.Wrap(true)),
				),
				int(scroll),
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

func panelDynamic(frame Frame, toggle Toggle, fb *taiui.FrameBufferContent) taiui.Element {
	return taiui.Rect(
		taiui.Border(true),
		taiui.Padding(1),
		taiui.Fill(true),
		taiui.BGColor(taiui.HexColor(0x141414)),
		taiui.Column(
			taiui.Weighted(1, panelTitle("State \u00b7 FrameBuffer")),
			taiui.Weighted(3, taiui.FrameBuffer(fb)),
			taiui.Weighted(2, taiui.Text(
				fmt.Sprintf("frame %d \u00b7 toggle %v", int64(frame), bool(toggle)),
				taiui.Fill(true),
				taiui.If(bool(toggle), taiui.BGColor(taiui.HexColor(0x103010))),
			)),
		),
	)
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
	if taiui.DisplayWidthOptions().EastAsianWidth {
		return "EA=wide"
	}
	return "EA=narrow"
}
