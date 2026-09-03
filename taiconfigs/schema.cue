// applications
cmd_ai?: {
  model_name?: string
  model?: string
  // fast_model_name specifies the fast model for summarization and lightweight tasks.
  fast_model_name?: string
  // fast_model is an alias for fast_model_name.
  // handoff_model_name specifies the model used for handoff.
handoff_model_name?: string
  // handoff_model is an alias for handoff_model_name.
handoff_model?: string
  fast_model?: string
}

model_name?: string
model?: string
fast_model_name?: string
fast_model?: string

// handoff_model specifies the model used for handoff: the summarization
// of truncated or failed generation output before retry, and periodic
// thought summaries. When empty, the fast model is used if configured;
// otherwise the default model is used.
handoff_model?: string
// handoff_model_name is an alias for handoff_model.
handoff_model_name?: string

// max_tokens limits the total context tokens (input + output).
// Deprecated in favor of max_context_tokens.
max_tokens?: int

// max_context_tokens limits the total context tokens (input + output).
max_context_tokens?: int

// extra_system_prompt provides additional instructions to the AI.
// Accepts a single string or a list of strings; values from multiple
// config files are aggregated additively.
// family_extra_system_prompt provides additional instructions to the AI
// keyed by model family. Accepts a map from family name to a single string
// or a list of strings; values from multiple config files are aggregated
// additively per family.
family_extra_system_prompt?: {[string]: string | [...string]}
extra_system_prompt?: string | [...string]

// match provides a regex to filter files by path.
match?: string

// match_patterns provides a list of regex patterns to filter files by path.
match_patterns?: [...string]

// effort specifies the reasoning effort level (e.g., low, medium, high).
effort?: string

// shell enables shell block execution during generation.
shell?: bool

// thoughts controls whether model reasoning thoughts are shown in output.
thoughts?: bool

// summarize_thoughts enables periodic summarization of model reasoning thoughts.
summarize_thoughts?: bool

// temperature controls the randomness of the output.
temperature?: float

// apply controls whether change blocks are applied to the working tree during generation.
apply?: bool

// plan enables mandatory planning and multi-round generation.
plan?: bool

// log_level sets the log level (debug, info, warn, error).
log_level?: string

// no_memory disables user profile memory persistence.
no_memory?: bool
// record enables recording of interaction sessions for self-improvement
// analysis. When enabled, every generation command records its sessions
// into a single sqlite database file.
record?: bool
// confidential_mode restricts model selection to zero-data-retention
// models when enabled.
confidential_mode?: bool

// review enables a review loop after generation to review and fix changes.
review?: bool

// review_models lists the models used for the review loop, in order.
review_models?: [...string]

// ignore excludes files or patterns from the context.
ignore?: [...string]

// files specifies files to include in the context by path or glob pattern.
files?: [...string]

// focus specifies aspects to focus on for the task.
focus?: [...string]

// go contains settings for Go language project analysis.
go?: {
	// load_dir specifies the root directory for loading Go packages.
	// Defaults to the current working directory.
	load_dir?: string
	// dir is an alias for load_dir.
	dir?: string

	// load_patterns specifies the patterns for loading Go packages.
	// Defaults to ["./..."].
	load_patterns?: [...string]
	// packages is an alias for load_patterns.
	packages?: [...string]
	// pkgs is an alias for load_patterns.
	pkgs?: [...string]

	// context_patterns specifies additional patterns for context packages.
	context_patterns?: [...string]

	// doc_patterns specifies additional Go package paths whose documentation
	// (go doc -all -cmd) is included in the context as reference.
	doc_patterns?: [...string]
// hidden specifies import-path patterns of packages that are always
	// hidden from the context: no code, no documentation, and no go-src
	// symbol resolution. A pattern ending in "/..." hides the base
	// package and every subpackage (e.g., "github.com/foo/bar/..."
	// hides github.com/foo/bar and all packages under it).
	hidden?: [...string]

	// no_tests, if true, excludes test files from the context.
	no_tests?: bool

	// all_src, if true, includes the full source code of focus packages,
	// including tests, as their initial context instead of package
	// documentation.
	all_src?: bool

	// show_token_counts, if true, displays token counts for each included file.
	show_token_counts?: bool

	// envs provides additional environment variables for the 'go list' command.
	envs?: [...string]
// extra_system_prompt provides Go-specific additional instructions to
	// the AI. Unlike the top-level extra_system_prompt, these are
	// introduced whenever the codes generation pipeline is active (go,
	// any, goal commands); the ai command is unaffected.
// family_extra_system_prompt provides Go-specific additional
// instructions to the AI keyed by model family.
family_extra_system_prompt?: {[string]: string | [...string]}
	
	extra_system_prompt?: string | [...string]
}

// go_envs is a top-level alias for go.envs, providing additional
// environment variables for the 'go list' command.
go_envs?: [...string]

// debug flags for individual modules.
debug_gemini?: bool
debug_openai?: bool
tap_openai?: bool
debug_codes?: bool
debug_gotools?: bool
debug_anytexts?: bool

// _gen defines the structure of a generator (AI model configuration).
// It supports recursive variants for hierarchical spec organization.
_gen: {
	// name is the unique identifier for the generator.
	name: string
	// type specifies the generator type (e.g., "gemini", "openai", "deepseek").
	type: string
	// base_url is the API endpoint for the model.
	base_url?: string
	// api_key is the authentication key for the API.
	api_key?: string
	// model is the specific model name (e.g., "gpt-4o", "gemini-1.5-pro").
	model?: string
	// family is the model name without version information.
	family?: string
	// context_tokens is the maximum context window size for the model.
	context_tokens?: int
	// max_generate_tokens is the maximum number of tokens to generate.
	max_generate_tokens?: int
	// max_thinking_tokens is the maximum number of tokens for reasoning/thinking.
	max_thinking_tokens?: int
	// temperature controls the randomness of the output.
	temperature?: float
	// disable_search, if true, disables search capabilities for the model.
	disable_search?: bool
	// disable_tools, if true, disables tool usage for the model.
	disable_tools?: bool
	// is_open_router, if true, uses OpenRouter-specific request formatting.
	is_open_router?: bool
	// api_version specifies the API version for Azure deployments.
	api_version?: string
	// is_azure, if true, uses Azure-specific request formatting.
	is_azure?: bool
	// service_tier specifies the service tier for the model.
	service_tier?: string
	// reasoning_effort specifies the reasoning effort level.
	reasoning_effort?: string
	// aliases provides alternative names for the generator.
	aliases?: [...string]
	// redirect extends the resolved path with additional components.
	redirect?: string
	// no_proxy, if true, bypasses the proxy for this generator.
	no_proxy?: bool
	// preserved_thinking, if true, sends reasoning thoughts back to the model in subsequent requests.
	preserved_thinking?: bool
// zero_data_retention, if true, marks the generator as retaining no input
	// or output data, permitting its use in confidential mode.
	zero_data_retention?: bool
	// extra_arguments allows for provider-specific parameters.
	extra_arguments?: {[string]: _}
	// variants defines nested generator configurations that inherit parent fields.
	variants?: [..._gen]
}

// generators defines a list of available AI model configurations.
generators?: [..._gen]

// api keys
openai_api_key?:      string
anthropic_api_key?:   string
google_api_key?:      string
huoshan_api_key?:     string
baidu_api_key?:       string
deepseek_api_key?:    string
open_router_api_key?: string
openrouter_api_key?:  string
tencent_api_key?:     string
aliyun_api_key?:      string
zhipu_api_key?:       string
vercel_api_key?:      string
nvidia_api_key?:      string
azure_api_key?:       string
aws_bedrock_api_key?: string
opencode_go_api_key?: string

proxy_addr?: string
proxy_address?: string
http_proxy?: string
socks_proxy?: string
openrouter_endpoint?: string
azure_endpoint?: string
azure_api_version?: string

_var: {
  name?: string
  type: "none" | "nil" | "string" | "str" | "number" | "num" | "int" | "integer" | "bool" | "boolean" | "array" | "list" | "object" | "struct"
  optional?: bool
  description?: string
  item_type?: _var
  properties?: [..._var]
}

functions?: [...{
  name: string
  description?: string
  params: [..._var]
  returns: [..._var]
}]

// tui configures the colors of the terminal UI. Each field accepts a
// W3C color name (e.g., "red") or a hex value (e.g., "#ff0000").
// Backgrounds left empty paint no background; foregrounds left empty
// keep the built-in defaults.
tui?: {
	// tab_unfocused_bg is the background of unfocused tabs.
	tab_unfocused_bg?: string
	// tab_focused_bg is the background of the focused tab.
	tab_focused_bg?: string
	// label_fg is the label color of an unfocused tab.
	label_fg?: string
	// focus_label_fg is the label color of the focused tab.
	focus_label_fg?: string
	// active_label_fg highlights a label with an active request.
	active_label_fg?: string
	// unseen_dot_color colors the unseen dot on a collapsed strip.
	unseen_dot_color?: string
	// user_color colors user input lines.
	user_color?: string
	// tool_color colors tool call lines.
	tool_color?: string
	// system_color colors system message lines.
	system_color?: string
	// log_color colors log and event lines.
	log_color?: string
	// thought_color colors thought summary headers.
	thought_color?: string
	// input_focused_fg colors the chat input line while focused.
	input_focused_fg?: string
	// input_unfocused_fg colors the chat input line while unfocused.
	input_unfocused_fg?: string
}
// thoughts_summarize_language sets the output language for thought summaries.
// When empty (the default), no language hint is given to the summarizer.
// When set (e.g., "zh", "en"), the summarizer is instructed to output
// summaries in that language.
thoughts_summarize_language?: string
// summary_language sets the output language for summary blocks. When
// empty (the default), no language hint is given to the model. When set
// (e.g., "zh", "en"), the summary block prompt instructs the model to
// write the summary bullet items in that language.
summary_language?: string
