package tailang

import "sync"

type Symbol int

var (
	symbolsMu sync.RWMutex
	strToSym  = make(map[string]Symbol)
	symToStr  map[Symbol]string
)

func Intern(name string) Symbol {
	symbolsMu.RLock()
	sym, ok := strToSym[name]
	symbolsMu.RUnlock()
	if ok {
		return sym
	}

	symbolsMu.Lock()
	defer symbolsMu.Unlock()

	if sym, ok = strToSym[name]; ok {
		return sym
	}

	sym = Symbol(len(strToSym))
	strToSym[name] = sym

	if symToStr == nil {
		symToStr = make(map[Symbol]string)
	}
	symToStr[sym] = name

	return sym
}

var undefined = &struct{}{}

func SnapshotSymbols() []string {
	symbolsMu.RLock()
	defer symbolsMu.RUnlock()
	res := make([]string, len(strToSym))
	for sym, str := range symToStr {
		res[int(sym)] = str
	}
	return res
}
