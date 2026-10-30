package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"strings"
)

func buildStrength(name string, exercises []any) (map[string]any, error) {
	steps := []any{}
	order := 1
	for index, v := range exercises {
		ex, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("exercise must be an object")
		}
		exName := "Exercise"
		if v, ok := ex["name"].(string); ok {
			exName = v
		}
		sets, err := intArg(ex, "sets", 1)
		if err != nil || sets < 1 {
			return nil, fmt.Errorf("exercise '%s': sets must be >= 1", exName)
		}
		reps, err := intArg(ex, "reps", 1)
		if err != nil || reps < 1 {
			return nil, fmt.Errorf("exercise '%s': reps must be >= 1", exName)
		}
		rest, err := intArg(ex, "rest_seconds", 60)
		if err != nil || rest < 0 {
			return nil, fmt.Errorf("exercise '%s': rest_seconds must be >= 0", exName)
		}
		step := map[string]any{"type": "ExecutableStepDTO", "stepOrder": order, "stepType": map[string]any{"stepTypeId": 3, "stepTypeKey": "interval"}, "description": fmt.Sprintf("%s: %d sets x %d reps", exName, sets, reps), "endCondition": map[string]any{"conditionTypeId": 10, "conditionTypeKey": "reps"}, "endConditionValue": float64(reps), "targetType": map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"}, "exerciseName": exName}
		if ex["category"] != nil {
			category, ok := ex["category"].(string)
			if !ok || strings.TrimSpace(category) == "" {
				return nil, fmt.Errorf("category for exercise '%s' must be a non-empty string", exName)
			}
			step["category"] = strings.ToUpper(strings.TrimSpace(category))
		}
		if sets > 1 {
			step["stepOrder"] = order + 1
			children := []any{step}
			if rest > 0 {
				children = append(children, workoutStepTime(order+2, 4, "recovery", fmt.Sprintf("Rest %ds", rest), rest, nil))
			}
			steps = append(steps, map[string]any{"type": "RepeatGroupDTO", "stepOrder": order, "stepType": map[string]any{"stepTypeId": 6, "stepTypeKey": "repeat"}, "numberOfIterations": sets, "smartRepeat": false, "endCondition": map[string]any{"conditionTypeId": 7, "conditionTypeKey": "iterations"}, "endConditionValue": float64(sets), "workoutSteps": children})
			order += 1 + len(children)
			continue
		}
		steps = append(steps, step)
		order++
		if rest > 0 && index != len(exercises)-1 {
			steps = append(steps, workoutStepTime(order, 4, "recovery", fmt.Sprintf("Rest %ds", rest), rest, nil))
			order++
		}
	}
	sport := map[string]any{"sportTypeId": 5, "sportTypeKey": "strength_training"}
	return map[string]any{"workoutName": name, "description": fmt.Sprintf("Strength: %d exercises", len(exercises)), "sportType": sport, "workoutSegments": []any{map[string]any{"segmentOrder": 1, "sportType": sport, "workoutSteps": steps}}}, nil
}
func registerStrengthBuilder(srv *mcp.Server, m *manager) {
	addTool(srv, m, "create_strength_workout", description("create_strength_workout"), map[string]any{"name": map[string]any{"type": "string"}, "exercises": map[string]any{"anyOf": []any{map[string]any{"type": "array", "items": map[string]any{"type": "object"}}, map[string]any{"type": "string"}}}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		name, ok := args["name"].(string)
		if !ok {
			return nil, fmt.Errorf("name is required")
		}
		v := args["exercises"]
		if str, ok := v.(string); ok {
			if err := json.Unmarshal([]byte(str), &v); err != nil {
				return nil, err
			}
		}
		exercises, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("exercises must be a list")
		}
		workout, err := buildStrength(name, exercises)
		if err != nil {
			return nil, fmt.Errorf("Error creating strength workout: %w", err)
		}
		return uploadWorkout(ctx, s, m, "create_strength_workout", workout)
	}, "name", "exercises")
}
