package codes

import (
	"strings"
	"testing"

	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/generators"
)

type mockCodeProvider struct{}

var _ codetypes.CodeProvider = mockCodeProvider{}

func (mockCodeProvider) Parts(int, func(string) (int, error), []string) ([]generators.Part, error) {
	return nil, nil
}

func (mockCodeProvider) Functions() []*generators.Function {
	return nil
}

func (mockCodeProvider) SystemPrompt() string {
	return ""
}

func TestSystemPromptDynamicContext(t *testing.T) {
	module := Module{}

	t.Run("Disabled", func(t *testing.T) {
		prompt := module.SystemPrompt(
			mockCodeProvider{},
			BoundaryDiffHandler{},
			DynamicContext(false),
			Shell(false),
			ExtraSystemPrompt(""),
		)
		if strings.Contains(string(prompt), "Request-Context Block Kind") {
			t.Fatal("system prompt must not include request-context section when dynamic context is disabled")
		}
	})

	t.Run("Enabled", func(t *testing.T) {
		prompt := module.SystemPrompt(
			mockCodeProvider{},
			BoundaryDiffHandler{},
			DynamicContext(true),
			Shell(false),
			ExtraSystemPrompt(""),
		)
		if !strings.Contains(string(prompt), "Request-Context Block Kind") {
			t.Fatal("system prompt must include request-context section when dynamic context is enabled")
		}
	})
}

func TestSystemPromptReadOnlyFiles(t *testing.T) {
	module := Module{}
	prompt := module.SystemPrompt(
		mockCodeProvider{},
		BoundaryDiffHandler{},
		DynamicContext(false),
		Shell(false),
		ExtraSystemPrompt(""),
	)
	if !strings.Contains(string(prompt), "Read-Only Files") {
		t.Fatal("system prompt must include the read-only files section")
	}
	if !strings.Contains(string(prompt), "read-only") {
		t.Fatal("system prompt must reference read-only files")
	}
}

func TestSystemPromptContinueBlock(t *testing.T) {
	module := Module{}
	prompt := module.SystemPrompt(
		mockCodeProvider{},
		BoundaryDiffHandler{},
		DynamicContext(false),
		Shell(false),
		ExtraSystemPrompt(""),
	)
	if !strings.Contains(string(prompt), "Continue Block Kind") {
		t.Fatal("system prompt must include continue block section")
	}
	if !strings.Contains(string(prompt), ":::continue") {
		t.Fatal("system prompt must include continue block format")
	}
	if !strings.Contains(string(prompt), "Task Decomposition") {
		t.Fatal("system prompt must include task decomposition strategy for complex tasks")
	}
	if !strings.Contains(string(prompt), "task list") {
		t.Fatal("system prompt must include task list concept for multi-round continue blocks")
	}
}

func TestSystemPromptMandatoryPlanning(t *testing.T) {
	module := Module{}
	prompt := string(module.SystemPrompt(
		mockCodeProvider{},
		BoundaryDiffHandler{},
		DynamicContext(false),
		Shell(false),
		ExtraSystemPrompt(""),
	))
	if !strings.Contains(prompt, "Mandatory Planning") {
		t.Fatal("system prompt must include the mandatory planning section")
	}
	if !strings.Contains(prompt, "overall plan") {
		t.Fatal("system prompt must require an overall plan before any change blocks")
	}
	if !strings.Contains(prompt, "Emit NO change blocks in the planning round") {
		t.Fatal("system prompt must forbid change blocks in the planning round")
	}
	if !strings.Contains(prompt, "supersedes") {
		t.Fatal("system prompt must state the mandate supersedes the single-response exemption")
	}
}

func TestSystemPromptTaskDecompositionStrategies(t *testing.T) {
	module := Module{}
	prompt := string(module.SystemPrompt(
		mockCodeProvider{},
		BoundaryDiffHandler{},
		DynamicContext(false),
		Shell(false),
		ExtraSystemPrompt(""),
	))

	categories := []string{
		"Structural strategies",
		"Adaptive strategies",
		"Quality strategies",
		"Scheduling strategies",
	}
	for _, c := range categories {
		if !strings.Contains(prompt, c) {
			t.Fatalf("system prompt must include category %q", c)
		}
	}

	strategies := []string{
		"Input-driven",
		"Logical-step-driven",
		"Interface-first",
		"Output-length-driven",
		"Progressive refinement",
		"Error recovery",
		"Feedback-driven",
		"Verification-driven",
		"Risk-driven",
		"Context-collection-first",
		"Dependency-driven",
		"Blast-radius-driven",
		"Token-budget-driven",
		"Reversibility-driven",
	}
	for _, s := range strategies {
		if !strings.Contains(prompt, s) {
			t.Fatalf("system prompt must include strategy %q", s)
		}
	}
}
