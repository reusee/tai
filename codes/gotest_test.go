package codes

import (
	"strings"
	"testing"

	"github.com/reusee/tai/flags"
)

func TestSystemPromptGoTestBlock(t *testing.T) {
	module := Module{}
	comps := module.CodesComponents(
		BoundaryDiffHandler{},
		mockCodeProvider{},
		ExtraSystemPrompt(""),
		DynamicContext(false),
		Apply(true),
		Plan(false),
		flags.Shell(false),
	)
	prompt := module.SystemPrompt(
		comps,
		mockCodeProvider{},
	)
	if !strings.Contains(string(prompt), "Go-Test Block Kind") {
		t.Fatal("system prompt must include go-test block section")
	}
	if !strings.Contains(string(prompt), "<go-test>") {
		t.Fatal("system prompt must include go-test block format")
	}
	// The go-test prompt must instruct the model to emit a summary block
	// even when emitting a go-test block. Without this, the model may omit
	// the summary, causing unnecessary retries (see TheoryOfSummaryCompletionRetry
	// in codes/generate.go and TheoryOfGoTestBlocks in blocks/gotest.go).
	if !strings.Contains(string(prompt), "go-test block is NOT a completion signal") {
		t.Fatal("system prompt must state that go-test block is not a completion signal and summary is still required")
	}
}
