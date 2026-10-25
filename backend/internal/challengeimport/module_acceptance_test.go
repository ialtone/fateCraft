package challengeimport

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/kernel"
	"github.com/ialtone/fatecraft/internal/storage"
)

func testModuleContent(t *testing.T, doc challenge.VersionDocument) contract.Content {
	t.Helper()
	runtime, err := storage.DecodeGameplayRuntime(doc.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	c := contract.Content{Runtime: runtime}
	for _, d := range doc.Definitions {
		definition := contract.Definition{Kind: d.Kind, Key: d.Key, Name: d.Name, Content: d.Content, Tags: d.Tags, Visibility: d.Visibility, SortOrder: d.SortOrder}
		if contract.IsPlatformDefinitionKind(d.Kind) {
			c.PlatformDefinitions = append(c.PlatformDefinitions, definition)
			continue
		}
		c.Definitions = append(c.Definitions, definition)
	}
	for _, o := range doc.Outcomes {
		c.Outcomes = append(c.Outcomes, contract.Outcome{Key: o.OutcomeKey, Enabled: o.Enabled, Category: o.Category, RewardPoints: o.RewardPoints})
	}
	return c
}

func TestEightCategoryContentAndRuntimeAcceptance(t *testing.T) {
	wantCategories := []string{"生存探索", "经营建设", "潜入逃脱", "关系叙事", "古墓探险", "地牢远征", "修仙飞升", "宗门经营"}
	for i, c := range contract.Categories() {
		if i >= len(wantCategories) || c.Name != wantCategories[i] {
			t.Fatalf("category order changed: %v", contract.Categories())
		}
	}
	if len(contract.Categories()) != len(wantCategories) {
		t.Fatal("unexpected category count")
	}
	registry := defaultRegistry()
	for _, entry := range []struct{ slug, category, stop string }{
		{"island-rescue", "生存探索", "survival.abandon"},
		{"mountain-inn", "经营建设", "management.close"},
		{"night-blockade", "潜入逃脱", "stealth.abandon"},
		{"old-town-promise", "关系叙事", "relationship.leave"},
		{"lost-chambers", "古墓探险", "tomb.abandon"},
		{"underground-watchtower", "地牢远征", "dungeon.abandon"},
		{"mortal-path", "修仙飞升", ""},
		{"reincarnation-trials", "修仙飞升", ""},
		{"qinglan-sect", "宗门经营", "sect.dissolve"},
	} {
		t.Run(entry.slug, func(t *testing.T) {
			loaded, err := loadDirectory(filepath.Join("..", "..", "data", "preset_packs", entry.slug), time.Unix(0, 0), registry)
			if err != nil {
				t.Fatal(err)
			}
			// VersionDocument is the administrator's export/edit/import payload.
			raw, err := json.Marshal(loaded.document)
			if err != nil {
				t.Fatal(err)
			}
			var copy challenge.VersionDocument
			if err := json.Unmarshal(raw, &copy); err != nil {
				t.Fatal(err)
			}
			if err := challenge.ValidateDocument(&copy, registry); err != nil {
				t.Fatal(err)
			}
			hash, err := challenge.ContentHashDocument(&copy)
			if err != nil || hash != loaded.hash {
				t.Fatalf("export/import changed content hash: %v", err)
			}
			content := testModuleContent(t, copy)
			descriptor, err := registry.Module(content.Runtime.Engine)
			if err != nil || descriptor.CategoryName != entry.category {
				t.Fatalf("module category: %+v %v", descriptor, err)
			}
			if entry.stop == "" {
				return
			} // cultivation deep rules belong to phase two
			for _, outcome := range copy.Outcomes {
				if outcome.Category != "success" && outcome.RewardPoints != 0 {
					t.Fatal("failure and neutral endings must not pay account points")
				}
			}
			// Publication and execution must not require an installed cultivation module.
			isolated := contract.NewCoreRegistry()
			isolated.RegisterModule(descriptor)
			if err := challenge.ValidateDocument(&copy, isolated); err != nil {
				t.Fatal(err)
			}
			module := descriptor.Runtime
			initial, err := kernel.Initialize(module, content, contract.InitialInput{PersonaKey: loaded.manifest.DefaultConfig.RecommendedPersona, Random: kernel.NewRandom(37, "acceptance")})
			if err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(initial)
			for _, audience := range []string{contract.AudiencePlayer, contract.AudienceModel, contract.AudienceInquiry} {
				if _, err := kernel.Project(module, content, initial, audience); err != nil {
					t.Fatal(err)
				}
			}
			for _, actor := range []string{"player", "model"} {
				if _, err := kernel.Preview(module, content, initial, actor); err != nil {
					t.Fatal(err)
				}
			}
			request := contract.RuntimeRequest{ActionKey: entry.stop, Actor: "model", Arguments: map[string]any{}, Random: kernel.NewRandom(1, "denied")}
			if _, err := kernel.Execute(module, content, initial, request); err == nil {
				t.Fatal("model finalized a player decision")
			}
			request.Actor, request.Random = "player", kernel.NewRandom(1, "stop")
			transition, err := kernel.Execute(module, content, initial, request)
			if err != nil {
				t.Fatal(err)
			}
			if transition.State.Status != contract.RunFailed || transition.State.OutcomeKey == "" || transition.State.Revision != initial.Revision+1 {
				t.Fatalf("invalid terminal transition: %+v", transition.State)
			}
			saved, _ := json.Marshal(transition.State)
			var restored contract.RunState
			if err := json.Unmarshal(saved, &restored); err != nil {
				t.Fatal(err)
			}
			if err := kernel.ValidateState(module, content, restored); err != nil {
				t.Fatal(err)
			}
			if _, err := kernel.Execute(module, content, restored, request); err == nil {
				t.Fatal("terminal state accepted another effect")
			}
			after, _ := json.Marshal(initial)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("preview or execution mutated caller checkpoint")
			}
		})
	}
}

type personaProbe struct{ contract.RuntimeModule }

func (m personaProbe) Initialize(c contract.Content, in contract.InitialInput) (contract.RunState, error) {
	if in.PersonaKey == "unavailable" {
		return contract.RunState{}, fmt.Errorf("probe birth unavailable")
	}
	return m.RuntimeModule.Initialize(c, in)
}

func TestPublicationExercisesEverySelectablePersona(t *testing.T) {
	loaded, err := loadDirectory(filepath.Join("..", "..", "data", "preset_packs", "island-rescue"), time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	c := testModuleContent(t, loaded.document)
	descriptor, _ := defaultRegistry().Module(c.Runtime.Engine)
	descriptor.Runtime = personaProbe{descriptor.Runtime}
	registry := contract.NewCoreRegistry()
	registry.RegisterModule(descriptor)
	loaded.document.Assets = append(loaded.document.Assets, storage.ChallengeAsset{Kind: storage.ChallengeAssetPersona, Slug: "unavailable", Name: "Unavailable", Content: storage.JSONDoc(`{"id":"unavailable"}`)})
	manifest := loaded.manifest
	manifest.DefaultConfig.AllowedPersonas = []string{manifest.DefaultConfig.RecommendedPersona}
	raw, _ := json.Marshal(manifest)
	loaded.document.Version.Manifest = raw
	if err := challenge.ValidateDocument(&loaded.document, registry); err != nil {
		t.Fatal(err)
	}
	// An empty allowlist exposes every persona, including non-default choices.
	manifest.DefaultConfig.AllowedPersonas = nil
	raw, _ = json.Marshal(manifest)
	loaded.document.Version.Manifest = raw
	if err := challenge.ValidateDocument(&loaded.document, registry); err == nil || !strings.Contains(err.Error(), "出身 unavailable") {
		t.Fatalf("bad secondary persona not rejected: %v", err)
	}
}

func TestPublicationRejectsModuleDisplayMismatch(t *testing.T) {
	for _, kind := range []string{"missing-field", "numeric-widget", "item-field", "unknown-version", "unknown-renderer"} {
		t.Run(kind, func(t *testing.T) {
			loaded, err := loadDirectory(filepath.Join("..", "..", "data", "preset_packs", "mountain-inn"), time.Unix(0, 0))
			if err != nil {
				t.Fatal(err)
			}
			c := testModuleContent(t, loaded.document)
			switch kind {
			case "missing-field":
				c.Runtime.Presentation.CampaignView.Header.TitlePath = "gameplay.missing"
			case "numeric-widget":
				c.Runtime.Presentation.CampaignView.Sections[0].Fields[0].Widget = "number"
			case "item-field":
				c.Runtime.Presentation.CampaignView.Sections[2].Fields[2].ItemFields[0].Path = "missing"
			case "unknown-version":
				c.Runtime.Engine.Version++
			case "unknown-renderer":
				c.Runtime.Presentation.CampaignView.Renderer = "not-installed"
			}
			loaded.document.Runtime, err = storage.EncodeGameplayRuntime(*c.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			if err := challenge.ValidateDocument(&loaded.document, defaultRegistry()); err == nil {
				t.Fatal("invalid publication accepted")
			}
		})
	}
}

func TestPublicationRequiresCompleteModuleActions(t *testing.T) {
	for _, slug := range []string{"island-rescue", "mountain-inn", "night-blockade", "old-town-promise", "lost-chambers", "underground-watchtower", "qinglan-sect"} {
		t.Run(slug, func(t *testing.T) {
			loaded, err := loadDirectory(filepath.Join("..", "..", "data", "preset_packs", slug), time.Unix(0, 0))
			if err != nil {
				t.Fatal(err)
			}
			content := testModuleContent(t, loaded.document)
			original := slices.Clone(content.Runtime.Actions.Domain)
			// Retain the first writer and terminal action so generic ownership
			// checks cannot mask a missing progression dependency.
			removed := original[1]
			content.Runtime.Actions.Domain = slices.Delete(slices.Clone(original), 1, 2)
			if err := defaultRegistry().ValidateRuntime(content.Runtime); err == nil || !strings.Contains(err.Error(), removed) {
				t.Fatalf("missing domain action accepted: %v", err)
			}
			if content.Runtime.Campaign != nil {
				content.Runtime.Actions.Domain = original
				content.Runtime.Campaign.ActionAllowlist = slices.DeleteFunc(content.Runtime.Campaign.ActionAllowlist, func(key string) bool { return key == removed })
				if err := defaultRegistry().ValidateRuntime(content.Runtime); err == nil || !strings.Contains(err.Error(), removed) {
					t.Fatalf("missing campaign action accepted: %v", err)
				}
			}
		})
	}
}
