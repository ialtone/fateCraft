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
	"github.com/ialtone/fatecraft/internal/storage"
)

var (
	ErrBuiltinNotFound = errors.New("内置挑战不存在")
	ErrVersionConflict = errors.New("内置挑战版本冲突")
)

const (
	StatusAvailable = "available"
	StatusImported  = "imported"
	StatusConflict  = "conflict"
)

type ImportResult struct {
	Slug        string `json:"slug"`
	Version     string `json:"version"`
	ContentHash string `json:"content_hash"`
	Created     bool   `json:"created"`
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
}

type Importer struct {
	repo    *storage.ChallengeRepository
	dataDir string
}

func New(repo *storage.ChallengeRepository, dataDir string) *Importer {
	return &Importer{repo: repo, dataDir: dataDir}
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
		imports, err := New(tx, i.dataDir).importBuiltins(ctx)
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
		results, err = New(tx, i.dataDir).importBuiltins(ctx)
		return err
	})
	return results, err
}

// ImportDirectory validates and publishes one challenge directory atomically.
func (i *Importer) ImportDirectory(ctx context.Context, directory string) (*ImportResult, error) {
	var result *ImportResult
	err := i.repo.WithTx(ctx, func(tx *storage.ChallengeRepository) error {
		var err error
		result, err = New(tx, i.dataDir).importDirectory(ctx, directory)
		return err
	})
	return result, err
}

// ImportBuiltin imports one repository-maintained challenge selected by slug.
// The caller cannot supply a filesystem path.
func (i *Importer) ImportBuiltin(ctx context.Context, slug string) (*ImportResult, error) {
	directory, err := i.directoryBySlug(slug)
	if err != nil {
		return nil, err
	}
	var result *ImportResult
	err = i.repo.WithTx(ctx, func(tx *storage.ChallengeRepository) error {
		var importErr error
		result, importErr = New(tx, i.dataDir).importDirectory(ctx, directory)
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
		loaded, err := loadDirectory(directory, time.Now())
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
		result, err := i.importDirectory(ctx, directory)
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
		loaded, err := loadDirectory(directory, time.Now())
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
		ContentHash: loaded.hash, Status: StatusAvailable, Importable: true,
	}
	pack, err := i.repo.GetPackBySlug(ctx, loaded.manifest.ID)
	if errors.Is(err, storage.ErrNotFound) {
		return info, nil
	}
	if err != nil {
		return BuiltinInfo{}, err
	}
	currentVersion := ""
	if pack.CurrentVersionID != nil {
		current, err := i.repo.GetVersion(ctx, *pack.CurrentVersionID)
		if err != nil {
			return BuiltinInfo{}, err
		}
		info.CurrentVersion = current.Version
		currentVersion = current.Version
	}
	existing, err := i.repo.VersionByNumber(ctx, pack.ID, loaded.manifest.Version)
	if errors.Is(err, storage.ErrNotFound) {
		if currentVersion != "" {
			comparison, compareErr := compareStableVersions(loaded.manifest.Version, currentVersion)
			if compareErr != nil || comparison <= 0 {
				info.Status = StatusConflict
				info.Importable = false
			}
		}
		return info, nil
	}
	if err != nil {
		return BuiltinInfo{}, err
	}
	info.Importable = false
	if existing.Status == storage.ChallengeVersionPublished && existing.ContentHash == loaded.hash {
		info.Status = StatusImported
	} else {
		info.Status = StatusConflict
	}
	return info, nil
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

func (i *Importer) importDirectory(ctx context.Context, directory string) (*ImportResult, error) {
	now := time.Now()
	loaded, err := loadDirectory(directory, now)
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
			Description: manifest.Description, Status: storage.ChallengePackHidden,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := i.repo.CreatePack(ctx, pack); err != nil {
			return nil, err
		}
	}

	existing, err := i.repo.VersionByNumber(ctx, pack.ID, manifest.Version)
	if err == nil {
		if existing.Status == storage.ChallengeVersionPublished && existing.ContentHash == loaded.hash {
			return &ImportResult{
				Slug: pack.Slug, Version: existing.Version, ContentHash: loaded.hash,
			}, nil
		}
		return nil, fmt.Errorf("%w: 挑战 %s/%s 已存在且内容不同，必须使用新版本号", ErrVersionConflict, pack.Slug, manifest.Version)
	}
	if !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	if pack.CurrentVersionID != nil {
		current, err := i.repo.GetVersion(ctx, *pack.CurrentVersionID)
		if err != nil {
			return nil, err
		}
		comparison, err := compareStableVersions(manifest.Version, current.Version)
		if err != nil || comparison <= 0 {
			return nil, fmt.Errorf("%w: 仓库版本 %s 不高于数据库当前版本 %s", ErrVersionConflict, manifest.Version, current.Version)
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
	pack.Name = manifest.Name
	pack.Description = manifest.Description
	pack.UpdatedAt = now
	if err := i.repo.SavePack(ctx, pack); err != nil {
		return nil, err
	}
	if err := i.repo.CreateDraftWithContent(ctx, version, document.Assets, document.RewardRules); err != nil {
		return nil, err
	}
	published, err := challenge.NewService(i.repo).Publish(ctx, version.ID, uuid.Nil)
	if err != nil {
		return nil, err
	}
	return &ImportResult{
		Slug: pack.Slug, Version: published.Version.Version,
		ContentHash: published.Version.ContentHash, Created: true,
	}, nil
}
