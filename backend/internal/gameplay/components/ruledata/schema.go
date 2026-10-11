package ruledata

import (
	"reflect"
	"strconv"
	"strings"
)

// Schema publishes a bounded structural editor for a compiled configuration
// type. Module validation remains the authority for numeric and cross-field
// rules; the schema contains no executable expressions.
func Schema[T any](labels map[string]string) map[string]any {
	var value T
	var build func(reflect.Type, int) map[string]any
	build = func(t reflect.Type, depth int) map[string]any {
		if depth > 8 {
			panic("rule schema nesting too deep")
		}
		switch t.Kind() {
		case reflect.Struct:
			properties := map[string]any{}
			required := []string{}
			for i := 0; i < t.NumField(); i++ {
				f := t.Field(i)
				key := strings.Split(f.Tag.Get("json"), ",")[0]
				if key == "" || key == "-" {
					continue
				}
				s := build(f.Type, depth+1)
				for _, bound := range []string{"minimum", "maximum"} {
					if raw := f.Tag.Get(bound); raw != "" {
						n, err := strconv.ParseInt(raw, 10, 64)
						if err != nil {
							panic("invalid compiled schema bound: " + raw)
						}
						leaf := s
						for leaf["type"] == "object" || leaf["type"] == "array" {
							key := "additionalProperties"
							if leaf["type"] == "array" {
								key = "items"
							}
							var ok bool
							leaf, ok = leaf[key].(map[string]any)
							if !ok {
								panic("schema bounds require numeric leaves")
							}
						}
						if leaf["type"] != "integer" {
							panic("schema bounds require numeric leaves")
						}
						leaf[bound] = n
					}
				}
				label := labels[key]
				if label == "" {
					label = key
				}
				s["title"] = label
				properties[key] = s
				required = append(required, key)
			}
			return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
		case reflect.Map:
			return map[string]any{"type": "object", "additionalProperties": build(t.Elem(), depth+1), "maxProperties": 64}
		case reflect.Slice:
			return map[string]any{"type": "array", "items": build(t.Elem(), depth+1), "maxItems": 64}
		case reflect.String:
			return map[string]any{"type": "string", "maxLength": 256}
		case reflect.Int, reflect.Int64:
			return map[string]any{"type": "integer", "minimum": 0, "maximum": 1000000}
		case reflect.Bool:
			return map[string]any{"type": "boolean"}
		default:
			panic("unsupported compiled configuration type: " + t.String())
		}
	}
	return build(reflect.TypeOf(value), 0)
}
