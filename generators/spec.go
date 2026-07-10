package generators

const SpecTheory = `
Spec merging uses pointer values for optional booleans (DisableSearch, DisableTools, IsOpenRouter, IsAzure, NoProxy)
to distinguish between "explicitly set to false" and "not provided". This allows a child spec to disable a feature
that a parent spec enabled.
Variants allow hierarchical organization of specs where child specs are nested under their parent.
The Name field represents the path component at its level, not the full path.
The Family field represents the model name without version information and is merged from parent to child when non-empty.
Redirect extends the resolved path with additional components: a spec at path "foo/bar" with Redirect "baz"
resolves as "foo/bar/baz". Redirect is not merged from parent to child; only the final spec in the path
determines whether a redirect applies. Cycle detection prevents infinite redirect loops.
`

type Spec struct {
	Name              string         `json:"name"`
	Type              string         `json:"type"`
	BaseURL           string         `json:"base_url"`
	APIKey            string         `json:"api_key"`
	Model             string         `json:"model"`
	Family            string         `json:"family"`
	ContextTokens     int            `json:"context_tokens"`
	MaxGenerateTokens *int           `json:"max_generate_tokens"`
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
	NoProxy           *bool          `json:"no_proxy,omitempty"`
	Variants          []Spec         `json:"variants,omitempty"`
}
