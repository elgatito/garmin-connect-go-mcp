package server

import (
	"context"
	"errors"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/url"
	"strconv"
)

func findCustomFood(ctx context.Context, s *Session, id, name string) (map[string]any, error) {
	for _, search := range []string{name, ""} {
		for start := 0; start < 500; start += 20 {
			v, err := s.Client.GetJSON(ctx, "/nutrition-service/customFood", url.Values{"searchExpression": {search}, "start": {strconv.Itoa(start)}, "limit": {"20"}, "includeContent": {"true"}})
			if err != nil {
				return nil, err
			}
			data, _ := v.(map[string]any)
			foods, _ := data["customFoods"].([]any)
			for _, v := range foods {
				food, _ := v.(map[string]any)
				meta, _ := food["foodMetaData"].(map[string]any)
				if fmt.Sprint(meta["foodId"]) == id {
					return food, nil
				}
			}
			if len(foods) < 20 {
				break
			}
		}
	}
	return nil, nil
}
func findLogEntry(v any, id string) map[string]any {
	switch v := v.(type) {
	case map[string]any:
		if v["logId"] != nil && fmt.Sprint(v["logId"]) == id {
			return v
		}
		for _, item := range v {
			if found := findLogEntry(item, id); found != nil {
				return found
			}
		}
	case []any:
		for _, item := range v {
			if found := findLogEntry(item, id); found != nil {
				return found
			}
		}
	}
	return nil
}
func registerNutritionDelete(srv *mcp.Server, m *manager) {
	addTool(srv, m, "delete_custom_food", description("delete_custom_food"), map[string]any{"food_id": map[string]any{"type": "string"}, "confirm": map[string]any{"type": "boolean", "default": false}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		id, ok := args["food_id"].(string)
		if !ok || !regexpGearUUID(id) {
			return nil, fmt.Errorf("invalid food_id")
		}
		tracker := newWriteTracker("delete_custom_food", "Error deleting custom food", m.opt.Log)
		confirm, _ := args["confirm"].(bool)
		if !confirm {
			food, err := findCustomFood(ctx, s, id, "")
			if err != nil {
				return nil, tracker.Fail(err)
			}
			if food == nil {
				return map[string]any{"status": "not_found", "food_id": id}, nil
			}
			return map[string]any{"status": "needs_confirmation", "food_id": id, "food": food, "message": "Review this food, then call again with confirm=true to permanently delete it."}, nil
		}
		tracker.Sent()
		_, err := s.Client.Do(ctx, garmin.Req{Method: http.MethodDelete, Path: "/nutrition-service/customFood/" + id})
		if err != nil {
			return nil, tracker.Fail(err)
		}
		tracker.Committed()
		return map[string]any{"status": "success", "food_id": id, "message": fmt.Sprintf("Custom food %s deleted successfully.", id)}, nil
	}, "food_id")
	addTool(srv, m, "delete_food_log", description("delete_food_log"), map[string]any{"log_id": map[string]any{"type": "string"}, "meal_date": map[string]any{"type": "string"}, "confirm": map[string]any{"type": "boolean", "default": false}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		id, ok := args["log_id"].(string)
		if !ok || id == "" {
			return nil, fmt.Errorf("invalid log_id")
		}
		date, err := dateArgument(args, "meal_date")
		if err != nil {
			return nil, err
		}
		path := "/nutrition-service/food/logs/" + date
		tracker := newWriteTracker("delete_food_log", "Error deleting food log", m.opt.Log)
		confirm, _ := args["confirm"].(bool)
		if !confirm {
			v, err := s.Client.GetJSON(ctx, path, nil)
			if err != nil {
				return nil, tracker.Fail(err)
			}
			entry := findLogEntry(v, id)
			if entry == nil {
				return map[string]any{"status": "not_found", "log_id": id, "meal_date": date}, nil
			}
			return map[string]any{"status": "needs_confirmation", "log_id": id, "meal_date": date, "entry": entry, "message": "Review this entry, then call again with confirm=true to permanently delete it."}, nil
		}
		tracker.Sent()
		_, err = s.Client.Do(ctx, garmin.Req{Method: http.MethodDelete, Path: path, JSON: map[string]any{"logIds": []string{id}}})
		var api *garmin.APIError
		if errors.As(err, &api) && api.Status == 404 {
			return map[string]any{"status": "not_found", "log_id": id, "message": fmt.Sprintf("Food log entry %s was not found.", id)}, nil
		}
		if err != nil {
			return nil, tracker.Fail(err)
		}
		tracker.Committed()
		return map[string]any{"status": "success", "log_id": id, "message": fmt.Sprintf("Food log entry %s deleted successfully.", id)}, nil
	}, "log_id", "meal_date")
}
