package generators

import (
	"github.com/reusee/dscope"
	"github.com/reusee/e5"
)

type Scope = dscope.Scope

var (
	wrap = e5.Wrap.With(e5.WrapStacktrace)
)
