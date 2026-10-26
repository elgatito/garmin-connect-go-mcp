package server

import (
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Ported from the weight_management cases of tests/integration/test_other_modules_tools.py.

const (
	weighRange    = "GET /weight-service/weight/range/2024-01-08/2024-01-15"
	weighDay      = "GET /weight-service/weight/dayview/2024-01-15"
	weighDelete   = "DELETE /weight-service/weight/2024-01-15/byversion/"
	userWeight    = "POST /weight-service/user-weight"
	tsLayoutRegex = `^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}$`
)

func measurement70() map[string]any {
	return map[string]any{"weight_grams": 70000.0, "weight_kg": 70.0, "bmi": 22.5, "body_fat_percent": 15.0, "body_water_percent": 60.0,
		"bone_mass_grams": 3200.0, "muscle_mass_grams": 32500.0, "source_type": "MANUAL", "timestamp_gmt": 1705276800000.0}
}

func TestGetWeighInsTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(weighRange, fixture(t, "MOCK_WEIGH_INS"))
	got := resultMap(t, callOK(t, newSession(t, f, nil), "get_weigh_ins", map[string]any{"start_date": "2024-01-08", "end_date": "2024-01-15"}))
	m := measurement70()
	m["date"] = "2024-01-15"
	want := map[string]any{"date_range": map[string]any{"start": "2024-01-08", "end": "2024-01-15"}, "measurement_count": 1.0,
		"days_with_data": 1.0, "measurements": []any{m}, "average_weight_grams": 70000.0, "average_weight_kg": 70.0}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	if c := theCall(t, f, weighRange); c.Query.Get("includeAll") != "True" || len(c.Query) != 1 {
		t.Errorf("query = %v", c.Query)
	}
}

func TestGetWeighInsSortsNewestFirstAndCurates(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(weighRange, map[string]any{"dailyWeightSummaries": []any{
		map[string]any{"allWeightMetrics": []any{map[string]any{"calendarDate": "2024-01-09", "weight": 69555.0}}},
		map[string]any{"allWeightMetrics": []any{map[string]any{"calendarDate": "2024-01-12", "weight": 0.0, "bmi": nil}}}},
		"totalAverage": map[string]any{"weight": 69777.0}})
	got := resultMap(t, callOK(t, newSession(t, f, nil), "get_weigh_ins", map[string]any{"start_date": "2024-01-08", "end_date": "2024-01-15"}))
	want := []any{map[string]any{"date": "2024-01-12", "weight_grams": 0.0}, map[string]any{"date": "2024-01-09", "weight_grams": 69555.0, "weight_kg": 69.56}}
	if !reflect.DeepEqual(got["measurements"], want) || got["average_weight_kg"] != 69.78 {
		t.Errorf("got %v", got)
	}
}

func TestGetWeighInsNoData(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(weighRange, map[string]any{"dailyWeightSummaries": []any{}})
	text := callOK(t, newSession(t, f, nil), "get_weigh_ins", map[string]any{"start_date": "2024-01-08", "end_date": "2024-01-15"})
	if text != "No weight measurements found between 2024-01-08 and 2024-01-15." {
		t.Errorf("text = %q", text)
	}
}

func TestGetWeighInsHandlesNullOptionalSections(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(weighRange, map[string]any{"dailyWeightSummaries": []any{map[string]any{"allWeightMetrics": nil}}, "totalAverage": nil})
	got := resultMap(t, callOK(t, newSession(t, f, nil), "get_weigh_ins", map[string]any{"start_date": "2024-01-08", "end_date": "2024-01-15"}))
	if got["measurement_count"] != 0.0 || !reflect.DeepEqual(got["measurements"], []any{}) {
		t.Errorf("got %v", got)
	}
}

func TestGetDailyWeighInsTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(weighDay, fixture(t, "MOCK_DAILY_WEIGH_INS"))
	got := resultMap(t, callOK(t, newSession(t, f, nil), "get_daily_weigh_ins", map[string]any{"date": "2024-01-15"}))
	m := measurement70()
	want := map[string]any{"date": "2024-01-15", "measurement_count": 1.0, "measurements": []any{m}, "average_weight_grams": 70000.0, "average_weight_kg": 70.0}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	if c := theCall(t, f, weighDay); c.Query.Get("includeAll") != "True" {
		t.Errorf("query = %v", c.Query)
	}
}

func TestGetDailyWeighInsInvalidDate(t *testing.T) {
	text, isErr := callTool(t, newSession(t, newFakeGarmin(t), nil), "get_daily_weigh_ins", map[string]any{"date": "yesterday"})
	if !isErr || text != "Error retrieving daily weight measurements: cdate must be in format 'YYYY-MM-DD', got: yesterday" {
		t.Errorf("%v %q", isErr, text)
	}
}

func dayList(pks ...int) map[string]any {
	var l []any
	for _, i := range pks {
		l = append(l, map[string]any{"samplePk": float64(1705276800000 + i), "weight": float64(70000 + i*100), "sourceType": "MANUAL", "timestampGMT": float64(1705316400000 + i)})
	}
	return map[string]any{"dateWeightList": l}
}

func deleteFake(t *testing.T, day any) *fakeGarmin {
	f := newFakeGarmin(t)
	f.on(weighDay, day)
	for _, i := range []int{0, 1} {
		f.on(fmt.Sprintf("%s%d", weighDelete, 1705276800000+i), map[string]any{})
	}
	return f
}

func deleteCalls(f *fakeGarmin) int {
	n := 0
	for _, c := range f.requests("") {
		if c.Method == http.MethodDelete {
			n++
		}
	}
	return n
}

func TestDeleteWeighInsTool(t *testing.T) {
	f := deleteFake(t, dayList(0))
	got := resultMap(t, callOK(t, newSession(t, f, nil), "delete_weigh_ins", map[string]any{"date": "2024-01-15", "delete_all": true, "confirm": true}))
	want := map[string]any{"status": "success", "date": "2024-01-15", "deleted_count": 1.0, "message": "Weight measurements deleted for 2024-01-15"}
	if !reflect.DeepEqual(got, want) || len(f.requests(weighDelete+"1705276800000")) != 1 || len(f.requests(weighDay)) != 1 {
		t.Errorf("got %v requests=%v", got, f.requests(""))
	}
}

func TestDeleteWeighInsDeletesEveryEntryOfTheDay(t *testing.T) {
	f := deleteFake(t, dayList(0, 1))
	got := resultMap(t, callOK(t, newSession(t, f, nil), "delete_weigh_ins", map[string]any{"date": "2024-01-15", "confirm": true}))
	if got["deleted_count"] != 2.0 || deleteCalls(f) != 2 || len(f.requests(weighDelete+"1705276800001")) != 1 {
		t.Errorf("got %v requests=%v", got, f.requests(""))
	}
}

func TestDeleteWeighInsNothingFound(t *testing.T) {
	f := deleteFake(t, map[string]any{"dateWeightList": []any{}})
	got := resultMap(t, callOK(t, newSession(t, f, nil), "delete_weigh_ins", map[string]any{"date": "2024-01-15", "confirm": true}))
	want := map[string]any{"status": "nothing_deleted", "date": "2024-01-15", "deleted_count": 0.0,
		"message": "No weight measurements found for 2024-01-15; nothing was deleted"}
	if !reflect.DeepEqual(got, want) || deleteCalls(f) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestDeleteWeighInsSeveralWithoutDeleteAll(t *testing.T) {
	f := deleteFake(t, dayList(0, 1))
	got := resultMap(t, callOK(t, newSession(t, f, nil), "delete_weigh_ins", map[string]any{"date": "2024-01-15", "delete_all": false, "confirm": true}))
	if got["status"] != "nothing_deleted" || got["deleted_count"] != 0.0 || !strings.Contains(got["message"].(string), "delete_all=true") || deleteCalls(f) != 0 {
		t.Errorf("got %v", got)
	}
	if got["message"] != "Nothing was deleted for 2024-01-15: the day has either no weight measurements or more than one, and deleting more than one requires delete_all=true" {
		t.Errorf("message = %v", got["message"])
	}
}

func TestDeleteWeighInsPreviewsBeforeDeleting(t *testing.T) {
	for _, count := range []int{1, 2} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			pks := make([]int, count)
			for i := range pks {
				pks[i] = i
			}
			f := deleteFake(t, dayList(pks...))
			got := resultMap(t, callOK(t, newSession(t, f, nil), "delete_weigh_ins", map[string]any{"date": "2024-01-15"}))
			first := asMap(asList(got["measurements"])[0])
			if got["status"] != "needs_confirmation" || got["measurement_count"] != float64(count) || got["delete_all"] != true ||
				first["weight_kg"] != 70.0 || first["source_type"] != "MANUAL" || first["id"] != 1705276800000.0 ||
				!strings.Contains(got["message"].(string), "confirm=true") || deleteCalls(f) != 0 {
				t.Errorf("got %v", got)
			}
		})
	}
}

func TestDeleteWeighInsPreviewEmptyDayDoesNotDelete(t *testing.T) {
	for name, list := range map[string]any{"null": nil, "empty": []any{}} {
		t.Run(name, func(t *testing.T) {
			f := deleteFake(t, map[string]any{"dateWeightList": list})
			got := resultMap(t, callOK(t, newSession(t, f, nil), "delete_weigh_ins", map[string]any{"date": "2024-01-15"}))
			if got["status"] != "nothing_deleted" || got["message"] != "No weight measurements found for 2024-01-15; nothing was deleted" ||
				len(f.requests(weighDay)) != 1 || deleteCalls(f) != 0 {
				t.Errorf("got %v", got)
			}
		})
	}
}

func TestDeleteWeighInsPreviewRespectsDeleteAllFalse(t *testing.T) {
	f := deleteFake(t, map[string]any{"dateWeightList": []any{map[string]any{"weight": 70000.0}, map[string]any{"weight": 70100.0}}})
	got := resultMap(t, callOK(t, newSession(t, f, nil), "delete_weigh_ins", map[string]any{"date": "2024-01-15", "delete_all": false}))
	if got["status"] != "nothing_deleted" || !strings.Contains(got["message"].(string), "delete_all=true") || deleteCalls(f) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestDeleteWeighInsPreviewFailureNeverDeletes(t *testing.T) {
	f := deleteFake(t, status(403, map[string]any{"message": "denied"})) // 401 would trigger a token refresh
	text, isErr := callTool(t, newSession(t, f, nil), "delete_weigh_ins", map[string]any{"date": "2024-01-15"})
	if !isErr || !strings.Contains(text, "API Error 403") || !strings.HasPrefix(text, "Error deleting weight measurements: ") || deleteCalls(f) != 0 {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestDeleteWeighInsPathStaysUnderEndpoint(t *testing.T) {
	// samplePk comes from Garmin, but is still escaped as one path segment.
	f := deleteFake(t, map[string]any{"dateWeightList": []any{map[string]any{"samplePk": "../../x?y"}}})
	f.on("DELETE /weight-service/weight/2024-01-15/byversion/../../x?y", map[string]any{})
	callOK(t, newSession(t, f, nil), "delete_weigh_ins", map[string]any{"date": "2024-01-15", "confirm": true})
	for _, c := range f.requests("") {
		if c.Method == http.MethodDelete && !strings.HasPrefix(c.Path, "/weight-service/weight/2024-01-15/byversion/") {
			t.Errorf("escaped path: %s", c.Path)
		}
	}
	if deleteCalls(f) != 1 {
		t.Errorf("requests = %v", f.requests(""))
	}
}

func TestAddWeighInAcceptsDocumentedLbAlias(t *testing.T) {
	for _, tool := range []string{"add_weigh_in", "add_weigh_in_with_timestamps"} {
		t.Run(tool, func(t *testing.T) {
			setLocal(t, time.FixedZone("LA", -8*3600))
			f := newFakeGarmin(t)
			f.on(userWeight, map[string]any{})
			args := map[string]any{"weight": 155, "unit_key": "lb"}
			if strings.HasSuffix(tool, "timestamps") {
				args["date_timestamp"], args["gmt_timestamp"] = "2024-01-15T07:30:00", "2024-01-15T15:30:00"
			}
			got := resultMap(t, callOK(t, newSession(t, f, nil), tool, args))
			if got["unit"] != "lbs" || bodyOf(t, theCall(t, f, userWeight))["unitKey"] != "lbs" {
				t.Errorf("got %v", got)
			}
		})
	}
}

func TestAddWeighInTool(t *testing.T) {
	setLocal(t, time.FixedZone("LA", -8*3600))
	f := newFakeGarmin(t)
	f.on(userWeight, map[string]any{"status": "success", "weightPk": 12346.0})
	got := resultMap(t, callOK(t, newSession(t, f, nil), "add_weigh_in", map[string]any{"weight": 70.5, "unit_key": " KG "}))
	if !reflect.DeepEqual(got, map[string]any{"status": "success", "weight": 70.5, "unit": "kg", "message": "Weight measurement added successfully"}) {
		t.Errorf("got %v", got)
	}
	b := bodyOf(t, theCall(t, f, userWeight))
	local, gmt := b["dateTimestamp"].(string), b["gmtTimestamp"].(string)
	re := regexp.MustCompile(tsLayoutRegex)
	if !re.MatchString(local) || !re.MatchString(gmt) || b["unitKey"] != "kg" || b["sourceType"] != "MANUAL" || b["value"] != 70.5 || len(b) != 5 {
		t.Fatalf("body = %v", b)
	}
	l, _ := time.Parse("2006-01-02T15:04:05.000", local)
	g, _ := time.Parse("2006-01-02T15:04:05.000", gmt)
	if g.Sub(l) != 8*time.Hour {
		t.Errorf("gmt %s is not local %s + 8h", gmt, local)
	}
	if time.Since(g) > time.Minute { // sanity: a fresh timestamp
		t.Errorf("stale timestamp %s", gmt)
	}
}

func TestAddWeighInRejectsBadInput(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"weight": 70, "unit_key": "stone"}, "Error adding weight measurement: unitKey must be one of {'kg', 'lbs'}"},
		{map[string]any{"weight": 0}, "Error adding weight measurement: weight must be positive, got: 0.0"},
		{map[string]any{"weight": -2.5}, "Error adding weight measurement: weight must be positive, got: -2.5"},
	} {
		if text, isErr := callTool(t, cs, "add_weigh_in", c.args); !isErr || text != c.want {
			t.Errorf("%v: %v %q", c.args, isErr, text)
		}
	}
	if len(f.requests(userWeight)) != 0 {
		t.Error("a weigh-in was sent")
	}
}

func TestAddWeighInWithTimestampsTool(t *testing.T) {
	setLocal(t, time.FixedZone("LA", -8*3600))
	f := newFakeGarmin(t)
	f.on(userWeight, map[string]any{"status": "success", "weightPk": 12347.0})
	got := resultMap(t, callOK(t, newSession(t, f, nil), "add_weigh_in_with_timestamps", map[string]any{"weight": 70.5, "unit_key": "kg"}))
	re := regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d$`)
	if got["status"] != "success" || !re.MatchString(got["timestamp_local"].(string)) || !re.MatchString(got["timestamp_gmt"].(string)) {
		t.Errorf("got %v", got)
	}
	b := bodyOf(t, theCall(t, f, userWeight))
	if b["dateTimestamp"] != got["timestamp_local"].(string)+".000" || b["gmtTimestamp"] != got["timestamp_gmt"].(string)+".000" {
		t.Errorf("body = %v", b)
	}
}

func TestAddWeighInWithTimestampsKeepsGivenLocalTimestamp(t *testing.T) {
	setLocal(t, time.FixedZone("LA", -8*3600))
	f := newFakeGarmin(t)
	f.on(userWeight, map[string]any{})
	got := resultMap(t, callOK(t, newSession(t, f, nil), "add_weigh_in_with_timestamps",
		map[string]any{"weight": 70.5, "unit_key": "kg", "date_timestamp": "2024-01-15T07:30:00"}))
	if got["status"] != "success" || got["timestamp_local"] != "2024-01-15T07:30:00" || got["timestamp_gmt"] != nil {
		t.Errorf("got %v", got)
	}
	want := map[string]any{"dateTimestamp": "2024-01-15T07:30:00.000", "gmtTimestamp": "2024-01-15T15:30:00.000", "unitKey": "kg", "sourceType": "MANUAL", "value": 70.5}
	if b := bodyOf(t, theCall(t, f, userWeight)); !reflect.DeepEqual(b, want) {
		t.Errorf("body = %v", b)
	}
}

func TestAddWeighInWithTimestampsPassesBothTimestamps(t *testing.T) {
	setLocal(t, time.FixedZone("LA", -8*3600))
	f := newFakeGarmin(t)
	f.on(userWeight, map[string]any{})
	callOK(t, newSession(t, f, nil), "add_weigh_in_with_timestamps",
		map[string]any{"weight": 70.5, "date_timestamp": "2024-01-15T07:30:00", "gmt_timestamp": "2024-01-15T15:30:00"})
	want := map[string]any{"dateTimestamp": "2024-01-15T07:30:00.000", "gmtTimestamp": "2024-01-15T15:30:00.000", "unitKey": "kg", "sourceType": "MANUAL", "value": 70.5}
	if b := bodyOf(t, theCall(t, f, userWeight)); !reflect.DeepEqual(b, want) {
		t.Errorf("body = %v", b)
	}
}

func TestAddWeighInWithTimestampsRequiresLocalWithGmt(t *testing.T) {
	f := newFakeGarmin(t)
	text, isErr := callTool(t, newSession(t, f, nil), "add_weigh_in_with_timestamps", map[string]any{"weight": 70.5, "gmt_timestamp": "2024-01-15T15:30:00"})
	if isErr || !strings.Contains(text, "date_timestamp is required") || len(f.requests(userWeight)) != 0 {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestAddWeighInWithTimestampsOffsetAndErrors(t *testing.T) {
	setLocal(t, time.FixedZone("LA", -8*3600))
	f := newFakeGarmin(t)
	f.on(userWeight, map[string]any{})
	cs := newSession(t, f, nil)
	// A local timestamp with an offset is converted to the server's zone; a GMT one with an offset to UTC.
	callOK(t, cs, "add_weigh_in_with_timestamps", map[string]any{"weight": 70.5, "date_timestamp": "2024-01-15T17:30:00+02:00", "gmt_timestamp": "2024-01-15T17:30:00+02:00"})
	want := map[string]any{"dateTimestamp": "2024-01-15T07:30:00.000", "gmtTimestamp": "2024-01-15T15:30:00.000", "unitKey": "kg", "sourceType": "MANUAL", "value": 70.5}
	if b := bodyOf(t, theCall(t, f, userWeight)); !reflect.DeepEqual(b, want) {
		t.Errorf("body = %v", b)
	}
	text, isErr := callTool(t, cs, "add_weigh_in_with_timestamps", map[string]any{"weight": 70.5, "date_timestamp": "nope"})
	if !isErr || text != "Error adding weight measurement with timestamps: Invalid isoformat string: 'nope'" {
		t.Errorf("%v %q", isErr, text)
	}
}
