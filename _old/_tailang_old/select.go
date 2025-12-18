package tailang

import (
	"fmt"
	"reflect"
)

type Select struct{}

var _ Function = Select{}

func (s Select) FunctionName() string {
	return "select"
}

func (s Select) Call(env *Env, stream TokenStream, expectedType reflect.Type) (any, error) {
	bodyVal, err := env.evalExpr(stream, nil)
	if err != nil {
		return nil, err
	}
	block, ok := bodyVal.(*Block)
	if !ok {
		return nil, fmt.Errorf("expected block for select body, got %T", bodyVal)
	}

	bodyStream := NewSliceTokenStream(block.Body)

	var cases []reflect.SelectCase
	var callbacks []func(reflect.Value, bool) (any, error)

	for {
		tok, err := bodyStream.Current()
		if err != nil || tok.Kind == TokenEOF {
			break
		}

		if tok.Kind == TokenIdentifier && tok.Text == "default" {
			bodyStream.Consume()

			// Block
			blockVal, err := env.evalExpr(bodyStream, nil)
			if err != nil {
				return nil, err
			}
			block, ok := blockVal.(*Block)
			if !ok {
				return nil, fmt.Errorf("expected block for default case")
			}

			cases = append(cases, reflect.SelectCase{
				Dir: reflect.SelectDefault,
			})
			callbacks = append(callbacks, func(v reflect.Value, ok bool) (any, error) {
				return env.NewScope().Evaluate(NewSliceTokenStream(block.Body))
			})
			continue
		}

		if tok.Kind == TokenIdentifier && tok.Text == "case" {
			bodyStream.Consume()

			dirTok, err := bodyStream.Current()
			if err != nil {
				return nil, err
			}
			bodyStream.Consume()

			if dirTok.Text == "recv" {
				// Channel
				chanVal, err := env.evalExpr(bodyStream, nil)
				if err != nil {
					return nil, err
				}

				// Var name
				nameTok, err := bodyStream.Current()
				if err != nil {
					return nil, err
				}
				if nameTok.Kind != TokenIdentifier && nameTok.Kind != TokenUnquotedString {
					return nil, fmt.Errorf("expected identifier for recv variable")
				}
				varName := nameTok.Text
				bodyStream.Consume()

				// Block
				blockVal, err := env.evalExpr(bodyStream, nil)
				if err != nil {
					return nil, err
				}
				block, ok := blockVal.(*Block)
				if !ok {
					return nil, fmt.Errorf("expected block for recv case")
				}

				cases = append(cases, reflect.SelectCase{
					Dir:  reflect.SelectRecv,
					Chan: reflect.ValueOf(chanVal),
				})
				callbacks = append(callbacks, func(v reflect.Value, ok bool) (any, error) {
					scope := env.NewScope()
					if ok {
						scope.Define(varName, v.Interface())
					} else {
						scope.Define(varName, nil)
					}
					return scope.Evaluate(NewSliceTokenStream(block.Body))
				})

			} else if dirTok.Text == "send" {
				// Channel
				chanVal, err := env.evalExpr(bodyStream, nil)
				if err != nil {
					return nil, err
				}

				// Value
				sendVal, err := env.evalExpr(bodyStream, nil)
				if err != nil {
					return nil, err
				}

				// Block
				blockVal, err := env.evalExpr(bodyStream, nil)
				if err != nil {
					return nil, err
				}
				block, ok := blockVal.(*Block)
				if !ok {
					return nil, fmt.Errorf("expected block for send case")
				}

				cases = append(cases, reflect.SelectCase{
					Dir:  reflect.SelectSend,
					Chan: reflect.ValueOf(chanVal),
					Send: reflect.ValueOf(sendVal),
				})
				callbacks = append(callbacks, func(v reflect.Value, ok bool) (any, error) {
					return env.NewScope().Evaluate(NewSliceTokenStream(block.Body))
				})

			} else {
				return nil, fmt.Errorf("expected recv or send after case, got %s", dirTok.Text)
			}
			continue
		}

		return nil, fmt.Errorf("unexpected token in select: %v", tok)
	}

	chosen, recv, recvOk := reflect.Select(cases)
	return callbacks[chosen](recv, recvOk)
}
