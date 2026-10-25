package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

var ErrCampaignCheckpointMissing = errors.New("长期存档缺少该回合的完整检查点，无法恢复")

// The list is compiled storage metadata, never supplied by a module or a
// content pack. Parent tables precede dependants; restore deletes in reverse.
var campaignCheckpointTables = []string{
	"campaign_runtime_states", "campaign_characters", "campaign_techniques",
	"campaign_item_stacks", "campaign_item_instances", "campaign_known_recipes",
	"campaign_market_states", "campaign_market_stocks", "campaign_location_discoveries",
	"campaign_npcs", "campaign_relationships", "campaign_faction_standings",
	"campaign_scheduled_events", "campaign_region_states", "campaign_quests", "campaign_encounters",
	"campaign_stage_progress",
}

type campaignCheckpointData struct {
	Version     int                        `json:"version"`
	CampaignID  uuid.UUID                  `json:"campaign_id"`
	ContentHash string                     `json:"content_hash"`
	GameTime    int64                      `json:"game_time"`
	Status      string                     `json:"status"`
	Outcome     string                     `json:"outcome"`
	CompletedAt *int64                     `json:"completed_at"`
	Location    string                     `json:"location"`
	Tables      map[string]json.RawMessage `json:"tables"`
}
type CampaignCheckpoint struct {
	TurnID     uuid.UUID `gorm:"type:uuid;primaryKey"`
	CampaignID uuid.UUID `gorm:"type:uuid"`
	Snapshot   JSONDoc   `gorm:"type:jsonb"`
}

func captureCampaignCheckpoint(tx *gorm.DB, conv Conversation, turnID uuid.UUID) error {
	if conv.CampaignID == nil {
		return nil
	}
	var row Campaign
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", *conv.CampaignID).Error; err != nil {
		return err
	}
	snapshot := campaignCheckpointData{Version: 1, CampaignID: row.ID, ContentHash: row.ChallengeHash, GameTime: row.GameTime, Status: row.Status, Outcome: row.OutcomeKey, CompletedAt: row.CompletedAtGameTime, Location: row.CurrentLocationKey, Tables: map[string]json.RawMessage{}}
	for _, table := range campaignCheckpointTables {
		var raw string
		if err := tx.Raw("SELECT COALESCE(jsonb_agg(to_jsonb(r)), '[]'::jsonb)::text FROM (SELECT * FROM "+table+" WHERE campaign_id = ?) r", row.ID).Scan(&raw).Error; err != nil {
			return err
		}
		snapshot.Tables[table] = json.RawMessage(raw)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if len(raw) > 4*1024*1024 {
		return fmt.Errorf("长期玩法检查点超过 4 MiB 上限")
	}
	return tx.Create(&CampaignCheckpoint{TurnID: turnID, CampaignID: row.ID, Snapshot: JSONDoc(raw)}).Error
}

func restoreCampaignCheckpoint(tx *gorm.DB, conv Conversation, turnID uuid.UUID) error {
	if conv.CampaignID == nil {
		return nil
	}
	var row Campaign
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", *conv.CampaignID).Error; err != nil {
		return err
	}
	var checkpoint CampaignCheckpoint
	if err := tx.First(&checkpoint, "turn_id = ? AND campaign_id = ?", turnID, row.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrCampaignCheckpointMissing
		}
		return err
	}
	var snapshot campaignCheckpointData
	if err := json.Unmarshal(checkpoint.Snapshot, &snapshot); err != nil {
		return err
	}
	if snapshot.Version != 1 || snapshot.CampaignID != row.ID || snapshot.ContentHash != row.ChallengeHash || len(snapshot.Tables) != len(campaignCheckpointTables) {
		return fmt.Errorf("长期检查点格式或归属不匹配")
	}
	if row.Revision >= 9007199254740990 || snapshot.GameTime < 0 || snapshot.GameTime > row.GameTime {
		return fmt.Errorf("检查点时间或修订范围无效")
	}
	var moduleRows []CampaignRuntimeState
	if err := json.Unmarshal(snapshot.Tables["campaign_runtime_states"], &moduleRows); err != nil {
		return err
	}
	var currentModuleCount int64
	if err := tx.Model(&CampaignRuntimeState{}).Where("campaign_id = ?", row.ID).Count(&currentModuleCount).Error; err != nil {
		return err
	}
	if len(moduleRows) > 1 || int64(len(moduleRows)) != currentModuleCount {
		return fmt.Errorf("检查点与当前存档的模块存储格式不一致")
	}
	if len(moduleRows) == 1 {
		if err := validateCheckpointRuntime(snapshot, moduleRows[0].State, row.Revision); err != nil {
			return err
		}
	}
	for _, table := range campaignCheckpointTables {
		raw, ok := snapshot.Tables[table]
		if !ok {
			return fmt.Errorf("检查点缺少领域表 %s", table)
		}
		if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '[' {
			return fmt.Errorf("检查点领域表 %s 必须是行数组", table)
		}
		var entries []struct {
			CampaignID uuid.UUID `json:"campaign_id"`
		}
		if err := json.Unmarshal(raw, &entries); err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.CampaignID != row.ID {
				return fmt.Errorf("检查点包含其他存档的领域行")
			}
		}
	}
	// Account balances, ledgers, published rules, seeds and append-only history
	// are deliberately absent. A replay can never recreate reward eligibility.
	for index := len(campaignCheckpointTables) - 1; index >= 0; index-- {
		if err := tx.Exec("DELETE FROM "+campaignCheckpointTables[index]+" WHERE campaign_id = ?", row.ID).Error; err != nil {
			return err
		}
	}
	for _, table := range campaignCheckpointTables {
		if err := tx.Exec("INSERT INTO "+table+" SELECT * FROM jsonb_populate_recordset(NULL::"+table+", ?::jsonb)", string(snapshot.Tables[table])).Error; err != nil {
			return err
		}
	}
	revision := row.Revision + 1
	if err := tx.Model(&Campaign{}).Where("id = ?", row.ID).Updates(map[string]any{"revision": revision, "game_time": snapshot.GameTime, "status": snapshot.Status, "outcome_key": snapshot.Outcome, "completed_at_game_time": snapshot.CompletedAt, "current_location_key": snapshot.Location, "updated_at": time.Now()}).Error; err != nil {
		return err
	}
	if err := tx.Exec("UPDATE campaign_runtime_states SET state = jsonb_set(state, '{revision}', to_jsonb(?::bigint)) WHERE campaign_id = ?", revision, row.ID).Error; err != nil {
		return err
	}
	restored := row
	restored.Revision = revision
	restored.GameTime = snapshot.GameTime
	restored.Status = snapshot.Status
	restored.OutcomeKey = snapshot.Outcome
	restored.CompletedAtGameTime = snapshot.CompletedAt
	restored.CurrentLocationKey = snapshot.Location
	hashContext := tx.Statement.Context
	if hashContext == nil {
		hashContext = context.Background()
	}
	stateHash, err := CampaignStateHash(hashContext, tx, restored)
	if err != nil {
		return err
	}
	return tx.Model(&Campaign{}).Where("id = ?", row.ID).Update("state_hash", stateHash).Error
}

// Domain-specific invariants are checked by the pinned module on projection
// and before every action. Restore first validates the immutable storage
// envelope, so malformed headers never become the current writable cursor.
func validateCheckpointRuntime(snapshot campaignCheckpointData, data JSONMap, revision int64) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if len(raw) > 64*1024 {
		return fmt.Errorf("检查点模块状态超过预算")
	}
	var run gameplay.RunState
	if err := json.Unmarshal(raw, &run); err != nil {
		return err
	}
	if run.SchemaVersion < 1 || run.Revision < 0 || run.Revision > revision || run.GameTime != snapshot.GameTime || run.Data == nil || run.OutcomeKey != snapshot.Outcome {
		return fmt.Errorf("检查点模块状态与存档头不一致")
	}
	if snapshot.Status == CampaignStatusActive {
		if run.Status != gameplay.RunActive || run.OutcomeKey != "" {
			return fmt.Errorf("活动检查点包含终局模块状态")
		}
	} else if (run.Status != gameplay.RunCompleted && run.Status != gameplay.RunFailed) || run.OutcomeKey == "" {
		return fmt.Errorf("终局检查点缺少模块完成事实")
	}
	return nil
}

// History remains append-only. Validity follows its originating turn, so a
// superseded branch cannot supply context or replay a successful old commit.
func activeCampaignEvents(db *gorm.DB) *gorm.DB {
	return db.Where("(campaign_events.turn_id IS NULL OR EXISTS (SELECT 1 FROM conversation_turns t WHERE t.id = campaign_events.turn_id AND t.invalidated_at IS NULL))")
}
