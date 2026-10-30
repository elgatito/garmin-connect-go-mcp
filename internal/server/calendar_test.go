package server

import "testing"

func TestCalendarEventDistanceAndGoal(t *testing.T) {
	event := calendarEvent(map[string]any{"completionTarget": map[string]any{"unitType": "distance", "unit": "mile", "value": float64(2)}, "location": "  Park  "}, map[string]any{"eventCustomization": map[string]any{"customGoal": map[string]any{"unitType": "time", "unit": "second", "value": float64(600)}}})
	if event["distance_meters"] != 3218.688 || event["goal_time_seconds"] != float64(600) || event["location"] != "Park" {
		t.Fatal(event)
	}
}
