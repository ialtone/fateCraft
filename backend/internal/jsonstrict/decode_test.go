package jsonstrict

import "testing"

func TestRejectAmbiguousContent(t *testing.T) {
	for _, raw := range []string{`{"name":1,"name":2}`, `{"nested":{"x":1,"x":2}}`, `{"n":9007199254740993}`, `{"n":1e999}`, `{} {}`, `{"n":0e1000000000}`, `{"n":1.0000000000000000001}`} {
		var target map[string]any
		if Decode([]byte(raw), &target) == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	var target struct {
		Name string `json:"name"`
	}
	if Decode([]byte(`{"name":"valid","unknown":1}`), &target) == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestPreserveRepresentableDecimals(t *testing.T) {
	for _, raw := range []string{`{"n":0.1}`, `{"n":1.50}`, `{"n":1e-7}`, `{"n":9007199254740991}`} {
		var target map[string]any
		if err := Decode([]byte(raw), &target); err != nil {
			t.Fatalf("valid numeric content rejected %s: %v", raw, err)
		}
	}
}
