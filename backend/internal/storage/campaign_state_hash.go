package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
)

// CampaignStateHash hashes the authoritative campaign header and every table
// that feeds the player projection. It deliberately excludes wall-clock
// columns and random_seed so the same logical state always produces the same
// hash. Table names come from compiled storage metadata, never from content.
func CampaignStateHash(ctx context.Context, tx *gorm.DB, campaign Campaign) (string, error) {
	if tx == nil {
		return "", fmt.Errorf("计算 Campaign 状态哈希缺少数据库事务")
	}
	header := map[string]any{
		"challenge_version_id":   campaign.ChallengeVersionID.String(),
		"challenge_hash":         campaign.ChallengeHash,
		"status":                 campaign.Status,
		"outcome_key":            campaign.OutcomeKey,
		"completed_at_game_time": campaign.CompletedAtGameTime,
		"current_location_key":   campaign.CurrentLocationKey,
		"game_time":              campaign.GameTime,
	}
	headerRaw, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte("fatecraft.campaign.state.v1\n"))
	_, _ = digest.Write(headerRaw)
	for _, table := range campaignCheckpointTables {
		var raw string
		query := "SELECT COALESCE(jsonb_agg(to_jsonb(r)), '[]'::jsonb)::text FROM " + table + " r WHERE campaign_id = ?"
		if err := tx.WithContext(ctx).Raw(query, campaign.ID).Scan(&raw).Error; err != nil {
			return "", fmt.Errorf("读取 %s 状态哈希: %w", table, err)
		}
		if strings.TrimSpace(raw) == "" {
			raw = "[]"
		}
		documents, err := canonicalCampaignDocuments(raw)
		if err != nil {
			return "", fmt.Errorf("规范化 %s 状态哈希: %w", table, err)
		}
		_, _ = digest.Write([]byte("\n" + table + "\n"))
		for _, document := range documents {
			_, _ = digest.Write([]byte(document))
			_, _ = digest.Write([]byte("\n"))
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func canonicalCampaignDocuments(raw string) ([]string, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var values []any
	if err := decoder.Decode(&values); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		document, ok := value.(map[string]any)
		if !ok {
			continue
		}
		delete(document, "created_at")
		delete(document, "updated_at")
		encoded, err := json.Marshal(document)
		if err != nil {
			return nil, err
		}
		out = append(out, string(encoded))
	}
	sort.Strings(out)
	return out, nil
}
