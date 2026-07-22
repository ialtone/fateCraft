package challengeimport

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ialtone/fatecraft/internal/challenge"
)

func TestBuiltinDirectoriesReturnsSortedDirectoriesOnly(t *testing.T) {
	dataDir := t.TempDir()
	root := filepath.Join(dataDir, "preset_packs")
	for _, name := range []string{"z-pack", "a-pack"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := builtinDirectories(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(root, "a-pack"), filepath.Join(root, "z-pack")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("builtinDirectories() = %v, want %v", got, want)
	}
}

func TestLoadBundledReincarnationTrials(t *testing.T) {
	root := filepath.Join("..", "..", "data", "preset_packs", "reincarnation-trials")
	loaded, err := loadDirectory(root, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.manifest.Version != "1.0.0" {
		t.Fatalf("version = %q, want 1.0.0", loaded.manifest.Version)
	}
	if loaded.hash == "" {
		t.Fatal("content hash is empty")
	}
	if err := challenge.ValidateDocument(&loaded.document); err != nil {
		t.Fatalf("bundled challenge validation failed: %v", err)
	}
}

func TestBuiltinDirectoriesRejectsEmptyRoot(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "preset_packs"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := builtinDirectories(dataDir)
	if err == nil || !strings.Contains(err.Error(), "没有找到内置挑战") {
		t.Fatalf("builtinDirectories() error = %v, want empty-root error", err)
	}
}

func TestDirectoryBySlugDoesNotAcceptFilesystemPaths(t *testing.T) {
	dataDir := filepath.Join("..", "..", "data")
	importer := New(nil, dataDir)
	directory, err := importer.directoryBySlug("reincarnation-trials")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(directory) != "reincarnation-trials" {
		t.Fatalf("directory = %q", directory)
	}
	if _, err := importer.directoryBySlug("../reincarnation-trials"); err != ErrBuiltinNotFound {
		t.Fatalf("path-like slug error = %v, want ErrBuiltinNotFound", err)
	}
}

func TestCompareStableVersions(t *testing.T) {
	tests := []struct {
		left  string
		right string
		want  int
	}{
		{left: "1.0.0", right: "1.0.0", want: 0},
		{left: "1.1.0", right: "1.0.9", want: 1},
		{left: "2.0.0", right: "10.0.0", want: -1},
	}
	for _, test := range tests {
		got, err := compareStableVersions(test.left, test.right)
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Fatalf("compareStableVersions(%q, %q) = %d, want %d", test.left, test.right, got, test.want)
		}
	}
	for _, invalid := range []string{"1", "1.0", "01.0.0", "1.0.0-beta"} {
		if _, err := parseStableVersion(invalid); err == nil {
			t.Fatalf("parseStableVersion(%q) error = nil", invalid)
		}
	}
}
