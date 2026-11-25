package generators

const TheoryOfSpec = `
Spec merging uses pointer values for optional booleans (DisableSearch, DisableTools, IsOpenRouter, IsAzure, NoProxy, PreservedThinking, ZeroDataRetention)
to distinguish between "explicitly set to false" and "not provided". This allows a child spec to disable a feature
that a parent spec enabled.
Variants allow hierarchical organization of specs where child specs are nested under their parent.
The Name field represents the path component at its level, not the full path.
The Family field represents the model name without version information and is merged from parent to child when non-empty.
Redirect extends the resolved path with additional components: a spec at path "foo/bar" with Redirect "baz"
resolves as "foo/bar/baz". An absolute redirect starting with "/" (e.g., "/foo/bar") resolves from the root
path, replacing the current path entirely instead of appending. Redirect is not merged from parent to child;
only the final spec in the path determines whether a redirect applies. Cycle detection prevents infinite
redirect loops.
RandomRedirect provides load balancing by randomly selecting one of several redirect targets. When the final
spec in the path has RandomRedirect set and Redirect is not set, one entry is randomly chosen and applied as
a redirect, following the same relative/absolute path resolution rules as Redirect. Redirect takes precedence
over RandomRedirect when both are set. Like Redirect, RandomRedirect is not merged from parent to child.
PreservedThinking controls whether reasoning thoughts from previous model responses are sent back to the
server in subsequent requests. When not set or false, thoughts are stripped from outgoing requests to avoid
sending reasoning content back to the model. When true, thoughts are included in the request so the model
can build on prior reasoning context, and the OpenAI generator additionally forwards the flag to the server
as chat_template_kwargs: {"preserve_thinking": true} in the request body.
ZeroDataRetention marks a generator whose provider retains no input or output data. It is consumed by
confidential mode (see TheoryOfConfidentialMode), which rejects any generator that does not set the field
when enabled. Like the other optional booleans, it is not merged from parent to child unless explicitly set.
The Type field names the generator implementation, and it is the only protocol selector: "systemone" selects
the System One protocol (see TheoryOfSystemOne) and "decisions" the Gemini typed-question protocol (see
TheoryOfDecisions), so no separate marker field exists. Type is merged from parent to child when non-empty,
so a variant inherits its parent's protocol.

MaxTokens names the model's maximum context window, covering input and output tokens together. It is the
ceiling every context assembly site caps against: the initial input budget starts from
min(MaxTokens, EffectiveMaxInitTokens), and each site then subtracts its own fixed and reserved parts.
The field merges from parent to child when non-zero, like the other scalars.

Provider holds routing preferences forwarded to OpenRouter in the request
body; see TheoryOfProviderRouting for the routing model and the field-wise
parent-to-child merge.

MaxInitTokens overrides the global -max-init-tokens budget for one generator:
the resolved spec's non-nil value replaces the session-wide flag value at
every context assembly site, so a model with a small context window carries a
proportionally smaller initial prompt without lowering the budget of the other
generators. EffectiveMaxInitTokens owns that precedence rule, so no call site
repeats it. The field merges from parent to child like the other optional
integers.
`

type Spec struct {
	Name              string         `json:"name"`
	Type              string         `json:"type"`
	BaseURL           string         `json:"base_url"`
	APIKey            string         `json:"api_key"`
	Model             string         `json:"model"`
	Family            string         `json:"family"`
	MaxTokens         int            `json:"max_tokens"`
	MaxInitTokens     *int           `json:"max_init_tokens,omitempty"`
	MaxGenerateTokens *int           `json:"max_generate_tokens"`
	MaxThinkingTokens *int           `json:"max_thinking_tokens,omitempty"`
	Temperature       *float32       `json:"temperature"`
	DisableSearch     *bool          `json:"disable_search,omitempty"`
	DisableTools      *bool          `json:"disable_tools,omitempty"`
	ExtraArguments    map[string]any `json:"extra_arguments"`
	IsOpenRouter      *bool          `json:"is_open_router,omitempty"`
	APIVersion        string         `json:"api_version"`
	IsAzure           *bool          `json:"is_azure,omitempty"`
	ServiceTier       string         `json:"service_tier"`
	ReasoningEffort   string         `json:"reasoning_effort"`
	Aliases           []string       `json:"aliases"`
	Redirect          string         `json:"redirect,omitempty"`
	RandomRedirect    []string       `json:"random_redirect,omitempty"`
	NoProxy           *bool          `json:"no_proxy,omitempty"`
	PreservedThinking *bool          `json:"preserved_thinking,omitempty"`
	ZeroDataRetention *bool          `json:"zero_data_retention,omitempty"`
	Provider          *Provider      `json:"provider,omitempty"`
	Variants          []Spec         `json:"variants,omitempty"`
}

// EffectiveMaxInitTokens resolves the initial input token budget for this
// generator: the spec's own MaxInitTokens when configured, otherwise the
// global budget passed in from the -max-init-tokens flag. The generator
// value wins over the global one, so a single model can declare a smaller
// initial context than the session default. The context window of the model
// still caps the result at the call site. See TheoryOfSpec.
func (s Spec) EffectiveMaxInitTokens(global int) int {
	if s.MaxInitTokens != nil {
		return *s.MaxInitTokens
	}
	return global
}
