# tai

`tai` is an AI-powered code generation and chat assistant command-line tool. It provides structured output parsing, multi-round generation, in-memory change application, per-model memory profiles, dynamic context fetching, and mandatory planning for complex tasks.

## Installation

```sh
go install github.com/reusee/tai/cmd/tai@latest
```

## Quick Start

```sh
# Generate Go code in the current project (default in Go modules)
tai "refactor the Foo function to handle nil inputs"

# Interactive AI chat with memory
tai ai

# Generate code for arbitrary text files
tai any "add a LICENSE file"

# Identify and execute the most valuable next step
tai next

# Apply a pre-existing boundary-delimited diff file
tai patch

# Test model reachability
tai ping -model gemini
```

## Commands

| Command | Description |
|---------|-------------|
| `tai` (default) | Generate Go code. Automatically selected when inside a Go module. |
| `tai ai` | Start an interactive AI chat session with per-model memory persistence. |
| `tai go` | Generate code for Go files using the Go-aware code provider. |
| `tai any` | Generate code for arbitrary text files using the general-purpose provider. |
| `tai next` | Identify and execute the most valuable next step to advance the user's goal. Single-shot generation with change block application. |
| `tai patch` | Apply a boundary-delimited diff file (default `.AI`) to the working tree without invoking a model. |
| `tai ping` | Test whether a model is reachable by sending a hello message. |

## Configuration

`tai` reads configuration from CUE files (`tai.cue` or `.tai.cue`) discovered in the working directory, user config directory, and `/etc`. Command-line flags override config file values.

### Example `tai.cue`

```cue
// Model selection
model: "gemini-pro"
fast_model: "gemini-flash"

// Context limits
max_context_tokens: 128000

// Extra system prompt
extra_system_prompt: "Always add doc comments to exported functions."

// File matching
match_patterns: [".*\\.go$"]
ignore: ["vendor/", "testdata/"]

// Go-specific settings
go: {
    load_patterns: ["./..."]
    max_distance: 2
    no_tests: false
    include_std: false
}

// Feature flags
shell: false
plan: false
apply: true
dynamic_context: false
thoughts: true
summarize_thoughts: false
no_memory: false

// API keys (config-only, not exposed in process listings)
google_api_key: "your-key"
deepseek_api_key: "your-key"

// Proxy
proxy_addr: "socks5://127.0.0.1:1080"

// Custom generators
generators: [
    {
        name: "my-model"
        type: "openai"
        base_url: "https://api.example.com/v1"
        api_key: "your-key"
        model: "example-model"
        context_tokens: 32000
    },
]
```

### Key Flags

| Flag | Description |
|------|-------------|
| `-model` | Set the model name for generation |
| `-fast-model` | Set the fast model for summarization and lightweight tasks |
| `-file` | Add a file to the context by path or glob pattern |
| `-match` / `-include` | Match files by regex pattern for inclusion |
| `-ignore` / `-skip` / `-exclude` | Exclude a file or pattern from the context |
| `-focus` | Focus on a specific aspect of the task |
| `-effort` | Set the reasoning effort level (low, medium, high) |
| `-shell` / `-no-shell` | Enable or disable shell block execution |
| `-thoughts` / `-no-thoughts` | Show or hide model reasoning thoughts |
| `-summarize-thoughts` | Enable periodic summarization of model reasoning |
| `-plan` | Enable mandatory planning and multi-round generation |
| `-apply` / `-no-apply` | Apply or skip change block application |
| `-dynamic-context` | Enable dynamic context fetching via request-context blocks |
| `-no-memory` | Disable user profile memory persistence |
| `-max-tokens` | Set the maximum token budget for context |
| `-temperature` | Set the generation temperature (0.0–2.0) |
| `-log-level` | Set the log level (debug, info, warn, error) |
| `-help` / `--help` / `-h` | Show available flags and descriptions |

## Supported Generators

| Type | Provider | Default Base URL |
|------|----------|-----------------|
| `gemini` | Google Gemini API | (API default) |
| `openai` | OpenAI-compatible | (user-specified) |
| `open-router` / `openrouter` | OpenRouter | `https://openrouter.ai/api/v1` |
| `deepseek` | DeepSeek | `https://api.deepseek.com/` |
| `baidu` | Baidu Qianfan | `https://qianfan.baidubce.com/v2/` |
| `tencent` | Tencent Hunyuan | `https://api.hunyuan.cloud.tencent.com/v1` |
| `huoshan` | Volcengine Ark | `https://ark.cn-beijing.volces.com/api/v3` |
| `aliyun` | Aliyun DashScope | `https://dashscope.aliyuncs.com/compatible-mode/v1` |
| `zhipu` | Zhipu BigModel | `https://open.bigmodel.cn/api/paas/v4/` |
| `vercel` | Vercel AI Gateway | `https://ai-gateway.vercel.sh/v1/` |
| `nvidia` | NVIDIA integrate | `https://integrate.api.nvidia.com/v1` |
| `azure` | Azure OpenAI | (user-specified endpoint) |
| `bedrock` | AWS Bedrock | `https://bedrock-mantle.ap-northeast-1.api.aws/v1` |
| `ollama` | Local Ollama | `http://127.0.0.1:11434/v1` |
| `opencode-go` | OpenCode Go | `https://opencode.ai/zen/go/v1` |

### Built-in Model Aliases

| Alias | Resolves To |
|-------|-------------|
| `flash` / `gemini-flash` | `models/gemini-flash-latest` |
| `gemini` / `pro` / `gemini-pro` | `models/gemini-pro-latest` |

## Structured Output: Boundary-Delimited Blocks

`tai` uses a boundary-delimited block format for structured AI output. Each block has a random boundary string (two uncommon Chinese characters), an XML element kind, attributes, and a body.

### Block Kinds

| Kind | Purpose |
|------|---------|
| `change` | Code modification (MODIFY, ADD_BEFORE, ADD_AFTER, DELETE, RENAME, WRITE) |
| `finish` | Terminal signal with one-sentence summary |
| `summary` | Per-round description displayed alongside statistics |
| `continue` | Self-prompting for multi-round generation |
| `go-test` | Run Go tests and feed results back |
| `shell` | Execute shell commands (allowlisted, read-only) |
| `request-context` | Fetch additional files or URLs mid-generation |
| `memory` | Update the per-model user profile |

### Change Block Example

```
<<CHG1 <change op="MODIFY" target="Foo" file-path="/path/to/file.go">
// Foo does something important.
func Foo() {
    println("fixed")
}
CHG1
```

### Change Operations

| Operation | Description |
|-----------|-------------|
| `MODIFY` | Replace an existing top-level declaration |
| `ADD_BEFORE` | Add new code before an existing declaration |
| `ADD_AFTER` | Add new code after an existing declaration |
| `DELETE` | Remove a declaration (or entire file with `target="*"`) |
| `RENAME` | Rename a file |
| `WRITE` | Replace the entire file content |

Special Go-only MODIFY targets: `package` (replace package clause) and `import` (replace all import declarations).

## Features

### In-Memory Apply

Change blocks are applied to an in-memory `MemoryStore` during streaming, deferring disk writes until the round succeeds. This enables early error detection (malformed change blocks stop generation immediately) and filesystem consistency on retry (failed or truncated rounds never touch disk).

### Per-Model Memory Profiles

The `ai` command maintains a persistent per-model user profile (`ai-memory.json` in the user config directory). The profile is read into the system prompt for long-term context and updated after each generation round from memory blocks or textual pseudo-calls.

### Dynamic Context

When enabled (`-dynamic-context`), the model can emit `request-context` blocks to fetch additional files, URLs, or glob results mid-generation. The system fetches the requested data and provides it as user content for the next round.

### Mandatory Planning

When enabled (`-plan`), every task begins with a plan-only first round (no change blocks), followed by execution rounds delimited by continue blocks. This prevents truncation on large or complex tasks by keeping each round's output bounded.

### Thought Summarization

When enabled (`-summarize-thoughts`), model reasoning thoughts are periodically summarized using the fast model. Summaries appear before the main text output, helping users quickly assess whether the model's reasoning is on track.

### Go-Aware Code Context

The `go` subcommand uses `go/packages` to load the project's dependency graph, sort files by module/package/distance/path for deterministic ordering, and simplify context files (delete comments, function bodies) to fit within a fixed token budget while preserving focus file content.

### Go-Test Integration

The model can emit `go-test` blocks to run tests after making code changes. Test output is fed back only when tests fail, enabling autonomous test-driven development.

### Shell Block Execution

When enabled (`-shell`), the model can execute read-only shell commands (ls, cat, grep, git status, go test, etc.) from an allowlist. Destructive commands (rm, mv, chmod, git commit, etc.) and output redirection are blocked.

### Container Isolation

On Linux, `tai` re-executes itself in a user namespace (`CLONE_NEWUSER` and `CLONE_NEWNS`) to isolate filesystem access, preventing AI-driven code generation from writing outside the intended project boundary.

## Architecture

```
cmd/tai/          CLI entry point and command definitions
  ai.go           Interactive chat with memory
  go.go           Go code generation (default in Go modules)
  any.go          Arbitrary text file generation
  next.go         Autonomous next-step execution
  patch.go        Offline diff application
  ping.go         Model reachability test
  ai_components.go    AI command component set
  ai_system_prompt.go  AI system prompt assembly
  files.go        File-to-parts conversion
generators/       Model generator implementations
  gemini.go       Google Gemini
  open_ai.go      OpenAI-compatible
  ...             Other providers
blocks/           Boundary-delimited block parsing and processing
  block.go        Core block parser
  parser_state.go  Streaming block state
  continue.go     Continue block processing
  finish.go       Finish block prompt
  gotest.go       Go-test block processing
  shell.go        Shell block processing
  request_context.go  Dynamic context fetching
  summary.go      Summary block processing
changes/          Change block application
  apply.go        Declaration-level edit logic
  file_store.go   In-memory and root file stores
  parse.go        Change block parsing
  boundary_diff.go  Diff file streaming
codes/            Code generation pipeline
  generate.go     Main generation loop with retry
  components.go    Codes component set
  system_prompt.go  System prompt assembly
components/       Component framework
  component.go    Unified extension mechanism
  common_components.go  Shared shell/continue components
configs/          CUE configuration loading
flags/            Command-line flag parsing
generators/       Model abstraction layer
gocodes/          Go-specific code provider
anytexts/         General-purpose code provider
memories/         Per-model memory profiles
states/           State layers (thoughts summarization)
taiconfigs/       tai-specific configuration schema
```
