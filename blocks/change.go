package blocks

import (
	"github.com/reusee/tai/codes/codetypes"
)

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
    - MODIFY: Replace an existing top-level declaration.
    - ADD_BEFORE: Add new code before an existing declaration.
    - ADD_AFTER: Add new code after an existing declaration.
    - DELETE: Remove an existing declaration, or remove an entire file when target is *.
    - RENAME: Rename a file. ` + "`target`" + ` is the new file path, ` + "`file-path`" + ` is the current file path. The code body is ignored and may be empty.
    - WRITE: Replace the entire content of the file specified by ` + "`file-path`" + `. The ` + "`target`" + ` attribute is ignored and may be omitted. The code body is the complete new file content. For Go files, the body must include the package declaration.
  - ` + "`target`" + `: For MODIFY, ADD_BEFORE, ADD_AFTER, and DELETE operations, the exact name of **exactly ONE** top-level declaration (function, method, type, const, var) or BEGIN/END for file-level operations. For DELETE, target can also be * to delete the entire file. The target must uniquely identify a single top-level entity. For methods, use TypeName.MethodName or *TypeName.MethodName. For RENAME operation, ` + "`target`" + ` is the new file path (relative or absolute). For WRITE operation, ` + "`target`" + ` is ignored.
  - ` + "`file-path`" + `: The absolute path to the file being modified.
- The code body directly follows the opening tag on the next line, with no blank line required before or after it. The code body is the COMPLETE definition of the target entity, including its signature, body, and associated comments. The code block MUST contain ONLY the target entity's definition and MUST NOT include any other top-level declarations. Do NOT use ellipsis (...) or placeholders. The code must be complete and properly formatted. For DELETE and RENAME operations, the code section can be empty. For WRITE, the code body is the complete new file content, including the package declaration for Go files.
- **STRICT ONE-ENTITY RULE**: Each change block MUST target exactly ONE top-level entity and contain ONLY that entity's complete definition. If you need to modify or add a type together with its methods, you MUST use SEPARATE blocks for each entity. For example: to add a struct with methods, use one block for the type definition, and individual blocks for each method (targeted as TypeName.MethodName). Do NOT group a type definition with its methods in the same block.

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
These changes should resolve the issue.
:::桀骥 <finish>
Fixed the Foo function, removed the unused Bar function, deleted the unused.go file, and rewrote the config file.
:::桀骥 </finish>
`

const ChangeBlockRestatePrompt = `**CRITICAL**: All code modifications MUST use the boundary-delimited format with XML attributes on the opening tag:
:::<boundary> <change op="<MODIFY|ADD_BEFORE|ADD_AFTER|DELETE|RENAME|WRITE>" target="<identifier_or_new_file_path>" file-path="<absolute_path>">
<complete code>
:::<boundary> </change>

- **ONE ENTITY PER BLOCK**: Each block MUST target exactly ONE top-level declaration and contain ONLY that entity's complete definition. Never include multiple top-level declarations in a single block.
- For methods, use TypeName.MethodName or *TypeName.MethodName as the target.
- For RENAME, ` + "`target`" + ` is the new file path; the code body is ignored.
- For DELETE with target *, the entire file is removed; the code body is ignored.
- For WRITE, ` + "`target`" + ` is ignored; the code body is the complete new file content.
- Include the COMPLETE declaration code of the targeted entity. No ellipsis or placeholders.
- If no changes are needed, omit all change blocks.
`
