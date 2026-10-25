package challenge

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestNonSuccessOutcomeCannotPayPoints(t *testing.T) {
	for _, category := range []string{"failure", "neutral"} {
		doc := validVersionDocument(t)
		doc.Outcomes[0].Category = category
		err := ValidateDocument(doc, testRegistry())
		if err == nil || !strings.Contains(err.Error(), "非成功结局") {
			t.Fatalf("%s outcome with points: %v", category, err)
		}
	}
}

func TestNonSuccessOutcomeWithZeroPointsIsAccepted(t *testing.T) {
	doc := validVersionDocument(t)
	abandoned := doc.Outcomes[0]
	abandoned.ID = uuid.New()
	abandoned.OutcomeKey = "abandoned"
	abandoned.Category = "neutral"
	abandoned.RewardPoints = 0
	doc.Outcomes = append(doc.Outcomes, abandoned)
	for index := range doc.Assets {
		if doc.Assets[index].Kind == "prompt" {
			doc.Assets[index].Content = []byte(`{"id":"prompt","segments":[{"key":"system_master","role":"system","content":"system success abandoned","enabled":true,"required":true},{"key":"settlement_security","role":"system","content":"success abandoned","enabled":true,"required":true},{"key":"agent_tool_loop","role":"system","content":"tools success abandoned","enabled":true,"required":true}]}`)
		}
	}
	if err := ValidateDocument(doc, testRegistry()); err != nil {
		t.Fatalf("zero-point neutral outcome rejected: %v", err)
	}
}
