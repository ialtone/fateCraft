package guidance

import (
	"encoding/json"
	"strings"
	"testing"
)

const validProposal = `{"title":"Next step","options":[{"label":"Ask","message":"I ask the shopkeeper."},{"label":"Explore","message":"I explore the road."}]}`

func TestProposalRejectsUnstructuredAndUnauthorizedData(t *testing.T) {
	for _, raw := range []string{
		"Some prose\n"+validProposal, validProposal+" extra", validProposal+"{}", "null",
		`{"title":"Next","options":[]}`,
		strings.Replace(validProposal, `"Next step"`, `" "`, 1),
		strings.Replace(validProposal, `"Explore"`, `"Ask"`, 1),
		strings.Replace(validProposal, `"I explore the road."`, `"I ask the shopkeeper."`, 1),
		strings.Replace(validProposal, `"label":"Ask"`, `"action_key":"grant_reward","label":"Ask"`, 1),
		strings.Replace(validProposal, `"label":"Ask"`, `"enabled":true,"label":"Ask"`, 1),
		strings.Replace(validProposal, `"label":"Ask"`, `"label":"`+strings.Repeat("a",61)+`"`, 1),
	} {
		if _, err := Decode([]byte(raw)); err == nil { t.Errorf("accepted invalid proposal %q", raw) }
	}
}

func TestProposalPublicProjectionAndSelection(t *testing.T) {
	p, err := Decode([]byte(validProposal))
	if err != nil { t.Fatal(err) }
	s := Snapshot{Proposal:p}
	public := s.Public("message-id")
	if public.ID != "message-id" || len(public.Items) != 2 || public.Items[1].Key != "option-2" { t.Fatalf("unexpected projection: %#v", public) }
	raw, _ := json.Marshal(public)
	if strings.Contains(string(raw), "shopkeeper") { t.Fatal("public payload exposed authoritative submitted text") }
	if message, ok := s.Message("option-2"); !ok || message != p.Options[1].Message { t.Fatal("selection did not resolve stored intent") }
	if _, ok := s.Message("forged-key"); ok { t.Fatal("accepted forged key") }
}
