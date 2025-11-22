package generators

type Func struct {
	Decl FuncDecl
	Func func(args map[string]any) (map[string]any, error)
}
