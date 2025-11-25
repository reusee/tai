package tailang

type Def struct{}

var _ Function = Def{}

func (d Def) Name() string {
	return "def"
}

func (d Def) Call(env *Env, name string, value any) any {
	env.Globals[name] = value
	return value
}
