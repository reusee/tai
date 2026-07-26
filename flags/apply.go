package flags

import (
	"cuelang.org/go/cue"

	"github.com/reusee/tai/configs"
)

const TheoryOfImmediateApply = `
Immediate apply enables change blocks parsed from streamed model output to be
applied to the working tree as soon as they are parsed during streaming, rather
than buffering all output and applying after the full generation session finishes.
This reuses the ParserState decorator (shared with dynamic context) to intercept
change blocks from model output. ParserState is activated when either dynamic
context or immediate apply is enabled, because both features parse structured
blocks from streamed output. An apply error aborts generation immediately so the
user can inspect the partial state and the failing change block rather than continuing
to produce changes that build on a broken foundation. The streaming apply
mechanism is implemented via a BlockHandler callback on ParserState; see
TheoryOfStreamingApply in generate.go for details.
Immediate apply is enabled by default; the -no-apply flag disables it so change
blocks are not applied to the working tree during generation.
`

// Apply controls whether change blocks are applied to the working tree
// immediately as they are parsed from model output during generation.
// When true, ParserState is activated to intercept change blocks, and each
// complete change block is applied via changes.ApplyChangeBlock after a generation
// phase. An apply error aborts generation. See TheoryOfImmediateApply.
type Apply bool

func (Module) Apply() Apply {
	return true
}

var _ configs.Config = Apply(true)

var _ Flag = Apply(true)

func (a Apply) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	switch key {
	case "-apply":
		ret := Apply(true)
		return &ret, args, nil
	case "-no-apply":
		ret := Apply(false)
		return &ret, args, nil
	}
	panic("key not handle: " + key)
}

func (a Apply) Keys() map[string]string {
	return map[string]string{
		"-apply":    "Apply change blocks to the working tree during generation",
		"-no-apply": "Do not apply change blocks during generation",
	}
}

func (a Apply) ConfigPaths() []string {
	return []string{"apply"}
}

func (a Apply) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	ret := Apply(b)
	return &ret, nil
}
