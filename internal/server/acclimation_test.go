package server

import "testing"

func TestToleranceUnits(t *testing.T) {
	out := toleranceSummary(map[string]any{"acuteImpactLoad": float64(12000), "acuteDistance": float64(10000)}, "2026-01-01")
	if out["acute_load_km"] != float64(12) || out["load_ratio"] != float64(1.2) {
		t.Fatal(out)
	}
}
