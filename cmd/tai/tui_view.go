package main

import (
	"fmt"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/taiui"
)

// State providers: each piece of TUI display state is its own provider
// type, so forking one piece recomputes only the components that depend
// on it. See TheoryOfTUI.
type TUISize struct {
	Width  int
	Height int
}

type TUITabs struct {
	Expanded      [3]bool
	Focus         int
	SplitVertical bool
}

type TUIOutputLines []taiui.Line

type TUILogsLines []string

type TUISignals []taiui.Line

// TUIScrolls carries the three per-tab scroll states.
type TUIScrolls [3]taiui.ScrollState

type TUIFinished bool

type TUIGenerating bool

type TUIConfirmQuit bool

// TUIDisplays carries the derived wrapped lines per tab; a collapsed tab's
// entry is nil.
type TUIDisplays [3][]taiui.Line

// Element providers: one type per panel, so each panel is cached and
// recomputed independently. See TheoryOfTUI.
type OutputPanel taiui.Element

type SummaryPanel taiui.Element

type LogsPanel taiui.Element

// TUIView is the provider container for the TUI view: every provider is a
// method on TUIView, so dscope.New(new(TUIView)) creates the view scope.
// State and UI are unified in one provider tree, following the taiuidemo
// pattern. See TheoryOfTUI.
type TUIView struct {
	dscope.Module
}

// Default state providers declare each state piece's initial value, so
// the view scope is self-contained. render() forks the current state
// values over these defaults; dscope resolves the forked providers, so
// derived providers always see the current values. See TheoryOfTUI.
func (v *TUIView) Size() TUISize {
	return TUISize{Width: 80, Height: 25}
}

func (v *TUIView) Tabs() TUITabs {
	return TUITabs{Expanded: [3]bool{}, Focus: -1}
}

func (v *TUIView) OutputLines() TUIOutputLines { return nil }

func (v *TUIView) LogsLines() TUILogsLines { return nil }

func (v *TUIView) Signals() TUISignals { return nil }

func (v *TUIView) Scrolls() TUIScrolls {
	return TUIScrolls{
		{Offset: 1 << 30},
		{Offset: 1 << 30},
		{Offset: 1 << 30},
	}
}

func (v *TUIView) Finished() TUIFinished { return false }

func (v *TUIView) Generating() TUIGenerating { return false }

func (v *TUIView) ConfirmQuit() TUIConfirmQuit { return false }

// tabBox computes the box of one tab under the given tabs and size.
func tabBox(idx int, tabs TUITabs, size TUISize) taiui.Box {
	t := taiui.NewTabs(3)
	t.Expanded = tabs.Expanded[:]
	t.Focus = tabs.Focus
	t.SplitVertical = tabs.SplitVertical
	return t.Boxes(size.Width, size.Height)[idx]
}

// outputTabLabel returns the Output tab's title with the session-state
// hint: "Output (generating...)" while the model is actively generating,
// "Output (done)" after the session ends, and "Output" otherwise. The
// highlight result reports whether the title should be drawn in
// tabActiveLabelFg. Finished takes precedence over generating. See
// TheoryOfTUI.
func outputTabLabel(finished bool, generating bool) (label string, highlight bool) {
	label = tabNames[0]
	switch {
	case finished:
		label = "Output (done)"
	case generating:
		label = "Output (generating...)"
		highlight = true
	}
	return
}

// Displays derives the wrapped, colored lines for each tab from its
// content and the current layout. A collapsed tab's entry is nil; the
// panel providers render a collapsed strip instead. See TheoryOfTUI.
func (v *TUIView) Displays(
	output TUIOutputLines,
	logs TUILogsLines,
	signals TUISignals,
	tabs TUITabs,
	size TUISize,
) TUIDisplays {
	var ret TUIDisplays
	for i := 0; i < 3; i++ {
		if !tabs.Expanded[i] {
			continue
		}
		box := tabBox(i, tabs, size)
		if box.Width() <= 0 || box.Height() <= 0 {
			continue
		}
		// The content width reserves one column for the scrollbar,
		// matching the scroll's visible-width rendering. See TheoryOfTUI.
		contentWidth := max(box.Width()-1, 1)
		switch i {
		case 0:
			ret[i] = taiui.WrapLinesColored([]taiui.Line(output), contentWidth)
		case 1:
			ret[i] = taiui.WrapLinesColored([]taiui.Line(signals), contentWidth)
		case 2:
			// The logs tab alternates line backgrounds derived from its
			// tab background, so consecutive log entries are visually
			// distinct. The base is the focused or unfocused tab
			// background, whichever the logs tab currently has.
			// See taiui.PlainLines.
			base := panelStyle.BaseBG
			if tabs.Focus == 2 {
				base = panelStyle.FocusBG
			}
			ret[i] = taiui.WrapLinesColored(
				taiui.PlainLines([]string(logs), base),
				contentWidth,
			)
		}
	}
	return ret
}

// Element providers turn a tab's display and scroll state into a
// taiui.Panel, or a taiui.CollapsedPanel when the tab is collapsed. A
// degenerate box yields a nil panel, which Root skips. See TheoryOfTUI.
func (v *TUIView) OutputPanel(
	displays TUIDisplays,
	tabs TUITabs,
	size TUISize,
	scrolls TUIScrolls,
	finished TUIFinished,
	generating TUIGenerating,
) OutputPanel {
	box := tabBox(0, tabs, size)
	if box.Width() <= 0 || box.Height() <= 0 {
		return nil
	}
	if !tabs.Expanded[0] {
		return OutputPanel(taiui.CollapsedPanel(
			box,
			fmt.Sprintf("%d %s", 1, tabNames[0]),
			tabs.Focus == 0,
			panelStyle,
		))
	}
	label, highlight := outputTabLabel(bool(finished), bool(generating))
	return OutputPanel(taiui.Panel(
		box,
		label,
		highlight,
		displays[0],
		scrolls[0].Offset,
		tabs.Focus == 0,
		scrolls[0].Follow,
		panelStyle,
	))
}

func (v *TUIView) SummaryPanel(
	displays TUIDisplays,
	tabs TUITabs,
	size TUISize,
	scrolls TUIScrolls,
) SummaryPanel {
	box := tabBox(1, tabs, size)
	if box.Width() <= 0 || box.Height() <= 0 {
		return nil
	}
	if !tabs.Expanded[1] {
		return SummaryPanel(taiui.CollapsedPanel(
			box,
			fmt.Sprintf("%d %s", 2, tabNames[1]),
			tabs.Focus == 1,
			panelStyle,
		))
	}
	return SummaryPanel(taiui.Panel(
		box,
		tabNames[1],
		false,
		displays[1],
		scrolls[1].Offset,
		tabs.Focus == 1,
		scrolls[1].Follow,
		panelStyle,
	))
}

func (v *TUIView) LogsPanel(
	displays TUIDisplays,
	tabs TUITabs,
	size TUISize,
	scrolls TUIScrolls,
) LogsPanel {
	box := tabBox(2, tabs, size)
	if box.Width() <= 0 || box.Height() <= 0 {
		return nil
	}
	if !tabs.Expanded[2] {
		return LogsPanel(taiui.CollapsedPanel(
			box,
			fmt.Sprintf("%d %s", 3, tabNames[2]),
			tabs.Focus == 2,
			panelStyle,
		))
	}
	return LogsPanel(taiui.Panel(
		box,
		tabNames[2],
		false,
		displays[2],
		scrolls[2].Offset,
		tabs.Focus == 2,
		scrolls[2].Follow,
		panelStyle,
	))
}

// Root composes the panel elements and the optional quit-confirmation bar
// into the root element. It is the only provider that depends on all
// panels, so it is recomputed whenever any panel changes. See TheoryOfTUI.
func (v *TUIView) Root(
	output OutputPanel,
	summary SummaryPanel,
	logs LogsPanel,
	confirmQuit TUIConfirmQuit,
	size TUISize,
) taiui.Root {
	specs := []any{output, summary, logs}
	var elements []any
	for _, spec := range specs {
		if spec != nil {
			elements = append(elements, spec)
		}
	}
	root := taiui.Root{Element: taiui.Overlay(elements...)}
	if bool(confirmQuit) {
		// A pending quit confirmation draws a confirmation bar over the
		// bottom row of the screen, on top of every tab, so it is always
		// visible. See TheoryOfTUI.
		root.Element = taiui.Overlay(
			root.Element,
			taiui.Rect(
				taiui.Box{Top: size.Height - 1, Left: 0, Bottom: size.Height, Right: size.Width},
				taiui.Fill(true),
				taiui.BGColor(taiui.HexColor(0x800000)),
				taiui.Bold(true),
				taiui.Text(" Quit? Press q again to confirm, any other key to cancel "),
			),
		)
	}
	return root
}
