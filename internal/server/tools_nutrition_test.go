package server

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	nutritionFoodLogsPathTest     = "/nutrition-service/food/logs"
	nutritionSummaryPathTest      = "/nutrition-service/food/logs/range"
	nutritionMealsPathTest        = "/nutrition-service/meals"
	nutritionSettingsPathTest     = "/nutrition-service/settings"
	nutritionFoodSearchPathTest   = "/nutrition-service/food/search"
	nutritionCustomFoodPathTest   = "/nutrition-service/customFood"
	nutritionServingUnitsPathTest = "/nutrition-service/metadata/customFoodServingUnits"
	nutritionMealDate             = "2024-01-15"
	nutritionFoodLogPath          = nutritionFoodLogsPathTest + "/" + nutritionMealDate
	nutritionMealsPath            = nutritionMealsPathTest + "/" + nutritionMealDate
	nutritionSettingsPath         = nutritionSettingsPathTest + "/" + nutritionMealDate
	nutritionSettingsSepPath      = nutritionSettingsPathTest + "/2024-09-01"
)

func nutritionFoodLog() map[string]any {
	return map[string]any{
		"foodLogEntries": []any{
			map[string]any{
				"foodName": "Banana",
				"calories": 105,
				"carbs":    27.0,
				"fat":      0.4,
				"protein":  1.3,
				"mealType": "BREAKFAST",
			},
		},
		"totalCalories": 105,
	}
}

func nutritionMeals() map[string]any {
	return map[string]any{
		"meals": []any{
			map[string]any{"mealId": 20249, "mealName": "BREAKFAST", "startTime": "06:00:00", "endTime": "09:00:00"},
			map[string]any{"mealId": 20250, "mealName": "LUNCH", "startTime": "11:00:00", "endTime": "14:00:00"},
			map[string]any{"mealId": 20251, "mealName": "DINNER", "startTime": "18:00:00", "endTime": "21:00:00"},
			map[string]any{"mealId": 20252, "mealName": "SNACKS"},
		},
	}
}

func nutritionCustomFoods() map[string]any {
	return map[string]any{
		"customFoods": []any{
			map[string]any{
				"foodMetaData":      map[string]any{"foodId": "food001", "foodName": "Greek Yogurt"},
				"nutritionContents": []any{map[string]any{"servingId": "srv001", "calories": "100"}},
			},
		},
	}
}

func nutritionStoredFood() map[string]any {
	return map[string]any{
		"foodMetaData": map[string]any{"foodId": "abc123", "foodName": "Simple Food"},
		"nutritionContents": []any{
			map[string]any{"servingId": "srv456", "servingUnit": "G", "numberOfUnits": 100, "calories": 100},
		},
	}
}

func nutritionSettings() map[string]any {
	return map[string]any{
		"weightChangeType":          "LOSS",
		"targetDate":                "2024-06-01",
		"weightChangeRate":          0.25,
		"activeDailyCalories":       400,
		"userDefinedActiveCalories": false,
		"calorieGoal":               2000,
		"macroGoals":                map[string]any{"carbs": 250, "fat": 65, "protein": 120},
		"autoCalorieAdjustment":     false,
		"startingWeight":            80,
		"targetWeightGoal":          75,
		"nutritionStatus":           "ACTIVE",
	}
}

func nutritionSearchResponse() map[string]any {
	return map[string]any{
		"results": []any{
			map[string]any{
				"foodMetaData": map[string]any{
					"foodId":       "4132350",
					"foodName":     "Cheerios",
					"foodType":     "BRANDED",
					"source":       "FATSECRET",
					"brandName":    "General Mills",
					"regionCode":   "US",
					"languageCode": "en",
				},
				"nutritionContents": []any{
					map[string]any{
						"servingId":     "srv001",
						"servingUnit":   "cup",
						"numberOfUnits": 1.0,
						"calories":      100.0,
						"carbs":         20.0,
						"protein":       3.0,
						"fat":           2.0,
					},
				},
			},
		},
		"moreDataAvailable": false,
	}
}

func deepCopyValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = deepCopyValue(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = deepCopyValue(val)
		}
		return out
	}
	return v
}

func nutritionStringPtr(v string) *string { return &v }

func nutritionLastPUT(t *testing.T, f *fakeGarmin, path string) map[string]any {
	t.Helper()
	calls := f.callsMatching("PUT", path)
	if len(calls) == 0 {
		t.Fatalf("no PUT %s", path)
	}
	return decodeBody(t, calls[len(calls)-1].Body)
}

// --- get_nutrition_daily_food_log ---

func TestGetNutritionDailyFoodLog(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionFoodLogPath, nutritionFoodLog())
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_nutrition_daily_food_log", map[string]any{"date": nutritionMealDate})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if calls := f.callsMatching("GET", nutritionFoodLogPath); len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
}

func TestGetNutritionDailyFoodLogEmpty(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionFoodLogPath, nil)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_nutrition_daily_food_log", map[string]any{"date": nutritionMealDate})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "No food log data found") {
		t.Fatalf("text = %q", text)
	}
}

func TestGetNutritionDailyFoodLogError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionFoodLogPath, status(500, map[string]any{"message": "API error"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_nutrition_daily_food_log", map[string]any{"date": nutritionMealDate})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	if !strings.Contains(text, "Error retrieving food log data") {
		t.Fatalf("text = %q", text)
	}
}

// --- get_nutrition_summary_between_dates ---

func TestGetNutritionSummaryBetweenDates(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionSummaryPathTest, map[string]any{
		"dailyNutritionSummaries": []any{
			map[string]any{
				"mealDate": "2024-01-14",
				"mealDetails": []any{
					map[string]any{"mealName": "BREAKFAST", "loggedFoods": []any{}},
					map[string]any{"mealName": "LUNCH", "loggedFoods": []any{}},
				},
			},
			map[string]any{
				"mealDate": "2024-01-15",
				"dailyNutritionContent": map[string]any{
					"calories": 1499,
					"carbs":    107.0,
					"fat":      63.0,
					"protein":  142.0,
				},
				"mealDetails": []any{
					map[string]any{"mealName": "BREAKFAST", "loggedFoods": []any{map[string]any{"foodMetaData": map[string]any{}}}},
					map[string]any{"mealName": "LUNCH", "loggedFoods": []any{
						map[string]any{"foodMetaData": map[string]any{}},
						map[string]any{"foodMetaData": map[string]any{}},
					}},
				},
			},
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_nutrition_summary_between_dates", map[string]any{
		"start_date": "2024-01-14",
		"end_date":   "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	calls := f.callsMatching("GET", nutritionSummaryPathTest)
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	if calls[0].Query.Get("startDate") != "2024-01-14" || calls[0].Query.Get("endDate") != "2024-01-15" {
		t.Fatalf("query = %v", calls[0].Query)
	}

	var got map[string]any
	decodeJSON(t, text, &got)
	if got["start_date"] != "2024-01-14" || got["end_date"] != "2024-01-15" {
		t.Fatalf("result = %v", got)
	}
	days := mutList(t, got["days"])
	unlogged := mutMap(t, days[0])
	logged := mutMap(t, days[1])
	if unlogged["date"] != "2024-01-14" || unlogged["item_count"] != float64(0) || unlogged["calories"] != nil {
		t.Fatalf("unlogged = %v", unlogged)
	}
	if logged["date"] != "2024-01-15" || logged["item_count"] != float64(3) {
		t.Fatalf("logged = %v", logged)
	}
	if logged["calories"] != float64(1499) || logged["carbs"] != 107.0 || logged["protein"] != 142.0 || logged["fat"] != 63.0 {
		t.Fatalf("logged = %v", logged)
	}
}

func TestGetNutritionSummaryBetweenDatesRejectsOversizedRange(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_nutrition_summary_between_dates", map[string]any{
		"start_date": "2024-01-01",
		"end_date":   "2024-04-01",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "too large") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.recorded(); len(calls) != 0 {
		t.Fatalf("calls = %d, want 0", len(calls))
	}
}

func TestGetNutritionSummaryBetweenDatesRejectsInvertedRange(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_nutrition_summary_between_dates", map[string]any{
		"start_date": "2024-01-15",
		"end_date":   "2024-01-01",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "end_date must be on or after start_date") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.recorded(); len(calls) != 0 {
		t.Fatalf("calls = %d, want 0", len(calls))
	}
}

func TestGetNutritionSummaryBetweenDatesEmpty(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionSummaryPathTest, map[string]any{"dailyNutritionSummaries": []any{}})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_nutrition_summary_between_dates", map[string]any{
		"start_date": "2024-01-14",
		"end_date":   "2024-01-15",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "No nutrition data found") {
		t.Fatalf("text = %q", text)
	}
}

func TestGetNutritionSummaryBetweenDatesError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionSummaryPathTest, status(500, map[string]any{"message": "API error"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_nutrition_summary_between_dates", map[string]any{
		"start_date": "2024-01-14",
		"end_date":   "2024-01-15",
	})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	if !strings.Contains(text, "Error retrieving nutrition summary") {
		t.Fatalf("text = %q", text)
	}
}

// --- get_nutrition_daily_meals / settings ---

func TestGetNutritionDailyMeals(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "get_nutrition_daily_meals", map[string]any{"date": nutritionMealDate})
	if isErr {
		t.Fatal("unexpected error")
	}
	if calls := f.callsMatching("GET", nutritionMealsPath); len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
}

func TestGetNutritionDailyMealsEmpty(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionMealsPath, nil)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_nutrition_daily_meals", map[string]any{"date": nutritionMealDate})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "No meal data found") {
		t.Fatalf("text = %q", text)
	}
}

func TestGetNutritionDailyMealsError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionMealsPath, status(500, map[string]any{"message": "API error"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_nutrition_daily_meals", map[string]any{"date": nutritionMealDate})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	if !strings.Contains(text, "Error retrieving meal data") {
		t.Fatalf("text = %q", text)
	}
}

func TestGetNutritionDailySettings(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionSettingsPath, nutritionSettings())
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "get_nutrition_daily_settings", map[string]any{"date": nutritionMealDate})
	if isErr {
		t.Fatal("unexpected error")
	}
	if calls := f.callsMatching("GET", nutritionSettingsPath); len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
}

func TestGetNutritionDailySettingsEmpty(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionSettingsPath, nil)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_nutrition_daily_settings", map[string]any{"date": nutritionMealDate})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "No nutrition settings found") {
		t.Fatalf("text = %q", text)
	}
}

func TestGetNutritionDailySettingsError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionSettingsPath, status(500, map[string]any{"message": "API error"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_nutrition_daily_settings", map[string]any{"date": nutritionMealDate})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	if !strings.Contains(text, "Error retrieving nutrition settings") {
		t.Fatalf("text = %q", text)
	}
}

// --- custom food readers ---

func TestGetCustomFoods(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, nutritionCustomFoods())
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "get_custom_foods", nil)
	if isErr {
		t.Fatal("unexpected error")
	}
	calls := f.callsMatching("GET", nutritionCustomFoodPathTest)
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	q := calls[0].Query
	want := map[string]string{
		"searchExpression": "",
		"start":            "0",
		"limit":            "20",
		"includeContent":   "true",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Fatalf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
}

func TestGetCustomFoodsWithSearch(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, []any{})
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "get_custom_foods", map[string]any{
		"search": "cookie & cream/é",
		"start":  0,
		"limit":  10,
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	calls := f.callsMatching("GET", nutritionCustomFoodPathTest)
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	q := calls[0].Query
	want := map[string]string{
		"searchExpression": "cookie & cream/é",
		"start":            "0",
		"limit":            "10",
		"includeContent":   "true",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Fatalf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
}

func TestGetCustomFoodsEmpty(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, nil)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_custom_foods", nil)
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "No custom foods found") {
		t.Fatalf("text = %q", text)
	}
}

func TestGetCustomFoodsForbiddenIsAnError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, status(403, map[string]any{"message": "localized or non-English message"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_custom_foods", nil)
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	if !strings.Contains(text, "Error retrieving custom foods:") || !strings.Contains(text, "localized or non-English message") {
		t.Fatalf("text = %q", text)
	}
}

func TestGetCustomFoodsUnsupportedForbiddenIsUnavailable(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, status(403, map[string]any{"message": "Custom foods not supported for this account"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_custom_foods", nil)
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	want := "No custom foods found; Garmin custom food API returned HTTP 403 Forbidden."
	if text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
}

func TestGetCustomFoodServingUnits(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionServingUnitsPathTest, []any{
		map[string]any{"unitKey": "G", "unitName": "Grams"},
		map[string]any{"unitKey": "ML", "unitName": "Milliliters"},
	})
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "get_custom_food_serving_units", nil)
	if isErr {
		t.Fatal("unexpected error")
	}
	if calls := f.callsMatching("GET", nutritionServingUnitsPathTest); len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
}

func TestGetCustomFoodServingUnitsError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionServingUnitsPathTest, status(500, map[string]any{"message": "API error"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_custom_food_serving_units", nil)
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	if !strings.Contains(text, "Error retrieving serving units") {
		t.Fatalf("text = %q", text)
	}
}

func TestGetCustomFoodServingUnitsForbiddenIsAnError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionServingUnitsPathTest, status(403, map[string]any{"message": "localized or non-English message"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_custom_food_serving_units", nil)
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	if !strings.Contains(text, "Error retrieving serving units:") || !strings.Contains(text, "localized or non-English message") {
		t.Fatalf("text = %q", text)
	}
}

func TestGetCustomFoodServingUnitsUnsupportedForbiddenIsUnavailable(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionServingUnitsPathTest, status(403, map[string]any{"message": "custom food unsupported in this region"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_custom_food_serving_units", nil)
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	want := "No serving units found; Garmin custom food API returned HTTP 403 Forbidden."
	if text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
}

// --- create_custom_food ---

func TestCreateCustomFood(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("PUT "+nutritionCustomFoodPathTest, map[string]any{"foodId": "abc123", "foodName": "Homemade Cookies", "servingId": "srv456"})
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "create_custom_food", map[string]any{
		"food_name":       "Homemade Cookies",
		"calories":        150,
		"serving_unit":    "G",
		"number_of_units": 30,
		"carbs":           20,
		"protein":         2,
		"fat":             7,
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	payload := nutritionLastPUT(t, f, nutritionCustomFoodPathTest)
	if mutMap(t, payload["foodMetaData"])["foodName"] != "Homemade Cookies" {
		t.Fatalf("payload = %v", payload)
	}
	nc := mutMap(t, mutList(t, payload["nutritionContents"])[0])
	want := map[string]any{
		"calories":      "150",
		"carbs":         "20",
		"protein":       "2",
		"fat":           "7",
		"servingUnit":   "G",
		"numberOfUnits": "30",
	}
	for k, v := range want {
		if nc[k] != v {
			t.Fatalf("%s = %v, want %v", k, nc[k], v)
		}
	}
}

func TestCreateCustomFoodMinimal(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("PUT "+nutritionCustomFoodPathTest, map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "create_custom_food", map[string]any{"food_name": "Simple Food", "calories": 100})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "Custom food created") {
		t.Fatalf("text = %q", text)
	}
	nc := mutMap(t, mutList(t, nutritionLastPUT(t, f, nutritionCustomFoodPathTest)["nutritionContents"])[0])
	for _, key := range []string{"carbs", "protein", "fat"} {
		if _, ok := nc[key]; ok {
			t.Fatalf("%s present in %v", key, nc)
		}
	}
}

func TestCreateCustomFoodError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("PUT "+nutritionCustomFoodPathTest, status(500, map[string]any{"message": "API error"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "create_custom_food", map[string]any{"food_name": "Test", "calories": 100})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	if !strings.Contains(text, "Error creating custom food") {
		t.Fatalf("text = %q", text)
	}
}

func TestCreateCustomFoodForbiddenIsAnError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("PUT "+nutritionCustomFoodPathTest, status(403, map[string]any{"message": "localized or non-English message"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "create_custom_food", map[string]any{"food_name": "Test", "calories": 100})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if msg, _ := got["message"].(string); !strings.Contains(msg, "Error creating custom food:") || !strings.Contains(msg, "localized or non-English message") {
		t.Fatalf("message = %v", got["message"])
	}
	if got["write_committed"] != false || got["status"] != "error" {
		t.Fatalf("result = %v", got)
	}
}

func TestCreateCustomFoodForbiddenFallsBackToStatusInMessage(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("PUT "+nutritionCustomFoodPathTest, status(403, map[string]any{"message": "acceso denegado"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "create_custom_food", map[string]any{"food_name": "Test", "calories": 100})
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["message"] != "Error creating custom food: API Error 403 - acceso denegado" {
		t.Fatalf("message = %v", got["message"])
	}
	if got["upstream_status"] != float64(403) || got["write_committed"] != false {
		t.Fatalf("result = %v", got)
	}
}

func TestCreateCustomFoodWithBrandAndMicros(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("PUT "+nutritionCustomFoodPathTest, map[string]any{"foodMetaData": map[string]any{"foodId": "x"}})
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "create_custom_food", map[string]any{
		"food_name":  "Branded Bar",
		"calories":   200,
		"brand_name": "ACME",
		"trans_fat":  0.5,
		"calcium":    130,
		"iron":       2,
		"vitamin_d":  2.5,
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	payload := nutritionLastPUT(t, f, nutritionCustomFoodPathTest)
	if mutMap(t, payload["foodMetaData"])["brandName"] != "ACME" {
		t.Fatalf("payload = %v", payload)
	}
	nc := mutMap(t, mutList(t, payload["nutritionContents"])[0])
	want := map[string]any{"transFat": "0.5", "calcium": "130", "iron": "2", "vitaminD": "2.5"}
	for k, v := range want {
		if nc[k] != v {
			t.Fatalf("%s = %v, want %v", k, nc[k], v)
		}
	}
}

func TestCreateCustomFoodMinimalNoBrand(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("PUT "+nutritionCustomFoodPathTest, map[string]any{})
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "create_custom_food", map[string]any{"food_name": "Plain Food", "calories": 50})
	if isErr {
		t.Fatal("unexpected error")
	}
	payload := nutritionLastPUT(t, f, nutritionCustomFoodPathTest)
	if _, ok := mutMap(t, payload["foodMetaData"])["brandName"]; ok {
		t.Fatalf("brandName present: %v", payload)
	}
	nc := mutMap(t, mutList(t, payload["nutritionContents"])[0])
	for _, key := range []string{"transFat", "calcium", "iron", "vitaminD"} {
		if _, ok := nc[key]; ok {
			t.Fatalf("%s present in %v", key, nc)
		}
	}
}

// --- update_custom_food ---

func TestUpdateCustomFood(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, map[string]any{"customFoods": []any{nutritionStoredFood()}})
	f.on("PUT "+nutritionCustomFoodPathTest, map[string]any{"foodId": "abc123", "foodName": "Homemade Cookies Updated", "servingId": "srv456"})
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "update_custom_food", map[string]any{
		"food_id":         "abc123",
		"serving_id":      "srv456",
		"food_name":       "Homemade Cookies Updated",
		"calories":        160,
		"serving_unit":    "G",
		"number_of_units": 30,
		"carbs":           22,
		"protein":         3,
		"fat":             8,
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	payload := nutritionLastPUT(t, f, nutritionCustomFoodPathTest)
	if mutMap(t, payload["foodMetaData"])["foodId"] != "abc123" || mutMap(t, payload["foodMetaData"])["foodName"] != "Homemade Cookies Updated" {
		t.Fatalf("payload = %v", payload)
	}
	nc := mutMap(t, mutList(t, payload["nutritionContents"])[0])
	if nc["servingId"] != "srv456" || nc["calories"] != "160" || nc["carbs"] != "22" || nc["protein"] != "3" || nc["fat"] != "8" {
		t.Fatalf("nutrition = %v", nc)
	}
	calls := f.callsMatching("GET", nutritionCustomFoodPathTest)
	if len(calls) != 1 {
		t.Fatalf("search calls = %d, want 1", len(calls))
	}
	q := calls[0].Query
	if q.Get("searchExpression") != "Homemade Cookies Updated" || q.Get("start") != "0" || q.Get("limit") != "20" || q.Get("includeContent") != "true" {
		t.Fatalf("query = %v", q)
	}
}

func TestUpdateCustomFoodHandlesNullFoodMetadata(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, map[string]any{"customFoods": []any{
		map[string]any{"foodMetaData": nil, "nutritionContents": nil},
		nutritionStoredFood(),
	}})
	f.on("PUT "+nutritionCustomFoodPathTest, map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "update_custom_food", map[string]any{
		"food_id":    "abc123",
		"serving_id": "srv456",
		"food_name":  "Simple Food",
		"calories":   100,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if strings.Contains(text, "Error") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("PUT", nutritionCustomFoodPathTest); len(calls) != 1 {
		t.Fatalf("PUT calls = %d, want 1", len(calls))
	}
}

func TestUpdateCustomFood204(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, map[string]any{"customFoods": []any{nutritionStoredFood()}})
	f.on("PUT "+nutritionCustomFoodPathTest, map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "update_custom_food", map[string]any{
		"food_id":    "abc123",
		"serving_id": "srv456",
		"food_name":  "Simple Food",
		"calories":   100,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "Custom food updated") {
		t.Fatalf("text = %q", text)
	}
}

func TestUpdateCustomFoodError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, map[string]any{"customFoods": []any{nutritionStoredFood()}})
	f.on("PUT "+nutritionCustomFoodPathTest, status(500, map[string]any{"message": "API error"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "update_custom_food", map[string]any{
		"food_id":    "abc123",
		"serving_id": "srv456",
		"food_name":  "Test",
		"calories":   100,
	})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	if !strings.Contains(text, "Error updating custom food") {
		t.Fatalf("text = %q", text)
	}
}

func TestUpdateCustomFoodForbiddenIsAnError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, map[string]any{"customFoods": []any{nutritionStoredFood()}})
	f.on("PUT "+nutritionCustomFoodPathTest, status(403, map[string]any{"message": "localized or non-English message"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "update_custom_food", map[string]any{
		"food_id":    "abc123",
		"serving_id": "srv456",
		"food_name":  "Test",
		"calories":   100,
	})
	var got map[string]any
	decodeJSON(t, text, &got)
	if msg, _ := got["message"].(string); !strings.Contains(msg, "Error updating custom food:") || !strings.Contains(msg, "localized or non-English message") {
		t.Fatalf("message = %v", got["message"])
	}
	if got["write_committed"] != false || got["status"] != "error" {
		t.Fatalf("result = %v", got)
	}
}

func TestUpdateCustomFoodWithBrandAndMicros(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, map[string]any{"customFoods": []any{nutritionStoredFood()}})
	f.on("PUT "+nutritionCustomFoodPathTest, map[string]any{"foodMetaData": map[string]any{"foodId": "abc123"}})
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "update_custom_food", map[string]any{
		"food_id":    "abc123",
		"serving_id": "srv456",
		"food_name":  "Branded Food",
		"calories":   300,
		"brand_name": "BigCo",
		"trans_fat":  1.0,
		"calcium":    260,
		"iron":       4,
		"vitamin_d":  5,
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	payload := nutritionLastPUT(t, f, nutritionCustomFoodPathTest)
	if mutMap(t, payload["foodMetaData"])["brandName"] != "BigCo" {
		t.Fatalf("payload = %v", payload)
	}
	nc := mutMap(t, mutList(t, payload["nutritionContents"])[0])
	want := map[string]any{"transFat": "1", "calcium": "260", "iron": "4", "vitaminD": "5"}
	for k, v := range want {
		if nc[k] != v {
			t.Fatalf("%s = %v, want %v", k, nc[k], v)
		}
	}
}

func TestUpdateCustomFoodPreservesBrandAndMicros(t *testing.T) {
	existing := map[string]any{
		"foodMetaData": map[string]any{"foodId": "abc123", "foodName": "Existing Food", "brandName": "OriginalBrand"},
		"nutritionContents": []any{
			map[string]any{"servingId": "srv456", "calories": 200, "transFat": 0.5, "calcium": 100, "iron": 3, "vitaminD": 2},
		},
	}
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, map[string]any{"customFoods": []any{existing}})
	f.on("PUT "+nutritionCustomFoodPathTest, map[string]any{})
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "update_custom_food", map[string]any{
		"food_id":    "abc123",
		"serving_id": "srv456",
		"food_name":  "Existing Food",
		"calories":   200,
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	payload := nutritionLastPUT(t, f, nutritionCustomFoodPathTest)
	if mutMap(t, payload["foodMetaData"])["brandName"] != "OriginalBrand" {
		t.Fatalf("payload = %v", payload)
	}
	nc := mutMap(t, mutList(t, payload["nutritionContents"])[0])
	want := map[string]any{"transFat": "0.5", "calcium": "100", "iron": "3", "vitaminD": "2"}
	for k, v := range want {
		if nc[k] != v {
			t.Fatalf("%s = %v, want %v", k, nc[k], v)
		}
	}
}

func TestUpdateCustomFoodBrandOverridesExisting(t *testing.T) {
	existing := map[string]any{
		"foodMetaData":      map[string]any{"foodId": "abc123", "foodName": "Food", "brandName": "OldBrand"},
		"nutritionContents": []any{map[string]any{"servingId": "srv456", "calories": 100}},
	}
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, map[string]any{"customFoods": []any{existing}})
	f.on("PUT "+nutritionCustomFoodPathTest, map[string]any{})
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "update_custom_food", map[string]any{
		"food_id":    "abc123",
		"serving_id": "srv456",
		"food_name":  "Food",
		"calories":   100,
		"brand_name": "NewBrand",
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	payload := nutritionLastPUT(t, f, nutritionCustomFoodPathTest)
	if mutMap(t, payload["foodMetaData"])["brandName"] != "NewBrand" {
		t.Fatalf("payload = %v", payload)
	}
}

func TestUpdateCustomFoodPreservesServingBasis(t *testing.T) {
	existing := map[string]any{
		"foodMetaData":      map[string]any{"foodId": "abc123", "foodName": "NA Beer"},
		"nutritionContents": []any{map[string]any{"servingId": "srv456", "servingUnit": "ML", "numberOfUnits": 355, "calories": 90}},
	}
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, map[string]any{"customFoods": []any{existing}})
	f.on("PUT "+nutritionCustomFoodPathTest, map[string]any{})
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "update_custom_food", map[string]any{
		"food_id":    "abc123",
		"serving_id": "srv456",
		"food_name":  "NA Beer",
		"calories":   90,
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	nc := mutMap(t, mutList(t, nutritionLastPUT(t, f, nutritionCustomFoodPathTest)["nutritionContents"])[0])
	if nc["servingUnit"] != "ML" || nc["numberOfUnits"] != "355" {
		t.Fatalf("nutrition = %v", nc)
	}
}

func TestUpdateCustomFoodOverridesServingBasis(t *testing.T) {
	existing := map[string]any{
		"foodMetaData":      map[string]any{"foodId": "abc123", "foodName": "NA Beer"},
		"nutritionContents": []any{map[string]any{"servingId": "srv456", "servingUnit": "ML", "numberOfUnits": 355, "calories": 90}},
	}
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, map[string]any{"customFoods": []any{existing}})
	f.on("PUT "+nutritionCustomFoodPathTest, map[string]any{})
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "update_custom_food", map[string]any{
		"food_id":         "abc123",
		"serving_id":      "srv456",
		"food_name":       "NA Beer",
		"calories":        90,
		"serving_unit":    "OZ",
		"number_of_units": 12,
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	nc := mutMap(t, mutList(t, nutritionLastPUT(t, f, nutritionCustomFoodPathTest)["nutritionContents"])[0])
	if nc["servingUnit"] != "OZ" || nc["numberOfUnits"] != "12" {
		t.Fatalf("nutrition = %v", nc)
	}
}

func TestUpdateCustomFoodPartialServingOverride(t *testing.T) {
	existing := map[string]any{
		"foodMetaData":      map[string]any{"foodId": "abc123", "foodName": "NA Beer"},
		"nutritionContents": []any{map[string]any{"servingId": "srv456", "servingUnit": "ML", "numberOfUnits": 355, "calories": 90}},
	}
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, map[string]any{"customFoods": []any{existing}})
	f.on("PUT "+nutritionCustomFoodPathTest, map[string]any{})
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "update_custom_food", map[string]any{
		"food_id":         "abc123",
		"serving_id":      "srv456",
		"food_name":       "NA Beer",
		"calories":        90,
		"number_of_units": 500,
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	nc := mutMap(t, mutList(t, nutritionLastPUT(t, f, nutritionCustomFoodPathTest)["nutritionContents"])[0])
	if nc["servingUnit"] != "ML" || nc["numberOfUnits"] != "500" {
		t.Fatalf("nutrition = %v", nc)
	}
}

func TestUpdateCustomFoodRefusesWithoutExistingRecord(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, map[string]any{"customFoods": []any{}})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "update_custom_food", map[string]any{
		"food_id":    "abc123",
		"serving_id": "srv456",
		"food_name":  "Simple Food",
		"calories":   100,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "could not read the current record for food_id abc123") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("PUT", nutritionCustomFoodPathTest); len(calls) != 0 {
		t.Fatalf("PUT calls = %d, want 0", len(calls))
	}
	calls := f.callsMatching("GET", nutritionCustomFoodPathTest)
	searches := make([]string, 0, len(calls))
	for _, call := range calls {
		searches = append(searches, call.Query.Get("searchExpression"))
	}
	if strings.Join(searches, ",") != "Simple Food," {
		t.Fatalf("searches = %v", searches)
	}
}

func TestUpdateCustomFoodRefusesWhenLookupFails(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, status(500, map[string]any{"message": "lookup failed"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "update_custom_food", map[string]any{
		"food_id":    "abc123",
		"serving_id": "srv456",
		"food_name":  "Simple Food",
		"calories":   100,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "could not read the current record") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("PUT", nutritionCustomFoodPathTest); len(calls) != 0 {
		t.Fatalf("PUT calls = %d, want 0", len(calls))
	}
}

func TestUpdateCustomFoodFullReplaceWithoutExistingRecord(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, map[string]any{"customFoods": []any{}})
	f.on("PUT "+nutritionCustomFoodPathTest, map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "update_custom_food", map[string]any{
		"food_id":         "abc123",
		"serving_id":      "srv456",
		"food_name":       "Simple Food",
		"calories":        100,
		"serving_unit":    "ML",
		"number_of_units": 250,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "Custom food updated") {
		t.Fatalf("text = %q", text)
	}
	nc := mutMap(t, mutList(t, nutritionLastPUT(t, f, nutritionCustomFoodPathTest)["nutritionContents"])[0])
	if nc["servingUnit"] != "ML" || nc["numberOfUnits"] != "250" {
		t.Fatalf("nutrition = %v", nc)
	}
}

func TestUpdateCustomFoodRenamePreservesOmittedFields(t *testing.T) {
	stored := map[string]any{
		"foodMetaData": map[string]any{"foodId": "abc123", "foodName": "Protein Shake", "brandName": "Acme"},
		"nutritionContents": []any{
			map[string]any{"servingId": "srv456", "servingUnit": "ML", "numberOfUnits": 355, "calories": 160, "protein": 30, "carbs": 5, "sodium": 200},
		},
	}
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, sequence{
		map[string]any{"customFoods": []any{}},
		map[string]any{"customFoods": []any{stored}},
	})
	f.on("PUT "+nutritionCustomFoodPathTest, map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "update_custom_food", map[string]any{
		"food_id":    "abc123",
		"serving_id": "srv456",
		"food_name":  "Vanilla Whey",
		"calories":   160,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "Custom food updated") {
		t.Fatalf("text = %q", text)
	}
	payload := nutritionLastPUT(t, f, nutritionCustomFoodPathTest)
	if mutMap(t, payload["foodMetaData"])["foodName"] != "Vanilla Whey" || mutMap(t, payload["foodMetaData"])["brandName"] != "Acme" {
		t.Fatalf("payload = %v", payload)
	}
	nc := mutMap(t, mutList(t, payload["nutritionContents"])[0])
	want := map[string]any{"servingUnit": "ML", "numberOfUnits": "355", "protein": "30", "carbs": "5", "sodium": "200"}
	for k, v := range want {
		if nc[k] != v {
			t.Fatalf("%s = %v, want %v", k, nc[k], v)
		}
	}
}

func TestUpdateCustomFoodFindsRecordBeyondFirstPage(t *testing.T) {
	others := make([]any, 0, 25)
	for i := 0; i < 25; i++ {
		others = append(others, map[string]any{
			"foodMetaData":      map[string]any{"foodId": "other" + strconv.Itoa(i), "foodName": "Other " + strconv.Itoa(i)},
			"nutritionContents": []any{map[string]any{"servingId": "s" + strconv.Itoa(i), "calories": 1}},
		})
	}
	stored := map[string]any{
		"foodMetaData":      map[string]any{"foodId": "abc123", "foodName": "Old Name", "brandName": "Acme"},
		"nutritionContents": []any{map[string]any{"servingId": "srv456", "calories": 90, "fat": 3}},
	}
	page0 := append([]any{}, others[:20]...)
	page1 := append(append([]any{}, others[20:]...), stored)
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, sequence{
		map[string]any{"customFoods": []any{}},
		map[string]any{"customFoods": page0},
		map[string]any{"customFoods": page1},
	})
	f.on("PUT "+nutritionCustomFoodPathTest, map[string]any{})
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "update_custom_food", map[string]any{
		"food_id":    "abc123",
		"serving_id": "srv456",
		"food_name":  "New Name",
		"calories":   90,
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	payload := nutritionLastPUT(t, f, nutritionCustomFoodPathTest)
	if mutMap(t, payload["foodMetaData"])["brandName"] != "Acme" {
		t.Fatalf("payload = %v", payload)
	}
	if mutMap(t, mutList(t, payload["nutritionContents"])[0])["fat"] != "3" {
		t.Fatalf("payload = %v", payload)
	}
}

// --- log_food / log_custom_food ---

func TestLogFood(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionFoodLogsPathTest+"/quickAdd", map[string]any{"status": "ok"})
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "log_food", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "12:30:00",
		"name":      "Chicken Breast",
		"calories":  200,
		"carbs":     3,
		"protein":   40,
		"fat":       4,
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	if calls := f.callsMatching("GET", nutritionMealsPath); len(calls) != 1 {
		t.Fatalf("meal calls = %d, want 1", len(calls))
	}
	payload := nutritionLastPUT(t, f, nutritionFoodLogsPathTest+"/quickAdd")
	if payload["mealDate"] != nutritionMealDate {
		t.Fatalf("payload = %v", payload)
	}
	items := mutList(t, payload["quickAddItems"])
	if len(items) != 1 {
		t.Fatalf("items = %v", items)
	}
	item := mutMap(t, items[0])
	want := map[string]any{
		"name":        "Chicken Breast",
		"mealId":      float64(20250),
		"calories":    "200",
		"carbs":       "3",
		"protein":     "40",
		"fat":         "4",
		"logCategory": "QUICK_ADD",
		"action":      "ADD",
		"logId":       nil,
	}
	for k, v := range want {
		if item[k] != v {
			t.Fatalf("%s = %v, want %v", k, item[k], v)
		}
	}
}

func TestLogFoodFallsBackToSnacks(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionFoodLogsPathTest+"/quickAdd", map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "log_food", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "10:00:00",
		"name":      "Oats",
		"calories":  150,
		"carbs":     27,
		"protein":   5,
		"fat":       3,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "Food logged successfully") {
		t.Fatalf("text = %q", text)
	}
	item := mutMap(t, mutList(t, nutritionLastPUT(t, f, nutritionFoodLogsPathTest+"/quickAdd")["quickAddItems"])[0])
	if item["mealId"] != float64(20252) || item["mealTime"] != "10:00:00" {
		t.Fatalf("item = %v", item)
	}
}

func TestLogFoodError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionFoodLogsPathTest+"/quickAdd", status(500, map[string]any{"message": "API error"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "log_food", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "12:00:00",
		"name":      "Test",
		"calories":  100,
		"carbs":     10,
		"protein":   5,
		"fat":       2,
	})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	if !strings.Contains(text, "Error logging food") {
		t.Fatalf("text = %q", text)
	}
}

func TestLogFoodForbiddenIsAnError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionFoodLogsPathTest+"/quickAdd", status(403, map[string]any{"message": "localized or non-English message"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "log_food", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "12:00:00",
		"name":      "Test",
		"calories":  100,
		"carbs":     10,
		"protein":   5,
		"fat":       2,
	})
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["status"] != "error" || got["upstream_status"] != float64(403) || got["upstream_body"] != "localized or non-English message" || got["write_committed"] != false {
		t.Fatalf("result = %v", got)
	}
}

func TestLogFoodWithoutMealWindowsIsUnavailable(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionMealsPath, map[string]any{"meals": []any{}})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "log_food", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "12:00:00",
		"name":      "Test",
		"calories":  100,
		"carbs":     10,
		"protein":   5,
		"fat":       2,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	want := "No nutrition meal windows found for 2024-01-15; cannot log food at '12:00:00'."
	if text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
}

func TestLogCustomFood(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionFoodLogsPathTest, map[string]any{"status": "ok"})
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "log_custom_food", map[string]any{
		"meal_date":   nutritionMealDate,
		"meal_time":   "12:30:00",
		"food_id":     "abc123",
		"serving_id":  "srv456",
		"serving_qty": 3,
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	if calls := f.callsMatching("GET", nutritionMealsPath); len(calls) != 1 {
		t.Fatalf("meal calls = %d, want 1", len(calls))
	}
	payload := nutritionLastPUT(t, f, nutritionFoodLogsPathTest)
	item := mutMap(t, mutList(t, payload["foodLogItems"])[0])
	want := map[string]any{
		"mealId":      float64(20250),
		"foodId":      "abc123",
		"servingId":   "srv456",
		"servingQty":  float64(3),
		"logCategory": "REGULAR_LOG",
		"action":      "ADD",
		"mealTime":    "12:30:00",
	}
	for k, v := range want {
		if item[k] != v {
			t.Fatalf("%s = %v, want %v", k, item[k], v)
		}
	}
}

func TestLogCustomFoodFallsBackToSnacks(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionFoodLogsPathTest, map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "log_custom_food", map[string]any{
		"meal_date":  nutritionMealDate,
		"meal_time":  "10:00:00",
		"food_id":    "abc123",
		"serving_id": "srv456",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "Food logged successfully") {
		t.Fatalf("text = %q", text)
	}
	item := mutMap(t, mutList(t, nutritionLastPUT(t, f, nutritionFoodLogsPathTest)["foodLogItems"])[0])
	if item["mealId"] != float64(20252) || item["servingQty"] != float64(1) {
		t.Fatalf("item = %v", item)
	}
}

func TestLogCustomFoodError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionFoodLogsPathTest, status(500, map[string]any{"message": "API error"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "log_custom_food", map[string]any{
		"meal_date":  nutritionMealDate,
		"meal_time":  "12:00:00",
		"food_id":    "abc123",
		"serving_id": "srv456",
	})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	if !strings.Contains(text, "Error logging food") {
		t.Fatalf("text = %q", text)
	}
}

func TestLogCustomFoodForbiddenIsAnError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionFoodLogsPathTest, status(403, map[string]any{"message": "localized or non-English message"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "log_custom_food", map[string]any{
		"meal_date":  nutritionMealDate,
		"meal_time":  "12:00:00",
		"food_id":    "abc123",
		"serving_id": "srv456",
	})
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["status"] != "error" || got["upstream_status"] != float64(403) || got["upstream_body"] != "localized or non-English message" || got["write_committed"] != false {
		t.Fatalf("result = %v", got)
	}
}

func TestLogCustomFoodUsesFatsecretSource(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionFoodLogsPathTest, map[string]any{"success": true})
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "log_custom_food", map[string]any{
		"meal_date":  nutritionMealDate,
		"meal_time":  "08:00:00",
		"food_id":    "4132350",
		"serving_id": "srv001",
		"source":     "FATSECRET",
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	item := mutMap(t, mutList(t, nutritionLastPUT(t, f, nutritionFoodLogsPathTest)["foodLogItems"])[0])
	if item["source"] != "FATSECRET" {
		t.Fatalf("source = %v", item["source"])
	}
}

func TestLogCustomFoodDefaultsToGarminSource(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionFoodLogsPathTest, map[string]any{"success": true})
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "log_custom_food", map[string]any{
		"meal_date":  nutritionMealDate,
		"meal_time":  "08:00:00",
		"food_id":    "abc-uuid-123",
		"serving_id": "srv002",
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	item := mutMap(t, mutList(t, nutritionLastPUT(t, f, nutritionFoodLogsPathTest)["foodLogItems"])[0])
	if item["source"] != "GARMIN" {
		t.Fatalf("source = %v", item["source"])
	}
}

// --- delete_custom_food / delete_food_log ---

func TestDeleteCustomFood(t *testing.T) {
	foodID := "08b27145e29d41479e36d8d3788fcccf"
	f := newFakeGarmin(t)
	f.on("DELETE "+nutritionCustomFoodPathTest+"/"+foodID, map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "delete_custom_food", map[string]any{"confirm": true, "food_id": foodID})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "success") || !strings.Contains(text, foodID) {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("DELETE", nutritionCustomFoodPathTest+"/"+foodID); len(calls) != 1 {
		t.Fatalf("DELETE calls = %d, want 1", len(calls))
	}
}

func TestDeleteCustomFoodError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("DELETE "+nutritionCustomFoodPathTest+"/08b27145e29d41479e36d8d3788fcccf", status(500, map[string]any{"message": "API error"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "delete_custom_food", map[string]any{"confirm": true, "food_id": "08b27145e29d41479e36d8d3788fcccf"})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	if !strings.Contains(text, "Error deleting custom food") {
		t.Fatalf("text = %q", text)
	}
}

func TestDeleteCustomFoodPreviewsWithoutDeleting(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, nutritionCustomFoods())
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "delete_custom_food", map[string]any{"food_id": "food001"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["status"] != "needs_confirmation" {
		t.Fatalf("result = %v", got)
	}
	food := mutMap(t, got["food"])
	if mutMap(t, food["foodMetaData"])["foodName"] != "Greek Yogurt" {
		t.Fatalf("food = %v", food)
	}
	if calls := f.callsMatching("DELETE", nutritionCustomFoodPathTest+"/food001"); len(calls) != 0 {
		t.Fatalf("DELETE calls = %d, want 0", len(calls))
	}
}

func TestDeleteFoodLog(t *testing.T) {
	logID := "581f7dc8797f421f8d7eea83e5d2c939"
	f := newFakeGarmin(t)
	f.on("DELETE "+nutritionFoodLogPath, map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "delete_food_log", map[string]any{"confirm": true, "log_id": logID, "meal_date": nutritionMealDate})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "success") || !strings.Contains(text, logID) {
		t.Fatalf("text = %q", text)
	}
	calls := f.callsMatching("DELETE", nutritionFoodLogPath)
	if len(calls) != 1 {
		t.Fatalf("DELETE calls = %d, want 1", len(calls))
	}
	sameJSON(t, decodeBody(t, calls[0].Body), map[string]any{"logIds": []any{logID}})
}

func TestDeleteFoodLogError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("DELETE "+nutritionFoodLogPath, status(500, map[string]any{"message": "API error"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "delete_food_log", map[string]any{"confirm": true, "log_id": "99001", "meal_date": nutritionMealDate})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	if !strings.Contains(text, "Error deleting food log") {
		t.Fatalf("text = %q", text)
	}
}

func TestDeleteFoodLogNotFound(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("DELETE "+nutritionFoodLogPath, status(404, map[string]any{"message": "localized or non-English message"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "delete_food_log", map[string]any{"confirm": true, "log_id": "-1", "meal_date": nutritionMealDate})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	sameJSON(t, got, map[string]any{
		"status":  "not_found",
		"log_id":  "-1",
		"message": "Food log entry -1 was not found.",
	})
}

func TestDeleteFoodLogNotFoundFallsBackToStatusInMessage(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("DELETE "+nutritionFoodLogPath, status(404, map[string]any{"message": "recurso inexistente"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "delete_food_log", map[string]any{"confirm": true, "log_id": "-1", "meal_date": nutritionMealDate})
	var got map[string]any
	decodeJSON(t, text, &got)
	sameJSON(t, got, map[string]any{
		"status":  "not_found",
		"log_id":  "-1",
		"message": "Food log entry -1 was not found.",
	})
}

func TestDeleteFoodLogAcceptsHexUUID(t *testing.T) {
	hexLogID := "581f7dc8797f421f8d7eea83e5d2c939"
	f := newFakeGarmin(t)
	f.on("DELETE "+nutritionFoodLogPath, map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "delete_food_log", map[string]any{"confirm": true, "log_id": hexLogID, "meal_date": nutritionMealDate})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "success") || !strings.Contains(text, hexLogID) {
		t.Fatalf("text = %q", text)
	}
	calls := f.callsMatching("DELETE", nutritionFoodLogPath)
	if len(calls) != 1 {
		t.Fatalf("DELETE calls = %d, want 1", len(calls))
	}
	sameJSON(t, decodeBody(t, calls[0].Body), map[string]any{"logIds": []any{hexLogID}})
}

func TestDeleteFoodLogPreviewsMatchingNestedEntry(t *testing.T) {
	item := map[string]any{"logId": "target", "foodName": "Oats", "calories": 100}
	f := newFakeGarmin(t)
	f.on("GET "+nutritionFoodLogPath, map[string]any{
		"mealDetails": []any{
			map[string]any{"loggedFoods": []any{
				map[string]any{"logId": "other", "foodName": "Rice"},
				item,
			}},
		},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "delete_food_log", map[string]any{"log_id": "target", "meal_date": nutritionMealDate})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["status"] != "needs_confirmation" {
		t.Fatalf("result = %v", got)
	}
	sameJSON(t, got["entry"], item)
	if calls := f.callsMatching("DELETE", nutritionFoodLogPath); len(calls) != 0 {
		t.Fatalf("DELETE calls = %d, want 0", len(calls))
	}
}

func TestFoodDeletePreviewNotFoundNeverDeletes(t *testing.T) {
	cases := []struct {
		name string
		tool string
		args map[string]any
	}{
		{"custom_food", "delete_custom_food", map[string]any{"food_id": "missing"}},
		{"food_log", "delete_food_log", map[string]any{"log_id": "missing", "meal_date": nutritionMealDate}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on("GET "+nutritionCustomFoodPathTest, map[string]any{})
			f.on("GET "+nutritionFoodLogPath, map[string]any{})
			cs := newSession(t, f, nil)

			text, isErr := callTool(t, cs, tc.tool, tc.args)
			if isErr {
				t.Fatalf("unexpected error: %q", text)
			}
			var got map[string]any
			decodeJSON(t, text, &got)
			if got["status"] != "not_found" {
				t.Fatalf("result = %v", got)
			}
			if calls := f.callsMatching("DELETE", nutritionCustomFoodPathTest+"/missing"); len(calls) != 0 {
				t.Fatalf("DELETE calls = %d, want 0", len(calls))
			}
		})
	}
}

func TestFoodDeletePreviewErrorNeverDeletes(t *testing.T) {
	cases := []struct {
		name string
		tool string
		args map[string]any
	}{
		{"custom_food", "delete_custom_food", map[string]any{"food_id": "food001"}},
		{"food_log", "delete_food_log", map[string]any{"log_id": "target", "meal_date": nutritionMealDate}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on("GET "+nutritionCustomFoodPathTest, status(500, map[string]any{"message": "API error"}))
			f.on("GET "+nutritionFoodLogPath, status(500, map[string]any{"message": "API error"}))
			cs := newSession(t, f, nil)

			text, _ := callTool(t, cs, tc.tool, tc.args)
			if !strings.Contains(text, "Error deleting") {
				t.Fatalf("text = %q", text)
			}
			if calls := f.callsMatching("DELETE", nutritionCustomFoodPathTest+"/food001"); len(calls) != 0 {
				t.Fatalf("DELETE calls = %d, want 0", len(calls))
			}
			if calls := f.callsMatching("DELETE", nutritionFoodLogPath); len(calls) != 0 {
				t.Fatalf("DELETE calls = %d, want 0", len(calls))
			}
		})
	}
}

// --- upsert_and_log ---

func TestUpsertAndLogExistingFood(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, nutritionCustomFoods())
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionFoodLogsPathTest, map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "upsert_and_log", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "08:30:00",
		"food_name": "Greek Yogurt",
		"calories":  100,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "Food logged successfully") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("PUT", nutritionFoodLogsPathTest); len(calls) != 1 {
		t.Fatalf("log PUT calls = %d, want 1", len(calls))
	}
	if calls := f.callsMatching("PUT", nutritionCustomFoodPathTest); len(calls) != 0 {
		t.Fatalf("create PUT calls = %d, want 0", len(calls))
	}
	item := mutMap(t, mutList(t, nutritionLastPUT(t, f, nutritionFoodLogsPathTest)["foodLogItems"])[0])
	if item["foodId"] != "food001" || item["servingId"] != "srv001" || item["mealId"] != float64(20249) {
		t.Fatalf("item = %v", item)
	}
	q := f.callsMatching("GET", nutritionCustomFoodPathTest)[0].Query
	if q.Get("searchExpression") != "Greek Yogurt" || q.Get("start") != "0" || q.Get("limit") != "20" || q.Get("includeContent") != "true" {
		t.Fatalf("query = %v", q)
	}
}

func TestUpsertAndLogCreatesNewFood(t *testing.T) {
	createdFood := map[string]any{
		"foodMetaData":      map[string]any{"foodId": "food999", "foodName": "New Food"},
		"nutritionContents": []any{map[string]any{"servingId": "srv999"}},
	}
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, map[string]any{"customFoods": []any{}})
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionCustomFoodPathTest, createdFood)
	f.on("PUT "+nutritionFoodLogsPathTest, map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "upsert_and_log", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "12:00:00",
		"food_name": "New Food",
		"calories":  200,
		"protein":   20,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "Food logged successfully") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.recorded(); len(calls) != 4 {
		t.Fatalf("calls = %d, want 4", len(calls))
	}
	item := mutMap(t, mutList(t, nutritionLastPUT(t, f, nutritionFoodLogsPathTest)["foodLogItems"])[0])
	if item["foodId"] != "food999" || item["servingId"] != "srv999" {
		t.Fatalf("item = %v", item)
	}
	q := f.callsMatching("GET", nutritionCustomFoodPathTest)[0].Query
	if q.Get("searchExpression") != "New Food" {
		t.Fatalf("query = %v", q)
	}
}

func TestUpsertAndLogRecoversIdsAfterBodylessCreate(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, sequence{
		map[string]any{"customFoods": []any{}},
		nutritionCustomFoods(),
	})
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionCustomFoodPathTest, map[string]any{})
	f.on("PUT "+nutritionFoodLogsPathTest, map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "upsert_and_log", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "12:00:00",
		"food_name": "Greek Yogurt",
		"calories":  200,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "Food logged successfully") {
		t.Fatalf("text = %q", text)
	}
	item := mutMap(t, mutList(t, nutritionLastPUT(t, f, nutritionFoodLogsPathTest)["foodLogItems"])[0])
	if item["foodId"] != "food001" || item["servingId"] != "srv001" {
		t.Fatalf("item = %v", item)
	}
	calls := f.callsMatching("GET", nutritionCustomFoodPathTest)
	if len(calls) != 2 {
		t.Fatalf("search calls = %d, want 2", len(calls))
	}
	for _, call := range calls {
		if call.Query.Get("searchExpression") != "Greek Yogurt" || call.Query.Get("start") != "0" {
			t.Fatalf("query = %v", call.Query)
		}
	}
}

func TestUpsertAndLogWithBrandNameOnCreate(t *testing.T) {
	createdFood := map[string]any{
		"foodMetaData":      map[string]any{"foodId": "food999", "foodName": "Branded Beer"},
		"nutritionContents": []any{map[string]any{"servingId": "srv999"}},
	}
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, map[string]any{"customFoods": []any{}})
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionCustomFoodPathTest, createdFood)
	f.on("PUT "+nutritionFoodLogsPathTest, map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "upsert_and_log", map[string]any{
		"meal_date":  nutritionMealDate,
		"meal_time":  "12:00:00",
		"food_name":  "Branded Beer",
		"calories":   150,
		"brand_name": "Athletic Brewing",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "Food logged successfully") {
		t.Fatalf("text = %q", text)
	}
	if mutMap(t, nutritionLastPUT(t, f, nutritionCustomFoodPathTest)["foodMetaData"])["brandName"] != "Athletic Brewing" {
		t.Fatalf("payload = %v", nutritionLastPUT(t, f, nutritionCustomFoodPathTest))
	}
}

func TestUpsertAndLogNoBrandNameOmitsKeyOnCreate(t *testing.T) {
	createdFood := map[string]any{
		"foodMetaData":      map[string]any{"foodId": "food999", "foodName": "Plain Food"},
		"nutritionContents": []any{map[string]any{"servingId": "srv999"}},
	}
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, map[string]any{"customFoods": []any{}})
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionCustomFoodPathTest, createdFood)
	f.on("PUT "+nutritionFoodLogsPathTest, map[string]any{})
	cs := newSession(t, f, nil)

	_, isErr := callTool(t, cs, "upsert_and_log", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "12:00:00",
		"food_name": "Plain Food",
		"calories":  150,
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	if _, ok := mutMap(t, nutritionLastPUT(t, f, nutritionCustomFoodPathTest)["foodMetaData"])["brandName"]; ok {
		t.Fatal("brandName present")
	}
}

func TestUpsertAndLogBrandNameIgnoredOnExistingMatch(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, nutritionCustomFoods())
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionFoodLogsPathTest, map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "upsert_and_log", map[string]any{
		"meal_date":  nutritionMealDate,
		"meal_time":  "08:30:00",
		"food_name":  "Greek Yogurt",
		"calories":   100,
		"brand_name": "Chobani",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "Food logged successfully") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("PUT", nutritionFoodLogsPathTest); len(calls) != 1 {
		t.Fatalf("log PUT calls = %d, want 1", len(calls))
	}
	if calls := f.callsMatching("PUT", nutritionCustomFoodPathTest); len(calls) != 0 {
		t.Fatalf("create PUT calls = %d, want 0", len(calls))
	}
	item := mutMap(t, mutList(t, nutritionLastPUT(t, f, nutritionFoodLogsPathTest)["foodLogItems"])[0])
	if item["foodId"] != "food001" {
		t.Fatalf("item = %v", item)
	}
}

func TestUpsertAndLogError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, status(500, map[string]any{"message": "API error"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "upsert_and_log", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "12:00:00",
		"food_name": "Test Food",
		"calories":  100,
	})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	if !strings.Contains(text, "Error in upsert_and_log") {
		t.Fatalf("text = %q", text)
	}
}

func TestUpsertAndLogForbiddenIsAnError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, status(403, map[string]any{"message": "HTTP 403 Forbidden"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "upsert_and_log", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "12:00:00",
		"food_name": "Test Food",
		"calories":  100,
	})
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["message"] != "Error in upsert_and_log: API Error 403 - HTTP 403 Forbidden" {
		t.Fatalf("message = %v", got["message"])
	}
	if got["upstream_status"] != float64(403) || got["write_committed"] != false {
		t.Fatalf("result = %v", got)
	}
	if calls := f.callsMatching("PUT", nutritionCustomFoodPathTest); len(calls) != 0 {
		t.Fatalf("PUT calls = %d, want 0", len(calls))
	}
}

func TestUpsertAndLogLogTimeoutIsUnknown(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, nutritionCustomFoods())
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionFoodLogsPathTest, delay{wait: time.Second, body: map[string]any{}})
	cs := newSession(t, f, map[string]string{"GARMIN_MCP_CALL_TIMEOUT": "0.2"})

	text, isErr := callTool(t, cs, "upsert_and_log", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "12:00:00",
		"food_name": "Greek Yogurt",
		"calories":  100,
	})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["tool"] != "upsert_and_log" || got["stage"] != "garmin_request" || got["write_committed"] != "unknown" {
		t.Fatalf("result = %v", got)
	}
	if hint, _ := got["hint"].(string); !strings.Contains(hint, "before retrying") {
		t.Fatalf("hint = %v", got["hint"])
	}
	if calls := f.callsMatching("PUT", nutritionFoodLogsPathTest); len(calls) != 1 {
		t.Fatalf("log PUT calls = %d, want 1", len(calls))
	}
}

func TestUpsertAndLogCreateTimeoutIsUnknown(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, map[string]any{"customFoods": []any{}})
	f.on("PUT "+nutritionCustomFoodPathTest, delay{wait: time.Second, body: map[string]any{}})
	cs := newSession(t, f, map[string]string{"GARMIN_MCP_CALL_TIMEOUT": "0.2"})

	text, isErr := callTool(t, cs, "upsert_and_log", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "12:00:00",
		"food_name": "Greek Yogurt",
		"calories":  100,
	})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["write_committed"] != "unknown" {
		t.Fatalf("result = %v", got)
	}
	if calls := f.callsMatching("PUT", nutritionCustomFoodPathTest); len(calls) != 1 {
		t.Fatalf("create PUT calls = %d, want 1", len(calls))
	}
}

func TestUpsertAndLogLogRejectedAfterCreate(t *testing.T) {
	createdFood := map[string]any{
		"foodMetaData":      map[string]any{"foodId": "food999", "foodName": "Greek Yogurt"},
		"nutritionContents": []any{map[string]any{"servingId": "srv999"}},
	}
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, map[string]any{"customFoods": []any{}})
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionCustomFoodPathTest, createdFood)
	f.on("PUT "+nutritionFoodLogsPathTest, status(400, map[string]any{"message": "bad meal"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "upsert_and_log", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "12:00:00",
		"food_name": "Greek Yogurt",
		"calories":  100,
	})
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["write_committed"] != false || got["upstream_status"] != float64(400) {
		t.Fatalf("result = %v", got)
	}
	if msg, _ := got["message"].(string); !strings.Contains(msg, "custom food food999 was created") {
		t.Fatalf("message = %v", got["message"])
	}
	if got["custom_food_created"] != true {
		t.Fatalf("result = %v", got)
	}
	if hint, _ := got["hint"].(string); !strings.Contains(hint, "not logged") || strings.Contains(hint, "Nothing was written") {
		t.Fatalf("hint = %v", got["hint"])
	}
}

func TestUpsertAndLogSkipsNullSearchMetadata(t *testing.T) {
	search := map[string]any{
		"customFoods": []any{
			map[string]any{"foodMetaData": nil, "nutritionContents": nil},
			map[string]any{"foodMetaData": map[string]any{"foodId": "x", "foodName": nil}, "nutritionContents": []any{}},
			mutList(t, nutritionCustomFoods()["customFoods"])[0],
		},
	}
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, search)
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionFoodLogsPathTest, map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "upsert_and_log", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "12:00:00",
		"food_name": "Greek Yogurt",
		"calories":  100,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if text != "Food logged successfully." {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("PUT", nutritionCustomFoodPathTest); len(calls) != 0 {
		t.Fatalf("create PUT calls = %d, want 0", len(calls))
	}
	item := mutMap(t, mutList(t, nutritionLastPUT(t, f, nutritionFoodLogsPathTest)["foodLogItems"])[0])
	if item["foodId"] != "food001" || item["servingId"] != "srv001" {
		t.Fatalf("item = %v", item)
	}
}

func TestUpsertLookupFailureAfterCreateDoesNotCommitLog(t *testing.T) {
	cases := []struct {
		name    string
		second  any
		timeout bool
	}{
		{"garmin_500", status(500, map[string]any{"message": "lookup failed"}), false},
		{"garmin_401", status(401, map[string]any{"message": "expired"}), false},
		{"timeout", delay{wait: 500 * time.Millisecond, body: map[string]any{"customFoods": []any{}}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on("GET "+nutritionCustomFoodPathTest, sequence{
				map[string]any{"customFoods": []any{}},
				tc.second,
			})
			f.on("PUT "+nutritionCustomFoodPathTest, map[string]any{})
			env := map[string]string(nil)
			if tc.timeout {
				env = map[string]string{"GARMIN_MCP_CALL_TIMEOUT": "0.1"}
			}
			cs := newSession(t, f, env)

			text, _ := callTool(t, cs, "upsert_and_log", map[string]any{
				"meal_date": nutritionMealDate,
				"meal_time": "12:00:00",
				"food_name": "Greek Yogurt",
				"calories":  100,
			})
			var got map[string]any
			decodeJSON(t, text, &got)
			if got["write_committed"] != false || got["custom_food_created"] != true {
				t.Fatalf("result = %v", got)
			}
			if hint, _ := got["hint"].(string); !strings.Contains(hint, "not logged") || strings.Contains(hint, "Nothing was written") {
				t.Fatalf("hint = %v", got["hint"])
			}
			calls := f.callsMatching("PUT", nutritionCustomFoodPathTest)
			if len(calls) != 1 {
				t.Fatalf("create PUT calls = %d, want 1", len(calls))
			}
		})
	}
}

func TestUpsertMissingIdsAfterCreateIsStructured(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, map[string]any{"customFoods": []any{}})
	f.on("PUT "+nutritionCustomFoodPathTest, map[string]any{})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "upsert_and_log", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "12:00:00",
		"food_name": "Greek Yogurt",
		"calories":  100,
	})
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["status"] != "error" || got["write_committed"] != false || got["custom_food_created"] != true {
		t.Fatalf("result = %v", got)
	}
	if hint, _ := got["hint"].(string); !strings.Contains(hint, "not logged") {
		t.Fatalf("hint = %v", got["hint"])
	}
	if calls := f.callsMatching("PUT", nutritionCustomFoodPathTest); len(calls) != 1 {
		t.Fatalf("create PUT calls = %d, want 1", len(calls))
	}
}

func TestUpsertMissingMealsAfterCreateReportsPartialWrite(t *testing.T) {
	createdFood := mutList(t, nutritionCustomFoods()["customFoods"])[0]
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, map[string]any{"customFoods": []any{}})
	f.on("GET "+nutritionMealsPath, map[string]any{"meals": []any{}})
	f.on("PUT "+nutritionCustomFoodPathTest, createdFood)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "upsert_and_log", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "12:00:00",
		"food_name": "Greek Yogurt",
		"calories":  100,
	})
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["write_committed"] != false || got["custom_food_created"] != true || got["food_id"] != "food001" {
		t.Fatalf("result = %v", got)
	}
	if msg, _ := got["message"].(string); !strings.Contains(msg, "No nutrition meal windows") {
		t.Fatalf("message = %v", got["message"])
	}
	if calls := f.callsMatching("PUT", nutritionCustomFoodPathTest); len(calls) != 1 {
		t.Fatalf("create PUT calls = %d, want 1", len(calls))
	}
}

func TestUpsertPagesExactNameMatchBeforeCreating(t *testing.T) {
	variants := make([]any, 0, 20)
	for i := 0; i < 20; i++ {
		variants = append(variants, map[string]any{
			"foodMetaData":      map[string]any{"foodId": "variant" + strconv.Itoa(i), "foodName": "Greek Yogurt " + strconv.Itoa(i)},
			"nutritionContents": []any{map[string]any{"servingId": "s" + strconv.Itoa(i)}},
		})
	}
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, sequence{
		map[string]any{"customFoods": variants},
		nutritionCustomFoods(),
	})
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionFoodLogsPathTest, map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "upsert_and_log", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "12:00:00",
		"food_name": "Greek Yogurt",
		"calories":  100,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if text != "Food logged successfully." {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("PUT", nutritionCustomFoodPathTest); len(calls) != 0 {
		t.Fatalf("create PUT calls = %d, want 0", len(calls))
	}
	item := mutMap(t, mutList(t, nutritionLastPUT(t, f, nutritionFoodLogsPathTest)["foodLogItems"])[0])
	if item["foodId"] != "food001" {
		t.Fatalf("item = %v", item)
	}
	starts := make([]string, 0)
	for _, call := range f.callsMatching("GET", nutritionCustomFoodPathTest) {
		starts = append(starts, call.Query.Get("start"))
	}
	if strings.Join(starts, ",") != "0,20" {
		t.Fatalf("starts = %v", starts)
	}
}

func TestUpsertRefusesCreationWhenSearchLimitIsExhausted(t *testing.T) {
	foods := make([]any, 0, 20)
	for i := 0; i < 20; i++ {
		foods = append(foods, map[string]any{
			"foodMetaData": map[string]any{"foodName": "Greek Yogurt " + strconv.Itoa(i)},
		})
	}
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, map[string]any{"customFoods": foods})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "upsert_and_log", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "12:00:00",
		"food_name": "Greek Yogurt",
		"calories":  100,
	})
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["write_committed"] != false {
		t.Fatalf("result = %v", got)
	}
	if msg, _ := got["message"].(string); !strings.Contains(msg, "search limit") {
		t.Fatalf("message = %v", got["message"])
	}
	if calls := f.callsMatching("PUT", nutritionCustomFoodPathTest); len(calls) != 0 {
		t.Fatalf("PUT calls = %d, want 0", len(calls))
	}
}

func TestUpsertExistingFoodWithoutServingDoesNotCreateDuplicate(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionCustomFoodPathTest, map[string]any{"customFoods": []any{
		map[string]any{
			"foodMetaData":      map[string]any{"foodId": "food001", "foodName": "Greek Yogurt"},
			"nutritionContents": nil,
		},
	}})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "upsert_and_log", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "12:00:00",
		"food_name": "Greek Yogurt",
		"calories":  100,
	})
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["write_committed"] != false {
		t.Fatalf("result = %v", got)
	}
	if msg, _ := got["message"].(string); !strings.Contains(msg, "serving") {
		t.Fatalf("message = %v", got["message"])
	}
	if calls := f.callsMatching("PUT", nutritionCustomFoodPathTest); len(calls) != 0 {
		t.Fatalf("PUT calls = %d, want 0", len(calls))
	}
}

func TestUpsertResponseDecodeCommitStatusRefersToFoodLog(t *testing.T) {
	for _, phase := range []string{"create", "log"} {
		t.Run(phase, func(t *testing.T) {
			f := newFakeGarmin(t)
			if phase == "create" {
				f.on("GET "+nutritionCustomFoodPathTest, map[string]any{"customFoods": []any{}})
				f.on("PUT "+nutritionCustomFoodPathTest, rawBody([]byte("invalid JSON")))
			} else {
				f.on("GET "+nutritionCustomFoodPathTest, nutritionCustomFoods())
				f.on("GET "+nutritionMealsPath, nutritionMeals())
				f.on("PUT "+nutritionFoodLogsPathTest, rawBody([]byte("invalid JSON")))
			}
			cs := newSession(t, f, nil)

			text, _ := callTool(t, cs, "upsert_and_log", map[string]any{
				"meal_date": nutritionMealDate,
				"meal_time": "12:00:00",
				"food_name": "Greek Yogurt",
				"calories":  100,
			})
			var got map[string]any
			decodeJSON(t, text, &got)
			wantCommitted := phase == "log"
			if got["write_committed"] != wantCommitted {
				t.Fatalf("result = %v", got)
			}
			if phase == "create" {
				if got["custom_food_created"] != true {
					t.Fatalf("result = %v", got)
				}
				if hint, _ := got["hint"].(string); !strings.Contains(hint, "not logged") {
					t.Fatalf("hint = %v", got["hint"])
				}
			}
			if phase == "create" {
				if calls := f.callsMatching("PUT", nutritionCustomFoodPathTest); len(calls) != 1 {
					t.Fatalf("create PUT calls = %d, want 1", len(calls))
				}
			} else {
				if calls := f.callsMatching("PUT", nutritionFoodLogsPathTest); len(calls) != 1 {
					t.Fatalf("log PUT calls = %d, want 1", len(calls))
				}
			}
		})
	}
}

// --- set_nutrition_daily_settings ---

func TestSetNutritionDailySettingsUpdatesAllFields(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionSettingsPath, nutritionSettings())
	f.on("PUT "+nutritionSettingsPath, map[string]any{
		"calorieGoal": 1850,
		"macroGoals":  map[string]any{"carbs": 205, "fat": 60, "protein": 140},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_nutrition_daily_settings", map[string]any{
		"date":          nutritionMealDate,
		"calorie_goal":  1800,
		"carbs_grams":   200,
		"fat_grams":     60,
		"protein_grams": 140,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	sameJSON(t, got, map[string]any{
		"status":                  "updated",
		"date":                    nutritionMealDate,
		"calorie_goal":            float64(1850),
		"carbs_grams":             float64(205),
		"fat_grams":               float64(60),
		"protein_grams":           float64(140),
		"weight_goal_target_date": nil,
	})
	expected := nutritionSettings()
	expected["calorieGoal"] = float64(1800)
	expected["macroGoals"] = map[string]any{"carbs": float64(200), "fat": float64(60), "protein": float64(140)}
	sameJSON(t, nutritionLastPUT(t, f, nutritionSettingsPath), expected)
}

func TestSetNutritionDailySettingsReportsPassedWeightGoalTarget(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionSettingsSepPath, nutritionSettings())
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_nutrition_daily_settings", map[string]any{
		"date":         "2024-09-01",
		"calorie_goal": 1800,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "targetDate") || !strings.Contains(text, "2024-06-01") || !strings.Contains(text, "2024-09-01") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("PUT", nutritionSettingsSepPath); len(calls) != 0 {
		t.Fatalf("PUT calls = %d, want 0", len(calls))
	}
}

func TestSetNutritionDailySettingsAcceptsExplicitTargetDate(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionSettingsSepPath, nutritionSettings())
	f.on("PUT "+nutritionSettingsSepPath, map[string]any{
		"calorieGoal": 1800,
		"macroGoals":  nutritionSettings()["macroGoals"],
		"targetDate":  "2024-12-31",
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_nutrition_daily_settings", map[string]any{
		"date":         "2024-09-01",
		"calorie_goal": 1800,
		"target_date":  "2024-12-31",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["status"] != "updated" || got["weight_goal_target_date"] != "2024-12-31" {
		t.Fatalf("result = %v", got)
	}
	expected := nutritionSettings()
	expected["targetDate"] = "2024-12-31"
	expected["calorieGoal"] = float64(1800)
	sameJSON(t, nutritionLastPUT(t, f, nutritionSettingsSepPath), expected)
}

func TestSetNutritionDailySettingsRejectsTargetDateNotAfterDate(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionSettingsSepPath, nutritionSettings())
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_nutrition_daily_settings", map[string]any{
		"date":         "2024-09-01",
		"calorie_goal": 1800,
		"target_date":  "2024-09-01",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "must be after") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("PUT", nutritionSettingsSepPath); len(calls) != 0 {
		t.Fatalf("PUT calls = %d, want 0", len(calls))
	}
}

func TestSetNutritionDailySettingsPartialUpdate(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value float64
	}{
		{"calorie_goal", "calorieGoal", 1900},
		{"carbs_grams", "carbs", 0},
		{"fat_grams", "fat", 55},
		{"protein_grams", "protein", 130},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expected := nutritionSettings()
			if tc.key == "calorieGoal" {
				expected["calorieGoal"] = tc.value
			} else {
				expected["macroGoals"].(map[string]any)[tc.key] = tc.value
			}
			stored := deepCopyValue(expected).(map[string]any)
			stored["calorieGoal"] = float64(1950)

			f := newFakeGarmin(t)
			f.on("GET "+nutritionSettingsPath, sequence{nutritionSettings(), stored})
			f.on("PUT "+nutritionSettingsPath, nil)
			cs := newSession(t, f, nil)

			text, isErr := callTool(t, cs, "set_nutrition_daily_settings", map[string]any{
				"date":  nutritionMealDate,
				tc.name: tc.value,
			})
			if isErr {
				t.Fatalf("unexpected error: %q", text)
			}
			var got map[string]any
			decodeJSON(t, text, &got)
			if got["calorie_goal"] != float64(1950) {
				t.Fatalf("result = %v", got)
			}
			if num(t, got["carbs_grams"]) != num(t, expected["macroGoals"].(map[string]any)["carbs"]) {
				t.Fatalf("result = %v", got)
			}
			sameJSON(t, nutritionLastPUT(t, f, nutritionSettingsPath), expected)
			if calls := f.callsMatching("GET", nutritionSettingsPath); len(calls) != 2 {
				t.Fatalf("GET calls = %d, want 2", len(calls))
			}
		})
	}
}

func TestSetNutritionDailySettingsUnverifiedWrite(t *testing.T) {
	cases := []struct {
		name     string
		readback any
	}{
		{"none", nil},
		{"error", status(500, map[string]any{"message": "read-back unavailable"})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on("GET "+nutritionSettingsPath, sequence{nutritionSettings(), tc.readback})
			f.on("PUT "+nutritionSettingsPath, nil)
			cs := newSession(t, f, nil)

			text, isErr := callTool(t, cs, "set_nutrition_daily_settings", map[string]any{
				"date":         nutritionMealDate,
				"calorie_goal": 1900,
			})
			if isErr {
				t.Fatalf("unexpected error: %q", text)
			}
			if !strings.Contains(text, "could not verify") {
				t.Fatalf("text = %q", text)
			}
			if strings.Contains(text, `"status": "updated"`) {
				t.Fatalf("text = %q", text)
			}
		})
	}
}

func TestSetNutritionDailySettingsNoFieldsProvided(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_nutrition_daily_settings", map[string]any{"date": nutritionMealDate})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "No fields to update") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.recorded(); len(calls) != 0 {
		t.Fatalf("calls = %d, want 0", len(calls))
	}
}

func TestSetNutritionDailySettingsNoCurrentData(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionSettingsPath, nil)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_nutrition_daily_settings", map[string]any{
		"date":         nutritionMealDate,
		"calorie_goal": 1800,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "Could not read current") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("PUT", nutritionSettingsPath); len(calls) != 0 {
		t.Fatalf("PUT calls = %d, want 0", len(calls))
	}
}

func TestSetNutritionDailySettingsAPIError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionSettingsPath, nutritionSettings())
	f.on("PUT "+nutritionSettingsPath, status(403, map[string]any{"message": "403 Forbidden"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_nutrition_daily_settings", map[string]any{
		"date":         nutritionMealDate,
		"calorie_goal": 1800,
	})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	if !strings.Contains(text, "Error updating nutrition settings") || !strings.Contains(text, "403") {
		t.Fatalf("text = %q", text)
	}
}

func TestSetNutritionSettingsHandlesSparseMacros(t *testing.T) {
	cases := []struct {
		name   string
		macros any
		want   map[string]any
	}{
		{"null", nil, map[string]any{"protein": float64(130)}},
		{"empty", map[string]any{}, map[string]any{"protein": float64(130)}},
		{"fiber", map[string]any{"fiber": float64(30)}, map[string]any{"fiber": float64(30), "protein": float64(130)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			current := nutritionSettings()
			current["macroGoals"] = tc.macros
			expected := deepCopyValue(current).(map[string]any)
			expected["macroGoals"] = tc.want

			f := newFakeGarmin(t)
			f.on("GET "+nutritionSettingsPath, current)
			f.on("PUT "+nutritionSettingsPath, expected)
			cs := newSession(t, f, nil)

			text, isErr := callTool(t, cs, "set_nutrition_daily_settings", map[string]any{
				"date":          nutritionMealDate,
				"protein_grams": 130,
			})
			if isErr {
				t.Fatalf("unexpected error: %q", text)
			}
			var got map[string]any
			decodeJSON(t, text, &got)
			if got["protein_grams"] != float64(130) {
				t.Fatalf("result = %v", got)
			}
			sameJSON(t, nutritionLastPUT(t, f, nutritionSettingsPath), expected)
		})
	}
}

func TestSetNutritionSettingsRejectsInvalidMacros(t *testing.T) {
	current := nutritionSettings()
	current["macroGoals"] = []any{250, 65, 120}
	f := newFakeGarmin(t)
	f.on("GET "+nutritionSettingsPath, current)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_nutrition_daily_settings", map[string]any{
		"date":          nutritionMealDate,
		"protein_grams": 130,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "cannot apply update") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("PUT", nutritionSettingsPath); len(calls) != 0 {
		t.Fatalf("PUT calls = %d, want 0", len(calls))
	}
}

// --- search_foods ---

func TestSearchFoodsReturnsResults(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionFoodSearchPathTest, nutritionSearchResponse())
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "search_foods", map[string]any{"query": "Cheerios & Oats/é"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["count"] != float64(1) {
		t.Fatalf("result = %v", got)
	}
	results := mutList(t, got["results"])
	result := mutMap(t, results[0])
	if result["name"] != "Cheerios" || result["source"] != "FATSECRET" || result["brand"] != "General Mills" {
		t.Fatalf("result = %v", result)
	}
	servings := mutList(t, result["servings"])
	if mutMap(t, servings[0])["calories"] != 100.0 {
		t.Fatalf("servings = %v", servings)
	}
	if got["has_more"] != false {
		t.Fatalf("has_more = %v", got["has_more"])
	}
	calls := f.callsMatching("GET", nutritionFoodSearchPathTest)
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	q := calls[0].Query
	if q.Get("searchExpression") != "Cheerios & Oats/é" || q.Get("start") != "0" || q.Get("limit") != "20" {
		t.Fatalf("query = %v", q)
	}
}

func TestSearchFoodsEmptyResponse(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionFoodSearchPathTest, nil)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "search_foods", map[string]any{"query": "xyzunknownfood"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "No foods found") {
		t.Fatalf("text = %q", text)
	}
}

func TestSearchFoodsError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionFoodSearchPathTest, status(503, map[string]any{"message": "Service Unavailable"}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "search_foods", map[string]any{"query": "Cheerios"})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	if !strings.Contains(text, "Error searching foods") {
		t.Fatalf("text = %q", text)
	}
}

func TestSearchFoodsRetainsGoodRowsWithNullOptionalData(t *testing.T) {
	badRows := []any{
		nil,
		map[string]any{"foodMetaData": nil},
		map[string]any{"foodMetaData": map[string]any{"foodId": "incomplete"}, "nutritionContents": nil},
	}
	for i, badRow := range badRows {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			response := nutritionSearchResponse()
			response["results"] = append([]any{badRow}, mutList(t, response["results"])...)
			f := newFakeGarmin(t)
			f.on("GET "+nutritionFoodSearchPathTest, response)
			cs := newSession(t, f, nil)

			text, isErr := callTool(t, cs, "search_foods", map[string]any{"query": "Cheerios"})
			if isErr {
				t.Fatalf("unexpected error: %q", text)
			}
			var got map[string]any
			decodeJSON(t, text, &got)
			var good map[string]any
			for _, item := range mutList(t, got["results"]) {
				row := mutMap(t, item)
				if row["name"] == "Cheerios" {
					good = row
				}
			}
			if good == nil {
				t.Fatalf("results = %v", got["results"])
			}
			if mutMap(t, mutList(t, good["servings"])[0])["calories"] != float64(100) {
				t.Fatalf("good = %v", good)
			}
		})
	}
}

func TestSearchFoodsHandlesNullResults(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionFoodSearchPathTest, map[string]any{"results": nil})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "search_foods", map[string]any{"query": "Cheerios"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	results, ok := got["results"].([]any)
	if !ok || len(results) != 0 {
		t.Fatalf("results = %v", got["results"])
	}
}

// --- structured write errors ---

func TestLogFoodGarminRejectionIsStructured(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionFoodLogsPathTest+"/quickAdd", status(401, map[string]any{"message": "expired"}))
	f.on("POST /diauth", map[string]any{"access_token": "refreshed"})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "log_food", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "12:00:00",
		"name":      "Test",
		"calories":  100,
		"carbs":     10,
		"protein":   5,
		"fat":       2,
	})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["status"] != "error" || got["tool"] != "log_food" || got["stage"] != "garmin_request" {
		t.Fatalf("result = %v", got)
	}
	if got["upstream_status"] != float64(401) || got["write_committed"] != false {
		t.Fatalf("result = %v", got)
	}
	if cid, _ := got["correlation_id"].(string); cid == "" {
		t.Fatalf("result = %v", got)
	}
}

func TestLogFood5xxAfterSendIsUnknown(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionFoodLogsPathTest+"/quickAdd", status(504, map[string]any{"message": "<html>Gateway Time-out</html>"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "log_food", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "12:00:00",
		"name":      "Test",
		"calories":  100,
		"carbs":     10,
		"protein":   5,
		"fat":       2,
	})
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["stage"] != "garmin_request" || got["upstream_status"] != float64(504) || got["write_committed"] != "unknown" {
		t.Fatalf("result = %v", got)
	}
	if calls := f.callsMatching("PUT", nutritionFoodLogsPathTest+"/quickAdd"); len(calls) != 1 {
		t.Fatalf("PUT calls = %d, want 1", len(calls))
	}
}

func TestLogFoodTimeoutAfterSendIsUnknown(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionFoodLogsPathTest+"/quickAdd", delay{wait: 500 * time.Millisecond, body: map[string]any{}})
	cs := newSession(t, f, map[string]string{"GARMIN_MCP_CALL_TIMEOUT": "0.2"})

	text, _ := callTool(t, cs, "log_food", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "12:00:00",
		"name":      "Test",
		"calories":  100,
		"carbs":     10,
		"protein":   5,
		"fat":       2,
	})
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["write_committed"] != "unknown" {
		t.Fatalf("result = %v", got)
	}
	if calls := f.callsMatching("PUT", nutritionFoodLogsPathTest+"/quickAdd"); len(calls) != 1 {
		t.Fatalf("PUT calls = %d, want 1", len(calls))
	}
}

func TestLogFoodMealLookupFailureIsNotCommitted(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionMealsPath, delay{wait: 500 * time.Millisecond, body: nutritionMeals()})
	cs := newSession(t, f, map[string]string{"GARMIN_MCP_CALL_TIMEOUT": "0.2"})

	text, _ := callTool(t, cs, "log_food", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "12:00:00",
		"name":      "Test",
		"calories":  100,
		"carbs":     10,
		"protein":   5,
		"fat":       2,
	})
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["write_committed"] != false {
		t.Fatalf("result = %v", got)
	}
	if calls := f.callsMatching("PUT", nutritionFoodLogsPathTest+"/quickAdd"); len(calls) != 0 {
		t.Fatalf("PUT calls = %d, want 0", len(calls))
	}
}

func TestLogCustomFoodErrorIsStructured(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionFoodLogsPathTest, status(400, map[string]any{"message": "Required Parameter meal id is null"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "log_custom_food", map[string]any{
		"meal_date":  nutritionMealDate,
		"meal_time":  "12:00:00",
		"food_id":    "abc",
		"serving_id": "s",
	})
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["tool"] != "log_custom_food" || got["upstream_status"] != float64(400) || got["upstream_body"] != "Required Parameter meal id is null" {
		t.Fatalf("result = %v", got)
	}
}

func TestDeleteFoodLogErrorIsStructured(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("DELETE "+nutritionFoodLogPath, status(400, map[string]any{"message": "no route"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "delete_food_log", map[string]any{
		"confirm":   true,
		"log_id":    "581f7dc8797f421f8d7eea83e5d2c939",
		"meal_date": nutritionMealDate,
	})
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["tool"] != "delete_food_log" || got["write_committed"] != false {
		t.Fatalf("result = %v", got)
	}
	if msg, _ := got["message"].(string); !strings.HasPrefix(msg, "Error deleting food log") {
		t.Fatalf("message = %v", got["message"])
	}
}

func TestLogFoodSuccessShapeUnchanged(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionFoodLogsPathTest+"/quickAdd", map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "log_food", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "12:00:00",
		"name":      "Test",
		"calories":  100,
		"carbs":     10,
		"protein":   5,
		"fat":       2,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if text != "Food logged successfully." {
		t.Fatalf("text = %q", text)
	}
}

func TestLogFoodNoAttributeErrorOnSuccess(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+nutritionMealsPath, nutritionMeals())
	f.on("PUT "+nutritionFoodLogsPathTest+"/quickAdd", map[string]any{"logId": "abc123", "status": "logged"})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "log_food", map[string]any{
		"meal_date": nutritionMealDate,
		"meal_time": "12:30:00",
		"name":      "Test Food",
		"calories":  100,
		"carbs":     10,
		"protein":   5,
		"fat":       3,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if strings.Contains(text, "Error") || !strings.Contains(text, "logId") {
		t.Fatalf("text = %q", text)
	}
}

func TestFoodLoggingRejectsInvalidMealDatetimeBeforeRequests(t *testing.T) {
	tools := []struct {
		name string
		args map[string]any
	}{
		{"log_food", map[string]any{"name": "Oats", "calories": 100, "carbs": 10, "protein": 5, "fat": 2}},
		{"log_custom_food", map[string]any{"food_id": "food001", "serving_id": "srv001"}},
		{"upsert_and_log", map[string]any{"food_name": "Oats", "calories": 100}},
	}
	datetimes := []struct {
		date string
		time string
	}{
		{"2024-01-15", "8:30:00"},
		{"2024-01-15", "noon"},
		{"2024-01-15", "25:00:00"},
		{"2024-01-15", "12:30 PM"},
		{"2024-01-15", "2024-01-15T08:30:00"},
		{"2024-02-30", "08:30:00"},
	}
	for _, tool := range tools {
		for _, dt := range datetimes {
			t.Run(tool.name+"_"+dt.date+"_"+dt.time, func(t *testing.T) {
				f := newFakeGarmin(t)
				f.on("GET "+nutritionMealsPath, nutritionMeals())
				cs := newSession(t, f, nil)

				args := map[string]any{"meal_date": dt.date, "meal_time": dt.time}
				for k, v := range tool.args {
					args[k] = v
				}
				text, _ := callTool(t, cs, tool.name, args)
				var got map[string]any
				decodeJSON(t, text, &got)
				if got["write_committed"] != false {
					t.Fatalf("result = %v", got)
				}
				msg, _ := got["message"].(string)
				if !strings.Contains(msg, "meal_date") && !strings.Contains(msg, "meal_time") {
					t.Fatalf("message = %v", got["message"])
				}
				if calls := f.recorded(); len(calls) != 0 {
					t.Fatalf("calls = %d, want 0", len(calls))
				}
			})
		}
	}
}

func TestNutritionLibraryWritesReportCommitStatus(t *testing.T) {
	tools := []struct {
		name   string
		args   map[string]any
		method string
		path   string
	}{
		{
			"set_nutrition_daily_settings",
			map[string]any{"date": nutritionMealDate, "calorie_goal": 1800},
			"PUT", nutritionSettingsPath,
		},
		{
			"create_custom_food",
			map[string]any{"food_name": "Oats", "calories": 100},
			"PUT", nutritionCustomFoodPathTest,
		},
		{
			"update_custom_food",
			map[string]any{"food_id": "food001", "serving_id": "srv001", "food_name": "Greek Yogurt", "calories": 100},
			"PUT", nutritionCustomFoodPathTest,
		},
		{
			"delete_custom_food",
			map[string]any{"food_id": "food001", "confirm": true},
			"DELETE", nutritionCustomFoodPathTest + "/food001",
		},
	}
	errorKinds := []string{"gateway", "read_timeout", "rejection", "parse"}
	for _, tool := range tools {
		for _, errorKind := range errorKinds {
			t.Run(tool.name+"_"+errorKind, func(t *testing.T) {
				f := newFakeGarmin(t)
				if tool.name == "set_nutrition_daily_settings" {
					f.on("GET "+nutritionSettingsPath, nutritionSettings())
				}
				if tool.name == "update_custom_food" {
					f.on("GET "+nutritionCustomFoodPathTest, nutritionCustomFoods())
				}
				switch errorKind {
				case "gateway":
					f.on(tool.method+" "+tool.path, status(504, map[string]any{"message": "Gateway Time-out"}))
				case "rejection":
					f.on(tool.method+" "+tool.path, status(400, map[string]any{"message": "rejected"}))
				case "parse":
					f.on(tool.method+" "+tool.path, rawBody([]byte("invalid JSON")))
				case "read_timeout":
					f.on(tool.method+" "+tool.path, delay{wait: 500 * time.Millisecond, body: map[string]any{}})
				}
				env := map[string]string(nil)
				if errorKind == "read_timeout" {
					env = map[string]string{"GARMIN_MCP_CALL_TIMEOUT": "0.2"}
				}
				cs := newSession(t, f, env)

				text, _ := callTool(t, cs, tool.name, tool.args)
				var got map[string]any
				decodeJSON(t, text, &got)
				if got["tool"] != tool.name {
					t.Fatalf("result = %v", got)
				}
				var expected any = "unknown"
				if errorKind == "parse" {
					expected = true
				} else if errorKind == "rejection" {
					expected = false
				}
				if got["write_committed"] != expected {
					t.Fatalf("write_committed = %v, want %v", got["write_committed"], expected)
				}
				wantStage := "garmin_request"
				if errorKind == "parse" {
					wantStage = "response_parse"
				}
				if got["stage"] != wantStage {
					t.Fatalf("stage = %v, want %v", got["stage"], wantStage)
				}
				if calls := f.callsMatching(tool.method, tool.path); len(calls) != 1 {
					t.Fatalf("write calls = %d, want 1", len(calls))
				}
				if expected == "unknown" {
					if hint, _ := got["hint"].(string); !strings.Contains(hint, "before retrying") {
						t.Fatalf("hint = %v", got["hint"])
					}
				}
			})
		}
	}
}

// --- weight-goal target date helper ---

func TestValidStoredTargetIsLeftUntouched(t *testing.T) {
	newTarget, errMsg := resolveWeightGoalTargetDate("2030-01-01", "2024-02-01", nil)
	if newTarget != "" || errMsg != "" {
		t.Fatalf("target = %q, err = %q", newTarget, errMsg)
	}
}

func TestAbsentStoredTargetIsLeftUntouched(t *testing.T) {
	for _, stored := range []any{nil, ""} {
		newTarget, errMsg := resolveWeightGoalTargetDate(stored, "2024-02-01", nil)
		if newTarget != "" || errMsg != "" {
			t.Fatalf("stored = %v: target = %q, err = %q", stored, newTarget, errMsg)
		}
	}
}

func TestPassedTargetIsReportedNotSilentlyChanged(t *testing.T) {
	newTarget, errMsg := resolveWeightGoalTargetDate("2024-03-01", "2024-07-15", nil)
	if newTarget != "" || errMsg == "" {
		t.Fatalf("target = %q, err = %q", newTarget, errMsg)
	}
	if !strings.Contains(errMsg, "targetDate") || !strings.Contains(errMsg, "2024-03-01") || !strings.Contains(errMsg, "2024-07-15") || !strings.Contains(errMsg, "target_date") {
		t.Fatalf("err = %q", errMsg)
	}
}

func TestTargetEqualToDateIsRejected(t *testing.T) {
	_, errMsg := resolveWeightGoalTargetDate("2024-07-15", "2024-07-15", nil)
	if errMsg == "" {
		t.Fatal("expected error")
	}
}

func TestExplicitTargetDateOverridesAPassedOne(t *testing.T) {
	newTarget, errMsg := resolveWeightGoalTargetDate("2024-03-01", "2024-07-15", nutritionStringPtr("2024-12-31"))
	if newTarget != "2024-12-31" || errMsg != "" {
		t.Fatalf("target = %q, err = %q", newTarget, errMsg)
	}
}

func TestExplicitTargetDateMustBeAfterDate(t *testing.T) {
	for _, candidate := range []string{"2024-07-15", "2024-07-14"} {
		newTarget, errMsg := resolveWeightGoalTargetDate("2024-03-01", "2024-07-15", nutritionStringPtr(candidate))
		if newTarget != "" || !strings.Contains(errMsg, "must be after") {
			t.Fatalf("candidate = %q: target = %q, err = %q", candidate, newTarget, errMsg)
		}
	}
}

func TestExplicitTargetDateMustBeISO(t *testing.T) {
	for _, candidate := range []string{"31-12-2026", "not-a-date", "2024-13-01"} {
		newTarget, errMsg := resolveWeightGoalTargetDate("2024-03-01", "2024-07-15", nutritionStringPtr(candidate))
		if newTarget != "" || !strings.Contains(errMsg, "YYYY-MM-DD") {
			t.Fatalf("candidate = %q: target = %q, err = %q", candidate, newTarget, errMsg)
		}
	}
}
