package server

import "testing"

func TestIntervalWorkoutStepOrder(t *testing.T) {
	w, err := buildAerobic("Intervals", "create_run_interval_workout", map[string]any{"warmup_min": float64(10), "cooldown_min": float64(5), "repeats": float64(3), "rep_seconds": float64(300), "recovery_seconds": float64(120)})
	if err != nil {
		t.Fatal(err)
	}
	segments := w["workoutSegments"].([]any)
	steps := segments[0].(map[string]any)["workoutSteps"].([]any)
	group := steps[1].(map[string]any)
	children := group["workoutSteps"].([]any)
	if children[0].(map[string]any)["stepOrder"] != 3 || steps[2].(map[string]any)["stepOrder"] != 5 {
		t.Fatal(steps)
	}
}
func TestStrengthStepOrder(t *testing.T) {
	w, err := buildStrength("Strength", []any{map[string]any{"name": "Squat", "sets": float64(3), "reps": float64(5)}, map[string]any{"name": "Row", "sets": float64(1), "reps": float64(8)}})
	if err != nil {
		t.Fatal(err)
	}
	segments := w["workoutSegments"].([]any)
	steps := segments[0].(map[string]any)["workoutSteps"].([]any)
	if steps[0].(map[string]any)["stepOrder"] != 1 || steps[1].(map[string]any)["stepOrder"] != 4 {
		t.Fatal(steps)
	}
}
