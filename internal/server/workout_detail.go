package server

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func truthy(v any) bool {
	if v == nil {
		return false
	}
	switch x := v.(type) {
	case string:
		return x != ""
	case float64:
		return x != 0
	case bool:
		return x
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}
func workoutStep(s map[string]any) map[string]any {
	out := map[string]any{}
	if s["stepOrder"] != nil {
		out["order"] = s["stepOrder"]
	}
	typ, _ := s["stepType"].(map[string]any)
	if typ["stepTypeKey"] != nil {
		out["type"] = typ["stepTypeKey"]
	}
	condition, _ := s["endCondition"].(map[string]any)
	if truthy(condition["conditionTypeKey"]) {
		out["end_condition"] = condition["conditionTypeKey"]
	}
	for k, src := range map[string]string{"description": "description", "end_condition_value": "endConditionValue", "category": "category", "exercise_name": "exerciseName"} {
		if truthy(s[src]) {
			out[k] = s[src]
		}
	}
	for _, secondary := range []bool{false, true} {
		prefix := ""
		source := ""
		if secondary {
			prefix = "secondary_"
			source = "secondary"
		}
		targetKey := source + "TargetType"
		if source == "" {
			targetKey = "targetType"
		}
		target, _ := s[targetKey].(map[string]any)
		key, _ := target["workoutTargetTypeKey"].(string)
		if key == "" || key == "no.target" {
			continue
		}
		out[prefix+"target_type"] = key
		for k, suffix := range map[string]string{"target_value_low": "TargetValueOne", "target_value_high": "TargetValueTwo", "target_zone": "ZoneNumber"} {
			field := source + suffix
			if source == "" {
				field = strings.ToLower(suffix[:1]) + suffix[1:]
			}
			if s[field] != nil {
				out[prefix+k] = s[field]
			}
		}
		unitField := source + "TargetValueUnit"
		if source == "" {
			unitField = "targetValueUnit"
		}
		unit, _ := s[unitField].(map[string]any)
		if truthy(unit["unitKey"]) {
			out[prefix+"target_value_unit"] = unit["unitKey"]
		} else if key == "pace.zone" && (out[prefix+"target_value_low"] != nil || out[prefix+"target_value_high"] != nil) {
			out[prefix+"target_value_unit"] = "meters_per_second"
		}
	}
	for k, src := range map[string]string{"stroke_type": "strokeType", "equipment_type": "equipmentType", "drill_type": "drillType"} {
		obj, _ := s[src].(map[string]any)
		if truthy(obj[src+"Key"]) {
			out[k] = obj[src+"Key"]
		}
	}
	if s["weightValue"] != nil {
		out["weight_value"] = s["weightValue"]
		unit, _ := s["weightUnit"].(map[string]any)
		if truthy(unit["unitKey"]) {
			out["weight_unit"] = unit["unitKey"]
		}
	}
	if s["type"] == "RepeatGroupDTO" {
		if s["numberOfIterations"] != nil {
			out["repeat_count"] = s["numberOfIterations"]
		}
		if steps, ok := s["workoutSteps"].([]any); ok && len(steps) > 0 {
			out["steps"] = workoutSteps(steps)
			out["step_count"] = len(steps)
		}
	}
	return out
}
func workoutSteps(steps []any) []any {
	out := []any{}
	for _, v := range steps {
		if s, ok := v.(map[string]any); ok {
			out = append(out, workoutStep(s))
		}
	}
	return out
}
func workoutDetails(raw map[string]any) map[string]any {
	out := remapFields(raw, map[string]string{"id": "workoutId", "uuid": "workoutUuid", "name": "workoutName", "provider": "workoutProvider", "created_date": "createdDate", "updated_date": "updatedDate"})
	sport, _ := raw["sportType"].(map[string]any)
	if sport["sportTypeKey"] != nil {
		out["sport"] = sport["sportTypeKey"]
	}
	for k, src := range map[string]string{"description": "description", "avg_training_speed_mps": "avgTrainingSpeed", "pool_length": "poolLength", "workout_type": "workoutPhrase", "training_effect_label": "trainingEffectLabel", "estimated_training_effect": "estimatedTrainingEffect"} {
		if truthy(raw[src]) {
			out[k] = raw[src]
		}
	}
	for k, keys := range map[string][]string{"estimated_duration_seconds": {"estimatedDuration", "estimatedDurationInSecs"}, "estimated_distance_meters": {"estimatedDistance", "estimatedDistanceInMeters"}} {
		for _, key := range keys {
			if truthy(raw[key]) {
				out[k] = raw[key]
				break
			}
		}
	}
	if truthy(raw["poolLength"]) {
		unit, _ := raw["poolLengthUnit"].(map[string]any)
		if truthy(unit["unitKey"]) {
			out["pool_length_unit"] = unit["unitKey"]
		}
	}
	if segments, ok := raw["workoutSegments"].([]any); ok && len(segments) > 0 {
		list := []any{}
		for _, v := range segments {
			seg, ok := v.(map[string]any)
			if !ok {
				continue
			}
			entry := remapFields(seg, map[string]string{"order": "segmentOrder"})
			sport, _ := seg["sportType"].(map[string]any)
			if sport["sportTypeKey"] != nil {
				entry["sport"] = sport["sportTypeKey"]
			}
			for k, src := range map[string]string{"estimated_duration_seconds": "estimatedDurationInSecs", "estimated_distance_meters": "estimatedDistanceInMeters"} {
				if truthy(seg[src]) {
					entry[k] = seg[src]
				}
			}
			if steps, ok := seg["workoutSteps"].([]any); ok && len(steps) > 0 {
				entry["steps"] = workoutSteps(steps)
				entry["step_count"] = len(steps)
			}
			list = append(list, entry)
		}
		out["segments"] = list
		out["segment_count"] = len(segments)
	}
	return out
}
func registerWorkoutDetail(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_workout_by_id", description("get_workout_by_id"), map[string]any{"workout_id": map[string]any{"anyOf": []any{map[string]any{"type": "integer"}, map[string]any{"type": "string"}}}, "raw": map[string]any{"type": "boolean", "default": false}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		id := fmt.Sprint(args["workout_id"])
		path := "/workout-service/workout/"
		if strings.Contains(id, "-") {
			if !regexp.MustCompile(`^[A-Za-z0-9-]+$`).MatchString(id) {
				return nil, fmt.Errorf("Error retrieving workout: invalid workout ID")
			}
			path = "/workout-service/fbt-adaptive/"
		} else {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("Error retrieving workout: invalid workout ID")
			}
			id = strconv.FormatInt(n, 10)
		}
		v, err := s.Client.GetJSON(ctx, path+url.PathEscape(id), nil)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving workout: %w", err)
		}
		if empty(v) {
			return fmt.Sprintf("No workout found with ID %s.", id), nil
		}
		if raw, _ := args["raw"].(bool); raw {
			return v, nil
		}
		value, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Error retrieving workout: unexpected response")
		}
		return workoutDetails(value), nil
	}, "workout_id")
}
