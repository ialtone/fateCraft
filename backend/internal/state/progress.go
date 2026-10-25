package state

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"

	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/operation"
	"github.com/ialtone/fatecraft/internal/storage"
)

// PlatformStateKey is a platform-owned, internal region of conversation state.
// It is copied through ordinary state patches and revisions so a story branch
// restores the progress reached on that branch, but it is never declared in a
// pack schema and therefore cannot be written by a model or a client.
const (
	PlatformStateKey      = "_platform"
	platformStageProgress = "stage_progress"
)

// StageProgressEntry is one trusted milestone reached on the current branch.
// Names and amounts are intentionally not stored: they are always read from
// the pinned gameplay version.
type StageProgressEntry struct {
	StageKey          string    `json:"stage_key"`
	SourceTurnID      string    `json:"source_turn_id,omitempty"`
	SourceBranchEpoch int64     `json:"source_branch_epoch,omitempty"`
	SourceActionKey   string    `json:"source_action_key,omitempty"`
	ReachedAt         time.Time `json:"reached_at,omitempty"`
}

// StageProgressCandidate is a milestone that just passed its trusted verifier.
type StageProgressCandidate struct {
	StageKey  string
	ActionKey string
}

// ReadStageProgress returns the current branch's recorded milestones. Unknown
// or malformed entries are ignored rather than trusted.
func ReadStageProgress(state map[string]any) []StageProgressEntry {
	raw, ok := gameplay.ValueAtPath(state, PlatformStateKey+"."+platformStageProgress)
	if !ok {
		return nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]StageProgressEntry, 0, len(values))
	for _, value := range values {
		item, ok := value.(map[string]any)
		if !ok {
			continue
		}
		key, _ := item["stage_key"].(string)
		if key == "" {
			continue
		}
		entry := StageProgressEntry{StageKey: key}
		entry.SourceTurnID, _ = item["source_turn_id"].(string)
		entry.SourceActionKey, _ = item["source_action_key"].(string)
		switch epoch := item["source_branch_epoch"].(type) {
		case float64:
			entry.SourceBranchEpoch = int64(epoch)
		case int64:
			entry.SourceBranchEpoch = epoch
		case int:
			entry.SourceBranchEpoch = int64(epoch)
		}
		if text, ok := item["reached_at"].(string); ok {
			if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
				entry.ReachedAt = parsed
			}
		}
		out = append(out, entry)
	}
	return out
}

// RecordStageProgressInTx merges newly reached milestones into the platform
// region and writes a state revision in the caller's transaction. A revision
// is written only when at least one key is new, so repeated retries do not
// create duplicate history. The returned entries are complete, ordered views
// of the current branch's progress.
func RecordStageProgressInTx(
	ctx context.Context,
	repo *storage.StateRepo,
	convID uuid.UUID,
	schema gameplay.StateSchema,
	branchEpoch int64,
	candidates []StageProgressCandidate,
) (added []StageProgressEntry, all []StageProgressEntry, err error) {
	if repo == nil {
		return nil, nil, nil
	}
	before, err := getForUpdateOrInitial(ctx, repo, convID, schema)
	if err != nil {
		return nil, nil, err
	}
	existing := ReadStageProgress(before)
	seen := make(map[string]bool, len(existing))
	for _, entry := range existing {
		seen[entry.StageKey] = true
	}
	turnID, attemptID := uuid.Nil, uuid.Nil
	if metadata, ok := operation.FromContext(ctx); ok {
		turnID, attemptID = metadata.TurnID, metadata.AttemptID
	}
	now := time.Now().UTC()
	merged := append([]StageProgressEntry(nil), existing...)
	for _, candidate := range candidates {
		if candidate.StageKey == "" || seen[candidate.StageKey] {
			continue
		}
		seen[candidate.StageKey] = true
		entry := StageProgressEntry{
			StageKey:          candidate.StageKey,
			SourceBranchEpoch: branchEpoch,
			SourceActionKey:   candidate.ActionKey,
			ReachedAt:         now,
		}
		if turnID != uuid.Nil {
			entry.SourceTurnID = turnID.String()
		}
		merged = append(merged, entry)
		added = append(added, entry)
	}
	if len(added) == 0 {
		return nil, existing, nil
	}
	sort.SliceStable(merged, func(i, j int) bool {
		if !merged[i].ReachedAt.Equal(merged[j].ReachedAt) {
			return merged[i].ReachedAt.Before(merged[j].ReachedAt)
		}
		return merged[i].StageKey < merged[j].StageKey
	})
	after := deepCopy(before)
	if after == nil {
		after = map[string]any{}
	}
	setAtPath(after, []string{PlatformStateKey, platformStageProgress}, progressDocuments(merged), false)
	if err := repo.Upsert(ctx, &storage.ConversationState{
		ConversationID: convID, State: storage.JSONMap(after),
		SchemaVersion: schema.SchemaVersion, UpdatedAt: now,
	}); err != nil {
		return nil, nil, err
	}
	revision := &storage.StateRevision{
		ID: uuid.New(), ConversationID: convID, Source: storage.SourceModule,
		Patch:       storage.JSONMap{PlatformStateKey: map[string]any{platformStageProgress: progressDocuments(added)}},
		BeforeState: storage.JSONMap(before), AfterState: storage.JSONMap(after), CreatedAt: now,
	}
	if turnID != uuid.Nil {
		revision.TurnID = &turnID
	}
	if attemptID != uuid.Nil {
		revision.AttemptID = &attemptID
	}
	if err := repo.AddRevision(ctx, revision); err != nil {
		return nil, nil, err
	}
	return added, merged, nil
}

func progressDocuments(entries []StageProgressEntry) []any {
	out := make([]any, 0, len(entries))
	for _, entry := range entries {
		document := map[string]any{
			"stage_key":           entry.StageKey,
			"source_branch_epoch": entry.SourceBranchEpoch,
		}
		if entry.SourceTurnID != "" {
			document["source_turn_id"] = entry.SourceTurnID
		}
		if entry.SourceActionKey != "" {
			document["source_action_key"] = entry.SourceActionKey
		}
		if !entry.ReachedAt.IsZero() {
			document["reached_at"] = entry.ReachedAt.UTC().Format(time.RFC3339Nano)
		}
		out = append(out, document)
	}
	return out
}

// ReservedPlatformState copies the platform-owned region from an existing
// trusted state so schema filtering cannot discard branch progress.
func ReservedPlatformState(state map[string]any) map[string]any {
	if state == nil {
		return nil
	}
	value, ok := state[PlatformStateKey]
	if !ok {
		return nil
	}
	copy, _ := deepCopyValue(value).(map[string]any)
	return copy
}
