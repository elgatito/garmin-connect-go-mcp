package server

import "testing"

func TestToleranceTrendSorts(t *testing.T) {
	got := toleranceTrend([]any{map[string]any{"calendarDate": "2026-01-02", "tolerance": float64(20000)}, map[string]any{"calendarDate": "2026-01-01", "tolerance": float64(10000)}}, "weekly")
	if got[0]["date"] != "2026-01-01" || got[1]["tolerance_km"] != float64(20) {
		t.Fatal(got)
	}
}
