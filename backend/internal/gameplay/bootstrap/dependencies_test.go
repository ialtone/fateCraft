package bootstrap_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPlatformDoesNotImportConcreteGameplay(t *testing.T) {
	root := filepath.Join("..", "..")
	concrete := []string{"cultivation", "survival", "management", "stealth", "relationship", "tomb", "dungeon", "sect"}
	for _, directory := range []string{"api", "campaign", "challenge", "storage", "orchestration", "state", "contextbuilder", "gameplay/contract", "gameplay/kernel", "gameplay/campaigngeneric", "gameplay/components"} {
		err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(directory)), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, spec := range file.Imports {
				dependency, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					return err
				}
				for _, module := range concrete {
					prefix := "github.com/ialtone/fatecraft/internal/gameplay/" + module
					if dependency == prefix || strings.HasPrefix(dependency, prefix+"/") {
						t.Errorf("%s imports concrete module %s", path, dependency)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
