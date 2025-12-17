package tailang

type Symbol int

type SymbolTable struct {
	StrToSym map[string]Symbol
	SymToStr map[Symbol]string
}

func NewSymbolTable() *SymbolTable {
	return &SymbolTable{
		StrToSym: make(map[string]Symbol),
		SymToStr: make(map[Symbol]string),
	}
}

func (t *SymbolTable) Intern(name string) Symbol {
	if sym, ok := t.StrToSym[name]; ok {
		return sym
	}
	sym := Symbol(len(t.StrToSym))
	t.StrToSym[name] = sym
	t.SymToStr[sym] = name
	return sym
}

func (t *SymbolTable) Snapshot() []string {
	res := make([]string, len(t.StrToSym))
	for sym, str := range t.SymToStr {
		res[int(sym)] = str
	}
	return res
}

func (t *SymbolTable) Restore(syms []string) {
	t.StrToSym = make(map[string]Symbol, len(syms))
	t.SymToStr = make(map[Symbol]string, len(syms))
	for i, s := range syms {
		sym := Symbol(i)
		t.StrToSym[s] = sym
		t.SymToStr[sym] = s
	}
}
