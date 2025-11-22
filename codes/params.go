package codes

import "github.com/reusee/tai/generators"

var filePathParam = generators.Var{
	Name:        "file_path",
	Type:        generators.TypeString,
	Description: "file path",
}

var fileContentParam = generators.Var{
	Name:        "file_content",
	Type:        generators.TypeString,
	Description: "file content",
}

var editsParam = generators.Var{
	Name: "edits",
	Type: generators.TypeArray,
	ItemType: &generators.Var{
		Type:        generators.TypeObject,
		Description: "single edit operation",
		Properties: generators.Vars{
			{
				Name:        "old",
				Type:        generators.TypeString,
				Description: "old string to be replaced, must be unique in the file",
			},
			{
				Name:        "new",
				Type:        generators.TypeString,
				Description: "new string for replacing",
			},
		},
	},
	Description: "edit operations",
}
