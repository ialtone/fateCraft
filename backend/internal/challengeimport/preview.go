package challengeimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/storage"
)

var ErrPreviewChanged = errors.New("导入预览已变化，请重新查看差异并确认")

type RewardDefinitionDiff struct {
	Key      string                          `json:"key"`
	Before   storage.JSONMap                 `json:"before"`
	After    storage.JSONMap                 `json:"after"`
	Impact   *storage.RewardDefinitionImpact `json:"impact,omitempty"`
	Conflict bool                            `json:"conflict"`
}
type BuiltinPreview struct {
	Info           BuiltinInfo                   `json:"info"`
	CurrentVersion *storage.ChallengeVersion     `json:"current_version,omitempty"`
	CurrentRules   []storage.ChallengeRewardRule `json:"current_rules"`
	ProposedRules  []storage.JSONMap             `json:"proposed_rules"`
	Rewards        []RewardDefinitionDiff        `json:"rewards"`
	Token          string                        `json:"preview_token"`
}

func rewardPresentation(def storage.RewardDefinition) storage.JSONMap {
	return storage.JSONMap{"key": def.Key, "name": def.Name, "kind": def.Kind, "description": def.Description, "unit": def.Unit, "icon": def.Icon,
		"stackable": def.Stackable, "consumable": def.Consumable, "featured": def.Featured, "sort_order": def.SortOrder, "metadata": def.Metadata}
}

func (i *Importer) PreviewBuiltin(ctx context.Context, slug string) (*BuiltinPreview, error) {
	var result *BuiltinPreview
	err := i.repo.WithTx(ctx, func(tx *storage.ChallengeRepository) error {
		if err := tx.Rewards().LockConfiguration(ctx); err != nil {
			return err
		}
		var err error
		result, err = New(tx, i.dataDir, i.registry).previewBuiltin(ctx, slug)
		return err
	})
	return result, err
}

func (i *Importer) previewBuiltin(ctx context.Context, slug string) (*BuiltinPreview, error) {
	directory, err := i.directoryBySlug(slug)
	if err != nil {
		return nil, err
	}
	loaded, err := loadDirectory(directory, time.Now(), i.registry)
	if err != nil {
		return nil, err
	}
	info, err := i.builtinInfo(ctx, loaded)
	if err != nil {
		return nil, err
	}
	result := &BuiltinPreview{Info: info, CurrentRules: []storage.ChallengeRewardRule{}, ProposedRules: []storage.JSONMap{}, Rewards: []RewardDefinitionDiff{}}
	pack, err := i.repo.GetPackBySlug(ctx, slug)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	if err == nil && pack.CurrentVersionID != nil {
		result.CurrentVersion, err = i.repo.GetVersion(ctx, *pack.CurrentVersionID)
		if err != nil {
			return nil, err
		}
		result.CurrentRules, err = i.repo.ListRewardRules(ctx, *pack.CurrentVersionID)
		if err != nil {
			return nil, err
		}
	}
	for _, wanted := range loaded.rewards {
		diff := RewardDefinitionDiff{Key: wanted.Key, After: rewardPresentation(wanted)}
		current, err := i.repo.Rewards().DefinitionByKey(ctx, wanted.Key)
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			return nil, err
		}
		if err == nil {
			diff.Before = rewardPresentation(*current)
			diff.Conflict = current.Disabled || current.Kind != wanted.Kind || current.Stackable != wanted.Stackable || current.Consumable != wanted.Consumable
			diff.Impact, err = i.repo.Rewards().DefinitionImpact(ctx, current.ID)
			if err != nil {
				return nil, err
			}
		}
		if diff.Conflict || !reflect.DeepEqual(diff.Before, diff.After) {
			result.Rewards = append(result.Rewards, diff)
		}
	}
	for _, rule := range loaded.document.RewardRules {
		outcome := ""
		for _, candidate := range loaded.document.Outcomes {
			if candidate.ID == rule.OutcomeRuleID {
				outcome = candidate.OutcomeKey
				break
			}
		}
		key := ""
		for _, candidate := range loaded.rewards {
			if candidate.ID == rule.RewardDefinitionID {
				key = candidate.Key
				break
			}
		}
		result.ProposedRules = append(result.ProposedRules, storage.JSONMap{"outcome": outcome, "reward": key, "calculator": rule.CalculatorKey, "config": rule.CalculatorConfig, "enabled": rule.Enabled})
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(encoded)
	result.Token = hex.EncodeToString(hash[:])
	return result, nil
}

func (i *Importer) ConfirmBuiltin(ctx context.Context, slug string, overwrite bool, token string, actor uuid.UUID) (*ImportResult, error) {
	var result *ImportResult
	err := i.repo.WithTx(ctx, func(tx *storage.ChallengeRepository) error {
		if err := tx.Rewards().LockConfiguration(ctx); err != nil {
			return err
		}
		current := New(tx, i.dataDir, i.registry)
		preview, err := current.previewBuiltin(ctx, slug)
		if err != nil {
			return err
		}
		if token == "" || preview.Token != token {
			return ErrPreviewChanged
		}
		for _, diff := range preview.Rewards {
			if diff.Conflict {
				return storage.ErrRewardDefinitionConflict
			}
		}
		directory, err := current.directoryBySlug(slug)
		if err != nil {
			return err
		}
		result, err = current.importDirectory(ctx, directory, overwrite)
		if err != nil {
			return err
		}
		if result.ContentHash != preview.Info.ContentHash {
			return ErrPreviewChanged
		}
		return tx.Rewards().AuditBuiltinImport(ctx, actor, slug, storage.JSONMap{"preview": preview, "result": result})
	})
	return result, err
}
