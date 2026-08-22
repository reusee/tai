package blocks

import "strings"

const TheoryOfGoSrcBlocks = `
The go-src block is the symbol-level context-fetching kind: the model lists
Go symbol names — one per line — and the system resolves each symbol to its
declaration source, returned as user content in the next generation round.
It complements request-context: request-context fetches files and network
resources, while go-src fetches declaration source within the packages the
Go pipeline already loaded. Its purpose is precision under the visibility
system: a package shown at documentation visibility carries only go doc
output, so the model knows declaration signatures but not implementations;
go-src lets the model pull exactly the implementations it needs instead of
re-fetching whole files (see gocodes.TheoryOfVisibilityAllocation). Focus
packages are pinned at documentation level, which makes go-src the primary
path from the declaration surface to the implementation: the initial
context carries only declarations and test-function names, and the model
fetches exactly the source it needs before understanding, modifying, or
reviewing any focus declaration — including test functions, which the
focus package block lists by name.

The block body is opaque to the mechanism: each non-empty line is one
symbol name in the go doc form [<pkg>.][<sym>.][<methodOrField>] — a
plain name for a top-level declaration, TypeName.MethodName for a method,
with an optional package path prefix (full path or proper suffix)
restricting the match to that package, an optional leading * receiver
prefix ignored, and generic parameter lists on the type name ignored.
Name matching follows go doc's case rule: lower-case query letters match
either case, upper-case letters match exactly.

A symbol may also be a package reference: an exact loaded package import
path or package name resolves to the package's go doc documentation
instead of declaration source. Focus packages are documented with the
-cmd and -u flags — a main package's documentation and unexported
symbols are shown because the model edits focus packages — while a
context package shows its exported API surface. Package matching takes
precedence over symbol matching, mirroring go doc; a package name may
match several packages, all of which are returned.

The resolution lives with the Go package loader (gocodes.ResolveGoSymbols)
because it needs the parsed ASTs; the blocks package defines only the
block format and the symbol parse. Like request-context, go-src is
strictly read-only and is not a completion signal: a round carrying a
go-src block still needs a summary block, and because the kind is
processable it participates in the triggering-block check, so such a
round is not retried as truncated output (see loops.TheoryOfLoops).
`

const GoSrcBlockSystemPrompt = `
Go-Src Block Kind:

Use the "go-src" kind to request the source code of Go symbols that were not fully included in the context. The system resolves each symbol to its declaration source and provides it as user content in the next generation round.

**Rules:**
- Use go-src blocks when you need the implementation of a Go symbol that the context shows only as a signature or documentation (e.g., a package included at documentation visibility shows go doc output without function bodies).
- Focus packages appear in the context as documentation only: the declaration surface (go doc -all -cmd -u output) plus a list of the package's test-function names. Their implementation source is NOT included initially.
- Before understanding, modifying, or reviewing any focus declaration, fetch its source with a go-src block naming the declaration. Do not reason about, edit, or review a focus declaration from its documentation alone — fetch the source first, then act.
- Test functions listed in a focus package block (TestXxx, BenchmarkXxx, FuzzXxx, ExampleXxx) may be fetched by name like any other symbol. Fetch a test's source before modifying it or when checking behavior related to your change.
- The body contains ONLY symbol names, one per line, with no prose. Each non-empty line is one symbol.
- Symbol forms follow go doc: a plain name for a top-level declaration (function, type, const, var), e.g. NewReader; TypeName.MethodName for a method, e.g. Reader.Read; and an optional package path prefix that restricts matching to that package, e.g. encoding/json.Marshal or json.Marshal. The package path may be the full import path or a proper suffix of it. An optional leading * receiver prefix is ignored. Generic parameter lists on the type name are ignored (Pair.Swap and Pair[A, B].Swap both resolve). Do not qualify names with a package path unless restricting to a specific loaded package.
- A symbol that is itself a package — an exact loaded package import path (e.g., encoding/json) or package name (e.g., json) — returns that package's go doc documentation instead of declaration source. Focus packages include command and unexported documentation; context packages show the exported API.
- Name matching follows go doc's case rule: a lower-case letter in the query matches either case in the target, an upper-case letter matches exactly.
- A plain name may match declarations in several packages; all matches are returned with their package-qualified names and file locations. A package-qualified name returns only that package's matches.
- Only symbols in packages loaded in this session can be resolved. Symbols that match nothing are reported in the next round; correct the name and try again.
- The returned source includes the declaration's doc comments.
- Do not emit change blocks whose content depends on the requested source: request the source first, then emit changes in a subsequent response after the source is provided.
- After emitting a go-src block, stop generating and wait: the requested source arrives as user content in the next round.
- Close the go-src block with its closing line before emitting any other block (e.g., the summary block).
- The go-src block is NOT a completion signal. MUST still emit a summary block in the same round, after the go-src block. Every round must end with a summary block.
- Only use go-src blocks in Go projects.
`

const GoSrcBlockRestatePrompt = `- When you need the implementation of a Go symbol that the context shows only as a signature or documentation, emit a go-src block whose body lists symbol names, one per line. Symbol forms follow go doc: plain names for top-level declarations, TypeName.MethodName for methods, and an optional package path prefix (full path or proper suffix) restricting the match to that package. A leading * receiver prefix and generic parameter lists on the type name are ignored. Lower-case query letters match either case; upper-case letters match exactly. Only symbols in packages loaded in this session can be resolved; unmatched names are reported back. Only use go-src blocks in Go projects.
- Focus packages appear as documentation only (declaration surface plus test-function names). Before understanding, modifying, or reviewing any focus declaration — including a listed test function — fetch its source with a go-src block naming the declaration; do not act on documentation alone.
- A symbol that is a loaded package (exact import path or package name) returns that package's go doc documentation: focus packages include command and unexported documentation, context packages the exported API.
- A go-src block does NOT replace the summary block. MUST still emit a summary block in the same round, even when emitting a go-src block.`

// ParseGoSrcSymbols extracts the symbol names from go-src blocks: each
// non-empty, trimmed body line is one symbol. Blocks of other kinds are
// skipped. See TheoryOfGoSrcBlocks.
func ParseGoSrcSymbols(blocks []Block) []string {
	if len(blocks) == 0 {
		return nil
	}
	var symbols []string
	for _, block := range blocks {
		if block.Kind != "go-src" {
			continue
		}
		for line := range strings.SplitSeq(block.Body, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				symbols = append(symbols, line)
			}
		}
	}
	return symbols
}
