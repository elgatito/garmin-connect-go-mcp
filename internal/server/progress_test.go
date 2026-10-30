package server

import (
	"math"
	"testing"
)

func TestProgressSummaryCalories(t *testing.T) {
	raw := map[string]any{"stats": map[string]any{"running": map[string]any{"calories": map[string]any{"count": float64(2), "sum": float64(419)}}}}
	out := progressSummary(raw, "calories", "2026-01-01", "2026-01-02")
	stats := out["stats_by_activity_type"].(map[string]any)
	run := stats["running"].(map[string]any)
	if out["count_of_activities"] != float64(2) || math.Abs(run["sum"].(float64)-100) > 1e-9 {
		t.Fatal(out)
	}
}
