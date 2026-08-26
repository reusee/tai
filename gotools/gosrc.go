package gotools

import (
	"strings"

	"github.com/reusee/tai/blocks"
)

const TheoryOfGoSrcBlocks = `
The go-src block is the symbol-level context-fetching kind: the model lists
Go symbol names — one per line — and the system resolves each symbol to its
declaration source, returned as user content in the next generation round.
It complements ingest under a taught division of labor: for Go source code
the prompts prefer go-src, because a fetch returns the exact declaration
with its doc comments, the defining file and line, a references report of
the symbol's callers, a selector packages report listing the full import
paths of packages used in selector expressions within the declaration, and
interface relations for named types and interfaces — none of which a
whole-file ingest provides. The ingest kind keeps what go-src cannot fetch:
non-Go files, whole-file views, glob discovery, and network resources. The
division is taught only in the go-src prompts; the ingest prompt stays
language-neutral because the blocks package defines no Go-specific kind.
The kind's purpose is precision under the visibility system: a package
shown at documentation visibility carries only go doc output, so the model
knows declaration signatures but not implementations; go-src lets the model
pull exactly the implementations it needs instead of re-fetching whole
files (see TheoryOfVisibilityAllocation). Focus packages are pinned at
documentation level, which makes go-src the primary path from the
declaration surface to the implementation: the initial context carries only
declarations, test-function names, and file names, and the model fetches
exactly the source it needs before understanding, modifying, or reviewing
any focus declaration — including test functions, which the focus package
block lists by name. Under the -all-src flag focus packages are pinned at
full source, so the initial context already carries every focus
declaration's implementation and go-src fetching is unnecessary for focus
declarations; the kind remains the fetch path for context packages at
documentation visibility.

The block body is opaque to the mechanism: each non-empty line is one
symbol name in the go doc form [<pkg>.][<sym>.][<methodOrField>] — a
plain name for a top-level declaration, TypeName.MethodName for a method,
with an optional package qualifier (full import path, path suffix, or
the package's declared name) restricting the match to that package, an
optional leading * receiver
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

The prompts teach three facts about resolution results. The qualifier
should be the full import path when restricting a symbol to a package:
an import path names exactly one loaded package, while a bare package
name or path suffix may match several same-named packages and multiply
the results. Each resolved block names the defining file (with line),
so the fetched declaration doubles as the authoritative file-path for
change blocks targeting it. And resolution reads the in-memory file set
captured at context assembly — it never re-reads the disk — so a file
modified by change blocks during the session still yields
pre-modification content on a repeated fetch; verification of applied
changes belongs to go-test blocks and disk reads, not to re-fetching
symbols.

The go-test and go-src mechanisms are Go-specific, so they live in this
package together with the resolver (ResolveGoSymbols, which needs the
parsed ASTs); the blocks package defines only the generic block format
and the language-neutral kinds. Like ingest, go-src is strictly read-only
and is not a completion signal: a round carrying a go-src block still
needs a summary block, and the stop rule is phrased summary-first —
emit the summary block immediately after the last go-src block's closing
line, then end the response — so no stop instruction licenses halting at
the closing line, the observed failure shape of a lone go-src block
ending a response. A round that ends on a go-src block without a
summary is retried with feedback naming the missing summary; the go-src
block is discarded with the failed attempt and must be re-emitted
together with the summary block — re-emission is what makes the symbol
requests take effect (see pipeline.TheoryOfLoops).
`

const GoSrcBlockSystemPrompt = `
Go-Src Block Kind:

Use the "go-src" kind to request the source code of Go symbols that were not fully included in the context. The system resolves each symbol to its declaration source and provides it as user content in the next generation round.

**Rules:**
- Use go-src blocks when you need the implementation of a Go symbol that the context shows only as a signature or documentation (e.g., a package included at documentation visibility shows go doc output without function bodies). Only use go-src blocks in Go projects.
- Prefer go-src over ingest for Go source code: a fetch returns the exact declaration, names its defining file and line (usable as the change block file-path), and appends a references report of the symbol's callers — none of which a whole-file ingest provides. Use an ingest block only for what go-src cannot fetch: non-Go files, a whole-file view (imports, file layout, adjacent declarations), glob file discovery, or network resources.
- Focus packages appear in the context as documentation only: the declaration surface (go doc -all -cmd -u output) plus a list of the package's test-function names and a list of the package's source file names. Their implementation source is NOT included initially.
- Before understanding, modifying, or reviewing any focus declaration, fetch its source with a go-src block naming the declaration. Do not reason about, edit, or review a focus declaration from its documentation alone — fetch the source first, then act.
- Test functions listed in a focus package block (TestXxx, BenchmarkXxx, FuzzXxx, ExampleXxx) may be fetched by name like any other symbol. Fetch a test's source before modifying it or when checking behavior related to your change.
- The body contains ONLY symbol names, one per line, with no prose. Each non-empty line is one symbol.
- Symbol forms follow go doc: a plain name for a top-level declaration (function, type, const, var), e.g., NewReader; TypeName.MethodName for a method, e.g., Reader.Read; and an optional package qualifier that restricts matching to that package, e.g., encoding/json.Marshal, json.Marshal, or doublestar.Glob. The qualifier may be the full import path, a proper suffix of it, or the package's declared name — the declared-name form addresses major-version packages whose last path segment is a version (doublestar for …/v4). An optional leading * receiver prefix is ignored. Generic parameter lists on the type name are ignored (Pair.Swap and Pair[A, B].Swap both resolve). Do not qualify names with a package qualifier unless restricting to a specific loaded package.
- Prefer the full import path as the package qualifier: an import path identifies exactly one loaded package, while a bare package name or a path suffix may match several packages that share the name and return redundant results.
- A symbol that is itself a package — an exact loaded package import path (e.g., encoding/json) or package name (e.g., json) — returns that package's go doc documentation instead of declaration source. Focus packages include command and unexported documentation; context packages show the exported API.
- Name matching follows go doc's case rule: a lower-case letter in the query matches either case in the target, an upper-case letter matches exactly.
- A plain name may match declarations in several packages; all matches are returned with their package-qualified names and file locations. A package-qualified name returns only that package's matches.
- Only symbols in packages loaded in this session can be resolved. Symbols that match nothing are reported in the next round; correct the name and try again.
- The returned source includes the declaration's doc comments and names the defining file (with line) where the declaration lives. Use that file path as the change block's file-path attribute so modifications target the exact file the source was read from.
- Each resolved source part is followed by a references report: a "begin of references" block listing which top-level declarations reference the symbol, one per line as "package path: enclosing top-level declaration (file)", deduplicated per top-level declaration and possibly truncated at 100 entries. Use it to judge the blast radius and find callers before changing the symbol.
- Each resolved source part is followed by a selector packages report: a "begin of selector packages" block listing the full import paths of packages used in selector expressions within the declaration, deduplicated and sorted. Use these paths as package qualifiers in follow-up go-src blocks without scanning the import block.
- Fetching a named type or interface appends an interface relations report: a "begin of interface relations" block listing "satisfies pkg.I" lines (interfaces a concrete type fulfills via its value or pointer method set) or "implemented by pkg.N" / "implemented by *pkg.N" lines (loaded concrete types implementing a fetched interface; the leading * marks a pointer-only method set). Polymorphism is invisible in plain source text: use the report to jump between an interface and its implementations in one step.
- go-src resolves against an in-memory snapshot of the files loaded when the context was assembled; it does not re-read the disk. A file modified by change blocks during this session still yields its pre-modification content when the same symbol is fetched again. Verify applied changes with go-test blocks or by reading the file from disk (e.g., cat), not by re-fetching with go-src.
- Do not emit change blocks whose content depends on the requested source: request the source first, then emit changes in a subsequent response after the source is provided.
- After the last go-src block's closing line, emit the summary block IMMEDIATELY, then end the response and wait: the requested source arrives as user content in the next round.
- Close the go-src block with its closing line before emitting any other block (e.g., the summary block).
- The go-src block is NOT a completion signal. MUST still emit a summary block in the same round, after the go-src block. Every round must end with a summary block.
- Never end a response on a go-src block, and never stop at its closing line: stopping there omits the mandatory summary block, the response is treated as incomplete and retried, and its blocks are discarded — so the symbol requests are lost unless re-emitted with the summary.
`

const GoSrcBlockRestatePrompt = `- Prefer go-src over ingest for Go source code: a fetch returns the declaration, its defining file and line, and a references report of the symbol's callers. Use ingest only for non-Go files, whole-file views, glob discovery, or network resources.
- When you need the implementation of a Go symbol that the context shows only as a signature or documentation, emit a go-src block whose body lists symbol names, one per line. Symbol forms follow go doc: plain names for top-level declarations, TypeName.MethodName for methods, and an optional package qualifier (full import path, path suffix, or the package's declared name — e.g., doublestar.Glob for a …/v4 module) restricting the match to that package; prefer the full import path, which identifies exactly one loaded package. A leading * receiver prefix and generic parameter lists on the type name are ignored. Lower-case query letters match either case; upper-case letters match exactly. Only symbols in packages loaded in this session can be resolved; unmatched names are reported back. Only use go-src blocks in Go projects.
- Focus packages appear as documentation only (declaration surface plus test-function names and file names). Before understanding, modifying, or reviewing any focus declaration — including a listed test function — fetch its source with a go-src block naming the declaration; do not act on documentation alone.
- A symbol that is a loaded package (exact import path or package name) returns that package's go doc documentation: focus packages include command and unexported documentation, context packages the exported API.
- The resolved source names the defining file; use that file path as the change block's file-path. go-src returns an in-memory snapshot of the files loaded at context assembly and does not re-read the disk: a file modified by change blocks in this session still shows its pre-modification content. Verify applied changes with go-test blocks or by reading the file, not by re-fetching with go-src.
- Each resolved source part is followed by a references report listing which top-level declarations reference the symbol, one per line as "package path: enclosing top-level declaration (file)", deduplicated per top-level declaration and possibly truncated at 100 — use it to judge blast radius and callers before changing the symbol.
- Reports follow every resolved source part: references (callers), selector packages (full import paths of packages used in selector expressions within the declaration), and interface relations for named types and interfaces (satisfies / implemented by).
- After the last go-src block's closing line, emit the summary block IMMEDIATELY, then end the response and wait for the source — never stop at the closing line itself.
- A go-src block does NOT replace the summary block. MUST still emit a summary block in the same round, even when emitting a go-src block.
- Never end a response on a go-src block: after the go-src block's closing line, the next block MUST be the summary block.`

// ParseGoSrcSymbols extracts the symbol names from go-src blocks: each
// non-empty, trimmed body line is one symbol. Blocks of other kinds are
// skipped. See TheoryOfGoSrcBlocks.
func ParseGoSrcSymbols(bs []blocks.Block) []string {
	if len(bs) == 0 {
		return nil
	}
	var symbols []string
	for _, block := range bs {
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
