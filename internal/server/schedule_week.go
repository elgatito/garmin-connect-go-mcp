package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
)

func registerScheduleWeek(srv *mcp.Server, m *manager) {
	addTool(srv, m, "schedule_week", description("schedule_week"), map[string]any{"week": map[string]any{"anyOf": []any{map[string]any{"type": "array", "items": map[string]any{"type": "object"}}, map[string]any{"type": "string"}}}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		v := args["week"]
		if text, ok := v.(string); ok {
			if err := json.Unmarshal([]byte(text), &v); err != nil {
				return nil, err
			}
		}
		week, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("week must be a list")
		}
		results := []any{}
		for _, v := range week {
			item, ok := v.(map[string]any)
			if !ok {
				results = append(results, map[string]any{"status": "failed", "message": "entry must be an object"})
				continue
			}
			date, err := dateArgument(item, "date")
			if err != nil {
				results = append(results, map[string]any{"date": item["date"], "workout_id": item["workout_id"], "status": "failed", "message": err.Error()})
				continue
			}
			id, err := activityID(item["workout_id"])
			if err != nil {
				results = append(results, map[string]any{"date": date, "workout_id": item["workout_id"], "status": "failed", "message": "workout_id is required and must be a positive integer"})
				continue
			}
			query := fmt.Sprintf(`query{workoutScheduleSummariesScalar(startDate:"%s", endDate:"%s")}`, date, date)
			already := false
			if v, err := s.Client.DoJSON(ctx, garmin.Req{Method: http.MethodPost, Path: "/graphql-gateway/graphql", JSON: map[string]any{"query": query}}); err == nil {
				data, _ := v.(map[string]any)
				inner, _ := data["data"].(map[string]any)
				rows, _ := inner["workoutScheduleSummariesScalar"].([]any)
				for _, v := range rows {
					row, _ := v.(map[string]any)
					if row["workoutId"] == float64(id) && row["scheduleDate"] == date {
						already = true
						break
					}
				}
			}
			if already {
				results = append(results, map[string]any{"date": date, "workout_id": id, "status": "already_scheduled", "idempotent": true})
				continue
			}
			tracker := newWriteTracker("schedule_week", "Error scheduling workout", m.opt.Log)
			tracker.Sent()
			v, err = s.Client.DoJSON(ctx, garmin.Req{Method: http.MethodPost, Path: fmt.Sprintf("/workout-service/schedule/%d", id), JSON: map[string]any{"date": date}})
			if err != nil {
				failure := tracker.Fail(err)
				entry := map[string]any{"status": "error", "message": failure.Error()}
				if f, ok := failure.(*writeFailure); ok {
					entry = f.payload
				}
				entry["date"] = date
				entry["workout_id"] = id
				results = append(results, entry)
				continue
			}
			tracker.Committed()
			entry := map[string]any{"date": date, "workout_id": id, "status": "scheduled"}
			if d, ok := v.(map[string]any); ok && d["workoutScheduleId"] != nil {
				entry["scheduled_workout_id"] = d["workoutScheduleId"]
			}
			results = append(results, entry)
		}
		return map[string]any{"status": "complete", "scheduled": results}, nil
	}, "week")
}
