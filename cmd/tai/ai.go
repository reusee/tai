package main

import (
	"context"
	"os"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/memories"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/phases"
	"github.com/reusee/tai/vars"
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
each generation round to merge newly learned items into the profile.

The buf Output layer uses showThoughts=false so model reasoning (Thought parts)
is excluded from the buffer used for memory block parsing. Thoughts may contain
illustrative block markers that would interfere with memory block extraction.
The terminal Output (os.Stdout) retains showThoughts=true so the user still
sees reasoning content on screen.

Shell and Continue Blocks:
Shell blocks allow the model to execute shell commands and receive the output
as part of the next generation round. This enables autonomous testing, build
verification, and codebase exploration. Shell block execution is disabled by
default for safety; the -shell flag enables it.

Continue blocks allow the model to self-drive multi-turn generation by emitting
a continue block when the task is not yet complete. The system parses the
continue block, extracts its body as the next user message, and automatically
starts a new generation round. This enables the model to produce arbitrarily
long outputs by chaining multiple rounds.

All block kinds are wired through the Component mechanism (see
TheoryOfAIComponents), which couples each block kind's system prompt with its
processing function. The component list is shared between AISystemPrompt (prompt
assembly) and this generation loop (output processing), ensuring that any block
kind introduced in the prompt always has a matching processor. Shell and continue
blocks are processed in the loop via components.ProcessComponents, which
accumulates Parts into a single user message for the next round; memory blocks
are processed after the loop by memories.UpdateMemoryFromBlock.

Block Collection:
Blocks are collected by a BlockHandler callback set on ParserState during
generation. The handler appends each parsed block to an external collectedBlocks
slice. After generation, collectedBlocks are passed to ProcessComponents, which
filters by kind and dispatches to the appropriate component. Remaining blocks
(not matched by any component) are carried forward to the next cycle. This
eliminates the need for ParserState to store blocks or for reconciliation
between the state chain and block storage. See blocks.TheoryOfParserState and
components.TheoryOfComponents.
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
		currentMemory memories.CurrentMemory,
		appendMemory memories.AppendMemory,
		buildGenerate phases.BuildGenerate,
		buildChat phases.BuildChat,
		generator generators.Generator,
		flagFiles flags.Files,
		flagChats flags.Chats,
		noMemory NoMemory,
	) {
		ctx := context.Background()

		input := strings.Join(flagChats, "\n")

		stdin := getStdinContent()
		if len(stdin) > 0 {
			input = input + "\n" + string(stdin)
		}
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
			fileParts, err := filePathToParts(filePath)
			ce(err)
			parts = append(parts, fileParts...)
			logger.Info("file",
				"path", filePath,
			)
		}

		// Component user prompt parts are appended after file context,
		// before the user's input. See TheoryOfAIComponents and
		// components.TheoryOfComponents.
		parts = append(parts, comps.UserPromptParts()...)

		// User input is wrapped with markers so the model can distinguish
		// between reference file context and the task request.
		// See TheoryOfContextStructure in files.go.
		parts = append(parts, generators.Text(
			"\n``` begin of user input\n"+vars.FirstNonZero(input)+"\n``` end of user input\n",
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
		baseState = generators.NewOutput(baseState, os.Stdout, true).WithTools(false)
		// buf captures assistant text for memory block parsing.
		// showThoughts=false excludes Thought parts so model reasoning
		// (which may contain illustrative block markers) does not
		// interfere with memory block extraction.
		// See TheoryOfAiCommand.
		baseState = generators.NewOutput(baseState, buf, false).WithTools(false)

		// collectedBlocks stores blocks parsed during generation.
		// The BlockHandler appends to this slice; remaining blocks
		// after ProcessComponents are carried forward to the next cycle.
		// See TheoryOfAiCommand.
		var collectedBlocks []blocks.Block

		// Generation loop with block processing via components.
		// The component list couples each block kind's prompt with its
		// processing function, ensuring prompt-processing parity.
		// See TheoryOfAIComponents and components.TheoryOfComponents.
		for {
			// Handler collects all blocks for post-generation processing.
			handler := func(block blocks.Block) error {
				collectedBlocks = append(collectedBlocks, block)
				return nil
			}

			parserState := blocks.NewParserState(baseState, handler)
			state := generators.State(parserState)

			phase := buildGenerate(generator, nil)(
				buildChat(generator, nil)(
					nil,
				),
			)
			for phase != nil {
				phase, state, err = phase(ctx, state)
				ce(err)
			}

			// Unwrap ParserState to get the base state for the next
			// cycle. The state is already flushed by Generate.
			if ps, ok := generators.As[*blocks.ParserState](state); ok {
				baseState = ps.Unwrap()
			} else {
				baseState = state
			}

			// Process blocks via components. See components.TheoryOfComponents
			// and components.ProcessComponents.
			var combinedParts []generators.Part
			var triggered bool
			collectedBlocks, baseState, combinedParts, triggered, err = components.ProcessComponents(
				ctx, comps.ComponentSet, collectedBlocks, baseState, nil, nets.HTTPClient{}, nil, false,
			)
			ce(err)

			if triggered {
				if len(combinedParts) > 0 {
					baseState, err = baseState.AppendContent(&generators.Content{
						Role:  "user",
						Parts: combinedParts,
					})
					ce(err)
				}
				continue
			}

			break
		}

		// update memory from block
		if !noMemory {
			if err := memories.UpdateMemoryFromBlock(
				currentMemory,
				appendMemory,
				memories.GetModelID(generator.Spec()),
				buf.String(),
			); err != nil {
				logger.ErrorContext(ctx, "update memory", "err", err)
			}
		}

	},
}
