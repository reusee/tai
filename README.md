# tai

## Installation

```sh
go install github.com/reusee/tai/cmd/...@latest
```

This installs five commands: `ai`, `next`, `gotai`, `anytai`, `taipatch`.

## Commands

### `ai` — Interactive Chat AI

An interactive AI assistant with persistent memory, optional shell command execution, and multi-round self-driven generation.

```sh
# basic chat
ai

# provide context files
ai -file main.go -file config.go

# chat with an initial prompt
ai chat "explain this codebase"

# pipe input via stdin
cat error.log | ai

# enable shell command execution (AI can run tests, builds, etc.)
ai -shell

# disable memory persistence
ai -no-memory
```

The `ai` command supports **shell blocks** (AI executes shell commands and receives output) and **continue blocks** (AI chains multiple generation rounds for long outputs). Both require the `-shell` flag for shell blocks; continue blocks are always available.

### `next` — Next Step AI

An AI that analyzes code and proposes next steps (find bugs, plan refactors, suggest improvements).

```sh
# analyze Go files
next -file ./...

# focus on specific aspects
next -focus "error handling" -focus "concurrency"

# ignore certain aspects
next -ignore "stylistic" -ignore "naming"

# match files by regex pattern
next -match "\.go$"
```

### `gotai` — Go Code Generation

Generates Go code with full package analysis context. Automatically loads dependent packages, simplifies context to fit token budgets, and applies changes immediately.

```sh
# generate code for the current directory
gotai

# specify load patterns
gotai -load ./... -ctx ./internal/...

# exclude test files
gotai -no-tests

# limit import distance
gotai -load ./... -max-distance 1
```

### `anytai` — General Code/Text Generation

Generates code or text for any file type, without Go-specific analysis.

```sh
anytai
```

### `taipatch` — Apply AI Patches

Applies a `.AI` diff file (containing boundary-delimited change blocks) to the working tree.

```sh
taipatch
```

## Configuration

Configuration is loaded from CUE files. The loader searches these locations in order:

1. Working directory: `tai.cue`, `.tai.cue`
2. User config directory (`~/.config/`): `tai.cue`, `.tai.cue`
3. System-wide: `/etc/tai.cue`, `/etc/.tai.cue`

Later files do not override earlier ones; values are looked up in file order (first match wins).

### Minimal Configuration

```cue
// tai.cue
model: "gemini-pro"

google_api_key: "your-api-key"
```

### Model Configuration

```cue
model: "gemini-pro"

generators: [
    {
        name: "my-model"
        type: "openai"
        model: "gpt-4o"
        api_key: "sk-..."
        context_tokens: 128000
        max_generate_tokens: 16384
        temperature: 0.1
        aliases: ["gpt4", "4o"]
        variants: [
            {
                name: "fast"
                model: "gpt-4o-mini"
            }
        ]
    },
]
```

Model names resolve through a hierarchical path system. A model path like `my-model/fast` merges fields from parent to child. Aliases provide shorthand names. The `redirect` field extends or replaces the path (relative appends, absolute with `/` replaces from root).

### Provider Types

| Type | Description |
|------|-------------|
| `gemini` | Google Gemini API |
| `openai` | OpenAI API |
| `deepseek` | Deepseek API |
| `open-router` / `openrouter` | OpenRouter API |
| `azure` | Azure OpenAI |
| `ollama` | Local Ollama |
| `baidu` | Baidu Qianfan |
| `tencent` | Tencent Hunyuan |
| `huoshan` | Volcengine Ark |
| `aliyun` | Aliyun DashScope |
| `zhipu` | Zhipu GLM |
| `vercel` | Vercel AI Gateway |
| `nvidia` | NVIDIA NIM |
| `bedrock` | AWS Bedrock |
| `opencode-go` | OpenCode Go |

### API Keys

API keys are resolved from config first, then environment variables:

```cue
openai_api_key:      "sk-..."
google_api_key:      "AIza..."
deepseek_api_key:    "sk-..."
open_router_api_key: "sk-or-..."
anthropic_api_key:   "sk-ant-..."
huoshan_api_key:     "..."
baidu_api_key:       "..."
tencent_api_key:     "..."
aliyun_api_key:      "..."
zhipu_api_key:       "..."
vercel_api_key:      "..."
nvidia_api_key:      "..."
azure_api_key:       "..."
aws_bedrock_api_key: "..."
opencode_go_api_key: "..."
```

Or via environment variables: `OPENAI_API_KEY`, `GOOGLE_API_KEY`, etc.

### Proxy

```cue
proxy_addr: "socks5://127.0.0.1:1080"
// or
http_proxy: "http://127.0.0.1:7890"
```

Proxy is disabled in development mode. Set `no_proxy: true` on a generator spec to bypass the proxy for that specific model.

### Go Settings

```cue
go: {
    load_dir: "."
    load_patterns: ["./..."]
    context_patterns: ["./internal/..."]
    max_distance: 2
    no_tests: false
    envs: ["GOPROXY=https://goproxy.cn"]
}

// top-level alias for go.envs
go_envs: ["GOPROXY=https://goproxy.cn"]
```

### Token Limits

```cue
max_context_tokens: 100000
// max_tokens is deprecated, use max_context_tokens
```

### Extra System Prompt

```cue
extra_system_prompt: "Always respond in English."
```

### Custom Functions

Define functions available to the AI as tools:

```cue
functions: [
    {
        name: "get_weather"
        description: "Get current weather for a city"
        params: [
            { name: "city", type: "string", description: "City name" }
        ]
        returns: [
            { name: "temperature", type: "number" }
        ]
    }
]
```

### Logging

```cue
log_level: "debug" // debug | info | warn | error
```

## Flags

### Common Flags

| Flag | Description |
|------|-------------|
| `-model <name>` | Set the model name |
| `-file <path>` | Include a file as context (repeatable) |
| `-ignore <pattern>` | Exclude files matching pattern (alias: `-skip`, `-exclude`) |
| `-focus <aspect>` | Focus on specific aspects (repeatable) |
| `-match <regex>` | Filter files by regex (alias: `-include`) |
| `-max-tokens <n>` | Max context tokens |
| `-temperature <f>` | Set temperature |
| `-effort <level>` | Set reasoning effort level |
| `-thoughts` | Show AI reasoning |
| `-no-thoughts` | Hide AI reasoning |
| `-debug` | Enable verbose logging |

### Go-Specific Flags

| Flag | Description |
|------|-------------|
| `-load <pattern>` | Load patterns (alias: `-pkg`) |
| `-ctx <pattern>` | Context patterns (alias: `-dep`) |
| `-max-distance <n>` | Max import distance (default: 2) |
| `-no-tests` | Exclude test files |
| `-include-std` | Include standard library |
| `-load-dir <path>` | Root directory for loading |

### Feature Flags

| Flag | Command | Description |
|------|---------|-------------|
| `-shell` | `ai`, `gotai` | Enable shell command execution from AI output |
| `-plan` | `gotai` | Enable mandatory planning and multi-round generation |
| `-dynamic-context` (`-dyn`) | `gotai` | Enable request-context blocks |
| `-no-apply` | `gotai` | Disable immediate apply of change blocks |

## Block Format (For AI Callers)

The `tai` tool uses a boundary-delimited block format for structured AI output. Blocks are delimited by `:::<kind> <boundary>` and `:::end <boundary>` markers, where `<boundary>` is a random string of two uncommon Chinese characters.

### Change Blocks

Code modifications use XML metadata:

```
:::change <boundary>
<change op="MODIFY|ADD_BEFORE|ADD_AFTER|DELETE|RENAME|WRITE" target="<identifier>" file-path="<path>" />
<complete code>
:::end <boundary>
```

- **MODIFY**: Replace an existing top-level declaration
- **ADD_BEFORE** / **ADD_AFTER**: Insert before/after a declaration
- **DELETE**: Remove a declaration
- **RENAME**: Rename a file (`target` is new path)
- **WRITE**: Replace entire file content

Each block targets exactly ONE top-level declaration. For a struct with methods, use separate blocks for the type and each method.

### Finish Block

Signals completion with a one-sentence summary:

```
:::finish <boundary>
Fixed the Foo function and removed unused Bar.
:::end <boundary>
```

### Summary Block

Describes the current round's reasoning and actions. Emitted once per round, before finish/continue blocks:

```
:::summary <boundary>
Analyzed the code and identified a race condition in the counter.
:::end <boundary>
```

### Continue Block

Triggers a new generation round by feeding the body back as the next user message. Must be the last block when used:

```
:::continue <boundary>
<next user message content>
:::end <boundary>
```

### Shell Block

Executes a shell command and feeds output back as user content. Requires the `-shell` flag:

```
:::shell <boundary>
go test ./...
:::end <boundary>
```

### Request-Context Block

Requests additional context (files, URLs, file listings) during generation. Requires the `-dynamic-context` flag:

```
:::request-context <boundary>
<file path="src/main.go" />
<fetch addr="https://example.com/api" />
<glob pattern="src/**/*.go" />
:::end <boundary>
```

### Memory Block (ai command)

Updates the persistent user profile:

```
:::memory <boundary>
<memory>
  <memory-item>用户偏好简洁的回答</memory-item>
  <memory-item>用户使用Go语言</memory-item>
</memory>
:::end <boundary>
```

### Block Rules

1. Opening and closing markers must appear at the **beginning of a line**.
2. Each block uses a **unique random boundary** — never reuse example boundaries.
3. Boundary characters must not appear in the block body.
4. The closing marker must use the **exact same boundary** as the opening marker.
5. No blank lines are required before or after blocks.

## File Context Markers

Files provided to the AI are wrapped with markers:

```
``` begin of file <path>
<content>
``` end of file <path>
```

Binary files include the MIME type:

```
``` begin of file <path> (binary, image/png)
<binary content>
``` end of file <path>
```

Files introduced via symbolic links to external locations are marked as read-only:

```
``` begin of file <path> (read-only)
``` 

Read-only files must not be modified via change blocks.
