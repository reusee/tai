package components

import (
	"slices"
	"strings"
)

const TheoryOfDisabledBlocks = `
A component set teaches the model only the block kinds it processes. Models
still emit untaught kinds from training priors or from context that mentions
them (documentation, examples, other sessions). An unprocessed block is
silently ignored: it is never executed, applied, or answered, and it
triggers no new round — so the wasted output also implies an action that
never happened (the model may emit a shell block and then reason as if the
command had run).

DisabledBlocksNotice closes the gap by explicitly listing the kinds that are
NOT available in the current session, each with a replacement behavior
(shell: state the command in prose; continue: deliver the complete answer in
this response; change: describe the modification; go-test, go-src, ingest:
state the need in prose). DisabledBlocksComponent wraps the notice as a
prompt-only Component: no Kind, no Process function, so it never enters
Processable and cannot consume blocks. An empty notice (no kinds, or only
unknown kinds) renders the component inert — every assembly method skips
it. CommonComponents itself carries no notices: each caller owns its
complete disabled list in a single notice component, so a prompt never shows
the notice twice.

Determinism: kinds are sorted and deduplicated and each kind renders one
fixed line, so equal inputs produce byte-identical notices and the LLM
prefix cache is preserved across runs with equal configuration.

Scope: the notice lists only kinds with an entry in disabledKindDescriptions;
the map is open-ended, and a kind gains an entry when it has a meaningful
replacement behavior. Deliberately unlisted: codes under -no-apply keeps the
change prompt because change blocks are the deliverable of a dry run (see
TheoryOfCodesComponents in pipeline/components.go); the Go-module default
command's goal mode treats the done block as its completion contract, not a
component-driven kind.
`

// disabledBlocksNoticeHeader opens the disabled-blocks prompt section. It
// states the contract once — disabled blocks are discarded, never executed,
// and trigger no round — so each kind line stays a single line. See
// TheoryOfDisabledBlocks.
const disabledBlocksNoticeHeader = `**Disabled Block Kinds:**

The block kinds listed below are NOT available in this session. A block of a disabled kind is never processed — not executed, not applied, not answered — and triggers no new round: its content is discarded. Emitting one wastes output and implies an action that never happened. Do not emit them; use the replacement behavior stated for each kind.`

// disabledKindDescriptions maps a block kind to the one-line notice shown
// when the kind is not available in the current session. Each line names
// the kind, states that it is unavailable, and gives the replacement
// behavior. The map is open-ended: a kind gains an entry when it has a
// meaningful replacement behavior. See TheoryOfDisabledBlocks.
var disabledKindDescriptions = map[string]string{
	"shell":    "- `shell` — shell execution is disabled in this session; commands are never run. Do not emit shell blocks. When a command matters, state the command (or its expected output) in plain text instead.",
	"continue": "- `continue` — continue blocks are not accepted in this session: the body is never fed back and no round is started by one. Do not emit continue blocks. Deliver the complete answer in this response.",
	"change":   "- `change` — change blocks are not processed in this session and nothing is written to files. Do not emit change blocks. When a file modification is required, describe it precisely in plain text (path, operation, content) instead.",
	"go-test":  "- `go-test` — tests are never run in this session. Do not emit go-test blocks. When test verification matters, state in plain text which tests to run and what result is expected.",
	"go-src":   "- `go-src` — symbol sources are not fetched in this session. Do not emit go-src blocks. Work from the context already provided.",
	"ingest":   "- `ingest` — additional files and network resources are not fetched in this session. Do not emit ingest blocks. When essential content is missing, state exactly what is needed, then stop.",
	"memory":   "- `memory` — the user profile is not updated in this session. Do not emit memory blocks.",
}

// DisabledBlocksNotice returns a system prompt section that explicitly
// lists block kinds that are NOT available in the current session, each
// with its replacement behavior, or "" when no listed kind is disabled.
// Kinds are sorted and deduplicated so equal inputs produce byte-identical
// notices, preserving the LLM prefix cache. Unknown kinds are skipped.
// See TheoryOfDisabledBlocks.
func DisabledBlocksNotice(disabledKinds ...string) string {
	kinds := slices.Clone(disabledKinds)
	slices.Sort(kinds)
	kinds = slices.Compact(kinds)
	var notice strings.Builder
	for _, kind := range kinds {
		if desc, ok := disabledKindDescriptions[kind]; ok {
			notice.WriteString(desc)
			notice.WriteByte('\n')
		}
	}
	if notice.Len() == 0 {
		return ""
	}
	return disabledBlocksNoticeHeader + "\n\n" + notice.String()
}

// DisabledBlocksComponent returns a prompt-only Component carrying the
// disabled-blocks notice for the given kinds. It defines no Kind and no
// Process function, so it never enters Processable and cannot consume
// blocks; a zero notice (no kinds disabled) renders the component inert
// in every assembly method. Callers append the result unconditionally.
// See TheoryOfDisabledBlocks.
func DisabledBlocksComponent(disabledKinds ...string) Component {
	return Component{
		PromptSection: DisabledBlocksNotice(disabledKinds...),
	}
}
