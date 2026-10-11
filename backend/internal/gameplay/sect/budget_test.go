package sect

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
)

func TestBudgetChecksRecipesUnlockedAfterConstruction(t *testing.T) {
	c := fixture(t)
	for len(c.Disciples) < 8 {
		d := c.Disciples[3]
		d.Key = fmt.Sprintf("member%d", len(c.Disciples))
		c.Disciples = append(c.Disciples, d)
	}
	for len(c.Recipes) < 8 {
		r := c.Recipes[0]
		r.Key = fmt.Sprintf("recipe%d", len(c.Recipes))
		r.Name = strings.Repeat("产", 256)
		c.Recipes = append(c.Recipes, r)
	}
	if err := validate(c); err != nil {
		t.Fatal(err)
	}
	data, _ := ruledata.Object(c)
	content := contract.Content{Definitions: []contract.Definition{{Kind: DefinitionKind, Content: data}}}
	// These options do not appear in the initial unbuilt sect.
	initial, err := (Module{}).Initialize(content, contract.InitialInput{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (Module{}).Preview(content, initial, "player"); err != nil {
		t.Fatal(err)
	}
	if err := validateBudget(content, c); err == nil || !strings.Contains(err.Error(), "发展后的动作预览") {
		t.Fatalf("late preview overflow accepted: %v", err)
	}
}
