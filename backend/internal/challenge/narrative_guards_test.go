package challenge

import (
	"testing"

	"github.com/ialtone/fatecraft/internal/storage"
)

func TestContentHashIncludesNarrativeGuards(t *testing.T) {
	doc := validVersionDocument(t)
	hash := func() string {
		t.Helper()
		h, err := ContentHashDocument(doc)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	emptyHash := hash()
	doc.Runtime.NarrativeGuards = storage.JSONDoc(`[]`)
	if hash() != emptyHash {
		t.Fatal("empty guards must have the same identity as omitted guards")
	}
	doc.Runtime.NarrativeGuards = storage.JSONDoc(`[{"pattern":"永生不死","message":"本玩法不允许永生"}]`)
	guardHash := hash()
	if guardHash == emptyHash {
		t.Fatal("narrative guards are missing from content identity")
	}
	doc.Runtime.NarrativeGuards = storage.JSONDoc(`[{ "message": "本玩法不允许永生", "pattern": "永生不死" }]`)
	if hash() != guardHash {
		t.Fatal("guard JSON formatting changed content identity")
	}
	doc.Runtime.NarrativeGuards = storage.JSONDoc(`[{"pattern":"飞升仙界","message":"本玩法不允许永生"}]`)
	if hash() == guardHash {
		t.Fatal("modified guard did not change content identity")
	}
}
