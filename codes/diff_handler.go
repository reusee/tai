package codes

import (
	"fmt"

	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/vars"
)

type DiffHandlerName string

var diffHandlerName = cmds.Var[DiffHandlerName]("-diff")

func (Module) DiffHandlerName(
	defaultName DefaultDiffHandlerName,
) DiffHandlerName {
	return vars.FirstNonZero(
		*diffHandlerName,
		DiffHandlerName(defaultName),
	)
}

type DefaultDiffHandlerName DiffHandlerName

func (Module) DefaultDiffHandlerName() DefaultDiffHandlerName {
	return "xml"
}

func (Module) DiffHandler(
	name DiffHandlerName,
	unified UnifiedDiff,
) codetypes.DiffHandler {
	switch name {
	case "unified":
		return unified
	case "xml":
		return XmlDiffHandler{}
	case "":
		return DumbDiffHandler{}
	}
	panic(fmt.Errorf("unknown diff handler: %s", name))
}

type DumbDiffHandler struct{}

var _ codetypes.DiffHandler = DumbDiffHandler{}

func (d DumbDiffHandler) Functions() []*generators.Function {
	return nil
}

func (d DumbDiffHandler) SystemPrompt() string {
	return ""
}

func (d DumbDiffHandler) RestatePrompt() string {
	return ""
}

type XmlDiffHandler struct{}

func (x XmlDiffHandler) Functions() []*generators.Function {
	return nil
}

func (x XmlDiffHandler) SystemPrompt() string {
	return `When you need to propose changes to files, you must output the changes in XML format.
Each individual change must be a <change> element with the following attributes:
- op: one of MODIFY, ADD_BEFORE, ADD_AFTER, DELETE
- target: the name of the declaration to modify, or BEGIN/END for file-level operations.
- file-path: the path to the file to modify.

The body of the <change> element must contain the new code exactly as it should appear.
If the body contains characters that are special in XML (like <, >, &), you MUST enclose the entire body in a <![CDATA[ ... ]]> section.
You may include XML comments (<!-- ... -->) outside the <change> elements for your reasoning.

Example:
<change op="MODIFY" target="Foo" file-path="test.go">
<![CDATA[
func Foo() {
    println("new")
}
]]>
</change>
`
}

func (x XmlDiffHandler) RestatePrompt() string {
	return `Please output your file changes using the specified XML format.`
}