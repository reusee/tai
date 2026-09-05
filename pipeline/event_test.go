package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/tree"
)

// TestRunRecordsEventNodes verifies the loop's tree contract: every
// notable occurrence of a run is recorded as an event node in the
// session tree, and every yield carries the full tree. The run's first
// attempt misses the summary block (truncation retry with a handoff)
// and its second attempt completes with a summary and a usage part; the
// test asserts the ordered event nodes under the session root, the
// attempt attribution carried in the node contents, and that the
// handoff node carries the handoff summary as its multi-line body. See
// TheoryOfLoopEvents.
func TestRunRecordsEventNodes(t *testing.T) {
	withRun(t, func(run Run) {
		usage := generators.Usage{}
		usage.Prompt.TokenCount = 42
		usage.Candidates.TokenCount = 7

		callCount := 0
		opts := RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   nil,
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				callCount++
				if callCount == 1 {
					return appendPhase("incomplete output without summary")
				}
				return appendPhaseWithUsage("<<龘靐 summary\nDone.\n龘靐\n", usage)
			},
			RetryOnMissingCompletion: true,
			MaxRetries:               3,
			Handoff: func(text string) (*Handoff, error) {
				return &Handoff{Summary: "truncated summary", Prompt: "retry prompt"}, nil
			},
		}

		var result Result
		var lastTree *tree.Tree
		var terminalErr error
		for tr, err := range run(context.Background(), opts, &result) {
			if err != nil {
				terminalErr = err
			}
			lastTree = tr
		}
		if terminalErr != nil {
			t.Fatalf("unexpected terminal error: %v", terminalErr)
		}
		if callCount != 2 {
			t.Fatalf("expected 2 attempts (one retry), got %d", callCount)
		}
		if lastTree == nil {
			t.Fatal("expected the run to yield trees")
		}

		nodes := lastTree.ByCategory(tree.CategoryEvent)
		wantPrefixes := []string{
			"attempt-start", "truncated", "handoff-start", "handoff",
			"attempt-start", "usage", "completed",
		}
		if len(nodes) != len(wantPrefixes) {
			t.Fatalf("expected %d event nodes, got %v", len(wantPrefixes), nodeNames(nodes))
		}
		for i, n := range nodes {
			if !strings.HasPrefix(n.Name, wantPrefixes[i]) {
				t.Fatalf("event node %d: expected prefix %q, got %q", i, wantPrefixes[i], n.Name)
			}
		}

		// The truncated node precedes the handoff nodes and does not
		// repeat the handoff summary.
		if strings.Contains(nodes[1].Content, "truncated summary") {
			t.Fatal("the truncated node must not repeat the handoff summary")
		}
		if !strings.Contains(nodes[3].Content, "truncated summary") {
			t.Fatalf("expected the handoff summary on the handoff node, got %q", nodes[3].Content)
		}
		// The second attempt's usage node carries the counters.
		if got := nodes[5].Content; !strings.Contains(got, "prompt 42") || !strings.Contains(got, "completion 7") {
			t.Fatalf("unexpected usage node: %q", got)
		}
		// The completed node carries the attempt's summary body.
		if !strings.Contains(nodes[6].Content, "Done.") {
			t.Fatalf("unexpected completed node: %q", nodes[6].Content)
		}
		// The attempt-start nodes carry the session-wide attempt numbers.
		if got := nodes[0].Content; !strings.Contains(got, "attempt 1") {
			t.Fatalf("unexpected first attempt-start: %q", got)
		}
		if got := nodes[4].Content; !strings.Contains(got, "attempt 2") {
			t.Fatalf("unexpected second attempt-start: %q", got)
		}
	})
}

// nodeNames collects the names of the given nodes.
func nodeNames(nodes []*tree.Node) []string {
	var names []string
	for _, n := range nodes {
		names = append(names, n.Name)
	}
	return names
}

// requestEventGenerator is a minimal Generator whose Spec feeds the
// request-node assertions of TestRunRequestNodeContent; its Generate is
// never called because the test drives its own phase.
type requestEventGenerator struct {
	spec generators.Spec
}

func (g requestEventGenerator) Spec() generators.Spec {
	return g.spec
}

func (g requestEventGenerator) CountTokens(string) (int, error) {
	return 0, nil
}

func (g requestEventGenerator) Generate(ctx context.Context, state generators.State, options *generators.GenerateOptions) (generators.State, error) {
	return state, nil
}

// TestRunRequestNodeContent verifies that each attempt records a
// request node whose content describes the actual generation parameters
// resolved from the generator spec — the flag overrides stay unset in
// this scope, so the spec values are the effective ones. See
// TheoryOfLoopEvents.
func TestRunRequestNodeContent(t *testing.T) {
	withRun(t, func(run Run) {
		temperature := float32(0.5)
		maxTokens := 4096
		generator := requestEventGenerator{
			spec: generators.Spec{
				Name:              "test",
				Model:             "model-a",
				Family:            "family-a",
				Temperature:       &temperature,
				ReasoningEffort:   "low",
				MaxGenerateTokens: &maxTokens,
				ContextTokens:     100000,
			},
		}
		opts := RunOptions{
			Generator:    generator,
			InitialState: generators.NewPrompts("", nil),
			Components:   nil,
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return func(ctx context.Context, state generators.State) (generators.Phase, generators.State, error) {
					newState, err := state.AppendContent(&generators.Content{
						Role: generators.RoleLog,
						Parts: []generators.Part{
							generators.FinishReason("stop"),
							generators.Text("<<齉爩 summary\n- done\n齉爩\n"),
						},
					})
					if err != nil {
						return nil, state, err
					}
					return nil, newState, nil
				}
			},
		}

		var result Result
		var lastTree *tree.Tree
		var terminalErr error
		for tr, err := range run(context.Background(), opts, &result) {
			if err != nil {
				terminalErr = err
			}
			lastTree = tr
		}
		if terminalErr != nil {
			t.Fatalf("unexpected terminal error: %v", terminalErr)
		}
		var requestNode *tree.Node
		for _, n := range lastTree.ByCategory(tree.CategoryEvent) {
			if strings.HasPrefix(n.Name, "request") {
				requestNode = n
			}
		}
		if requestNode == nil {
			t.Fatal("expected a request event node")
		}
		for _, want := range []string{
			"model model-a",
			"family family-a",
			"temperature 0.5",
			"effort low",
			"max tokens 4096",
			"context 100000",
		} {
			if !strings.Contains(requestNode.Content, want) {
				t.Fatalf("request node content %q missing %q", requestNode.Content, want)
			}
		}
		if strings.Contains(requestNode.Content, "thinking tokens") {
			t.Fatalf("request node content %q must omit unset thinking tokens", requestNode.Content)
		}
	})
}

// TestDescribeRequest verifies the effective-value resolution of the
// request description: the resolved spec path leads the detail, the
// temperature and effort flags override the spec fields, unset values
// are omitted, and the model identity and token limits come from the
// spec. See TheoryOfLoopEvents.
func TestDescribeRequest(t *testing.T) {
	specTemperature := float32(0.2)
	maxTokens := 8192
	spec := generators.Spec{
		Name:              "provider/model-b",
		Model:             "model-b",
		Temperature:       &specTemperature,
		ReasoningEffort:   "low",
		MaxGenerateTokens: &maxTokens,
	}
	detail := describeRequest(spec, generators.TemperatureFlag{}, generators.EffortFlag(""))
	for _, want := range []string{
		"spec provider/model-b",
		"model model-b",
		"temperature 0.2",
		"effort low",
		"max tokens 8192",
	} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail %q missing %q", detail, want)
		}
	}
	if strings.Contains(detail, "thinking tokens") {
		t.Fatalf("detail %q must omit unset thinking tokens", detail)
	}

	flagTemperature := float32(0.9)
	detail = describeRequest(spec,
		generators.TemperatureFlag{Value: &flagTemperature},
		generators.EffortFlag("high"),
	)
	for _, want := range []string{
		"temperature 0.9",
		"effort high",
	} {
		if !strings.Contains(detail, want) {
			t.Fatalf("flag-overridden detail %q missing %q", detail, want)
		}
	}
	if strings.Contains(detail, "family") {
		t.Fatalf("unset family must be omitted, got %q", detail)
	}

	// A spec with no optional fields set renders the model identity
	// alone: every default value is omitted, including the spec path
	// when the spec was constructed without one.
	bare := describeRequest(generators.Spec{Model: "model-bare"},
		generators.TemperatureFlag{}, generators.EffortFlag(""))
	if want := "model model-bare"; bare != want {
		t.Fatalf("bare spec detail: want %q, got %q", want, bare)
	}
}

// eventSummaryGenerator is a minimal generators.Generator whose Generate
// returns a fixed summary block, so NewSummarizer can be exercised
// without a real model. See TestRunThoughtSummaryNode.
type eventSummaryGenerator struct{}

func (eventSummaryGenerator) Spec() generators.Spec {
	return generators.Spec{}
}

func (eventSummaryGenerator) CountTokens(text string) (int, error) {
	return 0, nil
}

func (eventSummaryGenerator) Generate(ctx context.Context, state generators.State, options *generators.GenerateOptions) (generators.State, error) {
	return state.AppendContent(&generators.Content{
		Role: generators.RoleModel,
		Parts: []generators.Part{
			generators.Text("<<齉爩 summary\n- condensed\n齉爩\n"),
		},
	})
}

// TestRunFinishNodeContent verifies that each generation attempt's finish
// reason is recorded as a finish event node, emitted immediately after
// the attempt's finish reason is known, before the attempt completes.
// See TheoryOfLoopEvents.
func TestRunFinishNodeContent(t *testing.T) {
	withRun(t, func(run Run) {
		opts := RunOptions{
			InitialState: generators.NewPrompts("", nil),
			Components:   nil,
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return func(ctx context.Context, state generators.State) (generators.Phase, generators.State, error) {
					newState, err := state.AppendContent(&generators.Content{
						Role: generators.RoleLog,
						Parts: []generators.Part{
							generators.FinishReason("stop"),
							generators.Text("<<齉爩 summary\n- done\n齉爩\n"),
						},
					})
					if err != nil {
						return nil, state, err
					}
					return nil, newState, nil
				}
			},
		}

		var result Result
		var lastTree *tree.Tree
		var terminalErr error
		for tr, err := range run(context.Background(), opts, &result) {
			if err != nil {
				terminalErr = err
			}
			lastTree = tr
		}
		if terminalErr != nil {
			t.Fatalf("unexpected terminal error: %v", terminalErr)
		}
		var finishNode *tree.Node
		for _, n := range lastTree.ByCategory(tree.CategoryEvent) {
			if strings.HasPrefix(n.Name, "finish") {
				finishNode = n
			}
		}
		if finishNode == nil {
			t.Fatal("expected a finish event node")
		}
		if got := finishNode.Content; got != "finish: stop" {
			t.Fatalf("unexpected finish node: %q", got)
		}
	})
}

// TestRunThoughtSummaryNode verifies that a thought summary produced by
// the ThoughtsSummarize state layer during generation joins the session
// tree as a thought-summary event node: Module.Run installs the layer's
// emitter, so the summary is recorded in the run's own tree. See
// TheoryOfLoopEvents and TheoryOfThoughtsSummarize.
func TestRunThoughtSummaryNode(t *testing.T) {
	withRun(t, func(run Run) {
		summarizer := NewSummarizer(eventSummaryGenerator{})
		initial := NewThoughtsSummarize(
			context.Background(),
			generators.NewPrompts("", nil),
			summarizer,
			&strings.Builder{},
		)
		opts := RunOptions{
			InitialState: initial,
			Components:   nil,
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return func(ctx context.Context, state generators.State) (generators.Phase, generators.State, error) {
					// A mixed content (thought plus answer text) flushes
					// the accumulated thoughts immediately, producing one
					// summary during phase execution.
					newState, err := state.AppendContent(&generators.Content{
						Role: generators.RoleModel,
						Parts: []generators.Part{
							generators.Thought("long reasoning\n"),
							generators.Text("answer\n"),
						},
					})
					if err != nil {
						return nil, state, err
					}
					return nil, newState, nil
				}
			},
		}

		var result Result
		var lastTree *tree.Tree
		var terminalErr error
		for tr, err := range run(context.Background(), opts, &result) {
			if err != nil {
				terminalErr = err
			}
			lastTree = tr
		}
		if terminalErr != nil {
			t.Fatalf("unexpected terminal error: %v", terminalErr)
		}
		var summaryNode *tree.Node
		for _, n := range lastTree.ByCategory(tree.CategoryEvent) {
			if strings.HasPrefix(n.Name, "thought-summary") {
				summaryNode = n
			}
		}
		if summaryNode == nil {
			t.Fatal("expected a thought-summary event node")
		}
		if !strings.Contains(summaryNode.Content, "- condensed") {
			t.Fatalf("unexpected thought summary node: %q", summaryNode.Content)
		}
	})
}

// TestTreeOutlineExcludesEventNodes verifies that the model-facing tree
// outline is a projection without the loop's event nodes: the nodes are
// program bookkeeping the model never sees. See TheoryOfSessionTree and
// TheoryOfLoopEvents.
func TestTreeOutlineExcludesEventNodes(t *testing.T) {
	tr, err := tree.New().WriteAll(
		tree.WriteOp{Parent: "root", Name: "input-1", Type: tree.TypeInput, Author: tree.AuthorUser, Content: "task"},
		tree.WriteOp{Parent: "root", Name: "attempt-start-1", Type: tree.TypeAttemptStart, Author: tree.AuthorProgram, Content: "attempt 1 start"},
	)
	if err != nil {
		t.Fatal(err)
	}
	out := string(treeOutlinePart(tr, "root"))
	if !strings.Contains(out, "input-1 [input/user]") {
		t.Fatalf("expected the input node in the outline, got: %s", out)
	}
	if strings.Contains(out, "attempt-start-1") {
		t.Fatalf("event nodes must be pruned from the model-facing outline, got: %s", out)
	}
}
