package server

import (
	"reflect"
	"testing"
)

func TestWorkoutSummary(t *testing.T) {
	got := workoutSummary(map[string]any{"workoutId": float64(42), "workoutName": "Run", "sportType": map[string]any{"sportTypeKey": "running"}, "estimatedDuration": float64(600), "ignored": true})
	want := map[string]any{"id": float64(42), "name": "Run", "sport": "running", "estimated_duration_seconds": float64(600)}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}
func TestWorkoutStepTargets(t *testing.T) {
	step := workoutStep(map[string]any{"stepType": map[string]any{"stepTypeKey": "interval"}, "targetType": map[string]any{"workoutTargetTypeKey": "pace.zone"}, "targetValueOne": float64(2), "targetValueTwo": float64(4)})
	if step["type"] != "interval" || step["target_value_low"] != float64(2) || step["target_value_unit"] != "meters_per_second" {
		t.Fatal(step)
	}
}
