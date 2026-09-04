package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/tree"
)

func TestBuildInitialTreeStructure(t *testing.T) {
	state := generators.NewPrompts("sys prompt", []*generators.Content{
		{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("task input")}},
	})
	tr := buildInitialTree(state)

	sp, ok := tr.Node("system-prompt-1")
	if !ok {
		t.Fatal("expected a system-prompt node")
	}
	if sp.Type != tree.TypeSystemPrompt || sp.Author != tree.AuthorProgram || sp.Content != "sys prompt" {
		t.Fatalf("unexpected system-prompt node: %+v", sp)
	}
	in, ok := tr.Node("input-1")
	if !ok {
		t.Fatal("expected an input node")
	}
	if in.Type != tree.TypeInput || in.Author != tree.AuthorUser || in.Content != "task input" {
		t.Fatalf("unexpected input node: %+v", in)
	}

	empty := buildInitialTree(generators.NewPrompts("", nil))
	if len(empty.Subtree("root")) != 1 {
		t.Fatal("an initial state without prompt and user text yields the root only")
	}
}

func TestWriteBlockNodesCollectedAndHandled(t *testing.T) {
	base, respName, err := tree.New().WriteAuto("root", "response", tree.TypeResponse, tree.AuthorModel, "resp")
	if err != nil {
		t.Fatal(err)
	}

	// Handled blocks (consumed by the BlockHandler during streaming) get
	// auto-named nodes with an applied result child; collected blocks
	// default to the current response as parent. See TheoryOfSessionTree.
	handled := []blocks.Block{{Kind: "change", Body: "func Foo() {}"}}
	collected := []blocks.Block{{Kind: "shell", Body: "echo hi"}}
	names, namingErrs, tr := writeBlockNodes(base, respName, handled, collected)
	if len(namingErrs) != 0 {
		t.Fatalf("unexpected naming errors: %v", namingErrs)
	}
	if len(names) != 1 || names[0] == "" {
		t.Fatalf("expected one collected block name, got %v", names)
	}
	if block, ok := tr.Node(names[0]); !ok || block.Parent != respName || block.Type != tree.TypeBlock {
		t.Fatalf("collected block must hang under the current response: %+v", block)
	}
	handledNode, ok := tr.Node("block-1")
	if !ok || handledNode.Parent != respName {
		t.Fatalf("handled block node missing under %s: %+v", respName, handledNode)
	}
	kids := handledNode.Children()
	if len(kids) != 1 || kids[0].Type != tree.TypeBlockResult || kids[0].Content != "applied" {
		t.Fatalf("handled block must carry an applied result child, got %+v", kids)
	}

	// A done block becomes a done node.
	doneNames, _, doneTree := writeBlockNodes(tr, respName, nil, []blocks.Block{{Kind: "done", Body: "goal achieved"}})
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
	batchNames, batchErrs, batchTree := writeBlockNodes(doneTree, respName, nil, invalid)
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
	base, respName, err := tree.New().WriteAuto("root", "response", tree.TypeResponse, tree.AuthorModel, "resp")
	if err != nil {
		t.Fatal(err)
	}
	collected := []blocks.Block{{Kind: "shell", Body: "a"}, {Kind: "shell", Body: "b"}}
	names, _, tr := writeBlockNodes(base, respName, nil, collected)

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

func TestTreeOutlinePart(t *testing.T) {
	got := string(treeOutlinePart(tree.New()))
	if !strings.Contains(got, "[Session tree]") || !strings.Contains(got, "root [root/]") {
		t.Fatalf("unexpected outline part: %q", got)
	}
}

func TestRecordIdleUserInput(t *testing.T) {
	ls := &loopState{sessionTree: tree.New()}
	state := generators.NewPrompts("", []*generators.Content{
		{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("typed line")}},
	})
	ls.recordIdleUserInput(state, 0)
	node, ok := ls.sessionTree.Node("input-1")
	if !ok {
		t.Fatal("expected an input node recorded from the idle handler's delta")
	}
	if node.Author != tree.AuthorUser || node.Content != "typed line" {
		t.Fatalf("unexpected input node: %+v", node)
	}

	// An empty delta records nothing.
	ls.recordIdleUserInput(state, 1)
	if _, ok := ls.sessionTree.Node("input-2"); ok {
		t.Fatal("an empty delta must not record an input node")
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
		// outline: the successful attempt's response, summary, block,
		// and result nodes are all visible in it. Substring matching,
		// because the outline part merges into the same user content
		// as the component's parts. See TheoryOfSessionTree.
		for _, want := range []string{
			"[Session tree]",
			"response-1 [response/model]",
			"summary-1 [summary/model]",
			"block-1 [block/model]",
			"result-1 [block-result/program]",
		} {
			if !strings.Contains(userText, want) {
				t.Fatalf("expected %q in the feedback's tree outline, got: %s", want, userText)
			}
		}
	})
}
