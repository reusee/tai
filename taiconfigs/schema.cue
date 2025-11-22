// action specifies the default action to perform.
// Can be "chat" or "do".
action?: string

// action_argument provides the input or goal for the action.
action_argument?: string

model_name?: string
model?: string

// plan_model specifies the model to use for the planning phase in the "do" action.
plan_model?: string

// code_model specifies the model to use for the code generation phase in the "do" action.
code_model?: string


// max_tokens limits the total context tokens (input + output).
// Deprecated in favor of max_context_tokens.
max_tokens?: int

// max_context_tokens limits the total context tokens (input + output).
max_context_tokens?: int

// match provides a regex to filter files by path.
match?: string

// diff specifies the diff handler to use. e.g., "unified".
diff?: string

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

	// max_distance sets the maximum import distance from root packages to include.
	// Defaults to 2.
	max_distance?: int

	// no_tests, if true, excludes test files from the context.
	no_tests?: bool

	// envs provides additional environment variables for the 'go list' command.
	envs?: [...string]
}

// generators defines a list of available AI model configurations.
generators?: [...{
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
	// context_tokens is the maximum context window size for the model.
	context_tokens?: int
	// max_generate_tokens is the maximum number of tokens to generate.
	max_generate_tokens?: int
	// temperature controls the randomness of the output.
	temperature?: float
	// disable_search, if true, disables search capabilities for the model.
	disable_search?: bool
	// disable_tools, if true, disables tool usage for the model.
	disable_tools?: bool
	// extra_arguments allows for provider-specific parameters.
	extra_arguments?: {[string]: _}
}]

// api keys
google_api_key?: string
huoshan_api_key?: string
baidu_api_key?: string
deepseek_api_key?: string
open_router_api_key?: string
openrouter_api_key?: string
tencent_api_key?: string
aliyun_api_key?: string
zhipu_api_key?: string
vercel_api_key?: string

proxy_addr?: string
proxy_address?: string
http_proxy?: string
socks_proxy?: string

_var: {
  name?: string
  type: "none" | "nil" | "string" | "str" | "number" | "num" | "int" | "integer" | "bool" | "boolean" | "array" | "list" | "object" | "struct"
  optional?: bool
  description?: string
  item_type?: _var
  properties?: [..._var]
}

functions: [...{
  name: string
  description?: string
  params: [..._var]
  returns: [..._var]
}]

// log_level sets the logging verbosity.
// Can be "debug", "info", "warn", or "error".
log_level?: "debug" | "info" | "warn" | "error"

