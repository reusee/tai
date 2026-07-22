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
}
