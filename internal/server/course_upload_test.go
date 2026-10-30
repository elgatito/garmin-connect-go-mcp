package server

import "testing"

func TestCoursePayloadDistanceAndBounds(t *testing.T) {
	payload, err := coursePayload(map[string]any{"geoPoints": []any{map[string]any{"latitude": float64(0), "longitude": float64(0)}, map[string]any{"latitude": float64(0), "longitude": float64(1)}}}, "Test", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	distance := payload["distanceMeter"].(float64)
	if distance < 111000 || distance > 112000 {
		t.Fatal(distance)
	}
}
