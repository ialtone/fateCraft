package challengeimport

import (
	"context"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/gameplay/bootstrap"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/originrules"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/testdb"
	"path/filepath"
	"testing"
)

func TestPostgresCampaignOriginsAddOnlyTheirConfiguredSupplies(t *testing.T) {
	db := testdb.Open(t, "campaign_origins")
	player := uuid.New()
	if err := db.Exec("INSERT INTO users (id, username, display_name) VALUES (?, 'origin-test', 'Origin Test')", player).Error; err != nil {
		t.Fatal(err)
	}
	ctx := identity.WithPlayerID(context.Background(), player.String())
	registry := bootstrap.NewRegistry()
	repo := storage.NewChallengeRepository(db)
	importer := New(repo, filepath.Join("..", "..", "data"))
	if _, err := importer.ImportBuiltin(ctx, "mortal-path"); err != nil {
		t.Fatal(err)
	}
	pack, err := repo.GetPackBySlug(ctx, "mortal-path")
	if err != nil {
		t.Fatal(err)
	}
	defs, err := repo.ListDefinitions(ctx, *pack.CurrentVersionID)
	if err != nil {
		t.Fatal(err)
	}
	var config originrules.Config
	initial := map[string]int64{}
	for _, d := range defs {
		if d.Kind == originrules.DefinitionKind {
			config, err = originrules.Parse(d.Content)
			if err != nil {
				t.Fatal(err)
			}
		}
		if d.Kind == storage.DefinitionItem {
			if n, ok := testDefinitionInt(d.Content["starting_quantity"]); ok && n > 0 {
				initial[d.Key] = n
			}
		}
	}
	if len(config.Origins) != 6 {
		t.Fatal("missing origin configurations")
	}
	service := campaign.NewService(storage.NewCampaignRepository(db), challenge.NewService(repo, registry), nil, identity.NewContext(), registry)
	service.SetRouteResolver(moduleTestRoute(storage.LLMRouteServer))
	bootstrap.InstallCampaign(service)
	for _, o := range config.Origins {
		row, err := service.Create(ctx, campaign.CreateInput{ChallengeSlug: "mortal-path", PersonaSlug: o.PersonaKey})
		if err != nil {
			t.Fatalf("%s: %v", o.PersonaKey, err)
		}
		var stacks []storage.CampaignItemStack
		if err = db.Where("campaign_id = ?", row.ID).Find(&stacks).Error; err != nil {
			t.Fatal(err)
		}
		if len(stacks) != len(initial) {
			t.Fatal("origin created non-starter or duplicate stack")
		}
		for _, stack := range stacks {
			if stack.Quantity != initial[stack.ItemKey]+o.Items[stack.ItemKey] {
				t.Fatalf("%s %s quantity=%d", o.PersonaKey, stack.ItemKey, stack.Quantity)
			}
			if o.Items[stack.ItemKey] > 0 && stack.Metadata["origin"] != o.PersonaKey {
				t.Fatal("origin provenance missing")
			}
			if stack.Metadata["name"] == nil {
				t.Fatal("origin overwrote base item metadata")
			}
		}
		var count int64
		if err = db.Model(&storage.RewardLedgerEntry{}).Where("player_id = ?", player).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("birth granted account rewards: count=%d error=%v", count, err)
		}
	}
}
