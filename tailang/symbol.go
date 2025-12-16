package tailang

import "sync"

type Symbol int

var (
	symbolsMu sync.RWMutex
	strToSym  = make(map[string]Symbol)
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
	return sym
}

var undefined = &struct{}{}
