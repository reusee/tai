package pipeline

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/gotools"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/pathutil"
	"github.com/reusee/tai/pipeline/codetypes"
	"github.com/reusee/tai/selfprompts"
)

func TestSystemPromptGoTestBlock(t *testing.T) {
	prompt := dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
	).Get[SystemPrompt]()
	if !strings.Contains(string(prompt), "Go-Test Block Kind") {
		t.Fatal("system prompt must include go-test block section")
	}
	// The go-test prompt must instruct the model to emit a summary block
	// even when emitting a go-test block. Without this, the model may omit
	// the summary, causing unnecessary retries (see TheoryOfSummaryCompletionRetry
	// in generate.go and TheoryOfGoTestBlocks in gotools/gotest.go).
	if !strings.Contains(string(prompt), "go-test block is NOT a completion signal") {
		t.Fatal("system prompt must state that go-test block is not a completion signal and summary is still required")
	}
}

func TestGoTestComponentPassTriggersRoundWithOutput(t *testing.T) {
	// Test output is always fed back as Parts, triggering a new round
	// regardless of whether tests pass or fail: the model needs the
	// results to decide whether to continue, and withholding output on
	// pass causes the system to exit prematurely when the model intended
	// to proceed. See TheoryOfCodesComponents and
	// gotools.TheoryOfGoTestBlocks.
	goTestBlocks := []blocks.Block{
		{Kind: "go-test", Body: "-run\n___nonexistent___"},
	}

	comps := dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
	).Get[CodesComponents]()
	for _, comp := range comps.Processable() {
		if comp.Kind != "go-test" {
			continue
		}
		result := comp.Process(context.Background(), &components.ProcessContext{
			Blocks: goTestBlocks,
		})
		if result.Err != nil {
			t.Fatalf("unexpected error: %v", result.Err)
		}
		if len(result.Parts) != 1 {
			t.Fatalf("expected Parts carrying the test output when tests pass, got %d parts", len(result.Parts))
		}
		text, ok := result.Parts[0].(generators.Text)
		if !ok {
			t.Fatalf("expected Text part, got %T", result.Parts[0])
		}
		if !strings.Contains(string(text), "Command succeeded") {
			t.Fatalf("expected the passing test output in Parts, got %q", text)
		}
		return
	}
	t.Fatal("go-test component not found")
}

func TestGoTestComponentFailTriggersRound(t *testing.T) {
	goTestBlocks := []blocks.Block{
		{Kind: "go-test", Body: "-bogusflag"},
	}

	comps := dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
	).Get[CodesComponents]()
	for _, comp := range comps.Processable() {
		if comp.Kind != "go-test" {
			continue
		}
		result := comp.Process(context.Background(), &components.ProcessContext{
			Blocks: goTestBlocks,
		})
		if result.Err != nil {
			t.Fatalf("unexpected error: %v", result.Err)
		}
		if len(result.Parts) == 0 {
			t.Fatal("expected parts when tests fail; go-test must produce Parts to trigger a new round")
		}
		return
	}
	t.Fatal("go-test component not found")
}

func TestCodesComponentsIncludesFamilyExtraSystemPrompt(t *testing.T) {
	comps := dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return gotools.PartsProvider{} },
		func() generators.ModelFamily { return "gemini" },
		func() flags.FamilyExtraSystemPrompt {
			return flags.FamilyExtraSystemPrompt{"gemini": {"gemini family prompt"}}
		},
		func() gotools.FamilyExtraSystemPrompt {
			return gotools.FamilyExtraSystemPrompt{"gemini": {"go gemini family prompt"}}
		},
	).Get[CodesComponents]()
	prompt := comps.PromptSections()
	if !strings.Contains(prompt, "gemini family prompt") {
		t.Fatal("expected top-level family prompt in system prompt")
	}
	if !strings.Contains(prompt, "go gemini family prompt") {
		t.Fatal("expected go-specific family prompt in system prompt")
	}
}

func TestCodesComponentsIncludesHiddenPackages(t *testing.T) {
	comps := dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
	).Get[CodesComponents]()
	defaultPrompt := comps.PromptSections()
	if strings.Contains(defaultPrompt, "Hidden Packages") {
		t.Fatal("no hidden patterns configured must not produce a hidden-packages section")
	}

	comps = dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
		func() gotools.HiddenPatterns { return gotools.HiddenPatterns{"example.com/foo/bar/..."} },
	).Get[CodesComponents]()
	prompt := comps.PromptSections()
	if !strings.Contains(prompt, "Hidden Packages") {
		t.Fatal("expected hidden-packages section in system prompt")
	}
	if !strings.Contains(prompt, "example.com/foo/bar/...") {
		t.Fatal("expected the configured pattern in the system prompt")
	}
}

func TestCodesComponentsExcludesNonMatchingFamilyPrompt(t *testing.T) {
	comps := dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
		func() generators.ModelFamily { return "other" },
		func() flags.FamilyExtraSystemPrompt {
			return flags.FamilyExtraSystemPrompt{"gemini": {"gemini family prompt"}}
		},
	).Get[CodesComponents]()
	if strings.Contains(comps.PromptSections(), "gemini family prompt") {
		t.Fatal("non-matching family prompt must not be included")
	}
}

func TestCodesComponentsExcludesGoFamilyPromptForNonGoProvider(t *testing.T) {
	// The Go-specific family prompt is gated on the Go session: a mock
	// (non-Go) parts provider must exclude it while the top-level family
	// prompt still applies.
	comps := dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
		func() generators.ModelFamily { return "gemini" },
		func() flags.FamilyExtraSystemPrompt {
			return flags.FamilyExtraSystemPrompt{"gemini": {"gemini family prompt"}}
		},
		func() gotools.FamilyExtraSystemPrompt {
			return gotools.FamilyExtraSystemPrompt{"gemini": {"go gemini family prompt"}}
		},
	).Get[CodesComponents]()
	prompt := comps.PromptSections()
	if !strings.Contains(prompt, "gemini family prompt") {
		t.Fatal("top-level family prompt must apply to non-go sessions too")
	}
	if strings.Contains(prompt, "go gemini family prompt") {
		t.Fatal("non-go session must not include go-specific family prompt")
	}
}

// TestCodesComponentsSelfPromptUsesInjectedTaiDir checks that the tai dir
// reaches the self-prompt component through the scope: a scope-level
// override redirects the state file, so a package that persists state takes
// the one resolved value instead of resolving a directory of its own.
// See pathutil.TheoryOfTaiDir and selfprompts.TheoryOfSelfPrompt.
func TestCodesComponentsSelfPromptUsesInjectedTaiDir(t *testing.T) {
	dir := pathutil.TaiDir(t.TempDir())
	comps := dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
		func() pathutil.TaiDir { return dir },
	).Get[CodesComponents]()
	for _, comp := range comps.Processable() {
		if comp.Kind != selfprompts.Kind {
			continue
		}
		result := comp.Process(context.Background(), &components.ProcessContext{
			Blocks: []blocks.Block{{
				Kind:       selfprompts.Kind,
				Attributes: map[string]string{"op": "add", "name": "tests"},
				Body:       "Run the tests before claiming a fix is complete.",
			}},
		})
		if result.Err != nil {
			t.Fatal(result.Err)
		}
		if len(result.Parts) != 1 {
			t.Fatalf("an applied batch must report itself, got %v", result.Parts)
		}
		if _, err := os.Stat(dir.StatePath("self-prompt")); err != nil {
			t.Fatalf("the batch did not reach the injected tai dir: %v", err)
		}
		return
	}
	t.Fatal("self-prompt component not found")
}
