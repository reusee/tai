package prompts

const UnifiedDiff = (`
Output code in the following block-based diff format:

**Hunk Structure:**
Each hunk starts with a header line '[[[ <operation> <target_identifier> IN <file_path>'.
- <operation> can be 'MODIFY', 'ADD_BEFORE', 'ADD_AFTER', or 'DELETE'.
- <target_identifier> is the unique name of a top-level declaration. For Go code, this is strictly limited to functions, methods, and top-level ` + "`const`" + `, ` + "`type`" + `, and ` + "`var`" + ` declarations. For methods, use 'TypeName.MethodName'. For file-level operations (adding to the beginning or end), use 'BEGIN' or 'END'. A filename is not a valid <target_identifier>. The target must be a top-level symbol, not a symbol defined inside a function or method.
- **IMPORTANT**: The 'MODIFY' operation MUST NOT be used with 'BEGIN' or 'END'. Use 'ADD_BEFORE BEGIN' or 'ADD_AFTER END' instead.
- <file_path> is the absolute path to the file being modified.
The hunk must contain the entire targeted declaration block. Do not include ` + "`package`" + ` declarations.

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
- **NO OMISSIONS**: Do not use ` + "`...`" + ` or comments like ` + "`// rest of function`" + ` to represent unchanged code. Every line of the declaration must be present. The output is processed by a program; partial code will result in a broken system.
- Do not include ` + "`package`" + ` declarations or unrelated code outside the target declaration in any hunk.
- All code blocks provided must be 'go fmt' formatted, with proper line-breaks.
- Multiple changes within the same file should be represented by multiple hunks.
- For 'MODIFY' operations, the new block must be different from the original block. Do not output modifications that result in identical code.
- Do not remove defensive checks, boundary condition handling, or specialized error logic unless they are proven to be unreachable or incorrect. Refactoring for brevity must not sacrifice robustness.
- **Incremental Theory Evolution**: When updating theoretical documentation recorded in global constants, modify only segments related to current changes. Superseded or failed theories must be moved to an obsolete theory constant (e.g., ` + "`ObsoleteTheory`" + `) rather than deleted, ensuring historical context and rationale are preserved.
- **Language Consistency**: Ensure comments and identifiers within hunks use the same language as the surrounding code in the file, regardless of the language of the user's query or the rest of your response. Do not insert comments in the user's input language into code that primarily uses another language.

Verification and no-op policy:
- Whitespace-only or formatting-only changes are not valid unless explicitly requested.
- If the requested task is already fully implemented or the code already meets the criteria, explicitly state this and explain why. Do not repeat existing code or generate no-op hunks.
- Before emitting any MODIFY hunk, verify that at least one meaningful token-level change exists compared to the original code.
- Remove any hunk that is a no-op. If after verification no effective changes remain, reply with "No changes required." and do not output any diff.
`)

const UnifiedDiffRestate = (`
**CRITICAL**: All code modifications MUST be presented in the block-based diff format specified in the system prompt, using '[[[ <operation> <target> IN <absolute_file_path>' headers. This is not optional. Adhere strictly to the format. Do not include ` + "`package`" + ` declarations in hunks. Do not output raw code blocks for changes. Do not output MODIFY hunks with no changes. Provide appropriate comments to explain non-obvious logic, ensuring that comments and implementation remain synchronized.

**STRICT NO-OMISSION POLICY**: Every hunk must contain the COMPLETE declaration. Do not use ellipsis ` + "`...`" + ` or placeholders. Omissions will break the automated file update process.

Final self-check before answering:
- For every MODIFY hunk, ensure the new declaration differs meaningfully from the original (not just formatting/comments).
- For theory-related changes, ensure only relevant segments are modified and unrelated rationale is preserved.
- Ensure no hunk contains a ` + "`package`" + ` header.
- Ensure no hunk contains placeholders or code omissions.
- Remove any no-op hunks. If nothing remains, reply with "No changes required." and stop.
`)