package generators

import (
	"encoding/json"
	"testing"
)

func TestTypeMarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    Type
		expected string
	}{
		{
			name:     "None type",
			input:    TypeNone,
			expected: `"none"`,
		},
		{
			name:     "String type",
			input:    TypeString,
			expected: `"string"`,
		},
		{
			name:     "Number type",
			input:    TypeNumber,
			expected: `"number"`,
		},
		{
			name:     "Integer type",
			input:    TypeInteger,
			expected: `"int"`,
		},
		{
			name:     "Boolean type",
			input:    TypeBoolean,
			expected: `"bool"`,
		},
		{
			name:     "Array type",
			input:    TypeArray,
			expected: `"array"`,
		},
		{
			name:     "Object type",
			input:    TypeObject,
			expected: `"object"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.input)
			if err != nil {
				t.Fatalf("MarshalJSON() error = %v", err)
			}
			got := string(data)
			if got != tt.expected {
				t.Errorf("MarshalJSON() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestTypeUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Type
		wantErr  bool
	}{
		// Valid type mappings
		{
			name:     "None type",
			input:    `"none"`,
			expected: TypeNone,
			wantErr:  false,
		},
		{
			name:     "None type alias nil",
			input:    `"nil"`,
			expected: TypeNone,
			wantErr:  false,
		},
		{
			name:     "String type",
			input:    `"string"`,
			expected: TypeString,
			wantErr:  false,
		},
		{
			name:     "String type alias str",
			input:    `"str"`,
			expected: TypeString,
			wantErr:  false,
		},
		{
			name:     "Number type",
			input:    `"number"`,
			expected: TypeNumber,
			wantErr:  false,
		},
		{
			name:     "Number type alias num",
			input:    `"num"`,
			expected: TypeNumber,
			wantErr:  false,
		},
		{
			name:     "Integer type",
			input:    `"int"`,
			expected: TypeInteger,
			wantErr:  false,
		},
		{
			name:     "Integer type alias integer",
			input:    `"integer"`,
			expected: TypeInteger,
			wantErr:  false,
		},
		{
			name:     "Boolean type",
			input:    `"bool"`,
			expected: TypeBoolean,
			wantErr:  false,
		},
		{
			name:     "Boolean type alias boolean",
			input:    `"boolean"`,
			expected: TypeBoolean,
			wantErr:  false,
		},
		{
			name:     "Array type",
			input:    `"array"`,
			expected: TypeArray,
			wantErr:  false,
		},
		{
			name:     "Array type alias list",
			input:    `"list"`,
			expected: TypeArray,
			wantErr:  false,
		},
		{
			name:     "Object type",
			input:    `"object"`,
			expected: TypeObject,
			wantErr:  false,
		},
		{
			name:     "Object type alias struct",
			input:    `"struct"`,
			expected: TypeObject,
			wantErr:  false,
		},
		// Error cases
		{
			name:     "Invalid type",
			input:    `"invalid_type"`,
			expected: TypeNone,
			wantErr:  true,
		},
		{
			name:     "Empty string",
			input:    `""`,
			expected: TypeNone,
			wantErr:  true,
		},
		{
			name:     "Malformed JSON",
			input:    `"invalid`,
			expected: TypeNone,
			wantErr:  true,
		},
		{
			name:     "Number type",
			input:    `"number"`,
			expected: TypeNumber,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Type
			err := json.Unmarshal([]byte(tt.input), &got)

			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && got != tt.expected {
				t.Errorf("UnmarshalJSON() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestTypeMarshalUnmarshalRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		input   Type
		wantErr bool
	}{
		{"none", TypeNone, false},
		{"string", TypeString, false},
		{"number", TypeNumber, false},
		{"int", TypeInteger, false},
		{"bool", TypeBoolean, false},
		{"array", TypeArray, false},
		{"object", TypeObject, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal to JSON
			data, err := json.Marshal(tt.input)
			if err != nil {
				t.Fatalf("MarshalJSON() error = %v", err)
			}

			// Unmarshal back to Type
			var got Type
			err = json.Unmarshal(data, &got)
			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && got != tt.input {
				t.Errorf("Round trip failed: got %v, want %v", got, tt.input)
			}
		})
	}
}

func TestTypeUnmarshalJSONCaseSensitivity(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Type
		wantErr  bool
	}{
		{
			name:     "Uppercase string",
			input:    `"STRING"`,
			expected: TypeNone,
			wantErr:  true,
		},
		{
			name:     "Mixed case string",
			input:    `"StRiNg"`,
			expected: TypeNone,
			wantErr:  true,
		},
		{
			name:     "Lowercase string",
			input:    `"string"`,
			expected: TypeString,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Type
			err := json.Unmarshal([]byte(tt.input), &got)

			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && got != tt.expected {
				t.Errorf("UnmarshalJSON() = %v, want %v", got, tt.expected)
			}
		})
	}
}
