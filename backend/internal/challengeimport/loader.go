package challengeimport

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/challenge"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/storage"
)

type loadedChallenge struct {
	manifest challenge.Manifest
	document challenge.VersionDocument
	hash     string
}

var assetSources = []struct {
	directory string
	kind      string
}{
	{"characters", storage.ChallengeAssetCharacter},
	{"personas", storage.ChallengeAssetPersona},
	{"scenarios", storage.ChallengeAssetScenario},
	{"prompts", storage.ChallengeAssetPrompt},
	{"worldbooks", storage.ChallengeAssetWorldBook},
}

type gameplayDefinitionFile struct {
	Kind       string         `json:"kind"`
	Key        string         `json:"key"`
	Name       string         `json:"name"`
	Content    map[string]any `json:"content"`
	Tags       []string       `json:"tags"`
	SearchText string         `json:"search_text"`
	Visibility string         `json:"visibility"`
	SortOrder  int            `json:"sort_order"`
}

const runtimeSourceContractVersion = 2

type runtimeSourceEntry struct {
	Schema          string                `json:"$schema"`
	ContractVersion int                   `json:"runtime_contract_version"`
	Engine          gameplay.EngineRef    `json:"engine"`
	Mode            string                `json:"mode"`
	Includes        runtimeSourceIncludes `json:"includes"`
}

type runtimeSourceIncludes struct {
	Campaign     string `json:"campaign,omitempty"`
	StateSchema  string `json:"state_schema"`
	Actions      string `json:"actions"`
	Settlement   string `json:"settlement"`
	Presentation string `json:"presentation"`
}

type runtimeCampaignFragment struct {
	Campaign *gameplay.CampaignConfig `json:"campaign"`
}

type runtimeStateSchemaFragment struct {
	StateSchema *gameplay.StateSchema `json:"state_schema"`
}

type runtimeActionsFragment struct {
	Actions *gameplay.ActionConfig `json:"actions"`
}

type runtimeSettlementFragment struct {
	Settlement *gameplay.SettlementConfig `json:"settlement"`
}

type runtimePresentationFragment struct {
	Presentation *gameplay.Presentation `json:"presentation"`
}

// loadDirectory only parses repository files. It does not access PostgreSQL.
func loadDirectory(directory string, now time.Time, registries ...*gameplay.Registry) (*loadedChallenge, error) {
	registry := defaultRegistry()
	if len(registries) > 0 && registries[0] != nil {
		registry = registries[0]
	}
	manifestBytes, err := os.ReadFile(filepath.Join(directory, "pack.json"))
	if err != nil {
		return nil, fmt.Errorf("读取 pack.json: %w", err)
	}
	var manifest challenge.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("解析 pack.json: %w", err)
	}
	if _, err := parseStableVersion(manifest.Version); err != nil {
		return nil, fmt.Errorf("内置挑战 version 无效: %w", err)
	}
	outcomeSpecs := append([]challenge.OutcomeSpec(nil), manifest.Outcomes...)
	manifest.Outcomes = nil
	canonicalManifest, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(manifest.RuntimeEntry) == "" {
		return nil, fmt.Errorf("pack.json 缺少 runtime_entry")
	}
	runtimeContract, err := loadRuntimeSource(directory, manifest.RuntimeEntry)
	if err != nil {
		return nil, err
	}
	if err := registry.ValidateRuntime(runtimeContract); err != nil {
		return nil, fmt.Errorf("Runtime 源文件无效: %w", err)
	}
	runtime, err := storage.EncodeGameplayRuntime(*runtimeContract)
	if err != nil {
		return nil, fmt.Errorf("编译 Runtime 源文件: %w", err)
	}
	runtime.CreatedAt = now
	runtime.UpdatedAt = now

	if _, err := os.Stat(filepath.Join(directory, "rewards.json")); err == nil {
		return nil, fmt.Errorf("不再支持 rewards.json，请在 outcomes 中配置 reward_points")
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	gameplayDefinitions, err := loadGameplayDefinitionCatalog(directory, now)
	if err != nil {
		return nil, err
	}
	assets := make([]storage.ChallengeAsset, 0)
	for _, source := range assetSources {
		files, err := filepath.Glob(filepath.Join(directory, source.directory, "*.json"))
		if err != nil {
			return nil, err
		}
		sort.Strings(files)
		for index, file := range files {
			raw, err := os.ReadFile(file)
			if err != nil {
				return nil, fmt.Errorf("读取 %s: %w", file, err)
			}
			var value map[string]any
			if err := json.Unmarshal(raw, &value); err != nil {
				return nil, fmt.Errorf("解析 %s: %w", file, err)
			}
			slug, _ := value["id"].(string)
			name, _ := value["name"].(string)
			canonical, err := json.Marshal(value)
			if err != nil {
				return nil, err
			}
			assets = append(assets, storage.ChallengeAsset{
				ID: uuid.New(), Kind: source.kind, Slug: slug, Name: name,
				Content: storage.JSONDoc(canonical), SortOrder: index,
				CreatedAt: now, UpdatedAt: now,
			})
		}
	}

	outcomes := make([]storage.ChallengeOutcomeRule, 0, len(outcomeSpecs))
	for _, spec := range outcomeSpecs {
		enabled := spec.Enabled == nil || *spec.Enabled
		outcome := storage.ChallengeOutcomeRule{
			ID: uuid.New(), OutcomeKey: spec.OutcomeKey, Category: spec.Category,
			Title: spec.Title, Tone: spec.Tone, VerifierKey: spec.VerifierKey, RewardPoints: spec.RewardPoints,
			VerifierConfig: storage.JSONMap(spec.VerifierConfig), Enabled: enabled,
			CreatedAt: now, UpdatedAt: now,
		}
		outcomes = append(outcomes, outcome)
	}

	document := challenge.VersionDocument{
		Version: storage.ChallengeVersion{
			Version: manifest.Version, Manifest: storage.JSONDoc(canonicalManifest),
		},
		Runtime: runtime, Assets: assets, Definitions: gameplayDefinitions,
		Outcomes: outcomes,
	}
	if err := challenge.ValidateDocument(&document, registry); err != nil {
		return nil, err
	}
	hash, err := challenge.ContentHashDocument(&document)
	if err != nil {
		return nil, err
	}
	return &loadedChallenge{manifest: manifest, document: document, hash: hash}, nil
}

func loadRuntimeSource(packDirectory, entryReference string) (*gameplay.RuntimeContract, error) {
	entryPath, err := resolveRuntimeSourcePath(packDirectory, entryReference)
	if err != nil {
		return nil, fmt.Errorf("runtime_entry 无效: %w", err)
	}
	var entry runtimeSourceEntry
	if err := decodeJSONFileStrict(entryPath, &entry); err != nil {
		return nil, fmt.Errorf("读取 Runtime 入口 %s: %w", filepath.ToSlash(entryReference), err)
	}
	if entry.Schema != "fatecraft.runtime.schema.json" {
		return nil, fmt.Errorf("Runtime 入口 $schema 无效")
	}
	if entry.ContractVersion != runtimeSourceContractVersion {
		return nil, fmt.Errorf("Runtime 契约版本必须为 %d", runtimeSourceContractVersion)
	}
	if entry.Mode != gameplay.RuntimeModeChallenge && entry.Mode != gameplay.RuntimeModeCampaign {
		return nil, fmt.Errorf("Runtime mode 无效: %q", entry.Mode)
	}
	entryDirectory := filepath.Dir(entryPath)
	required := map[string]string{
		"state_schema": entry.Includes.StateSchema,
		"actions":      entry.Includes.Actions,
		"settlement":   entry.Includes.Settlement,
		"presentation": entry.Includes.Presentation,
	}
	for name, reference := range required {
		if strings.TrimSpace(reference) == "" {
			return nil, fmt.Errorf("Runtime includes.%s 不能为空", name)
		}
	}
	mode := gameplay.NormalizeRuntimeMode(entry.Mode)
	if mode == gameplay.RuntimeModeCampaign && strings.TrimSpace(entry.Includes.Campaign) == "" {
		return nil, fmt.Errorf("Campaign Runtime 必须声明 includes.campaign")
	}
	if mode != gameplay.RuntimeModeCampaign && entry.Includes.Campaign != "" {
		return nil, fmt.Errorf("非 Campaign Runtime 不能声明 includes.campaign")
	}

	paths := map[string]string{}
	for name, reference := range map[string]string{
		"campaign":     entry.Includes.Campaign,
		"state_schema": entry.Includes.StateSchema,
		"actions":      entry.Includes.Actions,
		"settlement":   entry.Includes.Settlement,
		"presentation": entry.Includes.Presentation,
	} {
		if reference == "" {
			continue
		}
		path, err := resolveRuntimeSourcePath(entryDirectory, reference)
		if err != nil {
			return nil, fmt.Errorf("Runtime includes.%s 无效: %w", name, err)
		}
		for existingName, existingPath := range paths {
			if existingPath == path {
				return nil, fmt.Errorf("Runtime includes.%s 与 includes.%s 不能引用同一文件", name, existingName)
			}
		}
		paths[name] = path
	}

	contract := &gameplay.RuntimeContract{Engine: entry.Engine, Mode: mode}
	if path := paths["campaign"]; path != "" {
		var fragment runtimeCampaignFragment
		if err := decodeJSONFileStrict(path, &fragment); err != nil {
			return nil, fmt.Errorf("读取 Runtime campaign: %w", err)
		}
		if fragment.Campaign == nil {
			return nil, fmt.Errorf("Runtime campaign 文件缺少 campaign 对象")
		}
		contract.Campaign = fragment.Campaign
	}
	var stateFragment runtimeStateSchemaFragment
	if err := decodeJSONFileStrict(paths["state_schema"], &stateFragment); err != nil {
		return nil, fmt.Errorf("读取 Runtime state_schema: %w", err)
	}
	if stateFragment.StateSchema == nil {
		return nil, fmt.Errorf("Runtime state_schema 文件缺少 state_schema 对象")
	}
	contract.StateSchema = *stateFragment.StateSchema
	var actionsFragment runtimeActionsFragment
	if err := decodeJSONFileStrict(paths["actions"], &actionsFragment); err != nil {
		return nil, fmt.Errorf("读取 Runtime actions: %w", err)
	}
	if actionsFragment.Actions == nil {
		return nil, fmt.Errorf("Runtime actions 文件缺少 actions 对象")
	}
	contract.Actions = *actionsFragment.Actions
	var settlementFragment runtimeSettlementFragment
	if err := decodeJSONFileStrict(paths["settlement"], &settlementFragment); err != nil {
		return nil, fmt.Errorf("读取 Runtime settlement: %w", err)
	}
	if settlementFragment.Settlement == nil {
		return nil, fmt.Errorf("Runtime settlement 文件缺少 settlement 对象")
	}
	contract.Settlement = *settlementFragment.Settlement
	var presentationFragment runtimePresentationFragment
	if err := decodeJSONFileStrict(paths["presentation"], &presentationFragment); err != nil {
		return nil, fmt.Errorf("读取 Runtime presentation: %w", err)
	}
	if presentationFragment.Presentation == nil {
		return nil, fmt.Errorf("Runtime presentation 文件缺少 presentation 对象")
	}
	contract.Presentation = *presentationFragment.Presentation
	return contract, nil
}

func resolveRuntimeSourcePath(allowedDirectory, reference string) (string, error) {
	if reference == "" || strings.TrimSpace(reference) != reference || filepath.IsAbs(reference) ||
		filepath.VolumeName(reference) != "" || strings.Contains(reference, `\`) ||
		strings.ToLower(filepath.Ext(reference)) != ".json" {
		return "", fmt.Errorf("必须是玩法包内的相对 JSON 路径")
	}
	cleanReference := filepath.Clean(filepath.FromSlash(reference))
	if cleanReference == "." || cleanReference == ".." || strings.HasPrefix(cleanReference, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("路径不能离开玩法包")
	}
	allowedRoot, err := filepath.Abs(allowedDirectory)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(allowedDirectory, cleanReference))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(allowedRoot, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("路径不能离开玩法包")
	}
	return target, nil
}

func decodeJSONFileStrict(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 2<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("只能包含一个 JSON 值")
		}
		return err
	}
	return nil
}

func loadGameplayDefinitionCatalog(directory string, now time.Time) ([]storage.GameplayDefinition, error) {
	files := []string{filepath.Join(directory, "definitions.json")}
	fragments, err := filepath.Glob(filepath.Join(directory, "definitions", "*.json"))
	if err != nil {
		return nil, fmt.Errorf("枚举 definitions 分片: %w", err)
	}
	sort.Strings(fragments)
	files = append(files, fragments...)
	definitions := make([]storage.GameplayDefinition, 0)
	for _, file := range files {
		rows, err := loadGameplayDefinitions(file, now)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, rows...)
	}
	return definitions, nil
}

func loadGameplayDefinitions(path string, now time.Time) ([]storage.GameplayDefinition, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取 %s: %w", path, err)
	}
	var values []gameplayDefinitionFile
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", path, err)
	}
	out := make([]storage.GameplayDefinition, 0, len(values))
	for _, value := range values {
		visibility := strings.TrimSpace(value.Visibility)
		if visibility == "" {
			visibility = storage.CampaignVisibilityPlayer
		}
		out = append(out, storage.GameplayDefinition{
			ID: uuid.New(), Kind: value.Kind, Key: value.Key, Name: value.Name,
			Content: storage.JSONMap(value.Content), Tags: value.Tags,
			SearchText: value.SearchText, Visibility: visibility, SortOrder: value.SortOrder,
			CreatedAt: now, UpdatedAt: now,
		})
	}
	return out, nil
}

func parseStableVersion(version string) ([3]int, error) {
	var parsed [3]int
	parts := strings.Split(version, ".")
	if len(parts) != len(parsed) {
		return parsed, fmt.Errorf("%q 必须使用 major.minor.patch", version)
	}
	for index, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return parsed, fmt.Errorf("%q 不是稳定版本号", version)
		}
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return parsed, fmt.Errorf("%q 不是稳定版本号", version)
		}
		parsed[index] = value
	}
	return parsed, nil
}

func compareStableVersions(left, right string) (int, error) {
	a, err := parseStableVersion(left)
	if err != nil {
		return 0, err
	}
	b, err := parseStableVersion(right)
	if err != nil {
		return 0, err
	}
	for index := range a {
		if a[index] < b[index] {
			return -1, nil
		}
		if a[index] > b[index] {
			return 1, nil
		}
	}
	return 0, nil
}
