package server

import "testing"

func TestWorkoutNestedTargetValidation(t *testing.T) {
	step := map[string]any{"targetType": map[string]any{"workoutTargetTypeId": float64(4), "workoutTargetTypeKey": "heart.rate.zone", "targetValueOne": float64(3)}}
	workout := map[string]any{"workoutSegments": []any{map[string]any{"workoutSteps": []any{step}}}}
	if err := validateWorkout(workout); err != nil {
		t.Fatal(err)
	}
	if step["zoneNumber"] != 3 || step["targetValueOne"] != nil {
		t.Fatal(step)
	}
	step = map[string]any{"targetType": map[string]any{"targetValueOne": float64(3)}, "targetValueOne": float64(4)}
	workout = map[string]any{"workoutSegments": []any{map[string]any{"workoutSteps": []any{step}}}}
	if err := validateWorkout(workout); err == nil {
		t.Fatal("accepted conflicting targets")
	}
}
