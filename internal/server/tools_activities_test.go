package server

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

// Ported from tests/integration/test_activity_management_tools.py and tests/unit/test_delete_activity.py.

const (
	actID    = "12345678901"
	actBase  = "/activity-service/activity/"
	actRoute = "GET " + actBase + actID
	actPut   = "PUT " + actBase + actID
	typesURL = "GET /activity-service/activity/activityTypes"
)

// putJSON returns the decoded body of the single request to route.
func putJSON(t *testing.T, f *fakeGarmin, route string) map[string]any {
	t.Helper()
	reqs := f.requests(route)
	if len(reqs) != 1 {
		t.Fatalf("%d requests to %s, want 1", len(reqs), route)
	}
	return asMap(decode(t, string(reqs[0].Body)))
}

func resultMap(t *testing.T, text string) map[string]any {
	t.Helper()
	m, ok := decode(t, text).(map[string]any)
	if !ok {
		t.Fatalf("not an object: %s", text)
	}
	return m
}

func wantQuery(t *testing.T, got url.Values, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("query = %v, want %v", got, want)
	}
	for k, v := range want {
		if got.Get(k) != v {
			t.Errorf("query %s = %q, want %q (%v)", k, got.Get(k), v, got)
		}
	}
}

func noRequests(t *testing.T, f *fakeGarmin, method string) {
	t.Helper()
	for _, c := range f.requests("") {
		if c.Method == method {
			t.Errorf("unexpected %s %s", c.Method, c.Path)
		}
	}
}

func TestActivityToolsRegistered(t *testing.T) {
	want := strings.Fields(`get_activities_by_date get_activities_fordate get_activity set_activity_name set_activity_type
		set_activity_description set_activity_event_type set_perceived_effort set_activity_feel get_activity_splits
		get_activity_typed_splits get_activity_split_summaries get_activity_weather get_activity_hr_in_timezones
		get_activity_power_in_timezones get_activity_gear get_activity_exercise_sets set_activity_exercise_sets
		count_activities get_activities create_manual_activity delete_activity get_activity_types
		get_gear add_gear_to_activity remove_gear_from_activity`)
	if len(want) != 26 {
		t.Fatalf("%d names in the test", len(want))
	}
	have := map[string]bool{}
	for _, n := range toolNames(t, newSession(t, newFakeGarmin(t), nil)) {
		have[n] = true
	}
	for _, n := range want {
		if !have[n] {
			t.Errorf("tool %s is not registered", n)
		}
	}
}

func TestActivityToolSchemas(t *testing.T) {
	cs := newSession(t, newFakeGarmin(t), nil)
	for _, tool := range listTools(t, cs) {
		switch tool.Name {
		case "get_activity", "delete_activity", "set_activity_name":
			props := asMap(asMap(tool.InputSchema)["properties"])
			if _, ok := props["activity_id"]; !ok {
				t.Errorf("%s has no activity_id: %v", tool.Name, props)
			}
			if asMap(tool.InputSchema)["additionalProperties"] != false {
				t.Errorf("%s allows extra properties", tool.Name)
			}
		}
	}
}

// ── get_activities_by_date ─────────────────────────────────────────────────

func TestGetActivitiesByDateTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(activitiesPath, fixture(t, "MOCK_ACTIVITIES"))
	callOK(t, newSession(t, f, nil), "get_activities_by_date", map[string]any{"start_date": "2024-01-08", "end_date": "2024-01-15"})
	wantQuery(t, query(t, f, activitiesPath), map[string]string{"startDate": "2024-01-08", "endDate": "2024-01-15", "start": "0", "limit": "100"})
}

func TestGetActivitiesByDateHandlesNullActivityType(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(activitiesPath, []any{map[string]any{"activityId": 123, "activityName": "Untyped activity", "activityType": nil}})
	text := callOK(t, newSession(t, f, nil), "get_activities_by_date", map[string]any{"start_date": "2024-01-08", "end_date": "2024-01-15"})
	want := []any{map[string]any{"id": 123.0, "name": "Untyped activity"}}
	if got := asMap(decode(t, text))["activities"]; !reflect.DeepEqual(got, want) {
		t.Errorf("activities = %v", got)
	}
}

func TestGetActivitiesByDateWithType(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(activitiesPath, asList(fixture(t, "MOCK_ACTIVITIES"))[:1])
	callOK(t, newSession(t, f, nil), "get_activities_by_date",
		map[string]any{"start_date": "2024-01-08", "end_date": "2024-01-15", "activity_type": "running"})
	wantQuery(t, query(t, f, activitiesPath), map[string]string{"startDate": "2024-01-08", "endDate": "2024-01-15", "start": "0", "limit": "100", "activityType": "running"})
}

func TestGetActivitiesByDateIncludesEventType(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(activitiesPath, fixture(t, "MOCK_ACTIVITIES"))
	text := callOK(t, newSession(t, f, nil), "get_activities_by_date", map[string]any{"start_date": "2024-01-08", "end_date": "2024-01-15"})
	acts := asList(asMap(decode(t, text))["activities"])
	if asMap(acts[0])["event_type"] != "race" || asMap(acts[1])["event_type"] != "training" {
		t.Errorf("activities = %v", acts)
	}
}

func TestGetActivitiesByDateNoData(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(activitiesPath, []any{})
	text := callOK(t, newSession(t, f, nil), "get_activities_by_date", map[string]any{"start_date": "2024-01-08", "end_date": "2024-01-15"})
	want := map[string]any{"count": 0.0, "page": 0.0, "page_size": 100.0, "has_more": false,
		"date_range": map[string]any{"start": "2024-01-08", "end": "2024-01-15"}, "activities": []any{}}
	if got := decode(t, text); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestGetActivitiesByDatePaginationMetadataFirstPage(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(activitiesPath, fixture(t, "MOCK_ACTIVITIES"))
	text := callOK(t, newSession(t, f, nil), "get_activities_by_date", map[string]any{"start_date": "2024-01-01", "end_date": "2024-01-31"})
	d := resultMap(t, text)
	if d["page"] != 0.0 || d["page_size"] != 100.0 || d["count"] != 2.0 ||
		!reflect.DeepEqual(d["date_range"], map[string]any{"start": "2024-01-01", "end": "2024-01-31"}) {
		t.Errorf("got %v", d)
	}
}

func TestGetActivitiesByDateHasMoreWhenFullPage(t *testing.T) {
	f := newFakeGarmin(t)
	a := asList(fixture(t, "MOCK_ACTIVITIES"))
	f.on(activitiesPath, []any{a[0], a[1], a[0]})
	text := callOK(t, newSession(t, f, nil), "get_activities_by_date",
		map[string]any{"start_date": "2024-01-01", "end_date": "2024-12-31", "page_size": 3})
	d := resultMap(t, text)
	if d["has_more"] != true || d["next_page"] != 1.0 || d["count"] != 3.0 {
		t.Errorf("got %v", d)
	}
}

func TestGetActivitiesByDateNoMoreOnPartialPage(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(activitiesPath, fixture(t, "MOCK_ACTIVITIES"))
	text := callOK(t, newSession(t, f, nil), "get_activities_by_date", map[string]any{"start_date": "2024-01-01", "end_date": "2024-01-31"})
	d := resultMap(t, text)
	if _, has := d["next_page"]; d["has_more"] != false || has {
		t.Errorf("got %v", d)
	}
}

func TestGetActivitiesByDateSecondPageOffset(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(activitiesPath, fixture(t, "MOCK_ACTIVITIES"))
	callOK(t, newSession(t, f, nil), "get_activities_by_date",
		map[string]any{"start_date": "2024-01-01", "end_date": "2024-12-31", "page": 1, "page_size": 50})
	wantQuery(t, query(t, f, activitiesPath), map[string]string{"startDate": "2024-01-01", "endDate": "2024-12-31", "start": "50", "limit": "50"})
}

func TestGetActivitiesByDatePageSizeCappedAt200(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(activitiesPath, fixture(t, "MOCK_ACTIVITIES"))
	callOK(t, newSession(t, f, nil), "get_activities_by_date",
		map[string]any{"start_date": "2024-01-01", "end_date": "2024-12-31", "page_size": 999})
	if got := query(t, f, activitiesPath).Get("limit"); got != "200" {
		t.Errorf("limit = %q", got)
	}
}

func TestGetActivitiesByDateDefaultPageSizeIs100(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(activitiesPath, fixture(t, "MOCK_ACTIVITIES"))
	callOK(t, newSession(t, f, nil), "get_activities_by_date", map[string]any{"start_date": "2024-01-01", "end_date": "2024-01-31"})
	q := query(t, f, activitiesPath)
	if q.Get("limit") != "100" || q.Get("start") != "0" {
		t.Errorf("query = %v", q)
	}
}

func TestGetActivitiesByDateClampsPageSizeToOne(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(activitiesPath, []any{})
	callOK(t, newSession(t, f, nil), "get_activities_by_date",
		map[string]any{"start_date": "2024-01-01", "end_date": "2024-12-31", "page_size": 0, "page": 3})
	q := query(t, f, activitiesPath)
	if q.Get("limit") != "1" || q.Get("start") != "3" {
		t.Errorf("query = %v", q)
	}
}

// ── get_activities_fordate / get_activity ──────────────────────────────────

func TestGetActivitiesFordateTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /mobile-gateway/heartRate/forDate/2024-01-15", map[string]any{
		"ActivitiesForDay": map[string]any{"payload": []any{asList(fixture(t, "MOCK_ACTIVITIES"))[0]}},
	})
	text := callOK(t, newSession(t, f, nil), "get_activities_fordate", map[string]any{"date": "2024-01-15"})
	d := resultMap(t, text)
	a := asMap(asList(d["activities"])[0])
	if d["count"] != 1.0 || a["id"] != 12345678901.0 || a["event_type"] != "race" || d["date"] != "2024-01-15" {
		t.Errorf("got %v", d)
	}
}

func TestGetActivitiesFordateNoData(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /mobile-gateway/heartRate/forDate/2024-01-15", map[string]any{"ActivitiesForDay": map[string]any{"payload": []any{}}})
	text := callOK(t, newSession(t, f, nil), "get_activities_fordate", map[string]any{"date": "2024-01-15"})
	if text != "No activities found for 2024-01-15" {
		t.Error(text)
	}
	text, isErr := callTool(t, newSession(t, f, nil), "get_activities_fordate", map[string]any{"date": "2024-02-30"})
	if !isErr || !strings.HasPrefix(text, "Error retrieving activities for date: invalid fordate:") {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetActivityTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(actRoute, fixture(t, "MOCK_ACTIVITY_DETAILS"))
	text := callOK(t, newSession(t, f, nil), "get_activity", map[string]any{"activity_id": 12345678901})
	d := resultMap(t, text)
	if d["distance_meters"] != 5000.0 || d["duration_seconds"] != 1800.0 || d["calories"] != 350.0 || d["avg_hr_bpm"] != 145.0 ||
		d["elevation_gain_meters"] != 50.5 || d["lap_count"] != 2.0 || d["id"] != 12345678901.0 || d["parent_type"] != 17.0 {
		t.Errorf("got %v", d)
	}
	if n := len(f.requests(actRoute)); n != 1 {
		t.Errorf("%d requests", n)
	}
}

func TestGetActivityHandlesNullNestedSections(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+actBase+"123", map[string]any{"activityId": 123, "summaryDTO": nil, "activityTypeDTO": nil, "metadataDTO": nil})
	text := callOK(t, newSession(t, f, nil), "get_activity", map[string]any{"activity_id": 123})
	if got := decode(t, text); !reflect.DeepEqual(got, map[string]any{"id": 123.0}) {
		t.Errorf("got %v", got)
	}
}

func TestGetActivityAcceptsStringId(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+actBase+"22833209898", fixture(t, "MOCK_ACTIVITY_DETAILS"))
	callOK(t, newSession(t, f, nil), "get_activity", map[string]any{"activity_id": "22833209898"})
	if n := len(f.requests("GET " + actBase + "22833209898")); n != 1 {
		t.Errorf("%d requests", n)
	}
}

func TestGetActivityRejectsNonNumericId(t *testing.T) {
	f := newFakeGarmin(t)
	text, isErr := callTool(t, newSession(t, f, nil), "get_activity", map[string]any{"activity_id": "abc"})
	if !isErr || text != "Error retrieving activity: invalid literal for int() with base 10: 'abc'" {
		t.Errorf("%v %q", isErr, text)
	}
	text, isErr = callTool(t, newSession(t, f, nil), "get_activity", map[string]any{"activity_id": 0})
	if !isErr || text != "Error retrieving activity: activity_id must be a positive integer, got: 0" {
		t.Errorf("%v %q", isErr, text)
	}
	if n := len(f.requests("")); n != 0 {
		t.Errorf("%d requests", n)
	}
}

func TestGetActivityIncludesEventType(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(actRoute, fixture(t, "MOCK_ACTIVITY_DETAILS"))
	d := resultMap(t, callOK(t, newSession(t, f, nil), "get_activity", map[string]any{"activity_id": 12345678901}))
	if d["event_type"] != "race" {
		t.Errorf("got %v", d)
	}
}

func TestGetActivityIncludesDescription(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(actRoute, fixture(t, "MOCK_ACTIVITY_DETAILS"))
	d := resultMap(t, callOK(t, newSession(t, f, nil), "get_activity", map[string]any{"activity_id": 12345678901}))
	if d["description"] != "Felt strong throughout. New shoes." {
		t.Errorf("got %v", d)
	}
}

func TestGetActivityEventTypeUsesEventTypeDto(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+actBase+"1", map[string]any{
		"activityId": 1, "activityName": "Race", "eventTypeDTO": map[string]any{"typeKey": "race", "typeId": 1},
		"eventType": nil, "summaryDTO": map[string]any{},
	})
	d := resultMap(t, callOK(t, newSession(t, f, nil), "get_activity", map[string]any{"activity_id": 1}))
	if d["event_type"] != "race" {
		t.Errorf("got %v", d)
	}
}

func TestGetActivityException(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(actRoute, status(500, map[string]any{"message": "API Error"}))
	text, isErr := callTool(t, newSession(t, f, nil), "get_activity", map[string]any{"activity_id": 12345678901})
	if !isErr || !strings.HasPrefix(text, "Error retrieving activity: ") || !strings.Contains(text, "API Error") {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetActivityNotFound(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+actBase+"99999999999", nil)
	text := callOK(t, newSession(t, f, nil), "get_activity", map[string]any{"activity_id": 99999999999})
	if text != "No activity found with ID 99999999999" {
		t.Error(text)
	}
}

// ── write tools ─────────────────────────────────────────────────────────────

func TestSetActivityNameTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(actPut, map[string]any{})
	text := callOK(t, newSession(t, f, nil), "set_activity_name", map[string]any{"activity_id": 12345678901, "activity_name": "Morning Run - Easy"})
	if body := putJSON(t, f, actPut); !reflect.DeepEqual(body, map[string]any{"activityId": 12345678901.0, "activityName": "Morning Run - Easy"}) {
		t.Errorf("body = %v", body)
	}
	want := map[string]any{"success": true, "activity_id": 12345678901.0, "activity_name": "Morning Run - Easy", "message": "Activity name successfully updated"}
	if got := decode(t, text); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestSetActivityNameToolRejectsBlankName(t *testing.T) {
	f := newFakeGarmin(t)
	text := callOK(t, newSession(t, f, nil), "set_activity_name", map[string]any{"activity_id": 12345678901, "activity_name": "   "})
	if text != "Activity name cannot be empty" {
		t.Error(text)
	}
	noRequests(t, f, "PUT")
}

func TestSetActivityNameException(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(actPut, status(500, map[string]any{"message": "API Error"}))
	text, isErr := callTool(t, newSession(t, f, nil), "set_activity_name", map[string]any{"activity_id": 12345678901, "activity_name": "Morning Run"})
	if !isErr || text != "Error updating activity name: API Error 500 - API Error" {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestSetActivityTypeTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(typesURL, fixture(t, "MOCK_ACTIVITY_TYPES"))
	f.on(actPut, map[string]any{})
	text := callOK(t, newSession(t, f, nil), "set_activity_type", map[string]any{"activity_id": 12345678901, "type_key": "hiking"})
	want := map[string]any{"activityId": 12345678901.0, "activityTypeDTO": map[string]any{"typeId": 3.0, "typeKey": "hiking", "parentTypeId": 17.0}}
	if body := putJSON(t, f, actPut); !reflect.DeepEqual(body, want) {
		t.Errorf("body = %v", body)
	}
	d := resultMap(t, text)
	if d["success"] != true || d["type_key"] != "hiking" || d["type_id"] != 3.0 || d["message"] != "Activity type successfully updated" {
		t.Errorf("got %v", d)
	}
}

func TestSetActivityTypeRejectsUnknownKey(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(typesURL, fixture(t, "MOCK_ACTIVITY_TYPES"))
	text := callOK(t, newSession(t, f, nil), "set_activity_type", map[string]any{"activity_id": 12345678901, "type_key": "moonwalking"})
	if text != "Unknown activity type 'moonwalking'. Valid type keys: cycling, hiking, running, yoga" {
		t.Error(text)
	}
	noRequests(t, f, "PUT")
}

func TestSetActivityDescriptionTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(actPut, map[string]any{})
	text := callOK(t, newSession(t, f, nil), "set_activity_description",
		map[string]any{"activity_id": 12345678901, "description": "Felt strong. New shoes."})
	if body := putJSON(t, f, actPut); !reflect.DeepEqual(body, map[string]any{"activityId": 12345678901.0, "description": "Felt strong. New shoes."}) {
		t.Errorf("body = %v", body)
	}
	d := resultMap(t, text)
	if d["success"] != true || d["description"] != "Felt strong. New shoes." || d["message"] != "Activity description successfully updated" {
		t.Errorf("got %v", d)
	}
}

func TestSetActivityDescriptionClearsWithEmptyString(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(actPut, map[string]any{})
	callOK(t, newSession(t, f, nil), "set_activity_description", map[string]any{"activity_id": 12345678901, "description": ""})
	if body := putJSON(t, f, actPut); !reflect.DeepEqual(body, map[string]any{"activityId": 12345678901.0, "description": ""}) {
		t.Errorf("body = %v", body)
	}
}

func TestSetActivityEventTypeTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /activity-service/activity/eventTypes", []any{
		map[string]any{"typeId": 1, "typeKey": "race", "sortOrder": 5},
		map[string]any{"typeId": 4, "typeKey": "training", "sortOrder": 7},
	})
	f.on(actPut, map[string]any{})
	text := callOK(t, newSession(t, f, nil), "set_activity_event_type", map[string]any{"activity_id": 12345678901, "event_type": "race"})
	want := map[string]any{"activityId": 12345678901.0, "eventTypeDTO": map[string]any{"typeId": 1.0, "typeKey": "race", "sortOrder": 5.0}}
	if body := putJSON(t, f, actPut); !reflect.DeepEqual(body, want) {
		t.Errorf("body = %v", body)
	}
	if d := resultMap(t, text); d["event_type"] != "race" || d["message"] != "Activity event type successfully updated" {
		t.Errorf("got %v", d)
	}
}

func TestSetActivityEventTypeRejectsUnknown(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /activity-service/activity/eventTypes", []any{map[string]any{"typeId": 1, "typeKey": "race", "sortOrder": 5}})
	text := callOK(t, newSession(t, f, nil), "set_activity_event_type", map[string]any{"activity_id": 12345678901, "event_type": "wedding"})
	if text != "Unknown event type 'wedding'. Valid event types: race" {
		t.Error(text)
	}
	noRequests(t, f, "PUT")
}

func TestSetPerceivedEffortTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(actPut, map[string]any{})
	text := callOK(t, newSession(t, f, nil), "set_perceived_effort", map[string]any{"activity_id": 12345678901, "rpe": 7})
	want := map[string]any{"activityId": 12345678901.0, "summaryDTO": map[string]any{"directWorkoutRpe": 70.0}}
	if body := putJSON(t, f, actPut); !reflect.DeepEqual(body, want) {
		t.Errorf("body = %v", body)
	}
	if d := resultMap(t, text); d["rpe"] != 7.0 || d["message"] != "Perceived effort successfully updated" {
		t.Errorf("got %v", d)
	}
}

func TestSetPerceivedEffortRoundsHalfToEven(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(actPut, map[string]any{})
	callOK(t, newSession(t, f, nil), "set_perceived_effort", map[string]any{"activity_id": 12345678901, "rpe": 6.25})
	if got := at(putJSON(t, f, actPut), "summaryDTO", "directWorkoutRpe"); got != 62.0 {
		t.Errorf("rpe stored as %v, want 62 (round-half-even of 62.5)", got)
	}
}

func TestSetPerceivedEffortRejectsOutOfRange(t *testing.T) {
	f := newFakeGarmin(t)
	text := callOK(t, newSession(t, f, nil), "set_perceived_effort", map[string]any{"activity_id": 12345678901, "rpe": 11})
	if text != "rpe must be between 0 and 10" {
		t.Error(text)
	}
	noRequests(t, f, "PUT")
}

func TestSetActivityFeelTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(actPut, map[string]any{})
	text := callOK(t, newSession(t, f, nil), "set_activity_feel", map[string]any{"activity_id": 12345678901, "feel": 75})
	want := map[string]any{"activityId": 12345678901.0, "summaryDTO": map[string]any{"directWorkoutFeel": 75.0}}
	if body := putJSON(t, f, actPut); !reflect.DeepEqual(body, want) {
		t.Errorf("body = %v", body)
	}
	if d := resultMap(t, text); d["feel"] != 75.0 || d["message"] != "Activity feel successfully updated" {
		t.Errorf("got %v", d)
	}
}

func TestSetActivityFeelRejectsInvalidValue(t *testing.T) {
	f := newFakeGarmin(t)
	text := callOK(t, newSession(t, f, nil), "set_activity_feel", map[string]any{"activity_id": 12345678901, "feel": 60})
	if text != "feel must be one of 0, 25, 50, 75, 100" {
		t.Error(text)
	}
	noRequests(t, f, "PUT")
}

// ── splits, weather, zones, gear, exercise sets ────────────────────────────

func TestGetActivitySplitsTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(actRoute+"/splits", fixture(t, "MOCK_ACTIVITY_SPLITS"))
	d := resultMap(t, callOK(t, newSession(t, f, nil), "get_activity_splits", map[string]any{"activity_id": 12345678901}))
	if d["lap_count"] != 2.0 || len(asList(d["laps"])) != 2 {
		t.Errorf("got %v", d)
	}
}

func TestGetActivitySplitsElevationFields(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(actRoute+"/splits", fixture(t, "MOCK_ACTIVITY_SPLITS"))
	d := resultMap(t, callOK(t, newSession(t, f, nil), "get_activity_splits", map[string]any{"activity_id": 12345678901}))
	laps := asList(d["laps"])
	if len(laps) != 2 {
		t.Fatalf("laps = %v", laps)
	}
	for i, want := range [][2]float64{{25.5, 10.2}, {15.0, 30.8}} {
		lap := asMap(laps[i])
		if lap["elevation_gain_meters"] != want[0] || lap["elevation_loss_meters"] != want[1] {
			t.Errorf("lap %d = %v", i, lap)
		}
	}
}

func TestGetActivitySplitsIncludesSwimLengths(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+actBase+"22526515067/splits", fixture(t, "MOCK_SWIM_ACTIVITY_SPLITS"))
	d := resultMap(t, callOK(t, newSession(t, f, nil), "get_activity_splits", map[string]any{"activity_id": 22526515067}))
	if d["activity_id"] != 22526515067.0 || d["lap_count"] != 1.0 {
		t.Fatalf("got %v", d)
	}
	lap := asMap(asList(d["laps"])[0])
	if _, has := lap["start_time"]; has {
		t.Error("lap has start_time")
	}
	for k, want := range map[string]any{
		"start_time_gmt": "2026-04-14T17:19:08.0", "avg_swim_cadence": 22.0, "active_length_count": 92.0, "total_strokes": 1104.0,
		"avg_strokes": 12.0, "avg_swolf": 45.0, "moving_duration_seconds": 2995.565, "elapsed_duration_seconds": 2995.565,
		"avg_moving_speed_mps": 0.67566553875138, "bmr_calories": 85.0, "avg_stroke_distance": 0.0, "workout_step_index": 0.0,
	} {
		if lap[k] != want {
			t.Errorf("lap[%s] = %v, want %v", k, lap[k], want)
		}
	}
	lengths := asList(lap["lengths"])
	if len(lengths) != 2 {
		t.Fatalf("lengths = %v", lengths)
	}
	l := asMap(lengths[0])
	if _, has := l["start_time"]; has {
		t.Error("length has start_time")
	}
	for k, want := range map[string]any{"length_number": 1.0, "start_time_gmt": "2026-04-14T17:19:08.0", "distance_meters": 22.0, "duration_seconds": 31.0, "stroke": "FREESTYLE"} {
		if l[k] != want {
			t.Errorf("length[%s] = %v, want %v", k, l[k], want)
		}
	}
}

func TestGetActivitySplitsWrappedLapDistanceIsNulled(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+actBase+"24687347098/splits", map[string]any{"activityId": 24687347098, "lapDTOs": []any{
		map[string]any{"lapIndex": 4, "distance": 680.96, "duration": 301.797},
		map[string]any{"lapIndex": 5, "distance": 42949672.0, "duration": 0.0},
	}})
	d := resultMap(t, callOK(t, newSession(t, f, nil), "get_activity_splits", map[string]any{"activity_id": 24687347098}))
	laps := asList(d["laps"])
	l0, l1 := asMap(laps[0]), asMap(laps[1])
	if l0["distance_meters"] != 680.96 {
		t.Errorf("lap 0 = %v", l0)
	}
	if _, has := l0["distance_invalid"]; has {
		t.Errorf("lap 0 flagged: %v", l0)
	}
	if _, has := l1["distance_meters"]; has || l1["distance_invalid"] != true || l1["duration_seconds"] != 0.0 {
		t.Errorf("lap 1 = %v", l1)
	}
}

func TestGetActivityTypedSplitsTool(t *testing.T) {
	typed := map[string]any{"runSplits": asMap(fixture(t, "MOCK_ACTIVITY_SPLITS"))["lapDTOs"], "swimSplits": []any{}}
	f := newFakeGarmin(t)
	f.on(actRoute+"/typedsplits", typed)
	text := callOK(t, newSession(t, f, nil), "get_activity_typed_splits", map[string]any{"activity_id": 12345678901})
	if got := decode(t, text); !reflect.DeepEqual(got, jsonValue(t, typed)) {
		t.Errorf("got %v", got)
	}
}

func TestGetActivitySplitSummariesTool(t *testing.T) {
	sum := map[string]any{"totalDistance": 5000.0, "totalDuration": 1800.0, "avgSpeed": 2.78, "avgHR": 145}
	f := newFakeGarmin(t)
	f.on(actRoute+"/split_summaries", sum)
	text := callOK(t, newSession(t, f, nil), "get_activity_split_summaries", map[string]any{"activity_id": 12345678901})
	if got := decode(t, text); !reflect.DeepEqual(got, jsonValue(t, sum)) {
		t.Errorf("got %v", got)
	}
}

func TestGetActivityWeatherMetricConvertsToCelsius(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+actBase+"23651251274/weather", map[string]any{"temp": 57, "apparentTemp": 57, "dewPoint": 50, "relativeHumidity": 77, "windSpeed": 8})
	d := resultMap(t, callOK(t, newSession(t, f, nil), "get_activity_weather", map[string]any{"activity_id": 23651251274}))
	if d["temperature_unit"] != "C" || d["temperature"] != 13.9 || d["apparent_temperature"] != 13.9 || d["dew_point"] != 10.0 ||
		d["wind_speed"] != 8.0 || d["wind_speed_unit"] != "km/h" || d["humidity_percent"] != 77.0 || d["activity_id"] != 23651251274.0 {
		t.Errorf("got %v", d)
	}
}

func TestGetActivityWeatherStatuteUsKeepsFahrenheit(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /userprofile-service/userprofile/user-settings", map[string]any{"userData": map[string]any{"measurementSystem": "statute_us"}})
	f.on("GET "+actBase+"1/weather", map[string]any{"temp": 57, "dewPoint": 50, "windSpeed": 8})
	d := resultMap(t, callOK(t, newSession(t, f, nil), "get_activity_weather", map[string]any{"activity_id": 1}))
	if d["temperature_unit"] != "F" || d["temperature"] != 57.0 || d["dew_point"] != 50.0 || d["wind_speed"] != 8.0 || d["wind_speed_unit"] != "mph" {
		t.Errorf("got %v", d)
	}
	if n := len(f.requests("GET " + actBase + "1/weather")); n != 1 {
		t.Errorf("%d weather requests", n)
	}
}

func TestGetActivityHrInTimezonesTool(t *testing.T) {
	zones := map[string]any{"zones": []any{map[string]any{"zone": 1, "timeInZone": 300, "percentageInZone": 16.7}}}
	f := newFakeGarmin(t)
	f.on(actRoute+"/hrTimeInZones", zones)
	text := callOK(t, newSession(t, f, nil), "get_activity_hr_in_timezones", map[string]any{"activity_id": 12345678901})
	if got := decode(t, text); !reflect.DeepEqual(got, jsonValue(t, zones)) {
		t.Errorf("got %v", got)
	}
}

func TestGetActivityPowerInTimezonesTool(t *testing.T) {
	zones := []any{map[string]any{"zoneNumber": 1, "secsInZone": 120.5, "zoneLowBoundary": 0}}
	f := newFakeGarmin(t)
	f.on(actRoute+"/powerTimeInZones", zones)
	s := newSession(t, f, nil)
	text := callOK(t, s, "get_activity_power_in_timezones", map[string]any{"activity_id": 12345678901})
	if got := decode(t, text); !reflect.DeepEqual(got, jsonValue(t, zones)) {
		t.Errorf("got %v", got)
	}
	f.on(actRoute+"/powerTimeInZones", []any{})
	text = callOK(t, s, "get_activity_power_in_timezones", map[string]any{"activity_id": 12345678901})
	if text != "No power zone data found for activity 12345678901. Ensure the activity was recorded with a power meter." {
		t.Error(text)
	}
}

func TestGetActivityGearTool(t *testing.T) {
	gear := map[string]any{"gearId": 123, "displayName": "Running Shoes - Nike", "gearTypeName": "SHOE"}
	f := newFakeGarmin(t)
	f.on("GET /gear-service/gear/filterGear", gear)
	text := callOK(t, newSession(t, f, nil), "get_activity_gear", map[string]any{"activity_id": 12345678901})
	if got := decode(t, text); !reflect.DeepEqual(got, jsonValue(t, gear)) {
		t.Errorf("got %v", got)
	}
	wantQuery(t, query(t, f, "GET /gear-service/gear/filterGear"), map[string]string{"activityId": actID})
}

func TestGetActivityExerciseSetsTool(t *testing.T) {
	sets := map[string]any{"exercises": []any{map[string]any{"exerciseName": "Bench Press", "sets": []any{map[string]any{"setNumber": 1, "weight": 80.0, "reps": 10}}}}}
	f := newFakeGarmin(t)
	f.on(actRoute+"/exerciseSets", sets)
	text := callOK(t, newSession(t, f, nil), "get_activity_exercise_sets", map[string]any{"activity_id": 12345678901})
	if got := decode(t, text); !reflect.DeepEqual(got, jsonValue(t, sets)) {
		t.Errorf("got %v", got)
	}
}

func TestSetActivityExerciseSetsTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(actPut+"/exerciseSets", rawBody(""))
	text := callOK(t, newSession(t, f, nil), "set_activity_exercise_sets", map[string]any{
		"activity_id": 12345678901,
		"sets": []any{
			map[string]any{"category": "deadlift", "exercise_name": "barbell_deadlift", "reps": 5, "weight_kg": 152.5, "duration_seconds": 40, "rest_seconds": 180},
			map[string]any{"category": "BENCH_PRESS", "reps": 8, "weight_lb": 135},
			map[string]any{"category": "PULL_UP", "exercise_name": "PULL_UP", "reps": 10},
		},
	})
	want := map[string]any{"activityId": 12345678901.0, "exerciseSets": []any{
		map[string]any{"setType": "ACTIVE", "exercises": []any{map[string]any{"category": "DEADLIFT", "name": "BARBELL_DEADLIFT", "probability": 100.0}},
			"repetitionCount": 5.0, "weight": 152500.0, "duration": 40.0},
		map[string]any{"setType": "REST", "exercises": []any{}, "repetitionCount": nil, "weight": -1.0, "duration": 180.0},
		map[string]any{"setType": "ACTIVE", "exercises": []any{map[string]any{"category": "BENCH_PRESS", "name": nil, "probability": 100.0}},
			"repetitionCount": 8.0, "weight": 61235.0, "duration": nil},
		map[string]any{"setType": "ACTIVE", "exercises": []any{map[string]any{"category": "PULL_UP", "name": nil, "probability": 100.0}},
			"repetitionCount": 10.0, "weight": -1.0, "duration": nil},
	}}
	if body := putJSON(t, f, actPut+"/exerciseSets"); !reflect.DeepEqual(body, want) {
		t.Errorf("body = %v", body)
	}
	if d := resultMap(t, text); d["success"] != true || d["sets_written"] != 3.0 || d["message"] != "Activity exercise sets successfully replaced" {
		t.Errorf("got %v", d)
	}
}

func TestSetActivityExerciseSetsKeepTiming(t *testing.T) {
	unknown := []any{map[string]any{"category": "UNKNOWN", "name": nil, "probability": 99.6}}
	f := newFakeGarmin(t)
	f.on(actRoute+"/exerciseSets", map[string]any{"activityId": 12345678901, "exerciseSets": []any{
		map[string]any{"setType": "ACTIVE", "exercises": unknown, "repetitionCount": 4, "weight": 0.0, "duration": 192.1, "startTime": "2026-10-08T08:21:23.0", "messageIndex": 0},
		map[string]any{"setType": "REST", "exercises": []any{}, "repetitionCount": nil, "weight": nil, "duration": 90.5, "startTime": "2026-10-08T08:24:35.0", "messageIndex": 1},
		map[string]any{"setType": "ACTIVE", "exercises": unknown, "repetitionCount": 6, "weight": 0.0, "duration": 130.0, "startTime": "2026-10-08T08:26:06.0", "messageIndex": 2},
	}})
	f.on(actPut+"/exerciseSets", map[string]any{})
	text := callOK(t, newSession(t, f, nil), "set_activity_exercise_sets", map[string]any{
		"activity_id": 12345678901, "keep_timing": true,
		"sets": []any{
			map[string]any{"category": "DEADLIFT", "exercise_name": "RACK_PULL", "reps": 4, "weight_kg": 120, "duration_seconds": 999},
			map[string]any{"category": "SQUAT", "reps": 6},
		},
	})
	sent := asList(putJSON(t, f, actPut+"/exerciseSets")["exerciseSets"])
	if len(sent) != 3 {
		t.Fatalf("sent = %v", sent)
	}
	s0, s1, s2 := asMap(sent[0]), asMap(sent[1]), asMap(sent[2])
	if s0["setType"] != "ACTIVE" || s1["setType"] != "REST" || s2["setType"] != "ACTIVE" ||
		s0["duration"] != 192.1 || s1["duration"] != 90.5 || s2["duration"] != 130.0 || s0["startTime"] != "2026-10-08T08:21:23.0" {
		t.Errorf("timing not kept: %v", sent)
	}
	if !reflect.DeepEqual(s0["exercises"], []any{map[string]any{"category": "DEADLIFT", "name": "RACK_PULL", "probability": 100.0}}) ||
		s0["repetitionCount"] != 4.0 || s0["weight"] != 120000.0 || asMap(asList(s2["exercises"])[0])["category"] != "SQUAT" || s2["weight"] != -1.0 {
		t.Errorf("sets not replaced: %v", sent)
	}
	if resultMap(t, text)["success"] != true {
		t.Error(text)
	}
}

func TestSetActivityExerciseSetsKeepTimingRejectsCountMismatch(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(actRoute+"/exerciseSets", map[string]any{"exerciseSets": []any{map[string]any{"setType": "ACTIVE", "exercises": []any{}}}})
	text := callOK(t, newSession(t, f, nil), "set_activity_exercise_sets", map[string]any{
		"activity_id": 12345678901, "keep_timing": true, "sets": []any{map[string]any{"category": "SQUAT"}, map[string]any{"category": "SQUAT"}},
	})
	if text != "keep_timing needs one set per recorded set: activity 12345678901 has 1 active sets, got 2" {
		t.Error(text)
	}
	noRequests(t, f, "PUT")
}

func TestSetActivityExerciseSetsRejectsBadInput(t *testing.T) {
	cases := []struct {
		name  string
		sets  []any
		error string
	}{
		{"empty", []any{}, "sets cannot be empty"},
		{"no category", []any{map[string]any{"reps": 5}}, "set 1: category is required"},
		{"both weights", []any{map[string]any{"category": "SQUAT", "weight_kg": 100, "weight_lb": 220}}, "set 1: give weight_kg or weight_lb, not both"},
		{"fractional reps", []any{map[string]any{"category": "SQUAT", "reps": 5.5}}, "set 1: reps must be"},
		{"negative weight", []any{map[string]any{"category": "SQUAT", "weight_kg": -1}}, "set 1: weight cannot be negative"},
		{"negative reps", []any{map[string]any{"category": "SQUAT", "reps": -2}}, "set 1: reps must be a non-negative whole number"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGarmin(t)
			text, _ := callTool(t, newSession(t, f, nil), "set_activity_exercise_sets", map[string]any{"activity_id": 12345678901, "sets": tc.sets})
			if !strings.Contains(text, tc.error) {
				t.Errorf("text = %q, want it to contain %q", text, tc.error)
			}
			noRequests(t, f, "PUT")
		})
	}
}

// ── lists, counts, types ────────────────────────────────────────────────────

func TestCountActivitiesTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /activitylist-service/activities/count", map[string]any{"totalCount": fixture(t, "MOCK_ACTIVITY_COUNT")})
	d := resultMap(t, callOK(t, newSession(t, f, nil), "count_activities", nil))
	if d["total_activities"] != 523.0 || d["note"] != "Use get_activities() with pagination to retrieve activity details" {
		t.Errorf("got %v", d)
	}
	if n := len(f.requests("GET /activitylist-service/activities/count")); n != 1 {
		t.Errorf("%d requests", n)
	}
}

func TestGetActivitiesTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(activitiesPath, fixture(t, "MOCK_ACTIVITIES"))
	d := resultMap(t, callOK(t, newSession(t, f, nil), "get_activities", map[string]any{"start": 0, "limit": 20}))
	wantQuery(t, query(t, f, activitiesPath), map[string]string{"start": "0", "limit": "20"})
	if d["start"] != 0.0 || d["limit"] != 20.0 || d["count"] != 2.0 || d["has_more"] != false || d["next_start"] != nil || len(asList(d["activities"])) != 2 {
		t.Errorf("got %v", d)
	}
	if _, has := d["next_start"]; !has {
		t.Error("next_start must be present (null)")
	}
}

func TestGetActivitiesDefaultsAndCaps(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(activitiesPath, fixture(t, "MOCK_ACTIVITIES"))
	s := newSession(t, f, nil)
	callOK(t, s, "get_activities", nil)
	callOK(t, s, "get_activities", map[string]any{"start": 5, "limit": 500})
	reqs := f.requests(activitiesPath)
	if reqs[0].Query.Get("limit") != "20" || reqs[1].Query.Get("limit") != "100" || reqs[1].Query.Get("start") != "5" {
		t.Errorf("queries = %v %v", reqs[0].Query, reqs[1].Query)
	}
}

func TestGetActivitiesHasMore(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(activitiesPath, fixture(t, "MOCK_ACTIVITIES"))
	d := resultMap(t, callOK(t, newSession(t, f, nil), "get_activities", map[string]any{"start": 10, "limit": 2}))
	if d["has_more"] != true || d["next_start"] != 12.0 {
		t.Errorf("got %v", d)
	}
}

func TestGetActivitiesNoDataAndErrors(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(activitiesPath, []any{})
	s := newSession(t, f, nil)
	if text := callOK(t, s, "get_activities", map[string]any{"start": 40}); text != "No activities found at index 40" {
		t.Error(text)
	}
	text, isErr := callTool(t, s, "get_activities", map[string]any{"start": -1})
	if !isErr || text != "Error retrieving activities: start must be non-negative, got: -1" {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetActivitiesIncludesEventType(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(activitiesPath, fixture(t, "MOCK_ACTIVITIES"))
	acts := asList(resultMap(t, callOK(t, newSession(t, f, nil), "get_activities", map[string]any{"start": 0, "limit": 20}))["activities"])
	if asMap(acts[0])["event_type"] != "race" || asMap(acts[1])["event_type"] != "training" {
		t.Errorf("got %v", acts)
	}
}

func TestGetActivitiesOmitsEventTypeWhenAbsent(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(activitiesPath, []any{map[string]any{
		"activityId": 99999, "activityName": "Old Activity", "activityType": map[string]any{"typeKey": "running", "typeId": 1},
		"startTimeLocal": "2024-01-01 08:00:00", "duration": 1200.0,
	}})
	acts := asList(resultMap(t, callOK(t, newSession(t, f, nil), "get_activities", map[string]any{"start": 0, "limit": 20}))["activities"])
	if _, has := asMap(acts[0])["event_type"]; has {
		t.Errorf("got %v", acts[0])
	}
}

func TestGetActivityTypesTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(typesURL, fixture(t, "MOCK_ACTIVITY_TYPES"))
	d := resultMap(t, callOK(t, newSession(t, f, nil), "get_activity_types", nil))
	types := asList(d["activity_types"])
	if d["count"] != 4.0 || len(types) != 4 {
		t.Fatalf("got %v", d)
	}
	want := map[string]any{"type_id": 3.0, "type_key": "hiking", "display_name": "Hiking", "parent_type_id": 17.0, "is_hidden": false}
	if !reflect.DeepEqual(types[2], want) {
		t.Errorf("types[2] = %v", types[2])
	}
	if first := asMap(types[0]); first["parent_type_id"] != nil {
		if _, has := first["parent_type_id"]; has {
			t.Errorf("null parent kept: %v", first)
		}
	}
}

// ── create_manual_activity ──────────────────────────────────────────────────

const createRoute = "POST /activity-service/activity"

func TestCreateManualActivitySuccess(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(createRoute, map[string]any{"activityId": 999})
	text := callOK(t, newSession(t, f, nil), "create_manual_activity", map[string]any{"type_key": "yoga", "date": "2024-03-01", "duration_minutes": 60})
	if got := decode(t, text); !reflect.DeepEqual(got, map[string]any{"success": true, "activity": map[string]any{"activityId": 999.0}}) {
		t.Errorf("got %v", got)
	}
	want := map[string]any{
		"activityTypeDTO": map[string]any{"typeKey": "yoga"}, "accessControlRuleDTO": map[string]any{"typeId": 2.0, "typeKey": "private"},
		"timeZoneUnitDTO": map[string]any{"unitKey": "UTC"}, "activityName": "Yoga", "metadataDTO": map[string]any{"autoCalcCalories": true},
		"summaryDTO": map[string]any{"startTimeLocal": "2024-03-01T09:00:00.000", "distance": 0.0, "duration": 3600.0},
	}
	if body := putJSON(t, f, createRoute); !reflect.DeepEqual(body, want) {
		t.Errorf("body = %v", body)
	}
}

func TestCreateManualActivityCustomFields(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(createRoute, map[string]any{"activityId": 123})
	callOK(t, newSession(t, f, nil), "create_manual_activity", map[string]any{
		"type_key": "strength_training", "date": "2024-03-15", "duration_minutes": 45, "start_time": "07:30",
		"activity_name": "Morning Weights", "distance_km": 0.0, "time_zone": "Europe/Lisbon",
	})
	want := map[string]any{
		"activityTypeDTO": map[string]any{"typeKey": "strength_training"}, "accessControlRuleDTO": map[string]any{"typeId": 2.0, "typeKey": "private"},
		"timeZoneUnitDTO": map[string]any{"unitKey": "Europe/Lisbon"}, "activityName": "Morning Weights", "metadataDTO": map[string]any{"autoCalcCalories": true},
		"summaryDTO": map[string]any{"startTimeLocal": "2024-03-15T07:30:00.000", "distance": 0.0, "duration": 2700.0},
	}
	if body := putJSON(t, f, createRoute); !reflect.DeepEqual(body, want) {
		t.Errorf("body = %v", body)
	}
}

func TestCreateManualActivityConvertsDistanceToMetres(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(createRoute, map[string]any{})
	callOK(t, newSession(t, f, nil), "create_manual_activity", map[string]any{"type_key": "running", "date": "2024-03-15", "duration_minutes": 30, "distance_km": 5.5})
	if got := at(putJSON(t, f, createRoute), "summaryDTO", "distance"); got != 5500.0 {
		t.Errorf("distance = %v", got)
	}
}

func TestCreateManualActivityDefaultNameFromTypeKey(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(createRoute, map[string]any{})
	callOK(t, newSession(t, f, nil), "create_manual_activity", map[string]any{"type_key": "indoor_cycling", "date": "2024-04-01", "duration_minutes": 30})
	if got := putJSON(t, f, createRoute)["activityName"]; got != "Indoor Cycling" {
		t.Errorf("activityName = %v", got)
	}
}

func TestCreateManualActivityRejectsZeroDuration(t *testing.T) {
	f := newFakeGarmin(t)
	text, isErr := callTool(t, newSession(t, f, nil), "create_manual_activity", map[string]any{"type_key": "yoga", "date": "2024-03-01", "duration_minutes": 0})
	if !isErr || text != "Error: duration_minutes must be greater than 0" {
		t.Errorf("%v %q", isErr, text)
	}
	noRequests(t, f, "POST")
}

func TestCreateManualActivityRejectsEmptyTypeKey(t *testing.T) {
	f := newFakeGarmin(t)
	text, isErr := callTool(t, newSession(t, f, nil), "create_manual_activity", map[string]any{"type_key": "  ", "date": "2024-03-01", "duration_minutes": 30})
	if !isErr || text != "Error: type_key is required" {
		t.Errorf("%v %q", isErr, text)
	}
	noRequests(t, f, "POST")
}

func TestCreateManualActivityException(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(createRoute, status(500, map[string]any{"message": "Garmin API error"}))
	text, isErr := callTool(t, newSession(t, f, nil), "create_manual_activity", map[string]any{"type_key": "yoga", "date": "2024-03-01", "duration_minutes": 60})
	if !isErr || !strings.Contains(text, "Error") || !strings.Contains(text, "Garmin API error") {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestCreateManualActivityStripsTypeBeforePayloadAndDefaultName(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(createRoute, map[string]any{"activityId": 999})
	text := callOK(t, newSession(t, f, nil), "create_manual_activity", map[string]any{"type_key": " yoga ", "date": "2024-03-01", "duration_minutes": 30})
	if resultMap(t, text)["success"] != true {
		t.Error(text)
	}
	body := putJSON(t, f, createRoute)
	if at(body, "activityTypeDTO", "typeKey") != "yoga" || body["activityName"] != "Yoga" {
		t.Errorf("body = %v", body)
	}
}

func TestCreateManualActivityRejectsInvalidTimestampOrDistance(t *testing.T) {
	cases := []struct {
		key string
		val any
	}{
		{"date", "2024-02-30"}, {"date", "03/01/2024"}, {"date", "2024-3-1"},
		{"start_time", "09:00:00"}, {"start_time", "9:00"}, {"start_time", "24:00"},
		{"distance_km", -1},
	}
	for _, tc := range cases {
		t.Run(tc.key+"="+strings.ReplaceAll(strings.ReplaceAll(toString(tc.val), "/", "-"), ":", "-"), func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on(createRoute, map[string]any{"activityId": 999})
			args := map[string]any{"type_key": "yoga", "date": "2024-03-01", "duration_minutes": 30}
			args[tc.key] = tc.val
			text, isErr := callTool(t, newSession(t, f, nil), "create_manual_activity", args)
			if !isErr || !strings.Contains(text, "Error") || !strings.Contains(text, tc.key) {
				t.Errorf("%v %q", isErr, text)
			}
			noRequests(t, f, "POST")
		})
	}
}

func toString(v any) string { return pyStr(v) }

func TestCreateManualActivityReportsWriteCommitUncertainty(t *testing.T) {
	cases := []struct {
		name      string
		post      any
		env       map[string]string
		closeFake bool
		committed any
	}{
		{"read_timeout", func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }, map[string]string{"GARMIN_MCP_CALL_TIMEOUT": "0.3"}, false, "unknown"},
		{"server_error", status(503, map[string]any{"message": "gateway"}), nil, false, "unknown"},
		{"rejected", status(400, map[string]any{"message": "invalid request"}), nil, false, false},
		{"connect_failure", nil, nil, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on(createRoute, tc.post)
			s := newSession(t, f, tc.env)
			if tc.closeFake {
				f.Close()
			}
			text, isErr := callTool(t, s, "create_manual_activity", map[string]any{"type_key": "yoga", "date": "2024-03-01", "duration_minutes": 30})
			d := resultMap(t, text)
			if !isErr || d["status"] != "error" || d["write_committed"] != tc.committed || d["tool"] != "create_manual_activity" {
				t.Fatalf("got %v", d)
			}
			if tc.committed == "unknown" && !strings.Contains(pyStr(d["hint"]), "before retrying") {
				t.Errorf("hint = %v", d["hint"])
			}
			if !strings.HasPrefix(pyStr(d["message"]), "Error creating manual activity: ") {
				t.Errorf("message = %v", d["message"])
			}
			if !tc.closeFake {
				if n := len(f.requests(createRoute)); n != 1 {
					t.Errorf("%d POSTs", n)
				}
			}
		})
	}
}

func TestCreateManualActivityReportsCommittedWriteIfResponseCannotParse(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(createRoute, rawBody("<html>not json</html>"))
	text, isErr := callTool(t, newSession(t, f, nil), "create_manual_activity", map[string]any{"type_key": "yoga", "date": "2024-03-01", "duration_minutes": 30})
	d := resultMap(t, text)
	if !isErr || d["write_committed"] != true || d["stage"] != "response_parse" {
		t.Errorf("got %v", d)
	}
}

// ── delete_activity ─────────────────────────────────────────────────────────

func deleteFixture(t *testing.T) *fakeGarmin {
	t.Helper()
	f := newFakeGarmin(t)
	f.on(actRoute, map[string]any{
		"activityId": 12345678901, "activityName": "Morning Run", "activityTypeDTO": map[string]any{"typeKey": "running"},
		"summaryDTO": map[string]any{"startTimeLocal": "2024-01-15 07:00:00", "distance": 5000.0, "duration": 1800.0},
	})
	f.on("DELETE "+actBase+actID, map[string]any{})
	return f
}

func TestDeleteActivityRequiresConfirmation(t *testing.T) {
	f := deleteFixture(t)
	text := callOK(t, newSession(t, f, nil), "delete_activity", map[string]any{"activity_id": 12345678901})
	want := map[string]any{
		"status": "needs_confirmation",
		"activity": map[string]any{"activity_id": 12345678901.0, "name": "Morning Run", "type": "running", "start_time": "2024-01-15 07:00:00",
			"distance_meters": 5000.0, "duration_seconds": 1800.0},
		"message": "This permanently deletes the activity from Garmin Connect and cannot be undone. Call again with confirm=true to proceed.",
	}
	if got := decode(t, text); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	noRequests(t, f, "DELETE")
}

func TestDeleteActivityWithConfirm(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(actRoute, map[string]any{"activityId": 12345678901, "activityName": "Morning Run",
		"activityTypeDTO": map[string]any{"typeKey": "running"}, "summaryDTO": map[string]any{"startTimeLocal": "2024-01-15 07:00:00"}})
	f.on("DELETE "+actBase+actID, rawBody(""))
	var log bytes.Buffer
	srv, err := New(testOptions(f, newStore(t), nil, &log))
	if err != nil {
		t.Fatal(err)
	}
	text := callOK(t, connect(t, srv), "delete_activity", map[string]any{"activity_id": 12345678901, "confirm": true})
	d := resultMap(t, text)
	if d["status"] != "deleted" || at(d, "activity", "activity_id") != 12345678901.0 || d["message"] != "Activity 12345678901 permanently deleted." {
		t.Errorf("got %v", d)
	}
	if n := len(f.requests("DELETE " + actBase + actID)); n != 1 {
		t.Errorf("%d DELETEs", n)
	}
	if got, _, _ := strings.Cut(log.String(), "\n"); !strings.HasPrefix(got, "delete_activity: deleted activity 12345678901 name='Morning Run' at 20") || !strings.HasSuffix(got, "+00:00") {
		t.Errorf("log = %q", got)
	}
}

func TestDeleteActivityPreviewDoesNotLogOrDelete(t *testing.T) {
	f := deleteFixture(t)
	var log bytes.Buffer
	srv, err := New(testOptions(f, newStore(t), nil, &log))
	if err != nil {
		t.Fatal(err)
	}
	callOK(t, connect(t, srv), "delete_activity", map[string]any{"activity_id": 12345678901, "confirm": false})
	if strings.Contains(log.String(), "deleted activity") {
		t.Errorf("log = %q", log.String())
	}
	noRequests(t, f, "DELETE")
}

func TestDeleteActivityException(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(actRoute, map[string]any{"activityName": "Morning Run"})
	f.on("DELETE "+actBase+actID, status(500, map[string]any{"message": "Garmin API error"}))
	text, isErr := callTool(t, newSession(t, f, nil), "delete_activity", map[string]any{"activity_id": 12345678901, "confirm": true})
	if !isErr || !strings.HasPrefix(text, "Error deleting activity: ") || !strings.Contains(text, "Garmin API error") {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestDeleteActivityRejectsInvalidId(t *testing.T) {
	f := deleteFixture(t)
	text := callOK(t, newSession(t, f, nil), "delete_activity", map[string]any{"activity_id": "abc", "confirm": true})
	if text != "Invalid activity ID: abc" {
		t.Error(text)
	}
	if n := len(f.requests("")); n != 0 {
		t.Errorf("%d requests", n)
	}
}

type fakeActivityClient struct {
	activity   any
	activities any
	err        error
}

func (c fakeActivityClient) GetActivity(_ context.Context, id int64) (any, error) {
	return c.activity, c.err
}
func (c fakeActivityClient) GetActivities(_ context.Context, start, limit int, _ string) (any, error) {
	return c.activities, nil
}

func TestPreviewFromActivityDetails(t *testing.T) {
	c := fakeActivityClient{activity: map[string]any{
		"activityName": "Morning Run", "activityTypeDTO": map[string]any{"typeKey": "running"},
		"summaryDTO": map[string]any{"startTimeLocal": "2024-01-15 07:00:00", "distance": 5000.0, "duration": 1800.0},
	}}
	p := activityPreview(t.Context(), c, 99)
	if p["activity_id"] != int64(99) || p["name"] != "Morning Run" || p["type"] != "running" || p["distance_meters"] != 5000.0 {
		t.Errorf("preview = %v", p)
	}
}

func TestPreviewFallsBackToActivityListOnDetailsError(t *testing.T) {
	c := fakeActivityClient{err: errTest("403 Forbidden"), activities: []any{map[string]any{
		"activityId": 99.0, "activityName": "Easy Ride", "activityType": map[string]any{"typeKey": "cycling"},
		"startTimeLocal": "2024-01-14 16:00:00", "distance": 20000.0, "duration": 3600.0,
	}}}
	p := activityPreview(t.Context(), c, 99)
	if p["name"] != "Easy Ride" || p["type"] != "cycling" || p["start_time"] != "2024-01-14 16:00:00" {
		t.Errorf("preview = %v", p)
	}
}

func TestPreviewWithoutAnyDataIsJustTheId(t *testing.T) {
	c := fakeActivityClient{err: errTest("boom"), activities: "nope"}
	if p := activityPreview(t.Context(), c, 7); !reflect.DeepEqual(p, map[string]any{"activity_id": int64(7)}) {
		t.Errorf("preview = %v", p)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

// ── no-data messages of the helper-registered tools ────────────────────────

func TestActivityNoDataMessagesAreWellFormed(t *testing.T) {
	cases := []struct {
		tool, route, want string
	}{
		{"get_activity", actRoute, "No activity found with ID 12345678901"},
		{"get_activity_splits", actRoute + "/splits", "No splits found for activity with ID 12345678901"},
		{"get_activity_typed_splits", actRoute + "/typedsplits", "No typed splits found for activity with ID 12345678901"},
		{"get_activity_split_summaries", actRoute + "/split_summaries", "No split summaries found for activity with ID 12345678901"},
		{"get_activity_weather", actRoute + "/weather", "No weather data found for activity with ID 12345678901"},
		{"get_activity_hr_in_timezones", actRoute + "/hrTimeInZones", "No heart rate time zone data found for activity with ID 12345678901"},
		{"get_activity_power_in_timezones", actRoute + "/powerTimeInZones", "No power zone data found for activity 12345678901. Ensure the activity was recorded with a power meter."},
		{"get_activity_gear", "GET /gear-service/gear/filterGear", "No gear data found for activity with ID 12345678901"},
		{"get_activity_exercise_sets", actRoute + "/exerciseSets", "No exercise sets found for activity with ID 12345678901"},
	}
	for _, tc := range cases {
		for _, empty := range []any{nil, map[string]any{}, []any{}} {
			f := newFakeGarmin(t)
			f.on(tc.route, empty)
			text := callOK(t, newSession(t, f, nil), tc.tool, map[string]any{"activity_id": 12345678901})
			if strings.Contains(text, "%!") || text != tc.want {
				t.Errorf("%s (%v): %q, want %q", tc.tool, empty, text, tc.want)
			}
		}
	}
}
