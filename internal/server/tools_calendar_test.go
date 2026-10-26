package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// Ported from tests/integration/test_calendar_events_tools.py.

func monthRoute(year, month int) string {
	return fmt.Sprintf("GET /calendar-service/year/%d/month/%d", year, month-1)
}

func race(date string, overrides map[string]any) map[string]any {
	item := map[string]any{
		"itemType": "event", "activityTypeId": 1.0, "title": "Some Marathon", "date": date,
		"url": "https://www.ahotu.com/event/some-marathon", "isRace": true, "shareableEventUuid": "uuid-1",
		"completionTarget": map[string]any{"value": 42195.0, "unit": "meter", "unitType": "distance"},
		"shareableEvent":   true, "subscribed": true,
	}
	for k, v := range overrides {
		item[k] = v
	}
	return item
}

func month(items ...any) map[string]any { return map[string]any{"calendarItems": items} }

var octRange = map[string]any{"start_date": "2026-10-01", "end_date": "2026-10-31"}

func calendarEvents(t *testing.T, f *fakeGarmin, args map[string]any) map[string]any {
	return runTool(t, f, "get_calendar_events", args)
}

func octFake(t *testing.T, items ...any) *fakeGarmin {
	f := newFakeGarmin(t)
	f.on(monthRoute(2026, 10), month(items...))
	return f
}

func firstEvent(t *testing.T, d map[string]any) map[string]any {
	return asMap(toolList(t, "events", d)[0])
}

func TestGetCalendarEventsCuratesFields(t *testing.T) {
	f := octFake(t, race("2026-10-17", map[string]any{
		"title": "Gyeongju Marathon", "eventTimeLocal": map[string]any{"startTimeHhMm": "08:00", "timeZoneId": "Asia/Seoul"},
		"primaryEvent": true, "location": "KR",
	}))
	d := calendarEvents(t, f, octRange)
	e := firstEvent(t, d)
	if d["count"] != 1.0 || e["title"] != "Gyeongju Marathon" || e["date"] != "2026-10-17" || e["is_race"] != true || e["primary_event"] != true ||
		e["distance_meters"] != 42195.0 || e["start_time_local"] != "08:00" || e["time_zone"] != "Asia/Seoul" || e["location"] != "KR" || e["event_uuid"] != "uuid-1" {
		t.Errorf("e = %v", e)
	}
	if dr := asMap(d["date_range"]); dr["start"] != "2026-10-01" || dr["end"] != "2026-10-31" {
		t.Errorf("date_range = %v", dr)
	}
	// Every key is present, null when Garmin sends nothing.
	if _, has := e["goal_time_seconds"]; !has || len(e) != 12 {
		t.Errorf("keys = %v", e)
	}
}

func TestGetCalendarEventsRequestsTheMonthOneIndexed(t *testing.T) {
	f := octFake(t)
	calendarEvents(t, f, octRange)
	if n := len(f.requests("GET /calendar-service/year/2026/month/9")); n != 1 {
		t.Errorf("month requests = %d", n)
	}
}

func TestGetCalendarEventsWalksMonthsAcrossAYearBoundary(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(monthRoute(2026, 12), month())
	f.on(monthRoute(2027, 1), month())
	calendarEvents(t, f, map[string]any{"start_date": "2026-12-20", "end_date": "2027-01-10"})
	var got []string
	for _, c := range f.requests("") {
		if strings.HasPrefix(c.Path, "/calendar-service/year/") {
			got = append(got, c.Path)
		}
	}
	if strings.Join(got, ",") != "/calendar-service/year/2026/month/11,/calendar-service/year/2027/month/0" {
		t.Errorf("requested %v", got)
	}
}

func TestGetCalendarEventsClipsToTheRequestedRange(t *testing.T) {
	f := octFake(t,
		race("2026-10-03", map[string]any{"title": "Too Early", "shareableEventUuid": "uuid-early"}),
		race("2026-10-17", map[string]any{"title": "In Range", "shareableEventUuid": "uuid-mid"}),
		race("2026-10-31", map[string]any{"title": "Too Late", "shareableEventUuid": "uuid-late"}))
	d := calendarEvents(t, f, map[string]any{"start_date": "2026-10-10", "end_date": "2026-10-20"})
	if ev := toolList(t, "events", d); len(ev) != 1 || asMap(ev[0])["title"] != "In Range" {
		t.Errorf("events = %v", ev)
	}
}

func TestGetCalendarEventsIgnoresNonEventItems(t *testing.T) {
	f := octFake(t,
		race("2026-10-17", map[string]any{"title": "Gyeongju Marathon"}),
		map[string]any{"itemType": "fbtAdaptiveWorkout", "title": "Base", "date": "2026-10-15"},
		map[string]any{"itemType": "weight", "date": "2026-10-16", "weight": 70100.0},
		map[string]any{"itemType": "trainingPlan", "title": "Gyeongju Marathon Plan", "date": "2026-10-17"})
	d := calendarEvents(t, f, octRange)
	if d["count"] != 1.0 || firstEvent(t, d)["title"] != "Gyeongju Marathon" {
		t.Errorf("d = %v", d)
	}
}

func TestGetCalendarEventsDeduplicatesAcrossMonths(t *testing.T) {
	f := newFakeGarmin(t)
	same := month(race("2026-11-15", map[string]any{"title": "MBN Seoul Marathon", "shareableEventUuid": "uuid-seoul"}))
	f.on(monthRoute(2026, 11), same)
	f.on(monthRoute(2026, 12), same)
	d := calendarEvents(t, f, map[string]any{"start_date": "2026-11-01", "end_date": "2026-12-31"})
	n := 0
	for _, c := range f.requests("") {
		if strings.HasPrefix(c.Path, "/calendar-service/year/") {
			n++
		}
	}
	if n != 2 || d["count"] != 1.0 {
		t.Errorf("months %d, d = %v", n, d)
	}
}

func TestGetCalendarEventsSortsByDate(t *testing.T) {
	f := octFake(t,
		race("2026-10-17", map[string]any{"title": "Later", "shareableEventUuid": "uuid-later"}),
		race("2026-10-03", map[string]any{"title": "Earlier", "shareableEventUuid": "uuid-earlier"}))
	ev := toolList(t, "events", calendarEvents(t, f, octRange))
	if asMap(ev[0])["date"] != "2026-10-03" || asMap(ev[1])["date"] != "2026-10-17" {
		t.Errorf("events = %v", ev)
	}
}

func TestGetCalendarEventsHandlesMissingCompletionTarget(t *testing.T) {
	f := octFake(t, race("2026-10-17", map[string]any{"completionTarget": nil}))
	if e := firstEvent(t, calendarEvents(t, f, octRange)); e["distance_meters"] != nil {
		t.Errorf("e = %v", e)
	}
}

func TestGetCalendarEventsIgnoresNonDistanceTarget(t *testing.T) {
	f := octFake(t, race("2026-10-17", map[string]any{"completionTarget": map[string]any{"value": 3600.0, "unit": "second", "unitType": "time"}}))
	if e := firstEvent(t, calendarEvents(t, f, octRange)); e["distance_meters"] != nil {
		t.Errorf("e = %v", e)
	}
}

func TestGetCalendarEventsConvertsMileDistanceToMeters(t *testing.T) {
	f := octFake(t, race("2026-10-17", map[string]any{"completionTarget": map[string]any{"value": 1.0, "unit": "mile", "unitType": "distance"}}))
	if e := firstEvent(t, calendarEvents(t, f, octRange)); e["distance_meters"] != 1609.344 {
		t.Errorf("e = %v", e)
	}
}

func TestGetCalendarEventsNormalisesBlankLocation(t *testing.T) {
	f := octFake(t, race("2026-10-17", map[string]any{"location": ""}))
	if e := firstEvent(t, calendarEvents(t, f, octRange)); e["location"] != nil {
		t.Errorf("e = %v", e)
	}
}

func customGoal(value float64, unit, unitType string) map[string]any {
	return map[string]any{"eventCustomization": map[string]any{"customGoal": map[string]any{"value": value, "unit": unit, "unitType": unitType}}}
}

func TestGetCalendarEventsEnrichesCustomEventGoal(t *testing.T) {
	f := octFake(t, race("2026-10-17", map[string]any{
		"id": 12345.0, "shareableEventUuid": nil,
		"completionTarget": map[string]any{"value": 5.0, "unit": "kilometer", "unitType": "distance"},
	}))
	f.on("GET /calendar-service/event/12345", customGoal(1800, "second", "time"))
	e := firstEvent(t, calendarEvents(t, f, octRange))
	if e["distance_meters"] != 5000.0 || e["goal_time_seconds"] != 1800.0 || len(f.requests("GET /calendar-service/event/12345")) != 1 {
		t.Errorf("e = %v", e)
	}
}

func TestGetCalendarEventsEnrichesShareableEventGoal(t *testing.T) {
	f := octFake(t, race("2026-10-17", map[string]any{"id": nil, "shareableEventUuid": "shareable-uuid"}))
	f.on("GET /calendar-service/event/shareable-uuid/shareable", customGoal(14400, "second", "time"))
	e := firstEvent(t, calendarEvents(t, f, octRange))
	if e["distance_meters"] != 42195.0 || e["goal_time_seconds"] != 14400.0 || len(f.requests("GET /calendar-service/event/shareable-uuid/shareable")) != 1 {
		t.Errorf("e = %v", e)
	}
}

func TestGetCalendarEventsIgnoresNonTimeCustomGoal(t *testing.T) {
	f := octFake(t, race("2026-10-17", nil))
	f.on("GET /calendar-service/event/uuid-1/shareable", customGoal(10, "kilometer", "distance"))
	if e := firstEvent(t, calendarEvents(t, f, octRange)); e["goal_time_seconds"] != nil {
		t.Errorf("e = %v", e)
	}
}

func TestGetCalendarEventsSkipsDetailWithoutEventIdentity(t *testing.T) {
	f := octFake(t, race("2026-10-17", map[string]any{"id": nil, "shareableEventUuid": nil}))
	e := firstEvent(t, calendarEvents(t, f, octRange))
	for _, c := range f.requests("") {
		if strings.HasPrefix(c.Path, "/calendar-service/event/") {
			t.Errorf("unexpected detail request %s", c.Path)
		}
	}
	if e["goal_time_seconds"] != nil {
		t.Errorf("e = %v", e)
	}
}

func TestGetCalendarEventsKeepsEventWhenDetailFetchFails(t *testing.T) {
	f := octFake(t, race("2026-10-17", nil))
	f.on("GET /calendar-service/event/uuid-1/shareable", status(500, map[string]any{"message": "detail unavailable"}))
	d := calendarEvents(t, f, octRange)
	e := firstEvent(t, d)
	if d["count"] != 1.0 || e["distance_meters"] != 42195.0 || e["goal_time_seconds"] != nil {
		t.Errorf("d = %v", d)
	}
}

func TestGetCalendarEventsEmpty(t *testing.T) {
	d := calendarEvents(t, octFake(t), octRange)
	if ev, ok := d["events"].([]any); d["count"] != 0.0 || !ok || len(ev) != 0 {
		t.Errorf("d = %v", d)
	}
}

func TestGetCalendarEventsHandlesNullPayload(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(monthRoute(2026, 10), nil)
	if d := calendarEvents(t, f, octRange); d["count"] != 0.0 {
		t.Errorf("d = %v", d)
	}
}

func TestGetCalendarEventsRejectsReversedRange(t *testing.T) {
	f := newFakeGarmin(t)
	text, _ := callTool(t, newSession(t, f, nil), "get_calendar_events", map[string]any{"start_date": "2026-10-31", "end_date": "2026-10-01"})
	if !strings.Contains(text, "start_date must be on or before end_date") || len(f.requests("")) != 0 {
		t.Errorf("text = %q, requests %d", text, len(f.requests("")))
	}
}

func TestGetCalendarEventsRejectsBadDateFormat(t *testing.T) {
	f := newFakeGarmin(t)
	text, _ := callTool(t, newSession(t, f, nil), "get_calendar_events", map[string]any{"start_date": "10/01/2026", "end_date": "2026-10-31"})
	if text != "Invalid start_date '10/01/2026': expected YYYY-MM-DD" || len(f.requests("")) != 0 {
		t.Errorf("text = %q", text)
	}
}

func TestGetCalendarEventsErrorIsCaught(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(monthRoute(2026, 10), status(500, map[string]any{"message": "boom"}))
	text, isErr := callTool(t, newSession(t, f, nil), "get_calendar_events", octRange)
	if !isErr || text != "Error retrieving calendar events: API Error 500 - boom" {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetCalendarEventsDetailIdCannotEscapeItsEndpoint(t *testing.T) {
	var got []string
	f := octFake(t, race("2026-10-17", map[string]any{"id": "x/../../foo", "shareableEventUuid": nil}))
	h := f.Server.Config.Handler
	f.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.RequestURI)
		h.ServeHTTP(w, r)
	})
	calendarEvents(t, f, octRange)
	found := false
	for _, u := range got {
		found = found || u == "/calendar-service/event/x%2F..%2F..%2Ffoo"
	}
	if !found {
		t.Errorf("requests = %v", got)
	}
}
