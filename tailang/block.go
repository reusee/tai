package tailang

type Block struct {
	Body []*Token
}

type BlockDef struct{}

var _ Function = BlockDef{}

func (b BlockDef) FunctionName() string {
	return "{"
}

func (b BlockDef) Call(env *Env, stream TokenStream) (any, error) {
	return ParseBlockBody(stream)
}
