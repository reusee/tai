package generators

type GeneratorArgs struct {
	BaseURL           string         `json:"base_url"`
	APIKey            string         `json:"api_key"`
	Model             string         `json:"model"`
	ContextTokens     int            `json:"context_tokens"`
	MaxGenerateTokens *int           `json:"max_generate_tokens"`
	Temperature       *float32       `json:"temperature"`
	DisableSearch     bool           `json:"disable_search"`
	DisableTools      bool           `json:"disable_tools"`
	ExtraArguments    map[string]any `json:"extra_arguments"`
	IsOpenRouter      bool           `json:"is_open_router"`
}
