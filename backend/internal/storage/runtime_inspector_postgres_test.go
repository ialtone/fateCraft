package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/testdb"
)

func TestPostgresRuntimeInspectorIgnoresUnsuccessfulAndInvalidatedAttempts(t *testing.T) {
	db := testdb.Open(t, "runtime_inspector")
	id := seedGenerationTestConversation(t, db)
	var conv Conversation
	if err := db.First(&conv, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	agents, logs := NewAgentRepo(db), NewContextRepo(db)
	var successfulAttempt, successfulLog, successfulTurn uuid.UUID
	for i, status := range []string{AttemptStatusCompleted, AttemptStatusFailed, AttemptStatusPartial, AttemptStatusCancelled, AttemptStatusRunning} {
		now := time.Now().Add(time.Duration(i) * time.Second)
		turnStatus := status
		if status == AttemptStatusRunning {
			turnStatus = TurnStatusPending
		}
		turn := ConversationTurn{ID: uuid.New(), ConversationID: id, TurnSeq: int64(i + 1), TriggerType: "user_input", Status: turnStatus, CreatedAt: now}
		if err := db.Create(&turn).Error; err != nil {
			t.Fatal(err)
		}
		attempt := GenerationAttempt{ID: uuid.New(), ConversationID: id, TurnID: turn.ID, AttemptNo: 1, Status: status, StartedAt: now}
		if err := db.Create(&attempt).Error; err != nil {
			t.Fatal(err)
		}
		log := ContextBuildLog{ID: uuid.New(), ConversationID: id, ChallengeVersionID: conv.ChallengeVersionID, ChallengeHash: conv.ChallengeHash, AttemptID: &attempt.ID, TurnID: &turn.ID, Segments: JSONDoc(`[]`), IncludedMessageIDs: []string{}, ActivatedWorldEntryIDs: []string{}, CreatedAt: now}
		if err := logs.Create(ctx, &log); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			successfulAttempt, successfulLog, successfulTurn = attempt.ID, log.ID, turn.ID
		}
		performance, err := agents.LatestAttemptPerformance(ctx, id)
		if err != nil || performance == nil || performance.Attempt.ID != successfulAttempt {
			t.Fatalf("status %s changed performance: %+v, %v", status, performance, err)
		}
		last, err := logs.Last(ctx, id)
		if err != nil || last == nil || last.ID != successfulLog {
			t.Fatalf("status %s changed context: %+v, %v", status, last, err)
		}
		if i > 0 {
			if _, err := logs.GetSummary(ctx, log.ID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("unsuccessful context exposed: %v", err)
			}
		}
	}
	if err := db.Model(&ConversationTurn{}).Where("id = ?", successfulTurn).Update("invalidated_at", time.Now()).Error; err != nil {
		t.Fatal(err)
	}
	if got, err := agents.LatestAttemptPerformance(ctx, id); err != nil || got != nil {
		t.Fatalf("invalidated performance exposed: %+v %v", got, err)
	}
	if _, err := logs.Last(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalidated context exposed: %v", err)
	}
}
