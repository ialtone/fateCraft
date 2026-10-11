// Package personnel enforces exclusive, bounded assignments. Member identity,
// skills, loyalty, payment and task outcomes are supplied by each module.
package personnel

import "github.com/ialtone/fatecraft/internal/gameplay/contract"

type Assignment struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Due    int64  `json:"due"`
}

func Available(assignments map[string]Assignment, member string) bool {
	_, busy := assignments[member]
	return !busy
}
func Assign(assignments map[string]Assignment, member string, task Assignment) error {
	if !contract.ValidStableKey(member) || !Available(assignments, member) || len(assignments) >= 12 || !contract.ValidStableKey(task.Kind) || task.Due < 0 {
		return contract.Reject("personnel.busy", "成员已在其他岗位或任务中")
	}
	assignments[member] = task
	return nil
}
