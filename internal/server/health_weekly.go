package server

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerWeekly(srv *mcp.Server, m *manager) {
	for _, item := range []struct{ name, label, path string }{{"get_weekly_steps", "weekly steps data", "steps"}, {"get_weekly_stress", "weekly stress data", "stress"}, {"get_weekly_intensity_minutes", "weekly intensity minutes data", "im"}} {
		item := item
		addTool(srv, m, item.name, description(item.name), map[string]any{"end_date": map[string]any{"type": "string"}, "weeks": map[string]any{"type": "integer", "default": 4}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
			end, err := dateArgument(args, "end_date")
			if err != nil {
				return nil, fmt.Errorf("Error retrieving %s: %w", item.label, err)
			}
			weeks := 4
			if v, ok := args["weeks"].(float64); ok {
				if v != float64(int(v)) {
					return nil, fmt.Errorf("Error retrieving %s: weeks must be a positive integer", item.label)
				}
				weeks = int(v)
			}
			if weeks > 52 {
				weeks = 52
			}
			if weeks < 1 {
				return nil, fmt.Errorf("Error retrieving %s: weeks must be a positive integer", item.label)
			}
			suffix := fmt.Sprintf("%s/%d", end, weeks)
			if item.path == "im" {
				d, _ := time.Parse("2006-01-02", end)
				suffix = d.AddDate(0, 0, -(weeks*7-1)).Format("2006-01-02") + "/" + end
			}
			v, err := s.Client.GetJSON(ctx, "/usersummary-service/stats/"+item.path+"/weekly/"+suffix, nil)
			if err != nil {
				return nil, fmt.Errorf("Error retrieving %s: %w", item.label, err)
			}
			if empty(v) {
				return fmt.Sprintf("No %s found for %d weeks ending %s", item.label, weeks, end), nil
			}
			rows, ok := v.([]any)
			if !ok {
				return nil, fmt.Errorf("Error retrieving %s: unexpected response", item.label)
			}
			output := []map[string]any{}
			for _, row := range rows {
				raw, ok := row.(map[string]any)
				if !ok {
					continue
				}
				data := raw
				fields := map[string]string{}
				switch item.path {
				case "steps":
					fields = map[string]string{"total_steps": "totalSteps", "average_steps": "averageSteps", "total_distance_meters": "totalDistance", "average_distance_meters": "averageDistance", "days_with_data": "wellnessDataDaysCount"}
					data, _ = raw["values"].(map[string]any)
				case "stress":
					fields = map[string]string{"stress_value": "value"}
				case "im":
					fields = map[string]string{"weekly_goal": "weeklyGoal", "moderate_minutes": "moderateValue", "vigorous_minutes": "vigorousValue"}
				}
				out := map[string]any{}
				if date := raw["calendarDate"]; date != nil {
					out["week_start"] = date
				}
				for k, key := range fields {
					if data[key] != nil {
						out[k] = data[key]
					}
				}
				if item.path == "im" {
					mod, _ := raw["moderateValue"].(float64)
					vig, _ := raw["vigorousValue"].(float64)
					out["total_minutes"] = mod + 2*vig
				}
				output = append(output, out)
			}
			sort.SliceStable(output, func(i, j int) bool {
				a, _ := output[i]["week_start"].(string)
				b, _ := output[j]["week_start"].(string)
				return a > b
			})
			return map[string]any{"end_date": end, "weeks_requested": weeks, "weeks_returned": len(output), "weekly_data": output}, nil
		}, "end_date")
	}
}
