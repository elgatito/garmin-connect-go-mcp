package server

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const (
	activityServicePathTest  = "/activity-service/activity"
	activityTypesPathTest    = "/activity-service/activity/activityTypes"
	activityForDatePathTest  = "/mobile-gateway/heartRate/forDate"
	activityCountPathTest    = "/activitylist-service/activities/count"
	activitySearchPathTest   = "/activitylist-service/activities/search/activities"
	activityEventTypesPath   = "/activity-service/activity/eventTypes"
	deviceLastUsedPathTest   = "/device-service/deviceservice/mylastused"
	gearFilterPathTest       = "/gear-service/gear/filterGear"
	gearBasePathTest         = "/gear-service/gear"
	userSettingsPathTest     = "/userprofile-service/userprofile/user-settings"
	activityTestID           = "12345678901"
	activityDetailPath       = activityServicePathTest + "/" + activityTestID
	activitySplitsPath       = activityDetailPath + "/splits"
	activityTypedSplitsPath  = activityDetailPath + "/typedsplits"
	activitySplitSummPath    = activityDetailPath + "/split_summaries"
	activityWeatherPath      = activityDetailPath + "/weather"
	activityHRZonesPath      = activityDetailPath + "/hrTimeInZones"
	activityPowerZonesPath   = activityDetailPath + "/powerTimeInZones"
	activityExerciseSetPath  = activityDetailPath + "/exerciseSets"
	activityPTWeatherID      = "23651251274"
	activityPTWeatherPath    = activityServicePathTest + "/" + activityPTWeatherID + "/weather"
	activitySwimID           = "22526515067"
	activitySwimSplitsPath   = activityServicePathTest + "/" + activitySwimID + "/splits"
	activityWrappedID        = "24687347098"
	activityWrappedSplitsURL = activityServicePathTest + "/" + activityWrappedID + "/splits"
)

func decodeBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode request body %q: %v", body, err)
	}
	return m
}

func commonActivity() any {
	return map[string]any{
		"activityId":     12345678901,
		"activityName":   "Morning Run",
		"activityType":   map[string]any{"typeKey": "running", "typeId": 1},
		"eventType":      map[string]any{"typeKey": "race", "typeId": 1},
		"startTimeLocal": "2024-01-15 07:00:00",
		"distance":       5000.0,
		"duration":       1800.0,
		"averageHR":      145,
		"maxHR":          165,
		"calories":       350,
	}
}

func TestGetActivitiesByDateTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activitySearchPathTest, fixture(t, "MOCK_ACTIVITIES"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activities_by_date", map[string]any{
		"start_date": "2024-01-08",
		"end_date":   "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	calls := f.callsMatching("GET", activitySearchPathTest)
	if len(calls) != 1 {
		t.Fatalf("activity calls = %d, want 1", len(calls))
	}
	q := calls[0].Query
	want := map[string]string{
		"startDate": "2024-01-08",
		"endDate":   "2024-01-15",
		"start":     "0",
		"limit":     "100",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Fatalf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	if q.Has("activityType") {
		t.Fatalf("unexpected activityType %q", q.Get("activityType"))
	}
}

func TestGetActivitiesByDateHandlesNullActivityType(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activitySearchPathTest, []any{
		map[string]any{"activityId": 123, "activityName": "Untyped activity", "activityType": nil},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activities_by_date", map[string]any{
		"start_date": "2024-01-08",
		"end_date":   "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	sameJSON(t, got["activities"], []any{map[string]any{"id": 123, "name": "Untyped activity"}})
}

func TestGetActivitiesByDateWithType(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activitySearchPathTest, []any{commonActivity()})
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "get_activities_by_date", map[string]any{
		"start_date":    "2024-01-08",
		"end_date":      "2024-01-15",
		"activity_type": "running",
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	calls := f.callsMatching("GET", activitySearchPathTest)
	if len(calls) != 1 {
		t.Fatalf("activity calls = %d, want 1", len(calls))
	}
	if got := calls[0].Query.Get("activityType"); got != "running" {
		t.Fatalf("activityType = %q", got)
	}
}

func TestGetActivitiesFordateTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activityForDatePathTest+"/2024-01-15", map[string]any{
		"ActivitiesForDay": map[string]any{"payload": []any{commonActivity()}},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activities_fordate", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["count"] != float64(1) {
		t.Fatalf("count = %v", got["count"])
	}
	activities := mutList(t, got["activities"])
	first := mutMap(t, activities[0])
	if first["id"] != float64(12345678901) || first["event_type"] != "race" {
		t.Fatalf("activity = %v", first)
	}
	calls := f.callsMatching("GET", activityForDatePathTest+"/2024-01-15")
	if len(calls) != 1 {
		t.Fatalf("fordate calls = %d, want 1", len(calls))
	}
}

func TestGetActivityTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activityDetailPath, fixture(t, "MOCK_ACTIVITY_DETAILS"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activity", map[string]any{"activity_id": 12345678901})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	for key, want := range map[string]any{
		"distance_meters":       float64(5000),
		"duration_seconds":      float64(1800),
		"calories":              float64(350),
		"avg_hr_bpm":            float64(145),
		"elevation_gain_meters": float64(50.5),
		"lap_count":             float64(2),
	} {
		if got[key] != want {
			t.Fatalf("%s = %v, want %v", key, got[key], want)
		}
	}
	if calls := f.callsMatching("GET", activityDetailPath); len(calls) != 1 {
		t.Fatalf("activity calls = %d, want 1", len(calls))
	}
}

func TestGetActivityHandlesNullNestedSections(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activityServicePathTest+"/123", map[string]any{
		"activityId":      123,
		"summaryDTO":      nil,
		"activityTypeDTO": nil,
		"metadataDTO":     nil,
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activity", map[string]any{"activity_id": 123})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	sameJSON(t, got, map[string]any{"id": float64(123)})
}

func TestGetActivityAcceptsStringID(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activityServicePathTest+"/22833209898", fixture(t, "MOCK_ACTIVITY_DETAILS"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activity", map[string]any{"activity_id": "22833209898"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if calls := f.callsMatching("GET", activityServicePathTest+"/22833209898"); len(calls) != 1 {
		t.Fatalf("activity calls = %d, want 1", len(calls))
	}
}

func TestSetActivityNameTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("PUT "+activityDetailPath, map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_activity_name", map[string]any{
		"activity_id":   12345678901,
		"activity_name": "Morning Run - Easy",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	calls := f.callsMatching("PUT", activityDetailPath)
	if len(calls) != 1 {
		t.Fatalf("PUT calls = %d, want 1", len(calls))
	}
	sameJSON(t, decodeBody(t, calls[0].Body), map[string]any{
		"activityId":   float64(12345678901),
		"activityName": "Morning Run - Easy",
	})
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["success"] != true || got["activity_id"] != float64(12345678901) || got["activity_name"] != "Morning Run - Easy" {
		t.Fatalf("result = %v", got)
	}
}

func TestSetActivityNameToolRejectsBlankName(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_activity_name", map[string]any{
		"activity_id":   12345678901,
		"activity_name": "   ",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if text != "Activity name cannot be empty" {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("PUT", activityDetailPath); len(calls) != 0 {
		t.Fatalf("PUT calls = %d, want 0", len(calls))
	}
}

func TestSetActivityTypeTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activityTypesPathTest, fixture(t, "MOCK_ACTIVITY_TYPES"))
	f.on("PUT "+activityDetailPath, map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_activity_type", map[string]any{
		"activity_id": 12345678901,
		"type_key":    "hiking",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	calls := f.callsMatching("PUT", activityDetailPath)
	if len(calls) != 1 {
		t.Fatalf("PUT calls = %d, want 1", len(calls))
	}
	sameJSON(t, decodeBody(t, calls[0].Body), map[string]any{
		"activityId": float64(12345678901),
		"activityTypeDTO": map[string]any{
			"typeId":       float64(3),
			"typeKey":      "hiking",
			"parentTypeId": float64(17),
		},
	})
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["success"] != true || got["type_key"] != "hiking" {
		t.Fatalf("result = %v", got)
	}
}

func TestSetActivityTypeRejectsUnknownKey(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activityTypesPathTest, fixture(t, "MOCK_ACTIVITY_TYPES"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_activity_type", map[string]any{
		"activity_id": 12345678901,
		"type_key":    "moonwalking",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "Unknown activity type 'moonwalking'") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("PUT", activityDetailPath); len(calls) != 0 {
		t.Fatalf("PUT calls = %d, want 0", len(calls))
	}
}

func TestSetActivityDescriptionTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("PUT "+activityDetailPath, map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_activity_description", map[string]any{
		"activity_id": 12345678901,
		"description": "Felt strong. New shoes.",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	calls := f.callsMatching("PUT", activityDetailPath)
	if len(calls) != 1 {
		t.Fatalf("PUT calls = %d, want 1", len(calls))
	}
	sameJSON(t, decodeBody(t, calls[0].Body), map[string]any{
		"activityId":  float64(12345678901),
		"description": "Felt strong. New shoes.",
	})
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["success"] != true || got["description"] != "Felt strong. New shoes." {
		t.Fatalf("result = %v", got)
	}
}

func TestSetActivityEventTypeTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activityEventTypesPath, []any{
		map[string]any{"typeId": 1, "typeKey": "race", "sortOrder": 5},
		map[string]any{"typeId": 4, "typeKey": "training", "sortOrder": 7},
	})
	f.on("PUT "+activityDetailPath, map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_activity_event_type", map[string]any{
		"activity_id": 12345678901,
		"event_type":  "race",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	calls := f.callsMatching("PUT", activityDetailPath)
	if len(calls) != 1 {
		t.Fatalf("PUT calls = %d, want 1", len(calls))
	}
	sameJSON(t, decodeBody(t, calls[0].Body), map[string]any{
		"activityId": float64(12345678901),
		"eventTypeDTO": map[string]any{
			"typeId":    float64(1),
			"typeKey":   "race",
			"sortOrder": float64(5),
		},
	})
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["event_type"] != "race" {
		t.Fatalf("result = %v", got)
	}
}

func TestSetActivityEventTypeRejectsUnknown(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activityEventTypesPath, []any{
		map[string]any{"typeId": 1, "typeKey": "race", "sortOrder": 5},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_activity_event_type", map[string]any{
		"activity_id": 12345678901,
		"event_type":  "wedding",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "Unknown event type 'wedding'") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("PUT", activityDetailPath); len(calls) != 0 {
		t.Fatalf("PUT calls = %d, want 0", len(calls))
	}
}

func TestSetPerceivedEffortTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("PUT "+activityDetailPath, map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_perceived_effort", map[string]any{
		"activity_id": 12345678901,
		"rpe":         7,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	calls := f.callsMatching("PUT", activityDetailPath)
	if len(calls) != 1 {
		t.Fatalf("PUT calls = %d, want 1", len(calls))
	}
	sameJSON(t, decodeBody(t, calls[0].Body), map[string]any{
		"activityId": float64(12345678901),
		"summaryDTO": map[string]any{"directWorkoutRpe": float64(70)},
	})
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["rpe"] != float64(7) {
		t.Fatalf("result = %v", got)
	}
}

func TestSetPerceivedEffortRejectsOutOfRange(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_perceived_effort", map[string]any{
		"activity_id": 12345678901,
		"rpe":         11,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if text != "rpe must be between 0 and 10" {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("PUT", activityDetailPath); len(calls) != 0 {
		t.Fatalf("PUT calls = %d, want 0", len(calls))
	}
}

func TestSetActivityFeelTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("PUT "+activityDetailPath, map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_activity_feel", map[string]any{
		"activity_id": 12345678901,
		"feel":        75,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	calls := f.callsMatching("PUT", activityDetailPath)
	if len(calls) != 1 {
		t.Fatalf("PUT calls = %d, want 1", len(calls))
	}
	sameJSON(t, decodeBody(t, calls[0].Body), map[string]any{
		"activityId": float64(12345678901),
		"summaryDTO": map[string]any{"directWorkoutFeel": float64(75)},
	})
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["feel"] != float64(75) {
		t.Fatalf("result = %v", got)
	}
}

func TestSetActivityFeelRejectsInvalidValue(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_activity_feel", map[string]any{
		"activity_id": 12345678901,
		"feel":        60,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if text != "feel must be one of 0, 25, 50, 75, 100" {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("PUT", activityDetailPath); len(calls) != 0 {
		t.Fatalf("PUT calls = %d, want 0", len(calls))
	}
}

func TestGetActivitySplitsTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activitySplitsPath, fixture(t, "MOCK_ACTIVITY_SPLITS"))
	cs := newSession(t, f, nil)

	if _, isErr := callTool(t, cs, "get_activity_splits", map[string]any{"activity_id": 12345678901}); isErr {
		t.Fatal("unexpected error")
	}
	if calls := f.callsMatching("GET", activitySplitsPath); len(calls) != 1 {
		t.Fatalf("splits calls = %d, want 1", len(calls))
	}
}

func TestGetActivitySplitsElevationFields(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activitySplitsPath, fixture(t, "MOCK_ACTIVITY_SPLITS"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activity_splits", map[string]any{"activity_id": 12345678901})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	laps := mutList(t, got["laps"])
	if len(laps) != 2 {
		t.Fatalf("laps = %d", len(laps))
	}
	first := mutMap(t, laps[0])
	second := mutMap(t, laps[1])
	if first["elevation_gain_meters"] != float64(25.5) || first["elevation_loss_meters"] != float64(10.2) {
		t.Fatalf("lap 1 elevation = %v", first)
	}
	if second["elevation_gain_meters"] != float64(15) || second["elevation_loss_meters"] != float64(30.8) {
		t.Fatalf("lap 2 elevation = %v", second)
	}
}

func TestGetActivitySplitsIncludesSwimLengths(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activitySwimSplitsPath, fixture(t, "MOCK_SWIM_ACTIVITY_SPLITS"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activity_splits", map[string]any{"activity_id": 22526515067})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["activity_id"] != float64(22526515067) || got["lap_count"] != float64(1) {
		t.Fatalf("top-level = %v", got)
	}
	lap := mutMap(t, mutList(t, got["laps"])[0])
	for key, want := range map[string]any{
		"start_time_gmt":           "2026-04-14T17:19:08.0",
		"avg_swim_cadence":         float64(22),
		"active_length_count":      float64(92),
		"total_strokes":            float64(1104),
		"avg_strokes":              float64(12),
		"avg_swolf":                float64(45),
		"moving_duration_seconds":  float64(2995.565),
		"elapsed_duration_seconds": float64(2995.565),
		"avg_moving_speed_mps":     float64(0.67566553875138),
		"bmr_calories":             float64(85),
		"avg_stroke_distance":      float64(0),
		"workout_step_index":       float64(0),
	} {
		if lap[key] != want {
			t.Fatalf("%s = %v, want %v", key, lap[key], want)
		}
	}
	if _, exists := lap["start_time"]; exists {
		t.Fatalf("unexpected start_time in lap: %v", lap)
	}
	lengths := mutList(t, lap["lengths"])
	if len(lengths) != 2 {
		t.Fatalf("lengths = %d", len(lengths))
	}
	l0 := mutMap(t, lengths[0])
	if l0["length_number"] != float64(1) || l0["start_time_gmt"] != "2026-04-14T17:19:08.0" ||
		l0["distance_meters"] != float64(22) || l0["duration_seconds"] != float64(31) ||
		l0["stroke"] != "FREESTYLE" {
		t.Fatalf("length 0 = %v", l0)
	}
	if _, exists := l0["start_time"]; exists {
		t.Fatalf("unexpected start_time in length: %v", l0)
	}
}

func TestGetActivityTypedSplitsTool(t *testing.T) {
	f := newFakeGarmin(t)
	splits := map[string]any{
		"runSplits":  mutList(t, mutMap(t, fixture(t, "MOCK_ACTIVITY_SPLITS"))["lapDTOs"]),
		"swimSplits": []any{},
	}
	f.on("GET "+activityTypedSplitsPath, splits)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activity_typed_splits", map[string]any{"activity_id": 12345678901})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, splits)
	if calls := f.callsMatching("GET", activityTypedSplitsPath); len(calls) != 1 {
		t.Fatalf("typed splits calls = %d, want 1", len(calls))
	}
}

func TestGetActivitySplitSummariesTool(t *testing.T) {
	f := newFakeGarmin(t)
	summaries := map[string]any{
		"totalDistance": 5000.0,
		"totalDuration": 1800.0,
		"avgSpeed":      2.78,
		"avgHR":         145,
	}
	f.on("GET "+activitySplitSummPath, summaries)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activity_split_summaries", map[string]any{"activity_id": 12345678901})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, summaries)
	if calls := f.callsMatching("GET", activitySplitSummPath); len(calls) != 1 {
		t.Fatalf("split summary calls = %d, want 1", len(calls))
	}
}

func TestGetActivityWeatherMetricConvertsToCelsius(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activityPTWeatherPath, map[string]any{
		"temp": 57, "apparentTemp": 57, "dewPoint": 50, "relativeHumidity": 77,
		"windSpeed": 8,
	})
	f.on("GET "+userSettingsPathTest, map[string]any{
		"userData": map[string]any{"measurementSystem": "metric"},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activity_weather", map[string]any{"activity_id": 23651251274})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["temperature_unit"] != "C" || got["temperature"] != float64(13.9) {
		t.Fatalf("temperature = %v %v", got["temperature"], got["temperature_unit"])
	}
	if got["apparent_temperature"] != float64(13.9) {
		t.Fatalf("apparent = %v", got["apparent_temperature"])
	}
	if got["dew_point"] != float64(10) {
		t.Fatalf("dew point = %v", got["dew_point"])
	}
	if got["wind_speed"] != float64(8) || got["wind_speed_unit"] != "km/h" {
		t.Fatalf("wind = %v %v", got["wind_speed"], got["wind_speed_unit"])
	}
}

func TestGetActivityWeatherStatuteUSKeepsFahrenheit(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activityServicePathTest+"/1/weather", map[string]any{
		"temp": 57, "dewPoint": 50, "windSpeed": 8,
	})
	f.on("GET "+userSettingsPathTest, map[string]any{
		"userData": map[string]any{"measurementSystem": "statute_us"},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activity_weather", map[string]any{"activity_id": 1})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["temperature_unit"] != "F" || got["temperature"] != float64(57) || got["dew_point"] != float64(50) {
		t.Fatalf("temperature = %v", got)
	}
	if got["wind_speed"] != float64(8) || got["wind_speed_unit"] != "mph" {
		t.Fatalf("wind = %v %v", got["wind_speed"], got["wind_speed_unit"])
	}
	if calls := f.callsMatching("GET", activityServicePathTest+"/1/weather"); len(calls) != 1 {
		t.Fatalf("weather calls = %d, want 1", len(calls))
	}
}

func TestGetActivityHrInTimezonesTool(t *testing.T) {
	f := newFakeGarmin(t)
	hrZones := map[string]any{"zones": []any{
		map[string]any{"zone": 1, "timeInZone": 300, "percentageInZone": 16.7},
		map[string]any{"zone": 2, "timeInZone": 600, "percentageInZone": 33.3},
		map[string]any{"zone": 3, "timeInZone": 900, "percentageInZone": 50.0},
	}}
	f.on("GET "+activityHRZonesPath, hrZones)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activity_hr_in_timezones", map[string]any{"activity_id": 12345678901})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, hrZones)
	if calls := f.callsMatching("GET", activityHRZonesPath); len(calls) != 1 {
		t.Fatalf("hr zone calls = %d, want 1", len(calls))
	}
}

func TestGetActivityPowerInTimezonesTool(t *testing.T) {
	f := newFakeGarmin(t)
	powerZones := map[string]any{"zones": []any{map[string]any{"zone": 1, "timeInZone": 120}}}
	f.on("GET "+activityPowerZonesPath, powerZones)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activity_power_in_timezones", map[string]any{"activity_id": 12345678901})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, powerZones)
	if calls := f.callsMatching("GET", activityPowerZonesPath); len(calls) != 1 {
		t.Fatalf("power zone calls = %d, want 1", len(calls))
	}
}

func TestGetActivityGearTool(t *testing.T) {
	f := newFakeGarmin(t)
	gear := map[string]any{
		"gearId":       123,
		"displayName":  "Running Shoes - Nike",
		"gearTypeName": "SHOE",
	}
	f.on("GET "+gearFilterPathTest, gear)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activity_gear", map[string]any{"activity_id": 12345678901})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, gear)
	calls := f.callsMatching("GET", gearFilterPathTest)
	if len(calls) != 1 {
		t.Fatalf("gear calls = %d, want 1", len(calls))
	}
	if got := calls[0].Query.Get("activityId"); got != "12345678901" {
		t.Fatalf("activityId query = %q", got)
	}
}

func TestGetActivityExerciseSetsTool(t *testing.T) {
	f := newFakeGarmin(t)
	sets := map[string]any{"exercises": []any{map[string]any{
		"exerciseName": "Bench Press",
		"sets": []any{
			map[string]any{"setNumber": 1, "weight": 80.0, "reps": 10},
			map[string]any{"setNumber": 2, "weight": 80.0, "reps": 8},
			map[string]any{"setNumber": 3, "weight": 80.0, "reps": 6},
		},
	}}}
	f.on("GET "+activityExerciseSetPath, sets)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activity_exercise_sets", map[string]any{"activity_id": 12345678901})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, sets)
	if calls := f.callsMatching("GET", activityExerciseSetPath); len(calls) != 1 {
		t.Fatalf("exercise set calls = %d, want 1", len(calls))
	}
}

func TestSetActivityExerciseSetsTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("PUT "+activityExerciseSetPath, nil)
	cs := newSession(t, f, nil)

	sets := []any{
		map[string]any{"category": "deadlift", "exercise_name": "barbell_deadlift",
			"reps": 5, "weight_kg": 152.5, "duration_seconds": 40, "rest_seconds": 180},
		map[string]any{"category": "BENCH_PRESS", "reps": 8, "weight_lb": 135},
		map[string]any{"category": "PULL_UP", "exercise_name": "PULL_UP", "reps": 10},
	}
	text, isErr := callTool(t, cs, "set_activity_exercise_sets", map[string]any{
		"activity_id": 12345678901,
		"sets":        sets,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	calls := f.callsMatching("PUT", activityExerciseSetPath)
	if len(calls) != 1 {
		t.Fatalf("PUT calls = %d, want 1", len(calls))
	}
	sameJSON(t, decodeBody(t, calls[0].Body), map[string]any{
		"activityId": float64(12345678901),
		"exerciseSets": []any{
			map[string]any{"setType": "ACTIVE",
				"exercises":       []any{map[string]any{"category": "DEADLIFT", "name": "BARBELL_DEADLIFT", "probability": float64(100)}},
				"repetitionCount": float64(5), "weight": float64(152500), "duration": float64(40)},
			map[string]any{"setType": "REST", "exercises": []any{}, "repetitionCount": nil, "weight": float64(-1), "duration": float64(180)},
			map[string]any{"setType": "ACTIVE",
				"exercises":       []any{map[string]any{"category": "BENCH_PRESS", "name": nil, "probability": float64(100)}},
				"repetitionCount": float64(8), "weight": float64(61235), "duration": nil},
			map[string]any{"setType": "ACTIVE",
				"exercises":       []any{map[string]any{"category": "PULL_UP", "name": nil, "probability": float64(100)}},
				"repetitionCount": float64(10), "weight": float64(-1), "duration": nil},
		},
	})
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["success"] != true || got["sets_written"] != float64(3) {
		t.Fatalf("result = %v", got)
	}
}

func TestSetActivityExerciseSetsKeepTiming(t *testing.T) {
	f := newFakeGarmin(t)
	unknown := []any{map[string]any{"category": "UNKNOWN", "name": nil, "probability": 99.6}}
	f.on("GET "+activityExerciseSetPath, map[string]any{
		"activityId": 12345678901,
		"exerciseSets": []any{
			map[string]any{"setType": "ACTIVE", "exercises": unknown, "repetitionCount": 4,
				"weight": 0.0, "duration": 192.1, "startTime": "2026-10-08T08:21:23.0", "messageIndex": 0},
			map[string]any{"setType": "REST", "exercises": []any{}, "repetitionCount": nil,
				"weight": nil, "duration": 90.5, "startTime": "2026-10-08T08:24:35.0", "messageIndex": 1},
			map[string]any{"setType": "ACTIVE", "exercises": unknown, "repetitionCount": 6,
				"weight": 0.0, "duration": 130.0, "startTime": "2026-10-08T08:26:06.0", "messageIndex": 2},
		},
	})
	f.on("PUT "+activityExerciseSetPath, nil)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_activity_exercise_sets", map[string]any{
		"activity_id": 12345678901,
		"keep_timing": true,
		"sets": []any{
			map[string]any{"category": "DEADLIFT", "exercise_name": "RACK_PULL", "reps": 4, "weight_kg": 120, "duration_seconds": 999},
			map[string]any{"category": "SQUAT", "reps": 6},
		},
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	calls := f.callsMatching("PUT", activityExerciseSetPath)
	if len(calls) != 1 {
		t.Fatalf("PUT calls = %d, want 1", len(calls))
	}
	body := decodeBody(t, calls[0].Body)
	sent := mutList(t, body["exerciseSets"])
	if len(sent) != 3 {
		t.Fatalf("sent = %d sets", len(sent))
	}
	types := []string{}
	durations := []any{}
	for _, item := range sent {
		m := mutMap(t, item)
		types = append(types, m["setType"].(string))
		durations = append(durations, m["duration"])
	}
	sameJSON(t, types, []any{"ACTIVE", "REST", "ACTIVE"})
	sameJSON(t, durations, []any{float64(192.1), float64(90.5), float64(130)})
	first := mutMap(t, sent[0])
	if first["startTime"] != "2026-10-08T08:21:23.0" {
		t.Fatalf("startTime = %v", first["startTime"])
	}
	sameJSON(t, first["exercises"], []any{
		map[string]any{"category": "DEADLIFT", "name": "RACK_PULL", "probability": float64(100)},
	})
	if first["repetitionCount"] != float64(4) || first["weight"] != float64(120000) {
		t.Fatalf("first = %v", first)
	}
	third := mutMap(t, sent[2])
	thirdExercises := mutList(t, third["exercises"])
	if mutMap(t, thirdExercises[0])["category"] != "SQUAT" || third["weight"] != float64(-1) {
		t.Fatalf("third = %v", third)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["success"] != true {
		t.Fatalf("result = %v", got)
	}
}

func TestSetActivityExerciseSetsKeepTimingRejectsCountMismatch(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activityExerciseSetPath, map[string]any{
		"exerciseSets": []any{map[string]any{"setType": "ACTIVE", "exercises": []any{}}},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_activity_exercise_sets", map[string]any{
		"activity_id": 12345678901,
		"keep_timing": true,
		"sets":        []any{map[string]any{"category": "SQUAT"}, map[string]any{"category": "SQUAT"}},
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "has 1 active sets, got 2") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("PUT", activityExerciseSetPath); len(calls) != 0 {
		t.Fatalf("PUT calls = %d, want 0", len(calls))
	}
}

func TestSetActivityExerciseSetsRejectsBadInput(t *testing.T) {
	cases := []struct {
		name string
		sets []any
		want string
	}{
		{"empty", []any{}, "sets cannot be empty"},
		{"missing category", []any{map[string]any{"reps": 5}}, "set 1: category is required"},
		{"two weights", []any{map[string]any{"category": "SQUAT", "weight_kg": 100, "weight_lb": 220}}, "set 1: give weight_kg or weight_lb, not both"},
		{"fractional reps", []any{map[string]any{"category": "SQUAT", "reps": 5.5}}, "set 1: reps must be"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGarmin(t)
			cs := newSession(t, f, nil)
			text, isErr := callTool(t, cs, "set_activity_exercise_sets", map[string]any{
				"activity_id": 12345678901,
				"sets":        tc.sets,
			})
			if isErr {
				t.Fatalf("unexpected error: %q", text)
			}
			if !strings.Contains(text, tc.want) {
				t.Fatalf("text = %q, want contains %q", text, tc.want)
			}
			if calls := f.callsMatching("PUT", activityExerciseSetPath); len(calls) != 0 {
				t.Fatalf("PUT calls = %d, want 0", len(calls))
			}
		})
	}
}

func TestCountActivitiesTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activityCountPathTest, map[string]any{"totalCount": fixture(t, "MOCK_ACTIVITY_COUNT")})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "count_activities", nil)
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["total_activities"] != float64(523) {
		t.Fatalf("result = %v", got)
	}
	if calls := f.callsMatching("GET", activityCountPathTest); len(calls) != 1 {
		t.Fatalf("count calls = %d, want 1", len(calls))
	}
}

func TestGetActivitiesTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activitySearchPathTest, fixture(t, "MOCK_ACTIVITIES"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activities", map[string]any{"start": 0, "limit": 20})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["start"] != float64(0) || got["limit"] != float64(20) || got["count"] != float64(2) || got["has_more"] != false {
		t.Fatalf("result = %v", got)
	}
	calls := f.callsMatching("GET", activitySearchPathTest)
	if len(calls) != 1 {
		t.Fatalf("activity calls = %d, want 1", len(calls))
	}
	if calls[0].Query.Get("start") != "0" || calls[0].Query.Get("limit") != "20" {
		t.Fatalf("query = %v", calls[0].Query)
	}
}

func TestGetActivityTypesTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activityTypesPathTest, fixture(t, "MOCK_ACTIVITY_TYPES"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activity_types", nil)
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["count"] != float64(4) {
		t.Fatalf("count = %v", got["count"])
	}
	types := mutList(t, got["activity_types"])
	first := mutMap(t, types[0])
	if first["type_id"] != float64(1) || first["type_key"] != "running" || first["display_name"] != "Running" || first["is_hidden"] != false {
		t.Fatalf("first = %v", first)
	}
	if _, exists := first["parent_type_id"]; exists {
		t.Fatalf("parent_type_id should be omitted: %v", first)
	}
	if calls := f.callsMatching("GET", activityTypesPathTest); len(calls) != 1 {
		t.Fatalf("type calls = %d, want 1", len(calls))
	}
}

func TestGetActivitiesIncludesEventType(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activitySearchPathTest, fixture(t, "MOCK_ACTIVITIES"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activities", map[string]any{"start": 0, "limit": 20})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	activities := mutList(t, got["activities"])
	if mutMap(t, activities[0])["event_type"] != "race" || mutMap(t, activities[1])["event_type"] != "training" {
		t.Fatalf("activities = %v", activities)
	}
}

func TestGetActivitiesByDateIncludesEventType(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activitySearchPathTest, fixture(t, "MOCK_ACTIVITIES"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activities_by_date", map[string]any{
		"start_date": "2024-01-08",
		"end_date":   "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	activities := mutList(t, got["activities"])
	if mutMap(t, activities[0])["event_type"] != "race" || mutMap(t, activities[1])["event_type"] != "training" {
		t.Fatalf("activities = %v", activities)
	}
}

func TestGetActivitiesOmitsEventTypeWhenAbsent(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activitySearchPathTest, []any{map[string]any{
		"activityId":     99999,
		"activityName":   "Old Activity",
		"activityType":   map[string]any{"typeKey": "running", "typeId": 1},
		"startTimeLocal": "2024-01-01 08:00:00",
		"duration":       1200.0,
	}})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activities", map[string]any{"start": 0, "limit": 20})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	first := mutMap(t, mutList(t, got["activities"])[0])
	if _, exists := first["event_type"]; exists {
		t.Fatalf("event_type should be absent: %v", first)
	}
}

func TestGetActivityIncludesEventType(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activityDetailPath, fixture(t, "MOCK_ACTIVITY_DETAILS"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activity", map[string]any{"activity_id": 12345678901})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["event_type"] != "race" {
		t.Fatalf("event_type = %v", got["event_type"])
	}
}

func TestGetActivityIncludesDescription(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activityDetailPath, fixture(t, "MOCK_ACTIVITY_DETAILS"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activity", map[string]any{"activity_id": 12345678901})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["description"] != "Felt strong throughout. New shoes." {
		t.Fatalf("description = %v", got["description"])
	}
}

func TestGetActivityEventTypeUsesEventTypeDTO(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activityServicePathTest+"/1", map[string]any{
		"activityId":   1,
		"activityName": "Race",
		"eventTypeDTO": map[string]any{"typeKey": "race", "typeId": 1},
		"eventType":    nil,
		"summaryDTO":   map[string]any{},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activity", map[string]any{"activity_id": 1})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["event_type"] != "race" {
		t.Fatalf("event_type = %v", got["event_type"])
	}
}

func TestGetActivitiesByDateNoData(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activitySearchPathTest, []any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activities_by_date", map[string]any{
		"start_date": "2024-01-08",
		"end_date":   "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["count"] != float64(0) || got["has_more"] != false {
		t.Fatalf("result = %v", got)
	}
	sameJSON(t, got["activities"], []any{})
}

func TestGetActivityException(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activityDetailPath, status(500, map[string]any{"message": "API Error"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activity", map[string]any{"activity_id": 12345678901})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	if !strings.Contains(text, "Error retrieving activity") {
		t.Fatalf("text = %q", text)
	}
}

func TestGetActivityNotFound(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activityServicePathTest+"/99999999999", nil)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activity", map[string]any{"activity_id": 99999999999})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "No activity found with ID 99999999999") {
		t.Fatalf("text = %q", text)
	}
}

func TestSetActivityNameException(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("PUT "+activityDetailPath, status(500, map[string]any{"message": "API Error"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_activity_name", map[string]any{
		"activity_id":   12345678901,
		"activity_name": "Morning Run",
	})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	msg, _ := got["message"].(string)
	if !strings.Contains(msg, "Error updating activity name: API Error") {
		t.Fatalf("message = %q", msg)
	}
}

func TestGetActivitiesByDatePaginationMetadataFirstPage(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activitySearchPathTest, fixture(t, "MOCK_ACTIVITIES"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activities_by_date", map[string]any{
		"start_date": "2024-01-01",
		"end_date":   "2024-01-31",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["page"] != float64(0) || got["page_size"] != float64(100) || got["count"] != float64(2) {
		t.Fatalf("result = %v", got)
	}
	dr := mutMap(t, got["date_range"])
	if dr["start"] != "2024-01-01" || dr["end"] != "2024-01-31" {
		t.Fatalf("date_range = %v", dr)
	}
}

func TestGetActivitiesByDateHasMoreWhenFullPage(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activitySearchPathTest, []any{commonActivity(), commonActivity(), commonActivity()})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activities_by_date", map[string]any{
		"start_date": "2024-01-01",
		"end_date":   "2024-12-31",
		"page_size":  3,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["has_more"] != true || got["next_page"] != float64(1) || got["count"] != float64(3) {
		t.Fatalf("result = %v", got)
	}
}

func TestGetActivitiesByDateNoMoreOnPartialPage(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activitySearchPathTest, fixture(t, "MOCK_ACTIVITIES"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activities_by_date", map[string]any{
		"start_date": "2024-01-01",
		"end_date":   "2024-01-31",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["has_more"] != false {
		t.Fatalf("has_more = %v", got["has_more"])
	}
	if _, exists := got["next_page"]; exists {
		t.Fatalf("next_page should be absent: %v", got)
	}
}

func TestGetActivitiesByDateSecondPageOffset(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activitySearchPathTest, fixture(t, "MOCK_ACTIVITIES"))
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "get_activities_by_date", map[string]any{
		"start_date": "2024-01-01",
		"end_date":   "2024-12-31",
		"page":       1,
		"page_size":  50,
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	calls := f.callsMatching("GET", activitySearchPathTest)
	if len(calls) != 1 {
		t.Fatalf("activity calls = %d, want 1", len(calls))
	}
	if calls[0].Query.Get("start") != "50" || calls[0].Query.Get("limit") != "50" {
		t.Fatalf("query = %v", calls[0].Query)
	}
}

func TestGetActivitiesByDatePageSizeCappedAt200(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activitySearchPathTest, fixture(t, "MOCK_ACTIVITIES"))
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "get_activities_by_date", map[string]any{
		"start_date": "2024-01-01",
		"end_date":   "2024-12-31",
		"page_size":  999,
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	calls := f.callsMatching("GET", activitySearchPathTest)
	if want := "200"; calls[0].Query.Get("limit") != want {
		t.Fatalf("limit = %q, want %q", calls[0].Query.Get("limit"), want)
	}
}

func TestGetActivitiesByDateDefaultPageSizeIs100(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activitySearchPathTest, fixture(t, "MOCK_ACTIVITIES"))
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "get_activities_by_date", map[string]any{
		"start_date": "2024-01-01",
		"end_date":   "2024-01-31",
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	calls := f.callsMatching("GET", activitySearchPathTest)
	if calls[0].Query.Get("limit") != "100" || calls[0].Query.Get("start") != "0" {
		t.Fatalf("query = %v", calls[0].Query)
	}
}

func TestCreateManualActivitySuccess(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+activityServicePathTest, map[string]any{"activityId": 999})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "create_manual_activity", map[string]any{
		"type_key":         "yoga",
		"date":             "2024-03-01",
		"duration_minutes": 60,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["success"] != true {
		t.Fatalf("result = %v", got)
	}
	sameJSON(t, got["activity"], map[string]any{"activityId": float64(999)})
	calls := f.callsMatching("POST", activityServicePathTest)
	if len(calls) != 1 {
		t.Fatalf("POST calls = %d, want 1", len(calls))
	}
	sameJSON(t, decodeBody(t, calls[0].Body), map[string]any{
		"activityTypeDTO":      map[string]any{"typeKey": "yoga"},
		"accessControlRuleDTO": map[string]any{"typeId": float64(2), "typeKey": "private"},
		"timeZoneUnitDTO":      map[string]any{"unitKey": "UTC"},
		"activityName":         "Yoga",
		"metadataDTO":          map[string]any{"autoCalcCalories": true},
		"summaryDTO": map[string]any{
			"startTimeLocal": "2024-03-01T09:00:00.000",
			"distance":       float64(0),
			"duration":       float64(3600),
		},
	})
}

func TestCreateManualActivityCustomFields(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+activityServicePathTest, map[string]any{"activityId": 123})
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "create_manual_activity", map[string]any{
		"type_key":         "strength_training",
		"date":             "2024-03-15",
		"duration_minutes": 45,
		"start_time":       "07:30",
		"activity_name":    "Morning Weights",
		"distance_km":      0.0,
		"time_zone":        "Europe/Lisbon",
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	calls := f.callsMatching("POST", activityServicePathTest)
	body := decodeBody(t, calls[0].Body)
	if body["activityName"] != "Morning Weights" {
		t.Fatalf("activityName = %v", body["activityName"])
	}
	if mutMap(t, body["timeZoneUnitDTO"])["unitKey"] != "Europe/Lisbon" {
		t.Fatalf("timeZone = %v", body["timeZoneUnitDTO"])
	}
	summary := mutMap(t, body["summaryDTO"])
	if summary["startTimeLocal"] != "2024-03-15T07:30:00.000" || summary["distance"] != float64(0) || summary["duration"] != float64(2700) {
		t.Fatalf("summary = %v", summary)
	}
}

func TestCreateManualActivityDefaultNameFromTypeKey(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+activityServicePathTest, map[string]any{})
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "create_manual_activity", map[string]any{
		"type_key":         "indoor_cycling",
		"date":             "2024-04-01",
		"duration_minutes": 30,
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	body := decodeBody(t, f.callsMatching("POST", activityServicePathTest)[0].Body)
	if body["activityName"] != "Indoor Cycling" {
		t.Fatalf("activityName = %v", body["activityName"])
	}
}

func TestCreateManualActivityRejectsZeroDuration(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "create_manual_activity", map[string]any{
		"type_key":         "yoga",
		"date":             "2024-03-01",
		"duration_minutes": 0,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "Error") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("POST", activityServicePathTest); len(calls) != 0 {
		t.Fatalf("POST calls = %d, want 0", len(calls))
	}
}

func TestCreateManualActivityRejectsEmptyTypeKey(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "create_manual_activity", map[string]any{
		"type_key":         "  ",
		"date":             "2024-03-01",
		"duration_minutes": 30,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "Error") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("POST", activityServicePathTest); len(calls) != 0 {
		t.Fatalf("POST calls = %d, want 0", len(calls))
	}
}

func TestCreateManualActivityException(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+activityServicePathTest, status(400, map[string]any{"message": "Garmin API error"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "create_manual_activity", map[string]any{
		"type_key":         "yoga",
		"date":             "2024-03-01",
		"duration_minutes": 60,
	})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	if !strings.Contains(text, "Error creating manual activity") || !strings.Contains(text, "Garmin API error") {
		t.Fatalf("text = %q", text)
	}
}

func TestCreateManualActivityStripsTypeBeforePayloadAndDefaultName(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+activityServicePathTest, map[string]any{"activityId": 999})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "create_manual_activity", map[string]any{
		"type_key":         " yoga ",
		"date":             "2024-03-01",
		"duration_minutes": 30,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["success"] != true {
		t.Fatalf("result = %v", got)
	}
	body := decodeBody(t, f.callsMatching("POST", activityServicePathTest)[0].Body)
	if mutMap(t, body["activityTypeDTO"])["typeKey"] != "yoga" || body["activityName"] != "Yoga" {
		t.Fatalf("body = %v", body)
	}
}

func TestCreateManualActivityRejectsInvalidTimestampOrDistance(t *testing.T) {
	cases := []struct {
		name  string
		field string
		value any
	}{
		{"impossible date", "date", "2024-02-30"},
		{"slashed date", "date", "03/01/2024"},
		{"short date", "date", "2024-3-1"},
		{"seconds in time", "start_time", "09:00:00"},
		{"hour not padded", "start_time", "9:00"},
		{"hour out of range", "start_time", "24:00"},
		{"negative distance", "distance_km", -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGarmin(t)
			cs := newSession(t, f, nil)
			args := map[string]any{
				"type_key":         "yoga",
				"date":             "2024-03-01",
				"duration_minutes": 30,
			}
			args[tc.field] = tc.value
			text, isErr := callTool(t, cs, "create_manual_activity", args)
			if isErr {
				t.Fatalf("unexpected error: %q", text)
			}
			if !strings.Contains(text, "Error") || !strings.Contains(text, tc.field) {
				t.Fatalf("text = %q, want field %q", text, tc.field)
			}
			if calls := f.callsMatching("POST", activityServicePathTest); len(calls) != 0 {
				t.Fatalf("POST calls = %d, want 0", len(calls))
			}
		})
	}
}

func TestCreateManualActivityReportsWriteCommitUncertainty(t *testing.T) {
	cases := []struct {
		name      string
		failure   string
		committed any
	}{
		{"read_timeout", "timeout", "unknown"},
		{"server_error", "server", "unknown"},
		{"rejected", "rejected", false},
		{"connect_timeout", "connect", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGarmin(t)
			env := map[string]string(nil)
			switch tc.failure {
			case "timeout":
				f.on("POST "+activityServicePathTest, delay{wait: time.Second, body: map[string]any{}})
				env = map[string]string{"GARMIN_MCP_CALL_TIMEOUT": "0.1"}
			case "server":
				f.on("POST "+activityServicePathTest, status(503, map[string]any{"message": "gateway"}))
			case "rejected":
				f.on("POST "+activityServicePathTest, status(400, map[string]any{"message": "invalid request"}))
			case "connect":
				break
			}
			cs := newSession(t, f, env)
			if tc.failure == "connect" {
				f.Close()
			}

			text, isErr := callTool(t, cs, "create_manual_activity", map[string]any{
				"type_key":         "yoga",
				"date":             "2024-03-01",
				"duration_minutes": 30,
			})
			if !isErr {
				t.Fatalf("expected error, got %q", text)
			}
			var got map[string]any
			decodeJSON(t, text, &got)
			if got["status"] != "error" {
				t.Fatalf("status = %v", got["status"])
			}
			if got["write_committed"] != tc.committed {
				t.Fatalf("write_committed = %v, want %v", got["write_committed"], tc.committed)
			}
			if tc.committed == "unknown" {
				hint, _ := got["hint"].(string)
				if !strings.Contains(hint, "before retrying") {
					t.Fatalf("hint = %q", hint)
				}
			}
		})
	}
}

func TestCreateManualActivityReportsCommittedWriteIfResponseCannotSerialize(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+activityServicePathTest, rawBody([]byte("not json")))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "create_manual_activity", map[string]any{
		"type_key":         "yoga",
		"date":             "2024-03-01",
		"duration_minutes": 30,
	})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["write_committed"] != true || got["stage"] != "response_parse" {
		t.Fatalf("result = %v", got)
	}
}

func TestGetActivitySplitsWrappedLapDistanceIsNulled(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activityWrappedSplitsURL, map[string]any{
		"activityId": 24687347098,
		"lapDTOs": []any{
			map[string]any{"lapIndex": 4, "distance": 680.96, "duration": 301.797},
			map[string]any{"lapIndex": 5, "distance": 42949672.0, "duration": 0.0},
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activity_splits", map[string]any{"activity_id": 24687347098})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	laps := mutList(t, got["laps"])
	first := mutMap(t, laps[0])
	second := mutMap(t, laps[1])
	if first["distance_meters"] != float64(680.96) {
		t.Fatalf("first = %v", first)
	}
	if _, exists := first["distance_invalid"]; exists {
		t.Fatalf("first distance_invalid should be absent: %v", first)
	}
	if _, exists := second["distance_meters"]; exists {
		t.Fatalf("second distance_meters should be absent: %v", second)
	}
	if second["distance_invalid"] != true || second["duration_seconds"] != float64(0) {
		t.Fatalf("second = %v", second)
	}
}

func TestDeleteActivityRequiresConfirmation(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activityDetailPath, map[string]any{
		"activityId":      12345678901,
		"activityName":    "Morning Run",
		"activityTypeDTO": map[string]any{"typeKey": "running"},
		"summaryDTO": map[string]any{
			"startTimeLocal": "2024-01-15 07:00:00",
			"distance":       5000.0,
			"duration":       1800.0,
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "delete_activity", map[string]any{"activity_id": 12345678901})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["status"] != "needs_confirmation" {
		t.Fatalf("status = %v", got["status"])
	}
	if mutMap(t, got["activity"])["name"] != "Morning Run" {
		t.Fatalf("activity = %v", got["activity"])
	}
	if calls := f.callsMatching("DELETE", activityDetailPath); len(calls) != 0 {
		t.Fatalf("DELETE calls = %d, want 0", len(calls))
	}
}

func TestDeleteActivityWithConfirm(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activityDetailPath, map[string]any{
		"activityId":      12345678901,
		"activityName":    "Morning Run",
		"activityTypeDTO": map[string]any{"typeKey": "running"},
		"summaryDTO":      map[string]any{"startTimeLocal": "2024-01-15 07:00:00"},
	})
	f.on("DELETE "+activityDetailPath, respStatus{code: 204})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "delete_activity", map[string]any{
		"activity_id": 12345678901,
		"confirm":     true,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["status"] != "deleted" {
		t.Fatalf("status = %v", got["status"])
	}
	if mutMap(t, got["activity"])["activity_id"] != float64(12345678901) {
		t.Fatalf("activity = %v", got["activity"])
	}
	if calls := f.callsMatching("DELETE", activityDetailPath); len(calls) != 1 {
		t.Fatalf("DELETE calls = %d, want 1", len(calls))
	}
}

func TestDeleteActivityException(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activityDetailPath, map[string]any{"activityName": "Morning Run"})
	f.on("DELETE "+activityDetailPath, status(500, map[string]any{"message": "Garmin API error"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "delete_activity", map[string]any{
		"activity_id": 12345678901,
		"confirm":     true,
	})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	if !strings.Contains(text, "Error deleting activity") || !strings.Contains(text, "Garmin API error") {
		t.Fatalf("text = %q", text)
	}
}

func TestDeleteActivityLogsDeletionToStderr(t *testing.T) {
	var log bytes.Buffer
	f := newFakeGarmin(t)
	f.on("GET "+activityDetailPath, map[string]any{
		"activityId":   12345678901,
		"activityName": "Morning Run",
	})
	f.on("DELETE "+activityDetailPath, respStatus{code: 204})
	cs := newSessionWithLog(t, f, &log)

	if _, isErr := callTool(t, cs, "delete_activity", map[string]any{
		"activity_id": 12345678901,
		"confirm":     true,
	}); isErr {
		t.Fatal("unexpected error")
	}
	if !strings.Contains(log.String(), "delete_activity: deleted activity 12345678901 name='Morning Run'") {
		t.Fatalf("log = %q", log.String())
	}
}

func TestPreviewFromActivityDetails(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activityServicePathTest+"/99", map[string]any{
		"activityName":    "Morning Run",
		"activityTypeDTO": map[string]any{"typeKey": "running"},
		"summaryDTO": map[string]any{
			"startTimeLocal": "2024-01-15 07:00:00",
			"distance":       5000.0,
			"duration":       1800.0,
		},
	})
	preview := activityPreview(context.Background(), healthClient(t, f), 99)
	if num(t, preview["activity_id"]) != 99 {
		t.Fatalf("activity_id = %v", preview["activity_id"])
	}
	if preview["name"] != "Morning Run" || preview["type"] != "running" || preview["distance_meters"] != float64(5000) {
		t.Fatalf("preview = %v", preview)
	}
}

func TestPreviewFallsBackToActivityListOnDetailsError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+activityServicePathTest+"/99", status(403, map[string]any{"message": "Forbidden"}))
	f.on("GET "+activitySearchPathTest, []any{map[string]any{
		"activityId":     99,
		"activityName":   "Easy Ride",
		"activityType":   map[string]any{"typeKey": "cycling"},
		"startTimeLocal": "2024-01-14 16:00:00",
		"distance":       20000.0,
		"duration":       3600.0,
	}})
	preview := activityPreview(context.Background(), healthClient(t, f), 99)
	if preview["name"] != "Easy Ride" || preview["type"] != "cycling" {
		t.Fatalf("preview = %v", preview)
	}
}
