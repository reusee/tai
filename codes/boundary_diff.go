package codes

import (
	"bytes"
	"fmt"
	"iter"
	"os"

	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/generators"
)

// BoundaryDiffHandler implements the DiffHandler interface using a boundary-delimited format.
// Changes are wrapped in :::change <boundary> / :::end <boundary> blocks, where the boundary
// is a random string chosen by the AI to prevent parsing conflicts with code content.
// This format eliminates escape requirements (unlike XML) while maintaining structural parseability.
type BoundaryDiffHandler struct{}

var _ codetypes.DiffHandler = BoundaryDiffHandler{}

func (b BoundaryDiffHandler) Functions() []*generators.Function {
	return nil
}

func (b BoundaryDiffHandler) SystemPrompt() string {
	return BlockFormatSystemPrompt + "\n" +
		`**Change Block Kind:**

The "change" kind defines code modifications using the boundary block format. Each change block contains an XML metadata tag specifying the operation, target, and file path, followed by the complete declaration code.

**Change Block Format:**

:::change <boundary>
<change op="<MODIFY|ADD_BEFORE|ADD_AFTER|DELETE|RENAME|WRITE>" target="<declaration_identifier|BEGIN|END|new_file_path>" file-path="<absolute_path>" />
<complete_declaration_code>
:::end <boundary>

**Rules:**
- The metadata is a self-closing XML tag: ` + "`<change op=\"...\" target=\"...\" file-path=\"...\" />`" + `
  - ` + "`op`" + `: The operation to perform:
    - MODIFY: Replace an existing top-level declaration.
    - ADD_BEFORE: Add new code before an existing declaration.
    - ADD_AFTER: Add new code after an existing declaration.
    - DELETE: Remove an existing declaration.
    - RENAME: Rename a file. ` + "`" + `target` + "`" + ` is the new file path, ` + "`" + `file-path` + "`" + ` is the current file path. The code block is ignored and may be empty.
    - WRITE: Replace the entire content of the file specified by ` + "`" + `file-path` + "`" + `. The ` + "`" + `target` + "`" + ` field is ignored and may be omitted. The code body is the complete new file content. For Go files, the body must include the package declaration.
  - ` + "`target`" + `: For MODIFY, ADD_BEFORE, ADD_AFTER, and DELETE operations, the exact name of **exactly ONE** top-level declaration (function, method, type, const, var) or BEGIN/END for file-level operations. The target must uniquely identify a single top-level entity. For methods, use TypeName.MethodName or *TypeName.MethodName. For RENAME operation, ` + "`" + `target` + "`" + ` is the new file path (relative or absolute). For WRITE operation, ` + "`" + `target` + "`" + ` is ignored.
  - ` + "`file-path`" + `: The absolute path to the file being modified.
- The code body directly follows the XML tag on the next line, with no blank line required before or after it. The code body is the COMPLETE definition of the target entity, including its signature, body, and associated comments. The code block MUST contain ONLY the target entity's definition and MUST NOT include any other top-level declarations. Do NOT use ellipsis (...) or placeholders. The code must be complete and properly formatted. For DELETE and RENAME operations, the code section can be empty. For WRITE, the code body is the complete new file content, including the package declaration for Go files.
- **STRICT ONE-ENTITY RULE**: Each change block MUST target exactly ONE top-level entity and contain ONLY that entity's complete definition. If you need to modify or add a type together with its methods, you MUST use SEPARATE blocks for each entity. For example: to add a struct with methods, use one block for the type definition, and individual blocks for each method (targeted as TypeName.MethodName). Do NOT group a type definition with its methods in the same block.
- **Boundary uniqueness**: Use a distinct, freshly generated random boundary for each block. Never reuse a boundary string shown in the examples below; the parser matches the first :::end <boundary> marker, so a reused boundary closes the wrong block and corrupts the output.
- No blank lines are required before or after a block. A block can appear on consecutive lines with other text or other blocks, but every marker must start at the beginning of its own line.
- **Line-start requirement**: Every marker (:::change ... and :::end ...) MUST start at the beginning of its own line. Never glue a marker to the end of a prose line; always start it on a new line.

**Example:**

I analyzed the code and found an issue with the Foo function...
:::change 徕珑
<change op="MODIFY" target="Foo" file-path="/home/user/foo.go" />
// Foo does something important.
func Foo() {
	println("fixed")
}
:::end 徕珑
The Bar function is now unused and should be removed...
:::change 栢彣
<change op="DELETE" target="Bar" file-path="/home/user/foo.go" />
:::end 栢彣
The config file needs to be completely rewritten...
:::change 瑱魃
<change op="WRITE" file-path="/home/user/config.go" />
package config

func New() *Config {
	return &Config{}
}
:::end 瑱魃
These changes should resolve the issue.
:::finish 桀骥
Fixed the Foo function, removed the unused Bar function, and rewrote the config file.
:::end 桀骥

Note: Each block above uses a distinct boundary (徕珑, 栢彣, 瑱魃, 桀骥) for illustration only. **Never reuse these or any boundary string that appears in this prompt.** Generate a fresh random pair of two uncommon, meaningless Chinese characters for every block.
`
}

func (b BoundaryDiffHandler) RestatePrompt() string {
	return `**CRITICAL**: All code modifications MUST use the boundary-delimited format with an XML metadata tag:
:::change <random_boundary>
<change op="<MODIFY|ADD_BEFORE|ADD_AFTER|DELETE|RENAME|WRITE>" target="<identifier_or_new_file_path>" file-path="<absolute_path>" />
<complete code>
:::end <random_boundary>

- Generate a boundary string of two random uncommon meaningless Chinese characters for each block. Each block in the response MUST use a distinct boundary.
- **Never reuse a boundary string that appears in the system prompt examples** (such as 徕珑, 栢彣, 瑱魃, or 桀骥). Reusing an example boundary causes the parser to close the wrong block. Always generate a fresh random pair that does not appear anywhere in this prompt.
- The metadata is a self-closing XML tag: ` + "`<change op=\"...\" target=\"...\" file-path=\"...\" />`" + `
- **ONE ENTITY PER BLOCK**: Each block MUST target exactly ONE top-level declaration and contain ONLY that entity's complete definition. Never include multiple top-level declarations in a single block.
- For methods, use TypeName.MethodName or *TypeName.MethodName as the target.
- For RENAME, ` + "`" + `target` + "`" + ` is the new file path; the code block is ignored.
- For WRITE, ` + "`" + `target` + "`" + ` is ignored; the code body is the complete new file content.
- Include the COMPLETE declaration code of the targeted entity. No ellipsis or placeholders.
- No blank lines are required before or after the code body, nor before or after a block.
- **Line-start requirement**: Each marker (:::change ... and :::end ...) MUST start at the beginning of its own line. Never append a marker to the end of a prose line; always start it on a new line after a newline.
- If no changes are needed, omit all change blocks.
`
}

func (b BoundaryDiffHandler) Apply(root *os.Root, diffFilePath string) iter.Seq2[codetypes.Hunk, error] {
	return func(yield func(codetypes.Hunk, error) bool) {
		content, err := os.ReadFile(diffFilePath)
		if err != nil {
			yield(codetypes.Hunk{}, err)
			return
		}
		cursor := 0
		for {
			block, relStart, relEnd, ok, err := ParseFirstBlock(content[cursor:])
			if err != nil {
				yield(codetypes.Hunk{}, err)
				return
			}
			if !ok {
				break
			}
			start := cursor + relStart
			end := cursor + relEnd
			// Non-change blocks (e.g., finish summary) carry no file
			// modifications and are preserved in the diff file. Only
			// successfully applied change blocks are removed. See
			// TheoryOfFinishBlock.
			if block.Kind != "change" {
				cursor = end
				continue
			}
			h, parsedOk := parseChangeXMLBody(block.Body)
			if !parsedOk {
				// Unparseable change blocks are not applied and therefore
				// preserved rather than deleted from the diff file.
				cursor = end
				continue
			}
			if err := applyHunk(root, h); err != nil {
				yield(h, fmt.Errorf("hunk %s %s: %w", h.Op, h.Target, err))
				return
			}
			// Remove only the applied change block from the diff file. The
			// in-memory content is kept untrimmed so block offsets stay
			// stable for subsequent searches; the persisted file is trimmed
			// for cleanliness.
			newContent := append(content[:start], content[end:]...)
			if err := os.WriteFile(diffFilePath, bytes.TrimSpace(newContent), 0644); err != nil {
				yield(codetypes.Hunk{}, err)
				return
			}
			content = newContent
			// Everything before `start` has already been processed (preserved
			// non-change blocks or previously removed change blocks), so
			// resume searching from `start`, clamped to the content length.
			cursor = start
			if cursor > len(content) {
				cursor = len(content)
			}
			if !yield(h, nil) {
				return
			}
		}
	}
}