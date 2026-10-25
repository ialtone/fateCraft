package challengeimport

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/gameplay/bootstrap"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/testdb"
)

type moduleTestRoute string

func (r moduleTestRoute) ReadyRoute(context.Context, string) (string, error) { return string(r), nil }

// This uses normal draft/save/publication, Campaign initialization, player
// actions, rewind and settlement. It never modifies a published fixture.
func TestPostgresModulePublicationSettlementReplayAndRewind(t *testing.T) {
	for _, scenario := range []struct {
		name, route string
		points      int64
	}{
		{"server-points", storage.LLMRouteServer, 12},
		{"server-zero", storage.LLMRouteServer, 0},
		{"client-no-points", storage.LLMRouteClient, 12},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			db := testdb.Open(t, "module_lifecycle")
			player := uuid.New()
			if err := db.Exec("INSERT INTO users (id, username, display_name) VALUES (?, 'module-test', 'Module Test')", player).Error; err != nil {
				t.Fatal(err)
			}
			ctx := identity.WithPlayerID(context.Background(), player.String())
			registry := bootstrap.NewRegistry()
			repo := storage.NewChallengeRepository(db)
			challenges := challenge.NewService(repo, registry)
			loaded, err := loadDirectory(filepath.Join("..", "..", "data", "preset_packs", "mountain-inn"), time.Now(), registry)
			if err != nil {
				t.Fatal(err)
			}
			for i := range loaded.document.Outcomes {
				if loaded.document.Outcomes[i].OutcomeKey == "prosperous" {
					loaded.document.Outcomes[i].RewardPoints = scenario.points
				}
			}
			// The progress-reward contract derives its cap from the lowest
			// positive success outcome. Keep this lifecycle fixture valid for
			// the small positive payout, and omit progress rewards entirely for
			// the zero-payout case.
			if scenario.points == 0 {
				filtered := loaded.document.Definitions[:0]
				for _, definition := range loaded.document.Definitions {
					if definition.Kind != contract.DefinitionKindChallengeProgressRewards {
						filtered = append(filtered, definition)
					}
				}
				loaded.document.Definitions = filtered
			} else {
				for i := range loaded.document.Definitions {
					definition := &loaded.document.Definitions[i]
					if definition.Kind != contract.DefinitionKindChallengeProgressRewards {
						continue
					}
					definition.Content["stage_cap"] = int64(scenario.points / 2)
					if stages, ok := definition.Content["stages"].([]any); ok {
						for _, raw := range stages {
							if stage, ok := raw.(map[string]any); ok {
								stage["points"] = int64(2)
							}
						}
					}
				}
			}
			pack, err := challenges.CreatePack(ctx, player, challenge.CreatePackInput{Slug: loaded.manifest.ID, Name: loaded.manifest.Name, Description: loaded.manifest.Description})
			if err != nil {
				t.Fatal(err)
			}
			draft, err := challenges.CreateDraft(ctx, pack.ID, player, loaded.manifest.Version)
			if err != nil {
				t.Fatal(err)
			}
			_, err = challenges.SaveDraft(ctx, draft.Version.ID, challenge.SaveVersionInput{Manifest: json.RawMessage(loaded.document.Version.Manifest), Runtime: loaded.document.Runtime, Assets: loaded.document.Assets, Definitions: loaded.document.Definitions, Outcomes: loaded.document.Outcomes})
			if err != nil {
				t.Fatal(err)
			}
			published, err := challenges.Publish(ctx, draft.Version.ID, player)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := challenges.SetEnabled(ctx, pack.ID, true); err != nil {
				t.Fatal(err)
			}
			exported, err := challenges.VersionDocument(ctx, published.Version.ID)
			if err != nil {
				t.Fatal(err)
			}
			hash, err := challenge.ContentHashDocument(exported)
			if err != nil || hash != published.Version.ContentHash {
				t.Fatalf("published export changed hash: %v", err)
			}
			campaigns := campaign.NewService(storage.NewCampaignRepository(db), challenges, nil, identity.NewContext(), registry)
			campaigns.SetRouteResolver(moduleTestRoute(scenario.route))
			bootstrap.InstallCampaign(campaigns)
			row, err := campaigns.Create(ctx, campaign.CreateInput{ChallengeSlug: pack.Slug})
			if err != nil {
				t.Fatal(err)
			}
			conv, err := campaigns.CreateConversation(ctx, row.ID)
			if err != nil {
				t.Fatal(err)
			}
			var characterCount int64
			if err := db.Model(&storage.CampaignCharacter{}).Where("campaign_id = ?", row.ID).Count(&characterCount).Error; err != nil {
				t.Fatal(err)
			}
			if characterCount != 0 {
				t.Fatal("non-cultivation module created a cultivation character")
			}
			do := func(key, arg string, value any) {
				t.Helper()
				args := map[string]any{}
				if arg != "" {
					args[arg] = value
				}
				result, err := campaigns.ExecutePlayerAction(ctx, row.ID, campaign.ActionInput{ActionID: uuid.NewString(), ExpectedRevision: row.Revision, Key: "management." + key, Arguments: args})
				if err != nil {
					t.Fatalf("%s: %v", key, err)
				}
				*row = result.Campaign
			}
			for _, key := range []string{"kitchen", "guestroom"} {
				do("build", "facility", key)
			}
			do("upgrade", "facility", "guestroom")
			for _, key := range []string{"caravan", "pilgrims"} {
				do("accept", "order", key)
			}
			for range 3 {
				do("produce", "recipe", "meal")
				do("advance", "hours", int64(2))
			}
			for range 3 {
				do("produce", "recipe", "lodging")
				do("advance", "hours", int64(3))
			}
			do("deliver", "order", "caravan")
			do("deliver", "order", "pilgrims")
			// Rewind a completed delivery through the actual charged regenerate path.
			if scenario.route == storage.LLMRouteServer {
				_, err := storage.NewRewardRepository(db).Grant(ctx, storage.RewardMutation{PlayerID: player, RewardKey: storage.MessageRewriteCreditKey, Quantity: 1, SourceType: "test", SourceID: "rewind", IdempotencyKey: "rewind-credit", Metadata: storage.JSONMap{}})
				if err != nil {
					t.Fatal(err)
				}
			}
			regenerated, err := storage.NewTurnRepo(db).BeginRegenerate(ctx, storage.RegenerateInput{ConversationID: conv.ID, PlayerID: player, IdempotencyKey: "module-rewind", InitialState: storage.JSONMap{}})
			if err != nil {
				t.Fatal(err)
			}
			if err := storage.NewTurnRepo(db).SealStatus(ctx, regenerated.Turn.ID, regenerated.Attempt.ID, storage.AttemptStatusCompleted, "", ""); err != nil {
				t.Fatal(err)
			}
			row, err = campaigns.GetOwned(ctx, row.ID)
			if err != nil {
				t.Fatal(err)
			}
			domain, err := storage.NewCampaignRepository(db).DomainState(ctx, row.ID)
			if err != nil {
				t.Fatal(err)
			}
			data := domain.RuntimeState["data"].(map[string]any)
			if data["revenue"] != float64(100) || data["orders"].(map[string]any)["pilgrims"] != "accepted" {
				t.Fatalf("delivery state not restored: %+v", data)
			}
			do("deliver", "order", "pilgrims")
			finish := campaign.ActionInput{ActionID: "finish-once", ExpectedRevision: row.Revision, Key: "management.finish", Arguments: map[string]any{}}
			if _, err := campaigns.ExecuteModelAction(ctx, row.ID, finish); err == nil {
				t.Fatal("model chose final result")
			}
			// Fail after module state and reward writes, before the action commit.
			for _, sql := range []string{
				`CREATE FUNCTION reject_module_finish() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action_key = 'management.finish' THEN RAISE EXCEPTION 'injected module commit failure'; END IF; RETURN NEW; END; $$`,
				`CREATE TRIGGER reject_module_finish BEFORE INSERT ON campaign_action_commits FOR EACH ROW EXECUTE FUNCTION reject_module_finish()`,
			} {
				if err := db.Exec(sql).Error; err != nil {
					t.Fatal(err)
				}
			}
			if _, err := campaigns.ExecutePlayerAction(ctx, row.ID, finish); err == nil {
				t.Fatal("injected failure did not abort")
			}
			fresh, err := campaigns.GetOwned(ctx, row.ID)
			if err != nil || fresh.Revision != row.Revision || fresh.Status != storage.CampaignStatusActive {
				t.Fatalf("half-committed campaign: %+v %v", fresh, err)
			}
			var ledgerCount int64
			if err := db.Model(&storage.RewardLedgerEntry{}).Where("source_type = 'campaign_outcome'").Count(&ledgerCount).Error; err != nil {
				t.Fatal(err)
			}
			if ledgerCount != 0 {
				t.Fatal("rolled-back finish left a reward")
			}
			if _, err := campaigns.Snapshot(ctx, row.ID); err != nil {
				t.Fatalf("runtime state failed rollback: %v", err)
			}
			if err := db.Exec("DROP TRIGGER reject_module_finish ON campaign_action_commits").Error; err != nil {
				t.Fatal(err)
			}
			// Two identical HTTP-equivalent retries must share one terminal action.
			errors := make(chan error, 2)
			for range 2 {
				go func() {
					result, err := campaigns.ExecutePlayerAction(ctx, row.ID, finish)
					if err == nil && (result.Campaign.Status != storage.CampaignStatusCompleted || result.Campaign.Revision != finish.ExpectedRevision+1) {
						err = fmt.Errorf("bad replay result: %+v", result)
					}
					errors <- err
				}()
			}
			for range 2 {
				if err := <-errors; err != nil {
					t.Fatal(err)
				}
			}
			if err := db.Model(&storage.RewardLedgerEntry{}).Where("source_type = 'campaign_outcome'").Count(&ledgerCount).Error; err != nil {
				t.Fatal(err)
			}
			wantCount := int64(0)
			if scenario.route == storage.LLMRouteServer && scenario.points > 0 {
				wantCount = 1
			}
			if ledgerCount != wantCount {
				t.Fatalf("ledger count %d, want %d", ledgerCount, wantCount)
			}
			if wantCount == 1 {
				var entry storage.RewardLedgerEntry
				if err := db.First(&entry, "source_type = 'campaign_outcome'").Error; err != nil {
					t.Fatal(err)
				}
				if entry.QuantityDelta != scenario.points || entry.BalanceAfter != scenario.points {
					t.Fatalf("wrong account settlement: %+v", entry)
				}
			}
			// Legacy abandon endpoint must update the pure runtime and journal too.
			other, err := campaigns.Create(ctx, campaign.CreateInput{ChallengeSlug: pack.Slug})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := campaigns.CreateConversation(ctx, other.ID); err != nil {
				t.Fatal(err)
			}
			abandon := campaign.AbandonInput{ActionID: "close-once", ExpectedRevision: 0}
			for range 2 {
				if _, err := campaigns.Abandon(ctx, other.ID, abandon); err != nil {
					t.Fatal(err)
				}
			}
			domain, err = storage.NewCampaignRepository(db).DomainState(ctx, other.ID)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(domain.RuntimeState)
			var stopped contract.RunState
			if err := json.Unmarshal(raw, &stopped); err != nil {
				t.Fatal(err)
			}
			if stopped.Revision != 1 || stopped.Status != contract.RunFailed || stopped.OutcomeKey != "closed" {
				t.Fatalf("abandon bypassed module: %+v", stopped)
			}
			if _, err := campaigns.Snapshot(ctx, other.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}
