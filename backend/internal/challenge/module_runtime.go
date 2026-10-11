package challenge

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/kernel"
	"github.com/ialtone/fatecraft/internal/state"
)

// Publication exercises the compiled module as well as its declarative schema.
// This is a deterministic dry run; it never creates a save or consumes a seed.
func validateInitialModule(registry *gameplay.Registry, content gameplay.Content, personas ...string) error {
	descriptor, err := registry.Module(content.Runtime.Engine)
	if err != nil {
		return err
	}
	if descriptor.Runtime == nil {
		return nil
	}
	if len(personas) == 0 || len(personas) > 64 {
		return fmt.Errorf("可信模块需要 1–64 个可选出身")
	}
	for _, persona := range personas {
		if err := validateInitialPersona(descriptor.Runtime, content, persona, descriptor.AbandonAction); err != nil {
			return fmt.Errorf("出身 %s: %w", persona, err)
		}
	}
	return nil
}

func validateInitialPersona(module gameplay.RuntimeModule, content gameplay.Content, persona string, exits ...string) error {
	run, err := kernel.Initialize(module, content, gameplay.InitialInput{PersonaKey: persona, Random: kernel.NewRandom(0, "content-validation")})
	if err != nil {
		return err
	}
	for _, actor := range []string{"player", "model"} {
		if _, err := kernel.Preview(module, content, run, actor); err != nil {
			return err
		}
	}
	descriptor := ""
	if len(exits) > 0 {
		descriptor = exits[0]
	}
	if descriptor != "" {
		exit, err := kernel.Execute(module, content, run, gameplay.RuntimeRequest{Actor: "player", ActionKey: descriptor, Arguments: map[string]any{}, Random: kernel.NewRandom(0, "exit-validation")})
		if err != nil {
			return fmt.Errorf("主动退出不可用: %w", err)
		}
		if exit.State.Status == gameplay.RunActive || exit.State.GameTime != run.GameTime {
			return fmt.Errorf("主动退出必须立即结束且不推进时间")
		}
		found := false
		for _, outcome := range content.Outcomes {
			if outcome.Key == exit.State.OutcomeKey && outcome.Enabled && outcome.Category == "neutral" && outcome.RewardPoints == 0 {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("主动退出必须使用启用的零积分 neutral 结局")
		}
	}
	for _, audience := range []string{gameplay.AudiencePlayer, gameplay.AudienceModel, gameplay.AudienceInquiry} {
		if _, err := kernel.Project(module, content, run, audience); err != nil {
			return err
		}
	}
	if content.Runtime.Mode == gameplay.RuntimeModeChallenge {
		patch, err := kernel.EnvelopePatch(module, content, kernel.Envelope{Engine: content.Runtime.Engine, Seed: "0", State: run})
		if err != nil {
			return err
		}
		return state.ValidatePatch(content.Runtime.StateSchema, gameplay.SourceModule, patch)
	}
	view, err := kernel.Project(module, content, run, gameplay.AudiencePlayer)
	if err != nil {
		return err
	}
	if presentation := content.Runtime.Presentation.CampaignView; presentation != nil {
		for _, path := range []string{presentation.Header.TitlePath, presentation.Header.TimePath} {
			if strings.HasPrefix(path, "gameplay.") {
				if _, exists := gameplay.ValueAtPath(view, strings.TrimPrefix(path, "gameplay.")); !exists {
					return fmt.Errorf("页头引用了模块未提供的字段 %s", path)
				}
			}
		}
		for _, section := range presentation.Sections {
			for _, field := range section.Fields {
				if strings.HasPrefix(field.Path, "gameplay.") {
					value, exists := gameplay.ValueAtPath(view, strings.TrimPrefix(field.Path, "gameplay."))
					if !exists {
						return fmt.Errorf("展示引用了模块未提供的字段 %s", field.Path)
					}
					if err := validateModuleDisplayValue(field.Path, field.Widget, value, field.ItemFields); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// JSON projection has already been normalized by the kernel. Empty lists are
// valid; item fields are checked for every present item, including nested lists.
func validateModuleDisplayValue(path, widget string, value any, fields []gameplay.CampaignViewItemField) error {
	if value == nil {
		return nil // optional public values may not exist until later in a run
	}
	// Item fields use the renderer's automatic collection formatting when no
	// widget is specified. A nested object is one item, rather than a dictionary.
	if widget == "" {
		switch value.(type) {
		case []any:
			widget = "list"
		case map[string]any:
			widget, value = "item_list", []any{value}
		}
	}
	switch widget {
	case "number", "progress":
		if _, ok := value.(float64); !ok {
			return fmt.Errorf("展示字段 %s 需要数值", path)
		}
	case "list", "item_list":
		items, ok := value.([]any)
		if object, isObject := value.(map[string]any); isObject {
			ok = true
			for _, item := range object {
				items = append(items, item)
			}
		}
		if !ok {
			return fmt.Errorf("展示字段 %s 需要列表", path)
		}
		for index, item := range items {
			for _, field := range fields {
				object, ok := item.(map[string]any)
				if !ok {
					return fmt.Errorf("展示字段 %s[%d] 需要条目对象", path, index)
				}
				v, exists := gameplay.ValueAtPath(object, field.Path)
				if !exists {
					return fmt.Errorf("展示字段 %s[%d] 缺少 %s", path, index, field.Path)
				}
				if err := validateModuleDisplayValue(fmt.Sprintf("%s[%d].%s", path, index, field.Path), field.Widget, v, field.ItemFields); err != nil {
					return err
				}
			}
		}
	case "", "text", "title", "badge":
		switch value.(type) {
		case string, float64, bool:
		default:
			return fmt.Errorf("展示字段 %s 需要文本或标量，请使用列表控件展示集合", path)
		}
	}
	return nil
}

// ModuleContentForVersion fixes definitions before callers enter a gameplay
// transaction, so rule execution never fetches a second database connection.
func (s *Service) ModuleContentForVersion(ctx context.Context, versionID uuid.UUID, hash string) (gameplay.Content, error) {
	runtime, err := s.RuntimeForVersion(ctx, versionID, hash)
	if err != nil {
		return gameplay.Content{}, err
	}
	compiled, err := s.compiledVersion(ctx, versionID)
	if err != nil {
		return gameplay.Content{}, err
	}
	document, err := cloneVersionDocument(compiled.document)
	if err != nil {
		return gameplay.Content{}, err
	}
	return moduleContent(runtime, document), nil
}

func (s *Service) ModuleProjection(content gameplay.Content, trusted map[string]any, actor string) (map[string]any, error) {
	module, err := s.registry.Module(content.Runtime.Engine)
	if err != nil {
		return nil, err
	}
	if module.Runtime == nil {
		return nil, fmt.Errorf("当前玩法没有可信模块执行器")
	}
	envelope, err := kernel.ReadEnvelope(content, trusted)
	if err != nil {
		return nil, err
	}
	previews, err := kernel.Preview(module.Runtime, content, envelope.State, actor)
	if err != nil {
		return nil, err
	}
	capabilities := []map[string]any{}
	endings := []map[string]any{}
	for _, preview := range previews {
		if !slices.Contains(content.Runtime.Actions.Domain, preview.Action.Key) {
			continue
		}
		capability := map[string]any{"key": preview.Action.Key, "ending": preview.Action.Ending, "description": preview.Action.Description, "available": preview.Available, "revision": envelope.State.Revision, "argument_schema": preview.Action.ArgumentSchema(), "metadata": map[string]any{"label": preview.Action.Label, "minutes": preview.Minutes, "costs": preview.Costs, "risk": preview.Risk}}
		capability["parameters"] = preview.Action.Parameters
		capability["plans"] = preview.Plans
		capability["cost_labels"] = preview.CostLabels
		if !preview.Available {
			capability["violations"] = []map[string]any{{"code": "action.unavailable", "message": preview.Reason}}
		}
		capabilities = append(capabilities, capability)
		if preview.Action.Ending {
			endings = append(endings, capability)
		}
	}
	if actor == "model" {
		playerProjection, err := s.ModuleProjection(content, trusted, "player")
		if err != nil {
			return nil, err
		}
		endings, _ = playerProjection["ending_capabilities"].([]map[string]any)
	}
	audience := gameplay.AudienceModel
	if actor == "player" {
		audience = gameplay.AudiencePlayer
	}
	view, err := kernel.Project(module.Runtime, content, envelope.State, audience)
	if err != nil {
		return nil, err
	}
	facts, err := gameplay.SelectNarrativeFacts(view, gameplay.NarrativeFactPaths(module.Runtime))
	if err != nil {
		return nil, err
	}
	return map[string]any{"revision": envelope.State.Revision, "status": envelope.State.Status, "gameplay": view, "narrative_facts": facts, "action_capabilities": capabilities, "ending_capabilities": endings}, nil
}

// ProjectInquiry projects the caller's already-versioned state snapshot. It
// never rereads live state or asks the module to preview or execute actions.
func (s *Service) ProjectInquiry(ctx context.Context, id uuid.UUID, trusted map[string]any) (map[string]any, error) {
	conv, err := s.repo.Conversation(ctx, id)
	if err != nil {
		return nil, err
	}
	content, err := s.ModuleContentForVersion(ctx, conv.ChallengeVersionID, conv.ChallengeHash)
	if err != nil {
		return nil, err
	}
	module, err := s.registry.Module(content.Runtime.Engine)
	if err != nil {
		return nil, err
	}
	public := gameplay.PlayerState(*content.Runtime, trusted)
	if module.Runtime == nil || content.Runtime.Mode != gameplay.RuntimeModeChallenge {
		return public, nil
	}
	envelope, err := kernel.ReadEnvelope(content, trusted)
	if err != nil {
		return nil, err
	}
	public["gameplay"], err = kernel.Project(module.Runtime, content, envelope.State, gameplay.AudienceInquiry)
	return public, err
}

func (s *Service) InitialConversationState(ctx context.Context, selection *Selection) (map[string]any, error) {
	initial := gameplay.InitialState(selection.Contract.StateSchema)
	module, err := s.registry.Module(selection.Contract.Engine)
	if err != nil {
		return nil, err
	}
	if module.Runtime == nil {
		return initial, nil
	}
	content, err := s.ModuleContentForVersion(ctx, selection.Version.ID, selection.Version.ContentHash)
	if err != nil {
		return nil, err
	}
	persona := ""
	if selection.Persona != nil {
		persona = selection.Persona.Slug
	}
	envelope, err := kernel.NewEnvelope(module.Runtime, content, persona)
	if err != nil {
		return nil, err
	}
	patch, err := kernel.EnvelopePatch(module.Runtime, content, envelope)
	if err != nil {
		return nil, err
	}
	if err := state.ValidatePatch(selection.Contract.StateSchema, gameplay.SourceModule, patch); err != nil {
		return nil, err
	}
	after, _, skipped := state.ApplyPatch(selection.Contract.StateSchema, gameplay.SourceModule, initial, patch)
	if len(skipped) > 0 {
		return nil, fmt.Errorf("初始化玩法字段被拒绝: %v", skipped)
	}
	return after, nil
}
