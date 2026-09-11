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

// Replay rebuilds a tree from an operation stream: writes are applied in
// order, modifies replace the named node's content, and deletes remove
// the named node and its descendants. The stream must be an applied
// history — the operations a sink received — or Replay fails on an
// operation the rebuilt tree cannot apply. The rebuilt tree carries no
// sink: it is a reconstruction, not the live session.
// See TheoryOfOperationLog.
func Replay(ops []Op) (*Tree, error) {
	tr := New()
	for _, op := range ops {
		next, err := tr.applyOp(op)
		if err != nil {
			return nil, err
		}
		tr = next
	}
	return tr, nil
}

// applyOp applies one replayed operation, descending to the tree's own
// mutation methods so replay and live application share one semantics.
// See TheoryOfOperationLog.
func (t *Tree) applyOp(op Op) (*Tree, error) {
	switch op.Kind {
	case OpWrite:
		next, _, err := t.writeOp(WriteOp{
			Parent:     op.Parent,
			Name:       op.Name,
			Type:       op.Type,
			Author:     op.Author,
			Content:    op.Content,
			InsertTime: op.Time,
		})
		return next, err
	case OpModify:
		return t.Modify(op.Name, op.Content)
	case OpDelete:
		return t.Delete(op.Name)
	}
	return nil, fmt.Errorf("unknown operation kind %q", op.Kind)
}
