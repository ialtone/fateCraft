package operation

import (
	"context"

	"github.com/google/uuid"
)

// Metadata identifies the immutable generation attempt associated with writes.
type Metadata struct {
	ConversationID uuid.UUID
	TurnID         uuid.UUID
	AttemptID      uuid.UUID
	MemoryID       *uuid.UUID
	BranchEpoch    int64
	TriggerType    string
	TextOnly       bool
}

// Generation identifies the durable conversation generation run that owns a
// write. It is kept separate from Metadata because the run is acquired before
// a turn/attempt exists.
type Generation struct {
	ConversationID uuid.UUID
	ID             uuid.UUID
}

type generationContextKey struct{}

// WithGeneration binds a generation run to a context. Storage write
// boundaries use this value to reject stale work after End/Archive/Delete.
func WithGeneration(ctx context.Context, conversationID, generationID uuid.UUID) context.Context {
	return context.WithValue(ctx, generationContextKey{}, Generation{
		ConversationID: conversationID,
		ID:             generationID,
	})
}

func GenerationFromContext(ctx context.Context) (Generation, bool) {
	generation, ok := ctx.Value(generationContextKey{}).(Generation)
	return generation, ok && generation.ConversationID != uuid.Nil && generation.ID != uuid.Nil
}

type contextKey struct{}

func WithMetadata(ctx context.Context, metadata Metadata) context.Context {
	return context.WithValue(ctx, contextKey{}, metadata)
}

func FromContext(ctx context.Context) (Metadata, bool) {
	metadata, ok := ctx.Value(contextKey{}).(Metadata)
	return metadata, ok && metadata.TurnID != uuid.Nil && metadata.AttemptID != uuid.Nil
}
