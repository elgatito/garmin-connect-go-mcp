package server

import (
	"context"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

func workoutPreview(ctx context.Context, s *Session, id int64) map[string]any {
	out := map[string]any{"workout_id": id}
	merge := func(w map[string]any) {
		summary := workoutSummary(w)
		for _, key := range []string{"name", "sport", "provider", "updated_date"} {
			if summary[key] != nil {
				out[key] = summary[key]
			}
		}
	}
	v, err := s.Client.GetJSON(ctx, fmt.Sprintf("/workout-service/workout/%d", id), nil)
	if err == nil {
		if w, ok := v.(map[string]any); ok && len(w) > 0 {
			merge(w)
			return out
		}
	}
	seen := map[string]bool{}
	for start := 1; ; start += 100 {
		v, err = s.Client.GetJSON(ctx, "/workout-service/workouts", url.Values{"start": {strconv.Itoa(start)}, "limit": {"100"}})
		if err != nil {
			break
		}
		page, ok := v.([]any)
		if !ok || len(page) == 0 {
			break
		}
		added := 0
		for _, v := range page {
			w, ok := v.(map[string]any)
			if !ok {
				continue
			}
			key := fmt.Sprint(w["workoutId"])
			if seen[key] {
				continue
			}
			seen[key] = true
			added++
			if key == fmt.Sprint(id) {
				merge(w)
				return out
			}
		}
		if len(page) < 100 || added == 0 {
			break
		}
	}
	return out
}
func registerWorkoutDelete(srv *mcp.Server, m *manager) {
	addTool(srv, m, "download_workout", description("download_workout"), map[string]any{"workout_id": map[string]any{"type": "integer"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		id, err := activityID(args["workout_id"])
		if err != nil {
			return nil, fmt.Errorf("Error downloading workout: %w", err)
		}
		data, err := s.Client.Download(ctx, fmt.Sprintf("/workout-service/workout/FIT/%d", id), nil)
		if err != nil {
			return nil, fmt.Errorf("Error downloading workout: %w", err)
		}
		if len(data) == 0 {
			return fmt.Sprintf("No workout data found for workout with ID %d.", id), nil
		}
		return map[string]any{"workout_id": id, "format": "FIT", "size_bytes": len(data), "message": "Workout data is available in FIT format. Use Garmin Connect API to save to file."}, nil
	}, "workout_id")
	addTool(srv, m, "delete_workout", description("delete_workout"), map[string]any{"workout_id": map[string]any{"type": "integer"}, "confirm": map[string]any{"type": "boolean", "default": false}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		id, err := activityID(args["workout_id"])
		if err != nil {
			return nil, err
		}
		preview := workoutPreview(ctx, s, id)
		confirm, _ := args["confirm"].(bool)
		if !confirm {
			return map[string]any{"status": "needs_confirmation", "workout": preview, "message": "This permanently deletes the workout from Garmin Connect and cannot be undone. Call again with confirm=true to proceed."}, nil
		}
		tracker := newWriteTracker("delete_workout", "Error deleting workout", m.opt.Log)
		tracker.Sent()
		_, err = s.Client.Do(ctx, garmin.Req{Method: http.MethodDelete, Path: fmt.Sprintf("/workout-service/workout/%d", id)})
		if err != nil {
			return nil, tracker.Fail(err)
		}
		tracker.Committed()
		fmt.Fprintf(m.opt.Log, "delete_workout: deleted workout %d at %s\n", id, time.Now().UTC().Format(time.RFC3339))
		return map[string]any{"status": "success", "workout_id": id, "workout": preview, "message": fmt.Sprintf("Workout %d deleted successfully", id)}, nil
	}, "workout_id")
}
