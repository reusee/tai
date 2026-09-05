package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/tree"
)

// TestWriteInitialTreeNodes verifies the session's initial nodes: the
// system node and the merged initial user input, written under
// the given session root. See TheoryOfSessionTree.
func TestWriteInitialTreeNodes(t *testing.T) {
	state := generators.NewPrompts("sys prompt", []*generators.Content{
		{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("task input")}},
	})
	tr := writeInitialTreeNodes(tree.New(), "root", state)

	sp, ok := tr.Node("system-1")
	if !ok {
		t.Fatal("expected a system node")
	}
	if sp.Type != tree.TypeSystem || sp.Author != tree.AuthorProgram || sp.Content != "sys prompt" {
		t.Fatalf("unexpected system node: %+v", sp)
	}
	in, ok := tr.Node("user-1")
	if !ok {
		t.Fatal("expected a user node")
	}
	if in.Type != tree.TypeUser || in.Author != tree.AuthorUser || in.Content != "task input" {
		t.Fatalf("unexpected user node: %+v", in)
	}

	empty := writeInitialTreeNodes(tree.New(), "root", generators.NewPrompts("", nil))
	if len(empty.Subtree("root")) != 1 {
		t.Fatal("an initial state without prompt and user text yields the root only")
	}
}

// TestRunAttemptNodesUnderAttemptNode verifies the session tree's
// attempt structure: each attempt opens an attempt node under the
// session parent, the attempt's events, response, and summaries hang
// under it, and the session's system and initial user nodes stay the
// attempt nodes' siblings. See TheoryOfSessionTree.
func TestRunAttemptNodesUnderAttemptNode(t *testing.T) {
	withRun(t, func(run Run) {
		result, err := runOnce(run, RunOptions{
			Generator: nil,
			InitialState: generators.NewPrompts("sys prompt", []*generators.Content{
				{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("task input")}},
			}),
			Components: nil,
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return appendPhase("<<龘靐 summary\nDone.\n龘靐\n")
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		tr := result.SessionTree
		attempts := tr.ByType(tree.TypeAttempt)
		if len(attempts) != 1 {
			t.Fatalf("expected one attempt node, got %d", len(attempts))
		}
		attempt := attempts[0]
		if attempt.Parent != "root" {
			t.Fatalf("the attempt node must hang under the session parent, got parent %q", attempt.Parent)
		}
		kinds := map[tree.Type]int{}
		for _, child := range attempt.Children() {
			kinds[child.Type]++
		}
		if kinds[tree.TypeAttemptStart] != 1 || kinds[tree.TypeCompleted] != 1 || kinds[tree.TypeModel] != 1 {
			t.Fatalf("the attempt node must carry the attempt's events and response, got %+v", kinds)
		}
		model, ok := tr.Node("model-1")
		if !ok || model.Parent != attempt.Name {
			t.Fatalf("the response node must hang under the attempt node, got ok=%v node=%+v", ok, model)
		}
		if kids := model.Children(); len(kids) == 0 || kids[0].Type != tree.TypeSummary {
			t.Fatalf("the summary node must hang under the response node, got %+v", kids)
		}
		if node, ok := tr.Node("system-1"); !ok || node.Parent != "root" {
			t.Fatalf("the system node must stay the attempt node's sibling, got ok=%v node=%+v", ok, node)
		}
		if node, ok := tr.Node("user-1"); !ok || node.Parent != "root" {
			t.Fatalf("the initial user node must stay the attempt node's sibling, got ok=%v node=%+v", ok, node)
		}
	})
}

func TestWriteBlockNodesCollectedAndHandled(t *testing.T) {
	base, respName, err := tree.New().WriteAuto("root", "model", tree.TypeModel, tree.AuthorModel, "resp")
	if err != nil {
		t.Fatal(err)
	}

	// Handled blocks (consumed by the BlockHandler during streaming) get
	// auto-named nodes with an applied result child; collected blocks
	// default to the current response as parent. A block node's type is
	// the block kind it records. See TheoryOfSessionTree.
	handled := []blocks.Block{{Kind: "change", Body: "func Foo() {}"}}
	collected := []blocks.Block{{Kind: "shell", Body: "echo hi"}}
	names, _, namingErrs, tr := writeBlockNodes(base, respName, handled, collected)
	if len(namingErrs) != 0 {
		t.Fatalf("unexpected naming errors: %v", namingErrs)
	}
	if len(names) != 1 || names[0] == "" {
		t.Fatalf("expected one collected block name, got %v", names)
	}
	if block, ok := tr.Node(names[0]); !ok || block.Parent != respName || block.Type != tree.Type("shell") {
		t.Fatalf("collected block must hang under the current response: %+v", block)
	}
	handledNode, ok := tr.Node("change-1")
	if !ok || handledNode.Parent != respName {
		t.Fatalf("handled block node missing under %s: %+v", respName, handledNode)
	}
	kids := handledNode.Children()
	if len(kids) != 1 || kids[0].Type != tree.TypeBlockResult || kids[0].Content != "applied" {
		t.Fatalf("handled block must carry an applied result child, got %+v", kids)
	}

	// A done block becomes a done node.
	doneNames, _, _, doneTree := writeBlockNodes(tr, respName, nil, []blocks.Block{{Kind: "done", Body: "goal achieved"}})
	if len(doneNames) != 1 || doneNames[0] == "" {
		t.Fatalf("expected a done node name, got %v", doneNames)
	}
	if node, ok := doneTree.Node(doneNames[0]); !ok || node.Type != tree.TypeDone {
		t.Fatalf("expected a done node, got %+v", node)
	}

	// A new-plan block without the parent header parameter discards the
	// whole batch and records the naming error. The error node is written
	// to the returned tree — the original tree is immutable — so the
	// search targets the returned tree.
	invalid := []blocks.Block{
		{Kind: "new-plan", Boundary: "甲乙", Body: "plan"},
		{Kind: "shell", Body: "echo again"},
	}
	batchNames, _, batchErrs, batchTree := writeBlockNodes(doneTree, respName, nil, invalid)
	if len(batchErrs) == 0 {
		t.Fatal("expected naming errors for the new-plan block without parent")
	}
	if batchNames != nil {
		t.Fatalf("a naming fault must discard the whole batch, got %v", batchNames)
	}
	hasErrorNode := false
	for _, n := range batchTree.Subtree(respName) {
		if n.Type == tree.TypeError && n.Author == tree.AuthorProgram {
			hasErrorNode = true
		}
	}
	if !hasErrorNode {
		t.Fatal("expected an error node under the current response")
	}
}

func TestWriteBlockResultNodesZip(t *testing.T) {
	base, respName, err := tree.New().WriteAuto("root", "model", tree.TypeModel, tree.AuthorModel, "resp")
	if err != nil {
		t.Fatal(err)
	}
	collected := []blocks.Block{{Kind: "shell", Body: "a"}, {Kind: "shell", Body: "b"}}
	names, _, _, tr := writeBlockNodes(base, respName, nil, collected)

	// One part per block: a result child per block node.
	zipped := writeBlockResultNodes(tr, []components.ComponentOutput{{
		Kind:         "shell",
		Blocks:       collected,
		BlockIndexes: []int{0, 1},
		Parts:        []generators.Part{generators.Text("out a"), generators.Text("out b")},
	}}, names)
	b0, _ := zipped.Node(names[0])
	b1, _ := zipped.Node(names[1])
	if kids := b0.Children(); len(kids) != 1 || kids[0].Type != tree.TypeBlockResult || kids[0].Content != "out a" {
		t.Fatalf("expected a result child on block one, got %+v", kids)
	}
	if kids := b1.Children(); len(kids) != 1 || kids[0].Content != "out b" {
		t.Fatalf("expected a result child on block two, got %+v", kids)
	}

	// Any other shape: one shared result node under the first block.
	shared := writeBlockResultNodes(tr, []components.ComponentOutput{{
		Kind:         "shell",
		Blocks:       collected,
		BlockIndexes: []int{0, 1},
		Parts:        []generators.Part{generators.Text("both outputs")},
	}}, names)
	first, _ := shared.Node(names[0])
	second, _ := shared.Node(names[1])
	if kids := first.Children(); len(kids) != 1 || kids[0].Content != "both outputs" {
		t.Fatalf("expected one shared result node on the first block, got %+v", kids)
	}
	if kids := second.Children(); len(kids) != 0 {
		t.Fatalf("the second block must stay childless under a shared result, got %d children", len(kids))
	}
}

// TestWriteBlockNodesInBatchReferences verifies that a collected block
// may reference a parent node an earlier new-plan block of the same
// batch declares: the reference passes validation, the block node's
// write is deferred, and writeDeferredBlockNodes records it under the
// named node once the component has created it. A duplicate declared
// name and a forward reference are naming faults. See
// TheoryOfSessionTree.
func TestWriteBlockNodesInBatchReferences(t *testing.T) {
	base, respName, err := tree.New().WriteAuto("root", "model", tree.TypeModel, tree.AuthorModel, "resp")
	if err != nil {
		t.Fatal(err)
	}
	collected := []blocks.Block{
		{Kind: "new-plan", Attributes: map[string]string{"parent": "root", "name": "plan-1"}, Body: "plan"},
		{Kind: "shell", Attributes: map[string]string{"parent": "plan-1"}, Body: "echo hi"},
	}
	names, deferred, namingErrs, tr := writeBlockNodes(base, respName, nil, collected)
	if len(namingErrs) != 0 {
		t.Fatalf("unexpected naming errors: %v", namingErrs)
	}
	if len(deferred) != 1 || deferred[0] != 1 {
		t.Fatalf("expected the shell block deferred, got %v", deferred)
	}
	if names[1] != "" {
		t.Fatalf("the deferred block's name must stay empty until written, got %q", names[1])
	}
	if names[0] == "" {
		t.Fatal("expected the new-plan block node written immediately")
	}
	// The new-plan component writes the named plan node.
	tr, err = tr.Write("root", "plan-1", tree.TypePlan, tree.AuthorModel, "plan")
	if err != nil {
		t.Fatal(err)
	}
	tr = writeDeferredBlockNodes(tr, respName, collected, names, deferred)
	shellNode, ok := tr.Node(names[1])
	if !ok || shellNode.Parent != "plan-1" {
		t.Fatalf("expected the deferred block under plan-1, got ok=%v node=%+v", ok, shellNode)
	}

	// Two blocks declaring the same name discard the batch.
	dup := []blocks.Block{
		{Kind: "new-plan", Attributes: map[string]string{"parent": "root", "name": "plan-1"}, Body: "a"},
		{Kind: "new-plan", Attributes: map[string]string{"parent": "root", "name": "plan-1"}, Body: "b"},
	}
	_, _, dupErrs, _ := writeBlockNodes(base, respName, nil, dup)
	if len(dupErrs) == 0 {
		t.Fatal("expected a naming error for the duplicate declared name")
	}

	// A reference to a name declared later in the batch is a naming
	// fault, because the components write in block order.
	fwd := []blocks.Block{
		{Kind: "shell", Attributes: map[string]string{"parent": "plan-2"}, Body: "echo"},
		{Kind: "new-plan", Attributes: map[string]string{"parent": "root", "name": "plan-2"}, Body: "p"},
	}
	_, _, fwdErrs, _ := writeBlockNodes(base, respName, nil, fwd)
	if len(fwdErrs) == 0 {
		t.Fatal("expected a naming error for the forward reference")
	}
}

func TestRunGoalCarriesOneSessionTree(t *testing.T) {
	responses := []string{"loop one", "loop two"}
	calls := 0
	res := RunGoal(context.Background(), GoalOptions{
		Output: &strings.Builder{},
		Generate: func(ctx context.Context, _ int, _ GoalFeedback, _ GoalLoopSummaries, _ string, continuation SessionTreeContinuation) (Result, []AttemptStat, error) {
			calls++
			if continuation.Tree == nil || continuation.Parent == "" {
				t.Fatalf("expected a continuation, got %+v", continuation)
			}
			if want := fmt.Sprintf("loop-%d", calls); continuation.Parent != want {
				t.Fatalf("expected %q as the continuation parent, got %q", want, continuation.Parent)
			}
			// The loop writes its response node under the continuation
			// parent and returns the evolved tree.
			next, _, err := continuation.Tree.WriteAuto(continuation.Parent, "model", tree.TypeModel, tree.AuthorModel, responses[calls-1])
			if err != nil {
				t.Fatal(err)
			}
			if calls == 1 {
				return Result{SessionTree: next}, nil, nil
			}
			// The second loop emits the done block without changes: the
			// run's only exit. See TheoryOfGoalMode.
			result := doneResult()
			result.SessionTree = next
			return result, nil, nil
		},
		Review: noopReview,
	})
	if calls != 2 {
		t.Fatalf("ran %d loops, want 2", calls)
	}
	if res.Tree == nil {
		t.Fatal("expected the goal result to carry the run's one tree")
	}
	// Each loop is a loop-N child of the root, and each loop's session
	// nodes hang under its loop node.
	loop1, ok := res.Tree.Node("loop-1")
	if !ok || loop1.Type != tree.TypeLoop {
		t.Fatalf("expected a loop-1 node, got ok=%v node=%+v", ok, loop1)
	}
	// The loop node carries its label so the display front-end's Tree
	// tab distinguishes the loops' collapsed rows.
	if loop1.Content != "goal loop 1" {
		t.Fatalf("expected the loop node to carry its label, got %q", loop1.Content)
	}
	resp1, ok := res.Tree.Node("model-1")
	if !ok || resp1.Parent != "loop-1" || resp1.Content != "loop one" {
		t.Fatalf("expected loop one's response node under loop-1, got ok=%v node=%+v", ok, resp1)
	}
	resp2, ok := res.Tree.Node("model-2")
	if !ok || resp2.Parent != "loop-2" || resp2.Content != "loop two" {
		t.Fatalf("expected loop two's response node under loop-2, got ok=%v node=%+v", ok, resp2)
	}
}

// TestRunErrorNodesRecorded verifies that an attempt's unprocessable
// output joins the tree as error nodes under the current response,
// visible in the feedback's tree outline: a malformed block the parser
// could not parse and a well-formed block of an unavailable kind each
// produce one. The nodes are the record; the correction budget governs
// only the feedback. Phases flush the state, so the unclosed block is
// collected as a parse error at Flush. See TheoryOfSessionTree.
func TestRunErrorNodesRecorded(t *testing.T) {
	withRun(t, func(run Run) {
		t.Run("malformed block", func(t *testing.T) {
			callCount := 0
			phaseBuilder := func(g generators.Generator) generators.Phase {
				callCount++
				if callCount == 1 {
					return appendPhaseWithFlush("<<貞觀 summary\nRound 1.\n貞觀\n<<龘靐 shell\necho hi\n")
				}
				return appendPhaseWithFlush("<<貞觀 summary\nDone.\n貞觀\n")
			}
			result, err := runOnce(run, RunOptions{
				Generator:    nil,
				InitialState: generators.NewPrompts("", nil),
				PhaseBuilder: phaseBuilder,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var userText string
			for c := range result.FinalState.Contents() {
				if c.Role == generators.RoleUser {
					for _, p := range c.Parts {
						if text, ok := p.(generators.Text); ok {
							userText += string(text)
						}
					}
				}
			}
			for _, want := range []string{
				"error-1 [error/program]",
				"malformed block",
			} {
				if !strings.Contains(userText, want) {
					t.Fatalf("expected %q in the feedback's tree outline, got: %s", want, userText)
				}
			}
		})
		t.Run("unavailable kind", func(t *testing.T) {
			callCount := 0
			phaseBuilder := func(g generators.Generator) generators.Phase {
				callCount++
				if callCount == 1 {
					return appendPhaseWithFlush("<<永樂 mystery\nbody\n永樂\n<<崇禎 summary\nDone.\n崇禎\n")
				}
				return appendPhaseWithFlush("<<崇禎 summary\nDone.\n崇禎\n")
			}
			comps := components.ComponentSet{
				{Kind: "summary", PromptSection: "summary prompt"},
			}
			result, err := runOnce(run, RunOptions{
				Generator:       nil,
				InitialState:    generators.NewPrompts("", nil),
				Components:      comps,
				PhaseBuilder:    phaseBuilder,
				KnownBlockKinds: comps.KnownKinds(),
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var userText string
			for c := range result.FinalState.Contents() {
				if c.Role == generators.RoleUser {
					for _, p := range c.Parts {
						if text, ok := p.(generators.Text); ok {
							userText += string(text)
						}
					}
				}
			}
			for _, want := range []string{
				"error-1 [error/program]",
				"unavailable kind",
			} {
				if !strings.Contains(userText, want) {
					t.Fatalf("expected %q in the feedback's tree outline, got: %s", want, userText)
				}
			}
		})
	})
}

func TestTreeOutlinePart(t *testing.T) {
	got := string(treeOutlinePart(tree.New(), "root"))
	if !strings.HasPrefix(got, "\n[Session tree]") {
		t.Fatalf("the outline part must start on its own line, got %q", got)
	}
	if !strings.Contains(got, "root [root/]") {
		t.Fatalf("unexpected outline part: %q", got)
	}
}

// assertUserTextContains joins the user-role Text parts of the result's
// final state and asserts every want substring is present.
func assertUserTextContains(t *testing.T, result Result, wants ...string) {
	t.Helper()
	var userText string
	for c := range result.FinalState.Contents() {
		if c.Role != generators.RoleUser {
			continue
		}
		for _, p := range c.Parts {
			if text, ok := p.(generators.Text); ok {
				userText += string(text)
			}
		}
	}
	for _, want := range wants {
		if !strings.Contains(userText, want) {
			t.Fatalf("expected %q in the user content, got: %s", want, userText)
		}
	}
}

// TestResultCarriesSessionTree verifies that the run's result carries
// the final session tree, so callers outside the loop can extract
// subtree projections from it. See TheoryOfSessionTree.
func TestResultCarriesSessionTree(t *testing.T) {
	withRun(t, func(run Run) {
		result, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   nil,
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return appendPhase("<<龘靐 summary\nDone.\n龘靐\n")
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.SessionTree == nil {
			t.Fatal("expected the result to carry the session tree")
		}
		if _, ok := result.SessionTree.Node("model-1"); !ok {
			t.Fatal("expected the attempt's response node in the result's tree")
		}
	})
}

// TestRunRetryFeedbackCarriesTreeOutline verifies that the retry
// feedback of a truncated or errored attempt ends with the session
// tree outline: the retry is a round-triggering feedback. See
// TheoryOfSessionTree.
func TestRunRetryFeedbackCarriesTreeOutline(t *testing.T) {
	withRun(t, func(run Run) {
		t.Run("missing summary", func(t *testing.T) {
			callCount := 0
			phaseBuilder := func(g generators.Generator) generators.Phase {
				callCount++
				if callCount == 1 {
					return appendPhase("incomplete output without summary")
				}
				return appendPhase("<<龘靐 summary\nDone.\n龘靐\n")
			}
			result, err := runOnce(run, RunOptions{
				Generator: nil,
				InitialState: generators.NewPrompts("", []*generators.Content{
					{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("task input")}},
				}),
				Components:               nil,
				PhaseBuilder:             phaseBuilder,
				RetryOnMissingCompletion: true,
				MaxRetries:               3,
				Handoff: func(text string) (*Handoff, error) {
					return &Handoff{Summary: "summary", Prompt: "retry prompt"}, nil
				},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertUserTextContains(t, result, "[Session tree]", "user-1 [user/user]")
		})
		t.Run("error retry", func(t *testing.T) {
			callCount := 0
			phaseBuilder := func(g generators.Generator) generators.Phase {
				callCount++
				if callCount == 1 {
					return appendThenErrorPhase("partial output", errors.New("boom"))
				}
				return appendPhase("<<龘靐 summary\nDone.\n龘靐\n")
			}
			result, err := runOnce(run, RunOptions{
				Generator: nil,
				InitialState: generators.NewPrompts("", []*generators.Content{
					{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("task input")}},
				}),
				Components:   nil,
				PhaseBuilder: phaseBuilder,
				RetryOnError: true,
				MaxRetries:   3,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertUserTextContains(t, result, "[Session tree]", "user-1 [user/user]")
		})
	})
}

// TestRunHandoffInputCarriesTreeOutline verifies that the handoff input
// prefixes the incomplete output with the session tree outline, so the
// handoff summary carries the session's structure into the retry
// attempt. See TheoryOfSessionTree.
func TestRunHandoffInputCarriesTreeOutline(t *testing.T) {
	withRun(t, func(run Run) {
		var capturedInput string
		phaseBuilder := func(g generators.Generator) generators.Phase {
			return appendPhase("incomplete output without summary")
		}
		_, err := runOnce(run, RunOptions{
			Generator: nil,
			InitialState: generators.NewPrompts("", []*generators.Content{
				{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("task input")}},
			}),
			Components:               nil,
			PhaseBuilder:             phaseBuilder,
			RetryOnMissingCompletion: true,
			MaxRetries:               1,
			Handoff: func(text string) (*Handoff, error) {
				capturedInput = text
				return &Handoff{Summary: "summary", Prompt: "retry prompt"}, nil
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(capturedInput, "[Session tree]") {
			t.Fatalf("expected the tree outline in the handoff input, got: %s", capturedInput)
		}
		if !strings.Contains(capturedInput, "incomplete output without summary") {
			t.Fatalf("expected the incomplete output in the handoff input, got: %s", capturedInput)
		}
	})
}

// TestHandoffInputPrunesExecutionNodes verifies that the handoff input's
// tree outline is the decision-level projection: block and block-result
// nodes are pruned, while the user, model, and summary nodes stay.
// A continued session renders from its loop node, so other loops'
// content stays out of the handoff outline. See TheoryOfSessionTree and
// tree.TheoryOfSubtree.
func TestHandoffInputPrunesExecutionNodes(t *testing.T) {
	tr, err := tree.New().WriteAll(
		tree.WriteOp{Parent: "root", Name: "user-1", Type: tree.TypeUser, Author: tree.AuthorUser, Content: "task"},
		tree.WriteOp{Parent: "root", Name: "model-1", Type: tree.TypeModel, Author: tree.AuthorModel, Content: "resp"},
		tree.WriteOp{Parent: "model-1", Name: "summary-1", Type: tree.TypeSummary, Author: tree.AuthorModel, Content: "sum"},
		tree.WriteOp{Parent: "model-1", Name: "block-1", Type: tree.Type("change"), Author: tree.AuthorModel, Content: "change"},
		tree.WriteOp{Parent: "block-1", Name: "result-1", Type: tree.TypeBlockResult, Author: tree.AuthorProgram, Content: "applied"},
	)
	if err != nil {
		t.Fatal(err)
	}
	out := handoffInput("incomplete output", tr, "root")
	for _, want := range []string{
		"user-1 [user/user]",
		"model-1 [model/model]",
		"summary-1 [summary/model]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in the handoff outline, got: %s", want, out)
		}
	}
	for _, gone := range []string{"block-1", "result-1"} {
		if strings.Contains(out, gone) {
			t.Fatalf("execution node %q must be pruned from the handoff outline, got: %s", gone, out)
		}
	}
	if !strings.Contains(out, "incomplete output") {
		t.Fatal("the incomplete output must follow the outline")
	}

	// A continued session renders from its loop node: the loop's own
	// nodes are visible and the other loops' content stays out.
	oneRun, err := tr.WriteAll(
		tree.WriteOp{Parent: "root", Name: "loop-1", Type: tree.TypeLoop, Author: tree.AuthorProgram},
		tree.WriteOp{Parent: "root", Name: "loop-2", Type: tree.TypeLoop, Author: tree.AuthorProgram},
		tree.WriteOp{Parent: "loop-1", Name: "model-2", Type: tree.TypeModel, Author: tree.AuthorModel, Content: "loop one"},
		tree.WriteOp{Parent: "loop-2", Name: "model-3", Type: tree.TypeModel, Author: tree.AuthorModel, Content: "loop two"},
	)
	if err != nil {
		t.Fatal(err)
	}
	out = handoffInput("incomplete output", oneRun, "loop-2")
	if !strings.Contains(out, "model-3 [model/model] loop two") {
		t.Fatalf("expected the session's own response node in the handoff outline, got: %s", out)
	}
	if strings.Contains(out, "loop one") || strings.Contains(out, "model-2") {
		t.Fatalf("another loop's content must stay out of the handoff outline, got: %s", out)
	}
	if got := handoffInput("", tr, "root"); got != "" {
		t.Fatalf("an empty output must yield an empty input, got %q", got)
	}
	if got := handoffInput("text", nil, "root"); got != "text" {
		t.Fatalf("a nil tree must yield the bare output, got %q", got)
	}
}

func TestRecordIdleUserInput(t *testing.T) {
	ls := &loopState{sessionTree: tree.New()}
	state := generators.NewPrompts("", []*generators.Content{
		{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("typed line")}},
	})
	ls.recordIdleUserInput(state, 0)
	node, ok := ls.sessionTree.Node("user-1")
	if !ok {
		t.Fatal("expected a user node recorded from the idle handler's delta")
	}
	if node.Author != tree.AuthorUser || node.Content != "typed line" {
		t.Fatalf("unexpected user node: %+v", node)
	}

	// An empty delta records nothing.
	ls.recordIdleUserInput(state, 1)
	if _, ok := ls.sessionTree.Node("user-2"); ok {
		t.Fatal("an empty delta must not record a user node")
	}
}

func TestRunFeedbackCarriesTreeOutline(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) generators.Phase {
			callCount++
			if callCount == 1 {
				return appendPhase("<<龘靐 shell\necho hello\n龘靐\n<<贞观 summary\nRound 1 done.\n贞观\n")
			}
			return appendPhase("<<贞观 summary\nDone.\n贞观\n")
		}
		comps := components.ComponentSet{
			{
				Kind: "shell",
				Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
					return components.ProcessResult{
						Parts: []generators.Part{generators.Text("shell output")},
					}
				},
			},
		}
		result, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   comps,
			PhaseBuilder: phaseBuilder,
			HTTPClient:   nets.HTTPClient{},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount != 2 {
			t.Fatalf("expected 2 generations, got %d", callCount)
		}
		var userText string
		for c := range result.FinalState.Contents() {
			if c.Role == generators.RoleUser {
				for _, p := range c.Parts {
					if text, ok := p.(generators.Text); ok {
						userText += string(text)
					}
				}
			}
		}
		// The round-triggering feedback closes with the session tree
		// outline: the successful attempt's model, summary, block,
		// and result nodes are all visible in it. Substring matching,
		// because the outline part merges into the same user content
		// as the component's parts. See TheoryOfSessionTree.
		for _, want := range []string{
			"[Session tree]",
			"model-1 [model/model]",
			"summary-1 [summary/model]",
			"shell-1 [shell/model]",
			"result-1 [block-result/program]",
		} {
			if !strings.Contains(userText, want) {
				t.Fatalf("expected %q in the feedback's tree outline, got: %s", want, userText)
			}
		}
	})
}

// TestRunInertProcessAttachesBlockResult verifies that a component
// whose Process function returns an empty ProcessResult — the shape of
// the ai command's memory component, whose actual profile update runs
// in the OnAttemptSuccess hook — still records a ComponentOutput, so
// the block node carries a block-result child instead of reading as
// unprocessed, and the inert processing triggers no new generation.
// See TheoryOfSessionTree.
func TestRunInertProcessAttachesBlockResult(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) generators.Phase {
			callCount++
			if callCount > 1 {
				t.Fatal("an inert component processing must not trigger a new generation")
			}
			return appendPhase("<<萬曆 memory\n<memory>\n  <memory-item>likes go</memory-item>\n</memory>\n萬曆\n<<天祐 summary\nDone.\n天祐\n")
		}
		comps := components.ComponentSet{
			{
				Kind: "memory",
				Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
					return components.ProcessResult{}
				},
			},
		}
		result, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   comps,
			PhaseBuilder: phaseBuilder,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount != 1 {
			t.Fatalf("expected 1 generation, got %d", callCount)
		}
		if result.SessionTree == nil {
			t.Fatal("expected the result to carry the session tree")
		}
		var memNode *tree.Node
		for _, n := range result.SessionTree.ByType(tree.Type("memory")) {
			if strings.Contains(n.Content, "memory-item") {
				memNode = n
			}
		}
		if memNode == nil {
			t.Fatal("expected a memory block node in the session tree")
		}
		kids := memNode.Children()
		if len(kids) != 1 || kids[0].Type != tree.TypeBlockResult {
			t.Fatalf("the processed block node must carry a block-result child, got %+v", kids)
		}
	})
}
