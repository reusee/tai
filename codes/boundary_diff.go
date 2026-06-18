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
	return `**Code Change Output Format (Boundary-Delimited):**

Your response can include reasoning, explanations, and code modifications in any order.
To propose code modifications, use delimited change blocks with a randomly generated boundary string.

**Change Block Format:**

---change <boundary>
op: <MODIFY|ADD_BEFORE|ADD_AFTER|DELETE>
target: <declaration_identifier|BEGIN|END>
file-path: <absolute_path>

<complete_declaration_code>

---end <boundary>

**Rules:**
- <boundary>: Generate a boundary string composed of three random uncommon words separated by hyphens (e.g., 'cobalt-vigil-frost').
  The same boundary MUST be used for both the ---change and ---end markers of a block.
  A sufficiently random boundary ensures it cannot conflict with any code content.
  Use a different boundary for each response.
- <op>: The operation to perform:
  - MODIFY: Replace an existing top-level declaration.
  - ADD_BEFORE: Add new code before an existing declaration.
  - ADD_AFTER: Add new code after an existing declaration.
  - DELETE: Remove an existing declaration.
- <target>: The exact name of a top-level declaration (function, method, type, const, var) 
  or BEGIN/END for file-level operations. For methods, use TypeName.MethodName or *TypeName.MethodName.
- <file-path>: The absolute path to the file being modified.
- <code>: For MODIFY and ADD operations, provide the COMPLETE declaration including its signature,
  body, and associated comments. Do NOT use ellipsis (...) or placeholders.
  The code must be complete and properly formatted. For DELETE operations, the code section can be empty.
- Each change block MUST target exactly ONE top-level declaration.
  Do NOT group a type definition with its methods in the same block.
- Content outside change blocks (including reasoning, explanations, and comments) is preserved verbatim.
- If no changes are needed, simply omit all change blocks.

**Example:**

I analyzed the code and found an issue with the Foo function...

---change cobalt-vigil-frost
op: MODIFY
target: Foo
file-path: <absolute_path>

// Foo does something important.
func Foo() {
	println("fixed")
}

---end cobalt-vigil-frost

The Bar function is now unused and should be removed...

---change cobalt-vigil-frost
op: DELETE
target: Bar
file-path: <absolute_path>

---end cobalt-vigil-frost

These changes should resolve the issue.
`
}

func (b BoundaryDiffHandler) RestatePrompt() string {
	return `**REMINDER**: All code modifications MUST use the boundary-delimited format:
---change <random_boundary>
op: <MODIFY|ADD_BEFORE|ADD_AFTER|DELETE>
target: <identifier>
file-path: <absolute_path>

<complete code>

---end <random_boundary>

- Generate a boundary string of three random uncommon words (e.g., 'cobalt-vigil-frost') for each response.
- Each block targets exactly ONE declaration. Do NOT group.
- Include the COMPLETE declaration code. No ellipsis or placeholders.
- If no changes are needed, omit all change blocks.
`
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
