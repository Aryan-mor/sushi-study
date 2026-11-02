package llm

import (
	"reflect"
	"testing"
)

func TestVertexSchemaDropsAdditionalProperties(t *testing.T) {
	t.Parallel()
	in := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"year": map[string]any{"type": []any{"integer", "null"}},
			"name": map[string]any{"type": "string"},
		},
		"required": []any{"name"},
	}
	got := vertexSchema(in)
	if _, ok := got["additionalProperties"]; ok {
		t.Fatalf("additionalProperties still present")
	}
	if got["type"] != "OBJECT" {
		t.Fatalf("type = %v, want OBJECT", got["type"])
	}
	props := got["properties"].(map[string]any)
	year := props["year"].(map[string]any)
	if year["type"] != "INTEGER" {
		t.Fatalf("year type = %v, want INTEGER", year["type"])
	}
	if !reflect.DeepEqual(got["required"], []any{"name"}) {
		t.Fatalf("required = %v", got["required"])
	}
}

func TestVertexSchemaNil(t *testing.T) {
	t.Parallel()
	if vertexSchema(nil) != nil {
		t.Fatalf("nil schema should stay nil")
	}
}
