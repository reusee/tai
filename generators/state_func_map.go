package generators

import (
	"iter"
	"sort"
)

const TheoryOfPrefixCaching = `
Function declarations and schema fields embedded in the prompt must appear in
a deterministic order across runs to preserve the LLM prefix cache. Go map
iteration is non-deterministic, so all function collections are sorted by name
before yielding. When multiple state layers contribute functions (e.g., FuncMap
wrapping another FuncMap), all layers are merged and globally sorted by name,
with outer layers taking precedence over inner layers for duplicate names.
Schema "required" fields are sorted alphabetically so that adding a new required
field inserts it at its natural position without reordering existing fields,
minimizing token-level changes to the serialized schema.
`

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

func (f FuncMap) Functions() iter.Seq[*Function] {
	return func(yield func(*Function) bool) {
		// Collect functions from this layer and upstream, then globally sort.
		// This layer's functions take precedence over upstream functions
		// with the same name, following the layering semantics where outer
		// layers override inner layers. Global sorting across all layers
		// ensures that adding a function to any layer inserts it at its
		// natural alphabetical position without reordering existing entries,
		// minimizing token-level changes that would invalidate the prefix cache.
		all := make(map[string]*Function)
		for fn := range f.upstream.Functions() {
			if fn != nil {
				all[fn.Decl.Name] = fn
			}
		}
		for name, fn := range f.m {
			all[name] = fn
		}
		names := make([]string, 0, len(all))
		for name := range all {
			names = append(names, name)
		}
		sort.SliceStable(names, func(i, j int) bool {
			return names[i] < names[j]
		})
		for _, name := range names {
			if !yield(all[name]) {
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
		// Collect functions from this layer and upstream, then globally sort.
		// This layer's functions take precedence over upstream functions
		// with the same name, following the layering semantics where outer
		// layers override inner layers. Global sorting ensures deterministic
		// ordering for prefix caching.
		all := make(map[string]*Function)
		for fn := range w.upstream.Functions() {
			if fn != nil {
				all[fn.Decl.Name] = fn
			}
		}
		for _, fn := range w.fns {
			if fn == nil {
				continue
			}
			all[fn.Decl.Name] = fn
		}
		names := make([]string, 0, len(all))
		for name := range all {
			names = append(names, name)
		}
		sort.SliceStable(names, func(i, j int) bool {
			return names[i] < names[j]
		})
		for _, name := range names {
			if !yield(all[name]) {
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