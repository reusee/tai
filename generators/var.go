package generators

import (
	"fmt"

	"cloud.google.com/go/ai/generativelanguage/apiv1beta/generativelanguagepb"
)

type Var struct {
	Name        string `json:"name"`
	Type        Type   `json:"type"`
	Optional    bool   `json:"optional"`
	Description string `json:"description"`
	ItemType    *Var   `json:"item_type"`  // for TypeArray
	Properties  Vars   `json:"properties"` // for TypeObject
}

type Vars []Var

func (v Vars) ToGemini() *generativelanguagepb.Schema {
	props := make(map[string]*generativelanguagepb.Schema)
	var required []string
	for _, variable := range v {
		props[variable.Name] = variable.ToGemini()
		if !variable.Optional {
			required = append(required, variable.Name)
		}
	}
	return &generativelanguagepb.Schema{
		Type:        generativelanguagepb.Type_OBJECT,
		Properties:  props,
		Required:    required,
		Description: "Parameters for the function call.",
	}
}

func (v Var) ToGemini() *generativelanguagepb.Schema {
	ret := &generativelanguagepb.Schema{
		Title:       v.Name,
		Nullable:    v.Optional,
		Description: v.Description,
	}
	switch v.Type {
	case TypeString:
		ret.Type = generativelanguagepb.Type_STRING
	case TypeNumber:
		ret.Type = generativelanguagepb.Type_NUMBER
	case TypeInteger:
		ret.Type = generativelanguagepb.Type_INTEGER
	case TypeBoolean:
		ret.Type = generativelanguagepb.Type_BOOLEAN
	case TypeArray:
		ret.Type = generativelanguagepb.Type_ARRAY
		ret.Items = v.ItemType.ToGemini()
	case TypeObject:
		ret.Type = generativelanguagepb.Type_OBJECT
		props := make(map[string]*generativelanguagepb.Schema)
		for _, v := range v.Properties {
			props[v.Name] = v.ToGemini()
		}
		ret.Properties = props
	default:
		panic(fmt.Errorf("unknown type: %v", v.Type))
	}
	return ret
}

func (v Vars) ToOpenAI() map[string]any {
	props := make(map[string]any)
	var required []string
	for _, variable := range v {
		props[variable.Name] = variable.ToOpenAI()
		if !variable.Optional {
			required = append(required, variable.Name)
		}
	}
	return map[string]any{
		"type":        "object",
		"properties":  props,
		"required":    required,
		"description": "Parameters for the function call.",
	}
}

func (v Var) ToOpenAI() map[string]any {
	ret := map[string]any{
		"description": v.Description,
	}
	switch v.Type {
	case TypeString:
		ret["type"] = "string"
	case TypeNumber:
		ret["type"] = "number"
	case TypeInteger:
		ret["type"] = "integer"
	case TypeBoolean:
		ret["type"] = "boolean"
	case TypeArray:
		ret["type"] = "array"
		ret["items"] = v.ItemType.ToOpenAI()
	case TypeObject:
		ret["type"] = "object"
		objSchema := v.Properties.ToOpenAI()
		if props, ok := objSchema["properties"]; ok {
			ret["properties"] = props
		}
		if required, ok := objSchema["required"]; ok {
			ret["required"] = required
		}
	default:
		panic(fmt.Errorf("unknown type: %v", v.Type))
	}
	return ret
}
