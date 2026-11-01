package server

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// Ported from tests/integration/test_nutrition_tools.py (read tools).

const nutritionPrefix = "/nutrition-service"

// nutritionRequests lists the recorded calls that hit the nutrition service.
func nutritionRequests(f *fakeGarmin) []recordedCall {
	var out []recordedCall
	for _, c := range f.requests("") {
		if strings.HasPrefix(c.Path, nutritionPrefix) {
			out = append(out, c)
		}
	}
	return out
}

func apiErr(status int, msg string) statusBody {
	return statusBody{status, map[string]any{"message": msg}}
}

// nutritionRead runs a tool against a single nutrition route and returns the fake, text and error flag.
func nutritionRead(t *testing.T, route string, body any, tool string, args map[string]any) (*fakeGarmin, string, bool) {
	t.Helper()
	f := newFakeGarmin(t)
	f.on(route, body)
	text, isErr := callTool(t, newSession(t, f, nil), tool, args)
	return f, text, isErr
}

func onlyNutritionCall(t *testing.T, f *fakeGarmin, wantPath string, wantQ map[string]string) {
	t.Helper()
	calls := nutritionRequests(f)
	if len(calls) != 1 || calls[0].Path != wantPath {
		t.Fatalf("nutrition calls = %+v, want one to %s", calls, wantPath)
	}
	wantQuery(t, calls[0].Query, wantQ)
}

// ── get_nutrition_daily_food_log ────────────────────────────────────────────

func TestGetNutritionDailyFoodLog(t *testing.T) {
	foodLog := map[string]any{
		"foodLogEntries": []any{map[string]any{"foodName": "Banana", "calories": 105, "carbs": 27.0, "fat": 0.4, "protein": 1.3, "mealType": "BREAKFAST"}},
		"totalCalories":  105,
	}
	f, text, isErr := nutritionRead(t, "GET /nutrition-service/food/logs/2024-01-15", foodLog, "get_nutrition_daily_food_log", map[string]any{"date": "2024-01-15"})
	if isErr || !reflect.DeepEqual(decode(t, text), jsonValue(t, foodLog)) {
		t.Errorf("%v %s", isErr, text)
	}
	onlyNutritionCall(t, f, "/nutrition-service/food/logs/2024-01-15", nil)
}

func TestGetNutritionDailyFoodLogEmpty(t *testing.T) {
	_, text, isErr := nutritionRead(t, "GET /nutrition-service/food/logs/2024-01-15", nil, "get_nutrition_daily_food_log", map[string]any{"date": "2024-01-15"})
	if isErr || text != "No food log data found for 2024-01-15." {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetNutritionDailyFoodLogError(t *testing.T) {
	_, text, isErr := nutritionRead(t, "GET /nutrition-service/food/logs/2024-01-15", apiErr(500, "API error"), "get_nutrition_daily_food_log", map[string]any{"date": "2024-01-15"})
	if !isErr || text != "Error retrieving food log data: API Error 500 - API error" {
		t.Errorf("%v %q", isErr, text)
	}
}

// ── get_nutrition_summary_between_dates ─────────────────────────────────────

func TestGetNutritionSummaryBetweenDates(t *testing.T) {
	body := map[string]any{"dailyNutritionSummaries": []any{
		map[string]any{"mealDate": "2024-01-14", "mealDetails": []any{
			map[string]any{"mealName": "BREAKFAST", "loggedFoods": []any{}},
			map[string]any{"mealName": "LUNCH", "loggedFoods": []any{}},
		}},
		map[string]any{
			"mealDate":              "2024-01-15",
			"dailyNutritionContent": map[string]any{"calories": 1499, "carbs": 107.0, "fat": 63.0, "protein": 142.0},
			"mealDetails": []any{
				map[string]any{"mealName": "BREAKFAST", "loggedFoods": []any{map[string]any{"foodMetaData": map[string]any{}}}},
				map[string]any{"mealName": "LUNCH", "loggedFoods": []any{map[string]any{"foodMetaData": map[string]any{}}, map[string]any{"foodMetaData": map[string]any{}}}},
			},
		},
	}}
	f, text, isErr := nutritionRead(t, "GET /nutrition-service/food/logs/range", body, "get_nutrition_summary_between_dates",
		map[string]any{"start_date": "2024-01-14", "end_date": "2024-01-15"})
	if isErr {
		t.Fatal(text)
	}
	onlyNutritionCall(t, f, "/nutrition-service/food/logs/range", map[string]string{"startDate": "2024-01-14", "endDate": "2024-01-15"})
	want := map[string]any{"start_date": "2024-01-14", "end_date": "2024-01-15", "days": []any{
		map[string]any{"date": "2024-01-14", "calories": nil, "carbs": nil, "protein": nil, "fat": nil, "item_count": 0.0},
		map[string]any{"date": "2024-01-15", "calories": 1499.0, "carbs": 107.0, "protein": 142.0, "fat": 63.0, "item_count": 3.0},
	}}
	if got := decode(t, text); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestGetNutritionSummaryBetweenDatesRejectsOversizedRange(t *testing.T) {
	f, text, _ := nutritionRead(t, "GET /nutrition-service/food/logs/range", map[string]any{}, "get_nutrition_summary_between_dates",
		map[string]any{"start_date": "2024-01-01", "end_date": "2024-04-01"})
	if text != "Date range too large (92 days). Maximum is 61 days." || len(nutritionRequests(f)) != 0 {
		t.Errorf("%q %v", text, nutritionRequests(f))
	}
	// 61 days is the largest accepted range.
	f, text, isErr := nutritionRead(t, "GET /nutrition-service/food/logs/range", map[string]any{"dailyNutritionSummaries": []any{}}, "get_nutrition_summary_between_dates",
		map[string]any{"start_date": "2024-01-01", "end_date": "2024-03-01"})
	if isErr || strings.Contains(text, "too large") || len(nutritionRequests(f)) != 1 {
		t.Errorf("61 days: %v %q", isErr, text)
	}
}

func TestGetNutritionSummaryBetweenDatesRejectsInvertedRange(t *testing.T) {
	f, text, _ := nutritionRead(t, "GET /nutrition-service/food/logs/range", map[string]any{}, "get_nutrition_summary_between_dates",
		map[string]any{"start_date": "2024-01-15", "end_date": "2024-01-01"})
	if text != "end_date must be on or after start_date." || len(nutritionRequests(f)) != 0 {
		t.Errorf("%q", text)
	}
}

func TestGetNutritionSummaryBetweenDatesInvalidDate(t *testing.T) {
	for args, want := range map[[2]string]string{
		{"nope", "2024-01-15"}:       "Invalid date format: Invalid isoformat string: 'nope'. Use YYYY-MM-DD.",
		{"2024-01-15", "2024-13-01"}: "Invalid date format: month must be in 1..12. Use YYYY-MM-DD.",
	} {
		f, text, _ := nutritionRead(t, "GET /nutrition-service/food/logs/range", map[string]any{}, "get_nutrition_summary_between_dates",
			map[string]any{"start_date": args[0], "end_date": args[1]})
		if text != want || len(nutritionRequests(f)) != 0 {
			t.Errorf("%v: %q", args, text)
		}
	}
}

func TestGetNutritionSummaryBetweenDatesEmpty(t *testing.T) {
	_, text, isErr := nutritionRead(t, "GET /nutrition-service/food/logs/range", map[string]any{"dailyNutritionSummaries": []any{}}, "get_nutrition_summary_between_dates",
		map[string]any{"start_date": "2024-01-14", "end_date": "2024-01-15"})
	if isErr || text != "No nutrition data found between 2024-01-14 and 2024-01-15." {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetNutritionSummaryBetweenDatesError(t *testing.T) {
	_, text, isErr := nutritionRead(t, "GET /nutrition-service/food/logs/range", apiErr(500, "API error"), "get_nutrition_summary_between_dates",
		map[string]any{"start_date": "2024-01-14", "end_date": "2024-01-15"})
	if !isErr || !strings.HasPrefix(text, "Error retrieving nutrition summary: ") {
		t.Errorf("%v %q", isErr, text)
	}
}

// ── get_nutrition_daily_meals ───────────────────────────────────────────────

func TestGetNutritionDailyMeals(t *testing.T) {
	meals := map[string]any{"meals": []any{map[string]any{"mealId": 185360, "mealType": "BREAKFAST", "totalCalories": 450}}}
	f, text, isErr := nutritionRead(t, "GET /nutrition-service/meals/2024-01-15", meals, "get_nutrition_daily_meals", map[string]any{"date": "2024-01-15"})
	if isErr || !reflect.DeepEqual(decode(t, text), jsonValue(t, meals)) {
		t.Errorf("%v %s", isErr, text)
	}
	onlyNutritionCall(t, f, "/nutrition-service/meals/2024-01-15", nil)
}

func TestGetNutritionDailyMealsEmpty(t *testing.T) {
	_, text, isErr := nutritionRead(t, "GET /nutrition-service/meals/2024-01-15", nil, "get_nutrition_daily_meals", map[string]any{"date": "2024-01-15"})
	if isErr || text != "No meal data found for 2024-01-15." {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetNutritionDailyMealsError(t *testing.T) {
	_, text, isErr := nutritionRead(t, "GET /nutrition-service/meals/2024-01-15", apiErr(500, "API error"), "get_nutrition_daily_meals", map[string]any{"date": "2024-01-15"})
	if !isErr || !strings.HasPrefix(text, "Error retrieving meal data: ") {
		t.Errorf("%v %q", isErr, text)
	}
}

// ── get_nutrition_daily_settings ────────────────────────────────────────────

func TestGetNutritionDailySettings(t *testing.T) {
	settings := map[string]any{"calorieGoal": 2000, "carbsGoalPercent": 50, "fatGoalPercent": 30, "proteinGoalPercent": 20}
	f, text, isErr := nutritionRead(t, "GET /nutrition-service/settings/2024-01-15", settings, "get_nutrition_daily_settings", map[string]any{"date": "2024-01-15"})
	if isErr || !reflect.DeepEqual(decode(t, text), jsonValue(t, settings)) {
		t.Errorf("%v %s", isErr, text)
	}
	onlyNutritionCall(t, f, "/nutrition-service/settings/2024-01-15", nil)
}

func TestGetNutritionDailySettingsEmpty(t *testing.T) {
	_, text, isErr := nutritionRead(t, "GET /nutrition-service/settings/2024-01-15", nil, "get_nutrition_daily_settings", map[string]any{"date": "2024-01-15"})
	if isErr || text != "No nutrition settings found for 2024-01-15." {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetNutritionDailySettingsError(t *testing.T) {
	_, text, isErr := nutritionRead(t, "GET /nutrition-service/settings/2024-01-15", apiErr(500, "API error"), "get_nutrition_daily_settings", map[string]any{"date": "2024-01-15"})
	if !isErr || !strings.HasPrefix(text, "Error retrieving nutrition settings: ") {
		t.Errorf("%v %q", isErr, text)
	}
}

// ── get_custom_foods ────────────────────────────────────────────────────────

const customFoodRoute = "GET /nutrition-service/customFood"

func TestGetCustomFoods(t *testing.T) {
	foods := []any{map[string]any{"foodId": "abc123", "foodName": "Homemade Cookies", "servingId": "srv456"}}
	f, text, isErr := nutritionRead(t, customFoodRoute, foods, "get_custom_foods", map[string]any{})
	if isErr || !reflect.DeepEqual(decode(t, text), jsonValue(t, foods)) {
		t.Errorf("%v %s", isErr, text)
	}
	onlyNutritionCall(t, f, "/nutrition-service/customFood", map[string]string{"searchExpression": "", "start": "0", "limit": "20", "includeContent": "true"})
}

func TestGetCustomFoodsWithSearch(t *testing.T) {
	f, text, isErr := nutritionRead(t, customFoodRoute, []any{}, "get_custom_foods", map[string]any{"search": "cookie & cream/é", "start": 0, "limit": 10})
	if isErr || text == "" {
		t.Errorf("%v %q", isErr, text)
	}
	onlyNutritionCall(t, f, "/nutrition-service/customFood", map[string]string{"searchExpression": "cookie & cream/é", "start": "0", "limit": "10", "includeContent": "true"})
}

func TestGetCustomFoodsEmpty(t *testing.T) {
	_, text, isErr := nutritionRead(t, customFoodRoute, nil, "get_custom_foods", map[string]any{})
	if isErr || text != "No custom foods found." {
		t.Errorf("%v %q", isErr, text)
	}
}

// The Python mocks raise an exception whose message is the bare text; the Go client formats Garmin's body as
// "API Error <status> - <message>", which is the text the Python client itself produces.
func TestGetCustomFoodsForbiddenIsAnError(t *testing.T) {
	_, text, isErr := nutritionRead(t, customFoodRoute, apiErr(403, "localized or non-English message"), "get_custom_foods", map[string]any{})
	if !isErr || text != "Error retrieving custom foods: API Error 403 - localized or non-English message" {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetCustomFoodsUnsupportedForbiddenIsUnavailable(t *testing.T) {
	_, text, isErr := nutritionRead(t, customFoodRoute, apiErr(403, "Custom foods not supported for this account"), "get_custom_foods", map[string]any{})
	if isErr || text != "No custom foods found; Garmin custom food API returned HTTP 403 Forbidden." {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetCustomFoodsUnsupportedPatternOnlyOn403(t *testing.T) {
	_, text, isErr := nutritionRead(t, customFoodRoute, apiErr(500, "custom food unsupported"), "get_custom_foods", map[string]any{})
	if !isErr || !strings.HasPrefix(text, "Error retrieving custom foods: ") {
		t.Errorf("%v %q", isErr, text)
	}
}

// ── get_custom_food_serving_units ───────────────────────────────────────────

const unitsRoute = "GET /nutrition-service/metadata/customFoodServingUnits"

func TestGetCustomFoodServingUnits(t *testing.T) {
	units := []any{map[string]any{"unitKey": "G", "unitName": "Grams"}, map[string]any{"unitKey": "ML", "unitName": "Milliliters"}}
	f, text, isErr := nutritionRead(t, unitsRoute, units, "get_custom_food_serving_units", map[string]any{})
	if isErr || !reflect.DeepEqual(decode(t, text), jsonValue(t, units)) {
		t.Errorf("%v %s", isErr, text)
	}
	onlyNutritionCall(t, f, "/nutrition-service/metadata/customFoodServingUnits", nil)
}

func TestGetCustomFoodServingUnitsEmpty(t *testing.T) {
	_, text, isErr := nutritionRead(t, unitsRoute, nil, "get_custom_food_serving_units", map[string]any{})
	if isErr || text != "No serving units found." {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetCustomFoodServingUnitsError(t *testing.T) {
	_, text, isErr := nutritionRead(t, unitsRoute, apiErr(500, "API error"), "get_custom_food_serving_units", map[string]any{})
	if !isErr || !strings.HasPrefix(text, "Error retrieving serving units: ") {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetCustomFoodServingUnitsForbiddenIsAnError(t *testing.T) {
	_, text, isErr := nutritionRead(t, unitsRoute, apiErr(403, "localized or non-English message"), "get_custom_food_serving_units", map[string]any{})
	if !isErr || text != "Error retrieving serving units: API Error 403 - localized or non-English message" {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetCustomFoodServingUnitsUnsupportedForbiddenIsUnavailable(t *testing.T) {
	_, text, isErr := nutritionRead(t, unitsRoute, apiErr(403, "custom food unsupported in this region"), "get_custom_food_serving_units", map[string]any{})
	if isErr || text != "No serving units found; Garmin custom food API returned HTTP 403 Forbidden." {
		t.Errorf("%v %q", isErr, text)
	}
}

// ── search_foods ────────────────────────────────────────────────────────────

const searchRoute = "GET /nutrition-service/food/search"

func searchResponse() map[string]any {
	return map[string]any{
		"results": []any{map[string]any{
			"foodMetaData": map[string]any{
				"foodId": "4132350", "foodName": "Cheerios", "foodType": "BRANDED", "source": "FATSECRET",
				"brandName": "General Mills", "regionCode": "US", "languageCode": "en",
			},
			"nutritionContents": []any{map[string]any{
				"servingId": "srv001", "servingUnit": "cup", "numberOfUnits": 1.0, "calories": 100.0, "carbs": 20.0, "protein": 3.0, "fat": 2.0,
			}},
		}},
		"moreDataAvailable": false,
	}
}

func TestSearchFoodsReturnsResults(t *testing.T) {
	f, text, isErr := nutritionRead(t, searchRoute, searchResponse(), "search_foods", map[string]any{"query": "Cheerios & Oats/é"})
	if isErr {
		t.Fatal(text)
	}
	want := map[string]any{"count": 1.0, "has_more": false, "results": []any{map[string]any{
		"food_id": "4132350", "name": "Cheerios", "food_type": "BRANDED", "source": "FATSECRET", "region": "US", "language": "en", "brand": "General Mills",
		"servings": []any{map[string]any{"serving_id": "srv001", "serving_unit": "cup", "number_of_units": 1.0, "calories": 100.0, "carbs_g": 20.0, "protein_g": 3.0, "fat_g": 2.0}},
	}}}
	if got := decode(t, text); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	onlyNutritionCall(t, f, "/nutrition-service/food/search", map[string]string{"searchExpression": "Cheerios & Oats/é", "start": "0", "limit": "20"})
}

func TestSearchFoodsPaginationAndMore(t *testing.T) {
	resp := searchResponse()
	resp["moreDataAvailable"] = true
	f, text, _ := nutritionRead(t, searchRoute, resp, "search_foods", map[string]any{"query": "x", "start": 20, "limit": 5})
	if at(decode(t, text), "has_more") != true {
		t.Errorf("text = %s", text)
	}
	onlyNutritionCall(t, f, "/nutrition-service/food/search", map[string]string{"searchExpression": "x", "start": "20", "limit": "5"})
}

func TestSearchFoodsEmptyResponse(t *testing.T) {
	_, text, isErr := nutritionRead(t, searchRoute, nil, "search_foods", map[string]any{"query": "xyzunknownfood"})
	if isErr || text != "No foods found." {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestSearchFoodsError(t *testing.T) {
	_, text, isErr := nutritionRead(t, searchRoute, apiErr(503, "503 Service Unavailable"), "search_foods", map[string]any{"query": "Cheerios"})
	if !isErr || !strings.HasPrefix(text, "Error searching foods: ") {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestSearchFoodsRetainsGoodRowsWithNullOptionalData(t *testing.T) {
	for name, bad := range map[string]any{
		"none":               nil,
		"null metadata":      map[string]any{"foodMetaData": nil},
		"incomplete+no nutr": map[string]any{"foodMetaData": map[string]any{"foodId": "incomplete"}, "nutritionContents": nil},
	} {
		t.Run(name, func(t *testing.T) {
			resp := searchResponse()
			resp["results"] = append([]any{bad}, resp["results"].([]any)...)
			_, text, isErr := nutritionRead(t, searchRoute, resp, "search_foods", map[string]any{"query": "Cheerios"})
			if isErr {
				t.Fatal(text)
			}
			var good map[string]any
			for _, r := range asList(at(decode(t, text), "results")) {
				if asMap(r)["name"] == "Cheerios" {
					good = asMap(r)
				}
			}
			if at(asList(good["servings"])[0], "calories") != 100.0 {
				t.Errorf("good row = %v", good)
			}
		})
	}
}

func TestSearchFoodsHandlesNullResults(t *testing.T) {
	_, text, _ := nutritionRead(t, searchRoute, map[string]any{"results": nil}, "search_foods", map[string]any{"query": "Cheerios"})
	if got := at(decode(t, text), "results"); !reflect.DeepEqual(got, []any{}) {
		t.Errorf("results = %#v", got)
	}
}

// Free-text day values travel in the path: the raw request URI must stay under its endpoint.
func TestNutritionToolsKeepFreeTextDatesInTheirPath(t *testing.T) {
	const evil = "x/../../foo?a=b#frag"
	for prefix, tool := range map[string]string{
		"/nutrition-service/food/logs/": "get_nutrition_daily_food_log",
		"/nutrition-service/meals/":     "get_nutrition_daily_meals",
		"/nutrition-service/settings/":  "get_nutrition_daily_settings",
	} {
		f := newFakeGarmin(t)
		var uri string
		f.on("GET /nutrition-service/anything", nil)
		f.Server.Config.Handler = handlerRecording(f.Server.Config.Handler, prefix, &uri)
		callTool(t, newSession(t, f, nil), tool, map[string]any{"date": evil})
		if !strings.HasPrefix(uri, prefix+"x%2F..%2F..%2Ffoo%3Fa=b%23frag") {
			t.Errorf("%s: uri = %q", tool, uri)
		}
	}
}

// handlerRecording stores the raw request URI of the last request under prefix.
func handlerRecording(next http.Handler, prefix string, uri *string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.RequestURI, prefix) {
			*uri = r.RequestURI
		}
		next.ServeHTTP(w, r)
	})
}
