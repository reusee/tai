package taivm

type Struct struct {
	TypeName string
	Fields   map[string]any
	Embedded []string
}

func (s *Struct) Copy() *Struct {
	newFields := make(map[string]any, len(s.Fields))
	for k, v := range s.Fields {
		newFields[k] = v
	}
	return &Struct{
		TypeName: s.TypeName,
		Fields:   newFields,
		Embedded: s.Embedded,
	}
}
