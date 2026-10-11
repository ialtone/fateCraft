package storage

import "testing"

func TestCheckpointRuntimeHeaderMatchesCampaign(t *testing.T) {
	snapshot := campaignCheckpointData{GameTime: 60, Status: CampaignStatusActive}
	valid := func() JSONMap {
		return JSONMap{"schema_version": 1, "revision": 3, "game_time": 60, "status": "active", "data": map[string]any{"stock": 2}}
	}
	if err := validateCheckpointRuntime(snapshot, valid(), 5); err != nil {
		t.Fatal(err)
	}
	for _, change := range []JSONMap{{"schema_version": 0}, {"revision": 6}, {"game_time": 61}, {"status": "completed"}, {"outcome_key": "forged"}, {"data": nil}} {
		data := valid()
		for key, value := range change {
			data[key] = value
		}
		if err := validateCheckpointRuntime(snapshot, data, 5); err == nil {
			t.Fatalf("invalid checkpoint accepted: %+v", change)
		}
	}
}
