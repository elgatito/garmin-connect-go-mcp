package server

import "testing"

func TestTrainingEffectRecoveryUnits(t *testing.T) {
	got := trainingEffect(map[string]any{"summaryDTO": map[string]any{"trainingEffect": float64(3), "recoveryTime": float64(90)}}, 1, "activity_details")
	if got["aerobic_effect"] != float64(3) || got["recovery_time_hours"] != float64(1.5) {
		t.Fatal(got)
	}
}
