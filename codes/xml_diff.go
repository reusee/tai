package codes

import (
	"bytes"
	"fmt"
	"os"

	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/generators"
)

type XmlDiffHandler struct{}

var _ codetypes.DiffHandler = XmlDiffHandler{}

func (x XmlDiffHandler) Functions() []*generators.Function {
	return nil
}

func (x XmlDiffHandler) SystemPrompt() string {
	return `Your entire response must be a valid XML document with the root element <xml>.
All content, including reasoning, explanations, and file changes, must be inside this <xml> root element.
Do not output any text outside the <xml> document.
You may include XML comments (<!-- ... -->) anywhere for reasoning and explanations.
If no file changes are needed, output a minimal <xml></xml> or include a comment explaining why no changes are necessary.

To propose changes to files, include <change> elements inside the <xml> root, with the following attributes:
- op: one of MODIFY, ADD_BEFORE, ADD_AFTER, DELETE
- target: the name of the declaration to modify, or BEGIN/END for file-level operations.
- file-path: the path to the file to modify.

The body of the <change> element must contain the new code exactly as it should appear.
If the body contains characters that are special in XML (like <, >, &), you generally MUST enclose the entire body in a <![CDATA[ ... ]]> section. However, CDATA sections cannot contain the literal substring ']]>'. If your Go code contains ']]>', using CDATA will cause a parsing error. Therefore, when the code you generate includes ']]>', you MUST NOT use CDATA for that change; instead, escape the special XML characters using standard XML entities: replace '<' with '&lt;', '>' with '&gt;', and '&' with '&amp;'. For all other code, you can continue using CDATA.

Example:
<xml>
<!---
reasoning and explanations
-->
<change op="MODIFY" target="Foo" file-path="test.go">
<![CDATA[
func Foo() {
    println("new")
}
]]>
</change>
</xml>

**General Guidelines:**
- Each <change> must contain the *entire* declaration block, including its signature, body, and associated comments. Do not use ellipsis (...) or placeholders to represent unchanged code.
- **Bugfix Regression Guard**: Every bug fix MUST be accompanied by a reproduction test case in the same set of changes. This ensures the fix is validated and prevents future regressions.
- Do not remove defensive checks, boundary condition handling, or specialized error logic unless they are proven to be unreachable or incorrect. Refactoring for brevity must not sacrifice robustness.
- **Incremental Theory Evolution**: When updating theoretical documentation recorded in global constants, modify only segments related to current changes to maintain continuity. Avoid large-scale replacements or simplifications of existing theoretical text; evolution must be incremental to ensure changes remain reviewable.
- **Language Consistency**: Ensure comments and identifiers within changes use the same language as the surrounding code in the file, regardless of the language of the user's query or the rest of your response. Do not insert comments in the user's input language into code that primarily uses another language.
- **Consistency and Synchrony**: Maintain strict consistency among code, comments, theory constants, and specifications (specs). If a feature is added, modified, or removed, you MUST update the corresponding documentation and specifications in the same set of changes to prevent the system's "Theory" from drifting.
- **Preserve Construction Logic**: Maintain the symbolic structure and construction logic of declarations. If a value is defined using an expression (e.g., string concatenation, bitwise operations, or references to other constants/variables), do not "flatten" or "inline" these into literals during modification. Preserve the modularity and intent of the original code.
- **Emotional Neutrality**: Maintain a purely objective and technical stance. Do not allow emotional, irrational, or extreme language in the request to influence the generated code or the structure of the diff.
- All code within <change> elements must be properly formatted according to the language's conventions.

**Verification and no-op policy:**
- Whitespace-only or formatting-only changes are not valid unless explicitly requested.
- If the requested task is already fully implemented or the code already meets the criteria, explicitly state this and explain why. Do not repeat existing code or provide redundant analysis.
- Before emitting any MODIFY change, verify that at least one meaningful token-level change exists compared to the original code.
- Remove any change that is a no-op. If after verification no effective changes remain, reply with "No changes required." and stop.
`
}

func (x XmlDiffHandler) RestatePrompt() string {
	return `**CRITICAL**: All code modifications MUST be presented as XML <change> elements within an <xml> root as specified in the system prompt. This is not optional. Adhere strictly to the format. Do not output raw code blocks for changes. Do not output MODIFY changes with no changes. Provide appropriate comments to explain non-obvious logic, ensuring that comments and implementation remain synchronized.

**IMPORTANT**: 
1. Each <change> element must contain the COMPLETE declaration. Do not use ellipsis (...) or placeholders. Omissions will break the automated file update process.
2. Every <change> must target exactly ONE top-level declaration. Never group a struct/type definition and its methods in the same <change>. They must be separate.

**CDATA Safety**: CDATA sections cannot contain the literal substring ']]>'. If your generated Go code contains ']]>', do NOT use CDATA for that change; instead, escape the special XML characters using XML entity escaping (replace '<' with '&lt;', '>' with '&gt;', '&' with '&amp;').

Final self-check before answering:
- Does every MODIFY change contain a meaningful change (not just formatting)?
- Is each change targeting exactly one top-level declaration? (Ensure types and methods are NOT grouped).
- Are the changes free of placeholders and complete?
- Did I remove all no-op changes? 

If no effective changes are needed, reply with "No changes required." and stop.
`
}

func (x XmlDiffHandler) Apply(root *os.Root, diffFilePath string) error {
	content, err := os.ReadFile(diffFilePath)
	if err != nil {
		return err
	}

	hunks, remainingContent, err := parseXmlHunks(content)
	if err != nil {
		return err
	}

	for _, h := range hunks {
		if err := applyHunk(root, h); err != nil {
			return fmt.Errorf("hunk %s %s: %w", h.Op, h.Target, err)
		}
	}

	return os.WriteFile(diffFilePath, bytes.TrimSpace(remainingContent), 0644)
}
