# tai

## Core Philosophy

**Staged context.** The initial context is an outline: `go doc` declaration surfaces for focus packages, theory constants and code comments that state design intent, system prompts, task instructions, and file listings. The model fetches the detail it needs with `go-src` and `ingest` blocks, and each fetch arrives as user content in the next round, so a task advances over multiple rounds — component rounds, continue blocks, retries, and plan-driven rounds — until it is done. Pruning removes irrelevant files, simplification assigns package-level visibility, and token budgeting caps how much one round carries.

The system still rejects dialogue-grown context: detail arrives through structured fetch blocks, never through accumulating conversation, so a run carries no conversational overhead and the outline stays cacheable. One generation also does far more than a tool call — the block protocol charges no round trip per block, so a single response may carry any number of blocks (changes, fetches, test runs, verification requests), and the outcomes arrive together in the next round. Batching keeps the total round count low.

**Doc-first context with on-demand source.** Two poles bound the design space: full-source context misses no detail but is token-heavy and dilutes attention; agentic exploration via semantic search is cheap but misses details and never grasps the whole architecture. The system takes the middle path: focus packages enter the initial context as `go doc` documentation — the complete declaration surface — and the model pulls implementation source on demand with `go-src` blocks, targeted at symbols it can already see rather than found by search. No detail is unreachable; no token is spent on code the task never reads.

**Prefix cache stability.** The system treats the LLM prefix cache as a first-class performance concern. Files are sorted in three tiers — non-root-module files first, root-module context files second, root-module focus files last — so that editing a focus file never shifts the position of any context file. Function declarations are globally sorted by name. Required schema fields are alphabetized. Context simplification uses a deterministic token budget derived from the focus package size, so context files are simplified to the same level for identical focus content across requests. When focus files change, all preceding content remains byte-identical and fully cacheable. Dynamic content — the current time, the memory profile, the user input, and the goal loop feedback — is placed at the end of its prompt so that static sections remain in the cached prefix.

**Software as theory.** The codebase carries its design rationale in `Theory` constants — global string variables with descriptive names like `TheoryOfContextPhilosophy`, `TheoryOfInMemoryApply`, `TheoryOfPrefixCaching`. These constants document why decisions were made, not just what the code does. They evolve incrementally alongside the code. The theory is the project's primary competitive advantage: a deep, documented mental model that guides every change.

**In-memory apply with filesystem consistency.** Change blocks are applied to an in-memory store during streaming, not directly to disk. If a change block fails — invalid target, malformed code — generation stops immediately and the in-memory store is discarded. Only after a generation succeeds are changes flushed to disk in a single batch. The disk is never left in a partially modified state by an interrupted round.

**Security by isolation.** On Linux, the tool re-executes itself in a user namespace with read-only-everything filesystem hardening. Only the current working directory, Go toolchain directories, the user config directory, `/tmp`, and `/dev/shm` are writable. Shell block execution permits any program; AST-level parsing filters only common destructive patterns such as `rm -rf /`. Focus files outside writable directories are marked read-only at collection time.

## What It Is

`tai` is a general-purpose AI tool. It sends context — files, user input, or arbitrary text — to an AI model and applies the model's output to your working tree. It supports multiple AI providers and runs in a sandboxed environment.

The default command is auto-detected: inside a Go module it generates Go code via goal loops with the Go parts provider; outside one it generates changes for arbitrary text files. Subcommands cover interactive AI chat with persistent user profiles (`ai`), text-output tasks on any input (`next`), and boundary-delimited diff application (`patch`). Not all of these involve code.

## Installation

```
go install github.com/reusee/tai/cmd/tai@latest
```

## Commands

| Command | Description |
|---------|-------------|
| `tai` (default) | Auto-detected: Go code generation via goal loops inside a Go module, arbitrary text file generation otherwise |
| `tai ai` | Start an interactive AI chat session with memory |
| `tai next` | Answer a task from the assembled context (text output; no file changes) |
| `tai patch` | Apply a boundary-delimited diff file to the working tree |
| `tai ping` | Test whether a model is reachable |
| `tai record` | List, show, and analyze recorded interaction sessions |

## Usage Examples

Interactive AI chat with persistent user profiles:

```
tai ai -model gemini-pro
```

Text-output task on arbitrary input:

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

Text-output task execution:

```
tai next -file main.go chat "explain the nil pointer dereference in the init function"
```

### Terminal UI

Every command runs in a terminal UI by default when stdout is a terminal: a Tree tab for the run's session tree (the pipeline yields the full tree after every recorded occurrence, so the tab renders — and projects — the same tree the pipeline writes), an Output tab for model output, and a Logs tab for log records. The Tree tab starts expanded and focused; the Output and Logs tabs stay collapsed until their first content arrives. Hovering a collapse/expand marker — a Tree node's fold glyph or an Output section's fold glyph — renders it reversed. When stdout is redirected to another program or a file (for example `tai next | tee .AI`), the default is plain command-line output: the TUI discards stdout, so a piped consumer would receive nothing. The `-tui` flag forces the TUI explicitly; the `-cli` flag switches to plain command-line output.
## Configuration

Configuration is loaded from CUE files (`tai.cue` or `.tai.cue`) in the working directory, at the root of the Go module (when the working directory is inside a Go module), in the user config directory, and in `/etc`. Command-line flags override config file values.

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
| `-doc` | Add a package whose documentation (go doc -all -cmd) is included in the context |
| `-all-src` | Include full source of focus packages, including tests, in the context |
| `-shell` | Enable shell block execution |
| `-stdin` | Add standard input content to the chat messages |
| `clean` | Add the code cleanup prompt to the chat messages |
| `-apply` / `-no-apply` | Control whether change blocks are applied |
| `-no-memory` | Disable user profile memory persistence |
| `-record` | Record interaction sessions for self-improvement analysis |
| `-review` | Run a review loop after generation to review and fix changes |
| `-thoughts` / `-no-thoughts` | Control reasoning thought visibility |
| `-summarize-thoughts` | Enable periodic summarization of thoughts |
| `-summary-language` | Set the output language for summary blocks |
| `-confidential` | Restrict model selection to zero-data-retention models |
| `-pkg` / `-load` | Add a Go package loading pattern (focus packages) |
| `-ctx` / `-dep` | Add a context package pattern for dependency analysis |
| `-match` | Match files by regex pattern for inclusion |
| `-tui` | Force the terminal UI (the default when stdout is a terminal; a redirected stdout defaults to CLI) |
| `-cli` | Use the plain command-line interface, disabling the TUI |

## Architecture

### Packages

| Package | Responsibility |
|---------|----------------|
| `cmd/tai` | Command definitions and entry point |
| `generators` | AI model abstraction (Gemini, OpenAI-compatible) |
| `pipeline` | Generation loop, generation pipeline, and state layers |
| `gotools` | Go-specific parts provider, simplification, and the Go block kinds (go-test, go-src) |
| `anytexts` | General-purpose text file parts provider |
| `changes` | Change block parsing and application |
| `blocks` | Heredoc block format parsing |
| `components` | Component mechanism for block processing |
| `tree` | Immutable session tree (path-copying writes) |
| `configs` | CUE configuration loading |
| `flags` | Command-line flag parsing |
| `security` | Container isolation and shell security |
| `pathutil` | Path safety utilities |
| `nets` | HTTP client and proxy support |
| `logs` | Structured logging |
| `debugs` | Debug tap (Starlark REPL) |
| `memories` | Per-model user profile persistence |
| `records` | Interaction recording and self-improvement analysis |

### Block Format

The model emits structured output as heredoc-delimited blocks. Each block has a kind (a function name), parameters, and a body:

```
<<貞觀 change(op="MODIFY", target="Foo", file-path="/path/to/file.go")
func Foo() {
    // modified code
}
貞觀
```

Block kinds: `change`, `shell`, `go-test`, `go-src`, `continue`, `summary`, `ingest`, `memory`, `done`, `new-plan`, `response`, `plan-op`.

### Session Tree

Every operation of a run — user input, model response, summary, blocks, block results, errors, round feedback, and idle input — is expressed as a write to one immutable session tree (the `tree` package). Writes use path copying: a write copies only its path to the root, so untouched subtrees are shared by pointer, and the tree never joins the generation state chain. After a successful attempt the loop writes the response node, one summary node per summary body, and one validated batch of block nodes; each block's execution result hangs under it as a block-result child. A block header may carry an optional `parent` parameter; the `new-plan` and `response` block kinds must carry both `parent` and `name`. Node names are validated by the program: a duplicate name or an unknown parent discards the whole block batch and is fed back as a System note. A plan is revised by aborting the old plan node (an abort child records who and why) and writing a new one, never by mutation. The loop's own bookkeeping — generator specs, finish reasons, token usage, truncations, retries, handoffs, completions, continuations, thought summaries, and the terminal error — joins the same tree as event nodes, and the goal runner's verdicts as goal structure nodes, and every event write yields the full tree to the consumer, so a display front-end renders the same tree the pipeline writes. Every round-triggering feedback closes with the session tree outline, so the model sees the session's structure. Change blocks carry no `parent` or `name` header — they are recorded post hoc with auto names — and are the one exception.

### Context Pipeline

1. Go packages are loaded via `go/packages` with lightweight modes (no type checking)
2. Files are sorted by module → package → distance → path for cache stability
3. Focus packages are included as `go doc -all -cmd -u` documentation with their test-function names and source file names; implementation source is fetched on demand via go-src blocks. Non-Go focus files (embed, markdown) are listed by name in the package's file list and fetched on demand via `ingest` blocks; markdown files at the module root are listed in a separate part. Files explicitly requested via `-file` are appended at full content last. With `-all-src`, focus packages are included as full source code, including tests, instead of documentation
4. Context packages are assigned a package-level visibility (invisible, short documentation, package documentation, code without tests, or full content) to fit a dynamic token budget derived from the focus documentation size: focusTokens / 4, rounded to the nearest 32K multiple, floored at 32K
5. Extra files from `-file` patterns are appended after focus files

### Generation Loop

Each generation wraps the state with a `ParserState` that collects blocks during streaming. After the generation, components process collected blocks. If a component produces parts or modifies state, a new generation starts. When no component triggers, the loop ends (or prompts for input in interactive mode). The plan tree is always available: the model decides whether to plan — a simple task is done directly; a non-simple task is decomposed into `plan-op` entries and the program feeds the next pending plan entry as each round's feedback. Continue blocks remain the model's way of prompting the next round's user input.
Block kinds that are not available in a session are announced as disabled in the system prompt (for example shell blocks without `-shell`, or the pipeline block kinds in `tai ai`), so the model does not emit blocks that would be silently ignored.

### State Immutability

All state implementations are immutable. `AppendContent` and `Flush` return new state instances. This enables snapshot-based retry: a failed generation attempt does not corrupt the pre-generation state.

## Development

```
git clone https://github.com/reusee/tai.git
cd tai
go test ./...
```
