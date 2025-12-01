package tailang

import "reflect"

type GoFunc struct {
	Name  string
	Func  any
	cache *goFuncCache
}

type goFuncCache struct {
	fnVal      reflect.Value
	fnType     reflect.Type
	numIn      int
	isVariadic bool
	inTypes    []reflect.Type
	errorIndex int
}

func (g *GoFunc) init() {
	if g.cache != nil {
		return
	}
	c := &goFuncCache{}
	c.fnVal = reflect.ValueOf(g.Func)
	c.fnType = c.fnVal.Type()
	c.numIn = c.fnType.NumIn()
	c.isVariadic = c.fnType.IsVariadic()
	c.inTypes = make([]reflect.Type, c.numIn)
	for i := 0; i < c.numIn; i++ {
		c.inTypes[i] = c.fnType.In(i)
	}

	c.errorIndex = -1
	numOut := c.fnType.NumOut()
	if numOut > 0 {
		lastOut := c.fnType.Out(numOut - 1)
		if lastOut.Implements(errorType) {
			c.errorIndex = numOut - 1
		}
	}

	g.cache = c
}

var _ Function = GoFunc{}

func (g GoFunc) FunctionName() string {
	return g.Name
}

func (g GoFunc) Call(env *Env, stream TokenStream, expectedType reflect.Type) (any, error) {
	c := g.cache
	if c == nil {
		// No cache (e.g. struct literal), compute temporary
		c = &goFuncCache{}
		c.fnVal = reflect.ValueOf(g.Func)
		c.fnType = c.fnVal.Type()
		c.numIn = c.fnType.NumIn()
		c.isVariadic = c.fnType.IsVariadic()
		c.inTypes = make([]reflect.Type, c.numIn)
		for i := 0; i < c.numIn; i++ {
			c.inTypes[i] = c.fnType.In(i)
		}
		c.errorIndex = -1
		numOut := c.fnType.NumOut()
		if numOut > 0 {
			lastOut := c.fnType.Out(numOut - 1)
			if lastOut.Implements(errorType) {
				c.errorIndex = numOut - 1
			}
		}
	}

	args := make([]reflect.Value, 0, c.numIn)

	for i := 0; i < c.numIn; i++ {
		argType := c.inTypes[i]

		val, err := env.evalExpr(stream, argType)
		if err != nil {
			return nil, err
		}

		vArg, err := PrepareAssign(val, argType)
		if err != nil {
			return nil, err
		}
		args = append(args, vArg)
	}

	var results []reflect.Value
	if c.isVariadic {
		results = c.fnVal.CallSlice(args)
	} else {
		results = c.fnVal.Call(args)
	}

	if len(results) == 0 {
		return nil, nil
	}

	if c.errorIndex >= 0 {
		last := results[c.errorIndex]
		if !last.IsNil() {
			return nil, last.Interface().(error)
		}
		if len(results) > 1 {
			return results[0].Interface(), nil
		}
		return nil, nil
	}
	return results[0].Interface(), nil
}
