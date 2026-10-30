package server

import "testing"

func TestCoachData(t *testing.T) {
	v := coachData([]any{map[string]any{"planName": "Plan", "trainingPlanId": float64(1), "workoutScheduleSummaries": []any{map[string]any{"workoutName": "Run"}}}}, "2026-01-01")
	out := v.(map[string]any)
	if out["count"] != 1 || len(out["plans"].([]any)) != 1 {
		t.Fatal(out)
	}
}
