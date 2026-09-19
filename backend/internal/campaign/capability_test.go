package campaign

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
)

type testProjectionProvider struct{}

func (testProjectionProvider) Snapshot(_ context.Context, _ ProjectionContext) (map[string]any, error) {
	return map[string]any{"topic": "snapshot"}, nil
}

type reservedSnapshotProjectionProvider struct{}

func (reservedSnapshotProjectionProvider) Snapshot(_ context.Context, _ ProjectionContext) (map[string]any, error) {
	return map[string]any{"outcome_rewards": "forged"}, nil
}

func (reservedSnapshotProjectionProvider) Context(_ context.Context, _ ProjectionContext) (ProjectionView, error) {
	return ProjectionView{}, nil
}

func TestSnapshotAddsPlatformOutcomeRewardsAndRejectsProviderOverride(t *testing.T) {
	playerID := uuid.New()
	identityProvider, ctx := testIdentityContext(playerID)
	domain := &storage.CampaignDomainState{
		Definitions: map[string]storage.GameplayDefinition{},
		OutcomeRewards: []storage.CampaignOutcomeReward{{
			Key: "world-essence", Name: "世界精粹", Unit: "份", Quantity: 3, Balance: 7,
		}},
	}
	repo := &campaignRepoStub{row: storage.Campaign{
		ID: uuid.New(), PlayerID: playerID, Status: storage.CampaignStatusCompleted,
		ChallengeVersionID: uuid.New(), ChallengeHash: "hash",
	}, domain: domain}
	contract := campaignContract()
	contract.Campaign.ProjectionHandler = "test.projection"
	service := NewService(repo, challengeStub{contract: contract}, nil, identityProvider)
	service.RegisterProjection("test.projection", testProjectionProvider{})

	snapshot, err := service.Snapshot(ctx, repo.row.ID)
	if err != nil {
		t.Fatal(err)
	}
	rewards, ok := snapshot["outcome_rewards"].([]storage.CampaignOutcomeReward)
	if !ok || len(rewards) != 1 || rewards[0].Key != "world-essence" {
		t.Fatalf("platform outcome rewards = %#v", snapshot["outcome_rewards"])
	}

	contract.Campaign.ProjectionHandler = "test.reserved"
	service.RegisterProjection("test.reserved", reservedSnapshotProjectionProvider{})
	if _, err := service.Snapshot(ctx, repo.row.ID); err == nil || !strings.Contains(err.Error(), "平台保留字段") {
		t.Fatalf("reserved outcome_rewards override error = %v", err)
	}
}

func (testProjectionProvider) Context(_ context.Context, _ ProjectionContext) (ProjectionView, error) {
	return ProjectionView{Data: map[string]any{"topic": "context"}, Priority: []string{"topic"}}, nil
}

func TestContextProjectionUsesRuntimeProviderAndDynamicCapabilities(t *testing.T) {
	playerID := uuid.New()
	identityProvider, ctx := testIdentityContext(playerID)
	repo := &campaignRepoStub{row: storage.Campaign{
		ID: uuid.New(), PlayerID: playerID, Status: storage.CampaignStatusActive,
		Revision: 7, ChallengeVersionID: uuid.New(), ChallengeHash: "hash",
	}}
	contract := campaignContract()
	contract.Campaign.ProjectionHandler = "test.projection"
	contract.Campaign.ContextPolicy = "test.context"
	contract.Campaign.ActionAllowlist = []string{"test.dynamic"}
	service := NewService(repo, challengeStub{contract: contract}, nil, identityProvider)
	service.RegisterProjection("test.projection", testProjectionProvider{})
	service.RegisterAction(RegisteredAction{
		Definition: ActionDefinition{
			Key: "test.dynamic", Description: "dynamic action", Actors: []string{"model"},
			ArgumentSchema: storage.JSONMap{
				"type": "object", "required": []string{"amount"}, "additionalProperties": false,
				"properties": map[string]any{"amount": map[string]any{"type": "integer", "minimum": int64(1), "maximum": int64(10)}},
			},
		},
		Capability: ActionCapabilityProviderFunc(func(_ context.Context, input CapabilityContext, definition ActionDefinition) (ActionCapability, error) {
			schema := cloneJSONMap(definition.ArgumentSchema)
			schema["properties"].(map[string]any)["amount"].(map[string]any)["maximum"] = int64(3)
			return ActionCapability{Available: true, ArgumentSchema: schema}, nil
		}),
		Handler: ActionHandlerFunc(func(context.Context, ActionExecution, map[string]any) (storage.CampaignActionOutcome, error) {
			return storage.CampaignActionOutcome{}, nil
		}),
	})

	projection, err := service.ContextProjection(ctx, repo.row.ID, "query")
	if err != nil {
		t.Fatal(err)
	}
	if projection.Data["topic"] != "context" {
		t.Fatalf("topic projection = %#v", projection.Data["topic"])
	}
	if _, legacy := projection.Data["available_domain_actions"]; legacy {
		t.Fatal("legacy static action directory is still present")
	}
	capabilities := projection.Data["action_capabilities"].([]ActionCapability)
	if len(capabilities) != 1 || capabilities[0].Revision != 7 || !capabilities[0].Available {
		t.Fatalf("capabilities = %#v", capabilities)
	}
	maximum := capabilities[0].ArgumentSchema["properties"].(map[string]any)["amount"].(map[string]any)["maximum"]
	if maximum != int64(3) {
		t.Fatalf("dynamic maximum = %#v", maximum)
	}
	if len(projection.Priority) != 2 || projection.Priority[0] != "recent_events" || projection.Priority[1] != "topic" {
		t.Fatalf("projection priority = %#v", projection.Priority)
	}
}

func TestArgumentSchemaRejectsUnknownOutOfRangeAndWrongEnum(t *testing.T) {
	schema := storage.JSONMap{
		"type": "object", "required": []string{"mode", "amount"}, "additionalProperties": false,
		"properties": map[string]any{
			"mode":   map[string]any{"type": "string", "enum": []any{"safe"}},
			"amount": map[string]any{"type": "integer", "minimum": int64(1), "maximum": int64(3)},
		},
	}
	for _, arguments := range []map[string]any{
		{"mode": "unsafe", "amount": float64(1)},
		{"mode": "safe", "amount": float64(4)},
		{"mode": "safe", "amount": float64(1), "extra": true},
	} {
		if err := validateActionArguments(schema, arguments); !errors.Is(err, ErrInvalidAction) {
			t.Fatalf("arguments %#v error = %v", arguments, err)
		}
	}
	if err := validateActionArguments(schema, map[string]any{"mode": "safe", "amount": float64(3)}); err != nil {
		t.Fatalf("valid arguments rejected: %v", err)
	}
}

func TestPublicActionViolationRequiresExplicitVisibility(t *testing.T) {
	publicErr := RejectAction(ActionViolation{Code: "resource.insufficient", Message: "资源不足", Public: true, Details: storage.JSONMap{"secret": "hidden"}})
	violation, ok := PublicActionViolation(publicErr)
	if !ok || violation.Message != "资源不足" || violation.Details != nil {
		t.Fatalf("public violation = %#v, %v", violation, ok)
	}
	privateErr := RejectAction(ActionViolation{Code: "internal.failure", Message: "secret", Public: false})
	if _, ok := PublicActionViolation(privateErr); ok {
		t.Fatal("private violation was exposed")
	}
}

func TestDynamicOptionCombinationAndLimitsAreEnforced(t *testing.T) {
	capability := ActionCapability{
		Available: true,
		ArgumentSchema: storage.JSONMap{
			"type": "object", "required": []string{"target", "amount"}, "additionalProperties": false,
			"properties": map[string]any{
				"target": map[string]any{"type": "string", "enum": []any{"a", "b"}},
				"amount": map[string]any{"type": "integer", "minimum": int64(1), "maximum": int64(10)},
			},
		},
		Options: []storage.JSONMap{
			{"target": "a", "limits": storage.JSONMap{"amount": storage.JSONMap{"minimum": int64(1), "maximum": int64(3)}}},
			{"target": "b", "limits": storage.JSONMap{"amount": storage.JSONMap{"minimum": int64(1), "maximum": int64(8)}}},
		},
	}
	if err := validateCapabilityArguments(capability, map[string]any{"target": "a", "amount": float64(3)}); err != nil {
		t.Fatalf("valid option rejected: %v", err)
	}
	if err := validateCapabilityArguments(capability, map[string]any{"target": "a", "amount": float64(4)}); !errors.Is(err, ErrActionRejected) {
		t.Fatalf("option limit error = %v", err)
	}
}

func TestDynamicOptionsRejectSelectorsBelongingToAnotherCandidate(t *testing.T) {
	capability := ActionCapability{
		Available: true,
		ArgumentSchema: storage.JSONMap{
			"type": "object", "required": []string{"kind"}, "additionalProperties": false,
			"properties": map[string]any{
				"kind":             map[string]any{"type": "string", "enum": []any{"item", "equipment"}},
				"item_instance_id": map[string]any{"type": "string", "minLength": int64(1), "maxLength": int64(160)},
			},
		},
		Options: []storage.JSONMap{
			{"kind": "item"},
			{"kind": "equipment", "item_instance_id": "instance-a"},
		},
	}
	arguments := map[string]any{"kind": "item", "item_instance_id": "instance-a"}
	if err := validateCapabilityArguments(capability, arguments); !errors.Is(err, ErrActionRejected) {
		t.Fatalf("cross-candidate selector error = %v", err)
	}
}

func TestProjectedCapabilitySchemaCanOnlyNarrowDefinition(t *testing.T) {
	definition := ActionDefinition{
		Key: "test.narrowing", Description: "test", Actors: []string{"model"},
		ArgumentSchema: storage.JSONMap{
			"type": "object", "required": []string{"amount"}, "additionalProperties": false,
			"properties": map[string]any{
				"amount": map[string]any{"type": "integer", "minimum": int64(1), "maximum": int64(10)},
			},
		},
	}
	narrowed := cloneJSONMap(definition.ArgumentSchema)
	narrowed["properties"].(map[string]any)["amount"].(map[string]any)["maximum"] = int64(3)
	if _, err := validateProjectedCapability(
		ActionCapability{Available: true, ArgumentSchema: narrowed}, definition, 1, CapabilityBudget{},
	); err != nil {
		t.Fatalf("narrowed schema rejected: %v", err)
	}

	broadened := cloneJSONMap(definition.ArgumentSchema)
	broadened["properties"].(map[string]any)["amount"].(map[string]any)["minimum"] = int64(0)
	if _, err := validateProjectedCapability(
		ActionCapability{Available: true, ArgumentSchema: broadened}, definition, 1, CapabilityBudget{},
	); err == nil {
		t.Fatal("broadened dynamic schema was accepted")
	}
}

func TestCapabilityBudgetsAndProviderFailureAreEnforced(t *testing.T) {
	definition := ActionDefinition{
		Key: "test.capability", Description: "test", Actors: []string{"model"},
		ArgumentSchema: storage.JSONMap{
			"type": "object", "required": []string{}, "properties": map[string]any{}, "additionalProperties": false,
		},
	}
	handler := ActionHandlerFunc(func(context.Context, ActionExecution, map[string]any) (storage.CampaignActionOutcome, error) {
		return storage.CampaignActionOutcome{}, nil
	})

	t.Run("options are truncated explicitly", func(t *testing.T) {
		service := NewService(nil, nil, nil, nil)
		service.RegisterAction(RegisteredAction{
			Definition: definition, Handler: handler,
			Capability: ActionCapabilityProviderFunc(func(context.Context, CapabilityContext, ActionDefinition) (ActionCapability, error) {
				options := make([]storage.JSONMap, 100)
				for index := range options {
					options[index] = storage.JSONMap{"index": index}
				}
				return ActionCapability{Available: true, Options: options}, nil
			}),
		})
		capabilities, err := service.actionCapabilities(context.Background(), storage.Campaign{}, &storage.CampaignDomainState{}, []string{definition.Key}, "model", "")
		if err != nil {
			t.Fatal(err)
		}
		if len(capabilities[0].Options) != defaultCapabilityOptionsPerAction || !capabilities[0].Truncated {
			t.Fatalf("bounded capability = %#v", capabilities[0])
		}
	})

	t.Run("provider failure stops projection", func(t *testing.T) {
		service := NewService(nil, nil, nil, nil)
		service.RegisterAction(RegisteredAction{
			Definition: definition, Handler: handler,
			Capability: ActionCapabilityProviderFunc(func(ctx context.Context, _ CapabilityContext, _ ActionDefinition) (ActionCapability, error) {
				<-ctx.Done()
				return ActionCapability{}, ctx.Err()
			}),
		})
		if _, err := service.actionCapabilities(context.Background(), storage.Campaign{}, &storage.CampaignDomainState{}, []string{definition.Key}, "model", ""); err == nil {
			t.Fatal("timed-out provider was treated as available")
		}
	})

	t.Run("total bytes are bounded", func(t *testing.T) {
		service := NewService(nil, nil, nil, nil)
		service.RegisterAction(RegisteredAction{
			Definition: definition, Handler: handler,
			Capability: ActionCapabilityProviderFunc(func(context.Context, CapabilityContext, ActionDefinition) (ActionCapability, error) {
				return ActionCapability{Available: true, Metadata: storage.JSONMap{"large": strings.Repeat("x", defaultCapabilityTotalBytes)}}, nil
			}),
		})
		if _, err := service.actionCapabilities(context.Background(), storage.Campaign{}, &storage.CampaignDomainState{}, []string{definition.Key}, "model", ""); err == nil {
			t.Fatal("oversized capability projection was accepted")
		}
	})
}
