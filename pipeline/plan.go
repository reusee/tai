package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/tree"
)

const TheoryOfPlan = `
Plan-driven flow theory:
- The plan tree is the flow definition of one loop: a subtree of the
  session tree rooted at the loop's plan root — a child of the
  session's parent node (the loop node of a goal run, the tree's root
  for a fresh run), named by suffixing the parent with "-plan" via
  planRootNameOf. Each loop owns its plan: the root is ensured lazily
  when the first plan-op batch of that loop arrives, and no plan
  carries across loops — a later loop starts with no plan and plans
  only its own work. Cross-loop information transfer is GoalFeedback
  and GoalLoopSummaries, which is sufficient. Entries are TypePlan
  nodes the model adds; a done mark is a TypeDone child the program
  writes when it applies a plan-op done block. The task itself stays
  in the session's user input; the root anchors the tree.
- The model decides whether to plan: a simple task — one bounded piece
  of work — is done directly, and an empty plan produces no plan
  feedback; a non-simple task is decomposed into plan-op add entries
  first. No flag selects this: the plan-op component is unconditional
  in CodesComponents, so every codes session carries the plan tree.
- The model operates the plan through plan-op blocks with five ops:
  add (create an entry; parent defaults to the loop's plan root, so
  add with a parent is refinement), done (mark complete; the body
  carries the completion note), edit, delete, and reopen (clear a
  done mark when an entry needs more work). delete is soft: the
  program writes a deleted-mark child (type "deleted", program
  author, the block body as the marker content) under the entry for
  tracking and audit; the entry and its subtasks stay in the tree,
  deleting an already-deleted entry is a no-op, and every op other
  than delete on a deleted entry is rejected. The batch is atomic:
  operations apply sequentially to a working copy and any failure
  discards the whole batch, returning the input tree with the
  collected errors as feedback, so a partially applied plan never
  persists. The structural root write is the one exception: it
  persists even when the batch is discarded, because it is
  scaffolding, not model work.
- The invariant: a done node's subtree is fully resolved. done is
  rejected while any plan-entry child lacks a done or deleted mark, and
  add under a done or deleted node is rejected, so a done subtree never
  hides pending work.
- While the plan carries entries, the program drives the flow: every
  round's feedback ends with the plan feedback — every pending entry,
  found by depth-first search (descent stops at done, deleted, or
  aborted nodes; a node all of whose children are resolved surfaces
  itself for the model to mark or refine). The model decides how much
  to complete per round: batching several entries into one response
  reduces the number of rounds. A resolved root yields the root
  prompt; a done root yields the completion notice exactly once
  (loopState.planCompletionNotified, reset when the plan reopens),
  closing the session. A model that does not update the plan is
  re-provided the same entries — not updating the plan means the work
  is not done. Entries may span multiple rounds: the remaining entries
  are re-provided each round until the plan is updated, and no round
  demands that every entry completes in that response.
- The loop's plan root is exempt from new-plan revision:
  writeNamedTreeNodes never aborts it, because it is program-managed
  scaffolding, not a model-authored plan.
- Plan mode is selected by RunOptions.PlanMode, which the codes
  pipeline derives from the plan-op component's presence; the plan-op
  component is unconditional in CodesComponents, so every codes session
  is plan-enabled. The continue component stays available: continue
  blocks prompt the next round's user input alongside the plan-driven
  feedback.
`

const planRootSeedContent = "plan root: this loop's plan tree. Entries under this node decompose the task this loop works on; the program feeds the next pending entry as each round's feedback."

// planRootNameOf derives the session-tree node name of the plan
// tree's root for the given session parent: the parent's name with a
// "-plan" suffix. The plan root is a child of the session parent —
// the loop node of a goal run, the tree's root for a fresh run — so
// every loop's plan hangs under its own loop node and the names never
// collide across loops. See TheoryOfPlan.
func planRootNameOf(sessionParent string) string {
	if sessionParent == "" {
		sessionParent = "root"
	}
	return sessionParent + "-plan"
}

// planTypeDeleted is the node type of a soft-delete mark: the program
// writes it as a child under a plan entry when the model issues the
// plan-op delete operation. The mark keeps the entry and its subtasks
// in the tree for tracking and audit. It is the plan-op block kind
// expressed as a block type. See TheoryOfPlan.
const planTypeDeleted tree.Type = "block::deleted"

const PlanBlockSystemPrompt = `
Plan-Op Block Kind:

The plan tree is the flow definition of this loop: a tree of plan
nodes under this loop's plan node, created automatically the first
time a plan-op block arrives. Each loop owns its plan and maintains
it independently; plans do not carry across loops — cross-loop
context arrives through the loop summaries and feedback.
You decide whether to plan: a simple task — one bounded piece of work
that fits this response — needs no plan, do the work directly; a
non-simple task — multi-step analysis, implementation, or refactoring
spanning several rounds — needs a plan: decompose it into entries with
plan-op add blocks before executing work. When the plan carries
entries, the program provides every pending entry each round as user
content; work on as many as fit in the response — batching entries
into one round reduces the number of rounds — and update the plan
with plan-op blocks as entries complete. Entries may span several
rounds: when you do not update the plan, the remaining entries are
provided again next round — updating the plan is how completed work
is recorded.

**Operations** (parameters in the opening header; the body carries the text):
- add: create an entry. name=<unique-name> is required; parent=<node-name>
  is optional and defaults to this loop's plan root — adding with a
  parent refines that entry into subtasks. Body: the entry's work
  description.
- done: mark an entry complete. name required. Body: what was
  accomplished. Every subtask must be done or deleted first; the
  program rejects a done mark while a subtask is pending.
- edit: rewrite an entry's description. name required. Body: the new
  description.
- delete: soft-delete an entry. name required. The plan root cannot be
  deleted. A deleted mark is recorded under the entry for tracking and
  audit; the entry and its subtasks stay in the tree. Deleting an
  already-deleted entry is a no-op. Deleted entries are skipped by the
  pending-entry search and count as resolved subtasks.
- reopen: clear a done mark when an entry needs more work. name required.

**Rules:**
- The batch is atomic: one invalid operation discards the whole batch
  and no plan change takes effect; the errors are fed back.
- When the plan carries pending entries, work on as many as fit in
  this response (change blocks, tests, and other blocks as needed) and
  mark each completed entry done. An entry may span several rounds:
  when a response cannot finish every entry, use a continue block and
  the program provides the remaining entries again next round. When an
  entry's work completes, update the plan: mark the entry done, or
  refine it into subtasks when it needs splitting.
- When every subtask of a node is done, the node itself surfaces: mark
  it done or refine it. Mark the plan root done when the whole task is
  complete; the flow then ends.
- Partition large analysis or implementation work into entries instead
  of attempting it in one response; use continue blocks to carry
  remaining work across rounds when a response cannot finish it.
`

const planCompleteNotice = `[Plan] The plan is complete. Close the session: emit the done block when this run's protocol requires one; otherwise end with the summary block. If feedback above shows unfinished work, reopen or restructure the plan with plan-op blocks first.`

const planEntriesNotice = `[Plan] Pending plan entries:
%s

Work on as many entries as fit in this response (change blocks, tests,
and other blocks as needed): batching entries into one round reduces
the number of rounds. When an entry's work completes, update the plan:
emit a plan-op done block for it with a completion note, or refine it
into subtasks with plan-op add blocks when it needs splitting. When
the response cannot finish every entry, use a continue block and the
remaining entries are provided next round. End the response with the
summary block.`

const planRootNotice = `[Plan] Every entry of the plan is done. Mark the plan root %q done with a plan-op done block to end the flow, or refine the plan with plan-op add blocks if work remains. End the response with the summary block.`

const goalPlanModeNote = `[Plan] The plan tree is available in this loop: for a non-simple task, decompose it into plan entries with plan-op add blocks and work through the entries — the program provides every pending entry each round, so batch as many entries as fit into each response to minimize rounds; a simple task needs no plan, do the work directly. Each loop maintains its own plan under its loop node; plans do not carry across loops — cross-loop context arrives through the summaries and feedback. Continue blocks remain available for chaining rounds as the goal protocol above describes. Marking the plan root done completes the plan flow; while the plan still carries pending entries, a done block does not end the run — complete every plan entry (done or deleted) before emitting the done block; the run still ends only per the goal protocol above.`

const goalDonePendingPlanPrompt = `[System note: The previous goal loop emitted a done block while its own plan still carried pending entries; the done block was ignored because the declared completion was premature. This loop starts with its own plan: assess the remaining work against the goal, plan it with plan-op add blocks when non-trivial, complete it, and only then emit the done block.]`

// ensurePlanRoot returns the plan tree's root of the current loop: the
// existing root when plan-op blocks arrived earlier in the same loop,
// or a freshly written root under the session parent. See
// TheoryOfPlan.
func ensurePlanRoot(tr *tree.Tree, sessionParent string) (*tree.Tree, string, error) {
	root := planRootNameOf(sessionParent)
	if n, ok := tr.Node(root); ok {
		if n.Type == tree.TypePlan {
			return tr, root, nil
		}
	}
	next, err := tr.Write(sessionParent, root, tree.TypePlan, tree.AuthorProgram, planRootSeedContent)
	if err != nil {
		return tr, "", err
	}
	return next, root, nil
}

// planDoneChild returns the node's done-marker child, or nil.
func planDoneChild(n *tree.Node) *tree.Node {
	for _, c := range n.Children() {
		if c.Type == tree.TypeDone {
			return c
		}
	}
	return nil
}

// planDeletedChild returns the node's deleted-marker child, or nil. A
// deleted mark soft-deletes a plan entry: the entry and its subtasks
// stay in the tree for tracking and audit. See TheoryOfPlan.
func planDeletedChild(n *tree.Node) *tree.Node {
	for _, c := range n.Children() {
		if c.Type == planTypeDeleted {
			return c
		}
	}
	return nil
}

// planEntries returns the node's plan-entry children in insertion order.
func planEntries(n *tree.Node) []*tree.Node {
	var entries []*tree.Node
	for _, c := range n.Children() {
		if c.Type == tree.TypePlan {
			entries = append(entries, c)
		}
	}
	return entries
}

// planEntryNode returns the plan-entry node with the given name, or an
// error when the name does not address a plan entry. See TheoryOfPlan.
func planEntryNode(tr *tree.Tree, name string) (*tree.Node, error) {
	n, ok := tr.Node(name)
	if !ok {
		return nil, fmt.Errorf("entry %q does not exist", name)
	}
	if n.Type != tree.TypePlan {
		return nil, fmt.Errorf("%q is not a plan entry", name)
	}
	return n, nil
}

// rejectDeletedPlanEntry returns an error when the entry carries a
// deleted mark, so every plan operation other than delete is rejected
// on a deleted entry. See TheoryOfPlan.
func rejectDeletedPlanEntry(n *tree.Node, name string) error {
	if planDeletedChild(n) != nil {
		return fmt.Errorf("entry %q is deleted", name)
	}
	return nil
}

// pendingPlanEntries returns every plan node the model can work on:
// depth-first, in insertion order, the nodes carrying no done,
// deleted, or abort mark whose every plan-entry child is resolved. A
// done root reports complete; a root without entries reports empty.
// See TheoryOfPlan.
func pendingPlanEntries(tr *tree.Tree, root string) (entries []*tree.Node, complete, empty bool) {
	rootNode, ok := tr.Node(root)
	if !ok || rootNode.Type != tree.TypePlan {
		return nil, false, true
	}
	if planDoneChild(rootNode) != nil {
		return nil, true, false
	}
	if planDeletedChild(rootNode) != nil {
		return nil, false, true
	}
	if len(planEntries(rootNode)) == 0 {
		return nil, false, true
	}
	var visit func(n *tree.Node)
	visit = func(n *tree.Node) {
		if planDoneChild(n) != nil || planDeletedChild(n) != nil || n.IsAborted() {
			return
		}
		surfacedChild := false
		for _, c := range planEntries(n) {
			before := len(entries)
			visit(c)
			if len(entries) > before {
				surfacedChild = true
			}
		}
		if !surfacedChild {
			entries = append(entries, n)
		}
	}
	visit(rootNode)
	if len(entries) == 0 {
		// The root subtree resolved or was aborted without a done mark:
		// the root surfaces for the model to mark or refine.
		return []*tree.Node{rootNode}, false, false
	}
	return entries, false, false
}

// planHasPendingWork reports whether the given loop's plan tree
// carries pending work: a plan root with entries whose done mark is
// absent (the root or an entry still surfaces as pending). A loop
// without a plan, or with an empty or completed plan, carries no
// pending work. The plan root is the loop's own — the check never
// consults another loop's plan. See TheoryOfGoalMode.
func planHasPendingWork(tr *tree.Tree, planRoot string) bool {
	if tr == nil {
		return false
	}
	_, complete, empty := pendingPlanEntries(tr, planRoot)
	if empty {
		return false
	}
	return !complete
}

// planFeedback renders the plan-driven round feedback: every pending
// entry, the surfaced-root prompt, or — exactly once — the completion
// notice of a resolved plan. An empty plan yields no feedback: the
// model decides whether to plan. See TheoryOfPlan.
func planFeedback(tr *tree.Tree, root string, completionNotified bool) (parts []generators.Part, complete bool) {
	entries, complete, empty := pendingPlanEntries(tr, root)
	if complete {
		if completionNotified {
			return nil, true
		}
		return []generators.Part{generators.Text(planCompleteNotice + "\n\n")}, true
	}
	switch {
	case empty:
		return nil, false
	case len(entries) == 1 && entries[0].Name == root:
		return []generators.Part{generators.Text(fmt.Sprintf(planRootNotice+"\n\n", root))}, false
	default:
		var list strings.Builder
		for _, e := range entries {
			fmt.Fprintf(&list, "- %s: %s\n", e.Name, e.Content)
		}
		return []generators.Part{generators.Text(fmt.Sprintf(planEntriesNotice+"\n\n", strings.TrimRight(list.String(), "\n")))}, false
	}
}

// hasPlanOpComponent reports whether the component set carries the
// plan-op component, which marks plan availability. CodesComponents
// includes the component unconditionally, so every codes session
// carries the plan tree and the model decides whether to plan; other
// component sets (e.g., the ai command's) leave it out. The codes
// pipeline derives RunOptions.PlanMode from it, and
// GoalSystemPromptText appends the plan note on it. See TheoryOfPlan.
func hasPlanOpComponent(comps CodesComponents) bool {
	for _, c := range comps.ComponentSet {
		if c.Kind == "plan-op" {
			return true
		}
	}
	return false
}

// NewPlanOpComponent returns the plan-op block component: the model
// operates the plan tree through plan-op blocks and the component
// applies the operations to the session tree. The plan-driven round
// feedback — the next pending entry — is produced by the generation
// loop, not by this component. See TheoryOfPlan.
func NewPlanOpComponent() components.Component {
	return components.Component{
		Kind:          "plan-op",
		PromptSection: PlanBlockSystemPrompt,
		Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
			return applyPlanOps(pctx)
		},
	}
}

// applyPlanOps applies the plan-op blocks of one attempt to the
// session tree. The batch is atomic: the operations apply sequentially
// to a working copy, and any failing operation discards the whole
// batch — the input tree is returned with the collected errors as
// feedback, so a partially applied plan never persists. The loop's
// plan root is ensured before the batch: its write persists even when
// the batch is discarded, because it is structural scaffolding, and it
// hangs under the session's parent node so each loop owns its plan.
// See TheoryOfPlan.
func applyPlanOps(pctx *components.ProcessContext) components.ProcessResult {
	tr := pctx.SessionTree
	if tr == nil {
		return components.ProcessResult{}
	}
	root := ""
	if len(pctx.Blocks) > 0 {
		var err error
		var next *tree.Tree
		next, root, err = ensurePlanRoot(tr, pctx.SessionParent)
		if err != nil {
			return components.ProcessResult{
				Parts: []generators.Part{generators.Text(fmt.Sprintf(
					"[Plan] The plan root could not be ensured; the batch was discarded: %v\n\n", err,
				))},
				Tree: tr,
			}
		}
		tr = next
	}
	cur := tr
	var parts []generators.Part
	var errs []string
	for _, block := range pctx.Blocks {
		next, part, err := applyOnePlanOp(cur, block, root)
		if err != nil {
			errs = append(errs, fmt.Sprintf("block %q: %v", block.Boundary, err))
			continue
		}
		cur = next
		parts = append(parts, part)
	}
	if len(errs) > 0 {
		return components.ProcessResult{
			Parts: []generators.Part{generators.Text(fmt.Sprintf(
				"[Plan] The plan-op batch was discarded; no plan change took effect:\n%s\n\n",
				strings.Join(errs, "\n"),
			))},
			Tree: tr,
		}
	}
	return components.ProcessResult{Parts: parts, Tree: cur}
}

// applyOnePlanOp applies one plan-op block to the tree and returns the
// new tree with a confirmation part, or an error describing why the
// operation is invalid. planRoot is the plan root of the loop the
// block belongs to: the add default parent and the delete protection
// resolve against it. See TheoryOfPlan.
func applyOnePlanOp(tr *tree.Tree, block blocks.Block, planRoot string) (*tree.Tree, generators.Part, error) {
	op := block.Attributes["op"]
	name := block.Attributes["name"]
	parent := block.Attributes["parent"]
	switch op {
	case "add":
		if name == "" {
			return nil, nil, fmt.Errorf("op add needs a name header parameter")
		}
		if strings.TrimSpace(block.Body) == "" {
			return nil, nil, fmt.Errorf("op add needs a non-empty body describing the entry")
		}
		if parent == "" {
			parent = planRoot
		}
		pn, ok := tr.Node(parent)
		if !ok {
			return nil, nil, fmt.Errorf("parent %q does not exist", parent)
		}
		if pn.Type != tree.TypePlan {
			return nil, nil, fmt.Errorf("parent %q is not a plan node", parent)
		}
		if pn.IsAborted() {
			return nil, nil, fmt.Errorf("parent %q is aborted", parent)
		}
		if planDeletedChild(pn) != nil {
			return nil, nil, fmt.Errorf("parent %q is deleted", parent)
		}
		if planDoneChild(pn) != nil {
			return nil, nil, fmt.Errorf("parent %q is done; reopen it before adding subtasks", parent)
		}
		if existing, exists := tr.Node(name); exists {
			if planDeletedChild(existing) != nil {
				return nil, nil, fmt.Errorf("entry %q already exists and is deleted; choose a new name", name)
			}
			return nil, nil, fmt.Errorf("entry %q already exists", name)
		}
		next, err := tr.Write(parent, name, tree.TypePlan, tree.AuthorModel, block.Body)
		if err != nil {
			return nil, nil, err
		}
		return next, generators.Text(fmt.Sprintf("[Plan] entry %q added under %q.\n\n", name, parent)), nil
	case "done":
		if name == "" {
			return nil, nil, fmt.Errorf("op done needs a name header parameter")
		}
		n, err := planEntryNode(tr, name)
		if err != nil {
			return nil, nil, err
		}
		if err := rejectDeletedPlanEntry(n, name); err != nil {
			return nil, nil, err
		}
		if n.IsAborted() {
			return nil, nil, fmt.Errorf("entry %q is aborted", name)
		}
		if planDoneChild(n) != nil {
			return nil, nil, fmt.Errorf("entry %q is already done", name)
		}
		for _, c := range planEntries(n) {
			if planDoneChild(c) == nil && planDeletedChild(c) == nil {
				return nil, nil, fmt.Errorf("cannot mark %q done: subtask %q is still pending", name, c.Name)
			}
		}
		next, _, err := tr.WriteAuto(name, "done", tree.TypeDone, tree.AuthorProgram, block.Body)
		if err != nil {
			return nil, nil, err
		}
		return next, generators.Text(fmt.Sprintf("[Plan] entry %q marked done.\n\n", name)), nil
	case "edit":
		if name == "" {
			return nil, nil, fmt.Errorf("op edit needs a name header parameter")
		}
		if strings.TrimSpace(block.Body) == "" {
			return nil, nil, fmt.Errorf("op edit needs a non-empty body with the new description")
		}
		n, err := planEntryNode(tr, name)
		if err != nil {
			return nil, nil, err
		}
		if err := rejectDeletedPlanEntry(n, name); err != nil {
			return nil, nil, err
		}
		next, err := tr.Modify(name, block.Body)
		if err != nil {
			return nil, nil, err
		}
		return next, generators.Text(fmt.Sprintf("[Plan] entry %q updated.\n\n", name)), nil
	case "delete":
		if name == "" {
			return nil, nil, fmt.Errorf("op delete needs a name header parameter")
		}
		if name == planRoot {
			return nil, nil, fmt.Errorf("the plan root cannot be deleted")
		}
		n, err := planEntryNode(tr, name)
		if err != nil {
			return nil, nil, err
		}
		if planDeletedChild(n) != nil {
			// Soft delete is idempotent: an already-deleted entry
			// deletes as a no-op.
			return tr, generators.Text(fmt.Sprintf("[Plan] entry %q is already deleted.\n\n", name)), nil
		}
		content := strings.TrimSpace(block.Body)
		if content == "" {
			content = "deleted"
		}
		next, _, err := tr.WriteAuto(name, "deleted", planTypeDeleted, tree.AuthorProgram, content)
		if err != nil {
			return nil, nil, err
		}
		return next, generators.Text(fmt.Sprintf("[Plan] entry %q soft-deleted: a deleted mark was recorded under it.\n\n", name)), nil
	case "reopen":
		if name == "" {
			return nil, nil, fmt.Errorf("op reopen needs a name header parameter")
		}
		n, err := planEntryNode(tr, name)
		if err != nil {
			return nil, nil, err
		}
		if err := rejectDeletedPlanEntry(n, name); err != nil {
			return nil, nil, err
		}
		done := planDoneChild(n)
		if done == nil {
			return nil, nil, fmt.Errorf("entry %q is not done", name)
		}
		next, err := tr.Delete(done.Name)
		if err != nil {
			return nil, nil, err
		}
		return next, generators.Text(fmt.Sprintf("[Plan] entry %q reopened.\n\n", name)), nil
	default:
		return nil, nil, fmt.Errorf("unknown op %q", op)
	}
}
