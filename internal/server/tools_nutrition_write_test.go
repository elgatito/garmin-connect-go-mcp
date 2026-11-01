package server

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Ported from tests/integration/test_nutrition_tools.py (write tools and the structured write errors).

const (
	mealsRoute    = "GET /nutrition-service/meals/2024-01-15"
	logsRoute     = "PUT /nutrition-service/food/logs"
	quickRoute    = "PUT /nutrition-service/food/logs/quickAdd"
	putFoodRoute  = "PUT /nutrition-service/customFood"
	settingsRoute = "/nutrition-service/settings/2024-01-15"
)

func mockMeals() map[string]any {
	return map[string]any{"meals": []any{
		map[string]any{"mealId": 20249, "mealName": "BREAKFAST", "startTime": "06:00:00", "endTime": "09:00:00"},
		map[string]any{"mealId": 20250, "mealName": "LUNCH", "startTime": "11:00:00", "endTime": "14:00:00"},
		map[string]any{"mealId": 20251, "mealName": "DINNER", "startTime": "18:00:00", "endTime": "21:00:00"},
		map[string]any{"mealId": 20252, "mealName": "SNACKS"},
	}}
}

func mockCustomFoods() map[string]any {
	return map[string]any{"customFoods": []any{map[string]any{
		"foodMetaData":      map[string]any{"foodId": "food001", "foodName": "Greek Yogurt"},
		"nutritionContents": []any{map[string]any{"servingId": "srv001", "calories": "100"}},
	}}}
}

func storedCustomFood() map[string]any {
	return map[string]any{
		"foodMetaData":      map[string]any{"foodId": "abc123", "foodName": "Simple Food"},
		"nutritionContents": []any{map[string]any{"servingId": "srv456", "servingUnit": "G", "numberOfUnits": 100, "calories": 100}},
	}
}

func currentSettings() map[string]any {
	return map[string]any{
		"weightChangeType": "LOSS", "targetDate": "2024-06-01", "weightChangeRate": 0.25, "activeDailyCalories": 400,
		"userDefinedActiveCalories": false, "calorieGoal": 2000, "macroGoals": map[string]any{"carbs": 250, "fat": 65, "protein": 120},
		"autoCalorieAdjustment": false, "startingWeight": 80, "targetWeightGoal": 75, "nutritionStatus": "ACTIVE",
	}
}

// timeoutEnv bounds a tool call at 0.3s so a handler that never answers reads as a read timeout.
var timeoutEnv = map[string]string{"GARMIN_MCP_CALL_TIMEOUT": "0.3"}

func blockUntilCancelled(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }

// bodyOf decodes the JSON body of a recorded request.
func bodyOf(t *testing.T, c recordedCall) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(c.Body, &m); err != nil {
		t.Fatalf("body of %s %s is not a JSON object: %q", c.Method, c.Path, c.Body)
	}
	return m
}

// theCall returns the only request to route.
func theCall(t *testing.T, f *fakeGarmin, route string) recordedCall {
	t.Helper()
	reqs := f.requests(route)
	if len(reqs) != 1 {
		t.Fatalf("%d requests to %s, want 1", len(reqs), route)
	}
	return reqs[0]
}

func nutritionOf(t *testing.T, c recordedCall) map[string]any {
	t.Helper()
	return asMap(asList(bodyOf(t, c)["nutritionContents"])[0])
}

// failureOf asserts a structured write error (tool error with a JSON payload) and returns it.
func failureOf(t *testing.T, text string, isErr bool) map[string]any {
	t.Helper()
	d := resultMap(t, text)
	if !isErr || d["status"] != "error" {
		t.Fatalf("not a structured error (isErr=%v): %s", isErr, text)
	}
	return d
}

func mealsFake(t *testing.T) *fakeGarmin {
	t.Helper()
	f := newFakeGarmin(t)
	f.on(mealsRoute, mockMeals())
	f.on(logsRoute, map[string]any{})
	f.on(quickRoute, map[string]any{})
	return f
}

// customFoodCatalog is Python's _custom_food_catalog: name-substring search with start/limit paging.
func customFoodCatalog(foods ...any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		term := strings.ToLower(q.Get("searchExpression"))
		start, _ := strconv.Atoi(q.Get("start"))
		limit, _ := strconv.Atoi(q.Get("limit"))
		hits := []any{}
		for _, f := range foods {
			if strings.Contains(strings.ToLower(pyStr(at(f, "foodMetaData", "foodName"))), term) {
				hits = append(hits, f)
			}
		}
		hits = hits[min(start, len(hits)):min(start+limit, len(hits))]
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"customFoods": hits})
	}
}

var logTimestampRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.000Z$`)

// ── create_custom_food ──────────────────────────────────────────────────────

func TestCreateCustomFood(t *testing.T) {
	f := newFakeGarmin(t)
	resp := map[string]any{"foodId": "abc123", "foodName": "Homemade Cookies", "servingId": "srv456"}
	f.on(putFoodRoute, resp)
	text := callOK(t, newSession(t, f, nil), "create_custom_food", map[string]any{
		"food_name": "Homemade Cookies", "calories": 150, "serving_unit": "G", "number_of_units": 30, "carbs": 20, "protein": 2, "fat": 7,
	})
	if !reflect.DeepEqual(decode(t, text), jsonValue(t, resp)) {
		t.Errorf("text = %s", text)
	}
	body := bodyOf(t, theCall(t, f, putFoodRoute))
	want := map[string]any{
		"foodMetaData": map[string]any{"foodName": "Homemade Cookies", "foodType": "GENERIC", "source": "GARMIN", "regionCode": "US", "languageCode": "en"},
		"nutritionContents": []any{map[string]any{
			"servingUnit": "G", "numberOfUnits": "30", "calories": "150", "carbs": "20", "protein": "2", "fat": "7",
		}},
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %v", body)
	}
}

func TestCreateCustomFoodMinimal(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(putFoodRoute, map[string]any{})
	text := callOK(t, newSession(t, f, nil), "create_custom_food", map[string]any{"food_name": "Simple Food", "calories": 100})
	if !strings.Contains(text, "Custom food created") {
		t.Errorf("text = %q", text)
	}
	nc := nutritionOf(t, theCall(t, f, putFoodRoute))
	for _, k := range []string{"carbs", "protein", "fat"} {
		if _, ok := nc[k]; ok {
			t.Errorf("%s present in %v", k, nc)
		}
	}
	// defaults: G / 100
	if nc["servingUnit"] != "G" || nc["numberOfUnits"] != "100" || nc["calories"] != "100" {
		t.Errorf("nc = %v", nc)
	}
}

func TestCreateCustomFoodError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(putFoodRoute, apiErr(500, "API error"))
	text, isErr := callTool(t, newSession(t, f, nil), "create_custom_food", map[string]any{"food_name": "Test", "calories": 100})
	if !isErr || !strings.Contains(text, "Error creating custom food") {
		t.Errorf("%v %s", isErr, text)
	}
}

// The Python mock raises an exception whose text is the bare message; the real client prefixes "API Error <status> - ".
func TestCreateCustomFoodForbiddenIsAnError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(putFoodRoute, apiErr(403, "localized or non-English message"))
	text, isErr := callTool(t, newSession(t, f, nil), "create_custom_food", map[string]any{"food_name": "Test", "calories": 100})
	d := failureOf(t, text, isErr)
	if d["message"] != "Error creating custom food: API Error 403 - localized or non-English message" || d["write_committed"] != false {
		t.Errorf("d = %v", d)
	}
}

func TestCreateCustomFoodForbiddenFallsBackToStatusInMessage(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(putFoodRoute, apiErr(403, "acceso denegado"))
	text, isErr := callTool(t, newSession(t, f, nil), "create_custom_food", map[string]any{"food_name": "Test", "calories": 100})
	d := failureOf(t, text, isErr)
	if d["message"] != "Error creating custom food: API Error 403 - acceso denegado" || d["upstream_status"] != 403.0 || d["write_committed"] != false {
		t.Errorf("d = %v", d)
	}
}

func TestCreateCustomFoodWithBrandAndMicros(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(putFoodRoute, map[string]any{"foodMetaData": map[string]any{"foodId": "x"}})
	callOK(t, newSession(t, f, nil), "create_custom_food", map[string]any{
		"food_name": "Branded Bar", "calories": 200, "brand_name": "ACME", "trans_fat": 0.5, "calcium": 130, "iron": 2, "vitamin_d": 2.5,
	})
	c := theCall(t, f, putFoodRoute)
	if at(bodyOf(t, c), "foodMetaData", "brandName") != "ACME" {
		t.Errorf("body = %v", bodyOf(t, c))
	}
	nc := nutritionOf(t, c)
	if nc["transFat"] != "0.5" || nc["calcium"] != "130" || nc["iron"] != "2" || nc["vitaminD"] != "2.5" {
		t.Errorf("nc = %v", nc)
	}
}

func TestCreateCustomFoodMinimalNoBrand(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(putFoodRoute, map[string]any{})
	callOK(t, newSession(t, f, nil), "create_custom_food", map[string]any{"food_name": "Plain Food", "calories": 50})
	c := theCall(t, f, putFoodRoute)
	if _, ok := asMap(bodyOf(t, c)["foodMetaData"])["brandName"]; ok {
		t.Error("brandName present")
	}
	nc := nutritionOf(t, c)
	for _, k := range []string{"transFat", "calcium", "iron", "vitaminD"} {
		if _, ok := nc[k]; ok {
			t.Errorf("%s present", k)
		}
	}
}

// Zero is a supplied value, not "not given" (Python checks `is not None`).
func TestCreateCustomFoodSendsExplicitZeroAndAllOptionalNutrients(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(putFoodRoute, map[string]any{})
	callOK(t, newSession(t, f, nil), "create_custom_food", map[string]any{
		"food_name": "Zero", "calories": 0, "number_of_units": 0, "carbs": 0, "protein": 1, "fat": 2, "fiber": 3, "sugar": 4,
		"saturated_fat": 5, "sodium": 6, "cholesterol": 7, "potassium": 8, "trans_fat": 9, "calcium": 10, "iron": 11, "vitamin_d": 12,
	})
	want := map[string]any{
		"servingUnit": "G", "numberOfUnits": "0", "calories": "0", "carbs": "0", "protein": "1", "fat": "2", "fiber": "3", "sugar": "4",
		"saturatedFat": "5", "sodium": "6", "cholesterol": "7", "potassium": "8", "transFat": "9", "calcium": "10", "iron": "11", "vitaminD": "12",
	}
	if nc := nutritionOf(t, theCall(t, f, putFoodRoute)); !reflect.DeepEqual(nc, want) {
		t.Errorf("nc = %v", nc)
	}
}

// ── update_custom_food ──────────────────────────────────────────────────────

func updateFake(t *testing.T, stored ...any) *fakeGarmin {
	t.Helper()
	f := newFakeGarmin(t)
	f.on(customFoodRoute, map[string]any{"customFoods": stored})
	f.on(putFoodRoute, map[string]any{})
	return f
}

func updateArgs(extra map[string]any) map[string]any {
	a := map[string]any{"food_id": "abc123", "serving_id": "srv456", "food_name": "Simple Food", "calories": 100}
	for k, v := range extra {
		a[k] = v
	}
	return a
}

func TestUpdateCustomFood(t *testing.T) {
	f := updateFake(t, storedCustomFood())
	resp := map[string]any{"foodId": "abc123", "foodName": "Homemade Cookies Updated", "servingId": "srv456"}
	f.on(putFoodRoute, resp)
	text := callOK(t, newSession(t, f, nil), "update_custom_food", map[string]any{
		"food_id": "abc123", "serving_id": "srv456", "food_name": "Homemade Cookies Updated", "calories": 160,
		"serving_unit": "G", "number_of_units": 30, "carbs": 22, "protein": 3, "fat": 8,
	})
	if !reflect.DeepEqual(decode(t, text), jsonValue(t, resp)) {
		t.Errorf("text = %s", text)
	}
	body := bodyOf(t, theCall(t, f, putFoodRoute))
	wantMeta := map[string]any{"foodId": "abc123", "foodName": "Homemade Cookies Updated", "foodType": "GENERIC", "source": "GARMIN", "regionCode": "US", "languageCode": "en"}
	wantNC := map[string]any{"servingId": "srv456", "servingUnit": "G", "numberOfUnits": "30", "calories": "160", "carbs": "22", "protein": "3", "fat": "8"}
	if !reflect.DeepEqual(body["foodMetaData"], wantMeta) || !reflect.DeepEqual(asList(body["nutritionContents"]), []any{wantNC}) {
		t.Errorf("body = %v", body)
	}
	wantQuery(t, theCall(t, f, customFoodRoute).Query, map[string]string{"searchExpression": "Homemade Cookies Updated", "start": "0", "limit": "20", "includeContent": "true"})
}

func TestUpdateCustomFoodHandlesNullFoodMetadata(t *testing.T) {
	f := updateFake(t, map[string]any{"foodMetaData": nil, "nutritionContents": nil}, storedCustomFood())
	text := callOK(t, newSession(t, f, nil), "update_custom_food", updateArgs(nil))
	if strings.Contains(text, "Error") || len(f.requests(putFoodRoute)) != 1 {
		t.Errorf("text = %q", text)
	}
}

func TestUpdateCustomFood204(t *testing.T) {
	f := updateFake(t, storedCustomFood())
	f.on(putFoodRoute, status(204, rawBody("")))
	text := callOK(t, newSession(t, f, nil), "update_custom_food", updateArgs(nil))
	if text != "Custom food updated (no response data returned)." {
		t.Errorf("text = %q", text)
	}
}

func TestUpdateCustomFoodError(t *testing.T) {
	f := updateFake(t, storedCustomFood())
	f.on(putFoodRoute, apiErr(500, "API error"))
	text, isErr := callTool(t, newSession(t, f, nil), "update_custom_food", updateArgs(map[string]any{"food_name": "Test"}))
	if !isErr || !strings.Contains(text, "Error updating custom food") {
		t.Errorf("%v %s", isErr, text)
	}
}

func TestUpdateCustomFoodForbiddenIsAnError(t *testing.T) {
	f := updateFake(t, storedCustomFood())
	f.on(putFoodRoute, apiErr(403, "localized or non-English message"))
	text, isErr := callTool(t, newSession(t, f, nil), "update_custom_food", updateArgs(map[string]any{"food_name": "Test"}))
	d := failureOf(t, text, isErr)
	if d["message"] != "Error updating custom food: API Error 403 - localized or non-English message" || d["write_committed"] != false {
		t.Errorf("d = %v", d)
	}
}

func TestUpdateCustomFoodWithBrandAndMicros(t *testing.T) {
	f := updateFake(t, storedCustomFood())
	f.on(putFoodRoute, map[string]any{"foodMetaData": map[string]any{"foodId": "abc123"}})
	callOK(t, newSession(t, f, nil), "update_custom_food", updateArgs(map[string]any{
		"food_name": "Branded Food", "calories": 300, "brand_name": "BigCo", "trans_fat": 1.0, "calcium": 260, "iron": 4, "vitamin_d": 5,
	}))
	c := theCall(t, f, putFoodRoute)
	if at(bodyOf(t, c), "foodMetaData", "brandName") != "BigCo" {
		t.Errorf("body = %v", bodyOf(t, c))
	}
	nc := nutritionOf(t, c)
	if nc["transFat"] != "1" || nc["calcium"] != "260" || nc["iron"] != "4" || nc["vitaminD"] != "5" {
		t.Errorf("nc = %v", nc)
	}
}

func TestUpdateCustomFoodPreservesBrandAndMicros(t *testing.T) {
	// The real API returns nutritionContents values as numbers (not strings).
	f := updateFake(t, map[string]any{
		"foodMetaData": map[string]any{"foodId": "abc123", "foodName": "Existing Food", "brandName": "OriginalBrand"},
		"nutritionContents": []any{map[string]any{
			"servingId": "srv456", "calories": 200, "transFat": 0.5, "calcium": 100, "iron": 3, "vitaminD": 2,
		}},
	})
	callOK(t, newSession(t, f, nil), "update_custom_food", updateArgs(map[string]any{"food_name": "Existing Food", "calories": 200}))
	c := theCall(t, f, putFoodRoute)
	if at(bodyOf(t, c), "foodMetaData", "brandName") != "OriginalBrand" {
		t.Errorf("body = %v", bodyOf(t, c))
	}
	nc := nutritionOf(t, c)
	if nc["transFat"] != "0.5" || nc["calcium"] != "100" || nc["iron"] != "3" || nc["vitaminD"] != "2" {
		t.Errorf("nc = %v", nc)
	}
}

func TestUpdateCustomFoodBrandOverridesExisting(t *testing.T) {
	f := updateFake(t, map[string]any{
		"foodMetaData":      map[string]any{"foodId": "abc123", "foodName": "Food", "brandName": "OldBrand"},
		"nutritionContents": []any{map[string]any{"servingId": "srv456", "calories": 100}},
	})
	callOK(t, newSession(t, f, nil), "update_custom_food", updateArgs(map[string]any{"food_name": "Food", "brand_name": "NewBrand"}))
	if at(bodyOf(t, theCall(t, f, putFoodRoute)), "foodMetaData", "brandName") != "NewBrand" {
		t.Error("brand not replaced")
	}
}

func beerFood() map[string]any {
	return map[string]any{
		"foodMetaData":      map[string]any{"foodId": "abc123", "foodName": "NA Beer"},
		"nutritionContents": []any{map[string]any{"servingId": "srv456", "servingUnit": "ML", "numberOfUnits": 355, "calories": 90}},
	}
}

func TestUpdateCustomFoodPreservesServingBasis(t *testing.T) {
	f := updateFake(t, beerFood())
	callOK(t, newSession(t, f, nil), "update_custom_food", updateArgs(map[string]any{"food_name": "NA Beer", "calories": 90}))
	if nc := nutritionOf(t, theCall(t, f, putFoodRoute)); nc["servingUnit"] != "ML" || nc["numberOfUnits"] != "355" {
		t.Errorf("nc = %v", nc)
	}
}

func TestUpdateCustomFoodOverridesServingBasis(t *testing.T) {
	f := updateFake(t, beerFood())
	callOK(t, newSession(t, f, nil), "update_custom_food", updateArgs(map[string]any{"food_name": "NA Beer", "calories": 90, "serving_unit": "OZ", "number_of_units": 12}))
	if nc := nutritionOf(t, theCall(t, f, putFoodRoute)); nc["servingUnit"] != "OZ" || nc["numberOfUnits"] != "12" {
		t.Errorf("nc = %v", nc)
	}
}

func TestUpdateCustomFoodPartialServingOverride(t *testing.T) {
	f := updateFake(t, beerFood())
	callOK(t, newSession(t, f, nil), "update_custom_food", updateArgs(map[string]any{"food_name": "NA Beer", "calories": 90, "number_of_units": 500}))
	if nc := nutritionOf(t, theCall(t, f, putFoodRoute)); nc["servingUnit"] != "ML" || nc["numberOfUnits"] != "500" {
		t.Errorf("nc = %v", nc)
	}
}

func TestUpdateCustomFoodRefusesWithoutExistingRecord(t *testing.T) {
	f := updateFake(t) // empty catalog
	text := callOK(t, newSession(t, f, nil), "update_custom_food", updateArgs(nil))
	want := "Error updating custom food: could not read the current record for food_id abc123, so omitted fields cannot be preserved. " +
		"Check the food_id with get_custom_foods, or pass serving_unit and number_of_units (plus every nutrient and brand to keep) to replace the record in full."
	if text != want {
		t.Errorf("text = %q", text)
	}
	if n := len(f.requests(putFoodRoute)); n != 0 {
		t.Errorf("%d PUTs", n)
	}
	// Searched by name, then fell back to the unfiltered list.
	var searches []string
	for _, c := range f.requests(customFoodRoute) {
		searches = append(searches, c.Query.Get("searchExpression"))
	}
	if !reflect.DeepEqual(searches, []string{"Simple Food", ""}) {
		t.Errorf("searches = %q", searches)
	}
}

func TestUpdateCustomFoodRefusesWhenLookupFails(t *testing.T) {
	f := updateFake(t)
	f.on(customFoodRoute, apiErr(500, "lookup failed"))
	text := callOK(t, newSession(t, f, nil), "update_custom_food", updateArgs(nil))
	if !strings.Contains(text, "could not read the current record") || len(f.requests(putFoodRoute)) != 0 {
		t.Errorf("text = %q", text)
	}
}

func TestUpdateCustomFoodFullReplaceWithoutExistingRecord(t *testing.T) {
	f := updateFake(t)
	text := callOK(t, newSession(t, f, nil), "update_custom_food", updateArgs(map[string]any{"serving_unit": "ML", "number_of_units": 250}))
	if !strings.Contains(text, "Custom food updated") {
		t.Errorf("text = %q", text)
	}
	if nc := nutritionOf(t, theCall(t, f, putFoodRoute)); nc["servingUnit"] != "ML" || nc["numberOfUnits"] != "250" {
		t.Errorf("nc = %v", nc)
	}
}

func TestUpdateCustomFoodRenamePreservesOmittedFields(t *testing.T) {
	stored := map[string]any{
		"foodMetaData": map[string]any{"foodId": "abc123", "foodName": "Protein Shake", "brandName": "Acme"},
		"nutritionContents": []any{map[string]any{
			"servingId": "srv456", "servingUnit": "ML", "numberOfUnits": 355, "calories": 160, "protein": 30, "carbs": 5, "sodium": 200,
		}},
	}
	f := updateFake(t)
	f.on(customFoodRoute, customFoodCatalog(stored))
	text := callOK(t, newSession(t, f, nil), "update_custom_food",
		map[string]any{"food_id": "abc123", "serving_id": "srv456", "food_name": "Vanilla Whey", "calories": 160})
	if !strings.Contains(text, "Custom food updated") {
		t.Errorf("text = %q", text)
	}
	c := theCall(t, f, putFoodRoute)
	if at(bodyOf(t, c), "foodMetaData", "foodName") != "Vanilla Whey" || at(bodyOf(t, c), "foodMetaData", "brandName") != "Acme" {
		t.Errorf("body = %v", bodyOf(t, c))
	}
	nc := nutritionOf(t, c)
	if nc["servingUnit"] != "ML" || nc["numberOfUnits"] != "355" || nc["protein"] != "30" || nc["carbs"] != "5" || nc["sodium"] != "200" {
		t.Errorf("nc = %v", nc)
	}
}

func TestUpdateCustomFoodFindsRecordBeyondFirstPage(t *testing.T) {
	var foods []any
	for i := 0; i < 25; i++ {
		foods = append(foods, map[string]any{
			"foodMetaData":      map[string]any{"foodId": "other" + strconv.Itoa(i), "foodName": "Other " + strconv.Itoa(i)},
			"nutritionContents": []any{map[string]any{"servingId": "s" + strconv.Itoa(i), "calories": 1}},
		})
	}
	foods = append(foods, map[string]any{
		"foodMetaData":      map[string]any{"foodId": "abc123", "foodName": "Old Name", "brandName": "Acme"},
		"nutritionContents": []any{map[string]any{"servingId": "srv456", "calories": 90, "fat": 3}},
	})
	f := updateFake(t)
	f.on(customFoodRoute, customFoodCatalog(foods...))
	callOK(t, newSession(t, f, nil), "update_custom_food", map[string]any{"food_id": "abc123", "serving_id": "srv456", "food_name": "New Name", "calories": 90})
	c := theCall(t, f, putFoodRoute)
	if at(bodyOf(t, c), "foodMetaData", "brandName") != "Acme" || nutritionOf(t, c)["fat"] != "3" {
		t.Errorf("body = %v", bodyOf(t, c))
	}
}

// ── log_food ────────────────────────────────────────────────────────────────

func logFoodArgs(extra map[string]any) map[string]any {
	a := map[string]any{"meal_date": "2024-01-15", "meal_time": "12:00:00", "name": "Test", "calories": 100, "carbs": 10, "protein": 5, "fat": 2}
	for k, v := range extra {
		a[k] = v
	}
	return a
}

func TestLogFood(t *testing.T) {
	f := mealsFake(t)
	f.on(quickRoute, map[string]any{"status": "ok"})
	text := callOK(t, newSession(t, f, nil), "log_food", map[string]any{
		"meal_date": "2024-01-15", "meal_time": "12:30:00", "name": "Chicken Breast", "calories": 200, "carbs": 3, "protein": 40, "fat": 4,
	})
	if !reflect.DeepEqual(decode(t, text), map[string]any{"status": "ok"}) {
		t.Errorf("text = %s", text)
	}
	if n := len(nutritionRequests(f)); n != 2 {
		t.Errorf("%d nutrition requests", n)
	}
	theCall(t, f, mealsRoute)
	body := bodyOf(t, theCall(t, f, quickRoute))
	items := asList(body["quickAddItems"])
	if body["mealDate"] != "2024-01-15" || len(items) != 1 || len(body) != 2 {
		t.Fatalf("body = %v", body)
	}
	item := asMap(items[0])
	ts, _ := item["logTimestamp"].(string)
	if !logTimestampRe.MatchString(ts) {
		t.Errorf("logTimestamp = %q", ts)
	}
	delete(item, "logTimestamp")
	want := map[string]any{
		"name": "Chicken Breast", "logId": nil, "logSource": "GCW", "logCategory": "QUICK_ADD", "mealTime": "12:30:00",
		"mealId": 20250.0, "action": "ADD", "calories": "200", "carbs": "3", "protein": "40", "fat": "4",
	}
	if !reflect.DeepEqual(item, want) {
		t.Errorf("item = %v", item)
	}
}

func TestLogFoodFallsBackToSnacks(t *testing.T) {
	f := mealsFake(t)
	text := callOK(t, newSession(t, f, nil), "log_food", logFoodArgs(map[string]any{"meal_time": "10:00:00", "name": "Oats"}))
	if !strings.Contains(text, "Food logged successfully") {
		t.Errorf("text = %q", text)
	}
	item := asMap(asList(bodyOf(t, theCall(t, f, quickRoute))["quickAddItems"])[0])
	if item["mealId"] != 20252.0 || item["mealTime"] != "10:00:00" {
		t.Errorf("item = %v", item)
	}
}

func TestLogFoodError(t *testing.T) {
	f := mealsFake(t)
	f.on(quickRoute, apiErr(500, "API error"))
	text, isErr := callTool(t, newSession(t, f, nil), "log_food", logFoodArgs(nil))
	if !isErr || !strings.Contains(text, "Error logging food") {
		t.Errorf("%v %s", isErr, text)
	}
}

func TestLogFoodForbiddenIsAnError(t *testing.T) {
	f := mealsFake(t)
	f.on(quickRoute, apiErr(403, "localized or non-English message"))
	text, isErr := callTool(t, newSession(t, f, nil), "log_food", logFoodArgs(nil))
	d := failureOf(t, text, isErr)
	if d["upstream_status"] != 403.0 || d["upstream_body"] != "localized or non-English message" || d["write_committed"] != false {
		t.Errorf("d = %v", d)
	}
}

func TestLogFoodWithoutMealWindowsIsUnavailable(t *testing.T) {
	f := mealsFake(t)
	f.on(mealsRoute, map[string]any{"meals": []any{}})
	text, isErr := callTool(t, newSession(t, f, nil), "log_food", logFoodArgs(nil))
	if isErr || text != "No nutrition meal windows found for 2024-01-15; cannot log food at '12:00:00'." {
		t.Errorf("%v %q", isErr, text)
	}
	if n := len(f.requests(quickRoute)); n != 0 {
		t.Errorf("%d PUTs", n)
	}
}

// ── log_custom_food ─────────────────────────────────────────────────────────

func logCustomArgs(extra map[string]any) map[string]any {
	a := map[string]any{"meal_date": "2024-01-15", "meal_time": "12:00:00", "food_id": "abc123", "serving_id": "srv456"}
	for k, v := range extra {
		a[k] = v
	}
	return a
}

func TestLogCustomFood(t *testing.T) {
	f := mealsFake(t)
	f.on(logsRoute, map[string]any{"status": "ok"})
	text := callOK(t, newSession(t, f, nil), "log_custom_food", logCustomArgs(map[string]any{"meal_time": "12:30:00", "serving_qty": 3}))
	if !reflect.DeepEqual(decode(t, text), map[string]any{"status": "ok"}) {
		t.Errorf("text = %s", text)
	}
	theCall(t, f, mealsRoute)
	body := bodyOf(t, theCall(t, f, logsRoute))
	items := asList(body["foodLogItems"])
	if body["mealDate"] != "2024-01-15" || len(items) != 1 || len(body) != 2 {
		t.Fatalf("body = %v", body)
	}
	item := asMap(items[0])
	ts, _ := item["logTimestamp"].(string)
	if !logTimestampRe.MatchString(ts) {
		t.Errorf("logTimestamp = %q", ts)
	}
	delete(item, "logTimestamp")
	want := map[string]any{
		"logSource": "GCW", "logCategory": "REGULAR_LOG", "mealTime": "12:30:00", "action": "ADD", "mealId": 20250.0, "foodId": "abc123",
		"servingId": "srv456", "source": "GARMIN", "regionCode": "US", "languageCode": "en", "servingQty": 3.0,
	}
	if !reflect.DeepEqual(item, want) {
		t.Errorf("item = %v", item)
	}
}

func TestLogCustomFoodFallsBackToSnacks(t *testing.T) {
	f := mealsFake(t)
	text := callOK(t, newSession(t, f, nil), "log_custom_food", logCustomArgs(map[string]any{"meal_time": "10:00:00"}))
	if !strings.Contains(text, "Food logged successfully") {
		t.Errorf("text = %q", text)
	}
	item := asMap(asList(bodyOf(t, theCall(t, f, logsRoute))["foodLogItems"])[0])
	if item["mealId"] != 20252.0 || item["servingQty"] != 1.0 {
		t.Errorf("item = %v", item)
	}
}

func TestLogCustomFoodError(t *testing.T) {
	f := mealsFake(t)
	f.on(logsRoute, apiErr(500, "API error"))
	text, isErr := callTool(t, newSession(t, f, nil), "log_custom_food", logCustomArgs(nil))
	if !isErr || !strings.Contains(text, "Error logging food") {
		t.Errorf("%v %s", isErr, text)
	}
}

func TestLogCustomFoodForbiddenIsAnError(t *testing.T) {
	f := mealsFake(t)
	f.on(logsRoute, apiErr(403, "localized or non-English message"))
	text, isErr := callTool(t, newSession(t, f, nil), "log_custom_food", logCustomArgs(nil))
	d := failureOf(t, text, isErr)
	if d["upstream_status"] != 403.0 || d["upstream_body"] != "localized or non-English message" || d["write_committed"] != false {
		t.Errorf("d = %v", d)
	}
}

func TestLogCustomFoodUsesFatsecretSource(t *testing.T) {
	f := mealsFake(t)
	f.on(logsRoute, map[string]any{"success": true})
	callOK(t, newSession(t, f, nil), "log_custom_food", logCustomArgs(map[string]any{"meal_time": "08:00:00", "food_id": "4132350", "serving_id": "srv001", "source": "FATSECRET"}))
	if item := asMap(asList(bodyOf(t, theCall(t, f, logsRoute))["foodLogItems"])[0]); item["source"] != "FATSECRET" {
		t.Errorf("item = %v", item)
	}
}

func TestLogCustomFoodDefaultsToGarminSource(t *testing.T) {
	f := mealsFake(t)
	f.on(logsRoute, map[string]any{"success": true})
	callOK(t, newSession(t, f, nil), "log_custom_food", logCustomArgs(map[string]any{"meal_time": "08:00:00", "food_id": "abc-uuid-123", "serving_id": "srv002"}))
	if item := asMap(asList(bodyOf(t, theCall(t, f, logsRoute))["foodLogItems"])[0]); item["source"] != "GARMIN" {
		t.Errorf("item = %v", item)
	}
}

func TestLogCustomFoodWithoutMealWindowsIsUnavailable(t *testing.T) {
	f := mealsFake(t)
	f.on(mealsRoute, map[string]any{"meals": []any{}})
	text, isErr := callTool(t, newSession(t, f, nil), "log_custom_food", logCustomArgs(nil))
	if isErr || text != "No nutrition meal windows found for 2024-01-15; cannot log custom food at '12:00:00'." {
		t.Errorf("%v %q", isErr, text)
	}
}

// ── delete_custom_food / delete_food_log ────────────────────────────────────

const hexFoodID = "08b27145e29d41479e36d8d3788fcccf"

func TestDeleteCustomFood(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("DELETE /nutrition-service/customFood/"+hexFoodID, map[string]any{})
	text := callOK(t, newSession(t, f, nil), "delete_custom_food", map[string]any{"confirm": true, "food_id": hexFoodID})
	want := map[string]any{"status": "success", "food_id": hexFoodID, "message": "Custom food " + hexFoodID + " deleted successfully."}
	if !reflect.DeepEqual(decode(t, text), want) {
		t.Errorf("text = %s", text)
	}
	if c := theCall(t, f, "DELETE /nutrition-service/customFood/"+hexFoodID); len(c.Body) != 0 {
		t.Errorf("DELETE body = %q", c.Body)
	}
	if n := len(f.requests("GET /nutrition-service/customFood")); n != 0 {
		t.Errorf("%d lookups on confirm", n)
	}
}

func TestDeleteCustomFoodError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("DELETE /nutrition-service/customFood/"+hexFoodID, apiErr(500, "API error"))
	text, isErr := callTool(t, newSession(t, f, nil), "delete_custom_food", map[string]any{"confirm": true, "food_id": hexFoodID})
	if !isErr || !strings.Contains(text, "Error deleting custom food") {
		t.Errorf("%v %s", isErr, text)
	}
}

func TestDeleteFoodLog(t *testing.T) {
	const id = "581f7dc8797f421f8d7eea83e5d2c939"
	f := newFakeGarmin(t)
	f.on("DELETE /nutrition-service/food/logs/2024-01-15", map[string]any{})
	text := callOK(t, newSession(t, f, nil), "delete_food_log", map[string]any{"confirm": true, "log_id": id, "meal_date": "2024-01-15"})
	want := map[string]any{"status": "success", "log_id": id, "message": "Food log entry " + id + " deleted successfully."}
	if !reflect.DeepEqual(decode(t, text), want) {
		t.Errorf("text = %s", text)
	}
	if body := bodyOf(t, theCall(t, f, "DELETE /nutrition-service/food/logs/2024-01-15")); !reflect.DeepEqual(body, map[string]any{"logIds": []any{id}}) {
		t.Errorf("body = %v", body)
	}
}

func TestDeleteFoodLogError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("DELETE /nutrition-service/food/logs/2024-01-15", apiErr(500, "API error"))
	text, isErr := callTool(t, newSession(t, f, nil), "delete_food_log", map[string]any{"confirm": true, "log_id": "99001", "meal_date": "2024-01-15"})
	if !isErr || !strings.Contains(text, "Error deleting food log") {
		t.Errorf("%v %s", isErr, text)
	}
}

// The status comes from the structured error in Python; both Python variants (structured and text fallback)
// reach the same Go path because the client's error always carries the status.
func TestDeleteFoodLogNotFound(t *testing.T) {
	for _, msg := range []string{"localized or non-English message", "recurso inexistente"} {
		f := newFakeGarmin(t)
		f.on("DELETE /nutrition-service/food/logs/2024-01-15", apiErr(404, msg))
		text := callOK(t, newSession(t, f, nil), "delete_food_log", map[string]any{"confirm": true, "log_id": "-1", "meal_date": "2024-01-15"})
		want := map[string]any{"status": "not_found", "log_id": "-1", "message": "Food log entry -1 was not found."}
		if !reflect.DeepEqual(decode(t, text), want) {
			t.Errorf("%s: text = %s", msg, text)
		}
	}
}

func TestDeleteFoodLogNotFoundFallsBackToStatusInMessage(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("DELETE /nutrition-service/food/logs/2024-01-15", status(404, rawBody("API Error 404 - recurso inexistente")))
	text := callOK(t, newSession(t, f, nil), "delete_food_log", map[string]any{"confirm": true, "log_id": "-1", "meal_date": "2024-01-15"})
	want := map[string]any{"status": "not_found", "log_id": "-1", "message": "Food log entry -1 was not found."}
	if !reflect.DeepEqual(decode(t, text), want) {
		t.Errorf("text = %s", text)
	}
}

func TestDeleteFoodLogAcceptsHexUuid(t *testing.T) {
	const hexLogID = "581f7dc8797f421f8d7eea83e5d2c939"
	f := newFakeGarmin(t)
	f.on("DELETE /nutrition-service/food/logs/2024-01-15", map[string]any{})
	text := callOK(t, newSession(t, f, nil), "delete_food_log", map[string]any{"confirm": true, "log_id": hexLogID, "meal_date": "2024-01-15"})
	if !strings.Contains(text, "success") || !strings.Contains(text, hexLogID) {
		t.Errorf("text = %s", text)
	}
	if body := bodyOf(t, theCall(t, f, "DELETE /nutrition-service/food/logs/2024-01-15")); !reflect.DeepEqual(body, map[string]any{"logIds": []any{hexLogID}}) {
		t.Errorf("body = %v", body)
	}
}

func TestDeleteCustomFoodPreviewsWithoutDeleting(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(customFoodRoute, mockCustomFoods())
	text := callOK(t, newSession(t, f, nil), "delete_custom_food", map[string]any{"food_id": "food001"})
	d := resultMap(t, text)
	if d["status"] != "needs_confirmation" || at(d, "food", "foodMetaData", "foodName") != "Greek Yogurt" || d["food_id"] != "food001" ||
		d["message"] != "Review this food, then call again with confirm=true to permanently delete it." {
		t.Errorf("d = %v", d)
	}
	noRequests(t, f, "DELETE")
}

func TestDeleteFoodLogPreviewsMatchingNestedEntry(t *testing.T) {
	item := map[string]any{"logId": "target", "foodName": "Oats", "calories": 100}
	f := newFakeGarmin(t)
	f.on("GET /nutrition-service/food/logs/2024-01-15", map[string]any{
		"mealDetails": []any{map[string]any{"loggedFoods": []any{map[string]any{"logId": "other", "foodName": "Rice"}, item}}},
	})
	text := callOK(t, newSession(t, f, nil), "delete_food_log", map[string]any{"log_id": "target", "meal_date": "2024-01-15"})
	d := resultMap(t, text)
	if d["status"] != "needs_confirmation" || !reflect.DeepEqual(d["entry"], jsonValue(t, item)) || d["meal_date"] != "2024-01-15" || d["log_id"] != "target" {
		t.Errorf("d = %v", d)
	}
	noRequests(t, f, "DELETE")
}

func TestFoodDeletePreviewNotFoundNeverDeletes(t *testing.T) {
	for _, tc := range []struct {
		tool  string
		args  map[string]any
		route string
	}{
		{"delete_custom_food", map[string]any{"food_id": "missing"}, customFoodRoute},
		{"delete_food_log", map[string]any{"log_id": "missing", "meal_date": "2024-01-15"}, "GET /nutrition-service/food/logs/2024-01-15"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on(tc.route, map[string]any{})
			d := resultMap(t, callOK(t, newSession(t, f, nil), tc.tool, tc.args))
			if d["status"] != "not_found" {
				t.Errorf("d = %v", d)
			}
			noRequests(t, f, "DELETE")
		})
	}
}

func TestFoodDeletePreviewErrorNeverDeletes(t *testing.T) {
	for _, tc := range []struct {
		tool  string
		args  map[string]any
		route string
	}{
		{"delete_custom_food", map[string]any{"food_id": "food001"}, customFoodRoute},
		{"delete_food_log", map[string]any{"log_id": "target", "meal_date": "2024-01-15"}, "GET /nutrition-service/food/logs/2024-01-15"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on(tc.route, apiErr(403, "expired"))
			text, isErr := callTool(t, newSession(t, f, nil), tc.tool, tc.args)
			if !strings.Contains(text, "Error deleting") || !isErr {
				t.Errorf("%v %s", isErr, text)
			}
			noRequests(t, f, "DELETE")
		})
	}
}

// A 404 while previewing is also "not found" for delete_food_log (the handler covers the whole try block).
func TestDeleteFoodLogPreviewNotFoundOnHttp404(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /nutrition-service/food/logs/2024-01-15", apiErr(404, "gone"))
	d := resultMap(t, callOK(t, newSession(t, f, nil), "delete_food_log", map[string]any{"log_id": "x", "meal_date": "2024-01-15"}))
	if d["status"] != "not_found" || d["message"] != "Food log entry x was not found." {
		t.Errorf("d = %v", d)
	}
}

func TestDeleteCustomFoodPreviewScansBothSearchPasses(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(customFoodRoute, map[string]any{"customFoods": []any{}})
	callOK(t, newSession(t, f, nil), "delete_custom_food", map[string]any{"food_id": "missing"})
	if n := len(f.requests(customFoodRoute)); n != 2 {
		t.Errorf("%d lookups, want 2 (named pass, then unfiltered pass)", n)
	}
}

// ── upsert_and_log ──────────────────────────────────────────────────────────

var upsertArgs = map[string]any{"meal_date": "2024-01-15", "meal_time": "12:00:00", "food_name": "Greek Yogurt", "calories": 100}

func upsertFake(t *testing.T) *fakeGarmin {
	t.Helper()
	f := newFakeGarmin(t)
	f.on(customFoodRoute, mockCustomFoods())
	f.on(mealsRoute, mockMeals())
	f.on(putFoodRoute, map[string]any{})
	f.on(logsRoute, map[string]any{})
	return f
}

func withArgs(base map[string]any, extra map[string]any) map[string]any {
	a := map[string]any{}
	for k, v := range base {
		a[k] = v
	}
	for k, v := range extra {
		a[k] = v
	}
	return a
}

func searchQuery(food string) map[string]string {
	return map[string]string{"searchExpression": food, "start": "0", "limit": "20", "includeContent": "true"}
}

func TestUpsertAndLogExistingFood(t *testing.T) {
	f := upsertFake(t)
	text := callOK(t, newSession(t, f, nil), "upsert_and_log", withArgs(upsertArgs, map[string]any{"meal_time": "08:30:00"}))
	if text != "Food logged successfully." {
		t.Errorf("text = %q", text)
	}
	if n := len(f.requests(putFoodRoute)); n != 0 {
		t.Errorf("%d creates", n)
	}
	item := asMap(asList(bodyOf(t, theCall(t, f, logsRoute))["foodLogItems"])[0])
	if item["foodId"] != "food001" || item["servingId"] != "srv001" || item["mealId"] != 20249.0 || item["source"] != "GARMIN" || item["servingQty"] != 1.0 {
		t.Errorf("item = %v", item)
	}
	wantQuery(t, f.requests(customFoodRoute)[0].Query, searchQuery("Greek Yogurt"))
}

func TestUpsertAndLogCreatesNewFood(t *testing.T) {
	f := upsertFake(t)
	f.on(customFoodRoute, map[string]any{"customFoods": []any{}})
	f.on(putFoodRoute, map[string]any{
		"foodMetaData": map[string]any{"foodId": "food999", "foodName": "New Food"}, "nutritionContents": []any{map[string]any{"servingId": "srv999"}},
	})
	text := callOK(t, newSession(t, f, nil), "upsert_and_log", map[string]any{
		"meal_date": "2024-01-15", "meal_time": "12:00:00", "food_name": "New Food", "calories": 200, "protein": 20,
	})
	if !strings.Contains(text, "Food logged successfully") {
		t.Errorf("text = %q", text)
	}
	item := asMap(asList(bodyOf(t, theCall(t, f, logsRoute))["foodLogItems"])[0])
	if item["foodId"] != "food999" || item["servingId"] != "srv999" {
		t.Errorf("item = %v", item)
	}
	wantQuery(t, f.requests(customFoodRoute)[0].Query, searchQuery("New Food"))
	want := map[string]any{
		"foodMetaData":      map[string]any{"foodName": "New Food", "foodType": "GENERIC", "source": "GARMIN", "regionCode": "US", "languageCode": "en"},
		"nutritionContents": []any{map[string]any{"servingUnit": "G", "numberOfUnits": "100", "calories": "200", "protein": "20"}},
	}
	if body := bodyOf(t, theCall(t, f, putFoodRoute)); !reflect.DeepEqual(body, want) {
		t.Errorf("create body = %v", body)
	}
	// creation precedes the meal lookup and the log
	order := []string{}
	for _, c := range nutritionRequests(f) {
		order = append(order, c.Method+" "+c.Path)
	}
	if !reflect.DeepEqual(order, []string{customFoodRoute, putFoodRoute, mealsRoute, logsRoute}) {
		t.Errorf("order = %v", order)
	}
}

func TestUpsertAndLogRecoversIdsAfterBodylessCreate(t *testing.T) {
	f := upsertFake(t)
	f.on(customFoodRoute, sequence(map[string]any{"customFoods": []any{}}, mockCustomFoods()))
	text := callOK(t, newSession(t, f, nil), "upsert_and_log", withArgs(upsertArgs, map[string]any{"calories": 200}))
	if !strings.Contains(text, "Food logged successfully") {
		t.Errorf("text = %q", text)
	}
	item := asMap(asList(bodyOf(t, f.requests(logsRoute)[0])["foodLogItems"])[0])
	if item["foodId"] != "food001" || item["servingId"] != "srv001" {
		t.Errorf("item = %v", item)
	}
	lookups := f.requests(customFoodRoute)
	if len(lookups) != 2 {
		t.Fatalf("%d lookups", len(lookups))
	}
	for _, c := range lookups {
		wantQuery(t, c.Query, searchQuery("Greek Yogurt"))
	}
}

func TestUpsertAndLogWithBrandNameOnCreate(t *testing.T) {
	f := upsertFake(t)
	f.on(customFoodRoute, map[string]any{"customFoods": []any{}})
	f.on(putFoodRoute, map[string]any{
		"foodMetaData": map[string]any{"foodId": "food999", "foodName": "Branded Beer"}, "nutritionContents": []any{map[string]any{"servingId": "srv999"}},
	})
	text := callOK(t, newSession(t, f, nil), "upsert_and_log", map[string]any{
		"meal_date": "2024-01-15", "meal_time": "12:00:00", "food_name": "Branded Beer", "calories": 150, "brand_name": "Athletic Brewing",
	})
	if !strings.Contains(text, "Food logged successfully") || at(bodyOf(t, theCall(t, f, putFoodRoute)), "foodMetaData", "brandName") != "Athletic Brewing" {
		t.Errorf("text = %q", text)
	}
}

func TestUpsertAndLogNoBrandNameOmitsKeyOnCreate(t *testing.T) {
	f := upsertFake(t)
	f.on(customFoodRoute, map[string]any{"customFoods": []any{}})
	f.on(putFoodRoute, map[string]any{
		"foodMetaData": map[string]any{"foodId": "food999", "foodName": "Plain Food"}, "nutritionContents": []any{map[string]any{"servingId": "srv999"}},
	})
	callOK(t, newSession(t, f, nil), "upsert_and_log", map[string]any{"meal_date": "2024-01-15", "meal_time": "12:00:00", "food_name": "Plain Food", "calories": 150})
	if _, ok := asMap(bodyOf(t, theCall(t, f, putFoodRoute))["foodMetaData"])["brandName"]; ok {
		t.Error("brandName present")
	}
}

func TestUpsertAndLogBrandNameIgnoredOnExistingMatch(t *testing.T) {
	f := upsertFake(t)
	text := callOK(t, newSession(t, f, nil), "upsert_and_log", withArgs(upsertArgs, map[string]any{"meal_time": "08:30:00", "brand_name": "Chobani"}))
	if !strings.Contains(text, "Food logged successfully") || len(f.requests(putFoodRoute)) != 0 {
		t.Errorf("text = %q", text)
	}
	if item := asMap(asList(bodyOf(t, theCall(t, f, logsRoute))["foodLogItems"])[0]); item["foodId"] != "food001" {
		t.Errorf("item = %v", item)
	}
}

func TestUpsertAndLogError(t *testing.T) {
	f := upsertFake(t)
	f.on(customFoodRoute, apiErr(500, "API error"))
	text, isErr := callTool(t, newSession(t, f, nil), "upsert_and_log", map[string]any{"meal_date": "2024-01-15", "meal_time": "12:00:00", "food_name": "Test Food", "calories": 100})
	if !isErr || !strings.Contains(text, "Error in upsert_and_log") {
		t.Errorf("%v %s", isErr, text)
	}
}

func TestUpsertAndLogForbiddenIsAnError(t *testing.T) {
	f := upsertFake(t)
	f.on(customFoodRoute, apiErr(403, "HTTP 403 Forbidden"))
	text, isErr := callTool(t, newSession(t, f, nil), "upsert_and_log", map[string]any{"meal_date": "2024-01-15", "meal_time": "12:00:00", "food_name": "Test Food", "calories": 100})
	d := failureOf(t, text, isErr)
	if d["message"] != "Error in upsert_and_log: API Error 403 - HTTP 403 Forbidden" || d["upstream_status"] != 403.0 || d["write_committed"] != false {
		t.Errorf("d = %v", d)
	}
	if n := len(f.requests("PUT")) + len(f.requests(putFoodRoute)) + len(f.requests(logsRoute)); n != 0 {
		t.Errorf("%d writes", n)
	}
}

func TestUpsertAndLogLogTimeoutIsUnknown(t *testing.T) {
	f := upsertFake(t)
	f.on(logsRoute, blockUntilCancelled)
	text, isErr := callTool(t, newSession(t, f, timeoutEnv), "upsert_and_log", upsertArgs)
	d := failureOf(t, text, isErr)
	if d["tool"] != "upsert_and_log" || d["stage"] != "garmin_request" || d["write_committed"] != "unknown" || d["hint"] == nil {
		t.Errorf("d = %v", d)
	}
	if n := len(f.requests(logsRoute)); n != 1 { // never auto-retried
		t.Errorf("%d log PUTs", n)
	}
}

func TestUpsertAndLogCreateTimeoutIsUnknown(t *testing.T) {
	f := upsertFake(t)
	f.on(customFoodRoute, map[string]any{"customFoods": []any{}})
	f.on(putFoodRoute, blockUntilCancelled)
	text, isErr := callTool(t, newSession(t, f, timeoutEnv), "upsert_and_log", upsertArgs)
	d := failureOf(t, text, isErr)
	if d["write_committed"] != "unknown" {
		t.Errorf("d = %v", d)
	}
	if n := len(f.requests(putFoodRoute)); n != 1 {
		t.Errorf("%d create PUTs", n)
	}
}

func TestUpsertAndLogLogRejectedAfterCreate(t *testing.T) {
	f := upsertFake(t)
	f.on(customFoodRoute, map[string]any{"customFoods": []any{}})
	f.on(putFoodRoute, map[string]any{
		"foodMetaData": map[string]any{"foodId": "food999", "foodName": "Greek Yogurt"}, "nutritionContents": []any{map[string]any{"servingId": "srv999"}},
	})
	f.on(logsRoute, apiErr(400, "bad meal"))
	text, isErr := callTool(t, newSession(t, f, nil), "upsert_and_log", upsertArgs)
	d := failureOf(t, text, isErr)
	if d["write_committed"] != false || d["upstream_status"] != 400.0 || !strings.Contains(pyStr(d["message"]), "custom food food999 was created") ||
		d["custom_food_created"] != true || d["food_id"] != "food999" {
		t.Errorf("d = %v", d)
	}
	if h := pyStr(d["hint"]); !strings.Contains(h, "not logged") || strings.Contains(h, "Nothing was written") {
		t.Errorf("hint = %q", h)
	}
}

func TestUpsertAndLogSkipsNullSearchMetadata(t *testing.T) {
	f := upsertFake(t)
	foods := []any{
		map[string]any{"foodMetaData": nil, "nutritionContents": nil},
		map[string]any{"foodMetaData": map[string]any{"foodId": "x", "foodName": nil}, "nutritionContents": []any{}},
	}
	f.on(customFoodRoute, map[string]any{"customFoods": append(foods, asList(mockCustomFoods()["customFoods"])...)})
	text := callOK(t, newSession(t, f, nil), "upsert_and_log", upsertArgs)
	if text != "Food logged successfully." {
		t.Errorf("text = %q", text)
	}
	item := asMap(asList(bodyOf(t, theCall(t, f, logsRoute))["foodLogItems"])[0])
	if item["foodId"] != "food001" || item["servingId"] != "srv001" {
		t.Errorf("item = %v", item)
	}
}

func TestUpsertLookupFailureAfterCreateDoesNotCommitLog(t *testing.T) {
	for name, failure := range map[string]any{
		"500":     apiErr(500, "lookup failed"),
		"401":     apiErr(401, "expired"),
		"timeout": http.HandlerFunc(blockUntilCancelled),
	} {
		t.Run(name, func(t *testing.T) {
			f := upsertFake(t)
			f.on(customFoodRoute, sequence(map[string]any{"customFoods": []any{}}, failure))
			text, isErr := callTool(t, newSession(t, f, timeoutEnv), "upsert_and_log", upsertArgs)
			d := failureOf(t, text, isErr)
			if d["write_committed"] != false || d["custom_food_created"] != true {
				t.Errorf("d = %v", d)
			}
			if h := pyStr(d["hint"]); !strings.Contains(h, "not logged") || strings.Contains(h, "Nothing was written") {
				t.Errorf("hint = %q", h)
			}
			if puts := f.requests(putFoodRoute); len(puts) != 1 || len(f.requests(logsRoute)) != 0 {
				t.Errorf("puts = %d", len(puts))
			}
		})
	}
}

func TestUpsertMissingIdsAfterCreateIsStructured(t *testing.T) {
	f := upsertFake(t)
	f.on(customFoodRoute, map[string]any{"customFoods": []any{}})
	text, isErr := callTool(t, newSession(t, f, nil), "upsert_and_log", upsertArgs)
	d := failureOf(t, text, isErr)
	if d["write_committed"] != false || d["custom_food_created"] != true || !strings.Contains(pyStr(d["hint"]), "not logged") {
		t.Errorf("d = %v", d)
	}
	if !strings.Contains(pyStr(d["message"]), "could not retrieve foodId/servingId for 'Greek Yogurt' after creation") {
		t.Errorf("message = %v", d["message"])
	}
	if _, ok := d["food_id"]; ok {
		t.Errorf("food_id present: %v", d)
	}
	if n := len(f.requests(putFoodRoute)); n != 1 || len(f.requests(logsRoute)) != 0 {
		t.Errorf("%d creates", n)
	}
}

func TestUpsertMissingMealsAfterCreateReportsPartialWrite(t *testing.T) {
	f := upsertFake(t)
	f.on(customFoodRoute, map[string]any{"customFoods": []any{}})
	f.on(mealsRoute, map[string]any{"meals": []any{}})
	f.on(putFoodRoute, asList(mockCustomFoods()["customFoods"])[0])
	text, isErr := callTool(t, newSession(t, f, nil), "upsert_and_log", upsertArgs)
	d := failureOf(t, text, isErr)
	if d["write_committed"] != false || d["custom_food_created"] != true || d["food_id"] != "food001" ||
		!strings.Contains(pyStr(d["message"]), "No nutrition meal windows") {
		t.Errorf("d = %v", d)
	}
	if n := len(f.requests(putFoodRoute)); n != 1 || len(f.requests(logsRoute)) != 0 {
		t.Errorf("%d creates", n)
	}
}

// Without a creation the same condition is a plain message.
func TestUpsertMissingMealsWithoutCreateIsPlainMessage(t *testing.T) {
	f := upsertFake(t)
	f.on(mealsRoute, map[string]any{"meals": []any{}})
	text, isErr := callTool(t, newSession(t, f, nil), "upsert_and_log", upsertArgs)
	if isErr || text != "No nutrition meal windows found for 2024-01-15; cannot log food at '12:00:00'." {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestUpsertPagesExactNameMatchBeforeCreating(t *testing.T) {
	var catalog []any
	for i := 0; i < 20; i++ {
		catalog = append(catalog, map[string]any{
			"foodMetaData":      map[string]any{"foodId": "variant" + strconv.Itoa(i), "foodName": "Greek Yogurt " + strconv.Itoa(i)},
			"nutritionContents": []any{map[string]any{"servingId": "s" + strconv.Itoa(i)}},
		})
	}
	catalog = append(catalog, asList(mockCustomFoods()["customFoods"])...)
	f := upsertFake(t)
	f.on(customFoodRoute, customFoodCatalog(catalog...))
	text := callOK(t, newSession(t, f, nil), "upsert_and_log", upsertArgs)
	if text != "Food logged successfully." || len(f.requests(putFoodRoute)) != 0 {
		t.Errorf("text = %q", text)
	}
	if item := asMap(asList(bodyOf(t, theCall(t, f, logsRoute))["foodLogItems"])[0]); item["foodId"] != "food001" {
		t.Errorf("item = %v", item)
	}
	var starts []string
	for _, c := range f.requests(customFoodRoute) {
		starts = append(starts, c.Query.Get("start"))
	}
	if !reflect.DeepEqual(starts, []string{"0", "20"}) {
		t.Errorf("starts = %v", starts)
	}
}

func TestUpsertRefusesCreationWhenSearchLimitIsExhausted(t *testing.T) {
	var foods []any
	for i := 0; i < 20; i++ {
		foods = append(foods, map[string]any{"foodMetaData": map[string]any{"foodName": "Greek Yogurt " + strconv.Itoa(i)}})
	}
	f := upsertFake(t)
	f.on(customFoodRoute, map[string]any{"customFoods": foods})
	text, isErr := callTool(t, newSession(t, f, nil), "upsert_and_log", upsertArgs)
	d := failureOf(t, text, isErr)
	if d["write_committed"] != false || !strings.Contains(pyStr(d["message"]), "search limit") {
		t.Errorf("d = %v", d)
	}
	if n := len(f.requests(customFoodRoute)); n != 25 { // range(0, 500, 20)
		t.Errorf("%d pages", n)
	}
	if n := len(f.requests(putFoodRoute)); n != 0 {
		t.Errorf("%d creates", n)
	}
}

func TestUpsertExistingFoodWithoutServingDoesNotCreateDuplicate(t *testing.T) {
	f := upsertFake(t)
	f.on(customFoodRoute, map[string]any{"customFoods": []any{map[string]any{
		"foodMetaData": map[string]any{"foodId": "food001", "foodName": "Greek Yogurt"}, "nutritionContents": nil,
	}}})
	text, isErr := callTool(t, newSession(t, f, nil), "upsert_and_log", upsertArgs)
	d := failureOf(t, text, isErr)
	if d["write_committed"] != false || !strings.Contains(pyStr(d["message"]), "serving") {
		t.Errorf("d = %v", d)
	}
	if n := len(f.requests(putFoodRoute)) + len(f.requests(logsRoute)); n != 0 {
		t.Errorf("%d writes", n)
	}
}

func TestUpsertResponseDecodeCommitStatusRefersToFoodLog(t *testing.T) {
	for _, phase := range []string{"create", "log"} {
		t.Run(phase, func(t *testing.T) {
			f := upsertFake(t)
			if phase == "create" {
				f.on(customFoodRoute, map[string]any{"customFoods": []any{}})
				f.on(putFoodRoute, rawBody("bad"))
			} else {
				f.on(logsRoute, rawBody("bad"))
			}
			text, isErr := callTool(t, newSession(t, f, nil), "upsert_and_log", upsertArgs)
			d := failureOf(t, text, isErr)
			if d["write_committed"] != (phase == "log") {
				t.Errorf("write_committed = %v", d["write_committed"])
			}
			if phase == "create" && (d["custom_food_created"] != true || !strings.Contains(pyStr(d["hint"]), "not logged")) {
				t.Errorf("d = %v", d)
			}
			if n := len(f.requests("PUT /nutrition-service/customFood")) + len(f.requests(logsRoute)); n != 1 {
				t.Errorf("%d writes", n)
			}
		})
	}
}

// ── set_nutrition_daily_settings ────────────────────────────────────────────

func settingsFake(t *testing.T, current any) *fakeGarmin {
	t.Helper()
	f := newFakeGarmin(t)
	f.on("GET "+settingsRoute, current)
	f.on("PUT "+settingsRoute, map[string]any{})
	return f
}

func settingsArgs(extra map[string]any) map[string]any {
	return withArgs(map[string]any{"date": "2024-01-15"}, extra)
}

func TestSetNutritionDailySettingsUpdatesAllFields(t *testing.T) {
	f := settingsFake(t, currentSettings())
	// Server values can differ from the requested values.
	f.on("PUT "+settingsRoute, map[string]any{"calorieGoal": 1850, "macroGoals": map[string]any{"carbs": 205, "fat": 60, "protein": 140}})
	text := callOK(t, newSession(t, f, nil), "set_nutrition_daily_settings", settingsArgs(map[string]any{
		"calorie_goal": 1800, "carbs_grams": 200, "fat_grams": 60, "protein_grams": 140,
	}))
	want := map[string]any{
		"status": "updated", "date": "2024-01-15", "calorie_goal": 1850.0, "carbs_grams": 205.0, "fat_grams": 60.0, "protein_grams": 140.0,
		"weight_goal_target_date": nil,
	}
	if !reflect.DeepEqual(decode(t, text), want) {
		t.Errorf("text = %s", text)
	}
	expected := currentSettings()
	expected["calorieGoal"] = 1800
	expected["macroGoals"] = map[string]any{"carbs": 200, "fat": 60, "protein": 140}
	if body := bodyOf(t, theCall(t, f, "PUT "+settingsRoute)); !reflect.DeepEqual(body, jsonValue(t, expected)) {
		t.Errorf("body = %v", body)
	}
}

func TestSetNutritionDailySettingsReportsPassedWeightGoalTarget(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /nutrition-service/settings/2024-09-01", currentSettings())
	text := callOK(t, newSession(t, f, nil), "set_nutrition_daily_settings", map[string]any{"date": "2024-09-01", "calorie_goal": 1800})
	for _, want := range []string{"targetDate", "2024-06-01", "2024-09-01"} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q: %s", want, text)
		}
	}
	if n := len(f.requests("PUT /nutrition-service/settings/2024-09-01")); n != 0 {
		t.Errorf("%d PUTs", n)
	}
}

func TestSetNutritionDailySettingsAcceptsExplicitTargetDate(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /nutrition-service/settings/2024-09-01", currentSettings())
	f.on("PUT /nutrition-service/settings/2024-09-01", map[string]any{
		"calorieGoal": 1800, "macroGoals": currentSettings()["macroGoals"], "targetDate": "2024-12-31",
	})
	text := callOK(t, newSession(t, f, nil), "set_nutrition_daily_settings", map[string]any{"date": "2024-09-01", "calorie_goal": 1800, "target_date": "2024-12-31"})
	d := resultMap(t, text)
	if d["status"] != "updated" || d["weight_goal_target_date"] != "2024-12-31" {
		t.Errorf("d = %v", d)
	}
	expected := currentSettings()
	expected["targetDate"] = "2024-12-31"
	expected["calorieGoal"] = 1800
	if body := bodyOf(t, theCall(t, f, "PUT /nutrition-service/settings/2024-09-01")); !reflect.DeepEqual(body, jsonValue(t, expected)) {
		t.Errorf("body = %v", body)
	}
}

func TestSetNutritionDailySettingsRejectsTargetDateNotAfterDate(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /nutrition-service/settings/2024-09-01", currentSettings())
	text := callOK(t, newSession(t, f, nil), "set_nutrition_daily_settings", map[string]any{"date": "2024-09-01", "calorie_goal": 1800, "target_date": "2024-09-01"})
	if !strings.Contains(text, "must be after") || len(f.requests("PUT /nutrition-service/settings/2024-09-01")) != 0 {
		t.Errorf("text = %q", text)
	}
}

func TestSetNutritionDailySettingsPartialUpdate(t *testing.T) {
	for _, tc := range []struct {
		name      string
		overrides map[string]any
		key       string
		value     int
	}{
		{"calorie_goal", map[string]any{"calorie_goal": 1900}, "calorieGoal", 1900},
		{"carbs_grams zero", map[string]any{"carbs_grams": 0}, "carbs", 0},
		{"fat_grams", map[string]any{"fat_grams": 55}, "fat", 55},
		{"protein_grams", map[string]any{"protein_grams": 130}, "protein", 130},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expected := currentSettings()
			if tc.key == "calorieGoal" {
				expected[tc.key] = tc.value
			} else {
				expected["macroGoals"].(map[string]any)[tc.key] = tc.value
			}
			stored := currentSettings()
			stored["macroGoals"] = expected["macroGoals"]
			stored["calorieGoal"] = 1950
			f := newFakeGarmin(t)
			f.on("GET "+settingsRoute, sequence(currentSettings(), stored))
			f.on("PUT "+settingsRoute, rawBody("null"))
			d := resultMap(t, callOK(t, newSession(t, f, nil), "set_nutrition_daily_settings", settingsArgs(tc.overrides)))
			if d["calorie_goal"] != 1950.0 || d["carbs_grams"] != jsonValue(t, asMap(stored["macroGoals"])["carbs"]) {
				t.Errorf("d = %v", d)
			}
			if body := bodyOf(t, theCall(t, f, "PUT "+settingsRoute)); !reflect.DeepEqual(body, jsonValue(t, expected)) {
				t.Errorf("body = %v", body)
			}
			if n := len(f.requests("GET " + settingsRoute)); n != 2 {
				t.Errorf("%d GETs", n)
			}
		})
	}
}

func TestSetNutritionDailySettingsUnverifiedWrite(t *testing.T) {
	for name, readback := range map[string]any{"none": nil, "error": apiErr(500, "read-back unavailable")} {
		t.Run(name, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on("GET "+settingsRoute, sequence(currentSettings(), readback))
			f.on("PUT "+settingsRoute, rawBody("null"))
			text := callOK(t, newSession(t, f, nil), "set_nutrition_daily_settings", settingsArgs(map[string]any{"calorie_goal": 1900}))
			if !strings.Contains(text, "could not verify") || strings.Contains(text, `"status": "updated"`) {
				t.Errorf("text = %q", text)
			}
		})
	}
}

func TestSetNutritionDailySettingsNoFieldsProvided(t *testing.T) {
	f := newFakeGarmin(t)
	text := callOK(t, newSession(t, f, nil), "set_nutrition_daily_settings", settingsArgs(nil))
	if text != "No fields to update — supply at least one of calorie_goal, carbs_grams, fat_grams, protein_grams." || len(nutritionRequests(f)) != 0 {
		t.Errorf("text = %q", text)
	}
}

func TestSetNutritionDailySettingsNoCurrentData(t *testing.T) {
	f := settingsFake(t, nil)
	text := callOK(t, newSession(t, f, nil), "set_nutrition_daily_settings", settingsArgs(map[string]any{"calorie_goal": 1800}))
	if text != "Could not read current nutrition settings for 2024-01-15 — cannot apply update." || len(f.requests("PUT "+settingsRoute)) != 0 {
		t.Errorf("text = %q", text)
	}
}

func TestSetNutritionDailySettingsApiError(t *testing.T) {
	f := settingsFake(t, currentSettings())
	f.on("PUT "+settingsRoute, apiErr(403, "403 Forbidden"))
	text, isErr := callTool(t, newSession(t, f, nil), "set_nutrition_daily_settings", settingsArgs(map[string]any{"calorie_goal": 1800}))
	if !isErr || !strings.Contains(text, "Error updating nutrition settings") || !strings.Contains(text, "403") {
		t.Errorf("%v %s", isErr, text)
	}
}

func TestSetNutritionSettingsHandlesSparseMacros(t *testing.T) {
	for name, macros := range map[string]any{"none": nil, "empty": map[string]any{}, "other": map[string]any{"fiber": 30}} {
		t.Run(name, func(t *testing.T) {
			current := currentSettings()
			current["macroGoals"] = macros
			f := newFakeGarmin(t)
			f.on("GET "+settingsRoute, current)
			f.on("PUT "+settingsRoute, func(w http.ResponseWriter, r *http.Request) { // echo the request body
				b := bodyOf(t, recordedCall{Method: "PUT", Path: r.URL.Path, Body: mustRead(t, r)})
				json.NewEncoder(w).Encode(b)
			})
			text := callOK(t, newSession(t, f, nil), "set_nutrition_daily_settings", settingsArgs(map[string]any{"protein_grams": 130}))
			if resultMap(t, text)["protein_grams"] != 130.0 {
				t.Errorf("text = %s", text)
			}
			expected := currentSettings()
			merged := map[string]any{}
			for k, v := range asMap(macros) {
				merged[k] = v
			}
			merged["protein"] = 130
			expected["macroGoals"] = merged
			if body := bodyOf(t, theCall(t, f, "PUT "+settingsRoute)); !reflect.DeepEqual(body, jsonValue(t, expected)) {
				t.Errorf("body = %v", body)
			}
		})
	}
}

func TestSetNutritionSettingsRejectsInvalidMacros(t *testing.T) {
	current := currentSettings()
	current["macroGoals"] = []any{250, 65, 120}
	f := settingsFake(t, current)
	text := callOK(t, newSession(t, f, nil), "set_nutrition_daily_settings", settingsArgs(map[string]any{"protein_grams": 130}))
	if text != "Could not read current macro goals — cannot apply update." || len(f.requests("PUT "+settingsRoute)) != 0 {
		t.Errorf("text = %q", text)
	}
}

// Everything past the PUT is "response handling": the write is committed.
func TestSetNutritionDailySettingsResponseProblemsAfterPutAreCommitted(t *testing.T) {
	f := settingsFake(t, currentSettings())
	f.on("PUT "+settingsRoute, map[string]any{"calorieGoal": 1, "macroGoals": []any{1}})
	text, isErr := callTool(t, newSession(t, f, nil), "set_nutrition_daily_settings", settingsArgs(map[string]any{"calorie_goal": 1800}))
	d := failureOf(t, text, isErr)
	if d["write_committed"] != true || d["stage"] != "response_parse" {
		t.Errorf("d = %v", d)
	}
}

// ── structured write errors ─────────────────────────────────────────────────

func TestLogFoodGarminRejectionIsStructured(t *testing.T) {
	f := mealsFake(t)
	// Python's client retries a 401 once after refreshing the token and raises when the retry fails too.
	f.on("POST /di-oauth2-service/oauth/token", map[string]any{"access_token": fakeJWT(time.Now().Add(time.Hour), "client-"+testEmail), "refresh_token": "r2"})
	f.on(quickRoute, apiErr(401, ""))
	text, isErr := callTool(t, newSession(t, f, nil), "log_food", logFoodArgs(nil))
	d := failureOf(t, text, isErr)
	if d["tool"] != "log_food" || d["stage"] != "garmin_request" || d["upstream_status"] != 401.0 || d["write_committed"] != false || d["correlation_id"] == "" {
		t.Errorf("d = %v", d)
	}
}

func TestLogFood5xxAfterSendIsUnknown(t *testing.T) {
	f := mealsFake(t)
	f.on(quickRoute, status(504, rawBody("<html>Gateway Time-out</html>")))
	text, isErr := callTool(t, newSession(t, f, nil), "log_food", logFoodArgs(nil))
	d := failureOf(t, text, isErr)
	if d["stage"] != "garmin_request" || d["upstream_status"] != 504.0 || d["write_committed"] != "unknown" {
		t.Errorf("d = %v", d)
	}
	if n := len(f.requests(quickRoute)); n != 1 { // never auto-retried
		t.Errorf("%d PUTs", n)
	}
}

func TestLogFoodTimeoutAfterSendIsUnknown(t *testing.T) {
	f := mealsFake(t)
	f.on(quickRoute, blockUntilCancelled)
	text, isErr := callTool(t, newSession(t, f, timeoutEnv), "log_food", logFoodArgs(nil))
	d := failureOf(t, text, isErr)
	if d["write_committed"] != "unknown" || len(f.requests(quickRoute)) != 1 {
		t.Errorf("d = %v", d)
	}
}

func TestLogFoodMealLookupFailureIsNotCommitted(t *testing.T) {
	f := mealsFake(t)
	f.on(mealsRoute, blockUntilCancelled)
	text, isErr := callTool(t, newSession(t, f, timeoutEnv), "log_food", logFoodArgs(nil))
	d := failureOf(t, text, isErr)
	if d["write_committed"] != false || len(f.requests(quickRoute)) != 0 {
		t.Errorf("d = %v", d)
	}
}

func TestLogCustomFoodErrorIsStructured(t *testing.T) {
	f := mealsFake(t)
	f.on(logsRoute, apiErr(400, "Required Parameter meal id is null"))
	text, isErr := callTool(t, newSession(t, f, nil), "log_custom_food", logCustomArgs(map[string]any{"food_id": "abc", "serving_id": "s"}))
	d := failureOf(t, text, isErr)
	if d["tool"] != "log_custom_food" || d["upstream_status"] != 400.0 || d["upstream_body"] != "Required Parameter meal id is null" {
		t.Errorf("d = %v", d)
	}
}

func TestDeleteFoodLogErrorIsStructured(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("DELETE /nutrition-service/food/logs/2024-01-15", nil)
	s := newSession(t, f, nil)
	f.Close() // the connection can never be opened
	text, isErr := callTool(t, s, "delete_food_log", map[string]any{"confirm": true, "log_id": "581f7dc8797f421f8d7eea83e5d2c939", "meal_date": "2024-01-15"})
	d := failureOf(t, text, isErr)
	if d["tool"] != "delete_food_log" || !strings.HasPrefix(pyStr(d["message"]), "Error deleting food log") || d["write_committed"] != false {
		t.Errorf("d = %v", d)
	}
}

func TestLogFoodSuccessShapeUnchanged(t *testing.T) {
	f := mealsFake(t)
	text := callOK(t, newSession(t, f, nil), "log_food", logFoodArgs(nil))
	if text != "Food logged successfully." {
		t.Errorf("text = %q", text)
	}
}

func TestNutritionLibraryWritesReportCommitStatus(t *testing.T) {
	type tool struct {
		name  string
		args  map[string]any
		write string
	}
	tools := []tool{
		{"set_nutrition_daily_settings", map[string]any{"date": "2024-01-15", "calorie_goal": 1800}, "PUT " + settingsRoute},
		{"create_custom_food", map[string]any{"food_name": "Oats", "calories": 100}, putFoodRoute},
		{"update_custom_food", map[string]any{"food_id": "food001", "serving_id": "srv001", "food_name": "Greek Yogurt", "calories": 100}, putFoodRoute},
		{"delete_custom_food", map[string]any{"food_id": "food001", "confirm": true}, "DELETE /nutrition-service/customFood/food001"},
	}
	kinds := []struct {
		name      string
		response  any
		env       map[string]string
		committed any
	}{
		{"gateway", status(504, map[string]any{"message": "Gateway Time-out"}), nil, "unknown"},
		{"read_timeout", http.HandlerFunc(blockUntilCancelled), timeoutEnv, "unknown"},
		{"rejection", apiErr(400, "rejected"), nil, false},
		{"parse", rawBody("bad"), nil, true},
	}
	for _, tl := range tools {
		for _, k := range kinds {
			t.Run(tl.name+"/"+k.name, func(t *testing.T) {
				f := newFakeGarmin(t)
				f.on("GET "+settingsRoute, currentSettings())
				f.on(customFoodRoute, mockCustomFoods())
				f.on(tl.write, k.response)
				text, isErr := callTool(t, newSession(t, f, k.env), tl.name, tl.args)
				d := failureOf(t, text, isErr)
				wantStage := "garmin_request"
				if k.name == "parse" {
					wantStage = "response_parse"
				}
				if d["tool"] != tl.name || d["write_committed"] != k.committed || d["stage"] != wantStage {
					t.Errorf("d = %v", d)
				}
				if n := len(f.requests(tl.write)); n != 1 {
					t.Errorf("%d writes", n)
				}
				if k.committed == "unknown" && !strings.Contains(pyStr(d["hint"]), "before retrying") {
					t.Errorf("hint = %v", d["hint"])
				}
			})
		}
	}
}

func TestFoodLoggingRejectsInvalidMealDatetimeBeforeRequests(t *testing.T) {
	tools := map[string]map[string]any{
		"log_food":        {"name": "Oats", "calories": 100, "carbs": 10, "protein": 5, "fat": 2},
		"log_custom_food": {"food_id": "food001", "serving_id": "srv001"},
		"upsert_and_log":  {"food_name": "Oats", "calories": 100},
	}
	for name, extra := range tools {
		for _, dt := range [][2]string{
			{"2024-01-15", "8:30:00"}, {"2024-01-15", "noon"}, {"2024-01-15", "25:00:00"}, {"2024-01-15", "12:30 PM"},
			{"2024-01-15", "2024-01-15T08:30:00"}, {"2024-02-30", "08:30:00"},
		} {
			t.Run(name+"/"+dt[0]+" "+dt[1], func(t *testing.T) {
				f := mealsFake(t)
				text, isErr := callTool(t, newSession(t, f, nil), name, withArgs(extra, map[string]any{"meal_date": dt[0], "meal_time": dt[1]}))
				d := failureOf(t, text, isErr)
				if d["write_committed"] != false || !(strings.Contains(pyStr(d["message"]), "meal_date") || strings.Contains(pyStr(d["message"]), "meal_time")) {
					t.Errorf("d = %v", d)
				}
				if reqs := nutritionRequests(f); len(reqs) != 0 {
					t.Errorf("requests: %v", reqs)
				}
			})
		}
	}
}

// ── free-text IDs and dates stay inside their endpoint ──────────────────────

func TestNutritionWriteToolsKeepFreeTextInTheirPath(t *testing.T) {
	const evil = "x/../../foo?a=b#frag"
	const seg = "x%2F..%2F..%2Ffoo%3Fa=b%23frag"
	for _, tc := range []struct {
		tool, prefix string
		args         map[string]any
	}{
		{"delete_custom_food", "/nutrition-service/customFood/", map[string]any{"food_id": evil, "confirm": true}},
		{"delete_food_log", "/nutrition-service/food/logs/", map[string]any{"log_id": "L", "meal_date": evil, "confirm": true}},
		{"delete_food_log", "/nutrition-service/food/logs/", map[string]any{"log_id": "L", "meal_date": evil}},
		{"set_nutrition_daily_settings", "/nutrition-service/settings/", map[string]any{"date": evil, "calorie_goal": 1}},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			f := newFakeGarmin(t)
			var uris []string
			f.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.RequestURI, tc.prefix) {
					uris = append(uris, r.RequestURI)
				}
				w.Write([]byte(`{"settings":1}`))
			})
			callTool(t, newSession(t, f, nil), tc.tool, tc.args)
			if len(uris) == 0 {
				t.Fatal("no request under the endpoint prefix")
			}
			for _, u := range uris {
				if !strings.HasPrefix(u, tc.prefix+seg) {
					t.Errorf("uri = %q", u)
				}
			}
		})
	}
}

func mustRead(t *testing.T, r *http.Request) []byte {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestNutritionWriteToolsRegistered(t *testing.T) {
	cs := newSession(t, newFakeGarmin(t), nil)
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tl := range res.Tools {
		got[tl.Name] = true
	}
	for _, name := range strings.Fields(`get_nutrition_daily_food_log get_nutrition_summary_between_dates get_nutrition_daily_meals
		get_nutrition_daily_settings set_nutrition_daily_settings search_foods get_custom_foods get_custom_food_serving_units create_custom_food
		update_custom_food delete_custom_food log_custom_food log_food delete_food_log upsert_and_log`) {
		if !got[name] {
			t.Errorf("tool %s not registered", name)
		}
	}
}

func TestLogFoodNoAttributeErrorOnSuccess(t *testing.T) {
	f := mealsFake(t)
	f.on(quickRoute, map[string]any{"logId": "abc123", "status": "logged"})
	text := callOK(t, newSession(t, f, nil), "log_food", map[string]any{
		"meal_date": "2024-01-15", "meal_time": "12:30:00", "name": "Test Food", "calories": 100, "carbs": 10, "protein": 5, "fat": 3,
	})
	if strings.Contains(text, "Error") || !strings.Contains(text, "logId") {
		t.Errorf("text = %s", text)
	}
}

func TestNutritionToolSchemas(t *testing.T) {
	cs := newSession(t, newFakeGarmin(t), nil)
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	required := map[string][]string{
		"create_custom_food":           {"email", "food_name", "calories"},
		"update_custom_food":           {"email", "food_id", "serving_id", "food_name", "calories"},
		"log_custom_food":              {"email", "meal_date", "meal_time", "food_id", "serving_id"},
		"log_food":                     {"email", "meal_date", "meal_time", "name", "calories", "carbs", "protein", "fat"},
		"upsert_and_log":               {"email", "meal_date", "meal_time", "food_name", "calories"},
		"set_nutrition_daily_settings": {"email", "date"},
		"delete_custom_food":           {"email", "food_id"},
		"delete_food_log":              {"email", "log_id", "meal_date"},
		"search_foods":                 {"email", "query"},
		"get_custom_foods":             {"email"},
	}
	optional := map[string][]string{
		"create_custom_food": {"serving_unit", "number_of_units", "brand_name", "carbs", "vitamin_d", "saturated_fat"},
		"log_custom_food":    {"serving_qty", "source"},
		"upsert_and_log":     {"carbs", "brand_name", "serving_unit", "number_of_units", "serving_qty"},
	}
	for _, tl := range res.Tools {
		want, ok := required[tl.Name]
		if !ok {
			continue
		}
		b, _ := json.Marshal(tl.InputSchema)
		var sch struct {
			Required   []string       `json:"required"`
			Properties map[string]any `json:"properties"`
			Additional any            `json:"additionalProperties"`
		}
		json.Unmarshal(b, &sch)
		if !reflect.DeepEqual(toSet(sch.Required), toSet(want)) || sch.Additional != false {
			t.Errorf("%s: required %v additional %v, want %v", tl.Name, sch.Required, sch.Additional, want)
		}
		for _, p := range optional[tl.Name] {
			if _, has := sch.Properties[p]; !has {
				t.Errorf("%s: property %s missing", tl.Name, p)
			}
		}
	}
}
