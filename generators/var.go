package generators

import (
	"fmt"
	"sort"

	"google.golang.org/genai"
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

func (v Vars) ToGemini() *genai.Schema {
	props := make(map[string]*genai.Schema)
	var required []string
	for _, variable := range v {
		props[variable.Name] = variable.ToGemini()
		if !variable.Optional {
			required = append(required, variable.Name)
		}
	}
	// Sort required fields alphabetically to ensure deterministic schema
	// serialization. Without sorting, adding a new required field could
	// reorder existing fields in the JSON output, invalidating the prefix
	// cache for the entire schema portion of the request.
	sort.Strings(required)
	return &genai.Schema{
		Type:        genai.TypeObject,
		Properties:  props,
		Required:    required,
		Description: "Parameters for the function call.",
	}
}

func (v Var) ToGemini() *genai.Schema {
	ret := &genai.Schema{
		Nullable:    &v.Optional,
		Description: v.Description,
	}
	switch v.Type {
	case TypeString:
		ret.Type = genai.TypeString
	case TypeNumber:
		ret.Type = genai.TypeNumber
	case TypeInteger:
		ret.Type = genai.TypeInteger
	case TypeBoolean:
		ret.Type = genai.TypeBoolean
	case TypeArray:
		ret.Type = genai.TypeArray
		ret.Items = v.ItemType.ToGemini()
	case TypeObject:
		ret.Type = genai.TypeObject
		objSchema := v.Properties.ToGemini()
		ret.Properties = objSchema.Properties
		ret.Required = objSchema.Required
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
	// Sort required fields alphabetically for deterministic schema serialization,
	// preserving the prefix cache when new required fields are added.
	sort.Strings(required)
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