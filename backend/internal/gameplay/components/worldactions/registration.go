package worldactions

import (
	"context"
	"errors"
	"fmt"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/components/factionrules"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldcontract"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/storage"
)

type EntryCheck func(campaign.ActionExecution, storage.JSONMap) ([]string, error)
type EntryAvailability func(*storage.CampaignDomainState, storage.JSONMap) bool
type NPCInitializer func(storage.GameplayDefinition, *storage.CampaignNPC)

// Register installs the shared world operations for an exact module version.
// Modules with additional rules compose these handlers in their own adapter.
func Register(service *campaign.Service, engine contract.EngineRef) {
	for _, action := range Actions() {
		service.RegisterAction(engine, action)
	}
}

func Actions() []campaign.RegisteredAction {
	type spec struct {
		key, description string
		actors           []string
		handler          campaign.ActionHandlerFunc
	}
	actors := []string{"player", "model"}
	modelActors := []string{"model", "system"}
	specs := []spec{
		{worldcontract.ActionGainItem, "按定义授予可堆叠物品", modelActors, GainItem},
		{worldcontract.ActionTradeBuy, "从当前位置的市场购买有限库存商品", actors, TradeBuy},
		{worldcontract.ActionTradeSell, "向当前位置的市场出售持有的商品", actors, TradeSell},
		{worldcontract.ActionAdjustMarket, "调整市场价格系数", []string{"system"}, AdjustMarketPrice},
		{worldcontract.ActionTravel, "沿已发现的相邻连接旅行", actors, func(ctx context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
			return Travel(ctx, execution, arguments, MissingEntryPermissions)
		}},
		{worldcontract.ActionInvestigate, "调查当前位置并发现相邻地点", actors, Investigate},
		{worldcontract.ActionResolveUncertain, "判定有明确目标、真实风险及失败后果的不确定行动；普通交谈与已知信息查询不使用判定", []string{"model"}, ResolveUncertainAction},
		{worldcontract.ActionInstantiateNPC, "按版本原型和实例上限创建 NPC", modelActors, func(ctx context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
			return InstantiateNPC(ctx, execution, arguments, nil)
		}},
		{worldcontract.ActionMoveNPC, "沿地点连接移动 NPC", modelActors, MoveNPC},
		{worldcontract.ActionSetNPCStatus, "按状态机更新 NPC 状态", modelActors, SetNPCStatus},
		{worldcontract.ActionRecordInteraction, "记录当前位置 NPC 的互动", modelActors, RecordInteraction},
		{worldcontract.ActionAdjustFaction, "调整已定义势力的声望与贡献", modelActors, AdjustFaction},
		{worldcontract.ActionAcceptQuest, "接受当前位置已提供的任务", actors, AcceptQuest},
		{worldcontract.ActionSubmitQuest, "核验任务目标、消耗投入并发放版本奖励", actors, SubmitQuest},
	}
	result := make([]campaign.RegisteredAction, 0, len(specs))
	for _, entry := range specs {
		handler := entry.handler
		result = append(result, campaign.RegisteredAction{
			Definition: campaign.ActionDefinition{Key: entry.key, Description: entry.description, Actors: entry.actors, ArgumentSchema: ArgumentSchema(entry.key)},
			Capability: CapabilityProvider{},
			Handler: campaign.ActionHandlerFunc(func(ctx context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
				// This world module chooses its own neutral D100 rule. It must
				// never inherit a global cultivation attribute configuration.
				execution.Roll = nil
				outcome, err := handler(ctx, execution, arguments)
				if err != nil || outcome.Status != nil {
					return outcome, err
				}
				if err := ProcessDueScheduledEvents(execution, &outcome); err != nil {
					return storage.CampaignActionOutcome{}, err
				}
				return outcome, nil
			}),
		})
	}
	return result
}

func MissingEntryPermissions(execution campaign.ActionExecution, content storage.JSONMap) ([]string, error) {
	requirements, ok := factionrules.EntryRequirements(content)
	if !ok {
		return nil, Invalid("地点准入条件无效")
	}
	missing := make([]string, 0)
	for _, requirement := range requirements {
		// A generic world cannot satisfy a module-specific progression gate.
		if requirement.MinRealmRank > 0 {
			return nil, Invalid("通用世界不支持境界准入条件")
		}
		standing, err := execution.Store.FactionStanding(execution.Campaign.ID, requirement.FactionKey)
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			return nil, err
		}
		if standing == nil || !factionrules.IsGranted(standing.Permissions, requirement.PermissionKey) {
			missing = append(missing, fmt.Sprintf("%s.%s", requirement.FactionKey, requirement.PermissionKey))
		}
	}
	return missing, nil
}

func EntryRequirementsMet(state *storage.CampaignDomainState, content storage.JSONMap) bool {
	requirements, ok := factionrules.EntryRequirements(content)
	if !ok {
		return false
	}
	for _, requirement := range requirements {
		if requirement.MinRealmRank > 0 {
			return false
		}
		granted := false
		for _, standing := range state.Factions {
			if standing.FactionKey == requirement.FactionKey && factionrules.IsGranted(standing.Permissions, requirement.PermissionKey) {
				granted = true
				break
			}
		}
		if !granted {
			return false
		}
	}
	return true
}
