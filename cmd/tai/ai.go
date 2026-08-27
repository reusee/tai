package main

import (
	"context"
	"os"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/memories"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/pipeline"
	"github.com/reusee/tai/records"
)

const TheoryOfAiCommand = `
Memory and Tool Usage:
The AI's memory is a persistent per-model user profile (ai-memory.json) that is
fed into the system prompt for long-term context. The full memory
implementation — profile storage with advisory locking and atomic writes,
memory block parsing, the textual pseudo-call fallback, and the fact-only
profiling policy — lives in the memories package (see memories.TheoryOfMemory).
This command wires memories into the dscope graph, feeds the current profile
into the memory Component's prompt section (assembled into the system prompt
via comps.PromptSections), and invokes memories.UpdateMemoryFromBlock after
each attempt to apply memory additions and deletions: deletions
remove items by exact string match and win over a same-attempt addition. The
memory prompts teach both the memory-item and memory-delete elements.

The buf Output layer uses showThoughts=false so model reasoning (Thought parts)
is excluded from the buffer used for memory block parsing. Thoughts may contain
illustrative block markers that would interfere with memory block extraction.
The terminal Output (os.Stdout) shows reasoning content on screen unless
-summarize-thoughts is enabled, in which case the stdout layer suppresses raw
thoughts and periodic summaries replace them (see the Thought Summarization
paragraph below).

Shell Blocks and Interactive Input:
Shell blocks allow the model to execute shell commands and receive the output
as part of the next generation, enabling autonomous testing, build
verification, and codebase exploration. Shell block execution is disabled by
default for safety; the -shell flag enables it.

The continue block is deliberately not part of the ai command. In an
interactive chat the user's next input arrives through OnIdle
(pipeline.BuildChatIdle) after the attempt ends; a continue component would feed
the model's own body back as user content, bypassing the prompt and allowing
the model to emit meaningless self-prompts such as "Please provide the next
task or user input." See TheoryOfAIComponents.

Shell and memory blocks are wired through the Component mechanism (see
TheoryOfAIComponents), which couples each block kind's system prompt with its
processing function. The component list is shared between AISystemPrompt (prompt
assembly) and this generation loop (output processing), ensuring that any block
kind introduced in the prompt always has a matching processor. Shell blocks
are processed in the loop via components.ProcessComponents, which
accumulates Parts into a single user message for the next generation; memory blocks
are processed after each attempt via the OnAttemptSuccess hook, which calls
memories.UpdateMemoryFromBlock before the user is prompted for the next input.

Block Collection:
Blocks are collected by a BlockHandler callback set on ParserState during
generation. The handler appends each parsed block to an external collectedBlocks
slice. After generation, collectedBlocks are passed to ProcessComponents, which
filters by kind and dispatches to the appropriate component. Remaining blocks
(not matched by any component) are carried forward to the next cycle. This
eliminates the need for ParserState to store blocks or for reconciliation
between the state chain and block storage. See blocks.TheoryOfParserState and
components.TheoryOfComponents.

Automated Actions Before Interactive Input:
The generation loop processes automated actions (shell blocks) and persists
memory updates before prompting the user for interactive input. The PhaseBuilder includes only
the generate phase (not chat); the chat prompt is handled by OnIdle, which is
invoked by the loop as a fallback when no component triggers. This ensures the
model can chain multiple generations of autonomous shell execution without user
intervention, and the user is only prompted when the model has no pending
automated actions. See pipeline.TheoryOfIdleHandler and pipeline.TheoryOfLoops.

User Prompt Ordering and Prefix Cache:
The user prompt places file context first, then the verbatim system prompt
restate, and the dynamic user input last. The restate repeats the full system
prompt under a short re-read instruction
(components.SystemPromptRestate), so the model re-reads every rule immediately
before generating, while the static sections stay in the LLM prefix cache:
when the user input changes across sessions, only the final element changes,
and the file context and the restate remain byte-identical and fully
cacheable — the restate varies only when the system prompt itself varies.
This is the same dynamic-content-last principle that places the current time
at the end of the system prompt (see AISystemPrompt) and the memory profile
at the end of the system prompt sections (see TheoryOfAIComponents). See
TheoryOfPrefixCaching in generators/state_func_map.go.

Thought Summarization:
The -summarize-thoughts flag wires pipeline.NewThoughtsSummarize around the
output layers, mirroring the generation pipeline: when enabled, the stdout
Output layer suppresses raw thoughts and the summarizer writes periodic
summaries to the generation output stream (os.Stdout), and each summary also
flows to the run's event stream as an EventThoughtSummary, which the TUI
renders in its Events tab (see pipeline.TheoryOfThoughtsSummarize and
pipeline.TheoryOfLoopEvents). In TUI mode the tuiOutputState decorator still
streams raw thoughts to the Output tab, so both streams are visible
concurrently. The buf layer that captures
assistant text for memory parsing always excludes thoughts, so
summarization never interferes with memory block extraction.
`

var AICommand = Command{
	Defs: []any{
		modes.ForProduction(),
		new(apps.Name("cmd_ai")),
	},
	Main: func(
		logger logs.Logger,
		getSystemPrompt AISystemPrompt,
		comps AIComponents,
		updateMemoryFromBlock memories.UpdateMemoryFromBlock,
		buildGenerate generators.BuildGenerate,
		buildChatIdle pipeline.BuildChatIdle,
		getDefaultGenerator generators.GetDefaultGenerator,
		flagFiles flags.Files,
		nameMatch anytexts.NameMatch,
		flagChats flags.Chats,
		noMemory NoMemory,
		noHuman NoHuman,
		loopRun pipeline.Run,
		recorder *records.Recorder,
		getDefaultSummarizer pipeline.GetDefaultSummarizer,
		summarizeThoughts flags.SummarizeThoughts,
	) {
		ctx := context.Background()

		generator, err := getDefaultGenerator()
		ce(err)

		input := strings.Join(flagChats, "\n")
		logger.InfoContext(ctx, "input", "len", len(input))

		systemPrompt, err := getSystemPrompt()
		ce(err)

		var files []string
		for pattern := range flagFiles {
			paths, err := doublestar.FilepathGlob(pattern)
			if err != nil {
				files = append(files, pattern)
			} else {
				for _, path := range paths {
					info, err := os.Stat(path)
					if err != nil {
						continue
					}
					if info.IsDir() {
						continue
					}
					files = append(files, path)
				}
			}
		}
		sort.Strings(files)

		var parts []generators.Part

		for _, filePath := range files {

			if !nameMatch(filePath) {
				continue
			}
			fileParts, err := filePathToParts(filePath)
			ce(err)
			parts = append(parts, fileParts...)
			logger.Info("file",
				"path", filePath,
			)
		}

		parts = append(parts, comps.UserPromptParts()...)

		parts = append(parts, components.SystemPromptRestate(systemPrompt))

		parts = append(parts, generators.Text(
			"\n``` begin of user input\n"+input+"\n``` end of user input\n",
		))

		var baseState generators.State
		baseState = generators.NewPrompts(
			systemPrompt,
			[]*generators.Content{
				{
					Role:  "user",
					Parts: parts,
				},
			},
		)
		buf := new(strings.Builder)

		outputShowThoughts := !bool(summarizeThoughts)
		baseState = generators.NewOutput(baseState, os.Stdout, outputShowThoughts).WithTools(false)

		baseState = generators.NewOutput(baseState, buf, false).WithTools(false)
		if bool(summarizeThoughts) {
			summarizer, err := getDefaultSummarizer()
			ce(err)
			baseState = pipeline.NewThoughtsSummarize(ctx, baseState, summarizer, os.Stdout)
		}

		prevBufLen := 0

		// When NoHuman is set, OnIdle is nil so the loop ends without
		// prompting for input, enabling unattended operation.
		var onIdle pipeline.IdleHandler
		if !bool(noHuman) {
			onIdle = buildChatIdle(generator, nil)
		}

		// Run the unified generation loop. The PhaseBuilder includes only
		// the generate phase (not chat); the chat prompt is handled by
		// OnIdle, which is invoked by the loop when no component triggers.
		// This ensures automated actions (continue, shell) are processed
		// before prompting the user for input, and memory is persisted
		// after each attempt via OnAttemptSuccess. The interaction
		// recorder is passed explicitly so the session is captured when
		// -record is enabled. The result is filled into result as the run
		// progresses; the iterator yields the run's events, and the
		// terminal error, if any, arrives with the final yield's error
		// component.
		// See pipeline.TheoryOfIdleHandler, pipeline.TheoryOfLoops and
		// pipeline.TheoryOfLoopEvents.
		var result pipeline.Result
		for _, e := range loopRun(ctx, pipeline.RunOptions{
			Generator:           generator,
			InitialState:        baseState,
			Components:          comps.ComponentSet,
			Command:             "ai",
			InteractionRecorder: recorder,
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return buildGenerate(g, nil)(nil)
			},
			OnAttemptSuccess: func(roundState generators.State, summaries []string) error {
				if !noMemory {
					newText := buf.String()[prevBufLen:]
					prevBufLen = buf.Len()
					if err := updateMemoryFromBlock(
						memories.GetModelID(generator.Spec()),
						newText,
					); err != nil {
						logger.ErrorContext(ctx, "update memory", "err", err)
					}
				}
				return nil
			},
			OnIdle:     onIdle,
			HTTPClient: nets.HTTPClient{},
		}, &result) {
			if e != nil {
				err = e
			}
		}
		ce(err)

	},
}
