package server

import "testing"

func TestBuildExerciseSets(t *testing.T) {
	sets, err := buildExerciseSets([]any{map[string]any{"category": "squat", "reps": float64(5), "weight_lb": float64(10), "rest_seconds": float64(60)}})
	if err != nil || len(sets) != 2 {
		t.Fatal(sets, err)
	}
	first := sets[0].(map[string]any)
	if first["weight"] != float64(4536) || sets[1].(map[string]any)["setType"] != "REST" {
		t.Fatal(sets)
	}
}
