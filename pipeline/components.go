package pipeline

import (
	"context"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/gotools"
)

const TheoryOfCodesComponents = `
CodesComponents is a distinct named type embedding components.ComponentSet so that
dscope resolves it independently from other modules' ComponentSet providers (e.g.,
the ai command's AIComponents).

The pipeline reuses components.CommonComponents for the shell and continue
component kinds, prepending its codes-specific components (change, go-test,
go-src, ingest) and appending summary, read-only files (prompt-only),
hidden packages (prompt-only, conditional), mandatory planning (prompt-only,
conditional), and extra system prompt (prompt-only).

The unified block format prompt (blocks.BlockFormatSystemPrompt) is included
as the first prompt-only component: every block-using component set must carry
it, and the kind prompts describe only their kind-specific semantics without
restating the heredoc format. See blocks.TheoryOfBlockFormatGeneral.

When the shell flag is off, the set carries the disabled-blocks notice for
shell (components.DisabledBlocksComponent), so the model is explicitly told
that shell blocks are unavailable instead of finding the shell slot silent.
Under -no-apply the change prompt is still included and change is
deliberately not listed as disabled: the blocks are the deliverable of a dry
run, reviewed by the user, so the model must keep emitting them. See
components.TheoryOfDisabledBlocks.

The go-test component runs Go tests after change blocks are applied. Test
output (stdout and stderr) is always fed back to the model as Parts,
triggering a new generation regardless of whether tests pass or fail: the
model needs the results to decide whether to continue, and withholding
output on pass causes the system to exit prematurely when the model intended
to proceed. The go-test component is placed after change so tests run
against the updated source, and before summary so test output is available
for the next generation.

The go-src component resolves go-src block symbols — Go symbol names, one
per line — through gotools.ResolveGoSymbols, appended as user content for
the next generation. Like ingest it is read-only context fetching, but
unconditional: symbol resolution reuses the packages the loader already
fetched, so it is always available in the codes pipeline. The codes session
presents both kinds, and the go-src prompt teaches their division of labor:
Go source is fetched by symbol — gaining the defining file, line, and the
references report — while ingest serves non-Go files, whole-file views, glob
discovery, and network resources. See gotools.TheoryOfGoSrcBlocks.

The ingest component carries the session's language-server handler. blocks
parses the lsp tag language-neutrally and defines the LSPHandler contract;
gotools provides the gopls-backed handler — one gopls process per
directory, lazily started at the first lsp request (see
gotools.TheoryOfGopls). The Go-specific lsp tag documentation
(gotools.LSPIngestTagSystemPrompt) is appended to the ingest prompt only when
the handler is attached, keeping the base ingest prompt language-neutral. A
nil handler keeps the section out of the prompt entirely; an lsp tag
emitted in such a session returns an explicit unavailability error part
rather than being silently ignored, matching the disabled-blocks
philosophy (see components.TheoryOfDisabledBlocks).

Read-only files, hidden packages, and mandatory planning are prompt-only
Components: they contribute system prompt sections without defining a block
kind or processing blocks. The hidden-packages notice
(gotools.HiddenPackagesSystemPrompt, from the go.hidden configuration)
appears only when at least one pattern is configured; it lists the hidden
import-path patterns so the model neither fetches their symbols nor reads
their files. See gotools.TheoryOfHiddenPackages.

ExtraSystemPrompt is also a prompt-only Component. The components carry no
reminder text of their own: the late reminder role is filled by the verbatim
system prompt restate (components.SystemPromptRestate), which the generation
pipeline appends as the last user prompt part before the dynamic chat input.
See TheoryOfComponents in the components package.

The generation loop checks for the summary block to distinguish a normally
ended attempt from truncated or non-conforming output: no other block kind
completes an attempt, so an attempt carrying component-triggering blocks
(ingest, shell, continue, go-test, go-src) without a summary block is
retried with feedback naming the missing summary (see TheoryOfLoops). Every
kind prompt that stops and waits states the summary requirement with the
same wording and adds the sequence rule — the block after the kind's
closing line must be the summary block — so no stop instruction licenses
omitting the summary block.
`

const TheoryOfFamilyExtraSystemPrompt = `
Family-specific extra system prompts extend the generic extra_system_prompt
mechanism with prompts keyed by the model family. The top-level
family_extra_system_prompt applies to every generation command (codes, ai,
next); the go.family_extra_system_prompt applies only when the codes
generation pipeline is active (go, any), mirroring the split between
extra_system_prompt and go.extra_system_prompt. Prompts are selected by the
family of the resolved default generator (generators.Spec.Family) and are
appended as prompt-only components after the generic extra prompts, so a
family-specific prompt refines or extends the generic instructions without
replacing them. The family is resolved through the generators.ModelFamily
provider, which derives the family from the resolved default generator, so
no customization is needed.
`

// CodesComponents is the component set type for the codes pipeline. It embeds
// components.ComponentSet as an anonymous struct field so that dscope can
// resolve it independently from other modules' ComponentSet providers.
// See TheoryOfCodesComponents.
type CodesComponents struct {
	components.ComponentSet
}

func (Module) CodesComponents(
	extra flags.ExtraSystemPrompt,
	goExtra gotools.ExtraSystemPrompt,
	familyExtra flags.FamilyExtraSystemPrompt,
	goFamilyExtra gotools.FamilyExtraSystemPrompt,
	modelFamily generators.ModelFamily,
	apply flags.Apply,
	plan flags.Plan,
	flagShell flags.Shell,
	applyChangeBlocks changes.ApplyChangeBlocks,
	resolveGoSymbols gotools.ResolveGoSymbols,
	hiddenPatterns gotools.HiddenPatterns,
	lspHandler blocks.LSPHandler,
) CodesComponents {
	var comps components.ComponentSet

	// The unified block format prompt is the first prompt-only component:
	// it teaches the heredoc-delimited block format that every kind prompt
	// below assumes. Kind prompts describe only their kind-specific
	// semantics without restating the format. See
	// blocks.TheoryOfBlockFormatGeneral.
	comps = append(comps, components.Component{
		PromptSection: blocks.BlockFormatSystemPrompt,
	})

	// Change component: prompt always included (from the change block
	// prompt, which describes only the change-kind semantics; the unified
	// block format is the first component above). Processing is
	// conditional on the apply flag. See TheoryOfCodesComponents.
	if bool(apply) {
		comps = append(comps, components.Component{
			Kind:          "change",
			PromptSection: changes.ChangeBlockPrompt,
			Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
				err := applyChangeBlocks(pctx.Blocks, pctx.Root)
				return components.ProcessResult{Err: err}
			},
		})
	} else {
		// Change blocks are not applied when -no-apply is set; the
		// prompt is still included so the model knows the format.
		comps = append(comps, components.Component{
			Kind:          "change",
			PromptSection: changes.ChangeBlockPrompt,
		})
	}

	// Go-test component: run Go tests after change blocks are applied.
	// Test output is always fed back to the model as Parts, triggering a
	// new generation regardless of whether tests pass or fail: the model
	// needs the results to decide whether to continue, and withholding
	// output on pass causes the system to exit prematurely when the model
	// intended to proceed. Placed after change so tests run against
	// updated source, and before summary so test output is available for
	// the next generation.
	// See TheoryOfCodesComponents and gotools.TheoryOfGoTestBlocks.
	comps = append(comps, components.Component{
		Kind:          "go-test",
		PromptSection: gotools.GoTestBlockSystemPrompt,
		Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
			parts, err := gotools.ProcessGoTestBlocks(pctx.Blocks, ctx)
			return components.ProcessResult{
				Parts: parts,
				Err:   err,
			}
		},
	})

	// Go-src component: resolves go-src block symbols to declaration
	// source. Read-only and unconditional: symbol resolution reuses the
	// packages the loader already fetched, so it is always available in
	// the codes pipeline. Placed with ingest before shell and continue
	// so fetched context is available for the next generation.
	// See gotools.TheoryOfGoSrcBlocks and gotools.TheoryOfGoSrcResolution.
	comps = append(comps, components.Component{
		Kind:          "go-src",
		PromptSection: gotools.GoSrcBlockSystemPrompt,
		Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
			symbols := gotools.ParseGoSrcSymbols(pctx.Blocks)
			if len(symbols) == 0 {
				// An empty go-src block is a format error the model can
				// correct: feed back a usage hint instead of a silent
				// no-op, so the next generation can list the symbols
				// properly. The feedback ends with a blank line so
				// consecutive parts stay paragraph-separated; see
				// generators.TheoryOfContentUnitSeparation.
				return components.ProcessResult{
					Parts: []generators.Part{
						generators.Text("The go-src block body was empty; list one Go symbol per line (plain names or TypeName.MethodName).\n\n"),
					},
				}
			}
			parts, err := resolveGoSymbols(symbols)
			if err != nil {
				return components.ProcessResult{Err: err}
			}
			// A brief header tells the model why the source appeared in
			// the next generation's user content.
			parts = append([]generators.Part{generators.Text(
				"[Requested source of the go-src symbols]\n\n")}, parts...)
			return components.ProcessResult{Parts: parts}
		},
	})

	// Ingest component: always enabled — dynamic context has no toggle; the
	// model may request additional files and network resources
	// mid-generation in every codes session. Processed before
	// shell/continue so fetched context is available for the next
	// generation. The session's language-server handler is attached
	// when one resolves; its Go-specific lsp tag documentation is appended
	// to the ingest prompt only then. A nil handler keeps the section out of
	// the prompt; an emitted lsp tag then returns an explicit
	// unavailability error part instead of being silently ignored.
	// See blocks.TheoryOfIngestBlocks, gotools.TheoryOfGopls, and
	// TheoryOfCodesComponents.
	ingestPrompt := blocks.IngestBlockSystemPrompt
	if lspHandler != nil {
		ingestPrompt += gotools.LSPIngestTagSystemPrompt
	}
	comps = append(comps, components.Component{
		Kind:          "ingest",
		PromptSection: ingestPrompt,
		Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
			state, hasIngest, err := blocks.ProcessIngestBlocks(
				pctx.Blocks, ctx, pctx.Root, pctx.HttpClient, lspHandler, pctx.State,
			)
			result := components.ProcessResult{
				Err: err,
			}
			// Only set State when ingest blocks were found and fetched
			// content was appended, so that result.State != nil reliably
			// signals a state modification that triggers a new generation.
			if hasIngest {
				result.State = state
			}
			return result
		},
	})

	// Common components: shell (conditional on flagShell) and continue.
	// Reused from components.CommonComponents so that shell and continue
	// configuration is shared across all generation commands.
	// See TheoryOfCommonComponents in components/common_components.go.
	comps = append(comps, components.CommonComponents(bool(flagShell))...)

	// Disabled-blocks notice: when the shell flag is off, state it
	// explicitly instead of leaving the shell slot silent. A model that
	// emits shell blocks from habit would have them silently ignored
	// while implying commands had run. Under -no-apply the change prompt
	// above is still included and change is deliberately not listed as
	// disabled: the blocks are the deliverable of a dry run. See
	// components.TheoryOfDisabledBlocks and TheoryOfCodesComponents.
	if !bool(flagShell) {
		comps = append(comps, components.DisabledBlocksComponent("shell"))
	}

	// Summary component: processed in runGeneration for completion
	// detection and attempt statistics, not in the main component loop.
	// See TheoryOfCodesComponents.
	comps = append(comps, components.Component{
		Kind:          "summary",
		PromptSection: blocks.SummaryBlockSystemPrompt,
	})

	// Read-only files: prompt-only component, no block kind.
	comps = append(comps, components.Component{
		PromptSection: ReadOnlyFilesSystemPrompt,
	})

	// Hidden packages: prompt-only component listing the go.hidden
	// import-path patterns. Visible code may still reference a hidden
	// package's import path, so without the notice the model could
	// discover the package and burn generations on go-src and ingest
	// blocks that the hide renders futile. The notice appears only when
	// at least one pattern is configured. See gotools.TheoryOfHiddenPackages.
	if section := gotools.HiddenPackagesSystemPrompt(hiddenPatterns); section != "" {
		comps = append(comps, components.Component{
			PromptSection: section,
		})
	}

	// Mandatory planning: prompt-only component, conditional on plan.
	if bool(plan) {
		comps = append(comps, components.Component{
			PromptSection: MandatoryPlanningSystemPrompt,
		})
	}

	// Extra system prompt from configuration: prompt-only Component.
	// Each entry is added as a separate prompt-only Component so that
	// multiple config sources are all included.
	// Unified under the Component framework so all prompt contributions
	// are assembled through comps.PromptSections(). See TheoryOfCodesComponents.
	for _, prompt := range extra {
		if prompt != "" {
			comps = append(comps, components.Component{
				PromptSection: prompt,
			})
		}
	}

	// Go-specific extra system prompt from configuration
	// (go.extra_system_prompt): prompt-only Component, appended after the
	// top-level extra prompts so the go project context is introduced
	// whenever the codes generation pipeline is active (go, any, goal
	// commands). The ai command uses AIComponents and is unaffected.
	// See gotools.ExtraSystemPrompt.
	for _, prompt := range goExtra {
		if prompt != "" {
			comps = append(comps, components.Component{
				PromptSection: prompt,
			})
		}
	}

	// Family-specific extra system prompts: top-level and go-specific
	// prompts keyed by the model family. The family is resolved from the
	// scope via generators.ModelFamily; when the family matches a key, the
	// corresponding prompts are appended as prompt-only components
	// after the generic extra prompts. See
	// TheoryOfFamilyExtraSystemPrompt.
	for _, prompt := range familyExtra[string(modelFamily)] {
		if prompt != "" {
			comps = append(comps, components.Component{
				PromptSection: prompt,
			})
		}
	}
	for _, prompt := range goFamilyExtra[string(modelFamily)] {
		if prompt != "" {
			comps = append(comps, components.Component{
				PromptSection: prompt,
			})
		}
	}

	return CodesComponents{comps}
}
