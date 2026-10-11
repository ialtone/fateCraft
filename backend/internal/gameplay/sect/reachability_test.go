package sect

import (
	"strings"
	"testing"
)

func TestSectRejectsUnreachableDependencies(t *testing.T) {
	for _, test := range []struct {
		name    string
		edit    func(*Config)
		message string
	}{
		{"self-supplied mission", func(c *Config) { delete(c.InitialStock, "manual"); c.Missions[0].Cost = map[string]int64{"manual": 1} }, "传承"},
		{"closed construction cycle", func(c *Config) { delete(c.InitialStock, "stone"); c.Missions[0].Reward = map[string]int64{"manual": 1} }, "目标设施"},
		{"unavailable recruit cost", func(c *Config) {
			c.Resources["token"] = "信物"
			c.Disciples[3].RecruitCost = map[string]int64{"token": 1}
		}, "目标人数"},
		{"impossible defense", func(c *Config) { c.Threat = 100 }, "防御上限"},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := fixture(t)
			test.edit(&c)
			if err := validate(c); err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("expected %s rejection, got %v", test.message, err)
			}
		})
	}
}
