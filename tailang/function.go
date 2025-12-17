package tailang

import "sync"

type Function struct {
	Name       string
	NumParams  int
	ParamNames []string
	Code       []OpCode
	Constants  []any

	ParamSymbols []Symbol
	symbolsOnce  sync.Once
}

func (f *Function) EnsureParamSymbols(st *SymbolTable) {
	f.symbolsOnce.Do(func() {
		f.ParamSymbols = make([]Symbol, len(f.ParamNames))
		for i, name := range f.ParamNames {
			f.ParamSymbols[i] = st.Intern(name)
		}
	})
}
