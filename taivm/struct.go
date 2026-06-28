package taivm

import "maps"

type Struct struct {
	TypeName string
	Fields   map[string]any
	Embedded []string
}

func (s *Struct) Copy() *Struct {
	newFields := make(map[string]any, len(s.Fields))
	maps.Copy(newFields, s.Fields)
	return &Struct{
		TypeName: s.TypeName,
		Fields:   newFields,
		Embedded: s.Embedded,
	}
}
