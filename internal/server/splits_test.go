package server

import "testing"

func TestWrappedLapDistance(t *testing.T) {
	v := curateSplits(map[string]any{"activityId": float64(1), "lapDTOs": []any{map[string]any{"distance": float64(42949672.92)}}})
	lap := v["laps"].([]any)[0].(map[string]any)
	if lap["distance_invalid"] != true || lap["distance_meters"] != nil {
		t.Fatal(lap)
	}
}
