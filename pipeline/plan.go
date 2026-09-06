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
- The plan tree is the session's flow definition: a subtree of the
  session tree rooted at the node "plan" (TypePlan, program author,
  fixed seed content naming the root's role). Entries are TypePlan
  nodes the model adds; a done mark is a TypeDone child the program
  writes when it applies a plan-op done block. The task itself stays
  in the session's user input; the root anchors the tree. The root is
  ensured lazily: applyPlanOps creates it when the first plan-op batch
  arrives and the tree lacks it, and reuses the existing root in a
  continued run, so the plan persists across goal loops and the flow
  spans attempts.
- The model decides whether to plan: a simple task — one bounded piece
  of work — is done directly, and an empty plan produces no plan
  feedback; a non-simple task is decomposed into plan-op add entries
  first. No flag selects this: the plan-op component is unconditional
  in CodesComponents, so every codes session carries the plan tree.
- The model operates the plan through plan-op blocks with five ops:
  add (create an entry; parent defaults to the plan root, so add with a
  parent is refinement), done (mark complete; the body carries the
  completion note), edit, delete, and reopen (clear a done mark when an
  entry needs more work). The batch is atomic: operations apply
  sequentially to a working copy and any failure discards the whole
  batch, returning the input tree with the collected errors as
  feedback, so a partially applied plan never persists. The structural
  root write is the one exception: it persists even when the batch is
  discarded, because it is scaffolding, not model work.
- The invariant: a done node's subtree is fully resolved. done is
  rejected while any plan-entry child lacks a done mark, and add under
  a done node is rejected (reopen first), so a done subtree never hides
  pending work.
- While the plan carries entries, the program drives the flow: every
  round's feedback ends with the plan feedback — the next pending entry
  found by depth-first search (descent stops at done or aborted nodes; a
  node all of whose children are resolved surfaces itself for the model
  to mark or refine). A resolved root yields the root prompt; a done
  root yields the completion notice exactly once
  (loopState.planCompletionNotified, reset when the plan reopens),
  closing the session. A model that does not update the plan is
  re-provided the same entry — not updating the plan means the work is
  not done.
- The flow root is exempt from new-plan revision: writeNamedTreeNodes
  never aborts it, because it is program-managed scaffolding, not a
  model-authored plan.
- Plan mode is selected by RunOptions.PlanMode, which the codes
  pipeline derives from the plan-op component's presence; the plan-op
  component is unconditional in CodesComponents, so every codes session
  is plan-enabled. The continue component stays available: continue
  blocks prompt the next round's user input alongside the plan-driven
  feedback.
`

// planRootName is the session-tree node name of the plan tree's root.
const planRootName = "plan"

// planRootSeedContent is the plan root's content: it names the root's
// role; the task itself stays in the session's user input. See
// TheoryOfPlan.
const planRootSeedContent = "plan root: the session's plan tree. Entries under this node decompose the task stated in the session's initial user input; the program feeds the next pending entry as each round's feedback."

const PlanBlockSystemPrompt = `
Plan-Op Block Kind:

The plan tree is the session's flow definition: a tree of plan nodes
in the session tree, rooted at the node "plan".
You decide whether to plan: a simple task — one bounded piece of work
that fits this response — needs no plan, do the work directly; a
non-simple task — multi-step analysis, implementation, or refactoring
spanning several rounds — needs a plan: decompose it into entries with
plan-op add blocks before executing work. When the plan carries
entries, the program provides the next pending entry each round as
user content; you do the entry's work and update the plan with plan-op
blocks in the same response. When you do not update the plan, the same
entry is provided again next round — updating the plan is how completed
work is recorded.

**Operations** (parameters in the opening header; the body carries the text):
- add: create an entry. name=<unique-name> is required; parent=<node-name>
  is optional and defaults to the plan root — adding with a parent
  refines that entry into subtasks. Body: the entry's work description.
- done: mark an entry complete. name required. Body: what was
  accomplished. Every subtask must be done first; the program rejects a
  done mark while a subtask is pending.
- edit: rewrite an entry's description. name required. Body: the new
  description.
- delete: remove an entry and its subtasks. name required. The plan
  root cannot be deleted.
- reopen: clear a done mark when an entry needs more work. name required.

**Rules:**
- The batch is atomic: one invalid operation discards the whole batch
  and no plan change takes effect; the errors are fed back.
- When the plan carries a pending entry, execute the provided entry's
  work in this response (change blocks, tests, and other blocks as
  needed), then update the plan in the same response: mark the entry
  done, or refine it into subtasks when it needs splitting.
- When every subtask of a node is done, the node itself surfaces: mark
  it done or refine it. Mark the plan root done when the whole task is
  complete; the flow then ends.
- Partition large analysis or implementation work into entries instead
  of attempting it in one response; use continue blocks to carry
  remaining work across rounds when a response cannot finish it.
`

const planCompleteNotice = `[Plan] The plan is complete. Close the session: emit the done block when this run's protocol requires one; otherwise end with the summary block. If feedback above shows unfinished work, reopen or restructure the plan with plan-op blocks first.`

const planRootNotice = `[Plan] Every entry of the plan is done. Mark the plan root %q done with a plan-op done block to end the flow, or refine the plan with plan-op add blocks if work remains. End the response with the summary block.`

const planEntryNotice = `[Plan] Current entry: %s
%s

Do this entry's work in this response (change blocks, tests, and other
blocks as needed), then update the plan in the same response: emit a
plan-op done block for %s with a completion note, or refine %s into
subtasks with plan-op add blocks when it needs splitting. End the
response with the summary block.`

// goalPlanModeNote tells the goal-mode model that the plan tree is
// available alongside the continue mechanism. See TheoryOfPlan.
const goalPlanModeNote = `[Plan] The plan tree is available in this session: for a non-simple task, decompose it into plan entries with plan-op add blocks and work through the entries — the program provides the next pending entry each round; a simple task needs no plan, do the work directly. Continue blocks remain available for chaining rounds as the goal protocol above describes. Marking the plan root done completes the plan flow; the run still ends only per the goal protocol above.`

// ensurePlanRoot returns the plan tree's root: the existing "plan" node
// when one is already present (a continued run), or a freshly written
// root. See TheoryOfPlan.
func ensurePlanRoot(tr *tree.Tree) (*tree.Tree, string, error) {
	if n, ok := tr.Node(planRootName); ok {
		if n.Type == tree.TypePlan {
			return tr, planRootName, nil
		}
	}
	next, err := tr.Write("root", planRootName, tree.TypePlan, tree.AuthorProgram, planRootSeedContent)
	if err != nil {
		return tr, "", err
	}
	return next, planRootName, nil
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

// nextPendingPlanEntry returns the plan node the model works on next:
// the first node in depth-first order that carries no done mark while
// every plan-entry child of it is resolved. A done root reports
// complete; a root without entries reports empty. See TheoryOfPlan.
func nextPendingPlanEntry(tr *tree.Tree, root string) (name, content string, complete, empty bool) {
	rootNode, ok := tr.Node(root)
	if !ok || rootNode.Type != tree.TypePlan {
		return "", "", false, true
	}
	if planDoneChild(rootNode) != nil {
		return "", "", true, false
	}
	if len(planEntries(rootNode)) == 0 {
		return "", "", false, true
	}
	var visit func(n *tree.Node) (string, string)
	visit = func(n *tree.Node) (string, string) {
		if planDoneChild(n) != nil || n.IsAborted() {
			return "", ""
		}
		for _, c := range planEntries(n) {
			if entryName, entryContent := visit(c); entryName != "" {
				return entryName, entryContent
			}
		}
		return n.Name, n.Content
	}
	name, content = visit(rootNode)
	if name == "" {
		// The root subtree resolved without a done mark (for example an
		// aborted root): the root surfaces for the model to mark or
		// refine.
		return root, rootNode.Content, false, false
	}
	return name, content, false, false
}

// planFeedback renders the plan-driven round feedback: the next pending
// entry, the surfaced-root prompt, or — exactly once — the completion
// notice of a resolved plan. An empty plan yields no feedback: the
// model decides whether to plan. See TheoryOfPlan.
func planFeedback(tr *tree.Tree, root string, completionNotified bool) (parts []generators.Part, complete bool) {
	name, content, complete, empty := nextPendingPlanEntry(tr, root)
	if complete {
		if completionNotified {
			return nil, true
		}
		return []generators.Part{generators.Text(planCompleteNotice + "\n\n")}, true
	}
	switch {
	case empty:
		return nil, false
	case name == root:
		return []generators.Part{generators.Text(fmt.Sprintf(planRootNotice+"\n\n", root))}, false
	default:
		return []generators.Part{generators.Text(fmt.Sprintf(planEntryNotice+"\n\n", name, content, name, name))}, false
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

// applyPlanOps applies the plan-op blocks of one attempt to the session
// tree. The batch is atomic: the operations apply sequentially to a
// working copy, and any failing operation discards the whole batch —
// the input tree is returned with the collected errors as feedback, so
// a partially applied plan never persists. The plan root is ensured
// before the batch: its write persists even when the batch is
// discarded, because it is structural scaffolding. See TheoryOfPlan.
func applyPlanOps(pctx *components.ProcessContext) components.ProcessResult {
	tr := pctx.SessionTree
	if tr == nil {
		return components.ProcessResult{}
	}
	if len(pctx.Blocks) > 0 {
		next, _, err := ensurePlanRoot(tr)
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
		next, part, err := applyOnePlanOp(cur, block)
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
// operation is invalid. See TheoryOfPlan.
func applyOnePlanOp(tr *tree.Tree, block blocks.Block) (*tree.Tree, generators.Part, error) {
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
			parent = planRootName
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
		if planDoneChild(pn) != nil {
			return nil, nil, fmt.Errorf("parent %q is done; reopen it before adding subtasks", parent)
		}
		if _, exists := tr.Node(name); exists {
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
		if n.IsAborted() {
			return nil, nil, fmt.Errorf("entry %q is aborted", name)
		}
		if planDoneChild(n) != nil {
			return nil, nil, fmt.Errorf("entry %q is already done", name)
		}
		for _, c := range planEntries(n) {
			if planDoneChild(c) == nil {
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
		if _, err := planEntryNode(tr, name); err != nil {
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
		if name == planRootName {
			return nil, nil, fmt.Errorf("the plan root cannot be deleted")
		}
		if _, err := planEntryNode(tr, name); err != nil {
			return nil, nil, err
		}
		next, err := tr.Delete(name)
		if err != nil {
			return nil, nil, err
		}
		return next, generators.Text(fmt.Sprintf("[Plan] entry %q deleted.\n\n", name)), nil
	case "reopen":
		if name == "" {
			return nil, nil, fmt.Errorf("op reopen needs a name header parameter")
		}
		n, err := planEntryNode(tr, name)
		if err != nil {
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
