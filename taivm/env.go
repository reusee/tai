package taivm

type Env struct {
	Parent *Env
	Vars   []EnvVar
}

type EnvVar struct {
	Name string
	Val  any
}

func (e *Env) Get(name string) (any, bool) {
	for _, v := range e.Vars {
		if v.Name == name {
			return v.Val, true
		}
	}
	if e.Parent != nil {
		return e.Parent.Get(name)
	}
	return nil, false
}

func (e *Env) Def(name string, val any) {
	for i, v := range e.Vars {
		if v.Name == name {
			e.Vars[i].Val = val
			return
		}
	}
	e.Vars = append(e.Vars, EnvVar{
		Name: name,
		Val:  val,
	})
}

func (e *Env) Set(name string, val any) bool {
	for i, v := range e.Vars {
		if v.Name == name {
			e.Vars[i].Val = val
			return true
		}
	}
	if e.Parent != nil {
		return e.Parent.Set(name, val)
	}
	return false
}

func (e *Env) NewChild() *Env {
	return &Env{
		Parent: e,
	}
}
