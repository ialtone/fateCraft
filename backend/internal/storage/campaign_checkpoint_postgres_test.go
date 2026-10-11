package storage

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/testdb"
	"gorm.io/gorm"
)

func TestPostgresCampaignCheckpointRestoresDependentTablesAtomically(t *testing.T) {
	db := testdb.Open(t, "relational_checkpoint")
	id := seedGenerationTestConversation(t, db)
	var conv Conversation
	if err := db.First(&conv, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	row := Campaign{ID: uuid.New(), PlayerID: uuid.MustParse(conv.PlayerID), ChallengeVersionID: conv.ChallengeVersionID, ChallengeHash: conv.ChallengeHash, Title: "Checkpoint", Status: CampaignStatusActive, LLMRoute: LLMRouteServer, CurrentLocationKey: "start", RandomSeed: 1}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	conv.CampaignID = &row.ID
	if err := db.Model(&Conversation{}).Where("id = ?", id).Update("campaign_id", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	character, npc, item := uuid.New(), uuid.New(), uuid.New()
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO campaign_characters (id,campaign_id,realm_key,stage_key,realm_rank,cultivation_required,lifespan_days,health,base_max_health,max_health,mana,base_max_mana,max_mana,spiritual_sense,base_max_spiritual_sense,max_spiritual_sense) VALUES (?,?,'mortal','early',0,100,10000,10,10,10,10,10,10,10,10,10)`, []any{character, row.ID}},
		{`INSERT INTO campaign_item_instances (id,campaign_id,definition_key,durability,max_durability,bound_character_id) VALUES (?,?,'sword',10,10,?)`, []any{item, row.ID, character}},
		{`INSERT INTO campaign_npcs (id,campaign_id,archetype_key,instance_key,stable_name,realm_key,stage_key,current_location_key) VALUES (?,?,'merchant','merchant','商人','','','start')`, []any{npc, row.ID}},
		{`INSERT INTO campaign_relationships (campaign_id,npc_id,trust) VALUES (?,?,10)`, []any{row.ID, npc}},
		{`INSERT INTO campaign_market_states (campaign_id,market_key,location_key) VALUES (?,'market','start')`, []any{row.ID}},
		{`INSERT INTO campaign_market_stocks (campaign_id,market_key,item_kind,item_key,quantity) VALUES (?,'market','item','food',3)`, []any{row.ID}},
	} {
		if err := db.Exec(statement.sql, statement.args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	turn, err := NewTurnRepo(db).BeginUserTurn(context.Background(), id, "user_input", "test checkpoint", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE campaigns SET revision=1, game_time=60 WHERE id=?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE campaign_relationships SET trust=50 WHERE campaign_id=?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE campaign_market_stocks SET quantity=1 WHERE campaign_id=?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	var checkpoint CampaignCheckpoint
	if err := db.First(&checkpoint, "turn_id=?", turn.Turn.ID).Error; err != nil {
		t.Fatal(err)
	}
	valid := append(JSONDoc(nil), checkpoint.Snapshot...)
	var broken campaignCheckpointData
	if err := json.Unmarshal(valid, &broken); err != nil {
		t.Fatal(err)
	}
	var stocks []map[string]any
	if err := json.Unmarshal(broken.Tables["campaign_market_stocks"], &stocks); err != nil {
		t.Fatal(err)
	}
	stocks[0]["market_key"] = "missing-parent"
	broken.Tables["campaign_market_stocks"], _ = json.Marshal(stocks)
	raw, _ := json.Marshal(broken)
	if err := db.Model(&CampaignCheckpoint{}).Where("turn_id=?", turn.Turn.ID).Update("snapshot", JSONDoc(raw)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return restoreCampaignCheckpoint(tx, conv, turn.Turn.ID) }); err == nil {
		t.Fatal("invalid foreign key restored")
	}
	assertValue := func(sql string, expected int64) {
		t.Helper()
		var value int64
		if err := db.Raw(sql, row.ID).Scan(&value).Error; err != nil {
			t.Fatal(err)
		}
		if value != expected {
			t.Fatalf("%s = %d, want %d", sql, value, expected)
		}
	}
	assertValue("SELECT trust FROM campaign_relationships WHERE campaign_id=?", 50)
	assertValue("SELECT quantity FROM campaign_market_stocks WHERE campaign_id=?", 1)
	assertValue("SELECT revision FROM campaigns WHERE id=?", 1)
	if err := db.Model(&CampaignCheckpoint{}).Where("turn_id=?", turn.Turn.ID).Update("snapshot", valid).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return restoreCampaignCheckpoint(tx, conv, turn.Turn.ID) }); err != nil {
		t.Fatal(err)
	}
	assertValue("SELECT trust FROM campaign_relationships WHERE campaign_id=?", 10)
	assertValue("SELECT quantity FROM campaign_market_stocks WHERE campaign_id=?", 3)
	assertValue("SELECT game_time FROM campaigns WHERE id=?", 0)
	assertValue("SELECT revision FROM campaigns WHERE id=?", 2)
	assertValue("SELECT COUNT(*) FROM campaign_item_instances WHERE campaign_id=? AND bound_character_id IS NOT NULL", 1)
}
