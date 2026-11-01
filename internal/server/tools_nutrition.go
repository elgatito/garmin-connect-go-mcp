package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

// Port of garmin_mcp/nutrition.py: the read tools. Write tools are in tools_nutrition_write.go.

type searchFoodsIn struct {
	Account
	Query string `json:"query" jsonschema:"Food name or brand to search for (e.g. \"Cheerios\", \"Greek yogurt\")"`
	Start *int   `json:"start,omitempty" jsonschema:"Starting index for pagination (default 0)"`
	Limit *int   `json:"limit,omitempty" jsonschema:"Maximum number of results per page (default 20)"`
}

type customFoodsIn struct {
	Account
	Search *string `json:"search,omitempty" jsonschema:"Search term to filter foods by name (default: list all)"`
	Start  *int    `json:"start,omitempty" jsonschema:"Starting index for pagination (default 0)"`
	Limit  *int    `json:"limit,omitempty" jsonschema:"Maximum number of results (default 20)"`
}

func deref[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

// isCustomFoodUnsupported403 is _is_custom_food_unsupported_403: only Garmin's known unsupported-feature 403.
func isCustomFoodUnsupported403(err error) bool {
	var apiErr *garmin.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 403 {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, p := range []string{"custom food unsupported", "custom foods unsupported", "custom food not supported", "custom foods not supported"} {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

func registerNutritionTools(r *registry) {
	addDayTool(r, "get_nutrition_daily_food_log", foodLogDesc, "Error retrieving food log data", "No food log data found for %s.",
		(*garmin.Client).GetFoodLog, nil)
	addTool(r, "get_nutrition_summary_between_dates", nutritionSummaryDesc, getNutritionSummary)
	addDayTool(r, "get_nutrition_daily_meals", dailyMealsDesc, "Error retrieving meal data", "No meal data found for %s.",
		(*garmin.Client).GetMeals, nil)
	addDayTool(r, "get_nutrition_daily_settings", dailySettingsDesc, "Error retrieving nutrition settings", "No nutrition settings found for %s.",
		(*garmin.Client).GetNutritionSettings, nil)
	addTool(r, "search_foods", searchFoodsDesc, searchFoods)
	addTool(r, "get_custom_foods", customFoodsDesc, func(ctx context.Context, s *Session, in customFoodsIn) (any, error) {
		data, err := s.Client.GetCustomFoods(ctx, deref(in.Search, ""), deref(in.Start, 0), deref(in.Limit, 20))
		if err != nil {
			if isCustomFoodUnsupported403(err) {
				return "No custom foods found; Garmin custom food API returned HTTP 403 Forbidden.", nil
			}
			return nil, fmt.Errorf("Error retrieving custom foods: %w", err)
		}
		if falsy(data) {
			return "No custom foods found.", nil
		}
		return data, nil
	})
	addTool(r, "get_custom_food_serving_units", servingUnitsDesc, func(ctx context.Context, s *Session, in noArgs) (any, error) {
		data, err := s.Client.GetCustomFoodServingUnits(ctx)
		if err != nil {
			if isCustomFoodUnsupported403(err) {
				return "No serving units found; Garmin custom food API returned HTTP 403 Forbidden.", nil
			}
			return nil, fmt.Errorf("Error retrieving serving units: %w", err)
		}
		if falsy(data) {
			return "No serving units found.", nil
		}
		return data, nil
	})
	registerNutritionWriteTools(r)
}

const maxNutritionSummaryDays = 61

func getNutritionSummary(ctx context.Context, s *Session, in rangeIn) (any, error) {
	start, err := parseISODate(in.StartDate)
	var end = start
	if err == nil {
		end, err = parseISODate(in.EndDate)
	}
	if err != nil {
		return fmt.Sprintf("Invalid date format: %v. Use YYYY-MM-DD.", err), nil
	}
	days := daysBetween(start, end) + 1
	if days < 1 {
		return "end_date must be on or after start_date.", nil
	}
	if days > maxNutritionSummaryDays {
		return fmt.Sprintf("Date range too large (%d days). Maximum is %d days.", days, maxNutritionSummaryDays), nil
	}
	data, err := s.Client.GetFoodLogRange(ctx, in.StartDate, in.EndDate)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving nutrition summary: %w", err)
	}
	summaries := asList(asMap(data)["dailyNutritionSummaries"])
	if len(summaries) == 0 {
		return fmt.Sprintf("No nutrition data found between %s and %s.", in.StartDate, in.EndDate), nil
	}
	daily := []any{}
	for _, d := range summaries {
		day := asMap(d)
		content := asMap(day["dailyNutritionContent"])
		itemCount := 0
		for _, meal := range asList(day["mealDetails"]) {
			itemCount += len(asList(asMap(meal)["loggedFoods"]))
		}
		daily = append(daily, map[string]any{
			"date": day["mealDate"], "calories": content["calories"], "carbs": content["carbs"],
			"protein": content["protein"], "fat": content["fat"], "item_count": itemCount,
		})
	}
	return map[string]any{"start_date": in.StartDate, "end_date": in.EndDate, "days": daily}, nil
}

func searchFoods(ctx context.Context, s *Session, in searchFoodsIn) (any, error) {
	data, err := s.Client.SearchFoods(ctx, in.Query, deref(in.Start, 0), deref(in.Limit, 20))
	if err != nil {
		return nil, fmt.Errorf("Error searching foods: %w", err)
	}
	if falsy(data) {
		return "No foods found.", nil
	}
	m := asMap(data)
	results := []any{}
	for _, item := range asList(m["results"]) {
		meta := asMap(asMap(item)["foodMetaData"])
		if meta == nil {
			continue
		}
		servings := []any{}
		for _, sv := range asList(asMap(item)["nutritionContents"]) {
			if sm := asMap(sv); sm != nil {
				servings = append(servings, dropNil(pick(sm, "serving_id", "servingId", "serving_unit", "servingUnit",
					"number_of_units", "numberOfUnits", "calories", "calories", "carbs_g", "carbs", "protein_g", "protein",
					"fat_g", "fat", "fiber_g", "fiber", "sodium_mg", "sodium")))
			}
		}
		entry := pick(meta, "food_id", "foodId", "name", "foodName", "food_type", "foodType", "source", "source",
			"region", "regionCode", "language", "languageCode")
		entry["servings"] = servings
		if !falsy(meta["brandName"]) {
			entry["brand"] = meta["brandName"]
		}
		results = append(results, dropNil(entry))
	}
	return map[string]any{"count": len(results), "has_more": !falsy(m["moreDataAvailable"]), "results": results}, nil
}
