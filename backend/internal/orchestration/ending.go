package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/campaign"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/realtime"
	"github.com/ialtone/fatecraft/internal/state"
	"github.com/ialtone/fatecraft/internal/storage"
	"reflect"
	"slices"
)

func endingCapabilities(projection map[string]any) ([]gameplay.EndingCapability, error) {
	raw, err := json.Marshal(projection["ending_capabilities"])
	if err != nil {
		return nil, err
	}
	if len(raw) > 16*1024 {
		return nil, fmt.Errorf("终局预览超过公开信息预算")
	}
	var options []gameplay.EndingCapability
	if err := json.Unmarshal(raw, &options); err != nil {
		return nil, err
	}
	return options, nil
}

func addEndingTool(snapshot *agentToolSnapshot, projection map[string]any) error {
	options, err := endingCapabilities(projection)
	if err != nil {
		return err
	}
	if len(options) == 0 {
		return nil
	}
	keys := []string{}
	for _, option := range options {
		if !slices.Contains(keys, option.Key) {
			keys = append(keys, option.Key)
		}
	}
	snapshot.Bindings["fc_propose_ending"] = agentToolBinding{Kind: agentToolEnding, ExecutionMode: agentToolExecutionSerial, EndingOptions: options, Definition: llm.ToolDefinition{Name: "fc_propose_ending", Description: "玩家提出结束、撤离、放弃或确认领域结局时，提出一个由玩家界面确认的操作。只生成待确认入口，不执行、不结算、不表示本局已结束。条件不足也可说明原因；不要用口头反复确认代替界面操作。", Parameters: agentClosedObject(map[string]any{"action_key": map[string]any{"type": "string", "enum": keys}, "arguments_json": map[string]any{"type": "string", "maxLength": 4096, "description": "所选公开方案的参数 JSON 对象；零参数动作用 {}"}})}}
	return nil
}

func endingNotice(snapshot agentToolSnapshot) string {
	options := snapshot.Bindings["fc_propose_ending"].EndingOptions
	if len(options) == 0 {
		return ""
	}
	raw, _ := json.Marshal(options)
	return "<ending_options>\n" + string(raw) + "\n这是只读的玩家终局目录，不授予模型执行权。玩家提出结束意愿（包括文字确认）时调用 fc_propose_ending 显示界面确认，不再口头循环确认。条件不足就说明原因，可提出模块的主动退出作为替代；没有可信终态时不能宣称死亡、通关或此世终。\n</ending_options>"
}

func (g *Generator) proposeEnding(ctx context.Context, conv *storage.Conversation, binding agentToolBinding, args map[string]any, callID string, tx *agentToolTx) agentToolOutcome {
	reject := func(err error) agentToolOutcome {
		return agentToolOutcome{Status: storage.ToolInvocationRejected, Code: "ending_rejected", Result: storage.JSONMap{"ok": false, "code": "ending_rejected", "message": err.Error(), "data": rejectedActionFacts()}}
	}
	if g.challenge == nil || (conv.CampaignID != nil && g.campaign == nil) {
		return reject(fmt.Errorf("终局提议服务未就绪"))
	}
	if tx == nil || binding.ModuleContent == nil {
		return reject(fmt.Errorf("终局提议缺少可信快照"))
	}
	if len(args) != 2 {
		return reject(fmt.Errorf("终局提议参数无效"))
	}
	key, _ := args["action_key"].(string)
	encoded, ok := args["arguments_json"].(string)
	if !ok || len(encoded) > 4096 {
		return reject(fmt.Errorf("终局参数必须是有界 JSON 对象"))
	}
	var arguments map[string]any
	if err := json.Unmarshal([]byte(encoded), &arguments); err != nil || arguments == nil {
		return reject(fmt.Errorf("终局参数必须是 JSON 对象"))
	}
	var projection map[string]any
	var err error
	if conv.CampaignID != nil {
		projection, err = g.campaign.PlayerModuleProjectionInTx(ctx, tx.challenges, conv, binding.ModuleContent.Runtime)
	} else {
		var trusted map[string]any
		trusted, err = state.GetInTx(ctx, tx.states, conv.ID, binding.ModuleContent.Runtime.StateSchema)
		if err == nil {
			projection, err = g.challenge.ModuleProjection(*binding.ModuleContent, trusted, "player")
		}
	}
	if err != nil {
		return reject(err)
	}
	options, err := endingCapabilities(projection)
	if err != nil {
		return reject(err)
	}
	for _, option := range options {
		if option.Key == key {
			if err := campaign.ValidateEndingArguments(option.ArgumentSchema, arguments); err != nil {
				return reject(err)
			}
			if option.Available && len(option.Plans) > 0 {
				wanted, _ := json.Marshal(arguments)
				matched := false
				for _, plan := range option.Plans {
					allowed, _ := json.Marshal(plan.Arguments)
					if reflect.DeepEqual(wanted, allowed) {
						matched = true
						option.Metadata = map[string]any{"label": option.Label(), "minutes": plan.Minutes, "costs": plan.Costs, "risk": plan.Risk}
						break
					}
				}
				if !matched {
					return reject(fmt.Errorf("终局参数不匹配当前公开方案"))
				}
			}
			proposal := storage.EndingProposal{ID: uuid.NewSHA1(conv.ID, []byte(attemptToolCallID(ctx, callID))).String(), ConversationID: conv.ID, BranchEpoch: conv.BranchEpoch, Revision: option.Revision, Key: key, Arguments: arguments, Label: option.Label(), Available: option.Available, Metadata: option.Metadata}
			if !option.Available {
				proposal.Reason = option.Reason()
			}
			return agentToolOutcome{Status: storage.ToolInvocationCommitted, Code: "ending_proposed", Result: storage.JSONMap{"ok": true, "code": "ending_proposed", "message": "仅提出待确认操作，本局尚未结束；玩家需在界面确认后由服务端执行。", "data": proposal}, Events: []realtime.Event{{Type: realtime.TypeEndingProposed, ConversationID: conv.ID.String()}}}
		}
	}
	return reject(fmt.Errorf("此操作不是当前玩法的玩家终局动作"))
}
