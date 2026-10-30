package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func findFoodByName(ctx context.Context, s *Session, name string) (map[string]any, error) {
	for start := 0; start < 500; start += 20 {
		v, err := s.Client.GetJSON(ctx, "/nutrition-service/customFood", url.Values{"searchExpression": {name}, "start": {strconv.Itoa(start)}, "limit": {"20"}, "includeContent": {"true"}})
		if err != nil {
			return nil, err
		}
		data, _ := v.(map[string]any)
		foods, _ := data["customFoods"].([]any)
		for _, v := range foods {
			food, _ := v.(map[string]any)
			meta, _ := food["foodMetaData"].(map[string]any)
			if len(meta) == 0 {
				meta = food
			}
			stored, _ := meta["foodName"].(string)
			if strings.EqualFold(stored, name) {
				return food, nil
			}
		}
		if len(foods) < 20 {
			return nil, nil
		}
	}
	return nil, fmt.Errorf("Custom food search limit reached; cannot safely determine whether the food exists.")
}
func foodIDs(food map[string]any) (string, string) {
	if food == nil {
		return "", ""
	}
	meta, _ := food["foodMetaData"].(map[string]any)
	if len(meta) == 0 {
		meta = food
	}
	id := meta["foodId"]
	if !truthy(id) {
		id = food["foodId"]
	}
	contents, _ := food["nutritionContents"].([]any)
	var serving any
	if len(contents) > 0 {
		m, _ := contents[0].(map[string]any)
		serving = m["servingId"]
	}
	a, b := "", ""
	if id != nil {
		a = fmt.Sprint(id)
	}
	if serving != nil {
		b = fmt.Sprint(serving)
	}
	return a, b
}
func registerNutritionUpsert(srv *mcp.Server, m *manager) {
	schema := map[string]any{"meal_date": map[string]any{"type": "string"}, "meal_time": map[string]any{"type": "string"}, "food_name": map[string]any{"type": "string"}, "calories": map[string]any{"type": "number"}, "brand_name": map[string]any{"type": "string"}, "serving_unit": map[string]any{"type": "string", "default": "G"}, "number_of_units": map[string]any{"type": "number", "default": 100}, "serving_qty": map[string]any{"type": "number", "default": 1}}
	for _, k := range []string{"carbs", "protein", "fat"} {
		schema[k] = map[string]any{"type": "number"}
	}
	addTool(srv, m, "upsert_and_log", description("upsert_and_log"), schema, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		tracker := newWriteTracker("upsert_and_log", "Error in upsert_and_log", m.opt.Log)
		created := false
		foodID := ""
		fail := func(err error) (any, error) {
			failure := tracker.Fail(err)
			if f, ok := failure.(*writeFailure); ok && created {
				f.payload["custom_food_created"] = true
				if foodID != "" {
					f.payload["food_id"] = foodID
				}
				if f.payload["write_committed"] == false {
					f.payload["hint"] = "The custom food was created but not logged. Check the custom food library before retrying; reuse the existing food to log it."
				}
			}
			return nil, failure
		}
		date, err := dateArgument(args, "meal_date")
		if err != nil {
			return fail(err)
		}
		mealTime, ok := args["meal_time"].(string)
		if !ok {
			return fail(fmt.Errorf("meal_time is required"))
		}
		if parsed, e := time.Parse("15:04:05", mealTime); e != nil || parsed.Format("15:04:05") != mealTime {
			return fail(fmt.Errorf("meal_time must be in HH:MM:SS format"))
		}
		name, ok := args["food_name"].(string)
		if !ok {
			return fail(fmt.Errorf("food_name is required"))
		}
		cal, err := numString(args["calories"])
		if err != nil {
			return fail(err)
		}
		existing, err := findFoodByName(ctx, s, name)
		if err != nil {
			return fail(err)
		}
		foodID, servingID := foodIDs(existing)
		if existing != nil && (foodID == "" || servingID == "") {
			return fail(fmt.Errorf("Existing custom food has no usable foodId/servingId; not creating a duplicate."))
		}
		if foodID == "" || servingID == "" {
			unit := "G"
			if v, ok := args["serving_unit"].(string); ok {
				unit = v
			}
			units := "100"
			if v := args["number_of_units"]; v != nil {
				units, err = numString(v)
				if err != nil {
					return fail(err)
				}
			}
			nutrition := map[string]any{"servingUnit": unit, "numberOfUnits": units, "calories": cal}
			for _, k := range []string{"carbs", "protein", "fat"} {
				if v := args[k]; v != nil {
					str, e := numString(v)
					if e != nil {
						return fail(e)
					}
					nutrition[k] = str
				}
			}
			meta := map[string]any{"foodName": name, "foodType": "GENERIC", "source": "GARMIN", "regionCode": "US", "languageCode": "en"}
			if v := args["brand_name"]; v != nil {
				meta["brandName"] = v
			}
			tracker.Sent()
			body, err := s.Client.Do(ctx, garmin.Req{Method: http.MethodPut, Path: "/nutrition-service/customFood", JSON: map[string]any{"foodMetaData": meta, "nutritionContents": []any{nutrition}}})
			if err != nil {
				return fail(err)
			}
			created = true
			tracker = newWriteTracker("upsert_and_log", "Error in upsert_and_log (custom food was created)", m.opt.Log)
			var response map[string]any
			if len(body) > 0 {
				if err = json.Unmarshal(body, &response); err != nil {
					return fail(err)
				}
			}
			foodID, servingID = foodIDs(response)
			if foodID == "" || servingID == "" {
				found, err := findFoodByName(ctx, s, name)
				if err != nil {
					return fail(err)
				}
				id, serving := foodIDs(found)
				if id != "" {
					foodID = id
				}
				if serving != "" {
					servingID = serving
				}
			}
			if foodID == "" || servingID == "" {
				return fail(fmt.Errorf("could not retrieve foodId/servingId for '%s' after creation", name))
			}
		}
		v, err := s.Client.GetJSON(ctx, "/nutrition-service/meals/"+date, nil)
		if err != nil {
			return fail(err)
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
			message := fmt.Sprintf("No nutrition meal windows found for %s; cannot log food at '%s'.", date, mealTime)
			if created {
				return fail(fmt.Errorf("%s", message))
			}
			return message, nil
		}
		qty := any(float64(1))
		if args["serving_qty"] != nil {
			if _, ok := args["serving_qty"].(float64); !ok {
				return fail(fmt.Errorf("invalid serving_qty"))
			}
			qty = args["serving_qty"]
		}
		entry := map[string]any{"logTimestamp": time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), "logSource": "GCW", "logCategory": "REGULAR_LOG", "mealTime": mealTime, "action": "ADD", "mealId": mealID, "foodId": foodID, "servingId": servingID, "source": "GARMIN", "regionCode": "US", "languageCode": "en", "servingQty": qty}
		tracker.Sent()
		v, err = s.Client.DoJSON(ctx, garmin.Req{Method: http.MethodPut, Path: "/nutrition-service/food/logs", JSON: map[string]any{"mealDate": date, "foodLogItems": []any{entry}}})
		if err != nil {
			return fail(err)
		}
		tracker.Committed()
		if empty(v) {
			return "Food logged successfully.", nil
		}
		return v, nil
	}, "meal_date", "meal_time", "food_name", "calories")
}
