package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"math"
	"net/http"
	"strings"
)

func buildExerciseSets(sets []any) ([]any, error) {
	out := []any{}
	for i, v := range sets {
		s, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("set %d must be an object", i+1)
		}
		category, _ := s["category"].(string)
		category = strings.ToUpper(strings.TrimSpace(category))
		if category == "" {
			return nil, fmt.Errorf("set %d: category is required", i+1)
		}
		name, _ := s["exercise_name"].(string)
		name = strings.ToUpper(strings.TrimSpace(name))
		var exercise any
		if name != "" && name != category {
			exercise = name
		}
		if s["weight_kg"] != nil && s["weight_lb"] != nil {
			return nil, fmt.Errorf("set %d: give weight_kg or weight_lb, not both", i+1)
		}
		weight := float64(-1)
		for k, multiplier := range map[string]float64{"weight_kg": 1, "weight_lb": 0.45359237} {
			if s[k] != nil {
				n, ok := s[k].(float64)
				if !ok || n < 0 || math.IsNaN(n) || math.IsInf(n, 0) {
					return nil, fmt.Errorf("set %d: weight cannot be negative", i+1)
				}
				weight = math.RoundToEven(n * multiplier * 1000)
			}
		}
		reps := s["reps"]
		if reps != nil {
			n, ok := reps.(float64)
			if !ok || n < 0 || n != math.Trunc(n) {
				return nil, fmt.Errorf("set %d: reps must be a non-negative whole number", i+1)
			}
		}
		out = append(out, map[string]any{"setType": "ACTIVE", "exercises": []any{map[string]any{"category": category, "name": exercise, "probability": 100}}, "repetitionCount": reps, "weight": weight, "duration": s["duration_seconds"]})
		if truthy(s["rest_seconds"]) {
			out = append(out, map[string]any{"setType": "REST", "exercises": []any{}, "repetitionCount": nil, "weight": -1, "duration": s["rest_seconds"]})
		}
	}
	return out, nil
}
func registerExerciseSets(srv *mcp.Server, m *manager) {
	addTool(srv, m, "set_activity_exercise_sets", description("set_activity_exercise_sets"), map[string]any{"activity_id": map[string]any{"anyOf": []any{map[string]any{"type": "integer"}, map[string]any{"type": "string"}}}, "sets": map[string]any{"anyOf": []any{map[string]any{"type": "array", "items": map[string]any{"type": "object"}}, map[string]any{"type": "string"}}}, "keep_timing": map[string]any{"type": "boolean", "default": false}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		id, err := activityID(args["activity_id"])
		if err != nil {
			return nil, fmt.Errorf("Error updating activity exercise sets: %w", err)
		}
		raw := args["sets"]
		if text, ok := raw.(string); ok {
			if err = json.Unmarshal([]byte(text), &raw); err != nil {
				return nil, err
			}
		}
		sets, ok := raw.([]any)
		if !ok {
			return nil, fmt.Errorf("sets must be a list")
		}
		if len(sets) == 0 {
			return "sets cannot be empty", nil
		}
		newSets, err := buildExerciseSets(sets)
		if err != nil {
			return nil, fmt.Errorf("Error updating activity exercise sets: %w", err)
		}
		tracker := newWriteTracker("set_activity_exercise_sets", "Error updating activity exercise sets", m.opt.Log)
		path := fmt.Sprintf("/activity-service/activity/%d/exerciseSets", id)
		keep, _ := args["keep_timing"].(bool)
		if keep {
			v, err := s.Client.GetJSON(ctx, path, nil)
			if err != nil {
				return nil, tracker.Fail(err)
			}
			data, _ := v.(map[string]any)
			existing, _ := data["exerciseSets"].([]any)
			active := 0
			for _, v := range existing {
				e, _ := v.(map[string]any)
				if e["setType"] == "ACTIVE" {
					active++
				}
			}
			if active != len(sets) {
				return fmt.Sprintf("keep_timing needs one set per recorded set: activity %d has %d active sets, got %d", id, active, len(sets)), nil
			}
			index := 0
			for _, v := range existing {
				e, _ := v.(map[string]any)
				if e["setType"] != "ACTIVE" {
					continue
				}
				for index < len(newSets) {
					n := newSets[index].(map[string]any)
					index++
					if n["setType"] == "ACTIVE" {
						for _, k := range []string{"exercises", "repetitionCount", "weight"} {
							e[k] = n[k]
						}
						break
					}
				}
			}
			newSets = existing
		}
		tracker.Sent()
		_, err = s.Client.Do(ctx, garmin.Req{Method: http.MethodPut, Path: path, JSON: map[string]any{"activityId": id, "exerciseSets": newSets}})
		if err != nil {
			return nil, tracker.Fail(err)
		}
		tracker.Committed()
		return map[string]any{"success": true, "activity_id": id, "sets_written": len(sets), "message": "Activity exercise sets successfully replaced"}, nil
	}, "activity_id", "sets")
}
