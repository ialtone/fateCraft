package challengeimport

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestBundledBootstrapIncludesEveryBuiltin(t *testing.T) {
	root := filepath.Join("..", "..", "data")
	defaults, err := bootstrapDirectories(root)
	if err != nil {
		t.Fatal(err)
	}
	builtins, err := builtinDirectories(root)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(defaults)
	if len(defaults) != 9 || !reflect.DeepEqual(defaults, builtins) {
		t.Fatalf("startup manifest must contain all nine bundled packs: %v / %v", defaults, builtins)
	}
}

func TestBootstrapManifestDoesNotPublishUnlistedExamples(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"default", "example"} {
		if err := os.MkdirAll(filepath.Join(dir, "preset_packs", name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "bootstrap.json"), []byte(`{"version":1,"packs":["default"]}`), 0644); err != nil {
		t.Fatal(err)
	}
	directories, err := bootstrapDirectories(dir)
	if err != nil || len(directories) != 1 || filepath.Base(directories[0]) != "default" {
		t.Fatalf("unexpected defaults: %v %v", directories, err)
	}
	for _, invalid := range []string{`{"version":1,"packs":["../example"]}`, `{"version":1,"packs":["missing"]}`, `{"version":1,"packs":["default","default"]}`, `{"version":2,"packs":[]}`, `{"version":1,"packs":[],"ignored":true}`} {
		if err := os.WriteFile(filepath.Join(dir, "bootstrap.json"), []byte(invalid), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := bootstrapDirectories(dir); err == nil {
			t.Fatalf("invalid manifest accepted: %s", invalid)
		}
	}
}
