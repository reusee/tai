package codes

import "github.com/reusee/tai/generators"

type XmlDiffHandler struct{}

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
If the body contains characters that are special in XML (like <, >, &), you MUST enclose the entire body in a <![CDATA[ ... ]]> section.

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
`
}

func (x XmlDiffHandler) RestatePrompt() string {
	return `Please ensure your entire response is a valid XML document with the root element <xml>. Do not output any text outside the <xml> root.`
}
