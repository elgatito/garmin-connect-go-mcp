package server

import "testing"

func TestCurateGoal(t *testing.T) {
	got := curateGoal(map[string]any{"name": "Run", "progress": map[string]any{"percent": float64(40), "distanceInMeters": float64(4000)}, "distanceInMeters": float64(10000)})
	if got["progress_percent"] != float64(40) || got["progress_distance_meters"] != float64(4000) || got["target_distance_meters"] != float64(10000) {
		t.Fatal(got)
	}
}
