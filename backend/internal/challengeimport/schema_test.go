package challengeimport

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Keep the offline structural schema aligned with typed strict decoding.
// Module invariants, identities and references remain compiler validations.
func portableSchema() map[string]any {
	defs := map[string]any{}
	var schema func(reflect.Type) map[string]any
	schema = func(t reflect.Type) map[string]any {
		if t == reflect.TypeOf(json.RawMessage{}) {
			return map[string]any{}
		}
		switch t.Kind() {
		case reflect.Pointer:
			return map[string]any{"anyOf": []any{schema(t.Elem()), map[string]any{"type": "null"}}}
		case reflect.Interface:
			return map[string]any{}
		case reflect.String:
			return map[string]any{"type": "string"}
		case reflect.Bool:
			return map[string]any{"type": "boolean"}
		case reflect.Int, reflect.Int32, reflect.Int64:
			return map[string]any{"type": "integer", "minimum": -9007199254740991, "maximum": 9007199254740991}
		case reflect.Slice:
			return map[string]any{"type": []string{"array", "null"}, "items": schema(t.Elem())}
		case reflect.Map:
			return map[string]any{"type": []string{"object", "null"}, "additionalProperties": schema(t.Elem())}
		case reflect.Struct:
			name := t.Name()
			if _, ok := defs[name]; !ok {
				defs[name] = nil
				properties := map[string]any{}
				required := []string{}
				for index := 0; index < t.NumField(); index++ {
					field := t.Field(index)
					tag := strings.Split(field.Tag.Get("json"), ",")
					if tag[0] == "-" {
						continue
					}
					key := tag[0]
					if key == "" {
						key = field.Name
					}
					properties[key] = schema(field.Type)
					if len(tag) == 1 {
						required = append(required, key)
					}
				}
				defs[name] = map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}
			}
			return map[string]any{"$ref": "#/$defs/" + name}
		default:
			panic(t.String())
		}
	}
	root := schema(reflect.TypeOf(PortablePackage{}))
	properties := defs["PortablePackage"].(map[string]any)["properties"].(map[string]any)
	properties["format"] = map[string]any{"const": PackageFormat}
	properties["format_version"] = map[string]any{"const": 1}
	properties["runtime_contract_version"] = map[string]any{"const": 2}
	properties["content_hash"] = map[string]any{"type": "string", "pattern": "^[a-f0-9]{64}$"}
	manifest := defs["Manifest"].(map[string]any)["properties"].(map[string]any)
	manifest["version"] = map[string]any{"type": "string", "pattern": "^(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$"}
	manifest["outcomes"] = map[string]any{"type": []string{"array", "null"}, "maxItems": 0}
	manifest["dependencies"] = map[string]any{"type": []string{"array", "null"}, "maxItems": 0}
	root["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	root["$id"] = "fatecraft.gameplay-package.schema.json"
	root["title"] = "Fatecraft portable gameplay package v1"
	root["description"] = "Structural schema. The server additionally validates exact trusted modules, permissions, references, budgets and normalized content hashes."
	root["$defs"] = defs
	return root
}

func TestPortableSchemaAndExample(t *testing.T) {
	schemaPath := filepath.Join("..", "..", "data", "schemas", "fatecraft.gameplay-package.schema.json")
	expected, err := json.MarshalIndent(portableSchema(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	examplePath := filepath.Join("..", "..", "..", "docs", "examples", "underground-watchtower.fatecraft.json")
	if os.Getenv("FATECRAFT_UPDATE_PACKAGE_EXAMPLE") == "true" {
		if err := os.WriteFile(schemaPath, append(expected, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
		loaded, err := loadDirectory(filepath.Join("..", "..", "data", "preset_packs", "underground-watchtower"), time.Now())
		if err != nil {
			t.Fatal(err)
		}
		portable, err := portableFromDocument(&loaded.document)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.MarshalIndent(portable, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(examplePath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(examplePath, append(raw, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
	actual, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(actual), expected) {
		t.Fatal("portable schema differs from protocol; regenerate with FATECRAFT_UPDATE_PACKAGE_EXAMPLE=true")
	}
	raw, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := decodePortable(raw, defaultRegistry()); err != nil {
		t.Fatalf("published authoring example is invalid: %v", err)
	}
}
