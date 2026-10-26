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
