package taivm

type Struct struct {
	TypeName string
	Fields   map[string]any
	Embedded []string
}
