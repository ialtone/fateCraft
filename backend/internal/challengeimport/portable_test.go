package challengeimport

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestBuiltinPackageExchangeRoundTrip(t *testing.T) {
	dirs, err := builtinDirectories(filepath.Join("..", "..", "data"))
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			loaded, err := loadDirectory(dir, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			// Database edits can reorder assets independently of their source filenames.
			loaded.document.Assets[0].SortOrder = 71
			p, err := portableFromDocument(&loaded.document)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			fromJSON, _, err := decodePortable(raw, defaultRegistry())
			if err != nil {
				t.Fatal(err)
			}
			if fromJSON.hash != p.ContentHash {
				t.Fatal("JSON content hash changed")
			}
			zipped, err := encodeArchive(p)
			if err != nil {
				t.Fatal(err)
			}
			fromZIP, _, err := decodeArchive(zipped, defaultRegistry())
			if err != nil {
				t.Fatal(err)
			}
			if fromZIP.hash != p.ContentHash {
				t.Fatal("ZIP content hash changed")
			}
			if fromZIP.document.Assets[0].ID == loaded.document.Assets[0].ID {
				t.Fatal("database identity leaked")
			}
			p.ContentHash = "wrong"
			raw, _ = json.Marshal(p)
			if _, _, err := decodePortable(raw, defaultRegistry()); err == nil {
				t.Fatal("forged hash accepted")
			}
		})
	}
}

func TestUnsafeArchivesRejected(t *testing.T) {
	for _, name := range []string{"../pack.json", "C:/pack.json", "folder/../../pack.json", "folder\\pack.json", "file:stream.json", "CON.json", "folder./pack.json"} {
		t.Run(name, func(t *testing.T) {
			var b bytes.Buffer
			w := zip.NewWriter(&b)
			f, _ := w.Create(name)
			f.Write([]byte("{}"))
			w.Close()
			if _, _, err := decodeArchive(b.Bytes(), defaultRegistry()); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}
