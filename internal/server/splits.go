package server

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func remapFields(src map[string]any, fields map[string]string) map[string]any {
	out := map[string]any{}
	for k, field := range fields {
		if src[field] != nil {
			out[k] = src[field]
		}
	}
	return out
}
func curateSplits(raw map[string]any) map[string]any {
	laps, _ := raw["lapDTOs"].([]any)
	out := map[string]any{"activity_id": raw["activityId"], "lap_count": len(laps)}
	result := []any{}
	for _, v := range laps {
		lap, ok := v.(map[string]any)
		if !ok {
			continue
		}
		row := remapFields(lap, map[string]string{"lap_number": "lapIndex", "start_time_gmt": "startTimeGMT", "distance_meters": "distance", "duration_seconds": "duration", "moving_duration_seconds": "movingDuration", "elapsed_duration_seconds": "elapsedDuration", "avg_speed_mps": "averageSpeed", "avg_moving_speed_mps": "averageMovingSpeed", "max_speed_mps": "maxSpeed", "avg_hr_bpm": "averageHR", "max_hr_bpm": "maxHR", "calories": "calories", "bmr_calories": "bmrCalories", "avg_cadence": "averageRunCadence", "avg_power_watts": "averagePower", "avg_swim_cadence": "averageSwimCadence", "active_length_count": "numberOfActiveLengths", "total_strokes": "totalNumberOfStrokes", "avg_strokes": "averageStrokes", "avg_swolf": "averageSWOLF", "avg_stroke_distance": "averageStrokeDistance", "intensity_type": "intensityType", "elevation_gain_meters": "elevationGain", "elevation_loss_meters": "elevationLoss", "workout_step_index": "wktStepIndex"})
		if n, ok := lap["distance"].(float64); ok && n >= 42949672.96-1000 && n <= 42949672.96 {
			delete(row, "distance_meters")
			row["distance_invalid"] = true
		}
		if lengths, ok := lap["lengthDTOs"].([]any); ok && len(lengths) > 0 {
			outLengths := []any{}
			for _, v := range lengths {
				if l, ok := v.(map[string]any); ok {
					outLengths = append(outLengths, remapFields(l, map[string]string{"length_number": "lengthIndex", "start_time_gmt": "startTimeGMT", "distance_meters": "distance", "duration_seconds": "duration", "avg_speed_mps": "averageSpeed", "max_speed_mps": "maxSpeed", "calories": "calories", "avg_hr_bpm": "averageHR", "max_hr_bpm": "maxHR", "total_strokes": "totalNumberOfStrokes", "avg_swolf": "averageSWOLF", "stroke": "swimStroke"}))
				}
			}
			row["lengths"] = outLengths
		}
		result = append(result, row)
	}
	out["laps"] = result
	return out
}
func registerSplits(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_activity_splits", description("get_activity_splits"), map[string]any{"activity_id": map[string]any{"anyOf": []any{map[string]any{"type": "integer"}, map[string]any{"type": "string"}}}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		var id int64
		var err error
		switch v := args["activity_id"].(type) {
		case string:
			id, err = strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		case float64:
			id = int64(v)
			if float64(id) != v {
				err = fmt.Errorf("invalid activity ID")
			}
		default:
			err = fmt.Errorf("invalid activity ID")
		}
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("Error retrieving activity splits: activity_id must be a positive integer")
		}
		v, err := s.Client.GetJSON(ctx, fmt.Sprintf("/activity-service/activity/%d/splits", id), nil)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving activity splits: %w", err)
		}
		if empty(v) {
			return fmt.Sprintf("No splits found for activity with ID %d", id), nil
		}
		raw, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Error retrieving activity splits: unexpected response")
		}
		return curateSplits(raw), nil
	}, "activity_id")
}
