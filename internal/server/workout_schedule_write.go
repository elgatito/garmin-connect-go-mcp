package server

import (
	"context"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
)

func registerScheduleWrites(srv *mcp.Server, m *manager) {
	for _, item := range []struct{ name, idKey string }{{"schedule_workout", "workout_id"}, {"reschedule_workout", "scheduled_workout_id"}, {"unschedule_workout", "scheduled_workout_id"}} {
		item := item
		schema := map[string]any{item.idKey: map[string]any{"type": "integer"}}
		required := []string{item.idKey}
		if item.name != "unschedule_workout" {
			schema["calendar_date"] = map[string]any{"type": "string"}
			required = append(required, "calendar_date")
		}
		addTool(srv, m, item.name, description(item.name), schema, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
			id, err := activityID(args[item.idKey])
			if err != nil {
				return nil, err
			}
			date := ""
			if item.name != "unschedule_workout" {
				date, err = dateArgument(args, "calendar_date")
				if err != nil {
					return map[string]any{"status": "failed", item.idKey: id, "scheduled_date": args["calendar_date"], "message": err.Error()}, nil
				}
			}
			tracker := newWriteTracker(item.name, "Error "+item.name, m.opt.Log)
			path := fmt.Sprintf("/workout-service/schedule/%d", id)
			if item.name == "schedule_workout" {
				query := fmt.Sprintf(`query{workoutScheduleSummariesScalar(startDate:"%s", endDate:"%s")}`, date, date)
				v, err := s.Client.DoJSON(ctx, garmin.Req{Method: http.MethodPost, Path: "/graphql-gateway/graphql", JSON: map[string]any{"query": query}})
				if err == nil {
					data, _ := v.(map[string]any)
					inner, _ := data["data"].(map[string]any)
					rows, _ := inner["workoutScheduleSummariesScalar"].([]any)
					for _, v := range rows {
						row, _ := v.(map[string]any)
						if row["workoutId"] == float64(id) && row["scheduleDate"] == date {
							return map[string]any{"status": "success", "workout_id": id, "scheduled_date": date, "idempotent": true, "message": fmt.Sprintf("Workout %d already scheduled for %s — no action taken", id, date)}, nil
						}
					}
				}
				tracker.Sent()
				_, err = s.Client.Do(ctx, garmin.Req{Method: http.MethodPost, Path: path, JSON: map[string]any{"date": date}})
				if err != nil {
					return nil, tracker.Fail(err)
				}
				tracker.Committed()
				return map[string]any{"status": "success", "workout_id": id, "scheduled_date": date, "message": fmt.Sprintf("Successfully scheduled workout %d for %s", id, date)}, nil
			}
			if item.name == "unschedule_workout" {
				tracker.Sent()
				_, err = s.Client.Do(ctx, garmin.Req{Method: http.MethodDelete, Path: path})
				if err != nil {
					return nil, tracker.Fail(err)
				}
				tracker.Committed()
				return map[string]any{"status": "success", "scheduled_workout_id": id, "message": fmt.Sprintf("Scheduled workout %d removed from calendar", id)}, nil
			}
			v, err := s.Client.GetJSON(ctx, path, nil)
			if err != nil {
				return nil, tracker.Fail(err)
			}
			current, ok := v.(map[string]any)
			if !ok || len(current) == 0 {
				return map[string]any{"status": "failed", "scheduled_workout_id": id, "message": fmt.Sprintf("Scheduled workout %d not found", id)}, nil
			}
			workout, _ := current["workout"].(map[string]any)
			var ref any
			if len(workout) > 0 {
				ref = map[string]any{"workoutId": workout["workoutId"]}
			}
			previous := current["calendarDate"]
			payload := map[string]any{}
			for k, v := range current {
				payload[k] = v
			}
			payload["calendarDate"] = date
			payload["workout"] = ref
			tracker.Sent()
			_, err = s.Client.Do(ctx, garmin.Req{Method: http.MethodPut, Path: path, JSON: payload})
			if err != nil {
				return nil, tracker.Fail(err)
			}
			tracker.Committed()
			return map[string]any{"status": "success", "scheduled_workout_id": id, "workout_id": workout["workoutId"], "previous_date": previous, "scheduled_date": date, "message": fmt.Sprintf("Moved scheduled workout %d to %s", id, date)}, nil
		}, required...)
	}
}
