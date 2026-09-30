package challenge

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"testing"

	"github.com/ialtone/fatecraft/internal/storage"
)

func TestDraftRejectsOldManifestAndBadPointsBeforeWriting(t *testing.T) {
	service := &Service{}
	for _, input := range []SaveVersionInput{
		{Manifest: json.RawMessage(`{"reward_rules":[]}`)},
		{Manifest: json.RawMessage(`{}`), Outcomes: []storage.ChallengeOutcomeRule{{RewardPoints: -1}}},
		{Manifest: json.RawMessage(`{}`), Outcomes: []storage.ChallengeOutcomeRule{{RewardPoints: storage.MaxRewardQuantity + 1}}},
	} {
		if _, err := service.SaveDraft(context.Background(), uuid.New(), input); err == nil {
			t.Fatal("invalid draft reached storage")
		}
	}
}

func TestRewardPointsAreBoundedAndPartOfVersionHash(t *testing.T) {
	doc := validVersionDocument(t)
	before, err := ContentHashDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	doc.Outcomes[0].RewardPoints++
	after, err := ContentHashDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("changing points did not change version hash")
	}
	for _, quantity := range []int64{-1, storage.MaxRewardQuantity + 1} {
		doc.Outcomes[0].RewardPoints = quantity
		if err := ValidateDocument(doc, testRegistry()); err == nil {
			t.Fatalf("accepted %d points", quantity)
		}
	}
	doc.Outcomes[0].RewardPoints = 0
	if err := ValidateDocument(doc, testRegistry()); err != nil {
		t.Fatalf("zero points: %v", err)
	}
}

func TestRemovedRewardConfigurationIsRejected(t *testing.T) {
	for _, payload := range []string{
		`{"reward_rules":[]}`,
		`{"outcomes":[{"outcome_key":"end","reward_definition_id":"old"}]}`,
		`{"outcomes":[{"outcome_key":"end","reward_points":1.5}]}`,
	} {
		var manifest Manifest
		if err := json.Unmarshal([]byte(payload), &manifest); err == nil {
			t.Fatalf("accepted manifest: %s", payload)
		}
	}
	var input SaveVersionInput
	if err := json.Unmarshal([]byte(`{"reward_rules":[]}`), &input); err == nil {
		t.Fatal("accepted removed draft reward rules")
	}
	for _, key := range []string{"unknown.coin", "reincarnation-trials.spirit-stone"} {
		doc := validVersionDocument(t)
		var manifest Manifest
		if err := json.Unmarshal(doc.Version.Manifest, &manifest); err != nil {
			t.Fatal(err)
		}
		manifest.EntryRequirement = map[string]any{"type": "reward_balance", "reward_key": key, "quantity": 1}
		doc.Version.Manifest, _ = json.Marshal(manifest)
		if err := ValidateDocument(doc, testRegistry()); err == nil {
			t.Fatalf("accepted entry currency: %s", key)
		}
	}
	doc := validVersionDocument(t)
	doc.Runtime.ActionConfig = storage.JSONDoc(`{"model":["complete_challenge"],"player":[],"capabilities":{"inspect":{"cost_reward_key":"custom.coin","cost_quantity":1}}}`)
	if err := ValidateDocument(doc, testRegistry()); err == nil || !strings.Contains(err.Error(), "消耗配置") {
		t.Fatalf("custom capability cost: %v", err)
	}
}
