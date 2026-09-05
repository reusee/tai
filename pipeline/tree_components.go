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

const TheoryOfSessionTree = `
Session tree theory: every operation of the run is a write to one
immutable tree, and one run owns exactly one tree.
- The tree is owned by the generation loop and never joins the
  generators.State chain: the state already carries its own snapshot
  semantics, and threading the tree through every state layer would
  couple each layer to it. The loop holds sessionTree, sessionRoot,
  and currentResponse on loopState; tree-writing components receive the
  tree through ProcessContext.SessionTree and return the updated tree
  through ProcessResult.Tree.
- One run, one tree. A fresh Run builds the tree under the tree's
  root; a continued Run — a goal loop — receives SessionTreeContinuation
  (dscope-provided; the zero default opens a fresh tree) carrying the
  run's tree and the loop node the runner prepared, and writes every
  session node of the loop under that node, so each loop is one child
  (tree.TypeLoop) of the run's single tree. The session root — the
  tree's root for a fresh run, the loop node for a continued one —
  anchors the session's own nodes: writeInitialTreeNodes writes the
  system-prompt node (program author, content = the initial state's
  system prompt) and one input node (user author) merging the initial
  user contents' Text parts under it.
- The loop's own bookkeeping joins the same tree as event nodes
  (event-subtype types in Category event, program author) under the
  session parent: attempt lifecycle, request parameters, finish
  reasons, token usage, truncations, retries, handoffs, completions,
  continuations, thought summaries, and the terminal error. Every
  event write yields the full tree to the run's consumer (see
  TheoryOfLoopEvents). Event nodes are program bookkeeping: every
  model-facing outline excludes them by category, so the model never
  sees the loop's own bookkeeping.
- A successful attempt writes a response node under the session root
  (model author, content = the attempt's model-role Text parts; thought
  parts never enter the tree) and one summary node per summary body
  under it.
- Block nodes are written in one validated batch before the components
  process the blocks: a block's header may carry a parent parameter
  (default: the current response node); new-plan and response blocks
  must carry parent and name — their named plan or response node is
  written by the component, while the block node itself is auto-named;
  a done block becomes a done node. One naming fault discards the
  whole batch, writes an error node, and joins the shared
  block-correction budget (see TheoryOfUnknownBlockKinds), so the
  model re-emits the batch with corrected parameters.
- In-batch references: a collected block may name as its parent a node
  that an earlier new-plan or response block of the same batch
  declares. The reference passes validation, the block node's write is
  deferred, and writeDeferredBlockNodes records it after the
  components have created the named node, so one response's tree
  writes stay atomic with respect to naming. A declared name used
  twice in one batch, or a reference to a name declared later in the
  batch — the components write in block order, so the named node
  would not exist yet — is a naming fault like any other.
- Malformed blocks and unavailable-kind blocks join the tree as error
  nodes (program author) under the current response, written by
  recordAttemptErrorNodes regardless of the correction budget: the
  nodes are the tree's error record, extractable with ByType; the
  shared budget governs only whether the model is asked to correct.
  Unavailable-kind block nodes stay childless — the childless node is
  the unprocessed signal — so their correction error is recorded as a
  sibling error node.
- A block node without children reads as an unprocessed block, except
  done and summary. Component outputs with one part per block attach a
  block-result child per block; any other shape attaches one shared
  result node under the first block. Blocks consumed by the
  BlockHandler during streaming (change) receive an applied result
  child for the same reason. Change blocks carry no parent/name header
  — the changes package stays untouched — and are recorded post hoc
  with auto names; they are the one exception. A change block's node
  content leads with its op, target, and file header, so the outline's
  preview identifies the modification: the outline is the
  applied-changes record, and no separate applied-change note is fed
  back.
- Every round-triggering feedback — the component feedback and the
  retry feedback of a truncated or errored attempt — ends with the
  session-tree outline part, and the feedback content is written as an
  input node (program author) under the session root. The outline is
  always the current loop's subtree, rendered from the session root —
  the loop node for a continued run, the tree root for a fresh one —
  so each round's feedback volume stays independent of the run's
  length and of other loops' content. The idle handler's user input is
  recorded as an input node (user author), extracted by content-count
  delta: only the delta is visible, not the handler's internal loop.
- The handoff input prefixes the incomplete output with the handoff
  subtree: the session's tree projected onto its decision-level nodes
  (every node outside the block and event categories) and rendered
  from the session root, so the summary carries the session's plans,
  decisions, and earlier summaries — without execution detail and
  without other loops' content (see tree.TheoryOfSubtree).
- The run's Result carries the final session tree, so callers outside
  the loop — a review pass, a display front-end — extract subtree
  projections from it (see tree.TheoryOfSubtree). In goal mode every
  loop writes into the run's one tree under its loop node and returns
  the evolved tree; the runner adopts it, and GoalResult.Tree carries
  the whole run's tree.
`

const SessionTreeSystemPrompt = `
**Session Tree:**

The session's history is an immutable tree: every user input, model
response, summary, block, and block result is a node. The loop owns the
tree; each round's feedback ends with its outline, so the whole
session's structure stays visible.

**Rules:**
- Any block kind's opening header may carry an optional parent
  parameter (parent=<node-name>): the block node is recorded under that
  node. Without it, the block is recorded under the current round's
  response node.
- A block may reference a parent node that an earlier new-plan or
  response block of the same response declares: the program records
  such a block once that node exists.
- new-plan and response blocks MUST carry both parent and name
  parameters (parent=<node-name>&name=<unique-name>).
- Node names are globally unique and validated by the program. A
  duplicate name or unknown parent discards the whole batch of block
  nodes and produces a System note listing the errors; re-emit the
  batch with corrected parameters.
- Plans are revised by abort, never by mutation: recording a new plan
  under an existing plan node aborts the old plan (an abort child
  records who and why) and records the new plan as its child.
`

const NewPlanBlockSystemPrompt = `
New-Plan Block Kind:

Use the "new-plan" kind to record or revise a plan in the session tree.
The opening header MUST carry parent and name parameters, for example
new-plan:?parent=plan-1&name=plan-2. The body is the plan text.

**Rules:**
- When parent names an existing plan node, the program aborts that
  plan (writing an abort child) and records the new plan as its child;
  this is how a plan is revised. When parent names any other node, the
  plan is recorded under it.
- name must be globally unique. A duplicate name or unknown parent
  discards the whole block batch and produces a System note naming the
  errors.
- The confirmation arrives as user content in the next round; emit the
  summary block after it, per the shared block rules.
`

const ResponseBlockSystemPrompt = `
Response Block Kind:

Use the "response" kind to record the round's reply as a named node in
the session tree. The opening header MUST carry parent and name
parameters, for example response:?parent=root&name=response-final. The
body is the reply text.

**Rules:**
- The program writes the body as a response node under parent; the
  confirmation arrives as user content in the next round.
- name must be globally unique. A duplicate name or unknown parent
  discards the whole block batch and produces a System note naming the
  errors.
`

// writeInitialTreeNodes writes the session's initial nodes — the
// system prompt and the merged initial user input — under the given
// parent: the tree's root for a fresh run, the goal loop's loop node
// for a continued one. See TheoryOfSessionTree.
func writeInitialTreeNodes(tr *tree.Tree, parent string, state generators.State) *tree.Tree {
	if state == nil {
		return tr
	}
	if prompt := state.SystemPrompt(); prompt != "" {
		if next, _, err := tr.WriteAuto(parent, "system-prompt", tree.TypeSystemPrompt, tree.AuthorProgram, prompt); err == nil {
			tr = next
		}
	}
	var texts []string
	for c := range state.Contents() {
		if c.Role != generators.RoleUser {
			continue
		}
		for _, p := range c.Parts {
			if t, ok := p.(generators.Text); ok {
				texts = append(texts, string(t))
			}
		}
	}
	if len(texts) > 0 {
		if next, _, err := tr.WriteAuto(parent, "input", tree.TypeInput, tree.AuthorUser, strings.Join(texts, "\n")); err == nil {
			tr = next
		}
	}
	return tr
}

// NewPlanComponent returns the new-plan block component: the model names
// the plan node (parent and name header parameters) and the component
// writes it, aborting an existing plan under the same parent first —
// plan revision is abort plus a new node. See TheoryOfSessionTree and
// tree.TheoryOfTree.
func NewPlanComponent() components.Component {
	return components.Component{
		Kind:          "new-plan",
		PromptSection: NewPlanBlockSystemPrompt,
		Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
			return writeNamedTreeNodes(pctx, tree.TypePlan, "plan")
		},
	}
}

// ResponseComponent returns the response block component: the model
// names the response node and the component writes the body into it.
// See TheoryOfSessionTree.
func ResponseComponent() components.Component {
	return components.Component{
		Kind:          "response",
		PromptSection: ResponseBlockSystemPrompt,
		Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
			return writeNamedTreeNodes(pctx, tree.TypeResponse, "response")
		},
	}
}

// writeNamedTreeNodes writes one named plan or response node per block,
// reading the parent and name header parameters. A block missing a
// parameter, naming an unknown parent, or colliding with an existing
// name is skipped with feedback text; the plan revision aborts the old
// plan first. See TheoryOfSessionTree.
func writeNamedTreeNodes(pctx *components.ProcessContext, typ tree.Type, prefix string) components.ProcessResult {
	tr := pctx.SessionTree
	if tr == nil {
		return components.ProcessResult{}
	}
	var parts []generators.Part
	for _, block := range pctx.Blocks {
		name := block.Attributes["name"]
		parent := block.Attributes["parent"]
		if name == "" || parent == "" {
			parts = append(parts, generators.Text(fmt.Sprintf(
				"[Session tree] The %s block with boundary %q was not recorded: it needs both parent and name header parameters.\n\n",
				prefix, block.Boundary)))
			continue
		}
		parentNode, ok := tr.Node(parent)
		if !ok {
			parts = append(parts, generators.Text(fmt.Sprintf(
				"[Session tree] The %s block %q was not recorded: parent %q does not exist in the session tree.\n\n",
				prefix, name, parent)))
			continue
		}
		if typ == tree.TypePlan && parentNode.Type == tree.TypePlan {
			aborted, err := tr.Abort(parent, tree.AuthorModel, "superseded by "+name)
			if err != nil {
				parts = append(parts, generators.Text(fmt.Sprintf(
					"[Session tree] The %s block %q was not recorded: %v\n\n", prefix, name, err)))
				continue
			}
			tr = aborted
		}
		next, err := tr.Write(parent, name, typ, tree.AuthorModel, block.Body)
		if err != nil {
			parts = append(parts, generators.Text(fmt.Sprintf(
				"[Session tree] The %s block %q was not recorded: %v\n\n", prefix, name, err)))
			continue
		}
		tr = next
		parts = append(parts, generators.Text(fmt.Sprintf(
			"[Session tree] %s node %q recorded under %q.\n\n", prefix, name, parent)))
	}
	if len(parts) == 0 {
		return components.ProcessResult{}
	}
	return components.ProcessResult{Parts: parts, Tree: tr}
}

// writeBlockNodes writes the attempt's block nodes in one validated
// batch: handled blocks (consumed by the BlockHandler during streaming)
// get auto-named nodes with an applied result child; collected blocks
// are pre-validated — one naming fault discards the whole batch, writes
// an error node, and returns the naming errors. The batch's new-plan
// and response blocks declare named nodes the components will create:
// a collected block may reference such a name as its parent, so its
// node write is deferred — deferredIndexes lists those blocks' indexes
// into collected, their names stay empty, and writeDeferredBlockNodes
// fills both after the components have run. The returned names align
// with collected by index. See TheoryOfSessionTree.
func writeBlockNodes(
	tr *tree.Tree,
	currentResponse string,
	handled, collected []blocks.Block,
) (
	collectedNames []string,
	deferredIndexes []int,
	namingErrs []string,
	newTr *tree.Tree,
) {
	if tr == nil {
		return nil, nil, nil, tr
	}
	// Validation runs against the pre-batch tree plus the names the
	// batch's own earlier blocks declare. A reference to a name declared
	// later in the batch fails, because the components write in block
	// order and the named node would not exist yet.
	declared := make(map[string]bool)
	for _, block := range collected {
		if err := validateBlockParent(tr, currentResponse, block, declared); err != nil {
			namingErrs = append(namingErrs, err.Error())
		}
		if block.Kind != "new-plan" && block.Kind != "response" {
			continue
		}
		if name := block.Attributes["name"]; name != "" {
			if declared[name] {
				namingErrs = append(namingErrs, fmt.Sprintf(
					"block kind %q (boundary %q) declares name %q, which an earlier block of the same batch already declares",
					block.Kind, block.Boundary, name))
			} else {
				declared[name] = true
			}
		}
	}
	if len(namingErrs) > 0 {
		if next, _, err := tr.WriteAuto(currentResponse, "error", tree.TypeError, tree.AuthorProgram,
			"session-tree naming errors discarded the whole block batch:\n"+strings.Join(namingErrs, "\n")); err == nil {
			tr = next
		}
		return nil, nil, namingErrs, tr
	}
	cur := tr
	for _, block := range handled {
		var name string
		cur, name = writeBlockNode(cur, parentAttribute(block, currentResponse), tree.TypeBlock, block)
		if name == "" {
			continue
		}
		if next, _, err := cur.WriteAuto(name, "result", tree.TypeBlockResult, tree.AuthorProgram, "applied"); err == nil {
			cur = next
		}
	}
	names := make([]string, len(collected))
	for i, block := range collected {
		typ := tree.TypeBlock
		if block.Kind == "done" {
			typ = tree.TypeDone
		}
		parent := parentAttribute(block, currentResponse)
		if _, ok := cur.Node(parent); !ok {
			// The parent names a node an earlier block of the batch will
			// create: defer the node write until the components have run.
			deferredIndexes = append(deferredIndexes, i)
			continue
		}
		var name string
		cur, name = writeBlockNode(cur, parent, typ, block)
		names[i] = name
	}
	return names, deferredIndexes, nil, cur
}

// blockNodeContent renders a block node's tree content. A change block
// leads with its op, target, and file header — the same line a
// per-block list would show — so the outline's one-line preview
// carries the modification detail; the body follows on the next line.
// Other blocks carry the body only.
func blockNodeContent(block blocks.Block) string {
	if block.Kind != "change" {
		return block.Body
	}
	op := block.Attributes["op"]
	if op == "" {
		return block.Body
	}
	target := block.Attributes["target"]
	filePath := block.Attributes["file-path"]
	if target != "" {
		return fmt.Sprintf("%s %s in %s\n%s", op, target, filePath, block.Body)
	}
	return fmt.Sprintf("%s in %s\n%s", op, filePath, block.Body)
}

// writeBlockNode writes one auto-named block node; a failed write keeps
// the tree and yields no name, so the caller skips dependent nodes.
// The node content comes from blockNodeContent, so a change block's
// outline preview identifies its op, target, and file. See
// TheoryOfSessionTree.
func writeBlockNode(tr *tree.Tree, parent string, typ tree.Type, block blocks.Block) (*tree.Tree, string) {
	prefix := "block"
	if typ == tree.TypeDone {
		prefix = "done"
	}
	next, name, err := tr.WriteAuto(parent, prefix, typ, tree.AuthorModel, blockNodeContent(block))
	if err != nil || name == "" {
		return tr, ""
	}
	return next, name
}

// parentAttribute resolves a block's parent node: the header's parent
// parameter when present, otherwise the current response node. See
// TheoryOfSessionTree.
func parentAttribute(block blocks.Block, currentResponse string) string {
	if parent := block.Attributes["parent"]; parent != "" {
		return parent
	}
	return currentResponse
}

// validateBlockParent checks one block's session-tree placement: the
// parent (after defaulting) must exist in the tree, or be a name an
// earlier block of the same batch declares (declared); new-plan and
// response blocks must carry both parent and name. See
// TheoryOfSessionTree.
func validateBlockParent(tr *tree.Tree, currentResponse string, block blocks.Block, declared map[string]bool) error {
	switch block.Kind {
	case "new-plan", "response":
		if block.Attributes["parent"] == "" {
			return fmt.Errorf("block kind %q (boundary %q) needs a parent header parameter", block.Kind, block.Boundary)
		}
		if block.Attributes["name"] == "" {
			return fmt.Errorf("block kind %q (boundary %q) needs a name header parameter", block.Kind, block.Boundary)
		}
	}
	parent := parentAttribute(block, currentResponse)
	if _, ok := tr.Node(parent); ok {
		return nil
	}
	if declared[parent] {
		return nil
	}
	return fmt.Errorf("block kind %q (boundary %q) names parent %q, which does not exist in the session tree",
		block.Kind, block.Boundary, parent)
}

// writeBlockResultNodes attaches block-result nodes to the component
// outputs: one part per block writes a result child per block node; any
// other shape writes one shared result node under the first block.
// Block indexes refer to the collected block list, which collectedNames
// aligns with. See TheoryOfSessionTree.
func writeBlockResultNodes(tr *tree.Tree, outputs []components.ComponentOutput, collectedNames []string) *tree.Tree {
	if tr == nil {
		return tr
	}
	cur := tr
	for _, out := range outputs {
		if len(out.Blocks) == 0 || len(out.BlockIndexes) == 0 {
			continue
		}
		if len(out.Parts) == len(out.Blocks) {
			for i := range out.Blocks {
				nodeName := blockNodeName(out.BlockIndexes[i], collectedNames)
				if nodeName == "" {
					continue
				}
				if next, _, err := cur.WriteAuto(nodeName, "result", tree.TypeBlockResult, tree.AuthorProgram, joinTextParts(out.Parts[i:i+1])); err == nil {
					cur = next
				}
			}
			continue
		}
		firstName := blockNodeName(out.BlockIndexes[0], collectedNames)
		if firstName == "" {
			continue
		}
		if next, _, err := cur.WriteAuto(firstName, "result", tree.TypeBlockResult, tree.AuthorProgram, joinTextParts(out.Parts)); err == nil {
			cur = next
		}
	}
	return cur
}

// blockNodeName maps an original block index to its session-tree node
// name; an out-of-range index yields no name. See TheoryOfSessionTree.
func blockNodeName(originalIndex int, collectedNames []string) string {
	if originalIndex < 0 || originalIndex >= len(collectedNames) {
		return ""
	}
	return collectedNames[originalIndex]
}

// writeDeferredBlockNodes writes the deferred block nodes after the
// components have run: a deferred block's parent names a node that an
// earlier block of its batch created, which exists only once the
// component has written it. Each write takes a fresh auto name, and
// names is filled in place so the block-result nodes can attach. See
// TheoryOfSessionTree.
func writeDeferredBlockNodes(
	tr *tree.Tree,
	currentResponse string,
	collected []blocks.Block,
	names []string,
	deferredIndexes []int,
) *tree.Tree {
	if tr == nil || len(deferredIndexes) == 0 {
		return tr
	}
	cur := tr
	for _, i := range deferredIndexes {
		if i < 0 || i >= len(collected) || i >= len(names) {
			continue
		}
		block := collected[i]
		typ := tree.TypeBlock
		if block.Kind == "done" {
			typ = tree.TypeDone
		}
		var name string
		cur, name = writeBlockNode(cur, parentAttribute(block, currentResponse), typ, block)
		if name == "" {
			continue
		}
		names[i] = name
	}
	return cur
}

// joinTextParts concatenates the Text parts verbatim; the callers
// assemble units that already end with blank lines, so no separator is
// added. See generators.TheoryOfContentUnitSeparation.
func joinTextParts(parts []generators.Part) string {
	var sb strings.Builder
	for _, p := range parts {
		if t, ok := p.(generators.Text); ok {
			sb.WriteString(string(t))
		}
	}
	return sb.String()
}

// treeOutlinePart renders the session tree outline as a compact user
// part appended to every round-triggering feedback, so the model sees
// the session structure without the nodes' full content. The outline
// is always the current loop's subtree, rendered from the session
// root, projected onto the model-facing nodes — the loop's own event
// nodes are program bookkeeping and are excluded by category — so
// each round's feedback volume stays independent of the run's length
// and of other loops' content. The leading newline keeps the outline
// on its own line: parts concatenate verbatim, and the content before
// it may end without a line break. See TheoryOfSessionTree.
func treeOutlinePart(tr *tree.Tree, parent string) generators.Text {
	if tr == nil {
		return generators.Text("")
	}
	proj := tr.Extract(func(n *tree.Node) bool { return n.Category() != tree.CategoryEvent })
	return generators.Text("\n[Session tree]\n" + sessionOutline(proj, parent) + "\n")
}

// handoffOutlinePart renders the handoff's tree outline: the projection
// of the session's tree onto its decision-level nodes — every node
// outside the block and event categories — rendered from the session
// root, so the handoff summary carries the session's plans, decisions,
// and earlier summaries without execution detail and without other
// loops' content. See TheoryOfSessionTree and tree.TheoryOfSubtree.
func handoffOutlinePart(tr *tree.Tree, parent string) string {
	if tr == nil {
		return ""
	}
	proj := tr.Extract(func(n *tree.Node) bool {
		return n.Category() != tree.CategoryBlock && n.Category() != tree.CategoryEvent
	})
	return "[Session tree]\n" + sessionOutline(proj, parent) + "\n"
}

// handoffInput prefixes the incomplete output with the handoff subtree
// outline, so the handoff summary carries the session's plans, decisions,
// and earlier summaries — without the block and block-result execution
// detail — into the retry attempt. An empty output yields an empty input,
// keeping the caller's threshold gate. See TheoryOfSessionTree and
// tree.TheoryOfSubtree.
func handoffInput(incompleteText string, tr *tree.Tree, parent string) string {
	if incompleteText == "" {
		return ""
	}
	outline := handoffOutlinePart(tr, parent)
	if outline == "" {
		return incompleteText
	}
	return outline + "\n" + incompleteText
}

// sessionOutline renders the session's outline: always the current
// loop's subtree, from the session root — the loop node for a
// continued run, the tree root for a fresh one — so each round's
// feedback volume stays independent of the run's length and of other
// loops' content. The preview cap of 120 runes lets a change block's
// op, target, and file header line show in full, so the outline
// carries the applied-changes detail of a per-block list. See
// TheoryOfSessionTree.
func sessionOutline(tr *tree.Tree, parent string) string {
	if parent == "" {
		parent = "root"
	}
	return tr.RenderSubtree(parent, 120)
}

// SessionTreeContinuation continues one run's session tree: Tree is the
// tree to write into and Parent is the node under which this session's
// nodes are written — in goal mode the run's loop-N node. The zero
// value opens a fresh tree under the root. The type is dscope-provided
// so a goal loop's scope carries the continuation the runner prepared,
// and Module.Run binds it at provider resolution. See
// TheoryOfSessionTree and TheoryOfGoalMode.
type SessionTreeContinuation struct {
	Tree   *tree.Tree
	Parent string
}

// SessionTreeContinuation provides the default continuation: zero, so a
// run that is not a goal loop opens a fresh tree. The goal runner forks
// the run tree and the loop node into each loop's scope. See
// TheoryOfSessionTree.
func (Module) SessionTreeContinuation() SessionTreeContinuation {
	return SessionTreeContinuation{}
}

// extractModelTexts joins the Text parts of the model-role contents
// appended after sinceCount; Thought parts never enter the tree. See
// TheoryOfSessionTree.
func extractModelTexts(state generators.State, sinceCount int) string {
	var texts []string
	i := 0
	for c := range state.Contents() {
		if i >= sinceCount &&
			(c.Role == generators.RoleModel || c.Role == generators.RoleAssistant) {
			for _, p := range c.Parts {
				if t, ok := p.(generators.Text); ok {
					texts = append(texts, string(t))
				}
			}
		}
		i++
	}
	return strings.Join(texts, "")
}

// extractUserTextsSince joins the Text parts of the user-role contents
// appended after sinceCount. See TheoryOfSessionTree.
func extractUserTextsSince(state generators.State, sinceCount int) string {
	var texts []string
	i := 0
	for c := range state.Contents() {
		if i >= sinceCount && c.Role == generators.RoleUser {
			for _, p := range c.Parts {
				if t, ok := p.(generators.Text); ok {
					texts = append(texts, string(t))
				}
			}
		}
		i++
	}
	return strings.Join(texts, "")
}

// recordAttemptTree writes the successful attempt's nodes: the response
// node under the session root (the attempt's model-role Text parts), one
// summary node per summary body, and the block batch (handled plus
// collected). Blocks whose parent names a node an earlier block of the
// batch creates are deferred: their collected indexes return to the
// caller, and writeDeferredBlockNodes writes their nodes after the
// components have run. On a naming fault the batch is discarded, the
// error node recorded, the errors stored for the shared correction
// decision, and the returned names are nil. Every tree write ends with
// a full-tree yield, so the consumer sees the attempt's session nodes
// the moment they exist. See TheoryOfSessionTree and TheoryOfLoopEvents.
func (ls *loopState) recordAttemptTree(
	phaseState generators.State,
	attemptBase int,
	summaries []string,
	handled, collected []blocks.Block,
) (collectedNames []string, deferredIndexes []int) {
	if ls.sessionTree == nil {
		return nil, nil
	}
	next, responseName, err := ls.sessionTree.WriteAuto(ls.sessionParent(), "response", tree.TypeResponse, tree.AuthorModel, extractModelTexts(phaseState, attemptBase))
	if err != nil {
		if ls.rec != nil && ls.rec.Enabled() {
			ls.rec.Event("decision", fmt.Sprintf("session tree: response node not written: %v", err))
		}
	} else {
		ls.sessionTree = next
		ls.currentResponse = responseName
		for _, summary := range summaries {
			if snext, _, serr := ls.sessionTree.WriteAuto(responseName, "summary", tree.TypeSummary, tree.AuthorModel, summary); serr == nil {
				ls.sessionTree = snext
			}
		}
	}
	names, deferred, namingErrs, blockTree := writeBlockNodes(ls.sessionTree, ls.currentResponse, handled, collected)
	if len(namingErrs) > 0 {
		ls.namingErrs = namingErrs
		ls.sessionTree = blockTree
		ls.emitTree()
		return nil, nil
	}
	ls.sessionTree = blockTree
	ls.emitTree()
	return names, deferred
}

// writeFeedbackInputNode records round feedback as an input node
// written by the program under the session root. See
// TheoryOfSessionTree.
func (ls *loopState) writeFeedbackInputNode(parts []generators.Part) {
	if ls.sessionTree == nil || len(parts) == 0 {
		return
	}
	if next, _, err := ls.sessionTree.WriteAuto(ls.sessionParent(), "input", tree.TypeInput, tree.AuthorProgram, joinTextParts(parts)); err == nil {
		ls.sessionTree = next
	}
}

// recordIdleUserInput records the idle handler's user input as an input
// node written under the session root, extracted from the contents
// appended since sinceCount. See TheoryOfSessionTree.
func (ls *loopState) recordIdleUserInput(state generators.State, sinceCount int) {
	text := extractUserTextsSince(state, sinceCount)
	if text == "" || ls.sessionTree == nil {
		return
	}
	if next, _, err := ls.sessionTree.WriteAuto(ls.sessionParent(), "input", tree.TypeInput, tree.AuthorUser, text); err == nil {
		ls.sessionTree = next
	}
}

// sessionParent names the node under which this session writes its own
// nodes: the goal loop's loop node for a continued run, the tree's root
// for a fresh one. The fallback keeps a loopState built outside Run —
// in tests — writing under the root. See TheoryOfSessionTree.
func (ls *loopState) sessionParent() string {
	if ls.sessionRoot == "" {
		return "root"
	}
	return ls.sessionRoot
}

// recordAttemptErrorNodes writes the attempt's unprocessable output as
// an error node (program author) under the current response: the
// malformed blocks the parser could not parse and the well-formed
// blocks whose kind the session cannot process. The node is recorded
// regardless of the correction budget — it is the tree's error record,
// extractable with ByType; the shared budget governs only whether the
// model is asked to correct. Unavailable-kind block nodes stay
// childless, so the childless node keeps its unprocessed meaning. See
// TheoryOfSessionTree.
func (ls *loopState) recordAttemptErrorNodes(
	parseErrors []*blocks.BlockParseError,
	unknownKinds []blocks.Block,
) {
	if ls.sessionTree == nil || ls.currentResponse == "" {
		return
	}
	if len(parseErrors) == 0 && len(unknownKinds) == 0 {
		return
	}
	var sb strings.Builder
	for _, parseErr := range parseErrors {
		sb.WriteString("malformed block: ")
		sb.WriteString(parseErr.Error())
		sb.WriteString("\n")
	}
	for _, block := range unknownKinds {
		fmt.Fprintf(&sb, "unavailable kind: kind %q, boundary %q was never processed\n", block.Kind, block.Boundary)
	}
	if next, _, err := ls.sessionTree.WriteAuto(ls.currentResponse, "error", tree.TypeError, tree.AuthorProgram, sb.String()); err == nil {
		ls.sessionTree = next
	}
}
