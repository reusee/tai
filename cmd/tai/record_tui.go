package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3/color"
	"github.com/gdamore/tcell/v3/tty"
	"github.com/reusee/dscope"
	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/records"
	"github.com/reusee/tai/taiui"
	"github.com/reusee/tai/tree"
)

const TheoryOfRecordBrowser = `
Record browser theory (cmd/tai):
- The record subcommand's TUI is a dedicated browser, not the generation
  TUI: one tab — the record tab — with no generation state, no output
  pane, and no chat input. It owns its own terminal session
  (taiui.Session) and renders two views of that one tab: the record list
  and the tree of an opened record.
- The RecordBrowserEnabled marker states that the selected command's
  TUI is the browser. The record command provides it for its browsing
  modes — the list and an opened record — and withdraws it in the
  analysis mode, which streams model output and runs a generation the
  browser does not render, so an analysis keeps the generation TUI.
- The list view shows every recorded session, most recent first, one
  line per record, read through records.ListSessionInfos; the -session
  flag opens its record directly on entry. The selected record is the
  browser's one focus: the up and down keys, the page keys, home and
  end, and the wheel move it, and Enter or a double click opens it.
- The tree view renders the opened record's tree with the Tree tab's own
  rendering and pointer interaction — the same rows, fold column,
  previews, expansions, and collapse-all — so a record is browsed
  exactly like a live session, except that the tree carries no keyboard
  focus: the up, down, page, home, and end keys and the wheel scroll its
  content, matching the generation TUI's pane scrolling, and Enter is
  inert there. The tree is reconstructed from the recorded operation
  stream with tree.Replay (records.LoadSessionTree); the pane is one TUI
  value whose tab layout holds exactly one tab, so the shared paths read
  the pane's box, scroll state, and row ranges from it and what is drawn
  is what is pressed. The elapsed timer counts from the record's start,
  so a row shows how far into the session its node was written.
- The scroll keys refresh the pane's display and scroll bounds before
  scrolling, so a key press never depends on a render having happened
  since the last change; the row ranges the pointer mapping reads come
  from the same display.
- The title row's left side carries the navigation: the record list's
  label and, while a record is open, the record's id after it. A press on
  the navigation returns to the list; the two leading cells stay
  untouched, keeping the title row's rule at the box edge, like the Tree
  tab's status.
- Rendering stays a plain function of the browser's state: the browser
  rebuilds a full element tree per frame from its records, the opened
  tree, and the projection, following the TUI's rendering pattern.
`

// The record browser's fixed labels. See TheoryOfRecordBrowser.
const (
	// recordTabTitle names the browser's single tab.
	recordTabTitle = "Record"
	// recordNavListLabel labels the navigation bar's list segment; a
	// press on the navigation returns to the list.
	recordNavListLabel = "records"
	// recordNavIndent keeps the title row's two leading cells untouched,
	// so the title's own rule stays visible at the box edge.
	recordNavIndent = 2
)

var recordBrowserHelp = []string{
	"up / down\tmove the record selection; scroll the tree",
	"enter\topen the selected record (the tree carries no focus)",
	"page up / down\ta page of records; a page of tree rows",
	"home / end\tfirst / last record or row",
	"esc\treturn to the record list",
	"click\tselect a record; a tree row acts on a double click",
	"double-click\topen a record; expand or collapse a node",
	"tree column\tclick ▸ / ▾ to collapse / expand the node",
	"c\tfold every node of the tree; press again to restore",
	"v\tcycle the tree's projection (all / events / summary / model / program / user / stream)",
	"wheel\tscroll the tree; move the record selection",
	"r\treload the record list",
	"nav bar\tclick records on the title row to return to the list",
	"q / Ctrl-C\tquit (press again to confirm)",
	"?\ttoggle this help overlay",
}

// RecordBrowserEnabled marks an app whose TUI is the dedicated record
// browser instead of the generation TUI. See TheoryOfRecordBrowser.
type RecordBrowserEnabled bool

func (Module) RecordBrowserEnabled() RecordBrowserEnabled {
	return false
}

// runRecordBrowser starts the record subcommand's TUI: the recorded
// session list and the tree of an opened record. The app's definitions
// are already layered on the scope. See TheoryOfRecordBrowser.
func runRecordBrowser(scope dscope.Scope) {
	// The display colors come from the resolved tui config section, like
	// the generation TUI. See TheoryOfUIStyle.
	scope.Get[UIStyle]().apply()
	browser, err := newRecordBrowser(
		scope.Get[records.ListSessionInfos](),
		scope.Get[records.LoadSessionTree](),
		int64(scope.Get[records.SessionID]()),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot start TUI: %v; continuing without TUI\n", err)
		scope.Get[apps.App]().Call(scope)
		return
	}
	if err := browser.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

type RecordBrowser struct {
	tty      tty.Tty
	screen   *taiui.TerminalScreen
	updateCh chan struct{}
	width    int
	height   int
	// tabs is the browser's one-tab layout: the record tab is always
	// expanded and focused, and the shared tree paths read the pane's
	// box from it.
	tabs *taiui.Tabs

	listSessions records.ListSessionInfos
	loadTree     records.LoadSessionTree
	// openSession is the record the -session flag selects: the browser
	// opens it before the list is browsed. Zero means the list view.
	openSession int64

	infos    []records.SessionInfo
	selected int

	// currentID is the opened record's id; 0 means the list view.
	currentID int64
	// treePane carries the opened record's tree and the Tree tab's
	// rendering and interaction state. See TheoryOfRecordBrowser.
	treePane *TUI

	// lastListPress records the previous left press in the list view,
	// so a second press at the same cell within the double-click window
	// opens the record under it.
	lastListPress  time.Time
	lastListPressX int
	lastListPressY int

	err      string
	quit     taiui.QuitConfirm
	showHelp bool

	mouseReporting bool
}

// newRecordTreePane builds the tree pane of one record: a TUI value whose
// tab layout holds exactly one tab, so the shared tree rendering and
// press paths read the pane's box, scroll state, and row ranges from it.
// See TheoryOfRecordBrowser.
func newRecordTreePane(tabs *taiui.Tabs, width, height int) *TUI {
	return &TUI{
		tabs:     tabs,
		scrolls:  []taiui.ScrollState{{}},
		updateCh: make(chan struct{}, 1),
		width:    width,
		height:   height,
	}
}

func newRecordBrowser(
	listSessions records.ListSessionInfos,
	loadTree records.LoadSessionTree,
	openSession int64,
) (*RecordBrowser, error) {
	term, err := taiui.OpenTty()
	if err != nil {
		return nil, err
	}
	width, height := 80, 25
	if ws, err := term.WindowSize(); err == nil && ws.Width > 0 && ws.Height > 0 {
		width, height = ws.Width, ws.Height
	}
	tabs := taiui.NewTabs(1)
	// The single tab is always expanded and focused: the record view is
	// the whole browser, so no tab-machine navigation applies.
	tabs.FocusTab(0)
	return &RecordBrowser{
		tty:            term,
		screen:         taiui.NewTerminalScreen(term, width, height),
		updateCh:       make(chan struct{}, 1),
		width:          width,
		height:         height,
		tabs:           tabs,
		listSessions:   listSessions,
		loadTree:       loadTree,
		openSession:    openSession,
		treePane:       newRecordTreePane(tabs, width, height),
		mouseReporting: true,
	}, nil
}

// Run drives the browser's terminal session until the user quits. The
// session owns the raw-mode lifecycle, mouse reporting, key decoding,
// and resize notification, and the browser supplies its two views and
// its key dispatch. See taiui.TheoryOfSession.
func (b *RecordBrowser) Run() error {
	b.loadList()
	if b.openSession != 0 {
		b.openRecord(b.openSession)
	}
	return (&taiui.Session{
		Tty:    b.tty,
		Screen: b.screen,
		Update: b.updateCh,
		Mouse:  true,
		Render: b.render,
		Key:    b.handleKey,
		OnResize: func(width, height int) {
			b.width, b.height = width, height
		},
	}).Run()
}

// loadList reads the recorded sessions into the browser's list state. An
// unavailable database is reported in the pane instead of aborting the
// browser. See TheoryOfRecordBrowser.
func (b *RecordBrowser) loadList() {
	infos, err := b.listSessions()
	if err != nil {
		b.err = err.Error()
		return
	}
	b.err = ""
	b.infos = infos
	b.selectRecord(b.selected)
}

// selectRecord moves the list selection to idx, clamped to the list.
func (b *RecordBrowser) selectRecord(idx int) {
	if len(b.infos) == 0 {
		b.selected = 0
		return
	}
	b.selected = min(max(idx, 0), len(b.infos)-1)
}

// openRecord loads the record's tree and switches the browser to the
// tree view. A load failure keeps the list view and reports the reason.
// See TheoryOfRecordBrowser.
func (b *RecordBrowser) openRecord(id int64) {
	tr, err := b.loadTree(id)
	if err != nil {
		b.err = err.Error()
		b.currentID = 0
		return
	}
	b.err = ""
	p := b.treePane
	paneIdx := p.treeTab.paneIdx
	p.mu.Lock()
	p.treeView = tr
	// A fresh pane state per record: the expansion, projection, row
	// ranges, and wrap cache all belong to the opened record's tree.
	p.treeTab = treeTabState{paneIdx: paneIdx}
	p.treeTab.expanded = make(map[string]bool)
	p.treeTab.seen = make(map[string]bool)
	p.seedTreeExpansions(tr.Root())
	p.startTime = recordStartTime(b.infos, id, tr)
	p.scrolls[paneIdx] = taiui.ScrollState{}
	p.mu.Unlock()
	b.currentID = id
}

// backToList returns to the record list from an opened record.
func (b *RecordBrowser) backToList() {
	if b.currentID == 0 {
		return
	}
	p := b.treePane
	paneIdx := p.treeTab.paneIdx
	p.mu.Lock()
	p.treeView = nil
	p.treeTab = treeTabState{paneIdx: paneIdx}
	p.mu.Unlock()
	b.currentID = 0
	b.err = ""
}

// recordStartTime returns the anchor of the tree's elapsed timer: the
// record's recorded start time when it parses, otherwise the earliest
// node time of the replayed tree. See TheoryOfRecordBrowser.
func recordStartTime(infos []records.SessionInfo, id int64, tr *tree.Tree) time.Time {
	for _, info := range infos {
		if info.ID != id {
			continue
		}
		if t, err := time.Parse(time.RFC3339Nano, info.StartTime); err == nil {
			return t
		}
		break
	}
	var start time.Time
	for _, n := range tr.Filter(func(*tree.Node) bool { return true }) {
		if n.InsertTime.IsZero() {
			continue
		}
		if start.IsZero() || n.InsertTime.Before(start) {
			start = n.InsertTime
		}
	}
	if start.IsZero() {
		// The timer must never count from the zero time: every node
		// would report a half-century of elapsed time.
		return time.Now()
	}
	return start
}

// render builds the browser's element tree from its state: the list or
// the opened record's tree, the title row's navigation, and the quit and
// help overlays. See TheoryOfRecordBrowser.
func (b *RecordBrowser) render() {
	width := max(b.width, 1)
	height := max(b.height, 1)
	box := b.tabBox()
	b.syncTreePane()
	var view taiui.Element
	if b.currentID == 0 {
		view = b.listView(box)
	} else {
		view = b.treeView(box)
	}
	elements := []any{view, b.navElement(box)}
	if b.err != "" {
		elements = append(elements, b.errorElement(width, height))
	}
	root := taiui.Overlay(elements...)
	if b.showHelp {
		root = taiui.Overlay(root, taiui.HelpOverlay(recordBrowserHelp, 16, width, height))
	}
	if b.quit.Pending() {
		root = taiui.Overlay(root, taiui.QuitConfirmBar(width, height))
	}
	taiui.Render(root, b.screen)
}

// syncTreePane carries the browser's terminal size and mouse state into
// the tree pane, which the shared press paths read.
func (b *RecordBrowser) syncTreePane() {
	p := b.treePane
	p.width = b.width
	p.height = b.height
	p.mouseReporting = b.mouseReporting
}

// tabBox is the single tab's box, which tiles the whole screen.
func (b *RecordBrowser) tabBox() taiui.Box {
	return b.tabs.Boxes(max(b.width, 1), max(b.height, 1))[0]
}

// listBox is the list's box: the tab's content area below the title row.
func (b *RecordBrowser) listBox() taiui.Box {
	box := b.tabBox()
	return taiui.Box{Top: box.Top + 1, Left: box.Left, Bottom: box.Bottom, Right: box.Right}
}

// treeScroll is the tree pane's scroll state.
func (b *RecordBrowser) treeScroll() *taiui.ScrollState {
	return &b.treePane.scrolls[b.treePane.tabIndex(tabTree)]
}

// navText renders the navigation bar's text: the record list's label and
// the opened record's id. See TheoryOfRecordBrowser.
func (b *RecordBrowser) navText() string {
	if b.currentID == 0 {
		return recordNavListLabel
	}
	return fmt.Sprintf("%s › #%d", recordNavListLabel, b.currentID)
}

// navHit reports whether a press landed on the navigation bar: the drawn
// text's own cells on the title row. See TheoryOfRecordBrowser.
func (b *RecordBrowser) navHit(x, y int) bool {
	box := b.tabBox()
	if y != box.Top || x < box.Left+recordNavIndent {
		return false
	}
	w := taiui.DisplayWidthOptions().String(b.navText())
	return x < box.Left+recordNavIndent+w
}

// navElement draws the navigation bar over the title row's left side; the
// two leading cells stay untouched. See TheoryOfRecordBrowser.
func (b *RecordBrowser) navElement(box taiui.Box) taiui.Element {
	specs := []any{
		b.navText(),
		taiui.Box{Top: box.Top, Left: box.Left + recordNavIndent, Bottom: box.Top + 1, Right: box.Right},
		taiui.FGColor(panelStyle.FocusLabelFG),
		taiui.Bold(true),
	}
	if panelStyle.FocusBG != taiui.NoColor {
		specs = append(specs, taiui.BGColor(panelStyle.FocusBG))
	}
	return taiui.Text(specs...)
}

// errorElement shows the last reported error on the screen's bottom row,
// so a failed load is visible without leaving the browser.
func (b *RecordBrowser) errorElement(width, height int) taiui.Element {
	row := max(height-1, 0)
	return taiui.Text(b.err,
		taiui.Box{Top: row, Left: 0, Bottom: height, Right: width},
		taiui.FGColor(color.PaletteColor(9)),
	)
}

// listView renders the record list: the panel that carries the tab's
// title row plus a List, whose own window centers on the selection; the
// list's box is the panel's content area. See TheoryOfRecordBrowser.
func (b *RecordBrowser) listView(box taiui.Box) taiui.Element {
	label := fmt.Sprintf("%s (%d)", recordTabTitle, len(b.infos))
	if len(b.infos) == 0 {
		return taiui.TabPanel(box, recordTabTitle, label, true, true, false,
			[]taiui.Line{{Text: "no recorded sessions"}}, taiui.ScrollState{}, panelStyle)
	}
	items := make([]string, 0, len(b.infos))
	for _, info := range b.infos {
		items = append(items, recordListItem(info))
	}
	panel := taiui.TabPanel(box, recordTabTitle, label, true, true, false, nil, taiui.ScrollState{}, panelStyle)
	list := taiui.List(items, b.selected,
		b.listBox(),
		taiui.ListStyle(taiui.SameStyle.SetReverse(true)),
	)
	return taiui.Overlay(panel, list)
}

// treeView renders the opened record's tree: the pane's shared tree
// display through the tab panel, with the fold column's hover affordance
// on top. See TheoryOfRecordBrowser.
func (b *RecordBrowser) treeView(box taiui.Box) taiui.Element {
	p := b.treePane
	label := fmt.Sprintf("%s #%d", recordTabTitle, b.currentID)
	if p.treeView == nil {
		return taiui.TabPanel(box, recordTabTitle, label, true, true, false,
			[]taiui.Line{{Text: "no nodes in the recorded tree"}}, taiui.ScrollState{}, panelStyle)
	}
	paneIdx := p.treeTab.paneIdx
	contentWidth := treeContentWidth(box.Width())
	p.mu.Lock()
	display := p.treeDisplay(contentWidth, panelStyle.FocusBG)
	p.scrolls[paneIdx].Update(len(display), taiui.PaneHeight(box))
	p.floatTreeControls(box, display)
	p.mu.Unlock()
	panel := taiui.TabPanel(box, recordTabTitle, label, true, true, false,
		display, p.scrolls[paneIdx], panelStyle)
	elements := []any{panel}
	if el := p.treeFoldHoverElement(box, display); el != nil {
		elements = append(elements, el)
	}
	return taiui.Overlay(elements...)
}

// syncTreeScroll recomputes the tree pane's display when a record is
// open and updates the pane's scroll bounds from it, so the row ranges
// the pointer mapping reads and the content extent the scroll keys clamp
// against are current. The pane caches its wrapped node lines, so the
// call is cheap; the shared press paths already recompute the display,
// and the scroll keys read the same extent, so they must not depend on a
// render having happened since the last change. The caller must not hold
// the pane's lock. See TheoryOfRecordBrowser.
func (b *RecordBrowser) syncTreeScroll() {
	p := b.treePane
	if p.treeView == nil {
		return
	}
	box := b.tabBox()
	p.mu.Lock()
	display := p.treeDisplay(treeContentWidth(box.Width()), panelStyle.FocusBG)
	p.scrolls[p.treeTab.paneIdx].Update(len(display), taiui.PaneHeight(box))
	p.mu.Unlock()
}

// recordListItem renders one list row: the record's id, command, start,
// status, and operation count. See TheoryOfRecordBrowser.
func recordListItem(info records.SessionInfo) string {
	status := info.Status
	if status == "" {
		status = "unknown"
	}
	return fmt.Sprintf("#%-4d %-12s %-19s %-8s %6d ops",
		info.ID, info.Command, recordStartText(info.StartTime), status, info.OpCount)
}

// recordStartText renders a record's start time compactly; an
// unparseable value is truncated as is.
func recordStartText(start string) string {
	if t, err := time.Parse(time.RFC3339Nano, start); err == nil {
		return t.Local().Format("2006-01-02 15:04:05")
	}
	return displaywidth.TruncateString(start, 19, "…")
}

// move handles the up and down keys and the wheel: the list view moves
// its selection — the browser's one focus — and the tree view scrolls its
// content by one row. The tree's display and scroll bounds are refreshed
// first, so a scroll never depends on a render having happened since the
// last change. See TheoryOfRecordBrowser.
func (b *RecordBrowser) move(delta int) {
	if b.currentID == 0 {
		b.selectRecord(b.selected + delta)
		return
	}
	b.syncTreeScroll()
	b.treeScroll().Scroll(delta)
}

// pageMove moves the list's selection by a page of records, or scrolls
// the tree view by a page of rows. The tree's display and scroll bounds
// are refreshed first. See TheoryOfRecordBrowser.
func (b *RecordBrowser) pageMove(direction int) {
	if b.currentID == 0 {
		b.selectRecord(b.selected + direction*max(b.listBox().Height(), 1))
		return
	}
	b.syncTreeScroll()
	b.treeScroll().PageScroll(direction, taiui.PaneHeight(b.tabBox()))
}

// moveTo moves the list's selection or the tree view to the first or
// last entry: the home and end keys. See TheoryOfRecordBrowser.
func (b *RecordBrowser) moveTo(last bool) {
	if b.currentID == 0 {
		if last {
			b.selectRecord(len(b.infos) - 1)
		} else {
			b.selectRecord(0)
		}
		return
	}
	top := 0
	if last {
		// A large sentinel offset sticks the view to the tail, matching
		// the generation TUI's end key. See taiui.ScrollState.ScrollTo.
		top = 1 << 30
	}
	b.syncTreeScroll()
	b.treeScroll().ScrollTo(top)
}

// openSelected opens the list's selected record, the Enter key's action
// in the list view. The tree view carries no focus, so Enter is inert
// there. See TheoryOfRecordBrowser.
func (b *RecordBrowser) openSelected() {
	if b.currentID != 0 {
		return
	}
	if len(b.infos) == 0 {
		return
	}
	b.openRecord(b.infos[b.selected].ID)
}

// listPress handles a left press in the list view: the press selects the
// record under it, and a second press at the same cell within the
// double-click window opens the selected record. The row maps onto a
// record through the same window the List renders with.
// See TheoryOfRecordBrowser.
func (b *RecordBrowser) listPress(x, y int) {
	if len(b.infos) == 0 {
		return
	}
	box := b.listBox()
	if x < box.Left || x >= box.Right || y < box.Top || y >= box.Bottom {
		return
	}
	from := taiui.ListWindow(len(b.infos), box.Height(), b.selected)
	idx := from + (y - box.Top)
	if idx < 0 || idx >= len(b.infos) {
		return
	}
	now := time.Now()
	if now.Sub(b.lastListPress) <= treeDoubleClickWindow &&
		x == b.lastListPressX && y == b.lastListPressY && idx == b.selected {
		b.lastListPress = time.Time{}
		b.openRecord(b.infos[idx].ID)
		return
	}
	b.lastListPress = now
	b.lastListPressX = x
	b.lastListPressY = y
	b.selectRecord(idx)
}

// treePress handles a left press in the tree view: the fold column's
// control toggles the node under it — the Tree tab's own path — and any
// other text press runs the Tree tab's click handling, which pairs the
// press for a double-click toggle. A press never moves a focus: the tree
// view carries none. See TheoryOfRecordBrowser.
func (b *RecordBrowser) treePress(x, y int) {
	p := b.treePane
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.toggleTreeControlAtClick(x, y) {
		return
	}
	p.treeAtClick(x, y)
}

func (b *RecordBrowser) handleMouseKey(key string) bool {
	event, x, y, ok := taiui.ParseMouseKey(key)
	if !ok {
		return false
	}
	switch event {
	case "motion":
		// No-button motion drives the tree's fold-column hover, the
		// same affordance the Tree tab shows.
		b.treePane.ctlHover = true
		b.treePane.ctlHoverX = x
		b.treePane.ctlHoverY = y
	case "wheel-up":
		b.move(-1)
	case "wheel-down":
		b.move(1)
	case "left":
		// Any press cancels a pending quit confirmation before its
		// normal processing, so an accidental quit press never loses
		// the session.
		b.quit.Cancel()
		if b.showHelp {
			box := taiui.HelpOverlayBox(recordBrowserHelp, b.width, b.height)
			if x >= box.Left && x < box.Right && y >= box.Top && y < box.Bottom {
				b.showHelp = false
			}
			return false
		}
		if b.navHit(x, y) {
			b.backToList()
			return false
		}
		if b.currentID == 0 {
			b.listPress(x, y)
		} else {
			b.treePress(x, y)
		}
	}
	return false
}

// handleKey handles one decoded key name; returning true quits the
// browser. See TheoryOfRecordBrowser.
func (b *RecordBrowser) handleKey(key string) bool {
	if strings.HasPrefix(key, taiui.MouseKeyPrefix) {
		return b.handleMouseKey(key)
	}
	// Any key other than a quit key cancels a pending quit confirmation
	// before its normal processing, so an accidental quit press never
	// loses the session. See taiui.TheoryOfSessionChrome.
	if key != "q" && key != "Q" && key != "ctrl-c" {
		b.quit.Cancel()
	}
	switch key {
	case "q", "Q", "ctrl-c":
		return b.quit.QuitKeyPressed()
	case "?":
		b.showHelp = !b.showHelp
	case "esc":
		if b.showHelp {
			b.showHelp = false
			break
		}
		b.backToList()
	case "up":
		b.move(-1)
	case "down":
		b.move(1)
	case "pageup":
		b.pageMove(-1)
	case "pagedown":
		b.pageMove(1)
	case "home":
		b.moveTo(false)
	case "end":
		b.moveTo(true)
	case "enter":
		b.openSelected()
	case "v":
		if b.currentID != 0 {
			b.treePane.cycleTreeView()
		}
	case "c":
		if b.currentID != 0 {
			b.treePane.collapseAllTreeNodes()
		}
	case "r":
		b.loadList()
	}
	return false
}
