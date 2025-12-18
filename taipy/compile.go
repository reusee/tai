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
	//TODO compile file to taivm function
	_ = file
	return nil, nil
}

var fileOptions = &syntax.FileOptions{
	Set:             true,
	While:           true,
	TopLevelControl: true,
	GlobalReassign:  true,
}
