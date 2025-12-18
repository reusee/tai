package taipy

import (
	"io"

	"github.com/reusee/tai/taivm"
	"go.starlark.net/syntax"
)

func Compile(name string, source io.Reader) (*taivm.Function, error) {
	file, err := fileOptions.Parse(name, source, 0)
	if err != nil {
		return nil, err
	}

	c := newCompiler(name)
	if err := c.compileStmts(file.Stmts); err != nil {
		return nil, err
	}
	// Implicit return nil at end of module/function
	c.emit(taivm.OpLoadConst.With(c.addConst(nil)))
	c.emit(taivm.OpReturn)

	return c.toFunction(), nil
}

var fileOptions = &syntax.FileOptions{
	Set:             true,
	While:           true,
	TopLevelControl: true,
	GlobalReassign:  true,
}
