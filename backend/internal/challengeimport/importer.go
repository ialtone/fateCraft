// Package challengeimport imports repository-maintained challenge content into
// the versioned challenge domain. Runtime challenge services never read files.
package challengeimport

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/gameplay/bootstrap"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/storage"
)

var (
	ErrBuiltinNotFound   = errors.New("内置挑战不存在")
	ErrOverwriteRequired = errors.New("内置内容已存在，请显式确认覆盖")
)

const (
	StatusAvailable       = "available"
	StatusImported        = "imported"
	StatusUpdateAvailable = "update_available"
)

type ImportResult struct {
	VersionID   uuid.UUID `json:"version_id"`
	Status      string    `json:"status"`
	Slug        string    `json:"slug"`
	Version     string    `json:"version"`
	ContentHash string    `json:"content_hash"`
	Created     bool      `json:"created"`
	Overwritten bool      `json:"overwritten"`
}

type BootstrapResult struct {
	Initialized bool           `json:"initialized"`
	Imports     []ImportResult `json:"imports"`
}

type BuiltinInfo struct {
	Slug           string     `json:"slug"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	Version        string     `json:"version"`
	ContentHash    string     `json:"content_hash"`
	CurrentVersion string     `json:"current_version,omitempty"`
	Status         string     `json:"status"`
	Importable     bool       `json:"importable"`
	Overwritable   bool       `json:"overwritable"`
	PackID         *uuid.UUID `json:"pack_id,omitempty"`
	Enabled        bool       `json:"enabled"`
}

type Importer struct {
	repo     *storage.ChallengeRepository
	dataDir  string
	registry *gameplay.Registry
}

func New(repo *storage.ChallengeRepository, dataDir string, registries ...*gameplay.Registry) *Importer {
	registry := defaultRegistry()
	if len(registries) > 0 && registries[0] != nil {
		registry = registries[0]
	}
	return &Importer{repo: repo, dataDir: dataDir, registry: registry}
}

func defaultRegistry() *gameplay.Registry {
	return bootstrap.NewRegistry()
}

// BootstrapBuiltins imports only the explicit startup manifest, in one
// transaction. Existing packs are never upgraded, overwritten or re-enabled.
func (i *Importer) BootstrapBuiltins(ctx context.Context) (*BootstrapResult, error) {
	result := &BootstrapResult{}
	err := i.repo.WithTx(ctx, func(tx *storage.ChallengeRepository) error {
		if err := tx.LockBootstrap(ctx); err != nil {
			return fmt.Errorf("获取内置挑战初始化锁: %w", err)
		}
		directories, err := bootstrapDirectories(i.dataDir)
		if err != nil {
			return err
		}
		bound := New(tx, i.dataDir, i.registry)
		for _, directory := range directories {
			loaded, err := loadDirectory(directory, time.Now(), i.registry)
			if err != nil {
				return err
			}
			_, err = tx.GetPackBySlug(ctx, loaded.manifest.ID)
			if err == nil {
				continue
			}
			if !errors.Is(err, storage.ErrNotFound) {
				return err
			}
			imported, err := bound.importDirectory(ctx, directory, false)
			if err != nil {
				return err
			}
			result.Imports = append(result.Imports, *imported)
		}
		result.Initialized = len(result.Imports) > 0
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ImportBuiltins is the reusable import operation independent of startup. It
// imports every directory under DATA_DIR/preset_packs atomically.
func (i *Importer) ImportBuiltins(ctx context.Context) ([]ImportResult, error) {
	var results []ImportResult
	err := i.repo.WithTx(ctx, func(tx *storage.ChallengeRepository) error {
		var err error
		results, err = New(tx, i.dataDir, i.registry).importBuiltins(ctx)
		return err
	})
	return results, err
}

// ImportDirectory validates and publishes one challenge directory atomically.
func (i *Importer) ImportDirectory(ctx context.Context, directory string) (*ImportResult, error) {
	var result *ImportResult
	err := i.repo.WithTx(ctx, func(tx *storage.ChallengeRepository) error {
		var err error
		result, err = New(tx, i.dataDir, i.registry).importDirectory(ctx, directory, false)
		return err
	})
	return result, err
}

// ImportBuiltin imports one repository-maintained challenge selected by slug.
// The caller cannot supply a filesystem path.
func (i *Importer) ImportBuiltin(ctx context.Context, slug string) (*ImportResult, error) {
	return i.importBuiltin(ctx, slug, false)
}

// OverwriteBuiltin only replaces an unpublished draft; published identities are immutable.
func (i *Importer) OverwriteBuiltin(ctx context.Context, slug string) (*ImportResult, error) {
	return i.importBuiltin(ctx, slug, true)
}

func (i *Importer) importBuiltin(ctx context.Context, slug string, overwrite bool) (*ImportResult, error) {
	directory, err := i.directoryBySlug(slug)
	if err != nil {
		return nil, err
	}
	var result *ImportResult
	err = i.repo.WithTx(ctx, func(tx *storage.ChallengeRepository) error {
		var importErr error
		result, importErr = New(tx, i.dataDir, i.registry).importDirectory(ctx, directory, overwrite)
		if importErr != nil {
			return importErr
		}
		if result.Slug != slug {
			return ErrBuiltinNotFound
		}
		return nil
	})
	return result, err
}

// ListBuiltins compares repository sources with database versions for the
// administrator import screen.
func (i *Importer) ListBuiltins(ctx context.Context) ([]BuiltinInfo, error) {
	directories, err := builtinDirectories(i.dataDir)
	if err != nil {
		return nil, err
	}
	items := make([]BuiltinInfo, 0, len(directories))
	for _, directory := range directories {
		loaded, err := loadDirectory(directory, time.Now(), i.registry)
		if err != nil {
			return nil, fmt.Errorf("读取 %s: %w", directory, err)
		}
		info, err := i.builtinInfo(ctx, loaded)
		if err != nil {
			return nil, err
		}
		items = append(items, info)
	}
	return items, nil
}

func (i *Importer) importBuiltins(ctx context.Context) ([]ImportResult, error) {
	directories, err := builtinDirectories(i.dataDir)
	if err != nil {
		return nil, err
	}
	return i.importDirectories(ctx, directories)
}

func (i *Importer) importDirectories(ctx context.Context, directories []string) ([]ImportResult, error) {
	results := make([]ImportResult, 0, len(directories))
	for _, directory := range directories {
		result, err := i.importDirectory(ctx, directory, false)
		if err != nil {
			return nil, fmt.Errorf("导入 %s: %w", directory, err)
		}
		results = append(results, *result)
	}
	return results, nil
}

func (i *Importer) directoryBySlug(slug string) (string, error) {
	directories, err := builtinDirectories(i.dataDir)
	if err != nil {
		return "", err
	}
	for _, directory := range directories {
		loaded, err := loadDirectory(directory, time.Now(), i.registry)
		if err != nil {
			return "", fmt.Errorf("读取 %s: %w", directory, err)
		}
		if loaded.manifest.ID == slug {
			return directory, nil
		}
	}
	return "", ErrBuiltinNotFound
}

func (i *Importer) builtinInfo(ctx context.Context, loaded *loadedChallenge) (BuiltinInfo, error) {
	info := BuiltinInfo{
		Slug: loaded.manifest.ID, Name: loaded.manifest.Name,
		Description: loaded.manifest.Description, Version: loaded.manifest.Version,
		ContentHash: loaded.hash,
	}
	pack, err := i.repo.GetPackBySlug(ctx, loaded.manifest.ID)
	if errors.Is(err, storage.ErrNotFound) {
		info.Status = StatusAvailable
		info.Importable = true
		return info, nil
	}
	if err != nil {
		return BuiltinInfo{}, err
	}
	info.PackID = &pack.ID
	info.Enabled = pack.Status == storage.ChallengePackActive
	var current *storage.ChallengeVersion
	if pack.CurrentVersionID != nil {
		current, err = i.repo.GetVersion(ctx, *pack.CurrentVersionID)
		if err != nil {
			return BuiltinInfo{}, err
		}
		info.CurrentVersion = current.Version
	}
	existing, err := i.repo.VersionByNumber(ctx, pack.ID, loaded.manifest.Version)
	if errors.Is(err, storage.ErrNotFound) {
		existing = nil
	} else if err != nil {
		return BuiltinInfo{}, err
	}
	info.Status, info.Importable, info.Overwritable = classifyBuiltin(
		loaded.manifest.Version, loaded.hash, current, existing,
	)
	return info, nil
}

func classifyBuiltin(
	sourceVersion, sourceHash string,
	current, existing *storage.ChallengeVersion,
) (status string, importable, overwritable bool) {
	if current == nil && existing == nil {
		return StatusAvailable, true, false
	}
	if current != nil && current.Status == storage.ChallengeVersionPublished &&
		current.Version == sourceVersion && current.ContentHash == sourceHash {
		return StatusImported, false, false
	}
	if current != nil && existing == nil {
		return StatusAvailable, true, false
	}
	// 已导入的非入口版本由版本管理选择，内置列表不提供发布快照覆盖。
	if current != nil && existing != nil && existing.ID != current.ID {
		return StatusUpdateAvailable, false, false
	}
	if existing != nil && existing.Status != storage.ChallengeVersionDraft {
		return StatusUpdateAvailable, false, false
	}
	return StatusUpdateAvailable, false, true
}

func builtinDirectories(dataDir string) ([]string, error) {
	root := filepath.Join(dataDir, "preset_packs")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("读取内置挑战目录 %s: %w", root, err)
	}
	directories := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			directories = append(directories, filepath.Join(root, entry.Name()))
		}
	}
	sort.Strings(directories)
	if len(directories) == 0 {
		return nil, fmt.Errorf("没有找到内置挑战: %s", root)
	}
	return directories, nil
}

func (i *Importer) importDirectory(ctx context.Context, directory string, overwrite bool) (*ImportResult, error) {
	now := time.Now()
	loaded, err := loadDirectory(directory, now, i.registry)
	if err != nil {
		return nil, err
	}
	manifest := loaded.manifest
	document := loaded.document

	pack, err := i.repo.GetPackBySlug(ctx, manifest.ID)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	if pack == nil {
		pack = &storage.ChallengePack{
			ID: uuid.New(), Slug: manifest.ID, Name: manifest.Name,
			Description: manifest.Description, Status: storage.ChallengePackHidden, Source: "builtin",
			CreatedAt: now, UpdatedAt: now,
		}
		if err := i.repo.CreatePack(ctx, pack); err != nil {
			return nil, err
		}
	}
	existing, err := i.repo.VersionByNumber(ctx, pack.ID, manifest.Version)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	if errors.Is(err, storage.ErrNotFound) {
		existing = nil
	}
	if existing != nil && existing.Status != storage.ChallengeVersionDraft && existing.ContentHash == loaded.hash {
		return &ImportResult{
			Slug: pack.Slug, Version: existing.Version, ContentHash: loaded.hash, VersionID: existing.ID, Status: existing.Status,
		}, nil
	}
	if existing != nil && existing.Status != storage.ChallengeVersionDraft {
		return nil, storage.ErrPublishedVersion
	}
	if existing != nil && !overwrite {
		return nil, fmt.Errorf("%w: 挑战 %s/%s 已存在", ErrOverwriteRequired, pack.Slug, manifest.Version)
	}
	// 导入的版本号不是当前发布版本时（无论同号版本是否存在），
	// 都要求仓库版本高于数据库当前版本：禁止用覆盖流程回退到旧版本，
	// 已有对话保持原版本；启动导入不提供版本回退。
	if pack.CurrentVersionID != nil {
		current, err := i.repo.GetVersion(ctx, *pack.CurrentVersionID)
		if err != nil {
			return nil, err
		}
		if manifest.Version != current.Version {
			comparison, err := compareStableVersions(manifest.Version, current.Version)
			if err != nil || comparison <= 0 {
				return nil, fmt.Errorf("%w: 仓库版本 %s 不高于数据库当前版本 %s；更新已使用内容必须提升版本号", ErrOverwriteRequired, manifest.Version, current.Version)
			}
		}
	}

	pack.Name = manifest.Name
	pack.Description = manifest.Description
	pack.UpdatedAt = now
	if err := i.repo.SavePack(ctx, pack); err != nil {
		return nil, err
	}
	overwritten := overwrite && pack.CurrentVersionID != nil
	if existing != nil {
		overwritten = true
		if err := i.repo.ReplaceDraftChecked(ctx, existing.ID, existing.Revision, document.Version.Manifest, document.Runtime, document.Assets, document.Definitions, document.Outcomes); err != nil {
			return nil, err
		}
		return &ImportResult{Slug: pack.Slug, Version: existing.Version, ContentHash: loaded.hash, VersionID: existing.ID, Status: existing.Status, Overwritten: true}, nil
	}

	version := &storage.ChallengeVersion{
		ID: uuid.New(), ChallengePackID: pack.ID, Version: manifest.Version,
		Status: storage.ChallengeVersionDraft, Manifest: document.Version.Manifest,
		CreatedAt: now, UpdatedAt: now,
	}
	for index := range document.Assets {
		document.Assets[index].ChallengeVersionID = version.ID
	}
	if err := i.repo.CreateDraftWithContent(
		ctx, version, document.Runtime, document.Assets, document.Definitions, document.Outcomes,
	); err != nil {
		return nil, err
	}
	published, err := challenge.NewService(i.repo, i.registry).Publish(ctx, version.ID, uuid.Nil)
	if err != nil {
		return nil, err
	}
	return &ImportResult{
		VersionID: version.ID, Status: published.Version.Status,
		Slug: pack.Slug, Version: published.Version.Version,
		ContentHash: published.Version.ContentHash, Created: true, Overwritten: overwritten,
	}, nil
}

func stringFromJSONMap(values storage.JSONMap, key string) string {
	if value, ok := values[key].(string); ok {
		return value
	}
	return ""
}
