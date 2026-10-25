package orchestration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/challenge"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/guidance"
	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/realtime"
	"github.com/ialtone/fatecraft/internal/roll"
	"github.com/ialtone/fatecraft/internal/state"
	"github.com/ialtone/fatecraft/internal/storage"
)

var agentToolNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,127}$`)

const (
	agentToolCatalogVersion = "fatecraft-agent-tools-v2"
	maxDomainToolsPerStep   = 16
	maxToolUpdates          = 16
)

// agentToolExecutionMode describes how a batch of calls may be handled. The
// current built-in tools are serialized, including presentation staging whose
// validity depends on preceding writes. A read-only tool may opt into parallel_read once its handler
// declares a stable, non-overlapping read set.
type agentToolExecutionMode string

const (
	agentToolExecutionSerial       agentToolExecutionMode = "serial"
	agentToolExecutionParallelRead agentToolExecutionMode = "parallel_read"
)

type agentToolKind string

const (
	agentToolStateText    agentToolKind = "state_text"
	agentToolStateNumber  agentToolKind = "state_number"
	agentToolStateBoolean agentToolKind = "state_boolean"
	agentToolStateReplace agentToolKind = "state_replace"
	agentToolStateAppend  agentToolKind = "state_append"
	agentToolStateDynamic agentToolKind = "state_dynamic"
	agentToolRoll         agentToolKind = "roll"
	agentToolCompletion   agentToolKind = "completion"
	agentToolCampaign     agentToolKind = "campaign"
	agentToolModule       agentToolKind = "module"
	agentToolGuidance     agentToolKind = "guidance"
	agentToolEnding       agentToolKind = "ending_proposal"
)

type agentToolBinding struct {
	EndingOptions    []gameplay.EndingCapability
	Definition       llm.ToolDefinition
	Kind             agentToolKind
	ActionKey        string
	CampaignRevision int64
	Mutating         bool
	ExecutionMode    agentToolExecutionMode
	ModuleContent    *gameplay.Content
}

type agentToolSnapshotEntry struct {
	Definition     llm.ToolDefinition     `json:"definition"`
	HandlerKey     string                 `json:"handler_key"`
	HandlerVersion string                 `json:"handler_version"`
	OwnershipHash  string                 `json:"ownership_hash,omitempty"`
	Mutating       bool                   `json:"mutating"`
	ExecutionMode  agentToolExecutionMode `json:"execution_mode"`
}

type agentToolSnapshot struct {
	Definitions      []llm.ToolDefinition
	Bindings         map[string]agentToolBinding
	JSON             []byte
	Hash             string
	CatalogVersion   string
	CampaignRevision *int64
	OwnershipHash    string
	Restrictions     map[string]agentActionRestriction
	ObservedRevision *int64
}

func buildAgentToolSnapshot(
	contract *gameplay.RuntimeContract,
	projection map[string]any,
	outcomeKeys []string,
	rollEnabled bool,
	textOnly bool,
) (agentToolSnapshot, error) {
	snapshot := agentToolSnapshot{
		Bindings: make(map[string]agentToolBinding), CatalogVersion: agentToolCatalogVersion,
	}
	if contract == nil {
		return snapshot, errors.New("构建工具快照缺少 Runtime")
	}
	ownership, err := gameplay.CompileOwnershipRegistry(contract)
	if err != nil {
		return snapshot, err
	}
	snapshot.OwnershipHash = ownership.Hash
	snapshot.Bindings[guidance.ToolName] = agentToolBinding{
		Definition: llm.ToolDefinition{Name: guidance.ToolName, Description: "仅在玩家缺少方向、重要决策分叉或主动索要建议时提交 2 到 4 个可点击的玩家意图；不要每回合使用。先完成本轮结算，再提交提示，随后输出普通叙事。不要在正文输出选项 JSON。", Parameters: guidance.Schema(), Strict: true},
		Kind:       agentToolGuidance, Mutating: false, ExecutionMode: agentToolExecutionSerial,
	}
	if revision, ok := exactInt64(projection["revision"]); ok {
		snapshot.CampaignRevision = &revision
	}
	if textOnly {
		return finalizeDomainSnapshot(snapshot, contract, projection, true)
	}

	if err := addEndingTool(&snapshot, projection); err != nil {
		return snapshot, err
	}
	add := func(binding agentToolBinding) error {
		name := binding.Definition.Name
		if _, exists := snapshot.Bindings[name]; exists {
			return fmt.Errorf("工具名冲突: %s", name)
		}
		snapshot.Bindings[name] = binding
		return nil
	}
	if modelActionAllowed(contract.Actions.Model, "update_state") {
		for _, binding := range stateToolBindings(contract.StateSchema, contract.Mode != gameplay.RuntimeModeCampaign) {
			if err := add(binding); err != nil {
				return snapshot, err
			}
		}
	}
	if rollEnabled && modelActionAllowed(contract.Actions.Model, "roll") && len(contract.Actions.Rolls) > 0 {
		keys := make([]string, 0, len(contract.Actions.Rolls))
		for key := range contract.Actions.Rolls {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if err := add(agentToolBinding{
			Definition: llm.ToolDefinition{
				Name: "fc_request_roll", Description: "请求服务端执行当前玩法已发布的可信判定；不要自行决定概率或结果。",
				Parameters: agentClosedObject(map[string]any{
					"key":         map[string]any{"type": "string", "enum": keys},
					"title":       map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
					"description": map[string]any{"type": "string", "minLength": 1, "maxLength": 512},
				}),
			}, Kind: agentToolRoll, Mutating: true,
		}); err != nil {
			return snapshot, err
		}
	}
	if modelActionAllowed(contract.Actions.Model, "complete_challenge") && len(outcomeKeys) > 0 {
		keys := append([]string(nil), outcomeKeys...)
		sort.Strings(keys)
		if err := add(agentToolBinding{
			Definition: llm.ToolDefinition{
				Name: "fc_complete_challenge", Description: "提交一个已发布结局供服务端复核和结算；不能自行决定奖励。",
				Parameters: agentClosedObject(map[string]any{
					"outcome_key": map[string]any{"type": "string", "enum": keys},
					"summary":     map[string]any{"type": "string", "minLength": 1, "maxLength": 1000},
				}),
			}, Kind: agentToolCompletion, Mutating: true,
		}); err != nil {
			return snapshot, err
		}
	}
	if modelActionAllowed(contract.Actions.Model, "domain_action") {
		capabilities, err := projectedActionCapabilities(projection)
		if err != nil {
			return snapshot, err
		}
		added := 0
		for _, capability := range capabilities {
			if !domainActionDeclared(contract, capability.Key) {
				continue
			}
			if !capability.Available || capability.Key == "" || capability.ArgumentSchema == nil {
				continue
			}
			if added >= maxDomainToolsPerStep {
				break
			}
			name := campaignToolName(capability.Key)
			description := capability.Description
			for _, ownershipField := range ownership.Fields {
				if ownershipField.WriteCapability != capability.Key {
					continue
				}
				description += fmt.Sprintf(" 写入权威字段：%s。", ownershipField.Path)
				if len(ownershipField.EventTypes) > 0 {
					description += " 事件：" + strings.Join(ownershipField.EventTypes, ",") + "。"
				}
			}
			if len(capability.Options) > 0 {
				if encoded, err := json.Marshal(capability.Options); err == nil && len(encoded) <= 4096 {
					description += " 当前可用选项：" + string(encoded)
				}
			}
			kind := agentToolCampaign
			if contract.Mode == gameplay.RuntimeModeChallenge {
				kind = agentToolModule
			}
			if err := add(agentToolBinding{
				Definition: llm.ToolDefinition{
					Name: name, Description: description,
					Parameters: map[string]any(capability.ArgumentSchema),
				},
				Kind: kind, ActionKey: capability.Key,
				CampaignRevision: capability.Revision, Mutating: true,
			}); err != nil {
				return snapshot, err
			}
			added++
		}
	}
	return finalizeDomainSnapshot(snapshot, contract, projection, false)
}

func stateToolBindings(schema gameplay.StateSchema, allowDynamic bool) []agentToolBinding {
	groups := map[agentToolKind][]string{}
	for _, field := range schema.Fields {
		if field.UpdatePolicy != gameplay.UpdateModel {
			continue
		}
		switch field.Type {
		case "string", "enum":
			groups[agentToolStateText] = append(groups[agentToolStateText], field.Path)
		case "integer", "number":
			groups[agentToolStateNumber] = append(groups[agentToolStateNumber], field.Path)
		case "boolean":
			groups[agentToolStateBoolean] = append(groups[agentToolStateBoolean], field.Path)
		case "list":
			groups[agentToolStateReplace] = append(groups[agentToolStateReplace], field.Path)
			groups[agentToolStateAppend] = append(groups[agentToolStateAppend], field.Path)
		case "object":
			if gameplay.CanReplaceObject(schema, field.Path) {
				groups[agentToolStateReplace] = append(groups[agentToolStateReplace], field.Path)
			}
			if allowDynamic && field.AllowChildren {
				groups[agentToolStateDynamic] = append(groups[agentToolStateDynamic], field.Path)
			}
		}
	}
	definitions := []struct {
		kind        agentToolKind
		name        string
		description string
		valueSchema map[string]any
	}{
		{agentToolStateText, "fc_state_set_text", "原子更新一个或多个允许模型维护的文本或枚举状态。", map[string]any{"type": "string"}},
		{agentToolStateNumber, "fc_state_set_number", "原子更新一个或多个允许模型维护的数值状态；提交更新后的绝对值。", map[string]any{"type": "number"}},
		{agentToolStateBoolean, "fc_state_set_boolean", "原子更新一个或多个允许模型维护的布尔状态。", map[string]any{"type": "boolean"}},
		{agentToolStateReplace, "fc_state_replace_json", "以 JSON 字符串整体替换一个或多个允许替换的对象或列表状态。", map[string]any{"type": "string", "maxLength": 8192}},
		{agentToolStateAppend, "fc_state_append_json", "向一个或多个允许追加的列表状态追加 JSON 值。", map[string]any{"type": "string", "maxLength": 8192}},
	}
	out := make([]agentToolBinding, 0, len(definitions)+1)
	for _, definition := range definitions {
		paths := groups[definition.kind]
		if len(paths) == 0 {
			continue
		}
		sort.Strings(paths)
		valueKey := "value"
		if definition.kind == agentToolStateReplace || definition.kind == agentToolStateAppend {
			valueKey = "value_json"
		}
		item := agentClosedObject(map[string]any{
			"path": map[string]any{"type": "string", "enum": paths}, valueKey: definition.valueSchema,
		})
		out = append(out, agentToolBinding{
			Definition: llm.ToolDefinition{
				Name: definition.name, Description: definition.description,
				Parameters: agentClosedObject(map[string]any{
					"updates": map[string]any{"type": "array", "minItems": 1, "maxItems": maxToolUpdates, "items": item},
				}),
			}, Kind: definition.kind, Mutating: true, ExecutionMode: agentToolExecutionSerial,
		})
	}
	if paths := groups[agentToolStateDynamic]; len(paths) > 0 {
		sort.Strings(paths)
		out = append(out, agentToolBinding{
			Definition: llm.ToolDefinition{
				Name: "fc_state_set_dynamic_json", Description: "原子更新允许动态子路径的对象；child_path 必须是相对父路径的稳定子路径。",
				Parameters: agentClosedObject(map[string]any{
					"updates": map[string]any{
						"type": "array", "minItems": 1, "maxItems": maxToolUpdates,
						"items": agentClosedObject(map[string]any{
							"path":       map[string]any{"type": "string", "enum": paths},
							"child_path": map[string]any{"type": "string", "minLength": 1, "maxLength": 256},
							"value_json": map[string]any{"type": "string", "maxLength": 8192},
						}),
					},
				}),
			}, Kind: agentToolStateDynamic, Mutating: true, ExecutionMode: agentToolExecutionSerial,
		})
	}
	return out
}

func finalizeAgentToolSnapshot(snapshot agentToolSnapshot) (agentToolSnapshot, error) {
	names := make([]string, 0, len(snapshot.Bindings))
	for name := range snapshot.Bindings {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]agentToolSnapshotEntry, 0, len(names))
	for _, name := range names {
		binding := snapshot.Bindings[name]
		mode := binding.ExecutionMode
		if mode == "" {
			mode = agentToolExecutionSerial
		}
		snapshot.Definitions = append(snapshot.Definitions, binding.Definition)
		entries = append(entries, agentToolSnapshotEntry{
			Definition: binding.Definition, HandlerKey: string(binding.Kind),
			HandlerVersion: agentToolCatalogVersion, OwnershipHash: snapshot.OwnershipHash, Mutating: binding.Mutating,
			ExecutionMode: mode,
		})
	}
	encoded, err := json.Marshal(entries)
	if err != nil {
		return snapshot, err
	}
	hash := sha256.Sum256(encoded)
	snapshot.JSON = encoded
	snapshot.Hash = hex.EncodeToString(hash[:])
	return snapshot, nil
}

func projectedActionCapabilities(projection map[string]any) ([]campaign.ActionCapability, error) {
	if projection == nil || projection["action_capabilities"] == nil {
		return nil, nil
	}
	if typed, ok := projection["action_capabilities"].([]campaign.ActionCapability); ok {
		return typed, nil
	}
	encoded, err := json.Marshal(projection["action_capabilities"])
	if err != nil {
		return nil, err
	}
	var capabilities []campaign.ActionCapability
	if err := json.Unmarshal(encoded, &capabilities); err != nil {
		return nil, fmt.Errorf("解析 Campaign 动作能力: %w", err)
	}
	return capabilities, nil
}

func campaignToolName(actionKey string) string {
	var out strings.Builder
	out.WriteString("fc_action_")
	for _, char := range actionKey {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9':
			out.WriteRune(char)
		default:
			out.WriteByte('_')
		}
	}
	return out.String()
}

func agentClosedObject(properties map[string]any) map[string]any {
	required := make([]string, 0, len(properties))
	for key := range properties {
		required = append(required, key)
	}
	sort.Strings(required)
	return map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": properties, "required": required,
	}
}

func decodeToolArguments(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" || len(raw) > 16*1024 {
		return nil, errors.New("工具参数为空或超过 16 KiB")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var arguments map[string]any
	if err := decoder.Decode(&arguments); err != nil || arguments == nil {
		return nil, errors.New("工具参数不是完整 JSON 对象")
	}
	var trailing any
	if decoder.Decode(&trailing) == nil {
		return nil, errors.New("工具参数包含对象外内容")
	}
	return normalizeJSONNumbers(arguments).(map[string]any), nil
}

func normalizeJSONNumbers(value any) any {
	switch typed := value.(type) {
	case json.Number:
		if integer, err := typed.Int64(); err == nil {
			return integer
		}
		if number, err := typed.Float64(); err == nil {
			return number
		}
		return typed.String()
	case map[string]any:
		for key, item := range typed {
			typed[key] = normalizeJSONNumbers(item)
		}
		return typed
	case []any:
		for index, item := range typed {
			typed[index] = normalizeJSONNumbers(item)
		}
		return typed
	default:
		return value
	}
}

func decodeStateToolPatch(kind agentToolKind, arguments map[string]any) (map[string]any, error) {
	rawUpdates, ok := arguments["updates"].([]any)
	if !ok || len(rawUpdates) == 0 || len(rawUpdates) > maxToolUpdates || len(arguments) != 1 {
		return nil, errors.New("updates 必须包含 1 到 16 项")
	}
	patch := make(map[string]any, len(rawUpdates))
	for _, raw := range rawUpdates {
		update, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("updates 项必须是对象")
		}
		path, _ := update["path"].(string)
		if path == "" || strings.TrimSpace(path) != path {
			return nil, errors.New("状态路径无效")
		}
		key := path
		var value any
		switch kind {
		case agentToolStateText, agentToolStateNumber, agentToolStateBoolean:
			if len(update) != 2 {
				return nil, errors.New("状态更新包含未知字段")
			}
			value = update["value"]
		case agentToolStateReplace, agentToolStateAppend:
			if len(update) != 2 {
				return nil, errors.New("JSON 状态更新包含未知字段")
			}
			encoded, ok := update["value_json"].(string)
			if !ok || json.Unmarshal([]byte(encoded), &value) != nil {
				return nil, errors.New("value_json 不是有效 JSON")
			}
			if kind == agentToolStateAppend {
				key += "+"
			}
		case agentToolStateDynamic:
			if len(update) != 3 {
				return nil, errors.New("动态状态更新包含未知字段")
			}
			child, _ := update["child_path"].(string)
			if !validAgentChildPath(child) {
				return nil, errors.New("child_path 无效")
			}
			encoded, ok := update["value_json"].(string)
			if !ok || json.Unmarshal([]byte(encoded), &value) != nil {
				return nil, errors.New("value_json 不是有效 JSON")
			}
			key += "." + child
		default:
			return nil, errors.New("未知状态工具")
		}
		if _, duplicate := patch[key]; duplicate {
			return nil, fmt.Errorf("状态路径重复: %s", key)
		}
		patch[key] = value
	}
	return patch, nil
}

func validAgentChildPath(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") || strings.Contains(value, "..") {
		return false
	}
	for _, segment := range strings.Split(value, ".") {
		for _, char := range segment {
			if unicode.IsLetter(char) || unicode.IsDigit(char) || char == '_' || char == '-' {
				continue
			}
			return false
		}
	}
	return true
}

type agentToolOutcome struct {
	Result   storage.JSONMap
	Status   string
	Code     string
	Mutating bool
	Fatal    bool
	Events   []realtime.Event
}

func (g *Generator) executeAgentToolInTx(
	ctx context.Context,
	conv *storage.Conversation,
	contract *gameplay.RuntimeContract,
	platform *storage.SystemSettings,
	binding agentToolBinding,
	arguments map[string]any,
	callID string,
	tx *agentToolTx,
) agentToolOutcome {
	reject := func(code string, err error) agentToolOutcome {
		return agentToolOutcome{
			Status: storage.ToolInvocationRejected, Code: code,
			Result: storage.JSONMap{"ok": false, "code": code, "message": err.Error(), "data": rejectedActionFacts()},
		}
	}
	fail := func(code string, err error) agentToolOutcome {
		return agentToolOutcome{
			Status: storage.ToolInvocationFailed, Code: code, Fatal: true,
			Result: storage.JSONMap{"ok": false, "code": code, "message": "工具执行失败", "data": map[string]any{}},
		}
	}
	success := func(code, message string, data any) agentToolOutcome {
		return agentToolOutcome{
			Status: storage.ToolInvocationCommitted, Code: code, Mutating: binding.Mutating,
			Result: storage.JSONMap{"ok": true, "code": code, "message": message, "data": data},
		}
	}

	switch binding.Kind {
	case agentToolEnding:
		return g.proposeEnding(ctx, conv, binding, arguments, callID, tx)
	case agentToolModule:
		return g.executeModuleToolInTx(ctx, conv, binding, arguments, tx)
	case agentToolGuidance:
		raw, err := json.Marshal(arguments)
		if err != nil {
			return reject("invalid_guidance", err)
		}
		proposal, err := guidance.Decode(raw)
		if err != nil {
			return reject("invalid_guidance", err)
		}
		return success("guidance_staged", "行动提示已暂存，将在本轮叙事成功后展示；现在输出普通叙事，无需重复调用或列出选项。", proposal)
	case agentToolStateText, agentToolStateNumber, agentToolStateBoolean, agentToolStateReplace, agentToolStateAppend, agentToolStateDynamic:
		patch, err := decodeStateToolPatch(binding.Kind, arguments)
		if err != nil {
			return reject("invalid_arguments", err)
		}
		if contract.Mode == gameplay.RuntimeModeCampaign {
			ownership, ownershipErr := gameplay.CompileOwnershipRegistry(contract)
			if ownershipErr != nil {
				return fail("ownership_invalid", ownershipErr)
			}
			for path := range patch {
				resolvedPath := strings.TrimSuffix(path, "+")
				owner, ok := ownership.OwnerForPath(resolvedPath)
				if !ok {
					return reject("state_path_forbidden", fmt.Errorf("状态路径 %s 未在当前 Runtime 的 conversation schema 注册，不能通过状态工具创建", resolvedPath))
				}
				if owner != gameplay.OwnerConversation {
					return reject("state_path_forbidden", fmt.Errorf("状态路径 %s 属于 Campaign 权威域，必须使用对应的 fc_action_* 工具", resolvedPath))
				}
			}
		}
		if err := state.ValidatePatch(contract.StateSchema, storage.SourceAI, patch); err != nil {
			return reject("state_rejected", err)
		}
		if tx == nil {
			return fail("state_commit_failed", errors.New("状态工具缺少事务"))
		}
		_, skipped, err := state.ApplyInTx(ctx, tx.states, conv.ID, contract.StateSchema, patch, storage.SourceAI, nil)
		if err != nil {
			var validation *state.PatchValidationError
			if errors.As(err, &validation) {
				return reject("state_rejected", err)
			}
			return fail("state_commit_failed", err)
		}
		if len(skipped) > 0 {
			return reject("state_rejected", fmt.Errorf("状态字段被拒绝: %s", strings.Join(skipped, ", ")))
		}
		return success("state_committed", "状态已更新", map[string]any{"paths": sortedMapKeys(patch)})

	case agentToolCampaign:
		if conv.CampaignID == nil || g.campaign == nil {
			return reject("campaign_required", campaign.ErrCampaignModeRequired)
		}
		if tx == nil {
			return fail("action_commit_failed", errors.New("领域动作缺少事务"))
		}
		input := campaign.ActionInput{
			ActionID: attemptToolCallID(ctx, callID),
			Key:      binding.ActionKey, ExpectedRevision: binding.CampaignRevision, Arguments: arguments,
		}
		// 判定历史绑定到事务连接；玩法合约与平台设置沿用回合开始时固定的读数。
		var txRoll *roll.Service
		if g.roll != nil {
			txRoll = g.roll.WithHistory(tx.rollHist)
		}
		result, err := g.campaign.ExecuteModelActionWithCommitter(ctx, tx.campaigns, *conv.CampaignID, input, nil,
			&campaign.TrustedActionContext{Contract: contract, Platform: platform, Roll: txRoll})
		if err != nil {
			if errors.Is(err, campaign.ErrActionRejected) || errors.Is(err, campaign.ErrInvalidAction) ||
				errors.Is(err, storage.ErrCampaignRevisionConflict) || errors.Is(err, storage.ErrCampaignNotActive) {
				return reject("action_rejected", publicToolError(err))
			}
			return fail("action_commit_failed", err)
		}
		domainResult := storage.JSONMap{}
		for key, value := range result.Result {
			if key != "narrative_feedback" {
				domainResult[key] = value
			}
		}
		outcome := success("action_committed", "领域动作已执行", map[string]any{
			"action_key": binding.ActionKey, "revision": result.Campaign.Revision,
			"result": domainResult, "events": result.Events,
			"narrative_feedback": result.Result["narrative_feedback"],
		})
		if result.RollMessage != nil {
			outcome.Events = append(outcome.Events, realtime.Event{
				Type: realtime.TypeMessageCreated, ConversationID: conv.ID.String(),
				MessageID: result.RollMessage.ID.String(), Message: result.RollMessage,
			})
		}
		outcome.Events = append(outcome.Events, realtime.Event{
			Type: realtime.TypeCampaignUpdated, ConversationID: conv.ID.String(),
			Campaign: result.Campaign, DomainEvents: result.Events,
		})
		if result.Campaign.Status != storage.CampaignStatusActive {
			outcome.Result["data"].(map[string]any)["completion"] = storage.CompletionFromResult(result.Result)
			outcome.Code = "module_settled"
			outcome.Result["code"] = "module_settled"
			outcome.Events = append(outcome.Events, realtime.Event{Type: realtime.TypeChallengeCompleted, ConversationID: conv.ID.String(), Completion: storage.CompletionFromResult(result.Result)})
		}
		return outcome

	case agentToolRoll:
		key, _ := arguments["key"].(string)
		title, _ := arguments["title"].(string)
		description, _ := arguments["description"].(string)
		rule, ok := contract.Actions.Rolls[key]
		if !ok || strings.TrimSpace(title) == "" || strings.TrimSpace(description) == "" {
			return reject("roll_rejected", errors.New("判定参数无效"))
		}
		if g.roll == nil || g.rollCommitter == nil {
			return fail("roll_unavailable", errors.New("判定服务未加载"))
		}
		// 平台设置由协调器在回合开始固定（见 runAgentGeneration），判定开关
		// 与难度模式在整个 attempt 内和工具目录保持一致。
		if platform == nil {
			platform = storage.DefaultSystemSettings()
		}
		if !platform.RollSystemEnabled {
			return reject("roll_disabled", errors.New("平台判定系统未启用"))
		}
		if tx == nil {
			return fail("roll_unavailable", errors.New("判定工具缺少事务"))
		}
		// 状态读取、判定历史与结果提交全部走事务连接。
		trusted, err := state.GetInTx(ctx, tx.states, conv.ID, contract.StateSchema)
		if err != nil {
			return fail("roll_state_failed", err)
		}
		if err := gameplay.CheckRollRequires(rule, trusted); err != nil {
			return reject("requirement_not_met", err)
		}
		request := &roll.Request{
			Key: key, Title: strings.TrimSpace(title), Description: strings.TrimSpace(description),
			Category: rule.Category, BaseSuccessRate: rule.BaseSuccessRate, Sides: rule.Sides, RewardValue: rule.RewardValue,
		}
		detailed := g.roll.WithHistory(tx.rollHist).CalculateResult(ctx, request, trusted, conv.ID, platform.RollDifficultyMode)
		rolled, err := roll.ExecuteRoll(detailed, request.Sides)
		if err != nil {
			return fail("roll_random_failed", err)
		}
		effect := rule.Failure
		if rolled.Success {
			effect = rule.Success
		}
		formatted := roll.FormatResult(title, description, rolled)
		metadata := map[string]any{
			"roll_key":        key,
			"roll_title":      title,
			"roll_category":   rule.Category,
			"roll_outcome":    rolled,
			"conditions":      gameplay.BuildRollConditionSnapshots(contract, rule, trusted),
			"details_version": gameplay.RollDetailsVersion,
		}
		rollInput := RollCommitInput{
			ConversationID: conv.ID, Schema: contract.StateSchema, ExpectedState: trusted,
			Record: true, RollKey: key, Title: title, Category: rule.Category, Success: rolled.Success,
			Content: formatted, Markdown: formatted,
			Metadata: metadata, EffectPatch: effect,
		}
		commit, err := g.rollCommitter.CommitInCurrentTx(ctx, tx.db, rollInput)
		if err != nil {
			var validation *state.PatchValidationError
			if errors.As(err, &validation) {
				return reject("roll_effect_rejected", err)
			}
			return fail("roll_commit_failed", err)
		}
		outcome := success("roll_committed", "判定已执行", map[string]any{"key": key, "outcome": rolled})
		if commit != nil && commit.Display != nil {
			outcome.Events = append(outcome.Events, realtime.Event{Type: realtime.TypeMessageCreated, ConversationID: conv.ID.String(), MessageID: commit.Display.ID.String(), Message: commit.Display})
		}
		return outcome

	case agentToolCompletion:
		handler := g.completion
		if handler == nil {
			return fail("settlement_unavailable", errors.New("结算服务未加载"))
		}
		if tx == nil {
			return fail("settlement_failed", errors.New("结算工具缺少事务"))
		}
		completion, err := handler.PrepareInTx(ctx, tx, conv.ID, contract, arguments)
		if err != nil {
			return reject("settlement_rejected", err)
		}
		result, err := completion.handler.challenge.SettleWithRepository(ctx, tx.challenges, completion.input)
		if err != nil {
			if errors.Is(err, challenge.ErrSettlementDenied) || errors.Is(err, storage.ErrAlreadySettled) {
				return reject("settlement_rejected", err)
			}
			return fail("settlement_failed", err)
		}
		view := storage.NewCompletion(*conv, 0, "challenge:"+result.Run.ID.String(), storage.ChallengeOutcomeRule{OutcomeKey: result.Outcome.Key, Category: result.Outcome.Category, Title: result.Outcome.Title, Tone: result.Outcome.Tone})
		view.Summary, view.RewardsDisabledReason = result.Run.Summary, result.RewardsDisabledReason
		for _, reward := range result.Rewards {
			view.Rewards = append(view.Rewards, storage.CompletionReward{RewardKey: reward.RewardKeySnapshot, Name: reward.NameSnapshot, Unit: reward.UnitSnapshot, Icon: reward.IconSnapshot, Quantity: reward.Quantity, BalanceAfter: reward.BalanceAfter})
		}
		// 模型工具直接提交终局：阶段事实可能来自更早的动作，必须在本事务
		// 内附加到可恢复回执，不能只让 HTTP 动作路径装配。
		view.RewardPoints = storage.TerminalRewardPoints(view.Rewards)
		if err := tx.challenges.FillStageRewards(ctx, view); err != nil {
			return fail("settlement_failed", err)
		}
		outcomePayload := map[string]any{"completion": view, "outcome_key": result.Outcome.Key, "rewards": result.Rewards}
		outcomeMap := map[string]any{"key": result.Outcome.Key, "category": result.Outcome.Category, "title": result.Outcome.Title, "summary": result.Run.Summary, "tone": result.Outcome.Tone}
		if result.RewardsDisabledReason != "" {
			outcomePayload["rewards_disabled_reason"] = result.RewardsDisabledReason
			outcomeMap["rewards_disabled_reason"] = result.RewardsDisabledReason
		}
		outcome := success("settlement_committed", "玩法已结算", outcomePayload)
		outcome.Events = append(outcome.Events, realtime.Event{
			Type: realtime.TypeChallengeCompleted, ConversationID: conv.ID.String(), RunID: result.Run.ID.String(), Completion: view,
			Outcome:      outcomeMap,
			RewardGrants: result.Rewards, NextActions: []string{"replay", "back_to_challenges"},
		})
		return outcome
	}
	return reject("unknown_tool", errors.New("工具未注册"))
}

func publicToolError(err error) error {
	if violation, ok := campaign.PublicActionViolation(err); ok && strings.TrimSpace(violation.Message) != "" {
		return errors.New(violation.Message)
	}
	if errors.Is(err, storage.ErrCampaignRevisionConflict) {
		return errors.New("存档状态已经变化，请根据最新状态重新选择动作")
	}
	return err
}

func sortedMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, strings.TrimSuffix(key, "+"))
	}
	sort.Strings(keys)
	return keys
}

func hashJSONMap(value map[string]any) string {
	encoded, _ := json.Marshal(value)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

func narrativeProjection(projection map[string]any) map[string]any {
	if projection == nil {
		return nil
	}
	copy := make(map[string]any, len(projection)-1)
	for key, value := range projection {
		if key != "action_capabilities" {
			copy[key] = value
		}
	}
	return copy
}

func unknownToolBinding(name string) agentToolBinding {
	return agentToolBinding{
		Definition:    llm.ToolDefinition{Name: name, Parameters: agentClosedObject(map[string]any{})},
		ExecutionMode: agentToolExecutionSerial,
	}
}

// validateAgentToolCallBatch validates the part of a multi-call response that
// must be true before any side effect is entered. The calls themselves are
// still executed in wire order by the coordinator, so providers that ignore
// parallel_tool_calls=false cannot create unordered state writes.
func validateAgentToolCallBatch(calls []llm.ToolCall) error {
	seen := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		if err := stableToolCallID(call); err != nil {
			return err
		}
		if _, exists := seen[call.ID]; exists {
			return fmt.Errorf("工具调用包含重复 call_id: %s", call.ID)
		}
		seen[call.ID] = struct{}{}
	}
	return nil
}

func stableToolCallID(call llm.ToolCall) error {
	if strings.TrimSpace(call.ID) == "" || call.ID != strings.TrimSpace(call.ID) || len(call.ID) > 256 {
		return errors.New("工具调用缺少稳定 call_id")
	}
	if !agentToolNamePattern.MatchString(call.Name) {
		return errors.New("工具调用名称无效")
	}
	return nil
}

func conversationCampaignRevision(conv *storage.Conversation, snapshot agentToolSnapshot) *int64 {
	if conv == nil || conv.CampaignID == nil {
		return nil
	}
	return snapshot.CampaignRevision
}

func attemptToolCallID(ctx context.Context, callID string) string {
	metadata, ok := attemptMetadata(ctx)
	if !ok {
		return callID
	}
	return metadata.AttemptID.String() + ":" + callID
}
