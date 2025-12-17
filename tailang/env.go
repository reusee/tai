package tailang

type Env struct {
	Parent *Env
	Vars   []any
}

func (e *Env) GetSym(sym Symbol) (any, bool) {
	idx := int(sym)
	if idx < len(e.Vars) {
		val := e.Vars[idx]
		if val != nil {
			return val, true
		}
	}
	if e.Parent != nil {
		return e.Parent.GetSym(sym)
	}
	return nil, false
}

func (e *Env) DefSym(sym Symbol, val any) {
	idx := int(sym)
	e.Grow(idx)
	e.Vars[idx] = val
}

func (e *Env) SetSym(sym Symbol, val any) bool {
	idx := int(sym)
	if idx < len(e.Vars) && e.Vars[idx] != nil {
		e.Vars[idx] = val
		return true
	}
	if e.Parent != nil {
		return e.Parent.SetSym(sym, val)
	}
	return false
}

func (e *Env) NewChild() *Env {
	return &Env{
		Parent: e,
	}
}

func (e *Env) Grow(idx int) {
	if idx < len(e.Vars) {
		return
	}
	newCap := idx * 2
	if newCap < idx+1 {
		newCap = idx + 1
	}
	newVars := make([]any, newCap)
	copy(newVars, e.Vars)
	for i := len(e.Vars); i < newCap; i++ {
		newVars[i] = nil
	}
	e.Vars = newVars
}
