package kernel

import "github.com/ialtone/fatecraft/internal/gameplay/contract"

// NarrativeView contains only facts shared by player and model, plus the
// public runtime header. It never projects seeds or random evidence.
func NarrativeView(module contract.RuntimeModule, content contract.Content, run contract.RunState) (map[string]any, bool, error) {
	player, err := Project(module, content, run, contract.AudiencePlayer)
	if err != nil {
		return nil, false, err
	}
	model, err := Project(module, content, run, contract.AudienceModel)
	if err != nil {
		return nil, false, err
	}
	view, partial, err := contract.SharedNarrativeView(player, model)
	if err != nil {
		return nil, false, err
	}
	view["run"] = map[string]any{"status": run.Status, "game_time": run.GameTime, "outcome_key": run.OutcomeKey}
	return view, partial, nil
}

func NarrativeFactPaths(module contract.RuntimeModule) []string {
	paths := append([]string{}, contract.NarrativeFactPaths(module)...)
	return append(paths, "run.status", "run.game_time", "run.outcome_key")
}
