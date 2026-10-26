package server

import (
	"strings"
	"testing"
)

const (
	calEventsPath = "/calendar-service/year/2026/month/9"
)

// calendarEvent builds a raw calendar event item as Garmin returns it.
func calendarEvent(date, title, uuid string, overrides map[string]any) map[string]any {
	item := map[string]any{
		"itemType":           "event",
		"activityTypeId":     1,
		"title":              title,
		"date":               date,
		"url":                "https://www.ahotu.com/event/some-marathon",
		"isRace":             true,
		"shareableEventUuid": uuid,
		"completionTarget":   map[string]any{"value": 42195.0, "unit": "meter", "unitType": "distance"},
		"shareableEvent":     true,
		"subscribed":         true,
	}
	for k, v := range overrides {
		item[k] = v
	}
	return item
}

func calendarMonth(items ...any) map[string]any {
	return map[string]any{"calendarItems": items}
}

func calendarText(t *testing.T, text string) map[string]any {
	t.Helper()
	var got map[string]any
	decodeJSON(t, text, &got)
	return got
}

func calendarEvents(t *testing.T, text string) []any {
	t.Helper()
	return mutList(t, calendarText(t, text)["events"])
}

func TestGetCalendarEventsCuratesFields(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+calEventsPath, calendarMonth(
		calendarEvent("2026-10-17", "Gyeongju Marathon", "uuid-1", map[string]any{
			"eventTimeLocal": map[string]any{"startTimeHhMm": "08:00", "timeZoneId": "Asia/Seoul"},
			"primaryEvent":   true,
			"location":       "KR",
		}),
	))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_calendar_events", map[string]any{
		"start_date": "2026-10-01", "end_date": "2026-10-31",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := calendarText(t, text)
	if num(t, data["count"]) != 1 {
		t.Fatalf("count = %v", data["count"])
	}
	event := mutMap(t, calendarEvents(t, text)[0])
	checks := map[string]any{
		"title":            "Gyeongju Marathon",
		"date":             "2026-10-17",
		"is_race":          true,
		"primary_event":    true,
		"distance_meters":  42195.0,
		"start_time_local": "08:00",
		"time_zone":        "Asia/Seoul",
		"location":         "KR",
		"event_uuid":       "uuid-1",
	}
	for k, want := range checks {
		if event[k] != want {
			t.Errorf("%s = %v, want %v", k, event[k], want)
		}
	}
}

func TestGetCalendarEventsRequestsTheMonthOneIndexed(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+calEventsPath, calendarMonth())
	cs := newSession(t, f, nil)

	_, _ = callTool(t, cs, "get_calendar_events", map[string]any{
		"start_date": "2026-10-01", "end_date": "2026-10-31",
	})
	calls := f.callsMatching("GET", calEventsPath)
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
}

func TestGetCalendarEventsWalksMonthsAcrossAYearBoundary(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /calendar-service/year/2026/month/11", calendarMonth())
	f.on("GET /calendar-service/year/2027/month/0", calendarMonth())
	cs := newSession(t, f, nil)

	_, _ = callTool(t, cs, "get_calendar_events", map[string]any{
		"start_date": "2026-12-20", "end_date": "2027-01-10",
	})
	if calls := f.callsMatching("GET", "/calendar-service/year/2026/month/11"); len(calls) != 1 {
		t.Fatalf("december calls = %d", len(calls))
	}
	if calls := f.callsMatching("GET", "/calendar-service/year/2027/month/0"); len(calls) != 1 {
		t.Fatalf("january calls = %d", len(calls))
	}
}

func TestGetCalendarEventsClipsToTheRequestedRange(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+calEventsPath, calendarMonth(
		calendarEvent("2026-10-03", "Too Early", "uuid-early", nil),
		calendarEvent("2026-10-17", "In Range", "uuid-mid", nil),
		calendarEvent("2026-10-31", "Too Late", "uuid-late", nil),
	))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_calendar_events", map[string]any{
		"start_date": "2026-10-10", "end_date": "2026-10-20",
	})
	events := calendarEvents(t, text)
	if len(events) != 1 || mutMap(t, events[0])["title"] != "In Range" {
		t.Fatalf("events = %v", events)
	}
}

func TestGetCalendarEventsIgnoresNonEventItems(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+calEventsPath, calendarMonth(
		calendarEvent("2026-10-17", "Gyeongju Marathon", "uuid-1", nil),
		map[string]any{"itemType": "fbtAdaptiveWorkout", "title": "Base", "date": "2026-10-15"},
		map[string]any{"itemType": "weight", "date": "2026-10-16", "weight": 70100.0},
		map[string]any{"itemType": "trainingPlan", "title": "Gyeongju Marathon Plan", "date": "2026-10-17"},
	))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_calendar_events", map[string]any{
		"start_date": "2026-10-01", "end_date": "2026-10-31",
	})
	data := calendarText(t, text)
	if num(t, data["count"]) != 1 {
		t.Fatalf("count = %v", data["count"])
	}
	if mutMap(t, calendarEvents(t, text)[0])["title"] != "Gyeongju Marathon" {
		t.Fatal("wrong event kept")
	}
}

func TestGetCalendarEventsDeduplicatesAcrossMonths(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /calendar-service/year/2026/month/10", calendarMonth(
		calendarEvent("2026-11-15", "MBN Seoul Marathon", "uuid-seoul", nil),
	))
	f.on("GET /calendar-service/year/2026/month/11", calendarMonth(
		calendarEvent("2026-11-15", "MBN Seoul Marathon", "uuid-seoul", nil),
	))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_calendar_events", map[string]any{
		"start_date": "2026-11-01", "end_date": "2026-12-31",
	})
	if num(t, calendarText(t, text)["count"]) != 1 {
		t.Fatalf("count = %v", calendarText(t, text)["count"])
	}
}

func TestGetCalendarEventsSortsByDate(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+calEventsPath, calendarMonth(
		calendarEvent("2026-10-17", "Later", "uuid-later", nil),
		calendarEvent("2026-10-03", "Earlier", "uuid-earlier", nil),
	))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_calendar_events", map[string]any{
		"start_date": "2026-10-01", "end_date": "2026-10-31",
	})
	events := calendarEvents(t, text)
	if len(events) != 2 || mutMap(t, events[0])["date"] != "2026-10-03" || mutMap(t, events[1])["date"] != "2026-10-17" {
		t.Fatalf("events = %v", events)
	}
}

func TestGetCalendarEventsHandlesMissingCompletionTarget(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+calEventsPath, calendarMonth(
		calendarEvent("2026-10-17", "Some Marathon", "uuid-1", map[string]any{"completionTarget": nil}),
	))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_calendar_events", map[string]any{
		"start_date": "2026-10-01", "end_date": "2026-10-31",
	})
	event := mutMap(t, calendarEvents(t, text)[0])
	if event["distance_meters"] != nil {
		t.Fatalf("distance = %v", event["distance_meters"])
	}
}

func TestGetCalendarEventsIgnoresNonDistanceTarget(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+calEventsPath, calendarMonth(
		calendarEvent("2026-10-17", "Some Marathon", "uuid-1", map[string]any{
			"completionTarget": map[string]any{"value": 3600.0, "unit": "second", "unitType": "time"},
		}),
	))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_calendar_events", map[string]any{
		"start_date": "2026-10-01", "end_date": "2026-10-31",
	})
	if mutMap(t, calendarEvents(t, text)[0])["distance_meters"] != nil {
		t.Fatal("time goal reported as distance")
	}
}

func TestGetCalendarEventsConvertsMileDistanceToMeters(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+calEventsPath, calendarMonth(
		calendarEvent("2026-10-17", "Some Marathon", "uuid-1", map[string]any{
			"completionTarget": map[string]any{"value": 1.0, "unit": "mile", "unitType": "distance"},
		}),
	))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_calendar_events", map[string]any{
		"start_date": "2026-10-01", "end_date": "2026-10-31",
	})
	if got := num(t, mutMap(t, calendarEvents(t, text)[0])["distance_meters"]); got != 1609.344 {
		t.Fatalf("distance = %v", got)
	}
}

func TestGetCalendarEventsNormalisesBlankLocation(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+calEventsPath, calendarMonth(
		calendarEvent("2026-10-17", "Some Marathon", "uuid-1", map[string]any{"location": ""}),
	))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_calendar_events", map[string]any{
		"start_date": "2026-10-01", "end_date": "2026-10-31",
	})
	if mutMap(t, calendarEvents(t, text)[0])["location"] != nil {
		t.Fatal("blank location not normalised")
	}
}

func TestGetCalendarEventsEnrichesCustomEventGoal(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+calEventsPath, calendarMonth(
		calendarEvent("2026-10-17", "Some Marathon", "", map[string]any{
			"id":                 12345,
			"shareableEventUuid": nil,
			"completionTarget":   map[string]any{"value": 5.0, "unit": "kilometer", "unitType": "distance"},
		}),
	))
	f.on("GET /calendar-service/event/12345", map[string]any{
		"eventCustomization": map[string]any{"customGoal": map[string]any{
			"value": 1800.0, "unit": "second", "unitType": "time",
		}},
	})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_calendar_events", map[string]any{
		"start_date": "2026-10-01", "end_date": "2026-10-31",
	})
	event := mutMap(t, calendarEvents(t, text)[0])
	if num(t, event["distance_meters"]) != 5000.0 || num(t, event["goal_time_seconds"]) != 1800.0 {
		t.Fatalf("event = %v", event)
	}
	if calls := f.callsMatching("GET", "/calendar-service/event/12345"); len(calls) != 1 {
		t.Fatalf("detail calls = %d", len(calls))
	}
}

func TestGetCalendarEventsEnrichesShareableEventGoal(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+calEventsPath, calendarMonth(
		calendarEvent("2026-10-17", "Some Marathon", "shareable-uuid", map[string]any{"id": nil}),
	))
	f.on("GET /calendar-service/event/shareable-uuid/shareable", map[string]any{
		"eventCustomization": map[string]any{"customGoal": map[string]any{
			"value": 14400.0, "unit": "second", "unitType": "time",
		}},
	})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_calendar_events", map[string]any{
		"start_date": "2026-10-01", "end_date": "2026-10-31",
	})
	event := mutMap(t, calendarEvents(t, text)[0])
	if num(t, event["goal_time_seconds"]) != 14400.0 {
		t.Fatalf("event = %v", event)
	}
}

func TestGetCalendarEventsIgnoresNonTimeCustomGoal(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+calEventsPath, calendarMonth(calendarEvent("2026-10-17", "Some Marathon", "uuid-1", nil)))
	f.on("GET /calendar-service/event/uuid-1/shareable", map[string]any{
		"eventCustomization": map[string]any{"customGoal": map[string]any{
			"value": 10.0, "unit": "kilometer", "unitType": "distance",
		}},
	})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_calendar_events", map[string]any{
		"start_date": "2026-10-01", "end_date": "2026-10-31",
	})
	// The fixture has no id, so the UUID path is used instead; the detail value
	// must still be ignored because it is not a time goal.
	if mutMap(t, calendarEvents(t, text)[0])["goal_time_seconds"] != nil {
		t.Fatal("non-time custom goal reported")
	}
}

func TestGetCalendarEventsSkipsDetailWithoutEventIdentity(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+calEventsPath, calendarMonth(
		calendarEvent("2026-10-17", "Some Marathon", "", map[string]any{"id": nil, "shareableEventUuid": nil}),
	))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_calendar_events", map[string]any{
		"start_date": "2026-10-01", "end_date": "2026-10-31",
	})
	if mutMap(t, calendarEvents(t, text)[0])["goal_time_seconds"] != nil {
		t.Fatal("goal without identity")
	}
	for _, call := range f.recorded() {
		if strings.HasPrefix(call.Path, "/calendar-service/event/") {
			t.Fatalf("unexpected detail call %s", call.Path)
		}
	}
}

func TestGetCalendarEventsKeepsEventWhenDetailFetchFails(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+calEventsPath, calendarMonth(calendarEvent("2026-10-17", "Some Marathon", "uuid-1", nil)))
	f.on("GET /calendar-service/event/uuid-1/shareable", status(500, map[string]any{"message": "detail unavailable"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_calendar_events", map[string]any{
		"start_date": "2026-10-01", "end_date": "2026-10-31",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	event := mutMap(t, calendarEvents(t, text)[0])
	if num(t, event["distance_meters"]) != 42195.0 || event["goal_time_seconds"] != nil {
		t.Fatalf("event = %v", event)
	}
}

func TestGetCalendarEventsEmpty(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+calEventsPath, calendarMonth())
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_calendar_events", map[string]any{
		"start_date": "2026-10-01", "end_date": "2026-10-31",
	})
	data := calendarText(t, text)
	if num(t, data["count"]) != 0 || len(mutList(t, data["events"])) != 0 {
		t.Fatalf("data = %v", data)
	}
}

func TestGetCalendarEventsHandlesNullPayload(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+calEventsPath, nil)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_calendar_events", map[string]any{
		"start_date": "2026-10-01", "end_date": "2026-10-31",
	})
	if num(t, calendarText(t, text)["count"]) != 0 {
		t.Fatal("null payload not treated as empty")
	}
}

func TestGetCalendarEventsRejectsReversedRange(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_calendar_events", map[string]any{
		"start_date": "2026-10-31", "end_date": "2026-10-01",
	})
	if !strings.Contains(text, "start_date must be on or before end_date") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("GET", calEventsPath); len(calls) != 0 {
		t.Fatalf("calls = %d", len(calls))
	}
}

func TestGetCalendarEventsRejectsBadDateFormat(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_calendar_events", map[string]any{
		"start_date": "10/01/2026", "end_date": "2026-10-31",
	})
	if !strings.Contains(text, "Invalid start_date") {
		t.Fatalf("text = %q", text)
	}
	if len(f.recorded()) != 0 {
		t.Fatalf("unexpected calls: %v", f.recorded())
	}
}

func TestGetCalendarEventsErrorIsCaught(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+calEventsPath, status(500, map[string]any{"message": "boom"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_calendar_events", map[string]any{
		"start_date": "2026-10-01", "end_date": "2026-10-31",
	})
	if !strings.Contains(text, "Error retrieving calendar events") {
		t.Fatalf("text = %q", text)
	}
}
