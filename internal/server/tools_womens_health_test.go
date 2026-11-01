package server

import (
	"reflect"
	"testing"
)

// Ported from the womens_health cases of tests/integration/test_other_modules_tools.py.

const (
	pregnancyRoute = "GET /periodichealth-service/menstrualcycle/pregnancysnapshot"
	dayviewRoute   = "GET /periodichealth-service/menstrualcycle/dayview/2024-01-15"
	calendarBase   = "GET /periodichealth-service/menstrualcycle/calendar/"
)

func TestGetPregnancySummaryTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(pregnancyRoute, map[string]any{"isPregnant": false})
	got := decode(t, callOK(t, newSession(t, f, nil), "get_pregnancy_summary", nil))
	if !reflect.DeepEqual(got, map[string]any{"isPregnant": false}) {
		t.Errorf("got %v", got)
	}
	theCall(t, f, pregnancyRoute)
}

func TestGetPregnancySummaryEmpty(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(pregnancyRoute, map[string]any{})
	if text := callOK(t, newSession(t, f, nil), "get_pregnancy_summary", nil); text != "No pregnancy summary data found." {
		t.Errorf("text = %q", text)
	}
}

func TestGetMenstrualDataForDateTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(dayviewRoute, fixture(t, "MOCK_MENSTRUAL_DATA"))
	got := decode(t, callOK(t, newSession(t, f, nil), "get_menstrual_data_for_date", map[string]any{"date": "2024-01-15"}))
	if !reflect.DeepEqual(got, fixture(t, "MOCK_MENSTRUAL_DATA")) {
		t.Errorf("got %v", got)
	}
	theCall(t, f, dayviewRoute)
}

func TestGetMenstrualDataForDateNoDataAndBadDate(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(dayviewRoute, map[string]any{})
	cs := newSession(t, f, nil)
	if text := callOK(t, cs, "get_menstrual_data_for_date", map[string]any{"date": "2024-01-15"}); text != "No menstrual data found for 2024-01-15." {
		t.Errorf("text = %q", text)
	}
	text, isErr := callTool(t, cs, "get_menstrual_data_for_date", map[string]any{"date": "x"})
	if !isErr || text != "Error retrieving menstrual data: fordate must be in format 'YYYY-MM-DD', got: x" {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetMenstrualCalendarDataTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(calendarBase+"2024-01-01/2024-01-31", []any{fixture(t, "MOCK_MENSTRUAL_DATA")})
	got := decode(t, callOK(t, newSession(t, f, nil), "get_menstrual_calendar_data", map[string]any{"start_date": "2024-01-01", "end_date": "2024-01-31"}))
	if !reflect.DeepEqual(got, []any{fixture(t, "MOCK_MENSTRUAL_DATA")}) {
		t.Errorf("got %v", got)
	}
	theCall(t, f, calendarBase+"2024-01-01/2024-01-31")
}

func TestGetMenstrualCalendarDataChunking(t *testing.T) {
	second := fixture(t, "MOCK_MENSTRUAL_DATA").(map[string]any)
	second["calendarDate"], second["cycleDay"] = "2026-04-03", 1.0
	f := newFakeGarmin(t)
	f.on(calendarBase+"2026-01-01/2026-04-02", []any{fixture(t, "MOCK_MENSTRUAL_DATA")})
	f.on(calendarBase+"2026-04-03/2026-06-30", []any{second})
	got := decode(t, callOK(t, newSession(t, f, nil), "get_menstrual_calendar_data", map[string]any{"start_date": "2026-01-01", "end_date": "2026-06-30"}))
	if !reflect.DeepEqual(got, []any{fixture(t, "MOCK_MENSTRUAL_DATA"), second}) || len(f.requests("")) != 2 {
		t.Errorf("got %v requests %v", got, f.requests(""))
	}
}

func TestMenstrualCalendarDeduplicatesCyclesCrossingChunkSeam(t *testing.T) {
	for _, dictShape := range []bool{false, true} {
		t.Run(map[bool]string{false: "list", true: "dict"}[dictShape], func(t *testing.T) {
			before := map[string]any{"cycleId": 1.0, "startDate": "2026-02-20"}
			spanning := map[string]any{"cycleId": 2.0, "startDate": "2026-03-20", "endDate": "2026-04-16"}
			after := map[string]any{"cycleId": 3.0, "startDate": "2026-04-17"}
			wrap := func(items ...any) any {
				if dictShape {
					return map[string]any{"cycleSummaries": items}
				}
				return items
			}
			f := newFakeGarmin(t)
			f.on(calendarBase+"2026-01-01/2026-04-02", wrap(before, spanning))
			f.on(calendarBase+"2026-04-03/2026-06-30", wrap(map[string]any{"endDate": "2026-04-16", "startDate": "2026-03-20", "cycleId": 2.0}, after))
			got := decode(t, callOK(t, newSession(t, f, nil), "get_menstrual_calendar_data", map[string]any{"start_date": "2026-01-01", "end_date": "2026-06-30"}))
			if !reflect.DeepEqual(got, jsonValue(t, wrap(before, spanning, after))) {
				t.Errorf("got %v", got)
			}
		})
	}
}

func TestMenstrualCalendarStitchShapes(t *testing.T) {
	cases := []struct {
		name   string
		chunks []any
		want   any
	}{
		{"single chunk untouched", []any{[]any{1.0, 1.0}}, []any{1.0, 1.0}},
		{"dict of lists merges per key", []any{map[string]any{"a": []any{1.0}}, map[string]any{"a": []any{1.0, 2.0}, "b": []any{3.0}}},
			map[string]any{"a": []any{1.0, 2.0}, "b": []any{3.0}}},
		{"mixed shapes stay per chunk", []any{[]any{1.0}, map[string]any{"a": 1.0}}, []any{[]any{1.0}, map[string]any{"a": 1.0}}},
	}
	for _, c := range cases {
		if got := stitchMenstrualChunks(c.chunks); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}

func TestGetMenstrualCalendarDataEdges(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(calendarBase+"2024-01-01/2024-01-31", map[string]any{})
	cs := newSession(t, f, nil)
	for _, c := range []struct{ start, end, want string }{
		{"2024-01-31", "2024-01-01", "end_date 2024-01-01 is before start_date 2024-01-31."},
		{"2024-01-01", "2024-01-31", "No menstrual calendar data found between 2024-01-01 and 2024-01-31."},
		{"01/01/2024", "2024-01-31", "Error retrieving menstrual calendar data: Invalid isoformat string: '01/01/2024'"},
	} {
		text, _ := callTool(t, cs, "get_menstrual_calendar_data", map[string]any{"start_date": c.start, "end_date": c.end})
		if text != c.want {
			t.Errorf("%s..%s: %q", c.start, c.end, text)
		}
	}
	// 92 days is one window; 93 days is two.
	f.on(calendarBase+"2024-01-01/2024-04-01", []any{1.0})
	callOK(t, cs, "get_menstrual_calendar_data", map[string]any{"start_date": "2024-01-01", "end_date": "2024-04-01"})
	if n := len(f.requests(calendarBase + "2024-01-01/2024-04-01")); n != 1 {
		t.Errorf("92 days: %d requests", n)
	}
	f.on(calendarBase+"2024-04-02/2024-04-02", []any{2.0})
	callOK(t, cs, "get_menstrual_calendar_data", map[string]any{"start_date": "2024-01-01", "end_date": "2024-04-02"})
	if n := len(f.requests(calendarBase + "2024-04-02/2024-04-02")); n != 1 {
		t.Errorf("93 days: %d requests to the second window", n)
	}
}
