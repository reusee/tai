package generators

import (
	"fmt"

	"github.com/reusee/dscope"
	"github.com/reusee/e5"
)

type Scope = dscope.Scope

var (
	pt   = fmt.Printf
	wrap = e5.Wrap.With(e5.WrapStacktrace)
)
