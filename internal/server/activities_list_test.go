package server

import (
	"reflect"
	"testing"
)

func TestActivityRows(t *testing.T) {
	rows := []any{map[string]any{"activityId": float64(7), "activityName": "run", "activityType": map[string]any{"typeKey": "running"}, "eventType": map[string]any{"typeKey": "race"}, "movingDuration": float64(12), "other": "ignore"}}
	got := activityRows(rows, "all")
	want := []any{map[string]any{"id": float64(7), "name": "run", "type": "running", "event_type": "race", "moving_duration_seconds": float64(12)}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
}
