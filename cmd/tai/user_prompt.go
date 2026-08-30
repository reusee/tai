package main

import (
	"maps"
	"slices"
	"strings"

	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
)

type UserPrompt []generators.Part

// TheoryOfUserPromptFileContext documents the two file-context regimes of
// the UserPrompt provider. See the constant body for the rule.
const TheoryOfUserPromptFileContext = `
UserPrompt assembles file context under two regimes. Directory-scoped
consumers (next, and the any default through the pipeline's own prompt
assembly) treat an empty -file set as the current working directory:
anytexts.PartsProvider.IterFiles replaces empty patterns with ".", so the
whole directory enters the context without an explicit pattern. The ai
command is a direct-conversation command: its Defs fork
UserPromptDirectoryFallback to false, and Module.UserPrompt then skips the
parts provider entirely when no -file pattern is given — no directory
scan, no working directory hint, and no chat bracketing copy, because
there is no context to bracket; the prompt is the thresholded system
prompt restate alone — omitted when the assembled prompt sits within
components.SystemPromptRestateThreshold — and the command appends its
user input marker after it. Explicit -file patterns behave identically in
every command: the patterns are sorted for deterministic prompt bytes and
rendered under the token budget.
`

// UserPromptDirectoryFallback selects the meaning of an empty -file set
// for Module.UserPrompt: true passes empty patterns through to the parts
// provider, whose IterFiles replaces them with "." (the whole current
// working directory); false skips the provider entirely, so the user
// prompt carries files only when the user names them with -file. The
// default is true, preserving the directory-scoped commands (next); the
// ai command forks the value to false because it is a
// direct-conversation command. See TheoryOfUserPromptFileContext and
// TheoryOfAiCommand.
type UserPromptDirectoryFallback bool

func (Module) UserPrompt(
	partsProvider anytexts.PartsProvider,
	getDefaultGenerator generators.GetDefaultGenerator,
	systemPrompt SystemPrompt,
	maxTokens flags.MaxTokens,
	flagFiles flags.Files,
	flagChats flags.Chats,
	directoryFallback UserPromptDirectoryFallback,
) UserPrompt {

	// File patterns come from a map (flags.Files); Go map iteration
	// order is randomized per range, so the keys must be sorted before
	// reaching Parts. Pattern order reaches the prompt bytes: IterFiles
	// enqueues pattern matches in order and deduplicates followed symlink
	// targets by first-wins, so two aliases of one directory yield
	// different file paths depending on which pattern is seen first.
	// Sorting makes the emitted file set — and therefore the prompt
	// prefix — byte-identical across runs with equal configuration.
	// pipeline.Module.Patterns sorts the same map for the codes pipeline;
	// see TheoryOfPrefixCaching in generators/state_func_map.go.
	patterns := slices.Collect(maps.Keys(flagFiles))
	slices.Sort(patterns)

	// File context is assembled only when a -file pattern is given or the
	// directory fallback is enabled. With the fallback disabled (the ai
	// command) an empty -file set means no file context at all: no
	// directory scan, no working directory hint, and no chat bracketing
	// copy. See TheoryOfUserPromptFileContext.
	var parts []generators.Part
	if len(patterns) > 0 || bool(directoryFallback) {

		generator, err := getDefaultGenerator()
		ce(err)

		args := generator.Spec()
		maxInputTokens := min(
			args.ContextTokens,
			int(maxTokens),
		)
		maxGenerateTokens := 8192
		if args.MaxGenerateTokens != nil {
			maxGenerateTokens = *args.MaxGenerateTokens
		}
		maxInputTokens -= maxGenerateTokens
		systemPromptTokens, err := generator.CountTokens(string(systemPrompt))
		ce(err)
		// The system prompt is charged twice: once as the actual system
		// prompt, and once for the verbatim restate that the user prompt
		// carries when it exceeds the restate threshold
		// (components.SystemPromptRestateForUserPrompt). The charge is
		// unconditional and therefore conservative when the restate is
		// omitted: whether the restate appears depends on the assembled
		// size, which is known only after assembly. See
		// components.TheoryOfComponents.
		maxInputTokens -= systemPromptTokens * 2

		// The chat input precedes the parts provider content when -chat
		// arguments are given: the model reads the task before the long file
		// context, while the restate after the context re-exposes the rules
		// immediately before generating. The copy exists only when file
		// context is assembled — it brackets the provider content; without
		// file context there is nothing to bracket, and the command's user
		// input marker (appended after the restate) is the only carrier of
		// the -chat text. The part ends with a blank line so the context
		// starts a fresh paragraph. See pipeline.TheoryOfChatBracketing and
		// generators.TheoryOfContentUnitSeparation.
		if chats := strings.Join(flagChats, "\n"); chats != "" {
			parts = append(parts, generators.Text(chats+"\n\n"))
		}
		providerParts, err := partsProvider.Parts(
			maxInputTokens,
			generator.CountTokens,
			patterns,
		)
		ce(err)
		parts = append(parts, providerParts...)

		// The system prompt restate is the last user prompt part before
		// the dynamic user input, appended only when the assembled user
		// prompt exceeds the restate threshold: the restate re-exposes
		// the complete rules across long intervening content, and a user
		// prompt within the threshold leaves the system prompt close to
		// the generation point, so the verbatim copy is omitted and its
		// tokens saved. The part ends with a blank line so the user input
		// that follows starts a fresh paragraph. See
		// components.TheoryOfComponents,
		// components.SystemPromptRestateThreshold and
		// generators.TheoryOfContentUnitSeparation.
		restateParts, _, err := components.SystemPromptRestateForUserPrompt(
			parts,
			string(systemPrompt),
			generator.CountTokens,
		)
		ce(err)
		parts = append(parts, restateParts...)

	}

	return UserPrompt(parts)
}

func (Module) UserPromptDirectoryFallback() UserPromptDirectoryFallback {
	return true
}
