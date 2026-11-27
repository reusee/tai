package pathutil

import (
	"os"

	"github.com/reusee/dscope"
)

// Module is the dscope module of the tai dir. Embedding it makes the
// process's tai dir resolvable from the scope, so every package that
// persists state depends on the one type instead of resolving a directory of
// its own. See TheoryOfTaiDir.
type Module struct {
	dscope.Module
}

// ResolveTaiDir resolves the tai dir of the process working directory. It is
// the one resolution the command performs: the value feeds the sandbox and
// the scope, so a state file never lands outside the writable directory. A
// working directory that cannot be determined yields the relative working
// directory, so a caller always receives a usable value.
// See TheoryOfTaiDir.
func ResolveTaiDir() TaiDir {
	cwd, err := os.Getwd()
	if err != nil {
		return TaiDir(".")
	}
	return TaiDir(FindTaiDir(cwd))
}

// TaiDir provides the tai dir of the process working directory, the default
// the command overrides with its own single resolution. See TheoryOfTaiDir.
func (Module) TaiDir() TaiDir {
	return ResolveTaiDir()
}
