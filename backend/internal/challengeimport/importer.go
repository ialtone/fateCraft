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
	"github.com/ialtone/fatecraft/internal/gameplay"
	"github.com/ialtone/fatecraft/internal/gameplay/campaigngeneric"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation"
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
	Slug        string `json:"slug"`
	Version     string `json:"version"`
	ContentHash string `json:"content_hash"`
	Created     bool   `json:"created"`
	Overwritten bool   `json:"overwritten"`
}

type BootstrapResult struct {
	Initialized bool           `json:"initialized"`
	Imports     []ImportResult `json:"imports"`
}

type BuiltinInfo struct {
	Slug           string `json:"slug"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Version        string `json:"version"`
	ContentHash    string `json:"content_hash"`
	CurrentVersion string `json:"current_version,omitempty"`
	Status         string `json:"status"`
	Importable     bool   `json:"importable"`
	Overwritable   bool   `json:"overwritable"`
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
	registry := gameplay.NewCoreRegistry()
	campaigngeneric.Register(registry)
	cultivation.Register(registry)
	return registry
}

// BootstrapBuiltins is the startup policy: import every bundled challenge in
// one transaction only when challenge_packs is completely empty.
func (i *Importer) BootstrapBuiltins(ctx context.Context) (*BootstrapResult, error) {
	result := &BootstrapResult{}
	err := i.repo.WithTx(ctx, func(tx *storage.ChallengeRepository) error {
		if err := tx.LockBootstrap(ctx); err != nil {
			return fmt.Errorf("获取内置挑战初始化锁: %w", err)
		}
		count, err := tx.CountPacks(ctx)
		if err != nil {
			return fmt.Errorf("检查挑战内容: %w", err)
		}
		if count > 0 {
			return nil
		}
		imports, err := New(tx, i.dataDir, i.registry).importBuiltins(ctx)
		if err != nil {
			return err
		}
		result.Initialized = true
		result.Imports = imports
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

// OverwriteBuiltin explicitly replaces or reactivates the repository version.
// Runtime callers cannot supply content or a filesystem path.
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
		comparison, err := compareStableVersions(sourceVersion, current.Version)
		if err == nil && comparison > 0 {
			return StatusAvailable, true, false
		}
	}
	// 库里已存在同号版本、但与当前发布版本不同：这是已退役的旧版本。
	// 用它覆盖会删掉旧版本、重建发布，并把较新的当前版本退役、
	// 封存其下所有 active 对话，等同于版本回退，一律不再标为可覆盖。
	if current != nil && existing != nil && existing.ID != current.ID {
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

	// 奖励定义是跨玩法共享的实体。导入时按稳定 key 精确复用，
	// 再把规则中的临时 UUID 替换成数据库中的真实 UUID。
	definitionIDs := make(map[string]uuid.UUID, len(loaded.rewards))
	for index := range loaded.rewards {
		definition, err := i.repo.Rewards().UpsertDefinitionExact(ctx, &loaded.rewards[index])
		if err != nil {
			return nil, fmt.Errorf("导入奖励定义 %s: %w", loaded.rewards[index].Key, err)
		}
		definitionIDs[definition.Key] = definition.ID
	}
	for index := range document.RewardRules {
		key := stringFromJSONMap(document.RewardRules[index].PublicSnapshot, "key")
		definitionID, ok := definitionIDs[key]
		if !ok {
			return nil, fmt.Errorf("奖励规则引用了未导入的定义 %s", key)
		}
		document.RewardRules[index].RewardDefinitionID = definitionID
	}

	pack, err := i.repo.GetPackBySlug(ctx, manifest.ID)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	if pack == nil {
		pack = &storage.ChallengePack{
			ID: uuid.New(), Slug: manifest.ID, Name: manifest.Name,
			Description: manifest.Description, Status: storage.ChallengePackHidden,
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
	if existing != nil && existing.Status == storage.ChallengeVersionPublished &&
		existing.ContentHash == loaded.hash && pack.CurrentVersionID != nil &&
		*pack.CurrentVersionID == existing.ID {
		return &ImportResult{
			Slug: pack.Slug, Version: existing.Version, ContentHash: loaded.hash,
		}, nil
	}
	if existing != nil && !overwrite {
		return nil, fmt.Errorf("%w: 挑战 %s/%s 已存在", ErrOverwriteRequired, pack.Slug, manifest.Version)
	}
	// 导入的版本号不是当前发布版本时（无论同号版本是否存在），
	// 都要求仓库版本高于数据库当前版本：禁止用覆盖流程回退到旧版本，
	// 否则发布会退役较新的当前版本并封存其下所有 active 对话。
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
		if existing.Status == storage.ChallengeVersionPublished && existing.ContentHash == loaded.hash {
			if err := i.repo.ActivatePublishedVersion(ctx, pack.ID, existing.ID); err != nil {
				return nil, err
			}
			return &ImportResult{
				Slug: pack.Slug, Version: existing.Version, ContentHash: loaded.hash, Overwritten: true,
			}, nil
		}
		if err := i.repo.DeleteVersionIfUnused(ctx, existing.ID); err != nil {
			return nil, err
		}
	}

	version := &storage.ChallengeVersion{
		ID: uuid.New(), ChallengePackID: pack.ID, Version: manifest.Version,
		Status: storage.ChallengeVersionDraft, Manifest: document.Version.Manifest,
		CreatedAt: now, UpdatedAt: now,
	}
	for index := range document.Assets {
		document.Assets[index].ChallengeVersionID = version.ID
	}
	for index := range document.RewardRules {
		document.RewardRules[index].ChallengeVersionID = version.ID
	}
	if err := i.repo.CreateDraftWithContent(
		ctx, version, document.Runtime, document.Assets, document.Definitions, document.Outcomes, document.RewardRules,
	); err != nil {
		return nil, err
	}
	published, err := challenge.NewService(i.repo, i.registry).Publish(ctx, version.ID, uuid.Nil)
	if err != nil {
		return nil, err
	}
	return &ImportResult{
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
