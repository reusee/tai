package tailang

import (
	"fmt"
	"reflect"
)

type Switch struct{}

type DefaultCase struct{}

var _ Function = Switch{}

func (s Switch) FunctionName() string {
	return "switch"
}

func (s Switch) Call(env *Env, stream TokenStream, expectedType reflect.Type) (any, error) {
	val, err := env.evalExpr(stream, nil)
	if err != nil {
		return nil, err
	}

	bodyVal, err := env.evalExpr(stream, nil)
	if err != nil {
		return nil, err
	}
	bodyBlock, ok := bodyVal.(*Block)
	if !ok {
		return nil, fmt.Errorf("expected block for switch body, got %T", bodyVal)
	}

	clauses, err := parseSwitchClauses(env, NewSliceTokenStream(bodyBlock.Body))
	if err != nil {
		return nil, err
	}

	for _, clause := range clauses {
		if clause.defaultCase {
			continue
		}
		for _, caseVal := range clause.values {
			if Eq(val, caseVal) {
				return env.NewScope().Evaluate(NewSliceTokenStream(clause.block.Body))
			}
		}
	}

	for _, clause := range clauses {
		if clause.defaultCase {
			return env.NewScope().Evaluate(NewSliceTokenStream(clause.block.Body))
		}
	}
	return nil, nil
}

type switchClause struct {
	values      []any
	block       *Block
	defaultCase bool
}

func parseSwitchClauses(env *Env, bodyStream TokenStream) ([]*switchClause, error) {
	var clauses []*switchClause
	var curVals []any
	var hasDefault bool

	for {
		tok, err := bodyStream.Current()
		if err != nil {
			return nil, err
		}
		if tok.Kind == TokenEOF {
			break
		}

		if tok.Kind == TokenIdentifier && tok.Text == "default" {
			bodyStream.Consume()
			if len(curVals) > 0 {
				return nil, fmt.Errorf("default case cannot have values")
			}
			val, err := env.evalExpr(bodyStream, nil)
			if err != nil {
				return nil, err
			}
			block, ok := val.(*Block)
			if !ok {
				return nil, fmt.Errorf("expected block after default, got %T", val)
			}
			if hasDefault {
				return nil, fmt.Errorf("multiple default clauses")
			}
			clauses = append(clauses, &switchClause{defaultCase: true, block: block})
			hasDefault = true
			continue
		}

		val, err := env.evalExpr(bodyStream, nil)
		if err != nil {
			return nil, err
		}
		if block, ok := val.(*Block); ok {
			if len(curVals) == 0 {
				return nil, fmt.Errorf("block without preceding case values")
			}
			clauses = append(clauses, &switchClause{values: curVals, block: block})
			curVals = nil
		} else {
			curVals = append(curVals, val)
		}
	}

	if len(curVals) > 0 {
		return nil, fmt.Errorf("case values without a block")
	}
	return clauses, nil
}
