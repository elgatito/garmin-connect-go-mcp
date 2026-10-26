package server

import (
	"testing"
)

const (
	womensPregnancyPath = "/periodichealth-service/menstrualcycle/pregnancysnapshot"
	womensDayviewPath   = "/periodichealth-service/menstrualcycle/dayview/2024-01-15"
	womensCalendarPath  = "/periodichealth-service/menstrualcycle/calendar/2024-01-01/2024-01-31"
)

func TestGetPregnancySummaryTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+womensPregnancyPath, map[string]any{"isPregnant": false})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_pregnancy_summary", nil)
	if isErr {
		t.Fatalf("error: %s", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["isPregnant"] != false {
		t.Fatalf("data = %v", data)
	}
	if calls := f.callsMatching("GET", womensPregnancyPath); len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
}

func TestGetMenstrualDataForDateTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+womensDayviewPath, fixture(t, "MOCK_MENSTRUAL_DATA"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_menstrual_data_for_date", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("error: %s", text)
	}
	var data any
	decodeJSON(t, text, &data)
	sameJSON(t, data, fixture(t, "MOCK_MENSTRUAL_DATA"))
}

func TestGetMenstrualCalendarDataTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+womensCalendarPath, []any{fixture(t, "MOCK_MENSTRUAL_DATA")})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_menstrual_calendar_data", map[string]any{
		"start_date": "2024-01-01", "end_date": "2024-01-31",
	})
	if isErr {
		t.Fatalf("error: %s", text)
	}
	var data any
	decodeJSON(t, text, &data)
	sameJSON(t, data, []any{fixture(t, "MOCK_MENSTRUAL_DATA")})
}

func TestGetMenstrualCalendarDataChunking(t *testing.T) {
	first := fixture(t, "MOCK_MENSTRUAL_DATA")
	second := asMap(deepCopyValue(first))
	second["calendarDate"] = "2026-04-03"
	second["cycleDay"] = 1.0
	f := newFakeGarmin(t)
	f.on("GET /periodichealth-service/menstrualcycle/calendar/2026-01-01/2026-04-02", []any{first})
	f.on("GET /periodichealth-service/menstrualcycle/calendar/2026-04-03/2026-06-30", []any{second})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_menstrual_calendar_data", map[string]any{
		"start_date": "2026-01-01", "end_date": "2026-06-30",
	})
	if isErr {
		t.Fatalf("error: %s", text)
	}
	calls := f.callsMatching("GET", "/periodichealth-service/menstrualcycle/calendar/2026-01-01/2026-04-02")
	if len(calls) != 1 {
		t.Fatalf("first chunk calls = %d", len(calls))
	}
	calls = f.callsMatching("GET", "/periodichealth-service/menstrualcycle/calendar/2026-04-03/2026-06-30")
	if len(calls) != 1 {
		t.Fatalf("second chunk calls = %d", len(calls))
	}
	var data any
	decodeJSON(t, text, &data)
	sameJSON(t, data, []any{first, second})
}

func TestMenstrualCalendarDeduplicatesCyclesCrossingChunkSeam(t *testing.T) {
	before := map[string]any{"cycleId": 1.0, "startDate": "2026-02-20"}
	spanning := map[string]any{"cycleId": 2.0, "startDate": "2026-03-20", "endDate": "2026-04-16"}
	after := map[string]any{"cycleId": 3.0, "startDate": "2026-04-17"}
	for _, dictShape := range []bool{false, true} {
		wrap := func(items []any) any {
			if dictShape {
				return map[string]any{"cycleSummaries": items}
			}
			return items
		}
		f := newFakeGarmin(t)
		f.on("GET /periodichealth-service/menstrualcycle/calendar/2026-01-01/2026-04-02", wrap([]any{before, spanning}))
		f.on("GET /periodichealth-service/menstrualcycle/calendar/2026-04-03/2026-06-30", wrap([]any{spanning, after}))
		cs := newSession(t, f, nil)

		text, isErr := callTool(t, cs, "get_menstrual_calendar_data", map[string]any{
			"start_date": "2026-01-01", "end_date": "2026-06-30",
		})
		if isErr {
			t.Fatalf("dictShape %v: error: %s", dictShape, text)
		}
		var data any
		decodeJSON(t, text, &data)
		sameJSON(t, data, wrap([]any{before, spanning, after}))
	}
}
