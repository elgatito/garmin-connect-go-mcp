package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

// Port of garmin_mcp/nutrition.py: the write tools.

const (
	customFoodPageSize = 20
	customFoodMaxScan  = 500
)

type setSettingsIn struct {
	Account
	Date         string  `json:"date" jsonschema:"Date in YYYY-MM-DD format (settings are typically set once and inherited across days, but Garmin accepts per-day overrides)"`
	CalorieGoal  *int    `json:"calorie_goal,omitempty" jsonschema:"Daily calorie target in kcal"`
	CarbsGrams   *int    `json:"carbs_grams,omitempty" jsonschema:"Daily carbohydrate target in grams"`
	FatGrams     *int    `json:"fat_grams,omitempty" jsonschema:"Daily fat target in grams"`
	ProteinGrams *int    `json:"protein_grams,omitempty" jsonschema:"Daily protein target in grams"`
	TargetDate   *string `json:"target_date,omitempty" jsonschema:"Optional new target date (YYYY-MM-DD) for the weight goal that this endpoint carries. Only needed when the stored target date has already passed, which otherwise makes Garmin reject the update; must be after date."`
}

// foodNutrients are the optional per-serving amounts shared by create_custom_food and update_custom_food.
type foodNutrients struct {
	Carbs        *float64 `json:"carbs,omitempty" jsonschema:"Carbohydrates in grams per serving"`
	Protein      *float64 `json:"protein,omitempty" jsonschema:"Protein in grams per serving"`
	Fat          *float64 `json:"fat,omitempty" jsonschema:"Total fat in grams per serving"`
	Fiber        *float64 `json:"fiber,omitempty" jsonschema:"Fiber in grams per serving"`
	Sugar        *float64 `json:"sugar,omitempty" jsonschema:"Sugar in grams per serving"`
	SaturatedFat *float64 `json:"saturated_fat,omitempty" jsonschema:"Saturated fat in grams per serving"`
	Sodium       *float64 `json:"sodium,omitempty" jsonschema:"Sodium in mg per serving"`
	Cholesterol  *float64 `json:"cholesterol,omitempty" jsonschema:"Cholesterol in mg per serving"`
	Potassium    *float64 `json:"potassium,omitempty" jsonschema:"Potassium in mg per serving"`
	TransFat     *float64 `json:"trans_fat,omitempty" jsonschema:"Trans fat in grams per serving"`
	Calcium      *float64 `json:"calcium,omitempty" jsonschema:"Calcium in mg per serving (NOT %DV)"`
	Iron         *float64 `json:"iron,omitempty" jsonschema:"Iron in mg per serving (NOT %DV)"`
	VitaminD     *float64 `json:"vitamin_d,omitempty" jsonschema:"Vitamin D in mcg per serving (NOT %DV)"`
}

// byAPIName is Python's optional_fields dict: Garmin's field name -> the supplied value (nil when not given).
func (n foodNutrients) byAPIName() map[string]*float64 {
	return map[string]*float64{
		"carbs": n.Carbs, "protein": n.Protein, "fat": n.Fat, "fiber": n.Fiber, "sugar": n.Sugar, "saturatedFat": n.SaturatedFat,
		"sodium": n.Sodium, "cholesterol": n.Cholesterol, "potassium": n.Potassium, "transFat": n.TransFat,
		"calcium": n.Calcium, "iron": n.Iron, "vitaminD": n.VitaminD,
	}
}

type createCustomFoodIn struct {
	Account
	FoodName      string   `json:"food_name" jsonschema:"Name of the custom food (e.g. \"Homemade Chocolate Cookies\")"`
	Calories      float64  `json:"calories" jsonschema:"Calories per serving"`
	ServingUnit   *string  `json:"serving_unit,omitempty" jsonschema:"Unit for serving size (e.g. \"G\", \"ML\", \"OZ\"). Default \"G\""`
	NumberOfUnits *float64 `json:"number_of_units,omitempty" jsonschema:"Serving size in the specified unit. Default 100"`
	BrandName     *string  `json:"brand_name,omitempty" jsonschema:"Brand or vendor name (e.g. \"Three Bridges\")"`
	foodNutrients
}

type updateCustomFoodIn struct {
	Account
	FoodID        string   `json:"food_id" jsonschema:"ID of the custom food to update (from get_custom_foods)"`
	ServingID     string   `json:"serving_id" jsonschema:"Serving ID of the food (from get_custom_foods)"`
	FoodName      string   `json:"food_name" jsonschema:"Name of the custom food"`
	Calories      float64  `json:"calories" jsonschema:"Calories per serving"`
	ServingUnit   *string  `json:"serving_unit,omitempty" jsonschema:"Unit for serving size (e.g. \"G\", \"ML\", \"OZ\"); omit to preserve the existing value"`
	NumberOfUnits *float64 `json:"number_of_units,omitempty" jsonschema:"Serving size in the specified unit; omit to preserve the existing value"`
	BrandName     *string  `json:"brand_name,omitempty" jsonschema:"Brand or vendor name; omit to preserve the existing value"`
	foodNutrients
}

type deleteCustomFoodIn struct {
	Account
	FoodID  string `json:"food_id" jsonschema:"ID of the custom food to delete — a 32-char hex string (from get_custom_foods or create_custom_food)"`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"Set true to delete after reviewing the default preview"`
}

type logCustomFoodIn struct {
	Account
	MealDate   string   `json:"meal_date" jsonschema:"Date in YYYY-MM-DD format"`
	MealTime   string   `json:"meal_time" jsonschema:"Time in HH:MM:SS format (e.g. \"12:30:00\", account timezone)"`
	FoodID     string   `json:"food_id" jsonschema:"Food ID from get_custom_foods (GARMIN) or search_foods (FATSECRET)"`
	ServingID  string   `json:"serving_id" jsonschema:"Serving ID from get_custom_foods or search_foods"`
	ServingQty *float64 `json:"serving_qty,omitempty" jsonschema:"Number of servings (default 1)"`
	Source     *string  `json:"source,omitempty" jsonschema:"Food namespace — \"GARMIN\" (default) or \"FATSECRET\""`
}

type logFoodIn struct {
	Account
	MealDate string  `json:"meal_date" jsonschema:"Date in YYYY-MM-DD format"`
	MealTime string  `json:"meal_time" jsonschema:"Time in HH:MM:SS format (account timezone)"`
	Name     string  `json:"name" jsonschema:"Display name for the food entry"`
	Calories float64 `json:"calories" jsonschema:"Calories (kcal)"`
	Carbs    float64 `json:"carbs" jsonschema:"Carbohydrates in grams"`
	Protein  float64 `json:"protein" jsonschema:"Protein in grams"`
	Fat      float64 `json:"fat" jsonschema:"Fat in grams"`
}

type deleteFoodLogIn struct {
	Account
	LogID    string `json:"log_id" jsonschema:"Log entry ID to delete — a 32-char hex UUID (from get_nutrition_daily_food_log)"`
	MealDate string `json:"meal_date" jsonschema:"Date of the log entry in YYYY-MM-DD format"`
	Confirm  bool   `json:"confirm,omitempty" jsonschema:"Set true to delete after reviewing the default preview"`
}

type upsertAndLogIn struct {
	Account
	MealDate      string   `json:"meal_date" jsonschema:"Date in YYYY-MM-DD format"`
	MealTime      string   `json:"meal_time" jsonschema:"Time in HH:MM:SS format (account timezone); used to determine the meal automatically"`
	FoodName      string   `json:"food_name" jsonschema:"Name of the food to find or create"`
	Calories      float64  `json:"calories" jsonschema:"Calories per serving (new food only)"`
	Carbs         *float64 `json:"carbs,omitempty" jsonschema:"Carbohydrates in grams per serving (new food only)"`
	Protein       *float64 `json:"protein,omitempty" jsonschema:"Protein in grams per serving (new food only)"`
	Fat           *float64 `json:"fat,omitempty" jsonschema:"Total fat in grams per serving (new food only)"`
	BrandName     *string  `json:"brand_name,omitempty" jsonschema:"Brand or vendor name (new food only)"`
	ServingUnit   *string  `json:"serving_unit,omitempty" jsonschema:"Unit for serving size (e.g. \"G\", \"ML\", \"OZ\"). Default \"G\" (new food only)"`
	NumberOfUnits *float64 `json:"number_of_units,omitempty" jsonschema:"Serving size in the specified unit. Default 100 (new food only)"`
	ServingQty    *float64 `json:"serving_qty,omitempty" jsonschema:"Number of servings to log (default 1)"`
}

func registerNutritionWriteTools(r *registry) {
	addTool(r, "set_nutrition_daily_settings", setSettingsDesc, setNutritionSettings)
	addTool(r, "create_custom_food", createCustomFoodDesc, createCustomFood)
	addTool(r, "update_custom_food", updateCustomFoodDesc, updateCustomFood)
	addTool(r, "delete_custom_food", deleteCustomFoodDesc, deleteCustomFood)
	addTool(r, "log_custom_food", logCustomFoodDesc, logCustomFood)
	addTool(r, "log_food", logFoodDesc, logFood)
	addTool(r, "delete_food_log", deleteFoodLogDesc, deleteFoodLog)
	addTool(r, "upsert_and_log", upsertAndLogDesc, upsertAndLog)
}

// fnum is _num_to_str for a float64 (which cannot fail).
func fnum(f float64) string {
	s, _ := numToStr(f)
	return s
}

// isHTTPStatus reports whether err is a Garmin API error with this status.
func isHTTPStatus(err error, status int) bool {
	var apiErr *garmin.APIError
	return errors.As(err, &apiErr) && apiErr.Status == status
}

// isJSONDecode reports whether err is a response body that was not JSON (requests' JSONDecodeError).
func isJSONDecode(err error) bool {
	tr := newWriteTracker("", "", nil)
	stage, _, _ := tr.classify(err)
	return stage == "response_parse"
}

// ── custom food lookups ─────────────────────────────────────────────────────

func customFoodPage(ctx context.Context, c *garmin.Client, search string, start int) ([]any, error) {
	data, err := c.GetCustomFoods(ctx, search, start, customFoodPageSize)
	if err != nil {
		return nil, err
	}
	return asList(asMap(data)["customFoods"]), nil
}

// findCustomFood is _find_custom_food: the stored custom food with foodID, or nil. Garmin has no read-by-ID, so it
// searches by foodName first and then pages through the unfiltered list (after a rename the new name does not match
// the stored record).
func findCustomFood(ctx context.Context, c *garmin.Client, foodID, foodName string) (map[string]any, error) {
	for _, search := range []string{foodName, ""} {
		for start := 0; start < customFoodMaxScan; start += customFoodPageSize {
			foods, err := customFoodPage(ctx, c, search, start)
			if err != nil {
				return nil, err
			}
			for _, f := range foods {
				food := asMap(f)
				if food == nil {
					continue
				}
				if id, ok := asMap(food["foodMetaData"])["foodId"]; pyStrOr(id, ok, "") == foodID {
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

// pyStrOr is str(m.get(key, def)).
func pyStrOr(v any, present bool, def string) string {
	if !present {
		return def
	}
	return pyStrValue(v)
}

// findCustomFoodByName is _find_custom_food_by_name: an exact (case-insensitive) name across the substring-search pages,
// or nil. When the bounded scan is exhausted it refuses to guess that the food is absent.
func findCustomFoodByName(ctx context.Context, c *garmin.Client, foodName string) (map[string]any, error) {
	for start := 0; start < customFoodMaxScan; start += customFoodPageSize {
		foods, err := customFoodPage(ctx, c, foodName, start)
		if err != nil {
			return nil, err
		}
		for _, f := range foods {
			food := asMap(f)
			if food == nil {
				continue
			}
			meta := asMap(firstTruthy(food["foodMetaData"], food))
			if name, _ := meta["foodName"].(string); strings.EqualFold(name, foodName) {
				return food, nil
			}
		}
		if len(foods) < customFoodPageSize {
			return nil, nil
		}
	}
	return nil, valueError("Custom food search limit reached; cannot safely determine whether the food exists.")
}

// resolveMealID matches mealTime against each meal's startTime/endTime window and falls back to the SNACKS meal.
// ok is false when the day has neither.
func resolveMealID(ctx context.Context, c *garmin.Client, mealDate, mealTime string) (mealID any, ok bool, err error) {
	data, err := c.GetMeals(ctx, mealDate)
	if err != nil {
		return nil, false, err
	}
	meals := asList(asMap(data)["meals"])
	id := func(m map[string]any) (any, error) {
		v, has := m["mealId"]
		if !has {
			return nil, valueError("'mealId'") // Python's KeyError text
		}
		return v, nil
	}
	for _, mv := range meals {
		m := asMap(mv)
		start, _ := m["startTime"].(string)
		end, _ := m["endTime"].(string)
		if start != "" && end != "" && start <= mealTime && mealTime <= end {
			v, err := id(m)
			return v, err == nil, err
		}
	}
	for _, mv := range meals {
		if m := asMap(mv); m["mealName"] == "SNACKS" {
			v, err := id(m)
			return v, err == nil, err
		}
	}
	return nil, false, nil
}

// logTimestamp is strftime("%Y-%m-%dT%H:%M:%S.000Z"): the milliseconds are always zero.
func logTimestamp() string { return time.Now().UTC().Format("2006-01-02T15:04:05") + ".000Z" }

// foodLogPayload is the PUT /food/logs body for one regular (non-quick-add) item.
func foodLogPayload(mealDate, mealTime string, mealID any, foodID, servingID, source string, servingQty float64) map[string]any {
	return map[string]any{
		"mealDate": mealDate,
		"foodLogItems": []any{map[string]any{
			"logTimestamp": logTimestamp(), "logSource": "GCW", "logCategory": "REGULAR_LOG", "mealTime": mealTime, "action": "ADD",
			"mealId": mealID, "foodId": foodID, "servingId": servingID, "source": source, "regionCode": "US", "languageCode": "en",
			"servingQty": servingQty,
		}},
	}
}

// customFoodMeta is the foodMetaData of a new or replaced custom food.
func customFoodMeta(foodName string) map[string]any {
	return map[string]any{"foodName": foodName, "foodType": "GENERIC", "source": "GARMIN", "regionCode": "US", "languageCode": "en"}
}

// loggedOrResponse is the shared success rendering of the log tools.
func loggedOrResponse(resp any) any {
	if falsy(resp) {
		return "Food logged successfully."
	}
	return resp
}

// ── set_nutrition_daily_settings ────────────────────────────────────────────

func setNutritionSettings(ctx context.Context, s *Session, in setSettingsIn) (any, error) {
	if in.CalorieGoal == nil && in.CarbsGrams == nil && in.FatGrams == nil && in.ProteinGrams == nil {
		return "No fields to update — supply at least one of calorie_goal, carbs_grams, fat_grams, protein_grams.", nil
	}
	tr := newWriteTracker("set_nutrition_daily_settings", "Error updating nutrition settings", s.Log)
	date := in.Date
	current, err := s.Client.GetNutritionSettings(ctx, date)
	if err != nil {
		return nil, tr.Fail(err)
	}
	if falsy(current) {
		return fmt.Sprintf("Could not read current nutrition settings for %s — cannot apply update.", date), nil
	}
	cur, ok := current.(map[string]any)
	if !ok {
		return nil, tr.Fail(fmt.Errorf("'%s' object has no attribute 'get'", pyTypeName(current)))
	}
	newTarget, targetErr := resolveWeightGoalTargetDate(cur["targetDate"], date, in.TargetDate)
	if targetErr != "" {
		return targetErr, nil
	}
	if newTarget != "" {
		cur["targetDate"] = newTarget
	}
	if in.CalorieGoal != nil {
		cur["calorieGoal"] = *in.CalorieGoal
	}
	overrides := map[string]*int{"carbs": in.CarbsGrams, "fat": in.FatGrams, "protein": in.ProteinGrams}
	if in.CarbsGrams != nil || in.FatGrams != nil || in.ProteinGrams != nil {
		var macros map[string]any
		switch m := cur["macroGoals"].(type) {
		case nil:
			macros = map[string]any{}
		case map[string]any:
			macros = m
		default:
			return "Could not read current macro goals — cannot apply update.", nil
		}
		for k, v := range overrides {
			if v != nil {
				macros[k] = *v
			}
		}
		cur["macroGoals"] = macros
	}
	tr.Sent()
	resp, err := s.Client.PutNutritionSettings(ctx, date, cur)
	if err != nil {
		return nil, tr.Fail(err)
	}
	tr.Committed()
	result := resp
	if falsy(result) {
		if result, err = s.Client.GetNutritionSettings(ctx, date); err != nil {
			return fmt.Sprintf("Nutrition update submitted, but could not verify stored settings: %v", err), nil
		}
	}
	res, ok := result.(map[string]any)
	if !ok || len(res) == 0 {
		return "Nutrition update submitted, but could not verify stored settings.", nil
	}
	rawMacros := firstTruthy(res["macroGoals"], map[string]any{})
	macros, ok := rawMacros.(map[string]any)
	if !ok {
		return nil, tr.Fail(fmt.Errorf("'%s' object has no attribute 'get'", pyTypeName(rawMacros)))
	}
	return map[string]any{
		"status": "updated", "date": date, "calorie_goal": res["calorieGoal"], "carbs_grams": macros["carbs"],
		"fat_grams": macros["fat"], "protein_grams": macros["protein"], "weight_goal_target_date": res["targetDate"],
	}, nil
}

// ── custom food library ─────────────────────────────────────────────────────

func createCustomFood(ctx context.Context, s *Session, in createCustomFoodIn) (any, error) {
	tr := newWriteTracker("create_custom_food", "Error creating custom food", s.Log)
	nutrition := map[string]any{
		"servingUnit":   deref(in.ServingUnit, "G"),
		"numberOfUnits": fnum(deref(in.NumberOfUnits, 100)),
		"calories":      fnum(in.Calories),
	}
	for key, v := range in.byAPIName() {
		if v != nil {
			nutrition[key] = fnum(*v)
		}
	}
	meta := customFoodMeta(in.FoodName)
	if in.BrandName != nil {
		meta["brandName"] = *in.BrandName
	}
	payload := map[string]any{"foodMetaData": meta, "nutritionContents": []any{nutrition}}
	tr.Sent()
	resp, err := s.Client.PutCustomFood(ctx, payload)
	if err != nil {
		return nil, tr.Fail(err)
	}
	tr.Committed()
	if falsy(resp) {
		return "Custom food created (no response data returned).", nil
	}
	return resp, nil
}

func updateCustomFood(ctx context.Context, s *Session, in updateCustomFoodIn) (any, error) {
	tr := newWriteTracker("update_custom_food", "Error updating custom food", s.Log)
	// Fetch the current record so omitted fields are preserved (not wiped); a failed lookup counts as "not found".
	existing, err := findCustomFood(ctx, s.Client, in.FoodID, in.FoodName)
	if err != nil {
		existing = nil
	}
	if existing == nil && (in.ServingUnit == nil || in.NumberOfUnits == nil) {
		// The PUT is a full replace: writing without the stored record would silently wipe the brand, nutrients and serving size.
		return fmt.Sprintf("Error updating custom food: could not read the current record for food_id %s, so omitted fields cannot be preserved. "+
			"Check the food_id with get_custom_foods, or pass serving_unit and number_of_units (plus every nutrient and brand to keep) to "+
			"replace the record in full.", in.FoodID), nil
	}
	var existingNutrition map[string]any
	if contents := asList(existing["nutritionContents"]); len(contents) > 0 {
		existingNutrition = asMap(contents[0])
	}
	existingBrand := asMap(existing["foodMetaData"])["brandName"]

	// Effective serving basis: caller-supplied wins, else the stored one, else the create-time defaults.
	var servingUnit any = getDefault(existingNutrition, "servingUnit", "G")
	if in.ServingUnit != nil {
		servingUnit = *in.ServingUnit
	}
	var units any = getDefault(existingNutrition, "numberOfUnits", 100.0)
	if in.NumberOfUnits != nil {
		units = *in.NumberOfUnits
	}
	unitsStr, err := numToStr(units)
	if err != nil {
		return nil, tr.Fail(err)
	}
	nutrition := map[string]any{"servingId": in.ServingID, "servingUnit": servingUnit, "numberOfUnits": unitsStr, "calories": fnum(in.Calories)}
	updates := in.byAPIName()
	// Carry forward the stored optional fields, then overlay the caller's values.
	for key, v := range existingNutrition {
		if _, optional := updates[key]; optional && v != nil {
			str, err := numToStr(v)
			if err != nil {
				return nil, tr.Fail(err)
			}
			nutrition[key] = str
		}
	}
	for key, v := range updates {
		if v != nil {
			nutrition[key] = fnum(*v)
		}
	}
	meta := customFoodMeta(in.FoodName)
	meta["foodId"] = in.FoodID
	if in.BrandName != nil {
		meta["brandName"] = *in.BrandName
	} else if existingBrand != nil {
		meta["brandName"] = existingBrand
	}
	payload := map[string]any{"foodMetaData": meta, "nutritionContents": []any{nutrition}}
	tr.Sent()
	resp, err := s.Client.PutCustomFood(ctx, payload)
	if err != nil {
		return nil, tr.Fail(err)
	}
	tr.Committed()
	if falsy(resp) {
		return "Custom food updated (no response data returned).", nil
	}
	return resp, nil
}

func deleteCustomFood(ctx context.Context, s *Session, in deleteCustomFoodIn) (any, error) {
	tr := newWriteTracker("delete_custom_food", "Error deleting custom food", s.Log)
	if !in.Confirm {
		food, err := findCustomFood(ctx, s.Client, in.FoodID, "")
		if err != nil {
			return nil, tr.Fail(err)
		}
		if food == nil {
			return map[string]any{"status": "not_found", "food_id": in.FoodID}, nil
		}
		return map[string]any{
			"status": "needs_confirmation", "food_id": in.FoodID, "food": food,
			"message": "Review this food, then call again with confirm=true to permanently delete it.",
		}, nil
	}
	tr.Sent()
	if _, err := s.Client.DeleteCustomFood(ctx, in.FoodID); err != nil {
		return nil, tr.Fail(err)
	}
	tr.Committed()
	return map[string]any{"status": "success", "food_id": in.FoodID, "message": fmt.Sprintf("Custom food %s deleted successfully.", in.FoodID)}, nil
}

// ── food log ────────────────────────────────────────────────────────────────

func logCustomFood(ctx context.Context, s *Session, in logCustomFoodIn) (any, error) {
	tr := newWriteTracker("log_custom_food", "Error logging food", s.Log)
	if err := validateMealDatetime(in.MealDate, in.MealTime); err != nil {
		return nil, tr.Fail(err)
	}
	mealID, ok, err := resolveMealID(ctx, s.Client, in.MealDate, in.MealTime)
	if err != nil {
		return nil, tr.Fail(err)
	}
	if !ok {
		return fmt.Sprintf("No nutrition meal windows found for %s; cannot log custom food at '%s'.", in.MealDate, in.MealTime), nil
	}
	payload := foodLogPayload(in.MealDate, in.MealTime, mealID, in.FoodID, in.ServingID, deref(in.Source, "GARMIN"), deref(in.ServingQty, 1))
	tr.Sent()
	resp, err := s.Client.PutFoodLogs(ctx, payload)
	if err != nil {
		return nil, tr.Fail(err)
	}
	tr.Committed()
	return loggedOrResponse(resp), nil
}

func logFood(ctx context.Context, s *Session, in logFoodIn) (any, error) {
	tr := newWriteTracker("log_food", "Error logging food", s.Log)
	if err := validateMealDatetime(in.MealDate, in.MealTime); err != nil {
		return nil, tr.Fail(err)
	}
	mealID, ok, err := resolveMealID(ctx, s.Client, in.MealDate, in.MealTime)
	if err != nil {
		return nil, tr.Fail(err)
	}
	if !ok {
		return fmt.Sprintf("No nutrition meal windows found for %s; cannot log food at '%s'.", in.MealDate, in.MealTime), nil
	}
	payload := map[string]any{
		"mealDate": in.MealDate,
		"quickAddItems": []any{map[string]any{
			"name": in.Name, "logId": nil, "logTimestamp": logTimestamp(), "logSource": "GCW", "logCategory": "QUICK_ADD",
			"mealTime": in.MealTime, "mealId": mealID, "action": "ADD", "calories": fnum(in.Calories), "carbs": fnum(in.Carbs),
			"protein": fnum(in.Protein), "fat": fnum(in.Fat),
		}},
	}
	tr.Sent()
	resp, err := s.Client.PutQuickAdd(ctx, payload)
	if err != nil {
		return nil, tr.Fail(err)
	}
	tr.Committed()
	return loggedOrResponse(resp), nil
}

func deleteFoodLog(ctx context.Context, s *Session, in deleteFoodLogIn) (any, error) {
	tr := newWriteTracker("delete_food_log", "Error deleting food log", s.Log)
	fail := func(err error) (any, error) {
		if isHTTPStatus(err, http.StatusNotFound) {
			return map[string]any{"status": "not_found", "log_id": in.LogID, "message": fmt.Sprintf("Food log entry %s was not found.", in.LogID)}, nil
		}
		return nil, tr.Fail(err)
	}
	if !in.Confirm {
		data, err := s.Client.GetFoodLog(ctx, in.MealDate)
		if err != nil {
			return fail(err)
		}
		entry := findFoodLogEntry(data, in.LogID)
		if entry == nil {
			return map[string]any{"status": "not_found", "log_id": in.LogID, "meal_date": in.MealDate}, nil
		}
		return map[string]any{
			"status": "needs_confirmation", "log_id": in.LogID, "meal_date": in.MealDate, "entry": entry,
			"message": "Review this entry, then call again with confirm=true to permanently delete it.",
		}, nil
	}
	tr.Sent()
	if _, err := s.Client.DeleteFoodLog(ctx, in.MealDate, in.LogID); err != nil {
		return fail(err)
	}
	tr.Committed()
	return map[string]any{"status": "success", "log_id": in.LogID, "message": fmt.Sprintf("Food log entry %s deleted successfully.", in.LogID)}, nil
}

// ── upsert_and_log ──────────────────────────────────────────────────────────

func upsertAndLog(ctx context.Context, s *Session, in upsertAndLogIn) (any, error) {
	const prefix = "Error in upsert_and_log"
	tr := newWriteTracker("upsert_and_log", prefix, s.Log)
	created := false
	var foodID, servingID string
	// fail renders err with the tracker as it stands now; once the food exists the error says so.
	fail := func(err error) (any, error) {
		failure := tr.Fail(err)
		var wf *writeFailure
		if created && errors.As(failure, &wf) {
			wf.payload["custom_food_created"] = true
			if foodID != "" {
				wf.payload["food_id"] = foodID
			}
			if c, isBool := wf.payload["write_committed"].(bool); isBool && !c {
				wf.payload["hint"] = "The custom food was created but not logged. Check the custom food library before retrying; reuse the existing food to log it."
			}
		}
		return nil, failure
	}

	if err := validateMealDatetime(in.MealDate, in.MealTime); err != nil {
		return fail(err)
	}

	// 1. Exhaust the bounded name search before deciding to create.
	existing, err := findCustomFoodByName(ctx, s.Client, in.FoodName)
	if err != nil {
		return fail(err)
	}
	if foodID, servingID, err = customFoodIDs(existing); err != nil {
		return fail(err)
	}
	if existing != nil && (foodID == "" || servingID == "") {
		return fail(valueError("Existing custom food has no usable foodId/servingId; not creating a duplicate."))
	}

	// 2. Create if not found.
	if foodID == "" || servingID == "" {
		nutrition := map[string]any{
			"servingUnit": deref(in.ServingUnit, "G"), "numberOfUnits": fnum(deref(in.NumberOfUnits, 100)), "calories": fnum(in.Calories),
		}
		for key, v := range map[string]*float64{"carbs": in.Carbs, "protein": in.Protein, "fat": in.Fat} {
			if v != nil {
				nutrition[key] = fnum(*v)
			}
		}
		meta := customFoodMeta(in.FoodName)
		if in.BrandName != nil {
			meta["brandName"] = *in.BrandName
		}
		tr.Sent()
		createResp, err := s.Client.PutCustomFood(ctx, map[string]any{"foodMetaData": meta, "nutritionContents": []any{nutrition}})
		const createdPrefix = prefix + " (custom food was created)"
		if err != nil {
			if isJSONDecode(err) {
				// The body is decoded only after Garmin accepted the create: the food exists, but no log was sent.
				created = true
				tr = newWriteTracker("upsert_and_log", createdPrefix, s.Log)
			}
			return fail(err)
		}
		tr.Committed()
		created = true
		// Creation is committed, but the food log has not been sent: re-scope before parsing or looking up the response.
		tr = newWriteTracker("upsert_and_log", createdPrefix, s.Log)
		if foodID, servingID, err = customFoodIDs(createResp); err != nil {
			return fail(err)
		}
		if foodID == "" || servingID == "" {
			found, err := findCustomFoodByName(ctx, s.Client, in.FoodName)
			if err != nil {
				return fail(err)
			}
			fid, sid, err := customFoodIDs(found)
			if err != nil {
				return fail(err)
			}
			if fid != "" {
				foodID = fid
			}
			if sid != "" {
				servingID = sid
			}
		}
		if foodID == "" || servingID == "" {
			return fail(valueError(fmt.Sprintf("could not retrieve foodId/servingId for '%s' after creation", in.FoodName)))
		}
	}

	msg := prefix
	if created {
		msg = fmt.Sprintf("%s (custom food %s was created)", prefix, foodID)
	}
	tr = newWriteTracker("upsert_and_log", msg, s.Log)

	// 3. Resolve the meal from meal_time.
	mealID, ok, err := resolveMealID(ctx, s.Client, in.MealDate, in.MealTime)
	if err != nil {
		return fail(err)
	}
	if !ok {
		message := fmt.Sprintf("No nutrition meal windows found for %s; cannot log food at '%s'.", in.MealDate, in.MealTime)
		if created {
			return fail(valueError(message))
		}
		return message, nil
	}

	// 4. Log.
	tr.Sent()
	logResp, err := s.Client.PutFoodLogs(ctx, foodLogPayload(in.MealDate, in.MealTime, mealID, foodID, servingID, "GARMIN", deref(in.ServingQty, 1)))
	if err != nil {
		return fail(err)
	}
	tr.Committed()
	return loggedOrResponse(logResp), nil
}
