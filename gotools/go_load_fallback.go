package gotools

import (
	"github.com/reusee/tai/generators"
)

// TheoryOfGoLoadFallback documents the Go context provider's raw-file
// fallback: when the package load fails, the provider serves the anytexts
// raw-file context led by the load error note instead of erroring out.
// See the constant body for the rule and its rationale.
const TheoryOfGoLoadFallback = `
The Go context provider serves the Go context and falls back to the
raw-file context when the package load fails. A failed load means the package
graph cannot be resolved — a non-compiling project reports the failure here:
the provider's GetFiles returns the load error, carrying the diagnostics
collected from pkg.Errors and the module errors — so no declaration surface,
project files, or go-src resolution can be assembled. The fallback serves
the anytexts context — the text files under the working directory — led by
the load error note, which states the failure, carries the diagnostics, and
directs the model to fix the build first.

The decision is per call, never cached in the provider: the probe is the
provider's own GetFiles, and a goal loop resolves the provider from a scope
reset per loop, so every loop re-runs the package load against the current
filesystem state. A loop that fixes the build returns to the Go context on
its own; a loop that breaks it drops back to the raw context. No flag and no
cross-loop state is involved.

Only a failed load selects the fallback. An error after a successful load —
a failing go doc reference, an unreadable file — propagates unchanged:
substituting raw files for such a fault would hide it instead of surfacing it
in the retry feedback.

The load error note is bounded and always served: it is charged against the
token budget first and the raw-file context receives the remainder, so file
content never exceeds the budget, and only the bounded truncation text can
exceed a budget too small for it. A raw-context failure degrades to the note
alone, keeping the load error and its fix directive in the prompt. The
raw-file context serves full content, not skeletons: a broken build must be
read from source. The pipeline gates the Go-specific extra system prompts on
the session's provider satisfying this package's PartsProvider type, so the
fallback lives inside the provider; a command-level wrapper would hide the
type and silently drop those prompts (see TheoryOfCodesComponents in the
pipeline package).
`

// goLoadFallbackNote introduces the raw-file context served when the package
// load fails: it names the fallback, directs the model to fix the load error
// first, and states the automatic return to the Go context. It is the
// fallback's counterpart of a disabled-blocks notice line.
// See TheoryOfGoLoadFallback.
const goLoadFallbackNote = `The Go package load failed, so this context is the raw file context instead of the Go declaration surface, and go-src blocks cannot resolve symbols. Fix the load error below first; read further files with ingest blocks. Once the load succeeds, the next loop returns to the Go context automatically.`

// maxLoadErrorRunes caps the load error text the note carries. A broken build
// can report many errors, and an unbounded dump would flood the user prompt.
const maxLoadErrorRunes = 2000

// anyTextLoadFallback serves the raw-file context when the package load
// fails: the load error note, charged against the token budget, followed by
// the anytexts context under the remaining budget. A raw-context failure
// degrades to the note alone, keeping the load error and its fix directive
// in the prompt. See TheoryOfGoLoadFallback.
func (c PartsProvider) anyTextLoadFallback(
	maxTokens int,
	countTokens func(string) (int, error),
	patterns []string,
	loadErr error,
) ([]generators.Part, error) {
	note := goLoadFallbackNote + "\n\n" +
		"``` begin of load error\n" +
		truncateLoadError(loadErr.Error()) + "\n" +
		"``` end of load error\n\n"
	noteTokens, err := countTokens(note)
	if err != nil {
		return nil, err
	}
	remaining := maxTokens - noteTokens
	if remaining < 0 {
		remaining = 0
	}
	parts, err := c.AnyTexts().Parts(remaining, countTokens, patterns)
	if err != nil {
		c.Logger().Warn("raw file context failed", "error", err)
		return []generators.Part{generators.Text(note)}, nil
	}
	return append([]generators.Part{generators.Text(note)}, parts...), nil
}

// truncateLoadError caps the load error text at maxLoadErrorRunes runes,
// cutting at a rune boundary and marking the cut.
func truncateLoadError(text string) string {
	runes := []rune(text)
	if len(runes) <= maxLoadErrorRunes {
		return text
	}
	return string(runes[:maxLoadErrorRunes]) + "\n... (truncated)"
}
