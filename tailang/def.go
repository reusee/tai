package tailang

type Def struct{}

var _ Function = Def{}

func (d Def) FunctionName() string {
	return "def"
}

func (d Def) Call(env *Env, name string, value any) any {
	env.Define(name, value)
	return value
}
