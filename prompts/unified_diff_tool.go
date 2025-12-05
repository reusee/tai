package prompts

const UnifiedDiffTool = `
You have access to a tool named ` + "`apply_change`" + `. Use this tool to apply modifications to files.
Do not output diffs or code blocks as text. Always use the ` + "`apply_change`" + ` tool.

**Tool Usage:**
Call ` + "`apply_change`" + ` with the following parameters:
- ` + "`operation`" + `: One of "MODIFY", "ADD_BEFORE", "ADD_AFTER", "DELETE".
- ` + "`target`" + `: The unique name of the top-level declaration (function, method, const, type, var) to modify. For methods, use 'TypeName.MethodName'. Use 'BEGIN' or 'END' for file-level operations.
- ` + "`path`" + `: The file path.
- ` + "`content`" + `: The new code content.

**1. To modify an existing top-level declaration:**
Call ` + "`apply_change`" + ` with ` + "`operation='MODIFY'`" + `.
The ` + "`content`" + ` must be the *entire new declaration block*.
Example:
apply_change(operation="MODIFY", target="myFunc", path="main.go", content="func myFunc() {\n  // new content\n}")

**2. To add a top-level declaration:**
Call ` + "`apply_change`" + ` with ` + "`operation='ADD_AFTER'`" + ` (or ` + "`ADD_BEFORE`" + `).
Example:
apply_change(operation="ADD_AFTER", target="existingFunc", path="main.go", content="func newFunc() {}")

**3. To delete a top-level declaration:**
Call ` + "`apply_change`" + ` with ` + "`operation='DELETE'`" + `. ` + "`content`" + ` can be empty.
Example:
apply_change(operation="DELETE", target="oldFunc", path="main.go")

**Important Notes:**
- The content must be the *entire* declaration block, including signature and comments.
- Do not make changes to Context Files.
`

const UnifiedDiffToolRestate = `
**CRITICAL**: You MUST use the ` + "`apply_change`" + ` tool for all code modifications. Do not output raw code blocks or text-based diffs.
- For ` + "`MODIFY`" + `, ensure the ` + "`content`" + ` is the complete replacement for the target declaration.
- Check that ` + "`path`" + ` is correct and the ` + "`target`" + ` exists in the file.
`
