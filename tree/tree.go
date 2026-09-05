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
tree theory: one write operation, immutable path-copying trees.
- Every operation of every participant — user, model, program — is expressed
  as a write: Write(parent, name, type, author, content). Names are globally
  unique; the program validates them (empty name, duplicate name, unknown
  parent, invalid author) and feeds errors back so the model can correct its
  naming in the next round.
- Immutability is implemented by path copying: Write copies every node on
  the path from the parent up to the root, swaps the copied child pointer
  into each copied ancestor's children slice, and returns a new Tree.
  Untouched subtrees share node pointers with the old tree, so a write
  costs O(depth) node copies. The byName index is rebuilt by whole-map copy
  per write, an O(n) cost accepted for a pure-value tree. Write and Merge
  share one path-copying core, so their immutability semantics stay
  identical.
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
- Plan revision is Abort plus a new node: Abort writes an abort child under
  the abandoned node, recording who aborted and why; the revised plan is a
  new node, never a mutation.
- A block node without children is an unprocessed block, except blocks that
  need no processing (done, summary). Block execution results are written
  as block-result child nodes by the program.
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
	TypeRoot         Type = "root"
	TypeSystemPrompt Type = "system-prompt"
	TypeInput        Type = "input"
	TypePlan         Type = "plan"
	TypeResponse     Type = "response"
	TypeBlock        Type = "block"
	TypeBlockResult  Type = "block-result"
	TypeError        Type = "error"
	TypeSummary      Type = "summary"
	TypeDone         Type = "done"
	TypeAbort        Type = "abort"
)

// TypeLoop marks one loop of a goal run: the run's tree carries one
// loop node per loop, and the loop's session nodes hang under it.
// See pipeline.TheoryOfSessionTree and pipeline.TheoryOfGoalMode.
const TypeLoop Type = "loop"

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
// parent — by path copying. It is the shared core of Write and Merge, so
// both carry identical path-copying and re-indexing semantics. See
// TheoryOfTree.
func (t *Tree) writeChild(child *Node) (*Tree, error) {
	parentNode, ok := t.byName[child.Parent]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownParent, child.Parent)
	}

	// Path copying: copy the parent, append the child, then copy every
	// ancestor up to the root, swapping in the copied child pointer.
	newParent := shallowCopy(parentNode)
	newParent.children = append(slices.Clone(parentNode.children), child)

	copied := []*Node{newParent}
	cur := newParent
	curName := child.Parent
	for {
		ancestorName := cur.Parent
		if ancestorName == "" {
			break
		}
		ancestor := t.byName[ancestorName]
		newAncestor := shallowCopy(ancestor)
		newAncestor.children = slices.Clone(ancestor.children)
		for i, c := range newAncestor.children {
			if c.Name == curName {
				newAncestor.children[i] = cur
				break
			}
		}
		copied = append(copied, newAncestor)
		cur = newAncestor
		curName = ancestorName
	}
	newRoot := cur

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
