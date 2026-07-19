package taivm

type Env struct {
	Parent   *Env
	Vars     []EnvVar
	Captured bool
}

type EnvVar struct {
	Name string
	Val  any
	Type *Type
}

func (e *Env) Get(name string) (any, bool) {
	for i := len(e.Vars) - 1; i >= 0; i-- {
		if e.Vars[i].Name == name {
			return e.Vars[i].Val, true
		}
	}
	if e.Parent != nil {
		return e.Parent.Get(name)
	}
	return nil, false
}

func (e *Env) Def(name string, val any) {
	e.DefWithType(name, val, nil)
}

func (e *Env) Set(name string, val any) bool {
	for i := len(e.Vars) - 1; i >= 0; i-- {
		if e.Vars[i].Name == name {
			e.Vars[i].Val = val
			return true
		}
	}
	if e.Parent != nil {
		return e.Parent.Set(name, val)
	}
	return false
}

func (e *Env) DefWithType(name string, val any, typ *Type) {
	for i, v := range e.Vars {
		if v.Name == name {
			e.Vars[i].Val = val
			e.Vars[i].Type = typ
			return
		}
	}
	e.Vars = append(e.Vars, EnvVar{
		Name: name,
		Val:  val,
		Type: typ,
	})
}

func (e *Env) SetWithType(name string, val any, typ *Type) bool {
	for i := len(e.Vars) - 1; i >= 0; i-- {
		if e.Vars[i].Name == name {
			e.Vars[i].Val = val
			e.Vars[i].Type = typ
			return true
		}
	}
	if e.Parent != nil {
		return e.Parent.SetWithType(name, val, typ)
	}
	return false
}

func (e *Env) GetVar(name string) (EnvVar, bool) {
	for i := len(e.Vars) - 1; i >= 0; i-- {
		if e.Vars[i].Name == name {
			return e.Vars[i], true
		}
	}
	if e.Parent != nil {
		return e.Parent.GetVar(name)
	}
	return EnvVar{}, false
}

func (e *Env) NewChild() *Env {
	return &Env{
		Parent: e,
	}
}

func (e *Env) MarkCaptured() {
	for curr := e; curr != nil && !curr.Captured; curr = curr.Parent {
		curr.Captured = true
	}
}
