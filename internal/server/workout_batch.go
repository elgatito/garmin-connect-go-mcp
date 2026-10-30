package server

import (
	"context"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"time"
)

func registerWorkoutBatch(srv *mcp.Server, m *manager) {
	addTool(srv, m, "delete_workouts", description("delete_workouts"), map[string]any{"workout_ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}}, "confirm": map[string]any{"type": "boolean", "default": false}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		items, ok := args["workout_ids"].([]any)
		if !ok {
			return nil, fmt.Errorf("workout_ids is required")
		}
		ids := make([]int64, 0, len(items))
		for _, v := range items {
			id, err := activityID(v)
			if err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
		previews := make([]map[string]any, 0, len(ids))
		for _, id := range ids {
			previews = append(previews, workoutPreview(ctx, s, id))
		}
		confirm, _ := args["confirm"].(bool)
		if !confirm {
			return map[string]any{"status": "needs_confirmation", "total": len(ids), "workouts": previews, "message": "This permanently deletes these workouts from Garmin Connect and cannot be undone. Call again with confirm=true to proceed."}, nil
		}
		results := []any{}
		success := 0
		for i, id := range ids {
			tracker := newWriteTracker("delete_workouts", "Error deleting workout", m.opt.Log)
			tracker.Sent()
			_, err := s.Client.Do(ctx, garmin.Req{Method: http.MethodDelete, Path: fmt.Sprintf("/workout-service/workout/%d", id)})
			if err != nil {
				failure := tracker.Fail(err)
				entry := map[string]any{"status": "error", "message": failure.Error()}
				if f, ok := failure.(*writeFailure); ok {
					entry = f.payload
				}
				entry["workout_id"] = id
				results = append(results, entry)
				continue
			}
			tracker.Committed()
			success++
			fmt.Fprintf(m.opt.Log, "delete_workouts: deleted workout %d at %s\n", id, time.Now().UTC().Format(time.RFC3339))
			results = append(results, map[string]any{"status": "success", "workout_id": id, "workout": previews[i], "message": fmt.Sprintf("Workout %d deleted successfully", id)})
		}
		return map[string]any{"total": len(results), "succeeded": success, "failed": len(results) - success, "results": results}, nil
	}, "workout_ids")
	addTool(srv, m, "unschedule_workouts", description("unschedule_workouts"), map[string]any{"scheduled_workout_ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		ids, ok := args["scheduled_workout_ids"].([]any)
		if !ok {
			return nil, fmt.Errorf("scheduled_workout_ids is required")
		}
		results := []any{}
		success := 0
		for _, v := range ids {
			id, err := activityID(v)
			if err != nil {
				results = append(results, map[string]any{"status": "error", "scheduled_workout_id": v, "message": "Error unscheduling workout: " + err.Error()})
				continue
			}
			tracker := newWriteTracker("unschedule_workouts", "Error unscheduling workout", m.opt.Log)
			tracker.Sent()
			_, err = s.Client.Do(ctx, garmin.Req{Method: http.MethodDelete, Path: fmt.Sprintf("/workout-service/schedule/%d", id)})
			if err != nil {
				failure := tracker.Fail(err)
				entry := map[string]any{"status": "error", "message": failure.Error()}
				if f, ok := failure.(*writeFailure); ok {
					entry = f.payload
				}
				entry["scheduled_workout_id"] = id
				results = append(results, entry)
				continue
			}
			tracker.Committed()
			success++
			results = append(results, map[string]any{"status": "success", "scheduled_workout_id": id, "message": fmt.Sprintf("Scheduled workout %d removed from calendar", id)})
		}
		return map[string]any{"total": len(results), "succeeded": success, "failed": len(results) - success, "results": results}, nil
	}, "scheduled_workout_ids")
}
