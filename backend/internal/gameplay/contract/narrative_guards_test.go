package contract

import "testing"

func TestNarrativeGuardsValidationAndRoundTrip(t *testing.T) {
	contract := validCampaignContract()
	contract.NarrativeGuards = []NarrativeGuard{{Pattern: "永生不死", Message: "不允许永生"}}
	if err := ValidateContract(&contract); err != nil {
		t.Fatal(err)
	}
	row, err := EncodeRuntime(contract)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRuntime(row)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.NarrativeGuards) != 1 || decoded.NarrativeGuards[0] != contract.NarrativeGuards[0] {
		t.Fatalf("guards did not round-trip: %#v", decoded.NarrativeGuards)
	}
	for _, guards := range [][]NarrativeGuard{
		{{Pattern: "[", Message: "bad regex"}},
		{{Pattern: "valid", Message: " "}},
		make([]NarrativeGuard, 17),
	} {
		contract.NarrativeGuards = guards
		if err := ValidateContract(&contract); err == nil {
			t.Fatalf("invalid guards accepted: %#v", guards)
		}
	}
}
