package changes

import (
	"fmt"
	"strings"

	"github.com/reusee/tai/blocks"
)

const TheoryOfNonGoFileChanges = `
Non-Go files cannot be structurally parsed to identify top-level declarations.
Therefore, change block operations that require structural identification
(MODIFY, ADD_BEFORE, ADD_AFTER, and DELETE with a specific declaration target)
are only valid for Go files. For non-Go files, file-level operations (WRITE,
RENAME, DELETE with target=*) and text-level operations (REPLACE,
INSERT_BEFORE, INSERT_AFTER) are permitted. Text-level operations use a find
attribute to locate a unique string anchor in the file and apply the edit
relative to that anchor, enabling partial edits without replacing the entire
file. The find string must be unique in the file; if it cannot be made unique,
WRITE must be used to replace the entire file content. Conversely, text-level
operations are not permitted for Go files because the model cannot reliably
reproduce whitespace in the find string; structural operations must be used
instead. See TheoryOfTextLevelOperations for the design rationale.
`

const TheoryOfTextLevelOperations = `
Text-level operations (REPLACE, INSERT_BEFORE, INSERT_AFTER) enable partial
edits to non-Go text files without structural parsing. They use a find
attribute to locate a unique string anchor in the file and apply the edit
relative to that anchor. The find string must appear exactly once in the
file; if it appears zero or multiple times, the operation fails with an
error, and the model must either choose a more specific (longer) find string
or fall back to WRITE for full-file replacement.

REPLACE substitutes the found string with the block body. An empty body
effectively deletes the found text. INSERT_BEFORE inserts the body before the
found anchor; INSERT_AFTER inserts it after. These operations are
particularly useful for non-Go text files (e.g., Markdown, YAML, JSON,
configuration) that cannot be structurally parsed to identify declarations.

Text-level operations are restricted to non-Go files. For Go files,
structural operations (MODIFY, ADD_BEFORE, ADD_AFTER, DELETE) are always
available and must be used instead. The model has difficulty correctly
reproducing whitespace characters (indentation, blank lines, trailing spaces)
in the find string, causing the string match to fail on Go source code where
whitespace is semantically significant for formatting. AST-based declaration
matching used by structural operations does not depend on exact whitespace
reproduction and is therefore more robust for Go files.

The uniqueness requirement is the integrity guarantee: it prevents ambiguous
edits where the model's find string matches multiple locations, which could
silently modify the wrong part of the file. When the model cannot construct a
unique find string (e.g., replacing a common pattern that appears many
times), it must use WRITE to replace the entire file, ensuring the edit is
unambiguous. Line-number-based approaches are deliberately avoided because
models cannot reliably generate accurate line numbers; a unique string anchor
is more robust because it is content-addressed rather than position-addressed.
`

const TheoryOfSpecialGoTargets = `
The "package" and "import" targets are special Go-only targets that support
only the MODIFY operation. They enable token-efficient modification of the
package clause and import block without requiring WRITE to replace the entire
file — essential when moving a file to a different package, renaming a
package, or updating imports across dependent files.

The "package" target replaces the file's package clause (the "package xxx"
line). The body must be the new package clause (e.g., "package newpkg"). If
the body contains extra declarations, only the package clause is extracted.
This target is Go-only; applying it to a non-Go file is rejected by
ValidateChangeBlock because MODIFY is not a file-level operation.

The "import" target replaces ALL import declarations in the file as a group.
The body must be the new import block(s) (e.g., "import (\n\t\"fmt\"\n)") or
individual import declarations. If the file has no existing imports, the new
imports are inserted after the package clause. An empty body removes all
imports; goimports adds back any imports still needed by the remaining code.
This target unifies replacement and insertion into a single "set imports"
operation. It is Go-only for the same reason as "package".

Both targets run goimports after replacement to ensure valid formatting and
import synchronization. If the file does not exist or has no real package
clause, MODIFY is a no-op (consistent with existing MODIFY behavior).
`

const TheoryOfPreciseModifications = `
Precise modifications (MODIFY, ADD_BEFORE, ADD_AFTER, DELETE, REPLACE,
INSERT_BEFORE, INSERT_AFTER) target a specific declaration or text anchor,
touching only the bytes that need to change. WRITE replaces the entire file
content, which is token-expensive and has a large review blast radius: every
byte must be re-verified, and unrelated code may be accidentally altered.
Therefore WRITE should be reserved for two cases: creating a new file from
scratch, or replacing the majority of a file's content. For small or
localized changes, precise modifications must always be preferred. This
principle minimizes token cost, reduces review burden, and preserves the
integrity of untouched code.
` // isGoFile reports whether the given file path has a .go extension.
func isGoFile(path string) bool {
	return strings.HasSuffix(path, ".go")
}

// isFileLevelOperation reports whether the operation does not require
// structural identification of declarations within the file. WRITE replaces
// the entire file content, RENAME renames a file, and DELETE with target=*
// removes an entire file; none of these require parsing the file's structure.
// All other operations (MODIFY, ADD_BEFORE, ADD_AFTER, and DELETE with a
// specific declaration target) require structural identification and are
// only valid for Go files. See TheoryOfNonGoFileChanges.
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
// INSERT_BEFORE, INSERT_AFTER). See TheoryOfNonGoFileChanges and
// TheoryOfTextLevelOperations.
// Go files do not support text-level operations because the model cannot
// reliably reproduce whitespace in the find string; structural operations
// must be used instead. See TheoryOfTextLevelOperations.
// "package" and "import" are special Go-only targets that support only MODIFY.
// See TheoryOfSpecialGoTargets.
func ValidateChangeBlock(h ChangeBlock) error {
	if !isGoFile(h.FilePath) && !isFileLevelOperation(h.Op, h.Target) && !isTextLevelOperation(h.Op) {
		return fmt.Errorf("non-Go file %q only supports WRITE, RENAME, DELETE with target=*, REPLACE, INSERT_BEFORE, or INSERT_AFTER; got op=%q", h.FilePath, h.Op)
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
// and body. In the boundary-delimited format, the change block's metadata
// (op, target, file-path, find) is specified as XML attributes on the opening tag,
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

	// Non-Go files cannot be structurally parsed to identify declarations,
	// so only file-level operations are permitted. See TheoryOfNonGoFileChanges.
	if err := ValidateChangeBlock(h); err != nil {
		return h, 0, 0, false, err
	}

	return h, start, end, true, nil
}

const ChangeBlockPrompt = `**Change Block Kind:**

The "change" kind defines code modifications using the boundary block format. The opening tag's XML attributes specify the operation, target, and file path. The body is the complete declaration code.

**Change Block Format:**

:::<boundary> <change op="<MODIFY|ADD_BEFORE|ADD_AFTER|DELETE|RENAME|WRITE|REPLACE|INSERT_BEFORE|INSERT_AFTER>" target="<declaration_identifier|BEGIN|END|new_file_path>" find="<unique_string_anchor>" file-path="<absolute_path>">
<complete_declaration_code>
:::<boundary> </change>

**Rules:**
- The opening tag attributes:
  - ` + "`op`" + `: The operation to perform:
    - MODIFY: Replace an existing top-level declaration. Also supports the special Go-only targets ` + "`package`" + ` and ` + "`import`" + ` to replace the package clause or all import declarations as a group (see below).
    - ADD_BEFORE: Add new code before an existing declaration.
    - ADD_AFTER: Add new code after an existing declaration.
    - DELETE: Remove an existing declaration, or remove an entire file when target is *.
    - RENAME: Rename a file. ` + "`target`" + ` is the new file path, ` + "`file-path`" + ` is the current file path. The code body is ignored and may be empty.
    - WRITE: Replace the entire content of the file specified by ` + "`file-path`" + `. The ` + "`target`" + ` attribute is ignored and may be omitted. The code body is the complete new file content. For Go files, the body must include the package declaration. WRITE should only be used when creating a new file or when the majority of the file content is changing; for small or localized changes, prefer precise modifications (MODIFY, ADD_BEFORE, ADD_AFTER, DELETE, REPLACE, INSERT_BEFORE, INSERT_AFTER).
    - REPLACE: Find a unique string in the file (specified by the ` + "`find`" + ` attribute) and replace it with the body content. The find string must be unique in the file; if it appears multiple times, use WRITE instead. Works on non-Go text files only. For Go files, use structural operations (MODIFY, ADD_BEFORE, ADD_AFTER) instead.
    - INSERT_BEFORE: Insert the body content before a unique anchor string (specified by the ` + "`find`" + ` attribute) in the file. The find string must be unique. Works on non-Go text files only. For Go files, use structural operations (MODIFY, ADD_BEFORE, ADD_AFTER) instead.
    - INSERT_AFTER: Insert the body content after a unique anchor string (specified by the ` + "`find`" + ` attribute) in the file. The find string must be unique. Works on non-Go text files only. For Go files, use structural operations (MODIFY, ADD_BEFORE, ADD_AFTER) instead.
  - ` + "`target`" + `: For MODIFY, ADD_BEFORE, ADD_AFTER, and DELETE operations, the exact name of **exactly ONE** top-level declaration (function, method, type, const, var) or BEGIN/END for file-level operations. For DELETE, target can also be * to delete the entire file. The target must uniquely identify a single top-level entity. For methods, use TypeName.MethodName or *TypeName.MethodName. For RENAME operation, ` + "`target`" + ` is the new file path (relative or absolute). For WRITE, REPLACE, INSERT_BEFORE, and INSERT_AFTER, ` + "`target`" + ` is ignored.
  - ` + "`find`" + `: For REPLACE, INSERT_BEFORE, and INSERT_AFTER operations, the exact string to search for in the file. The string must be unique (appear exactly once) in the file. If the string cannot be made unique, use WRITE to replace the entire file instead. For other operations, ` + "`find`" + ` is ignored.
- The code body directly follows the opening tag on the next line, with no blank line required before or after it. The code body is the COMPLETE definition of the target entity, including its signature, body, and associated comments. The code block MUST contain ONLY the target entity's definition and MUST NOT include any other top-level declarations. Do NOT use ellipsis (...) or placeholders. The code must be complete and properly formatted. For DELETE and RENAME operations, the code section can be empty. For WRITE, the code body is the complete new file content, including the package declaration for Go files. For REPLACE, the body is the replacement text. For INSERT_BEFORE and INSERT_AFTER, the body is the text to insert.
- **STRICT ONE-ENTITY RULE**: Each change block MUST target exactly ONE top-level entity and contain ONLY that entity's complete definition. If you need to modify or add a type together with its methods, you MUST use SEPARATE blocks for each entity. For example: to add a struct with methods, use one block for the type definition, and individual blocks for each method (targeted as TypeName.MethodName). Do NOT group a type definition with its methods in the same block.
- **Non-Go file restriction**: For non-Go files (files not ending in .go), file-level operations (WRITE, RENAME, DELETE with target=*) and text-level operations (REPLACE, INSERT_BEFORE, INSERT_AFTER) are supported. Operations that require structural identification of declarations (MODIFY, ADD_BEFORE, ADD_AFTER, and DELETE with a specific declaration target) are not valid for non-Go files because the system cannot parse their structure to locate declarations. For partial edits to non-Go files, use REPLACE, INSERT_BEFORE, or INSERT_AFTER with a unique find string. For full-file replacement, use WRITE.
- **Go file restriction**: Text-level operations (REPLACE, INSERT_BEFORE, INSERT_AFTER) are not supported for Go files because the model cannot reliably reproduce whitespace characters (indentation, blank lines) in the find string, causing matching failures. For Go files, use structural operations (MODIFY, ADD_BEFORE, ADD_AFTER, DELETE) instead, which use AST-based declaration matching and do not depend on exact whitespace reproduction.

**Prefer Precise Modifications:**
Prefer precise modifications (MODIFY, ADD_BEFORE, ADD_AFTER, DELETE, REPLACE, INSERT_BEFORE, INSERT_AFTER) over WRITE whenever the change is small or localized. WRITE replaces the entire file, which is token-expensive, requires re-reviewing every line, and risks altering unrelated code. Reserve WRITE for creating new files or when the majority of the file content is changing. See TheoryOfPreciseModifications.` + "**Special Go-Only Targets (MODIFY):**" + `

The ` + "`package`" + ` and ` + "`import`" + ` targets are special Go-only targets that support only the MODIFY operation. They enable token-efficient modification of the package clause and import block without requiring WRITE to replace the entire file — essential when moving a file to a different package, renaming a package, or updating imports across dependent files.

- **package**: Replaces the file's package clause (the ` + "`package xxx`" + ` line). The body must be the new package clause (e.g., ` + "`package newpkg`" + `). If the body contains extra declarations, only the package clause is extracted.
- **import**: Replaces ALL import declarations in the file as a group. The body must be the new import block(s) (e.g., ` + "`import (\n\t\"fmt\"\n)`" + `) or individual import declarations. If the file has no existing imports, the new imports are inserted after the package clause. An empty body removes all imports; goimports adds back any imports still needed by the remaining code.
- Both targets run goimports after replacement to ensure valid formatting and import synchronization.

**Example:**

I analyzed the code and found an issue with the Foo function...
:::徕珑 <change op="MODIFY" target="Foo" file-path="/home/user/foo.go">
// Foo does something important.
func Foo() {
	println("fixed")
}
:::徕珑 </change>
The Bar function is now unused and should be removed...
:::栢彣 <change op="DELETE" target="Bar" file-path="/home/user/foo.go">
:::栢彣 </change>
The unused.go file should be removed entirely...
:::骐骎 <change op="DELETE" target="*" file-path="/home/user/unused.go">
:::骐骎 </change>
The config file needs to be completely rewritten...
:::瑱魃 <change op="WRITE" file-path="/home/user/config.go">
package config

func New() *Config {
	return &Config{}
}
:::瑱魃 </change>
Moving this file to a new package, just update the package clause...
:::羿聕 <change op="MODIFY" target="package" file-path="/home/user/moved.go">
package newpkg
:::羿聕 </change>
Replacing a unique string in a Markdown file...
:::崓嶆 <change op="REPLACE" find="old description text" file-path="/home/user/readme.md">
new description text
:::崓嶆 </change>
Inserting content after a unique anchor in a config file...
:::壴惉 <change op="INSERT_AFTER" find="[dependencies]" file-path="/home/user/Cargo.toml">
serde = { version = "1.0", features = ["derive"] }
:::壴惉 </change>
These changes should resolve the issue.
:::桀骥 <summary>
- Fixed the Foo function
- Removed the unused Bar function
- Deleted the unused.go file
- Rewrote the config file
- Updated the Markdown description
- Added a dependency
:::桀骥 </summary>
`

const ChangeBlockRestatePromptText = `**CRITICAL**: All code modifications MUST use the boundary-delimited format with XML attributes on the opening tag:
:::<boundary> <change op="<MODIFY|ADD_BEFORE|ADD_AFTER|DELETE|RENAME|WRITE|REPLACE|INSERT_BEFORE|INSERT_AFTER>" target="<identifier_or_new_file_path>" find="<unique_string_anchor>" file-path="<absolute_path>">
<complete code>
:::<boundary> </change>

- **ONE ENTITY PER BLOCK**: Each block MUST target exactly ONE top-level entity and contain ONLY that entity's complete definition. Never include multiple top-level declarations in a single block.
- For methods, use TypeName.MethodName or *TypeName.MethodName as the target.
- For RENAME, ` + "`target`" + ` is the new file path; the code body is ignored.
- For DELETE with target *, the entire file is removed; the code body is ignored.
- For WRITE, ` + "`target`" + ` is ignored; the code body is the complete new file content.
- For REPLACE, INSERT_BEFORE, and INSERT_AFTER, use the ` + "`find`" + ` attribute to specify a unique string anchor in the file. The find string must appear exactly once. For REPLACE, the body is the replacement text. For INSERT_BEFORE and INSERT_AFTER, the body is the text to insert before or after the anchor.
- **Non-Go files**: For files not ending in .go, only WRITE, RENAME, DELETE (target=*), REPLACE, INSERT_BEFORE, and INSERT_AFTER are allowed. MODIFY, ADD_BEFORE, ADD_AFTER, and DELETE with a specific target require structural identification and are not supported for non-Go files. For partial edits, use REPLACE, INSERT_BEFORE, or INSERT_AFTER with a unique find string. For full-file replacement, use WRITE.
- **Go files**: Text-level operations (REPLACE, INSERT_BEFORE, INSERT_AFTER) are not supported for Go files because the model cannot reliably reproduce whitespace in find strings. Use structural operations (MODIFY, ADD_BEFORE, ADD_AFTER, DELETE) instead.
- **Special Go-only MODIFY targets**: Use ` + "`target=\"package\"`" + ` to replace the file's package clause, and ` + "`target=\"import\"`" + ` to replace all import declarations as a group. Both run goimports after replacement to ensure valid formatting and import synchronization.
- Include the COMPLETE declaration code of the targeted entity. No ellipsis or placeholders.- **Prefer precise modifications over WRITE**: Use WRITE only when creating a new file or when the majority of the file content is changing. For small or localized changes, use MODIFY, ADD_BEFORE, ADD_AFTER, DELETE, REPLACE, INSERT_BEFORE, or INSERT_AFTER to minimize token cost and review blast radius.
- If no changes are needed, omit all change blocks.
- Even when no change blocks are emitted, a summary block is still required. Generate a summary block with "No changes were needed." as the only bullet point.
`
