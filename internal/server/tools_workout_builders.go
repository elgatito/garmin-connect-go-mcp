package server

import (
	"context"
	"fmt"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

// Port of the tool registrations in garmin_mcp/workout_builders.py. Python returns plain "Error creating ...: {e}"
// strings (no write tracker), so failures are plain errors with Python's text.

type createWalkRunIn struct {
	Account
	Name        string  `json:"name" jsonschema:"Workout name (e.g. \"W3 Mié 2:2\") Max 256 characters: Garmin SILENTLY TRUNCATES longer names."`
	RunSeconds  int     `json:"run_seconds" jsonschema:"Duration of each run interval in seconds"`
	WalkSeconds int     `json:"walk_seconds" jsonschema:"Duration of each walk/recovery interval in seconds"`
	Repeats     int     `json:"repeats" jsonschema:"Number of run/walk repetitions"`
	WarmupMin   int     `json:"warmup_min" jsonschema:"Warmup duration in minutes"`
	CooldownMin int     `json:"cooldown_min" jsonschema:"Cooldown duration in minutes"`
	HrZone      *string `json:"hr_zone,omitempty" jsonschema:"Target heart-rate zone (Z1-Z5, default Z3)"`
}

type createRunIn struct {
	Account
	Name        string  `json:"name" jsonschema:"Workout name (e.g. \"Step 8 - 30min continuous\") Max 256 characters: Garmin SILENTLY TRUNCATES longer names."`
	RunSeconds  int     `json:"run_seconds" jsonschema:"Duration of the run in seconds"`
	WarmupMin   int     `json:"warmup_min" jsonschema:"Warmup walk duration in minutes"`
	CooldownMin int     `json:"cooldown_min" jsonschema:"Cooldown walk duration in minutes"`
	HrZone      *string `json:"hr_zone,omitempty" jsonschema:"Target heart-rate zone (Z1-Z5, default Z3). Ignored if hr_min/hr_max are given."`
	HrMin       *int    `json:"hr_min,omitempty" jsonschema:"Optional custom target heart rate range, minimum bpm (must be given with hr_max)"`
	HrMax       *int    `json:"hr_max,omitempty" jsonschema:"Optional custom target heart rate range, maximum bpm (must be given with hr_min)"`
}

type createRunIntervalIn struct {
	Account
	Name            string  `json:"name" jsonschema:"Workout name (e.g. \"Threshold 3x6min\")"`
	Repeats         int     `json:"repeats" jsonschema:"Number of work intervals"`
	RepSeconds      int     `json:"rep_seconds" jsonschema:"Duration of each work interval in seconds"`
	RecoverySeconds int     `json:"recovery_seconds" jsonschema:"Jog recovery between work intervals in seconds (0 to omit)"`
	WarmupMin       int     `json:"warmup_min" jsonschema:"Warmup duration in minutes"`
	CooldownMin     int     `json:"cooldown_min" jsonschema:"Cooldown duration in minutes"`
	HrZone          *string `json:"hr_zone,omitempty" jsonschema:"Target zone for the work intervals (Z1-Z5, default Z4). Ignored if hr_min/hr_max are given."`
	HrMin           *int    `json:"hr_min,omitempty" jsonschema:"Custom target HR range for the work intervals, minimum bpm (with hr_max)"`
	HrMax           *int    `json:"hr_max,omitempty" jsonschema:"Custom target HR range for the work intervals, maximum bpm (with hr_min)"`
	RecoveryHrMin   *int    `json:"recovery_hr_min,omitempty" jsonschema:"Optional HR range for recovery jogs, minimum bpm (with recovery_hr_max)"`
	RecoveryHrMax   *int    `json:"recovery_hr_max,omitempty" jsonschema:"Optional HR range for recovery jogs, maximum bpm (with recovery_hr_min)"`
}

type createZ2WalkIn struct {
	Account
	Name        string `json:"name" jsonschema:"Workout name Max 256 characters: Garmin SILENTLY TRUNCATES longer names."`
	DurationMin int    `json:"duration_min" jsonschema:"Main walking block duration in minutes"`
	HrMin       int    `json:"hr_min" jsonschema:"Minimum heart rate in bpm (used for description; target is Z2)"`
	HrMax       int    `json:"hr_max" jsonschema:"Maximum heart rate in bpm (used for description; target is Z2)"`
}

type createStrengthIn struct {
	Account
	Name      string           `json:"name" jsonschema:"Workout name Max 256 characters: Garmin SILENTLY TRUNCATES longer names."`
	Exercises []map[string]any `json:"exercises" jsonschema:"List of dicts with keys: name, sets, reps, rest_seconds and an optional category. Category is omitted from the payload when not given; Garmin accepts that. When given it must be one of Garmin's exercise categories (e.g. SQUAT, DEADLIFT, PUSH_UP, CARRY, SLED) — anything else, including \"UNASSIGNED\" and \"OTHER\", is rejected with 400 Invalid category. Full list: https://connect.garmin.com/web-data/exercises/Exercises.json"`
}

type scheduleWeekIn struct {
	Account
	Week []map[string]any `json:"week" jsonschema:"List of dicts with keys: date (YYYY-MM-DD), workout_id (int)"`
}

func registerWorkoutBuilderTools(r *registry) {
	addTool(r, "create_walk_run_workout", createWalkRunDesc, func(ctx context.Context, s *Session, in createWalkRunIn) (any, error) {
		w, err := buildWalkRunJSON(in.Name, in.RunSeconds, in.WalkSeconds, in.Repeats, in.WarmupMin, in.CooldownMin, orDefault(in.HrZone, "Z3"))
		return uploadBuilt(ctx, s.Client, "Error creating walk/run workout", "Workout uploaded successfully", w, err)
	})
	addTool(r, "create_run_workout", createRunDesc, func(ctx context.Context, s *Session, in createRunIn) (any, error) {
		w, err := buildRunJSON(in.Name, in.RunSeconds, in.WarmupMin, in.CooldownMin, orDefault(in.HrZone, "Z3"), in.HrMin, in.HrMax)
		return uploadBuilt(ctx, s.Client, "Error creating run workout", "Workout uploaded successfully", w, err)
	})
	addTool(r, "create_run_interval_workout", createRunIntervalDesc, func(ctx context.Context, s *Session, in createRunIntervalIn) (any, error) {
		w, err := buildRunIntervalJSON(in.Name, in.Repeats, in.RepSeconds, in.RecoverySeconds, in.WarmupMin, in.CooldownMin,
			orDefault(in.HrZone, "Z4"), in.HrMin, in.HrMax, in.RecoveryHrMin, in.RecoveryHrMax)
		return uploadBuilt(ctx, s.Client, "Error creating run interval workout", "Interval workout uploaded successfully", w, err)
	})
	addTool(r, "create_z2_walk_workout", createZ2WalkDesc, func(ctx context.Context, s *Session, in createZ2WalkIn) (any, error) {
		w, err := buildZ2WalkJSON(in.Name, in.DurationMin, in.HrMin, in.HrMax)
		return uploadBuilt(ctx, s.Client, "Error creating Z2 walk workout", "Workout uploaded successfully", w, err)
	})
	addTool(r, "create_strength_workout", createStrengthDesc, func(ctx context.Context, s *Session, in createStrengthIn) (any, error) {
		w, err := buildStrengthJSON(in.Name, in.Exercises)
		return uploadBuilt(ctx, s.Client, "Error creating strength workout", "Workout uploaded successfully", w, err)
	})
	addTool(r, "schedule_week", scheduleWeekDesc, func(ctx context.Context, s *Session, in scheduleWeekIn) (any, error) {
		results := []any{}
		for _, item := range in.Week {
			results = append(results, scheduleWeekItem(ctx, s.Client, item))
		}
		return map[string]any{"status": "complete", "scheduled": results}, nil
	})
}

func orDefault(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

// uploadBuilt uploads a built workout as-is (Python calls upload_workout directly, without the upload-path repairs) and
// curates the response. buildErr is the builder's error.
func uploadBuilt(ctx context.Context, c *garmin.Client, errPrefix, message string, w map[string]any, buildErr error) (any, error) {
	if buildErr != nil {
		return nil, fmt.Errorf("%s: %w", errPrefix, buildErr)
	}
	result, err := c.UploadWorkout(ctx, w)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		return result, nil
	}
	return dropNil(map[string]any{"status": "success", "workout_id": m["workoutId"], "name": m["workoutName"], "message": message}), nil
}

// scheduleWeekItem handles one entry on its own so a failing item never discards the results of the others.
func scheduleWeekItem(ctx context.Context, c *garmin.Client, item map[string]any) map[string]any {
	calendarDate, workoutID := item["date"], item["workout_id"]
	entry := func(status string, extra map[string]any) map[string]any {
		m := map[string]any{"date": calendarDate, "workout_id": workoutID, "status": status}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	// Validate here: isAlreadyScheduled swallows a bad date, which would then be POSTed as-is.
	if _, err := validateWorkoutDate(calendarDate, "date"); err != nil {
		return entry("failed", map[string]any{"message": err.Error()})
	}
	date := pyStr(calendarDate) // validateWorkoutDate accepted it, so it is a string
	id, err := pyInt(workoutID)
	if workoutID == nil {
		err = fmt.Errorf("Missing required field: workout_id")
	} else if err == nil {
		workoutID = id // Python reports the converted id once int() succeeded
		if id < 1 {
			err = fmt.Errorf("workout_id must be a positive integer")
		}
	}
	if err != nil {
		return entry("failed", map[string]any{"message": "workout_id is required and must be a positive integer"})
	}
	if isAlreadyScheduled(ctx, c, id, date) {
		return entry("already_scheduled", map[string]any{"idempotent": true})
	}
	resp, err := c.ScheduleWorkout(ctx, id, date)
	if err != nil {
		return entry("error", map[string]any{"message": "Error scheduling workout: " + err.Error()})
	}
	out := entry("scheduled", nil)
	if sid := asMap(resp)["workoutScheduleId"]; sid != nil {
		out["scheduled_workout_id"] = sid
	}
	return out
}
