package server

import (
	"context"
	"fmt"
	"net/http"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func scheduledWorkout(s map[string]any) map[string]any {
	out := remapFields(s, map[string]string{"date": "scheduleDate", "scheduled_workout_id": "scheduledWorkoutId", "workout_uuid": "workoutUuid", "workout_id": "workoutId", "training_plan_id": "trainingPlanId", "fbt_adaptive_plan_id": "fbtAdaptivePlanId", "tp_type": "tpType", "name": "workoutName", "sport": "workoutType"})
	out["completed"] = s["associatedActivityId"] != nil
	if s["associatedActivityId"] != nil {
		out["activity_id"] = s["associatedActivityId"]
	}
	for k, src := range map[string]string{"training_plan": "tpPlanName", "workout_type": "workoutPhrase", "is_rest_day": "isRestDay", "is_race_day": "race", "estimated_duration_seconds": "estimatedDurationInSecs", "estimated_distance_meters": "estimatedDistanceInMeters"} {
		if truthy(s[src]) {
			out[k] = s[src]
		}
	}
	return out
}
func registerScheduled(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_scheduled_workouts", description("get_scheduled_workouts"), map[string]any{"start_date": map[string]any{"type": "string"}, "end_date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		start, err := dateArgument(args, "start_date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving scheduled workouts: %w", err)
		}
		end, err := dateArgument(args, "end_date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving scheduled workouts: %w", err)
		}
		query := fmt.Sprintf(`query{workoutScheduleSummariesScalar(startDate:"%s", endDate:"%s")}`, start, end)
		v, err := s.Client.DoJSON(ctx, garmin.Req{Method: http.MethodPost, Path: "/graphql-gateway/graphql", JSON: map[string]any{"query": query}})
		if err != nil {
			return nil, fmt.Errorf("Error retrieving scheduled workouts: %w", err)
		}
		result, ok := v.(map[string]any)
		if !ok {
			return "No scheduled workouts found or error querying data.", nil
		}
		data, exists := result["data"]
		if !exists {
			return "No scheduled workouts found or error querying data.", nil
		}
		obj, _ := data.(map[string]any)
		rows, _ := obj["workoutScheduleSummariesScalar"].([]any)
		if len(rows) == 0 {
			return fmt.Sprintf("No workouts scheduled between %s and %s.", start, end), nil
		}
		out := []any{}
		for _, v := range rows {
			if item, ok := v.(map[string]any); ok {
				out = append(out, scheduledWorkout(item))
			}
		}
		return map[string]any{"count": len(rows), "date_range": map[string]any{"start": start, "end": end}, "scheduled_workouts": out}, nil
	}, "start_date", "end_date")
}
