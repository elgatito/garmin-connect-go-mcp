package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func workoutSummary(w map[string]any) map[string]any {
	out := map[string]any{}
	for k, source := range map[string]string{"id": "workoutId", "name": "workoutName", "provider": "workoutProvider", "created_date": "createdDate", "updated_date": "updatedDate"} {
		if w[source] != nil {
			out[k] = w[source]
		}
	}
	if sport, ok := w["sportType"].(map[string]any); ok && sport["sportTypeKey"] != nil {
		out["sport"] = sport["sportTypeKey"]
	}
	if d, ok := w["description"].(string); ok && d != "" {
		out["description"] = d
	}
	for k, keys := range map[string][]string{"estimated_duration_seconds": {"estimatedDurationInSecs", "estimatedDuration"}, "estimated_distance_meters": {"estimatedDistanceInMeters", "estimatedDistance"}} {
		for _, key := range keys {
			if w[key] != nil {
				out[k] = w[key]
				break
			}
		}
	}
	return out
}
func registerWorkouts(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_workouts", description("get_workouts"), nil, func(ctx context.Context, s *Session, _ map[string]any) (any, error) {
		seen := map[string]bool{}
		out := []any{}
		for start := 1; ; start += 100 {
			v, err := s.Client.GetJSON(ctx, "/workout-service/workouts", url.Values{"start": {strconv.Itoa(start)}, "limit": {"100"}})
			if err != nil {
				return nil, fmt.Errorf("Error retrieving workouts: %w", err)
			}
			page, ok := v.([]any)
			if !ok {
				break
			}
			added := 0
			for _, v := range page {
				w, ok := v.(map[string]any)
				if !ok {
					continue
				}
				b, _ := json.Marshal(w["workoutId"])
				key := string(b)
				if seen[key] {
					continue
				}
				seen[key] = true
				added++
				out = append(out, workoutSummary(w))
			}
			if len(page) < 100 || added == 0 {
				break
			}
		}
		if len(out) == 0 {
			return "No workouts found.", nil
		}
		return map[string]any{"count": len(out), "workouts": out}, nil
	})
}
