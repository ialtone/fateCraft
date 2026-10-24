package challengeimport

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/storage"
)

func TestNarrativeGuardsPackageExchangeRoundTrip(t *testing.T) {
	loaded, err := loadDirectory(filepath.Join("..", "..", "data", "preset_packs", "reincarnation-trials"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	guards := []gameplay.NarrativeGuard{{Pattern: "永生不死", Message: "本玩法不允许永生"}}
	encoded, err := json.Marshal(guards)
	if err != nil {
		t.Fatal(err)
	}
	loaded.document.Runtime.NarrativeGuards = storage.JSONDoc(encoded)
	loaded.document.Version.ContentHash = ""
	p, err := portableFromDocument(&loaded.document)
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"json", "zip"} {
		t.Run(format, func(t *testing.T) {
			var raw []byte
			var decoded *loadedChallenge
			if format == "json" {
				raw, err = json.Marshal(p)
				if err == nil {
					decoded, _, err = decodePortable(raw, defaultRegistry())
				}
			} else {
				raw, err = encodeArchive(p)
				if err == nil {
					// ZIP re-import exercises runtime.json strict loading as well.
					decoded, _, err = decodeArchive(raw, defaultRegistry())
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			runtime, err := storage.DecodeGameplayRuntime(decoded.document.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(runtime.NarrativeGuards, guards) || decoded.hash != p.ContentHash {
				t.Fatalf("%s exchange changed guards or content hash: %#v", format, runtime.NarrativeGuards)
			}
		})
	}
}
