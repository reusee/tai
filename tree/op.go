package tree

import (
	"fmt"
	"time"
)

const TheoryOfOperationLog = `
tree operation log theory:
- Every mutation of the session tree is an operation, and recording the
  operations records the session. A tree version carries an op sink; the
  sink is called as each operation is applied, so a recorder observes the
  session's tree as it grows instead of a later snapshot.
- The operation set is minimal: every mutation decomposes into a write (a
  node appended under an existing parent), a modify (a node's content
  replaced), or a delete (a node and its descendants removed). Merge is
  writes of the grafted nodes, Abort is a write of an abort child, and
  WriteAll is a sequence of writes, so a consumer reconstructs the tree
  from writes, modifies, and deletes alone.
- A write op carries the appended node's full identity — parent, name,
  type, author, content, and insert time — so the recorded stream is
  self-contained: a consumer renders the session or rebuilds the tree
  from the ops without the process that produced them.
- The sink is attached with WithOpSink and shared by every version
  derived from the result, so one attachment covers a session's writes,
  including the versions a continued run receives through its
  continuation. A tree without a sink records nothing; the receiver and
  the snapshots taken before the attachment stay unchanged.
- Only applied operations report: a write to an unknown parent, a modify
  of a missing node, a failed batch, and a conflicting merge all return
  an error and emit nothing, so the recorded stream is exactly the
  applied history.
- Replay reconstructs in place: a reconstruction has no earlier version
  to preserve, so it skips the live path copying and index cloning. The
  live path's per-operation index copy is a pure-value cost that a
  reconstruction must not carry — a recorded session holds every
  operation of a whole run, and a quadratic reconstruction stalls the
  consumer long enough that opening a long record reads as nothing
  happening. Replay keeps the live semantics: the same validation, the
  same duplicate and unknown-node failures, and the same insert-time
  rule.
- A projection (Extract) is a view, not the session: it carries no sink.
`

// OpKind classifies one tree operation. See TheoryOfOperationLog.
type OpKind string

const (
	// OpWrite appends one node under an existing parent.
	OpWrite OpKind = "write"
	// OpModify replaces one node's content.
	OpModify OpKind = "modify"
	// OpDelete removes one node and its descendants.
	OpDelete OpKind = "delete"
)

// Op is one applied tree operation: the mutation kind and the affected
// node's identity. See TheoryOfOperationLog.
type Op struct {
	Kind    OpKind
	Parent  string
	Name    string
	Type    Type
	Author  Author
	Content string
	// Time is the operation's timestamp: for a write, the appended
	// node's insert time — a merge preserves the grafted node's
	// original time — and for a modify or delete, the moment the
	// mutation was applied.
	Time time.Time
}

// OpSink receives every operation applied to a tree that carries it.
// See TheoryOfOperationLog.
type OpSink func(Op)

// WithOpSink returns a tree that reports every subsequent operation to
// sink, as the operation is applied. The sink is shared by every version
// derived from the result, so one attachment covers a session's writes;
// the receiver and the snapshots taken before the attachment are
// unchanged and report nothing. See TheoryOfOperationLog.
func (t *Tree) WithOpSink(sink OpSink) *Tree {
	next := *t
	next.sink = sink
	return &next
}

// withoutSink returns a tree sharing the receiver's structure with no op
// sink: the staging area of a multi-operation mutation that must report
// nothing unless the whole mutation succeeds. See TheoryOfOperationLog.
func (t *Tree) withoutSink() *Tree {
	next := *t
	next.sink = nil
	return &next
}

// emitOp reports one operation to the tree's sink, when one is attached.
// See TheoryOfOperationLog.
func (t *Tree) emitOp(op Op) {
	if t.sink == nil {
		return
	}
	t.sink(op)
}

// opForNode returns the write operation that attached child.
// See TheoryOfOperationLog.
func opForNode(child *Node) Op {
	return Op{
		Kind:    OpWrite,
		Parent:  child.Parent,
		Name:    child.Name,
		Type:    child.Type,
		Author:  child.Author,
		Content: child.Content,
		Time:    child.InsertTime,
	}
}

// replayBuilder applies an operation stream in place: the reconstruction
// target of Replay. Replay differs from live application in cost only —
// the live tree is immutable, so every mutation copies the path to the
// root and the whole name index, while a reconstruction has no earlier
// version to preserve. The semantics are identical: the same validation,
// the same duplicate and unknown-node failures, and the same insert-time
// rule. See TheoryOfOperationLog.
type replayBuilder struct {
	root   *Node
	byName map[string]*Node
}

// apply applies one recorded operation in place. The switch mirrors the
// operation set exactly, so a stream the live tree accepted reconstructs
// without error and a stream it would have rejected fails here too.
// See TheoryOfOperationLog.
func (b *replayBuilder) apply(op Op) error {
	switch op.Kind {
	case OpWrite:
		if err := validateWrite(op.Name, op.Author); err != nil {
			return err
		}
		if _, exists := b.byName[op.Name]; exists {
			return fmt.Errorf("%w: %q", ErrDuplicateName, op.Name)
		}
		parent, ok := b.byName[op.Parent]
		if !ok {
			return fmt.Errorf("%w: %q", ErrUnknownParent, op.Parent)
		}
		insertTime := op.Time
		if insertTime.IsZero() {
			insertTime = time.Now()
		}
		parent.children = append(parent.children, &Node{
			Name:       op.Name,
			Parent:     op.Parent,
			Type:       op.Type,
			Author:     op.Author,
			Content:    op.Content,
			InsertTime: insertTime,
		})
		b.byName[op.Name] = parent.children[len(parent.children)-1]
		return nil
	case OpModify:
		n, ok := b.byName[op.Name]
		if !ok {
			return fmt.Errorf("%w: %q", ErrUnknownNode, op.Name)
		}
		if n.Name == b.root.Name {
			return fmt.Errorf("%w: cannot modify the root node", ErrBadName)
		}
		n.Content = op.Content
		return nil
	case OpDelete:
		n, ok := b.byName[op.Name]
		if !ok {
			return fmt.Errorf("%w: %q", ErrUnknownNode, op.Name)
		}
		if n.Name == b.root.Name {
			return fmt.Errorf("%w: cannot delete the root node", ErrBadName)
		}
		parent, ok := b.byName[n.Parent]
		if !ok {
			return fmt.Errorf("%w: %q", ErrUnknownParent, n.Parent)
		}
		kept := parent.children[:0]
		for _, c := range parent.children {
			if c.Name != n.Name {
				kept = append(kept, c)
			}
		}
		parent.children = kept
		var drop func(*Node)
		drop = func(x *Node) {
			delete(b.byName, x.Name)
			for _, c := range x.children {
				drop(c)
			}
		}
		drop(n)
		return nil
	}
	return fmt.Errorf("unknown operation kind %q", op.Kind)
}

// Replay rebuilds a tree from an operation stream: writes are applied in
// order, modifies replace the named node's content, and deletes remove
// the named node and its descendants. The stream must be an applied
// history — the operations a sink received — or Replay fails on an
// operation the rebuilt tree cannot apply. The rebuilt tree carries no
// sink: it is a reconstruction, not the live session.
//
// The reconstruction applies the stream in place rather than through the
// live immutable path: a recorded session carries every operation of a
// whole run, and copying the path and the name index per operation would
// make the reconstruction quadratic in the record's size. The result is
// the tree the live sequence produced, because the applied history holds
// exactly the operations that sequence applied. See TheoryOfOperationLog.
func Replay(ops []Op) (*Tree, error) {
	tr := New()
	b := &replayBuilder{root: tr.root, byName: tr.byName}
	for _, op := range ops {
		if err := b.apply(op); err != nil {
			return nil, err
		}
	}
	return tr, nil
}
