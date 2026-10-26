package server

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	createWalkRunWorkoutDescription = `Create a walk/run interval workout and upload it to Garmin Connect.

Builds the internal Garmin JSON automatically and returns the new workout ID.`

	createRunWorkoutDescription = `Create a continuous run workout and upload it to Garmin Connect.

Builds a single uninterrupted run interval with warmup and cooldown walks.

Targets a named Garmin heart-rate zone by default. Named zones (Z1-Z5)
don't line up with every real training target -- e.g. a 136-148 bpm
Zone 2 goal straddles Garmin's Z2 (118-137) and Z3 (138-157). Pass
hr_min and hr_max together to target that exact bpm range instead;
the watch will then show "in range" only for the range you actually
want, not a whole zone that over- or under-shoots it.`

	createRunIntervalWorkoutDescription = `Create a repeat-based interval run workout and upload it to Garmin Connect.

Builds warmup -> repeats x [work interval + jog recovery] -> cooldown. Use this
instead of create_run_workout whenever the session has more than one work
interval (threshold reps, VO2 intervals, cruise intervals, strides).

Work intervals target a named heart-rate zone by default, but named zones are
usually far too wide for interval work: a threshold session at 172-180 bpm sits
inside a Z3 that may span 158-184. Pass hr_min and hr_max together to target the
exact bpm range, so the watch alerts as soon as the effort drifts off target.

Recovery steps carry no target unless recovery_hr_min/max are given -- a capped
recovery alerts constantly right after a hard rep, since heart rate lags effort.
Recovery HR bounds require recovery_seconds > 0.`

	createZ2WalkWorkoutDescription = `Create a steady Z2 walking workout and upload it to Garmin Connect.`

	createStrengthWorkoutDescription = `Create a strength workout and upload it to Garmin Connect.

Each exercise becomes a reps-based step; when sets > 1 the exercise is
emitted as a repeat group of that many iterations (work + rest per set),
so Garmin shows the real set count. The name is kept in the step
description; it is also sent as exerciseName, which Garmin only retains when
it matches one of its own exercise keys (e.g. "FARMERS_CARRY").`

	scheduleWeekDescription = `Schedule a list of workouts for the week in a single call.

Idempotent: if a workout is already scheduled for that date, it is
reported as already scheduled and the POST is skipped (avoids
duplicating calendar entries).

Each entry is handled on its own: an invalid or failing entry is
reported with status "failed" or "error" and the rest still run.
A successful entry includes scheduled_workout_id when Garmin returns
the calendar-entry ID; pass it to unschedule_workout to remove the entry.`
)

type createWalkRunWorkoutInput struct {
	Account
	Name        string `json:"name" jsonschema:"Workout name (e.g. \"W3 Mié 2:2\"). Max 256 characters: Garmin SILENTLY TRUNCATES longer names."`
	RunSeconds  int    `json:"run_seconds" jsonschema:"Duration of each run interval in seconds"`
	WalkSeconds int    `json:"walk_seconds" jsonschema:"Duration of each walk/recovery interval in seconds"`
	Repeats     int    `json:"repeats" jsonschema:"Number of run/walk repetitions"`
	WarmupMin   int    `json:"warmup_min" jsonschema:"Warmup duration in minutes"`
	CooldownMin int    `json:"cooldown_min" jsonschema:"Cooldown duration in minutes"`
	HRZone      string `json:"hr_zone,omitempty" jsonschema:"Target heart-rate zone (Z1-Z5, default Z3)"`
}

type createRunWorkoutInput struct {
	Account
	Name        string `json:"name" jsonschema:"Workout name (e.g. \"Step 8 - 30min continuous\"). Max 256 characters: Garmin SILENTLY TRUNCATES longer names."`
	RunSeconds  int    `json:"run_seconds" jsonschema:"Duration of the run in seconds"`
	WarmupMin   int    `json:"warmup_min" jsonschema:"Warmup walk duration in minutes"`
	CooldownMin int    `json:"cooldown_min" jsonschema:"Cooldown walk duration in minutes"`
	HRZone      string `json:"hr_zone,omitempty" jsonschema:"Target heart-rate zone (Z1-Z5, default Z3). Ignored if hr_min/hr_max are given."`
	HRMin       *int   `json:"hr_min,omitempty" jsonschema:"Optional custom target heart rate range, minimum bpm (must be given with hr_max)"`
	HRMax       *int   `json:"hr_max,omitempty" jsonschema:"Optional custom target heart rate range, maximum bpm (must be given with hr_min)"`
}

type createRunIntervalWorkoutInput struct {
	Account
	Name            string `json:"name" jsonschema:"Workout name (e.g. \"Threshold 3x6min\")"`
	Repeats         int    `json:"repeats" jsonschema:"Number of work intervals"`
	RepSeconds      int    `json:"rep_seconds" jsonschema:"Duration of each work interval in seconds"`
	RecoverySeconds int    `json:"recovery_seconds" jsonschema:"Jog recovery between work intervals in seconds (0 to omit)"`
	WarmupMin       int    `json:"warmup_min" jsonschema:"Warmup duration in minutes"`
	CooldownMin     int    `json:"cooldown_min" jsonschema:"Cooldown duration in minutes"`
	HRZone          string `json:"hr_zone,omitempty" jsonschema:"Target zone for the work intervals (Z1-Z5, default Z4). Ignored if hr_min/hr_max are given."`
	HRMin           *int   `json:"hr_min,omitempty" jsonschema:"Custom target HR range for the work intervals, minimum bpm (with hr_max)"`
	HRMax           *int   `json:"hr_max,omitempty" jsonschema:"Custom target HR range for the work intervals, maximum bpm (with hr_min)"`
	RecoveryHRMin   *int   `json:"recovery_hr_min,omitempty" jsonschema:"Optional HR range for recovery jogs, minimum bpm (with recovery_hr_max)"`
	RecoveryHRMax   *int   `json:"recovery_hr_max,omitempty" jsonschema:"Optional HR range for recovery jogs, maximum bpm (with recovery_hr_min)"`
}

type createZ2WalkWorkoutInput struct {
	Account
	Name        string `json:"name" jsonschema:"Workout name. Max 256 characters: Garmin SILENTLY TRUNCATES longer names."`
	DurationMin int    `json:"duration_min" jsonschema:"Main walking block duration in minutes"`
	HRMin       int    `json:"hr_min" jsonschema:"Minimum heart rate in bpm (used for description; target is Z2)"`
	HRMax       int    `json:"hr_max" jsonschema:"Maximum heart rate in bpm (used for description; target is Z2)"`
}

type createStrengthWorkoutInput struct {
	Account
	Name      string           `json:"name" jsonschema:"Workout name. Max 256 characters: Garmin SILENTLY TRUNCATES longer names."`
	Exercises []map[string]any `json:"exercises" jsonschema:"List of dicts with keys: name, sets, reps, rest_seconds and an optional category. Category is omitted from the payload when not given; Garmin accepts that. When given it must be one of Garmin's exercise categories (e.g. SQUAT, DEADLIFT, PUSH_UP, CARRY, SLED) — anything else, including \"UNASSIGNED\" and \"OTHER\", is rejected with 400 Invalid category."`
}

type scheduleWeekInput struct {
	Account
	Week []map[string]any `json:"week" jsonschema:"List of dicts with keys: date (YYYY-MM-DD), workout_id (int)"`
}

func registerWorkoutBuilderTools(r *registry) {
	addTool(r, "create_walk_run_workout", createWalkRunWorkoutDescription, createWalkRunWorkout)
	addTool(r, "create_run_workout", createRunWorkoutDescription, createRunWorkout)
	addTool(r, "create_run_interval_workout", createRunIntervalWorkoutDescription, createRunIntervalWorkout)
	addTool(r, "create_z2_walk_workout", createZ2WalkWorkoutDescription, createZ2WalkWorkout)
	addTool(r, "create_strength_workout", createStrengthWorkoutDescription, createStrengthWorkout)
	addTool(r, "schedule_week", scheduleWeekDescription, scheduleWeek)
}

// --- builders ---

var hrZoneMap = map[string]int{"Z1": 1, "Z2": 2, "Z3": 3, "Z4": 4, "Z5": 5}

func requireMinimum(minimum int, names []string, values []int) error {
	for i, name := range names {
		if values[i] < minimum {
			return fmt.Errorf("%s must be >= %d", name, minimum)
		}
	}
	return nil
}

func zoneNumber(zone string) (int, error) {
	upper := strings.ToUpper(strings.TrimSpace(zone))
	if z, ok := hrZoneMap[upper]; ok {
		return z, nil
	}
	if n, err := strconv.Atoi(upper); err == nil && n >= 1 && n <= 5 {
		return n, nil
	}
	return 0, fmt.Errorf("Invalid hr_zone '%s'. Use Z1-Z5 or 1-5.", zone)
}

func hrTarget(hrZone string, hrMin, hrMax *int, prefix string) (map[string]any, string, error) {
	if hrMin != nil || hrMax != nil {
		if hrMin == nil || hrMax == nil {
			return nil, "", fmt.Errorf("%shr_min and %shr_max must both be provided together.", prefix, prefix)
		}
		if *hrMin >= *hrMax {
			return nil, "", fmt.Errorf("%shr_min (%d) must be less than %shr_max (%d).", prefix, *hrMin, prefix, *hrMax)
		}
		return map[string]any{"targetValueOne": float64(*hrMin), "targetValueTwo": float64(*hrMax)},
			fmt.Sprintf("%d-%dbpm", *hrMin, *hrMax), nil
	}
	zone, err := zoneNumber(hrZone)
	if err != nil {
		return nil, "", err
	}
	return map[string]any{"zoneNumber": zone}, fmt.Sprintf("Z%d", zone), nil
}

func runDisplay(seconds int) string {
	if seconds%60 == 0 {
		return fmt.Sprintf("%dm", seconds/60)
	}
	return fmt.Sprintf("%ds", seconds)
}

func buildRunJSON(name string, runSeconds, warmupMin, cooldownMin int, hrZone string, hrMin, hrMax *int) (map[string]any, error) {
	if err := requireMinimum(1, []string{"run_seconds"}, []int{runSeconds}); err != nil {
		return nil, err
	}
	if err := requireMinimum(0, []string{"warmup_min", "cooldown_min"}, []int{warmupMin, cooldownMin}); err != nil {
		return nil, err
	}
	targetFields, hrDesc, err := hrTarget(hrZone, hrMin, hrMax, "")
	if err != nil {
		return nil, err
	}
	interval := map[string]any{
		"type": "ExecutableStepDTO", "stepOrder": 2,
		"stepType":          map[string]any{"stepTypeId": 3, "stepTypeKey": "interval"},
		"description":       fmt.Sprintf("Run %ds %s", runSeconds, hrDesc),
		"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
		"endConditionValue": float64(runSeconds),
		"targetType":        map[string]any{"workoutTargetTypeId": 4, "workoutTargetTypeKey": "heart.rate.zone"},
	}
	for k, v := range targetFields {
		interval[k] = v
	}
	return map[string]any{
		"workoutName": name,
		"description": fmt.Sprintf("%dm warmup + %s run %s + %dm cooldown", warmupMin, runDisplay(runSeconds), hrDesc, cooldownMin),
		"sportType":   map[string]any{"sportTypeId": 1, "sportTypeKey": "running"},
		"workoutSegments": []any{map[string]any{
			"segmentOrder": 1,
			"sportType":    map[string]any{"sportTypeId": 1, "sportTypeKey": "running"},
			"workoutSteps": []any{
				map[string]any{
					"type": "ExecutableStepDTO", "stepOrder": 1,
					"stepType":          map[string]any{"stepTypeId": 1, "stepTypeKey": "warmup"},
					"description":       fmt.Sprintf("Warmup %d min", warmupMin),
					"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
					"endConditionValue": float64(warmupMin * 60),
					"targetType":        map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"},
				},
				interval,
				map[string]any{
					"type": "ExecutableStepDTO", "stepOrder": 3,
					"stepType":          map[string]any{"stepTypeId": 2, "stepTypeKey": "cooldown"},
					"description":       fmt.Sprintf("Cooldown %d min", cooldownMin),
					"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
					"endConditionValue": float64(cooldownMin * 60),
					"targetType":        map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"},
				},
			},
		}},
	}, nil
}

func buildRunIntervalJSON(name string, repeats, repSeconds, recoverySeconds, warmupMin, cooldownMin int, hrZone string, hrMin, hrMax, recoveryHRMin, recoveryHRMax *int) (map[string]any, error) {
	if err := requireMinimum(1, []string{"repeats", "rep_seconds"}, []int{repeats, repSeconds}); err != nil {
		return nil, err
	}
	if err := requireMinimum(0, []string{"recovery_seconds", "warmup_min", "cooldown_min"}, []int{recoverySeconds, warmupMin, cooldownMin}); err != nil {
		return nil, err
	}
	targetFields, hrDesc, err := hrTarget(hrZone, hrMin, hrMax, "")
	if err != nil {
		return nil, err
	}
	var recFields map[string]any
	recDesc := ""
	if recoveryHRMin != nil || recoveryHRMax != nil {
		recFields, recDesc, err = hrTarget("", recoveryHRMin, recoveryHRMax, "recovery_")
		if err != nil {
			return nil, err
		}
		if recoverySeconds == 0 {
			return nil, errors.New("recovery_hr_min/recovery_hr_max require recovery_seconds > 0")
		}
	}
	work := map[string]any{
		"type": "ExecutableStepDTO", "stepOrder": 3,
		"stepType":          map[string]any{"stepTypeId": 3, "stepTypeKey": "interval"},
		"description":       fmt.Sprintf("Rep %ds %s", repSeconds, hrDesc),
		"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
		"endConditionValue": float64(repSeconds),
		"targetType":        map[string]any{"workoutTargetTypeId": 4, "workoutTargetTypeKey": "heart.rate.zone"},
	}
	for k, v := range targetFields {
		work[k] = v
	}
	children := []any{work}
	if recoverySeconds > 0 {
		recovery := map[string]any{
			"type": "ExecutableStepDTO", "stepOrder": 4,
			"stepType":          map[string]any{"stepTypeId": 4, "stepTypeKey": "recovery"},
			"description":       fmt.Sprintf("Jog %ds", recoverySeconds),
			"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
			"endConditionValue": float64(recoverySeconds),
			"targetType":        map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"},
		}
		if recFields != nil {
			recovery["targetType"] = map[string]any{"workoutTargetTypeId": 4, "workoutTargetTypeKey": "heart.rate.zone"}
			for k, v := range recFields {
				recovery[k] = v
			}
			recovery["description"] = fmt.Sprintf("Jog %ds %s", recoverySeconds, recDesc)
		}
		children = append(children, recovery)
	}
	cooldownOrder := 3 + len(children)
	intervalDescription := fmt.Sprintf("%dx(%s %s", repeats, runDisplay(repSeconds), hrDesc)
	if recoverySeconds > 0 {
		intervalDescription += fmt.Sprintf(" / %s jog", runDisplay(recoverySeconds))
	}
	intervalDescription += ")"
	return map[string]any{
		"workoutName": name,
		"description": fmt.Sprintf("%dm warmup + %s + %dm cooldown", warmupMin, intervalDescription, cooldownMin),
		"sportType":   map[string]any{"sportTypeId": 1, "sportTypeKey": "running"},
		"workoutSegments": []any{map[string]any{
			"segmentOrder": 1,
			"sportType":    map[string]any{"sportTypeId": 1, "sportTypeKey": "running"},
			"workoutSteps": []any{
				map[string]any{
					"type": "ExecutableStepDTO", "stepOrder": 1,
					"stepType":          map[string]any{"stepTypeId": 1, "stepTypeKey": "warmup"},
					"description":       fmt.Sprintf("Warmup %d min", warmupMin),
					"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
					"endConditionValue": float64(warmupMin * 60),
					"targetType":        map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"},
				},
				map[string]any{
					"type": "RepeatGroupDTO", "stepOrder": 2,
					"stepType":           map[string]any{"stepTypeId": 6, "stepTypeKey": "repeat"},
					"numberOfIterations": repeats,
					"smartRepeat":        false,
					"endCondition":       map[string]any{"conditionTypeId": 7, "conditionTypeKey": "iterations"},
					"endConditionValue":  float64(repeats),
					"workoutSteps":       children,
				},
				map[string]any{
					"type": "ExecutableStepDTO", "stepOrder": cooldownOrder,
					"stepType":          map[string]any{"stepTypeId": 2, "stepTypeKey": "cooldown"},
					"description":       fmt.Sprintf("Cooldown %d min", cooldownMin),
					"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
					"endConditionValue": float64(cooldownMin * 60),
					"targetType":        map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"},
				},
			},
		}},
	}, nil
}

func buildWalkRunJSON(name string, runSeconds, walkSeconds, repeats, warmupMin, cooldownMin int, hrZone string) (map[string]any, error) {
	if err := requireMinimum(1, []string{"run_seconds", "walk_seconds", "repeats"}, []int{runSeconds, walkSeconds, repeats}); err != nil {
		return nil, err
	}
	if err := requireMinimum(0, []string{"warmup_min", "cooldown_min"}, []int{warmupMin, cooldownMin}); err != nil {
		return nil, err
	}
	zone, err := zoneNumber(hrZone)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"workoutName": name,
		"description": fmt.Sprintf("%dm warmup + %dx(%ds run / %ds walk) Z%d + %dm cooldown",
			warmupMin, repeats, runSeconds, walkSeconds, zone, cooldownMin),
		"sportType": map[string]any{"sportTypeId": 1, "sportTypeKey": "running"},
		"workoutSegments": []any{map[string]any{
			"segmentOrder": 1,
			"sportType":    map[string]any{"sportTypeId": 1, "sportTypeKey": "running"},
			"workoutSteps": []any{
				map[string]any{
					"type": "ExecutableStepDTO", "stepOrder": 1,
					"stepType":          map[string]any{"stepTypeId": 1, "stepTypeKey": "warmup"},
					"description":       fmt.Sprintf("Warmup %d min", warmupMin),
					"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
					"endConditionValue": float64(warmupMin * 60),
					"targetType":        map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"},
				},
				map[string]any{
					"type": "RepeatGroupDTO", "stepOrder": 2,
					"stepType":           map[string]any{"stepTypeId": 6, "stepTypeKey": "repeat"},
					"numberOfIterations": repeats,
					"smartRepeat":        false,
					"endCondition":       map[string]any{"conditionTypeId": 7, "conditionTypeKey": "iterations"},
					"endConditionValue":  float64(repeats),
					"workoutSteps": []any{
						map[string]any{
							"type": "ExecutableStepDTO", "stepOrder": 3,
							"stepType":          map[string]any{"stepTypeId": 3, "stepTypeKey": "interval"},
							"description":       fmt.Sprintf("Run %ds Z%d", runSeconds, zone),
							"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
							"endConditionValue": float64(runSeconds),
							"targetType":        map[string]any{"workoutTargetTypeId": 4, "workoutTargetTypeKey": "heart.rate.zone"},
							"zoneNumber":        zone,
						},
						map[string]any{
							"type": "ExecutableStepDTO", "stepOrder": 4,
							"stepType":          map[string]any{"stepTypeId": 4, "stepTypeKey": "recovery"},
							"description":       fmt.Sprintf("Walk %ds Z%d", walkSeconds, zone),
							"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
							"endConditionValue": float64(walkSeconds),
							"targetType":        map[string]any{"workoutTargetTypeId": 4, "workoutTargetTypeKey": "heart.rate.zone"},
							"zoneNumber":        zone,
						},
					},
				},
				map[string]any{
					"type": "ExecutableStepDTO", "stepOrder": 5,
					"stepType":          map[string]any{"stepTypeId": 2, "stepTypeKey": "cooldown"},
					"description":       fmt.Sprintf("Cooldown %d min", cooldownMin),
					"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
					"endConditionValue": float64(cooldownMin * 60),
					"targetType":        map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"},
				},
			},
		}},
	}, nil
}

func buildZ2WalkJSON(name string, durationMin, hrMin, hrMax int) (map[string]any, error) {
	if err := requireMinimum(1, []string{"duration_min"}, []int{durationMin}); err != nil {
		return nil, err
	}
	return map[string]any{
		"workoutName": name,
		"description": fmt.Sprintf("Walk %d min at Z2 (%d-%d bpm)", durationMin, hrMin, hrMax),
		"sportType":   map[string]any{"sportTypeId": 12, "sportTypeKey": "walking"},
		"workoutSegments": []any{map[string]any{
			"segmentOrder": 1,
			"sportType":    map[string]any{"sportTypeId": 12, "sportTypeKey": "walking"},
			"workoutSteps": []any{
				map[string]any{
					"type": "ExecutableStepDTO", "stepOrder": 1,
					"stepType":          map[string]any{"stepTypeId": 1, "stepTypeKey": "warmup"},
					"description":       "Warmup 5 min",
					"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
					"endConditionValue": 300.0,
					"targetType":        map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"},
				},
				map[string]any{
					"type": "ExecutableStepDTO", "stepOrder": 2,
					"stepType":          map[string]any{"stepTypeId": 3, "stepTypeKey": "interval"},
					"description":       fmt.Sprintf("Walk %d min Z2", durationMin),
					"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
					"endConditionValue": float64(durationMin * 60),
					"targetType":        map[string]any{"workoutTargetTypeId": 4, "workoutTargetTypeKey": "heart.rate.zone"},
					"zoneNumber":        2,
				},
				map[string]any{
					"type": "ExecutableStepDTO", "stepOrder": 3,
					"stepType":          map[string]any{"stepTypeId": 2, "stepTypeKey": "cooldown"},
					"description":       "Cooldown 5 min",
					"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
					"endConditionValue": 300.0,
					"targetType":        map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"},
				},
			},
		}},
	}, nil
}

// strengthIntField distinguishes a missing key (Python's default) from a
// present-but-unconvertible value, which Python's int(...) rejects.
func strengthIntField(exercise map[string]any, key string, defaultValue int) (int, error) {
	value, exists := exercise[key]
	if !exists {
		return defaultValue, nil
	}
	n, err := pythonInt(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return n, nil
}

func strengthRestStep(order, restSeconds int) map[string]any {
	return map[string]any{
		"type": "ExecutableStepDTO", "stepOrder": order,
		"stepType":          map[string]any{"stepTypeId": 4, "stepTypeKey": "recovery"},
		"description":       fmt.Sprintf("Rest %ds", restSeconds),
		"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
		"endConditionValue": float64(restSeconds),
		"targetType":        map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"},
	}
}

func buildStrengthJSON(name string, exercises []map[string]any) (map[string]any, error) {
	steps := []any{}
	stepOrder := 1
	for index, exercise := range exercises {
		exNameValue, exists := exercise["name"]
		if !exists {
			exNameValue = "Exercise"
		}
		exName := fmt.Sprint(exNameValue)
		sets, err := strengthIntField(exercise, "sets", 1)
		if err != nil {
			return nil, fmt.Errorf("exercise %s: %w", pythonRepr(exName), err)
		}
		reps, err := strengthIntField(exercise, "reps", 1)
		if err != nil {
			return nil, fmt.Errorf("exercise %s: %w", pythonRepr(exName), err)
		}
		restSeconds, err := strengthIntField(exercise, "rest_seconds", 60)
		if err != nil {
			return nil, fmt.Errorf("exercise %s: %w", pythonRepr(exName), err)
		}
		if err := requireMinimum(1, []string{"sets", "reps"}, []int{sets, reps}); err != nil {
			return nil, fmt.Errorf("exercise %s: %w", pythonRepr(exName), err)
		}
		if err := requireMinimum(0, []string{"rest_seconds"}, []int{restSeconds}); err != nil {
			return nil, fmt.Errorf("exercise %s: %w", pythonRepr(exName), err)
		}

		step := map[string]any{
			"type": "ExecutableStepDTO", "stepOrder": stepOrder,
			"stepType":          map[string]any{"stepTypeId": 3, "stepTypeKey": "interval"},
			"description":       fmt.Sprintf("%s: %d sets x %d reps", exName, sets, reps),
			"endCondition":      map[string]any{"conditionTypeId": 10, "conditionTypeKey": "reps"},
			"endConditionValue": float64(reps),
			"targetType":        map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"},
			"exerciseName":      exNameValue,
		}
		if category, exists := exercise["category"]; exists && category != nil {
			categoryString, ok := category.(string)
			if !ok || strings.TrimSpace(categoryString) == "" {
				return nil, fmt.Errorf("category for exercise %s must be a non-empty string", pythonRepr(exName))
			}
			step["category"] = strings.ToUpper(strings.TrimSpace(categoryString))
		}

		if sets > 1 {
			workStep := map[string]any{}
			for k, v := range step {
				workStep[k] = v
			}
			workStep["stepOrder"] = stepOrder + 1
			groupSteps := []any{workStep}
			if restSeconds > 0 {
				groupSteps = append(groupSteps, strengthRestStep(stepOrder+2, restSeconds))
			}
			steps = append(steps, map[string]any{
				"type": "RepeatGroupDTO", "stepOrder": stepOrder,
				"stepType":           map[string]any{"stepTypeId": 6, "stepTypeKey": "repeat"},
				"numberOfIterations": sets,
				"smartRepeat":        false,
				"endCondition":       map[string]any{"conditionTypeId": 7, "conditionTypeKey": "iterations"},
				"endConditionValue":  float64(sets),
				"workoutSteps":       groupSteps,
			})
			stepOrder += 1 + len(groupSteps)
			continue
		}
		steps = append(steps, step)
		stepOrder++
		if restSeconds > 0 && index != len(exercises)-1 {
			steps = append(steps, strengthRestStep(stepOrder, restSeconds))
			stepOrder++
		}
	}
	return map[string]any{
		"workoutName": name,
		"description": fmt.Sprintf("Strength: %d exercises", len(exercises)),
		"sportType":   map[string]any{"sportTypeId": 5, "sportTypeKey": "strength_training"},
		"workoutSegments": []any{map[string]any{
			"segmentOrder": 1,
			"sportType":    map[string]any{"sportTypeId": 5, "sportTypeKey": "strength_training"},
			"workoutSteps": steps,
		}},
	}, nil
}

// --- tool handlers ---

func createWorkoutFromBuilder(ctx context.Context, s *Session, tool, errorPrefix, successMessage string, workout map[string]any, err error) (any, error) {
	if err != nil {
		return fmt.Sprintf("%s: %v", errorPrefix, err), nil
	}
	tracker := newWriteTracker(tool, s.logWriter()).withPrefix(errorPrefix)
	tracker.Sent()
	result, err := s.Client.UploadWorkout(ctx, workout)
	if err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	return uploadResultValue(result, successMessage), nil
}

func createWalkRunWorkout(ctx context.Context, s *Session, in createWalkRunWorkoutInput) (any, error) {
	hrZone := in.HRZone
	if hrZone == "" {
		hrZone = "Z3"
	}
	workout, err := buildWalkRunJSON(in.Name, in.RunSeconds, in.WalkSeconds, in.Repeats, in.WarmupMin, in.CooldownMin, hrZone)
	return createWorkoutFromBuilder(ctx, s, "create_walk_run_workout", "Error creating walk/run workout", "Workout uploaded successfully", workout, err)
}

func createRunWorkout(ctx context.Context, s *Session, in createRunWorkoutInput) (any, error) {
	hrZone := in.HRZone
	if hrZone == "" {
		hrZone = "Z3"
	}
	workout, err := buildRunJSON(in.Name, in.RunSeconds, in.WarmupMin, in.CooldownMin, hrZone, in.HRMin, in.HRMax)
	return createWorkoutFromBuilder(ctx, s, "create_run_workout", "Error creating run workout", "Workout uploaded successfully", workout, err)
}

func createRunIntervalWorkout(ctx context.Context, s *Session, in createRunIntervalWorkoutInput) (any, error) {
	hrZone := in.HRZone
	if hrZone == "" {
		hrZone = "Z4"
	}
	workout, err := buildRunIntervalJSON(in.Name, in.Repeats, in.RepSeconds, in.RecoverySeconds, in.WarmupMin, in.CooldownMin, hrZone, in.HRMin, in.HRMax, in.RecoveryHRMin, in.RecoveryHRMax)
	return createWorkoutFromBuilder(ctx, s, "create_run_interval_workout", "Error creating run interval workout", "Interval workout uploaded successfully", workout, err)
}

func createZ2WalkWorkout(ctx context.Context, s *Session, in createZ2WalkWorkoutInput) (any, error) {
	workout, err := buildZ2WalkJSON(in.Name, in.DurationMin, in.HRMin, in.HRMax)
	return createWorkoutFromBuilder(ctx, s, "create_z2_walk_workout", "Error creating Z2 walk workout", "Workout uploaded successfully", workout, err)
}

func createStrengthWorkout(ctx context.Context, s *Session, in createStrengthWorkoutInput) (any, error) {
	workout, err := buildStrengthJSON(in.Name, in.Exercises)
	return createWorkoutFromBuilder(ctx, s, "create_strength_workout", "Error creating strength workout", "Workout uploaded successfully", workout, err)
}

func scheduleWeek(ctx context.Context, s *Session, in scheduleWeekInput) (any, error) {
	results := []any{}
	for _, item := range in.Week {
		dateValue := item["date"]
		workoutIDValue := item["workout_id"]
		date, err := validateWorkoutDateValue(dateValue, "date")
		if err != nil {
			results = append(results, map[string]any{
				"date": dateValue, "workout_id": workoutIDValue, "status": "failed", "message": err.Error(),
			})
			continue
		}
		workoutID, err := workoutIDNumber(workoutIDValue)
		if workoutIDValue == nil || err != nil || workoutID < 1 {
			results = append(results, map[string]any{
				"date": date, "workout_id": workoutIDValue, "status": "failed",
				"message": "workout_id is required and must be a positive integer",
			})
			continue
		}
		if alreadyScheduled(ctx, s, workoutID, date) {
			results = append(results, map[string]any{
				"date": date, "workout_id": workoutID, "status": "already_scheduled", "idempotent": true,
			})
			continue
		}
		tracker := newWriteTracker("schedule_week", s.logWriter()).withPrefix("Error scheduling workout")
		tracker.Sent()
		result, err := s.Client.ScheduleWorkout(ctx, workoutID, date)
		if err != nil {
			results = append(results, map[string]any{
				"date": date, "workout_id": workoutID, "status": "error", "message": writeFailureMessage(tracker, err),
			})
			continue
		}
		tracker.Committed()
		entry := map[string]any{"date": date, "workout_id": workoutID, "status": "scheduled"}
		if payload, ok := result.(map[string]any); ok && payload["workoutScheduleId"] != nil {
			entry["scheduled_workout_id"] = payload["workoutScheduleId"]
		}
		results = append(results, entry)
	}
	return map[string]any{"status": "complete", "scheduled": results}, nil
}
