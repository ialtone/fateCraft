package challengeimport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/challenge"
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

// loadDirectory 只负责把一个仓库目录解析成可发布版本文档，不访问数据库。
func loadDirectory(directory string, now time.Time) (*loadedChallenge, error) {
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
	rewardSpecs := append([]challenge.RewardSpec(nil), manifest.RewardRules...)
	manifest.RewardRules = nil
	canonicalManifest, err := json.Marshal(manifest)
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

	rules := make([]storage.ChallengeRewardRule, 0, len(rewardSpecs))
	for _, spec := range rewardSpecs {
		enabled := spec.Enabled == nil || *spec.Enabled
		rules = append(rules, storage.ChallengeRewardRule{
			ID: uuid.New(), RewardKey: spec.RewardKey, EndingType: spec.EndingType,
			EndingTitle: spec.EndingTitle, Conditions: storage.JSONMap(spec.Conditions),
			Calculation: storage.JSONMap(spec.Calculation), MaxReward: spec.MaxReward,
			Enabled: enabled, CreatedAt: now, UpdatedAt: now,
		})
	}
	document := challenge.VersionDocument{
		Version: storage.ChallengeVersion{
			Version: manifest.Version, Manifest: storage.JSONDoc(canonicalManifest),
		},
		Assets: assets, RewardRules: rules,
	}
	if err := challenge.ValidateDocument(&document); err != nil {
		return nil, err
	}
	hash, err := challenge.ContentHash(document.Version.Manifest, document.Assets, document.RewardRules)
	if err != nil {
		return nil, err
	}
	return &loadedChallenge{manifest: manifest, document: document, hash: hash}, nil
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
