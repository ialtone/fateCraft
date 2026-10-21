package challengeimport

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/testdb"
)

func TestPostgresExchangeAtomicDraftReplayAndConflict(t *testing.T) {
	db := testdb.Open(t, "exchange")
	ctx := context.Background()
	actor, other := uuid.New(), uuid.New()
	for index, id := range []uuid.UUID{actor, other} {
		if err := db.Exec("INSERT INTO users(id,username,display_name) VALUES(?,?,?)", id, "exchange-"+string(rune('a'+index)), "Admin").Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := storage.NewChallengeRepository(db)
	importer := New(repo, filepath.Join("..", "..", "data"))
	loaded, err := loadDirectory(filepath.Join("..", "..", "data", "preset_packs", "underground-watchtower"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	portable, err := portableFromDocument(&loaded.document)
	if err != nil {
		t.Fatal(err)
	}
	portable.Manifest.ID = "exchange-test"
	portable.ContentHash = ""
	raw, _ := json.Marshal(portable)
	preview, err := importer.PreviewUpload(ctx, actor, "test.fatecraft.json", raw, PreviewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetPackBySlug(ctx, "exchange-test"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("preview wrote pack: %v", err)
	}
	if _, err := importer.ConfirmUpload(ctx, other, preview.ID, uuid.New()); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("foreign confirmation: %v", err)
	}
	request := uuid.New()
	result, err := importer.ConfirmUpload(ctx, actor, preview.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := repo.ImportRecord(ctx, preview.ID, actor, false)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Status != "imported" || confirmed.Payload != nil || confirmed.RequestID == nil || *confirmed.RequestID != request {
		t.Fatalf("confirmation did not persist and clear its payload: status=%s payload=%s", confirmed.Status, confirmed.Payload)
	}
	replay, err := importer.ConfirmUpload(ctx, actor, preview.ID, request)
	if err != nil || replay.ResultVersionID == nil || *replay.ResultVersionID != *result.ResultVersionID {
		t.Fatalf("lost response replay: %+v %v", replay, err)
	}
	pack, err := repo.GetPackBySlug(ctx, "exchange-test")
	if err != nil {
		t.Fatal(err)
	}
	if pack.Status != storage.ChallengePackHidden || pack.CurrentVersionID != nil {
		t.Fatal("upload automatically opened pack")
	}
	version, err := repo.GetVersion(ctx, *result.ResultVersionID)
	if err != nil || version.Status != storage.ChallengeVersionDraft {
		t.Fatalf("import published: %+v %v", version, err)
	}
	if _, _, err := importer.ExportVersion(ctx, version.ID, "json"); err != nil {
		t.Fatalf("export imported draft: %v", err)
	}
	if _, _, err := importer.ExportVersion(ctx, version.ID, "zip"); err != nil {
		t.Fatalf("export imported draft as zip: %v", err)
	}
	again, err := importer.PreviewUpload(ctx, actor, "test.json", raw, PreviewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := importer.ConfirmUpload(ctx, actor, again.ID, request); !errors.Is(err, storage.ErrRevisionConflict) {
		t.Fatalf("request accepted for different preview: %v", err)
	}
	if _, err := importer.ConfirmUpload(ctx, actor, again.ID, uuid.New()); err != nil {
		t.Fatalf("same content draft not idempotent: %v", err)
	}
	stale, err := importer.PreviewUpload(ctx, actor, "test.json", raw, PreviewOptions{Strategy: "replace_draft"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&storage.ChallengeVersion{}).Where("id=?", version.ID).Update("revision", version.Revision+1).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := importer.ConfirmUpload(ctx, actor, stale.ID, uuid.New()); !errors.Is(err, ErrPreviewChanged) {
		t.Fatalf("stale draft overwritten: %v", err)
	}
	for _, status := range []string{"cancelled", "expired"} {
		t.Run(status, func(t *testing.T) {
			pending, err := importer.PreviewUpload(ctx, actor, "test.json", raw, PreviewOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if status == "cancelled" {
				if err := importer.CancelImport(ctx, actor, pending.ID); err != nil {
					t.Fatal(err)
				}
			} else if err := db.Model(&storage.GameplayImport{}).Where("id = ?", pending.ID).
				Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
				t.Fatal(err)
			}
			stored, err := importer.ImportRecord(ctx, actor, pending.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status != status || stored.Payload != nil {
				t.Fatalf("preview payload not cleared: status=%s payload=%s", stored.Status, stored.Payload)
			}
		})
	}
	builtin, err := importer.PreviewBuiltin(ctx, "underground-watchtower", actor)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repo.ImportRecord(ctx, builtin.ID, actor, false)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Source != "builtin" || len(stored.Payload) == 0 || stored.Status != "preview" {
		t.Fatal("builtin preview did not retain its source and payload")
	}
	if _, err := importer.ConfirmUpload(ctx, actor, builtin.ID, uuid.New()); err != nil {
		t.Fatalf("confirm builtin preview: %v", err)
	}
	stored, err = repo.ImportRecord(ctx, builtin.ID, actor, false)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Source != "builtin" || stored.Payload != nil || stored.Status != "imported" {
		t.Fatal("builtin confirmation did not retain its source and clear its payload")
	}
}

func TestPostgresOldVersionContinuesAndPauseFencesWrites(t *testing.T) {
	db := testdb.Open(t, "version_policy")
	ctx := context.Background()
	actor := uuid.New()
	if err := db.Exec("INSERT INTO users(id,username,display_name) VALUES(?,'policy-player','Player')", actor).Error; err != nil {
		t.Fatal(err)
	}
	repo := storage.NewChallengeRepository(db)
	importer := New(repo, filepath.Join("..", "..", "data"))
	initial, err := importer.ImportBuiltin(ctx, "underground-watchtower")
	if err != nil {
		t.Fatal(err)
	}
	pack, err := repo.GetPackBySlug(ctx, initial.Slug)
	if err != nil {
		t.Fatal(err)
	}
	conversation := storage.Conversation{ID: uuid.New(), PlayerID: actor.String(), Title: "Old save", Status: storage.StatusActive, ChallengeVersionID: initial.VersionID, ChallengeHash: initial.ContentHash, LLMRoute: storage.LLMRouteServer}
	if err := db.Create(&conversation).Error; err != nil {
		t.Fatal(err)
	}
	service := challenge.NewService(repo, defaultRegistry())
	draft, err := service.CreateDraft(ctx, pack.ID, actor, "2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PublishOptions(ctx, draft.Version.ID, actor, draft.Version.Revision, true); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&conversation, "id=?", conversation.ID).Error; err != nil {
		t.Fatal(err)
	}
	if conversation.Status != storage.StatusActive || conversation.ChallengeVersionID != initial.VersionID || conversation.ChallengeHash != initial.ContentHash {
		t.Fatal("publication changed old save")
	}
	if err := service.CheckSavedVersion(ctx, initial.VersionID, initial.ContentHash); err != nil {
		t.Fatalf("retired version cannot continue: %v", err)
	}
	message := storage.ChatMessage{ID: uuid.New(), ConversationID: conversation.ID, Role: storage.RoleAssistant, Content: "before pause", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := storage.NewMessageRepo(db).CreateChat(ctx, &message); err != nil {
		t.Fatal(err)
	}
	old, err := repo.GetVersion(ctx, initial.VersionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetRunPolicy(ctx, old.ID, actor, "blocked", "test pause", old.PolicyRevision); err != nil {
		t.Fatal(err)
	}
	message.ID = uuid.New()
	message.MessageSeq++
	message.Content = "must roll back"
	message.ContentBlocks = storage.JSONDoc(`[{"kind":"text","text":"must roll back"}]`)
	if err := db.Create(&message).Error; !errors.Is(storage.GameplayPolicyError(err), storage.ErrVersionBlocked) {
		t.Fatalf("pause did not fence write: %v", err)
	}
	var count int64
	if err := db.Model(&storage.ChatMessage{}).Where("conversation_id=?", conversation.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("pause changed committed messages: %d %v", count, err)
	}
	if err := repo.DeleteVersionIfUnused(ctx, old.ID); !errors.Is(err, storage.ErrVersionInUse) {
		t.Fatalf("referenced old snapshot deleted: %v", err)
	}
	if err := repo.SetRunPolicy(ctx, old.ID, actor, "allowed", "", old.PolicyRevision+1); err != nil {
		t.Fatal(err)
	}
	message.ID = uuid.New()
	if err := db.Create(&message).Error; err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	if err := db.Model(&storage.ChatMessage{}).Where("conversation_id=?", conversation.ID).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("resume did not append a message: %d %v", count, err)
	}
}
