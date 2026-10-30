package server

import (
	"context"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
)

func nutritionString(v any) (string, error) {
	if str, ok := v.(string); ok {
		return str, nil
	}
	return numString(v)
}
func registerNutritionUpdate(srv *mcp.Server, m *manager) {
	schema := map[string]any{"food_id": map[string]any{"type": "string"}, "serving_id": map[string]any{"type": "string"}, "food_name": map[string]any{"type": "string"}, "calories": map[string]any{"type": "number"}, "serving_unit": map[string]any{"type": "string"}, "number_of_units": map[string]any{"type": "number"}, "brand_name": map[string]any{"type": "string"}}
	optional := map[string]string{"carbs": "carbs", "protein": "protein", "fat": "fat", "fiber": "fiber", "sugar": "sugar", "saturated_fat": "saturatedFat", "sodium": "sodium", "cholesterol": "cholesterol", "potassium": "potassium", "trans_fat": "transFat", "calcium": "calcium", "iron": "iron", "vitamin_d": "vitaminD"}
	for k := range optional {
		schema[k] = map[string]any{"type": "number"}
	}
	addTool(srv, m, "update_custom_food", description("update_custom_food"), schema, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		food, ok := args["food_id"].(string)
		if !ok || food == "" {
			return nil, fmt.Errorf("food_id is required")
		}
		serving, ok := args["serving_id"].(string)
		if !ok || serving == "" {
			return nil, fmt.Errorf("serving_id is required")
		}
		name, ok := args["food_name"].(string)
		if !ok {
			return nil, fmt.Errorf("food_name is required")
		}
		cal, err := numString(args["calories"])
		if err != nil {
			return nil, fmt.Errorf("calories is required")
		}
		existing, _ := findCustomFood(ctx, s, food, name)
		if existing == nil && (args["serving_unit"] == nil || args["number_of_units"] == nil) {
			return fmt.Sprintf("Error updating custom food: could not read the current record for food_id %s, so omitted fields cannot be preserved. Check the food_id with get_custom_foods, or pass serving_unit and number_of_units (plus every nutrient and brand to keep) to replace the record in full.", food), nil
		}
		metaExisting, _ := existing["foodMetaData"].(map[string]any)
		contents, _ := existing["nutritionContents"].([]any)
		nutritionExisting := map[string]any{}
		if len(contents) > 0 {
			nutritionExisting, _ = contents[0].(map[string]any)
		}
		unit := args["serving_unit"]
		if unit == nil {
			unit = nutritionExisting["servingUnit"]
			if unit == nil {
				unit = "G"
			}
		}
		quantity := args["number_of_units"]
		if quantity == nil {
			quantity = nutritionExisting["numberOfUnits"]
			if quantity == nil {
				quantity = float64(100)
			}
		}
		number, err := nutritionString(quantity)
		if err != nil {
			return nil, err
		}
		nutrition := map[string]any{"servingId": serving, "servingUnit": unit, "numberOfUnits": number, "calories": cal}
		for key, field := range optional {
			value := nutritionExisting[field]
			if args[key] != nil {
				value = args[key]
			}
			if value != nil {
				str, err := nutritionString(value)
				if err != nil {
					return nil, fmt.Errorf("invalid %s", key)
				}
				nutrition[field] = str
			}
		}
		meta := map[string]any{"foodId": food, "foodName": name, "foodType": "GENERIC", "source": "GARMIN", "regionCode": "US", "languageCode": "en"}
		brand := args["brand_name"]
		if brand == nil {
			brand = metaExisting["brandName"]
		}
		if brand != nil {
			meta["brandName"] = brand
		}
		tracker := newWriteTracker("update_custom_food", "Error updating custom food", m.opt.Log)
		tracker.Sent()
		v, err := s.Client.DoJSON(ctx, garmin.Req{Method: http.MethodPut, Path: "/nutrition-service/customFood", JSON: map[string]any{"foodMetaData": meta, "nutritionContents": []any{nutrition}}})
		if err != nil {
			return nil, tracker.Fail(err)
		}
		tracker.Committed()
		if empty(v) {
			return "Custom food updated (no response data returned).", nil
		}
		return v, nil
	}, "food_id", "serving_id", "food_name", "calories")
}
