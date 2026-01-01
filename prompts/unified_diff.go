package prompts

const UnifiedDiff = (`
Output code in the following block-based diff format:

**Hunk Structure:**
Each hunk starts with a header line '[[[ <operation> <target_identifier> IN <file_path>'.
- <operation> can be 'MODIFY', 'ADD_BEFORE', 'ADD_AFTER', or 'DELETE'.
- <target_identifier> is the unique name of a top-level declaration. For Go code, this is strictly limited to functions, methods, and top-level ` + "`const`" + `, ` + "`type`" + `, and ` + "`var`" + ` declarations. For methods, use 'TypeName.MethodName'. For file-level operations (adding to the beginning or end), use 'BEGIN' or 'END'. A filename is not a valid <target_identifier>. The target must be a top-level symbol, not a symbol defined inside a function or method.
- **IMPORTANT**: The 'MODIFY' operation MUST NOT be used with 'BEGIN' or 'END'. Use 'ADD_BEFORE BEGIN' or 'ADD_AFTER END' instead.
- <file_path> is the absolute path to the file being modified.
The hunk must contain the entire new declaration block.

**1. To modify an existing top-level declaration:**
- Use '[[[ MODIFY <declaration_identifier> IN <file_path>' as the hunk header.
- The hunk must contain the *entire new declaration block*. This means the complete code for the declaration *after* your modifications.
- This operation is idempotent. If the target declaration does not exist, it has no effect.
Example:
` + "[[[ MODIFY myFunc IN /absolute/path/to/file.go" + `
func myFunc() {
      // new content
}
` + "]]]" + `
` + "[[[ MODIFY MyType.MyMethod IN /absolute/path/to/file.go" + `
func (t MyType) MyMethod() {
	// new content
}
` + "]]]" + `
` + "[[[ MODIFY MyConst IN /absolute/path/to/file.go" + `
const (
	MyConst = 1
)
` + "]]]" + `
` + "[[[ MODIFY MyVar IN /absolute/path/to/file.go" + `
var (
	MyVar float64
	MyVar2 string
)
` + "]]]" + `
` + "[[[ MODIFY MyType IN /absolute/path/to/file.go" + `
type MyType struct {
	I int64
}
` + "]]]" + `

**2. To add or update a top-level declaration:**
- This operation is idempotent. It ensures a declaration exists in a specific state and position.
- If a declaration with the same name as the one in the hunk's content already exists, it will be replaced (acting like a ` + "`MODIFY`" + ` operation).
- If the declaration does not exist, it will be added at the location specified by the < target_identifier >.
- To add before an existing declaration: '[[[ ADD_BEFORE <existing_declaration_identifier> IN <file_path>'
- To add after an existing declaration: '[[[ ADD_AFTER <existing_declaration_identifier> IN <file_path>'
- To add at the beginning of the file: '[[[ ADD_BEFORE BEGIN IN <file_path>'
- To add at the end of the file: '[[[ ADD_AFTER END IN <file_path>'
- The hunk must contain the *entire new declaration block*.
Example (add newFunc after existingFunc):
` + "[[[ ADD_AFTER existingFunc IN /absolute/path/to/file.go" + `
func newFunc() {
      // new content
}
` + "]]]" + `
` + "[[[ ADD_AFTER MyType.MyMethod IN /absolute/path/to/file.go" + `
func (t MyType) Foo() {
	 // new content
}
` + "]]]" + `

Example (add new test function):
` + "[[[ ADD_AFTER END IN /absolute/path/to/file_test.go" + `
func TestMyType(t *testing.T) {
	// new content
}
` + "]]]" + `

**3. To delete a top-level declaration:**
- Use '[[[ DELETE <declaration_identifier> IN <file_path>' as the hunk header.
- The hunk for a DELETE operation consists only of the header line, without a code block.
- This operation is idempotent. If the declaration does not exist, it has no effect.
Example:
` + "[[[ DELETE oldFunction IN /absolute/path/to/file.go ]]]" + `
` + "[[[ DELETE MyVar IN /absolute/path/to/file.go ]]]" + `

**Important Notes:**
- The content within the code fence must be the *entire* declaration block, including its signature, body, and associated comments (if applicable to the change).
- All code blocks provided must be 'go fmt' formatted, with proper line-breaks.
- Multiple changes within the same file should be represented by multiple hunks.
- For 'MODIFY' operations, the new block must be different from the original block. Do not output modifications that result in identical code.

Verification and no-op policy:
- Whitespace-only or formatting-only changes are not valid unless explicitly requested.
- Before emitting any MODIFY hunk, verify that at least one meaningful token-level change exists compared to the original code.
- Remove any hunk that is a no-op. If after verification no effective changes remain, reply with "No changes required." and do not output any diff.
`)

const UnifiedDiffRestate = (`
**CRITICAL**: All code modifications MUST be presented in the block-based diff format specified in the system prompt, using '[[[ <operation> <target> IN <absolute_file_path>' headers. This is not optional. Adhere strictly to the format. Do not output raw code blocks for changes. Do not output MODIFY hunks with no changes. Prioritize self-explanatory code over comments.

Final self-check before answering:
- For every MODIFY hunk, ensure the new declaration differs meaningfully from the original (not just formatting/comments).
- Remove any no-op hunks. If nothing remains, reply with "No changes required." and stop.
`)
