package tailang

type Env struct {
	Globals map[string]Value
}

func NewEnv() *Env {
	return &Env{
		Globals: map[string]Value{
			"printf": Printf{},
			"now":    Now{},
			"[":      List{},
			"join":   Join{},
			"def":    Def{},
		},
	}
}
