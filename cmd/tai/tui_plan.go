package main

import (
	"fmt"

	"github.com/reusee/tai/taiui"
	"github.com/reusee/tai/tree"
)

// treePlanDeleteMarkType is the type of the soft-delete mark the
// pipeline's plan-op component writes under a deleted plan entry. See
// TheoryOfTUIDynamicPlanTab.
const treePlanDeleteMarkType tree.Type = "block::deleted"

// tuiPlanRootName derives the session-tree name of a session parent's
// plan root, matching the pipeline's planRootNameOf: the parent's name
// suffixed with "-plan", the tree root's name when the parent is empty.
// See TheoryOfTUIDynamicPlanTab.
func tuiPlanRootName(parent string) string {
	if parent == "" {
		parent = "root"
	}
	return parent + "-plan"
}

// treeNodeHasChildType reports whether the node carries a child of the
// given type.
func treeNodeHasChildType(n *tree.Node, typ tree.Type) bool {
	for _, c := range n.Children() {
		if c.Type == typ {
			return true
		}
	}
	return false
}

// withPane runs fn with the given tree-shaped pane as the active pane:
// the shared tree rendering and press paths read t.treeView and
// t.treeTab, so the Plan pane swaps its own projection and pane state
// in for the call. The caller holds t.mu, and the swap ends with the
// call, so one call touches exactly one pane — what is drawn and what
// is pressed always belong to the same pane. See
// TheoryOfTUIDynamicPlanTab.
func (t *TUI) withPane(kind tuiTab, fn func()) {
	if kind != tabPlan {
		fn()
		return
	}
	view, state := t.treeView, t.treeTab
	t.treeView, t.treeTab = t.planView, t.plan
	fn()
	t.plan, t.treeTab = t.treeTab, state
	t.treeView = view
}

// paneScrollLocked returns the scroll state of the tab at idx, or nil
// when the TUI carries no layout state for it (a TUI built directly as a
// struct literal in tests). Callers treat nil as a zero state, so the
// pane arithmetic degrades to offset 0 instead of panicking while the
// pane's content still renders and its presses still resolve. See
// TheoryOfTUIDynamicPlanTab.
func (t *TUI) paneScrollLocked(idx int) *taiui.ScrollState {
	if idx < 0 || idx >= len(t.scrolls) {
		return nil
	}
	return &t.scrolls[idx]
}

// syncPlanTabLocked opens or closes the Plan tab and rebuilds its view:
// the tab is present exactly while the current loop carries a plan. The
// current loop is the latest loop-N node — a fresh run has none, so the
// session parent is the tree root — and its plan root is the session
// parent's "-plan" child. A newer loop means the previous loop ended, so
// its Plan tab is destroyed first. The caller holds t.mu. See
// TheoryOfTUIDynamicPlanTab.
func (t *TUI) syncPlanTabLocked() {
	if t.treeView == nil {
		return
	}
	parent := "root"
	if loops := t.treeView.ByType(tree.TypeLoop); len(loops) > 0 {
		latest := loops[len(loops)-1].Name
		if t.currentLoop != latest {
			t.closePlanTabLocked()
			t.currentLoop = latest
		}
		parent = latest
	}
	root := tuiPlanRootName(parent)
	n, ok := t.treeView.Node(root)
	if !ok || n.Type != tree.TypePlan {
		t.closePlanTabLocked()
		return
	}
	if !t.hasPlanTab {
		t.openPlanTabLocked()
	}
	t.planRoot = root
	// The projection keeps the plan root's path context and prunes the
	// rest of the session, so the Plan tab renders the plan subtree with
	// the Tree tab's own rendering. See TheoryOfTUIDynamicPlanTab.
	inPlan := make(map[string]bool)
	for _, node := range t.treeView.Subtree(root) {
		inPlan[node.Name] = true
	}
	t.planView = t.treeView.Extract(func(node *tree.Node) bool {
		return inPlan[node.Name]
	})
}

// planProgressLocked counts the plan's entries: the ones carrying a done
// mark over the ones still carrying work. The root is the container, not
// an entry, so it is excluded; a deleted or aborted entry left the plan,
// so it counts in neither number. The caller holds t.mu. See
// TheoryOfTUIDynamicPlanTab.
func (t *TUI) planProgressLocked() (done, pending int) {
	if t.planView == nil || t.planRoot == "" {
		return 0, 0
	}
	for _, n := range t.planView.Subtree(t.planRoot) {
		if n.Name == t.planRoot || n.Type != tree.TypePlan {
			continue
		}
		switch {
		case treeNodeHasChildType(n, tree.TypeDone):
			done++
		case treeNodeHasChildType(n, treePlanDeleteMarkType) || n.IsAborted():
			// A deleted or aborted entry counts in neither number.
			// See TheoryOfTUIDynamicPlanTab.
		default:
			pending++
		}
	}
	return done, pending
}

// planTabLabel renders the Plan tab's label as its progress: the entries
// marked done over the entries still carrying work. See
// TheoryOfTUIDynamicPlanTab.
func (t *TUI) planTabLabel() string {
	done, pending := t.planProgressLocked()
	return fmt.Sprintf("%s (%d / %d)", tabTitleOf(tabPlan), done, pending)
}

// paneTreeDisplay renders one tree-shaped pane's display: the Plan pane
// swaps its own projection and pane state in for the call, so the shared
// tree rendering produces the pane's rows and records the pane's row
// ranges. See TheoryOfTUIDynamicPlanTab.
func (t *TUI) paneTreeDisplay(kind tuiTab, contentWidth int, base taiui.Color) []taiui.Line {
	var out []taiui.Line
	t.withPane(kind, func() {
		out = t.treeDisplay(contentWidth, base)
	})
	return out
}

// treePaneAtLocked resolves the tree-shaped pane a press falls into: the
// Tree tab or the Plan tab, with the pane's layout index. The caller
// holds t.mu. See TheoryOfTUIDynamicPlanTab.
func (t *TUI) treePaneAtLocked(x, y int) (tuiTab, bool) {
	boxes := t.tabs.Boxes(t.width, t.height)
	for _, kind := range t.tabKinds() {
		if kind != tabTree && kind != tabPlan {
			continue
		}
		idx := t.tabIndex(kind)
		if idx < 0 || idx >= len(boxes) || !t.tabs.Expanded[idx] {
			continue
		}
		box := boxes[idx]
		if x >= box.Left && x < box.Right && y > box.Top && y < box.Bottom {
			return kind, true
		}
	}
	return 0, false
}

// toggleTreePaneControlLocked toggles the tree-shaped pane's node under
// the pressed fold control and reports whether a control consumed the
// press. The caller holds t.mu. See TheoryOfTUIDynamicPlanTab.
func (t *TUI) toggleTreePaneControlLocked(kind tuiTab, x, y int) bool {
	consumed := false
	t.withPane(kind, func() {
		consumed = t.toggleTreeControlAtClick(x, y)
	})
	return consumed
}

// treePaneClickLocked routes a press inside a tree-shaped pane to that
// pane's click handler: the attempt node's jump marker and the
// double-click expansion. The caller holds t.mu. See
// TheoryOfTUIDynamicPlanTab.
func (t *TUI) treePaneClickLocked(kind tuiTab, x, y int) {
	t.withPane(kind, func() {
		t.treeAtClick(x, y)
	})
}
