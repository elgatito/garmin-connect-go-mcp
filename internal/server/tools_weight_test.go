package server

import (
	"strings"
	"testing"
)

const (
	weightRangePath    = "/weight-service/weight/range/2024-01-08/2024-01-15"
	weightDayviewPath  = "/weight-service/weight/dayview/2024-01-15"
	weightAddPath      = "/weight-service/user-weight"
	weightDeletePrefix = "/weight-service/weight/2024-01-15/byversion/"
)

func TestGetWeighInsTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+weightRangePath, fixture(t, "MOCK_WEIGH_INS"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_weigh_ins", map[string]any{
		"start_date": "2024-01-08", "end_date": "2024-01-15",
	})
	if isErr {
		t.Fatalf("error: %s", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["measurement_count"] != 1.0 || data["days_with_data"] != 1.0 {
		t.Fatalf("data = %v", data)
	}
	measurement := asMap(asList(data["measurements"])[0])
	if measurement["weight_kg"] != 70.0 || measurement["bmi"] != 22.5 {
		t.Fatalf("measurement = %v", measurement)
	}
	calls := f.callsMatching("GET", weightRangePath)
	if calls[0].Query.Get("includeAll") != "true" {
		t.Fatalf("query = %v", calls[0].Query)
	}
}

func TestGetWeighInsHandlesNullOptionalSections(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+weightRangePath, map[string]any{
		"dailyWeightSummaries": []any{map[string]any{"allWeightMetrics": nil}},
		"totalAverage":         nil,
	})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_weigh_ins", map[string]any{
		"start_date": "2024-01-08", "end_date": "2024-01-15",
	})
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["measurement_count"] != 0.0 {
		t.Fatalf("measurement_count = %v", data["measurement_count"])
	}
	if len(asList(data["measurements"])) != 0 {
		t.Fatalf("measurements = %v", data["measurements"])
	}
}

func TestGetDailyWeighInsTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+weightDayviewPath, fixture(t, "MOCK_DAILY_WEIGH_INS"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_daily_weigh_ins", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("error: %s", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["date"] != "2024-01-15" || data["measurement_count"] != 1.0 {
		t.Fatalf("data = %v", data)
	}
	if calls := f.callsMatching("GET", weightDayviewPath); len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
}

func TestDeleteWeighInsTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+weightDayviewPath, fixture(t, "MOCK_DAILY_WEIGH_INS"))
	f.on("DELETE "+weightDeletePrefix+"1705276800000", nil)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "delete_weigh_ins", map[string]any{
		"date": "2024-01-15", "delete_all": true, "confirm": true,
	})
	if isErr {
		t.Fatalf("error: %s", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["status"] != "success" || data["deleted_count"] != 1.0 {
		t.Fatalf("data = %v", data)
	}
	if calls := f.callsMatching("DELETE", weightDeletePrefix+"1705276800000"); len(calls) != 1 {
		t.Fatalf("delete calls = %d", len(calls))
	}
}

func TestDeleteWeighInsNothingFound(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+weightDayviewPath, map[string]any{"dateWeightList": []any{}})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "delete_weigh_ins", map[string]any{
		"date": "2024-01-15", "confirm": true,
	})
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["status"] != "nothing_deleted" || data["deleted_count"] != 0.0 {
		t.Fatalf("data = %v", data)
	}
	if !strings.Contains(data["message"].(string), "No weight measurements found") {
		t.Fatalf("message = %v", data["message"])
	}
}

func TestDeleteWeighInsSeveralWithoutDeleteAll(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+weightDayviewPath, map[string]any{"dateWeightList": []any{
		map[string]any{"samplePk": 1.0, "weight": 70000.0},
		map[string]any{"samplePk": 2.0, "weight": 70100.0},
	}})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "delete_weigh_ins", map[string]any{
		"date": "2024-01-15", "delete_all": false, "confirm": true,
	})
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["status"] != "nothing_deleted" {
		t.Fatalf("data = %v", data)
	}
	if !strings.Contains(data["message"].(string), "delete_all=true") {
		t.Fatalf("message = %v", data["message"])
	}
}

func TestDeleteWeighInsPreviewsBeforeDeleting(t *testing.T) {
	for _, count := range []int{1, 2} {
		list := make([]any, 0, count)
		for i := 0; i < count; i++ {
			list = append(list, map[string]any{
				"samplePk":     float64(i),
				"weight":       70000.0 + float64(i)*100,
				"sourceType":   "MANUAL",
				"timestampGMT": 1705316400000.0 + float64(i),
			})
		}
		f := newFakeGarmin(t)
		f.on("GET "+weightDayviewPath, map[string]any{"dateWeightList": list})
		cs := newSession(t, f, nil)

		text, _ := callTool(t, cs, "delete_weigh_ins", map[string]any{"date": "2024-01-15"})
		var data map[string]any
		decodeJSON(t, text, &data)
		if data["status"] != "needs_confirmation" || data["measurement_count"] != float64(count) {
			t.Fatalf("count %d: data = %v", count, data)
		}
		measurement := asMap(asList(data["measurements"])[0])
		if measurement["weight_kg"] != 70.0 || measurement["source_type"] != "MANUAL" {
			t.Fatalf("measurement = %v", measurement)
		}
		if !strings.Contains(data["message"].(string), "confirm=true") {
			t.Fatalf("message = %v", data["message"])
		}
		if calls := f.callsMatching("DELETE", weightDeletePrefix); len(calls) != 0 {
			// callsMatching matches exact paths; assert no delete calls at all.
			t.Fatalf("delete calls = %d", len(calls))
		}
		for _, call := range f.recorded() {
			if call.Method == "DELETE" {
				t.Fatalf("unexpected delete %s", call.Path)
			}
		}
	}
}

func TestDeleteWeighInsMissingSamplePkNeverDeletes(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+weightDayviewPath, map[string]any{"dateWeightList": []any{
		map[string]any{"samplePk": 1705276800000.0, "weight": 70000.0},
		map[string]any{"weight": 70100.0},
	}})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "delete_weigh_ins", map[string]any{
		"date": "2024-01-15", "confirm": true,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "samplePk") {
		t.Fatalf("text = %q", text)
	}
	if strings.Contains(text, "write_committed") {
		t.Fatalf("validation failure reported a write: %q", text)
	}
	for _, call := range f.recorded() {
		if call.Method == "DELETE" {
			t.Fatalf("unexpected delete %s", call.Path)
		}
	}
}

func TestDeleteWeighInsPreviewEmptyDayDoesNotDelete(t *testing.T) {
	for _, weightList := range []any{nil, []any{}} {
		f := newFakeGarmin(t)
		f.on("GET "+weightDayviewPath, map[string]any{"dateWeightList": weightList})
		cs := newSession(t, f, nil)

		text, _ := callTool(t, cs, "delete_weigh_ins", map[string]any{"date": "2024-01-15"})
		var data map[string]any
		decodeJSON(t, text, &data)
		if data["status"] != "nothing_deleted" {
			t.Fatalf("data = %v", data)
		}
		if calls := f.callsMatching("GET", weightDayviewPath); len(calls) != 1 {
			t.Fatalf("dayview calls = %d", len(calls))
		}
		for _, call := range f.recorded() {
			if call.Method == "DELETE" {
				t.Fatalf("unexpected delete %s", call.Path)
			}
		}
	}
}

func TestDeleteWeighInsPreviewRespectsDeleteAllFalse(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+weightDayviewPath, map[string]any{"dateWeightList": []any{
		map[string]any{"weight": 70000.0},
		map[string]any{"weight": 70100.0},
	}})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "delete_weigh_ins", map[string]any{
		"date": "2024-01-15", "delete_all": false,
	})
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["status"] != "nothing_deleted" {
		t.Fatalf("data = %v", data)
	}
	if !strings.Contains(data["message"].(string), "delete_all=true") {
		t.Fatalf("message = %v", data["message"])
	}
	for _, call := range f.recorded() {
		if call.Method == "DELETE" {
			t.Fatalf("unexpected delete %s", call.Path)
		}
	}
}

func TestDeleteWeighInsPreviewFailureNeverDeletes(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+weightDayviewPath, status(500, nil))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "delete_weigh_ins", map[string]any{"date": "2024-01-15"})
	if !strings.Contains(text, "Error deleting weight measurements") {
		t.Fatalf("text = %q", text)
	}
	for _, call := range f.recorded() {
		if call.Method == "DELETE" {
			t.Fatalf("unexpected delete %s", call.Path)
		}
	}
}

func TestAddWeighInAcceptsDocumentedLbAlias(t *testing.T) {
	for _, toolName := range []string{"add_weigh_in", "add_weigh_in_with_timestamps"} {
		t.Run(toolName, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on("POST "+weightAddPath, map[string]any{})
			cs := newSession(t, f, nil)

			args := map[string]any{"weight": 155, "unit_key": "lb"}
			if toolName == "add_weigh_in_with_timestamps" {
				args["date_timestamp"] = "2024-01-15T07:30:00"
				args["gmt_timestamp"] = "2024-01-15T15:30:00"
			}
			text, _ := callTool(t, cs, toolName, args)
			var data map[string]any
			decodeJSON(t, text, &data)
			if data["unit"] != "lbs" {
				t.Fatalf("data = %v", data)
			}
			calls := f.callsMatching("POST", weightAddPath)
			var payload map[string]any
			decodeJSON(t, string(calls[0].Body), &payload)
			if payload["unitKey"] != "lbs" {
				t.Fatalf("payload = %v", payload)
			}
		})
	}
}

func TestAddWeighInTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+weightAddPath, map[string]any{"status": "success", "weightPk": 12346.0})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "add_weigh_in", map[string]any{"weight": 70.5, "unit_key": "kg"})
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["status"] != "success" || data["weight"] != 70.5 {
		t.Fatalf("data = %v", data)
	}
	calls := f.callsMatching("POST", weightAddPath)
	var payload map[string]any
	decodeJSON(t, string(calls[0].Body), &payload)
	if payload["value"] != 70.5 || payload["unitKey"] != "kg" || payload["sourceType"] != "MANUAL" {
		t.Fatalf("payload = %v", payload)
	}
}

func TestAddWeighInWithTimestampsTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+weightAddPath, map[string]any{"status": "success", "weightPk": 12347.0})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "add_weigh_in_with_timestamps", map[string]any{
		"weight": 70.5, "unit_key": "kg",
	})
	if isErr {
		t.Fatalf("error: %s", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["status"] != "success" || data["timestamp_local"] == "" {
		t.Fatalf("data = %v", data)
	}
}

func TestAddWeighInWithTimestampsKeepsGivenLocalTimestamp(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+weightAddPath, map[string]any{})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "add_weigh_in_with_timestamps", map[string]any{
		"weight": 70.5, "unit_key": "kg", "date_timestamp": "2024-01-15T07:30:00",
	})
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["status"] != "success" || data["timestamp_local"] != "2024-01-15T07:30:00" {
		t.Fatalf("data = %v", data)
	}
	calls := f.callsMatching("POST", weightAddPath)
	var payload map[string]any
	decodeJSON(t, string(calls[0].Body), &payload)
	if payload["dateTimestamp"] != "2024-01-15T07:30:00.000" {
		t.Fatalf("payload = %v", payload)
	}
}

func TestAddWeighInWithTimestampsPassesBothTimestamps(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+weightAddPath, map[string]any{})
	cs := newSession(t, f, nil)

	if _, isErr := callTool(t, cs, "add_weigh_in_with_timestamps", map[string]any{
		"weight":         70.5,
		"date_timestamp": "2024-01-15T07:30:00",
		"gmt_timestamp":  "2024-01-15T15:30:00",
	}); isErr {
		t.Fatal("add_weigh_in_with_timestamps returned an error")
	}
	calls := f.callsMatching("POST", weightAddPath)
	var payload map[string]any
	decodeJSON(t, string(calls[0].Body), &payload)
	if payload["dateTimestamp"] != "2024-01-15T07:30:00.000" || payload["gmtTimestamp"] != "2024-01-15T15:30:00.000" {
		t.Fatalf("payload = %v", payload)
	}
}

func TestAddWeighInWithTimestampsRequiresLocalWithGmt(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "add_weigh_in_with_timestamps", map[string]any{
		"weight": 70.5, "gmt_timestamp": "2024-01-15T15:30:00",
	})
	if !strings.Contains(text, "date_timestamp is required") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("POST", weightAddPath); len(calls) != 0 {
		t.Fatalf("post calls = %d", len(calls))
	}
}

// --- pre-send validation and tracker ordering ---

func TestAddWeighInRejectsInvalidInputBeforeWrite(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"invalid unit", map[string]any{"weight": 70.5, "unit_key": "stones"}, "unitKey must be one of"},
		{"zero weight", map[string]any{"weight": 0, "unit_key": "kg"}, "weight must be positive"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGarmin(t)
			cs := newSession(t, f, nil)
			text, isErr := callTool(t, cs, "add_weigh_in", tc.args)
			if isErr {
				t.Fatalf("unexpected structured error: %s", text)
			}
			if !strings.Contains(text, "Error adding weight measurement:") || !strings.Contains(text, tc.want) {
				t.Fatalf("text = %q", text)
			}
			if calls := f.callsMatching("POST", weightAddPath); len(calls) != 0 {
				t.Fatalf("post calls = %d", len(calls))
			}
		})
	}
}

func TestAddWeighInWithTimestampsRejectsInvalidTimestampBeforeWrite(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "add_weigh_in_with_timestamps", map[string]any{
		"weight": 70.5, "date_timestamp": "not-a-time",
	})
	if isErr {
		t.Fatalf("unexpected structured error: %s", text)
	}
	if !strings.Contains(text, "Error adding weight measurement with timestamps:") ||
		!strings.Contains(text, "invalid timestamp format") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("POST", weightAddPath); len(calls) != 0 {
		t.Fatalf("post calls = %d", len(calls))
	}
}

func TestAddWeighInWriteFailureReportsNotCommitted(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+weightAddPath, status(400, map[string]any{"message": "bad weight"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "add_weigh_in", map[string]any{"weight": 70.5})
	if !isErr {
		t.Fatalf("expected write failure, got %q", text)
	}
	var payload map[string]any
	decodeJSON(t, text, &payload)
	if payload["write_committed"] != false {
		t.Fatalf("write_committed = %v", payload["write_committed"])
	}
	if payload["stage"] != "garmin_request" {
		t.Fatalf("stage = %v", payload["stage"])
	}
}

func TestDeleteWeighInsConfirmPreReadFailureDoesNotDelete(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+weightDayviewPath, status(500, nil))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "delete_weigh_ins", map[string]any{
		"date": "2024-01-15", "confirm": true,
	})
	if !isErr || !strings.Contains(text, "Error deleting weight measurements") {
		t.Fatalf("text = %q, isErr = %v", text, isErr)
	}
	if strings.Contains(text, "write_committed") || strings.Contains(text, "may have reached Garmin") {
		t.Fatalf("pre-read failure reported as a write: %q", text)
	}
	for _, call := range f.recorded() {
		if call.Method == "DELETE" {
			t.Fatalf("unexpected delete %s", call.Path)
		}
	}
}

func TestDeleteWeighInsWriteFailureReportsNotCommitted(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+weightDayviewPath, map[string]any{"dateWeightList": []any{
		map[string]any{"samplePk": 1705276800000.0, "weight": 70000.0},
	}})
	f.on("DELETE "+weightDeletePrefix+"1705276800000", status(400, map[string]any{"message": "no"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "delete_weigh_ins", map[string]any{
		"date": "2024-01-15", "confirm": true,
	})
	if !isErr {
		t.Fatalf("expected write failure, got %q", text)
	}
	var payload map[string]any
	decodeJSON(t, text, &payload)
	if payload["write_committed"] != false {
		t.Fatalf("write_committed = %v", payload["write_committed"])
	}
}
