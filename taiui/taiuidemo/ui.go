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

const TheoryOfDemoArchitecture = `
taiuidemo architecture theory:
- The demo state lives in one State struct; BuildRoot is a plain function
  that turns the current state into a Root element tree, and the event
  loop mutates the State and calls Render only when something changed.
- The demo used to be built on dscope providers: every provider was a
  method on App, each piece of state was its own provider type, each
  panel its own provider, and forking a scope preserved the cached
  results of the unchanged providers so a state change recomputed exactly
  the components that depended on it. The machinery was not worth it: the
  demo rebuilds a full Frame per render and the screens diff whole
  frames, so per-component caching saved nothing while adding a provider
  layer to read and reason about. The simplification removes the scope
  entirely: state is a struct, UI is a function.
- The state split remains in the struct: the key-handled state (scroll,
  toggle, w1 weight, modal, rotation) is mutated by HandleKey; the
  dynamic state (terminal size, frame counter, clock) is updated by the
  event loop. HandleKey reports whether the state changed, so a key
  press that changes nothing (e.g., up at the scroll clamp) skips the
  render entirely.
- The canvas content is derived state: buildCanvas builds it from the
  frame counter, so the ball is a pure function of state and the event
  loop never mutates the content in place. Rebuilding the canvas per
  render is cheap: it is an fbWidth by fbHeight cell grid.
`

// State is the demo's state. The key-handled state lives in the fields
// mutated by HandleKey; the dynamic state (terminal size, frame counter,
// clock) is updated by the event loop. See TheoryOfDemoArchitecture.
type State struct {
	Width    int
	Height   int
	Scroll   int
	Toggle   bool
	W1Weight int
	Modal    bool
	Rotation int
	Frame    int64
	Now      time.Time
}

// maxW1Weight bounds the w1 flex weight adjustable with the left and
// right arrow keys; the weight must stay positive for Weighted.
const maxW1Weight = 10

// HandleKey processes one key and mutates the state. It reports whether
// anything changed, so the event loop skips the render for a key that had
// no effect (e.g., up at the scroll clamp), and whether the key quits the
// demo.
func (s *State) HandleKey(key string) (changed bool, quit bool) {
	switch key {
	case "up":
		// The scroll offset never goes negative: the view clamps at the
		// content start.
		if s.Scroll > 0 {
			s.Scroll--
			changed = true
		}
	case "down":
		s.Scroll++
		changed = true
	case "left":
		// The w1 weight never drops below 1: Weighted requires a positive
		// weight, so the w1 box always keeps a share of the row.
		if s.W1Weight > 1 {
			s.W1Weight--
			changed = true
		}
	case "right":
		if s.W1Weight < maxW1Weight {
			s.W1Weight++
			changed = true
		}
	case "space":
		s.Toggle = !s.Toggle
		changed = true
	case "modal":
		// The modal is part of the element tree, derived from state: an
		// Overlay stacks it over the main UI.
		s.Modal = !s.Modal
		changed = true
	case "tab":
		// The rotation cycles 0..3, so four presses return to the
		// original arrangement.
		s.Rotation = (s.Rotation + 1) % 4
		changed = true
	case "quit":
		return false, true
	}
	return
}

// BuildRoot builds the root element tree from the current state.
func BuildRoot(s State) taiui.Element {
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
	// The four panels are arranged in clockwise order (top-left,
	// top-right, bottom-right, bottom-left) and rotated by the rotation
	// state, so the tab key moves each panel one position clockwise.
	rotated := rotatePanels(
		panelText(),
		panelBox(s.Toggle, s.W1Weight),
		panelDynamic(s.Frame, s.Toggle, buildCanvas(s.Frame)),
		panelScroll(s.Scroll),
		s.Rotation,
	)
	var root taiui.Element = taiui.Column(
		taiui.Weighted(1, header(s.Toggle, s.Now)),
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
		taiui.Weighted(1, footer()),
	)
	if s.Modal {
		// The modal is part of the element tree, derived from state: an
		// Overlay stacks it over the main UI, so toggling the modal state
		// re-renders the overlay without any imperative layer management.
		root = taiui.Overlay(
			root,
			taiui.Rect(
				taiui.Box{Top: s.Height / 4, Left: s.Width / 4, Bottom: 3 * s.Height / 4, Right: 3 * s.Width / 4},
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
func rotatePanels(pt, pb, pd, ps taiui.Element, rotation int) [4]taiui.Element {
	panels := [...]taiui.Element{pt, pb, pd, ps}
	var rotated [4]taiui.Element
	for p := 0; p < 4; p++ {
		rotated[p] = panels[rotatedPanelIndex(p, rotation)]
	}
	return rotated
}

// rotatedPanelIndex returns the index (in clockwise order) of the panel
// shown at position p under the given clockwise rotation.
func rotatedPanelIndex(p, rotation int) int {
	return (p - rotation%4 + 4) % 4
}

// buildCanvas builds the canvas content from the frame counter: the ball
// position is a pure function of state, so the content is rebuilt on every
// render and the event loop never mutates it in place.
func buildCanvas(frame int64) *taiui.CanvasContent {
	fb := taiui.NewCanvasContent(fbWidth, fbHeight)
	fb.Clear(vt.BaseStyle.WithBg(taiui.HexColor(0x101010)))
	bx := bounce(int(frame*2), fbWidth-1)
	by := bounce(int(frame), fbHeight-1)
	fb.SetContent(bx, by, '\u25CF', nil, vt.BaseStyle.WithFg(taiui.HexColor(0xff8800)))
	return fb
}

func header(toggle bool, now time.Time) taiui.Element {
	return taiui.Row(
		taiui.Weighted(3, taiui.Text(
			" taiui demo ",
			taiui.Bold(true),
			taiui.Alt(toggle, taiui.BGColor(taiui.HexColor(0x303030)), taiui.BGColor(taiui.HexColor(0x202020))),
			taiui.Fill(true),
		)),
		taiui.Weighted(2, taiui.Text(
			fmt.Sprintf("%s \u00b7 %s", runeWidthEnv(), now.Format("15:04:05")),
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

func panelBox(toggle bool, w1 int) taiui.Element {
	return taiui.Rect(
		taiui.Border(true),
		taiui.Padding(1),
		taiui.Fill(true),
		taiui.BGColor(taiui.HexColor(0x141414)),
		taiui.Column(
			taiui.Weighted(1, panelTitle(fmt.Sprintf("Box \u00b7 Flex \u00b7 w1:w2 = %d:2", w1))),
			taiui.Weighted(5, taiui.Rect(
				taiui.Margin(1),
				taiui.Border(true),
				taiui.BorderType(taiui.BorderRounded),
				bigBoxBorder(toggle), // zero-arg function spec
				taiui.Fill(true),
				taiui.BGColor(taiui.HexColor(0x181818)),
				taiui.Row(
					taiui.Weighted(w1, fillRect("w1", 0x800000)),
					taiui.Weighted(2, fillRect("w2", 0x008000)),
				),
			)),
		),
	)
}

// bigBoxBorder is a zero-argument function spec: element constructors
// evaluate such specs eagerly at construction, so the border color follows
// the toggle state on every rebuild.
func bigBoxBorder(toggle bool) func() taiui.Spec {
	return func() taiui.Spec {
		if toggle {
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

func panelScroll(scroll int) taiui.Element {
	return taiui.Rect(
		taiui.Border(true),
		taiui.Padding(1),
		taiui.Fill(true),
		taiui.BGColor(taiui.HexColor(0x141414)),
		taiui.Column(
			taiui.Weighted(1, panelTitle(fmt.Sprintf("Scroll (\u2191\u2193) \u00b7 offset %d", scroll))),
			taiui.Weighted(5, taiui.VerticalScroll(
				taiui.Rect(
					// The right padding keeps wrapped lines clear of the
					// scrollbar column reserved at the window edge.
					taiui.Padding(0, 1, 0, 0),
					taiui.Text(scrollLines(), taiui.Wrap(true)),
				),
				scroll,
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

func panelDynamic(frame int64, toggle bool, fb *taiui.CanvasContent) taiui.Element {
	return taiui.Rect(
		taiui.Border(true),
		taiui.Padding(1),
		taiui.Fill(true),
		taiui.BGColor(taiui.HexColor(0x141414)),
		taiui.Column(
			taiui.Weighted(1, panelTitle("State \u00b7 Canvas")),
			taiui.Weighted(3, taiui.Canvas(fb)),
			taiui.Weighted(2, taiui.Text(
				fmt.Sprintf("frame %d \u00b7 toggle %v", frame, toggle),
				taiui.Fill(true),
				taiui.If(toggle, taiui.BGColor(taiui.HexColor(0x103010))),
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
