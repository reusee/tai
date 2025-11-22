package generators

type FuncMap struct {
	upstream State
	m        map[string]*Func
}

func NewFuncMap(upstream State, funcs ...*Func) FuncMap {
	ret := FuncMap{
		upstream: upstream,
		m:        make(map[string]*Func),
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

		res, err := fn.Func(call.Args)
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
		ret.upstream, err = f.upstream.AppendContent(&Content{
			Role:  RoleTool,
			Parts: results,
		})
		if err != nil {
			return ret, err
		}
	}

	return ret, nil
}

func (f FuncMap) Contents() []*Content {
	return f.upstream.Contents()
}

func (f FuncMap) FuncMap() map[string]*Func {
	return f.m
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
