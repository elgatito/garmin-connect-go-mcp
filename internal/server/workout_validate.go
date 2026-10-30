package server

import (
	"fmt"
	"reflect"
	"strconv"
)

type workoutStepRef struct {
	data map[string]any
	path string
}

func workoutStepRefs(workout map[string]any) ([]workoutStepRef, error) {
	var result []workoutStepRef
	var walk func(any, string) error
	walk = func(v any, path string) error {
		step, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		result = append(result, workoutStepRef{step, path})
		nested := step["workoutSteps"]
		if nested == nil {
			return nil
		}
		children, ok := nested.([]any)
		if !ok {
			return fmt.Errorf("%s.workoutSteps must be a list", path)
		}
		for i, v := range children {
			if err := walk(v, fmt.Sprintf("%s.workoutSteps[%d]", path, i)); err != nil {
				return err
			}
		}
		return nil
	}
	segments := workout["workoutSegments"]
	if segments == nil {
		return result, nil
	}
	list, ok := segments.([]any)
	if !ok {
		return nil, fmt.Errorf("workoutSegments must be a list")
	}
	for i, v := range list {
		path := fmt.Sprintf("workoutSegments[%d]", i)
		seg, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s must be an object", path)
		}
		raw := seg["workoutSteps"]
		if raw == nil {
			continue
		}
		steps, ok := raw.([]any)
		if !ok {
			return nil, fmt.Errorf("%s.workoutSteps must be a list", path)
		}
		for j, v := range steps {
			if err := walk(v, fmt.Sprintf("%s.workoutSteps[%d]", path, j)); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}
func integer(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		i := int(n)
		return i, n == float64(i)
	case int:
		return n, true
	case string:
		i, err := strconv.Atoi(n)
		return i, err == nil
	}
	return 0, false
}
func validateWorkout(workout map[string]any) error {
	refs, err := workoutStepRefs(workout)
	if err != nil {
		return err
	}
	layouts := map[string][]string{"targetType": {"targetValueOne", "targetValueTwo", "zoneNumber"}, "secondaryTargetType": {"secondaryTargetValueOne", "secondaryTargetValueTwo", "secondaryZoneNumber"}}
	for _, ref := range refs {
		for target, fields := range layouts {
			block, _ := ref.data[target].(map[string]any)
			if block == nil {
				continue
			}
			for _, field := range fields {
				nested := block[field]
				top := ref.data[field]
				if nested != nil && top != nil && !reflect.DeepEqual(nested, top) {
					return fmt.Errorf("%s.%s conflicts with %s.%s.%s; keep only the step-level %s", ref.path, field, ref.path, target, field, field)
				}
			}
			zone := ref.data[fields[2]]
			if zone == nil {
				zone = block[fields[2]]
			}
			for _, field := range fields[:2] {
				value := ref.data[field]
				if value == nil {
					value = block[field]
				}
				if zone != nil && value != nil {
					return fmt.Errorf("%s mixes %s with custom range fields; use either a named zone or a custom range", ref.path, fields[2])
				}
			}
		}
	}
	for _, ref := range refs {
		step := ref.data
		for target, fields := range layouts {
			block, _ := step[target].(map[string]any)
			for _, field := range fields {
				v, exists := block[field]
				if exists {
					delete(block, field)
					if v != nil && step[field] == nil {
						step[field] = v
					}
				}
			}
		}
		target, _ := step["targetType"].(map[string]any)
		if target["workoutTargetTypeKey"] == "heart.rate.zone" && step["zoneNumber"] == nil {
			if n, ok := integer(step["targetValueOne"]); ok && n >= 1 && n <= 5 {
				step["zoneNumber"] = n
				delete(step, "targetValueOne")
				delete(step, "targetValueTwo")
			}
		}
		condition, _ := step["endCondition"].(map[string]any)
		if step["type"] == "RepeatGroupDTO" {
			if condition["conditionTypeKey"] == "iterations" && condition["conditionTypeId"] == nil {
				condition["conditionTypeId"] = 7
			}
			if step["numberOfIterations"] == nil && step["endConditionValue"] != nil {
				n, ok := integer(step["endConditionValue"])
				if !ok {
					return fmt.Errorf("%s.endConditionValue must be numeric", ref.path)
				}
				step["numberOfIterations"] = n
			}
		}
		endIDs := map[string]int{"lap.button": 1, "time": 2, "distance": 3, "calories": 4, "power": 5, "heart.rate": 6, "iterations": 7, "fixed.rest": 8, "fixed.repetition": 9, "reps": 10, "training.peaks.tss": 11}
		key, _ := condition["conditionTypeKey"].(string)
		rawID := condition["conditionTypeId"]
		id, valid := integer(rawID)
		if rawID != nil && !valid {
			return fmt.Errorf("%s.endCondition.conditionTypeId must be numeric", ref.path)
		}
		if rawID != nil {
			condition["conditionTypeId"] = id
		}
		if expected, known := endIDs[key]; known && (rawID == nil || id != expected) {
			return fmt.Errorf("%s.endCondition conditionTypeKey '%s' requires conditionTypeId %d", ref.path, key, expected)
		}
		if key != "" {
			for expectedKey, expectedID := range endIDs {
				if id == expectedID && key != expectedKey {
					return fmt.Errorf("%s.endCondition conditionTypeId %d requires conditionTypeKey '%s'", ref.path, id, expectedKey)
				}
			}
		}
		targetIDs := map[string]int{"no.target": 1, "power.zone": 2, "heart.rate.zone": 4, "pace.zone": 6, "power.between": 6}
		for _, field := range []string{"targetType", "secondaryTargetType"} {
			block, _ := step[field].(map[string]any)
			if block == nil {
				continue
			}
			targetID := block["workoutTargetTypeId"]
			id, valid := integer(targetID)
			if targetID != nil && !valid {
				return fmt.Errorf("%s.%s.workoutTargetTypeId must be numeric", ref.path, field)
			}
			if targetID != nil {
				block["workoutTargetTypeId"] = id
			}
			key, _ := block["workoutTargetTypeKey"].(string)
			if expected, known := targetIDs[key]; known && targetID != nil && id != expected {
				return fmt.Errorf("%s.%s mismatch: workoutTargetTypeKey '%s' requires workoutTargetTypeId %d, not %d", ref.path, field, key, expected, id)
			}
			if targetID != nil && key != "" && (id == 1 || id == 2 || id == 4 || id == 6) {
				expected, known := targetIDs[key]
				if !known || expected != id {
					return fmt.Errorf("%s.%s mismatch: workoutTargetTypeId %d does not match %q", ref.path, field, id, key)
				}
			}
		}
	}
	return nil
}
