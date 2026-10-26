package server

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

const (
	displayName           = "testuser"
	userSummaryPath       = "/usersummary-service/usersummary/daily/" + displayName
	stepsChartPath        = "/wellness-service/wellness/dailySummaryChart/" + displayName
	floorsPath            = "/wellness-service/wellness/floorsChartData/daily/2024-01-15"
	dailyStepsPath        = "/usersummary-service/stats/steps/daily/2024-01-08/2024-01-15"
	bodyCompositionPath   = "/weight-service/weight/dateRange"
	heartRatesPath        = "/wellness-service/wellness/dailyHeartRate/" + displayName
	sleepPath             = "/wellness-service/wellness/dailySleepData/" + displayName
	rhrPath               = "/userstats-service/wellness/daily/" + displayName
	trainingReadinessPath = "/metrics-service/metrics/trainingreadiness/2024-01-15"
	bodyBatteryPath       = "/wellness-service/wellness/bodyBattery/reports/daily"
	bodyBatteryEventsPath = "/wellness-service/wellness/bodyBattery/events/2024-01-15"
	bloodPressurePath     = "/bloodpressure-service/bloodpressure/range/2024-01-08/2024-01-15"
	hydrationPath         = "/usersummary-service/usersummary/hydration/daily/2024-01-15"
	stressPath            = "/wellness-service/wellness/dailyStress/2024-01-15"
	respirationPath       = "/wellness-service/wellness/daily/respiration/2024-01-15"
	spo2Path              = "/wellness-service/wellness/daily/spo2/2024-01-15"
	allDayEventsPath      = "/wellness-service/wellness/dailyEvents"
	lifestylePath         = "/lifestylelogging-service/dailyLog/2024-01-15"
	weeklyStepsPath       = "/usersummary-service/stats/steps/weekly/2024-01-10/4"
	weeklyStressPath      = "/usersummary-service/stats/stress/weekly/2024-01-10/4"
	weeklyIntensityPath   = "/usersummary-service/stats/im/weekly/2023-12-28/2024-01-10"
	weeklyIntensityPath4  = "/usersummary-service/stats/im/weekly/2023-12-14/2024-01-10"
	dailyStatsPath        = "/usersummary-service/stats/daily"
	nutritionRangePath    = "/nutrition-service/food/logs/range"
	activitiesSearchPath  = "/activitylist-service/activities/search/activities"
	morningReadinessPath  = "/metrics-service/metrics/trainingreadiness/2024-01-15"
	recoveryReadinessPath = "/metrics-service/metrics/trainingreadiness/2026-09-12"
	recoveryActivityPath  = "/activitylist-service/activities/search/activities"
)

func dayFromNow(days int) string {
	return time.Now().AddDate(0, 0, days).Format("2006-01-02")
}

func mutMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("value is %T, want map[string]any", v)
	}
	return m
}

func mutList(t *testing.T, v any) []any {
	t.Helper()
	l, ok := v.([]any)
	if !ok {
		t.Fatalf("value is %T, want []any", v)
	}
	return l
}

func num(t *testing.T, v any) float64 {
	t.Helper()
	f, ok := numberOf(v)
	if !ok {
		t.Fatalf("value %v (%T) is not numeric", v, v)
	}
	return f
}

func assertNum(t *testing.T, got any, want, tol float64) {
	t.Helper()
	g := num(t, got)
	if math.Abs(g-want) > tol {
		t.Fatalf("got %v, want %v (tol %v)", g, want, tol)
	}
}

func floatPtr(v float64) *float64 { return &v }

func healthClient(t *testing.T, f *fakeGarmin) *garmin.Client {
	t.Helper()
	st := newTestStore(t)
	seedAccount(t, st, "me@x.com", testJWT(nil))
	c, err := garmin.NewClient(garmin.Endpoints{ConnectAPI: f.URL}, &tokenStore{store: st, email: "me@x.com"}, nil)
	if err != nil {
		t.Fatalf("garmin.NewClient: %v", err)
	}
	return c
}

type intakeFixture struct {
	Date     string
	Calories any
	Items    int
}

type calorieFixture struct {
	Date  string
	Total any
}

func energyIntakeResponse(days []intakeFixture) map[string]any {
	summaries := make([]any, 0, len(days))
	for _, d := range days {
		loggedFoods := make([]any, d.Items)
		for i := range loggedFoods {
			loggedFoods[i] = map[string]any{}
		}
		entry := map[string]any{
			"mealDate":    d.Date,
			"mealDetails": []any{map[string]any{"loggedFoods": loggedFoods}},
		}
		if d.Calories != nil {
			entry["dailyNutritionContent"] = map[string]any{"calories": d.Calories}
		}
		summaries = append(summaries, entry)
	}
	return map[string]any{"dailyNutritionSummaries": summaries}
}

func energyCaloriesResponse(days []calorieFixture) map[string]any {
	values := make([]any, 0, len(days))
	for _, d := range days {
		values = append(values, map[string]any{
			"calendarDate": d.Date,
			"values":       map[string]any{"totalCalories": d.Total},
		})
	}
	return map[string]any{"values": values}
}

func bodyCompResponse(entries ...map[string]any) map[string]any {
	list := make([]any, 0, len(entries))
	for _, e := range entries {
		list = append(list, e)
	}
	return map[string]any{"dateWeightList": list}
}

func bodyCompEntry(date string, weight any, bodyFat any) map[string]any {
	return map[string]any{"calendarDate": date, "weight": weight, "bodyFat": bodyFat}
}

// --- get_stats ---

func TestGetStatsTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+userSummaryPath, fixture(t, "MOCK_STATS"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_stats", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if _, ok := got["date"]; ok {
		t.Fatalf("date should be pruned when absent, got %v", got["date"])
	}
	assertNum(t, got["total_steps"], 10000, 0)
	assertNum(t, got["active_calories"], 800, 0)
	assertNum(t, got["bmr_calories"], 1700, 0)
	assertNum(t, got["total_calories"], 2500, 0)
	if got["resting_heart_rate_bpm"] != 55.0 {
		t.Fatalf("resting_heart_rate_bpm = %v", got["resting_heart_rate_bpm"])
	}
	if calls := f.callsMatching("GET", userSummaryPath); len(calls) != 1 {
		t.Fatalf("user summary calls = %d, want 1", len(calls))
	}
}

func TestGetStatsPreservesZeroFloors(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+userSummaryPath, map[string]any{
		"calendarDate":    "2024-01-15",
		"totalSteps":      0,
		"floorsAscended":  0.0,
		"floorsDescended": 0,
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_stats", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	assertNum(t, got["floors_ascended"], 0, 0)
	assertNum(t, got["floors_descended"], 0, 0)
	assertNum(t, got["total_steps"], 0, 0)
}

// --- get_stats_range ---

func TestGetStatsRangeTool(t *testing.T) {
	today := dayFromNow(0)
	yesterday := dayFromNow(-1)
	caloriesResp := map[string]any{"values": []any{
		map[string]any{
			"calendarDate": yesterday,
			"values":       map[string]any{"restingCalories": 2081, "totalCalories": 2656, "activeCalories": 575},
		},
		map[string]any{
			"calendarDate": today,
			"values":       map[string]any{"restingCalories": 1393, "totalCalories": 1900, "activeCalories": 507},
		},
	}}
	stepsResp := map[string]any{"values": []any{
		map[string]any{"calendarDate": yesterday, "values": map[string]any{"totalSteps": 9238}},
	}}

	f := newFakeGarmin(t)
	f.on("GET "+dailyStatsPath+"/"+yesterday+"/"+today, sequence{caloriesResp, stepsResp})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_stats_range", map[string]any{
		"start_date": yesterday,
		"end_date":   today,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	days := map[string]map[string]any{}
	for _, d := range mutList(t, data["days"]) {
		day := mutMap(t, d)
		days[day["date"].(string)] = day
	}

	past := days[yesterday]
	assertNum(t, past["total_calories"], 2656, 0)
	assertNum(t, past["active_calories"], 575, 0)
	assertNum(t, past["resting_calories"], 2081, 0)
	assertNum(t, past["steps"], 9238, 0)
	if past["has_data"] != true {
		t.Fatalf("past has_data = %v", past["has_data"])
	}
	if past["is_partial"] != false {
		t.Fatalf("past is_partial = %v", past["is_partial"])
	}

	todayEntry := days[today]
	assertNum(t, todayEntry["total_calories"], 1900, 0)
	if todayEntry["steps"] != nil {
		t.Fatalf("today steps = %v, want nil", todayEntry["steps"])
	}
	if todayEntry["has_data"] != true {
		t.Fatalf("today has_data = %v", todayEntry["has_data"])
	}
	if todayEntry["is_partial"] != true {
		t.Fatalf("today is_partial = %v", todayEntry["is_partial"])
	}
}

func TestGetStatsRangeNoDataDay(t *testing.T) {
	path := dailyStatsPath + "/2024-01-14/2024-01-15"
	f := newFakeGarmin(t)
	f.on("GET "+path, sequence{
		map[string]any{"values": []any{
			map[string]any{
				"calendarDate": "2024-01-14",
				"values":       map[string]any{"restingCalories": 2081, "totalCalories": 2656, "activeCalories": 575},
			},
		}},
		map[string]any{"values": []any{
			map[string]any{"calendarDate": "2024-01-14", "values": map[string]any{"totalSteps": 9238}},
		}},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_stats_range", map[string]any{
		"start_date": "2024-01-14",
		"end_date":   "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	var missing map[string]any
	for _, d := range mutList(t, data["days"]) {
		day := mutMap(t, d)
		if day["date"] == "2024-01-15" {
			missing = day
		}
	}
	if missing == nil {
		t.Fatal("2024-01-15 not present")
	}
	if missing["has_data"] != false {
		t.Fatalf("has_data = %v", missing["has_data"])
	}
	for _, key := range []string{"total_calories", "active_calories", "resting_calories", "steps"} {
		if missing[key] != nil {
			t.Fatalf("%s = %v, want nil", key, missing[key])
		}
	}
}

func TestGetStatsRangeRejectsOversizedRange(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_stats_range", map[string]any{
		"start_date": "2024-01-01",
		"end_date":   "2024-03-01",
	})
	if !strings.Contains(text, "too large") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.recorded(); len(calls) != 0 {
		t.Fatalf("fake saw %d calls, want 0", len(calls))
	}
}

func TestGetStatsRangeRejectsInvertedRange(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_stats_range", map[string]any{
		"start_date": "2024-01-15",
		"end_date":   "2024-01-01",
	})
	if !strings.Contains(text, "end_date must be on or after start_date") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.recorded(); len(calls) != 0 {
		t.Fatalf("fake saw %d calls, want 0", len(calls))
	}
}

func TestGetStatsRangeError(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_stats_range", map[string]any{
		"start_date": "2024-01-14",
		"end_date":   "2024-01-15",
	})
	if !strings.Contains(text, "Error retrieving stats range") {
		t.Fatalf("text = %q", text)
	}
}

// --- get_energy_balance ---

func TestGetEnergyBalanceDerivesTdee(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionRangePath, energyIntakeResponse([]intakeFixture{
		{Date: "2024-01-01", Calories: nil, Items: 0},
		{Date: "2024-01-02", Calories: 2000, Items: 5},
		{Date: "2024-01-03", Calories: 2000, Items: 4},
	}))
	f.on("GET "+dailyStatsPath+"/2024-01-01/2024-01-03", energyCaloriesResponse([]calorieFixture{
		{Date: "2024-01-01", Total: 2500},
		{Date: "2024-01-02", Total: 2500},
	}))
	f.on("GET "+bodyCompositionPath, bodyCompResponse(
		bodyCompEntry("2024-01-01", 80000.0, 20.0),
		bodyCompEntry("2024-01-25", 79000.0, 18.75),
		bodyCompEntry("2024-01-05", 79500.0, nil),
	))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_energy_balance", map[string]any{
		"start_date": "2024-01-01",
		"end_date":   "2024-01-03",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)

	intake := mutMap(t, data["intake"])
	assertNum(t, intake["days_included"], 2, 0)
	assertNum(t, intake["days_excluded_unlogged"], 1, 0)
	assertNum(t, intake["mean_calories_per_day"], 2000, 0)
	if l := mutList(t, intake["low_item_count_dates"]); len(l) != 0 {
		t.Fatalf("low_item_count_dates = %v", l)
	}

	exp := mutMap(t, data["expenditure_garmin"])
	assertNum(t, exp["days_included"], 2, 0)
	assertNum(t, exp["days_excluded"], 1, 0)
	assertNum(t, exp["mean_total_calories_per_day"], 2500, 0)

	bc := mutMap(t, data["body_composition"])
	if bc["first_reading_date"] != "2024-01-01" || bc["last_reading_date"] != "2024-01-25" {
		t.Fatalf("reading dates = %v..%v", bc["first_reading_date"], bc["last_reading_date"])
	}
	assertNum(t, bc["days_between_readings"], 24, 0)
	assertNum(t, bc["observed_first_weight_kg"], 80.0, 0)
	assertNum(t, bc["observed_last_weight_kg"], 79.0, 0)
	assertNum(t, bc["observed_first_body_fat_percent"], 20.0, 0)
	assertNum(t, bc["observed_last_body_fat_percent"], 18.75, 0)
	assertNum(t, bc["fitted_first_weight_kg"], 80.0, 0)
	assertNum(t, bc["fitted_last_weight_kg"], 79.0, 0)
	assertNum(t, bc["weight_change_kg"], -1.0, 0)
	assertNum(t, bc["fat_mass_change_kg"], -1.1875, 0.001)
	assertNum(t, bc["lean_mass_change_kg"], 0.1875, 0.001)
	if _, ok := data["gate_note"]; ok {
		t.Fatalf("unexpected gate_note: %v", data["gate_note"])
	}

	derived := mutMap(t, data["derived"])
	assertNum(t, derived["implied_deficit_kcal_per_day"], 366.927, 0.1)
	assertNum(t, derived["composition_derived_tdee_kcal_per_day"], 2366.927, 0.1)
	assertNum(t, derived["garmin_tdee_kcal_per_day"], 2500.0, 0)
	if derived["implied_deficit_uncertainty_kcal_per_day"] != nil {
		t.Fatalf("uncertainty = %v, want nil", derived["implied_deficit_uncertainty_kcal_per_day"])
	}
	assumptions := mutMap(t, data["assumptions"])
	assertNum(t, assumptions["fat_kcal_per_kg"], 7700, 0)
}

func TestGetEnergyBalanceExcludesPartialAndNoDataDays(t *testing.T) {
	today := dayFromNow(0)
	noData := dayFromNow(-1)
	complete := dayFromNow(-2)

	f := newFakeGarmin(t)
	f.on("GET "+nutritionRangePath, energyIntakeResponse([]intakeFixture{
		{Date: complete, Calories: 2000, Items: 5},
		{Date: noData, Calories: 2000, Items: 5},
		{Date: today, Calories: 2000, Items: 5},
	}))
	f.on("GET "+dailyStatsPath+"/"+complete+"/"+today, energyCaloriesResponse([]calorieFixture{
		{Date: complete, Total: 2600},
		{Date: today, Total: 9999},
	}))
	f.on("GET "+bodyCompositionPath, bodyCompResponse())
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_energy_balance", map[string]any{
		"start_date": complete,
		"end_date":   today,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	exp := mutMap(t, data["expenditure_garmin"])
	assertNum(t, exp["days_included"], 1, 0)
	assertNum(t, exp["days_excluded"], 2, 0)
	assertNum(t, exp["mean_total_calories_per_day"], 2600, 0)
}

func TestGetEnergyBalanceSurfacesLowItemCountDays(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionRangePath, energyIntakeResponse([]intakeFixture{
		{Date: "2024-01-01", Calories: 2000, Items: 5},
		{Date: "2024-01-02", Calories: 800, Items: 1},
		{Date: "2024-01-03", Calories: nil, Items: 0},
	}))
	f.on("GET "+dailyStatsPath+"/2024-01-01/2024-01-03", energyCaloriesResponse(nil))
	f.on("GET "+bodyCompositionPath, bodyCompResponse())
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_energy_balance", map[string]any{
		"start_date": "2024-01-01",
		"end_date":   "2024-01-03",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	intake := mutMap(t, data["intake"])
	assertNum(t, intake["days_included"], 2, 0)
	assertNum(t, intake["days_excluded_unlogged"], 1, 0)
	low := mutList(t, intake["low_item_count_dates"])
	if len(low) != 1 || low[0] != "2024-01-02" {
		t.Fatalf("low_item_count_dates = %v", low)
	}
	assertNum(t, intake["mean_calories_per_day"], 1400, 0)
}

func TestGetEnergyBalanceFitsTrendAcrossMultipleReadings(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionRangePath, energyIntakeResponse([]intakeFixture{
		{Date: "2024-01-01", Calories: 2000, Items: 5},
	}))
	f.on("GET "+dailyStatsPath+"/2024-01-01/2024-01-25", energyCaloriesResponse([]calorieFixture{
		{Date: "2024-01-01", Total: 2600},
	}))
	f.on("GET "+bodyCompositionPath, bodyCompResponse(
		bodyCompEntry("2024-01-01", 80000.0, 20.0),
		bodyCompEntry("2024-01-07", 79500.0, 20.0),
		bodyCompEntry("2024-01-13", 79000.0, 20.0),
		bodyCompEntry("2024-01-19", 78500.0, 20.0),
		bodyCompEntry("2024-01-25", 78000.0, 20.0),
	))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_energy_balance", map[string]any{
		"start_date": "2024-01-01",
		"end_date":   "2024-01-25",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)

	bc := mutMap(t, data["body_composition"])
	assertNum(t, bc["readings_used"], 5, 0)
	assertNum(t, bc["days_between_readings"], 24, 0)
	if _, ok := data["gate_note"]; ok {
		t.Fatalf("unexpected gate_note: %v", data["gate_note"])
	}
	assertNum(t, bc["weight_change_kg"], -2.0, 0.01)
	assertNum(t, bc["fitted_first_weight_kg"], 80.0, 0.01)
	assertNum(t, bc["fitted_last_weight_kg"], 78.0, 0.01)

	derived := mutMap(t, data["derived"])
	assertNum(t, derived["implied_deficit_uncertainty_kcal_per_day"], 0, 0.01)
	if _, ok := derived["uncertainty_note"]; ok {
		t.Fatalf("unexpected uncertainty_note")
	}
}

func TestGetEnergyBalanceGatesOutShortSpan(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionRangePath, energyIntakeResponse([]intakeFixture{
		{Date: "2024-01-01", Calories: 2000, Items: 5},
	}))
	f.on("GET "+dailyStatsPath+"/2024-01-01/2024-01-19", energyCaloriesResponse([]calorieFixture{
		{Date: "2024-01-01", Total: 2600},
	}))
	f.on("GET "+bodyCompositionPath, bodyCompResponse(
		bodyCompEntry("2024-01-01", 90000.0, 28.0),
		bodyCompEntry("2024-01-18", 91000.0, 27.5),
		bodyCompEntry("2024-01-19", 89670.0, 28.3),
	))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_energy_balance", map[string]any{
		"start_date": "2024-01-01",
		"end_date":   "2024-01-19",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	if _, ok := data["body_composition"]; !ok {
		t.Fatal("body_composition missing")
	}
	assertNum(t, mutMap(t, data["body_composition"])["readings_used"], 3, 0)
	if _, ok := data["derived"]; ok {
		t.Fatal("derived should be suppressed")
	}
	if _, ok := data["assumptions"]; ok {
		t.Fatal("assumptions should be suppressed")
	}
	note, _ := data["gate_note"].(string)
	if !strings.Contains(note, "18 days") {
		t.Fatalf("gate_note = %q", note)
	}
}

func TestGetEnergyBalanceGatesOutClusteredLeverage(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionRangePath, energyIntakeResponse([]intakeFixture{
		{Date: "2024-01-01", Calories: 2000, Items: 5},
	}))
	f.on("GET "+dailyStatsPath+"/2024-01-01/2024-01-24", energyCaloriesResponse([]calorieFixture{
		{Date: "2024-01-01", Total: 2600},
	}))
	f.on("GET "+bodyCompositionPath, bodyCompResponse(
		bodyCompEntry("2024-01-01", 90000.0, 28.0),
		bodyCompEntry("2024-01-23", 89000.0, 27.5),
		bodyCompEntry("2024-01-24", 88670.0, 27.3),
	))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_energy_balance", map[string]any{
		"start_date": "2024-01-01",
		"end_date":   "2024-01-24",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	if _, ok := data["derived"]; ok {
		t.Fatal("derived should be suppressed")
	}
	note, _ := data["gate_note"].(string)
	if !strings.Contains(note, "cluster") {
		t.Fatalf("gate_note = %q", note)
	}
}

func TestGetEnergyBalanceDailyWeighInsAreNotOneCluster(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionRangePath, energyIntakeResponse([]intakeFixture{
		{Date: "2024-01-01", Calories: 2000, Items: 5},
	}))
	f.on("GET "+dailyStatsPath+"/2024-01-01/2024-01-28", energyCaloriesResponse([]calorieFixture{
		{Date: "2024-01-01", Total: 2600},
	}))
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	entries := make([]map[string]any, 0, 28)
	for i := 0; i < 28; i++ {
		entries = append(entries, bodyCompEntry(
			start.AddDate(0, 0, i).Format("2006-01-02"),
			80000.0-50.0*float64(i),
			20.0,
		))
	}
	f.on("GET "+bodyCompositionPath, bodyCompResponse(entries...))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_energy_balance", map[string]any{
		"start_date": "2024-01-01",
		"end_date":   "2024-01-28",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	assertNum(t, mutMap(t, data["body_composition"])["readings_used"], 28, 0)
	if _, ok := data["gate_note"]; ok {
		t.Fatalf("unexpected gate_note: %v", data["gate_note"])
	}
	assertNum(t, mutMap(t, data["derived"])["implied_deficit_kcal_per_day"], 0.010*7700+0.040*1800, 0.1)
}

func TestBodyCompGateRegularWeighInsPass(t *testing.T) {
	range28 := make([]float64, 28)
	for i := range range28 {
		range28[i] = float64(i)
	}
	if got := bodyCompGateFailure(range28); got != "" {
		t.Fatalf("daily weigh-ins gated: %q", got)
	}
	var everyOther []float64
	for i := 0; i < 29; i += 2 {
		everyOther = append(everyOther, float64(i))
	}
	if got := bodyCompGateFailure(everyOther); got != "" {
		t.Fatalf("every-other-day weigh-ins gated: %q", got)
	}
}

func TestBodyCompGateStillCatchesADenseCluster(t *testing.T) {
	for _, xs := range [][]float64{
		{0, 1, 2, 25},
		{0, 22, 23},
		{0, 23, 24, 25},
	} {
		if got := bodyCompGateFailure(xs); !strings.Contains(got, "cluster") {
			t.Fatalf("gate(%v) = %q, want cluster", xs, got)
		}
	}
}

func TestGetEnergyBalanceUncertaintyFormulaIsPinned(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionRangePath, energyIntakeResponse([]intakeFixture{
		{Date: "2024-01-01", Calories: 2000, Items: 5},
	}))
	f.on("GET "+dailyStatsPath+"/2024-01-01/2024-01-24", energyCaloriesResponse([]calorieFixture{
		{Date: "2024-01-01", Total: 2600},
	}))
	f.on("GET "+bodyCompositionPath, bodyCompResponse(
		bodyCompEntry("2024-01-01", 90000.0, 28.0),
		bodyCompEntry("2024-01-09", 89300.0, 27.6),
		bodyCompEntry("2024-01-16", 88900.0, 27.5),
		bodyCompEntry("2024-01-24", 88000.0, 27.0),
	))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_energy_balance", map[string]any{
		"start_date": "2024-01-01",
		"end_date":   "2024-01-24",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	bc := mutMap(t, data["body_composition"])
	assertNum(t, bc["weight_change_kg"], -1.942, 0.005)
	assertNum(t, bc["fat_mass_change_kg"], -1.373, 0.0005)
	assertNum(t, bc["lean_mass_change_kg"], -0.568, 0.0005)

	derived := mutMap(t, data["derived"])
	assertNum(t, derived["implied_deficit_kcal_per_day"], 504.289, 0.1)
	assertNum(t, derived["implied_deficit_uncertainty_kcal_per_day"], 55.664, 0.1)
	assertNum(t, derived["composition_derived_tdee_kcal_per_day"], 2504.289, 0.1)
}

func TestGetEnergyBalanceSingleBodyCompReading(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionRangePath, energyIntakeResponse([]intakeFixture{
		{Date: "2024-01-01", Calories: 2000, Items: 3},
	}))
	f.on("GET "+dailyStatsPath+"/2024-01-01/2024-01-01", energyCaloriesResponse([]calorieFixture{
		{Date: "2024-01-01", Total: 2500},
	}))
	f.on("GET "+bodyCompositionPath, bodyCompResponse(
		bodyCompEntry("2024-01-01", 80000.0, 20.0),
	))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_energy_balance", map[string]any{
		"start_date": "2024-01-01",
		"end_date":   "2024-01-01",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	if _, ok := data["derived"]; ok {
		t.Fatal("derived should be omitted")
	}
	if _, ok := data["body_composition_note"]; !ok {
		t.Fatal("body_composition_note missing")
	}
}

func TestGetEnergyBalanceChunksExpenditureOver28Days(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionRangePath, energyIntakeResponse(nil))
	f.on("GET "+dailyStatsPath+"/2024-01-01/2024-01-28", energyCaloriesResponse([]calorieFixture{
		{Date: "2024-01-01", Total: 2500},
	}))
	f.on("GET "+dailyStatsPath+"/2024-01-29/2024-02-05", energyCaloriesResponse([]calorieFixture{
		{Date: "2024-02-05", Total: 2600},
	}))
	f.on("GET "+bodyCompositionPath, bodyCompResponse())
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_energy_balance", map[string]any{
		"start_date": "2024-01-01",
		"end_date":   "2024-02-05",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	// Python counts 3 connectapi calls because its body-composition mock is a
	// separate client method; over HTTP this is intake + two chunks + body comp.
	if calls := f.recorded(); len(calls) != 4 {
		t.Fatalf("calls = %d, want 4", len(calls))
	}
	exp := mutMap(t, data["expenditure_garmin"])
	assertNum(t, exp["days_included"], 2, 0)
	assertNum(t, exp["mean_total_calories_per_day"], 2550, 0)
}

func TestGetEnergyBalanceRejectsOversizedRange(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_energy_balance", map[string]any{
		"start_date": "2024-01-01",
		"end_date":   "2024-04-01",
	})
	if !strings.Contains(text, "too large") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.recorded(); len(calls) != 0 {
		t.Fatalf("fake saw %d calls, want 0", len(calls))
	}
}

func TestGetEnergyBalanceRejectsInvertedRange(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_energy_balance", map[string]any{
		"start_date": "2024-01-15",
		"end_date":   "2024-01-01",
	})
	if !strings.Contains(text, "end_date must be on or after start_date") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.recorded(); len(calls) != 0 {
		t.Fatalf("fake saw %d calls, want 0", len(calls))
	}
}

func TestGetEnergyBalanceNoUsableData(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionRangePath, energyIntakeResponse(nil))
	f.on("GET "+dailyStatsPath+"/2024-01-01/2024-01-03", energyCaloriesResponse(nil))
	f.on("GET "+bodyCompositionPath, bodyCompResponse())
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_energy_balance", map[string]any{
		"start_date": "2024-01-01",
		"end_date":   "2024-01-03",
	})
	if !strings.Contains(text, "No usable data found") {
		t.Fatalf("text = %q", text)
	}
}

func TestGetEnergyBalanceError(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_energy_balance", map[string]any{
		"start_date": "2024-01-01",
		"end_date":   "2024-01-03",
	})
	if !strings.Contains(text, "Error retrieving energy balance data") {
		t.Fatalf("text = %q", text)
	}
}

// --- pass-through tools ---

func TestGetUserSummaryTool(t *testing.T) {
	summary := fixture(t, "MOCK_USER_SUMMARY")
	f := newFakeGarmin(t)
	f.on("GET "+userSummaryPath, summary)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_user_summary", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, summary)
	if calls := f.callsMatching("GET", userSummaryPath); len(calls) != 1 {
		t.Fatalf("user summary calls = %d, want 1", len(calls))
	}
}

func TestGetBodyCompositionSingleDate(t *testing.T) {
	body := fixture(t, "MOCK_BODY_COMPOSITION")
	f := newFakeGarmin(t)
	f.on("GET "+bodyCompositionPath, body)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_body_composition", map[string]any{"start_date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, body)
	calls := f.callsMatching("GET", bodyCompositionPath)
	if len(calls) != 1 {
		t.Fatalf("body composition calls = %d, want 1", len(calls))
	}
	if calls[0].Query.Get("startDate") != "2024-01-15" || calls[0].Query.Get("endDate") != "2024-01-15" {
		t.Fatalf("query = %v", calls[0].Query)
	}
}

func TestGetBodyCompositionDateRange(t *testing.T) {
	body := fixture(t, "MOCK_BODY_COMPOSITION")
	f := newFakeGarmin(t)
	f.on("GET "+bodyCompositionPath, body)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_body_composition", map[string]any{
		"start_date": "2024-01-08",
		"end_date":   "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	calls := f.callsMatching("GET", bodyCompositionPath)
	if len(calls) != 1 {
		t.Fatalf("body composition calls = %d, want 1", len(calls))
	}
	if calls[0].Query.Get("startDate") != "2024-01-08" || calls[0].Query.Get("endDate") != "2024-01-15" {
		t.Fatalf("query = %v", calls[0].Query)
	}
}

func TestGetStatsAndBodyTool(t *testing.T) {
	stats := fixture(t, "MOCK_STATS")
	body := fixture(t, "MOCK_BODY_COMPOSITION")
	f := newFakeGarmin(t)
	f.on("GET "+userSummaryPath, stats)
	f.on("GET "+bodyCompositionPath, body)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_stats_and_body", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, stats)
	if len(f.callsMatching("GET", userSummaryPath)) != 1 || len(f.callsMatching("GET", bodyCompositionPath)) != 1 {
		t.Fatalf("unexpected calls: %v", f.recorded())
	}
}

func TestGetStepsDataTool(t *testing.T) {
	steps := fixture(t, "MOCK_STEPS_DATA")
	f := newFakeGarmin(t)
	f.on("GET "+stepsChartPath, steps)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_steps_data", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, steps)
	if calls := f.callsMatching("GET", stepsChartPath); len(calls) != 1 {
		t.Fatalf("steps chart calls = %d, want 1", len(calls))
	}
}

func TestGetDailyStepsTool(t *testing.T) {
	steps := fixture(t, "MOCK_DAILY_STEPS")
	f := newFakeGarmin(t)
	f.on("GET "+dailyStepsPath, steps)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_daily_steps", map[string]any{
		"start_date": "2024-01-08",
		"end_date":   "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, steps)
	if calls := f.callsMatching("GET", dailyStepsPath); len(calls) != 1 {
		t.Fatalf("daily steps calls = %d, want 1", len(calls))
	}
}

func TestGetFloorsTool(t *testing.T) {
	floors := fixture(t, "MOCK_FLOORS")
	f := newFakeGarmin(t)
	f.on("GET "+floorsPath, floors)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_floors", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, floors)
	if calls := f.callsMatching("GET", floorsPath); len(calls) != 1 {
		t.Fatalf("floors calls = %d, want 1", len(calls))
	}
}

func TestGetRhrDayTool(t *testing.T) {
	rhr := fixture(t, "MOCK_RHR_DAY")
	f := newFakeGarmin(t)
	f.on("GET "+rhrPath, rhr)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_rhr_day", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, rhr)
	calls := f.callsMatching("GET", rhrPath)
	if len(calls) != 1 {
		t.Fatalf("rhr calls = %d, want 1", len(calls))
	}
	if calls[0].Query.Get("fromDate") != "2024-01-15" || calls[0].Query.Get("metricId") != "60" {
		t.Fatalf("query = %v", calls[0].Query)
	}
}

func TestGetHydrationDataTool(t *testing.T) {
	hydration := fixture(t, "MOCK_HYDRATION_DATA")
	f := newFakeGarmin(t)
	f.on("GET "+hydrationPath, hydration)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_hydration_data", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, hydration)
}

func TestGetBloodPressureTool(t *testing.T) {
	bp := fixture(t, "MOCK_BLOOD_PRESSURE")
	f := newFakeGarmin(t)
	f.on("GET "+bloodPressurePath, bp)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_blood_pressure", map[string]any{
		"start_date": "2024-01-08",
		"end_date":   "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, bp)
	calls := f.callsMatching("GET", bloodPressurePath)
	if len(calls) != 1 || calls[0].Query.Get("includeAll") != "true" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestGetBodyBatteryEventsTool(t *testing.T) {
	events := fixture(t, "MOCK_BODY_BATTERY_EVENTS")
	f := newFakeGarmin(t)
	f.on("GET "+bodyBatteryEventsPath, events)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_body_battery_events", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, events)
}

func TestGetAllDayStressTool(t *testing.T) {
	stress := fixture(t, "MOCK_STRESS_DATA")
	f := newFakeGarmin(t)
	f.on("GET "+stressPath, stress)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_all_day_stress", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, stress)
	if calls := f.callsMatching("GET", stressPath); len(calls) != 1 {
		t.Fatalf("stress calls = %d, want 1", len(calls))
	}
}

func TestGetAllDayEventsTool(t *testing.T) {
	events := map[string]any{"events": []any{map[string]any{"type": "STRESS", "timestamp": 1705276800000}}}
	f := newFakeGarmin(t)
	f.on("GET "+allDayEventsPath, events)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_all_day_events", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, events)
	calls := f.callsMatching("GET", allDayEventsPath)
	if len(calls) != 1 || calls[0].Query.Get("calendarDate") != "2024-01-15" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestGetLifestyleLoggingDataTool(t *testing.T) {
	lifestyle := fixture(t, "MOCK_LIFESTYLE_LOGGING_DATA")
	f := newFakeGarmin(t)
	f.on("GET "+lifestylePath, lifestyle)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_lifestyle_logging_data", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, lifestyle)
}

func TestGetTrainingReadinessTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trainingReadinessPath, fixture(t, "MOCK_TRAINING_READINESS"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_training_readiness", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data []map[string]any
	decodeJSON(t, text, &data)
	if len(data) != 2 || data[0]["context"] != "AFTER_WAKEUP_RESET" || data[1]["context"] != "AFTER_POST_EXERCISE_RESET" {
		t.Fatalf("contexts = %v", data)
	}
	morning := data[0]
	if morning["date"] != "2024-01-15" || morning["score"] != 62.0 || morning["level"] != "MODERATE" || morning["feedback"] != "WELL_RECOVERED" {
		t.Fatalf("morning = %v", morning)
	}
	if morning["sleep_score"] != 81.0 || morning["recovery_time_hours"] != 5.5 || morning["acute_load"] != 310.0 || morning["hrv_weekly_avg"] != 58.0 {
		t.Fatalf("morning factors = %v", morning)
	}
	if data[1]["score"] != 38.0 || data[1]["recovery_time_hours"] != 18.0 {
		t.Fatalf("second = %v", data[1])
	}
	if _, ok := data[1]["sleep_score"]; ok {
		t.Fatal("sleep_score should be pruned")
	}
	if calls := f.callsMatching("GET", trainingReadinessPath); len(calls) != 1 {
		t.Fatalf("readiness calls = %d, want 1", len(calls))
	}
}

func TestGetTrainingReadinessAcceptsSingleSnapshot(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trainingReadinessPath, map[string]any{
		"calendarDate": "2024-01-15",
		"score":        62,
		"level":        "MODERATE",
		"recoveryTime": 330,
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_training_readiness", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, []any{map[string]any{
		"date": "2024-01-15", "score": 62.0, "level": "MODERATE", "recovery_time_hours": 5.5,
	}})
}

func TestTrainingReadinessEmptyDictionaryRemainsNoData(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+trainingReadinessPath, map[string]any{})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_training_readiness", map[string]any{"date": "2024-01-15"})
	if text != "No training readiness data found for 2024-01-15" {
		t.Fatalf("text = %q", text)
	}
}

func TestGetMorningTrainingReadinessTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+morningReadinessPath, fixture(t, "MOCK_MORNING_TRAINING_READINESS"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_morning_training_readiness", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["date"] != "2024-01-15" || data["context"] != "AFTER_WAKEUP_RESET" {
		t.Fatalf("data = %v", data)
	}
	if data["score"] != 62.0 || data["level"] != "MODERATE" || data["feedback"] != "WELL_RECOVERED" {
		t.Fatalf("data = %v", data)
	}
	if data["sleep_score"] != 81.0 || data["recovery_time_hours"] != 5.5 || data["hrv_weekly_avg"] != 58.0 {
		t.Fatalf("data = %v", data)
	}
	if data["hrv_factor_feedback"] != "GOOD" || data["acute_load"] != 310.0 {
		t.Fatalf("data = %v", data)
	}
	if calls := f.callsMatching("GET", morningReadinessPath); len(calls) != 1 {
		t.Fatalf("readiness calls = %d, want 1", len(calls))
	}
}

func TestGetMorningTrainingReadinessRecoveryReachedZero(t *testing.T) {
	morning := mutMap(t, fixture(t, "MOCK_MORNING_TRAINING_READINESS"))
	morning["recoveryTime"] = 106
	morning["recoveryTimeChangePhrase"] = "REACHED_ZERO"

	f := newFakeGarmin(t)
	f.on("GET "+morningReadinessPath, morning)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_morning_training_readiness", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["recovery_time_hours"] != 0.0 {
		t.Fatalf("recovery_time_hours = %v", data["recovery_time_hours"])
	}
}

// --- body battery ---

func TestGetBodyBatteryTool(t *testing.T) {
	battery := fixture(t, "MOCK_BODY_BATTERY")
	f := newFakeGarmin(t)
	f.on("GET "+bodyBatteryPath, battery)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_body_battery", map[string]any{
		"start_date": "2024-01-08",
		"end_date":   "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data []map[string]any
	decodeJSON(t, text, &data)
	if len(data) != 1 {
		t.Fatalf("days = %d, want 1", len(data))
	}
	if events := mutList(t, data[0]["events"]); len(events) != 0 {
		t.Fatalf("events = %v", events)
	}
	calls := f.callsMatching("GET", bodyBatteryPath)
	if len(calls) != 1 || calls[0].Query.Get("startDate") != "2024-01-08" || calls[0].Query.Get("endDate") != "2024-01-15" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestGetBodyBatteryHandlesNullActivityEvents(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+bodyBatteryPath, []any{
		map[string]any{
			"date":                            "2024-01-15",
			"charged":                         100,
			"drained":                         20,
			"bodyBatteryActivityEvent":        nil,
			"bodyBatteryDynamicFeedbackEvent": nil,
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_body_battery", map[string]any{
		"start_date": "2024-01-15",
		"end_date":   "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if strings.Contains(text, "NoneType") || strings.Contains(text, "Error") {
		t.Fatalf("text = %q", text)
	}
	if !strings.Contains(text, "100") {
		t.Fatalf("text = %q", text)
	}
}

func TestGetBodyBatteryPreservesEventsWithNullDuration(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+bodyBatteryPath, []any{
		map[string]any{"date": "2024-01-14", "charged": 70, "bodyBatteryActivityEvent": []any{}},
		map[string]any{
			"date":    "2024-01-15",
			"charged": 80,
			"bodyBatteryActivityEvent": []any{
				map[string]any{"eventType": "SLEEP", "durationInMilliseconds": nil, "bodyBatteryImpact": 50},
				map[string]any{"eventType": "ACTIVITY", "durationInMilliseconds": 0},
				map[string]any{"eventType": "REST", "durationInMilliseconds": 120000},
			},
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_body_battery", map[string]any{
		"start_date": "2024-01-14",
		"end_date":   "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data []map[string]any
	decodeJSON(t, text, &data)
	if len(data) != 2 || data[0]["charged"] != 70.0 || data[1]["charged"] != 80.0 {
		t.Fatalf("data = %v", data)
	}
	events := data[1]["events"].([]any)
	first := events[0].(map[string]any)
	if first["type"] != "SLEEP" || first["body_battery_impact"] != 50.0 {
		t.Fatalf("first event = %v", first)
	}
	if _, ok := first["duration_minutes"]; ok {
		t.Fatal("duration_minutes should be omitted for a null duration")
	}
	if events[1].(map[string]any)["duration_minutes"] != 0.0 {
		t.Fatalf("second event = %v", events[1])
	}
	if events[2].(map[string]any)["duration_minutes"] != 2.0 {
		t.Fatalf("third event = %v", events[2])
	}
}

// --- heart rate summary ---

func TestGetHeartRatesTool(t *testing.T) {
	hr := fixture(t, "MOCK_HEART_RATES")
	f := newFakeGarmin(t)
	f.on("GET "+heartRatesPath, hr)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_heart_rates", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, hr)
}

func TestGetHeartRatesSummaryTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+heartRatesPath, fixture(t, "MOCK_HEART_RATES"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_heart_rates_summary", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["max_heart_rate_bpm"] != 180.0 || data["min_heart_rate_bpm"] != 45.0 {
		t.Fatalf("data = %v", data)
	}
	if data["resting_heart_rate_bpm"] != 55.0 || data["last_7_days_avg_resting_hr"] != 57.0 {
		t.Fatalf("data = %v", data)
	}
	if data["avg_heart_rate_bpm"] != 65.0 || data["data_points_count"] != 3.0 {
		t.Fatalf("data = %v", data)
	}
	if calls := f.callsMatching("GET", heartRatesPath); len(calls) != 1 {
		t.Fatalf("heart rate calls = %d, want 1", len(calls))
	}
}

// --- stress ---

func TestGetStressDataTool(t *testing.T) {
	stress := fixture(t, "MOCK_STRESS_DATA")
	f := newFakeGarmin(t)
	f.on("GET "+stressPath, stress)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_stress_data", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, stress)
}

func TestGetStressSummaryTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+stressPath, fixture(t, "MOCK_STRESS_DATA"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_stress_summary", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["date"] != "2024-01-15" || data["max_stress_level"] != 60.0 || data["avg_stress_level"] != 25.0 {
		t.Fatalf("data = %v", data)
	}
	if data["rest_percent"] != 66.7 || data["low_stress_percent"] != 33.3 {
		t.Fatalf("data = %v", data)
	}
	if data["medium_stress_percent"] != 0.0 || data["high_stress_percent"] != 0.0 {
		t.Fatalf("data = %v", data)
	}
	if data["data_points_count"] != 3.0 {
		t.Fatalf("data = %v", data)
	}
}

// --- respiration ---

func TestGetRespirationDataTool(t *testing.T) {
	resp := fixture(t, "MOCK_RESPIRATION_DATA")
	f := newFakeGarmin(t)
	f.on("GET "+respirationPath, resp)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_respiration_data", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, resp)
}

func TestGetRespirationSummaryTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+respirationPath, fixture(t, "MOCK_RESPIRATION_DATA"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_respiration_summary", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["date"] != "2024-01-15" {
		t.Fatalf("data = %v", data)
	}
	if _, ok := data["avg_waking_breaths_per_min"]; ok {
		t.Fatalf("data = %v", data)
	}
}

// --- spo2 ---

func TestGetSpo2DataTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+spo2Path, fixture(t, "MOCK_SPO2_DATA"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_spo2_data", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["date"] != "2024-01-15" {
		t.Fatalf("data = %v", data)
	}
	// The fixture spells these averageSpo2/lowestSpo2; the Python tool only
	// reads averageSpO2/lowestSpO2, so they are absent from the curated result.
	if _, ok := data["avg_spo2_percent"]; ok {
		t.Fatalf("avg_spo2_percent should be absent: %v", data)
	}
}

// --- sleep ---

func TestGetSleepDataTool(t *testing.T) {
	sleep := fixture(t, "MOCK_SLEEP_DATA")
	f := newFakeGarmin(t)
	f.on("GET "+sleepPath, sleep)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_sleep_data", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, sleep)
}

func TestGetSleepSummaryTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+sleepPath, fixture(t, "MOCK_SLEEP_DATA"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_sleep_summary", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["resting_heart_rate_bpm"] != 55.0 || data["restless_moments_count"] != 15.0 {
		t.Fatalf("data = %v", data)
	}
	if data["avg_spo2_percent"] != 96.0 || data["lowest_spo2_percent"] != 93.0 {
		t.Fatalf("data = %v", data)
	}
	if data["sleep_seconds"] != 28800.0 || data["sleep_hours"] != 8.0 {
		t.Fatalf("data = %v", data)
	}
	if data["sleep_score"] != 85.0 || data["sleep_score_qualifier"] != "GOOD" {
		t.Fatalf("data = %v", data)
	}
	if data["deep_sleep_percent"] != 25.0 || data["light_sleep_percent"] != 50.0 || data["rem_sleep_percent"] != 25.0 {
		t.Fatalf("data = %v", data)
	}
	if data["avg_overnight_hrv"] != 45.0 {
		t.Fatalf("data = %v", data)
	}
}

func TestGetSleepSummaryHandlesNullSleepScores(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+sleepPath, map[string]any{
		"dailySleepDTO": map[string]any{
			"sleepTimeSeconds":  28800,
			"deepSleepSeconds":  7200,
			"lightSleepSeconds": 14400,
			"remSleepSeconds":   7200,
			"awakeSleepSeconds": 0,
			"sleepScores":       nil,
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_sleep_summary", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if strings.Contains(text, "NoneType") || strings.Contains(text, "Error") {
		t.Fatalf("text = %q", text)
	}
	if !strings.Contains(text, "28800") {
		t.Fatalf("text = %q", text)
	}
}

func TestGetSleepSummaryHandlesNullSleepPhases(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+sleepPath, map[string]any{
		"dailySleepDTO": map[string]any{
			"sleepTimeSeconds":  28800,
			"deepSleepSeconds":  nil,
			"lightSleepSeconds": nil,
			"remSleepSeconds":   nil,
			"awakeSleepSeconds": nil,
			"sleepScores":       map[string]any{"overall": map[string]any{"value": 85, "qualifierKey": "GOOD"}},
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_sleep_summary", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if strings.Contains(text, "NoneType") || strings.Contains(text, "Error") {
		t.Fatalf("text = %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["sleep_seconds"] != 28800.0 || data["sleep_hours"] != 8.0 || data["sleep_score"] != 85.0 {
		t.Fatalf("data = %v", data)
	}
	for _, key := range []string{"deep_sleep_percent", "light_sleep_percent", "rem_sleep_percent"} {
		if _, ok := data[key]; ok {
			t.Fatalf("%s should be omitted", key)
		}
	}
}

func TestGetSleepSummaryHandlesPartialSleepPhases(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+sleepPath, map[string]any{
		"dailySleepDTO": map[string]any{
			"sleepTimeSeconds":  28800,
			"deepSleepSeconds":  7200,
			"lightSleepSeconds": nil,
			"remSleepSeconds":   nil,
			"awakeSleepSeconds": 0,
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_sleep_summary", map[string]any{"date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if strings.Contains(text, "NoneType") {
		t.Fatalf("text = %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["deep_sleep_percent"] != 25.0 {
		t.Fatalf("data = %v", data)
	}
	if _, ok := data["light_sleep_percent"]; ok {
		t.Fatal("light_sleep_percent should be omitted")
	}
	if _, ok := data["rem_sleep_percent"]; ok {
		t.Fatal("rem_sleep_percent should be omitted")
	}
}

func TestGetSleepSummaryRangeTool(t *testing.T) {
	sleep := fixture(t, "MOCK_SLEEP_DATA")
	f := newFakeGarmin(t)
	f.on("GET "+sleepPath, sleep)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_sleep_summary_range", map[string]any{
		"start_date": "2024-01-13",
		"end_date":   "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["start_date"] != "2024-01-13" || data["end_date"] != "2024-01-15" {
		t.Fatalf("data = %v", data)
	}
	if data["nights_requested"] != 3.0 || data["nights_returned"] != 3.0 {
		t.Fatalf("data = %v", data)
	}
	nights := mutList(t, data["nights"])
	wantDates := []string{"2024-01-13", "2024-01-14", "2024-01-15"}
	for i, n := range nights {
		night := mutMap(t, n)
		if night["date"] != wantDates[i] {
			t.Fatalf("nights = %v", nights)
		}
	}
	first := mutMap(t, nights[0])
	if first["sleep_score"] != 85.0 || first["sleep_hours"] != 8.0 {
		t.Fatalf("first night = %v", first)
	}
	if calls := f.callsMatching("GET", sleepPath); len(calls) != 3 {
		t.Fatalf("sleep calls = %d, want 3", len(calls))
	}
}

func TestGetSleepSummaryRangeSkipsNightsWithoutData(t *testing.T) {
	sleep := fixture(t, "MOCK_SLEEP_DATA")
	f := newFakeGarmin(t)
	f.on("GET "+sleepPath, sequence{sleep, nil, sleep})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_sleep_summary_range", map[string]any{
		"start_date": "2024-01-13",
		"end_date":   "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["nights_requested"] != 3.0 || data["nights_returned"] != 2.0 {
		t.Fatalf("data = %v", data)
	}
	nights := mutList(t, data["nights"])
	if mutMap(t, nights[0])["date"] != "2024-01-13" || mutMap(t, nights[1])["date"] != "2024-01-15" {
		t.Fatalf("nights = %v", nights)
	}
}

func TestGetSleepSummaryRangeRejectsInvalidDates(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_sleep_summary_range", map[string]any{
		"start_date": "not-a-date",
		"end_date":   "2024-01-15",
	})
	if !strings.Contains(text, "Invalid date format") {
		t.Fatalf("text = %q", text)
	}

	text, _ = callTool(t, cs, "get_sleep_summary_range", map[string]any{
		"start_date": "2024-01-15",
		"end_date":   "2024-01-13",
	})
	if !strings.Contains(text, "end_date must be on or after start_date") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("GET", sleepPath); len(calls) != 0 {
		t.Fatalf("sleep calls = %d, want 0", len(calls))
	}
}

func TestGetSleepSummaryRangeEnforcesMaxDays(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_sleep_summary_range", map[string]any{
		"start_date": "2024-01-01",
		"end_date":   "2024-04-15",
	})
	if !strings.Contains(text, "too large") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("GET", sleepPath); len(calls) != 0 {
		t.Fatalf("sleep calls = %d, want 0", len(calls))
	}
}

func TestSleepSummariesAcceptTopLevelMetricsAndUppercaseSpo2(t *testing.T) {
	payload := map[string]any{
		"dailySleepDTO":        map[string]any{"sleepTimeSeconds": 25200},
		"restingHeartRate":     49,
		"restlessMomentsCount": 0,
		"wellnessSpO2SleepSummaryDTO": map[string]any{
			"averageSPO2": 95,
			"lowestSPO2":  89,
		},
	}
	metrics := []struct {
		key  string
		want any
	}{
		{"resting_heart_rate_bpm", 49.0},
		{"restless_moments_count", 0.0},
		{"avg_spo2_percent", 95.0},
		{"lowest_spo2_percent", 89.0},
	}

	for _, tool := range []string{"get_sleep_summary", "get_sleep_summary_range"} {
		t.Run(tool, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on("GET "+sleepPath, payload)
			cs := newSession(t, f, nil)

			var args map[string]any
			if tool == "get_sleep_summary_range" {
				args = map[string]any{"start_date": "2024-01-15", "end_date": "2024-01-15"}
			} else {
				args = map[string]any{"date": "2024-01-15"}
			}
			text, isErr := callTool(t, cs, tool, args)
			if isErr {
				t.Fatalf("unexpected error: %q", text)
			}
			var data map[string]any
			decodeJSON(t, text, &data)
			var summary map[string]any
			if tool == "get_sleep_summary_range" {
				summary = mutMap(t, mutList(t, data["nights"])[0])
			} else {
				summary = data
			}
			for _, m := range metrics {
				if summary[m.key] != m.want {
					t.Fatalf("%s = %v, want %v", m.key, summary[m.key], m.want)
				}
			}
		})
	}
}

func TestSleepSummaryRangeSurfacesRequestFailures(t *testing.T) {
	cases := []struct {
		name string
		body any
		want string
	}{
		{"transient API error", status(500, map[string]any{"message": "transient API error"}), "transient API error"},
		{"auth", status(401, map[string]any{"message": "expired"}), "401"},
		{"rate limited", status(429, map[string]any{"message": "rate limited"}), "rate limited"},
		{"bad request", status(400, map[string]any{"message": "bad request"}), "bad request"},
		{"invalid JSON", rawBody([]byte("not-json")), "decode response"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on("POST /diauth", status(401, map[string]any{"error": "invalid_grant"}))
			f.on("GET "+sleepPath, tc.body)
			cs := newSession(t, f, nil)

			text, _ := callTool(t, cs, "get_sleep_summary_range", map[string]any{
				"start_date": "2024-01-13",
				"end_date":   "2024-01-15",
			})
			if !strings.HasPrefix(text, "Error retrieving sleep data for 2024-01-13:") {
				t.Fatalf("text = %q", text)
			}
			if !strings.Contains(text, tc.want) {
				t.Fatalf("text = %q, want %q", text, tc.want)
			}
			if calls := f.callsMatching("GET", sleepPath); len(calls) != 1 {
				t.Fatalf("sleep calls = %d, want 1", len(calls))
			}
		})
	}
}

func TestGetStepsDataNoData(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+stepsChartPath, nil)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_steps_data", map[string]any{"date": "2024-01-15"})
	if text != "No steps data found for 2024-01-15" {
		t.Fatalf("text = %q", text)
	}
}

func TestGetSleepDataException(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+sleepPath, status(500, map[string]any{"message": "API Error"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_sleep_data", map[string]any{"date": "2024-01-15"})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	if !strings.Contains(text, "Error retrieving sleep data") {
		t.Fatalf("text = %q", text)
	}
}

// --- weekly aggregates ---

func TestGetWeeklyStepsTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+weeklyStepsPath, fixture(t, "MOCK_WEEKLY_STEPS"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_weekly_steps", map[string]any{
		"end_date": "2024-01-10",
		"weeks":    4,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["weeks_requested"] != 4.0 || data["weeks_returned"] != 2.0 {
		t.Fatalf("data = %v", data)
	}
	latest := mutMap(t, mutList(t, data["weekly_data"])[0])
	if latest["week_start"] != "2024-01-08" || latest["total_steps"] != 70000.0 {
		t.Fatalf("latest = %v", latest)
	}
	if calls := f.callsMatching("GET", weeklyStepsPath); len(calls) != 1 {
		t.Fatalf("weekly steps calls = %d, want 1", len(calls))
	}
}

func TestGetWeeklyStepsToolDefaultWeeks(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+weeklyStepsPath, fixture(t, "MOCK_WEEKLY_STEPS"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_weekly_steps", map[string]any{"end_date": "2024-01-10"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if calls := f.callsMatching("GET", weeklyStepsPath); len(calls) != 1 {
		t.Fatalf("weekly steps calls = %d, want 1", len(calls))
	}
}

func TestGetWeeklyStressTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+weeklyStressPath, fixture(t, "MOCK_WEEKLY_STRESS"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_weekly_stress", map[string]any{
		"end_date": "2024-01-10",
		"weeks":    4,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	latest := mutMap(t, mutList(t, data["weekly_data"])[0])
	if latest["week_start"] != "2024-01-08" || latest["stress_value"] != 35.0 {
		t.Fatalf("latest = %v", latest)
	}
	if calls := f.callsMatching("GET", weeklyStressPath); len(calls) != 1 {
		t.Fatalf("weekly stress calls = %d, want 1", len(calls))
	}
}

func TestGetWeeklyIntensityMinutesTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+weeklyIntensityPath, fixture(t, "MOCK_WEEKLY_INTENSITY_MINUTES"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_weekly_intensity_minutes", map[string]any{
		"end_date": "2024-01-10",
		"weeks":    2,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if calls := f.callsMatching("GET", weeklyIntensityPath); len(calls) != 1 {
		t.Fatalf("weekly intensity calls = %d, want 1", len(calls))
	}
}

func TestGetWeeklyIntensityMinutesCountsVigorousDouble(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+"/usersummary-service/stats/im/weekly/2024-01-01/2024-01-14", fixture(t, "MOCK_WEEKLY_INTENSITY_MINUTES"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_weekly_intensity_minutes", map[string]any{
		"end_date": "2024-01-14",
		"weeks":    2,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	weeks := mutList(t, data["weekly_data"])
	latest := mutMap(t, weeks[0])
	if latest["week_start"] != "2024-01-08" || latest["weekly_goal"] != 150.0 {
		t.Fatalf("latest = %v", latest)
	}
	if latest["moderate_minutes"] != 120.0 || latest["vigorous_minutes"] != 45.0 {
		t.Fatalf("latest = %v", latest)
	}
	if latest["total_minutes"] != 210.0 {
		t.Fatalf("total_minutes = %v", latest["total_minutes"])
	}
	if mutMap(t, weeks[1])["total_minutes"] != 150.0 {
		t.Fatalf("second = %v", weeks[1])
	}
}

func TestGetWeeklyIntensityMinutesToolDefaultWeeks(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+weeklyIntensityPath4, fixture(t, "MOCK_WEEKLY_INTENSITY_MINUTES"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_weekly_intensity_minutes", map[string]any{"end_date": "2024-01-10"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if calls := f.callsMatching("GET", weeklyIntensityPath4); len(calls) != 1 {
		t.Fatalf("weekly intensity calls = %d, want 1", len(calls))
	}
}

// --- recovery time unit tests ---

func TestHoursFromRecoveryMinutesAndReachedZero(t *testing.T) {
	if got, ok := hoursFromRecoveryMinutes(1728, nil); !ok || got != 28.8 {
		t.Fatalf("1728 -> %v, %v", got, ok)
	}
	if got, ok := hoursFromRecoveryMinutes(106, "REACHED_ZERO"); !ok || got != 0.0 {
		t.Fatalf("REACHED_ZERO -> %v, %v", got, ok)
	}
	if _, ok := hoursFromRecoveryMinutes(nil, nil); ok {
		t.Fatal("nil should not convert")
	}
	if _, ok := hoursFromRecoveryMinutes(true, nil); ok {
		t.Fatal("bool should not convert")
	}
}

func TestRecoveryStateBands(t *testing.T) {
	if got := recoveryState(floatPtr(0), nil); got != "recovered" {
		t.Fatalf("0 -> %q", got)
	}
	if got := recoveryState(floatPtr(3), nil); got != "nearly_recovered" {
		t.Fatalf("3 -> %q", got)
	}
	if got := recoveryState(floatPtr(12), nil); got != "recovering" {
		t.Fatalf("12 -> %q", got)
	}
	if got := recoveryState(floatPtr(36), nil); got != "not_recovered" {
		t.Fatalf("36 -> %q", got)
	}
	if got := recoveryState(floatPtr(12), "REACHED_ZERO"); got != "recovered" {
		t.Fatalf("REACHED_ZERO -> %q", got)
	}
	if got := recoveryState(nil, nil); got != "unknown" {
		t.Fatalf("nil -> %q", got)
	}
}

func TestRecoveryFromReadinessUsesLatestSnapshot(t *testing.T) {
	payload := []any{
		map[string]any{
			"calendarDate":             "2026-09-12",
			"timestampLocal":           "2026-09-12T07:00:00",
			"recoveryTime":             1728,
			"score":                    51,
			"level":                    "MODERATE",
			"recoveryTimeChangePhrase": "NO_CHANGE_SLEEP",
		},
		map[string]any{
			"calendarDate":             "2026-09-12",
			"timestampLocal":           "2026-09-12T18:00:00",
			"recoveryTime":             106,
			"score":                    72,
			"level":                    "PRODUCTIVE",
			"recoveryTimeChangePhrase": "NO_CHANGE_SLEEP",
		},
	}
	curated := recoveryFromReadiness(payload, "training_readiness")
	if curated == nil {
		t.Fatal("no curated payload")
	}
	assertNum(t, curated["remaining_hours"], 1.8, 0)
	assertNum(t, curated["recovery_score"], 72, 0)
	if curated["state"] != "nearly_recovered" {
		t.Fatalf("state = %v", curated["state"])
	}
	if curated["source"] != "training_readiness" {
		t.Fatalf("source = %v", curated["source"])
	}
}

func TestCollectPrefersTrainingReadiness(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+recoveryReadinessPath, map[string]any{
		"recoveryTime":   600,
		"score":          80,
		"calendarDate":   "2026-09-12",
		"timestampLocal": "2026-09-12T08:00:00",
	})
	f.on("GET "+recoveryActivityPath, []any{map[string]any{"recoveryTime": 9999}})
	cs := healthClient(t, f)

	curated, err := collectRecoveryTime(context.Background(), cs, "2026-09-12", time.Now())
	if err != nil {
		t.Fatalf("collectRecoveryTime: %v", err)
	}
	if curated["source"] != "training_readiness" {
		t.Fatalf("source = %v", curated["source"])
	}
	assertNum(t, curated["remaining_hours"], 10.0, 0)
	assertNum(t, curated["recovery_score"], 80, 0)
}

func TestCollectFallsBackToDecayedActivityRecovery(t *testing.T) {
	now := time.Date(2026, 9, 12, 18, 0, 0, 0, time.UTC)
	f := newFakeGarmin(t)
	f.on("GET "+recoveryReadinessPath, []any{})
	f.on("GET "+recoveryReadinessPath, nil)
	f.on("GET "+recoveryActivityPath, []any{
		map[string]any{
			"activityId":     42,
			"activityName":   "Lunch run",
			"beginTimestamp": float64(time.Date(2026, 9, 12, 11, 0, 0, 0, time.UTC).UnixMilli()),
			"duration":       3600,
			"recoveryTime":   480,
		},
	})
	cs := healthClient(t, f)

	curated, err := collectRecoveryTime(context.Background(), cs, "2026-09-12", now)
	if err != nil {
		t.Fatalf("collectRecoveryTime: %v", err)
	}
	if curated["source"] != "recent_activity" {
		t.Fatalf("source = %v", curated["source"])
	}
	assertNum(t, curated["remaining_hours"], 2.0, 0)
	assertNum(t, curated["activity_id"], 42, 0)
	if curated["state"] != "nearly_recovered" {
		t.Fatalf("state = %v", curated["state"])
	}
}

func TestCollectUnavailableWhenNoSources(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+recoveryReadinessPath, []any{})
	f.on("GET "+recoveryReadinessPath, nil)
	f.on("GET "+recoveryActivityPath, []any{})
	cs := healthClient(t, f)

	curated, err := collectRecoveryTime(context.Background(), cs, "2026-09-12", time.Now())
	if err != nil {
		t.Fatalf("collectRecoveryTime: %v", err)
	}
	if curated["state"] != "unavailable" {
		t.Fatalf("state = %v", curated["state"])
	}
	if curated["remaining_hours"] != nil {
		t.Fatalf("remaining_hours = %v", curated["remaining_hours"])
	}
	if _, ok := curated["message"]; !ok {
		t.Fatal("message missing")
	}
}

func TestReadinessRecoveryExplicitlyLabelsSnapshotTime(t *testing.T) {
	curated := recoveryFromReadiness(map[string]any{
		"calendarDate":   "2026-09-12",
		"timestampLocal": "2026-09-12T06:30:00",
		"recoveryTime":   1728,
	}, "training_readiness")
	if curated["value_type"] != "snapshot" {
		t.Fatalf("value_type = %v", curated["value_type"])
	}
	assertNum(t, curated["remaining_hours_at_snapshot"], 28.8, 0)
	if curated["as_of"] != "2026-09-12T06:30:00" {
		t.Fatalf("as_of = %v", curated["as_of"])
	}
	assertNum(t, curated["remaining_hours"], 28.8, 0)
}

func TestCollectDoesNotLabelCurrentActivityEstimateAsPastDate(t *testing.T) {
	now := time.Date(2026, 10, 19, 18, 0, 0, 0, time.UTC)
	f := newFakeGarmin(t)
	f.on("GET "+recoveryReadinessPath, []any{})
	f.on("GET "+recoveryReadinessPath, nil)
	f.on("GET "+recoveryActivityPath, []any{map[string]any{
		"activityId":     42,
		"beginTimestamp": float64(time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC).UnixMilli()),
		"duration":       3600,
		"recoveryTime":   1440,
	}})
	cs := healthClient(t, f)

	curated, err := collectRecoveryTime(context.Background(), cs, "2026-09-12", now)
	if err != nil {
		t.Fatalf("collectRecoveryTime: %v", err)
	}
	if curated["state"] != "unavailable" {
		t.Fatalf("state = %v", curated["state"])
	}
	message, _ := curated["message"].(string)
	if !strings.Contains(message, "only available for today") {
		t.Fatalf("message = %q", message)
	}
	if _, ok := curated["activity_id"]; ok {
		t.Fatal("activity_id should not be present")
	}
	if calls := f.callsMatching("GET", recoveryActivityPath); len(calls) != 0 {
		t.Fatalf("activities calls = %d, want 0", len(calls))
	}
}

func TestCollectSurfacesErrorsWhenAllSourcesFail(t *testing.T) {
	now := time.Date(2026, 9, 12, 18, 0, 0, 0, time.UTC)
	f := newFakeGarmin(t)
	f.on("GET "+recoveryReadinessPath, sequence{
		status(500, map[string]any{"message": "readiness unavailable"}),
		status(500, map[string]any{"message": "morning unavailable"}),
	})
	f.on("GET "+recoveryActivityPath, status(500, map[string]any{"message": "activities unavailable"}))
	cs := healthClient(t, f)

	_, err := collectRecoveryTime(context.Background(), cs, "2026-09-12", now)
	if err == nil || !strings.Contains(err.Error(), "readiness unavailable") {
		t.Fatalf("err = %v", err)
	}
}

func TestCollectKeepsSourceErrorsWhenAnotherSourceSucceeds(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+recoveryReadinessPath, sequence{
		status(500, map[string]any{"message": "readiness unavailable"}),
		map[string]any{"recoveryTime": 120},
	})
	cs := healthClient(t, f)

	// A day other than the requested one keeps the activity fallback out of it.
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	curated, err := collectRecoveryTime(context.Background(), cs, "2026-09-12", now)
	if err != nil {
		t.Fatalf("collectRecoveryTime: %v", err)
	}
	assertNum(t, curated["remaining_hours"], 2, 0)
	if curated["source"] != "morning_training_readiness" {
		t.Fatalf("source = %v", curated["source"])
	}
	errs, ok := curated["source_errors"].([]map[string]any)
	if !ok || len(errs) != 1 {
		t.Fatalf("source_errors = %v", curated["source_errors"])
	}
	if errs[0]["source"] != "get_training_readiness" {
		t.Fatalf("source_errors = %v", errs)
	}
	if errs[0]["error"] != "API Error 500 - readiness unavailable" {
		t.Fatalf("source_errors = %v", errs)
	}
}

func TestCollectSurfacesActivityFailureAfterEmptyReadiness(t *testing.T) {
	now := time.Date(2026, 9, 12, 18, 0, 0, 0, time.UTC)
	f := newFakeGarmin(t)
	f.on("GET "+recoveryReadinessPath, []any{})
	f.on("GET "+recoveryReadinessPath, nil)
	f.on("GET "+recoveryActivityPath, status(500, map[string]any{"message": "activity request failed"}))
	cs := healthClient(t, f)

	_, err := collectRecoveryTime(context.Background(), cs, "2026-09-12", now)
	if err == nil || !strings.Contains(err.Error(), "activity request failed") {
		t.Fatalf("err = %v", err)
	}
}

// --- recovery time tool ---

func TestGetRecoveryTimeRemainingFromReadiness(t *testing.T) {
	path := "/metrics-service/metrics/trainingreadiness/2026-09-12"
	f := newFakeGarmin(t)
	f.on("GET "+path, []any{
		map[string]any{
			"calendarDate":             "2026-09-12",
			"timestampLocal":           "2026-09-12T08:00:00",
			"recoveryTime":             720,
			"score":                    64,
			"level":                    "MODERATE",
			"recoveryTimeChangePhrase": "NO_CHANGE_SLEEP",
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_recovery_time_remaining", map[string]any{"date": "2026-09-12"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	assertNum(t, data["remaining_hours"], 12.0, 0)
	assertNum(t, data["recovery_score"], 64, 0)
	if data["state"] != "recovering" || data["source"] != "training_readiness" {
		t.Fatalf("data = %v", data)
	}
	if calls := f.callsMatching("GET", path); len(calls) != 1 {
		t.Fatalf("readiness calls = %d, want 1", len(calls))
	}
}

func TestGetRecoveryTimeRemainingUnavailable(t *testing.T) {
	path := "/metrics-service/metrics/trainingreadiness/2026-09-12"
	morning := "/metrics-service/metrics/trainingreadiness/2026-09-12"
	f := newFakeGarmin(t)
	f.on("GET "+path, []any{})
	f.on("GET "+morning, nil)
	f.on("GET "+activitiesSearchPath, []any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_recovery_time_remaining", map[string]any{"date": "2026-09-12"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["state"] != "unavailable" || data["remaining_hours"] != nil {
		t.Fatalf("data = %v", data)
	}
	if calls := f.callsMatching("GET", activitiesSearchPath); len(calls) != 0 {
		t.Fatalf("activities calls = %d, want 0", len(calls))
	}
}

func TestRecoveryTimeSurfacesFailedRequestsInsteadOfEmptySources(t *testing.T) {
	path := "/metrics-service/metrics/trainingreadiness/" + dayFromNow(0)
	f := newFakeGarmin(t)
	f.on("GET "+path, sequence{
		status(500, map[string]any{"message": "expired"}),
		status(500, map[string]any{"message": "expired"}),
	})
	f.on("GET "+activitiesSearchPath, status(500, map[string]any{"message": "expired"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_recovery_time_remaining", nil)
	if !strings.HasPrefix(text, "Error retrieving recovery time remaining:") {
		t.Fatalf("text = %q", text)
	}
	if !strings.Contains(text, "expired") {
		t.Fatalf("text = %q", text)
	}
	if strings.Contains(text, "snapshots were empty") {
		t.Fatalf("text = %q", text)
	}
}
