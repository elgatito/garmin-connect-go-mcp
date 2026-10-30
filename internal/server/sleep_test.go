package server

import "testing"

func TestSleepSummaryMissingStages(t *testing.T) {
	got := sleepSummary(map[string]any{"dailySleepDTO": map[string]any{"sleepTimeSeconds": float64(3600), "deepSleepSeconds": float64(900), "lightSleepSeconds": nil, "sleepScores": nil}})
	if got["sleep_hours"] != float64(1) || got["deep_sleep_percent"] != float64(25) || got["light_sleep_percent"] != nil {
		t.Fatal(got)
	}
}
