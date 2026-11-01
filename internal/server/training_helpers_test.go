package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

// Ported from tests/unit/test_training_activity_types.py, test_training_device_selection.py,
// test_training_effect.py and test_training_vo2.py.

func strp(s string) *string { return &s }

func eq(t *testing.T, name string, got, want any) {
	t.Helper()
	if g, w := jsonValue(t, got), jsonValue(t, want); !reflect.DeepEqual(g, w) {
		t.Errorf("%s = %v, want %v", name, g, w)
	}
}

func TestActivityTypeMappingRetriesAfterTransientFailure(t *testing.T) {
	calls := 0
	fetch := func(context.Context) (any, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("temporary lookup failure")
		}
		return []any{map[string]any{"typeId": 3.0, "typeKey": "hiking"}}, nil
	}
	cache := &activityTypeCache{}
	ctx := context.Background()
	if got := cache.mapping(ctx, fetch); !reflect.DeepEqual(got, map[int]string{}) {
		t.Errorf("first = %v", got)
	}
	for i := 0; i < 2; i++ {
		if got := cache.mapping(ctx, fetch); !reflect.DeepEqual(got, map[int]string{3: "hiking"}) {
			t.Errorf("call %d = %v", i+2, got)
		}
	}
	if calls != 2 {
		t.Errorf("fetched %d times, want 2", calls)
	}
}

func TestDeviceLookupFailureOrMissingPrimaryKeepsIdentityUnknown(t *testing.T) {
	latest := map[string]any{"7": map[string]any{"trainingStatus": "PRODUCTIVE", "primaryTrainingDevice": true}}
	for name, devices := range map[string]any{"none": nil, "no primary": []any{map[string]any{"deviceId": 7.0}}} {
		if got := selectTrainingDevice(latest, nil, devices).Primary; got != nil {
			t.Errorf("%s: primary = %v, want unknown", name, *got)
		}
	}
}

func TestZeroDeviceIdIsNotTreatedAsMissingAndMalformedRecordsAreIgnored(t *testing.T) {
	sel := selectTrainingDevice(
		map[string]any{"0": map[string]any{"deviceId": 0.0, "primaryTrainingDevice": true}}, nil,
		[]any{nil, "bad", map[string]any{"deviceId": 0.0, "primaryDevice": true}})
	if sel.Data["deviceId"] != 0.0 || sel.ID == nil || *sel.ID != "0" || sel.Primary == nil || !*sel.Primary {
		t.Errorf("%+v", sel)
	}
}

func TestPrimaryTrainingDeviceIsDeterministicFallback(t *testing.T) {
	sel := selectTrainingDevice(
		map[string]any{"200": map[string]any{"deviceId": 200.0}, "100": map[string]any{"deviceId": 100.0, "primaryTrainingDevice": true}},
		[]string{"200", "100"},
		[]any{map[string]any{"deviceId": 100.0, "displayName": "Primary"}, map[string]any{"deviceId": 200.0}})
	if sel.ID == nil || *sel.ID != "100" || sel.Data["deviceId"] != 100.0 || sel.Primary != nil {
		t.Errorf("%+v", sel)
	}
}

func TestMissingPrimaryTrackerPrefersPrimaryTrainingMarker(t *testing.T) {
	sel := selectTrainingDevice(
		map[string]any{"200": map[string]any{"deviceId": 200.0}, "300": map[string]any{"deviceId": 300.0, "primaryTrainingDevice": true}},
		[]string{"200", "300"},
		[]any{map[string]any{"deviceId": 100.0, "primaryDevice": true}, map[string]any{"deviceId": 200.0}, map[string]any{"deviceId": 300.0}})
	if sel.ID == nil || *sel.ID != "300" || sel.Primary == nil || *sel.Primary {
		t.Errorf("%+v", sel)
	}
}

// Extra: without any marker the first device in Garmin's order is used, and the order is not alphabetical.
func TestSelectTrainingDeviceUsesGarminOrder(t *testing.T) {
	latest := map[string]any{"b": map[string]any{"x": 1.0}, "a": map[string]any{"x": 2.0}}
	if got := selectTrainingDevice(latest, []string{"b", "a"}, nil); got.Data["x"] != 1.0 || *got.ID != "b" {
		t.Errorf("%+v", got)
	}
	if got := selectTrainingDevice(latest, nil, nil); got.Data["x"] != 2.0 {
		t.Errorf("no recorded order must fall back to sorted keys: %+v", got)
	}
}

func TestReadinessRequiresMatchingTrackerAndDate(t *testing.T) {
	payload := []any{
		map[string]any{"deviceId": 200.0, "calendarDate": "2026-10-09", "acuteLoad": 999.0},
		map[string]any{"deviceId": 100.0, "calendarDate": "2026-10-08", "acuteLoad": 888.0},
		map[string]any{"deviceId": 100.0, "calendarDate": "2026-10-09", "acuteLoad": 123.0},
	}
	if load, reason := trackerAcuteLoad(payload, "2026-10-09", strp("100")); load != 123.0 || reason != "" {
		t.Errorf("%v %q", load, reason)
	}
	if load, _ := trackerAcuteLoad(payload[:2], "2026-10-09", strp("100")); load != nil {
		t.Errorf("load = %v", load)
	}
	noID := []any{map[string]any{"calendarDate": "2026-10-09", "acuteLoad": 77.0}}
	if load, _ := trackerAcuteLoad(noID, "2026-10-09", strp("100")); load != nil {
		t.Errorf("load = %v", load)
	}
}

func TestReadinessRejectsUndatedOrUnidentifiedTrackerRows(t *testing.T) {
	load, reason := trackerAcuteLoad([]any{map[string]any{"deviceId": 100.0, "acuteLoad": 77.0}}, "2026-10-09", strp("100"))
	if load != nil || reason != "readiness_device_or_date_mismatch" {
		t.Errorf("%v %q", load, reason)
	}
}

// Extra: the latest timestamp wins among several matching rows; a missing tracker id is its own reason.
func TestTrackerAcuteLoadLatestTimestampAndUnknownTracker(t *testing.T) {
	rows := []any{
		map[string]any{"deviceId": 1.0, "calendarDate": "d", "acuteLoad": 2.0, "timestampLocal": "2026-10-09T12:00:00"},
		map[string]any{"deviceId": 1.0, "calendarDate": "d", "acuteLoad": 1.0, "timestamp": "2026-10-09T08:00:00"},
	}
	if load, _ := trackerAcuteLoad(rows, "d", strp("1")); load != 2.0 {
		t.Errorf("load = %v", load)
	}
	if _, reason := trackerAcuteLoad(rows, "d", nil); reason != "tracker_id_unavailable" {
		t.Errorf("reason = %q", reason)
	}
}

func TestIsForbiddenErrorDetects403AndForbidden(t *testing.T) {
	for msg, want := range map[string]bool{
		"HTTP error: API Error 403 - HTTP 403 Forbidden": true,
		"Forbidden": true,
		"API Error": false,
		"timeout":   false,
	} {
		if got := isForbiddenError(errors.New(msg)); got != want {
			t.Errorf("%q = %v", msg, got)
		}
	}
}

// Extra (Python covers this only through the integration tests).
func TestIsFatalTrendError(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{&garmin.APIError{Status: 401}, true},
		{&garmin.APIError{Status: 429}, true},
		{&garmin.APIError{Status: 503}, true},
		{&garmin.APIError{Status: 400, Message: "HTTP error: API Error 401 - expired"}, false}, // the first status wins
		{&garmin.APIError{Status: 404}, false},
		{fmt.Errorf("x: %w", garmin.ErrSessionExpired), true},
		{&url.Error{Op: "Get", URL: "u", Err: errors.New("connection reset")}, true},
		{errors.New("range unavailable"), false},
	} {
		if got := isFatalTrendError(tc.err); got != tc.want {
			t.Errorf("%v = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestActivityIdMatchesIntAndStr(t *testing.T) {
	for _, tc := range []struct {
		item any
		want bool
	}{
		{map[string]any{"activityId": 123.0}, true},
		{map[string]any{"activityId": "123"}, true},
		{map[string]any{"activityId": 999.0}, false},
		{map[string]any{}, false},
	} {
		if got := activityIDMatches(tc.item, 123); got != tc.want {
			t.Errorf("%v = %v", tc.item, got)
		}
	}
}

func TestAsActivityListAcceptsListAndWrappedPayloads(t *testing.T) {
	item := map[string]any{"activityId": 1.0}
	want := []map[string]any{item}
	for name, payload := range map[string]any{
		"list": []any{item}, "activityList": map[string]any{"activityList": []any{item}}, "activities": map[string]any{"activities": []any{item}},
	} {
		if got := asActivityList(payload); !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v", name, got)
		}
	}
	for name, payload := range map[string]any{"nil": nil, "object": struct{}{}} {
		if got := asActivityList(payload); len(got) != 0 {
			t.Errorf("%s = %v", name, got)
		}
	}
}

func TestCurateTrainingEffectFromDetailsSummary(t *testing.T) {
	c := curateTrainingEffect(map[string]any{"summaryDTO": map[string]any{
		"trainingEffect": 3.5, "anaerobicTrainingEffect": 2.0, "trainingEffectLabel": "Highly Improving",
		"activityTrainingLoad": 150.0, "recoveryTime": 720.0, "performanceCondition": 95.0,
	}}, 123, "activity_details")
	eq(t, "curated", c, map[string]any{
		"activity_id": 123, "source": "activity_details", "training_effect": 3.5, "aerobic_effect": 3.5, "anaerobic_effect": 2.0,
		"training_effect_label": "Highly Improving", "recovery_time_hours": 12.0, "training_load": 150, "performance_condition": 95,
	})
}

func TestCurateTrainingEffectFromListItem(t *testing.T) {
	c := curateTrainingEffect(map[string]any{
		"activityId": 456.0, "aerobicTrainingEffect": 2.4, "anaerobicTrainingEffect": 0.3,
		"aerobicTrainingEffectMessage": "IMPROVING_AEROBIC_BASE_8", "activityTrainingLoad": 87.0,
	}, 456, "activity_list")
	eq(t, "curated", c, map[string]any{
		"activity_id": 456, "source": "activity_list", "training_effect": 2.4, "aerobic_effect": 2.4, "anaerobic_effect": 0.3,
		"training_effect_label": "IMPROVING_AEROBIC_BASE_8", "training_load": 87.0,
	})
}

func TestMinutesToHoursRejectsInvalidValues(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want any
	}{{60.0, 1.0}, {0.0, nil}, {nil, nil}, {true, nil}, {"60", nil}, {math.Inf(1), nil}, {-5.0, nil}, {100.0, 1.7}} {
		if got := minutesToHours(tc.in); got != tc.want {
			t.Errorf("%v = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// listClient is the fake of the activity search used by lookupActivityInList (Python's _FakeClient / ScanningClient).
type listClient struct {
	search func(q url.Values) any
	calls  []url.Values
}

func (c *listClient) SearchList(_ context.Context, q url.Values) (any, error) {
	c.calls = append(c.calls, q)
	return c.search(q), nil
}

func (c *listClient) Activities(context.Context, int, int) (any, error) { return nil, nil }

func TestLookupActivityInListPrefersActivityIdsFilter(t *testing.T) {
	item := map[string]any{"activityId": 99.0, "aerobicTrainingEffect": 3.1}
	c := &listClient{search: func(url.Values) any { return []any{item} }}
	if found := lookupActivityInList(context.Background(), c, 99, 100); !reflect.DeepEqual(found, item) {
		t.Errorf("found = %v", found)
	}
	if c.calls[0].Get("activityIds") != "99" || c.calls[0].Get("start") != "0" || c.calls[0].Get("limit") != "1" {
		t.Errorf("first query = %v", c.calls[0])
	}
}

func TestLookupActivityInListScansRecentPageWhenFilterMisses(t *testing.T) {
	wanted := map[string]any{"activityId": "77", "aerobicTrainingEffect": 1.2}
	c := &listClient{search: func(q url.Values) any {
		if q.Get("activityIds") != "" {
			return []any{}
		}
		return []any{wanted, map[string]any{"activityId": 1.0}}
	}}
	if found := lookupActivityInList(context.Background(), c, 77, 100); !reflect.DeepEqual(found, wanted) {
		t.Errorf("found = %v", found)
	}
	if c.calls[1].Get("limit") != "100" || c.calls[1].Get("start") != "0" {
		t.Errorf("second query = %v", c.calls[1])
	}
}

func TestLookupActivityInListReturnsNoneWhenMissing(t *testing.T) {
	c := &listClient{search: func(url.Values) any { return []any{map[string]any{"activityId": 1.0}} }}
	if found := lookupActivityInList(context.Background(), c, 99, 100); found != nil {
		t.Errorf("found = %v", found)
	}
}

func TestCurateTrainingEffectPreservesZeroSummaryValues(t *testing.T) {
	c := curateTrainingEffect(map[string]any{
		"summaryDTO":            map[string]any{"trainingEffect": 0.0, "anaerobicTrainingEffect": 0.0, "activityTrainingLoad": 0.0, "performanceCondition": 0.0},
		"aerobicTrainingEffect": 4.0, "anaerobicTrainingEffect": 3.0, "activityTrainingLoad": 200.0, "performanceCondition": 7.0,
	}, 123, "activity_details")
	for _, k := range []string{"aerobic_effect", "training_effect", "anaerobic_effect", "training_load", "performance_condition"} {
		if c[k] != 0.0 {
			t.Errorf("%s = %v, want 0", k, c[k])
		}
	}
}

func TestExtractVo2MeasurementsIgnoresMissingOrInvalidValues(t *testing.T) {
	for i, payload := range []any{
		nil, []any{}, []any{nil}, map[string]any{}, map[string]any{"generic": nil}, map[string]any{"vo2MaxValue": true},
		map[string]any{"vo2MaxValue": "48.0"}, map[string]any{"vo2MaxValue": math.NaN()}, map[string]any{"vo2MaxValue": math.Inf(1)},
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if got := extractVO2Measurements(payload); len(got) != 0 {
				t.Errorf("got %v", got)
			}
		})
	}
}

func TestExtractVo2MeasurementsReturnsBothSportsFromMixedPayload(t *testing.T) {
	got := extractVO2Measurements(map[string]any{"mostRecentVO2Max": map[string]any{
		"generic": map[string]any{"vo2MaxValue": 48.0}, "cycling": map[string]any{"vo2MaxValue": 55.0},
	}})
	if !reflect.DeepEqual(got, map[string]float64{"running": 48.0, "cycling": 55.0}) {
		t.Errorf("got %v", got)
	}
}

func TestExtractVo2MeasurementsMergesNestedListPayloads(t *testing.T) {
	got := extractVO2Measurements([]any{
		[]any{map[string]any{"generic": map[string]any{"vo2MaxValue": 48.0}}},
		map[string]any{"cycling": map[string]any{"vo2MaxPreciseValue": 55.12}},
	})
	if !reflect.DeepEqual(got, map[string]float64{"running": 48.0, "cycling": 55.12}) {
		t.Errorf("got %v", got)
	}
}

func TestExtractDatedVo2MeasurementsIndexesEachSportDate(t *testing.T) {
	got := extractDatedVO2Measurements([]any{
		map[string]any{
			"generic": map[string]any{"calendarDate": "2024-01-14", "vo2MaxValue": 48.0},
			"cycling": map[string]any{"calendarDate": "2024-01-14", "vo2MaxValue": 55.0},
		},
		[]any{map[string]any{"cycling": map[string]any{"calendarDate": "2024-01-15", "vo2MaxValue": 55.5}}},
	})
	want := map[string]map[string]float64{
		"2024-01-14": {"running": 48.0, "cycling": 55.0},
		"2024-01-15": {"cycling": 55.5},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestGetMaxMetricsRangeUsesClientEndpoint(t *testing.T) {
	var path string
	ep := metricsEndpoint{URL: "/metrics-service/metrics/maxmet/daily", Get: func(_ context.Context, p string) (any, error) {
		path = p
		return []any{map[string]any{"generic": map[string]any{"vo2MaxValue": 48.0}}}, nil
	}}
	supported, result, err := getMaxMetricsRange(context.Background(), ep, "2024-01-14", "2024-01-15")
	if err != nil || !supported {
		t.Fatalf("%v %v", supported, err)
	}
	eq(t, "result", result, []any{map[string]any{"generic": map[string]any{"vo2MaxValue": 48.0}}})
	if path != "/metrics-service/metrics/maxmet/daily/2024-01-14/2024-01-15" {
		t.Errorf("path = %s", path)
	}
}

func TestGetMaxMetricsRangeReportsUnsupportedClient(t *testing.T) {
	called := false
	ep := metricsEndpoint{Get: func(context.Context, string) (any, error) { called = true; return nil, nil }}
	supported, result, err := getMaxMetricsRange(context.Background(), ep, "2024-01-14", "2024-01-15")
	if supported || result != nil || err != nil || called {
		t.Errorf("%v %v %v called=%v", supported, result, err, called)
	}
}

func TestGetMaxMetricsRangeHandlesMissingConnectapi(t *testing.T) {
	ep := metricsEndpoint{URL: "/metrics-service/metrics/maxmet/daily"}
	if supported, result, err := getMaxMetricsRange(context.Background(), ep, "2024-01-14", "2024-01-15"); supported || result != nil || err != nil {
		t.Errorf("%v %v %v", supported, result, err)
	}
}

func TestExtractVo2MeasurementsPrefersPreciseValue(t *testing.T) {
	got := extractVO2Measurements(map[string]any{"generic": map[string]any{"vo2MaxValue": 48.0, "vo2MaxPreciseValue": 47.6}})
	if !reflect.DeepEqual(got, map[string]float64{"running": 47.6}) {
		t.Errorf("got %v", got)
	}
}

func date(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func TestBuildVo2TrendSeriesCarriesValuesForward(t *testing.T) {
	series, err := buildVO2TrendSeries([]vo2Point{
		{"2024-01-13", 47.6, "get_max_metrics"}, {"2024-01-15", 48.7, "get_training_status"},
	}, date(2024, 1, 16))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "series", series, []any{
		map[string]any{"date": "2024-01-13", "vo2_max": 47.6, "source": "get_max_metrics"},
		map[string]any{"date": "2024-01-14", "vo2_max": 47.6, "source": "get_max_metrics", "carried_forward": true},
		map[string]any{"date": "2024-01-15", "vo2_max": 48.7, "source": "get_training_status"},
		map[string]any{"date": "2024-01-16", "vo2_max": 48.7, "source": "get_training_status", "carried_forward": true},
	})
}

func TestBuildVo2TrendSeriesHandlesEmptyHistoryAndEarlyEnd(t *testing.T) {
	if s, err := buildVO2TrendSeries(nil, date(2024, 1, 16)); err != nil || len(s) != 0 {
		t.Errorf("%v %v", s, err)
	}
	if s, err := buildVO2TrendSeries([]vo2Point{{"2024-01-20", 48.0, "get_max_metrics"}}, date(2024, 1, 16)); err != nil || len(s) != 0 {
		t.Errorf("%v %v", s, err)
	}
}

func TestExtractDatedVo2MeasurementsPreservesRootDateForNestedSport(t *testing.T) {
	got := extractDatedVO2Measurements(map[string]any{
		"calendarDate": "2024-01-13", "mostRecentVO2Max": map[string]any{"generic": map[string]any{"vo2MaxValue": 48.0}},
	})
	if !reflect.DeepEqual(got, map[string]map[string]float64{"2024-01-13": {"running": 48.0}}) {
		t.Errorf("got %v", got)
	}
}

// Extra: the pure curation helpers of the progress and lactate tools.
func TestProgressAndLactateUnitConversions(t *testing.T) {
	if got, _ := convertProgressMetric("calories", 4190.0).(float64); math.Abs(got-1000.0) > 1e-9 {
		t.Errorf("calories = %v", got)
	}
	if got := convertProgressMetric("duration", 4190.0); got != 4190.0 {
		t.Errorf("duration = %v", got)
	}
	if got := convertProgressMetric("calories", nil); got != nil {
		t.Errorf("nil = %v", got)
	}
	if lactateSpeedMPS(0.0) != nil || lactateSpeedMPS(nil) != nil || math.Abs(lactateSpeedMPS(0.3888878).(float64)-3.888878) > 1e-9 {
		t.Error("lactate speed")
	}
}
