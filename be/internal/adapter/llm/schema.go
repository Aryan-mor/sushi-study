package llm

import "strings"

// vertexSchema rewrites a JSON Schema (OpenRouter / classify shape) into Vertex responseSchema.
// additionalProperties is dropped; type unions like ["integer","null"] become INTEGER.
func vertexSchema(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	return vertexMap(in)
}

func vertexMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		if k == "additionalProperties" {
			continue
		}
		if k == "type" {
			out[k] = vertexType(v)
			continue
		}
		out[k] = vertexAny(v)
	}
	return out
}

func vertexAny(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return vertexMap(t)
	case []any:
		items := make([]any, len(t))
		for i, x := range t {
			items[i] = vertexAny(x)
		}
		return items
	default:
		return v
	}
}

func vertexType(v any) string {
	switch t := v.(type) {
	case string:
		return vertexTypeName(t)
	case []any:
		for _, x := range t {
			s, ok := x.(string)
			if ok && !strings.EqualFold(s, "null") {
				return vertexTypeName(s)
			}
		}
	}
	return "STRING"
}

func vertexTypeName(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "object":
		return "OBJECT"
	case "array":
		return "ARRAY"
	case "integer":
		return "INTEGER"
	case "number":
		return "NUMBER"
	case "boolean":
		return "BOOLEAN"
	default:
		return "STRING"
	}
}
