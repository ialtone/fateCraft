package challengeimport

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/storage"
	"time"
)

var ErrPreviewChanged = errors.New("导入预览已变化，请重新查看差异并确认")

// Official content uses the same persisted preview and atomic confirmation as uploads.
func (i *Importer) PreviewBuiltin(ctx context.Context, slug string, actor uuid.UUID) (*storage.GameplayImport, error) {
	directory, err := i.directoryBySlug(slug)
	if err != nil {
		return nil, err
	}
	loaded, err := loadDirectory(directory, time.Now(), i.registry)
	if err != nil {
		return nil, err
	}
	portable, err := portableFromDocument(&loaded.document)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(portable)
	if err != nil {
		return nil, err
	}
	row, err := i.PreviewUpload(ctx, actor, slug+".fatecraft.json", raw, PreviewOptions{Strategy: "replace_draft", TrustedUpdate: true})
	if err != nil {
		return nil, err
	}
	err = i.repo.WithTx(ctx, func(tx *storage.ChallengeRepository) error {
		locked, err := tx.ImportRecord(ctx, row.ID, actor, true)
		if err != nil {
			return err
		}
		locked.Source = "builtin"
		row.Source = "builtin"
		return tx.SaveImport(ctx, locked)
	})
	return row, err
}
