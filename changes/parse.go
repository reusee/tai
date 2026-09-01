package changes

import (
	"fmt"
	"strings"

	"github.com/reusee/tai/blocks"
)

const TheoryOfNonGoFileChanges = `
Non-Go files split by capability. File-level operations (WRITE, RENAME,
DELETE with target=*) and text-level operations (REPLACE, INSERT_BEFORE,
INSERT_AFTER) work on every non-Go text file without structural parsing;
the find-anchor mechanics live in TheoryOfTextLevelOperations. Files backed
by a gotreesitter grammar additionally support tree-structured operations
addressed by outline path; the mechanism, its line-boundary semantics, and
its re-parse validation live in TheoryOfTreeStructuredEdits.
`

const TheoryOfTextLevelOperations = `
Text-level operations (REPLACE, INSERT_BEFORE, INSERT_AFTER) enable partial
edits to non-Go text files without structural parsing; ChangeBlockPrompt's
rules for them — the find anchor, its uniqueness requirement, and the
REPLACE/INSERT semantics — are the theory text for the contract and are not
repeated here.

INSERT_BEFORE and INSERT_AFTER always place the inserted content on its own
line(s): a newline separator is added automatically when the block body does
not already carry one. Block bodies are trimmed of leading and trailing
whitespace during parsing, so an emitted body never has a usable boundary
newline of its own; inserting it raw would merge the first inserted line with
the anchor line (INSERT_AFTER) or the anchor line with the last inserted line
(INSERT_BEFORE), corrupting two lines that should remain separate. This
mirrors the Go structural ADD operations (see buildModifiedSource in
apply.go), which also separate inserted declarations with newlines; the
text-level operations apply the same line-boundary principle to plain text.

The Go-file restriction exists because the model has difficulty correctly
reproducing whitespace characters in the find string, causing the string
match to fail on Go source code where whitespace is semantically significant
for formatting; AST-based declaration matching does not depend on exact
whitespace reproduction and is therefore more robust.

The uniqueness requirement is the integrity guarantee: it prevents ambiguous
edits where the model's find string matches multiple locations. Line-number-
based approaches are deliberately avoided because models cannot reliably
generate accurate line numbers; a unique string anchor is content-addressed.
`

const TheoryOfSpecialGoTargets = `
The "package" and "import" targets are special Go-only targets that support
only the MODIFY operation. They exist for token-efficient modification of
the package clause and the import block without WRITE replacing the entire
file — essential when moving a file to a different package, renaming a
package, or updating imports across dependent files. ChangeBlockPrompt's
"Special Go-Only Targets (MODIFY)" section is itself the theory text for
the targets' body contracts and is not repeated here.

ValidateChangeBlock rejects the targets on non-Go files, because MODIFY is
not a file-level operation, and rejects every operation other than MODIFY
for them. The "import" target unifies replacement and insertion into a
single "set imports" operation. If the file does not exist or has no real
package clause, MODIFY is a no-op (consistent with existing MODIFY
behavior). Both targets run goimports after replacement to ensure valid
formatting and import synchronization.
`

const TheoryOfPreciseModifications = `
Precise modifications are preferred over WRITE because they touch only the
bytes that need to change, while WRITE's whole-file replacement is
token-expensive, has a large review blast radius, and risks altering
unrelated code. ChangeBlockPrompt's "Prefer Precise Modifications" section
is itself the theory text for the rule and its reserved WRITE cases, and
is not repeated here.
` // isGoFile reports whether the given file path has a .go extension.
func isGoFile(path string) bool {
	return strings.HasSuffix(path, ".go")
}

// isFileLevelOperation reports whether the operation does not require
// structural identification of declarations within the file. WRITE replaces
// the entire file content, RENAME renames a file, and DELETE with target=*
// removes an entire file; none of these require parsing the file's structure.
// All other operations (MODIFY, ADD_BEFORE, ADD_AFTER, and DELETE with a
// specific declaration target) require structural identification: through
// go/ast for Go files, or through the outline path for non-Go files with a
// registered grammar. See TheoryOfNonGoFileChanges and
// TheoryOfTreeStructuredEdits.
func isFileLevelOperation(op, target string) bool {
	switch op {
	case "WRITE":
		return true
	case "RENAME":
		return true
	case "DELETE":
		return target == "*"
	default:
		return false
	}
}

// isTextLevelOperation reports whether the operation applies a text-based
// edit using a find string anchor rather than structural parsing or
// file-level replacement. REPLACE, INSERT_BEFORE, and INSERT_AFTER search
// for a unique string in the file and apply the edit relative to that
// anchor. These operations work on any text file, including non-Go files
// that cannot be structurally parsed. See TheoryOfTextLevelOperations.
func isTextLevelOperation(op string) bool {
	switch op {
	case "REPLACE", "INSERT_BEFORE", "INSERT_AFTER":
		return true
	default:
		return false
	}
}

// ValidateChangeBlock validates that the change block's operation is valid
// for the target file type. Non-Go files support file-level operations
// (WRITE, RENAME, DELETE with target=*) and text-level operations (REPLACE,
// INSERT_BEFORE, INSERT_AFTER). Non-Go files backed by a gotreesitter grammar
// additionally support tree-structured operations (MODIFY, ADD_BEFORE,
// ADD_AFTER, DELETE with a specific outline-path target). See
// TheoryOfNonGoFileChanges and TheoryOfTreeStructuredEdits.
// Go files do not support text-level operations because the model cannot
// reliably reproduce whitespace in the find string; structural operations
// must be used instead. See TheoryOfTextLevelOperations.
// "package" and "import" are special Go-only targets that support only MODIFY.
// See TheoryOfSpecialGoTargets.
func ValidateChangeBlock(h ChangeBlock) error {
	if !isGoFile(h.FilePath) && !isFileLevelOperation(h.Op, h.Target) && !isTextLevelOperation(h.Op) {
		// Grammar-registered non-Go files also support tree-structured
		// operations addressed by outline path. See
		// TheoryOfTreeStructuredEdits.
		if !isTreeStructuredOperation(h.Op, h.Target) || !isTreeStructuredTarget(h.FilePath) {
			return fmt.Errorf("non-Go file %q only supports WRITE, RENAME, DELETE with target=*, REPLACE, INSERT_BEFORE, INSERT_AFTER, or tree-structured operations (MODIFY, ADD_BEFORE, ADD_AFTER, DELETE by outline path) on files with a registered grammar; got op=%q target=%q", h.FilePath, h.Op, h.Target)
		}
	}
	// Go files do not support text-level operations (REPLACE, INSERT_BEFORE,
	// INSERT_AFTER) because the model has difficulty correctly reproducing
	// whitespace characters in the find string, leading to matching failures.
	// Structural operations (MODIFY, ADD_BEFORE, ADD_AFTER, DELETE) use
	// AST-based declaration matching and are always available for Go files.
	// See TheoryOfTextLevelOperations.
	if isGoFile(h.FilePath) && isTextLevelOperation(h.Op) {
		return fmt.Errorf("Go file %q does not support text-level operations (REPLACE, INSERT_BEFORE, INSERT_AFTER); use structural operations (MODIFY, ADD_BEFORE, ADD_AFTER, DELETE) instead", h.FilePath)
	}
	// "package" and "import" are special Go-only targets that support
	// only the MODIFY operation. See TheoryOfSpecialGoTargets.
	if (h.Target == "package" || h.Target == "import") && h.Op != "MODIFY" {
		return fmt.Errorf("target %q only supports MODIFY, got op=%q", h.Target, h.Op)
	}
	// Text-level operations require a non-empty find attribute to locate
	// the unique string anchor in the file. See TheoryOfTextLevelOperations.
	if isTextLevelOperation(h.Op) && h.Find == "" {
		return fmt.Errorf("op %q requires a non-empty find attribute", h.Op)
	}
	return nil
}

// ParseChangeBlock extracts a ChangeBlock from a change block's attributes
// and body. In the function-call format, the change block's metadata
// (op, target, file-path, find) is specified as named parameters on the opening header,
// and the body contains only the complete declaration code or replacement text.
func ParseChangeBlock(block blocks.Block) (h ChangeBlock, ok bool) {
	if block.Kind != "change" {
		return h, false
	}
	op, hasOp := block.Attributes["op"]
	if !hasOp {
		return h, false
	}
	h.Op = op
	h.Target = block.Attributes["target"]
	h.FilePath = block.Attributes["file-path"]
	h.Find = block.Attributes["find"]
	h.Body = block.Body
	return h, true
}

// ParseFirstBoundaryChangeBlock scans content for the first boundary-delimited
// change block, parses its attributes, and returns the resulting ChangeBlock.
func ParseFirstBoundaryChangeBlock(content []byte) (h ChangeBlock, start int, end int, ok bool, err error) {
	block, start, end, ok, err := blocks.ParseFirstBlock(content)
	if err != nil {
		return h, 0, 0, false, err
	}
	if !ok || block.Kind != "change" {
		return h, 0, 0, false, nil
	}

	h, parsedOk := ParseChangeBlock(block)
	if !parsedOk {
		return h, 0, 0, false, nil
	}

	// Validate the operation against the file's capabilities: non-Go files
	// support file-level, text-level, and — when a grammar is registered —
	// tree-structured operations. See TheoryOfNonGoFileChanges and
	// TheoryOfTreeStructuredEdits.
	if err := ValidateChangeBlock(h); err != nil {
		return h, 0, 0, false, err
	}

	return h, start, end, true, nil
}

const ChangeBlockPrompt = `**Change Block Kind:**

Use the "change" kind to define code modifications using the heredoc block format. The opening tag's function-call parameters specify the operation, target, and file path. The body is the complete declaration code.

**Rules:**
- The function-call parameters:
  - ` + "`op`" + `: The operation to perform:
    - MODIFY: Replace an existing top-level declaration. Also supports the special Go-only targets ` + "`package`" + ` and ` + "`import`" + ` to replace the package clause or all import declarations as a group (see below).
    - ADD_BEFORE: Add new code before an existing declaration.
    - ADD_AFTER: Add new code after an existing declaration.
    - DELETE: Remove an existing declaration, or remove an entire file when target is *.
    - RENAME: Rename a file. ` + "`target`" + ` is the new file path, ` + "`file-path`" + ` is the current file path. The code body is ignored and may be empty.
    - WRITE: Replace the entire content of the file specified by ` + "`file-path`" + `. The ` + "`target`" + ` parameter is ignored and may be omitted. The code body is the complete new file content. For Go files, the body must include the package declaration. WRITE should only be used when creating a new file or when the majority of the file content is changing; for small or localized changes, prefer precise modifications (MODIFY, ADD_BEFORE, ADD_AFTER, DELETE, REPLACE, INSERT_BEFORE, INSERT_AFTER).
    - REPLACE: Find a unique string in the file (specified by the ` + "`find`" + ` parameter) and replace it with the body content. The find string must be unique in the file; if it appears multiple times, use WRITE instead. Works on non-Go text files only. For Go files, use structural operations (MODIFY, ADD_BEFORE, ADD_AFTER) instead.
    - INSERT_BEFORE: Insert the body content before a unique anchor string (specified by the ` + "`find`" + ` parameter) in the file. The find string must be unique. Works on non-Go text files only. For Go files, use structural operations (MODIFY, ADD_BEFORE, ADD_AFTER) instead.
    - INSERT_AFTER: Insert the body content after a unique anchor string (specified by the ` + "`find`" + ` parameter) in the file. The find string must be unique. Works on non-Go text files only. For Go files, use structural operations (MODIFY, ADD_BEFORE, ADD_AFTER) instead.
  - ` + "`target`" + `: For MODIFY, ADD_BEFORE, ADD_AFTER, and DELETE operations, the exact name of **exactly ONE** top-level declaration (function, method, type, const, var), or BEGIN/END for file-level operations, or — on non-Go files with a registered grammar — a dotted outline path (see Tree-Structured Targets below). For DELETE, target can also be * to delete the entire file. The target must uniquely identify a single top-level entity. For methods, use TypeName.MethodName or *TypeName.MethodName. For RENAME operation, ` + "`target`" + ` is the new file path (relative or absolute). For WRITE, REPLACE, INSERT_BEFORE, and INSERT_AFTER, ` + "`target`" + ` is ignored.
  - ` + "`find`" + `: For REPLACE, INSERT_BEFORE, and INSERT_AFTER operations, the exact string to search for in the file. The string must be unique (appear exactly once) in the file. If the string cannot be made unique, use WRITE to replace the entire file instead. For other operations, ` + "`find`" + ` is ignored.
- The code body directly follows the opening tag on the next line, with no blank line required before or after it. The code body is the COMPLETE definition of the target entity, including its signature, body, and associated comments. The code block MUST contain ONLY the target entity's definition and MUST NOT include any other top-level declarations. Do NOT use ellipsis (...) or placeholders. The code must be complete and properly formatted. For DELETE and RENAME operations, the code section can be empty. For WRITE, the code body is the complete new file content, including the package declaration for Go files. For REPLACE, the body is the replacement text. For INSERT_BEFORE and INSERT_AFTER, the body is the text to insert.
- **STRICT ONE-ENTITY RULE**: Each change block MUST target exactly ONE top-level entity and contain ONLY that entity's complete definition. If you need to modify or add a type together with its methods, you MUST use SEPARATE blocks for each entity. For example: to add a struct with methods, use one block for the type definition, and individual blocks for each method (targeted as TypeName.MethodName). Do NOT group a type definition with its methods in the same block.
- **Non-Go file support**: For non-Go files (files not ending in .go), file-level operations (WRITE, RENAME, DELETE with target=*) and text-level operations (REPLACE, INSERT_BEFORE, INSERT_AFTER) are always supported. Non-Go files whose format has a registered grammar additionally support tree-structured operations — MODIFY, ADD_BEFORE, ADD_AFTER, and DELETE with a specific target — addressed by outline path (see the Tree-Structured Targets section below). For non-Go files without a registered grammar, use REPLACE, INSERT_BEFORE, or INSERT_AFTER with a unique find string for partial edits, and WRITE for full-file replacement.
- **Go file restriction**: Text-level operations (REPLACE, INSERT_BEFORE, INSERT_AFTER) are not supported for Go files because the model cannot reliably reproduce whitespace characters (indentation, blank lines) in the find string, causing matching failures. For Go files, use structural operations (MODIFY, ADD_BEFORE, ADD_AFTER, DELETE) instead, which use AST-based declaration matching and do not depend on exact whitespace reproduction.

**Prefer Precise Modifications:**
Prefer precise modifications (MODIFY, ADD_BEFORE, ADD_AFTER, DELETE, REPLACE, INSERT_BEFORE, INSERT_AFTER) over WRITE whenever the change is small or localized. WRITE replaces the entire file, which is token-expensive, requires re-reviewing every line, and risks altering unrelated code. Reserve WRITE for creating new files or when the majority of the file content is changing. See TheoryOfPreciseModifications.**Special Go-Only Targets (MODIFY):**

The ` + "`package`" + ` and ` + "`import`" + ` targets are special Go-only targets that support only the MODIFY operation. Use them to make token-efficient modifications to the package clause and import block without requiring WRITE to replace the entire file — essential when moving a file to a different package, renaming a package, or updating imports across dependent files.

- **package**: Replaces the file's package clause (the ` + "`package xxx`" + ` line). The body must be the new package clause (e.g., ` + "`package newpkg`" + `). If the body contains extra declarations, only the package clause is extracted.
- **import**: Replaces ALL import declarations in the file as a group. The body must be the new import block(s) (e.g., ` + "`import (\n\t\"fmt\"\n)`" + `) or individual import declarations. If the file has no existing imports, the new imports are inserted after the package clause. An empty body removes all imports; goimports adds back any imports still needed by the remaining code.
- Both targets run goimports after replacement to ensure valid formatting and import synchronization.` + "\n\n**Tree-Structured Targets (registered non-Go files):**\n\n" + `For non-Go files whose format has a registered grammar (e.g., .py, .js, .rs, .md), the target of MODIFY, ADD_BEFORE, ADD_AFTER, and DELETE is a dotted path of outline symbols — the same definitions the file's structural skeleton shows. Each path segment is ` + "`Kind/Name`" + ` (the kind string exactly as the skeleton shows it, e.g., ` + "`function/parse_args`" + `) or a bare ` + "`Name`" + `. A single-segment target is searched across the whole definition tree and must match exactly one definition; a multi-segment target (e.g., ` + "`class/Config.method/save`" + `) walks the nesting path. MODIFY replaces the target's full line span with the body; ADD_BEFORE and ADD_AFTER insert the body on its own lines before or after that span; DELETE removes the span. The body must be complete lines. When the target's line is indented and the body's first non-empty line is not, the target's indentation is applied to every non-empty body line. The result is re-parsed for validity: an edit that breaks the file's syntax is rejected. Ambiguous or missing targets are reported together with the file's definition list so the path can be corrected in the next round.`
