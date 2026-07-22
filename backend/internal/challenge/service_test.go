package challenge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
)

func validVersionDocument(t *testing.T) *VersionDocument {
	t.Helper()
	manifest := Manifest{
		ID: "test-pack", Name: "Test Pack", Version: "1.0.0", Description: "test",
		DefaultConfig: DefaultConfig{
			RecommendedCharacter: "character", RecommendedPersona: "persona",
			RecommendedScenario: "scenario", RecommendedPrompt: "prompt",
			AllowedPersonas: []string{"persona"},
		},
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	prompt := storage.JSONDoc(`{
		"id":"prompt",
		"segments":[
			{"key":"system_master","role":"system","content":"system","enabled":true,"required":true},
			{"key":"settlement_security","role":"system","content":"reward-a","enabled":true,"required":true},
			{"key":"output_schema","role":"system","content":"schema","enabled":true,"required":true}
		]
	}`)
	return &VersionDocument{
		Version: storage.ChallengeVersion{Version: manifest.Version, Manifest: storage.JSONDoc(manifestJSON)},
		Assets: []storage.ChallengeAsset{
			{Kind: storage.ChallengeAssetCharacter, Slug: "character", Name: "Character", Content: storage.JSONDoc(`{"id":"character"}`)},
			{Kind: storage.ChallengeAssetPersona, Slug: "persona", Name: "Persona", Content: storage.JSONDoc(`{"id":"persona"}`)},
			{Kind: storage.ChallengeAssetScenario, Slug: "scenario", Name: "Scenario", Content: storage.JSONDoc(`{"id":"scenario"}`)},
			{Kind: storage.ChallengeAssetPrompt, Slug: "prompt", Name: "Prompt", Content: prompt},
		},
		RewardRules: []storage.ChallengeRewardRule{{
			RewardKey: "reward-a", EndingType: "good", EndingTitle: "Success",
			Conditions: storage.JSONMap{
				"min_cultivation_rank": 0, "required_successful_rolls": []any{},
			},
			Calculation: storage.JSONMap{"multiplier_bps": 10000}, Enabled: true,
		}},
	}
}

type settlementReaderStub struct {
	rule       *storage.ChallengeRewardRule
	successful []string
}

func (s settlementReaderStub) GetRun(context.Context, uuid.UUID) (*storage.ChallengeRun, error) {
	return nil, storage.ErrNotFound
}

func (s settlementReaderStub) RewardRule(context.Context, uuid.UUID, string) (*storage.ChallengeRewardRule, error) {
	if s.rule == nil {
		return nil, storage.ErrNotFound
	}
	return s.rule, nil
}

func (s settlementReaderStub) SuccessfulRollTypes(context.Context, uuid.UUID, []string) ([]string, error) {
	return s.successful, nil
}

func TestContentHashCanonicalizesJSONAndDoesNotMutateInput(t *testing.T) {
	versionID := uuid.New()
	assetsA := []storage.ChallengeAsset{
		{ChallengeVersionID: versionID, Kind: "persona", Slug: "b", Name: "B", Content: storage.JSONDoc(`{"z":2,"a":1}`)},
		{ChallengeVersionID: versionID, Kind: "character", Slug: "a", Name: "A", Content: storage.JSONDoc(`{"id":"a"}`)},
	}
	assetsB := []storage.ChallengeAsset{
		{ChallengeVersionID: versionID, Kind: "character", Slug: "a", Name: "A", Content: storage.JSONDoc(`{ "id": "a" }`)},
		{ChallengeVersionID: versionID, Kind: "persona", Slug: "b", Name: "B", Content: storage.JSONDoc(`{"a":1, "z":2}`)},
	}
	rulesA := []storage.ChallengeRewardRule{
		{RewardKey: "z", EndingType: "good", EndingTitle: "Z", Conditions: storage.JSONMap{}, Calculation: storage.JSONMap{"multiplier_bps": 10000}, Enabled: true},
		{RewardKey: "a", EndingType: "good", EndingTitle: "A", Conditions: storage.JSONMap{}, Calculation: storage.JSONMap{"multiplier_bps": 20000}, Enabled: true},
	}
	rulesB := []storage.ChallengeRewardRule{rulesA[1], rulesA[0]}

	hashA, err := ContentHash(storage.JSONDoc(`{"name":"test","id":"pack"}`), assetsA, rulesA)
	if err != nil {
		t.Fatal(err)
	}
	hashB, err := ContentHash(storage.JSONDoc(`{ "id": "pack", "name": "test" }`), assetsB, rulesB)
	if err != nil {
		t.Fatal(err)
	}
	if hashA != hashB {
		t.Fatalf("canonical hashes differ: %s != %s", hashA, hashB)
	}
	if assetsA[0].Slug != "b" || rulesA[0].RewardKey != "z" {
		t.Fatal("ContentHash mutated caller slices")
	}
}

func TestValidateDocumentRequiresSettlementSecurity(t *testing.T) {
	doc := validVersionDocument(t)
	prompt := &doc.Assets[len(doc.Assets)-1]
	prompt.Content = storage.JSONDoc(strings.ReplaceAll(string(prompt.Content), "settlement_security", "optional_notes"))

	err := ValidateDocument(doc)
	if err == nil || !strings.Contains(err.Error(), "settlement_security") {
		t.Fatalf("ValidateDocument() error = %v, want missing settlement_security", err)
	}
}

func TestValidateDocumentRequiresRecommendedPersonaToBeAllowed(t *testing.T) {
	doc := validVersionDocument(t)
	var manifest Manifest
	if err := json.Unmarshal(doc.Version.Manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.DefaultConfig.AllowedPersonas = []string{"other-persona"}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	doc.Version.Manifest = storage.JSONDoc(manifestJSON)
	doc.Assets = append(doc.Assets, storage.ChallengeAsset{
		Kind: storage.ChallengeAssetPersona, Slug: "other-persona", Name: "Other",
		Content: storage.JSONDoc(`{"id":"other-persona"}`),
	})

	err = ValidateDocument(doc)
	if err == nil || !strings.Contains(err.Error(), "recommended_persona") {
		t.Fatalf("ValidateDocument() error = %v, want recommended_persona error", err)
	}
}

func TestCultivationRankNormalization(t *testing.T) {
	tests := map[string]int{
		"凡人": 0, "练气三层": 1, "筑基期": 2, "金丹大圆满": 3,
		"元嬰期": 4, "化神": 5, "炼虚后期": 6, "合体期": 7, "大乘期": 8,
		"未记录": -1, "尚未达到元婴期": -1,
	}
	for level, want := range tests {
		if got := cultivationRank(level); got != want {
			t.Errorf("cultivationRank(%q) = %d, want %d", level, got, want)
		}
	}
}

func TestValidateDocumentRejectsMalformedRewardConditions(t *testing.T) {
	tests := []struct {
		name       string
		conditions storage.JSONMap
	}{
		{name: "missing rank", conditions: storage.JSONMap{"required_successful_rolls": []any{}}},
		{name: "rank out of range", conditions: storage.JSONMap{"min_cultivation_rank": 9, "required_successful_rolls": []any{}}},
		{name: "fractional rank", conditions: storage.JSONMap{"min_cultivation_rank": 3.5, "required_successful_rolls": []any{}}},
		{name: "empty roll type", conditions: storage.JSONMap{"min_cultivation_rank": 3, "required_successful_rolls": []any{""}}},
		{name: "duplicate roll type", conditions: storage.JSONMap{"min_cultivation_rank": 3, "required_successful_rolls": []any{"结丹突破判定", "结丹突破判定"}}},
		{name: "unknown condition", conditions: storage.JSONMap{"min_cultivation_rank": 3, "required_successful_rolls": []any{}, "typo": true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := validVersionDocument(t)
			doc.RewardRules[0].Conditions = tt.conditions
			if err := ValidateDocument(doc); err == nil {
				t.Fatal("ValidateDocument() error = nil, want invalid conditions")
			}
		})
	}
}

func TestMissingRequiredRollsUsesExactUniqueTypes(t *testing.T) {
	required := []string{"筑基突破判定", "结丹突破判定", "仙缘获取判定"}
	successful := []string{"筑基突破判定", "结丹突破判定-伪造后缀", "仙缘获取判定", "仙缘获取判定"}
	missing := missingRequiredRolls(required, successful)
	if len(missing) != 1 || missing[0] != "结丹突破判定" {
		t.Fatalf("missingRequiredRolls() = %v, want [结丹突破判定]", missing)
	}
}

func TestEvaluateSettlementRejectsFabricatedAffinityEndingAtQiRefining(t *testing.T) {
	playerID := uuid.New()
	conv := &storage.Conversation{
		ID: uuid.New(), PlayerID: playerID.String(), Status: storage.StatusActive,
		ChallengeVersionID: uuid.New(),
	}
	rule := &storage.ChallengeRewardRule{
		RewardKey: "affinity_ascension", EndingType: "good", EndingTitle: "仙缘飞升", Enabled: true,
		Conditions: storage.JSONMap{
			"min_cultivation_rank":      3,
			"required_successful_rolls": []any{"筑基突破判定", "结丹突破判定", "仙缘获取判定"},
		},
		Calculation: storage.JSONMap{"multiplier_bps": 15000},
	}
	_, err := evaluateSettlement(context.Background(), settlementReaderStub{
		rule: rule, successful: []string{"筑基突破判定", "结丹突破判定", "仙缘获取判定"},
	}, conv, SettlementInput{
		ConversationID: conv.ID, PlayerID: playerID, EndingType: "good",
		EndingTitle: "仙缘飞升", RewardKey: "affinity_ascension",
		CultivationLevel: "练气三层", BaseReward: 999999,
	})
	if !errors.Is(err, ErrSettlementDenied) || !strings.Contains(err.Error(), "境界") {
		t.Fatalf("evaluateSettlement() error = %v, want cultivation denial", err)
	}
}

func TestEvaluateSettlementAcceptsExactServerEvidence(t *testing.T) {
	playerID := uuid.New()
	conv := &storage.Conversation{
		ID: uuid.New(), PlayerID: playerID.String(), Status: storage.StatusActive,
		ChallengeVersionID: uuid.New(),
	}
	required := []any{"筑基突破判定", "结丹突破判定", "仙缘获取判定"}
	rule := &storage.ChallengeRewardRule{
		RewardKey: "affinity_ascension", EndingType: "good", EndingTitle: "仙缘飞升", Enabled: true,
		Conditions:  storage.JSONMap{"min_cultivation_rank": 3, "required_successful_rolls": required},
		Calculation: storage.JSONMap{"multiplier_bps": 15000},
	}
	evaluation, err := evaluateSettlement(context.Background(), settlementReaderStub{
		rule: rule, successful: []string{"筑基突破判定", "结丹突破判定", "仙缘获取判定"},
	}, conv, SettlementInput{
		ConversationID: conv.ID, PlayerID: playerID, EndingType: "good",
		EndingTitle: "仙缘飞升", RewardKey: "affinity_ascension",
		CultivationLevel: "结丹初期", BaseReward: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.rewardAmount != 150 {
		t.Fatalf("rewardAmount = %d, want 150", evaluation.rewardAmount)
	}
}
