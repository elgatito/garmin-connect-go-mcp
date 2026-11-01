package server

import (
	"math"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Ported from tests/integration/test_health_wellness_tools.py.

const (
	summaryRoute   = "GET /usersummary-service/usersummary/daily/testuser"
	statsDayPrefix = "/usersummary-service/stats/daily/"
	nutritionRoute = "GET /nutrition-service/food/logs/range"
	bodyCompRoute  = "GET /weight-service/weight/dateRange"
	sleepRoute     = "GET /wellness-service/wellness/dailySleepData/testuser"
	readinessPath  = "/metrics-service/metrics/trainingreadiness/"
	activitiesPath = "GET /activitylist-service/activities/search/activities"
)

func today() string { return time.Now().Format(time.DateOnly) }

func daysAgo(n int) string { return time.Now().AddDate(0, 0, -n).Format(time.DateOnly) }

// at walks nested maps of a decoded result.
func at(v any, keys ...string) any {
	for _, k := range keys {
		v = asMap(v)[k]
	}
	return v
}

func approx(t *testing.T, name string, got any, want, tol float64) {
	t.Helper()
	f, ok := got.(float64)
	if !ok || math.Abs(f-want) > tol {
		t.Errorf("%s = %v, want %v +- %v", name, got, want, tol)
	}
}

func query(t *testing.T, f *fakeGarmin, route string) url.Values {
	t.Helper()
	reqs := f.requests(route)
	if len(reqs) != 1 {
		t.Fatalf("%d requests to %s, want 1", len(reqs), route)
	}
	return reqs[0].Query
}

// passthrough calls a tool whose result is the raw Garmin payload and checks the one request it made.
func passthrough(t *testing.T, tool string, args map[string]any, route, fixtureName string, wantQuery map[string]string) {
	t.Helper()
	f := newFakeGarmin(t)
	f.on(route, fixture(t, fixtureName))
	text := callOK(t, newSession(t, f, nil), tool, args)
	if got := decode(t, text); !reflect.DeepEqual(got, fixture(t, fixtureName)) {
		t.Errorf("result = %v, want fixture %s", got, fixtureName)
	}
	q := query(t, f, route)
	for k, v := range wantQuery {
		if q.Get(k) != v {
			t.Errorf("query %s = %q, want %q (%v)", k, q.Get(k), v, q)
		}
	}
}

// callWith registers body for route, calls tool and returns the fake and the result text.
func callWith(t *testing.T, route string, body any, tool string, args map[string]any) (*fakeGarmin, string) {
	t.Helper()
	f := newFakeGarmin(t)
	f.on(route, body)
	return f, callOK(t, newSession(t, f, nil), tool, args)
}

func TestGetStatsTool(t *testing.T) {
	f, text := callWith(t, summaryRoute, fixture(t, "MOCK_STATS"), "get_stats", map[string]any{"date": "2024-01-15"})
	want := map[string]any{
		"total_steps": 10000.0, "daily_step_goal": 8000.0, "total_calories": 2500.0, "active_calories": 800.0, "bmr_calories": 1700.0,
		"resting_heart_rate_bpm": 55.0, "avg_stress_level": 25.0, "max_stress_level": 60.0,
	}
	if got := decode(t, text); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
	if q := query(t, f, summaryRoute); q.Get("calendarDate") != "2024-01-15" {
		t.Error(q)
	}
}

func TestGetStatsDropsMissingFields(t *testing.T) {
	_, text := callWith(t, summaryRoute, map[string]any{"calendarDate": "2024-01-15"}, "get_stats", map[string]any{"date": "2024-01-15"})
	if got := decode(t, text); !reflect.DeepEqual(got, map[string]any{"date": "2024-01-15"}) {
		t.Errorf("got %v", got)
	}
}

func TestGetStatsEmptyResponseIsError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(summaryRoute, map[string]any{})
	cs := newSession(t, f, nil)
	text, isErr := callTool(t, cs, "get_stats", map[string]any{"date": "2024-01-15"})
	if !isErr || text != "Error retrieving stats: No data received from server" {
		t.Errorf("%v %q", isErr, text)
	}
	text, isErr = callTool(t, cs, "get_stats", map[string]any{"date": "2024-1-5"})
	if !isErr || !strings.Contains(text, "cdate must be in format 'YYYY-MM-DD', got: 2024-1-5") {
		t.Errorf("%v %q", isErr, text)
	}
}

// statsByType answers the stats/daily endpoint with a body chosen by the statsType query.
func statsByType(calories, steps any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body := calories
		if r.URL.Query().Get("statsType") == "STEPS" {
			body = steps
		}
		writeBody(w, r, http.StatusOK, body)
	}
}

func TestGetStatsRangeTool(t *testing.T) {
	td, yd := today(), daysAgo(1)
	f := newFakeGarmin(t)
	route := "GET " + statsDayPrefix + yd + "/" + td
	f.on(route, statsByType(
		map[string]any{"values": []any{
			map[string]any{"calendarDate": yd, "values": map[string]any{"restingCalories": 2081, "totalCalories": 2656, "activeCalories": 575}},
			map[string]any{"calendarDate": td, "values": map[string]any{"restingCalories": 1393, "totalCalories": 1900, "activeCalories": 507}},
		}},
		map[string]any{"values": []any{map[string]any{"calendarDate": yd, "values": map[string]any{"totalSteps": 9238}}}},
	))
	text := callOK(t, newSession(t, f, nil), "get_stats_range", map[string]any{"start_date": yd, "end_date": td})
	data := asMap(decode(t, text))
	days := map[string]map[string]any{}
	for _, d := range asList(data["days"]) {
		days[asMap(d)["date"].(string)] = asMap(d)
	}
	past := days[yd]
	if past["total_calories"] != 2656.0 || past["active_calories"] != 575.0 || past["resting_calories"] != 2081.0 ||
		past["steps"] != 9238.0 || past["has_data"] != true || past["is_partial"] != false {
		t.Errorf("past day: %v", past)
	}
	cur := days[td]
	if cur["total_calories"] != 1900.0 || cur["steps"] != nil || cur["has_data"] != true || cur["is_partial"] != true {
		t.Errorf("today: %v", cur)
	}
	if data["start_date"] != yd || data["end_date"] != td {
		t.Errorf("bounds: %v", data)
	}
	var types []string
	for _, c := range f.requests(route) {
		types = append(types, c.Query.Get("statsType"))
	}
	if !reflect.DeepEqual(types, []string{"CALORIES", "STEPS"}) {
		t.Errorf("statsType calls = %v", types)
	}
}

func TestGetStatsRangeNoDataDay(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+statsDayPrefix+"2024-01-14/2024-01-15", statsByType(
		map[string]any{"values": []any{map[string]any{"calendarDate": "2024-01-14", "values": map[string]any{"restingCalories": 2081, "totalCalories": 2656, "activeCalories": 575}}}},
		map[string]any{"values": []any{map[string]any{"calendarDate": "2024-01-14", "values": map[string]any{"totalSteps": 9238}}}},
	))
	text := callOK(t, newSession(t, f, nil), "get_stats_range", map[string]any{"start_date": "2024-01-14", "end_date": "2024-01-15"})
	missing := asMap(asList(at(decode(t, text), "days"))[1])
	want := map[string]any{"date": "2024-01-15", "total_calories": nil, "active_calories": nil, "resting_calories": nil,
		"steps": nil, "has_data": false, "is_partial": false}
	if !reflect.DeepEqual(missing, want) {
		t.Errorf("got %v", missing)
	}
}

func TestGetStatsRangeRejectsOversizedRange(t *testing.T) {
	f := newFakeGarmin(t)
	text := callOK(t, newSession(t, f, nil), "get_stats_range", map[string]any{"start_date": "2024-01-01", "end_date": "2024-03-01"})
	if text != "Date range too large (61 days). Maximum is 28 days." {
		t.Error(text)
	}
	if n := len(f.requests("")); n != 0 {
		t.Errorf("%d requests", n)
	}
}

func TestGetStatsRangeRejectsInvertedRange(t *testing.T) {
	f := newFakeGarmin(t)
	text := callOK(t, newSession(t, f, nil), "get_stats_range", map[string]any{"start_date": "2024-01-15", "end_date": "2024-01-01"})
	if text != "end_date must be on or after start_date." {
		t.Error(text)
	}
	if n := len(f.requests("")); n != 0 {
		t.Errorf("%d requests", n)
	}
	text = callOK(t, newSession(t, f, nil), "get_stats_range", map[string]any{"start_date": "nope", "end_date": "2024-01-01"})
	if text != "Invalid date format: Invalid isoformat string: 'nope'. Use YYYY-MM-DD." {
		t.Error(text)
	}
}

func TestGetStatsRangeError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+statsDayPrefix+"2024-01-14/2024-01-15", status(500, map[string]any{"message": "API error"}))
	text, isErr := callTool(t, newSession(t, f, nil), "get_stats_range", map[string]any{"start_date": "2024-01-14", "end_date": "2024-01-15"})
	if !isErr || !strings.HasPrefix(text, "Error retrieving stats range: ") || !strings.Contains(text, "API error") {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetStatsRangeNoStats(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+statsDayPrefix+"2024-01-14/2024-01-15", map[string]any{"values": []any{}})
	text := callOK(t, newSession(t, f, nil), "get_stats_range", map[string]any{"start_date": "2024-01-14", "end_date": "2024-01-15"})
	if text != "No stats found between 2024-01-14 and 2024-01-15." {
		t.Error(text)
	}
}

// --- get_energy_balance ---

// intakeResp builds a /nutrition-service/food/logs/range response from (mealDate, calories or nil, item count).
func intakeResp(days ...[3]any) any {
	var out []any
	for _, d := range days {
		foods := make([]any, d[2].(int))
		for i := range foods {
			foods[i] = map[string]any{}
		}
		e := map[string]any{"mealDate": d[0], "mealDetails": []any{map[string]any{"loggedFoods": foods}}}
		if d[1] != nil {
			e["dailyNutritionContent"] = map[string]any{"calories": d[1]}
		}
		out = append(out, e)
	}
	return map[string]any{"dailyNutritionSummaries": out}
}

func caloriesResp(days ...[2]any) any {
	vals := []any{}
	for _, d := range days {
		vals = append(vals, map[string]any{"calendarDate": d[0], "values": map[string]any{"totalCalories": d[1]}})
	}
	return map[string]any{"values": vals}
}

func weighIns(entries ...map[string]any) any {
	l := make([]any, len(entries))
	for i, e := range entries {
		l[i] = e
	}
	return map[string]any{"dateWeightList": l}
}

func weighIn(date string, weight, fat any) map[string]any {
	return map[string]any{"calendarDate": date, "weight": weight, "bodyFat": fat}
}

// energyBalance wires the three endpoints get_energy_balance reads (calories as a single chunk) and calls it.
func energyBalance(t *testing.T, start, end string, intake, calories, body any) (*fakeGarmin, string) {
	t.Helper()
	f := newFakeGarmin(t)
	f.on(nutritionRoute, intake)
	f.on("GET "+statsDayPrefix+start+"/"+end, calories)
	f.on(bodyCompRoute, body)
	return f, callOK(t, newSession(t, f, nil), "get_energy_balance", map[string]any{"start_date": start, "end_date": end})
}

func TestGetEnergyBalanceDerivesTdee(t *testing.T) {
	f, text := energyBalance(t, "2024-01-01", "2024-01-03",
		intakeResp([3]any{"2024-01-01", nil, 0}, [3]any{"2024-01-02", 2000, 5}, [3]any{"2024-01-03", 2000, 4}),
		caloriesResp([2]any{"2024-01-01", 2500}, [2]any{"2024-01-02", 2500}),
		weighIns(weighIn("2024-01-01", 80000.0, 20.0), weighIn("2024-01-25", 79000.0, 18.75), weighIn("2024-01-05", 79500.0, nil)))
	data := decode(t, text)

	for path, want := range map[string]any{
		"intake/days_included": 2.0, "intake/days_excluded_unlogged": 1.0, "intake/mean_calories_per_day": 2000.0,
		"expenditure_garmin/days_included": 2.0, "expenditure_garmin/days_excluded": 1.0,
		"expenditure_garmin/mean_total_calories_per_day": 2500.0,
		"body_composition/first_reading_date":            "2024-01-01", "body_composition/last_reading_date": "2024-01-25",
		"body_composition/days_between_readings": 24.0, "body_composition/observed_first_weight_kg": 80.0,
		"body_composition/observed_last_weight_kg": 79.0, "body_composition/observed_first_body_fat_percent": 20.0,
		"body_composition/observed_last_body_fat_percent": 18.75, "body_composition/fitted_first_weight_kg": 80.0,
		"body_composition/fitted_last_weight_kg": 79.0, "body_composition/weight_change_kg": -1.0,
		"derived/garmin_tdee_kcal_per_day":                 2500.0,
		"derived/implied_deficit_uncertainty_kcal_per_day": nil,
		"assumptions/fat_kcal_per_kg":                      7700.0,
		"days_requested":                                   3.0,
	} {
		if got := at(data, strings.Split(path, "/")...); got != want {
			t.Errorf("%s = %v, want %v", path, got, want)
		}
	}
	if got := at(data, "intake", "low_item_count_dates"); !reflect.DeepEqual(got, []any{}) {
		t.Errorf("low_item_count_dates = %#v", got)
	}
	approx(t, "fat_mass_change_kg", at(data, "body_composition", "fat_mass_change_kg"), -1.1875, 0.001)
	approx(t, "lean_mass_change_kg", at(data, "body_composition", "lean_mass_change_kg"), 0.1875, 0.001)
	approx(t, "implied_deficit", at(data, "derived", "implied_deficit_kcal_per_day"), 366.927, 0.1)
	approx(t, "tdee", at(data, "derived", "composition_derived_tdee_kcal_per_day"), 2366.927, 0.1)
	if _, ok := asMap(data)["gate_note"]; ok {
		t.Error("unexpected gate_note")
	}
	if _, ok := asMap(at(data, "derived"))["uncertainty_note"]; !ok {
		t.Error("missing uncertainty_note for exactly 2 readings")
	}

	if q := query(t, f, nutritionRoute); q.Get("startDate") != "2024-01-01" || q.Get("endDate") != "2024-01-03" {
		t.Errorf("nutrition query %v", q)
	}
	if q := query(t, f, "GET "+statsDayPrefix+"2024-01-01/2024-01-03"); q.Get("statsType") != "CALORIES" {
		t.Errorf("stats query %v", q)
	}
	if q := query(t, f, bodyCompRoute); q.Get("startDate") != "2024-01-01" || q.Get("endDate") != "2024-01-03" {
		t.Errorf("body composition query %v", q)
	}
}

func TestGetEnergyBalanceExcludesPartialAndNoDataDays(t *testing.T) {
	td, noDevice, complete := today(), daysAgo(1), daysAgo(2)
	_, text := energyBalance(t, complete, td,
		intakeResp([3]any{complete, 2000, 5}, [3]any{noDevice, 2000, 5}, [3]any{td, 2000, 5}),
		caloriesResp([2]any{complete, 2600}, [2]any{td, 9999}),
		weighIns())
	data := decode(t, text)
	exp := asMap(at(data, "expenditure_garmin"))
	if exp["days_included"] != 1.0 || exp["days_excluded"] != 2.0 || exp["mean_total_calories_per_day"] != 2600.0 {
		t.Errorf("expenditure_garmin = %v", exp)
	}
}

func TestGetEnergyBalanceSurfacesLowItemCountDays(t *testing.T) {
	_, text := energyBalance(t, "2024-01-01", "2024-01-03",
		intakeResp([3]any{"2024-01-01", 2000, 5}, [3]any{"2024-01-02", 800, 1}, [3]any{"2024-01-03", nil, 0}),
		caloriesResp(), weighIns())
	in := asMap(at(decode(t, text), "intake"))
	if in["days_included"] != 2.0 || in["days_excluded_unlogged"] != 1.0 || in["mean_calories_per_day"] != 1400.0 ||
		!reflect.DeepEqual(in["low_item_count_dates"], []any{"2024-01-02"}) {
		t.Errorf("intake = %v", in)
	}
}

func TestGetEnergyBalanceFitsTrendAcrossMultipleReadings(t *testing.T) {
	_, text := energyBalance(t, "2024-01-01", "2024-01-25",
		intakeResp([3]any{"2024-01-01", 2000, 5}), caloriesResp([2]any{"2024-01-01", 2600}),
		weighIns(weighIn("2024-01-01", 80000.0, 20.0), weighIn("2024-01-07", 79500.0, 20.0), weighIn("2024-01-13", 79000.0, 20.0),
			weighIn("2024-01-19", 78500.0, 20.0), weighIn("2024-01-25", 78000.0, 20.0)))
	data := decode(t, text)
	bc := asMap(at(data, "body_composition"))
	if bc["readings_used"] != 5.0 || bc["days_between_readings"] != 24.0 {
		t.Errorf("body_composition = %v", bc)
	}
	if _, ok := asMap(data)["gate_note"]; ok {
		t.Error("unexpected gate_note")
	}
	approx(t, "weight_change_kg", bc["weight_change_kg"], -2.0, 0.01)
	approx(t, "fitted_first_weight_kg", bc["fitted_first_weight_kg"], 80.0, 0.01)
	approx(t, "fitted_last_weight_kg", bc["fitted_last_weight_kg"], 78.0, 0.01)
	approx(t, "uncertainty", at(data, "derived", "implied_deficit_uncertainty_kcal_per_day"), 0.0, 0.01)
	if _, ok := asMap(at(data, "derived"))["uncertainty_note"]; ok {
		t.Error("unexpected uncertainty_note")
	}
}

func TestGetEnergyBalanceGatesOutShortSpan(t *testing.T) {
	_, text := energyBalance(t, "2024-01-01", "2024-01-19",
		intakeResp([3]any{"2024-01-01", 2000, 5}), caloriesResp([2]any{"2024-01-01", 2600}),
		weighIns(weighIn("2024-01-01", 90000.0, 28.0), weighIn("2024-01-18", 91000.0, 27.5), weighIn("2024-01-19", 89670.0, 28.3)))
	data := asMap(decode(t, text))
	if asMap(data["body_composition"])["readings_used"] != 3.0 {
		t.Errorf("body_composition = %v", data["body_composition"])
	}
	if _, ok := data["derived"]; ok {
		t.Error("derived must be omitted")
	}
	if _, ok := data["assumptions"]; ok {
		t.Error("assumptions must be omitted")
	}
	if note, _ := data["gate_note"].(string); !strings.Contains(note, "18 days") || !strings.HasPrefix(note, "Derived TDEE suppressed: ") {
		t.Errorf("gate_note = %q", note)
	}
}

func TestGetEnergyBalanceGatesOutClusteredLeverage(t *testing.T) {
	_, text := energyBalance(t, "2024-01-01", "2024-01-24",
		intakeResp([3]any{"2024-01-01", 2000, 5}), caloriesResp([2]any{"2024-01-01", 2600}),
		weighIns(weighIn("2024-01-01", 90000.0, 28.0), weighIn("2024-01-23", 89000.0, 27.5), weighIn("2024-01-24", 88670.0, 27.3)))
	data := asMap(decode(t, text))
	if _, ok := data["derived"]; ok {
		t.Error("derived must be omitted")
	}
	if note, _ := data["gate_note"].(string); !strings.Contains(note, "cluster") {
		t.Errorf("gate_note = %q", note)
	}
}

func TestGetEnergyBalanceDailyWeighInsAreNotOneCluster(t *testing.T) {
	var entries []map[string]any
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 28; i++ {
		entries = append(entries, weighIn(start.AddDate(0, 0, i).Format(time.DateOnly), 80000.0-50.0*float64(i), 20.0))
	}
	_, text := energyBalance(t, "2024-01-01", "2024-01-28",
		intakeResp([3]any{"2024-01-01", 2000, 5}), caloriesResp([2]any{"2024-01-01", 2600}), weighIns(entries...))
	data := decode(t, text)
	if at(data, "body_composition", "readings_used") != 28.0 {
		t.Errorf("body_composition = %v", at(data, "body_composition"))
	}
	if _, ok := asMap(data)["gate_note"]; ok {
		t.Errorf("gate_note = %v", asMap(data)["gate_note"])
	}
	approx(t, "implied_deficit", at(data, "derived", "implied_deficit_kcal_per_day"), 0.010*7700+0.040*1800, 0.1)
}

func floats(xs ...int) []float64 {
	out := make([]float64, len(xs))
	for i, x := range xs {
		out[i] = float64(x)
	}
	return out
}

func TestBodyCompGateRegularWeighInsPass(t *testing.T) {
	var daily, everyOther []int
	for i := 0; i < 28; i++ {
		daily = append(daily, i)
	}
	for i := 0; i <= 28; i += 2 {
		everyOther = append(everyOther, i)
	}
	for _, xs := range [][]int{daily, everyOther} {
		if got := bodyCompGateFailure(floats(xs...)); got != "" {
			t.Errorf("gate failed: %s", got)
		}
	}
}

func TestBodyCompGateStillCatchesADenseCluster(t *testing.T) {
	for _, xs := range [][]int{{0, 1, 2, 25}, {0, 22, 23}, {0, 23, 24, 25}} {
		if got := bodyCompGateFailure(floats(xs...)); !strings.Contains(got, "cluster") {
			t.Errorf("%v: %q", xs, got)
		}
	}
}

func TestGetEnergyBalanceUncertaintyFormulaIsPinned(t *testing.T) {
	_, text := energyBalance(t, "2024-01-01", "2024-01-24",
		intakeResp([3]any{"2024-01-01", 2000, 5}), caloriesResp([2]any{"2024-01-01", 2600}),
		weighIns(weighIn("2024-01-01", 90000.0, 28.0), weighIn("2024-01-09", 89300.0, 27.6),
			weighIn("2024-01-16", 88900.0, 27.5), weighIn("2024-01-24", 88000.0, 27.0)))
	data := decode(t, text)
	approx(t, "weight_change_kg", at(data, "body_composition", "weight_change_kg"), -1.942, 0.005)
	approx(t, "fat_mass_change_kg", at(data, "body_composition", "fat_mass_change_kg"), -1.373, 0.0005)
	approx(t, "lean_mass_change_kg", at(data, "body_composition", "lean_mass_change_kg"), -0.568, 0.0005)
	approx(t, "implied_deficit", at(data, "derived", "implied_deficit_kcal_per_day"), 504.289, 0.1)
	approx(t, "uncertainty", at(data, "derived", "implied_deficit_uncertainty_kcal_per_day"), 55.664, 0.1)
	approx(t, "tdee", at(data, "derived", "composition_derived_tdee_kcal_per_day"), 2504.289, 0.1)
}

func TestGetEnergyBalanceSingleBodyCompReading(t *testing.T) {
	_, text := energyBalance(t, "2024-01-01", "2024-01-01",
		intakeResp([3]any{"2024-01-01", 2000, 3}), caloriesResp([2]any{"2024-01-01", 2500}),
		weighIns(weighIn("2024-01-01", 80000.0, 20.0)))
	data := asMap(decode(t, text))
	if _, ok := data["derived"]; ok {
		t.Error("derived must be omitted")
	}
	want := "Only 1 qualifying body-composition reading (weight + body fat %) in range, on 2024-01-01. Need at least 2 to fit a trend."
	if data["body_composition_note"] != want {
		t.Errorf("note = %v", data["body_composition_note"])
	}
}

func TestGetEnergyBalanceChunksExpenditureOver28Days(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(nutritionRoute, intakeResp())
	f.on("GET "+statsDayPrefix+"2024-01-01/2024-01-28", caloriesResp([2]any{"2024-01-01", 2500}))
	f.on("GET "+statsDayPrefix+"2024-01-29/2024-02-05", caloriesResp([2]any{"2024-02-05", 2600}))
	f.on(bodyCompRoute, weighIns())
	text := callOK(t, newSession(t, f, nil), "get_energy_balance", map[string]any{"start_date": "2024-01-01", "end_date": "2024-02-05"})
	data := decode(t, text)
	if got := len(f.requests("GET " + statsDayPrefix + "2024-01-01/2024-01-28")); got != 1 {
		t.Errorf("chunk 1 requests = %d", got)
	}
	if got := len(f.requests("GET " + statsDayPrefix + "2024-01-29/2024-02-05")); got != 1 {
		t.Errorf("chunk 2 requests = %d", got)
	}
	exp := asMap(at(data, "expenditure_garmin"))
	if exp["days_included"] != 2.0 || exp["mean_total_calories_per_day"] != 2550.0 {
		t.Errorf("expenditure_garmin = %v", exp)
	}
}

func TestGetEnergyBalanceRejectsOversizedRange(t *testing.T) {
	f := newFakeGarmin(t)
	text := callOK(t, newSession(t, f, nil), "get_energy_balance", map[string]any{"start_date": "2024-01-01", "end_date": "2024-04-01"})
	if text != "Date range too large (92 days). Maximum is 61 days." {
		t.Error(text)
	}
	if n := len(f.requests("")); n != 0 {
		t.Errorf("%d requests", n)
	}
}

func TestGetEnergyBalanceRejectsInvertedRange(t *testing.T) {
	f := newFakeGarmin(t)
	text := callOK(t, newSession(t, f, nil), "get_energy_balance", map[string]any{"start_date": "2024-01-15", "end_date": "2024-01-01"})
	if text != "end_date must be on or after start_date." {
		t.Error(text)
	}
	if n := len(f.requests("")); n != 0 {
		t.Errorf("%d requests", n)
	}
}

func TestGetEnergyBalanceNoUsableData(t *testing.T) {
	_, text := energyBalance(t, "2024-01-01", "2024-01-03", intakeResp(), caloriesResp(), weighIns())
	if text != "No usable data found between 2024-01-01 and 2024-01-03." {
		t.Error(text)
	}
}

func TestGetEnergyBalanceError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(nutritionRoute, status(500, map[string]any{"message": "API error"}))
	text, isErr := callTool(t, newSession(t, f, nil), "get_energy_balance", map[string]any{"start_date": "2024-01-01", "end_date": "2024-01-03"})
	if !isErr || !strings.HasPrefix(text, "Error retrieving energy balance data: ") {
		t.Errorf("%v %q", isErr, text)
	}
}

// --- plain pass-through tools ---

func TestGetUserSummaryTool(t *testing.T) {
	passthrough(t, "get_user_summary", map[string]any{"date": "2024-01-15"}, summaryRoute, "MOCK_USER_SUMMARY",
		map[string]string{"calendarDate": "2024-01-15"})
}

func TestGetBodyCompositionSingleDate(t *testing.T) {
	passthrough(t, "get_body_composition", map[string]any{"start_date": "2024-01-15"}, bodyCompRoute, "MOCK_BODY_COMPOSITION",
		map[string]string{"startDate": "2024-01-15", "endDate": "2024-01-15"})
}

func TestGetBodyCompositionDateRange(t *testing.T) {
	passthrough(t, "get_body_composition", map[string]any{"start_date": "2024-01-08", "end_date": "2024-01-15"}, bodyCompRoute,
		"MOCK_BODY_COMPOSITION", map[string]string{"startDate": "2024-01-08", "endDate": "2024-01-15"})
}

func TestGetStatsAndBodyTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(summaryRoute, fixture(t, "MOCK_STATS"))
	f.on(bodyCompRoute, map[string]any{"totalAverage": fixture(t, "MOCK_BODY_COMPOSITION")})
	text := callOK(t, newSession(t, f, nil), "get_stats_and_body", map[string]any{"date": "2024-01-15"})
	want := asMap(fixture(t, "MOCK_STATS"))
	for k, v := range asMap(fixture(t, "MOCK_BODY_COMPOSITION")) {
		want[k] = v
	}
	if got := decode(t, text); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
	if q := query(t, f, summaryRoute); q.Get("calendarDate") != "2024-01-15" {
		t.Error(q)
	}
	if q := query(t, f, bodyCompRoute); q.Get("startDate") != "2024-01-15" || q.Get("endDate") != "2024-01-15" {
		t.Error(q)
	}
}

func TestGetStepsDataTool(t *testing.T) {
	passthrough(t, "get_steps_data", map[string]any{"date": "2024-01-15"}, "GET /wellness-service/wellness/dailySummaryChart/testuser",
		"MOCK_STEPS_DATA", map[string]string{"date": "2024-01-15"})
}

func TestGetDailyStepsTool(t *testing.T) {
	passthrough(t, "get_daily_steps", map[string]any{"start_date": "2024-01-08", "end_date": "2024-01-15"},
		"GET /usersummary-service/stats/steps/daily/2024-01-08/2024-01-15", "MOCK_DAILY_STEPS", nil)
}

func TestGetDailyStepsChunksLongRanges(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /usersummary-service/stats/steps/daily/2024-01-01/2024-01-28", []any{map[string]any{"calendarDate": "2024-01-01"}})
	f.on("GET /usersummary-service/stats/steps/daily/2024-01-29/2024-02-05", []any{map[string]any{"calendarDate": "2024-02-05"}})
	text := callOK(t, newSession(t, f, nil), "get_daily_steps", map[string]any{"start_date": "2024-01-01", "end_date": "2024-02-05"})
	want := []any{map[string]any{"calendarDate": "2024-01-01"}, map[string]any{"calendarDate": "2024-02-05"}}
	if got := decode(t, text); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestGetTrainingReadinessTool(t *testing.T) {
	f, text := callWith(t, "GET "+readinessPath+"2024-01-15", fixture(t, "MOCK_TRAINING_READINESS"), "get_training_readiness",
		map[string]any{"date": "2024-01-15"})
	data := asList(decode(t, text))
	if len(data) != 2 || asMap(data[0])["context"] != "AFTER_WAKEUP_RESET" || asMap(data[1])["context"] != "AFTER_POST_EXERCISE_RESET" {
		t.Fatalf("data = %v", data)
	}
	morning := asMap(data[0])
	for k, v := range map[string]any{"date": "2024-01-15", "score": 62.0, "level": "MODERATE", "feedback": "WELL_RECOVERED",
		"sleep_score": 81.0, "recovery_time_hours": 5.5, "acute_load": 310.0, "hrv_weekly_avg": 58.0} {
		if morning[k] != v {
			t.Errorf("morning[%s] = %v, want %v", k, morning[k], v)
		}
	}
	if asMap(data[1])["score"] != 38.0 || asMap(data[1])["recovery_time_hours"] != 18.0 {
		t.Errorf("second = %v", data[1])
	}
	if _, ok := asMap(data[1])["sleep_score"]; ok {
		t.Error("sleep_score must be dropped when absent")
	}
	if n := len(f.requests("GET " + readinessPath + "2024-01-15")); n != 1 {
		t.Errorf("%d requests", n)
	}
}

func TestGetBodyBatteryTool(t *testing.T) {
	f, text := callWith(t, "GET /wellness-service/wellness/bodyBattery/reports/daily", fixture(t, "MOCK_BODY_BATTERY"), "get_body_battery",
		map[string]any{"start_date": "2024-01-08", "end_date": "2024-01-15"})
	want := []any{map[string]any{"date": nil, "charged": nil, "drained": nil, "events": []any{}}}
	if got := decode(t, text); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	q := query(t, f, "GET /wellness-service/wellness/bodyBattery/reports/daily")
	if q.Get("startDate") != "2024-01-08" || q.Get("endDate") != "2024-01-15" {
		t.Error(q)
	}
}

func TestGetBodyBatteryEventsTool(t *testing.T) {
	passthrough(t, "get_body_battery_events", map[string]any{"date": "2024-01-15"},
		"GET /wellness-service/wellness/bodyBattery/events/2024-01-15", "MOCK_BODY_BATTERY_EVENTS", nil)
}

func TestGetBloodPressureTool(t *testing.T) {
	passthrough(t, "get_blood_pressure", map[string]any{"start_date": "2024-01-08", "end_date": "2024-01-15"},
		"GET /bloodpressure-service/bloodpressure/range/2024-01-08/2024-01-15", "MOCK_BLOOD_PRESSURE", map[string]string{"includeAll": "True"})
}

func TestGetFloorsTool(t *testing.T) {
	passthrough(t, "get_floors", map[string]any{"date": "2024-01-15"},
		"GET /wellness-service/wellness/floorsChartData/daily/2024-01-15", "MOCK_FLOORS", nil)
}

func TestGetRhrDayTool(t *testing.T) {
	passthrough(t, "get_rhr_day", map[string]any{"date": "2024-01-15"}, "GET /userstats-service/wellness/daily/testuser",
		"MOCK_RHR_DAY", map[string]string{"fromDate": "2024-01-15", "untilDate": "2024-01-15", "metricId": "60"})
}

const heartRoute = "GET /wellness-service/wellness/dailyHeartRate/testuser"

func TestGetHeartRatesTool(t *testing.T) {
	passthrough(t, "get_heart_rates", map[string]any{"date": "2024-01-15"}, heartRoute, "MOCK_HEART_RATES", map[string]string{"date": "2024-01-15"})
}

func TestGetHeartRatesSummaryTool(t *testing.T) {
	f, text := callWith(t, heartRoute, fixture(t, "MOCK_HEART_RATES"), "get_heart_rates_summary", map[string]any{"date": "2024-01-15"})
	want := map[string]any{"max_heart_rate_bpm": 180.0, "min_heart_rate_bpm": 45.0, "resting_heart_rate_bpm": 55.0,
		"last_7_days_avg_resting_hr": 57.0, "avg_heart_rate_bpm": 65.0, "data_points_count": 3.0}
	if got := decode(t, text); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	if q := query(t, f, heartRoute); q.Get("date") != "2024-01-15" {
		t.Error(q)
	}
}

func TestGetHydrationDataTool(t *testing.T) {
	passthrough(t, "get_hydration_data", map[string]any{"date": "2024-01-15"},
		"GET /usersummary-service/usersummary/hydration/daily/2024-01-15", "MOCK_HYDRATION_DATA", nil)
}

func TestGetSleepDataTool(t *testing.T) {
	passthrough(t, "get_sleep_data", map[string]any{"date": "2024-01-15"}, sleepRoute, "MOCK_SLEEP_DATA",
		map[string]string{"date": "2024-01-15", "nonSleepBufferMinutes": "60"})
}

func TestGetSleepSummaryTool(t *testing.T) {
	f, text := callWith(t, sleepRoute, fixture(t, "MOCK_SLEEP_DATA"), "get_sleep_summary", map[string]any{"date": "2024-01-15"})
	s := asMap(decode(t, text))
	if s["resting_heart_rate_bpm"] != 55.0 || s["restless_moments_count"] != 15.0 || s["avg_spo2_percent"] != 96.0 || s["lowest_spo2_percent"] != 93.0 {
		t.Errorf("summary = %v", s)
	}
	if s["sleep_score"] != 85.0 || s["sleep_score_qualifier"] != "GOOD" || s["sleep_hours"] != 8.0 ||
		s["deep_sleep_percent"] != 25.0 || s["light_sleep_percent"] != 50.0 || s["avg_overnight_hrv"] != 45.0 {
		t.Errorf("summary = %v", s)
	}
	if q := query(t, f, sleepRoute); q.Get("date") != "2024-01-15" {
		t.Error(q)
	}
}

// sleepByDate answers the sleep endpoint per requested date: nil means an empty (null) response.
func sleepByDate(forDate func(date string) any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body := forDate(r.URL.Query().Get("date"))
		if body == nil {
			body = rawBody("null")
		}
		writeBody(w, r, http.StatusOK, body)
	}
}

func TestGetSleepSummaryRangeTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(sleepRoute, sleepByDate(func(date string) any {
		m := asMap(fixture(t, "MOCK_SLEEP_DATA"))
		dto := asMap(m["dailySleepDTO"])
		dto["calendarDate"] = date
		return m
	}))
	text := callOK(t, newSession(t, f, nil), "get_sleep_summary_range", map[string]any{"start_date": "2024-01-13", "end_date": "2024-01-15"})
	data := asMap(decode(t, text))
	if data["start_date"] != "2024-01-13" || data["end_date"] != "2024-01-15" || data["nights_requested"] != 3.0 || data["nights_returned"] != 3.0 {
		t.Errorf("data = %v", data)
	}
	var dates []any
	for _, n := range asList(data["nights"]) {
		dates = append(dates, asMap(n)["date"])
	}
	if !reflect.DeepEqual(dates, []any{"2024-01-13", "2024-01-14", "2024-01-15"}) {
		t.Errorf("dates = %v", dates)
	}
	first := asMap(asList(data["nights"])[0])
	if first["sleep_score"] != 85.0 || first["sleep_hours"] != 8.0 {
		t.Errorf("first night = %v", first)
	}
	if n := len(f.requests(sleepRoute)); n != 3 {
		t.Errorf("%d sleep requests", n)
	}
}

func TestGetSleepSummaryRangeSkipsNightsWithoutData(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(sleepRoute, sleepByDate(func(date string) any {
		if date == "2024-01-14" {
			return nil
		}
		return fixture(t, "MOCK_SLEEP_DATA")
	}))
	text := callOK(t, newSession(t, f, nil), "get_sleep_summary_range", map[string]any{"start_date": "2024-01-13", "end_date": "2024-01-15"})
	data := asMap(decode(t, text))
	nights := asList(data["nights"])
	if data["nights_requested"] != 3.0 || data["nights_returned"] != 2.0 || len(nights) != 2 ||
		asMap(nights[0])["date"] != "2024-01-13" || asMap(nights[1])["date"] != "2024-01-15" {
		t.Errorf("data = %v", data)
	}
}

func TestGetSleepSummaryRangeRejectsInvalidDates(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	text := callOK(t, cs, "get_sleep_summary_range", map[string]any{"start_date": "not-a-date", "end_date": "2024-01-15"})
	if !strings.Contains(text, "Invalid date format") {
		t.Error(text)
	}
	text = callOK(t, cs, "get_sleep_summary_range", map[string]any{"start_date": "2024-01-15", "end_date": "2024-01-13"})
	if !strings.Contains(text, "end_date must be on or after start_date") {
		t.Error(text)
	}
	if n := len(f.requests(sleepRoute)); n != 0 {
		t.Errorf("%d sleep requests", n)
	}
}

func TestGetSleepSummaryRangeEnforcesMaxDays(t *testing.T) {
	f := newFakeGarmin(t)
	text := callOK(t, newSession(t, f, nil), "get_sleep_summary_range", map[string]any{"start_date": "2024-01-01", "end_date": "2024-04-15"})
	if text != "Date range too large (106 days). Maximum is 90 days." {
		t.Error(text)
	}
	if n := len(f.requests(sleepRoute)); n != 0 {
		t.Errorf("%d sleep requests", n)
	}
}

func TestGetSleepSummaryRangeNoNights(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(sleepRoute, rawBody("null"))
	text := callOK(t, newSession(t, f, nil), "get_sleep_summary_range", map[string]any{"start_date": "2024-01-13", "end_date": "2024-01-14"})
	if text != "No sleep data found between 2024-01-13 and 2024-01-14." {
		t.Error(text)
	}
}

const stressRoute = "GET /wellness-service/wellness/dailyStress/2024-01-15"

func TestGetStressDataTool(t *testing.T) {
	passthrough(t, "get_stress_data", map[string]any{"date": "2024-01-15"}, stressRoute, "MOCK_STRESS_DATA", nil)
}

func TestGetStressSummaryTool(t *testing.T) {
	_, text := callWith(t, stressRoute, fixture(t, "MOCK_STRESS_DATA"), "get_stress_summary", map[string]any{"date": "2024-01-15"})
	want := map[string]any{"date": "2024-01-15", "max_stress_level": 60.0, "avg_stress_level": 25.0, "rest_percent": 66.7,
		"low_stress_percent": 33.3, "medium_stress_percent": 0.0, "high_stress_percent": 0.0, "data_points_count": 3.0}
	if got := decode(t, text); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestGetRespirationDataTool(t *testing.T) {
	passthrough(t, "get_respiration_data", map[string]any{"date": "2024-01-15"},
		"GET /wellness-service/wellness/daily/respiration/2024-01-15", "MOCK_RESPIRATION_DATA", nil)
}

func TestGetRespirationSummaryTool(t *testing.T) {
	data := map[string]any{"calendarDate": "2024-01-15", "lowestRespirationValue": 11, "highestRespirationValue": 22,
		"avgWakingRespirationValue": 14.5, "avgSleepRespirationValue": nil}
	_, text := callWith(t, "GET /wellness-service/wellness/daily/respiration/2024-01-15", data, "get_respiration_summary",
		map[string]any{"date": "2024-01-15"})
	want := map[string]any{"date": "2024-01-15", "lowest_breaths_per_min": 11.0, "highest_breaths_per_min": 22.0, "avg_waking_breaths_per_min": 14.5}
	if got := decode(t, text); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestGetSpo2DataTool(t *testing.T) {
	data := map[string]any{"calendarDate": "2024-01-15", "averageSpO2": 96, "lowestSpO2": 93, "latestSpO2": 95,
		"latestSpO2TimestampLocal": "2024-01-15T07:00:00", "lastSevenDaysAvgSpO2": 95.5, "spO2HourlyAverages": []any{[]any{1, 96}}}
	_, text := callWith(t, "GET /wellness-service/wellness/daily/spo2/2024-01-15", data, "get_spo2_data", map[string]any{"date": "2024-01-15"})
	want := map[string]any{"date": "2024-01-15", "avg_spo2_percent": 96.0, "lowest_spo2_percent": 93.0, "latest_spo2_percent": 95.0,
		"latest_reading_time": "2024-01-15T07:00:00", "last_7_days_avg_spo2": 95.5, "hourly_averages": []any{[]any{1.0, 96.0}}}
	if got := decode(t, text); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestGetAllDayStressTool(t *testing.T) {
	passthrough(t, "get_all_day_stress", map[string]any{"date": "2024-01-15"}, stressRoute, "MOCK_STRESS_DATA", nil)
}

func TestGetAllDayEventsTool(t *testing.T) {
	events := map[string]any{"events": []any{map[string]any{"type": "STRESS", "timestamp": 1705276800000.0}}}
	f, text := callWith(t, "GET /wellness-service/wellness/dailyEvents", events, "get_all_day_events", map[string]any{"date": "2024-01-15"})
	if got := decode(t, text); !reflect.DeepEqual(got, events) {
		t.Errorf("got %v", got)
	}
	if q := query(t, f, "GET /wellness-service/wellness/dailyEvents"); q.Get("calendarDate") != "2024-01-15" {
		t.Error(q)
	}
}

func TestGetLifestyleLoggingDataTool(t *testing.T) {
	passthrough(t, "get_lifestyle_logging_data", map[string]any{"date": "2024-01-15"},
		"GET /lifestylelogging-service/dailyLog/2024-01-15", "MOCK_LIFESTYLE_LOGGING_DATA", nil)
}

const weeklyStepsRoute = "GET /usersummary-service/stats/steps/weekly/2024-01-10/4"

func TestGetWeeklyStepsTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /usersummary-service/stats/steps/weekly/2024-01-10/4", fixture(t, "MOCK_WEEKLY_STEPS"))
	text := callOK(t, newSession(t, f, nil), "get_weekly_steps", map[string]any{"end_date": "2024-01-10", "weeks": 4})
	want := map[string]any{"end_date": "2024-01-10", "weeks_requested": 4.0, "weeks_returned": 2.0, "weekly_data": []any{
		map[string]any{"week_start": "2024-01-08", "total_steps": 70000.0, "average_steps": 10000.0, "total_distance_meters": 50000.0,
			"average_distance_meters": 7142.0, "days_with_data": 7.0},
		map[string]any{"week_start": "2024-01-01", "total_steps": 65000.0, "average_steps": 9285.0, "total_distance_meters": 46000.0,
			"average_distance_meters": 6571.0, "days_with_data": 7.0},
	}}
	if got := decode(t, text); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestGetWeeklyStepsToolDefaultWeeks(t *testing.T) {
	f, _ := callWith(t, weeklyStepsRoute, fixture(t, "MOCK_WEEKLY_STEPS"), "get_weekly_steps", map[string]any{"end_date": "2024-01-10"})
	if n := len(f.requests(weeklyStepsRoute)); n != 1 {
		t.Errorf("%d requests to %s", n, weeklyStepsRoute)
	}
}

func TestGetWeeklyStepsCapsWeeksAt52(t *testing.T) {
	route := "GET /usersummary-service/stats/steps/weekly/2024-01-10/52"
	f, text := callWith(t, route, fixture(t, "MOCK_WEEKLY_STEPS"), "get_weekly_steps", map[string]any{"end_date": "2024-01-10", "weeks": 200})
	if n := len(f.requests(route)); n != 1 || at(decode(t, text), "weeks_requested") != 52.0 {
		t.Errorf("%d requests: %s", n, text)
	}
	_, isErr := callTool(t, newSession(t, f, nil), "get_weekly_steps", map[string]any{"end_date": "2024-01-10", "weeks": 0})
	if !isErr {
		t.Error("weeks=0 must fail GC's positive-integer validation")
	}
}

func TestGetWeeklyStressTool(t *testing.T) {
	route := "GET /usersummary-service/stats/stress/weekly/2024-01-10/4"
	f, text := callWith(t, route, fixture(t, "MOCK_WEEKLY_STRESS"), "get_weekly_stress", map[string]any{"end_date": "2024-01-10", "weeks": 4})
	want := map[string]any{"end_date": "2024-01-10", "weeks_requested": 4.0, "weeks_returned": 2.0, "weekly_data": []any{
		map[string]any{"week_start": "2024-01-08", "stress_value": 35.0},
		map[string]any{"week_start": "2024-01-01", "stress_value": 38.0},
	}}
	if got := decode(t, text); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	if n := len(f.requests(route)); n != 1 {
		t.Errorf("%d requests", n)
	}
}

const intensityPrefix = "GET /usersummary-service/stats/im/weekly/"

func TestGetWeeklyIntensityMinutesTool(t *testing.T) {
	// weeks=2 -> start is 13 days before 2024-01-10
	f, text := callWith(t, intensityPrefix+"2023-12-28/2024-01-10", fixture(t, "MOCK_WEEKLY_INTENSITY_MINUTES"),
		"get_weekly_intensity_minutes", map[string]any{"end_date": "2024-01-10", "weeks": 2})
	if n := len(f.requests(intensityPrefix + "2023-12-28/2024-01-10")); n != 1 {
		t.Errorf("%d requests", n)
	}
	if at(decode(t, text), "weeks_returned") != 2.0 {
		t.Error(text)
	}
}

func TestGetWeeklyIntensityMinutesCountsVigorousDouble(t *testing.T) {
	_, text := callWith(t, intensityPrefix+"2024-01-01/2024-01-14", fixture(t, "MOCK_WEEKLY_INTENSITY_MINUTES"),
		"get_weekly_intensity_minutes", map[string]any{"end_date": "2024-01-14", "weeks": 2})
	data := asMap(decode(t, text))
	weeks := asList(data["weekly_data"])
	want0 := map[string]any{"week_start": "2024-01-08", "weekly_goal": 150.0, "moderate_minutes": 120.0, "vigorous_minutes": 45.0, "total_minutes": 210.0}
	if !reflect.DeepEqual(weeks[0], want0) {
		t.Errorf("latest = %v", weeks[0])
	}
	if asMap(weeks[1])["total_minutes"] != 150.0 {
		t.Errorf("second = %v", weeks[1])
	}
}

func TestGetWeeklyIntensityMinutesToolDefaultWeeks(t *testing.T) {
	// weeks=4 -> start is 27 days before 2024-01-10
	route := intensityPrefix + "2023-12-14/2024-01-10"
	f, _ := callWith(t, route, fixture(t, "MOCK_WEEKLY_INTENSITY_MINUTES"), "get_weekly_intensity_minutes", map[string]any{"end_date": "2024-01-10"})
	if n := len(f.requests(route)); n != 1 {
		t.Errorf("%d requests to %s", n, route)
	}
}

func TestGetMorningTrainingReadinessTool(t *testing.T) {
	f, text := callWith(t, "GET "+readinessPath+"2024-01-15", fixture(t, "MOCK_TRAINING_READINESS"), "get_morning_training_readiness",
		map[string]any{"date": "2024-01-15"})
	data := asMap(decode(t, text))
	for k, v := range map[string]any{"date": "2024-01-15", "context": "AFTER_WAKEUP_RESET", "score": 62.0, "level": "MODERATE",
		"feedback": "WELL_RECOVERED", "sleep_score": 81.0, "recovery_time_hours": 5.5, "hrv_weekly_avg": 58.0,
		"hrv_factor_feedback": "GOOD", "acute_load": 310.0} {
		if data[k] != v {
			t.Errorf("%s = %v, want %v", k, data[k], v)
		}
	}
	if n := len(f.requests("GET " + readinessPath + "2024-01-15")); n != 1 {
		t.Errorf("%d requests", n)
	}
}

func TestGetMorningTrainingReadinessFallsBackToFirstEntry(t *testing.T) {
	list := asList(fixture(t, "MOCK_TRAINING_READINESS"))
	asMap(list[0])["inputContext"] = nil
	_, text := callWith(t, "GET "+readinessPath+"2024-01-15", list, "get_morning_training_readiness", map[string]any{"date": "2024-01-15"})
	if at(decode(t, text), "score") != 62.0 {
		t.Error(text)
	}
	_, text = callWith(t, "GET "+readinessPath+"2024-01-15", []any{}, "get_morning_training_readiness", map[string]any{"date": "2024-01-15"})
	if text != "No morning training readiness data found for 2024-01-15" {
		t.Error(text)
	}
}

func TestGetMorningTrainingReadinessRecoveryReachedZero(t *testing.T) {
	m := with(fixture(t, "MOCK_TRAINING_READINESS").([]any)[0], map[string]any{"recoveryTime": 106, "recoveryTimeChangePhrase": "REACHED_ZERO"})
	_, text := callWith(t, "GET "+readinessPath+"2024-01-15", m, "get_morning_training_readiness", map[string]any{"date": "2024-01-15"})
	if at(decode(t, text), "recovery_time_hours") != 0.0 {
		t.Error(text)
	}
}

// --- error handling and edge cases ---

func TestGetStepsDataNoData(t *testing.T) {
	_, text := callWith(t, "GET /wellness-service/wellness/dailySummaryChart/testuser", rawBody("null"), "get_steps_data",
		map[string]any{"date": "2024-01-15"})
	if text != "No steps data found for 2024-01-15" {
		t.Error(text)
	}
}

func TestGetSleepDataException(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(sleepRoute, status(500, map[string]any{"message": "API Error"}))
	text, isErr := callTool(t, newSession(t, f, nil), "get_sleep_data", map[string]any{"date": "2024-01-15"})
	if !isErr || !strings.HasPrefix(text, "Error retrieving sleep data: ") || !strings.Contains(text, "API Error") {
		t.Errorf("%v %q", isErr, text)
	}
}

func sleepSummary(t *testing.T, payload any) (string, map[string]any) {
	t.Helper()
	_, text := callWith(t, sleepRoute, payload, "get_sleep_summary", map[string]any{"date": "2024-01-15"})
	if strings.Contains(text, "Error") || strings.Contains(text, "nil") {
		t.Fatalf("summary failed: %s", text)
	}
	return text, asMap(decode(t, text))
}

func TestGetSleepSummaryHandlesNullSleepScores(t *testing.T) {
	text, _ := sleepSummary(t, map[string]any{"dailySleepDTO": map[string]any{
		"sleepTimeSeconds": 28800, "deepSleepSeconds": 7200, "lightSleepSeconds": 14400, "remSleepSeconds": 7200,
		"awakeSleepSeconds": 0, "sleepScores": nil}})
	if !strings.Contains(text, "28800") {
		t.Error(text)
	}
}

func TestGetSleepSummaryHandlesNullSleepPhases(t *testing.T) {
	_, data := sleepSummary(t, map[string]any{"dailySleepDTO": map[string]any{
		"sleepTimeSeconds": 28800, "deepSleepSeconds": nil, "lightSleepSeconds": nil, "remSleepSeconds": nil, "awakeSleepSeconds": nil,
		"sleepScores": map[string]any{"overall": map[string]any{"value": 85, "qualifierKey": "GOOD"}}}})
	if data["sleep_seconds"] != 28800.0 || data["sleep_hours"] != 8.0 || data["sleep_score"] != 85.0 {
		t.Errorf("data = %v", data)
	}
	for _, k := range []string{"deep_sleep_percent", "light_sleep_percent", "rem_sleep_percent"} {
		if _, ok := data[k]; ok {
			t.Errorf("%s must be omitted", k)
		}
	}
}

func TestGetSleepSummaryHandlesPartialSleepPhases(t *testing.T) {
	_, data := sleepSummary(t, map[string]any{"dailySleepDTO": map[string]any{
		"sleepTimeSeconds": 28800, "deepSleepSeconds": 7200, "lightSleepSeconds": nil, "remSleepSeconds": nil, "awakeSleepSeconds": 0}})
	if data["deep_sleep_percent"] != 25.0 {
		t.Errorf("data = %v", data)
	}
	for _, k := range []string{"light_sleep_percent", "rem_sleep_percent"} {
		if _, ok := data[k]; ok {
			t.Errorf("%s must be omitted", k)
		}
	}
}

func TestGetBodyBatteryHandlesNullActivityEvents(t *testing.T) {
	_, text := callWith(t, "GET /wellness-service/wellness/bodyBattery/reports/daily", []any{map[string]any{
		"date": "2024-01-15", "charged": 100, "drained": 20, "bodyBatteryActivityEvent": nil, "bodyBatteryDynamicFeedbackEvent": nil}},
		"get_body_battery", map[string]any{"start_date": "2024-01-15", "end_date": "2024-01-15"})
	want := []any{map[string]any{"date": "2024-01-15", "charged": 100.0, "drained": 20.0, "events": []any{}}}
	if got := decode(t, text); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestGetBodyBatteryAddsDynamicFeedback(t *testing.T) {
	_, text := callWith(t, "GET /wellness-service/wellness/bodyBattery/reports/daily", []any{map[string]any{
		"date": "2024-01-15", "bodyBatteryDynamicFeedbackEvent": map[string]any{"feedbackShortType": "GOOD", "bodyBatteryLevel": "HIGH"}}},
		"get_body_battery", map[string]any{"start_date": "2024-01-15", "end_date": "2024-01-15"})
	day := asMap(asList(decode(t, text))[0])
	if day["current_feedback"] != "GOOD" || day["body_battery_level"] != "HIGH" {
		t.Errorf("day = %v", day)
	}
}

func TestGetRecoveryTimeRemainingFromReadiness(t *testing.T) {
	route := "GET " + readinessPath + "2026-09-12"
	f, text := callWith(t, route, []any{map[string]any{
		"calendarDate": "2026-09-12", "timestampLocal": "2026-09-12T08:00:00", "recoveryTime": 720, "score": 64,
		"level": "MODERATE", "recoveryTimeChangePhrase": "NO_CHANGE_SLEEP"}}, "get_recovery_time_remaining", map[string]any{"date": "2026-09-12"})
	data := asMap(decode(t, text))
	if data["remaining_hours"] != 12.0 || data["recovery_score"] != 64.0 || data["state"] != "recovering" || data["source"] != "training_readiness" {
		t.Errorf("data = %v", data)
	}
	if n := len(f.requests(route)); n != 1 {
		t.Errorf("%d readiness requests", n)
	}
}

func TestGetRecoveryTimeRemainingUnavailable(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+readinessPath+"2026-09-12", []any{})
	f.on(activitiesPath, []any{})
	text := callOK(t, newSession(t, f, nil), "get_recovery_time_remaining", map[string]any{"date": "2026-09-12"})
	data := asMap(decode(t, text))
	if data["state"] != "unavailable" || data["remaining_hours"] != nil {
		t.Errorf("data = %v", data)
	}
	if _, ok := data["remaining_hours"]; !ok {
		t.Error("remaining_hours must be present as null")
	}
}

func TestRecoveryTimeSurfacesFailedRequestsInsteadOfEmptySources(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+readinessPath+today(), status(500, map[string]any{"message": "HTTP error: expired"}))
	f.on(activitiesPath, status(500, map[string]any{"message": "HTTP error: expired"}))
	text, isErr := callTool(t, newSession(t, f, nil), "get_recovery_time_remaining", nil)
	if !isErr || !strings.HasPrefix(text, "Error retrieving recovery time remaining:") || !strings.Contains(text, "expired") ||
		strings.Contains(text, "snapshots were empty") {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetRecoveryTimeRemainingRejectsInvalidDate(t *testing.T) {
	f := newFakeGarmin(t)
	text := callOK(t, newSession(t, f, nil), "get_recovery_time_remaining", map[string]any{"date": " 2026-13-45 "})
	if text != "Invalid date '2026-13-45'. Use YYYY-MM-DD." {
		t.Error(text)
	}
	if n := len(f.requests("")); n != 0 {
		t.Errorf("%d requests", n)
	}
}

func TestGetStatsPreservesZeroFloors(t *testing.T) {
	_, text := callWith(t, summaryRoute, map[string]any{"calendarDate": "2024-01-15", "totalSteps": 0, "floorsAscended": 0.0, "floorsDescended": 0},
		"get_stats", map[string]any{"date": "2024-01-15"})
	data := asMap(decode(t, text))
	if data["floors_ascended"] != 0.0 || data["floors_descended"] != 0.0 || data["total_steps"] != 0.0 {
		t.Errorf("data = %v", data)
	}
	_, text = callWith(t, summaryRoute, map[string]any{"calendarDate": "2024-01-15", "floorsAscended": 12.34}, "get_stats", map[string]any{"date": "2024-01-15"})
	if at(decode(t, text), "floors_ascended") != 12.3 {
		t.Error(text)
	}
}

func TestGetTrainingReadinessAcceptsSingleSnapshot(t *testing.T) {
	_, text := callWith(t, "GET "+readinessPath+"2024-01-15", map[string]any{
		"calendarDate": "2024-01-15", "score": 62, "level": "MODERATE", "recoveryTime": 330}, "get_training_readiness", map[string]any{"date": "2024-01-15"})
	want := []any{map[string]any{"date": "2024-01-15", "score": 62.0, "level": "MODERATE", "recovery_time_hours": 5.5}}
	if got := decode(t, text); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestGetBodyBatteryPreservesEventsWithNullDuration(t *testing.T) {
	_, text := callWith(t, "GET /wellness-service/wellness/bodyBattery/reports/daily", []any{
		map[string]any{"date": "2024-01-14", "charged": 70, "bodyBatteryActivityEvent": []any{}},
		map[string]any{"date": "2024-01-15", "charged": 80, "bodyBatteryActivityEvent": []any{
			map[string]any{"eventType": "SLEEP", "durationInMilliseconds": nil, "bodyBatteryImpact": 50},
			map[string]any{"eventType": "ACTIVITY", "durationInMilliseconds": 0},
			map[string]any{"eventType": "REST", "durationInMilliseconds": 120000},
		}}}, "get_body_battery", map[string]any{"start_date": "2024-01-14", "end_date": "2024-01-15"})
	data := asList(decode(t, text))
	if asMap(data[0])["charged"] != 70.0 || asMap(data[1])["charged"] != 80.0 {
		t.Fatalf("data = %v", data)
	}
	events := asList(asMap(data[1])["events"])
	e0 := asMap(events[0])
	if _, ok := e0["duration_minutes"]; ok || e0["type"] != "SLEEP" || e0["body_battery_impact"] != 50.0 {
		t.Errorf("event 0 = %v", e0)
	}
	if asMap(events[1])["duration_minutes"] != 0.0 || asMap(events[2])["duration_minutes"] != 2.0 {
		t.Errorf("events = %v", events)
	}
}

func TestSleepSummariesAcceptTopLevelMetricsAndUppercaseSpo2(t *testing.T) {
	payload := map[string]any{
		"dailySleepDTO": map[string]any{"sleepTimeSeconds": 25200}, "restingHeartRate": 49, "restlessMomentsCount": 0,
		"wellnessSpO2SleepSummaryDTO": map[string]any{"averageSPO2": 95, "lowestSPO2": 89},
	}
	for _, tool := range []string{"get_sleep_summary", "get_sleep_summary_range"} {
		for metric, want := range map[string]float64{"resting_heart_rate_bpm": 49, "restless_moments_count": 0, "avg_spo2_percent": 95, "lowest_spo2_percent": 89} {
			t.Run(tool+"/"+metric, func(t *testing.T) {
				args := map[string]any{"date": "2024-01-15"}
				if tool == "get_sleep_summary_range" {
					args = map[string]any{"start_date": "2024-01-15", "end_date": "2024-01-15"}
				}
				_, text := callWith(t, sleepRoute, payload, tool, args)
				summary := asMap(decode(t, text))
				if tool == "get_sleep_summary_range" {
					summary = asMap(asList(summary["nights"])[0])
				}
				if summary[metric] != want {
					t.Errorf("%s = %v, want %v (%v)", metric, summary[metric], want, summary)
				}
			})
		}
	}
}

func TestSleepSummaryRangeSurfacesRequestFailures(t *testing.T) {
	cases := []struct {
		name string
		resp any
		want string
	}{
		{"transient", status(500, map[string]any{"message": "transient API error"}), "transient API error"},
		{"unauthorized", status(401, map[string]any{"message": "expired"}), "has expired"}, // expired-session copy replaces the text
		{"rate limited", status(429, map[string]any{"message": "rate limited"}), "rate limited"},
		{"bad request", status(400, map[string]any{"message": "HTTP error: API Error 401 - expired"}), "401 - expired"},
		{"forbidden", status(403, map[string]any{"message": "HTTP error: API Error 429 - rate limited"}), "429 - rate limited"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on(sleepRoute, tc.resp)
			text, isErr := callTool(t, newSession(t, f, nil), "get_sleep_summary_range", map[string]any{"start_date": "2024-01-13", "end_date": "2024-01-15"})
			if !isErr || !strings.Contains(text, tc.want) {
				t.Errorf("%v %q, want it to contain %q", isErr, text, tc.want)
			}
			if tc.name != "unauthorized" && !strings.HasPrefix(text, "Error retrieving sleep data for 2024-01-13:") {
				t.Errorf("text = %q", text)
			}
			reqs := f.requests(sleepRoute)
			if len(reqs) != 1 || reqs[0].Query.Get("date") != "2024-01-13" {
				t.Errorf("requests = %v", reqs)
			}
		})
	}
}

func TestTrainingReadinessEmptyDictionaryRemainsNoData(t *testing.T) {
	_, text := callWith(t, "GET "+readinessPath+"2024-01-15", map[string]any{}, "get_training_readiness", map[string]any{"date": "2024-01-15"})
	if text != "No training readiness data found for 2024-01-15" {
		t.Error(text)
	}
}

func TestHealthToolsRejectMalformedDates(t *testing.T) {
	f := newFakeGarmin(t)
	text, isErr := callTool(t, newSession(t, f, nil), "get_floors", map[string]any{"date": "2024-02-30"})
	if !isErr || !strings.HasPrefix(text, "Error retrieving floors data: invalid cdate:") {
		t.Errorf("%v %q", isErr, text)
	}
	if n := len(f.requests("GET /wellness-service/wellness/floorsChartData/daily/2024-02-30")); n != 0 {
		t.Errorf("%d requests", n)
	}
}

func TestHealthToolsRegistered(t *testing.T) {
	want := strings.Fields(`get_stats get_stats_range get_energy_balance get_user_summary get_body_composition get_stats_and_body
		get_steps_data get_daily_steps get_training_readiness get_body_battery get_body_battery_events get_blood_pressure get_floors
		get_rhr_day get_heart_rates get_heart_rates_summary get_hydration_data get_sleep_data get_sleep_summary get_sleep_summary_range
		get_stress_data get_stress_summary get_respiration_data get_respiration_summary get_spo2_data get_all_day_stress
		get_all_day_events get_lifestyle_logging_data get_weekly_steps get_weekly_stress get_weekly_intensity_minutes
		get_morning_training_readiness get_recovery_time_remaining`)
	if len(want) != 33 {
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
