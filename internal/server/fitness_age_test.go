package server

import "testing"

func TestFitnessAge(t *testing.T) {
	got := fitnessAge(map[string]any{"chronologicalAge": float64(45), "fitnessAge": float64(40), "components": map[string]any{"bmi": map[string]any{"value": float64(22), "targetValue": float64(21)}}}, "2026-01-01", true)
	if got["age_difference_years"] != float64(5) || got["components"].(map[string]any)["bmi"].(map[string]any)["target"] != float64(21) {
		t.Fatal(got)
	}
}
