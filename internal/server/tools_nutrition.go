package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

const (
	nutritionFoodLogDescription = `Get daily food consumption records for a date

Returns food items logged throughout the day including calories,
macronutrients, and meal associations.`

	nutritionSummaryDescription = `Get per-day nutrition totals for every day in a date range.

Returns one lightweight entry per day (date, calories, carbs, protein,
fat, item_count) instead of the full per-item food log that
get_nutrition_daily_food_log returns for a single date. Use this for
multi-day intake analysis (e.g. mean intake for a TDEE estimate)
instead of calling get_nutrition_daily_food_log once per day.

item_count is the number of logged food items that day. A day with
item_count == 0 has no logged food at all, and a low but nonzero
item_count may mean only part of the day was logged (e.g. breakfast
only). Both cases read as low intake in the totals alone -- exclude
low-item_count days explicitly before averaging intake or deriving
TDEE; don't infer "unlogged" from a low calorie total.

Maximum range: 61 days per call (Garmin's own limit for this endpoint).`

	nutritionMealsDescription = `Get daily meal summaries for a date

Returns meal-level summaries (breakfast, lunch, dinner, snacks)
with nutritional totals for each meal. Each meal includes a mealId
needed for logging food items to that meal.`

	nutritionSettingsDescription = `Get nutrition plan/settings for a date

Returns the user's nutrition goals and targets including
calorie targets, macronutrient goals, and plan configuration.`

	setNutritionSettingsDescription = `Update daily nutrition goals (calorie target and macronutrient targets).

Reads the current settings for the date, applies the supplied overrides,
and writes the merged result back.  Only the fields you provide are
changed; omitted fields keep their existing values.

Macro arguments retain their existing names and are passed through to
Garmin's macroGoals without unit conversion. The endpoint's macro units
have not been independently verified. Returned goals come from Garmin's
response, or a settings read-back when the update has no response body.`

	searchFoodsDescription = `Search Garmin's general food catalog (FatSecret + Garmin custom foods)

Searches across the entire food catalog including FatSecret-sourced
branded and generic foods, not just the user's Garmin custom foods.
Use this to find branded packaged foods by name before logging them.

Returns food_id, source, name, brand, and all available servings with
macros. The source field ("FATSECRET" or "GARMIN") and food_id together
identify the right routing for log_custom_food — pass both to
log_custom_food's food_id and source parameters respectively.

For the user's own custom foods only, use get_custom_foods instead.`

	getCustomFoodsDescription = `Search or list user's custom foods

Returns custom foods the user has created. Use the search parameter
to find existing foods by name before creating duplicates — the
response includes foodId and servingId needed for log_custom_food.

For branded catalog foods (FatSecret), use search_foods instead.`

	getCustomFoodServingUnitsDescription = `Get available serving units for custom foods

Returns the list of valid serving units (e.g. G, ML, OZ)
that can be used when creating custom foods.`

	createCustomFoodDescription = `Create a custom food in the user's Garmin nutrition library

Creates a new food item with nutritional information per serving.
On success the response includes foodId and servingId needed for
log_custom_food. If the API returns no data (204), use
get_custom_foods(search=food_name) to retrieve those IDs.

All nutrient amounts are ABSOLUTE values per serving, not %DV.
Nutrition labels often print %DV for calcium/iron/vitamin D —
convert to absolute units before passing.`

	updateCustomFoodDescription = `Update an existing custom food in the user's Garmin nutrition library

Fetches the food's current record (by food_id, so renaming is safe)
before writing so that omitted optional fields (brand, carbs, protein,
fat, micros, etc.) preserve their existing values rather than being
cleared. Only the fields you explicitly pass are changed; everything
else is carried forward from the current record.

Garmin replaces the whole record on update. If the current record
cannot be read, the update is refused unless serving_unit and
number_of_units are given; the call then replaces the record in full,
and any brand or nutrient you omit is cleared.

Only future logs see the change. Each food-log entry stores its own
copy of the food's nutrition from when it was logged, so entries
already logged, and those days' summaries, keep the old values. To
correct a past day, delete_food_log each affected entry and log it
again after the update.

All nutrient amounts are ABSOLUTE values per serving, not %DV.
Nutrition labels often print %DV for calcium/iron/vitamin D —
convert to absolute units before passing.

Use get_custom_foods first to find the foodId and servingId.`

	deleteCustomFoodDescription = `Delete a custom food from the user's Garmin nutrition library

Permanently removes a custom food entry — this cannot be undone and
the deleted food's name/brand/nutrition data cannot be recovered from
the API afterward. The default call previews the food without deleting
it. Review that preview, then call again with confirm=true.

Deleting a food does NOT require it to be unreferenced: existing food
log entries that already reference this food are NOT blocked or
removed — they remain in the log (their nutrition still counts toward
daily totals) but are marked inactive (foodInactive: true) and can no
longer be edited/re-logged from the library.

Use get_custom_foods to find the foodId, and match on the exact
foodId (not just a name search) — Garmin's search can return loosely
related results for a given search term, not just exact matches.`

	logCustomFoodDescription = `Log a food item to a meal on a date

Adds a food entry to the nutrition log. The meal is determined
automatically by matching meal_time against each meal's
startTime/endTime window; falls back to SNACKS if no window matches.

Food sources:
  - "GARMIN" (default): user's custom food library. Use
    get_custom_foods to find food_id and serving_id.
  - "FATSECRET": branded/catalog food from FatSecret. Use
    search_foods to find food_id and serving_id. Pass the source
    value from the search_foods result (e.g. "FATSECRET").

Garmin custom food IDs are 32-char hex UUIDs; FatSecret IDs are
numeric strings (e.g. "4132350"). Passing the wrong source for a
given food_id returns a 400 from Garmin.`

	logFoodDescription = `Quick-add a food entry with macro values to the nutrition log

Logs food directly by name and macros without requiring a food ID.
Uses Garmin's Quick Add feature. The meal is determined automatically
by matching meal_time against each meal's startTime/endTime window;
falls back to SNACKS if no window matches.`

	deleteFoodLogDescription = `Delete a food log entry

Permanently removes a logged food item from the nutrition log.
Works for both QUICK_ADD and REGULAR_LOG entry types.
Use get_nutrition_daily_food_log to find the logId and date. The default
call previews the matching entry; call again with confirm=true to delete.`

	upsertAndLogDescription = `Find-or-create a custom food then log it in one step

Searches the user's custom food library for food_name. If found, logs
it immediately. If not found, creates it with the provided nutrition
data and then logs it. This avoids duplicate food entries and removes
the need for separate search → create → log round-trips.

Matching is by food_name alone (case-insensitive) — brand_name never
affects find-or-create lookup, so it can't cause a duplicate by missing
an existing unbranded record. If an existing match is found, it is
logged as stored, using its first serving: calories, carbs, protein,
fat, brand_name, serving_unit and number_of_units are all ignored and
the stored food is not changed; only serving_qty applies. Those values
only take effect when creating a new food. To change an existing
food's nutrition, serving size or brand, use update_custom_food.

On an error after creation, custom_food_created is true and
write_committed describes the food log, not the library food. A failed
or exhausted lookup never triggers another create automatically.`
)

const (
	customFoodPageSize = 20
	customFoodMaxScan  = 500
)

var errCustomFoodSearchLimit = errors.New("Custom food search limit reached; cannot safely determine whether the food exists.")

type nutritionDateInput struct {
	Account
	Date string `json:"date" jsonschema:"Date in YYYY-MM-DD format"`
}

type nutritionSummaryInput struct {
	Account
	StartDate string `json:"start_date" jsonschema:"Start date in YYYY-MM-DD format"`
	EndDate   string `json:"end_date" jsonschema:"End date in YYYY-MM-DD format"`
}

type setNutritionSettingsInput struct {
	Account
	Date         string  `json:"date" jsonschema:"Date in YYYY-MM-DD format (settings are typically set once and inherited across days, but Garmin accepts per-day overrides)"`
	CalorieGoal  *int    `json:"calorie_goal,omitempty" jsonschema:"Daily calorie target in kcal"`
	CarbsGrams   *int    `json:"carbs_grams,omitempty" jsonschema:"Daily carbohydrate target in grams"`
	FatGrams     *int    `json:"fat_grams,omitempty" jsonschema:"Daily fat target in grams"`
	ProteinGrams *int    `json:"protein_grams,omitempty" jsonschema:"Daily protein target in grams"`
	TargetDate   *string `json:"target_date,omitempty" jsonschema:"Optional new target date (YYYY-MM-DD) for the weight goal that this endpoint carries. Only needed when the stored target date has already passed, which otherwise makes Garmin reject the update; must be after 'date'."`
}

type searchFoodsInput struct {
	Account
	Query string `json:"query" jsonschema:"Food name or brand to search for (e.g. \"Cheerios\", \"Greek yogurt\")"`
	Start int    `json:"start,omitempty" jsonschema:"Starting index for pagination (default 0)"`
	Limit *int   `json:"limit,omitempty" jsonschema:"Maximum number of results per page (default 20)"`
}

type getCustomFoodsInput struct {
	Account
	Search string `json:"search,omitempty" jsonschema:"Search term to filter foods by name (default: list all)"`
	Start  int    `json:"start,omitempty" jsonschema:"Starting index for pagination (default 0)"`
	Limit  *int   `json:"limit,omitempty" jsonschema:"Maximum number of results (default 20)"`
}

type customFoodServingUnitsInput struct {
	Account
}

type createCustomFoodInput struct {
	Account
	FoodName      string   `json:"food_name" jsonschema:"Name of the custom food (e.g. \"Homemade Chocolate Cookies\")"`
	Calories      float64  `json:"calories" jsonschema:"Calories per serving"`
	ServingUnit   string   `json:"serving_unit,omitempty" jsonschema:"Unit for serving size (e.g. \"G\", \"ML\", \"OZ\"). Default \"G\""`
	NumberOfUnits *float64 `json:"number_of_units,omitempty" jsonschema:"Serving size in the specified unit. Default 100"`
	BrandName     *string  `json:"brand_name,omitempty" jsonschema:"Brand or vendor name (e.g. \"Three Bridges\")"`
	Carbs         *float64 `json:"carbs,omitempty" jsonschema:"Carbohydrates in grams per serving"`
	Protein       *float64 `json:"protein,omitempty" jsonschema:"Protein in grams per serving"`
	Fat           *float64 `json:"fat,omitempty" jsonschema:"Total fat in grams per serving"`
	Fiber         *float64 `json:"fiber,omitempty" jsonschema:"Fiber in grams per serving"`
	Sugar         *float64 `json:"sugar,omitempty" jsonschema:"Sugar in grams per serving"`
	SaturatedFat  *float64 `json:"saturated_fat,omitempty" jsonschema:"Saturated fat in grams per serving"`
	Sodium        *float64 `json:"sodium,omitempty" jsonschema:"Sodium in mg per serving"`
	Cholesterol   *float64 `json:"cholesterol,omitempty" jsonschema:"Cholesterol in mg per serving"`
	Potassium     *float64 `json:"potassium,omitempty" jsonschema:"Potassium in mg per serving"`
	TransFat      *float64 `json:"trans_fat,omitempty" jsonschema:"Trans fat in grams per serving"`
	Calcium       *float64 `json:"calcium,omitempty" jsonschema:"Calcium in mg per serving (NOT %DV)"`
	Iron          *float64 `json:"iron,omitempty" jsonschema:"Iron in mg per serving (NOT %DV)"`
	VitaminD      *float64 `json:"vitamin_d,omitempty" jsonschema:"Vitamin D in mcg per serving (NOT %DV)"`
}

type updateCustomFoodInput struct {
	Account
	FoodID        string   `json:"food_id" jsonschema:"ID of the custom food to update (from get_custom_foods)"`
	ServingID     string   `json:"serving_id" jsonschema:"Serving ID of the food (from get_custom_foods)"`
	FoodName      string   `json:"food_name" jsonschema:"Name of the custom food"`
	Calories      float64  `json:"calories" jsonschema:"Calories per serving"`
	ServingUnit   *string  `json:"serving_unit,omitempty" jsonschema:"Unit for serving size (e.g. \"G\", \"ML\", \"OZ\"); omit to preserve the existing value"`
	NumberOfUnits *float64 `json:"number_of_units,omitempty" jsonschema:"Serving size in the specified unit; omit to preserve the existing value"`
	BrandName     *string  `json:"brand_name,omitempty" jsonschema:"Brand or vendor name; omit to preserve the existing value"`
	Carbs         *float64 `json:"carbs,omitempty" jsonschema:"Carbohydrates in grams per serving"`
	Protein       *float64 `json:"protein,omitempty" jsonschema:"Protein in grams per serving"`
	Fat           *float64 `json:"fat,omitempty" jsonschema:"Total fat in grams per serving"`
	Fiber         *float64 `json:"fiber,omitempty" jsonschema:"Fiber in grams per serving"`
	Sugar         *float64 `json:"sugar,omitempty" jsonschema:"Sugar in grams per serving"`
	SaturatedFat  *float64 `json:"saturated_fat,omitempty" jsonschema:"Saturated fat in grams per serving"`
	Sodium        *float64 `json:"sodium,omitempty" jsonschema:"Sodium in mg per serving"`
	Cholesterol   *float64 `json:"cholesterol,omitempty" jsonschema:"Cholesterol in mg per serving"`
	Potassium     *float64 `json:"potassium,omitempty" jsonschema:"Potassium in mg per serving"`
	TransFat      *float64 `json:"trans_fat,omitempty" jsonschema:"Trans fat in grams per serving"`
	Calcium       *float64 `json:"calcium,omitempty" jsonschema:"Calcium in mg per serving (NOT %DV)"`
	Iron          *float64 `json:"iron,omitempty" jsonschema:"Iron in mg per serving (NOT %DV)"`
	VitaminD      *float64 `json:"vitamin_d,omitempty" jsonschema:"Vitamin D in mcg per serving (NOT %DV)"`
}

type deleteCustomFoodInput struct {
	Account
	FoodID  string `json:"food_id" jsonschema:"ID of the custom food to delete — a 32-char hex string (from get_custom_foods or create_custom_food)"`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"Set true to delete after reviewing the default preview"`
}

type logCustomFoodInput struct {
	Account
	MealDate   string   `json:"meal_date" jsonschema:"Date in YYYY-MM-DD format"`
	MealTime   string   `json:"meal_time" jsonschema:"Time in HH:MM:SS format (e.g. \"12:30:00\", account timezone)"`
	FoodID     string   `json:"food_id" jsonschema:"Food ID from get_custom_foods (GARMIN) or search_foods (FATSECRET)"`
	ServingID  string   `json:"serving_id" jsonschema:"Serving ID from get_custom_foods or search_foods"`
	ServingQty *float64 `json:"serving_qty,omitempty" jsonschema:"Number of servings (default 1)"`
	Source     string   `json:"source,omitempty" jsonschema:"Food namespace — \"GARMIN\" (default) or \"FATSECRET\""`
}

type logFoodInput struct {
	Account
	MealDate string  `json:"meal_date" jsonschema:"Date in YYYY-MM-DD format"`
	MealTime string  `json:"meal_time" jsonschema:"Time in HH:MM:SS format (account timezone)"`
	Name     string  `json:"name" jsonschema:"Display name for the food entry"`
	Calories float64 `json:"calories" jsonschema:"Calories (kcal)"`
	Carbs    float64 `json:"carbs" jsonschema:"Carbohydrates in grams"`
	Protein  float64 `json:"protein" jsonschema:"Protein in grams"`
	Fat      float64 `json:"fat" jsonschema:"Fat in grams"`
}

type deleteFoodLogInput struct {
	Account
	LogID    string `json:"log_id" jsonschema:"Log entry ID to delete — a 32-char hex UUID (from get_nutrition_daily_food_log)"`
	MealDate string `json:"meal_date" jsonschema:"Date of the log entry in YYYY-MM-DD format"`
	Confirm  bool   `json:"confirm,omitempty" jsonschema:"Set true to delete after reviewing the default preview"`
}

type upsertAndLogInput struct {
	Account
	MealDate      string   `json:"meal_date" jsonschema:"Date in YYYY-MM-DD format"`
	MealTime      string   `json:"meal_time" jsonschema:"Time in HH:MM:SS format (account timezone); used to determine the meal automatically"`
	FoodName      string   `json:"food_name" jsonschema:"Name of the food to find or create"`
	Calories      float64  `json:"calories" jsonschema:"Calories per serving (new food only)"`
	Carbs         *float64 `json:"carbs,omitempty" jsonschema:"Carbohydrates in grams per serving (new food only)"`
	Protein       *float64 `json:"protein,omitempty" jsonschema:"Protein in grams per serving (new food only)"`
	Fat           *float64 `json:"fat,omitempty" jsonschema:"Total fat in grams per serving (new food only)"`
	BrandName     *string  `json:"brand_name,omitempty" jsonschema:"Brand or vendor name (new food only)"`
	ServingUnit   string   `json:"serving_unit,omitempty" jsonschema:"Unit for serving size (e.g. \"G\", \"ML\", \"OZ\"). Default \"G\" (new food only)"`
	NumberOfUnits *float64 `json:"number_of_units,omitempty" jsonschema:"Serving size in the specified unit. Default 100 (new food only)"`
	ServingQty    *float64 `json:"serving_qty,omitempty" jsonschema:"Number of servings to log (default 1)"`
}

func registerNutritionTools(r *registry) {
	addTool(r, "get_nutrition_daily_food_log", nutritionFoodLogDescription, getNutritionDailyFoodLog)
	addTool(r, "get_nutrition_summary_between_dates", nutritionSummaryDescription, getNutritionSummaryBetweenDates)
	addTool(r, "get_nutrition_daily_meals", nutritionMealsDescription, getNutritionDailyMeals)
	addTool(r, "get_nutrition_daily_settings", nutritionSettingsDescription, getNutritionDailySettings)
	addTool(r, "set_nutrition_daily_settings", setNutritionSettingsDescription, setNutritionDailySettings)
	addTool(r, "search_foods", searchFoodsDescription, searchFoods)
	addTool(r, "get_custom_foods", getCustomFoodsDescription, getCustomFoods)
	addTool(r, "get_custom_food_serving_units", getCustomFoodServingUnitsDescription, getCustomFoodServingUnits)
	addTool(r, "create_custom_food", createCustomFoodDescription, createCustomFood)
	addTool(r, "update_custom_food", updateCustomFoodDescription, updateCustomFood)
	addTool(r, "delete_custom_food", deleteCustomFoodDescription, deleteCustomFood)
	addTool(r, "log_custom_food", logCustomFoodDescription, logCustomFood)
	addTool(r, "log_food", logFoodDescription, logFood)
	addTool(r, "delete_food_log", deleteFoodLogDescription, deleteFoodLog)
	addTool(r, "upsert_and_log", upsertAndLogDescription, upsertAndLog)
}

// --- helpers ---

// numToStr formats a number as Garmin's integer string when whole, matching
// Python's _num_to_str.
func numToStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1<<53 {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return strconv.FormatInt(n, 10)
		}
		if f, err := x.Float64(); err == nil {
			return numToStr(f)
		}
	}
	return fmt.Sprint(v)
}

func validISODate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

func validMealTime(s string) bool {
	if len(s) != 8 || s[2] != ':' || s[5] != ':' {
		return false
	}
	_, err := time.Parse("15:04:05", s)
	return err == nil
}

// validateMealDateTime mirrors _validate_meal_datetime.
func validateMealDateTime(mealDate, mealTime string) error {
	if !validISODate(mealDate) {
		return errors.New("meal_date must be a valid date in YYYY-MM-DD format")
	}
	if !validMealTime(mealTime) {
		return errors.New("meal_time must be a valid time in HH:MM:SS format")
	}
	return nil
}

// resolveWeightGoalTargetDate mirrors the Python helper: at most one return is
// non-empty; both empty means the stored value is valid and left untouched.
func resolveWeightGoalTargetDate(storedTarget any, date string, targetDate *string) (string, string) {
	if targetDate != nil {
		candidate := *targetDate
		if !validISODate(candidate) {
			return "", fmt.Sprintf("Invalid target_date %s. Use YYYY-MM-DD.", pythonRepr(candidate))
		}
		if candidate <= date {
			return "", fmt.Sprintf("target_date (%s) must be after date (%s); Garmin requires the weight goal's target to be later than the day being written.", candidate, date)
		}
		return candidate, ""
	}
	if truthy(storedTarget) && fmt.Sprint(storedTarget) <= date {
		return "", fmt.Sprintf("Cannot update nutrition settings for %s: the weight goal's targetDate (%v) is not after %s, and Garmin rejects the whole settings update in that case.\nEither set a new target date on the weight goal in Garmin Connect (or end the goal) and retry, or pass target_date=YYYY-MM-DD (after %s) to move the weight goal's target date as part of this update.", date, storedTarget, date, date)
	}
	return "", ""
}

func customFoodUnsupported(err error) bool {
	var api *garmin.APIError
	if !errors.As(err, &api) || api.Status != http.StatusForbidden {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, pattern := range []string{
		"custom food unsupported",
		"custom foods unsupported",
		"custom food not supported",
		"custom foods not supported",
	} {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}

func customFoodsPage(ctx context.Context, c *garmin.Client, search string, start int) ([]any, error) {
	data, err := c.GetCustomFoods(ctx, search, start, customFoodPageSize)
	if err != nil {
		return nil, err
	}
	return asList(asMap(data)["customFoods"]), nil
}

func customFoodIDString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// findCustomFood ports _find_custom_food: search by name first, then page the
// whole library because a renamed food no longer matches the stored name.
func findCustomFood(ctx context.Context, c *garmin.Client, foodID, foodName string) (map[string]any, error) {
	for _, search := range []string{foodName, ""} {
		for start := 0; start < customFoodMaxScan; start += customFoodPageSize {
			foods, err := customFoodsPage(ctx, c, search, start)
			if err != nil {
				return nil, err
			}
			for _, item := range foods {
				food := asMap(item)
				if food == nil {
					continue
				}
				meta := asMap(food["foodMetaData"])
				if meta == nil {
					meta = food
				}
				if customFoodIDString(meta["foodId"]) == foodID {
					return food, nil
				}
			}
			if len(foods) < customFoodPageSize {
				break
			}
		}
	}
	return nil, nil
}

// findCustomFoodByName ports _find_custom_food_by_name. Exhausting the bounded
// scan is an error: creating then could duplicate a record on a later page.
func findCustomFoodByName(ctx context.Context, c *garmin.Client, foodName string) (map[string]any, error) {
	for start := 0; start < customFoodMaxScan; start += customFoodPageSize {
		foods, err := customFoodsPage(ctx, c, foodName, start)
		if err != nil {
			return nil, err
		}
		for _, item := range foods {
			food := asMap(item)
			if food == nil {
				continue
			}
			meta := asMap(food["foodMetaData"])
			if meta == nil {
				meta = food
			}
			name, _ := meta["foodName"].(string)
			if strings.EqualFold(name, foodName) {
				return food, nil
			}
		}
		if len(foods) < customFoodPageSize {
			return nil, nil
		}
	}
	return nil, errCustomFoodSearchLimit
}

// customFoodIDs ports _custom_food_ids.
func customFoodIDs(food map[string]any) (string, string) {
	if food == nil {
		return "", ""
	}
	meta := asMap(food["foodMetaData"])
	if meta == nil {
		meta = food
	}
	var serving map[string]any
	if contents := asList(food["nutritionContents"]); len(contents) > 0 {
		serving = asMap(contents[0])
	}
	var foodID any
	if id := meta["foodId"]; truthy(id) {
		foodID = id
	} else {
		foodID = food["foodId"]
	}
	var servingID any
	if serving != nil {
		servingID = serving["servingId"]
	}
	return customFoodIDString(foodID), customFoodIDString(servingID)
}

func resolveMealID(meals []any, mealTime string) (any, bool) {
	for _, item := range meals {
		m := asMap(item)
		start, sok := m["startTime"].(string)
		end, eok := m["endTime"].(string)
		if sok && eok && start != "" && end != "" && start <= mealTime && mealTime <= end {
			return m["mealId"], true
		}
	}
	for _, item := range meals {
		m := asMap(item)
		if m["mealName"] == "SNACKS" {
			return m["mealId"], true
		}
	}
	return nil, false
}

func findFoodLogEntry(data any, logID string) map[string]any {
	switch v := data.(type) {
	case map[string]any:
		if v["logId"] != nil && fmt.Sprint(v["logId"]) == logID {
			return v
		}
		for _, value := range v {
			if entry := findFoodLogEntry(value, logID); entry != nil {
				return entry
			}
		}
	case []any:
		for _, value := range v {
			if entry := findFoodLogEntry(value, logID); entry != nil {
				return entry
			}
		}
	}
	return nil
}

func logTimestamp() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05") + ".000Z"
}

type nutritionField struct {
	key string
	val *float64
}

func customFoodNutrientFields(in createCustomFoodInput) []nutritionField {
	return []nutritionField{
		{"carbs", in.Carbs},
		{"protein", in.Protein},
		{"fat", in.Fat},
		{"fiber", in.Fiber},
		{"sugar", in.Sugar},
		{"saturatedFat", in.SaturatedFat},
		{"sodium", in.Sodium},
		{"cholesterol", in.Cholesterol},
		{"potassium", in.Potassium},
		{"transFat", in.TransFat},
		{"calcium", in.Calcium},
		{"iron", in.Iron},
		{"vitaminD", in.VitaminD},
	}
}

func customFoodUpdateFields(in updateCustomFoodInput) []nutritionField {
	return []nutritionField{
		{"carbs", in.Carbs},
		{"protein", in.Protein},
		{"fat", in.Fat},
		{"fiber", in.Fiber},
		{"sugar", in.Sugar},
		{"saturatedFat", in.SaturatedFat},
		{"sodium", in.Sodium},
		{"cholesterol", in.Cholesterol},
		{"potassium", in.Potassium},
		{"transFat", in.TransFat},
		{"calcium", in.Calcium},
		{"iron", in.Iron},
		{"vitaminD", in.VitaminD},
	}
}

// --- readers ---

func getNutritionDailyFoodLog(ctx context.Context, s *Session, in nutritionDateInput) (any, error) {
	data, err := s.Client.GetNutritionFoodLog(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving food log data: %w", err)
	}
	if !truthy(data) {
		return fmt.Sprintf("No food log data found for %s.", in.Date), nil
	}
	return data, nil
}

func getNutritionSummaryBetweenDates(ctx context.Context, s *Session, in nutritionSummaryInput) (any, error) {
	start, err := time.Parse("2006-01-02", in.StartDate)
	if err != nil {
		return fmt.Sprintf("Invalid date format: %v. Use YYYY-MM-DD.", err), nil
	}
	end, err := time.Parse("2006-01-02", in.EndDate)
	if err != nil {
		return fmt.Sprintf("Invalid date format: %v. Use YYYY-MM-DD.", err), nil
	}
	const maxDays = 61
	days := int(end.Sub(start).Hours()/24) + 1
	if days < 1 {
		return "end_date must be on or after start_date.", nil
	}
	if days > maxDays {
		return fmt.Sprintf("Date range too large (%d days). Maximum is %d days.", days, maxDays), nil
	}

	data, err := s.Client.GetNutritionSummary(ctx, in.StartDate, in.EndDate)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving nutrition summary: %w", err)
	}
	summaries := asList(asMap(data)["dailyNutritionSummaries"])
	if len(summaries) == 0 {
		return fmt.Sprintf("No nutrition data found between %s and %s.", in.StartDate, in.EndDate), nil
	}

	daily := make([]any, 0, len(summaries))
	for _, item := range summaries {
		day := asMap(item)
		content := asMap(day["dailyNutritionContent"])
		itemCount := 0
		for _, mealItem := range asList(day["mealDetails"]) {
			itemCount += len(asList(asMap(mealItem)["loggedFoods"]))
		}
		daily = append(daily, map[string]any{
			"date":       day["mealDate"],
			"calories":   content["calories"],
			"carbs":      content["carbs"],
			"protein":    content["protein"],
			"fat":        content["fat"],
			"item_count": itemCount,
		})
	}
	return map[string]any{
		"start_date": in.StartDate,
		"end_date":   in.EndDate,
		"days":       daily,
	}, nil
}

func getNutritionDailyMeals(ctx context.Context, s *Session, in nutritionDateInput) (any, error) {
	data, err := s.Client.GetNutritionMeals(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving meal data: %w", err)
	}
	if !truthy(data) {
		return fmt.Sprintf("No meal data found for %s.", in.Date), nil
	}
	return data, nil
}

func getNutritionDailySettings(ctx context.Context, s *Session, in nutritionDateInput) (any, error) {
	data, err := s.Client.GetNutritionSettings(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving nutrition settings: %w", err)
	}
	if !truthy(data) {
		return fmt.Sprintf("No nutrition settings found for %s.", in.Date), nil
	}
	return data, nil
}

func searchFoods(ctx context.Context, s *Session, in searchFoodsInput) (any, error) {
	limit := 20
	if in.Limit != nil {
		limit = *in.Limit
	}
	data, err := s.Client.SearchFoods(ctx, in.Query, in.Start, limit)
	if err != nil {
		return nil, fmt.Errorf("Error searching foods: %w", err)
	}
	if !truthy(data) {
		return "No foods found.", nil
	}

	rawResults := asList(asMap(data)["results"])
	hasMore := truthy(asMap(data)["moreDataAvailable"])
	results := make([]any, 0, len(rawResults))
	for _, item := range rawResults {
		entryMap := asMap(item)
		if entryMap == nil {
			continue
		}
		meta, ok := entryMap["foodMetaData"].(map[string]any)
		if !ok {
			continue
		}
		servings := make([]any, 0)
		for _, servingItem := range asList(entryMap["nutritionContents"]) {
			serving, ok := servingItem.(map[string]any)
			if !ok {
				continue
			}
			servings = append(servings, pruneNil(map[string]any{
				"serving_id":      serving["servingId"],
				"serving_unit":    serving["servingUnit"],
				"number_of_units": serving["numberOfUnits"],
				"calories":        serving["calories"],
				"carbs_g":         serving["carbs"],
				"protein_g":       serving["protein"],
				"fat_g":           serving["fat"],
				"fiber_g":         serving["fiber"],
				"sodium_mg":       serving["sodium"],
			}))
		}
		entry := map[string]any{
			"food_id":   meta["foodId"],
			"name":      meta["foodName"],
			"food_type": meta["foodType"],
			"source":    meta["source"],
			"region":    meta["regionCode"],
			"language":  meta["languageCode"],
			"servings":  servings,
		}
		if truthy(meta["brandName"]) {
			entry["brand"] = meta["brandName"]
		}
		results = append(results, pruneNil(entry))
	}
	return map[string]any{
		"count":    len(results),
		"has_more": hasMore,
		"results":  results,
	}, nil
}

func getCustomFoods(ctx context.Context, s *Session, in getCustomFoodsInput) (any, error) {
	limit := 20
	if in.Limit != nil {
		limit = *in.Limit
	}
	data, err := s.Client.GetCustomFoods(ctx, in.Search, in.Start, limit)
	if err != nil {
		if customFoodUnsupported(err) {
			return "No custom foods found; Garmin custom food API returned HTTP 403 Forbidden.", nil
		}
		return nil, fmt.Errorf("Error retrieving custom foods: %w", err)
	}
	if !truthy(data) {
		return "No custom foods found.", nil
	}
	return data, nil
}

func getCustomFoodServingUnits(ctx context.Context, s *Session, in customFoodServingUnitsInput) (any, error) {
	data, err := s.Client.GetCustomFoodServingUnits(ctx)
	if err != nil {
		if customFoodUnsupported(err) {
			return "No serving units found; Garmin custom food API returned HTTP 403 Forbidden.", nil
		}
		return nil, fmt.Errorf("Error retrieving serving units: %w", err)
	}
	if !truthy(data) {
		return "No serving units found.", nil
	}
	return data, nil
}

// --- writes ---

func createCustomFood(ctx context.Context, s *Session, in createCustomFoodInput) (any, error) {
	tracker := newWriteTracker("create_custom_food", s.logWriter()).
		withPrefix("Error creating custom food")

	servingUnit := in.ServingUnit
	if servingUnit == "" {
		servingUnit = "G"
	}
	numberOfUnits := 100.0
	if in.NumberOfUnits != nil {
		numberOfUnits = *in.NumberOfUnits
	}
	nutrition := map[string]any{
		"servingUnit":   servingUnit,
		"numberOfUnits": numToStr(numberOfUnits),
		"calories":      numToStr(in.Calories),
	}
	for _, field := range customFoodNutrientFields(in) {
		if field.val != nil {
			nutrition[field.key] = numToStr(*field.val)
		}
	}

	foodMeta := map[string]any{
		"foodName":     in.FoodName,
		"foodType":     "GENERIC",
		"source":       "GARMIN",
		"regionCode":   "US",
		"languageCode": "en",
	}
	if in.BrandName != nil {
		foodMeta["brandName"] = *in.BrandName
	}
	payload := map[string]any{
		"foodMetaData":      foodMeta,
		"nutritionContents": []any{nutrition},
	}

	tracker.Sent()
	resp, err := s.Client.PutCustomFood(ctx, payload)
	if err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	if !truthy(resp) {
		return "Custom food created (no response data returned).", nil
	}
	return resp, nil
}

func updateCustomFood(ctx context.Context, s *Session, in updateCustomFoodInput) (any, error) {
	tracker := newWriteTracker("update_custom_food", s.logWriter()).
		withPrefix("Error updating custom food")

	existing, err := findCustomFood(ctx, s.Client, in.FoodID, in.FoodName)
	if err != nil {
		existing = nil
	}
	if existing == nil && (in.ServingUnit == nil || in.NumberOfUnits == nil) {
		return fmt.Sprintf("Error updating custom food: could not read the current record for food_id %s, so omitted fields cannot be preserved. Check the food_id with get_custom_foods, or pass serving_unit and number_of_units (plus every nutrient and brand to keep) to replace the record in full.", in.FoodID), nil
	}

	existingNutrition := map[string]any{}
	if contents := asList(existing["nutritionContents"]); len(contents) > 0 {
		if first := asMap(contents[0]); first != nil {
			existingNutrition = first
		}
	}
	var existingBrand any
	if existing != nil {
		existingBrand = asMap(existing["foodMetaData"])["brandName"]
	}

	effectiveServingUnit := "G"
	if v, ok := existingNutrition["servingUnit"]; ok && v != nil {
		effectiveServingUnit = fmt.Sprint(v)
	}
	if in.ServingUnit != nil {
		effectiveServingUnit = *in.ServingUnit
	}
	var effectiveNumberOfUnits any = 100.0
	if v, ok := existingNutrition["numberOfUnits"]; ok && v != nil {
		effectiveNumberOfUnits = v
	}
	if in.NumberOfUnits != nil {
		effectiveNumberOfUnits = *in.NumberOfUnits
	}

	nutrition := map[string]any{
		"servingId":     in.ServingID,
		"servingUnit":   effectiveServingUnit,
		"numberOfUnits": numToStr(effectiveNumberOfUnits),
		"calories":      numToStr(in.Calories),
	}

	updateFields := customFoodUpdateFields(in)
	preserved := map[string]bool{}
	for _, field := range updateFields {
		preserved[field.key] = true
	}
	for key, value := range existingNutrition {
		if preserved[key] && value != nil {
			nutrition[key] = numToStr(value)
		}
	}
	for _, field := range updateFields {
		if field.val != nil {
			nutrition[field.key] = numToStr(*field.val)
		}
	}

	var effectiveBrand any
	if in.BrandName != nil {
		effectiveBrand = *in.BrandName
	} else {
		effectiveBrand = existingBrand
	}

	foodMeta := map[string]any{
		"foodId":       in.FoodID,
		"foodName":     in.FoodName,
		"foodType":     "GENERIC",
		"source":       "GARMIN",
		"regionCode":   "US",
		"languageCode": "en",
	}
	if effectiveBrand != nil {
		foodMeta["brandName"] = effectiveBrand
	}
	payload := map[string]any{
		"foodMetaData":      foodMeta,
		"nutritionContents": []any{nutrition},
	}

	tracker.Sent()
	resp, err := s.Client.PutCustomFood(ctx, payload)
	if err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	if !truthy(resp) {
		return "Custom food updated (no response data returned).", nil
	}
	return resp, nil
}

func deleteCustomFood(ctx context.Context, s *Session, in deleteCustomFoodInput) (any, error) {
	tracker := newWriteTracker("delete_custom_food", s.logWriter()).
		withPrefix("Error deleting custom food")

	if !in.Confirm {
		food, err := findCustomFood(ctx, s.Client, in.FoodID, "")
		if err != nil {
			return nil, tracker.Fail(err)
		}
		if food == nil {
			return map[string]any{"status": "not_found", "food_id": in.FoodID}, nil
		}
		return map[string]any{
			"status":  "needs_confirmation",
			"food_id": in.FoodID,
			"food":    food,
			"message": "Review this food, then call again with confirm=true to permanently delete it.",
		}, nil
	}

	tracker.Sent()
	if _, err := s.Client.DeleteCustomFood(ctx, in.FoodID); err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	return map[string]any{
		"status":  "success",
		"food_id": in.FoodID,
		"message": fmt.Sprintf("Custom food %s deleted successfully.", in.FoodID),
	}, nil
}

func logCustomFood(ctx context.Context, s *Session, in logCustomFoodInput) (any, error) {
	tracker := newWriteTracker("log_custom_food", s.logWriter()).
		withPrefix("Error logging food")

	if err := validateMealDateTime(in.MealDate, in.MealTime); err != nil {
		return nil, tracker.Fail(err)
	}
	meals, err := s.Client.GetNutritionMeals(ctx, in.MealDate)
	if err != nil {
		return nil, tracker.Fail(err)
	}
	mealID, ok := resolveMealID(asList(asMap(meals)["meals"]), in.MealTime)
	if !ok {
		return fmt.Sprintf("No nutrition meal windows found for %s; cannot log custom food at '%s'.", in.MealDate, in.MealTime), nil
	}

	servingQty := 1.0
	if in.ServingQty != nil {
		servingQty = *in.ServingQty
	}
	source := in.Source
	if source == "" {
		source = "GARMIN"
	}
	payload := map[string]any{
		"mealDate": in.MealDate,
		"foodLogItems": []any{map[string]any{
			"logTimestamp": logTimestamp(),
			"logSource":    "GCW",
			"logCategory":  "REGULAR_LOG",
			"mealTime":     in.MealTime,
			"action":       "ADD",
			"mealId":       mealID,
			"foodId":       in.FoodID,
			"servingId":    in.ServingID,
			"source":       source,
			"regionCode":   "US",
			"languageCode": "en",
			"servingQty":   servingQty,
		}},
	}

	tracker.Sent()
	resp, err := s.Client.LogFood(ctx, payload)
	if err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	if !truthy(resp) {
		return "Food logged successfully.", nil
	}
	return resp, nil
}

func logFood(ctx context.Context, s *Session, in logFoodInput) (any, error) {
	tracker := newWriteTracker("log_food", s.logWriter()).
		withPrefix("Error logging food")

	if err := validateMealDateTime(in.MealDate, in.MealTime); err != nil {
		return nil, tracker.Fail(err)
	}
	meals, err := s.Client.GetNutritionMeals(ctx, in.MealDate)
	if err != nil {
		return nil, tracker.Fail(err)
	}
	mealID, ok := resolveMealID(asList(asMap(meals)["meals"]), in.MealTime)
	if !ok {
		return fmt.Sprintf("No nutrition meal windows found for %s; cannot log food at '%s'.", in.MealDate, in.MealTime), nil
	}

	payload := map[string]any{
		"mealDate": in.MealDate,
		"quickAddItems": []any{map[string]any{
			"name":         in.Name,
			"logId":        nil,
			"logTimestamp": logTimestamp(),
			"logSource":    "GCW",
			"logCategory":  "QUICK_ADD",
			"mealTime":     in.MealTime,
			"mealId":       mealID,
			"action":       "ADD",
			"calories":     numToStr(in.Calories),
			"carbs":        numToStr(in.Carbs),
			"protein":      numToStr(in.Protein),
			"fat":          numToStr(in.Fat),
		}},
	}

	tracker.Sent()
	resp, err := s.Client.LogQuickFood(ctx, payload)
	if err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	if !truthy(resp) {
		return "Food logged successfully.", nil
	}
	return resp, nil
}

func deleteFoodLog(ctx context.Context, s *Session, in deleteFoodLogInput) (any, error) {
	tracker := newWriteTracker("delete_food_log", s.logWriter()).
		withPrefix("Error deleting food log")

	if !in.Confirm {
		data, err := s.Client.GetNutritionFoodLog(ctx, in.MealDate)
		if err != nil {
			return nil, tracker.Fail(err)
		}
		entry := findFoodLogEntry(data, in.LogID)
		if entry == nil {
			return map[string]any{"status": "not_found", "log_id": in.LogID, "meal_date": in.MealDate}, nil
		}
		return map[string]any{
			"status":    "needs_confirmation",
			"log_id":    in.LogID,
			"meal_date": in.MealDate,
			"entry":     entry,
			"message":   "Review this entry, then call again with confirm=true to permanently delete it.",
		}, nil
	}

	tracker.Sent()
	if _, err := s.Client.DeleteFoodLog(ctx, in.MealDate, in.LogID); err != nil {
		var api *garmin.APIError
		if errors.As(err, &api) && api.Status == http.StatusNotFound {
			return map[string]any{
				"status":  "not_found",
				"log_id":  in.LogID,
				"message": fmt.Sprintf("Food log entry %s was not found.", in.LogID),
			}, nil
		}
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	return map[string]any{
		"status":  "success",
		"log_id":  in.LogID,
		"message": fmt.Sprintf("Food log entry %s deleted successfully.", in.LogID),
	}, nil
}

func setNutritionDailySettings(ctx context.Context, s *Session, in setNutritionSettingsInput) (any, error) {
	if in.CalorieGoal == nil && in.CarbsGrams == nil && in.FatGrams == nil && in.ProteinGrams == nil {
		return "No fields to update — supply at least one of calorie_goal, carbs_grams, fat_grams, protein_grams.", nil
	}
	tracker := newWriteTracker("set_nutrition_daily_settings", s.logWriter()).
		withPrefix("Error updating nutrition settings")

	currentData, err := s.Client.GetNutritionSettings(ctx, in.Date)
	if err != nil {
		return nil, tracker.Fail(err)
	}
	if !truthy(currentData) {
		return fmt.Sprintf("Could not read current nutrition settings for %s — cannot apply update.", in.Date), nil
	}
	current := asMap(currentData)
	if current == nil {
		return fmt.Sprintf("Could not read current nutrition settings for %s — cannot apply update.", in.Date), nil
	}

	newTarget, targetError := resolveWeightGoalTargetDate(current["targetDate"], in.Date, in.TargetDate)
	if targetError != "" {
		return targetError, nil
	}
	if newTarget != "" {
		current["targetDate"] = newTarget
	}
	if in.CalorieGoal != nil {
		current["calorieGoal"] = *in.CalorieGoal
	}

	if in.CarbsGrams != nil || in.FatGrams != nil || in.ProteinGrams != nil {
		rawMacros, exists := current["macroGoals"]
		var macros map[string]any
		if !exists || rawMacros == nil {
			macros = map[string]any{}
		} else {
			m, ok := rawMacros.(map[string]any)
			if !ok {
				return "Could not read current macro goals — cannot apply update.", nil
			}
			macros = m
		}
		if in.CarbsGrams != nil {
			macros["carbs"] = *in.CarbsGrams
		}
		if in.FatGrams != nil {
			macros["fat"] = *in.FatGrams
		}
		if in.ProteinGrams != nil {
			macros["protein"] = *in.ProteinGrams
		}
		current["macroGoals"] = macros
	}

	tracker.Sent()
	result, err := s.Client.PutNutritionSettings(ctx, in.Date, current)
	if err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	if !truthy(result) {
		stored, readErr := s.Client.GetNutritionSettings(ctx, in.Date)
		if readErr != nil {
			return fmt.Sprintf("Nutrition update submitted, but could not verify stored settings: %v", readErr), nil
		}
		result = stored
	}
	resultMap := asMap(result)
	if resultMap == nil || len(resultMap) == 0 {
		return "Nutrition update submitted, but could not verify stored settings.", nil
	}
	resultMacros := asMap(resultMap["macroGoals"])
	return map[string]any{
		"status":                  "updated",
		"date":                    in.Date,
		"calorie_goal":            resultMap["calorieGoal"],
		"carbs_grams":             resultMacros["carbs"],
		"fat_grams":               resultMacros["fat"],
		"protein_grams":           resultMacros["protein"],
		"weight_goal_target_date": resultMap["targetDate"],
	}, nil
}

func upsertAndLog(ctx context.Context, s *Session, in upsertAndLogInput) (any, error) {
	tracker := newWriteTracker("upsert_and_log", s.logWriter()).
		withPrefix("Error in upsert_and_log")
	created := false
	foodID, servingID := "", ""

	fail := func(err error) (any, error) {
		ferr := tracker.Fail(err)
		var wf *writeFailure
		if created && errors.As(ferr, &wf) {
			wf.payload["custom_food_created"] = true
			if foodID != "" {
				wf.payload["food_id"] = foodID
			}
			if wf.payload["write_committed"] == false {
				wf.payload["hint"] = "The custom food was created but not logged. Check the custom food library before retrying; reuse the existing food to log it."
			}
		}
		return nil, ferr
	}

	if err := validateMealDateTime(in.MealDate, in.MealTime); err != nil {
		return fail(err)
	}

	existing, err := findCustomFoodByName(ctx, s.Client, in.FoodName)
	if err != nil {
		return fail(err)
	}
	foodID, servingID = customFoodIDs(existing)
	if existing != nil && (foodID == "" || servingID == "") {
		return fail(errors.New("Existing custom food has no usable foodId/servingId; not creating a duplicate."))
	}

	if foodID == "" || servingID == "" {
		servingUnit := in.ServingUnit
		if servingUnit == "" {
			servingUnit = "G"
		}
		numberOfUnits := 100.0
		if in.NumberOfUnits != nil {
			numberOfUnits = *in.NumberOfUnits
		}
		nutrition := map[string]any{
			"servingUnit":   servingUnit,
			"numberOfUnits": numToStr(numberOfUnits),
			"calories":      numToStr(in.Calories),
		}
		for _, field := range []nutritionField{{"carbs", in.Carbs}, {"protein", in.Protein}, {"fat", in.Fat}} {
			if field.val != nil {
				nutrition[field.key] = numToStr(*field.val)
			}
		}
		foodMeta := map[string]any{
			"foodName":     in.FoodName,
			"foodType":     "GENERIC",
			"source":       "GARMIN",
			"regionCode":   "US",
			"languageCode": "en",
		}
		if in.BrandName != nil {
			foodMeta["brandName"] = *in.BrandName
		}
		createPayload := map[string]any{
			"foodMetaData":      foodMeta,
			"nutritionContents": []any{nutrition},
		}

		tracker.Sent()
		createResp, createErr := s.Client.PutCustomFood(ctx, createPayload)
		if createErr != nil {
			var syntax *json.SyntaxError
			if errors.As(createErr, &syntax) {
				// Garmin accepted the create; only the response body failed.
				created = true
				tracker = newWriteTracker("upsert_and_log", s.logWriter()).
					withPrefix("Error in upsert_and_log (custom food was created)")
			}
			return fail(createErr)
		}
		tracker.Committed()
		created = true
		tracker = newWriteTracker("upsert_and_log", s.logWriter()).
			withPrefix("Error in upsert_and_log (custom food was created)")

		foodID, servingID = customFoodIDs(asMap(createResp))
		if foodID == "" || servingID == "" {
			found, foundErr := findCustomFoodByName(ctx, s.Client, in.FoodName)
			if foundErr != nil {
				return fail(foundErr)
			}
			foundID, foundServing := customFoodIDs(found)
			if foundID != "" {
				foodID = foundID
			}
			if foundServing != "" {
				servingID = foundServing
			}
		}
		if foodID == "" || servingID == "" {
			return fail(fmt.Errorf("could not retrieve foodId/servingId for '%s' after creation", in.FoodName))
		}
	}

	prefix := "Error in upsert_and_log"
	if created {
		prefix = fmt.Sprintf("Error in upsert_and_log (custom food %s was created)", foodID)
	}
	tracker = newWriteTracker("upsert_and_log", s.logWriter()).withPrefix(prefix)

	meals, err := s.Client.GetNutritionMeals(ctx, in.MealDate)
	if err != nil {
		return fail(err)
	}
	mealID, ok := resolveMealID(asList(asMap(meals)["meals"]), in.MealTime)
	if !ok {
		message := fmt.Sprintf("No nutrition meal windows found for %s; cannot log food at '%s'.", in.MealDate, in.MealTime)
		if created {
			return fail(errors.New(message))
		}
		return message, nil
	}

	servingQty := 1.0
	if in.ServingQty != nil {
		servingQty = *in.ServingQty
	}
	logPayload := map[string]any{
		"mealDate": in.MealDate,
		"foodLogItems": []any{map[string]any{
			"logTimestamp": logTimestamp(),
			"logSource":    "GCW",
			"logCategory":  "REGULAR_LOG",
			"mealTime":     in.MealTime,
			"action":       "ADD",
			"mealId":       mealID,
			"foodId":       foodID,
			"servingId":    servingID,
			"source":       "GARMIN",
			"regionCode":   "US",
			"languageCode": "en",
			"servingQty":   servingQty,
		}},
	}

	tracker.Sent()
	logResp, err := s.Client.LogFood(ctx, logPayload)
	if err != nil {
		return fail(err)
	}
	tracker.Committed()
	if !truthy(logResp) {
		return "Food logged successfully.", nil
	}
	return logResp, nil
}
