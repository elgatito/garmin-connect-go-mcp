package server

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"strconv"
	"strings"
)

func workoutZone(zone string) (int, error) {
	z := strings.ToUpper(strings.TrimSpace(zone))
	z = strings.TrimPrefix(z, "Z")
	n, err := strconv.Atoi(z)
	if err != nil || n < 1 || n > 5 {
		return 0, fmt.Errorf("Invalid hr_zone '%s'. Use Z1-Z5 or 1-5.", zone)
	}
	return n, nil
}
func hrTarget(zone string, minimum, maximum any, prefix string) (map[string]any, string, error) {
	if minimum != nil || maximum != nil {
		if minimum == nil || maximum == nil {
			return nil, "", fmt.Errorf("%shr_min and %shr_max must both be provided together.", prefix, prefix)
		}
		lo, ok := integer(minimum)
		hi, ok2 := integer(maximum)
		if !ok || !ok2 || lo >= hi {
			return nil, "", fmt.Errorf("%shr_min must be less than %shr_max.", prefix, prefix)
		}
		return map[string]any{"targetValueOne": float64(lo), "targetValueTwo": float64(hi)}, fmt.Sprintf("%d-%dbpm", lo, hi), nil
	}
	n, err := workoutZone(zone)
	if err != nil {
		return nil, "", err
	}
	return map[string]any{"zoneNumber": n}, fmt.Sprintf("Z%d", n), nil
}
func workoutStepTime(order, id int, key, description string, seconds int, target map[string]any) map[string]any {
	targetType := map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"}
	if target != nil {
		targetType = map[string]any{"workoutTargetTypeId": 4, "workoutTargetTypeKey": "heart.rate.zone"}
	}
	step := map[string]any{"type": "ExecutableStepDTO", "stepOrder": order, "stepType": map[string]any{"stepTypeId": id, "stepTypeKey": key}, "description": description, "endCondition": map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"}, "endConditionValue": float64(seconds), "targetType": targetType}
	for k, v := range target {
		step[k] = v
	}
	return step
}
func timeDisplay(seconds int) string {
	if seconds%60 == 0 {
		return fmt.Sprintf("%dm", seconds/60)
	}
	return fmt.Sprintf("%ds", seconds)
}
func buildAerobic(name, kind string, args map[string]any) (map[string]any, error) {
	number := func(k string, min int) (int, error) {
		v, ok := args[k]
		if !ok {
			return 0, fmt.Errorf("%s is required", k)
		}
		n, ok := integer(v)
		if !ok || n < min {
			return 0, fmt.Errorf("%s must be >= %d", k, min)
		}
		return n, nil
	}
	if kind == "create_z2_walk_workout" {
		duration, err := number("duration_min", 1)
		if err != nil {
			return nil, err
		}
		low, err := number("hr_min", 0)
		if err != nil {
			return nil, err
		}
		high, err := number("hr_max", 0)
		if err != nil {
			return nil, err
		}
		sport := map[string]any{"sportTypeId": 12, "sportTypeKey": "walking"}
		steps := []any{workoutStepTime(1, 1, "warmup", "Warmup 5 min", 300, nil), workoutStepTime(2, 3, "interval", fmt.Sprintf("Walk %d min Z2", duration), duration*60, map[string]any{"zoneNumber": 2}), workoutStepTime(3, 2, "cooldown", "Cooldown 5 min", 300, nil)}
		return map[string]any{"workoutName": name, "description": fmt.Sprintf("Walk %d min at Z2 (%d-%d bpm)", duration, low, high), "sportType": sport, "workoutSegments": []any{map[string]any{"segmentOrder": 1, "sportType": sport, "workoutSteps": steps}}}, nil
	}
	warm, err := number("warmup_min", 0)
	if err != nil {
		return nil, err
	}
	cool, err := number("cooldown_min", 0)
	if err != nil {
		return nil, err
	}
	zone := "Z3"
	if kind == "create_run_interval_workout" {
		zone = "Z4"
	}
	if z, ok := args["hr_zone"].(string); ok {
		zone = z
	}
	target, desc, err := hrTarget(zone, args["hr_min"], args["hr_max"], "")
	if err != nil {
		return nil, err
	}
	warmStep := workoutStepTime(1, 1, "warmup", fmt.Sprintf("Warmup %d min", warm), warm*60, nil)
	sport := map[string]any{"sportTypeId": 1, "sportTypeKey": "running"}
	var middle map[string]any
	coolOrder := 3
	var description string
	if kind == "create_run_workout" {
		seconds, err := number("run_seconds", 1)
		if err != nil {
			return nil, err
		}
		middle = workoutStepTime(2, 3, "interval", fmt.Sprintf("Run %ds %s", seconds, desc), seconds, target)
		description = fmt.Sprintf("%dm warmup + %s run %s + %dm cooldown", warm, timeDisplay(seconds), desc, cool)
	} else {
		repeats, err := number("repeats", 1)
		if err != nil {
			return nil, err
		}
		runKey := "rep_seconds"
		recoveryKey := "recovery_seconds"
		if kind == "create_walk_run_workout" {
			runKey = "run_seconds"
			recoveryKey = "walk_seconds"
		}
		run, err := number(runKey, 1)
		if err != nil {
			return nil, err
		}
		minRecovery := 0
		if kind == "create_walk_run_workout" {
			minRecovery = 1
		}
		recovery, err := number(recoveryKey, minRecovery)
		if err != nil {
			return nil, err
		}
		label := "Rep"
		if kind == "create_walk_run_workout" {
			label = "Run"
		}
		children := []any{workoutStepTime(3, 3, "interval", fmt.Sprintf("%s %ds %s", label, run, desc), run, target)}
		if recovery > 0 {
			recTarget := map[string]any(nil)
			recLabel := fmt.Sprintf("Jog %ds", recovery)
			if kind == "create_walk_run_workout" {
				recTarget = target
				recLabel = fmt.Sprintf("Walk %ds %s", recovery, desc)
			} else if args["recovery_hr_min"] != nil || args["recovery_hr_max"] != nil {
				var recDesc string
				recTarget, recDesc, err = hrTarget("", args["recovery_hr_min"], args["recovery_hr_max"], "recovery_")
				if err != nil {
					return nil, err
				}
				recLabel += " " + recDesc
			}
			children = append(children, workoutStepTime(4, 4, "recovery", recLabel, recovery, recTarget))
		} else if args["recovery_hr_min"] != nil || args["recovery_hr_max"] != nil {
			return nil, fmt.Errorf("recovery_hr_min/recovery_hr_max require recovery_seconds > 0")
		}
		middle = map[string]any{"type": "RepeatGroupDTO", "stepOrder": 2, "stepType": map[string]any{"stepTypeId": 6, "stepTypeKey": "repeat"}, "numberOfIterations": repeats, "smartRepeat": false, "endCondition": map[string]any{"conditionTypeId": 7, "conditionTypeKey": "iterations"}, "endConditionValue": float64(repeats), "workoutSteps": children}
		coolOrder = 3 + len(children)
		if kind == "create_walk_run_workout" {
			description = fmt.Sprintf("%dm warmup + %dx(%ds run / %ds walk) %s + %dm cooldown", warm, repeats, run, recovery, desc, cool)
		} else {
			interval := fmt.Sprintf("%dx(%s %s", repeats, timeDisplay(run), desc)
			if recovery > 0 {
				interval += " / " + timeDisplay(recovery) + " jog"
			}
			interval += ")"
			description = fmt.Sprintf("%dm warmup + %s + %dm cooldown", warm, interval, cool)
		}
	}
	steps := []any{warmStep, middle, workoutStepTime(coolOrder, 2, "cooldown", fmt.Sprintf("Cooldown %d min", cool), cool*60, nil)}
	return map[string]any{"workoutName": name, "description": description, "sportType": sport, "workoutSegments": []any{map[string]any{"segmentOrder": 1, "sportType": sport, "workoutSteps": steps}}}, nil
}
func registerAerobicBuilders(srv *mcp.Server, m *manager) {
	for _, item := range []struct {
		name, label string
		required    []string
	}{{"create_run_workout", "run workout", []string{"name", "run_seconds", "warmup_min", "cooldown_min"}}, {"create_walk_run_workout", "walk/run workout", []string{"name", "run_seconds", "walk_seconds", "repeats", "warmup_min", "cooldown_min"}}, {"create_run_interval_workout", "run interval workout", []string{"name", "repeats", "rep_seconds", "recovery_seconds", "warmup_min", "cooldown_min"}}, {"create_z2_walk_workout", "Z2 walk workout", []string{"name", "duration_min", "hr_min", "hr_max"}}} {
		item := item
		schema := map[string]any{"name": map[string]any{"type": "string"}}
		for _, key := range item.required {
			if key != "name" {
				schema[key] = map[string]any{"type": "integer"}
			}
		}
		if item.name != "create_z2_walk_workout" {
			schema["hr_zone"] = map[string]any{"type": "string"}
		}
		if item.name == "create_run_workout" || item.name == "create_run_interval_workout" {
			schema["hr_min"] = map[string]any{"type": "integer"}
			schema["hr_max"] = map[string]any{"type": "integer"}
		}
		if item.name == "create_run_interval_workout" {
			schema["recovery_hr_min"] = map[string]any{"type": "integer"}
			schema["recovery_hr_max"] = map[string]any{"type": "integer"}
		}
		addTool(srv, m, item.name, description(item.name), schema, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
			name, ok := args["name"].(string)
			if !ok {
				return nil, fmt.Errorf("Error creating %s: name is required", item.label)
			}
			workout, err := buildAerobic(name, item.name, args)
			if err != nil {
				return nil, fmt.Errorf("Error creating %s: %w", item.label, err)
			}
			v, err := uploadWorkout(ctx, s, m, item.name, workout)
			if err != nil {
				return nil, err
			}
			if item.name == "create_run_interval_workout" {
				if result, ok := v.(map[string]any); ok {
					result["message"] = "Interval workout uploaded successfully"
				}
			}
			return v, nil
		}, item.required...)
	}
}
