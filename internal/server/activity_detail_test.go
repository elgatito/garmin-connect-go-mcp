package server

import "testing"

func TestActivityDetailCuration(t *testing.T) {
	got := curateActivity(map[string]any{"activityId": float64(1), "activityTypeDTO": map[string]any{"typeKey": "running"}, "summaryDTO": map[string]any{"distance": float64(5000)}, "metadataDTO": map[string]any{"hasSplits": false}})
	if got["id"] != float64(1) || got["type"] != "running" || got["distance_meters"] != float64(5000) || got["has_splits"] != false {
		t.Fatal(got)
	}
}
