package server

import (
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

// Ported from tests/integration/test_training_tools.py.

const (
	progressRoute  = "GET /fitnessstats-service/activity"
	hillRoute      = "GET /metrics-service/metrics/hillscore/stats"
	enduranceRoute = "GET /metrics-service/metrics/endurancescore/stats"
	tsPrefix       = "/metrics-service/metrics/trainingstatus/aggregated/"
	devicesRoute   = "GET /device-service/deviceregistration/devices"
	maxMetricsDay  = "/metrics-service/metrics/maxmet/daily/"
	toleranceRoute = "GET /metrics-service/metrics/runningtolerance/stats"
	profileRoute   = "GET /userprofile-service/userprofile/user-settings"
	hrvPrefix      = "/hrv-service/hrv/"
	respPrefix     = "/wellness-service/wellness/daily/respiration/"
	ftpRoute       = "GET /biometric-service/biometric/latestFunctionalThresholdPower/CYCLING"
)

type m = map[string]any

func datesBetween(start, end string) []string {
	var out []string
	s, _ := time.Parse(time.DateOnly, start)
	e, _ := time.Parse(time.DateOnly, end)
	for d := s; !d.After(e); d = d.AddDate(0, 0, 1) {
		out = append(out, d.Format(time.DateOnly))
	}
	return out
}

// onDays registers the same body for prefix+date of every date.
func onDays(f *fakeGarmin, prefix string, body any, dates ...string) {
	for _, d := range dates {
		f.on("GET "+prefix+d, body)
	}
}

// onEach registers bodies[i] for the i-th date (Python side_effect lists).
func onEach(f *fakeGarmin, prefix string, dates []string, bodies ...any) {
	for i, d := range dates {
		f.on("GET "+prefix+d, bodies[i])
	}
}

func countPrefix(f *fakeGarmin, prefix string) int {
	n := 0
	for _, c := range f.requests("") {
		if strings.HasPrefix(c.Path, prefix) {
			n++
		}
	}
	return n
}

// callMap calls a tool that returns a JSON object.
func callMap(t *testing.T, f *fakeGarmin, tool string, args m) m {
	t.Helper()
	return resultMap(t, callOK(t, newSession(t, f, nil), tool, args))
}

func devices(primary, other float64) []any {
	return []any{
		m{"deviceId": primary, "displayName": "Forerunner", "primaryDevice": true},
		m{"deviceId": other, "displayName": "Edge", "primaryDevice": false},
	}
}

func TestTrainingToolsRegistered(t *testing.T) {
	want := strings.Fields(`get_progress_summary_between_dates get_hill_score get_endurance_score get_training_effect get_hrv_data
		get_fitnessage_data get_training_status get_cycling_ftp get_lactate_threshold request_reload get_training_load_trend
		get_training_load_balance get_hrv_trend get_vo2max_trend get_respiration_trend get_running_tolerance
		get_running_tolerance_trend get_acclimation`)
	if len(want) != 18 {
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

func TestGetProgressSummaryBetweenDatesTool(t *testing.T) {
	// MOCK_PROGRESS_SUMMARY is a dict, not the list the API returns; Python only checks the call.
	f, text := callWith(t, progressRoute, fixture(t, "MOCK_PROGRESS_SUMMARY"), "get_progress_summary_between_dates",
		m{"start_date": "2024-01-08", "end_date": "2024-01-15", "metric": "duration"})
	if text != "Unexpected response format from API" {
		t.Error(text)
	}
	wantQuery(t, query(t, f, progressRoute), map[string]string{
		"startDate": "2024-01-08", "endDate": "2024-01-15", "aggregation": "lifetime", "groupByParentActivityType": "True", "metric": "duration",
	})
}

func TestGetProgressSummaryHandlesNullStats(t *testing.T) {
	_, text := callWith(t, progressRoute, []any{m{"date": "2024-01-15", "stats": nil}}, "get_progress_summary_between_dates",
		m{"start_date": "2024-01-08", "end_date": "2024-01-15", "metric": "duration"})
	if strings.Contains(text, "NoneType") || strings.Contains(text, "Error") {
		t.Error(text)
	}
	eq(t, "result", decode(t, text), m{"metric": "duration", "start_date": "2024-01-08", "end_date": "2024-01-15",
		"stats_by_activity_type": m{}, "count_of_activities": 0})
}

func TestGetProgressSummaryConvertsCaloriesFromGarminUnits(t *testing.T) {
	_, text := callWith(t, progressRoute, []any{m{
		"date": "2024-06-01", "countOfActivities": 1,
		"stats": m{
			"running":           m{"calories": m{"count": 1, "min": 3703.9776799999995, "max": 3703.9776799999995, "avg": 3703.9776799999995, "sum": 3703.9776799999995}},
			"fitness_equipment": m{"calories": m{"count": 2, "min": 276.54132, "max": 1508.4071999999999, "avg": 892.47426, "sum": 1784.94852}},
		},
	}}, "get_progress_summary_between_dates", m{"start_date": "2024-01-08", "end_date": "2024-01-15", "metric": "calories"})
	data := resultMap(t, text)
	if _, ok := data["date"]; ok {
		t.Error("date must be dropped")
	}
	if data["count_of_activities"] != 3.0 {
		t.Errorf("count = %v", data["count_of_activities"])
	}
	running := at(data, "stats_by_activity_type", "running")
	for _, k := range []string{"sum", "min", "max", "avg"} {
		approx(t, "running "+k, at(running, k), 884.0, 0.01)
	}
	equipment := at(data, "stats_by_activity_type", "fitness_equipment")
	approx(t, "min", at(equipment, "min"), 66.0, 0.01)
	approx(t, "max", at(equipment, "max"), 360.0, 0.01)
	approx(t, "sum", at(equipment, "sum"), 426.0, 0.01)
}

func TestGetProgressSummaryLeavesNonCalorieMetricsUnconverted(t *testing.T) {
	_, text := callWith(t, progressRoute, []any{m{
		"date": "2024-06-01", "countOfActivities": 1,
		"stats": m{"running": m{"duration": m{"count": 1, "min": 4450.452, "max": 4450.452, "avg": 4450.452, "sum": 4450.452}}},
	}}, "get_progress_summary_between_dates", m{"start_date": "2024-01-08", "end_date": "2024-01-15", "metric": "duration"})
	data := resultMap(t, text)
	if at(data, "stats_by_activity_type", "running", "sum") != 4450.452 || data["count_of_activities"] != 1.0 {
		t.Error(text)
	}
}

// Extra: the no-data and error texts of the progress summary.
func TestGetProgressSummaryNoDataAndError(t *testing.T) {
	args := m{"start_date": "2024-01-08", "end_date": "2024-01-15", "metric": "duration"}
	_, text := callWith(t, progressRoute, []any{}, "get_progress_summary_between_dates", args)
	if text != "No progress summary found for duration between 2024-01-08 and 2024-01-15." {
		t.Error(text)
	}
	f := newFakeGarmin(t)
	f.on(progressRoute, status(500, m{"message": "boom"}))
	text, isErr := callTool(t, newSession(t, f, nil), "get_progress_summary_between_dates", args)
	if !isErr || !strings.HasPrefix(text, "Error retrieving progress summary: ") {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetHillScoreTool(t *testing.T) {
	f, text := callWith(t, hillRoute, m{"hillScore": 75, "dateRange": m{"start": "2024-01-08", "end": "2024-01-15"}}, "get_hill_score",
		m{"start_date": "2024-01-08", "end_date": "2024-01-15"})
	wantQuery(t, query(t, f, hillRoute), map[string]string{"startDate": "2024-01-08", "endDate": "2024-01-15", "aggregation": "daily"})
	eq(t, "result", decode(t, text), m{"start_date": "2024-01-08", "end_date": "2024-01-15", "daily_scores": []any{}})
}

func TestHillScorePreservesMetricsWithNullDailyScores(t *testing.T) {
	_, text := callWith(t, hillRoute, m{"hillScoreDTOList": nil, "periodAvgScore": nil, "maxScore": 74}, "get_hill_score",
		m{"start_date": "2024-01-08", "end_date": "2024-01-15"})
	data := resultMap(t, text)
	if data["max_score"] != 74.0 || !reflect.DeepEqual(data["daily_scores"], []any{}) {
		t.Error(text)
	}
	if _, ok := data["latest_date"]; ok {
		t.Error("latest_date must be absent")
	}
}

func TestHillScoreSelectsLatestCalendarDate(t *testing.T) {
	_, text := callWith(t, hillRoute, m{"periodAvgScore": m{"2024-01": 48}, "hillScoreDTOList": []any{
		m{"calendarDate": "2024-01-08", "overallScore": 40},
		m{"calendarDate": "2024-01-15", "overallScore": 55, "strengthScore": 60},
		m{"calendarDate": "2024-01-10", "overallScore": 45},
	}}, "get_hill_score", m{"start_date": "2024-01-08", "end_date": "2024-01-15"})
	data := resultMap(t, text)
	if data["latest_date"] != "2024-01-15" || data["latest_overall_score"] != 55.0 || data["latest_strength_score"] != 60.0 || data["period_avg_score"] != 48.0 {
		t.Error(text)
	}
	if n := len(asList(data["daily_scores"])); n != 3 {
		t.Errorf("%d daily scores", n)
	}
}

func TestGetEnduranceScoreTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(enduranceRoute, fixture(t, "MOCK_ENDURANCE_SCORE"))
	f.on(typesURL, fixture(t, "MOCK_ACTIVITY_TYPES"))
	data := callMap(t, f, "get_endurance_score", m{"start_date": "2024-01-08", "end_date": "2024-01-15"})
	wantQuery(t, query(t, f, enduranceRoute), map[string]string{"startDate": "2024-01-08", "endDate": "2024-01-15", "aggregation": "weekly"})

	if data["period_avg_score"] != 5631.0 || data["period_max_score"] != 5740.0 || data["current_score"] != 5712.0 ||
		data["current_date"] != "2024-01-15" || data["classification"] != "intermediate" || data["classification_id"] != 2.0 {
		t.Errorf("%v", data)
	}
	if at(data, "thresholds", "trained") != 5800.0 || at(data, "thresholds", "well_trained") != 6500.0 {
		t.Errorf("thresholds = %v", data["thresholds"])
	}
	contributors := asList(data["contributors"])
	if len(contributors) != 4 {
		t.Fatalf("%d contributors", len(contributors))
	}
	find := func(name string) map[string]any {
		for _, c := range contributors {
			if asMap(c)["activity_type"] == name {
				return asMap(c)
			}
		}
		t.Fatalf("no %s contributor", name)
		return nil
	}
	if h := find("hiking"); h["contribution_percent"] != 5.49 || h["activity_type_id"] != 3.0 {
		t.Errorf("hiking = %v", h)
	}
	if y := find("yoga"); y["contribution_percent"] != 3.13 {
		t.Errorf("yoga = %v", y)
	}
	weeks := asList(data["weekly_breakdown"])
	if len(weeks) != 1 {
		t.Fatalf("%d weeks", len(weeks))
	}
	if w := asMap(weeks[0]); w["week_start"] != "2024-01-08" || w["avg_score"] != 5548.0 || w["max_score"] != 5561.0 {
		t.Errorf("week = %v", w)
	}
}

func TestGetEnduranceScoreHandlesNullScoreDto(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(enduranceRoute, m{"userProfilePK": 1, "startDate": "2022-09-01", "endDate": "2022-09-01", "avg": nil, "max": nil,
		"groupMap": m{"2022-08-29": nil}, "enduranceScoreDTO": nil})
	f.on(typesURL, fixture(t, "MOCK_ACTIVITY_TYPES"))
	data := callMap(t, f, "get_endurance_score", m{"start_date": "2022-09-01", "end_date": "2022-09-01"})
	if _, ok := data["current_score"]; ok {
		t.Error("current_score must be absent")
	}
	eq(t, "result", data, m{"start_date": "2022-09-01", "end_date": "2022-09-01", "weekly_breakdown": []any{m{"week_start": "2022-08-29"}}})
}

func TestGetTrainingEffectTool(t *testing.T) {
	f, text := callWith(t, actRoute, m{"summaryDTO": m{
		"trainingEffect": 3.5, "anaerobicTrainingEffect": 2.0, "trainingEffectLabel": "Highly Improving",
		"activityTrainingLoad": 150, "recoveryTime": 720, "performanceCondition": 95,
	}}, "get_training_effect", m{"activity_id": 12345678901})
	data := resultMap(t, text)
	if data["training_effect"] != 3.5 || data["aerobic_effect"] != 3.5 || data["anaerobic_effect"] != 2.0 ||
		data["recovery_time_hours"] != 12.0 || data["source"] != "activity_details" || data["activity_id"] != 12345678901.0 {
		t.Error(text)
	}
	if n := len(f.requests(actRoute)); n != 1 {
		t.Errorf("%d requests", n)
	}
}

func TestGetTrainingEffectException(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(actRoute, status(500, m{"message": "API Error"}))
	text, isErr := callTool(t, newSession(t, f, nil), "get_training_effect", m{"activity_id": 12345678901})
	if !isErr || !strings.Contains(text, "Error retrieving training effect data") {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetTrainingEffectFallsBackToActivityListOn403(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(actRoute, status(403, m{"message": "HTTP 403 Forbidden"}))
	f.on(activitiesPath, []any{m{
		"activityId": 12345678901, "aerobicTrainingEffect": 3.2, "anaerobicTrainingEffect": 0.4,
		"aerobicTrainingEffectMessage": "IMPROVING_AEROBIC_BASE_8", "activityTrainingLoad": 140,
	}})
	data := callMap(t, f, "get_training_effect", m{"activity_id": 12345678901})
	if data["source"] != "activity_list" || data["training_effect"] != 3.2 || data["anaerobic_effect"] != 0.4 || data["training_load"] != 140.0 ||
		data["training_effect_label"] != "IMPROVING_AEROBIC_BASE_8" {
		t.Errorf("%v", data)
	}
	wantQuery(t, f.requests(activitiesPath)[0].Query, map[string]string{"activityIds": "12345678901", "start": "0", "limit": "1"})
}

func TestGetTrainingEffect403WithoutListMatch(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(actRoute, status(403, m{"message": "HTTP 403 Forbidden"}))
	f.on(activitiesPath, []any{})
	text, isErr := callTool(t, newSession(t, f, nil), "get_training_effect", m{"activity_id": 12345678901})
	if !isErr || !strings.Contains(text, "HTTP 403") || !strings.Contains(text, "12345678901") {
		t.Errorf("%v %q", isErr, text)
	}
	// both searches (activityIds filter, then the recent page of 100) ran
	reqs := f.requests(activitiesPath)
	if len(reqs) != 3 || reqs[1].Query.Get("limit") != "100" || reqs[2].Query.Get("limit") != "100" {
		t.Errorf("search requests = %v", reqs)
	}
}

// Extra: without a 403 an unknown activity gives the plain not-found text.
func TestGetTrainingEffectNotFound(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(actRoute, nil)
	f.on(activitiesPath, []any{})
	text := callOK(t, newSession(t, f, nil), "get_training_effect", m{"activity_id": 12345678901})
	if text != "No activity found with ID 12345678901." {
		t.Error(text)
	}
}

func TestGetHrvDataTool(t *testing.T) {
	f, text := callWith(t, "GET "+hrvPrefix+"2024-01-15", fixture(t, "MOCK_HRV_DATA"), "get_hrv_data", m{"date": "2024-01-15"})
	eq(t, "result", decode(t, text), m{
		"date": "2024-01-15", "last_night_avg_hrv_ms": 48, "last_night_5min_high_hrv_ms": 52, "weekly_avg_hrv_ms": 45,
		"baseline_balanced_low_ms": 40, "baseline_balanced_upper_ms": 55, "baseline_low_upper_ms": 35,
		"status": "BALANCED", "feedback": "HRV_BALANCED_2", "sleep_start": "2024-01-15T00:10:00.0", "sleep_end": "2024-01-15T07:30:00.0",
	})
	if n := len(f.requests("GET " + hrvPrefix + "2024-01-15")); n != 1 {
		t.Errorf("%d requests", n)
	}
}

func TestGetHrvDataNoData(t *testing.T) {
	_, text := callWith(t, "GET "+hrvPrefix+"2024-01-15", nil, "get_hrv_data", m{"date": "2024-01-15"})
	if text != "No HRV data found for 2024-01-15." {
		t.Error(text)
	}
}

func TestGetHrvDataHandlesNullSummary(t *testing.T) {
	_, text := callWith(t, "GET "+hrvPrefix+"2024-01-15", m{"hrvSummary": nil, "sleepStartTimestampLocal": nil}, "get_hrv_data", m{"date": "2024-01-15"})
	if strings.Contains(text, "NoneType") || strings.Contains(text, "Error") {
		t.Error(text)
	}
	eq(t, "result", decode(t, text), m{"date": "2024-01-15"})
}

func TestHrvTimeseriesPreservesSummaryWithNullReadings(t *testing.T) {
	for name, summary := range map[string]any{"null summary": nil, "summary": m{"lastNightAvg": 54}} {
		t.Run(name, func(t *testing.T) {
			_, text := callWith(t, "GET "+hrvPrefix+"2024-01-15", m{"hrvSummary": summary, "hrvReadings": nil}, "get_hrv_data",
				m{"date": "2024-01-15", "return_timeseries": true})
			data := resultMap(t, text)
			if data["date"] != "2024-01-15" || !reflect.DeepEqual(data["hrv_readings"], []any{}) || data["readings_count"] != 0.0 {
				t.Error(text)
			}
			if summary != nil && data["last_night_avg_hrv_ms"] != 54.0 {
				t.Error(text)
			}
		})
	}
}

// Extra: the timeseries lists readings with their local time.
func TestGetHrvDataTimeseries(t *testing.T) {
	_, text := callWith(t, "GET "+hrvPrefix+"2024-01-15", fixture(t, "MOCK_HRV_DATA"), "get_hrv_data", m{"date": "2024-01-15", "return_timeseries": true})
	data := resultMap(t, text)
	readings := asList(data["hrv_readings"])
	if data["readings_count"] != 3.0 || len(readings) != 3 || !reflect.DeepEqual(readings[0], m{"time": "2024-01-15T00:15:00.0", "hrv_ms": 45.0}) {
		t.Error(text)
	}
}

func TestGetFitnessageDataTool(t *testing.T) {
	f, text := callWith(t, "GET /fitnessage-service/fitnessage/2024-01-15",
		m{"fitnessAge": 25, "chronologicalAge": 30, "vo2Max": 52.5, "date": "2024-01-15"}, "get_fitnessage_data", m{"date": "2024-01-15"})
	eq(t, "result", decode(t, text), m{"date": "2024-01-15", "fitness_age_years": 25, "chronological_age_years": 30, "age_difference_years": 5})
	if n := len(f.requests("")); n != 1 {
		t.Errorf("%d requests", n)
	}
}

// Extra: details adds the component breakdown.
func TestGetFitnessageDataDetails(t *testing.T) {
	_, text := callWith(t, "GET /fitnessage-service/fitnessage/2024-01-15", m{
		"fitnessAge": 25.34, "chronologicalAge": 30, "achievableFitnessAge": 22.26, "previousFitnessAge": 0,
		"components": m{
			"rhr": m{"value": 52, "targetValue": 48, "improvementValue": 4, "potentialAge": 23.04, "priority": 1, "stale": false, "lastMeasurementDate": "2024-01-14"},
			"bad": "x",
		},
	}, "get_fitnessage_data", m{"date": "2024-01-15", "details": true})
	eq(t, "result", decode(t, text), m{
		"date": "2024-01-15", "fitness_age_years": 25.3, "chronological_age_years": 30, "age_difference_years": 4.7, "achievable_fitness_age_years": 22.3,
		"components": m{"rhr": m{"value": 52, "target": 48, "improvement_needed": 4, "potential_age_if_improved": 23.0, "priority": 1, "stale": false, "last_measurement": "2024-01-14"}},
	})
}

func TestGetTrainingStatusTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+tsPrefix+"2024-01-15", fixture(t, "MOCK_TRAINING_STATUS"))
	data := callMap(t, f, "get_training_status", m{"date": "2024-01-15"})
	eq(t, "result", data, m{
		"date": "2024-01-15", "reporting_device_id": "device-123456", "training_status": "PRODUCTIVE", "training_status_feedback": "MAINTAINING",
		"sport": "RUNNING", "fitness_trend": "INCREASING", "vo2_max": 52.5, "vo2_max_precise": 52.47, "cycling_vo2_max": 55.0, "cycling_vo2_max_precise": 55.12,
		"acute_load": 250, "chronic_load": 220, "load_ratio": 1.14, "acwr_status": "OPTIMAL", "acwr_percent": 75,
	})
	if n := len(f.requests("GET " + tsPrefix + "2024-01-15")); n != 1 {
		t.Errorf("%d status requests", n)
	}
}

func TestGetTrainingStatusIncludesCyclingVo2Max(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+tsPrefix+"2024-01-15", fixture(t, "MOCK_TRAINING_STATUS"))
	data := callMap(t, f, "get_training_status", m{"date": "2024-01-15"})
	if data["cycling_vo2_max"] != 55.0 || data["cycling_vo2_max_precise"] != 55.12 {
		t.Errorf("%v", data)
	}
}

func TestGetTrainingStatusNoCyclingVo2WhenAbsent(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+tsPrefix+"2024-01-15", m{"mostRecentVO2Max": m{"generic": m{"vo2MaxValue": 52.5, "vo2MaxPreciseValue": 52.47}}})
	data := callMap(t, f, "get_training_status", m{"date": "2024-01-15"})
	_, a := data["cycling_vo2_max"]
	_, b := data["cycling_vo2_max_precise"]
	if a || b || data["vo2_max"] != 52.5 {
		t.Errorf("%v", data)
	}
}

func TestGetTrainingStatusHandlesNullCyclingVo2(t *testing.T) {
	status := fixture(t, "MOCK_TRAINING_STATUS").(map[string]any)
	status["mostRecentVO2Max"].(map[string]any)["cycling"] = nil
	f := newFakeGarmin(t)
	f.on("GET "+tsPrefix+"2024-01-15", status)
	data := callMap(t, f, "get_training_status", m{"date": "2024-01-15"})
	if _, ok := data["cycling_vo2_max"]; ok || data["vo2_max"] != 52.5 {
		t.Errorf("%v", data)
	}
}

func TestGetTrainingStatusUsesTrackerReadinessWhenReportingDeviceIsNotPrimary(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(devicesRoute, devices(100, 200))
	f.on("GET "+tsPrefix+"2026-10-09", m{"mostRecentTrainingStatus": m{"latestTrainingStatusData": m{"200": m{
		"trainingStatus":       "DETRAINING",
		"acuteTrainingLoadDTO": m{"dailyTrainingLoadAcute": 0, "dailyTrainingLoadChronic": 80, "dailyAcuteChronicWorkloadRatio": 0},
	}}}})
	f.on("GET "+readinessPath+"2026-10-09", []any{m{"deviceId": 100, "calendarDate": "2026-10-09", "acuteLoad": 123}})
	data := callMap(t, f, "get_training_status", m{"date": "2026-10-09"})
	if data["reporting_device_id"] != 200.0 || data["reporting_device_name"] != "Edge" || data["acute_load"] != 123.0 || data["acute_load_source"] != "training_readiness" {
		t.Errorf("%v", data)
	}
	if _, ok := data["chronic_load"]; ok {
		t.Error("chronic_load must be absent")
	}
	if _, ok := data["load_ratio"]; ok {
		t.Error("load_ratio must be absent")
	}
	if len(f.requests(devicesRoute)) != 1 || len(f.requests("GET "+readinessPath+"2026-10-09")) != 1 {
		t.Error("devices and readiness must each be fetched once")
	}
}

func TestGetTrainingStatusPreservesStatusWhenReadinessFails(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(devicesRoute, devices(100, 200))
	f.on("GET "+tsPrefix+"2026-10-09", m{"mostRecentTrainingStatus": m{"latestTrainingStatusData": m{"200": m{"trainingStatus": "DETRAINING"}}}})
	f.on("GET "+readinessPath+"2026-10-09", status(500, m{"message": "timeout"}))
	data := callMap(t, f, "get_training_status", m{"date": "2026-10-09"})
	if data["training_status"] != "DETRAINING" || data["acute_load_source"] != "unavailable" || data["acute_load_unavailable_reason"] != "readiness_unavailable" {
		t.Errorf("%v", data)
	}
	if _, ok := data["acute_load"]; ok {
		t.Error("acute_load must be absent")
	}
}

func TestGetTrainingStatusRejectsWrongReadinessDeviceAndDate(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(devicesRoute, devices(100, 200))
	f.on("GET "+tsPrefix+"2026-10-09", m{"mostRecentTrainingStatus": m{"latestTrainingStatusData": m{"200": m{"trainingStatus": "DETRAINING"}}}})
	f.on("GET "+readinessPath+"2026-10-09", []any{
		m{"deviceId": 200, "calendarDate": "2026-10-09", "acuteLoad": 999},
		m{"deviceId": 100, "calendarDate": "2026-10-08", "acuteLoad": 888},
	})
	data := callMap(t, f, "get_training_status", m{"date": "2026-10-09"})
	if data["training_status"] != "DETRAINING" || data["acute_load_source"] != "unavailable" || data["acute_load_unavailable_reason"] != "readiness_device_or_date_mismatch" {
		t.Errorf("%v", data)
	}
	if _, ok := data["acute_load"]; ok {
		t.Error("acute_load must be absent")
	}
}

func TestGetTrainingStatusDoesNotCallReadinessWhenDeviceLookupFails(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(devicesRoute, status(500, m{"message": "device endpoint unavailable"}))
	f.on("GET "+tsPrefix+"2026-10-09", m{"mostRecentTrainingStatus": m{"latestTrainingStatusData": m{"200": m{
		"trainingStatus": "PRODUCTIVE", "acuteTrainingLoadDTO": m{"dailyTrainingLoadAcute": 0, "dailyTrainingLoadChronic": 80},
	}}}})
	data := callMap(t, f, "get_training_status", m{"date": "2026-10-09"})
	if data["training_status"] != "PRODUCTIVE" || data["acute_load"] != 0.0 || data["chronic_load"] != 80.0 {
		t.Errorf("%v", data)
	}
	if n := countPrefix(f, readinessPath); n != 0 {
		t.Errorf("%d readiness requests", n)
	}
}

func TestGetTrainingStatusSkipsNullDeviceEntries(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+tsPrefix+"2026-08-24", m{
		"mostRecentTrainingStatus": m{"latestTrainingStatusData": m{"device-null": nil, "device-valid": m{
			"calendarDate": "2026-08-24", "trainingStatus": "PRODUCTIVE", "acuteTrainingLoadDTO": m{"dailyTrainingLoadAcute": 250},
		}}},
		"mostRecentVO2Max":              m{"generic": m{"vo2MaxValue": 52.5}, "cycling": nil},
		"mostRecentTrainingLoadBalance": m{"metricsTrainingLoadBalanceDTOMap": m{"device-null": nil, "device-valid": m{"monthlyLoadAerobicLow": 100}}},
	})
	data := callMap(t, f, "get_training_status", m{"date": "2026-08-24"})
	if data["date"] != "2026-08-24" || data["training_status"] != "PRODUCTIVE" || data["acute_load"] != 250.0 || data["vo2_max"] != 52.5 || data["monthly_load_aerobic_low"] != 100.0 {
		t.Errorf("%v", data)
	}
	if n := len(f.requests("GET " + tsPrefix + "2026-08-24")); n != 1 {
		t.Errorf("%d requests", n)
	}
}

func TestGetTrainingStatusSkipsEmptyDeviceEntries(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+tsPrefix+"2026-08-24", m{
		"mostRecentTrainingStatus": m{"latestTrainingStatusData": m{"device-empty": m{}, "device-valid": m{
			"calendarDate": "2026-08-24", "trainingStatus": "PRODUCTIVE", "acuteTrainingLoadDTO": m{"dailyTrainingLoadAcute": 250},
		}}},
		"mostRecentTrainingLoadBalance": m{"metricsTrainingLoadBalanceDTOMap": m{"device-empty": m{}, "device-valid": m{"monthlyLoadAerobicLow": 100}}},
	})
	data := callMap(t, f, "get_training_status", m{"date": "2026-08-24"})
	if data["training_status"] != "PRODUCTIVE" || data["acute_load"] != 250.0 || data["monthly_load_aerobic_low"] != 100.0 {
		t.Errorf("%v", data)
	}
}

func TestGetTrainingStatusToleratesAllNullDeviceMaps(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+tsPrefix+"2026-08-24", m{
		"mostRecentTrainingStatus":      m{"latestTrainingStatusData": m{"device-null": nil}},
		"mostRecentVO2Max":              m{"generic": nil, "cycling": nil},
		"mostRecentTrainingLoadBalance": m{"metricsTrainingLoadBalanceDTOMap": m{"device-null": nil}},
	})
	eq(t, "result", callMap(t, f, "get_training_status", m{"date": "2026-08-24"}), m{"date": "2026-08-24"})
}

func TestGetTrainingStatusToleratesNonMappingResponse(t *testing.T) {
	_, text := callWith(t, "GET "+tsPrefix+"2026-08-24", []any{nil}, "get_training_status", m{"date": "2026-08-24"})
	if text != "No training status data found for 2026-08-24." {
		t.Error(text)
	}
}

func TestTrainingStatusRetainsMonthlyLoadWithoutStatusSnapshot(t *testing.T) {
	for name, statusMap := range map[string]any{"empty": m{}, "null": nil} {
		t.Run(name, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on(devicesRoute, []any{m{"deviceId": 100, "primaryDevice": true}, m{"deviceId": 200}})
			f.on("GET "+tsPrefix+"2024-01-15", m{
				"mostRecentTrainingStatus": m{"latestTrainingStatusData": statusMap},
				"mostRecentTrainingLoadBalance": m{"metricsTrainingLoadBalanceDTOMap": m{
					"200": m{"deviceId": 200, "monthlyLoadAerobicLow": 999},
					"100": m{"deviceId": 100, "monthlyLoadAerobicLow": 400, "monthlyLoadAerobicHigh": 700, "monthlyLoadAnaerobic": 250, "trainingBalanceFeedbackPhrase": "BALANCED"},
				}},
			})
			data := callMap(t, f, "get_training_status", m{"date": "2024-01-15"})
			if data["load_device_id"] != 100.0 || data["monthly_load_aerobic_low"] != 400.0 || data["monthly_load_aerobic_high"] != 700.0 ||
				data["monthly_load_anaerobic"] != 250.0 || data["training_balance_feedback"] != "BALANCED" {
				t.Errorf("%v", data)
			}
			if _, ok := data["reporting_device_id"]; ok {
				t.Error("reporting_device_id must be absent")
			}
			if _, ok := data["load_unavailable_reason"]; ok {
				t.Error("load_unavailable_reason must be absent")
			}
		})
	}
}

// Extra: a load record for another device is not attached to the reporting device.
func TestGetTrainingStatusFlagsLoadDeviceMismatch(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+tsPrefix+"2024-01-15", m{
		"mostRecentTrainingStatus":      m{"latestTrainingStatusData": m{"100": m{"trainingStatus": "PRODUCTIVE"}}},
		"mostRecentTrainingLoadBalance": m{"metricsTrainingLoadBalanceDTOMap": m{"200": m{"deviceId": 200, "monthlyLoadAerobicLow": 999}}},
	})
	data := callMap(t, f, "get_training_status", m{"date": "2024-01-15"})
	if data["load_source"] != "unavailable" || data["load_unavailable_reason"] != "load_device_mismatch" || data["reporting_device_id"] != 100.0 {
		t.Errorf("%v", data)
	}
	if _, ok := data["monthly_load_aerobic_low"]; ok {
		t.Error("monthly load of another device must not be reported")
	}
}

func TestGetCyclingFtpTool(t *testing.T) {
	f, text := callWith(t, ftpRoute, fixture(t, "MOCK_CYCLING_FTP"), "get_cycling_ftp", m{})
	eq(t, "result", decode(t, text), m{
		"sport": "CYCLING", "functional_threshold_power_watts": 294, "calendar_date": "2024-03-15T10:30:00.000", "is_stale": false,
		"biometric_source_type": "CHANGE_LOG",
	})
	if n := len(f.requests("")); n != 1 {
		t.Errorf("%d requests", n)
	}
}

func TestCyclingFtpAcceptsListPayload(t *testing.T) {
	_, text := callWith(t, ftpRoute, []any{fixture(t, "MOCK_CYCLING_FTP")}, "get_cycling_ftp", m{})
	data := resultMap(t, text)
	if data["functional_threshold_power_watts"] != 294.0 || data["sport"] != "CYCLING" {
		t.Error(text)
	}
}

// Extra: nothing to report.
func TestGetCyclingFtpNoData(t *testing.T) {
	_, text := callWith(t, ftpRoute, []any{}, "get_cycling_ftp", m{})
	if text != "No cycling FTP data found" {
		t.Error(text)
	}
}

const (
	lactateLatest  = "GET /biometric-service/biometric/latestLactateThreshold"
	lactateStats   = "/biometric-service/stats/"
	lactateSpeed   = "GET " + lactateStats + "lactateThresholdSpeed/range/2024-01-08/2024-01-15"
	lactateHR      = "GET " + lactateStats + "lactateThresholdHeartRate/range/2024-01-08/2024-01-15"
	lactatePower   = "GET " + lactateStats + "functionalThresholdPower/range/2024-01-08/2024-01-15"
	lactateLatestP = "/biometric-service/biometric/powerToWeight/latest/"
)

func TestGetLactateThresholdToolLatest(t *testing.T) {
	f := newFakeGarmin(t)
	fx := asMap(fixture(t, "MOCK_LACTATE_THRESHOLD"))
	sh := asMap(fx["speed_and_heart_rate"])
	// the endpoint lists two near-identical records: one with the speed, one with the heart rate
	f.on(lactateLatest, []any{
		m{"userProfilePK": sh["userProfilePK"], "version": 1, "calendarDate": sh["calendarDate"], "sequence": 2, "speed": sh["speed"]},
		m{"userProfilePK": sh["userProfilePK"], "heartRate": sh["heartRate"]},
	})
	f.on("GET "+lactateLatestP+today(), []any{fx["power"]})
	data := callMap(t, f, "get_lactate_threshold", m{})
	approx(t, "speed", data["lactate_threshold_speed_mps"], 3.888878, 1e-6)
	if got := int(1000/data["lactate_threshold_speed_mps"].(float64) + 0.5); got != 4*60+17 {
		t.Errorf("pace = %d s/km", got)
	}
	if data["lactate_threshold_heart_rate_bpm"] != 169.0 || data["functional_threshold_power_watts"] != 334.0 || data["sport"] != "RUNNING" || data["power_to_weight"] != 4.575 {
		t.Errorf("%v", data)
	}
	if data["speed_hr_date"] != "2024-01-15T10:30:00.000" || data["power_date"] != "2024-01-15T11:00:00.000" || data["weight_kg"] != 73.0 || data["is_stale"] != false {
		t.Errorf("%v", data)
	}
	if _, ok := data["heart_rate_cycling_bpm"]; ok {
		t.Error("null cycling heart rate must be dropped")
	}
	wantQuery(t, f.requests("GET " + lactateLatestP + today())[0].Query, map[string]string{"sport": "Running"})
}

func TestGetLactateThresholdToolRange(t *testing.T) {
	f := newFakeGarmin(t)
	fx := asMap(fixture(t, "MOCK_LACTATE_THRESHOLD_RANGE"))
	f.on(lactateSpeed, fx["speed"])
	f.on(lactateHR, fx["heart_rate"])
	f.on(lactatePower, fx["power"])
	data := callMap(t, f, "get_lactate_threshold", m{"start_date": "2024-01-08", "end_date": "2024-01-15"})
	if data["start_date"] != "2024-01-08" || data["end_date"] != "2024-01-15" {
		t.Errorf("%v", data)
	}
	speeds := asList(data["speed_history"])
	if len(speeds) != 3 || asMap(speeds[0])["date"] != "2024-01-08" {
		t.Fatalf("speed_history = %v", speeds)
	}
	approx(t, "speed", asMap(speeds[0])["speed_mps"], 3.9166557, 1e-6)
	if got := int(1000/asMap(speeds[0])["speed_mps"].(float64) + 0.5); got != 4*60+15 {
		t.Errorf("pace = %d s/km", got)
	}
	hrs := asList(data["heart_rate_history"])
	if len(hrs) != 3 || !reflect.DeepEqual(hrs[2], m{"date": "2024-01-15", "heart_rate_bpm": 169.0, "series": "running"}) {
		t.Errorf("heart_rate_history = %v", hrs)
	}
	if _, ok := data["power_history"]; !ok {
		t.Error("power_history missing")
	}
	for _, route := range []string{lactateSpeed, lactateHR, lactatePower} {
		wantQuery(t, query(t, f, route), map[string]string{"sport": "RUNNING", "aggregation": "daily", "aggregationStrategy": "LATEST"})
	}
}

func TestGetLactateThresholdRejectsPartialRange(t *testing.T) {
	for name, args := range map[string]m{"start only": {"start_date": "2024-01-08"}, "end only": {"end_date": "2024-01-15"}} {
		t.Run(name, func(t *testing.T) {
			f := newFakeGarmin(t)
			text := callOK(t, newSession(t, f, nil), "get_lactate_threshold", args)
			if !strings.Contains(text, "Provide both start_date and end_date") {
				t.Error(text)
			}
			noRequests(t, f, "GET")
		})
	}
}

// Extra: an empty latest result and an empty range are reported as no data.
func TestGetLactateThresholdNoData(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(lactateSpeed, nil)
	f.on(lactateHR, nil)
	f.on(lactatePower, nil)
	f.on(lactateLatest, []any{})
	f.on("GET "+lactateLatestP+today(), []any{})
	cs := newSession(t, f, nil)
	// latest always yields two keys, so only the range can come back empty... but each part is null here
	text := callOK(t, cs, "get_lactate_threshold", m{"start_date": "2024-01-08", "end_date": "2024-01-15"})
	eq(t, "range", decode(t, text), m{"start_date": "2024-01-08", "end_date": "2024-01-15"})
}

func TestRequestReloadTool(t *testing.T) {
	reload := m{"status": "success", "message": "Data reload requested"}
	f, text := callWith(t, "POST /wellness-service/wellness/epoch/request/2024-01-15", reload, "request_reload", m{"date": "2024-01-15"})
	eq(t, "result", decode(t, text), reload)
	if n := len(f.requests("POST /wellness-service/wellness/epoch/request/2024-01-15")); n != 1 {
		t.Errorf("%d requests", n)
	}
	if !strings.Contains(text, "\n  \"message\"") {
		t.Errorf("not indented: %s", text)
	}
}

// Extra: request_reload has no write tracker in Python; failures are plain "Error requesting data reload" strings.
func TestRequestReloadErrors(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST /wellness-service/wellness/epoch/request/2024-01-15", status(500, m{"message": "boom"}))
	text, isErr := callTool(t, newSession(t, f, nil), "request_reload", m{"date": "2024-01-15"})
	if !isErr || text != "Error requesting data reload: API Error 500 - boom" {
		t.Errorf("%v %q", isErr, text)
	}
	text, isErr = callTool(t, newSession(t, f, nil), "request_reload", m{"date": "nope"})
	if !isErr || !strings.HasPrefix(text, "Error requesting data reload: cdate must be in format") {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetTrainingLoadTrendOmitsMismatchedCtlAndUsesTrackerAcuteLoad(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(devicesRoute, devices(100, 200))
	f.on("GET "+tsPrefix+"2026-10-09", m{"mostRecentTrainingStatus": m{"latestTrainingStatusData": m{"200": m{
		"acuteTrainingLoadDTO": m{"dailyTrainingLoadAcute": 0, "dailyTrainingLoadChronic": 80},
	}}}})
	f.on("GET "+readinessPath+"2026-10-09", []any{m{"deviceId": 100, "calendarDate": "2026-10-09", "acuteLoad": 123}})
	data := callMap(t, f, "get_training_load_trend", m{"start_date": "2026-10-09", "end_date": "2026-10-09"})
	entry := asMap(asList(data["trend"])[0])
	if entry["reporting_device_id"] != 200.0 || entry["reporting_device_name"] != "Edge" || entry["atl"] != 123.0 || entry["atl_source"] != "training_readiness" {
		t.Errorf("%v", entry)
	}
	for _, k := range []string{"ctl", "tsb"} {
		if _, ok := entry[k]; ok {
			t.Errorf("%s must be absent", k)
		}
	}
	if len(f.requests(devicesRoute)) != 1 || len(f.requests("GET "+readinessPath+"2026-10-09")) != 1 {
		t.Error("devices and readiness must each be fetched once")
	}
}

func TestGetTrainingLoadTrendReportsPrimaryTrackerLoads(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(devicesRoute, []any{m{"deviceId": 100, "displayName": "Forerunner", "primaryDevice": true}})
	onDays(f, tsPrefix, m{"mostRecentTrainingStatus": m{"latestTrainingStatusData": m{"100": m{
		"deviceId": 100, "trainingStatusFeedbackPhrase": "PRODUCTIVE_1",
		"acuteTrainingLoadDTO": m{
			"dailyTrainingLoadAcute": 520, "dailyTrainingLoadChronic": 410, "dailyAcuteChronicWorkloadRatio": 1.27,
			"acwrStatus": "HIGH", "minTrainingLoadChronic": 330, "maxTrainingLoadChronic": 610,
		},
	}}}}, "2026-10-08", "2026-10-09")
	data := callMap(t, f, "get_training_load_trend", m{"start_date": "2026-10-08", "end_date": "2026-10-09"})
	if data["days_with_data"] != 2.0 {
		t.Errorf("%v", data)
	}
	trend := asList(data["trend"])
	entry := asMap(trend[len(trend)-1])
	eq(t, "entry", entry, m{
		"date": "2026-10-09", "reporting_device_id": 100, "reporting_device_name": "Forerunner", "atl": 520, "ctl": 410, "tsb": -110, "acwr": 1.27,
		"acwr_status": "HIGH", "optimal_chronic_load_min": 330, "optimal_chronic_load_max": 610, "training_status": "PRODUCTIVE_1",
	})
	if n := countPrefix(f, readinessPath); n != 0 {
		t.Errorf("%d readiness requests", n)
	}
}

func TestGetTrainingLoadTrendDescriptionUsesGarminLoadScale(t *testing.T) {
	for _, tool := range listTools(t, newSession(t, newFakeGarmin(t), nil)) {
		if tool.Name != "get_training_load_trend" {
			continue
		}
		d := tool.Description
		if strings.Contains(d, "42-day") || !strings.Contains(d, "not comparable to TrainingPeaks") || !strings.Contains(d, "acwr_status") {
			t.Errorf("description = %q", d)
		}
		return
	}
	t.Fatal("get_training_load_trend not listed")
}

func loadBalanceRecord(deviceID int, aerobicLow int, feedback string, extra m) m {
	r := m{
		"deviceId": deviceID, "calendarDate": "2026-10-09",
		"monthlyLoadAerobicLow": aerobicLow, "monthlyLoadAerobicLowTargetMin": 200, "monthlyLoadAerobicLowTargetMax": 600,
		"monthlyLoadAerobicHigh": 700, "monthlyLoadAerobicHighTargetMin": 300, "monthlyLoadAerobicHighTargetMax": 650,
		"monthlyLoadAnaerobic": 250, "monthlyLoadAnaerobicTargetMin": 100, "monthlyLoadAnaerobicTargetMax": 300,
		"trainingBalanceFeedbackPhrase": feedback,
	}
	for k, v := range extra {
		r[k] = v
	}
	return r
}

func TestGetTrainingLoadBalancePrefersPrimaryTrackerOverFirstRecord(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(devicesRoute, devices(100, 200))
	f.on("GET "+tsPrefix+"2026-10-09", m{"mostRecentTrainingLoadBalance": m{"metricsTrainingLoadBalanceDTOMap": m{
		"200": loadBalanceRecord(200, 50, "AEROBIC_LOW_SHORTAGE", nil),
		"100": loadBalanceRecord(100, 400, "BALANCED", nil),
	}}})
	data := callMap(t, f, "get_training_load_balance", m{"date": "2026-10-09"})
	if data["load_device_id"] != 100.0 || data["load_device_name"] != "Forerunner" || data["feedback"] != "BALANCED" {
		t.Errorf("%v", data)
	}
	eq(t, "aerobic_low", data["aerobic_low"], m{"load": 400, "target_min": 200, "target_max": 600, "status": "within"})
	if at(data, "aerobic_high", "status") != "above" || at(data, "anaerobic", "status") != "within" {
		t.Errorf("%v", data)
	}
}

func TestGetTrainingLoadBalanceFallsBackToPrimaryTrainingMarker(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(devicesRoute, status(500, m{"message": "devices unavailable"}))
	f.on("GET "+tsPrefix+"2026-10-09", m{"mostRecentTrainingLoadBalance": m{"metricsTrainingLoadBalanceDTOMap": m{
		"200": loadBalanceRecord(200, 50, "AEROBIC_LOW_SHORTAGE", nil),
		"100": loadBalanceRecord(100, 400, "BALANCED", m{"primaryTrainingDevice": true}),
	}}})
	data := callMap(t, f, "get_training_load_balance", m{"date": "2026-10-09"})
	if data["load_device_id"] != 100.0 || data["feedback"] != "BALANCED" {
		t.Errorf("%v", data)
	}
	if _, ok := data["load_device_name"]; ok {
		t.Error("load_device_name must be absent")
	}
}

func TestGetTrainingLoadBalanceNoData(t *testing.T) {
	_, text := callWith(t, "GET "+tsPrefix+"2026-10-09", m{"mostRecentTrainingLoadBalance": m{"metricsTrainingLoadBalanceDTOMap": nil}},
		"get_training_load_balance", m{"date": "2026-10-09"})
	if text != "No training load balance data found for 2026-10-09." {
		t.Error(text)
	}
}

// Extra: a band without a target range has no status; the request error is reported.
func TestGetTrainingLoadBalancePartialBandsAndError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+tsPrefix+"2026-10-09", m{"mostRecentTrainingLoadBalance": m{"metricsTrainingLoadBalanceDTOMap": m{
		"7": m{"monthlyLoadAerobicLow": 12.345, "monthlyLoadAnaerobicTargetMin": 3},
	}}})
	data := callMap(t, f, "get_training_load_balance", m{"date": "2026-10-09"})
	eq(t, "result", data, m{"date": "2026-10-09", "load_device_id": 7, "aerobic_low": m{"load": 12.3}, "anaerobic": m{"target_min": 3}})

	f.on("GET "+tsPrefix+"2026-10-10", status(500, m{"message": "boom"}))
	text, isErr := callTool(t, newSession(t, f, nil), "get_training_load_balance", m{"date": "2026-10-10"})
	if !isErr || !strings.HasPrefix(text, "Error retrieving training load balance: ") {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestTrainingLoadTrendStopsAfterDeviceAuthFailure(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(devicesRoute, status(401, m{"message": "expired"}))
	text, isErr := callTool(t, newSession(t, f, nil), "get_training_load_trend", m{"start_date": "2024-01-13", "end_date": "2024-01-15"})
	if !isErr || text != expiredCopy(testEmail) { // the expired-session copy replaces "...device data: API Error 401 - expired"
		t.Errorf("%v %q", isErr, text)
	}
	if n := countPrefix(f, tsPrefix); n != 0 {
		t.Errorf("%d status requests", n)
	}
}

// Extra: the same for a server error, whose text is kept.
func TestTrainingLoadTrendStopsAfterDeviceOutage(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(devicesRoute, status(503, m{"message": "down"}))
	text, isErr := callTool(t, newSession(t, f, nil), "get_training_load_trend", m{"start_date": "2024-01-13", "end_date": "2024-01-15"})
	if !isErr || text != "Error retrieving training load device data: API Error 503 - down" {
		t.Errorf("%v %q", isErr, text)
	}
	if n := countPrefix(f, tsPrefix); n != 0 {
		t.Errorf("%d status requests", n)
	}
}

func TestTrainingLoadTrendSurfacesReadinessRateLimit(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(devicesRoute, []any{m{"deviceId": 100, "primaryDevice": true}})
	onDays(f, tsPrefix, m{"mostRecentTrainingStatus": m{"latestTrainingStatusData": m{"200": m{"deviceId": 200}}}}, "2024-01-13", "2024-01-14", "2024-01-15")
	f.on("GET "+readinessPath+"2024-01-13", status(429, m{"message": "rate limited"}))
	text, isErr := callTool(t, newSession(t, f, nil), "get_training_load_trend", m{"start_date": "2024-01-13", "end_date": "2024-01-15"})
	if !isErr || text != "Error retrieving training load data for 2024-01-13: API Error 429 - rate limited" {
		t.Errorf("%v %q", isErr, text)
	}
	if n := countPrefix(f, readinessPath); n != 1 {
		t.Errorf("%d readiness requests", n)
	}
}

// Extra: a non-fatal readiness failure is kept in the entry and the trend goes on.
func TestTrainingLoadTrendKeepsNonFatalReadinessError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(devicesRoute, []any{m{"deviceId": 100, "primaryDevice": true}})
	f.on("GET "+tsPrefix+"2024-01-13", m{"mostRecentTrainingStatus": m{"latestTrainingStatusData": m{"200": m{"deviceId": 200}}}})
	f.on("GET "+readinessPath+"2024-01-13", status(404, m{"message": "gone"}))
	data := callMap(t, f, "get_training_load_trend", m{"start_date": "2024-01-13", "end_date": "2024-01-13"})
	entry := asMap(asList(data["trend"])[0])
	if entry["readiness_error"] != "API Error 404 - gone" || entry["atl_source"] != "unavailable" || entry["atl_unavailable_reason"] != "readiness_unavailable" {
		t.Errorf("%v", entry)
	}
}

// Extra: argument validation of the trend tools (texts from the Python source).
func TestTrendRangeValidation(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	for _, tool := range []string{"get_training_load_trend", "get_hrv_trend", "get_vo2max_trend", "get_respiration_trend", "get_running_tolerance_trend"} {
		for args, want := range map[string]string{
			"2024-01-15|2024-01-01": "end_date must be on or after start_date.",
			"2024-13-01|2024-01-01": "Invalid date format: month must be in 1..12. Use YYYY-MM-DD.",
			"nope|2024-01-01":       "Invalid date format: Invalid isoformat string: 'nope'. Use YYYY-MM-DD.",
			"2024-01-01|2025-12-31": "Date range too large (731 days). Maximum is ",
		} {
			parts := strings.Split(args, "|")
			text := callOK(t, cs, tool, m{"start_date": parts[0], "end_date": parts[1]})
			if !strings.HasPrefix(text, want) {
				t.Errorf("%s %s: %q", tool, args, text)
			}
		}
	}
	noRequests(t, f, "GET")
}

func TestHrvTrendUsesReportedNightlyAverages(t *testing.T) {
	f := newFakeGarmin(t)
	onEach(f, hrvPrefix, []string{"2026-09-04", "2026-09-05"},
		m{"hrvSummary": m{"lastNightAvg": 54, "lastNight": 999, "weeklyAvg": 60}},
		m{"hrvSummary": m{"lastNightAvg": 70, "lastNight5MinHigh": 107}})
	data := callMap(t, f, "get_hrv_trend", m{"start_date": "2026-09-04", "end_date": "2026-09-05"})
	trend := asList(data["trend"])
	if data["days_with_data"] != 2.0 || data["period_avg_hrv_ms"] != 62.0 || asMap(trend[0])["last_night_avg_hrv_ms"] != 54.0 ||
		asMap(trend[1])["last_night_avg_hrv_ms"] != 70.0 || asMap(trend[0])["weekly_avg_hrv_ms"] != 60.0 || asMap(trend[1])["last_night_5min_high_hrv_ms"] != 107.0 {
		t.Errorf("%v", data)
	}
}

func TestHrvTrendAveragesOnlyAvailableNights(t *testing.T) {
	f := newFakeGarmin(t)
	onEach(f, hrvPrefix, datesBetween("2026-09-01", "2026-09-05"),
		m{"hrvSummary": m{"lastNightAvg": 54}}, nil, m{"hrvSummary": m{"lastNightAvg": nil, "weeklyAvg": 60}}, m{"hrvSummary": nil}, m{"hrvSummary": m{"lastNightAvg": 71}})
	data := callMap(t, f, "get_hrv_trend", m{"start_date": "2026-09-01", "end_date": "2026-09-05"})
	if data["days_with_data"] != 3.0 || data["period_avg_hrv_ms"] != 62.5 {
		t.Errorf("%v", data)
	}
	if _, ok := asMap(asList(data["trend"])[1])["last_night_avg_hrv_ms"]; ok {
		t.Error("second trend entry must have no nightly average")
	}
}

func TestHrvTrendWithoutNightlyValues(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+hrvPrefix+"2026-09-05", m{"hrvSummary": m{"weeklyAvg": 60}})
	data := callMap(t, f, "get_hrv_trend", m{"start_date": "2026-09-05", "end_date": "2026-09-05"})
	v, ok := data["period_avg_hrv_ms"]
	if !ok || v != nil {
		t.Errorf("period_avg_hrv_ms = %v (present %v), want null", v, ok)
	}
	eq(t, "trend", data["trend"], []any{m{"date": "2026-09-05", "weekly_avg_hrv_ms": 60}})
}

// Extra: no data at all, with status and feedback kept when present.
func TestHrvTrendStatusAndNoData(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+hrvPrefix+"2026-09-05", m{"hrvSummary": m{"status": "BALANCED", "feedbackPhrase": "HRV_BALANCED_2"}})
	data := callMap(t, f, "get_hrv_trend", m{"start_date": "2026-09-05", "end_date": "2026-09-05"})
	eq(t, "trend", data["trend"], []any{m{"date": "2026-09-05", "status": "BALANCED", "feedback": "HRV_BALANCED_2"}})
	f.on("GET "+hrvPrefix+"2026-09-06", nil)
	text := callOK(t, newSession(t, f, nil), "get_hrv_trend", m{"start_date": "2026-09-06", "end_date": "2026-09-06"})
	if text != "No HRV data found between 2026-09-06 and 2026-09-06." {
		t.Error(text)
	}
}

func TestGetRespirationTrendTool(t *testing.T) {
	f := newFakeGarmin(t)
	onEach(f, respPrefix, []string{"2024-01-14", "2024-01-15", "2024-01-16"},
		m{"avgWakingRespirationValue": 14.26, "avgSleepRespirationValue": 12.04, "highestRespirationValue": 20, "lowestRespirationValue": 8.5},
		nil, m{"avgSleepRespirationValue": 13})
	data := callMap(t, f, "get_respiration_trend", m{"start_date": "2024-01-14", "end_date": "2024-01-16"})
	eq(t, "result", data, m{
		"start_date": "2024-01-14", "end_date": "2024-01-16", "days_with_data": 2, "period_avg_sleep_breaths_per_min": 12.5,
		"trend": []any{
			m{"date": "2024-01-14", "avg_waking_breaths_per_min": 14.3, "avg_sleep_breaths_per_min": 12.0, "highest_breaths_per_min": 20, "lowest_breaths_per_min": 8.5},
			m{"date": "2024-01-16", "avg_sleep_breaths_per_min": 13},
		},
	})
	f2 := newFakeGarmin(t)
	f2.on("GET "+respPrefix+"2024-01-14", nil)
	text := callOK(t, newSession(t, f2, nil), "get_respiration_trend", m{"start_date": "2024-01-14", "end_date": "2024-01-14"})
	if text != "No respiration data found between 2024-01-14 and 2024-01-14." {
		t.Error(text)
	}
}

// The Python test mocks one client method raising a connection error for 401/429/503. The Go client raises *APIError.
func TestTrainingTrendsSurfaceUpstreamRequestFailures(t *testing.T) {
	tools := []struct {
		tool, route string
	}{
		{"get_training_load_trend", "GET " + tsPrefix + "2024-01-13"},
		{"get_hrv_trend", "GET " + hrvPrefix + "2024-01-13"},
		{"get_respiration_trend", "GET " + respPrefix + "2024-01-13"},
	}
	for _, tc := range tools {
		for _, code := range []int{401, 429, 503} {
			t.Run(tc.tool+"/"+strconv.Itoa(code), func(t *testing.T) {
				f := newFakeGarmin(t)
				f.on(tc.route, status(code, m{"message": "failed request"}))
				text, isErr := callTool(t, newSession(t, f, nil), tc.tool, m{"start_date": "2024-01-13", "end_date": "2024-01-15"})
				if !isErr {
					t.Fatalf("not an error: %s", text)
				}
				if code == 401 { // the expired-session copy replaces the text
					if text != expiredCopy(testEmail) {
						t.Errorf("text = %q", text)
					}
				} else if !strings.HasPrefix(text, "Error retrieving") || !strings.Contains(text, strconv.Itoa(code)) {
					t.Errorf("text = %q", text)
				}
				if n := len(f.requests(tc.route)); n != 1 {
					t.Errorf("%d requests", n)
				}
				if n := len(f.requests("")); n > 3 { // at most: the one status request, devices, token refresh
					t.Errorf("%d requests: the trend must stop after the failure", n)
				}
			})
		}
	}
}

func TestGetRunningToleranceTool(t *testing.T) {
	f, text := callWith(t, toleranceRoute, fixture(t, "MOCK_RUNNING_TOLERANCE_DAILY"), "get_running_tolerance", m{"date": "2024-01-15"})
	wantQuery(t, query(t, f, toleranceRoute), map[string]string{"startDate": "2024-01-15", "endDate": "2024-01-15", "aggregation": "daily"})
	data := resultMap(t, text)
	if data["date"] != "2024-01-15" || data["tolerance_km"] != 34.0 || data["acute_load_km"] != 25.0 || data["distance_km"] != 22.0 ||
		data["load_ratio"] != round(25000.0/22000.0, 2) || data["feedback_phrase"] != "MEDIUM_LOAD" {
		t.Error(text)
	}
}

func TestGetRunningToleranceEmptyResultIsNotADeviceVerdict(t *testing.T) {
	_, text := callWith(t, toleranceRoute, []any{}, "get_running_tolerance", m{"date": "2024-01-15"})
	if !strings.HasPrefix(text, "No running tolerance data found") || strings.Contains(text, "does not support") {
		t.Error(text)
	}
}

func TestGetRunningToleranceError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(toleranceRoute, status(500, m{"message": "API Error"}))
	text, isErr := callTool(t, newSession(t, f, nil), "get_running_tolerance", m{"date": "2024-01-15"})
	if !isErr || !strings.Contains(text, "Error retrieving running tolerance data") {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetRunningToleranceTrendDailySortsByDate(t *testing.T) {
	f, text := callWith(t, toleranceRoute, fixture(t, "MOCK_RUNNING_TOLERANCE_DAILY_TREND"), "get_running_tolerance_trend",
		m{"start_date": "2024-01-15", "end_date": "2024-01-16", "aggregation": "daily"})
	wantQuery(t, query(t, f, toleranceRoute), map[string]string{"startDate": "2024-01-15", "endDate": "2024-01-16", "aggregation": "daily"})
	data := resultMap(t, text)
	trend := asList(data["trend"])
	if data["aggregation"] != "daily" || data["data_points"] != 2.0 || asMap(trend[0])["date"] != "2024-01-15" || asMap(trend[1])["date"] != "2024-01-16" ||
		data["first_tolerance_km"] != 34.0 || data["latest_tolerance_km"] != 34.5 || data["tolerance_change_km"] != 0.5 || asMap(trend[0])["feedback_phrase"] != "MEDIUM_LOAD" {
		t.Error(text)
	}
	if _, ok := asMap(trend[0])["start_of_week"]; ok {
		t.Error("daily points have no start_of_week")
	}
}

func TestGetRunningToleranceTrendWeeklyDefault(t *testing.T) {
	f, text := callWith(t, toleranceRoute, fixture(t, "MOCK_RUNNING_TOLERANCE_WEEKLY"), "get_running_tolerance_trend",
		m{"start_date": "2024-01-02", "end_date": "2024-01-15"})
	wantQuery(t, query(t, f, toleranceRoute), map[string]string{"startDate": "2024-01-02", "endDate": "2024-01-15", "aggregation": "weekly"})
	data := resultMap(t, text)
	week := asMap(asList(data["trend"])[0])
	if data["aggregation"] != "weekly" || week["start_of_week"] != "2024-01-02" || week["end_of_week"] != "2024-01-08" || week["week_index"] != 1900.0 ||
		week["tolerance_km"] != 33.0 || week["acute_load_km"] != 26.0 || week["distance_km"] != 24.0 {
		t.Error(text)
	}
	if _, ok := week["feedback_phrase"]; ok {
		t.Error("weekly points have no feedback_phrase")
	}
}

func TestGetRunningToleranceTrendEmptyResultIsNotADeviceVerdict(t *testing.T) {
	_, text := callWith(t, toleranceRoute, []any{}, "get_running_tolerance_trend", m{"start_date": "2024-01-01", "end_date": "2024-01-07"})
	if !strings.HasPrefix(text, "No running tolerance data found") || strings.Contains(text, "does not support") {
		t.Error(text)
	}
}

func TestGetRunningToleranceTrendError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(toleranceRoute, status(500, m{"message": "API Error"}))
	text, isErr := callTool(t, newSession(t, f, nil), "get_running_tolerance_trend", m{"start_date": "2024-01-01", "end_date": "2024-01-07"})
	if !isErr || !strings.Contains(text, "Error retrieving running tolerance trend") {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetRunningToleranceTrendInvalidRange(t *testing.T) {
	f := newFakeGarmin(t)
	text := callOK(t, newSession(t, f, nil), "get_running_tolerance_trend", m{"start_date": "2024-01-15", "end_date": "2024-01-01"})
	if !strings.Contains(text, "end_date must be on or after start_date") {
		t.Error(text)
	}
	noRequests(t, f, "GET")
}

func TestGetRunningToleranceTrendDailyRangeTooLarge(t *testing.T) {
	f := newFakeGarmin(t)
	text := callOK(t, newSession(t, f, nil), "get_running_tolerance_trend", m{"start_date": "2024-01-01", "end_date": "2024-04-15", "aggregation": "daily"})
	if text != "Date range too large (106 days). Maximum is 90 days for daily aggregation." {
		t.Error(text)
	}
	noRequests(t, f, "GET")
}

// Extra: Python's Literal["daily","weekly"] rejects other values; the weekly cap is 366 days.
func TestGetRunningToleranceTrendAggregationAndWeeklyCap(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	text, isErr := callTool(t, cs, "get_running_tolerance_trend", m{"start_date": "2024-01-01", "end_date": "2024-01-07", "aggregation": "monthly"})
	if !isErr || !strings.Contains(text, "'daily' or 'weekly'") {
		t.Errorf("%v %q", isErr, text)
	}
	text = callOK(t, cs, "get_running_tolerance_trend", m{"start_date": "2023-01-01", "end_date": "2024-01-02"})
	if text != "Date range too large (367 days). Maximum is 366 days for weekly aggregation." {
		t.Error(text)
	}
	noRequests(t, f, "GET")
}

func TestGetAcclimationReturnsHeatData(t *testing.T) {
	f, text := callWith(t, "GET "+maxMetricsDay+"2024-07-15/2024-07-15", []any{m{"heatAltitudeAcclimation": m{
		"calendarDate": "2024-07-15", "heatAcclimationPercentage": 72.5, "previousHeatAcclimationPercentage": 65.0, "heatTrend": "ACCLIMATIZED",
		"heatAcclimationDate": "2024-07-15", "altitudeAcclimation": 1200, "altitudeTrend": "INCREASING", "currentAltitude": 850,
	}}}, "get_acclimation", m{"date": "2024-07-15"})
	eq(t, "result", decode(t, text), m{
		"date": "2024-07-15", "heat_acclimation_percent": 72.5, "previous_heat_acclimation_percent": 65.0, "heat_trend": "ACCLIMATIZED",
		"heat_acclimation_date": "2024-07-15", "altitude_acclimation_meters": 1200, "altitude_trend": "INCREASING", "current_altitude_meters": 850,
		"heat_acclimation_change": 7.5,
	})
	if n := len(f.requests("GET " + maxMetricsDay + "2024-07-15/2024-07-15")); n != 1 {
		t.Errorf("%d requests", n)
	}
}

func TestGetAcclimationNoData(t *testing.T) {
	_, text := callWith(t, "GET "+maxMetricsDay+"2024-07-15/2024-07-15", []any{m{"generic": m{"calendarDate": "2024-07-15", "vo2MaxValue": 48.0}}},
		"get_acclimation", m{"date": "2024-07-15"})
	if !strings.Contains(text, "No acclimation data") || !strings.Contains(text, "Garmin populates this only after outdoor activities in heat or at altitude.") {
		t.Error(text)
	}
	_, text = callWith(t, "GET "+maxMetricsDay+"2024-07-15/2024-07-15", []any{}, "get_acclimation", m{"date": "2024-07-15"})
	if text != "No acclimation data found for 2024-07-15." {
		t.Error(text)
	}
}

func TestGetAcclimationException(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+maxMetricsDay+"2024-07-15/2024-07-15", status(500, m{"message": "API Error"}))
	text, isErr := callTool(t, newSession(t, f, nil), "get_acclimation", m{"date": "2024-07-15"})
	if !isErr || !strings.Contains(text, "Error") {
		t.Errorf("%v %q", isErr, text)
	}
}

// The trend tool against the HTTP fake. The Python tests that need a client without the range endpoint are in
// vo2_trend_test.go (a fake vo2Source), because the Go client always has it.

const maxRange = "GET " + maxMetricsDay + "2024-01-14/2024-01-15"

func TestGetVo2maxTrendFallsBackToProfile(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(maxRange, []any{})
	onDays(f, tsPrefix, m{}, "2024-01-14", "2024-01-15")
	f.on(profileRoute, m{"userData": m{"vo2MaxRunning": 28.0}})
	data := callMap(t, f, "get_vo2max_trend", m{"start_date": "2024-01-14", "end_date": "2024-01-15"})
	if data["data_points"] != 0.0 || data["first_vo2_max"] != nil || data["latest_vo2_max"] != nil || data["change"] != nil || !reflect.DeepEqual(data["trend"], []any{}) {
		t.Errorf("%v", data)
	}
	eq(t, "estimate", data["current_vo2_max_estimate"], m{"vo2_max": 28.0, "sport": "running", "source": "get_user_profile"})
	if !strings.Contains(data["note"].(string), "Historical VO2 max values were not available") {
		t.Errorf("note = %v", data["note"])
	}
	if n := countPrefix(f, tsPrefix); n != 2 {
		t.Errorf("%d status requests", n)
	}
	if len(f.requests(maxRange)) != 1 || len(f.requests(profileRoute)) != 1 {
		t.Error("range and profile must each be fetched once")
	}
	if n := countPrefix(f, maxMetricsDay); n != 1 {
		t.Errorf("%d max-metrics requests (no per-day get_max_metrics)", n)
	}
}

func TestGetVo2maxTrendUsesDailyMetrics(t *testing.T) {
	f := newFakeGarmin(t)
	onDays(f, tsPrefix, m{"mostRecentVO2Max": m{"generic": m{"vo2MaxValue": 99.0}}}, "2024-01-14", "2024-01-15")
	f.on(maxRange, []any{
		m{"generic": m{"calendarDate": "2024-01-14", "vo2MaxValue": 27.5}},
		m{"generic": m{"calendarDate": "2024-01-15", "vo2MaxValue": 28.0}},
	})
	data := callMap(t, f, "get_vo2max_trend", m{"start_date": "2024-01-14", "end_date": "2024-01-15"})
	if data["latest_vo2_max"] != 28.0 || data["change"] != 0.5 || data["sport"] != "running" {
		t.Errorf("%v", data)
	}
	eq(t, "trend", data["trend"], []any{
		m{"date": "2024-01-14", "vo2_max": 27.5, "source": "get_max_metrics"},
		m{"date": "2024-01-15", "vo2_max": 28.0, "source": "get_max_metrics"},
	})
	if countPrefix(f, tsPrefix) != 0 || len(f.requests(profileRoute)) != 0 || len(f.requests(maxRange)) != 1 {
		t.Errorf("unexpected requests: %v", f.requests(""))
	}
}

func TestGetVo2maxTrendCarriesForwardActivityDayValues(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+maxMetricsDay+"2024-01-12/2024-01-16", []any{
		m{"generic": m{"calendarDate": "2024-01-13", "vo2MaxValue": 48.0, "vo2MaxPreciseValue": 47.6}},
		m{"generic": m{"calendarDate": "2024-01-15", "vo2MaxValue": 49.0, "vo2MaxPreciseValue": 48.7}},
	})
	onDays(f, tsPrefix, m{}, datesBetween("2024-01-12", "2024-01-16")...)
	data := callMap(t, f, "get_vo2max_trend", m{"start_date": "2024-01-12", "end_date": "2024-01-16"})
	if data["sport"] != "running" || data["data_points"] != 4.0 || data["first_vo2_max"] != 47.6 || data["latest_vo2_max"] != 48.7 || data["change"] != 1.1 {
		t.Errorf("%v", data)
	}
	eq(t, "trend", data["trend"], []any{
		m{"date": "2024-01-13", "vo2_max": 47.6, "source": "get_max_metrics"},
		m{"date": "2024-01-14", "vo2_max": 47.6, "source": "get_max_metrics", "carried_forward": true},
		m{"date": "2024-01-15", "vo2_max": 48.7, "source": "get_max_metrics"},
		m{"date": "2024-01-16", "vo2_max": 48.7, "source": "get_max_metrics", "carried_forward": true},
	})
	// Only the days missing from the range response trigger a status call.
	if n := countPrefix(f, tsPrefix); n != 3 {
		t.Errorf("%d status requests, want 3", n)
	}
}

func TestGetVo2maxTrendPrefersTrainingStatus(t *testing.T) {
	f := newFakeGarmin(t)
	onEach(f, tsPrefix, []string{"2024-01-14", "2024-01-15"},
		m{"mostRecentVO2Max": m{"generic": m{"vo2MaxValue": 48.0}}}, m{"mostRecentVO2Max": m{"generic": m{"vo2MaxValue": 48.5}}})
	data := callMap(t, f, "get_vo2max_trend", m{"start_date": "2024-01-14", "end_date": "2024-01-15"})
	if data["latest_vo2_max"] != 48.5 || data["sport"] != "running" {
		t.Errorf("%v", data)
	}
	for _, p := range asList(data["trend"]) {
		if asMap(p)["source"] != "get_training_status" {
			t.Errorf("point = %v", p)
		}
	}
	if n := countPrefix(f, tsPrefix); n != 2 {
		t.Errorf("%d status requests", n)
	}
}

func TestGetVo2maxTrendPreservesSelectedCyclingFromMixedPayloads(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+maxMetricsDay+"2024-01-13/2024-01-15", []any{})
	onEach(f, tsPrefix, datesBetween("2024-01-13", "2024-01-15"),
		m{"mostRecentVO2Max": m{"cycling": m{"vo2MaxValue": 55.0}}},
		m{"mostRecentVO2Max": m{"generic": m{"vo2MaxValue": 48.0}, "cycling": m{"vo2MaxValue": 56.0}}},
		m{"mostRecentVO2Max": m{"generic": m{"vo2MaxValue": 48.5}, "cycling": m{"vo2MaxValue": 57.0}}})
	data := callMap(t, f, "get_vo2max_trend", m{"start_date": "2024-01-13", "end_date": "2024-01-15"})
	if data["sport"] != "cycling" || data["change"] != 2.0 {
		t.Errorf("%v", data)
	}
	eq(t, "trend", data["trend"], []any{
		m{"date": "2024-01-13", "vo2_max": 55.0, "source": "get_training_status"},
		m{"date": "2024-01-14", "vo2_max": 56.0, "source": "get_training_status"},
		m{"date": "2024-01-15", "vo2_max": 57.0, "source": "get_training_status"},
	})
}

func TestGetVo2maxTrendSelectsSportWithMostHistory(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+maxMetricsDay+"2024-01-13/2024-01-15", []any{})
	onEach(f, tsPrefix, datesBetween("2024-01-13", "2024-01-15"),
		m{"mostRecentVO2Max": m{"cycling": m{"vo2MaxValue": 55.0}}},
		m{"mostRecentVO2Max": m{"generic": m{"vo2MaxValue": 48.0}}},
		m{"mostRecentVO2Max": m{"generic": m{"vo2MaxValue": 48.5}}})
	data := callMap(t, f, "get_vo2max_trend", m{"start_date": "2024-01-13", "end_date": "2024-01-15"})
	if data["sport"] != "running" || data["data_points"] != 2.0 {
		t.Errorf("%v", data)
	}
	var values []any
	for _, p := range asList(data["trend"]) {
		values = append(values, asMap(p)["vo2_max"])
	}
	if !reflect.DeepEqual(values, []any{48.0, 48.5}) {
		t.Errorf("values = %v", values)
	}
}

func TestGetVo2maxTrendUsesCyclingRangeMetrics(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+maxMetricsDay+"2024-01-15/2024-01-15", []any{m{"cycling": m{"calendarDate": "2024-01-15", "vo2MaxValue": 55.1}}})
	data := callMap(t, f, "get_vo2max_trend", m{"start_date": "2024-01-15", "end_date": "2024-01-15"})
	if data["sport"] != "cycling" {
		t.Errorf("%v", data)
	}
	eq(t, "trend", data["trend"], []any{m{"date": "2024-01-15", "vo2_max": 55.1, "source": "get_max_metrics"}})
	if countPrefix(f, tsPrefix) != 0 {
		t.Error("no status calls expected")
	}
}

func TestGetVo2maxTrendRangeFailureDoesNotRetryMaxMetricsDaily(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(maxRange, status(400, m{"message": "API unavailable"}))
	onDays(f, tsPrefix, m{"mostRecentVO2Max": m{"generic": m{"vo2MaxValue": 48.0}}}, "2024-01-14", "2024-01-15")
	data := callMap(t, f, "get_vo2max_trend", m{"start_date": "2024-01-14", "end_date": "2024-01-15"})
	if data["sport"] != "running" {
		t.Errorf("%v", data)
	}
	if n := countPrefix(f, tsPrefix); n != 2 {
		t.Errorf("%d status requests", n)
	}
	if n := countPrefix(f, maxMetricsDay); n != 1 {
		t.Errorf("%d max-metrics requests", n)
	}
}

func TestGetVo2maxTrendPrefersRunningWhenSportCoverageIsTied(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+maxMetricsDay+"2024-01-15/2024-01-15", []any{})
	f.on("GET "+tsPrefix+"2024-01-15", m{"mostRecentVO2Max": m{"generic": m{"vo2MaxValue": 48.0}, "cycling": m{"vo2MaxValue": 55.0}}})
	data := callMap(t, f, "get_vo2max_trend", m{"start_date": "2024-01-15", "end_date": "2024-01-15"})
	if data["sport"] != "running" || data["latest_vo2_max"] != 48.0 {
		t.Errorf("%v", data)
	}
}

func TestGetVo2maxTrendNoHistoryAvoidsDailyMaxMetricsRequests(t *testing.T) {
	f := newFakeGarmin(t)
	dates := datesBetween("2024-01-01", "2024-03-30")
	f.on("GET "+maxMetricsDay+"2024-01-01/2024-03-30", []any{})
	onDays(f, tsPrefix, m{}, dates...)
	f.on(profileRoute, m{"userData": m{"vo2MaxRunning": 48.0}})
	data := callMap(t, f, "get_vo2max_trend", m{"start_date": "2024-01-01", "end_date": "2024-03-30"})
	if data["data_points"] != 0.0 {
		t.Errorf("%v", data)
	}
	if n := countPrefix(f, tsPrefix); n != 90 {
		t.Errorf("%d status requests, want 90", n)
	}
	if n := countPrefix(f, maxMetricsDay); n != 1 {
		t.Errorf("%d max-metrics requests, want 1", n)
	}
	if n := len(f.requests(profileRoute)); n != 1 {
		t.Errorf("%d profile requests", n)
	}
}

func TestVo2TrendStopsAfterRangeAuthFailure(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+maxMetricsDay+"2024-01-13/2024-01-15", status(401, m{"message": "expired"}))
	text, isErr := callTool(t, newSession(t, f, nil), "get_vo2max_trend", m{"start_date": "2024-01-13", "end_date": "2024-01-15"})
	if !isErr || text != expiredCopy(testEmail) { // the expired-session copy replaces "Error retrieving VO2 max data: ..."
		t.Errorf("%v %q", isErr, text)
	}
	if countPrefix(f, tsPrefix) != 0 || len(f.requests(profileRoute)) != 0 {
		t.Errorf("must stop after the range failure: %v", f.requests(""))
	}
}

// Extra: the same for an outage, whose text is kept.
func TestVo2TrendStopsAfterRangeOutage(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+maxMetricsDay+"2024-01-13/2024-01-15", status(503, m{"message": "down"}))
	text, isErr := callTool(t, newSession(t, f, nil), "get_vo2max_trend", m{"start_date": "2024-01-13", "end_date": "2024-01-15"})
	if !isErr || text != "Error retrieving VO2 max data: API Error 503 - down" {
		t.Errorf("%v %q", isErr, text)
	}
	if countPrefix(f, tsPrefix) != 0 {
		t.Error("must stop after the range failure")
	}
}

func TestVo2TrendRetainsMeasurementDatesAndMarksCarriedDays(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+maxMetricsDay+"2024-01-13/2024-01-16", []any{
		m{"generic": m{"calendarDate": "2024-01-13", "vo2MaxPreciseValue": 47.6}},
		m{"generic": m{"calendarDate": "2024-01-15", "vo2MaxPreciseValue": 48.7}},
	})
	for _, d := range datesBetween("2024-01-13", "2024-01-16") {
		date, value := "2024-01-13", 47.6
		if d >= "2024-01-15" {
			date, value = "2024-01-15", 48.7
		}
		f.on("GET "+tsPrefix+d, m{"mostRecentVO2Max": m{"generic": m{"calendarDate": date, "vo2MaxPreciseValue": value}}})
	}
	data := callMap(t, f, "get_vo2max_trend", m{"start_date": "2024-01-13", "end_date": "2024-01-16"})
	if data["data_points"] != 4.0 { // counts displayed days, including carried values
		t.Errorf("%v", data)
	}
	var carried []bool
	for _, p := range asList(data["trend"]) {
		carried = append(carried, asMap(p)["carried_forward"] == true)
		if asMap(p)["source"] != "get_max_metrics" {
			t.Errorf("point = %v", p)
		}
	}
	if !reflect.DeepEqual(carried, []bool{false, true, false, true}) {
		t.Errorf("carried = %v", carried)
	}
}

func TestVo2TrendCarriesPreviousMeasurementIntoRequestedRange(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+maxMetricsDay+"2024-01-13/2024-01-15", []any{})
	onDays(f, tsPrefix, m{"mostRecentVO2Max": m{"cycling": m{"calendarDate": "2024-01-10", "vo2MaxPreciseValue": 55.1}}}, datesBetween("2024-01-13", "2024-01-15")...)
	data := callMap(t, f, "get_vo2max_trend", m{"start_date": "2024-01-13", "end_date": "2024-01-15"})
	if data["sport"] != "cycling" {
		t.Errorf("%v", data)
	}
	var dates []string
	for _, p := range asList(data["trend"]) {
		pt := asMap(p)
		dates = append(dates, pt["date"].(string))
		if pt["carried_forward"] != true || pt["vo2_max"] != 55.1 {
			t.Errorf("point = %v", pt)
		}
	}
	if !reflect.DeepEqual(dates, []string{"2024-01-13", "2024-01-14", "2024-01-15"}) {
		t.Errorf("dates = %v", dates)
	}
}

func TestVo2TrendReportsEndpointFailureWhenFallbacksAreEmpty(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+maxMetricsDay+"2024-01-13/2024-01-15", status(400, m{"message": "range unavailable"}))
	onDays(f, tsPrefix, m{}, datesBetween("2024-01-13", "2024-01-15")...)
	f.on(profileRoute, m{})
	text, isErr := callTool(t, newSession(t, f, nil), "get_vo2max_trend", m{"start_date": "2024-01-13", "end_date": "2024-01-15"})
	if !isErr || text != "Error retrieving VO2 max data: API Error 400 - range unavailable" {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestVo2TrendDisclosesRecoveredEndpointFailure(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+maxMetricsDay+"2024-01-13/2024-01-15", status(400, m{"message": "range unavailable"}))
	onDays(f, tsPrefix, m{"mostRecentVO2Max": m{"generic": m{"vo2MaxValue": 48}}}, datesBetween("2024-01-13", "2024-01-15")...)
	data := callMap(t, f, "get_vo2max_trend", m{"start_date": "2024-01-13", "end_date": "2024-01-15"})
	if data["latest_vo2_max"] != 48.0 {
		t.Errorf("%v", data)
	}
	eq(t, "request_errors", data["request_errors"], []any{m{"source": "get_max_metrics", "error": "API Error 400 - range unavailable"}})
}

// Extra: a bad payload date is an error, not a panic.
func TestVo2TrendRejectsMalformedMeasurementDate(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+maxMetricsDay+"2024-01-15/2024-01-15", []any{})
	f.on("GET "+tsPrefix+"2024-01-15", m{"mostRecentVO2Max": m{"generic": m{"calendarDate": "2024-01-14T10:00:00", "vo2MaxValue": 50.0}}})
	text, isErr := callTool(t, newSession(t, f, nil), "get_vo2max_trend", m{"start_date": "2024-01-15", "end_date": "2024-01-15"})
	if !isErr || !strings.Contains(text, "Invalid isoformat string") {
		t.Errorf("%v %q", isErr, text)
	}
}

var _ = http.StatusOK
var _ garmin.Tokens

// Extra: the no-data texts are exact and carry no formatting artifacts, whatever empty value Garmin sends.
func TestTrainingNoDataMessagesAreWellFormed(t *testing.T) {
	cases := []struct {
		tool  string
		args  m
		route string
		want  string
	}{
		{"get_hill_score", m{"start_date": "2024-01-08", "end_date": "2024-01-15"}, hillRoute, "No hill score data found between 2024-01-08 and 2024-01-15."},
		{"get_endurance_score", m{"start_date": "2024-01-08", "end_date": "2024-01-15"}, enduranceRoute, "No endurance score data found between 2024-01-08 and 2024-01-15."},
		{"get_progress_summary_between_dates", m{"start_date": "2024-01-08", "end_date": "2024-01-15", "metric": "distance"}, progressRoute,
			"No progress summary found for distance between 2024-01-08 and 2024-01-15."},
		{"get_hrv_data", m{"date": "2024-01-15"}, "GET " + hrvPrefix + "2024-01-15", "No HRV data found for 2024-01-15."},
		{"get_fitnessage_data", m{"date": "2024-01-15"}, "GET /fitnessage-service/fitnessage/2024-01-15", "No fitness age data found for 2024-01-15."},
		{"get_training_status", m{"date": "2024-01-15"}, "GET " + tsPrefix + "2024-01-15", "No training status data found for 2024-01-15."},
		{"get_training_load_balance", m{"date": "2024-01-15"}, "GET " + tsPrefix + "2024-01-15", "No training load balance data found for 2024-01-15."},
		{"get_running_tolerance", m{"date": "2024-01-15"}, toleranceRoute, "No running tolerance data found for 2024-01-15."},
		{"get_running_tolerance_trend", m{"start_date": "2024-01-08", "end_date": "2024-01-15"}, toleranceRoute,
			"No running tolerance data found between 2024-01-08 and 2024-01-15."},
		{"get_acclimation", m{"date": "2024-01-15"}, "GET " + maxMetricsDay + "2024-01-15/2024-01-15", "No acclimation data found for 2024-01-15."},
		{"get_training_load_trend", m{"start_date": "2024-01-15", "end_date": "2024-01-15"}, "GET " + tsPrefix + "2024-01-15",
			"No training load data found between 2024-01-15 and 2024-01-15."},
		{"get_vo2max_trend", m{"start_date": "2024-01-15", "end_date": "2024-01-15"}, "GET " + tsPrefix + "2024-01-15",
			"No VO2 max data found between 2024-01-15 and 2024-01-15."},
	}
	for _, tc := range cases {
		for _, empty := range []any{nil, m{}, []any{}} {
			f := newFakeGarmin(t)
			f.on(tc.route, empty)
			f.on("GET "+maxMetricsDay+"2024-01-15/2024-01-15", []any{})
			f.on(typesURL, []any{})
			text := callOK(t, newSession(t, f, nil), tc.tool, tc.args)
			if strings.Contains(text, "%!") || text != tc.want {
				t.Errorf("%s (%v): %q, want %q", tc.tool, empty, text, tc.want)
			}
		}
	}
}

func TestTrainingToolSchemas(t *testing.T) {
	cs := newSession(t, newFakeGarmin(t), nil)
	want := map[string][]string{ // required properties besides email
		"get_progress_summary_between_dates": {"start_date", "end_date", "metric"},
		"get_training_effect":                {"activity_id"},
		"get_hrv_data":                       {"date"},
		"get_lactate_threshold":              {},
		"get_cycling_ftp":                    {},
		"get_running_tolerance_trend":        {"start_date", "end_date"},
		"get_vo2max_trend":                   {"start_date", "end_date"},
	}
	seen := 0
	for _, tool := range listTools(t, cs) {
		req, ok := want[tool.Name]
		if !ok {
			continue
		}
		seen++
		schema := asMap(tool.InputSchema)
		if schema["additionalProperties"] != false {
			t.Errorf("%s allows extra properties", tool.Name)
		}
		got := map[string]bool{}
		for _, r := range asList(schema["required"]) {
			got[pyStr(r)] = true
		}
		if len(got) != len(req)+1 || !got["email"] {
			t.Errorf("%s required = %v, want email + %v", tool.Name, got, req)
		}
		for _, r := range req {
			if !got[r] {
				t.Errorf("%s: %s must be required", tool.Name, r)
			}
		}
	}
	if seen != len(want) {
		t.Errorf("saw %d of %d tools", seen, len(want))
	}
}
