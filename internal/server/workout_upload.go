package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
)

func workoutObject(v any) (map[string]any, error) {
	if s, ok := v.(string); ok {
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			return nil, err
		}
	}
	out, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("workout must be an object")
	}
	return out, nil
}
func uploadWorkout(ctx context.Context, s *Session, m *manager, tool string, workout map[string]any) (any, error) {
	if err := validateWorkout(workout); err != nil {
		return nil, fmt.Errorf("Error uploading workout: %w", err)
	}
	tracker := newWriteTracker(tool, "Error uploading workout", m.opt.Log)
	tracker.Sent()
	v, err := s.Client.DoJSON(ctx, garmin.Req{Method: http.MethodPost, Path: "/workout-service/workout", JSON: workout})
	if err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	if result, ok := v.(map[string]any); ok {
		out := map[string]any{"status": "success", "message": "Workout uploaded successfully"}
		if result["workoutId"] != nil {
			out["workout_id"] = result["workoutId"]
		}
		if result["workoutName"] != nil {
			out["name"] = result["workoutName"]
		}
		return out, nil
	}
	return v, nil
}
func registerWorkoutUploads(srv *mcp.Server, m *manager) {
	addTool(srv, m, "upload_workout", description("upload_workout"), map[string]any{"workout_data": map[string]any{"anyOf": []any{map[string]any{"type": "object"}, map[string]any{"type": "string"}}}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		workout, err := workoutObject(args["workout_data"])
		if err != nil {
			return nil, fmt.Errorf("Error uploading workout: %w", err)
		}
		return uploadWorkout(ctx, s, m, "upload_workout", workout)
	}, "workout_data")
	addTool(srv, m, "upload_workouts", description("upload_workouts"), map[string]any{"workouts": map[string]any{"anyOf": []any{map[string]any{"type": "array", "items": map[string]any{"type": "object"}}, map[string]any{"type": "string"}}}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		v := args["workouts"]
		if str, ok := v.(string); ok {
			if err := json.Unmarshal([]byte(str), &v); err != nil {
				return nil, err
			}
		}
		workouts, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("workouts must be a list")
		}
		results := []any{}
		success := 0
		for _, v := range workouts {
			workout, err := workoutObject(v)
			if err != nil {
				results = append(results, map[string]any{"status": "error", "message": "Error uploading workout: " + err.Error()})
				continue
			}
			result, err := uploadWorkout(ctx, s, m, "upload_workouts", workout)
			if err != nil {
				entry := map[string]any{"status": "error", "name": workout["workoutName"], "message": err.Error()}
				if failure, ok := err.(*writeFailure); ok {
					entry = failure.payload
					entry["name"] = workout["workoutName"]
				}
				results = append(results, entry)
				continue
			}
			success++
			if entry, ok := result.(map[string]any); ok {
				results = append(results, entry)
			} else {
				results = append(results, map[string]any{"status": "success", "message": "Workout uploaded successfully"})
			}
		}
		return map[string]any{"total": len(results), "succeeded": success, "failed": len(results) - success, "results": results}, nil
	}, "workouts")
}
