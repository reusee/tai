package generators

var FuncNow = &Func{
	Decl: FuncDecl{
		Name:        "now",
		Description: "get current time",
		Params: Vars{
			{
				Name:        "timezone",
				Type:        TypeString,
				Description: "timezone",
			},
		},
	},
}
