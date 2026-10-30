package server

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func curateActivity(raw map[string]any) map[string]any {
	out := map[string]any{}
	extract := func(source map[string]any, fields map[string]string) {
		for k, key := range fields {
			if source[key] != nil {
				out[k] = source[key]
			}
		}
	}
	extract(raw, map[string]string{"id": "activityId", "name": "activityName", "description": "description"})
	typ, _ := raw["activityTypeDTO"].(map[string]any)
	extract(typ, map[string]string{"type": "typeKey", "parent_type": "parentTypeId"})
	event, _ := raw["eventTypeDTO"].(map[string]any)
	extract(event, map[string]string{"event_type": "typeKey"})
	summary, _ := raw["summaryDTO"].(map[string]any)
	extract(summary, map[string]string{"start_time_local": "startTimeLocal", "start_time_gmt": "startTimeGMT", "duration_seconds": "duration", "moving_duration_seconds": "movingDuration", "elapsed_duration_seconds": "elapsedDuration", "distance_meters": "distance", "avg_speed_mps": "averageSpeed", "max_speed_mps": "maxSpeed", "avg_hr_bpm": "averageHR", "max_hr_bpm": "maxHR", "min_hr_bpm": "minHR", "calories": "calories", "bmr_calories": "bmrCalories", "avg_cadence": "averageRunCadence", "max_cadence": "maxRunCadence", "avg_stride_length_cm": "strideLength", "avg_ground_contact_time_ms": "groundContactTime", "avg_vertical_oscillation_cm": "verticalOscillation", "steps": "steps", "avg_power_watts": "averagePower", "max_power_watts": "maxPower", "normalized_power_watts": "normalizedPower", "training_effect": "trainingEffect", "anaerobic_training_effect": "anaerobicTrainingEffect", "training_effect_label": "trainingEffectLabel", "training_load": "activityTrainingLoad", "moderate_intensity_minutes": "moderateIntensityMinutes", "vigorous_intensity_minutes": "vigorousIntensityMinutes", "elevation_gain_meters": "elevationGain", "elevation_loss_meters": "elevationLoss", "max_elevation_meters": "maxElevation", "min_elevation_meters": "minElevation", "recovery_hr_bpm": "recoveryHeartRate", "body_battery_impact": "differenceBodyBattery", "workout_feel": "directWorkoutFeel", "workout_rpe": "directWorkoutRpe"})
	metadata, _ := raw["metadataDTO"].(map[string]any)
	extract(metadata, map[string]string{"lap_count": "lapCount", "has_splits": "hasSplits", "device_manufacturer": "manufacturer"})
	return out
}
func registerActivityDetail(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_activity", description("get_activity"), map[string]any{"activity_id": map[string]any{"anyOf": []any{map[string]any{"type": "integer"}, map[string]any{"type": "string"}}}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
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
			return nil, fmt.Errorf("Error retrieving activity: activity_id must be a positive integer")
		}
		v, err := s.Client.GetJSON(ctx, fmt.Sprintf("/activity-service/activity/%d", id), nil)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving activity: %w", err)
		}
		if empty(v) {
			return fmt.Sprintf("No activity found with ID %d", id), nil
		}
		raw, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Error retrieving activity: unexpected response")
		}
		return curateActivity(raw), nil
	}, "activity_id")
}
