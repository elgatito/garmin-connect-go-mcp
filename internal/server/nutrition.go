package server

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func customFoodUnsupported(err error) bool {
	var api *garmin.APIError
	if !errors.As(err, &api) || api.Status != 403 {
		return false
	}
	message := strings.ToLower(api.Message)
	for _, pattern := range []string{"custom food unsupported", "custom foods unsupported", "custom food not supported", "custom foods not supported"} {
		if strings.Contains(message, pattern) {
			return true
		}
	}
	return false
}

func registerNutrition(srv *mcp.Server, m *manager) {
	for _, item := range []struct{ name, path, label string }{{"get_nutrition_daily_food_log", "food/logs/", "food log data"}, {"get_nutrition_daily_meals", "meals/", "meal data"}, {"get_nutrition_daily_settings", "settings/", "nutrition settings"}} {
		item := item
		addTool(srv, m, item.name, description(item.name), map[string]any{"date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
			date, err := dateArgument(args, "date")
			if err != nil {
				return nil, fmt.Errorf("Error retrieving %s: %w", item.label, err)
			}
			v, err := s.Client.GetJSON(ctx, "/nutrition-service/"+item.path+date, nil)
			if err != nil {
				return nil, fmt.Errorf("Error retrieving %s: %w", item.label, err)
			}
			if empty(v) {
				return fmt.Sprintf("No %s found for %s.", item.label, date), nil
			}
			return v, nil
		}, "date")
	}
	addTool(srv, m, "get_custom_food_serving_units", description("get_custom_food_serving_units"), nil, func(ctx context.Context, s *Session, _ map[string]any) (any, error) {
		v, err := s.Client.GetJSON(ctx, "/nutrition-service/metadata/customFoodServingUnits", nil)
		if customFoodUnsupported(err) {
			return "No serving units found; Garmin custom food API returned HTTP 403 Forbidden.", nil
		}
		if err != nil {
			return nil, fmt.Errorf("Error retrieving serving units: %w", err)
		}
		if empty(v) {
			return "No serving units found.", nil
		}
		return v, nil
	})
	addTool(srv, m, "get_custom_foods", description("get_custom_foods"), map[string]any{"search": map[string]any{"type": "string", "default": ""}, "start": map[string]any{"type": "integer", "default": 0}, "limit": map[string]any{"type": "integer", "default": 20}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		start, err := intArg(args, "start", 0)
		if err != nil {
			return nil, err
		}
		limit, err := intArg(args, "limit", 20)
		if err != nil {
			return nil, err
		}
		search, _ := args["search"].(string)
		v, err := s.Client.GetJSON(ctx, "/nutrition-service/customFood", url.Values{"searchExpression": {search}, "start": {strconv.Itoa(start)}, "limit": {strconv.Itoa(limit)}, "includeContent": {"true"}})
		if customFoodUnsupported(err) {
			return "No custom foods found; Garmin custom food API returned HTTP 403 Forbidden.", nil
		}
		if err != nil {
			return nil, fmt.Errorf("Error retrieving custom foods: %w", err)
		}
		if empty(v) {
			return "No custom foods found.", nil
		}
		return v, nil
	})
	addTool(srv, m, "get_nutrition_summary_between_dates", description("get_nutrition_summary_between_dates"), map[string]any{"start_date": map[string]any{"type": "string"}, "end_date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		start, err := dateArgument(args, "start_date")
		if err != nil {
			return nil, fmt.Errorf("Invalid date format: %w. Use YYYY-MM-DD.", err)
		}
		end, err := dateArgument(args, "end_date")
		if err != nil {
			return nil, fmt.Errorf("Invalid date format: %w. Use YYYY-MM-DD.", err)
		}
		a, _ := time.Parse("2006-01-02", start)
		b, _ := time.Parse("2006-01-02", end)
		days := int(b.Sub(a).Hours()/24) + 1
		if days < 1 {
			return "end_date must be on or after start_date.", nil
		}
		if days > 61 {
			return fmt.Sprintf("Date range too large (%d days). Maximum is 61 days.", days), nil
		}
		v, err := s.Client.GetJSON(ctx, "/nutrition-service/food/logs/range", url.Values{"startDate": {start}, "endDate": {end}})
		if err != nil {
			return nil, fmt.Errorf("Error retrieving nutrition summary: %w", err)
		}
		raw, _ := v.(map[string]any)
		summaries, _ := raw["dailyNutritionSummaries"].([]any)
		if len(summaries) == 0 {
			return fmt.Sprintf("No nutrition data found between %s and %s.", start, end), nil
		}
		out := []any{}
		for _, v := range summaries {
			day, ok := v.(map[string]any)
			if !ok {
				continue
			}
			content, _ := day["dailyNutritionContent"].(map[string]any)
			meals, _ := day["mealDetails"].([]any)
			count := 0
			for _, v := range meals {
				meal, _ := v.(map[string]any)
				foods, _ := meal["loggedFoods"].([]any)
				count += len(foods)
			}
			out = append(out, map[string]any{"date": day["mealDate"], "calories": content["calories"], "carbs": content["carbs"], "protein": content["protein"], "fat": content["fat"], "item_count": count})
		}
		return map[string]any{"start_date": start, "end_date": end, "days": out}, nil
	}, "start_date", "end_date")
	addTool(srv, m, "search_foods", description("search_foods"), map[string]any{"query": map[string]any{"type": "string"}, "start": map[string]any{"type": "integer", "default": 0}, "limit": map[string]any{"type": "integer", "default": 20}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		query, ok := args["query"].(string)
		if !ok {
			return nil, fmt.Errorf("Error searching foods: query is required")
		}
		start, err := intArg(args, "start", 0)
		if err != nil {
			return nil, fmt.Errorf("Error searching foods: %w", err)
		}
		limit, err := intArg(args, "limit", 20)
		if err != nil {
			return nil, fmt.Errorf("Error searching foods: %w", err)
		}
		v, err := s.Client.GetJSON(ctx, "/nutrition-service/food/search", url.Values{"searchExpression": {query}, "start": {strconv.Itoa(start)}, "limit": {strconv.Itoa(limit)}})
		if err != nil {
			return nil, fmt.Errorf("Error searching foods: %w", err)
		}
		if empty(v) {
			return "No foods found.", nil
		}
		data, _ := v.(map[string]any)
		raw, _ := data["results"].([]any)
		results := []any{}
		for _, v := range raw {
			item, ok := v.(map[string]any)
			if !ok {
				continue
			}
			meta, ok := item["foodMetaData"].(map[string]any)
			if !ok {
				continue
			}
			servings := []any{}
			if values, ok := item["nutritionContents"].([]any); ok {
				for _, v := range values {
					s, ok := v.(map[string]any)
					if !ok {
						continue
					}
					entry := map[string]any{}
					for k, source := range map[string]string{"serving_id": "servingId", "serving_unit": "servingUnit", "number_of_units": "numberOfUnits", "calories": "calories", "carbs_g": "carbs", "protein_g": "protein", "fat_g": "fat", "fiber_g": "fiber", "sodium_mg": "sodium"} {
						if s[source] != nil {
							entry[k] = s[source]
						}
					}
					servings = append(servings, entry)
				}
			}
			entry := map[string]any{"servings": servings}
			for k, source := range map[string]string{"food_id": "foodId", "name": "foodName", "food_type": "foodType", "source": "source", "region": "regionCode", "language": "languageCode"} {
				if meta[source] != nil {
					entry[k] = meta[source]
				}
			}
			if brand, ok := meta["brandName"].(string); ok && brand != "" {
				entry["brand"] = brand
			}
			results = append(results, entry)
		}
		more, _ := data["moreDataAvailable"].(bool)
		return map[string]any{"count": len(results), "has_more": more, "results": results}, nil
	}, "query")

}
