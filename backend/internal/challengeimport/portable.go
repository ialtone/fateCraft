package challengeimport

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/challenge"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/jsonstrict"
	"github.com/ialtone/fatecraft/internal/storage"
)

const PackageFormat = "fatecraft.gameplay-package"
const MaxUploadBytes = 16 << 20

type PortableAsset struct {
	Kind      string          `json:"kind"`
	Slug      string          `json:"slug"`
	Name      string          `json:"name"`
	Content   json.RawMessage `json:"content"`
	SortOrder int             `json:"sort_order"`
}
type Provenance struct {
	Author          string `json:"author,omitempty"`
	License         string `json:"license,omitempty"`
	UpstreamID      string `json:"upstream_id,omitempty"`
	UpstreamVersion string `json:"upstream_version,omitempty"`
	UpstreamHash    string `json:"upstream_hash,omitempty"`
	Note            string `json:"note,omitempty"`
}
type PortablePackage struct {
	Format                 string                   `json:"format"`
	FormatVersion          int                      `json:"format_version"`
	RuntimeContractVersion int                      `json:"runtime_contract_version"`
	Manifest               challenge.Manifest       `json:"manifest"`
	Runtime                gameplay.RuntimeContract `json:"runtime"`
	Assets                 []PortableAsset          `json:"assets"`
	Definitions            []gameplayDefinitionFile `json:"definitions"`
	Outcomes               []challenge.OutcomeSpec  `json:"outcomes"`
	ContentHash            string                   `json:"content_hash,omitempty"`
	Provenance             *Provenance              `json:"provenance,omitempty"`
}

func portableFromDocument(doc *challenge.VersionDocument) (*PortablePackage, error) {
	var manifest challenge.Manifest
	if err := jsonstrict.Decode(doc.Version.Manifest, &manifest); err != nil {
		return nil, err
	}
	contract, err := storage.DecodeGameplayRuntime(doc.Runtime)
	if err != nil {
		return nil, err
	}
	p := &PortablePackage{Format: PackageFormat, FormatVersion: 1, RuntimeContractVersion: 2, Manifest: manifest, Runtime: *contract, Assets: []PortableAsset{}, Definitions: []gameplayDefinitionFile{}, Outcomes: []challenge.OutcomeSpec{}}
	p.Manifest.Outcomes = nil
	if len(doc.Version.Provenance) > 0 {
		raw, err := json.Marshal(doc.Version.Provenance)
		if err != nil {
			return nil, err
		}
		p.Provenance = &Provenance{}
		if err := json.Unmarshal(raw, p.Provenance); err != nil {
			return nil, err
		}
	}
	for _, a := range doc.Assets {
		p.Assets = append(p.Assets, PortableAsset{a.Kind, a.Slug, a.Name, json.RawMessage(a.Content), a.SortOrder})
	}
	for _, d := range doc.Definitions {
		p.Definitions = append(p.Definitions, gameplayDefinitionFile{d.Kind, d.Key, d.Name, d.Content, d.Tags, d.SearchText, d.Visibility, d.SortOrder})
	}
	for _, o := range doc.Outcomes {
		enabled := o.Enabled
		p.Outcomes = append(p.Outcomes, challenge.OutcomeSpec{RewardPoints: o.RewardPoints, OutcomeKey: o.OutcomeKey, Category: o.Category, Title: o.Title, Tone: o.Tone, VerifierKey: o.VerifierKey, VerifierConfig: o.VerifierConfig, Enabled: &enabled})
	}
	p.ContentHash, err = challenge.ContentHashDocument(doc)
	if err != nil {
		return nil, err
	}
	if doc.Version.ContentHash != "" && p.ContentHash != doc.Version.ContentHash {
		return nil, fmt.Errorf("版本内容与固定哈希不一致")
	}
	return p, nil
}

func compilePortable(p *PortablePackage, registry *gameplay.Registry) (*loadedChallenge, error) {
	if p.Format != PackageFormat || p.FormatVersion != 1 || p.RuntimeContractVersion != 2 {
		return nil, packageError("package_format_unsupported", "不支持的玩法包格式或契约版本")
	}
	if registry != nil {
		if _, err := registry.Module(p.Runtime.Engine); err != nil {
			return nil, packageError("gameplay_module_unavailable", "%v", err)
		}
	}
	if len(p.Manifest.Outcomes) != 0 {
		return nil, fmt.Errorf("便携 manifest 不得重复声明 outcomes")
	}
	if len(p.Manifest.Dependencies) != 0 {
		return nil, packageError("package_dependency_unsupported", "首期不支持跨玩法包 dependencies")
	}
	if p.Manifest.RuntimeEntry != "" && (!safeArchivePath(p.Manifest.RuntimeEntry) || !strings.HasPrefix(p.Manifest.RuntimeEntry, "runtime/") || path.Ext(p.Manifest.RuntimeEntry) != ".json") {
		return nil, fmt.Errorf("runtime_entry 必须是包内 runtime/ 下的安全 JSON 路径")
	}
	if _, err := parseStableVersion(p.Manifest.Version); err != nil {
		return nil, err
	}
	if len(p.Assets)+len(p.Definitions)+len(p.Outcomes) > 4096 {
		return nil, fmt.Errorf("内容集合超过上限")
	}
	manifest, err := json.Marshal(p.Manifest)
	if err != nil {
		return nil, err
	}
	runtime, err := storage.EncodeGameplayRuntime(p.Runtime)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	doc := challenge.VersionDocument{Version: storage.ChallengeVersion{Version: p.Manifest.Version, Manifest: storage.JSONDoc(manifest)}, Runtime: runtime, Assets: []storage.ChallengeAsset{}, Definitions: []storage.GameplayDefinition{}, Outcomes: []storage.ChallengeOutcomeRule{}}
	if p.Provenance != nil {
		raw, err := json.Marshal(p.Provenance)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &doc.Version.Provenance); err != nil {
			return nil, err
		}
	}
	for _, a := range p.Assets {
		doc.Assets = append(doc.Assets, storage.ChallengeAsset{ID: uuid.New(), Kind: a.Kind, Slug: a.Slug, Name: a.Name, Content: storage.JSONDoc(a.Content), SortOrder: a.SortOrder, CreatedAt: now, UpdatedAt: now})
	}
	for _, d := range p.Definitions {
		doc.Definitions = append(doc.Definitions, storage.GameplayDefinition{ID: uuid.New(), Kind: d.Kind, Key: d.Key, Name: d.Name, Content: storage.JSONMap(d.Content), Tags: d.Tags, SearchText: d.SearchText, Visibility: d.Visibility, SortOrder: d.SortOrder, CreatedAt: now, UpdatedAt: now})
	}
	for _, o := range p.Outcomes {
		doc.Outcomes = append(doc.Outcomes, storage.ChallengeOutcomeRule{ID: uuid.New(), OutcomeKey: o.OutcomeKey, Category: o.Category, Title: o.Title, Tone: o.Tone, VerifierKey: o.VerifierKey, VerifierConfig: storage.JSONMap(o.VerifierConfig), RewardPoints: o.RewardPoints, Enabled: o.Enabled == nil || *o.Enabled, CreatedAt: now, UpdatedAt: now})
	}
	if err := challenge.ValidateDocument(&doc, registry); err != nil {
		return nil, err
	}
	hash, err := challenge.ContentHashDocument(&doc)
	if err != nil {
		return nil, err
	}
	if p.ContentHash != "" && p.ContentHash != hash {
		return nil, packageError("package_hash_mismatch", "content_hash 与实际内容不一致")
	}
	return &loadedChallenge{manifest: p.Manifest, document: doc, hash: hash}, nil
}

func decodePortable(raw []byte, registry *gameplay.Registry) (*loadedChallenge, *PortablePackage, error) {
	if len(raw) > MaxUploadBytes {
		return nil, nil, fmt.Errorf("上传文件超过 16 MiB")
	}
	var p PortablePackage
	if err := jsonstrict.Decode(raw, &p); err != nil {
		return nil, nil, err
	}
	loaded, err := compilePortable(&p, registry)
	return loaded, &p, err
}
