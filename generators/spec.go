package generators

const SpecTheory = `
Spec merging uses pointer values for optional booleans (DisableSearch, DisableTools, IsOpenRouter, IsAzure, NoProxy)
to distinguish between "explicitly set to false" and "not provided". This allows a child spec to disable a feature
that a parent spec enabled.
`

type Spec struct {
	Name              string         `json:"name"`
	Type              string         `json:"type"`
	BaseURL           string         `json:"base_url"`
	APIKey            string         `json:"api_key"`
	Model             string         `json:"model"`
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
	NoProxy           *bool          `json:"no_proxy,omitempty"`
}
