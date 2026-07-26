package blocks

import (
	"fmt"
	"strings"

	"github.com/reusee/tai/codes/codetypes"
)

const TheoryOfNonGoFileChanges = `
Non-Go files cannot be structurally parsed to identify top-level declarations.
Therefore, change block operations that require structural identification
(MODIFY, ADD_BEFORE, ADD_AFTER, and DELETE with a specific declaration target)
are only valid for Go files. For non-Go files, only file-level operations are
permitted: WRITE replaces the entire file content, RENAME renames the file,
and DELETE with target=* removes the entire file. This restriction ensures the
system never attempts to locate a declaration within a file format it cannot
parse, which would silently fail or produce incorrect results.
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
ValidateChangeBlockHunk because MODIFY is not a file-level operation.

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

// isGoFile reports whether the given file path has a .go extension.
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

// ValidateChangeBlockHunk validates that the hunk's operation is valid for
// the target file type. Non-Go files only support file-level operations
// (WRITE, RENAME, DELETE with target=*). See TheoryOfNonGoFileChanges.
// "package" and "import" are special Go-only targets that support only MODIFY.
// See TheoryOfSpecialGoTargets.
func ValidateChangeBlockHunk(h codetypes.Hunk) error {
	if !isGoFile(h.FilePath) && !isFileLevelOperation(h.Op, h.Target) {
		return fmt.Errorf("non-Go file %q only supports WRITE, RENAME, or DELETE with target=*; got op=%q", h.FilePath, h.Op)
	}
	// "package" and "import" are special Go-only targets that support
	// only the MODIFY operation. See TheoryOfSpecialGoTargets.
	if (h.Target == "package" || h.Target == "import") && h.Op != "MODIFY" {
		return fmt.Errorf("target %q only supports MODIFY, got op=%q", h.Target, h.Op)
	}
	return nil
}

// ParseChangeBlock extracts a Hunk from a change block's attributes and body.
// In the boundary-delimited format, the change block's metadata (op, target,
// file-path) is specified as XML attributes on the opening tag, and the body
// contains only the complete declaration code.
func ParseChangeBlock(block Block) (h codetypes.Hunk, ok bool) {
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
	h.Body = block.Body
	return h, true
}

// ParseFirstBoundaryHunk scans content for the first boundary-delimited change block,
// parses its attributes, and returns the resulting Hunk.
func ParseFirstBoundaryHunk(content []byte) (h codetypes.Hunk, start int, end int, ok bool, err error) {
	block, start, end, ok, err := ParseFirstBlock(content)
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
	if err := ValidateChangeBlockHunk(h); err != nil {
		return h, 0, 0, false, err
	}

	return h, start, end, true, nil
}

const ChangeBlockSystemPrompt = `**Change Block Kind:**

The "change" kind defines code modifications using the boundary block format. The opening tag's XML attributes specify the operation, target, and file path. The body is the complete declaration code.

**Change Block Format:**

:::<boundary> <change op="<MODIFY|ADD_BEFORE|ADD_AFTER|DELETE|RENAME|WRITE>" target="<declaration_identifier|BEGIN|END|new_file_path>" file-path="<absolute_path>">
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
    - WRITE: Replace the entire content of the file specified by ` + "`file-path`" + `. The ` + "`target`" + ` attribute is ignored and may be omitted. The code body is the complete new file content. For Go files, the body must include the package declaration.
  - ` + "`target`" + `: For MODIFY, ADD_BEFORE, ADD_AFTER, and DELETE operations, the exact name of **exactly ONE** top-level declaration (function, method, type, const, var) or BEGIN/END for file-level operations. For DELETE, target can also be * to delete the entire file. The target must uniquely identify a single top-level entity. For methods, use TypeName.MethodName or *TypeName.MethodName. For RENAME operation, ` + "`target`" + ` is the new file path (relative or absolute). For WRITE operation, ` + "`target`" + ` is ignored.
- The code body directly follows the opening tag on the next line, with no blank line required before or after it. The code body is the COMPLETE definition of the target entity, including its signature, body, and associated comments. The code block MUST contain ONLY the target entity's definition and MUST NOT include any other top-level declarations. Do NOT use ellipsis (...) or placeholders. The code must be complete and properly formatted. For DELETE and RENAME operations, the code section can be empty. For WRITE, the code body is the complete new file content, including the package declaration for Go files.
- **STRICT ONE-ENTITY RULE**: Each change block MUST target exactly ONE top-level entity and contain ONLY that entity's complete definition. If you need to modify or add a type together with its methods, you MUST use SEPARATE blocks for each entity. For example: to add a struct with methods, use one block for the type definition, and individual blocks for each method (targeted as TypeName.MethodName). Do NOT group a type definition with its methods in the same block.
- **Non-Go file restriction**: For non-Go files (files not ending in .go), only file-level operations are supported: WRITE (replace entire file content), RENAME (rename the file), and DELETE with target=* (delete the entire file). Operations that require structural identification of declarations (MODIFY, ADD_BEFORE, ADD_AFTER, and DELETE with a specific declaration target) are not valid for non-Go files because the system cannot parse their structure to locate declarations. To update a non-Go file, use WRITE to replace the entire file content.

**Special Go-Only Targets (MODIFY):**

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
These changes should resolve the issue.
:::桀骥 <finish>
Fixed the Foo function, removed the unused Bar function, deleted the unused.go file, and rewrote the config file.
:::桀骥 </finish>
`

const ChangeBlockRestatePrompt = `**CRITICAL**: All code modifications MUST use the boundary-delimited format with XML attributes on the opening tag:
:::<boundary> <change op="<MODIFY|ADD_BEFORE|ADD_AFTER|DELETE|RENAME|WRITE>" target="<identifier_or_new_file_path>" file-path="<absolute_path>">
<complete code>
:::<boundary> </change>

- **ONE ENTITY PER BLOCK**: Each block MUST target exactly ONE top-level entity and contain ONLY that entity's complete definition. Never include multiple top-level declarations in a single block.
- For methods, use TypeName.MethodName or *TypeName.MethodName as the target.
- For RENAME, ` + "`target`" + ` is the new file path; the code body is ignored.
- For DELETE with target *, the entire file is removed; the code body is ignored.
- For WRITE, ` + "`target`" + ` is ignored; the code body is the complete new file content.
- **Non-Go files**: For files not ending in .go, only WRITE, RENAME, and DELETE (target=*) are allowed. MODIFY, ADD_BEFORE, ADD_AFTER, and DELETE with a specific target require structural identification and are not supported for non-Go files. Use WRITE to replace the entire file content.
- **Special Go-only MODIFY targets**: Use ` + "`target=\"package\"`" + ` to replace the file's package clause, and ` + "`target=\"import\"`" + ` to replace all import declarations as a group. Both run goimports after replacement to ensure valid formatting and import synchronization.
- Include the COMPLETE declaration code of the targeted entity. No ellipsis or placeholders.
- If no changes are needed, omit all change blocks.
- Even when no change blocks are emitted, a finish block is still required. Generate a finish block with "No changes were needed." as the summary.
`
