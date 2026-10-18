package storage

import (
	"reflect"
	"testing"
)

func TestCanonicalCampaignDocumentsIsOrderAndTimestampStable(t *testing.T) {
	first, err := canonicalCampaignDocuments(`[
		{"id":"b","created_at":"2026-01-01","state":{"hp":10,"mp":20}},
		{"id":"a","created_at":"2026-01-02","state":{"mp":20,"hp":10}}
	]`)
	if err != nil {
		t.Fatal(err)
	}
	second, err := canonicalCampaignDocuments(`[
		{"id":"a","created_at":"2027-01-02","state":{"hp":10,"mp":20}},
		{"id":"b","created_at":"2027-01-01","state":{"mp":20,"hp":10}}
	]`)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("canonical documents changed:\n%v\n%v", first, second)
	}
	if first[0] != `{"id":"a","state":{"hp":10,"mp":20}}` {
		t.Fatalf("unexpected canonical document: %s", first[0])
	}
}
