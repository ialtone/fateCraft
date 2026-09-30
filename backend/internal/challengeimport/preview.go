package challengeimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/storage"
)

var ErrPreviewChanged = errors.New("导入预览已变化，请重新查看差异并确认")

type BuiltinPreview struct {
	Info           BuiltinInfo                    `json:"info"`
	CurrentVersion *storage.ChallengeVersion      `json:"current_version,omitempty"`
	CurrentRules   []storage.ChallengeOutcomeRule `json:"current_rules"`
	ProposedRules  []storage.JSONMap              `json:"proposed_rules"`
	Token          string                         `json:"preview_token"`
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
	result := &BuiltinPreview{Info: info, CurrentRules: []storage.ChallengeOutcomeRule{}, ProposedRules: []storage.JSONMap{}}
	pack, err := i.repo.GetPackBySlug(ctx, slug)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	if err == nil && pack.CurrentVersionID != nil {
		result.CurrentVersion, err = i.repo.GetVersion(ctx, *pack.CurrentVersionID)
		if err != nil {
			return nil, err
		}
		result.CurrentRules, err = i.repo.ListOutcomeRules(ctx, *pack.CurrentVersionID)
		if err != nil {
			return nil, err
		}
	}
	for _, outcome := range loaded.document.Outcomes {
		result.ProposedRules = append(result.ProposedRules, storage.JSONMap{"outcome": outcome.OutcomeKey, "title": outcome.Title, "points": outcome.RewardPoints, "enabled": outcome.Enabled})
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
