package campaigninit

import (
	"github.com/ialtone/fatecraft/internal/storage"
	"reflect"
	"testing"
)

func TestSelectStartingTechniquesUsesDeterministicPools(t *testing.T) {
	definitions := []storage.GameplayDefinition{
		{Kind: storage.DefinitionTechnique, Key: "fixed", Content: storage.JSONMap{"starting_known": true}},
		{Kind: storage.DefinitionTechnique, Key: "wood", Content: storage.JSONMap{"starting_pool": "primary", "starting_weight": int64(1)}},
		{Kind: storage.DefinitionTechnique, Key: "fire", Content: storage.JSONMap{"starting_pool": "primary", "starting_weight": int64(2)}},
		{Kind: storage.DefinitionTechnique, Key: "water", Content: storage.JSONMap{"starting_pool": "primary", "starting_weight": int64(1)}},
		{Kind: storage.DefinitionTechnique, Key: "alchemy", Content: storage.JSONMap{"starting_pool": "craft"}},
		{Kind: storage.DefinitionTechnique, Key: "forging", Content: storage.JSONMap{"starting_pool": "craft"}},
	}

	first, err := StartingTechniqueKeys(definitions, 42)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := StartingTechniqueKeys(definitions, 42)
	if err != nil || !reflect.DeepEqual(first, replayed) {
		t.Fatalf("same seed must reproduce selection: first=%v replayed=%v err=%v", first, replayed, err)
	}

	seenPrimary := map[string]bool{}
	for seed := int64(0); seed < 512; seed++ {
		selected, selectErr := StartingTechniqueKeys(definitions, seed)
		if selectErr != nil {
			t.Fatal(selectErr)
		}
		if !selected["fixed"] {
			t.Fatal("explicit starting technique was omitted")
		}
		primaryCount := 0
		for _, key := range []string{"wood", "fire", "water"} {
			if selected[key] {
				primaryCount++
				seenPrimary[key] = true
			}
		}
		craftCount := 0
		for _, key := range []string{"alchemy", "forging"} {
			if selected[key] {
				craftCount++
			}
		}
		if primaryCount != 1 || craftCount != 1 {
			t.Fatalf("each pool must select exactly one: seed=%d selected=%v", seed, selected)
		}
	}
	if len(seenPrimary) != 3 {
		t.Fatalf("seed range did not cover all primary candidates: %v", seenPrimary)
	}
}

func TestSelectStartingTechniquesRejectsInvalidPoolConfiguration(t *testing.T) {
	_, err := StartingTechniqueKeys([]storage.GameplayDefinition{{
		Kind: storage.DefinitionTechnique, Key: "invalid",
		Content: storage.JSONMap{"starting_known": true, "starting_pool": "primary"},
	}}, 1)
	if err == nil {
		t.Fatal("starting_known plus starting_pool must be rejected")
	}
}

func TestSelectInitialCampaignRealmReturnsKeyAndRankFromSameDefinition(t *testing.T) {
	definitions := []storage.GameplayDefinition{
		{Kind: storage.DefinitionRealm, Key: "higher", Content: storage.JSONMap{"rank": int64(9)}},
		{Kind: storage.DefinitionItem, Key: "unrelated", Content: storage.JSONMap{}},
		{Kind: storage.DefinitionRealm, Key: "initial", Content: storage.JSONMap{"rank": int64(3)}},
	}
	realm, rank, err := selectInitialCampaignRealm(definitions)
	if err != nil {
		t.Fatal(err)
	}
	if realm.Key != "initial" || rank != 3 {
		t.Fatalf("initial realm pair = %s/%d", realm.Key, rank)
	}
}

func TestSelectInitialCampaignRealmRejectsInvalidOrDuplicateRanks(t *testing.T) {
	tests := [][]storage.GameplayDefinition{
		{{Kind: storage.DefinitionRealm, Key: "missing", Content: storage.JSONMap{}}},
		{
			{Kind: storage.DefinitionRealm, Key: "first", Content: storage.JSONMap{"rank": int64(0)}},
			{Kind: storage.DefinitionRealm, Key: "second", Content: storage.JSONMap{"rank": int64(0)}},
		},
	}
	for index, definitions := range tests {
		if _, _, err := selectInitialCampaignRealm(definitions); err == nil {
			t.Fatalf("invalid realm ranks in case %d were accepted", index)
		}
	}
}
