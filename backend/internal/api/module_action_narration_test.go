package api

import (
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/challenge"
)

func TestModuleActionNarrationRequestRetriesAndScope(t *testing.T) {
	conversationID := uuid.New()
	input := challenge.ModulePlayerActionInput{RequestID: uuid.New(), BranchEpoch: 2}
	id := moduleActionNarrationRequestID(conversationID, input)
	if id == input.RequestID || id != moduleActionNarrationRequestID(conversationID, input) {
		t.Fatal("narration must use its own stable request ID on action retries")
	}
	if id == moduleActionNarrationRequestID(uuid.New(), input) {
		t.Fatal("different conversations shared a narration request ID")
	}
	input.BranchEpoch++
	if id == moduleActionNarrationRequestID(conversationID, input) {
		t.Fatal("new branch reused an old narration request ID")
	}
}
