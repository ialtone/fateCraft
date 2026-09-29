package storage

import "testing"

func TestRewardLedgerMetadataOmitsInternalRequests(t *testing.T) {
	view := publicRewardMetadata(JSONMap{"request_id": "private", "reason": "support note", "refund_of": "internal consume key", "record_id": "public record", "reward_snapshot": JSONMap{"name": "额度"}})
	for _, key := range []string{"request_id", "reason", "refund_of"} {
		if _, ok := view[key]; ok {
			t.Fatalf("private metadata leaked: %s", key)
		}
	}
	if view["record_id"] != "public record" || view["reward_snapshot"] == nil {
		t.Fatal("public trace missing")
	}
}
