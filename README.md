# tai

## Core Philosophy

**Single-shot context construction.** The system assembles all context the model needs — file contents, dependency graphs, system prompts, task instructions — before the first generation call. It does not discover context through multi-turn conversation. Pruning removes irrelevant files. Simplification strips function bodies and comments from non-focus packages. Token budgeting caps total input size. The model reasons over the complete picture in one pass and produces changes ready for human review.

This is the opposite of the mainstream agentic pattern where context grows through dialogue. Growing context through dialogue wastes tokens on conversation overhead and produces non-deterministic results. Single-shot construction is deterministic: the same files and the same task always produce the same input to the model.

**Prefix cache stability.** The system treats the LLM prefix cache as a first-class performance concern. Files are sorted in three tiers — non-root-module files first, root-module context files second, root-module focus files last — so that editing a focus file never shifts the position of any context file. Function declarations are globally sorted by name. Required schema fields are alphabetized. Context simplification uses a fixed token budget so that context files are simplified to the same level every request. When focus files change, all preceding content remains byte-identical and fully cacheable. Dynamic content — the current time, the memory profile, the user input, and the goal loop feedback — is placed at the end of its prompt so that static sections remain in the cached prefix.

**Software as theory.** The codebase carries its design rationale in `Theory` constants — global string variables with descriptive names like `TheoryOfContextPhilosophy`, `TheoryOfInMemoryApply`, `TheoryOfPrefixCaching`. These constants document why decisions were made, not just what the code does. They evolve incrementally alongside the code. The theory is the project's primary competitive advantage: a deep, documented mental model that guides every change.

**In-memory apply with filesystem consistency.** Change blocks are applied to an in-memory store during streaming, not directly to disk. If a change block fails — invalid target, malformed code — generation stops immediately and the in-memory store is discarded. Only after a round succeeds are changes flushed to disk in a single batch. The disk is never left in a partially modified state by an interrupted round.

**Security by isolation.** On Linux, the tool re-executes itself in a user namespace with read-only-everything filesystem hardening. Only the current working directory, Go toolchain directories, the user config directory, `/tmp`, and `/dev/shm` are writable. Shell block execution is governed by an AST-level command allowlist. Focus files outside writable directories are marked read-only at collection time.

## What It Is

`tai` is a general-purpose AI tool. It sends context — files, user input, or arbitrary text — to an AI model and applies the model's output to your working tree. It supports multiple AI providers and runs in a sandboxed environment.

While Go code generation is the default command inside Go modules, the tool also handles arbitrary text file editing (`any`), interactive AI chat with persistent user profiles (`ai`), single-shot tasks on any input (`next`), autonomous goal-directed workflows (`goal`), and boundary-delimited diff application (`patch`). Not all of these involve code.

## Installation

```
go install github.com/reusee/tai/cmd/tai@latest
```

## Commands

| Command | Description |
|---------|-------------|
| `tai` (default in Go modules) | Generate code for Go files |
| `tai any` | Generate code for arbitrary text files |
| `tai ai` | Start an interactive AI chat session with memory |
| `tai next` | Execute a single-shot task |
| `tai goal <description>` | Work toward a goal through multiple independent loops |
| `tai patch` | Apply a boundary-delimited diff file to the working tree |
| `tai ping` | Test whether a model is reachable |

## Usage Examples

Interactive AI chat with persistent user profiles:

```
tai ai -model gemini-pro
```

Single-shot task on arbitrary input:

```
tai next -model gemini-pro chat "explain the difference between TCP and UDP"
```

Generate code from a focus file:

```
tai -model gemini-pro -file internal/handler.go -file internal/handler_test.go \
    chat "add input validation to the CreateUser handler"
```

Interactive AI session with memory and shell blocks:

```
tai ai -model gemini-pro -shell
```

Goal-directed autonomous execution:

```
tai goal -model gemini-pro chat "refactor the database layer to use connection pooling"
```

Single-shot task execution:

```
tai next -file main.go chat "fix the nil pointer dereference in the init function"
```

## Configuration

Configuration is loaded from CUE files (`tai.cue` or `.tai.cue`) in the working directory, user config directory, and `/etc`. Command-line flags override config file values.

Example `tai.cue`:

```cue
model: "gemini-pro"
generators: [
    {
        name:  "gemini"
        type:  "gemini"
        model: "models/gemini-pro-latest"
    },
    {
        name:  "deepseek"
        type:  "deepseek"
        model: "deepseek-chat"
    },
]
```

## Supported Providers

Gemini, OpenAI, DeepSeek, Volcano Engine (Huoshan), Baidu, Tencent, Alibaba Cloud, Zhipu, Vercel, NVIDIA, Azure OpenAI, AWS Bedrock, OpenRouter, Ollama, OpenCodeGo.

## Key Flags

| Flag | Description |
|------|-------------|
| `-model` | Set the model name |
| `-fast-model` | Set the fast model for summarization |
| `-file` | Add a file to the context |
| `-shell` | Enable shell block execution |
| `-stdin` | Add standard input content to the chat messages |
| `-plan` | Enable mandatory planning and multi-round generation |
| `-dynamic-context` | Enable dynamic context fetching via request-context blocks |
| `-apply` / `-no-apply` | Control whether change blocks are applied |
| `-no-memory` | Disable user profile memory persistence |
| `-no-human` | Disable interactive chat for unattended operation |
| `-include-std` | Include standard library packages |
| `-thoughts` / `-no-thoughts` | Control reasoning thought visibility |
| `-summarize-thoughts` | Enable periodic summarization of thoughts |

## Architecture

### Packages

| Package | Responsibility |
|---------|----------------|
| `cmd/tai` | Command definitions and entry point |
| `generators` | AI model abstraction (Gemini, OpenAI-compatible) |
| `codes` | Code generation pipeline |
| `gocodes` | Go-specific code provider and simplification |
| `anytexts` | General-purpose text file code provider |
| `changes` | Change block parsing and application |
| `blocks` | Heredoc block format parsing |
| `components` | Component mechanism for block processing |
| `loops` | Unified generation loop |
| `phases` | Phase chain (generate, chat) |
| `states` | State layers (thoughts summarization) |
| `configs` | CUE configuration loading |
| `flags` | Command-line flag parsing |
| `security` | Container isolation and shell security |
| `pathutil` | Path safety utilities |
| `nets` | HTTP client and proxy support |
| `logs` | Structured logging |
| `debugs` | Debug tap (Starlark REPL) |
| `memories` | Per-model user profile persistence |

### Block Format

The model emits structured output as heredoc-delimited blocks. Each block has a kind (XML element name), attributes, and a body:

```
<<徕珑龘 <change op="MODIFY" target="Foo" file-path="/path/to/file.go">
func Foo() {
    // modified code
}
徕珑龘
```

Block kinds: `change`, `shell`, `go-test`, `continue`, `summary`, `request-context`, `memory`.

### Context Pipeline

1. Go packages are loaded via `go/packages` with lightweight modes (no type checking)
2. Files are sorted by module → package → distance → path for cache stability
3. Context files are simplified (comments stripped, function bodies deleted) to fit a fixed 32K token budget
4. Focus files (root package) are appended last
5. Extra files from `-file` patterns are appended after focus files

### Generation Loop

Each round wraps the state with a `ParserState` that collects blocks during streaming. After the round, components process collected blocks. If a component produces parts or modifies state, a new round starts. When no component triggers, the loop ends (or prompts for input in interactive mode).

### State Immutability

All state implementations are immutable. `AppendContent` and `Flush` return new state instances. This enables snapshot-based retry: a failed generation attempt does not corrupt the pre-generation state.

## Development

```
git clone https://github.com/reusee/tai.git
cd tai
go test ./...
```
