package server

// Tool descriptions: the Python docstrings without their Args sections (the argument text is in the input structs).

const (
	foodLogDesc = `Get daily food consumption records for a date

Returns food items logged throughout the day including calories,
macronutrients, and meal associations.`

	nutritionSummaryDesc = `Get per-day nutrition totals for every day in a date range.

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

	dailyMealsDesc = `Get daily meal summaries for a date

Returns meal-level summaries (breakfast, lunch, dinner, snacks)
with nutritional totals for each meal. Each meal includes a mealId
needed for logging food items to that meal.`

	dailySettingsDesc = `Get nutrition plan/settings for a date

Returns the user's nutrition goals and targets including
calorie targets, macronutrient goals, and plan configuration.`

	searchFoodsDesc = `Search Garmin's general food catalog (FatSecret + Garmin custom foods)

Searches across the entire food catalog including FatSecret-sourced
branded and generic foods, not just the user's Garmin custom foods.
Use this to find branded packaged foods by name before logging them.

Returns food_id, source, name, brand, and all available servings with
macros. The source field ("FATSECRET" or "GARMIN") and food_id together
identify the right routing for log_custom_food — pass both to
log_custom_food's food_id and source parameters respectively.

For the user's own custom foods only, use get_custom_foods instead.`

	customFoodsDesc = `Search or list user's custom foods

Returns custom foods the user has created. Use the search parameter
to find existing foods by name before creating duplicates — the
response includes foodId and servingId needed for log_custom_food.

For branded catalog foods (FatSecret), use search_foods instead.`

	servingUnitsDesc = `Get available serving units for custom foods

Returns the list of valid serving units (e.g. G, ML, OZ)
that can be used when creating custom foods.`
)

const (
	setSettingsDesc = `Update daily nutrition goals (calorie target and macronutrient targets).

Reads the current settings for the date, applies the supplied overrides,
and writes the merged result back.  Only the fields you provide are
changed; omitted fields keep their existing values.

Macro arguments retain their existing names and are passed through to
Garmin's macroGoals without unit conversion. The endpoint's macro units
have not been independently verified. Returned goals come from Garmin's
response, or a settings read-back when the update has no response body.`

	createCustomFoodDesc = `Create a custom food in the user's Garmin nutrition library

Creates a new food item with nutritional information per serving.
On success the response includes foodId and servingId needed for
log_custom_food. If the API returns no data (204), use
get_custom_foods(search=food_name) to retrieve those IDs.

All nutrient amounts are ABSOLUTE values per serving, not %DV.
Nutrition labels often print %DV for calcium/iron/vitamin D —
convert to absolute units before passing.`

	updateCustomFoodDesc = `Update an existing custom food in the user's Garmin nutrition library

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

	deleteCustomFoodDesc = `Delete a custom food from the user's Garmin nutrition library

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

	logCustomFoodDesc = `Log a food item to a meal on a date

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

	logFoodDesc = `Quick-add a food entry with macro values to the nutrition log

Logs food directly by name and macros without requiring a food ID.
Uses Garmin's Quick Add feature. The meal is determined automatically
by matching meal_time against each meal's startTime/endTime window;
falls back to SNACKS if no window matches.`

	deleteFoodLogDesc = `Delete a food log entry

Permanently removes a logged food item from the nutrition log.
Works for both QUICK_ADD and REGULAR_LOG entry types.
Use get_nutrition_daily_food_log to find the logId and date. The default
call previews the matching entry; call again with confirm=true to delete.`

	upsertAndLogDesc = `Find-or-create a custom food then log it in one step

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
