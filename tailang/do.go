package tailang

import "fmt"

type Do struct{}

var _ Function = Do{}

func (d Do) FunctionName() string {
	return "do"
}

func (d Do) Call(env *Env, stream TokenStream) (any, error) {
	blockVal, err := env.evalExpr(stream, nil)
	if err != nil {
		return nil, err
	}
	block, ok := blockVal.(*Block)
	if !ok {
		return nil, fmt.Errorf("expected block for do, got %T", blockVal)
	}

	return env.NewScope().Evaluate(NewSliceTokenStream(block.Body))
}
