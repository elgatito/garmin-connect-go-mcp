package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

const (
	trProgressPath         = "/fitnessstats-service/activity"
	trHillPath             = "/metrics-service/metrics/hillscore/stats"
	trEndurancePath        = "/metrics-service/metrics/endurancescore/stats"
	trHRVPath              = "/hrv-service/hrv"
	trFitnessAgePath       = "/fitnessage-service/fitnessage"
	trTrainingStatusPath   = "/metrics-service/metrics/trainingstatus/aggregated"
	trDevicesPath          = "/device-service/deviceregistration/devices"
	trMaxMetricsPath       = "/metrics-service/metrics/maxmet/daily"
	trRunningTolerancePath = "/metrics-service/metrics/runningtolerance/stats"
	trActivityTypesPath    = "/activity-service/activity/activityTypes"
	trBiometricPath        = "/biometric-service/biometric"
	trBiometricStatsPath   = "/biometric-service/stats"
	trReloadPath           = "/wellness-service/wellness/epoch/request"
	trActivitiesSearch     = "/activitylist-service/activities/search/activities"
	trUserSettingsPath     = "/userprofile-service/userprofile/user-settings"
)

func trMap(t *testing.T, text string) map[string]any {
	t.Helper()
	var got map[string]any
	decodeJSON(t, text, &got)
	return got
}

func trSub(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	return mutMap(t, m[key])
}

func trList(t *testing.T, m map[string]any, key string) []any {
	t.Helper()
	return mutList(t, m[key])
}

func trAssertNum(t *testing.T, m map[string]any, key string, want float64) {
	t.Helper()
	got := num(t, m[key])
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("%s = %v, want %v", key, got, want)
	}
}

func trAssertApprox(t *testing.T, m map[string]any, key string, want, tolerance float64) {
	t.Helper()
	got := num(t, m[key])
	if math.Abs(got-want) > tolerance {
		t.Fatalf("%s = %v, want %v (tol %v)", key, got, want, tolerance)
	}
}

// --- unit-level training-effect helpers ---

func TestIsForbiddenErrorDetects403AndForbidden(t *testing.T) {
	if !isForbiddenError(errors.New("HTTP error: API Error 403 - HTTP 403 Forbidden")) {
		t.Fatal("403 text not detected")
	}
	if !isForbiddenError(errors.New("Forbidden")) {
		t.Fatal("Forbidden not detected")
	}
	if isForbiddenError(errors.New("API Error")) {
		t.Fatal("plain API Error treated as forbidden")
	}
	if isForbiddenError(errors.New("timeout")) {
		t.Fatal("timeout treated as forbidden")
	}
	if !isForbiddenError(&garmin.APIError{Status: 403}) {
		t.Fatal("APIError 403 not detected")
	}
}

func TestActivityIdMatchesIntAndStr(t *testing.T) {
	cases := []struct {
		item  map[string]any
		id    int64
		match bool
	}{
		{map[string]any{"activityId": 123}, 123, true},
		{map[string]any{"activityId": "123"}, 123, true},
		{map[string]any{"activityId": 999}, 123, false},
		{map[string]any{}, 123, false},
	}
	for _, c := range cases {
		if got := activityIDMatches(c.item, c.id); got != c.match {
			t.Fatalf("activityIDMatches(%v, %d) = %v", c.item, c.id, got)
		}
	}
}

func TestAsActivityListAcceptsListAndWrappedPayloads(t *testing.T) {
	item := map[string]any{"activityId": 1}
	cases := []struct {
		payload any
		want    []map[string]any
	}{
		{[]any{item}, []map[string]any{item}},
		{map[string]any{"activityList": []any{item}}, []map[string]any{item}},
		{map[string]any{"activities": []any{item}}, []map[string]any{item}},
		{nil, []map[string]any{}},
		{"not a map", []map[string]any{}},
	}
	for _, c := range cases {
		sameJSON(t, asActivityList(c.payload), c.want)
	}
}

func TestCurateTrainingEffectFromDetailsSummary(t *testing.T) {
	curated := curateTrainingEffect(map[string]any{
		"summaryDTO": map[string]any{
			"trainingEffect":          3.5,
			"anaerobicTrainingEffect": 2.0,
			"trainingEffectLabel":     "Highly Improving",
			"activityTrainingLoad":    150,
			"recoveryTime":            720,
			"performanceCondition":    95,
		},
	}, 123, "activity_details")

	sameJSON(t, curated, map[string]any{
		"activity_id":           123,
		"source":                "activity_details",
		"training_effect":       3.5,
		"aerobic_effect":        3.5,
		"anaerobic_effect":      2.0,
		"training_effect_label": "Highly Improving",
		"recovery_time_hours":   12.0,
		"training_load":         150,
		"performance_condition": 95,
	})
}

func TestCurateTrainingEffectFromListItem(t *testing.T) {
	curated := curateTrainingEffect(map[string]any{
		"activityId":                   456,
		"aerobicTrainingEffect":        2.4,
		"anaerobicTrainingEffect":      0.3,
		"aerobicTrainingEffectMessage": "IMPROVING_AEROBIC_BASE_8",
		"activityTrainingLoad":         87.0,
	}, 456, "activity_list")

	sameJSON(t, curated, map[string]any{
		"activity_id":           456,
		"source":                "activity_list",
		"training_effect":       2.4,
		"aerobic_effect":        2.4,
		"anaerobic_effect":      0.3,
		"training_effect_label": "IMPROVING_AEROBIC_BASE_8",
		"training_load":         87.0,
	})
}

func TestMinutesToHoursRejectsInvalidValues(t *testing.T) {
	if got := minutesToHours(60); got == nil || *got != 1.0 {
		t.Fatalf("minutesToHours(60) = %v, want 1.0", got)
	}
	for _, v := range []any{0, nil, true} {
		if got := minutesToHours(v); got != nil {
			t.Fatalf("minutesToHours(%v) = %v, want nil", v, *got)
		}
	}
}

func TestLookupActivityInListPrefersActivityIdsFilter(t *testing.T) {
	f := newFakeGarmin(t)
	item := map[string]any{"activityId": 99, "aerobicTrainingEffect": 3.1}
	f.on("GET "+trActivitiesSearch, []any{item})
	c := healthClient(t, f)

	found, err := lookupActivityInList(context.Background(), c, 99, 100)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	sameJSON(t, found, item)
	calls := f.callsMatching("GET", trActivitiesSearch)
	if len(calls) == 0 || calls[0].Query.Get("activityIds") != "99" {
		t.Fatalf("first call activityIds = %q, want 99", calls[0].Query.Get("activityIds"))
	}
}

func TestLookupActivityInListScansRecentPageWhenFilterMisses(t *testing.T) {
	f := newFakeGarmin(t)
	wanted := map[string]any{"activityId": "77", "aerobicTrainingEffect": 1.2}
	f.on("GET "+trActivitiesSearch, sequence{
		[]any{},
		[]any{wanted, map[string]any{"activityId": 1}},
	})
	c := healthClient(t, f)

	found, err := lookupActivityInList(context.Background(), c, 77, 100)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	sameJSON(t, found, wanted)
}

func TestLookupActivityInListReturnsNoneWhenMissing(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trActivitiesSearch, []any{map[string]any{"activityId": 1}})
	c := healthClient(t, f)

	found, err := lookupActivityInList(context.Background(), c, 99, 100)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if found != nil {
		t.Fatalf("found = %v, want nil", found)
	}
}

func TestCurateTrainingEffectPreservesZeroSummaryValues(t *testing.T) {
	curated := curateTrainingEffect(map[string]any{
		"summaryDTO": map[string]any{
			"trainingEffect":          0.0,
			"anaerobicTrainingEffect": 0.0,
			"activityTrainingLoad":    0,
			"performanceCondition":    0,
		},
		"aerobicTrainingEffect":   4.0,
		"anaerobicTrainingEffect": 3.0,
		"activityTrainingLoad":    200,
		"performanceCondition":    7,
	}, 123, "activity_details")

	trAssertNum(t, curated, "aerobic_effect", 0)
	trAssertNum(t, curated, "training_effect", 0)
	trAssertNum(t, curated, "anaerobic_effect", 0)
	trAssertNum(t, curated, "training_load", 0)
	trAssertNum(t, curated, "performance_condition", 0)
}

// --- unit-level VO2 helpers ---

func TestExtractVo2MeasurementsIgnoresMissingOrInvalidValues(t *testing.T) {
	payloads := map[string]any{
		"nil":          nil,
		"empty list":   []any{},
		"nil list":     []any{nil},
		"empty map":    map[string]any{},
		"null generic": map[string]any{"generic": nil},
		"bool":         map[string]any{"vo2MaxValue": true},
		"string":       map[string]any{"vo2MaxValue": "48.0"},
		"nan":          map[string]any{"vo2MaxValue": math.NaN()},
		"inf":          map[string]any{"vo2MaxValue": math.Inf(1)},
	}
	for name, payload := range payloads {
		t.Run(name, func(t *testing.T) {
			sameJSON(t, extractVO2Measurements(payload), map[string]float64{})
		})
	}
}

func TestExtractVo2MeasurementsReturnsBothSportsFromMixedPayload(t *testing.T) {
	payload := map[string]any{
		"mostRecentVO2Max": map[string]any{
			"generic": map[string]any{"vo2MaxValue": 48.0},
			"cycling": map[string]any{"vo2MaxValue": 55.0},
		},
	}
	sameJSON(t, extractVO2Measurements(payload), map[string]float64{
		"running": 48.0,
		"cycling": 55.0,
	})
}

func TestExtractVo2MeasurementsMergesNestedListPayloads(t *testing.T) {
	payload := []any{
		[]any{map[string]any{"generic": map[string]any{"vo2MaxValue": 48.0}}},
		map[string]any{"cycling": map[string]any{"vo2MaxPreciseValue": 55.12}},
	}
	sameJSON(t, extractVO2Measurements(payload), map[string]float64{
		"running": 48.0,
		"cycling": 55.12,
	})
}

func TestExtractDatedVo2MeasurementsIndexesEachSportDate(t *testing.T) {
	payload := []any{
		map[string]any{
			"generic": map[string]any{"calendarDate": "2024-01-14", "vo2MaxValue": 48.0},
			"cycling": map[string]any{"calendarDate": "2024-01-14", "vo2MaxValue": 55.0},
		},
		[]any{
			map[string]any{
				"cycling": map[string]any{"calendarDate": "2024-01-15", "vo2MaxValue": 55.5},
			},
		},
	}
	sameJSON(t, extractDatedVO2Measurements(payload), map[string]map[string]float64{
		"2024-01-14": {"running": 48.0, "cycling": 55.0},
		"2024-01-15": {"cycling": 55.5},
	})
}

func TestGetMaxMetricsRangeUsesClientEndpoint(t *testing.T) {
	f := newFakeGarmin(t)
	body := []any{map[string]any{"generic": map[string]any{"vo2MaxValue": 48.0}}}
	f.on("GET "+trMaxMetricsPath+"/2024-01-14/2024-01-15", body)
	c := healthClient(t, f)

	got, err := c.GetMaxMetricsRange(context.Background(), "2024-01-14", "2024-01-15")
	if err != nil {
		t.Fatalf("GetMaxMetricsRange: %v", err)
	}
	sameJSON(t, got, body)
	if calls := f.callsMatching("GET", trMaxMetricsPath+"/2024-01-14/2024-01-15"); len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
}

func TestExtractVo2MeasurementsPrefersPreciseValue(t *testing.T) {
	payload := map[string]any{
		"generic": map[string]any{"vo2MaxValue": 48.0, "vo2MaxPreciseValue": 47.6},
	}
	sameJSON(t, extractVO2Measurements(payload), map[string]float64{"running": 47.6})
}

func TestBuildVo2TrendSeriesCarriesValuesForward(t *testing.T) {
	history := []map[string]any{
		{"date": "2024-01-13", "vo2_max": 47.6, "source": "get_max_metrics"},
		{"date": "2024-01-15", "vo2_max": 48.7, "source": "get_training_status"},
	}
	series := buildVO2TrendSeries(history, time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC))
	sameJSON(t, series, []map[string]any{
		{"date": "2024-01-13", "vo2_max": 47.6, "source": "get_max_metrics"},
		{"date": "2024-01-14", "vo2_max": 47.6, "source": "get_max_metrics", "carried_forward": true},
		{"date": "2024-01-15", "vo2_max": 48.7, "source": "get_training_status"},
		{"date": "2024-01-16", "vo2_max": 48.7, "source": "get_training_status", "carried_forward": true},
	})
}

func TestBuildVo2TrendSeriesHandlesEmptyHistoryAndEarlyEnd(t *testing.T) {
	end := time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC)
	sameJSON(t, buildVO2TrendSeries(nil, end), []map[string]any{})
	history := []map[string]any{{"date": "2024-01-20", "vo2_max": 48.0, "source": "get_max_metrics"}}
	sameJSON(t, buildVO2TrendSeries(history, end), []map[string]any{})
}

func TestExtractDatedVo2MeasurementsPreservesRootDateForNestedSport(t *testing.T) {
	payload := map[string]any{
		"calendarDate": "2024-01-13",
		"mostRecentVO2Max": map[string]any{
			"generic": map[string]any{"vo2MaxValue": 48},
		},
	}
	sameJSON(t, extractDatedVO2Measurements(payload), map[string]map[string]float64{
		"2024-01-13": {"running": 48},
	})
}

// --- unit-level activity types ---

func TestActivityTypeMappingRetriesAfterTransientFailure(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trActivityTypesPath, sequence{
		status(500, map[string]any{"message": "temporary lookup failure"}),
		fixture(t, "MOCK_ACTIVITY_TYPES"),
	})
	c := healthClient(t, f)
	ctx := context.Background()

	if got := trainingActivityTypes(ctx, c); len(got) != 0 {
		t.Fatalf("first mapping = %v, want empty", got)
	}
	mapping := trainingActivityTypes(ctx, c)
	if mapping[3] != "hiking" {
		t.Fatalf("mapping[3] = %q, want hiking", mapping[3])
	}
	again := trainingActivityTypes(ctx, c)
	if again[3] != "hiking" {
		t.Fatalf("cached mapping[3] = %q, want hiking", again[3])
	}
	if calls := f.callsMatching("GET", trActivityTypesPath); len(calls) != 2 {
		t.Fatalf("activity type calls = %d, want 2", len(calls))
	}
}

// --- unit-level device selection ---

func TestDeviceLookupFailureOrMissingPrimaryKeepsIdentityUnknown(t *testing.T) {
	latest := map[string]any{
		"7": map[string]any{"trainingStatus": "PRODUCTIVE", "primaryTrainingDevice": true},
	}
	if _, _, _, isPrimary := selectTrainingDevice(latest, nil); isPrimary != nil {
		t.Fatalf("nil devices isPrimary = %v, want nil", *isPrimary)
	}
	if _, _, _, isPrimary := selectTrainingDevice(latest, []any{map[string]any{"deviceId": 7}}); isPrimary != nil {
		t.Fatalf("no primary device isPrimary = %v, want nil", *isPrimary)
	}
}

func TestZeroDeviceIdIsNotTreatedAsMissingAndMalformedRecordsAreIgnored(t *testing.T) {
	selected, reportingID, _, isPrimary := selectTrainingDevice(
		map[string]any{"0": map[string]any{"deviceId": 0, "primaryTrainingDevice": true}},
		[]any{nil, "bad", map[string]any{"deviceId": 0, "primaryDevice": true}},
	)
	if selected["deviceId"] != 0 {
		t.Fatalf("selected deviceId = %v", selected["deviceId"])
	}
	if reportingID == nil || *reportingID != "0" {
		t.Fatalf("reporting id = %v, want 0", reportingID)
	}
	if isPrimary == nil || !*isPrimary {
		t.Fatalf("isPrimary = %v, want true", isPrimary)
	}
}

func TestPrimaryTrainingDeviceIsDeterministicFallback(t *testing.T) {
	selected, reportingID, _, isPrimary := selectTrainingDevice(
		map[string]any{
			"200": map[string]any{"deviceId": 200},
			"100": map[string]any{"deviceId": 100, "primaryTrainingDevice": true},
		},
		[]any{map[string]any{"deviceId": 100, "displayName": "Primary"}, map[string]any{"deviceId": 200}},
	)
	if reportingID == nil || *reportingID != "100" {
		t.Fatalf("reporting id = %v, want 100", reportingID)
	}
	if selected["deviceId"] != 100 {
		t.Fatalf("selected deviceId = %v", selected["deviceId"])
	}
	if isPrimary != nil {
		t.Fatalf("isPrimary = %v, want nil", *isPrimary)
	}
}

func TestMissingPrimaryTrackerPrefersPrimaryTrainingMarker(t *testing.T) {
	_, reportingID, _, isPrimary := selectTrainingDevice(
		map[string]any{
			"200": map[string]any{"deviceId": 200},
			"300": map[string]any{"deviceId": 300, "primaryTrainingDevice": true},
		},
		[]any{
			map[string]any{"deviceId": 100, "primaryDevice": true},
			map[string]any{"deviceId": 200},
			map[string]any{"deviceId": 300},
		},
	)
	if reportingID == nil || *reportingID != "300" {
		t.Fatalf("reporting id = %v, want 300", reportingID)
	}
	if isPrimary == nil || *isPrimary {
		t.Fatalf("isPrimary = %v, want false", isPrimary)
	}
}

func TestReadinessRequiresMatchingTrackerAndDate(t *testing.T) {
	payload := []any{
		map[string]any{"deviceId": 200, "calendarDate": "2026-10-09", "acuteLoad": 999},
		map[string]any{"deviceId": 100, "calendarDate": "2026-10-08", "acuteLoad": 888},
		map[string]any{"deviceId": 100, "calendarDate": "2026-10-09", "acuteLoad": 123},
	}
	tracker := "100"
	load, reason := trackerAcuteLoad(payload, "2026-10-09", &tracker)
	if load == nil || *load != 123 || reason != nil {
		t.Fatalf("load = %v, reason = %v", load, reason)
	}
	if load, _ := trackerAcuteLoad(payload[:2], "2026-10-09", &tracker); load != nil {
		t.Fatalf("mismatched load = %v, want nil", *load)
	}
	undated := []any{map[string]any{"calendarDate": "2026-10-09", "acuteLoad": 77}}
	if load, _ := trackerAcuteLoad(undated, "2026-10-09", &tracker); load != nil {
		t.Fatalf("undated load = %v, want nil", *load)
	}
}

func TestReadinessRejectsUndatedOrUnidentifiedTrackerRows(t *testing.T) {
	tracker := "100"
	load, reason := trackerAcuteLoad(
		[]any{map[string]any{"deviceId": 100, "acuteLoad": 77}},
		"2026-10-09",
		&tracker,
	)
	if load != nil {
		t.Fatalf("load = %v, want nil", *load)
	}
	if reason == nil || *reason != "readiness_device_or_date_mismatch" {
		t.Fatalf("reason = %v", reason)
	}
}

// --- basic training tools ---

func TestGetProgressSummaryBetweenDatesTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trProgressPath, fixture(t, "MOCK_PROGRESS_SUMMARY"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_progress_summary_between_dates", map[string]any{
		"start_date": "2024-01-08",
		"end_date":   "2024-01-15",
		"metric":     "duration",
	})
	if isErr || text == "" {
		t.Fatalf("unexpected result: %q", text)
	}
	calls := f.callsMatching("GET", trProgressPath)
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	q := calls[0].Query
	want := map[string]string{
		"startDate":                 "2024-01-08",
		"endDate":                   "2024-01-15",
		"aggregation":               "lifetime",
		"groupByParentActivityType": "True",
		"metric":                    "duration",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Fatalf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
}

func TestGetProgressSummaryHandlesNullStats(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trProgressPath, []any{
		map[string]any{"date": "2024-01-15", "stats": nil},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_progress_summary_between_dates", map[string]any{
		"start_date": "2024-01-08",
		"end_date":   "2024-01-15",
		"metric":     "duration",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if strings.Contains(text, "NoneType") || strings.Contains(text, "Error") {
		t.Fatalf("unexpected failure text: %q", text)
	}
	data := trMap(t, text)
	sameJSON(t, data["stats_by_activity_type"], map[string]any{})
	trAssertNum(t, data, "count_of_activities", 0)
}

func TestGetProgressSummaryConvertsCaloriesFromGarminUnits(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trProgressPath, []any{
		map[string]any{
			"date":              "2024-06-01",
			"countOfActivities": 1,
			"stats": map[string]any{
				"running": map[string]any{
					"calories": map[string]any{
						"count": 1, "min": 3703.9776799999995, "max": 3703.9776799999995,
						"avg": 3703.9776799999995, "sum": 3703.9776799999995,
					},
				},
				"fitness_equipment": map[string]any{
					"calories": map[string]any{
						"count": 2, "min": 276.54132, "max": 1508.4071999999999,
						"avg": 892.47426, "sum": 1784.94852,
					},
				},
			},
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_progress_summary_between_dates", map[string]any{
		"start_date": "2024-01-08",
		"end_date":   "2024-01-15",
		"metric":     "calories",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	if _, ok := data["date"]; ok {
		t.Fatalf("date must be dropped: %v", data)
	}
	trAssertNum(t, data, "count_of_activities", 3)

	running := trSub(t, trSub(t, data, "stats_by_activity_type"), "running")
	for _, key := range []string{"sum", "min", "max", "avg"} {
		got := num(t, running[key])
		if math.Abs(got-884.0) > 0.01 {
			t.Fatalf("running %s = %v, want ~884", key, got)
		}
	}
	equipment := trSub(t, trSub(t, data, "stats_by_activity_type"), "fitness_equipment")
	trAssertApprox(t, equipment, "min", 66.0, 0.01)
	trAssertApprox(t, equipment, "max", 360.0, 0.01)
	trAssertApprox(t, equipment, "sum", 426.0, 0.01)
}

func TestGetProgressSummaryLeavesNonCalorieMetricsUnconverted(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trProgressPath, []any{
		map[string]any{
			"date":              "2024-06-01",
			"countOfActivities": 1,
			"stats": map[string]any{
				"running": map[string]any{
					"duration": map[string]any{
						"count": 1, "min": 4450.452, "max": 4450.452,
						"avg": 4450.452, "sum": 4450.452,
					},
				},
			},
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_progress_summary_between_dates", map[string]any{
		"start_date": "2024-01-08",
		"end_date":   "2024-01-15",
		"metric":     "duration",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	running := trSub(t, trSub(t, data, "stats_by_activity_type"), "running")
	trAssertNum(t, running, "sum", 4450.452)
	trAssertNum(t, data, "count_of_activities", 1)
}

func TestGetHillScoreTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trHillPath, map[string]any{
		"hillScore": 75,
		"dateRange": map[string]any{"start": "2024-01-08", "end": "2024-01-15"},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_hill_score", map[string]any{
		"start_date": "2024-01-08",
		"end_date":   "2024-01-15",
	})
	if isErr || text == "" {
		t.Fatalf("unexpected result: %q", text)
	}
	if calls := f.callsMatching("GET", trHillPath); len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
}

func TestHillScorePreservesMetricsWithNullDailyScores(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trHillPath, map[string]any{
		"hillScoreDTOList": nil, "periodAvgScore": nil, "maxScore": 74,
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_hill_score", map[string]any{
		"start_date": "2024-01-08",
		"end_date":   "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	trAssertNum(t, data, "max_score", 74)
	sameJSON(t, data["daily_scores"], []any{})
	if _, ok := data["latest_date"]; ok {
		t.Fatalf("latest_date must be absent: %v", data)
	}
}

func TestHillScoreSelectsLatestCalendarDate(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trHillPath, map[string]any{
		"hillScoreDTOList": []any{
			map[string]any{"calendarDate": "2024-01-08", "overallScore": 40},
			map[string]any{"calendarDate": "2024-01-15", "overallScore": 55, "strengthScore": 60},
			map[string]any{"calendarDate": "2024-01-10", "overallScore": 45},
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_hill_score", map[string]any{
		"start_date": "2024-01-08",
		"end_date":   "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	if data["latest_date"] != "2024-01-15" {
		t.Fatalf("latest_date = %v", data["latest_date"])
	}
	trAssertNum(t, data, "latest_overall_score", 55)
	trAssertNum(t, data, "latest_strength_score", 60)
	if got := len(trList(t, data, "daily_scores")); got != 3 {
		t.Fatalf("daily_scores = %d, want 3", got)
	}
}

func TestGetEnduranceScoreTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trEndurancePath, fixture(t, "MOCK_ENDURANCE_SCORE"))
	f.on("GET "+trActivityTypesPath, fixture(t, "MOCK_ACTIVITY_TYPES"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_endurance_score", map[string]any{
		"start_date": "2024-01-08",
		"end_date":   "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if calls := f.callsMatching("GET", trEndurancePath); len(calls) != 1 {
		t.Fatalf("endurance calls = %d, want 1", len(calls))
	}
	data := trMap(t, text)
	trAssertNum(t, data, "period_avg_score", 5631)
	trAssertNum(t, data, "period_max_score", 5740)
	trAssertNum(t, data, "current_score", 5712)
	if data["current_date"] != "2024-01-15" {
		t.Fatalf("current_date = %v", data["current_date"])
	}
	if data["classification"] != "intermediate" {
		t.Fatalf("classification = %v", data["classification"])
	}
	trAssertNum(t, data, "classification_id", 2)
	thresholds := trSub(t, data, "thresholds")
	trAssertNum(t, thresholds, "trained", 5800)
	trAssertNum(t, thresholds, "well_trained", 6500)

	contributors := trList(t, data, "contributors")
	if len(contributors) != 4 {
		t.Fatalf("contributors = %d, want 4", len(contributors))
	}
	var hiking, yoga map[string]any
	for _, item := range contributors {
		c := mutMap(t, item)
		switch c["activity_type"] {
		case "hiking":
			hiking = c
		case "yoga":
			yoga = c
		}
	}
	if hiking == nil {
		t.Fatalf("hiking contributor missing")
	}
	trAssertNum(t, hiking, "contribution_percent", 5.49)
	trAssertNum(t, hiking, "activity_type_id", 3)
	if yoga == nil {
		t.Fatalf("yoga contributor missing")
	}
	trAssertNum(t, yoga, "contribution_percent", 3.13)

	weekly := trList(t, data, "weekly_breakdown")
	if len(weekly) != 1 {
		t.Fatalf("weekly_breakdown = %d, want 1", len(weekly))
	}
	week := mutMap(t, weekly[0])
	if week["week_start"] != "2024-01-08" {
		t.Fatalf("week_start = %v", week["week_start"])
	}
	trAssertNum(t, week, "avg_score", 5548)
	trAssertNum(t, week, "max_score", 5561)
}

func TestGetEnduranceScoreHandlesNullScoreDto(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trEndurancePath, map[string]any{
		"userProfilePK":     1,
		"startDate":         "2022-09-01",
		"endDate":           "2022-09-01",
		"avg":               nil,
		"max":               nil,
		"groupMap":          map[string]any{"2022-08-29": nil},
		"enduranceScoreDTO": nil,
	})
	f.on("GET "+trActivityTypesPath, fixture(t, "MOCK_ACTIVITY_TYPES"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_endurance_score", map[string]any{
		"start_date": "2022-09-01",
		"end_date":   "2022-09-01",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	if _, ok := data["current_score"]; ok {
		t.Fatalf("current_score must be absent: %v", data)
	}
	if data["start_date"] != "2022-09-01" {
		t.Fatalf("start_date = %v", data["start_date"])
	}
	sameJSON(t, data["weekly_breakdown"], []any{map[string]any{"week_start": "2022-08-29"}})
}

func TestGetTrainingEffectTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /activity-service/activity/12345678901", map[string]any{
		"summaryDTO": map[string]any{
			"trainingEffect":          3.5,
			"anaerobicTrainingEffect": 2.0,
			"trainingEffectLabel":     "Highly Improving",
			"activityTrainingLoad":    150,
			"recoveryTime":            720,
			"performanceCondition":    95,
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_training_effect", map[string]any{"activity_id": 12345678901})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	trAssertNum(t, data, "training_effect", 3.5)
	trAssertNum(t, data, "aerobic_effect", 3.5)
	trAssertNum(t, data, "anaerobic_effect", 2.0)
	trAssertNum(t, data, "recovery_time_hours", 12.0)
	if data["source"] != "activity_details" {
		t.Fatalf("source = %v", data["source"])
	}
}

func TestGetTrainingEffectException(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /activity-service/activity/12345678901", status(500, map[string]any{"message": "API Error"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_training_effect", map[string]any{"activity_id": 12345678901})
	if !strings.Contains(text, "Error retrieving training effect data") {
		t.Fatalf("text = %q", text)
	}
}

func TestGetTrainingEffectFallsBackToActivityListOn403(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /activity-service/activity/12345678901", status(403, map[string]any{"message": "HTTP 403 Forbidden"}))
	f.on("GET "+trActivitiesSearch, []any{
		map[string]any{
			"activityId":                   12345678901,
			"aerobicTrainingEffect":        3.2,
			"anaerobicTrainingEffect":      0.4,
			"aerobicTrainingEffectMessage": "IMPROVING_AEROBIC_BASE_8",
			"activityTrainingLoad":         140,
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_training_effect", map[string]any{"activity_id": 12345678901})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	if data["source"] != "activity_list" {
		t.Fatalf("source = %v", data["source"])
	}
	trAssertNum(t, data, "training_effect", 3.2)
	trAssertNum(t, data, "anaerobic_effect", 0.4)
	trAssertNum(t, data, "training_load", 140)
}

func TestGetTrainingEffect403WithoutListMatch(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /activity-service/activity/12345678901", status(403, map[string]any{"message": "HTTP 403 Forbidden"}))
	f.on("GET "+trActivitiesSearch, []any{})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_training_effect", map[string]any{"activity_id": 12345678901})
	if !strings.Contains(text, "HTTP 403") || !strings.Contains(text, "12345678901") {
		t.Fatalf("text = %q", text)
	}
}

func TestGetHrvDataTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trHRVPath+"/2024-01-15", fixture(t, "MOCK_HRV_DATA"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_hrv_data", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if calls := f.callsMatching("GET", trHRVPath+"/2024-01-15"); len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	data := trMap(t, text)
	trAssertNum(t, data, "last_night_avg_hrv_ms", 48)
	if data["status"] != "BALANCED" {
		t.Fatalf("status = %v", data["status"])
	}
	if _, ok := data["hrv_readings"]; ok {
		t.Fatalf("hrv_readings included without return_timeseries")
	}
}

func TestGetHrvDataNoData(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trHRVPath+"/2024-01-15", nil)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_hrv_data", map[string]any{"date": "2024-01-15"})
	if text != "No HRV data found for 2024-01-15." {
		t.Fatalf("text = %q", text)
	}
}

func TestGetHrvDataHandlesNullSummary(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trHRVPath+"/2024-01-15", map[string]any{
		"hrvSummary":               nil,
		"sleepStartTimestampLocal": nil,
	})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_hrv_data", map[string]any{"date": "2024-01-15"})
	if strings.Contains(text, "NoneType") || strings.Contains(text, "Error") {
		t.Fatalf("text = %q", text)
	}
}

func TestHrvTimeseriesPreservesSummaryWithNullReadings(t *testing.T) {
	cases := map[string]any{
		"nil summary":   nil,
		"valid summary": map[string]any{"lastNightAvg": 54},
	}
	for name, summary := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on("GET "+trHRVPath+"/2024-01-15", map[string]any{
				"hrvSummary":  summary,
				"hrvReadings": nil,
			})
			cs := newSession(t, f, nil)

			text, isErr := callTool(t, cs, "get_hrv_data", map[string]any{
				"date": "2024-01-15", "return_timeseries": true,
			})
			if isErr {
				t.Fatalf("unexpected error: %q", text)
			}
			data := trMap(t, text)
			if data["date"] != "2024-01-15" {
				t.Fatalf("date = %v", data["date"])
			}
			sameJSON(t, data["hrv_readings"], []any{})
			trAssertNum(t, data, "readings_count", 0)
			if summary != nil {
				trAssertNum(t, data, "last_night_avg_hrv_ms", 54)
			}
		})
	}
}

func TestGetFitnessageDataTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trFitnessAgePath+"/2024-01-15", map[string]any{
		"fitnessAge":       25,
		"chronologicalAge": 30,
		"vo2Max":           52.5,
		"date":             "2024-01-15",
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_fitnessage_data", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	trAssertNum(t, data, "fitness_age_years", 25)
	trAssertNum(t, data, "chronological_age_years", 30)
	trAssertNum(t, data, "age_difference_years", 5)
}

func TestGetCyclingFtpTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trBiometricPath+"/latestFunctionalThresholdPower/CYCLING", fixture(t, "MOCK_CYCLING_FTP"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_cycling_ftp", nil)
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	if data["sport"] != "CYCLING" {
		t.Fatalf("sport = %v", data["sport"])
	}
	trAssertNum(t, data, "functional_threshold_power_watts", 294)
	if data["calendar_date"] != "2024-03-15T10:30:00.000" {
		t.Fatalf("calendar_date = %v", data["calendar_date"])
	}
	if data["is_stale"] != false {
		t.Fatalf("is_stale = %v", data["is_stale"])
	}
	if data["biometric_source_type"] != "CHANGE_LOG" {
		t.Fatalf("biometric_source_type = %v", data["biometric_source_type"])
	}
}

func TestCyclingFtpAcceptsListPayload(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trBiometricPath+"/latestFunctionalThresholdPower/CYCLING", []any{fixture(t, "MOCK_CYCLING_FTP")})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_cycling_ftp", nil)
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	trAssertNum(t, data, "functional_threshold_power_watts", 294)
	if data["sport"] != "CYCLING" {
		t.Fatalf("sport = %v", data["sport"])
	}
}

func TestRequestReloadTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+trReloadPath+"/2024-01-15", map[string]any{
		"status": "success", "message": "Data reload requested",
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "request_reload", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if calls := f.callsMatching("POST", trReloadPath+"/2024-01-15"); len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
}

// --- get_training_status ---

func TestGetTrainingStatusTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trTrainingStatusPath+"/2024-01-15", fixture(t, "MOCK_TRAINING_STATUS"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_training_status", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if calls := f.callsMatching("GET", trTrainingStatusPath+"/2024-01-15"); len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	data := trMap(t, text)
	trAssertNum(t, data, "acute_load", 250)
	trAssertNum(t, data, "chronic_load", 220)
}

func TestGetTrainingStatusUsesTrackerReadinessWhenReportingDeviceIsNotPrimary(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trDevicesPath, []any{
		map[string]any{"deviceId": 100, "displayName": "Forerunner", "primaryDevice": true},
		map[string]any{"deviceId": 200, "displayName": "Edge", "primaryDevice": false},
	})
	f.on("GET "+trTrainingStatusPath+"/2026-10-09", map[string]any{
		"mostRecentTrainingStatus": map[string]any{
			"latestTrainingStatusData": map[string]any{
				"200": map[string]any{
					"trainingStatus": "DETRAINING",
					"acuteTrainingLoadDTO": map[string]any{
						"dailyTrainingLoadAcute":         0,
						"dailyTrainingLoadChronic":       80,
						"dailyAcuteChronicWorkloadRatio": 0,
					},
				},
			},
		},
	})
	f.on("GET "+"/metrics-service/metrics/trainingreadiness/2026-10-09", []any{
		map[string]any{"deviceId": 100, "calendarDate": "2026-10-09", "acuteLoad": 123},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_training_status", map[string]any{"date": "2026-10-09"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	trAssertNum(t, data, "reporting_device_id", 200)
	if data["reporting_device_name"] != "Edge" {
		t.Fatalf("reporting_device_name = %v", data["reporting_device_name"])
	}
	trAssertNum(t, data, "acute_load", 123)
	if data["acute_load_source"] != "training_readiness" {
		t.Fatalf("acute_load_source = %v", data["acute_load_source"])
	}
	for _, key := range []string{"chronic_load", "load_ratio"} {
		if _, ok := data[key]; ok {
			t.Fatalf("%s must be absent: %v", key, data)
		}
	}
	if calls := f.callsMatching("GET", trDevicesPath); len(calls) != 1 {
		t.Fatalf("device calls = %d, want 1", len(calls))
	}
	readinessPath := "/metrics-service/metrics/trainingreadiness/2026-10-09"
	if calls := f.callsMatching("GET", readinessPath); len(calls) != 1 {
		t.Fatalf("readiness calls = %d, want 1", len(calls))
	}
}

func TestGetTrainingStatusPreservesStatusWhenReadinessFails(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trDevicesPath, []any{
		map[string]any{"deviceId": 100, "displayName": "Forerunner", "primaryDevice": true},
		map[string]any{"deviceId": 200, "displayName": "Edge", "primaryDevice": false},
	})
	f.on("GET "+trTrainingStatusPath+"/2026-10-09", map[string]any{
		"mostRecentTrainingStatus": map[string]any{
			"latestTrainingStatusData": map[string]any{
				"200": map[string]any{"trainingStatus": "DETRAINING"},
			},
		},
	})
	f.on("GET "+"/metrics-service/metrics/trainingreadiness/2026-10-09", status(500, map[string]any{"message": "timeout"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_training_status", map[string]any{"date": "2026-10-09"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	if data["training_status"] != "DETRAINING" {
		t.Fatalf("training_status = %v", data["training_status"])
	}
	if _, ok := data["acute_load"]; ok {
		t.Fatalf("acute_load must be absent: %v", data)
	}
	if data["acute_load_source"] != "unavailable" {
		t.Fatalf("acute_load_source = %v", data["acute_load_source"])
	}
	if data["acute_load_unavailable_reason"] != "readiness_unavailable" {
		t.Fatalf("reason = %v", data["acute_load_unavailable_reason"])
	}
}

func TestGetTrainingStatusRejectsWrongReadinessDeviceAndDate(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trDevicesPath, []any{
		map[string]any{"deviceId": 100, "displayName": "Forerunner", "primaryDevice": true},
		map[string]any{"deviceId": 200, "displayName": "Edge", "primaryDevice": false},
	})
	f.on("GET "+trTrainingStatusPath+"/2026-10-09", map[string]any{
		"mostRecentTrainingStatus": map[string]any{
			"latestTrainingStatusData": map[string]any{
				"200": map[string]any{"trainingStatus": "DETRAINING"},
			},
		},
	})
	f.on("GET "+"/metrics-service/metrics/trainingreadiness/2026-10-09", []any{
		map[string]any{"deviceId": 200, "calendarDate": "2026-10-09", "acuteLoad": 999},
		map[string]any{"deviceId": 100, "calendarDate": "2026-10-08", "acuteLoad": 888},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_training_status", map[string]any{"date": "2026-10-09"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	if data["training_status"] != "DETRAINING" {
		t.Fatalf("training_status = %v", data["training_status"])
	}
	if _, ok := data["acute_load"]; ok {
		t.Fatalf("acute_load must be absent: %v", data)
	}
	if data["acute_load_unavailable_reason"] != "readiness_device_or_date_mismatch" {
		t.Fatalf("reason = %v", data["acute_load_unavailable_reason"])
	}
}

func TestGetTrainingStatusDoesNotCallReadinessWhenDeviceLookupFails(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trDevicesPath, status(500, map[string]any{"message": "device endpoint unavailable"}))
	f.on("GET "+trTrainingStatusPath+"/2026-10-09", map[string]any{
		"mostRecentTrainingStatus": map[string]any{
			"latestTrainingStatusData": map[string]any{
				"200": map[string]any{
					"trainingStatus": "PRODUCTIVE",
					"acuteTrainingLoadDTO": map[string]any{
						"dailyTrainingLoadAcute":   0,
						"dailyTrainingLoadChronic": 80,
					},
				},
			},
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_training_status", map[string]any{"date": "2026-10-09"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	if data["training_status"] != "PRODUCTIVE" {
		t.Fatalf("training_status = %v", data["training_status"])
	}
	trAssertNum(t, data, "acute_load", 0)
	trAssertNum(t, data, "chronic_load", 80)
	if calls := f.callsMatching("GET", "/metrics-service/metrics/trainingreadiness/2026-10-09"); len(calls) != 0 {
		t.Fatalf("readiness called %d times", len(calls))
	}
}

func TestGetTrainingStatusSkipsNullDeviceEntries(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trTrainingStatusPath+"/2026-08-24", map[string]any{
		"mostRecentTrainingStatus": map[string]any{
			"latestTrainingStatusData": map[string]any{
				"device-null": nil,
				"device-valid": map[string]any{
					"calendarDate":   "2026-08-24",
					"trainingStatus": "PRODUCTIVE",
					"acuteTrainingLoadDTO": map[string]any{
						"dailyTrainingLoadAcute": 250,
					},
				},
			},
		},
		"mostRecentVO2Max": map[string]any{
			"generic": map[string]any{"vo2MaxValue": 52.5},
			"cycling": nil,
		},
		"mostRecentTrainingLoadBalance": map[string]any{
			"metricsTrainingLoadBalanceDTOMap": map[string]any{
				"device-null":  nil,
				"device-valid": map[string]any{"monthlyLoadAerobicLow": 100},
			},
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_training_status", map[string]any{"date": "2026-08-24"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	if data["date"] != "2026-08-24" {
		t.Fatalf("date = %v", data["date"])
	}
	if data["training_status"] != "PRODUCTIVE" {
		t.Fatalf("training_status = %v", data["training_status"])
	}
	trAssertNum(t, data, "acute_load", 250)
	trAssertNum(t, data, "vo2_max", 52.5)
	trAssertNum(t, data, "monthly_load_aerobic_low", 100)
}

func TestGetTrainingStatusSkipsEmptyDeviceEntries(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trTrainingStatusPath+"/2026-08-24", map[string]any{
		"mostRecentTrainingStatus": map[string]any{
			"latestTrainingStatusData": map[string]any{
				"device-empty": map[string]any{},
				"device-valid": map[string]any{
					"calendarDate":   "2026-08-24",
					"trainingStatus": "PRODUCTIVE",
					"acuteTrainingLoadDTO": map[string]any{
						"dailyTrainingLoadAcute": 250,
					},
				},
			},
		},
		"mostRecentTrainingLoadBalance": map[string]any{
			"metricsTrainingLoadBalanceDTOMap": map[string]any{
				"device-empty": map[string]any{},
				"device-valid": map[string]any{"monthlyLoadAerobicLow": 100},
			},
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_training_status", map[string]any{"date": "2026-08-24"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	if data["training_status"] != "PRODUCTIVE" {
		t.Fatalf("training_status = %v", data["training_status"])
	}
	trAssertNum(t, data, "acute_load", 250)
	trAssertNum(t, data, "monthly_load_aerobic_low", 100)
}

func TestGetTrainingStatusToleratesAllNullDeviceMaps(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trTrainingStatusPath+"/2026-08-24", map[string]any{
		"mostRecentTrainingStatus": map[string]any{
			"latestTrainingStatusData": map[string]any{"device-null": nil},
		},
		"mostRecentVO2Max": map[string]any{
			"generic": nil,
			"cycling": nil,
		},
		"mostRecentTrainingLoadBalance": map[string]any{
			"metricsTrainingLoadBalanceDTOMap": map[string]any{"device-null": nil},
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_training_status", map[string]any{"date": "2026-08-24"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	sameJSON(t, trMap(t, text), map[string]any{"date": "2026-08-24"})
}

func TestGetTrainingStatusToleratesNonMappingResponse(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trTrainingStatusPath+"/2026-08-24", []any{nil})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_training_status", map[string]any{"date": "2026-08-24"})
	if text != "No training status data found for 2026-08-24." {
		t.Fatalf("text = %q", text)
	}
}

func TestGetTrainingStatusIncludesCyclingVo2Max(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trTrainingStatusPath+"/2024-01-15", fixture(t, "MOCK_TRAINING_STATUS"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_training_status", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	trAssertNum(t, data, "cycling_vo2_max", 55.0)
	trAssertNum(t, data, "cycling_vo2_max_precise", 55.12)
}

func TestGetTrainingStatusNoCyclingVo2WhenAbsent(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trTrainingStatusPath+"/2024-01-15", map[string]any{
		"mostRecentVO2Max": map[string]any{
			"generic": map[string]any{"vo2MaxValue": 52.5, "vo2MaxPreciseValue": 52.47},
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_training_status", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	for _, key := range []string{"cycling_vo2_max", "cycling_vo2_max_precise"} {
		if _, ok := data[key]; ok {
			t.Fatalf("%s must be absent: %v", key, data)
		}
	}
}

func TestGetTrainingStatusHandlesNullCyclingVo2(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trTrainingStatusPath+"/2024-01-15", map[string]any{
		"mostRecentVO2Max": map[string]any{
			"generic": map[string]any{"vo2MaxValue": 52.5, "vo2MaxPreciseValue": 52.47},
			"cycling": nil,
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_training_status", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	trAssertNum(t, data, "vo2_max", 52.5)
	if _, ok := data["cycling_vo2_max"]; ok {
		t.Fatalf("cycling_vo2_max must be absent: %v", data)
	}
}

func TestTrainingStatusRetainsMonthlyLoadWithoutStatusSnapshot(t *testing.T) {
	cases := map[string]any{
		"empty map": map[string]any{},
		"null map":  nil,
	}
	for name, statusMap := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on("GET "+trDevicesPath, []any{
				map[string]any{"deviceId": 100, "primaryDevice": true},
				map[string]any{"deviceId": 200},
			})
			f.on("GET "+trTrainingStatusPath+"/2024-01-15", map[string]any{
				"mostRecentTrainingStatus": map[string]any{
					"latestTrainingStatusData": statusMap,
				},
				"mostRecentTrainingLoadBalance": map[string]any{
					"metricsTrainingLoadBalanceDTOMap": map[string]any{
						"200": map[string]any{"deviceId": 200, "monthlyLoadAerobicLow": 999},
						"100": map[string]any{
							"deviceId":                      100,
							"monthlyLoadAerobicLow":         400,
							"monthlyLoadAerobicHigh":        700,
							"monthlyLoadAnaerobic":          250,
							"trainingBalanceFeedbackPhrase": "BALANCED",
						},
					},
				},
			})
			cs := newSession(t, f, nil)

			text, isErr := callTool(t, cs, "get_training_status", map[string]any{"date": "2024-01-15"})
			if isErr {
				t.Fatalf("unexpected error: %q", text)
			}
			data := trMap(t, text)
			trAssertNum(t, data, "load_device_id", 100)
			trAssertNum(t, data, "monthly_load_aerobic_low", 400)
			trAssertNum(t, data, "monthly_load_aerobic_high", 700)
			trAssertNum(t, data, "monthly_load_anaerobic", 250)
			if data["training_balance_feedback"] != "BALANCED" {
				t.Fatalf("feedback = %v", data["training_balance_feedback"])
			}
			for _, key := range []string{"reporting_device_id", "load_unavailable_reason"} {
				if _, ok := data[key]; ok {
					t.Fatalf("%s must be absent: %v", key, data)
				}
			}
		})
	}
}

// --- get_training_load_balance ---

func trLoadBalanceRecord(deviceID, aerobicLow float64, feedback string, extra map[string]any) map[string]any {
	record := map[string]any{
		"deviceId":                        deviceID,
		"calendarDate":                    "2026-10-09",
		"monthlyLoadAerobicLow":           aerobicLow,
		"monthlyLoadAerobicLowTargetMin":  200,
		"monthlyLoadAerobicLowTargetMax":  600,
		"monthlyLoadAerobicHigh":          700,
		"monthlyLoadAerobicHighTargetMin": 300,
		"monthlyLoadAerobicHighTargetMax": 650,
		"monthlyLoadAnaerobic":            250,
		"monthlyLoadAnaerobicTargetMin":   100,
		"monthlyLoadAnaerobicTargetMax":   300,
		"trainingBalanceFeedbackPhrase":   feedback,
	}
	for k, v := range extra {
		record[k] = v
	}
	return record
}

func TestGetTrainingLoadBalancePrefersPrimaryTrackerOverFirstRecord(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trDevicesPath, []any{
		map[string]any{"deviceId": 100, "displayName": "Forerunner", "primaryDevice": true},
		map[string]any{"deviceId": 200, "displayName": "Edge", "primaryDevice": false},
	})
	f.on("GET "+trTrainingStatusPath+"/2026-10-09", map[string]any{
		"mostRecentTrainingLoadBalance": map[string]any{
			"metricsTrainingLoadBalanceDTOMap": map[string]any{
				"200": trLoadBalanceRecord(200, 50, "AEROBIC_LOW_SHORTAGE", nil),
				"100": trLoadBalanceRecord(100, 400, "BALANCED", nil),
			},
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_training_load_balance", map[string]any{"date": "2026-10-09"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	trAssertNum(t, data, "load_device_id", 100)
	if data["load_device_name"] != "Forerunner" {
		t.Fatalf("load_device_name = %v", data["load_device_name"])
	}
	if data["feedback"] != "BALANCED" {
		t.Fatalf("feedback = %v", data["feedback"])
	}
	sameJSON(t, data["aerobic_low"], map[string]any{
		"load": 400, "target_min": 200, "target_max": 600, "status": "within",
	})
	if trSub(t, data, "aerobic_high")["status"] != "above" {
		t.Fatalf("aerobic_high = %v", data["aerobic_high"])
	}
	if trSub(t, data, "anaerobic")["status"] != "within" {
		t.Fatalf("anaerobic = %v", data["anaerobic"])
	}
}

func TestGetTrainingLoadBalanceFallsBackToPrimaryTrainingMarker(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trDevicesPath, status(500, map[string]any{"message": "devices unavailable"}))
	f.on("GET "+trTrainingStatusPath+"/2026-10-09", map[string]any{
		"mostRecentTrainingLoadBalance": map[string]any{
			"metricsTrainingLoadBalanceDTOMap": map[string]any{
				"200": trLoadBalanceRecord(200, 50, "AEROBIC_LOW_SHORTAGE", nil),
				"100": trLoadBalanceRecord(100, 400, "BALANCED", map[string]any{"primaryTrainingDevice": true}),
			},
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_training_load_balance", map[string]any{"date": "2026-10-09"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	trAssertNum(t, data, "load_device_id", 100)
	if _, ok := data["load_device_name"]; ok {
		t.Fatalf("load_device_name must be absent: %v", data)
	}
	if data["feedback"] != "BALANCED" {
		t.Fatalf("feedback = %v", data["feedback"])
	}
}

func TestGetTrainingLoadBalanceNoData(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trTrainingStatusPath+"/2026-10-09", map[string]any{
		"mostRecentTrainingLoadBalance": map[string]any{"metricsTrainingLoadBalanceDTOMap": nil},
	})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_training_load_balance", map[string]any{"date": "2026-10-09"})
	if text != "No training load balance data found for 2026-10-09." {
		t.Fatalf("text = %q", text)
	}
}

// --- get_lactate_threshold ---

func TestGetLactateThresholdToolLatest(t *testing.T) {
	f := newFakeGarmin(t)
	today := time.Now().Format(dateLayout)
	f.on("GET "+trBiometricPath+"/powerToWeight/latest/"+today, map[string]any{
		"functionalThresholdPower": 334,
		"weight":                   73.0,
		"powerToWeight":            4.575,
		"sport":                    "RUNNING",
	})
	f.on("GET "+trBiometricPath+"/latestLactateThreshold", []any{
		map[string]any{
			"userProfilePK": 12345678, "version": 1, "calendarDate": "2024-01-15",
			"sequence": 1, "speed": 0.3888878, "heartRate": 169,
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_lactate_threshold", nil)
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	got := num(t, data["lactate_threshold_speed_mps"])
	if math.Abs(got-3.888878) > 1e-6 {
		t.Fatalf("speed = %v", got)
	}
	if roundTo(1000/got, 0) != 4*60+17 {
		t.Fatalf("pace = %v", 1000/got)
	}
	trAssertNum(t, data, "lactate_threshold_heart_rate_bpm", 169)
	trAssertNum(t, data, "functional_threshold_power_watts", 334)
	if data["sport"] != "RUNNING" {
		t.Fatalf("sport = %v", data["sport"])
	}
	trAssertNum(t, data, "power_to_weight", 4.575)
}

func TestGetLactateThresholdToolRange(t *testing.T) {
	f := newFakeGarmin(t)
	for _, kind := range []string{"lactateThresholdSpeed", "lactateThresholdHeartRate", "functionalThresholdPower"} {
		var body any
		switch kind {
		case "lactateThresholdSpeed":
			body = fixture(t, "MOCK_LACTATE_THRESHOLD_RANGE").(map[string]any)["speed"]
		case "lactateThresholdHeartRate":
			body = fixture(t, "MOCK_LACTATE_THRESHOLD_RANGE").(map[string]any)["heart_rate"]
		default:
			body = fixture(t, "MOCK_LACTATE_THRESHOLD_RANGE").(map[string]any)["power"]
		}
		f.on("GET "+trBiometricStatsPath+"/"+kind+"/range/2024-01-08/2024-01-15", body)
	}
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_lactate_threshold", map[string]any{
		"start_date": "2024-01-08", "end_date": "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	if data["start_date"] != "2024-01-08" || data["end_date"] != "2024-01-15" {
		t.Fatalf("range = %v..%v", data["start_date"], data["end_date"])
	}
	speeds := trList(t, data, "speed_history")
	if len(speeds) != 3 {
		t.Fatalf("speed history = %d, want 3", len(speeds))
	}
	first := mutMap(t, speeds[0])
	if first["date"] != "2024-01-08" {
		t.Fatalf("first date = %v", first["date"])
	}
	got := num(t, first["speed_mps"])
	if math.Abs(got-3.9166557) > 1e-6 {
		t.Fatalf("first speed = %v", got)
	}
	if roundTo(1000/got, 0) != 4*60+15 {
		t.Fatalf("first pace = %v", 1000/got)
	}
	hrs := trList(t, data, "heart_rate_history")
	if len(hrs) != 3 {
		t.Fatalf("heart rate history = %d, want 3", len(hrs))
	}
	sameJSON(t, hrs[len(hrs)-1], map[string]any{
		"date": "2024-01-15", "heart_rate_bpm": 169, "series": "running",
	})
	if _, ok := data["power_history"]; !ok {
		t.Fatalf("power_history missing")
	}
}

func TestGetLactateThresholdRejectsPartialRange(t *testing.T) {
	cases := map[string]map[string]any{
		"start only": {"start_date": "2024-01-08"},
		"end only":   {"end_date": "2024-01-15"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeGarmin(t)
			cs := newSession(t, f, nil)
			text, _ := callTool(t, cs, "get_lactate_threshold", args)
			if !strings.Contains(text, "Provide both start_date and end_date") {
				t.Fatalf("text = %q", text)
			}
			if calls := f.callsMatching("GET", trBiometricPath+"/latestLactateThreshold"); len(calls) != 0 {
				t.Fatalf("lactate threshold called")
			}
		})
	}
}

// --- get_vo2max_trend ---

func trStatusRoutes(f *fakeGarmin, start string, days int, body any) {
	day, _ := time.Parse(dateLayout, start)
	for i := 0; i < days; i++ {
		f.on("GET "+trTrainingStatusPath+"/"+day.Format(dateLayout), body)
		day = day.AddDate(0, 0, 1)
	}
}

func trCountPath(f *fakeGarmin, method, path string) int {
	return len(f.callsMatching(method, path))
}

func TestGetVo2maxTrendFallsBackToProfile(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trMaxMetricsPath+"/2024-01-14/2024-01-15", []any{})
	trStatusRoutes(f, "2024-01-14", 2, map[string]any{})
	f.on("GET "+trUserSettingsPath, map[string]any{
		"userData": map[string]any{"vo2MaxRunning": 28.0},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_vo2max_trend", map[string]any{
		"start_date": "2024-01-14", "end_date": "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	trAssertNum(t, data, "data_points", 0)
	if data["first_vo2_max"] != nil || data["latest_vo2_max"] != nil || data["change"] != nil {
		t.Fatalf("expected null trend values: %v", data)
	}
	sameJSON(t, data["trend"], []any{})
	sameJSON(t, data["current_vo2_max_estimate"], map[string]any{
		"vo2_max": 28.0, "sport": "running", "source": "get_user_profile",
	})
	if !strings.Contains(data["note"].(string), "Historical VO2 max values were not available") {
		t.Fatalf("note = %v", data["note"])
	}
	if got := trCountPath(f, "GET", trTrainingStatusPath+"/2024-01-14"); got != 1 {
		t.Fatalf("status calls = %d, want 1", got)
	}
	if got := trCountPath(f, "GET", trMaxMetricsPath+"/2024-01-14/2024-01-15"); got != 1 {
		t.Fatalf("range calls = %d, want 1", got)
	}
	if got := trCountPath(f, "GET", trUserSettingsPath); got != 1 {
		t.Fatalf("profile calls = %d, want 1", got)
	}
}

func TestGetVo2maxTrendUsesDailyMetrics(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trMaxMetricsPath+"/2024-01-14/2024-01-15", []any{
		map[string]any{"generic": map[string]any{"calendarDate": "2024-01-14", "vo2MaxValue": 27.5}},
		map[string]any{"generic": map[string]any{"calendarDate": "2024-01-15", "vo2MaxValue": 28.0}},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_vo2max_trend", map[string]any{
		"start_date": "2024-01-14", "end_date": "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	trAssertNum(t, data, "latest_vo2_max", 28.0)
	trAssertNum(t, data, "change", 0.5)
	if data["sport"] != "running" {
		t.Fatalf("sport = %v", data["sport"])
	}
	sameJSON(t, data["trend"], []any{
		map[string]any{"date": "2024-01-14", "vo2_max": 27.5, "source": "get_max_metrics"},
		map[string]any{"date": "2024-01-15", "vo2_max": 28.0, "source": "get_max_metrics"},
	})
	if got := trCountPath(f, "GET", trTrainingStatusPath+"/2024-01-14"); got != 0 {
		t.Fatalf("training status called %d times", got)
	}
}

func TestGetVo2maxTrendCarriesForwardActivityDayValues(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trMaxMetricsPath+"/2024-01-12/2024-01-16", []any{
		map[string]any{"generic": map[string]any{
			"calendarDate": "2024-01-13", "vo2MaxValue": 48.0, "vo2MaxPreciseValue": 47.6,
		}},
		map[string]any{"generic": map[string]any{
			"calendarDate": "2024-01-15", "vo2MaxValue": 49.0, "vo2MaxPreciseValue": 48.7,
		}},
	})
	trStatusRoutes(f, "2024-01-14", 1, map[string]any{})
	trStatusRoutes(f, "2024-01-16", 1, map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_vo2max_trend", map[string]any{
		"start_date": "2024-01-12", "end_date": "2024-01-16",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	if data["sport"] != "running" {
		t.Fatalf("sport = %v", data["sport"])
	}
	trAssertNum(t, data, "data_points", 4)
	trAssertNum(t, data, "first_vo2_max", 47.6)
	trAssertNum(t, data, "latest_vo2_max", 48.7)
	trAssertNum(t, data, "change", 1.1)
	sameJSON(t, data["trend"], []any{
		map[string]any{"date": "2024-01-13", "vo2_max": 47.6, "source": "get_max_metrics"},
		map[string]any{"date": "2024-01-14", "vo2_max": 47.6, "source": "get_max_metrics", "carried_forward": true},
		map[string]any{"date": "2024-01-15", "vo2_max": 48.7, "source": "get_max_metrics"},
		map[string]any{"date": "2024-01-16", "vo2_max": 48.7, "source": "get_max_metrics", "carried_forward": true},
	})
	if got := trCountPath(f, "GET", trTrainingStatusPath+"/2024-01-14"); got != 1 {
		t.Fatalf("status 14 calls = %d", got)
	}
	if got := trCountPath(f, "GET", trTrainingStatusPath+"/2024-01-13"); got != 0 {
		t.Fatalf("status 13 calls = %d", got)
	}
}

func TestGetVo2maxTrendPrefersTrainingStatus(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trMaxMetricsPath+"/2024-01-14/2024-01-15", []any{})
	f.on("GET "+trTrainingStatusPath+"/2024-01-14", map[string]any{
		"mostRecentVO2Max": map[string]any{"generic": map[string]any{"vo2MaxValue": 48.0}},
	})
	f.on("GET "+trTrainingStatusPath+"/2024-01-15", map[string]any{
		"mostRecentVO2Max": map[string]any{"generic": map[string]any{"vo2MaxValue": 48.5}},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_vo2max_trend", map[string]any{
		"start_date": "2024-01-14", "end_date": "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	trAssertNum(t, data, "latest_vo2_max", 48.5)
	trend := trList(t, data, "trend")
	for _, item := range trend {
		if mutMap(t, item)["source"] != "get_training_status" {
			t.Fatalf("source = %v", mutMap(t, item)["source"])
		}
	}
	if got := trCountPath(f, "GET", trTrainingStatusPath+"/2024-01-14"); got != 1 {
		t.Fatalf("status calls = %d", got)
	}
}

func TestGetVo2maxTrendPreservesSelectedCyclingFromMixedPayloads(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trMaxMetricsPath+"/2024-01-13/2024-01-15", []any{})
	f.on("GET "+trTrainingStatusPath+"/2024-01-13", map[string]any{
		"mostRecentVO2Max": map[string]any{"cycling": map[string]any{"vo2MaxValue": 55.0}},
	})
	f.on("GET "+trTrainingStatusPath+"/2024-01-14", map[string]any{
		"mostRecentVO2Max": map[string]any{
			"generic": map[string]any{"vo2MaxValue": 48.0},
			"cycling": map[string]any{"vo2MaxValue": 56.0},
		},
	})
	f.on("GET "+trTrainingStatusPath+"/2024-01-15", map[string]any{
		"mostRecentVO2Max": map[string]any{
			"generic": map[string]any{"vo2MaxValue": 48.5},
			"cycling": map[string]any{"vo2MaxValue": 57.0},
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_vo2max_trend", map[string]any{
		"start_date": "2024-01-13", "end_date": "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	if data["sport"] != "cycling" {
		t.Fatalf("sport = %v", data["sport"])
	}
	trAssertNum(t, data, "change", 2.0)
	sameJSON(t, data["trend"], []any{
		map[string]any{"date": "2024-01-13", "vo2_max": 55.0, "source": "get_training_status"},
		map[string]any{"date": "2024-01-14", "vo2_max": 56.0, "source": "get_training_status"},
		map[string]any{"date": "2024-01-15", "vo2_max": 57.0, "source": "get_training_status"},
	})
}

func TestGetVo2maxTrendSelectsSportWithMostHistory(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trMaxMetricsPath+"/2024-01-13/2024-01-15", []any{})
	f.on("GET "+trTrainingStatusPath+"/2024-01-13", map[string]any{
		"mostRecentVO2Max": map[string]any{"cycling": map[string]any{"vo2MaxValue": 55.0}},
	})
	f.on("GET "+trTrainingStatusPath+"/2024-01-14", map[string]any{
		"mostRecentVO2Max": map[string]any{"generic": map[string]any{"vo2MaxValue": 48.0}},
	})
	f.on("GET "+trTrainingStatusPath+"/2024-01-15", map[string]any{
		"mostRecentVO2Max": map[string]any{"generic": map[string]any{"vo2MaxValue": 48.5}},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_vo2max_trend", map[string]any{
		"start_date": "2024-01-13", "end_date": "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	if data["sport"] != "running" {
		t.Fatalf("sport = %v", data["sport"])
	}
	trAssertNum(t, data, "data_points", 2)
	trend := trList(t, data, "trend")
	if got := num(t, mutMap(t, trend[0])["vo2_max"]); got != 48.0 {
		t.Fatalf("first = %v", got)
	}
	if got := num(t, mutMap(t, trend[1])["vo2_max"]); got != 48.5 {
		t.Fatalf("latest = %v", got)
	}
}

func TestGetVo2maxTrendUsesCyclingRangeMetrics(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trMaxMetricsPath+"/2024-01-15/2024-01-15", []any{
		map[string]any{"cycling": map[string]any{"calendarDate": "2024-01-15", "vo2MaxValue": 55.1}},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_vo2max_trend", map[string]any{
		"start_date": "2024-01-15", "end_date": "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	if data["sport"] != "cycling" {
		t.Fatalf("sport = %v", data["sport"])
	}
	sameJSON(t, data["trend"], []any{
		map[string]any{"date": "2024-01-15", "vo2_max": 55.1, "source": "get_max_metrics"},
	})
	if got := trCountPath(f, "GET", trTrainingStatusPath+"/2024-01-15"); got != 0 {
		t.Fatalf("training status called %d times", got)
	}
}

func TestGetVo2maxTrendRangeFailureDoesNotRetryMaxMetricsDaily(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trMaxMetricsPath+"/2024-01-14/2024-01-15", status(400, map[string]any{"message": "range unavailable"}))
	trStatusRoutes(f, "2024-01-14", 2, map[string]any{
		"mostRecentVO2Max": map[string]any{"generic": map[string]any{"vo2MaxValue": 48.0}},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_vo2max_trend", map[string]any{
		"start_date": "2024-01-14", "end_date": "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	if data["sport"] != "running" {
		t.Fatalf("sport = %v", data["sport"])
	}
	if got := trCountPath(f, "GET", trTrainingStatusPath+"/2024-01-14"); got != 1 {
		t.Fatalf("status calls = %d", got)
	}
	if got := trCountPath(f, "GET", trTrainingStatusPath+"/2024-01-15"); got != 1 {
		t.Fatalf("status calls = %d", got)
	}
	if got := trCountPath(f, "GET", trMaxMetricsPath+"/2024-01-14/2024-01-14"); got != 0 {
		t.Fatalf("daily max metrics called %d times", got)
	}
}

func TestGetVo2maxTrendUsesDailyMaxMetricsForOlderClients(t *testing.T) {
	// Go's client always supports the range endpoint, so the daily max-metrics
	// fallback that Python uses for older clients is unreachable here. A
	// single-day range payload is used directly.
	f := newFakeGarmin(t)
	f.on("GET "+trMaxMetricsPath+"/2024-01-15/2024-01-15", []any{
		map[string]any{"generic": map[string]any{"calendarDate": "2024-01-15", "vo2MaxValue": 48.0}},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_vo2max_trend", map[string]any{
		"start_date": "2024-01-15", "end_date": "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	trAssertNum(t, data, "latest_vo2_max", 48.0)
	if got := trCountPath(f, "GET", trMaxMetricsPath+"/2024-01-15/2024-01-15"); got != 1 {
		t.Fatalf("max metrics calls = %d, want 1", got)
	}
	if got := trCountPath(f, "GET", trTrainingStatusPath+"/2024-01-15"); got != 0 {
		t.Fatalf("training status called %d times", got)
	}
}

func TestGetVo2maxTrendPrefersRunningWhenSportCoverageIsTied(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trMaxMetricsPath+"/2024-01-15/2024-01-15", []any{})
	f.on("GET "+trTrainingStatusPath+"/2024-01-15", map[string]any{
		"mostRecentVO2Max": map[string]any{
			"generic": map[string]any{"vo2MaxValue": 48.0},
			"cycling": map[string]any{"vo2MaxValue": 55.0},
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_vo2max_trend", map[string]any{
		"start_date": "2024-01-15", "end_date": "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	if data["sport"] != "running" {
		t.Fatalf("sport = %v", data["sport"])
	}
	trAssertNum(t, data, "latest_vo2_max", 48.0)
}

func TestGetVo2maxTrendNoHistoryAvoidsDailyMaxMetricsRequests(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trMaxMetricsPath+"/2024-01-01/2024-03-30", []any{})
	day, _ := time.Parse(dateLayout, "2024-01-01")
	for i := 0; i < 90; i++ {
		f.on("GET "+trTrainingStatusPath+"/"+day.Format(dateLayout), map[string]any{})
		day = day.AddDate(0, 0, 1)
	}
	f.on("GET "+trUserSettingsPath, map[string]any{
		"userData": map[string]any{"vo2MaxRunning": 48.0},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_vo2max_trend", map[string]any{
		"start_date": "2024-01-01", "end_date": "2024-03-30",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	trAssertNum(t, data, "data_points", 0)
	statusCalls := 0
	for _, call := range f.recorded() {
		if strings.HasPrefix(call.Path, trTrainingStatusPath+"/") {
			statusCalls++
		}
	}
	if statusCalls != 90 {
		t.Fatalf("status calls = %d, want 90", statusCalls)
	}
	dailyMax := 0
	for _, call := range f.recorded() {
		if strings.HasPrefix(call.Path, trMaxMetricsPath+"/") {
			dailyMax++
		}
	}
	if dailyMax != 1 {
		t.Fatalf("max metrics calls = %d, want 1", dailyMax)
	}
	if got := trCountPath(f, "GET", trUserSettingsPath); got != 1 {
		t.Fatalf("profile calls = %d", got)
	}
}

func TestGetVo2maxTrendHandlesEndpointExceptionAndMissingMethod(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trMaxMetricsPath+"/2024-01-14/2024-01-15", []any{})
	trStatusRoutes(f, "2024-01-14", 2, status(400, map[string]any{"message": "API unavailable"}))
	f.on("GET "+trMaxMetricsPath+"/2024-01-14/2024-01-14", status(400, map[string]any{"message": "max unavailable"}))
	f.on("GET "+trMaxMetricsPath+"/2024-01-15/2024-01-15", status(400, map[string]any{"message": "max unavailable"}))
	f.on("GET "+trUserSettingsPath, map[string]any{
		"userData": map[string]any{"vo2MaxCycling": 55.0},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_vo2max_trend", map[string]any{
		"start_date": "2024-01-14", "end_date": "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	estimate := trSub(t, data, "current_vo2_max_estimate")
	if estimate["sport"] != "cycling" {
		t.Fatalf("sport = %v", estimate["sport"])
	}
	if got := trCountPath(f, "GET", trTrainingStatusPath+"/2024-01-14"); got != 1 {
		t.Fatalf("status calls = %d", got)
	}
}

func TestVo2TrendStopsAfterRangeAuthFailure(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trMaxMetricsPath+"/2024-01-13/2024-01-15", status(401, map[string]any{"message": "expired"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_vo2max_trend", map[string]any{
		"start_date": "2024-01-13", "end_date": "2024-01-15",
	})
	if !strings.HasPrefix(text, "Error retrieving VO2 max data:") {
		t.Fatalf("text = %q", text)
	}
	if got := trCountPath(f, "GET", trTrainingStatusPath+"/2024-01-13"); got != 0 {
		t.Fatalf("training status called %d times", got)
	}
	if got := trCountPath(f, "GET", trUserSettingsPath); got != 0 {
		t.Fatalf("profile called %d times", got)
	}
}

func TestVo2TrendRetainsMeasurementDatesAndMarksCarriedDays(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trMaxMetricsPath+"/2024-01-13/2024-01-16", []any{
		map[string]any{"generic": map[string]any{"calendarDate": "2024-01-13", "vo2MaxPreciseValue": 47.6}},
		map[string]any{"generic": map[string]any{"calendarDate": "2024-01-15", "vo2MaxPreciseValue": 48.7}},
	})
	f.on("GET "+trTrainingStatusPath+"/2024-01-14", map[string]any{
		"mostRecentVO2Max": map[string]any{"generic": map[string]any{
			"calendarDate": "2024-01-13", "vo2MaxPreciseValue": 47.6,
		}},
	})
	f.on("GET "+trTrainingStatusPath+"/2024-01-16", map[string]any{
		"mostRecentVO2Max": map[string]any{"generic": map[string]any{
			"calendarDate": "2024-01-15", "vo2MaxPreciseValue": 48.7,
		}},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_vo2max_trend", map[string]any{
		"start_date": "2024-01-13", "end_date": "2024-01-16",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	trAssertNum(t, data, "data_points", 4)
	trend := trList(t, data, "trend")
	wantCarried := []bool{false, true, false, true}
	for i, item := range trend {
		entry := mutMap(t, item)
		carried := false
		if v, ok := entry["carried_forward"]; ok {
			carried = v == true
		}
		if carried != wantCarried[i] {
			t.Fatalf("point %d carried = %v", i, carried)
		}
		if entry["source"] != "get_max_metrics" {
			t.Fatalf("point %d source = %v", i, entry["source"])
		}
	}
}

func TestVo2TrendCarriesPreviousMeasurementIntoRequestedRange(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trMaxMetricsPath+"/2024-01-13/2024-01-15", []any{})
	trStatusRoutes(f, "2024-01-13", 3, map[string]any{
		"mostRecentVO2Max": map[string]any{"cycling": map[string]any{
			"calendarDate": "2024-01-10", "vo2MaxPreciseValue": 55.1,
		}},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_vo2max_trend", map[string]any{
		"start_date": "2024-01-13", "end_date": "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	if data["sport"] != "cycling" {
		t.Fatalf("sport = %v", data["sport"])
	}
	trend := trList(t, data, "trend")
	if len(trend) != 3 {
		t.Fatalf("trend = %d, want 3", len(trend))
	}
	for _, item := range trend {
		entry := mutMap(t, item)
		if entry["carried_forward"] != true {
			t.Fatalf("point not carried: %v", entry)
		}
		trAssertNum(t, entry, "vo2_max", 55.1)
	}
	dates := []string{}
	for _, item := range trend {
		dates = append(dates, mutMap(t, item)["date"].(string))
	}
	sameJSON(t, dates, []string{"2024-01-13", "2024-01-14", "2024-01-15"})
}

func TestVo2TrendReportsEndpointFailureWhenFallbacksAreEmpty(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trMaxMetricsPath+"/2024-01-13/2024-01-15", status(400, map[string]any{"message": "range unavailable"}))
	trStatusRoutes(f, "2024-01-13", 3, map[string]any{})
	f.on("GET "+trUserSettingsPath, map[string]any{})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_vo2max_trend", map[string]any{
		"start_date": "2024-01-13", "end_date": "2024-01-15",
	})
	want := "Error retrieving VO2 max data: API Error 400 - range unavailable"
	if text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
}

func TestVo2TrendDisclosesRecoveredEndpointFailure(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trMaxMetricsPath+"/2024-01-13/2024-01-15", status(400, map[string]any{"message": "range unavailable"}))
	trStatusRoutes(f, "2024-01-13", 3, map[string]any{
		"mostRecentVO2Max": map[string]any{"generic": map[string]any{"vo2MaxValue": 48}},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_vo2max_trend", map[string]any{
		"start_date": "2024-01-13", "end_date": "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	trAssertNum(t, data, "latest_vo2_max", 48)
	sameJSON(t, data["request_errors"], []any{
		map[string]any{"source": "get_max_metrics", "error": "API Error 400 - range unavailable"},
	})
}

func TestVo2TrendPreservesUndatedSportAlongsideDatedSport(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trMaxMetricsPath+"/2024-01-13/2024-01-14", []any{})
	f.on("GET "+trTrainingStatusPath+"/2024-01-13", map[string]any{
		"mostRecentVO2Max": map[string]any{
			"generic": map[string]any{"vo2MaxValue": 48},
			"cycling": map[string]any{"calendarDate": "2024-01-10", "vo2MaxValue": 55},
		},
	})
	f.on("GET "+trTrainingStatusPath+"/2024-01-14", map[string]any{
		"mostRecentVO2Max": map[string]any{"generic": map[string]any{"vo2MaxValue": 48.5}},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_vo2max_trend", map[string]any{
		"start_date": "2024-01-13", "end_date": "2024-01-14",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	if data["sport"] != "running" {
		t.Fatalf("sport = %v", data["sport"])
	}
	trend := trList(t, data, "trend")
	values := []float64{}
	for _, item := range trend {
		values = append(values, num(t, mutMap(t, item)["vo2_max"]))
	}
	if len(values) != 2 || values[0] != 48 || values[1] != 48.5 {
		t.Fatalf("values = %v", values)
	}
}

// --- get_hrv_trend ---

func TestHrvTrendUsesReportedNightlyAverages(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trHRVPath+"/2026-09-04", map[string]any{
		"hrvSummary": map[string]any{"lastNightAvg": 54, "lastNight": 999, "weeklyAvg": 60},
	})
	f.on("GET "+trHRVPath+"/2026-09-05", map[string]any{
		"hrvSummary": map[string]any{"lastNightAvg": 70, "lastNight5MinHigh": 107},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_hrv_trend", map[string]any{
		"start_date": "2026-09-04", "end_date": "2026-09-05",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	trAssertNum(t, data, "days_with_data", 2)
	trend := trList(t, data, "trend")
	trAssertNum(t, mutMap(t, trend[0]), "last_night_avg_hrv_ms", 54)
	trAssertNum(t, mutMap(t, trend[1]), "last_night_avg_hrv_ms", 70)
	trAssertNum(t, data, "period_avg_hrv_ms", 62)
	trAssertNum(t, mutMap(t, trend[0]), "weekly_avg_hrv_ms", 60)
	trAssertNum(t, mutMap(t, trend[1]), "last_night_5min_high_hrv_ms", 107)
}

func TestHrvTrendAveragesOnlyAvailableNights(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trHRVPath+"/2026-09-01", map[string]any{"hrvSummary": map[string]any{"lastNightAvg": 54}})
	f.on("GET "+trHRVPath+"/2026-09-02", nil)
	f.on("GET "+trHRVPath+"/2026-09-03", map[string]any{
		"hrvSummary": map[string]any{"lastNightAvg": nil, "weeklyAvg": 60},
	})
	f.on("GET "+trHRVPath+"/2026-09-04", map[string]any{"hrvSummary": nil})
	f.on("GET "+trHRVPath+"/2026-09-05", map[string]any{"hrvSummary": map[string]any{"lastNightAvg": 71}})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_hrv_trend", map[string]any{
		"start_date": "2026-09-01", "end_date": "2026-09-05",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	trAssertNum(t, data, "days_with_data", 3)
	trAssertNum(t, data, "period_avg_hrv_ms", 62.5)
	trend := trList(t, data, "trend")
	if _, ok := mutMap(t, trend[1])["last_night_avg_hrv_ms"]; ok {
		t.Fatalf("second entry has last_night_avg_hrv_ms: %v", trend[1])
	}
}

func TestHrvTrendWithoutNightlyValues(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trHRVPath+"/2026-09-05", map[string]any{
		"hrvSummary": map[string]any{"weeklyAvg": 60},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_hrv_trend", map[string]any{
		"start_date": "2026-09-05", "end_date": "2026-09-05",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	if data["period_avg_hrv_ms"] != nil {
		t.Fatalf("period_avg_hrv_ms = %v, want null", data["period_avg_hrv_ms"])
	}
	sameJSON(t, data["trend"], []any{
		map[string]any{"date": "2026-09-05", "weekly_avg_hrv_ms": 60},
	})
}

// --- get_respiration_trend ---

func TestGetRespirationTrendTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /wellness-service/wellness/daily/respiration/2024-01-15", map[string]any{
		"avgWakingRespirationValue": 14.54,
		"avgSleepRespirationValue":  13.04,
		"highestRespirationValue":   18,
		"lowestRespirationValue":    12,
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_respiration_trend", map[string]any{
		"start_date": "2024-01-15", "end_date": "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	trAssertNum(t, data, "days_with_data", 1)
	trAssertNum(t, data, "period_avg_sleep_breaths_per_min", 13.0)
	entry := mutMap(t, trList(t, data, "trend")[0])
	trAssertNum(t, entry, "avg_waking_breaths_per_min", 14.5)
	trAssertNum(t, entry, "avg_sleep_breaths_per_min", 13.0)
	trAssertNum(t, entry, "highest_breaths_per_min", 18)
	trAssertNum(t, entry, "lowest_breaths_per_min", 12)
}

func TestGetRespirationTrendNoData(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /wellness-service/wellness/daily/respiration/2024-01-15", nil)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_respiration_trend", map[string]any{
		"start_date": "2024-01-15", "end_date": "2024-01-15",
	})
	if text != "No respiration data found between 2024-01-15 and 2024-01-15." {
		t.Fatalf("text = %q", text)
	}
}

func TestGetRespirationTrendError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /wellness-service/wellness/daily/respiration/2024-01-15", status(400, map[string]any{"message": "boom"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_respiration_trend", map[string]any{
		"start_date": "2024-01-15", "end_date": "2024-01-15",
	})
	if !strings.HasPrefix(text, "Error retrieving respiration data for 2024-01-15:") {
		t.Fatalf("text = %q", text)
	}
}

// --- get_running_tolerance ---

func TestGetRunningToleranceTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trRunningTolerancePath, fixture(t, "MOCK_RUNNING_TOLERANCE_DAILY"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_running_tolerance", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	calls := f.callsMatching("GET", trRunningTolerancePath)
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	q := calls[0].Query
	if q.Get("startDate") != "2024-01-15" || q.Get("endDate") != "2024-01-15" || q.Get("aggregation") != "daily" {
		t.Fatalf("query = %v", q)
	}
	data := trMap(t, text)
	if data["date"] != "2024-01-15" {
		t.Fatalf("date = %v", data["date"])
	}
	trAssertNum(t, data, "tolerance_km", 34.0)
	trAssertNum(t, data, "acute_load_km", 25.0)
	trAssertNum(t, data, "distance_km", 22.0)
	trAssertNum(t, data, "load_ratio", roundTo(25000.0/22000.0, 2))
	if data["feedback_phrase"] != "MEDIUM_LOAD" {
		t.Fatalf("feedback_phrase = %v", data["feedback_phrase"])
	}
}

func TestGetRunningToleranceEmptyResultIsNotADeviceVerdict(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trRunningTolerancePath, []any{})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_running_tolerance", map[string]any{"date": "2024-01-15"})
	if !strings.HasPrefix(text, "No running tolerance data found") {
		t.Fatalf("text = %q", text)
	}
	if strings.Contains(text, "does not support") {
		t.Fatalf("text = %q", text)
	}
}

func TestGetRunningToleranceError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trRunningTolerancePath, status(400, map[string]any{"message": "API Error"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_running_tolerance", map[string]any{"date": "2024-01-15"})
	if !strings.Contains(text, "Error retrieving running tolerance data") {
		t.Fatalf("text = %q", text)
	}
}

func TestGetRunningToleranceTrendDailySortsByDate(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trRunningTolerancePath, fixture(t, "MOCK_RUNNING_TOLERANCE_DAILY_TREND"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_running_tolerance_trend", map[string]any{
		"start_date": "2024-01-15", "end_date": "2024-01-16", "aggregation": "daily",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	calls := f.callsMatching("GET", trRunningTolerancePath)
	if len(calls) != 1 || calls[0].Query.Get("aggregation") != "daily" {
		t.Fatalf("query = %v", calls)
	}
	data := trMap(t, text)
	if data["aggregation"] != "daily" {
		t.Fatalf("aggregation = %v", data["aggregation"])
	}
	trAssertNum(t, data, "data_points", 2)
	trend := trList(t, data, "trend")
	if mutMap(t, trend[0])["date"] != "2024-01-15" || mutMap(t, trend[1])["date"] != "2024-01-16" {
		t.Fatalf("dates = %v, %v", mutMap(t, trend[0])["date"], mutMap(t, trend[1])["date"])
	}
	trAssertNum(t, data, "first_tolerance_km", 34.0)
	trAssertNum(t, data, "latest_tolerance_km", 34.5)
	trAssertNum(t, data, "tolerance_change_km", 0.5)
	if mutMap(t, trend[0])["feedback_phrase"] != "MEDIUM_LOAD" {
		t.Fatalf("feedback = %v", mutMap(t, trend[0])["feedback_phrase"])
	}
	if _, ok := mutMap(t, trend[0])["start_of_week"]; ok {
		t.Fatalf("start_of_week present in daily trend")
	}
}

func TestGetRunningToleranceTrendWeeklyDefault(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trRunningTolerancePath, fixture(t, "MOCK_RUNNING_TOLERANCE_WEEKLY"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_running_tolerance_trend", map[string]any{
		"start_date": "2024-01-02", "end_date": "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	calls := f.callsMatching("GET", trRunningTolerancePath)
	if len(calls) != 1 || calls[0].Query.Get("aggregation") != "weekly" {
		t.Fatalf("aggregation = %q", calls[0].Query.Get("aggregation"))
	}
	data := trMap(t, text)
	if data["aggregation"] != "weekly" {
		t.Fatalf("aggregation = %v", data["aggregation"])
	}
	week := mutMap(t, trList(t, data, "trend")[0])
	if week["start_of_week"] != "2024-01-02" {
		t.Fatalf("start_of_week = %v", week["start_of_week"])
	}
	if week["end_of_week"] != "2024-01-08" {
		t.Fatalf("end_of_week = %v", week["end_of_week"])
	}
	trAssertNum(t, week, "week_index", 1900)
	trAssertNum(t, week, "tolerance_km", 33.0)
	trAssertNum(t, week, "acute_load_km", 26.0)
	trAssertNum(t, week, "distance_km", 24.0)
	if _, ok := week["feedback_phrase"]; ok {
		t.Fatalf("feedback_phrase present in weekly trend")
	}
}

func TestGetRunningToleranceTrendEmptyResultIsNotADeviceVerdict(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trRunningTolerancePath, []any{})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_running_tolerance_trend", map[string]any{
		"start_date": "2024-01-01", "end_date": "2024-01-07",
	})
	if !strings.HasPrefix(text, "No running tolerance data found") {
		t.Fatalf("text = %q", text)
	}
	if strings.Contains(text, "does not support") {
		t.Fatalf("text = %q", text)
	}
}

func TestGetRunningToleranceTrendError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trRunningTolerancePath, status(400, map[string]any{"message": "API Error"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_running_tolerance_trend", map[string]any{
		"start_date": "2024-01-01", "end_date": "2024-01-07",
	})
	if !strings.Contains(text, "Error retrieving running tolerance trend") {
		t.Fatalf("text = %q", text)
	}
}

func TestGetRunningToleranceTrendInvalidRange(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_running_tolerance_trend", map[string]any{
		"start_date": "2024-01-15", "end_date": "2024-01-01",
	})
	if !strings.Contains(text, "end_date must be on or after start_date") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("GET", trRunningTolerancePath); len(calls) != 0 {
		t.Fatalf("running tolerance called %d times", len(calls))
	}
}

func TestGetRunningToleranceTrendDailyRangeTooLarge(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_running_tolerance_trend", map[string]any{
		"start_date": "2024-01-01", "end_date": "2024-04-15", "aggregation": "daily",
	})
	if !strings.Contains(text, "Date range too large") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("GET", trRunningTolerancePath); len(calls) != 0 {
		t.Fatalf("running tolerance called %d times", len(calls))
	}
}

// --- get_acclimation ---

func TestGetAcclimationReturnsHeatData(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trMaxMetricsPath+"/2024-07-15/2024-07-15", []any{
		map[string]any{"heatAltitudeAcclimation": map[string]any{
			"calendarDate":                      "2024-07-15",
			"heatAcclimationPercentage":         72.5,
			"previousHeatAcclimationPercentage": 65.0,
			"heatTrend":                         "ACCLIMATIZED",
			"heatAcclimationDate":               "2024-07-15",
			"altitudeAcclimation":               1200,
			"altitudeTrend":                     "INCREASING",
			"currentAltitude":                   850,
		}},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_acclimation", map[string]any{"date": "2024-07-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	if data["date"] != "2024-07-15" {
		t.Fatalf("date = %v", data["date"])
	}
	trAssertNum(t, data, "heat_acclimation_percent", 72.5)
	trAssertNum(t, data, "previous_heat_acclimation_percent", 65.0)
	if data["heat_trend"] != "ACCLIMATIZED" {
		t.Fatalf("heat_trend = %v", data["heat_trend"])
	}
	trAssertNum(t, data, "heat_acclimation_change", 7.5)
	trAssertNum(t, data, "altitude_acclimation_meters", 1200)
	if data["altitude_trend"] != "INCREASING" {
		t.Fatalf("altitude_trend = %v", data["altitude_trend"])
	}
	if calls := f.callsMatching("GET", trMaxMetricsPath+"/2024-07-15/2024-07-15"); len(calls) != 1 {
		t.Fatalf("max metrics calls = %d", len(calls))
	}
}

func TestGetAcclimationNoData(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trMaxMetricsPath+"/2024-07-15/2024-07-15", []any{
		map[string]any{"generic": map[string]any{"calendarDate": "2024-07-15", "vo2MaxValue": 48.0}},
	})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_acclimation", map[string]any{"date": "2024-07-15"})
	if !strings.Contains(text, "No acclimation data") {
		t.Fatalf("text = %q", text)
	}
}

func TestGetAcclimationException(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trMaxMetricsPath+"/2024-07-15/2024-07-15", status(500, map[string]any{"message": "API Error"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_acclimation", map[string]any{"date": "2024-07-15"})
	if !strings.Contains(text, "Error") {
		t.Fatalf("text = %q", text)
	}
}

// --- follow-up trend and load error handling ---

func TestGetTrainingLoadTrendOmitsMismatchedCtlAndUsesTrackerAcuteLoad(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trDevicesPath, []any{
		map[string]any{"deviceId": 100, "displayName": "Forerunner", "primaryDevice": true},
		map[string]any{"deviceId": 200, "displayName": "Edge", "primaryDevice": false},
	})
	f.on("GET "+trTrainingStatusPath+"/2026-10-09", map[string]any{
		"mostRecentTrainingStatus": map[string]any{
			"latestTrainingStatusData": map[string]any{
				"200": map[string]any{
					"acuteTrainingLoadDTO": map[string]any{
						"dailyTrainingLoadAcute":   0,
						"dailyTrainingLoadChronic": 80,
					},
				},
			},
		},
	})
	f.on("GET "+"/metrics-service/metrics/trainingreadiness/2026-10-09", []any{
		map[string]any{"deviceId": 100, "calendarDate": "2026-10-09", "acuteLoad": 123},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_training_load_trend", map[string]any{
		"start_date": "2026-10-09", "end_date": "2026-10-09",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	entry := mutMap(t, trList(t, trMap(t, text), "trend")[0])
	trAssertNum(t, entry, "reporting_device_id", 200)
	if entry["reporting_device_name"] != "Edge" {
		t.Fatalf("reporting_device_name = %v", entry["reporting_device_name"])
	}
	trAssertNum(t, entry, "atl", 123)
	if entry["atl_source"] != "training_readiness" {
		t.Fatalf("atl_source = %v", entry["atl_source"])
	}
	for _, key := range []string{"ctl", "tsb"} {
		if _, ok := entry[key]; ok {
			t.Fatalf("%s must be absent: %v", key, entry)
		}
	}
}

func TestGetTrainingLoadTrendReportsPrimaryTrackerLoads(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trDevicesPath, []any{
		map[string]any{"deviceId": 100, "displayName": "Forerunner", "primaryDevice": true},
	})
	status := map[string]any{
		"mostRecentTrainingStatus": map[string]any{
			"latestTrainingStatusData": map[string]any{
				"100": map[string]any{
					"deviceId":                     100,
					"trainingStatusFeedbackPhrase": "PRODUCTIVE_1",
					"acuteTrainingLoadDTO": map[string]any{
						"dailyTrainingLoadAcute":         520,
						"dailyTrainingLoadChronic":       410,
						"dailyAcuteChronicWorkloadRatio": 1.27,
						"acwrStatus":                     "HIGH",
						"minTrainingLoadChronic":         330,
						"maxTrainingLoadChronic":         610,
					},
				},
			},
		},
	}
	f.on("GET "+trTrainingStatusPath+"/2026-10-08", status)
	f.on("GET "+trTrainingStatusPath+"/2026-10-09", status)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_training_load_trend", map[string]any{
		"start_date": "2026-10-08", "end_date": "2026-10-09",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := trMap(t, text)
	trAssertNum(t, data, "days_with_data", 2)
	entry := mutMap(t, trList(t, data, "trend")[1])
	if entry["date"] != "2026-10-09" {
		t.Fatalf("date = %v", entry["date"])
	}
	trAssertNum(t, entry, "reporting_device_id", 100)
	trAssertNum(t, entry, "atl", 520)
	trAssertNum(t, entry, "ctl", 410)
	trAssertNum(t, entry, "tsb", -110)
	trAssertNum(t, entry, "acwr", 1.27)
	if entry["acwr_status"] != "HIGH" {
		t.Fatalf("acwr_status = %v", entry["acwr_status"])
	}
	trAssertNum(t, entry, "optimal_chronic_load_min", 330)
	trAssertNum(t, entry, "optimal_chronic_load_max", 610)
	if entry["training_status"] != "PRODUCTIVE_1" {
		t.Fatalf("training_status = %v", entry["training_status"])
	}
	if calls := f.callsMatching("GET", "/metrics-service/metrics/trainingreadiness/2026-10-08"); len(calls) != 0 {
		t.Fatalf("readiness called %d times", len(calls))
	}
}

func TestGetTrainingLoadTrendDescriptionUsesGarminLoadScale(t *testing.T) {
	cs := newSession(t, newFakeGarmin(t), nil)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	description := ""
	for _, tool := range res.Tools {
		if tool.Name == "get_training_load_trend" {
			description = tool.Description
		}
	}
	if description == "" {
		t.Fatal("get_training_load_trend missing")
	}
	if strings.Contains(description, "42-day") {
		t.Fatalf("description mentions 42-day: %q", description)
	}
	if !strings.Contains(description, "not comparable to TrainingPeaks") {
		t.Fatalf("description missing TrainingPeaks caveat: %q", description)
	}
	if !strings.Contains(description, "acwr_status") {
		t.Fatalf("description missing acwr_status: %q", description)
	}
}

func TestTrainingTrendsSurfaceUpstreamRequestFailures(t *testing.T) {
	statuses := []int{401, 429, 503}
	tools := []struct {
		name   string
		method string
	}{
		{"get_training_load_trend", "get_training_status"},
		{"get_hrv_trend", "get_hrv_data"},
		{"get_respiration_trend", "get_respiration_data"},
		{"get_vo2max_trend", "get_training_status"},
	}
	for _, tool := range tools {
		for _, code := range statuses {
			t.Run(fmt.Sprintf("%s/%d", tool.name, code), func(t *testing.T) {
				f := newFakeGarmin(t)
				failure := status(code, map[string]any{"message": "failed request"})
				if code == 401 {
					f.on("POST /diauth", map[string]any{"access_token": testJWT(nil)})
				}
				if tool.name == "get_vo2max_trend" {
					f.on("GET "+trMaxMetricsPath+"/2024-01-13/2024-01-15", []any{})
				}
				if tool.name == "get_hrv_trend" {
					f.on("GET "+trHRVPath+"/2024-01-13", failure)
				}
				if tool.name == "get_respiration_trend" {
					f.on("GET /wellness-service/wellness/daily/respiration/2024-01-13", failure)
				}
				if tool.name == "get_training_load_trend" || tool.name == "get_vo2max_trend" {
					f.on("GET "+trTrainingStatusPath+"/2024-01-13", failure)
				}
				cs := newSession(t, f, nil)

				text, _ := callTool(t, cs, tool.name, map[string]any{
					"start_date": "2024-01-13", "end_date": "2024-01-15",
				})
				if !strings.HasPrefix(text, "Error retrieving") {
					t.Fatalf("text = %q", text)
				}
				if !strings.Contains(text, strconv.Itoa(code)) {
					t.Fatalf("text = %q, want status %d", text, code)
				}
				expectedPath := trTrainingStatusPath + "/2024-01-13"
				switch tool.name {
				case "get_hrv_trend":
					expectedPath = trHRVPath + "/2024-01-13"
				case "get_respiration_trend":
					expectedPath = "/wellness-service/wellness/daily/respiration/2024-01-13"
				}
				// Go's client refreshes on 401 and retries the request, so the
				// fake records the original call and the retry.
				wantCalls := 1
				if code == 401 {
					wantCalls = 2
				}
				if calls := f.callsMatching("GET", expectedPath); len(calls) != wantCalls {
					t.Fatalf("%s calls = %d, want %d", expectedPath, len(calls), wantCalls)
				}
			})
		}
	}
}

func TestTrainingLoadTrendStopsAfterDeviceAuthFailure(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST /diauth", map[string]any{"access_token": testJWT(nil)})
	f.on("GET "+trDevicesPath, status(401, map[string]any{"message": "expired"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_training_load_trend", map[string]any{
		"start_date": "2024-01-13", "end_date": "2024-01-15",
	})
	want := "Error retrieving training load device data: API Error 401 - expired"
	if text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
	if calls := f.callsMatching("GET", trTrainingStatusPath+"/2024-01-13"); len(calls) != 0 {
		t.Fatalf("training status called")
	}
}

func TestTrainingLoadTrendSurfacesReadinessRateLimit(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trDevicesPath, []any{
		map[string]any{"deviceId": 100, "primaryDevice": true},
	})
	f.on("GET "+trTrainingStatusPath+"/2024-01-13", map[string]any{
		"mostRecentTrainingStatus": map[string]any{
			"latestTrainingStatusData": map[string]any{
				"200": map[string]any{"deviceId": 200},
			},
		},
	})
	f.on("GET "+"/metrics-service/metrics/trainingreadiness/2024-01-13", status(429, map[string]any{"message": "rate limited"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_training_load_trend", map[string]any{
		"start_date": "2024-01-13", "end_date": "2024-01-15",
	})
	want := "Error retrieving training load data for 2024-01-13: API Error 429 - rate limited"
	if text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
	if calls := f.callsMatching("GET", "/metrics-service/metrics/trainingreadiness/2024-01-13"); len(calls) != 1 {
		t.Fatalf("readiness calls = %d, want 1", len(calls))
	}
}
