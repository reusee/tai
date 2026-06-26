package codes

import (
	"bytes"
	"fmt"
	"os"

	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/generators"
)

// BoundaryDiffHandler implements the DiffHandler interface using a boundary-delimited format.
// Changes are wrapped in ---change <boundary> / ---end <boundary> blocks, where the boundary
// is a random string chosen by the AI to prevent parsing conflicts with code content.
// This format eliminates escape requirements (unlike XML) while maintaining structural parseability.
type BoundaryDiffHandler struct{}

var _ codetypes.DiffHandler = BoundaryDiffHandler{}

func (b BoundaryDiffHandler) Functions() []*generators.Function {
	return nil
}

func (b BoundaryDiffHandler) SystemPrompt() string {
	return ChangeBlockSystemPrompt()
}

func (b BoundaryDiffHandler) RestatePrompt() string {
	return ChangeBlockRestatePrompt()
}

func (b BoundaryDiffHandler) Apply(root *os.Root, diffFilePath string) error {
	content, err := os.ReadFile(diffFilePath)
	if err != nil {
		return err
	}

	for {
		h, start, end, ok := parseFirstBoundaryHunk(content)
		if !ok {
			break
		}
		if err := applyHunk(root, h); err != nil {
			return fmt.Errorf("hunk %s %s: %w", h.Op, h.Target, err)
		}
		newContent := append(content[:start], content[end:]...)
		if err := os.WriteFile(diffFilePath, bytes.TrimSpace(newContent), 0644); err != nil {
			return err
		}
		content, err = os.ReadFile(diffFilePath)
		if err != nil {
			return err
		}
	}

	return nil
}

func ChangeBlockSystemPrompt() string {
	return `**Code Change Output Format (Boundary-Delimited with XML Metadata):**

Your response can include reasoning, explanations, and code modifications in any order.
To propose code modifications, use delimited change blocks with a randomly generated boundary string.

**Change Block Format:**

---change <boundary>
<change op="<MODIFY|ADD_BEFORE|ADD_AFTER|DELETE|RENAME>" target="<declaration_identifier|BEGIN|END|new_file_path>" file-path="<absolute_path>" />

<complete_declaration_code>

---end <boundary>

**Rules:**
- <boundary>: Generate a boundary string composed of two random uncommon meaningless Chinese characters.
  The same boundary MUST be used for both the ---change and ---end markers of a block.
  A sufficiently random boundary ensures it cannot conflict with any code content.
  Use a different boundary for each response.
- The metadata is a self-closing XML tag: ` + "`<change op=\"...\" target=\"...\" file-path=\"...\" />`" + `.
  - ` + "`op`" + `: The operation to perform:
    - MODIFY: Replace an existing top-level declaration.
    - ADD_BEFORE: Add new code before an existing declaration.
    - ADD_AFTER: Add new code after an existing declaration.
    - DELETE: Remove an existing declaration.
    - RENAME: Rename a file. ` + "`" + `target` + "`" + ` is the new file path, ` + "`" + `file-path` + "`" + ` is the current file path. The code block is ignored and may be empty.
  - ` + "`target`" + `: For MODIFY, ADD_BEFORE, ADD_AFTER, and DELETE operations, the exact name of **exactly ONE** top-level declaration (function, method, type, const, var) or BEGIN/END for file-level operations. The target must uniquely identify a single top-level entity. For methods, use TypeName.MethodName or *TypeName.MethodName. For RENAME operation, ` + "`" + `target` + "`" + ` is the new file path (relative or absolute).
  - ` + "`file-path`" + `: The absolute path to the file being modified.
- A blank line separates the XML tag from the code body. The code body is the COMPLETE definition of the target entity, including its signature, body, and associated comments. The code block MUST contain ONLY the target entity's definition and MUST NOT include any other top-level declarations. Do NOT use ellipsis (...) or placeholders. The code must be complete and properly formatted. For DELETE and RENAME operations, the code section can be empty.
- **STRICT ONE-ENTITY RULE**: Each change block MUST target exactly ONE top-level entity and contain ONLY that entity's complete definition. If you need to modify or add a type together with its methods, you MUST use SEPARATE blocks for each entity. For example: to add a struct with methods, use one block for the type definition, and individual blocks for each method (targeted as TypeName.MethodName). Do NOT group a type definition with its methods in the same block.
- Content outside change blocks (including reasoning, explanations, and comments) is preserved verbatim.
- If no changes are needed, simply omit all change blocks.

**Example:**

I analyzed the code and found an issue with the Foo function...

---change 徕珑
<change op="MODIFY" target="Foo" file-path="/home/user/foo.go" />

// Foo does something important.
func Foo() {
	println("fixed")
}

---end 徕珑

The Bar function is now unused and should be removed...

---change 徕珑
<change op="DELETE" target="Bar" file-path="/home/user/foo.go" />

---end 徕珑

These changes should resolve the issue.
`
}

func ChangeBlockRestatePrompt() string {
	return `**CRITICAL**: All code modifications MUST use the boundary-delimited format with an XML metadata tag:
---change <random_boundary>
<change op="<MODIFY|ADD_BEFORE|ADD_AFTER|DELETE|RENAME>" target="<identifier_or_new_file_path>" file-path="<absolute_path>" />

<complete code>

---end <random_boundary>

- Generate a boundary string of two random uncommon meaningless Chinese characters for each response.
- The metadata is a self-closing XML tag: ` + "`<change op=\"...\" target=\"...\" file-path=\"...\" />`" + `.
- **ONE ENTITY PER BLOCK**: Each block MUST target exactly ONE top-level declaration and contain ONLY that entity's complete definition. Never include multiple top-level declarations in a single block.
- For methods, use TypeName.MethodName or *TypeName.MethodName as the target.
- For RENAME, ` + "`" + `target` + "`" + ` is the new file path; the code block is ignored.
- Include the COMPLETE declaration code of the targeted entity. No ellipsis or placeholders.
- If no changes are needed, omit all change blocks.
`
}