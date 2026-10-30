package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
)

func registerScheduleBatch(srv *mcp.Server, m *manager) {
	addTool(srv, m, "schedule_workouts", description("schedule_workouts"), map[string]any{"schedules": map[string]any{"anyOf": []any{map[string]any{"type": "array", "items": map[string]any{"type": "object"}}, map[string]any{"type": "string"}}}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		v := args["schedules"]
		if text, ok := v.(string); ok {
			if err := json.Unmarshal([]byte(text), &v); err != nil {
				return nil, err
			}
		}
		items, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("schedules must be a list")
		}
		results := []any{}
		success := 0
		for _, v := range items {
			item, ok := v.(map[string]any)
			if !ok {
				results = append(results, map[string]any{"status": "error", "message": "schedule must be an object"})
				continue
			}
			date, err := dateArgument(item, "calendar_date")
			if err != nil {
				results = append(results, map[string]any{"status": "failed", "workout_id": item["workout_id"], "scheduled_date": item["calendar_date"], "message": err.Error()})
				continue
			}
			idValue := item["workout_id"]
			var name any
			if item["workout_data"] != nil {
				workout, err := workoutObject(item["workout_data"])
				if err != nil {
					results = append(results, map[string]any{"status": "error", "scheduled_date": date, "message": err.Error()})
					continue
				}
				uploaded, err := uploadWorkout(ctx, s, m, "schedule_workouts", workout)
				if err != nil {
					entry := map[string]any{"status": "error", "scheduled_date": date, "message": err.Error()}
					if failure, ok := err.(*writeFailure); ok {
						entry = failure.payload
						entry["scheduled_date"] = date
					}
					results = append(results, entry)
					continue
				}
				data, _ := uploaded.(map[string]any)
				idValue = data["workout_id"]
				name = data["name"]
				if idValue == nil {
					results = append(results, map[string]any{"status": "failed", "scheduled_date": date, "message": "Upload succeeded but no workout_id returned"})
					continue
				}
			}
			if idValue == nil {
				results = append(results, map[string]any{"status": "failed", "workout_id": nil, "scheduled_date": date, "message": "Missing required fields: provide either workout_id or workout_data"})
				continue
			}
			id, err := activityID(idValue)
			if err != nil {
				results = append(results, map[string]any{"status": "error", "workout_id": idValue, "scheduled_date": date, "message": "Error scheduling workout: " + err.Error()})
				continue
			}
			query := fmt.Sprintf(`query{workoutScheduleSummariesScalar(startDate:"%s", endDate:"%s")}`, date, date)
			scheduled := false
			if v, err := s.Client.DoJSON(ctx, garmin.Req{Method: http.MethodPost, Path: "/graphql-gateway/graphql", JSON: map[string]any{"query": query}}); err == nil {
				data, _ := v.(map[string]any)
				inner, _ := data["data"].(map[string]any)
				rows, _ := inner["workoutScheduleSummariesScalar"].([]any)
				for _, v := range rows {
					row, _ := v.(map[string]any)
					if row["workoutId"] == float64(id) && row["scheduleDate"] == date {
						scheduled = true
						break
					}
				}
			}
			entry := map[string]any{"status": "success", "workout_id": id, "scheduled_date": date}
			if name != nil && truthy(name) {
				entry["workout_name"] = name
			}
			if scheduled {
				entry["idempotent"] = true
				entry["message"] = fmt.Sprintf("Workout %d already scheduled for %s — no action taken", id, date)
				success++
				results = append(results, entry)
				continue
			}
			tracker := newWriteTracker("schedule_workouts", "Error scheduling workout", m.opt.Log)
			tracker.Sent()
			_, err = s.Client.Do(ctx, garmin.Req{Method: http.MethodPost, Path: fmt.Sprintf("/workout-service/schedule/%d", id), JSON: map[string]any{"date": date}})
			if err != nil {
				failure := tracker.Fail(err)
				if f, ok := failure.(*writeFailure); ok {
					entry = f.payload
				} else {
					entry = map[string]any{"status": "error", "message": failure.Error()}
				}
				entry["workout_id"] = id
				entry["scheduled_date"] = date
				results = append(results, entry)
				continue
			}
			tracker.Committed()
			entry["message"] = fmt.Sprintf("Successfully scheduled workout %d for %s", id, date)
			success++
			results = append(results, entry)
		}
		return map[string]any{"total": len(results), "succeeded": success, "failed": len(results) - success, "results": results}, nil
	}, "schedules")
}
