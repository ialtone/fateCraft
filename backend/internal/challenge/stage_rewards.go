package challenge

import (
	"context"
	"errors"

	"github.com/google/uuid"

	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/state"
	"github.com/ialtone/fatecraft/internal/storage"
)

// recordStageRewards verifies every milestone that has not been recorded on
// the current branch and stores it as branch progress. It never writes the
// account ledger or a settlement receipt. Stage eligibility never depends on
// the terminal outcome: deep progress counts even when the run ends in
// failure. Verification errors mean "not reached" because the rule set was
// validated at publication time.
func (s *Service) recordStageRewards(ctx context.Context, repo *storage.ChallengeRepository, conv *storage.Conversation, content gameplay.Content, trustedState map[string]any, actionKey string) ([]state.StageProgressEntry, []gameplay.ProgressRewardStage, error) {
	passed, err := s.verifyStageRewards(ctx, repo, conv, content, trustedState)
	if err != nil || len(passed) == 0 {
		return nil, nil, err
	}
	candidates := make([]state.StageProgressCandidate, 0, len(passed))
	for _, stage := range passed {
		candidates = append(candidates, state.StageProgressCandidate{StageKey: stage.Key, ActionKey: actionKey})
	}
	added, _, err := state.RecordStageProgressInTx(ctx, repo.StateRepo(), conv.ID, content.Runtime.StateSchema, conv.BranchEpoch, candidates)
	if err != nil {
		return nil, nil, err
	}
	if len(added) == 0 {
		return nil, nil, nil
	}
	byKey := make(map[string]gameplay.ProgressRewardStage, len(passed))
	for _, stage := range passed {
		byKey[stage.Key] = stage
	}
	reached := make([]gameplay.ProgressRewardStage, 0, len(added))
	for _, entry := range added {
		if stage, ok := byKey[entry.StageKey]; ok {
			reached = append(reached, stage)
		}
	}
	return added, reached, nil
}

// verifyStageRewards evaluates every configured stage against the trusted
// post-action state. It does not skip stages already recorded: callers merge
// the result with branch progress.
func (s *Service) verifyStageRewards(ctx context.Context, repo *storage.ChallengeRepository, conv *storage.Conversation, content gameplay.Content, trustedState map[string]any) ([]gameplay.ProgressRewardStage, error) {
	rules, found, err := gameplay.ParseProgressRewards(content)
	if err != nil {
		return nil, err
	}
	if !found || len(rules.Stages) == 0 {
		return nil, nil
	}
	rollKeys, err := repo.SuccessfulRollKeys(ctx, conv.ID)
	if err != nil {
		return nil, err
	}
	turnCount, err := repo.UserTurnCount(ctx, conv.ID)
	if err != nil {
		return nil, err
	}
	rolls := make(map[string]bool, len(rollKeys))
	for _, key := range rollKeys {
		rolls[key] = true
	}
	evidence := gameplay.Evidence{
		State:              trustedState,
		SuccessfulRollKeys: rolls,
		TurnCount:          turnCount,
		Facts:              map[string]any{"engine_key": content.Runtime.Engine.Key},
	}
	reached := make([]gameplay.ProgressRewardStage, 0, len(rules.Stages))
	for _, stage := range gameplay.SortedProgressRewardStages(rules.Stages) {
		if err := s.registry.Verify(stage.VerifierKey, evidence, stage.VerifierConfig); err != nil {
			continue
		}
		reached = append(reached, stage)
	}
	return reached, nil
}

// loadStageProgress reads the current branch's progress from the trusted
// conversation state. The caller's transaction has already locked and updated
// that row, so this read observes the same snapshot.
func (s *Service) loadStageProgress(ctx context.Context, repo *storage.ChallengeRepository, conv *storage.Conversation, trustedState map[string]any) ([]state.StageProgressEntry, error) {
	stored, err := repo.StateRepo().Get(ctx, conv.ID)
	if err == nil {
		return state.ReadStageProgress(stored.State), nil
	}
	if errors.Is(err, storage.ErrNotFound) {
		return state.ReadStageProgress(trustedState), nil
	}
	return nil, err
}

// stageGrantsForProgress maps branch progress keys onto fixed-version rules.
// Amounts never come from stored progress or client input.
func stageGrantsForProgress(content gameplay.Content, progress []state.StageProgressEntry) ([]storage.StageRewardGrant, error) {
	rules, found, err := gameplay.ParseProgressRewards(content)
	if err != nil {
		return nil, err
	}
	if !found || len(rules.Stages) == 0 || len(progress) == 0 {
		return nil, nil
	}
	byKey := make(map[string]gameplay.ProgressRewardStage, len(rules.Stages))
	for _, stage := range gameplay.SortedProgressRewardStages(rules.Stages) {
		byKey[stage.Key] = stage
	}
	grants := make([]storage.StageRewardGrant, 0, len(progress))
	for _, entry := range progress {
		stage, ok := byKey[entry.StageKey]
		if !ok {
			continue
		}
		grant := storage.StageRewardGrant{
			Key: stage.Key, Name: stage.Name, SortOrder: stage.Order, Points: stage.Points,
			ActionKey: entry.SourceActionKey, SourceBranchEpoch: entry.SourceBranchEpoch,
		}
		if turnID, err := uuid.Parse(entry.SourceTurnID); err == nil {
			grant.SourceTurnID = turnID
		}
		grants = append(grants, grant)
	}
	return grants, nil
}

// stageProgressViews joins branch progress with fixed-version names and
// amounts for player-facing responses. Client-routed conversations are marked
// so the UI can explain that the configured amount will not be paid.
func stageProgressViews(content gameplay.Content, progress []state.StageProgressEntry, disabledReason string) []storage.StageProgressView {
	rules, found, err := gameplay.ParseProgressRewards(content)
	if err != nil || !found {
		return []storage.StageProgressView{}
	}
	byKey := make(map[string]gameplay.ProgressRewardStage, len(rules.Stages))
	for _, stage := range gameplay.SortedProgressRewardStages(rules.Stages) {
		byKey[stage.Key] = stage
	}
	views := make([]storage.StageProgressView, 0, len(progress))
	for _, entry := range progress {
		stage, ok := byKey[entry.StageKey]
		if !ok {
			continue
		}
		view := storage.StageProgressView{
			StageKey: stage.Key, Name: stage.Name, SortOrder: stage.Order,
			ConfiguredPoints: stage.Points, DisabledReason: disabledReason,
		}
		if !entry.ReachedAt.IsZero() {
			view.ReachedAt = entry.ReachedAt
		}
		views = append(views, view)
	}
	return views
}

// stageProgressViewsForReached renders only the milestones first reached by
// the current action. The full branch progress is exposed by the snapshot and
// ending endpoints, so an action response never repeats old notices.
func stageProgressViewsForReached(reached []gameplay.ProgressRewardStage, disabledReason string) ([]storage.StageProgressView, int64) {
	views := make([]storage.StageProgressView, 0, len(reached))
	var pending int64
	for _, stage := range reached {
		views = append(views, storage.StageProgressView{
			StageKey: stage.Key, Name: stage.Name, SortOrder: stage.Order,
			ConfiguredPoints: stage.Points, DisabledReason: disabledReason,
		})
		pending += stage.Points
	}
	return views, pending
}
