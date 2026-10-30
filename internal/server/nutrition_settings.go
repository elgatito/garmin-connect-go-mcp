package server

import (
	"context"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"time"
)

func targetDateError(stored any, date string, target *string) string {
	if target != nil {
		parsed, err := time.Parse("2006-01-02", *target)
		if err != nil || parsed.Format("2006-01-02") != *target {
			return fmt.Sprintf("Invalid target_date '%s'. Use YYYY-MM-DD.", *target)
		}
		if *target <= date {
			return fmt.Sprintf("target_date (%s) must be after date (%s); Garmin requires the weight goal's target to be later than the day being written.", *target, date)
		}
		return ""
	}
	if stored != nil && fmt.Sprint(stored) != "" && fmt.Sprint(stored) <= date {
		return fmt.Sprintf("Cannot update nutrition settings for %s: the weight goal's targetDate (%v) is not after %s, and Garmin rejects the whole settings update in that case.\nEither set a new target date on the weight goal in Garmin Connect (or end the goal) and retry, or pass target_date=YYYY-MM-DD (after %s) to move the weight goal's target date as part of this update.", date, stored, date, date)
	}
	return ""
}
func registerNutritionSettings(srv *mcp.Server, m *manager) {
	schema := map[string]any{"date": map[string]any{"type": "string"}, "target_date": map[string]any{"type": "string"}}
	for _, key := range []string{"calorie_goal", "carbs_grams", "fat_grams", "protein_grams"} {
		schema[key] = map[string]any{"type": "integer"}
	}
	addTool(srv, m, "set_nutrition_daily_settings", description("set_nutrition_daily_settings"), schema, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		supplied := false
		for _, k := range []string{"calorie_goal", "carbs_grams", "fat_grams", "protein_grams"} {
			if args[k] != nil {
				supplied = true
				if _, err := intArg(args, k, 0); err != nil {
					return nil, err
				}
			}
		}
		if !supplied {
			return "No fields to update — supply at least one of calorie_goal, carbs_grams, fat_grams, protein_grams.", nil
		}
		date, err := dateArgument(args, "date")
		if err != nil {
			return nil, err
		}
		path := "/nutrition-service/settings/" + date
		tracker := newWriteTracker("set_nutrition_daily_settings", "Error updating nutrition settings", m.opt.Log)
		v, err := s.Client.GetJSON(ctx, path, nil)
		if err != nil {
			return nil, tracker.Fail(err)
		}
		current, ok := v.(map[string]any)
		if !ok || len(current) == 0 {
			return fmt.Sprintf("Could not read current nutrition settings for %s — cannot apply update.", date), nil
		}
		var target *string
		if raw, ok := args["target_date"].(string); ok {
			target = &raw
		}
		if message := targetDateError(current["targetDate"], date, target); message != "" {
			return message, nil
		}
		if target != nil {
			current["targetDate"] = *target
		}
		if v := args["calorie_goal"]; v != nil {
			current["calorieGoal"] = v
		}
		macrosPresent := false
		for _, k := range []string{"carbs_grams", "fat_grams", "protein_grams"} {
			if args[k] != nil {
				macrosPresent = true
			}
		}
		if macrosPresent {
			macros := map[string]any{}
			if v := current["macroGoals"]; v != nil {
				var ok bool
				macros, ok = v.(map[string]any)
				if !ok {
					return "Could not read current macro goals — cannot apply update.", nil
				}
			}
			for k, src := range map[string]string{"carbs": "carbs_grams", "fat": "fat_grams", "protein": "protein_grams"} {
				if args[src] != nil {
					macros[k] = args[src]
				}
			}
			current["macroGoals"] = macros
		}
		tracker.Sent()
		v, err = s.Client.DoJSON(ctx, garmin.Req{Method: http.MethodPut, Path: path, JSON: current})
		if err != nil {
			return nil, tracker.Fail(err)
		}
		tracker.Committed()
		if empty(v) {
			v, err = s.Client.GetJSON(ctx, path, nil)
			if err != nil {
				return fmt.Sprintf("Nutrition update submitted, but could not verify stored settings: %v", err), nil
			}
		}
		result, ok := v.(map[string]any)
		if !ok || len(result) == 0 {
			return "Nutrition update submitted, but could not verify stored settings.", nil
		}
		macros, _ := result["macroGoals"].(map[string]any)
		return map[string]any{"status": "updated", "date": date, "calorie_goal": result["calorieGoal"], "carbs_grams": macros["carbs"], "fat_grams": macros["fat"], "protein_grams": macros["protein"], "weight_goal_target_date": result["targetDate"]}, nil
	}, "date")
}
