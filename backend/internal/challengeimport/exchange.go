package challengeimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/storage"
)

var importValidationSlots = make(chan struct{}, 2)

type PreviewOptions struct {
	Strategy      string
	TargetSlug    string
	TrustedUpdate bool
}
type ContentDifference struct {
	Path   string          `json:"path"`
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
}

func contentDifferences(before, after *PortablePackage) []ContentDifference {
	toMap := func(p *PortablePackage) map[string]json.RawMessage {
		result := map[string]json.RawMessage{}
		if p == nil {
			return result
		}
		raw, _ := json.Marshal(p)
		_ = json.Unmarshal(raw, &result)
		delete(result, "content_hash")
		delete(result, "provenance")
		for _, field := range []string{"assets", "definitions", "outcomes"} {
			var rows []map[string]json.RawMessage
			_ = json.Unmarshal(result[field], &rows)
			delete(result, field)
			for _, row := range rows {
				var a, b string
				if field == "outcomes" {
					_ = json.Unmarshal(row["outcome_key"], &a)
				} else {
					_ = json.Unmarshal(row["kind"], &a)
					key := "key"
					if field == "assets" {
						key = "slug"
					}
					_ = json.Unmarshal(row[key], &b)
				}
				key := field + "/" + a
				if b != "" {
					key += "/" + b
				}
				result[key], _ = json.Marshal(row)
			}
		}
		return result
	}
	old, next := toMap(before), toMap(after)
	keys := map[string]bool{}
	for k := range old {
		keys[k] = true
	}
	for k := range next {
		keys[k] = true
	}
	diffs := []ContentDifference{}
	for _, k := range sortedKeys(keys) {
		if string(old[k]) != string(next[k]) {
			diffs = append(diffs, ContentDifference{k, old[k], next[k]})
		}
	}
	return diffs
}

func (i *Importer) PreviewUpload(ctx context.Context, actor uuid.UUID, name string, raw []byte, opts PreviewOptions) (*storage.GameplayImport, error) {
	select {
	case importValidationSlots <- struct{}{}:
		defer func() { <-importValidationSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if len(raw) > MaxUploadBytes {
		return nil, packageError("package_too_large", "上传内容超过 16 MiB")
	}
	var loaded *loadedChallenge
	var p *PortablePackage
	var err error
	if strings.HasSuffix(strings.ToLower(name), ".zip") {
		loaded, p, err = decodeArchive(raw, i.registry)
	} else if strings.HasSuffix(strings.ToLower(name), ".json") {
		loaded, p, err = decodePortable(raw, i.registry)
	} else {
		return nil, packageError("package_format_unsupported", "请上传 .fatecraft.json 或 .zip")
	}
	if err != nil {
		var classified *PackageError
		if errors.As(err, &classified) {
			return nil, err
		}
		return nil, packageError("package_validation_failed", "%v", err)
	}
	integrityDeclared := p.ContentHash != ""
	if opts.Strategy == "" {
		opts.Strategy = "update"
	}
	if opts.Strategy != "update" && opts.Strategy != "replace_draft" && opts.Strategy != "fork" {
		return nil, fmt.Errorf("%w: 导入策略无效", challenge.ErrInvalidChallenge)
	}
	if opts.Strategy == "fork" {
		if opts.TargetSlug == "" || opts.TargetSlug == p.Manifest.ID {
			return nil, fmt.Errorf("%w: 另存为必须提供不同的新包标识", challenge.ErrInvalidChallenge)
		}
		// No generic cross-package reference rewriting: reject occurrences outside manifest.id.
		original := p.Manifest.ID
		p.Manifest.ID = ""
		check, _ := json.Marshal(p)
		if strings.Contains(string(check), `"`+original+`"`) {
			return nil, fmt.Errorf("%w: 内容含包身份引用，不能安全另存为；请修改源包后重新上传", challenge.ErrInvalidChallenge)
		}
		p.Manifest.ID = opts.TargetSlug
		p.Provenance = &Provenance{UpstreamID: original, UpstreamVersion: p.Manifest.Version, UpstreamHash: loaded.hash}
		p.ContentHash = ""
		loaded, err = compilePortable(p, i.registry)
		if err != nil {
			return nil, err
		}
	} else if opts.TargetSlug != "" && opts.TargetSlug != p.Manifest.ID {
		return nil, fmt.Errorf("%w: 更新目标与 manifest.id 不一致", challenge.ErrInvalidChallenge)
	}
	p.ContentHash = loaded.hash
	payload, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	if len(payload) > MaxUploadBytes {
		return nil, fmt.Errorf("%w: 编译内容超过 16 MiB", challenge.ErrInvalidChallenge)
	}
	sum := sha256.Sum256(raw)
	row := &storage.GameplayImport{ID: uuid.New(), ActorID: actor, Source: "upload", FileName: filepath.Base(name), FileHash: hex.EncodeToString(sum[:]), ContentHash: loaded.hash, TargetSlug: p.Manifest.ID, Strategy: opts.Strategy, Status: "preview", Payload: storage.JSONDoc(payload), CreatedAt: time.Now(), ExpiresAt: time.Now().Add(30 * time.Minute)}
	err = i.repo.WithTx(ctx, func(tx *storage.ChallengeRepository) error {
		if err := tx.LockBootstrap(ctx); err != nil {
			return err
		}
		if err := tx.CleanupImports(ctx); err != nil {
			return err
		}
		var current *PortablePackage
		var references *storage.VersionReferences
		pack, err := tx.GetPackBySlug(ctx, p.Manifest.ID)
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			return err
		}
		if err == nil {
			pack, err = tx.LockPack(ctx, pack.ID)
			if err != nil {
				return err
			}
			if opts.Strategy == "fork" {
				return fmt.Errorf("%w: 新包标识已存在", challenge.ErrInvalidChallenge)
			}
			if pack.Source != "upload" && !opts.TrustedUpdate {
				return fmt.Errorf("%w: 目标不是上传包，需要明确确认可信来源更新，或另存为新包", challenge.ErrInvalidChallenge)
			}
			row.PackRevision = pack.ManagementRevision
			existing, err := tx.VersionByNumber(ctx, pack.ID, p.Manifest.Version)
			if err != nil && !errors.Is(err, storage.ErrNotFound) {
				return err
			}
			if err == nil {
				if existing.Status == storage.ChallengeVersionDraft {
					row.DraftRevision = existing.Revision
					doc, err := challenge.NewService(tx, i.registry).VersionDocument(ctx, existing.ID)
					if err != nil {
						return err
					}
					hash, err := challenge.ContentHashDocument(doc)
					if err != nil {
						return err
					}
					if opts.Strategy != "replace_draft" && hash != loaded.hash {
						return fmt.Errorf("%w: 同号草稿已存在，请选择替换草稿", challenge.ErrInvalidChallenge)
					}
				} else if existing.ContentHash != loaded.hash {
					return storage.ErrPublishedVersion
				}
			}
			if pack.CurrentVersionID != nil {
				doc, err := challenge.NewService(tx, i.registry).VersionDocument(ctx, *pack.CurrentVersionID)
				if err != nil {
					return err
				}
				current, err = portableFromDocument(doc)
				if err != nil {
					return err
				}
				references, err = tx.VersionReferences(ctx, *pack.CurrentVersionID)
				if err != nil {
					return err
				}
			}
		}
		module, err := i.registry.Module(p.Runtime.Engine)
		if err != nil {
			return err
		}
		row.Preview = storage.JSONMap{"definitions": p.Definitions, "definition_schemas": module.DefinitionSchemas, "manifest": p.Manifest, "engine": p.Runtime.Engine, "mode": p.Runtime.Mode, "source": p.Provenance, "strategy": opts.Strategy, "trusted_update": opts.TrustedUpdate, "differences": contentDifferences(current, p), "current": currentManifest(current), "references": references, "outcomes": p.Outcomes, "integrity_declared": integrityDeclared}
		return tx.CreateImport(ctx, row)
	})
	return row, err
}
func currentManifest(p *PortablePackage) any {
	if p == nil {
		return nil
	}
	return p.Manifest
}

func (i *Importer) ConfirmUpload(ctx context.Context, actor, id, request uuid.UUID) (*storage.GameplayImport, error) {
	if request == uuid.Nil {
		return nil, fmt.Errorf("%w: 缺少 request_id", challenge.ErrInvalidChallenge)
	}
	var result *storage.GameplayImport
	err := i.repo.WithTx(ctx, func(tx *storage.ChallengeRepository) error {
		if err := tx.LockBootstrap(ctx); err != nil {
			return err
		}
		replay, err := tx.ImportRequest(ctx, request, actor)
		if err == nil {
			if replay.ID != id {
				return storage.ErrRevisionConflict
			}
			result = replay
			return nil
		}
		if !errors.Is(err, storage.ErrNotFound) {
			return err
		}
		row, err := tx.ImportRecord(ctx, id, actor, true)
		if err != nil {
			return err
		}
		result = row
		if row.Status != "preview" || time.Now().After(row.ExpiresAt) {
			return ErrPreviewChanged
		}
		loaded, _, err := decodePortable(row.Payload, i.registry)
		if err != nil {
			return err
		}
		if loaded.hash != row.ContentHash {
			return ErrPreviewChanged
		}
		if row.Source == "builtin" {
			directory, err := i.directoryBySlug(row.TargetSlug)
			if err != nil {
				return err
			}
			official, err := loadDirectory(directory, time.Now(), i.registry)
			if err != nil {
				return err
			}
			if official.hash != row.ContentHash {
				return ErrPreviewChanged
			}
		}
		pack, err := tx.GetPackBySlug(ctx, row.TargetSlug)
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			return err
		}
		if errors.Is(err, storage.ErrNotFound) {
			if row.PackRevision != 0 {
				return ErrPreviewChanged
			}
			pack = &storage.ChallengePack{ID: uuid.New(), Slug: row.TargetSlug, Name: loaded.manifest.Name, Description: loaded.manifest.Description, Status: storage.ChallengePackHidden, Source: row.Source, ManagementRevision: 1, CreatedBy: &actor, CreatedAt: time.Now(), UpdatedAt: time.Now()}
			if err := tx.CreatePack(ctx, pack); err != nil {
				return err
			}
		} else {
			pack, err = tx.LockPack(ctx, pack.ID)
			if err != nil {
				return err
			}
			if pack.ManagementRevision != row.PackRevision {
				return ErrPreviewChanged
			}
		}
		existing, err := tx.VersionByNumber(ctx, pack.ID, loaded.manifest.Version)
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			return err
		}
		if err == nil {
			existing, err = tx.LockVersion(ctx, existing.ID)
			if err != nil {
				return err
			}
			if existing.Status == storage.ChallengeVersionDraft {
				if existing.Revision != row.DraftRevision {
					return ErrPreviewChanged
				}
				doc, err := challenge.NewService(tx, i.registry).VersionDocument(ctx, existing.ID)
				if err != nil {
					return err
				}
				hash, err := challenge.ContentHashDocument(doc)
				if err != nil {
					return err
				}
				if hash != row.ContentHash {
					if row.Strategy != "replace_draft" {
						return ErrPreviewChanged
					}
					if err := tx.ReplaceDraftChecked(ctx, existing.ID, row.DraftRevision, loaded.document.Version.Manifest, loaded.document.Runtime, loaded.document.Assets, loaded.document.Definitions, loaded.document.Outcomes); err != nil {
						return err
					}
					if err := tx.SetDraftProvenance(ctx, existing.ID, loaded.document.Version.Provenance); err != nil {
						return err
					}
				}
			} else if existing.ContentHash != row.ContentHash {
				return storage.ErrPublishedVersion
			}
		} else {
			if row.DraftRevision != 0 {
				return ErrPreviewChanged
			}
			existing = &storage.ChallengeVersion{ID: uuid.New(), ChallengePackID: pack.ID, Version: loaded.manifest.Version, Status: storage.ChallengeVersionDraft, Manifest: loaded.document.Version.Manifest, Provenance: loaded.document.Version.Provenance, CreatedBy: &actor, CreatedAt: time.Now(), UpdatedAt: time.Now()}
			if err := tx.CreateDraftWithContent(ctx, existing, loaded.document.Runtime, loaded.document.Assets, loaded.document.Definitions, loaded.document.Outcomes); err != nil {
				return err
			}
		}
		row.Status = "imported"
		row.ResultVersionID = &existing.ID
		row.RequestID = &request
		row.Payload = nil
		if err := tx.SaveImport(ctx, row); err != nil {
			return err
		}
		return tx.AuditGameplay(ctx, actor, "gameplay_import", map[string]any{"import_id": id, "version_id": existing.ID, "content_hash": row.ContentHash, "preview": row.Preview})
	})
	return result, err
}

func (i *Importer) ImportRecord(ctx context.Context, actor, id uuid.UUID) (*storage.GameplayImport, error) {
	if err := i.repo.CleanupImports(ctx); err != nil {
		return nil, err
	}
	return i.repo.ImportRecord(ctx, id, actor, false)
}
func (i *Importer) ListImports(ctx context.Context, actor uuid.UUID, before *storage.ImportCursor) ([]storage.GameplayImport, error) {
	if err := i.repo.CleanupImports(ctx); err != nil {
		return nil, err
	}
	return i.repo.ListImports(ctx, actor, before)
}
func (i *Importer) CancelImport(ctx context.Context, actor, id uuid.UUID) error {
	return i.repo.WithTx(ctx, func(tx *storage.ChallengeRepository) error {
		row, err := tx.ImportRecord(ctx, id, actor, true)
		if err != nil {
			return err
		}
		if row.Status == "imported" {
			return fmt.Errorf("已导入版本不能通过取消预览撤销")
		}
		row.Status = "cancelled"
		row.Payload = nil
		return tx.SaveImport(ctx, row)
	})
}
func (i *Importer) ExportVersion(ctx context.Context, id uuid.UUID, format string) ([]byte, string, error) {
	doc, err := challenge.NewService(i.repo, i.registry).VersionDocument(ctx, id)
	if err != nil {
		return nil, "", err
	}
	p, err := portableFromDocument(doc)
	if err != nil {
		return nil, "", err
	}
	name := p.Manifest.ID + "-" + p.Manifest.Version
	if !safeArchivePath(name) || strings.Contains(name, "/") {
		return nil, "", fmt.Errorf("玩法身份不能用于安全文件名")
	}
	if format == "zip" {
		raw, err := encodeArchive(p)
		return raw, name + ".zip", err
	}
	if format != "json" && format != "" {
		return nil, "", fmt.Errorf("导出格式仅支持 json 或 zip")
	}
	raw, err := json.MarshalIndent(p, "", "  ")
	if len(raw) > MaxUploadBytes {
		return nil, "", fmt.Errorf("导出 JSON 超过 16 MiB")
	}
	return raw, name + ".fatecraft.json", err
}
