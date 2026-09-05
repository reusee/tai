// Package tree implements the session tree of the AI-assisted development
// pipeline: every operation of every participant — user, model, program —
// is expressed as a write to one immutable tree.
package tree

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

const TheoryOfTree = `
tree theory: writes and transforms on immutable path-copying trees.
- Every operation of every participant — user, model, program — is expressed
  as a write: Write(parent, name, type, author, content). Names are globally
  unique; the program validates them (empty name, duplicate name, unknown
  parent, invalid author) and feeds errors back so the model can correct its
  naming in the next round.
- Immutability is implemented by path copying: every operation copies the
  path from the target up to the root, swapping the copied child pointer
  into each copied ancestor's children slice, and returns a new Tree.
  Untouched subtrees share node pointers with the old tree, so an operation
  costs O(depth) node copies. The byName index is rebuilt by whole-map copy
  per operation, an O(n) cost accepted for a pure-value tree. Write, Merge,
  Modify, and Delete share one path-copying core, so their immutability
  semantics stay identical.
- Modify and Delete transform the tree beyond appends: Modify rewrites a
  node's content in place — name, parent, type, author, children, and
  InsertTime are preserved, so a rewrite changes what the node says, never
  where or when it was written. Delete removes a node and its descendants;
  their names leave the index, so they can be written again. The root is
  structural: it can be neither modified nor deleted. Every transformation
  returns a new tree, so earlier versions remain valid snapshots —
  immutability is untouched.
- Batch writes are atomic: WriteAll stages the writes in order on an
  intermediate tree; a failing op returns an error and leaves the receiver
  untouched, and a successful batch returns one tree carrying every write.
  Later ops in a batch may reference nodes written earlier in the batch.
- A write carries an insert time: the batch op form accepts one, the zero
  time takes the current time, and Merge keeps every grafted node's
  original time, so a replayed or merged subtree preserves its chronology.
- Concurrent branches merge: Merge grafts the nodes of another tree that
  the receiver lacks under the shared ancestors, preserving their original
  InsertTime values; a node present in both trees must be logically
  identical (same parent, type, author, and content) or the merge fails
  and the receiver is unchanged.
- Plan revision: a plan whose content changes is rewritten with Modify —
  the node keeps its identity, position, and chronology, and a direct
  rewrite avoids the drift of re-emitting the whole plan as a new node. A
  plan abandoned in direction is aborted: Abort writes an abort child under
  it, recording who aborted and why, and the replacement plan is a new
  node.
- A block node without children is an unprocessed block, except blocks that
  need no processing (done, summary). Block execution results are written
  as block-result child nodes by the program.
- Node kinds form two layers. Type is the fine-grained kind: structure
  nodes (root, loop, attempt), message content (system, user, plan, model,
  done, abort), per-occurrence event subtypes (attempt-start, request,
  finish, usage, truncated, retry, handoff-start, handoff, completed,
  synthesized-summary, thought-summary, continue, idle, run-error, goal),
  block execution (block-result and summary, plus the block kinds — a
  block node's type is the kind of the block it records, so unknown kinds
  form types dynamically), and error. Category is the coarse layer derived
  from the type (Node.Category): structure, message, event, block, error.
  Category is never written — it is a pure function of Type — so the write
  surface, merge identity, and chronology stay type-only, and consumers
  select whole families with ByCategory. Every event subtype's string
  equals the event node name prefix the pipeline has always written, so
  typed event nodes keep their historical names. A block kind sharing a
  string with an event subtype (continue) derives to that subtype's
  category; every other unknown string derives to block. Summary is a
  block kind: it records the response's summary block, not a message.
- Type.Emoji and Category.Emoji supply the display glyphs of user-facing
  trees. Built-in block kinds carry predefined glyphs; any other kind,
  and any unknown type, falls back to the brick glyph. They are
  presentation metadata, never identity: writes and merges ignore them.
`

const TheoryOfSubtree = `
Subtree extraction theory:
- The session history is a tree, so the context for different consumers is
  a subtree: Subtree walks depth-first from a named node, SubtreeToDepth
  bounds the walk by a relative depth from the named node, and
  RenderSubtree renders an indented outline with truncated content
  previews. The handoff content, the per-round tree outline fed back to
  the model, and review views are all subtree projections.
- ByType, ByAuthor, and Filter select nodes across the whole tree so
  consumers compose projections without walking the tree themselves.
- Extract projects the nodes matching a predicate, plus every ancestor
  above them, onto a new immutable tree: the projection keeps its path
  context, prunes everything else, preserves insert times, and composes
  with Merge, so concurrently processed subtrees rejoin into one tree.
`

// Type classifies a node's role in the session.
type Type string

const (
	TypeRoot        Type = "root"
	TypeSystem      Type = "system"
	TypeUser        Type = "user"
	TypeModel       Type = "model"
	TypePlan        Type = "plan"
	TypeBlockResult Type = "block-result"
	TypeError       Type = "error"
	TypeSummary     Type = "summary"
	TypeDone        Type = "done"
	TypeAbort       Type = "abort"
)

// TypeLoop marks one loop of a goal run: the run's tree carries one
// loop node per loop, and the loop's session nodes hang under it.
// See pipeline.TheoryOfSessionTree and pipeline.TheoryOfGoalMode.
const TypeLoop Type = "loop"

// TypeAttempt marks one attempt of a generation session: one pass
// through the phase chain. The pipeline writes one attempt node per
// attempt under the session parent — the goal loop's loop node for a
// continued run, the tree root for a fresh one — and the attempt's
// response, summaries, blocks, errors, and events hang under it. The
// session's system and initial user nodes stay the attempt nodes'
// siblings. See pipeline.TheoryOfSessionTree.
const TypeAttempt Type = "attempt"

// The event subtypes classify one recorded occurrence each: one type
// per occurrence kind of a run. Every constant's string equals the
// event node name prefix the pipeline has always written, so typed
// event nodes keep their historical names. All of them derive to
// CategoryEvent. See TheoryOfTree.
const (
	TypeAttemptStart       Type = "attempt-start"
	TypeRequest            Type = "request"
	TypeFinish             Type = "finish"
	TypeUsage              Type = "usage"
	TypeTruncated          Type = "truncated"
	TypeRetry              Type = "retry"
	TypeHandoffStart       Type = "handoff-start"
	TypeHandoff            Type = "handoff"
	TypeCompleted          Type = "completed"
	TypeSynthesizedSummary Type = "synthesized-summary"
	TypeThoughtSummary     Type = "thought-summary"
	TypeContinue           Type = "continue"
	TypeIdle               Type = "idle"
	TypeRunError           Type = "run-error"
	TypeGoal               Type = "goal"
)

// Category is the coarse classification layer above Type: a pure
// function of the type, never a written field. Consumers select whole
// families of nodes with it; the TUI's collapsed rows show it instead
// of the node name and author. See TheoryOfTree.
type Category string

const (
	CategoryStructure Category = "structure"
	CategoryMessage   Category = "message"
	CategoryEvent     Category = "event"
	CategoryBlock     Category = "block"
	CategoryError     Category = "error"
)

// Category returns the category the type belongs to. See TheoryOfTree.
func (t Type) Category() Category {
	switch t {
	case TypeRoot, TypeLoop, TypeAttempt:
		return CategoryStructure
	case TypeSystem, TypeUser, TypeModel, TypePlan,
		TypeDone, TypeAbort:
		return CategoryMessage
	case TypeAttemptStart, TypeRequest, TypeFinish, TypeUsage,
		TypeTruncated, TypeRetry, TypeHandoffStart, TypeHandoff,
		TypeCompleted, TypeSynthesizedSummary, TypeThoughtSummary,
		TypeContinue, TypeIdle, TypeRunError, TypeGoal:
		return CategoryEvent
	case TypeBlockResult, TypeSummary:
		return CategoryBlock
	case TypeError:
		return CategoryError
	default:
		// Any other type string is a block kind: a block node's type
		// is the kind of the block it records. See TheoryOfTree.
		return CategoryBlock
	}
}

// Emoji returns the display glyph decorating the type in user-facing
// trees. Presentation metadata, never identity: writes and merges
// ignore it. Built-in block kinds carry predefined glyphs; any other
// kind falls back to the brick. See TheoryOfTree.
func (t Type) Emoji() string {
	switch t {
	case TypeRoot:
		return "🌳"
	case TypeLoop:
		return "🔁"
	case TypeAttempt:
		return "⏱️"
	case TypeSystem:
		return "📜"
	case TypeUser:
		return "💬"
	case TypeModel:
		return "🤖"
	case TypePlan:
		return "🗺️"
	case TypeSummary:
		return "📝"
	case TypeDone:
		return "✅"
	case TypeAbort:
		return "🚫"
	case TypeAttemptStart:
		return "🚀"
	case TypeRequest:
		return "📤"
	case TypeFinish:
		return "🏁"
	case TypeUsage:
		return "🔢"
	case TypeTruncated:
		return "✂️"
	case TypeRetry:
		return "🔄"
	case TypeHandoffStart:
		return "🤲"
	case TypeHandoff:
		return "🤝"
	case TypeCompleted:
		return "🎉"
	case TypeSynthesizedSummary:
		return "🧩"
	case TypeThoughtSummary:
		return "💭"
	case TypeContinue:
		return "➡️"
	case TypeIdle:
		return "⏸️"
	case TypeRunError:
		return "❌"
	case TypeGoal:
		return "🎯"
	// Built-in block kinds: a block node's type is its block kind.
	case Type("change"):
		return "🔧"
	case Type("shell"):
		return "🐚"
	case Type("go-test"):
		return "🧪"
	case Type("go-src"):
		return "🔍"
	case Type("ingest"):
		return "📥"
	case Type("new-plan"):
		return "📋"
	case Type("response"):
		return "📨"
	case Type("memory"):
		return "🧠"
	case TypeBlockResult:
		return "📎"
	case TypeError:
		return "⚠️"
	default:
		// Fallback glyph for block kinds without a predefined one.
		return "🧱"
	}
}

// Emoji returns the display glyph decorating the category in
// user-facing trees. Presentation metadata, never identity. See
// TheoryOfTree.
func (c Category) Emoji() string {
	switch c {
	case CategoryStructure:
		return "🗂️"
	case CategoryMessage:
		return "✉️"
	case CategoryEvent:
		return "📡"
	case CategoryBlock:
		return "🔨"
	case CategoryError:
		return "🚨"
	default:
		return "•"
	}
}

// Category returns the node's category, derived from its type. See
// TheoryOfTree.
func (n *Node) Category() Category {
	return n.Type.Category()
}

// Author identifies who wrote a node.
type Author string

const (
	AuthorUser    Author = "user"
	AuthorModel   Author = "model"
	AuthorProgram Author = "program"
)

var (
	ErrBadName       = errors.New("bad node name")
	ErrDuplicateName = errors.New("duplicate node name")
	ErrUnknownParent = errors.New("unknown parent")
	ErrBadAuthor     = errors.New("invalid author")
)

// ErrUnknownNode reports that no node carries the given name.
var ErrUnknownNode = errors.New("unknown node")

// Node is one tree node. Nodes are never mutated after creation: a write
// copies the path to the root and shares untouched subtrees by pointer.
type Node struct {
	Name       string
	Parent     string
	Type       Type
	Author     Author
	Content    string
	InsertTime time.Time

	children []*Node
}

// Children returns the node's children in insertion order. The returned
// slice is a copy: mutating it does not affect the tree.
func (n *Node) Children() []*Node {
	ret := make([]*Node, len(n.children))
	copy(ret, n.children)
	return ret
}

// IsAborted reports whether the node carries an abort child.
func (n *Node) IsAborted() bool {
	for _, c := range n.children {
		if c.Type == TypeAbort {
			return true
		}
	}
	return false
}

// Tree is an immutable tree of session operations. Every Write returns a
// new Tree sharing untouched subtrees with the original by pointer.
// See TheoryOfTree.
type Tree struct {
	root   *Node
	byName map[string]*Node
}

// New returns a tree containing only the root node.
func New() *Tree {
	root := &Node{Name: "root", Type: TypeRoot}
	return &Tree{
		root:   root,
		byName: map[string]*Node{"root": root},
	}
}

// Root returns the tree's root node.
func (t *Tree) Root() *Node {
	return t.root
}

// Node returns the node with the given name.
func (t *Tree) Node(name string) (*Node, bool) {
	n, ok := t.byName[name]
	return n, ok
}

func validateWrite(name string, author Author) error {
	if name == "" {
		return fmt.Errorf("%w: node name is empty", ErrBadName)
	}
	switch author {
	case AuthorUser, AuthorModel, AuthorProgram:
	default:
		return fmt.Errorf("%w: %q", ErrBadAuthor, author)
	}
	return nil
}

// Write returns a new tree with one node written under parent. The original
// tree is unchanged; nodes off the write path are shared by pointer.
// See TheoryOfTree.
func (t *Tree) Write(parent, name string, typ Type, author Author, content string) (*Tree, error) {
	return t.writeOp(WriteOp{
		Parent:  parent,
		Name:    name,
		Type:    typ,
		Author:  author,
		Content: content,
	})
}

// writeOp applies one write: validation, duplicate check, the zero insert
// time taking the current time, and the shared path-copying core. Write
// and WriteAll delegate here, so both carry identical semantics.
// See TheoryOfTree.
func (t *Tree) writeOp(op WriteOp) (*Tree, error) {
	if err := validateWrite(op.Name, op.Author); err != nil {
		return nil, err
	}
	if _, exists := t.byName[op.Name]; exists {
		return nil, fmt.Errorf("%w: %q", ErrDuplicateName, op.Name)
	}
	insertTime := op.InsertTime
	if insertTime.IsZero() {
		insertTime = time.Now()
	}
	child := &Node{
		Name:       op.Name,
		Parent:     op.Parent,
		Type:       op.Type,
		Author:     op.Author,
		Content:    op.Content,
		InsertTime: insertTime,
	}
	return t.writeChild(child)
}

// writeChild attaches a fully built child node — unique name, existing
// parent — by path copying. It is the shared core of Write and Merge. The
// ancestor walk delegates to copyPathToRoot, shared with Modify and Delete,
// so all carry identical path-copying semantics. See TheoryOfTree.
func (t *Tree) writeChild(child *Node) (*Tree, error) {
	parentNode, ok := t.byName[child.Parent]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownParent, child.Parent)
	}

	// Path copying: copy the parent, append the child, then copy every
	// ancestor up to the root, swapping in the copied child pointer.
	newParent := shallowCopy(parentNode)
	newParent.children = append(slices.Clone(parentNode.children), child)
	newRoot, copied := t.copyPathToRoot(newParent)

	// Re-index the copied path and the new child. Copied nodes carry their
	// originals' names, so assignment overwrites the old-version entries;
	// untouched nodes keep their shared pointers.
	byName := maps.Clone(t.byName)
	for _, n := range copied {
		byName[n.Name] = n
	}
	byName[child.Name] = child

	return &Tree{root: newRoot, byName: byName}, nil
}

// copyPathToRoot copies every ancestor of newNode up to the root, swapping
// the copied node into each copied ancestor's children slice, and returns
// the new root together with every copied node for re-indexing. newNode
// must already sit at its position in its parent's children slice, appended
// or swapped in by the caller. It is the shared core of Write, Merge,
// Modify, and Delete, so all carry identical path-copying semantics. See
// TheoryOfTree.
func (t *Tree) copyPathToRoot(newNode *Node) (newRoot *Node, copied []*Node) {
	copied = []*Node{newNode}
	cur := newNode
	for {
		ancestorName := cur.Parent
		if ancestorName == "" {
			break
		}
		ancestor := t.byName[ancestorName]
		newAncestor := shallowCopy(ancestor)
		newAncestor.children = slices.Clone(ancestor.children)
		for i, c := range newAncestor.children {
			if c.Name == cur.Name {
				newAncestor.children[i] = cur
				break
			}
		}
		copied = append(copied, newAncestor)
		cur = newAncestor
	}
	return cur, copied
}

// Merge returns a new tree carrying every node of other that the
// receiver lacks, grafted under the shared ancestors of the two trees;
// both inputs are unchanged, and nodes off every graft path are shared
// by pointer. A node present in both trees must be logically identical —
// same parent, type, author, and content — or the merge fails with
// ErrDuplicateName and no tree is returned. Grafted nodes keep their
// original InsertTime. See TheoryOfTree.
func (t *Tree) Merge(other *Tree) (*Tree, error) {
	if other == nil {
		return t, nil
	}
	cur := t
	var graft func(n *Node) error
	graft = func(n *Node) error {
		if existing, ok := cur.byName[n.Name]; ok {
			// The node exists in both trees: it is the same write only
			// when every identity field matches.
			if existing.Parent != n.Parent || existing.Type != n.Type ||
				existing.Author != n.Author || existing.Content != n.Content {
				return fmt.Errorf("%w: %q conflicts with the receiver's node", ErrDuplicateName, n.Name)
			}
		} else {
			child := &Node{
				Name:       n.Name,
				Parent:     n.Parent,
				Type:       n.Type,
				Author:     n.Author,
				Content:    n.Content,
				InsertTime: n.InsertTime,
			}
			next, err := cur.writeChild(child)
			if err != nil {
				return err
			}
			cur = next
		}
		for _, c := range n.children {
			if err := graft(c); err != nil {
				return err
			}
		}
		return nil
	}
	// The roots are the same node conceptually; graft from the root's
	// children, depth-first, so a parent is always present before its
	// children are grafted.
	for _, c := range other.root.children {
		if err := graft(c); err != nil {
			return nil, err
		}
	}
	return cur, nil
}

// shallowCopy copies a node's value fields. The children slice is left nil:
// the caller builds its own slice, so append never writes into an array
// shared with another tree version.
func shallowCopy(n *Node) *Node {
	c := *n
	c.children = nil
	return &c
}

// WriteOp is one write of a batch. InsertTime is optional: the zero time
// takes the current time, and a replayed op carries its original time so
// a merged or replayed subtree keeps its chronology. See TheoryOfTree.
type WriteOp struct {
	Parent     string
	Name       string
	Type       Type
	Author     Author
	Content    string
	InsertTime time.Time
}

// WriteAll applies the writes in order, atomically: every write stages on
// an intermediate tree, so a failing op returns an error and leaves the
// receiver untouched, and a successful batch returns one tree carrying
// every write. Later ops may reference nodes written earlier in the batch.
func (t *Tree) WriteAll(ops ...WriteOp) (*Tree, error) {
	cur := t
	for _, op := range ops {
		next, err := cur.writeOp(op)
		if err != nil {
			return nil, err
		}
		cur = next
	}
	return cur, nil
}

// Modify returns a new tree with the named node's content replaced. The
// node keeps its name, parent, type, author, children, and InsertTime, so
// a rewrite changes what the node says, never where or when it was
// written. The root is structural and cannot be modified. See TheoryOfTree.
func (t *Tree) Modify(name, content string) (*Tree, error) {
	node, ok := t.byName[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownNode, name)
	}
	if node.Name == t.root.Name {
		return nil, fmt.Errorf("%w: cannot modify the root node", ErrBadName)
	}
	parentNode, ok := t.byName[node.Parent]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownParent, node.Parent)
	}
	// Swap the rewritten copy into its parent's children, then copy the
	// ancestors up to the root. The children slice is shared, never
	// mutated: every operation builds its own slice, so the untouched
	// subtree is shared by pointer.
	modified := shallowCopy(node)
	modified.Content = content
	modified.children = node.children
	newParent := shallowCopy(parentNode)
	newParent.children = slices.Clone(parentNode.children)
	for i, c := range newParent.children {
		if c.Name == modified.Name {
			newParent.children[i] = modified
			break
		}
	}
	newRoot, copied := t.copyPathToRoot(newParent)

	byName := maps.Clone(t.byName)
	for _, n := range copied {
		byName[n.Name] = n
	}
	byName[modified.Name] = modified

	return &Tree{root: newRoot, byName: byName}, nil
}

// Delete returns a new tree with the named node and its descendants
// removed. Their names leave the index, so they can be written again. The
// root is structural and cannot be deleted. See TheoryOfTree.
func (t *Tree) Delete(name string) (*Tree, error) {
	node, ok := t.byName[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownNode, name)
	}
	if node.Name == t.root.Name {
		return nil, fmt.Errorf("%w: cannot delete the root node", ErrBadName)
	}
	parentNode, ok := t.byName[node.Parent]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownParent, node.Parent)
	}
	// Drop the node from its parent's children, then copy the ancestors
	// up to the root.
	newParent := shallowCopy(parentNode)
	kept := make([]*Node, 0, len(parentNode.children)-1)
	for _, c := range parentNode.children {
		if c.Name != name {
			kept = append(kept, c)
		}
	}
	newParent.children = kept
	newRoot, copied := t.copyPathToRoot(newParent)

	byName := maps.Clone(t.byName)
	for _, n := range copied {
		byName[n.Name] = n
	}
	// The removed subtree's names leave the index; its nodes stay shared
	// by pointer in earlier tree versions.
	var drop func(n *Node)
	drop = func(n *Node) {
		delete(byName, n.Name)
		for _, c := range n.children {
			drop(c)
		}
	}
	drop(node)

	return &Tree{root: newRoot, byName: byName}, nil
}

// Abort marks the node as abandoned by writing an abort child under it,
// recording who aborted and why. Plan revision is Abort plus a new node,
// never a mutation. See TheoryOfTree.
func (t *Tree) Abort(name string, by Author, reason string) (*Tree, error) {
	if _, ok := t.byName[name]; !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownParent, name)
	}
	return t.WriteAll(WriteOp{
		Parent:  name,
		Name:    t.AutoName(name + "-abort"),
		Type:    TypeAbort,
		Author:  by,
		Content: reason,
	})
}

// AutoName returns a name unique in the tree, prefixed and numbered.
func (t *Tree) AutoName(prefix string) string {
	if prefix == "" {
		prefix = "node"
	}
	for i := 1; ; i++ {
		name := fmt.Sprintf("%s-%d", prefix, i)
		if _, exists := t.byName[name]; !exists {
			return name
		}
	}
}

// WriteAuto writes a node with an auto-generated unique name and returns
// the new tree together with the name used.
func (t *Tree) WriteAuto(parent, prefix string, typ Type, author Author, content string) (*Tree, string, error) {
	name := t.AutoName(prefix)
	newTree, err := t.Write(parent, name, typ, author, content)
	if err != nil {
		return t, "", err
	}
	return newTree, name, nil
}

// Subtree returns the named node and its descendants, depth-first. A
// missing node returns nil. See TheoryOfSubtree.
func (t *Tree) Subtree(name string) []*Node {
	start, ok := t.byName[name]
	if !ok {
		return nil
	}
	var out []*Node
	var walk func(n *Node)
	walk = func(n *Node) {
		out = append(out, n)
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(start)
	return out
}

// SubtreeToDepth returns the named node and its descendants down to the
// given relative depth, the start node at depth 0; descendants deeper
// than maxDepth are omitted. maxDepth < 0 returns only the start node. A
// missing node returns nil. See TheoryOfSubtree.
func (t *Tree) SubtreeToDepth(name string, maxDepth int) []*Node {
	start, ok := t.byName[name]
	if !ok {
		return nil
	}
	var out []*Node
	var walk func(n *Node, depth int)
	walk = func(n *Node, depth int) {
		out = append(out, n)
		if depth >= maxDepth {
			return
		}
		for _, c := range n.children {
			walk(c, depth+1)
		}
	}
	walk(start, 0)
	return out
}

// Depth returns the depth of the named node, the root at depth 0. A
// missing node returns -1.
func (t *Tree) Depth(name string) int {
	n, ok := t.byName[name]
	if !ok {
		return -1
	}
	depth := 0
	for n.Parent != "" {
		depth++
		n = t.byName[n.Parent]
	}
	return depth
}

// Filter returns every node matching pred, depth-first from the root.
func (t *Tree) Filter(pred func(*Node) bool) []*Node {
	var out []*Node
	var walk func(n *Node)
	walk = func(n *Node) {
		if pred(n) {
			out = append(out, n)
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(t.root)
	return out
}

// ByType returns every node of the given type.
func (t *Tree) ByType(typ Type) []*Node {
	return t.Filter(func(n *Node) bool { return n.Type == typ })
}

// ByAuthor returns every node written by the given author.
func (t *Tree) ByAuthor(author Author) []*Node {
	return t.Filter(func(n *Node) bool { return n.Author == author })
}

// ByCategory returns every node whose type belongs to the given
// category. See TheoryOfTree.
func (t *Tree) ByCategory(cat Category) []*Node {
	return t.Filter(func(n *Node) bool { return n.Category() == cat })
}

// Extract returns a new tree carrying every node matching pred together
// with the ancestors above them: the projection keeps its path context,
// prunes every node off the selected paths, and preserves each kept
// node's InsertTime. A matching node's descendants stay only when they
// also match or contain a match. The projection is a fresh tree that
// never shares nodes with the receiver, and it composes with Merge, so
// subtrees processed concurrently rejoin into one. See TheoryOfSubtree.
func (t *Tree) Extract(pred func(*Node) bool) *Tree {
	// keep[n.Name] reports whether n or one of its descendants matches,
	// so an ancestor of a match stays on the projection's path.
	keep := make(map[string]bool, len(t.byName))
	var mark func(n *Node) bool
	mark = func(n *Node) bool {
		matched := pred(n)
		for _, c := range n.children {
			if mark(c) {
				matched = true
			}
		}
		keep[n.Name] = matched
		return matched
	}
	mark(t.root)
	byName := make(map[string]*Node, len(t.byName))
	var build func(src *Node) *Node
	build = func(src *Node) *Node {
		node := &Node{
			Name:       src.Name,
			Parent:     src.Parent,
			Type:       src.Type,
			Author:     src.Author,
			Content:    src.Content,
			InsertTime: src.InsertTime,
		}
		byName[src.Name] = node
		for _, c := range src.children {
			if keep[c.Name] {
				node.children = append(node.children, build(c))
			}
		}
		return node
	}
	return &Tree{root: build(t.root), byName: byName}
}

// RenderOutline renders the whole tree as indented outline lines.
func (t *Tree) RenderOutline(maxPreview int) string {
	return t.RenderSubtree("root", maxPreview)
}

// RenderSubtree renders the named subtree as indented outline lines, one
// line per node: "name [type/author] preview", aborted nodes marked.
// See TheoryOfSubtree.
func (t *Tree) RenderSubtree(name string, maxPreview int) string {
	start, ok := t.byName[name]
	if !ok {
		return ""
	}
	var sb strings.Builder
	var walk func(n *Node, depth int)
	walk = func(n *Node, depth int) {
		sb.WriteString(strings.Repeat("  ", depth))
		sb.WriteString(n.Name)
		sb.WriteString(" [")
		sb.WriteString(string(n.Type))
		sb.WriteString("/")
		sb.WriteString(string(n.Author))
		sb.WriteString("]")
		if preview := PreviewRunes(n.Content, maxPreview); preview != "" {
			sb.WriteString(" ")
			sb.WriteString(preview)
		}
		if n.IsAborted() {
			sb.WriteString(" (aborted)")
		}
		sb.WriteString("\n")
		for _, c := range n.children {
			walk(c, depth+1)
		}
	}
	walk(start, 0)
	return sb.String()
}

// PreviewRunes returns the first line of s, trimmed, truncated to max
// runes with an ellipsis. max <= 0 returns the first line untruncated.
func PreviewRunes(s string, max int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) > max {
		return string(runes[:max]) + "…"
	}
	return s
}
