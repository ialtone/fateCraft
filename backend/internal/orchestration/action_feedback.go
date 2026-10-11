package orchestration

import (
	"encoding/json"
	"sort"
	"strings"

	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/storage"
)

type agentActionRestriction struct {
	ActionKey string `json:"action_key"`
	ToolName  string `json:"tool_name"`
	Revision  int64  `json:"revision"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

func finalizeDomainSnapshot(snapshot agentToolSnapshot, contract *gameplay.RuntimeContract, projection map[string]any, textOnly bool) (agentToolSnapshot, error) {
	snapshot.Restrictions = map[string]agentActionRestriction{}
	snapshot.ObservedRevision = snapshot.CampaignRevision
	if modelActionAllowed(contract.Actions.Model, "domain_action") {
		capabilities, err := projectedActionCapabilities(projection)
		if err != nil {
			return snapshot, err
		}
		for _, capability := range capabilities {
			if !domainActionDeclared(contract, capability.Key) {
				continue
			}
			name := campaignToolName(capability.Key)
			if capability.Key == "" || !agentToolNamePattern.MatchString(name) {
				continue
			}
			if _, exists := snapshot.Bindings[name]; exists {
				continue
			}
			restriction := agentActionRestriction{ActionKey: capability.Key, ToolName: name, Revision: capability.Revision, Code: "action_not_provided", Message: "当前步骤没有提供此动作工具，不能将预期结果描写为已发生。"}
			if textOnly {
				restriction.Message = "本步骤只允许叙事收尾，不执行新的玩法动作。"
			} else if !capability.Available {
				restriction.Code = "action_unavailable"
				restriction.Message = "当前前置条件不足，动作不可执行。"
				if len(capability.Violations) > 0 && strings.TrimSpace(capability.Violations[0].Message) != "" {
					restriction.Message = capability.Violations[0].Message
				}
			}
			runes := []rune(restriction.Message)
			if len(runes) > 256 {
				restriction.Message = string(runes[:256]) + "（说明已截短）"
			}
			snapshot.Restrictions[name] = restriction
		}
	}
	return finalizeAgentToolSnapshot(snapshot)
}

// The notice is read-only. Schemas and executable options remain exclusively
// in native tool definitions. Recent attempted tools receive budget priority.
func actionRestrictionNotice(snapshot agentToolSnapshot, history []storage.ChatMessage) string {
	if len(snapshot.Restrictions) == 0 {
		return ""
	}
	priority := map[string]int{}
	for index, message := range history {
		if message.ToolName != "" {
			priority[message.ToolName] = index + 1
		}
	}
	names := make([]string, 0, len(snapshot.Restrictions))
	for name := range snapshot.Restrictions {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if priority[names[i]] != priority[names[j]] {
			return priority[names[i]] > priority[names[j]]
		}
		return names[i] < names[j]
	})
	entries := []agentActionRestriction{}
	notice := map[string]any{"read_only": true, "actions": entries, "omitted_count": len(names), "instruction": "这些说明不授予执行权限。只能调用本步骤实际提供的工具；未执行的目标不能写成成功。"}
	for _, name := range names {
		entries = append(entries, snapshot.Restrictions[name])
		notice["actions"], notice["omitted_count"] = entries, len(names)-len(entries)
		raw, _ := json.Marshal(notice)
		if len(raw) > 4096 {
			entries = entries[:len(entries)-1]
			notice["actions"], notice["omitted_count"] = entries, len(names)-len(entries)
			break
		}
	}
	raw, _ := json.Marshal(notice)
	return "<action_restrictions>\n" + string(raw) + "\n</action_restrictions>"
}

func unavailableToolOutcome(snapshot agentToolSnapshot, name string) agentToolOutcome {
	code, message := "unknown_tool", "该工具不在当前步骤的可用快照中。本调用未执行，不能将预期结果描写为已发生。"
	data := rejectedActionFacts()
	if restriction, exists := snapshot.Restrictions[name]; exists {
		code, message = restriction.Code, restriction.Message
		data["action_key"], data["preview_revision"] = restriction.ActionKey, restriction.Revision
		if snapshot.ObservedRevision != nil && *snapshot.ObservedRevision != restriction.Revision {
			code, message = "action_snapshot_stale", "状态已在本步骤中变化，此动作未执行；旧预览原因不代表当前条件，请依据下一步骤的最新状态。"
			data["revision"] = *snapshot.ObservedRevision
		}
	}
	return agentToolOutcome{Status: storage.ToolInvocationRejected, Code: code, Result: storage.JSONMap{"ok": false, "code": code, "message": message, "data": data}}
}

func rejectedActionFacts() map[string]any {
	return map[string]any{"execution": "rejected", "instruction": "本调用没有提交，不代表本回合之前已提交的其他动作被撤销。当前身份、资源和结局以最新可信投影为准。"}
}

func domainActionDeclared(contract *gameplay.RuntimeContract, key string) bool {
	keys := contract.Actions.Domain
	if contract.Mode == gameplay.RuntimeModeCampaign {
		if contract.Campaign == nil {
			return false
		}
		keys = contract.Campaign.ActionAllowlist
	}
	for _, declared := range keys {
		if declared == key {
			return true
		}
	}
	return false
}

func outcomeRevision(outcome agentToolOutcome) *int64 {
	if outcome.Status != storage.ToolInvocationCommitted {
		return nil
	}
	raw, err := json.Marshal(outcome.Result["data"])
	if err != nil {
		return nil
	}
	var data struct {
		Revision *int64 `json:"revision"`
	}
	if json.Unmarshal(raw, &data) != nil {
		return nil
	}
	return data.Revision
}
