package kernel

import (
	"strings"
	"testing"

	"github.com/ialtone/fatecraft/internal/gameplay/contract"
)

type unlockOverflow struct {
	mysteryFixture
	projection bool
}

func (m unlockOverflow) Preview(c contract.Content, s contract.RunState, actor string) ([]contract.ActionPreview, error) {
	previews, err := m.mysteryFixture.Preview(c, s, actor)
	if s.Revision > 0 && !m.projection {
		previews[0].Risk = strings.Repeat("oversized", 5000)
	}
	return previews, err
}
func (m unlockOverflow) Project(c contract.Content, s contract.RunState, audience string) (map[string]any, error) {
	if s.Revision > 0 && m.projection {
		return map[string]any{"revealed": strings.Repeat("oversized", 5000)}, nil
	}
	return m.mysteryFixture.Project(c, s, audience)
}
func (m unlockOverflow) Execute(_ contract.Content, s contract.RunState, _ contract.RuntimeRequest) (contract.Transition, error) {
	s.Data["choice"] = "east"
	return contract.Transition{State: s}, nil
}
func TestTransitionCannotStrandSaveBehindPreviewOrProjectionOverflow(t *testing.T) {
	for _, projection := range []bool{false, true} {
		m := unlockOverflow{projection: projection}
		c := mysteryContent("east")
		s, err := Initialize(m, c, contract.InitialInput{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = Execute(m, c, s, contract.RuntimeRequest{ActionKey: "mystery.choose", Actor: "player", Arguments: map[string]any{"exit": "east"}, Random: NewRandom(1, "unlock")})
		if err == nil || !strings.Contains(err.Error(), "动作后的") {
			t.Fatalf("oversized next state was accepted: %v", err)
		}
		if s.Revision != 0 || s.Data["choice"] != "" {
			t.Fatal("failed unlock mutated caller save")
		}
	}
}
