package server

import (
	"context"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"strconv"
	"time"
)

func numString(v any) (string, error) {
	n, ok := v.(float64)
	if !ok {
		return "", fmt.Errorf("expected a number")
	}
	return strconv.FormatFloat(n, 'f', -1, 64), nil
}
func registerNutritionWrites(srv *mcp.Server, m *manager) {
	schema := map[string]any{"food_name": map[string]any{"type": "string"}, "calories": map[string]any{"type": "number"}, "serving_unit": map[string]any{"type": "string", "default": "G"}, "number_of_units": map[string]any{"type": "number", "default": 100}, "brand_name": map[string]any{"type": "string"}}
	optional := map[string]string{"carbs": "carbs", "protein": "protein", "fat": "fat", "fiber": "fiber", "sugar": "sugar", "saturated_fat": "saturatedFat", "sodium": "sodium", "cholesterol": "cholesterol", "potassium": "potassium", "trans_fat": "transFat", "calcium": "calcium", "iron": "iron", "vitamin_d": "vitaminD"}
	for k := range optional {
		schema[k] = map[string]any{"type": "number"}
	}
	addTool(srv, m, "create_custom_food", description("create_custom_food"), schema, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		name, ok := args["food_name"].(string)
		if !ok {
			return nil, fmt.Errorf("food_name is required")
		}
		cal, err := numString(args["calories"])
		if err != nil {
			return nil, fmt.Errorf("calories is required")
		}
		unit := "G"
		if v, ok := args["serving_unit"].(string); ok {
			unit = v
		}
		qty := "100"
		if v, ok := args["number_of_units"]; ok {
			qty, err = numString(v)
			if err != nil {
				return nil, err
			}
		}
		nutrition := map[string]any{"servingUnit": unit, "numberOfUnits": qty, "calories": cal}
		for key, target := range optional {
			if v := args[key]; v != nil {
				n, err := numString(v)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", key, err)
				}
				nutrition[target] = n
			}
		}
		meta := map[string]any{"foodName": name, "foodType": "GENERIC", "source": "GARMIN", "regionCode": "US", "languageCode": "en"}
		if v := args["brand_name"]; v != nil {
			meta["brandName"] = v
		}
		tracker := newWriteTracker("create_custom_food", "Error creating custom food", m.opt.Log)
		tracker.Sent()
		v, err := s.Client.DoJSON(ctx, garmin.Req{Method: http.MethodPut, Path: "/nutrition-service/customFood", JSON: map[string]any{"foodMetaData": meta, "nutritionContents": []any{nutrition}}})
		if err != nil {
			return nil, tracker.Fail(err)
		}
		tracker.Committed()
		if empty(v) {
			return "Custom food created (no response data returned).", nil
		}
		return v, nil
	}, "food_name", "calories")
	for _, item := range []struct {
		name   string
		custom bool
	}{{"log_food", false}, {"log_custom_food", true}} {
		item := item
		schema := map[string]any{"meal_date": map[string]any{"type": "string"}, "meal_time": map[string]any{"type": "string"}}
		required := []string{"meal_date", "meal_time"}
		if item.custom {
			schema["food_id"] = map[string]any{"type": "string"}
			schema["serving_id"] = map[string]any{"type": "string"}
			schema["serving_qty"] = map[string]any{"type": "number", "default": 1}
			schema["source"] = map[string]any{"type": "string", "default": "GARMIN"}
			required = append(required, "food_id", "serving_id")
		} else {
			schema["name"] = map[string]any{"type": "string"}
			required = append(required, "name")
			for _, key := range []string{"calories", "carbs", "protein", "fat"} {
				schema[key] = map[string]any{"type": "number"}
				required = append(required, key)
			}
		}
		addTool(srv, m, item.name, description(item.name), schema, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
			date, err := dateArgument(args, "meal_date")
			if err != nil {
				return nil, err
			}
			mealTime, ok := args["meal_time"].(string)
			if !ok {
				return nil, fmt.Errorf("meal_time is required")
			}
			parsed, err := time.Parse("15:04:05", mealTime)
			if err != nil || parsed.Format("15:04:05") != mealTime {
				return nil, fmt.Errorf("meal_time must be in HH:MM:SS format")
			}
			entry := map[string]any{"logTimestamp": time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), "logSource": "GCW", "mealTime": mealTime, "action": "ADD"}
			if item.custom {
				food, ok := args["food_id"].(string)
				if !ok {
					return nil, fmt.Errorf("food_id is required")
				}
				serving, ok := args["serving_id"].(string)
				if !ok {
					return nil, fmt.Errorf("serving_id is required")
				}
				qty := any(float64(1))
				if v := args["serving_qty"]; v != nil {
					if _, ok := v.(float64); !ok {
						return nil, fmt.Errorf("serving_qty must be a number")
					}
					qty = v
				}
				source := "GARMIN"
				if v, ok := args["source"].(string); ok {
					source = v
				}
				entry["logCategory"] = "REGULAR_LOG"
				entry["foodId"] = food
				entry["servingId"] = serving
				entry["source"] = source
				entry["regionCode"] = "US"
				entry["languageCode"] = "en"
				entry["servingQty"] = qty
			} else {
				name, ok := args["name"].(string)
				if !ok {
					return nil, fmt.Errorf("name is required")
				}
				entry["name"] = name
				entry["logId"] = nil
				entry["logCategory"] = "QUICK_ADD"
				for _, key := range []string{"calories", "carbs", "protein", "fat"} {
					n, err := numString(args[key])
					if err != nil {
						return nil, fmt.Errorf("%s is required", key)
					}
					entry[key] = n
				}
			}
			tracker := newWriteTracker(item.name, "Error logging food", m.opt.Log)
			v, err := s.Client.GetJSON(ctx, "/nutrition-service/meals/"+date, nil)
			if err != nil {
				return nil, tracker.Fail(err)
			}
			data, _ := v.(map[string]any)
			meals, _ := data["meals"].([]any)
			var mealID any
			for _, v := range meals {
				meal, _ := v.(map[string]any)
				start, _ := meal["startTime"].(string)
				end, _ := meal["endTime"].(string)
				if start != "" && end != "" && start <= mealTime && mealTime <= end {
					mealID = meal["mealId"]
					break
				}
			}
			if mealID == nil {
				for _, v := range meals {
					meal, _ := v.(map[string]any)
					if meal["mealName"] == "SNACKS" {
						mealID = meal["mealId"]
						break
					}
				}
			}
			if mealID == nil {
				kind := "food"
				if item.custom {
					kind = "custom food"
				}
				return fmt.Sprintf("No nutrition meal windows found for %s; cannot log %s at '%s'.", date, kind, mealTime), nil
			}
			entry["mealId"] = mealID
			path := "/nutrition-service/food/logs/quickAdd"
			field := "quickAddItems"
			if item.custom {
				path = "/nutrition-service/food/logs"
				field = "foodLogItems"
			}
			tracker.Sent()
			v, err = s.Client.DoJSON(ctx, garmin.Req{Method: http.MethodPut, Path: path, JSON: map[string]any{"mealDate": date, field: []any{entry}}})
			if err != nil {
				return nil, tracker.Fail(err)
			}
			tracker.Committed()
			if empty(v) {
				return "Food logged successfully.", nil
			}
			return v, nil
		}, required...)
	}
}
