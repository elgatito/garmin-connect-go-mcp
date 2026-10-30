package server

import (
	"context"
	"errors"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"math"
	"net/url"
	"strconv"
)

func trainingEffect(raw map[string]any, id int64, source string) map[string]any {
	summary, _ := raw["summaryDTO"].(map[string]any)
	first := func(fields ...string) any {
		for _, key := range fields {
			if summary[key] != nil {
				return summary[key]
			}
		}
		for _, key := range fields {
			if raw[key] != nil {
				return raw[key]
			}
		}
		return nil
	}
	aerobic := first("trainingEffect", "aerobicTrainingEffect")
	out := map[string]any{"activity_id": id, "source": source, "training_effect": aerobic, "aerobic_effect": aerobic, "anaerobic_effect": first("anaerobicTrainingEffect"), "training_effect_label": first("trainingEffectLabel", "aerobicTrainingEffectMessage"), "training_load": first("activityTrainingLoad"), "performance_condition": first("performanceCondition")}
	if n, ok := first("recoveryTime").(float64); ok && n > 0 {
		out["recovery_time_hours"] = math.RoundToEven(n/6) / 10
	}
	for k, v := range out {
		if v == nil {
			delete(out, k)
		}
	}
	return out
}
func registerTrainingEffect(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_training_effect", description("get_training_effect"), map[string]any{"activity_id": map[string]any{"type": "integer"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		id, err := activityID(args["activity_id"])
		if err != nil {
			return fmt.Sprintf("Invalid activity ID: %v", args["activity_id"]), nil
		}
		v, err := s.Client.GetJSON(ctx, fmt.Sprintf("/activity-service/activity/%d", id), nil)
		forbidden := false
		if err != nil {
			var api *garmin.APIError
			if !errors.As(err, &api) || api.Status != 403 {
				return nil, fmt.Errorf("Error retrieving training effect data: %w", err)
			}
			forbidden = true
		}
		activity, _ := v.(map[string]any)
		source := "activity_details"
		if len(activity) == 0 {
			source = "activity_list"
			for _, q := range []url.Values{{"activityIds": {strconv.FormatInt(id, 10)}, "start": {"0"}, "limit": {"1"}}, {"start": {"0"}, "limit": {"100"}}} {
				v, err := s.Client.GetJSON(ctx, "/activitylist-service/activities/search/activities", q)
				if err != nil {
					continue
				}
				rows, _ := v.([]any)
				if data, ok := v.(map[string]any); ok {
					rows, _ = data["activityList"].([]any)
					if rows == nil {
						rows, _ = data["activities"].([]any)
					}
				}
				for _, v := range rows {
					row, _ := v.(map[string]any)
					found, e := activityID(row["activityId"])
					if e == nil && found == id {
						activity = row
						break
					}
				}
				if activity != nil {
					break
				}
			}
			if len(activity) == 0 {
				if forbidden {
					return fmt.Sprintf("Error retrieving training effect data: Garmin returned HTTP 403 for activity %d details, and the activity was not found in recent activity search.", id), nil
				}
				return fmt.Sprintf("No activity found with ID %d.", id), nil
			}
		}
		return trainingEffect(activity, id, source), nil
	}, "activity_id")
}
