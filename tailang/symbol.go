package tailang

type Symbol int

type SymbolTable struct {
	strToSym map[string]Symbol
	symToStr map[Symbol]string
}

func NewSymbolTable() *SymbolTable {
	return &SymbolTable{
		strToSym: make(map[string]Symbol),
		symToStr: make(map[Symbol]string),
	}
}

func (t *SymbolTable) Intern(name string) Symbol {
	if sym, ok := t.strToSym[name]; ok {
		return sym
	}
	sym := Symbol(len(t.strToSym))
	t.strToSym[name] = sym
	t.symToStr[sym] = name
	return sym
}

func (t *SymbolTable) Snapshot() []string {
	res := make([]string, len(t.strToSym))
	for sym, str := range t.symToStr {
		res[int(sym)] = str
	}
	return res
}

func (t *SymbolTable) Restore(syms []string) {
	t.strToSym = make(map[string]Symbol, len(syms))
	t.symToStr = make(map[Symbol]string, len(syms))
	for i, s := range syms {
		sym := Symbol(i)
		t.strToSym[s] = sym
		t.symToStr[sym] = s
	}
}
