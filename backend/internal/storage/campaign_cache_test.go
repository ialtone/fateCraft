package storage

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

type definitionProviderStub struct {
	calls int
	id    uuid.UUID
	hash  string
	rows  map[string]GameplayDefinition
	err   error
}

func (s *definitionProviderStub) DefinitionsForVersion(_ context.Context, id uuid.UUID, hash string) (map[string]GameplayDefinition, error) {
	s.calls++
	s.id = id
	s.hash = hash
	return s.rows, s.err
}

func TestCampaignActionStoreUsesPinnedDefinitionProviderOnce(t *testing.T) {
	versionID := uuid.New()
	provider := &definitionProviderStub{rows: map[string]GameplayDefinition{
		definitionMapKey(DefinitionItem, "herb"): {Kind: DefinitionItem, Key: "herb", Name: "Herb"},
	}}
	store := newCampaignActionStore(context.Background(), nil, Campaign{
		ChallengeVersionID: versionID, ChallengeHash: "hash",
	}, provider)
	for range 2 {
		definition, err := store.Definition(versionID, DefinitionItem, "herb")
		if err != nil || definition.Name != "Herb" {
			t.Fatalf("Definition() = %#v, %v", definition, err)
		}
	}
	if provider.calls != 1 || provider.id != versionID || provider.hash != "hash" {
		t.Fatalf("provider calls = %d, id = %s, hash = %q", provider.calls, provider.id, provider.hash)
	}
	if _, err := store.Definition(uuid.New(), DefinitionItem, "herb"); err != ErrNotFound {
		t.Fatalf("cross-version lookup error = %v, want ErrNotFound", err)
	}
}
