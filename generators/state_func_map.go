package generators

import (
	"iter"
	"sort"
)

type FuncMap struct {
	upstream State
	m        map[string]*Function
}

func NewFuncMap(upstream State, funcs ...*Function) FuncMap {
	ret := FuncMap{
		upstream: upstream,
		m:        make(map[string]*Function),
	}
	for _, fn := range funcs {
		if fn == nil {
			continue
		}
		ret.m[fn.Decl.Name] = fn
	}
	return ret
}

var _ State = FuncMap{}

func (f FuncMap) AppendContent(content *Content) (State, error) {
	ret := f // copy
	var err error
	ret.upstream, err = f.upstream.AppendContent(content)
	if err != nil {
		return ret, err
	}

	// call
	var results []Part
	for _, part := range content.Parts {
		call, ok := part.(FuncCall)
		if !ok {
			continue
		}
		fn, ok := f.m[call.Name]
		if !ok {
			continue
		}
		if fn.Func == nil {
			continue
		}

		res, err := fn.Func(call.Arguments)
		if err != nil {
			return ret, err
		}

		results = append(results, CallResult{
			ID:      call.ID,
			Name:    call.Name,
			Results: res,
		})
	}

	if len(results) > 0 {
		ret.upstream, err = ret.upstream.AppendContent(&Content{
			Role:  RoleTool,
			Parts: results,
		})
		if err != nil {
			return ret, err
		}
	}

	return ret, nil
}

func (f FuncMap) Contents() iter.Seq[*Content] {
	return f.upstream.Contents()
}

// TheoryOfPrefixCaching: Function declarations embedded in the prompt must
// appear in a deterministic order across runs. Go map iteration is
// non-deterministic, so we sort keys by name before yielding to ensure
// the LLM prefix cache remains valid across repeated requests.
func (f FuncMap) Functions() iter.Seq[*Function] {
	return func(yield func(*Function) bool) {
		names := make([]string, 0, len(f.m))
		for name := range f.m {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if !yield(f.m[name]) {
				return
			}
		}
	}
}

func (f FuncMap) SystemPrompt() string {
	return f.upstream.SystemPrompt()
}

func (f FuncMap) Flush() (State, error) {
	ret := f // copy
	var err error
	ret.upstream, err = f.upstream.Flush()
	if err != nil {
		return ret, err
	}
	return ret, nil
}

func (f FuncMap) Unwrap() State {
	return f.upstream
}

type stateWithFunctions struct {
	upstream State
	fns      []*Function
}

func WithFunctions(upstream State, fns ...*Function) State {
	return stateWithFunctions{
		upstream: upstream,
		fns:      fns,
	}
}

func (w stateWithFunctions) Unwrap() State {
	return w.upstream
}

func (w stateWithFunctions) Flush() (State, error) {
	ret := w
	var err error
	ret.upstream, err = w.upstream.Flush()
	if err != nil {
		return ret, err
	}
	return ret, nil
}

func (w stateWithFunctions) SystemPrompt() string {
	return w.upstream.SystemPrompt()
}

func (w stateWithFunctions) Functions() iter.Seq[*Function] {
	return func(yield func(*Function) bool) {
		for _, fn := range w.fns {
			if fn == nil {
				continue
			}
			if !yield(fn) {
				return
			}
		}
		// Delegate to upstream. Note: upstream may be FuncMap, which
		// now yields in sorted (deterministic) order.
		for fn := range w.upstream.Functions() {
			if !yield(fn) {
				return
			}
		}
	}
}

func (w stateWithFunctions) Contents() iter.Seq[*Content] {
	return w.upstream.Contents()
}

func (w stateWithFunctions) AppendContent(content *Content) (State, error) {
	ret := w
	var err error
	ret.upstream, err = w.upstream.AppendContent(content)
	if err != nil {
		return ret, err
	}
	return ret, nil
}