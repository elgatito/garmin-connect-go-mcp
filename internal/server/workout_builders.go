package server

import (
	"fmt"
	"strconv"
	"strings"
)

// Port of the pure builders in garmin_mcp/workout_builders.py. They return the Garmin workout JSON as maps.

var hrZoneMap = map[string]int{"Z1": 1, "Z2": 2, "Z3": 3, "Z4": 4, "Z5": 5}

// requireMinimum rejects durations/counts below minimum; fields are checked in order.
func requireMinimum(minimum int, fields ...namedInt) error {
	for _, f := range fields {
		if f.value < minimum {
			return fmt.Errorf("%s must be >= %d", f.name, minimum)
		}
	}
	return nil
}

type namedInt struct {
	name  string
	value int
}

// zoneNumber resolves "Z3" (or a bare digit 1-5) to Garmin's zoneNumber.
func zoneNumber(zone string) (int, error) {
	up := strings.ToUpper(strings.TrimSpace(zone))
	if z, ok := hrZoneMap[up]; ok {
		return z, nil
	}
	if z, err := strconv.Atoi(up); err == nil && z >= 1 && z <= 5 {
		return z, nil
	}
	return 0, fmt.Errorf("Invalid hr_zone '%s'. Use Z1-Z5 or 1-5.", zone)
}

// hrTarget returns the target fields and a description suffix: a custom bpm range when hr_min/hr_max are given
// (hrZone is then ignored), else a named zone. prefix names the caller's parameters in errors.
func hrTarget(hrZone string, hrMin, hrMax *int, prefix string) (map[string]any, string, error) {
	if hrMin != nil || hrMax != nil {
		if hrMin == nil || hrMax == nil {
			return nil, "", fmt.Errorf("%shr_min and %shr_max must both be provided together.", prefix, prefix)
		}
		if *hrMin >= *hrMax {
			return nil, "", fmt.Errorf("%shr_min (%d) must be less than %shr_max (%d).", prefix, *hrMin, prefix, *hrMax)
		}
		return map[string]any{"targetValueOne": float64(*hrMin), "targetValueTwo": float64(*hrMax)}, fmt.Sprintf("%d-%dbpm", *hrMin, *hrMax), nil
	}
	z, err := zoneNumber(hrZone)
	if err != nil {
		return nil, "", err
	}
	return map[string]any{"zoneNumber": z}, fmt.Sprintf("Z%d", z), nil
}

func sportType(id int, key string) map[string]any {
	return map[string]any{"sportTypeId": id, "sportTypeKey": key}
}

func timeCondition() map[string]any {
	return map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"}
}

func noTarget() map[string]any {
	return map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"}
}

func hrZoneTargetType() map[string]any {
	return map[string]any{"workoutTargetTypeId": 4, "workoutTargetTypeKey": "heart.rate.zone"}
}

// execStep is an ExecutableStepDTO with a time end condition; extra fields (targets) are merged in.
func execStep(order int, typeID int, typeKey, desc string, seconds int, target map[string]any, extra map[string]any) map[string]any {
	s := map[string]any{
		"type": "ExecutableStepDTO", "stepOrder": order,
		"stepType":    map[string]any{"stepTypeId": typeID, "stepTypeKey": typeKey},
		"description": desc, "endCondition": timeCondition(), "endConditionValue": float64(seconds), "targetType": target,
	}
	for k, v := range extra {
		s[k] = v
	}
	return s
}

func repeatGroup(order, iterations int, children []any) map[string]any {
	return map[string]any{
		"type": "RepeatGroupDTO", "stepOrder": order,
		"stepType":           map[string]any{"stepTypeId": 6, "stepTypeKey": "repeat"},
		"numberOfIterations": iterations, "smartRepeat": false,
		"endCondition":      map[string]any{"conditionTypeId": 7, "conditionTypeKey": "iterations"},
		"endConditionValue": float64(iterations), "workoutSteps": children,
	}
}

func workoutDTO(name, desc string, sport map[string]any, steps []any) map[string]any {
	return map[string]any{
		"workoutName": name, "description": desc, "sportType": sport,
		"workoutSegments": []any{map[string]any{"segmentOrder": 1, "sportType": sport, "workoutSteps": steps}},
	}
}

func warmup(min int) map[string]any {
	return execStep(1, 1, "warmup", fmt.Sprintf("Warmup %d min", min), min*60, noTarget(), nil)
}

func cooldown(order, min int) map[string]any {
	return execStep(order, 2, "cooldown", fmt.Sprintf("Cooldown %d min", min), min*60, noTarget(), nil)
}

// secondsDisplay is "Nm" for whole minutes, else "Ns".
func secondsDisplay(s int) string {
	if s%60 == 0 {
		return fmt.Sprintf("%dm", s/60)
	}
	return fmt.Sprintf("%ds", s)
}

func buildRunJSON(name string, runSeconds, warmupMin, cooldownMin int, hrZone string, hrMin, hrMax *int) (map[string]any, error) {
	if err := requireMinimum(1, namedInt{"run_seconds", runSeconds}); err != nil {
		return nil, err
	}
	if err := requireMinimum(0, namedInt{"warmup_min", warmupMin}, namedInt{"cooldown_min", cooldownMin}); err != nil {
		return nil, err
	}
	fields, hrDesc, err := hrTarget(hrZone, hrMin, hrMax, "")
	if err != nil {
		return nil, err
	}
	return workoutDTO(name,
		fmt.Sprintf("%dm warmup + %s run %s + %dm cooldown", warmupMin, secondsDisplay(runSeconds), hrDesc, cooldownMin),
		sportType(1, "running"), []any{
			warmup(warmupMin),
			execStep(2, 3, "interval", fmt.Sprintf("Run %ds %s", runSeconds, hrDesc), runSeconds, hrZoneTargetType(), fields),
			cooldown(3, cooldownMin),
		}), nil
}

func buildRunIntervalJSON(name string, repeats, repSeconds, recoverySeconds, warmupMin, cooldownMin int, hrZone string, hrMin, hrMax, recHrMin, recHrMax *int) (map[string]any, error) {
	if err := requireMinimum(1, namedInt{"repeats", repeats}, namedInt{"rep_seconds", repSeconds}); err != nil {
		return nil, err
	}
	if err := requireMinimum(0, namedInt{"recovery_seconds", recoverySeconds}, namedInt{"warmup_min", warmupMin}, namedInt{"cooldown_min", cooldownMin}); err != nil {
		return nil, err
	}
	fields, hrDesc, err := hrTarget(hrZone, hrMin, hrMax, "")
	if err != nil {
		return nil, err
	}
	var recFields map[string]any
	var recDesc string
	if recHrMin != nil || recHrMax != nil {
		if recFields, recDesc, err = hrTarget("", recHrMin, recHrMax, "recovery_"); err != nil {
			return nil, err
		}
		if recoverySeconds == 0 {
			return nil, fmt.Errorf("recovery_hr_min/recovery_hr_max require recovery_seconds > 0")
		}
	}
	// The group takes one step order and its children continue the count; mis-numbering renders scrambled on the watch.
	children := []any{execStep(3, 3, "interval", fmt.Sprintf("Rep %ds %s", repSeconds, hrDesc), repSeconds, hrZoneTargetType(), fields)}
	if recoverySeconds > 0 {
		rec := execStep(4, 4, "recovery", fmt.Sprintf("Jog %ds", recoverySeconds), recoverySeconds, noTarget(), nil)
		if recFields != nil {
			rec = execStep(4, 4, "recovery", fmt.Sprintf("Jog %ds %s", recoverySeconds, recDesc), recoverySeconds, hrZoneTargetType(), recFields)
		}
		children = append(children, rec)
	}
	intervalDesc := fmt.Sprintf("%dx(%s %s", repeats, secondsDisplay(repSeconds), hrDesc)
	if recoverySeconds > 0 {
		intervalDesc += fmt.Sprintf(" / %s jog", secondsDisplay(recoverySeconds))
	}
	intervalDesc += ")"
	return workoutDTO(name,
		fmt.Sprintf("%dm warmup + %s + %dm cooldown", warmupMin, intervalDesc, cooldownMin),
		sportType(1, "running"), []any{
			warmup(warmupMin),
			repeatGroup(2, repeats, children),
			cooldown(3+len(children), cooldownMin),
		}), nil
}

func buildWalkRunJSON(name string, runSeconds, walkSeconds, repeats, warmupMin, cooldownMin int, hrZone string) (map[string]any, error) {
	if err := requireMinimum(1, namedInt{"run_seconds", runSeconds}, namedInt{"walk_seconds", walkSeconds}, namedInt{"repeats", repeats}); err != nil {
		return nil, err
	}
	if err := requireMinimum(0, namedInt{"warmup_min", warmupMin}, namedInt{"cooldown_min", cooldownMin}); err != nil {
		return nil, err
	}
	zone, err := zoneNumber(hrZone)
	if err != nil {
		return nil, err
	}
	zf := func() map[string]any { return map[string]any{"zoneNumber": zone} }
	return workoutDTO(name,
		fmt.Sprintf("%dm warmup + %dx(%ds run / %ds walk) Z%d + %dm cooldown", warmupMin, repeats, runSeconds, walkSeconds, zone, cooldownMin),
		sportType(1, "running"), []any{
			warmup(warmupMin),
			repeatGroup(2, repeats, []any{
				execStep(3, 3, "interval", fmt.Sprintf("Run %ds Z%d", runSeconds, zone), runSeconds, hrZoneTargetType(), zf()),
				execStep(4, 4, "recovery", fmt.Sprintf("Walk %ds Z%d", walkSeconds, zone), walkSeconds, hrZoneTargetType(), zf()),
			}),
			cooldown(5, cooldownMin),
		}), nil
}

func buildZ2WalkJSON(name string, durationMin, hrMin, hrMax int) (map[string]any, error) {
	if err := requireMinimum(1, namedInt{"duration_min", durationMin}); err != nil {
		return nil, err
	}
	return workoutDTO(name,
		fmt.Sprintf("Walk %d min at Z2 (%d-%d bpm)", durationMin, hrMin, hrMax),
		sportType(12, "walking"), []any{
			execStep(1, 1, "warmup", "Warmup 5 min", 300, noTarget(), nil),
			execStep(2, 3, "interval", fmt.Sprintf("Walk %d min Z2", durationMin), durationMin*60, hrZoneTargetType(), map[string]any{"zoneNumber": 2}),
			execStep(3, 2, "cooldown", "Cooldown 5 min", 300, noTarget(), nil),
		}), nil
}

// exerciseInt is int(ex.get(key, def)): absent uses def, anything else follows Python's int().
func exerciseInt(ex map[string]any, key string, def int) (int, error) {
	v, present := ex[key]
	if !present {
		return def, nil
	}
	n, err := pyInt(v)
	return int(n), err
}

func buildStrengthJSON(name string, exercises []map[string]any) (map[string]any, error) {
	steps := []any{}
	stepOrder := 1
	for index, ex := range exercises {
		exName := getDefault(ex, "name", "Exercise")
		sets, err := exerciseInt(ex, "sets", 1)
		if err != nil {
			return nil, err
		}
		reps, err := exerciseInt(ex, "reps", 1)
		if err != nil {
			return nil, err
		}
		rest, err := exerciseInt(ex, "rest_seconds", 60)
		if err != nil {
			return nil, err
		}
		err = requireMinimum(1, namedInt{"sets", sets}, namedInt{"reps", reps})
		if err == nil {
			err = requireMinimum(0, namedInt{"rest_seconds", rest})
		}
		if err != nil {
			return nil, fmt.Errorf("exercise %s: %w", pyReprValue(exName), err)
		}

		step := map[string]any{
			"type": "ExecutableStepDTO", "stepOrder": stepOrder,
			"stepType":     map[string]any{"stepTypeId": 3, "stepTypeKey": "interval"},
			"description":  fmt.Sprintf("%s: %d sets x %d reps", pyStrValue(exName), sets, reps),
			"endCondition": map[string]any{"conditionTypeId": 10, "conditionTypeKey": "reps"}, "endConditionValue": float64(reps),
			"targetType": noTarget(), "exerciseName": exName,
		}
		// Garmin rejects categories outside its enum and accepts an absent one, so only a supplied category is emitted.
		if category := ex["category"]; category != nil {
			s, ok := category.(string)
			if !ok || strings.TrimSpace(s) == "" {
				return nil, fmt.Errorf("category for exercise %s must be a non-empty string", pyReprValue(exName))
			}
			step["category"] = strings.ToUpper(strings.TrimSpace(s))
		}

		restStep := func(order int) map[string]any {
			return execStep(order, 4, "recovery", fmt.Sprintf("Rest %ds", rest), rest, noTarget(), nil)
		}
		if sets > 1 {
			// Sets = a repeat group iterated N times; children continue the workout-wide count after the group.
			work := map[string]any{}
			for k, v := range step {
				work[k] = v
			}
			work["stepOrder"] = stepOrder + 1
			group := []any{work}
			if rest > 0 {
				group = append(group, restStep(stepOrder+2))
			}
			steps = append(steps, repeatGroup(stepOrder, sets, group))
			stepOrder += 1 + len(group)
			continue // the in-group rest already separates this exercise from the next
		}
		steps = append(steps, step)
		stepOrder++
		// Compared by index, not value: identical exercises must still keep their rest step.
		if rest > 0 && index != len(exercises)-1 {
			steps = append(steps, restStep(stepOrder))
			stepOrder++
		}
	}
	return workoutDTO(name, fmt.Sprintf("Strength: %d exercises", len(exercises)), sportType(5, "strength_training"), steps), nil
}
