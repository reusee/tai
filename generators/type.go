package generators

import (
	"encoding/json"
	"fmt"
)

type Type uint8

const (
	TypeNone Type = iota
	TypeString
	TypeNumber
	TypeInteger
	TypeBoolean
	TypeArray
	TypeObject
)

var _ json.Marshaler = Type(0)

func (t Type) MarshalJSON() ([]byte, error) {
	switch t {
	case TypeNone:
		return []byte(`"none"`), nil
	case TypeString:
		return []byte(`"string"`), nil
	case TypeNumber:
		return []byte(`"number"`), nil
	case TypeInteger:
		return []byte(`"int"`), nil
	case TypeBoolean:
		return []byte(`"bool"`), nil
	case TypeArray:
		return []byte(`"array"`), nil
	case TypeObject:
		return []byte(`"object"`), nil
	}
	return nil, fmt.Errorf("invalid type: %v", t)
}

var _ json.Unmarshaler = new(Type)

func (t *Type) UnmarshalJSON(data []byte) error {
	switch s := string(data); s {
	case `"none"`, `"nil"`:
		*t = TypeNone
	case `"string"`, `"str"`:
		*t = TypeString
	case `"number"`, `"num"`:
		*t = TypeNumber
	case `"int"`, `"integer"`:
		*t = TypeInteger
	case `"bool"`, `"boolean"`:
		*t = TypeBoolean
	case `"array"`, `"list"`:
		*t = TypeArray
	case `"object"`, `"struct"`:
		*t = TypeObject
	default:
		return fmt.Errorf("invalid type: %s", data)
	}
	return nil
}
