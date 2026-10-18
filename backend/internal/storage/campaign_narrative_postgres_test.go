package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/testdb"
)

func TestPostgresCampaignNarrativeBindingAndStaleRejection(t *testing.T) {
	db := testdb.Open(t, "campaign_narrative")
	ctx := context.Background()
	playerID, packID, versionID, campaignID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	hash := strings.Repeat("0", 64)
	if err := db.Exec(`INSERT INTO users (id, username, display_name) VALUES (?, 'narrative-player', 'Narrative Player')`, playerID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO challenge_packs (id, slug, name) VALUES (?, 'narrative-pack', 'Narrative Pack')`, packID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO challenge_versions
        (id, challenge_pack_id, version, status, manifest, content_hash, published_at)
        VALUES (?, ?, '1.0.0', 'published', '{}'::jsonb, ?, CURRENT_TIMESTAMP)`, versionID, packID, hash).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`UPDATE challenge_packs SET current_version_id=?,status='active' WHERE id=?`, versionID, packID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO campaigns
        (id, player_id, title, challenge_version_id, challenge_hash, current_location_key, random_seed, llm_route)
        VALUES (?, ?, 'Narrative', ?, ?, 'world.start', 1, 'server')`, campaignID, playerID, versionID, hash).Error; err != nil {
		t.Fatal(err)
	}
	repository := NewCampaignRepository(db)
	now := time.Now()
	conversation := &Conversation{
		ID: uuid.New(), PlayerID: playerID.String(), Title: "Narrative",
		Status: StatusActive, ChallengeVersionID: versionID, ChallengeHash: hash,
		CampaignID: &campaignID, LLMRoute: LLMRouteServer, CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.CreateConversation(ctx, playerID, conversation, JSONMap{"scene": map[string]any{}}, 1); err != nil {
		t.Fatal(err)
	}

	result, err := repository.CommitAction(ctx, CommitCampaignActionInput{
		CampaignID: campaignID, PlayerID: playerID, ExpectedRevision: 0,
		IdempotencyKey: "action-1", ActionKey: "test.tick", ActorType: "player", Label: "测试动作",
	}, func(_ *CampaignActionStore, _ Campaign) (CampaignActionOutcome, error) {
		return CampaignActionOutcome{
			GameTimeDelta: 15,
			Events: []CampaignEventDraft{{
				EventType: "test.tick", Visibility: CampaignVisibilityPlayer, Payload: JSONMap{},
			}},
			Result: JSONMap{
				"narrative_feedback": map[string]any{
					"changes": []any{map[string]any{"path": "character.health", "before": 100, "after": 90}},
					"current_facts": map[string]any{
						"character.health": 90, "character.max_health": 100,
						"character.injuries": map[string]any{"arm": "bruise"},
					},
					"goal_succeeded": true,
				},
			},
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Narrative == nil {
		t.Fatal("committed action did not return a narrative result")
	}
	if len(result.Campaign.StateHash) != 64 {
		t.Fatalf("state hash = %q", result.Campaign.StateHash)
	}
	if result.Narrative.GoalStatus != "completed" || len(result.Narrative.Changes) != 1 {
		t.Fatalf("unexpected narrative result: %+v", result.Narrative)
	}
	var storedHash string
	if err := db.Raw(`SELECT state_hash FROM campaigns WHERE id = ?`, campaignID).Scan(&storedHash).Error; err != nil {
		t.Fatal(err)
	}
	if storedHash != result.Campaign.StateHash {
		t.Fatalf("stored hash %q != result hash %q", storedHash, result.Campaign.StateHash)
	}
	replayed, err := repository.CommitAction(ctx, CommitCampaignActionInput{
		CampaignID: campaignID, PlayerID: playerID, ExpectedRevision: 0,
		IdempotencyKey: "action-1", ActionKey: "test.tick", ActorType: "player", Label: "测试动作",
	}, func(_ *CampaignActionStore, _ Campaign) (CampaignActionOutcome, error) {
		t.Fatal("idempotent replay must not execute the action again")
		return CampaignActionOutcome{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || replayed.Narrative == nil || replayed.Narrative.Summary != result.Narrative.Summary {
		t.Fatalf("unexpected replay: %+v", replayed)
	}
	if replayed.Campaign.StateHash != result.Campaign.StateHash {
		t.Fatalf("replay hash %q != %q", replayed.Campaign.StateHash, result.Campaign.StateHash)
	}
	inputs, err := repository.NarrativeInputs(ctx, campaignID, result.Campaign.Revision, 4)
	if err != nil || len(inputs) != 1 {
		t.Fatalf("narrative inputs = %d, err=%v", len(inputs), err)
	}
	binding := NarrativeBinding{
		CampaignID:       campaignID,
		Revision:         result.Campaign.Revision,
		StateHash:        result.Campaign.StateHash,
		Actions:          []NarrativeActionBinding{{ID: "action-1", Revision: result.Campaign.Revision}},
		ValidationStatus: "passed",
	}
	displayID := uuid.New()
	_, display, bindErr := NewMessageRepo(db).CreateAssistantWithNarrativeBinding(
		ctx, conversation.ID, displayID, "叙事", "叙事", nil, nil, nil, binding,
	)
	if bindErr != nil {
		t.Fatal(bindErr)
	}
	if display.CampaignRevision == nil {
		t.Fatal("display campaign revision is missing")
	}
	if *display.CampaignRevision != result.Campaign.Revision {
		t.Fatalf("display revision = %d, want %d", *display.CampaignRevision, result.Campaign.Revision)
	}
	if display.CampaignStateHash == nil || *display.CampaignStateHash != result.Campaign.StateHash {
		t.Fatalf("display state hash = %v, want %s", display.CampaignStateHash, result.Campaign.StateHash)
	}
	if display.ValidationStatus != "passed" {
		t.Fatalf("display validation status = %q", display.ValidationStatus)
	}
	inputs, err = repository.NarrativeInputs(ctx, campaignID, result.Campaign.Revision, 4)
	if err != nil || len(inputs) != 0 {
		t.Fatalf("bound action remained unconsumed: %d, err=%v", len(inputs), err)
	}
	stale := binding
	stale.Revision = result.Campaign.Revision - 1
	if _, _, err := NewMessageRepo(db).CreateAssistantWithNarrativeBinding(
		ctx, conversation.ID, uuid.New(), "过期叙事", "过期叙事", nil, nil, nil, stale,
	); !errors.Is(err, ErrNarrativeStale) {
		t.Fatalf("stale binding error = %v, want ErrNarrativeStale", err)
	}
}
