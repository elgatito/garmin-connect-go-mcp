package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

const (
	getWorkoutsDescription = `Get all workouts with curated summary list

Returns a count and list of workout summaries with essential metadata only.
For detailed workout information including segments, use get_workout_by_id.`

	getWorkoutByIDDescription = `Get detailed information for a specific workout

Returns workout details including segments and step structure.

Accepts either:
- Numeric workout ID (from get_workouts, get_scheduled_workouts, or
  training-plan families that expose workout_id)
- Workout UUID (from adaptive Garmin Coach/training-plan workouts)

Rest-day UUIDs can resolve to a minimal record without a workout name
or segments.

Pace target values are speeds in meters per second. target_value_low
and target_value_high preserve Garmin's targetValueOne/targetValueTwo
order; they are not sorted by magnitude.`

	downloadWorkoutDescription = `Download a workout as a FIT file

Downloads the workout in FIT format. The binary data cannot be returned
directly through the MCP interface, but this confirms the workout is available.`

	uploadWorkoutDescription = `Upload a workout from JSON data

Creates a new workout in Garmin Connect from structured workout data.

IMPORTANT: Step types must use Garmin's DTO format:
- Use "ExecutableStepDTO" for regular steps (warmup, interval, cooldown, recovery)
- Use "RepeatGroupDTO" for repeat/interval groups with numberOfIterations.
  Always include endCondition with conditionTypeId 7 and conditionTypeKey
  "iterations"; omitting conditionTypeId causes the API to silently corrupt
  the repeat count.
- stepOrder is one depth-first counter over the whole workout: a repeat
  group takes the next number and its nested steps continue from there
  (group 2, children 3 and 4, cooldown 5). Do not restart at 1 inside a group.

IMPORTANT: Heart rate targets come in two forms:
- Named zone (e.g. Zone 2): set targetType to "heart.rate.zone" and use "zoneNumber" (1-5).
  Do NOT put the zone number in targetValueOne.
- Custom HR range (e.g. 105-143 bpm): set targetType to "heart.rate.zone" and use
  "targetValueOne" (low bpm) / "targetValueTwo" (high bpm). Do NOT set "zoneNumber".
  This matches Garmin Connect's "Custom" heart rate target.
For non-HR targets (pace, power, cadence), use targetValueOne/targetValueTwo directly.
Pace targets are speeds in meters per second, not min/km:
5:00/km = 1000 m / 300 s = 3.333 m/s.
Target values are fields on the workout step, alongside targetType; do not put
targetValueOne, targetValueTwo, or zoneNumber inside the targetType object.
Use either zoneNumber or targetValueOne/targetValueTwo, not both. Garmin silently
discards a custom range when a named zone is also present.

Note: a safety check converts targetValueOne 1-5 to zoneNumber when zoneNumber is missing,
to catch the common mistake of putting a zone index in targetValueOne. Typical bpm values
(e.g. 105, 143) are not affected.

IMPORTANT: Target type IDs and keys must match Garmin's canonical mapping.
Garmin treats workoutTargetTypeId as authoritative, so mismatches are rejected
before upload.  Known mappings:
- workoutTargetTypeId 1  -> "no.target"
- workoutTargetTypeId 2  -> "power.zone"  (cycling power zone 1-7, use zoneNumber)
- workoutTargetTypeId 4  -> "heart.rate.zone"
- workoutTargetTypeId 6  -> "pace.zone" (running/swim) OR "power.between" (cycling)

IMPORTANT: For cycling power targets use the correct target type:
- Power zone (zone 1-7 based on FTP %): use workoutTargetTypeId 2, key "power.zone",
  and "zoneNumber" (1-7).
- Custom % FTP range (e.g. 76-81% FTP): use workoutTargetTypeId 2, key "power.zone",
  "targetValueOne"/"targetValueTwo" as percentages and
  "targetValueUnit": {"unitKey": "percent"}. Garmin Connect shows it as
  "Custom Power 76-81% FTP" with the matching watts.
- Absolute watt range (e.g. 200-250 W): use workoutTargetTypeId 6, key "power.between",
  and "targetValueOne" (low watts) / "targetValueTwo" (high watts).
Using workoutTargetTypeId 2 with key "power.between" is a silent Garmin bug: the
workout uploads but Garmin stores it as "power.zone" and the intent is lost.

Use {"workoutTargetTypeId": 4, "workoutTargetTypeKey": "heart.rate.zone"} with
targetValueOne/targetValueTwo for custom heart-rate ranges.

IMPORTANT: Sport type IDs for workouts (different from activity API!):
- 1 = running, 2 = cycling, 5 = strength_training, 6 = cardio_training, 12 = walking

IMPORTANT: End condition IDs and keys must match Garmin's canonical mapping.
Garmin treats conditionTypeId as authoritative, so mismatches such as
{"conditionTypeId": 4, "conditionTypeKey": "heart.rate"} are rejected before
upload because Garmin would interpret them as "calories". Use
{"conditionTypeId": 6, "conditionTypeKey": "heart.rate"} for heart-rate
end conditions.

**Available Templates:**
Instead of building workout JSON from scratch, you can use these MCP resources as starting points:
- workout://templates/simple-run - Basic warmup/run/cooldown structure
- workout://templates/interval-running - Interval training with repeat groups
- workout://templates/tempo-run - Tempo run with heart rate zone targets
- workout://templates/strength-circuit - Strength training with exercises, reps, rest
- workout://reference/structure - Complete JSON structure reference with all fields

Access these resources using your MCP client's resource reading capability, modify the template
as needed, and pass the resulting JSON as the workout_data parameter.

**Strength training workouts** require these additional fields on each exercise step:
- "category": exercise category (e.g. "BENCH_PRESS", "PULL_UP", "CURL", "SHOULDER_PRESS",
  "ROW", "SQUAT", "DEADLIFT", "TRICEPS_EXTENSION", "PLANK", "LUNGE", "CARDIO")
- "exerciseName": specific exercise (e.g. "BARBELL_BENCH_PRESS", "PULL_UP",
  "DUMBBELL_BICEPS_CURL", "DUMBBELL_SHOULDER_PRESS", "BENT_OVER_ROW_WITH_DUMBELL",
  "BODY_WEIGHT_DIP", "BARBELL_SQUAT", "BARBELL_DEADLIFT")
- "weightValue" (optional): weight as number (e.g. 24.0)
- "weightUnit" (optional): {"unitId": 8, "unitKey": "kilogram", "factor": 1000.0}
Use endCondition reps (conditionTypeId: 10) for exercises, rest (stepTypeId: 5) between sets.

Example strength exercise step:
{
    "type": "ExecutableStepDTO",
    "stepOrder": 1,
    "stepType": {"stepTypeId": 3, "stepTypeKey": "interval"},
    "endCondition": {"conditionTypeId": 10, "conditionTypeKey": "reps"},
    "endConditionValue": 10.0,
    "targetType": {"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"},
    "category": "BENCH_PRESS",
    "exerciseName": "BARBELL_BENCH_PRESS",
    "weightValue": 60.0,
    "weightUnit": {"unitId": 8, "unitKey": "kilogram", "factor": 1000.0}
}

Example running workout with HR zone target:
{
    "workoutName": "My Workout",
    "sportType": {"sportTypeId": 1, "sportTypeKey": "running"},
    "workoutSegments": [{
        "segmentOrder": 1,
        "sportType": {"sportTypeId": 1, "sportTypeKey": "running"},
        "workoutSteps": [{
            "type": "ExecutableStepDTO",
            "stepOrder": 1,
            "stepType": {"stepTypeId": 3, "stepTypeKey": "interval"},
            "endCondition": {"conditionTypeId": 2, "conditionTypeKey": "time"},
            "endConditionValue": 1200.0,
            "targetType": {"workoutTargetTypeId": 4, "workoutTargetTypeKey": "heart.rate.zone"},
            "zoneNumber": 3
        }]
    }]
}`

	uploadWorkoutsDescription = `Upload multiple workouts from JSON data in a single call

Creates multiple new workouts in Garmin Connect. Each item in the list
uses the same structure as upload_workout.

IMPORTANT: Step types must use Garmin's DTO format:
- Use "ExecutableStepDTO" for regular steps (warmup, interval, cooldown, recovery)
- Use "RepeatGroupDTO" for repeat/interval groups with numberOfIterations.
  Always include endCondition with conditionTypeId 7 and conditionTypeKey
  "iterations"; omitting conditionTypeId causes the API to silently corrupt
  the repeat count.
- stepOrder is one depth-first counter over the whole workout: a repeat
  group takes the next number and its nested steps continue from there
  (group 2, children 3 and 4, cooldown 5). Do not restart at 1 inside a group.

IMPORTANT: For named heart rate zone targets, use "zoneNumber" (1-5), NOT targetValueOne/targetValueTwo.
For custom heart-rate ranges, use targetType {"workoutTargetTypeId": 4,
"workoutTargetTypeKey": "heart.rate.zone"} with targetValueOne/targetValueTwo.
Target values belong on the workout step, alongside targetType, not inside it.
For cycling power zone targets (zone-based), use workoutTargetTypeId 2, key "power.zone".
For cycling absolute watt range targets, use workoutTargetTypeId 6, key "power.between",
with targetValueOne (low watts) and targetValueTwo (high watts).
Pace targets use targetValueOne/targetValueTwo in meters per second, not
min/km: 5:00/km = 1000 m / 300 s = 3.333 m/s.
Target type IDs and keys must match Garmin's canonical mapping.

IMPORTANT: End condition IDs and keys must match Garmin's canonical mapping.
Garmin treats conditionTypeId as authoritative, so mismatches are rejected before upload.`

	deleteWorkoutDescription = `Delete a workout from Garmin Connect

Permanently removes a workout from your Garmin Connect workout library.
This cannot be undone. Call once with confirm=false (the default) to
preview the workout, then call again with confirm=true to delete it.`

	deleteWorkoutsDescription = `Delete multiple workouts from Garmin Connect in a single call

Permanently removes multiple workouts from your Garmin Connect workout library.
This cannot be undone. Call once with confirm=false (the default) to
preview the workouts, then call again with confirm=true to delete them.`

	scheduledWorkoutsDescription = `Get scheduled workouts between two dates with curated summary list

Returns workouts that have been scheduled on the Garmin Connect calendar,
including their scheduled dates and completion status.`

	garminCoachWorkoutsDescription = `Get Garmin Coach workouts around the given date

Returns workouts from the active Garmin Coach/training plan, including
plan metadata, workout identifiers, dates, sport, duration, completion
status, rest days, race days, and workout intent when Garmin provides
them. Adaptive plans expose only Garmin's currently generated window,
typically the current week; future dates may return no workouts even
while a plan is active. The count includes rest-day entries.

Garmin's standalone Daily Suggested Workouts are generated on compatible
devices. As of July 31, 2026, no supported or known Garmin Connect
web/API endpoint, including those exposed by this project's
python-garminconnect dependency, returns the device's upcoming DSW
schedule. This tool returns Garmin Coach/training-plan workouts and does
not synthesize device-generated suggestions.

This is the preferred tool for Garmin Coach requests. The legacy
get_training_plan_workouts tool returns the same data; do not call both.

Adaptive Coach plans typically expose workout_uuid; other plan families
may expose numeric workout_id. Pass whichever identifier is present to
get_workout_by_id. Rest-day UUIDs may return minimal detail without
workout segments.`

	trainingPlanWorkoutsDescription = `Compatibility alias for get_garmin_coach_workouts

Prefer get_garmin_coach_workouts for new requests. This legacy tool
returns the same Garmin Coach/training-plan data; do not call both for
one request. Adaptive plans expose only Garmin's currently generated
window, typically the current week; future dates may return no workouts
even while a plan is active.

Adaptive training plans typically expose workout_uuid; other plan
families may expose numeric workout_id. Pass whichever identifier is
present to get_workout_by_id. The returned count includes rest days.`

	scheduleWorkoutDescription = `Schedule a workout to a specific calendar date

This adds an existing workout from your Garmin workout library
to your Garmin Connect calendar on the specified date.

Idempotent: if the workout is already scheduled for that date, this
is a no-op that reports success without creating a duplicate entry.`

	scheduleWorkoutsDescription = `Schedule multiple workouts to specific calendar dates

This adds workouts to your Garmin Connect calendar in a single call.
Each item can either reference an existing workout by ID, or provide
inline workout_data to upload-and-schedule in one step.`

	rescheduleWorkoutDescription = `Move a scheduled workout to another date on the Garmin Connect calendar

Moves the existing calendar entry in place, so its id stays the same
and no duplicate entry is created. Prefer this over unschedule +
schedule. Safe to repeat: moving an entry to the date it is already
on changes nothing.

IMPORTANT: scheduled_workout_id is the calendar-entry id, which is
different from the workout's id. Get it from get_scheduled_workouts
(the "scheduled_workout_id" field), not from get_workouts.`

	unscheduleWorkoutDescription = `Remove a scheduled workout from the Garmin Connect calendar

Deletes a calendar entry without deleting the underlying workout
template — the workout stays in your library and can be re-scheduled.

IMPORTANT: scheduled_workout_id is the calendar-entry id, which is
different from the workout's id. Get it from get_scheduled_workouts
(the "scheduled_workout_id" field), not from get_workouts.

Note: the scheduled-workouts listing is an eventually-consistent index.
If you just scheduled this workout, allow a moment before unscheduling
so the id is available.`

	unscheduleWorkoutsDescription = `Remove multiple scheduled workouts from the Garmin Connect calendar

Deletes multiple calendar entries in a single call. The underlying
workout templates are left intact in your library.

IMPORTANT: each id is a calendar-entry id (the "scheduled_workout_id"
field from get_scheduled_workouts), not a workout id.`
)

var workoutDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func validateWorkoutDate(value, field string) (string, error) {
	if !workoutDatePattern.MatchString(value) {
		return "", fmt.Errorf("Invalid %s %s: expected YYYY-MM-DD", field, pythonRepr(value))
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return "", fmt.Errorf("Invalid %s %s: expected YYYY-MM-DD", field, pythonRepr(value))
	}
	return value, nil
}

func validateWorkoutDateValue(v any, field string) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("Invalid %s %s: expected YYYY-MM-DD", field, pythonRepr(v))
	}
	return validateWorkoutDate(s, field)
}

// --- input structs ---

type getWorkoutsInput struct{ Account }

type getWorkoutByIDInput struct {
	Account
	WorkoutID any  `json:"workout_id" jsonschema:"Workout ID (numeric) or UUID (for training plan workouts)"`
	Raw       bool `json:"raw,omitempty" jsonschema:"Return Garmin's full workout JSON (the DTO format upload_workout describes) instead of the simplified summary, which leaves fields out."`
}

type downloadWorkoutInput struct {
	Account
	WorkoutID int `json:"workout_id" jsonschema:"ID of the workout to download"`
}

type uploadWorkoutInput struct {
	Account
	WorkoutData map[string]any `json:"workout_data" jsonschema:"Dictionary containing workout structure (name, sport type, segments, etc.)"`
}

type uploadWorkoutsInput struct {
	Account
	Workouts []map[string]any `json:"workouts" jsonschema:"List of workout dictionaries, each containing workout structure (name, sport type, segments, etc.) — same format as upload_workout."`
}

type deleteWorkoutInput struct {
	Account
	WorkoutID int  `json:"workout_id" jsonschema:"ID of the workout to delete (get IDs from get_workouts)"`
	Confirm   bool `json:"confirm,omitempty" jsonschema:"Must be true to actually delete. Default false returns a preview."`
}

type deleteWorkoutsInput struct {
	Account
	WorkoutIDs []int `json:"workout_ids" jsonschema:"List of workout IDs to delete (get IDs from get_workouts)"`
	Confirm    bool  `json:"confirm,omitempty" jsonschema:"Must be true to actually delete. Default false returns a preview."`
}

type scheduledWorkoutsInput struct {
	Account
	StartDate string `json:"start_date" jsonschema:"Start date in YYYY-MM-DD format"`
	EndDate   string `json:"end_date" jsonschema:"End date in YYYY-MM-DD format"`
}

type coachWorkoutsInput struct {
	Account
	CalendarDate string `json:"calendar_date" jsonschema:"Reference date in YYYY-MM-DD format (returns week's workouts)"`
}

type scheduleWorkoutInput struct {
	Account
	WorkoutID    int    `json:"workout_id" jsonschema:"ID of the workout to schedule (get IDs from get_workouts)"`
	CalendarDate string `json:"calendar_date" jsonschema:"Date to schedule the workout in YYYY-MM-DD format"`
}

type scheduleWorkoutsInput struct {
	Account
	Schedules []map[string]any `json:"schedules" jsonschema:"List of workout schedules, each with calendar_date (required), workout_id (required unless workout_data is provided), and optional workout_data to upload-and-schedule. workout_data uses the same structure and target-value rules as upload_workout; pace targets are in m/s, not min/km (5:00/km = 3.333 m/s)."`
}

type rescheduleWorkoutInput struct {
	Account
	ScheduledWorkoutID int    `json:"scheduled_workout_id" jsonschema:"Calendar-entry id from get_scheduled_workouts"`
	CalendarDate       string `json:"calendar_date" jsonschema:"New date in YYYY-MM-DD format"`
}

type unscheduleWorkoutInput struct {
	Account
	ScheduledWorkoutID int `json:"scheduled_workout_id" jsonschema:"Calendar-entry id from get_scheduled_workouts"`
}

type unscheduleWorkoutsInput struct {
	Account
	ScheduledWorkoutIDs []int `json:"scheduled_workout_ids" jsonschema:"List of calendar-entry ids from get_scheduled_workouts"`
}

func registerWorkoutTools(r *registry) {
	addTool(r, "get_workouts", getWorkoutsDescription, getWorkouts)
	addTool(r, "get_workout_by_id", getWorkoutByIDDescription, getWorkoutByID)
	addTool(r, "download_workout", downloadWorkoutDescription, downloadWorkout)
	addTool(r, "upload_workout", uploadWorkoutDescription, uploadWorkout)
	addTool(r, "upload_workouts", uploadWorkoutsDescription, uploadWorkouts)
	addTool(r, "delete_workout", deleteWorkoutDescription, deleteWorkout)
	addTool(r, "delete_workouts", deleteWorkoutsDescription, deleteWorkouts)
	addTool(r, "get_scheduled_workouts", scheduledWorkoutsDescription, getScheduledWorkouts)
	addTool(r, "get_garmin_coach_workouts", garminCoachWorkoutsDescription, getGarminCoachWorkouts)
	addTool(r, "get_training_plan_workouts", trainingPlanWorkoutsDescription, getTrainingPlanWorkouts)
	addTool(r, "schedule_workout", scheduleWorkoutDescription, scheduleWorkout)
	addTool(r, "schedule_workouts", scheduleWorkoutsDescription, scheduleWorkouts)
	addTool(r, "reschedule_workout", rescheduleWorkoutDescription, rescheduleWorkout)
	addTool(r, "unschedule_workout", unscheduleWorkoutDescription, unscheduleWorkout)
	addTool(r, "unschedule_workouts", unscheduleWorkoutsDescription, unscheduleWorkouts)
}

// --- read tools ---

func workoutIDString(v any) string {
	if f, ok := v.(float64); ok {
		if f == math.Trunc(f) {
			return strconv.FormatInt(int64(f), 10)
		}
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

func workoutIDNumber(v any) (int64, error) {
	switch x := v.(type) {
	case int:
		return int64(x), nil
	case int64:
		return x, nil
	case float64:
		return int64(x), nil
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err != nil {
			return 0, err
		}
		return n, nil
	}
	return 0, fmt.Errorf("cannot convert %v to an integer", v)
}

func getAllWorkouts(ctx context.Context, c *garmin.Client) ([]map[string]any, error) {
	out := []map[string]any{}
	seen := map[string]bool{}
	start := 1
	for {
		payload, err := c.GetWorkouts(ctx, start, 100)
		if err != nil {
			return nil, err
		}
		page, ok := payload.([]any)
		if !ok {
			break
		}
		newCount := 0
		for _, item := range page {
			workout, ok := item.(map[string]any)
			if !ok {
				continue
			}
			key := fmt.Sprintf("%T:%v", workout["workoutId"], workout["workoutId"])
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, workout)
			newCount++
		}
		if newCount == 0 || len(page) < 100 {
			break
		}
		start += 100
	}
	return out, nil
}

func curateWorkoutSummary(workout map[string]any) map[string]any {
	sportType := asMap(workout["sportType"])
	summary := map[string]any{
		"id":           workout["workoutId"],
		"name":         workout["workoutName"],
		"sport":        sportType["sportTypeKey"],
		"provider":     workout["workoutProvider"],
		"created_date": workout["createdDate"],
		"updated_date": workout["updatedDate"],
	}
	if truthy(workout["description"]) {
		summary["description"] = workout["description"]
	}
	duration := workout["estimatedDurationInSecs"]
	if duration == nil {
		duration = workout["estimatedDuration"]
	}
	if duration != nil {
		summary["estimated_duration_seconds"] = duration
	}
	distance := workout["estimatedDistanceInMeters"]
	if distance == nil {
		distance = workout["estimatedDistance"]
	}
	if distance != nil {
		summary["estimated_distance_meters"] = distance
	}
	return pruneNil(summary)
}

func getWorkouts(ctx context.Context, s *Session, _ getWorkoutsInput) (any, error) {
	workouts, err := getAllWorkouts(ctx, s.Client)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving workouts: %w", err)
	}
	if len(workouts) == 0 {
		return "No workouts found.", nil
	}
	summaries := make([]any, 0, len(workouts))
	for _, workout := range workouts {
		summaries = append(summaries, curateWorkoutSummary(workout))
	}
	return map[string]any{"count": len(workouts), "workouts": summaries}, nil
}

func getWorkoutByID(ctx context.Context, s *Session, in getWorkoutByIDInput) (any, error) {
	workoutID := workoutIDString(in.WorkoutID)
	isUUID := strings.Contains(workoutID, "-")
	var (
		workout any
		err     error
	)
	if isUUID {
		workout, err = s.Client.GetWorkoutByUUID(ctx, workoutID)
	} else {
		id, convErr := strconv.ParseInt(strings.TrimSpace(workoutID), 10, 64)
		if convErr != nil {
			return nil, fmt.Errorf("Error retrieving workout: %w", convErr)
		}
		workout, err = s.Client.GetWorkoutByID(ctx, id)
	}
	if err != nil {
		return nil, fmt.Errorf("Error retrieving workout: %w", err)
	}
	if !truthy(workout) {
		return fmt.Sprintf("No workout found with ID %s.", workoutID), nil
	}
	if in.Raw {
		return workout, nil
	}
	return curateWorkoutDetails(asMap(workout)), nil
}

func downloadWorkout(ctx context.Context, s *Session, in downloadWorkoutInput) (any, error) {
	data, err := s.Client.DownloadWorkout(ctx, int64(in.WorkoutID))
	if err != nil {
		return nil, fmt.Errorf("Error downloading workout: %w", err)
	}
	if len(data) == 0 {
		return fmt.Sprintf("No workout data found for workout with ID %d.", in.WorkoutID), nil
	}
	return map[string]any{
		"workout_id": in.WorkoutID,
		"format":     "FIT",
		"size_bytes": len(data),
		"message":    "Workout data is available in FIT format. Use Garmin Connect API to save to file.",
	}, nil
}

// --- curation ---

func curateStepTarget(curated, step map[string]any, targetField, valueOneField, valueTwoField, zoneField, unitField, prefix string) {
	targetType := asMap(step[targetField])
	targetKey := targetType["workoutTargetTypeKey"]
	if !truthy(targetKey) || targetKey == "no.target" {
		return
	}
	curated[prefix+"target_type"] = targetKey
	if step[valueOneField] != nil {
		curated[prefix+"target_value_low"] = step[valueOneField]
	}
	if step[valueTwoField] != nil {
		curated[prefix+"target_value_high"] = step[valueTwoField]
	}
	if step[zoneField] != nil {
		curated[prefix+"target_zone"] = step[zoneField]
	}
	unit := asMap(step[unitField])
	if truthy(unit["unitKey"]) {
		curated[prefix+"target_value_unit"] = unit["unitKey"]
	} else if targetKey == "pace.zone" && (step[valueOneField] != nil || step[valueTwoField] != nil) {
		curated[prefix+"target_value_unit"] = "meters_per_second"
	}
}

func curateWorkoutStep(step map[string]any) map[string]any {
	stepType := asMap(step["stepType"])
	endCondition := asMap(step["endCondition"])
	curated := map[string]any{
		"order": step["stepOrder"],
		"type":  stepType["stepTypeKey"],
	}
	if truthy(step["description"]) {
		curated["description"] = step["description"]
	}
	if truthy(endCondition["conditionTypeKey"]) {
		curated["end_condition"] = endCondition["conditionTypeKey"]
	}
	if truthy(step["endConditionValue"]) {
		curated["end_condition_value"] = step["endConditionValue"]
	}
	curateStepTarget(curated, step, "targetType", "targetValueOne", "targetValueTwo", "zoneNumber", "targetValueUnit", "")
	curateStepTarget(curated, step, "secondaryTargetType", "secondaryTargetValueOne", "secondaryTargetValueTwo", "secondaryZoneNumber", "secondaryTargetValueUnit", "secondary_")

	if stroke := asMap(step["strokeType"]); truthy(stroke["strokeTypeKey"]) {
		curated["stroke_type"] = stroke["strokeTypeKey"]
	}
	if equipment := asMap(step["equipmentType"]); truthy(equipment["equipmentTypeKey"]) {
		curated["equipment_type"] = equipment["equipmentTypeKey"]
	}
	if drill := asMap(step["drillType"]); truthy(drill["drillTypeKey"]) {
		curated["drill_type"] = drill["drillTypeKey"]
	}
	if truthy(step["category"]) {
		curated["category"] = step["category"]
	}
	if truthy(step["exerciseName"]) {
		curated["exercise_name"] = step["exerciseName"]
	}
	if step["weightValue"] != nil {
		curated["weight_value"] = step["weightValue"]
		weightUnit := asMap(step["weightUnit"])
		if truthy(weightUnit["unitKey"]) {
			curated["weight_unit"] = weightUnit["unitKey"]
		}
	}
	if step["type"] == "RepeatGroupDTO" {
		curated["repeat_count"] = step["numberOfIterations"]
		nested := asList(step["workoutSteps"])
		if len(nested) > 0 {
			steps := make([]any, 0, len(nested))
			for _, nestedStep := range nested {
				steps = append(steps, curateWorkoutStep(asMap(nestedStep)))
			}
			curated["steps"] = steps
			curated["step_count"] = len(nested)
		}
	}
	return pruneNil(curated)
}

func curateWorkoutSegment(segment map[string]any) map[string]any {
	sportType := asMap(segment["sportType"])
	curated := map[string]any{
		"order": segment["segmentOrder"],
		"sport": sportType["sportTypeKey"],
	}
	if truthy(segment["estimatedDurationInSecs"]) {
		curated["estimated_duration_seconds"] = segment["estimatedDurationInSecs"]
	}
	if truthy(segment["estimatedDistanceInMeters"]) {
		curated["estimated_distance_meters"] = segment["estimatedDistanceInMeters"]
	}
	steps := asList(segment["workoutSteps"])
	if len(steps) > 0 {
		out := make([]any, 0, len(steps))
		for _, step := range steps {
			out = append(out, curateWorkoutStep(asMap(step)))
		}
		curated["steps"] = out
		curated["step_count"] = len(steps)
	}
	return pruneNil(curated)
}

func curateWorkoutDetails(workout map[string]any) map[string]any {
	sportType := asMap(workout["sportType"])
	details := map[string]any{
		"id":           workout["workoutId"],
		"uuid":         workout["workoutUuid"],
		"name":         workout["workoutName"],
		"sport":        sportType["sportTypeKey"],
		"provider":     workout["workoutProvider"],
		"created_date": workout["createdDate"],
		"updated_date": workout["updatedDate"],
	}
	if truthy(workout["description"]) {
		details["description"] = workout["description"]
	}
	duration := firstTruthy(workout["estimatedDuration"], workout["estimatedDurationInSecs"])
	if truthy(duration) {
		details["estimated_duration_seconds"] = duration
	}
	distance := firstTruthy(workout["estimatedDistance"], workout["estimatedDistanceInMeters"])
	if truthy(distance) {
		details["estimated_distance_meters"] = distance
	}
	if truthy(workout["avgTrainingSpeed"]) {
		details["avg_training_speed_mps"] = workout["avgTrainingSpeed"]
	}
	if truthy(workout["poolLength"]) {
		details["pool_length"] = workout["poolLength"]
		if unit := asMap(workout["poolLengthUnit"]); truthy(unit["unitKey"]) {
			details["pool_length_unit"] = unit["unitKey"]
		}
	}
	if truthy(workout["workoutPhrase"]) {
		details["workout_type"] = workout["workoutPhrase"]
	}
	if truthy(workout["trainingEffectLabel"]) {
		details["training_effect_label"] = workout["trainingEffectLabel"]
	}
	if truthy(workout["estimatedTrainingEffect"]) {
		details["estimated_training_effect"] = workout["estimatedTrainingEffect"]
	}
	segments := asList(workout["workoutSegments"])
	if len(segments) > 0 {
		out := make([]any, 0, len(segments))
		for _, segment := range segments {
			out = append(out, curateWorkoutSegment(asMap(segment)))
		}
		details["segments"] = out
		details["segment_count"] = len(segments)
	}
	return pruneNil(details)
}

func curateScheduledWorkout(scheduled map[string]any) map[string]any {
	completed := scheduled["associatedActivityId"] != nil
	summary := map[string]any{
		"date":                 scheduled["scheduleDate"],
		"scheduled_workout_id": scheduled["scheduledWorkoutId"],
		"workout_uuid":         scheduled["workoutUuid"],
		"workout_id":           scheduled["workoutId"],
		"training_plan_id":     scheduled["trainingPlanId"],
		"fbt_adaptive_plan_id": scheduled["fbtAdaptivePlanId"],
		"tp_type":              scheduled["tpType"],
		"name":                 scheduled["workoutName"],
		"sport":                scheduled["workoutType"],
		"completed":            completed,
	}
	if truthy(scheduled["tpPlanName"]) {
		summary["training_plan"] = scheduled["tpPlanName"]
	}
	if truthy(scheduled["workoutPhrase"]) {
		summary["workout_type"] = scheduled["workoutPhrase"]
	}
	if truthy(scheduled["isRestDay"]) {
		summary["is_rest_day"] = true
	}
	if truthy(scheduled["race"]) {
		summary["is_race_day"] = true
	}
	if truthy(scheduled["estimatedDurationInSecs"]) {
		summary["estimated_duration_seconds"] = scheduled["estimatedDurationInSecs"]
	}
	if truthy(scheduled["estimatedDistanceInMeters"]) {
		summary["estimated_distance_meters"] = scheduled["estimatedDistanceInMeters"]
	}
	if completed {
		summary["activity_id"] = scheduled["associatedActivityId"]
	}
	return pruneNil(summary)
}

// --- upload validation / repair ---

const (
	targetFieldPrimary   = "targetType"
	targetFieldSecondary = "secondaryTargetType"
)

var endConditionTypeIDs = map[string]int{
	"lap.button": 1, "time": 2, "distance": 3, "calories": 4, "power": 5,
	"heart.rate": 6, "iterations": 7, "fixed.rest": 8, "fixed.repetition": 9,
	"reps": 10, "training.peaks.tss": 11,
}

var endConditionTypeKeys = func() map[int]string {
	out := map[int]string{}
	for k, v := range endConditionTypeIDs {
		out[v] = k
	}
	return out
}()

var knownTargetTypeIDs = map[int][]string{
	1: {"no.target"},
	2: {"power.zone"},
	4: {"heart.rate.zone"},
	6: {"pace.zone", "power.between"},
}

var knownTargetTypeKeys = func() map[string]int {
	out := map[string]int{}
	for id, keys := range knownTargetTypeIDs {
		for _, key := range keys {
			out[key] = id
		}
	}
	return out
}()

type workoutStepRef struct {
	step map[string]any
	path string
}

func iterStepTree(value any, path string) ([]workoutStepRef, error) {
	step, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", path)
	}
	out := []workoutStepRef{{step: step, path: path}}
	if nestedValue, exists := step["workoutSteps"]; exists {
		nested, ok := nestedValue.([]any)
		if !ok {
			return nil, fmt.Errorf("%s.workoutSteps must be a list", path)
		}
		for i, child := range nested {
			childRefs, err := iterStepTree(child, fmt.Sprintf("%s.workoutSteps[%d]", path, i))
			if err != nil {
				return nil, err
			}
			out = append(out, childRefs...)
		}
	}
	return out, nil
}

func iterWorkoutSteps(workout map[string]any) ([]workoutStepRef, error) {
	segmentsValue, exists := workout["workoutSegments"]
	if !exists {
		segmentsValue = []any{}
	}
	segments, ok := segmentsValue.([]any)
	if !ok {
		return nil, errors.New("workoutSegments must be a list")
	}
	out := []workoutStepRef{}
	for i, segmentValue := range segments {
		segment, ok := segmentValue.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("workoutSegments[%d] must be an object", i)
		}
		stepsValue, exists := segment["workoutSteps"]
		if !exists {
			stepsValue = []any{}
		}
		steps, ok := stepsValue.([]any)
		if !ok {
			return nil, fmt.Errorf("workoutSegments[%d].workoutSteps must be a list", i)
		}
		for j, step := range steps {
			refs, err := iterStepTree(step, fmt.Sprintf("workoutSegments[%d].workoutSteps[%d]", i, j))
			if err != nil {
				return nil, err
			}
			out = append(out, refs...)
		}
	}
	return out, nil
}

func validateNestedTargetFields(step map[string]any, path string) error {
	for _, targetField := range []string{targetFieldPrimary, targetFieldSecondary} {
		targetType, ok := step[targetField].(map[string]any)
		if !ok {
			continue
		}
		var boundFields, zoneField string
		if targetField == targetFieldPrimary {
			boundFields, zoneField = "targetValueOne", "zoneNumber"
		} else {
			boundFields, zoneField = "secondaryTargetValueOne", "secondaryZoneNumber"
		}
		boundTwo := strings.Replace(boundFields, "One", "Two", 1)
		fields := []string{boundFields, boundTwo, zoneField}
		for _, field := range fields {
			nestedValue := targetType[field]
			if nestedValue == nil {
				continue
			}
			if stepValue, exists := step[field]; exists && stepValue != nil && !reflect.DeepEqual(stepValue, nestedValue) {
				return fmt.Errorf("%s.%s=%s conflicts with %s.%s.%s=%s; keep only the step-level %s",
					path, field, pythonRepr(stepValue), path, targetField, field, pythonRepr(nestedValue), field)
			}
		}

		zoneValue := step[zoneField]
		if zoneValue == nil {
			zoneValue = targetType[zoneField]
		}
		var bounds []string
		for _, field := range []string{boundFields, boundTwo} {
			value := step[field]
			if value == nil {
				value = targetType[field]
			}
			if value != nil {
				bounds = append(bounds, fmt.Sprintf("%s=%s", field, pythonRepr(value)))
			}
		}
		if zoneValue != nil && len(bounds) > 0 {
			return fmt.Errorf("%s mixes %s=%s with custom range fields (%s); use either a named zone or a custom range",
				path, zoneField, pythonRepr(zoneValue), strings.Join(bounds, ", "))
		}
	}
	return nil
}

func moveNestedTargetFields(step map[string]any) {
	for _, targetField := range []string{targetFieldPrimary, targetFieldSecondary} {
		targetType, ok := step[targetField].(map[string]any)
		if !ok {
			continue
		}
		boundOne, zoneField := "targetValueOne", "zoneNumber"
		if targetField == targetFieldSecondary {
			boundOne, zoneField = "secondaryTargetValueOne", "secondaryZoneNumber"
		}
		boundTwo := strings.Replace(boundOne, "One", "Two", 1)
		for _, field := range []string{boundOne, boundTwo, zoneField} {
			value, exists := targetType[field]
			if !exists {
				continue
			}
			delete(targetType, field)
			if value != nil && step[field] == nil {
				step[field] = value
			}
		}
	}
}

func numberValue(v any) (float64, bool) {
	switch x := v.(type) {
	case nil:
		return 0, false
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

func fixHRZoneStep(step map[string]any) {
	targetType := asMap(step[targetFieldPrimary])
	targetKey, _ := targetType["workoutTargetTypeKey"].(string)
	if targetKey == "heart.rate.zone" && step["zoneNumber"] == nil {
		if zone, ok := numberValue(step["targetValueOne"]); ok && zone == float64(int(zone)) && zone >= 1 && zone <= 5 {
			step["zoneNumber"] = int(zone)
			delete(step, "targetValueOne")
			delete(step, "targetValueTwo")
		}
	}
	for _, nested := range asList(step["workoutSteps"]) {
		fixHRZoneStep(asMap(nested))
	}
}

// pythonInt mirrors Python's int(): strings must be integer literals, floats
// truncate toward zero, bools become 0/1, and null/unconvertible values fail.
func pythonInt(v any) (int, error) {
	switch x := v.(type) {
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case int:
		return x, nil
	case int64:
		return int(x), nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return 0, errors.New("not an integer")
		}
		return int(x), nil
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return 0, errors.New("not an integer")
		}
		return n, nil
	}
	return 0, errors.New("not an integer")
}

func fixRepeatGroupStep(step map[string]any) error {
	if step["type"] != "RepeatGroupDTO" {
		for _, nested := range asList(step["workoutSteps"]) {
			if err := fixRepeatGroupStep(asMap(nested)); err != nil {
				return err
			}
		}
		return nil
	}
	if endCondition, ok := step["endCondition"].(map[string]any); ok {
		if endCondition["conditionTypeKey"] == "iterations" {
			if _, exists := endCondition["conditionTypeId"]; !exists {
				endCondition["conditionTypeId"] = 7
			}
		}
	}
	if step["numberOfIterations"] == nil {
		if value := step["endConditionValue"]; value != nil {
			iterations, err := pythonInt(value)
			if err != nil {
				return fmt.Errorf("numberOfIterations is missing and endConditionValue=%s is not an integer", pythonRepr(value))
			}
			step["numberOfIterations"] = iterations
		}
	}
	for _, nested := range asList(step["workoutSteps"]) {
		if err := fixRepeatGroupStep(asMap(nested)); err != nil {
			return err
		}
	}
	return nil
}

func normalizeWorkoutSteps(workout map[string]any) error {
	steps, err := iterWorkoutSteps(workout)
	if err != nil {
		return err
	}
	for _, ref := range steps {
		if err := validateNestedTargetFields(ref.step, ref.path); err != nil {
			return err
		}
	}
	for _, ref := range steps {
		moveNestedTargetFields(ref.step)
	}
	segments := asList(workout["workoutSegments"])
	for _, segment := range segments {
		for _, stepValue := range asList(asMap(segment)["workoutSteps"]) {
			step := asMap(stepValue)
			fixHRZoneStep(step)
			if err := fixRepeatGroupStep(step); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateEndConditionStep(step map[string]any, path string) error {
	if endCondition, ok := step["endCondition"].(map[string]any); ok {
		conditionKey := endCondition["conditionTypeKey"]
		conditionID := endCondition["conditionTypeId"]
		if conditionString, ok := conditionID.(string); ok {
			n, err := strconv.Atoi(strings.TrimSpace(conditionString))
			if err != nil {
				return fmt.Errorf("%s.endCondition.conditionTypeId must be numeric", path)
			}
			conditionID = n
			endCondition["conditionTypeId"] = n
		}
		keyString, _ := conditionKey.(string)
		expectedID, hasExpected := endConditionTypeIDs[keyString]
		idNumber, hasID := numericInt(conditionID)
		expectedKey, hasExpectedKey := endConditionTypeKeys[idNumber]
		if hasExpected {
			if conditionID == nil {
				return fmt.Errorf("%s.endCondition conditionTypeKey '%s' requires conditionTypeId %d", path, keyString, expectedID)
			}
			if !hasID || idNumber != expectedID {
				actual := expectedKey
				if actual == "" {
					actual = "unknown"
				}
				return fmt.Errorf("%s.endCondition conditionTypeKey '%s' requires conditionTypeId %d, got %s (%s)",
					path, keyString, expectedID, pythonRepr(conditionID), actual)
			}
		} else if hasExpectedKey && conditionKey != nil {
			return fmt.Errorf("%s.endCondition conditionTypeId %s requires conditionTypeKey '%s', got '%s'",
				path, pythonRepr(conditionID), expectedKey, keyString)
		}
	}
	for i, nested := range asList(step["workoutSteps"]) {
		if err := validateEndConditionStep(asMap(nested), fmt.Sprintf("%s.workoutSteps[%d]", path, i)); err != nil {
			return err
		}
	}
	return nil
}

func numericInt(v any) (int, bool) {
	f, ok := numberValue(v)
	if !ok || f != float64(int(f)) {
		return 0, false
	}
	return int(f), true
}

func validateEndConditionSteps(workout map[string]any) error {
	steps, err := iterWorkoutSteps(workout)
	if err != nil {
		return err
	}
	for _, ref := range steps {
		if err := validateEndConditionStep(ref.step, ref.path); err != nil {
			return err
		}
	}
	return nil
}

func validateTargetTypeBlock(step map[string]any, path, targetField string) error {
	targetType, ok := step[targetField].(map[string]any)
	if !ok {
		return nil
	}
	targetKey := targetType["workoutTargetTypeKey"]
	targetID := targetType["workoutTargetTypeId"]
	if idString, ok := targetID.(string); ok {
		n, err := strconv.Atoi(strings.TrimSpace(idString))
		if err != nil {
			return fmt.Errorf("%s.%s.workoutTargetTypeId must be numeric", path, targetField)
		}
		targetID = n
		targetType["workoutTargetTypeId"] = n
	}
	keyString, _ := targetKey.(string)
	idNumber, hasID := numericInt(targetID)
	if validKeys, ok := knownTargetTypeIDs[idNumber]; ok && hasID && targetKey != nil {
		found := false
		for _, key := range validKeys {
			if key == keyString {
				found = true
			}
		}
		if !found {
			if len(validKeys) == 1 {
				return fmt.Errorf("%s.%s mismatch: workoutTargetTypeId %d is '%s', not '%s'",
					path, targetField, idNumber, validKeys[0], keyString)
			}
			sortedKeys := append([]string(nil), validKeys...)
			sort.Strings(sortedKeys)
			quoted := make([]string, 0, len(sortedKeys))
			for _, key := range sortedKeys {
				quoted = append(quoted, "'"+key+"'")
			}
			return fmt.Errorf("%s.%s mismatch: workoutTargetTypeId %d is one of (%s), not '%s'",
				path, targetField, idNumber, strings.Join(quoted, ", "), keyString)
		}
	}
	if expectedID, ok := knownTargetTypeKeys[keyString]; ok && targetID != nil && (!hasID || idNumber != expectedID) {
		return fmt.Errorf("%s.%s mismatch: workoutTargetTypeKey '%s' requires workoutTargetTypeId %d, not %s",
			path, targetField, keyString, expectedID, pythonRepr(targetID))
	}
	return nil
}

func validateTargetTypeStep(step map[string]any, path string) error {
	if err := validateTargetTypeBlock(step, path, targetFieldPrimary); err != nil {
		return err
	}
	if err := validateTargetTypeBlock(step, path, targetFieldSecondary); err != nil {
		return err
	}
	for i, nested := range asList(step["workoutSteps"]) {
		if err := validateTargetTypeStep(asMap(nested), fmt.Sprintf("%s.workoutSteps[%d]", path, i)); err != nil {
			return err
		}
	}
	return nil
}

func validateTargetTypeSteps(workout map[string]any) error {
	steps, err := iterWorkoutSteps(workout)
	if err != nil {
		return err
	}
	for _, ref := range steps {
		if err := validateTargetTypeStep(ref.step, ref.path); err != nil {
			return err
		}
	}
	return nil
}

// --- writes ---

func writeFailureMessage(tracker *writeTracker, err error) string {
	if failure, ok := tracker.Fail(err).(*writeFailure); ok {
		if message, ok := failure.payload["message"].(string); ok {
			return message
		}
	}
	return fmt.Sprintf("%s: %v", tracker.prefix, err)
}

func uploadResultValue(result any, message string) any {
	if payload, ok := result.(map[string]any); ok {
		curated := map[string]any{
			"status":     "success",
			"workout_id": payload["workoutId"],
			"name":       payload["workoutName"],
			"message":    message,
		}
		return pruneNil(curated)
	}
	return result
}

func uploadWorkout(ctx context.Context, s *Session, in uploadWorkoutInput) (any, error) {
	if err := normalizeWorkoutSteps(in.WorkoutData); err != nil {
		return fmt.Sprintf("Error uploading workout: %v", err), nil
	}
	if err := validateEndConditionSteps(in.WorkoutData); err != nil {
		return fmt.Sprintf("Error uploading workout: %v", err), nil
	}
	if err := validateTargetTypeSteps(in.WorkoutData); err != nil {
		return fmt.Sprintf("Error uploading workout: %v", err), nil
	}
	tracker := newWriteTracker("upload_workout", s.logWriter()).withPrefix("Error uploading workout")
	tracker.Sent()
	result, err := s.Client.UploadWorkout(ctx, in.WorkoutData)
	if err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	return uploadResultValue(result, "Workout uploaded successfully"), nil
}

func uploadWorkouts(ctx context.Context, s *Session, in uploadWorkoutsInput) (any, error) {
	results := make([]any, 0, len(in.Workouts))
	for _, workout := range in.Workouts {
		tracker := newWriteTracker("upload_workouts", s.logWriter()).withPrefix("Error uploading workout")
		if err := normalizeWorkoutSteps(workout); err != nil {
			results = append(results, map[string]any{
				"status": "error", "name": workout["workoutName"], "message": "Error uploading workout: " + err.Error(),
			})
			continue
		}
		if err := validateEndConditionSteps(workout); err != nil {
			results = append(results, map[string]any{
				"status": "error", "name": workout["workoutName"], "message": "Error uploading workout: " + err.Error(),
			})
			continue
		}
		if err := validateTargetTypeSteps(workout); err != nil {
			results = append(results, map[string]any{
				"status": "error", "name": workout["workoutName"], "message": "Error uploading workout: " + err.Error(),
			})
			continue
		}
		tracker.Sent()
		result, err := s.Client.UploadWorkout(ctx, workout)
		if err != nil {
			results = append(results, map[string]any{
				"status": "error", "name": workout["workoutName"], "message": writeFailureMessage(tracker, err),
			})
			continue
		}
		tracker.Committed()
		if payload, ok := result.(map[string]any); ok {
			results = append(results, pruneNil(map[string]any{
				"status": "success", "workout_id": payload["workoutId"],
				"name": payload["workoutName"], "message": "Workout uploaded successfully",
			}))
		} else {
			results = append(results, map[string]any{"status": "success", "message": "Workout uploaded successfully"})
		}
	}
	succeeded := 0
	for _, result := range results {
		if asMap(result)["status"] == "success" {
			succeeded++
		}
	}
	return map[string]any{
		"total": len(results), "succeeded": succeeded, "failed": len(results) - succeeded, "results": results,
	}, nil
}

func workoutPreview(ctx context.Context, c *garmin.Client, id int64) map[string]any {
	preview := map[string]any{"workout_id": id}
	workout, err := c.GetWorkoutByID(ctx, id)
	if err == nil && truthy(workout) {
		summary := curateWorkoutSummary(asMap(workout))
		preview["name"] = summary["name"]
		preview["sport"] = summary["sport"]
		preview["provider"] = summary["provider"]
		preview["updated_date"] = summary["updated_date"]
		return pruneNil(preview)
	}
	items, err := getAllWorkouts(ctx, c)
	if err != nil {
		return pruneNil(preview)
	}
	for _, item := range items {
		if !sameWorkoutID(item["workoutId"], id) {
			continue
		}
		summary := curateWorkoutSummary(item)
		preview["name"] = summary["name"]
		preview["sport"] = summary["sport"]
		preview["provider"] = summary["provider"]
		preview["updated_date"] = summary["updated_date"]
		break
	}
	return pruneNil(preview)
}

func sameWorkoutID(v any, id int64) bool {
	switch x := v.(type) {
	case string:
		return x == strconv.FormatInt(id, 10)
	case float64:
		return x == float64(id)
	case int:
		return int64(x) == id
	case int64:
		return x == id
	}
	return false
}

func deleteWorkout(ctx context.Context, s *Session, in deleteWorkoutInput) (any, error) {
	preview := workoutPreview(ctx, s.Client, int64(in.WorkoutID))
	if !in.Confirm {
		return map[string]any{
			"status":  "needs_confirmation",
			"workout": preview,
			"message": "This permanently deletes the workout from Garmin Connect and cannot be undone. Call again with confirm=true to proceed.",
		}, nil
	}
	tracker := newWriteTracker("delete_workout", s.logWriter()).withPrefix("Failed to delete workout")
	tracker.Sent()
	if _, err := s.Client.DeleteWorkout(ctx, int64(in.WorkoutID)); err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	fmt.Fprintf(s.logWriter(), "delete_workout: deleted workout %d name=%s at %s\n",
		in.WorkoutID, pythonRepr(preview["name"]), time.Now().UTC().Format(time.RFC3339Nano))
	return map[string]any{
		"status": "success", "workout_id": in.WorkoutID, "workout": preview,
		"message": fmt.Sprintf("Workout %d deleted successfully", in.WorkoutID),
	}, nil
}

func deleteWorkouts(ctx context.Context, s *Session, in deleteWorkoutsInput) (any, error) {
	previews := make([]map[string]any, 0, len(in.WorkoutIDs))
	for _, id := range in.WorkoutIDs {
		previews = append(previews, workoutPreview(ctx, s.Client, int64(id)))
	}
	if !in.Confirm {
		out := make([]any, 0, len(previews))
		for _, preview := range previews {
			out = append(out, preview)
		}
		return map[string]any{
			"status": "needs_confirmation", "total": len(in.WorkoutIDs), "workouts": out,
			"message": "This permanently deletes these workouts from Garmin Connect and cannot be undone. Call again with confirm=true to proceed.",
		}, nil
	}
	results := make([]any, 0, len(in.WorkoutIDs))
	for i, id := range in.WorkoutIDs {
		tracker := newWriteTracker("delete_workouts", s.logWriter()).withPrefix("Error deleting workout")
		tracker.Sent()
		if _, err := s.Client.DeleteWorkout(ctx, int64(id)); err != nil {
			results = append(results, map[string]any{
				"status": "error", "workout_id": id, "message": writeFailureMessage(tracker, err),
			})
			continue
		}
		tracker.Committed()
		fmt.Fprintf(s.logWriter(), "delete_workouts: deleted workout %d name=%s at %s\n",
			id, pythonRepr(previews[i]["name"]), time.Now().UTC().Format(time.RFC3339Nano))
		results = append(results, map[string]any{
			"status": "success", "workout_id": id, "workout": previews[i],
			"message": fmt.Sprintf("Workout %d deleted successfully", id),
		})
	}
	succeeded := 0
	for _, result := range results {
		if asMap(result)["status"] == "success" {
			succeeded++
		}
	}
	return map[string]any{
		"total": len(results), "succeeded": succeeded, "failed": len(results) - succeeded, "results": results,
	}, nil
}

// --- calendar scheduling ---

func getScheduledWorkouts(ctx context.Context, s *Session, in scheduledWorkoutsInput) (any, error) {
	startDate, err := validateWorkoutDate(in.StartDate, "start_date")
	if err != nil {
		return nil, fmt.Errorf("Error retrieving scheduled workouts: %w", err)
	}
	endDate, err := validateWorkoutDate(in.EndDate, "end_date")
	if err != nil {
		return nil, fmt.Errorf("Error retrieving scheduled workouts: %w", err)
	}
	query := map[string]any{"query": fmt.Sprintf(`query{workoutScheduleSummariesScalar(startDate:"%s", endDate:"%s")}`, startDate, endDate)}
	result, err := s.Client.QueryGraphQL(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving scheduled workouts: %w", err)
	}
	resultMap, ok := result.(map[string]any)
	if !ok || !truthy(result) {
		return "No scheduled workouts found or error querying data.", nil
	}
	if _, ok := resultMap["data"]; !ok {
		return "No scheduled workouts found or error querying data.", nil
	}
	scheduled := asList(asMap(resultMap["data"])["workoutScheduleSummariesScalar"])
	if len(scheduled) == 0 {
		return fmt.Sprintf("No workouts scheduled between %s and %s.", startDate, endDate), nil
	}
	out := make([]any, 0, len(scheduled))
	for _, item := range scheduled {
		out = append(out, curateScheduledWorkout(asMap(item)))
	}
	return map[string]any{
		"count": len(scheduled), "date_range": map[string]any{"start": startDate, "end": endDate},
		"scheduled_workouts": out,
	}, nil
}

func coachWorkouts(ctx context.Context, s *Session, calendarDate string) (any, error) {
	date, err := validateWorkoutDate(calendarDate, "calendar_date")
	if err != nil {
		return nil, err
	}
	query := map[string]any{"query": fmt.Sprintf(`query{trainingPlanScalar(calendarDate:"%s", lang:"en-US", firstDayOfWeek:"monday")}`, date)}
	result, err := s.Client.QueryGraphQL(ctx, query)
	if err != nil {
		return nil, err
	}
	resultMap, ok := result.(map[string]any)
	if !ok {
		return "No training plan data found or error querying data.", nil
	}
	data, ok := resultMap["data"].(map[string]any)
	if !ok {
		return "No training plan data found or error querying data.", nil
	}
	planDataValue := data["trainingPlanScalar"]
	if planDataValue == nil {
		planDataValue = map[string]any{}
	}
	planData, ok := planDataValue.(map[string]any)
	if !ok {
		return "No training plan data found or error querying data.", nil
	}
	trainingPlans := asList(planData["trainingPlanWorkoutScheduleDTOS"])
	if len(trainingPlans) == 0 {
		return fmt.Sprintf("No training plan workouts scheduled for %s.", date), nil
	}

	allWorkouts := []any{}
	planNames := []any{}
	plans := []any{}
	validPlanCount := 0
	for _, planValue := range trainingPlans {
		plan, ok := planValue.(map[string]any)
		if !ok {
			continue
		}
		validPlanCount++
		planName, _ := plan["planName"].(string)
		if planName != "" && !containsValue(planNames, planName) {
			planNames = append(planNames, planName)
		}
		planDetails := asMap(plan["trainingPlanDetailsDTO"])
		athleteRace := asMap(planDetails["athleteRace"])
		summary := pruneNil(map[string]any{
			"name":                         planName,
			"training_plan_id":             plan["trainingPlanId"],
			"classification":               plan["trainingPlanClassification"],
			"training_type":                planDetails["trainingType"],
			"race_name":                    athleteRace["raceName"],
			"race_day":                     athleteRace["raceDay"],
			"workouts_per_week":            planDetails["workoutsPerWeek"],
			"remaining_workouts_this_week": planDetails["remainingAtpWorkoutsForWeek"],
			"registration_date":            planDetails["registrationDate"],
			"plan_completed":               planDetails["planCompleted"],
		})
		if len(summary) > 0 {
			plans = append(plans, summary)
		}
		for _, workout := range asList(plan["workoutScheduleSummaries"]) {
			if workoutMap, ok := workout.(map[string]any); ok {
				allWorkouts = append(allWorkouts, curateScheduledWorkout(workoutMap))
			}
		}
	}
	if validPlanCount == 0 {
		return fmt.Sprintf("No training plan workouts scheduled for %s.", date), nil
	}
	curated := map[string]any{
		"date":     date,
		"count":    len(allWorkouts),
		"workouts": allWorkouts,
	}
	if len(planNames) > 0 {
		curated["training_plans"] = planNames
	}
	if len(plans) > 0 {
		curated["plans"] = plans
	}
	return curated, nil
}

func containsValue(list []any, value any) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func getGarminCoachWorkouts(ctx context.Context, s *Session, in coachWorkoutsInput) (any, error) {
	result, err := coachWorkouts(ctx, s, in.CalendarDate)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving Garmin Coach workouts: %w", err)
	}
	return result, nil
}

func getTrainingPlanWorkouts(ctx context.Context, s *Session, in coachWorkoutsInput) (any, error) {
	result, err := coachWorkouts(ctx, s, in.CalendarDate)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving training plan workouts: %w", err)
	}
	return result, nil
}

func alreadyScheduled(ctx context.Context, s *Session, workoutID int64, calendarDate string) bool {
	if _, err := validateWorkoutDate(calendarDate, "calendar_date"); err != nil {
		return false
	}
	query := map[string]any{"query": fmt.Sprintf(`query{workoutScheduleSummariesScalar(startDate:"%s", endDate:"%s")}`, calendarDate, calendarDate)}
	result, err := s.Client.QueryGraphQL(ctx, query)
	if err != nil {
		return false
	}
	resultMap, ok := result.(map[string]any)
	if !ok {
		return false
	}
	for _, entry := range asList(asMap(resultMap["data"])["workoutScheduleSummariesScalar"]) {
		item := asMap(entry)
		if sameWorkoutID(item["workoutId"], workoutID) && item["scheduleDate"] == calendarDate {
			return true
		}
	}
	return false
}

func scheduleWorkout(ctx context.Context, s *Session, in scheduleWorkoutInput) (any, error) {
	date, err := validateWorkoutDate(in.CalendarDate, "calendar_date")
	if err != nil {
		return map[string]any{
			"status": "failed", "workout_id": in.WorkoutID, "scheduled_date": in.CalendarDate,
			"message": err.Error(),
		}, nil
	}
	if alreadyScheduled(ctx, s, int64(in.WorkoutID), date) {
		return map[string]any{
			"status": "success", "workout_id": in.WorkoutID, "scheduled_date": date, "idempotent": true,
			"message": fmt.Sprintf("Workout %d already scheduled for %s — no action taken", in.WorkoutID, date),
		}, nil
	}
	tracker := newWriteTracker("schedule_workout", s.logWriter()).withPrefix("Error scheduling workout")
	tracker.Sent()
	if _, err := s.Client.ScheduleWorkout(ctx, int64(in.WorkoutID), date); err != nil {
		var api *garmin.APIError
		if errors.As(err, &api) {
			_ = tracker.Fail(err)
			return map[string]any{
				"status": "failed", "workout_id": in.WorkoutID, "scheduled_date": date,
				"http_status": api.Status,
				"message":     fmt.Sprintf("Failed to schedule workout: HTTP %d", api.Status),
			}, nil
		}
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	return map[string]any{
		"status": "success", "workout_id": in.WorkoutID, "scheduled_date": date,
		"message": fmt.Sprintf("Successfully scheduled workout %d for %s", in.WorkoutID, date),
	}, nil
}

func scheduleWorkouts(ctx context.Context, s *Session, in scheduleWorkoutsInput) (any, error) {
	results := make([]any, 0, len(in.Schedules))
	for _, item := range in.Schedules {
		workoutIDValue := item["workout_id"]
		calendarDateValue := item["calendar_date"]
		workoutData := item["workout_data"]

		if calendarDateValue == nil {
			results = append(results, map[string]any{
				"status": "failed", "workout_id": workoutIDValue, "scheduled_date": calendarDateValue,
				"message": "Missing required field: calendar_date",
			})
			continue
		}
		date, err := validateWorkoutDateValue(calendarDateValue, "calendar_date")
		if err != nil {
			results = append(results, map[string]any{
				"status": "failed", "workout_id": workoutIDValue, "scheduled_date": calendarDateValue,
				"message": err.Error(),
			})
			continue
		}
		if workoutIDValue == nil && workoutData == nil {
			results = append(results, map[string]any{
				"status": "failed", "workout_id": nil, "scheduled_date": date,
				"message": "Missing required fields: provide either workout_id or workout_data",
			})
			continue
		}

		workoutName := ""
		var workoutID int64
		if workoutData != nil {
			data, ok := workoutData.(map[string]any)
			if !ok {
				results = append(results, map[string]any{
					"status": "error", "scheduled_date": date, "message": "Error scheduling workout: workout_data must be an object",
				})
				continue
			}
			tracker := newWriteTracker("schedule_workouts", s.logWriter()).withPrefix("Error scheduling workout")
			if err := normalizeWorkoutSteps(data); err != nil {
				results = append(results, map[string]any{"status": "error", "scheduled_date": date, "message": "Error scheduling workout: " + err.Error()})
				continue
			}
			if err := validateEndConditionSteps(data); err != nil {
				results = append(results, map[string]any{"status": "error", "scheduled_date": date, "message": "Error scheduling workout: " + err.Error()})
				continue
			}
			if err := validateTargetTypeSteps(data); err != nil {
				results = append(results, map[string]any{"status": "error", "scheduled_date": date, "message": "Error scheduling workout: " + err.Error()})
				continue
			}
			tracker.Sent()
			uploadResult, err := s.Client.UploadWorkout(ctx, data)
			if err != nil {
				results = append(results, map[string]any{"status": "error", "scheduled_date": date, "message": writeFailureMessage(tracker, err)})
				continue
			}
			tracker.Committed()
			uploadMap, ok := uploadResult.(map[string]any)
			if !ok || uploadMap["workoutId"] == nil {
				results = append(results, map[string]any{
					"status": "failed", "scheduled_date": date, "message": "Upload succeeded but no workout_id returned",
				})
				continue
			}
			workoutID, err = workoutIDNumber(uploadMap["workoutId"])
			if err != nil {
				results = append(results, map[string]any{"status": "error", "scheduled_date": date, "message": "Error scheduling workout: " + err.Error()})
				continue
			}
			if name, ok := uploadMap["workoutName"].(string); ok {
				workoutName = name
			}
		} else {
			var err error
			workoutID, err = workoutIDNumber(workoutIDValue)
			if err != nil {
				results = append(results, map[string]any{
					"status": "error", "workout_id": workoutIDValue, "scheduled_date": date,
					"message": "Error scheduling workout: " + err.Error(),
				})
				continue
			}
		}

		if alreadyScheduled(ctx, s, workoutID, date) {
			entry := map[string]any{
				"status": "success", "workout_id": workoutID, "scheduled_date": date, "idempotent": true,
				"message": fmt.Sprintf("Workout %d already scheduled for %s — no action taken", workoutID, date),
			}
			if workoutName != "" {
				entry["workout_name"] = workoutName
			}
			results = append(results, entry)
			continue
		}

		tracker := newWriteTracker("schedule_workouts", s.logWriter()).withPrefix("Error scheduling workout")
		tracker.Sent()
		if _, err := s.Client.ScheduleWorkout(ctx, workoutID, date); err != nil {
			results = append(results, map[string]any{
				"status": "error", "workout_id": workoutID, "scheduled_date": date,
				"message": writeFailureMessage(tracker, err),
			})
			continue
		}
		tracker.Committed()
		entry := map[string]any{
			"status": "success", "workout_id": workoutID, "scheduled_date": date,
			"message": fmt.Sprintf("Successfully scheduled workout %d for %s", workoutID, date),
		}
		if workoutName != "" {
			entry["workout_name"] = workoutName
		}
		results = append(results, entry)
	}
	succeeded := 0
	for _, result := range results {
		if asMap(result)["status"] == "success" {
			succeeded++
		}
	}
	return map[string]any{
		"total": len(results), "succeeded": succeeded, "failed": len(results) - succeeded, "results": results,
	}, nil
}

func rescheduleWorkout(ctx context.Context, s *Session, in rescheduleWorkoutInput) (any, error) {
	date, err := validateWorkoutDate(in.CalendarDate, "calendar_date")
	if err != nil {
		return map[string]any{
			"status": "failed", "scheduled_workout_id": in.ScheduledWorkoutID,
			"scheduled_date": in.CalendarDate, "message": err.Error(),
		}, nil
	}
	currentValue, err := s.Client.GetScheduledWorkoutByID(ctx, int64(in.ScheduledWorkoutID))
	if err != nil {
		return map[string]any{
			"status": "failed", "scheduled_workout_id": in.ScheduledWorkoutID, "scheduled_date": date,
			"message": "Failed to reschedule workout: " + err.Error(),
		}, nil
	}
	if !truthy(currentValue) {
		return map[string]any{
			"status": "failed", "scheduled_workout_id": in.ScheduledWorkoutID,
			"message": fmt.Sprintf("Scheduled workout %d not found", in.ScheduledWorkoutID),
		}, nil
	}
	current := asMap(currentValue)
	workout := asMap(current["workout"])
	var workoutRef any
	if len(workout) > 0 {
		workoutRef = map[string]any{"workoutId": workout["workoutId"]}
	}
	payload := make(map[string]any, len(current)+2)
	for k, v := range current {
		payload[k] = v
	}
	payload["calendarDate"] = date
	payload["workout"] = workoutRef

	tracker := newWriteTracker("reschedule_workout", s.logWriter()).withPrefix("Failed to reschedule workout")
	tracker.Sent()
	if _, err := s.Client.RescheduleWorkout(ctx, int64(in.ScheduledWorkoutID), payload); err != nil {
		var api *garmin.APIError
		if errors.As(err, &api) {
			_ = tracker.Fail(err)
			return map[string]any{
				"status": "failed", "scheduled_workout_id": in.ScheduledWorkoutID,
				"scheduled_date": date, "http_status": api.Status,
				"message": fmt.Sprintf("Failed to reschedule workout: HTTP %d", api.Status),
			}, nil
		}
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	return map[string]any{
		"status": "success", "scheduled_workout_id": in.ScheduledWorkoutID,
		"workout_id": workout["workoutId"], "previous_date": current["calendarDate"],
		"scheduled_date": date,
		"message":        fmt.Sprintf("Moved scheduled workout %d to %s", in.ScheduledWorkoutID, date),
	}, nil
}

func unscheduleWorkout(ctx context.Context, s *Session, in unscheduleWorkoutInput) (any, error) {
	tracker := newWriteTracker("unschedule_workout", s.logWriter()).withPrefix("Failed to unschedule workout")
	tracker.Sent()
	if _, err := s.Client.UnscheduleWorkout(ctx, int64(in.ScheduledWorkoutID)); err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	return map[string]any{
		"status": "success", "scheduled_workout_id": in.ScheduledWorkoutID,
		"message": fmt.Sprintf("Scheduled workout %d removed from calendar", in.ScheduledWorkoutID),
	}, nil
}

func unscheduleWorkouts(ctx context.Context, s *Session, in unscheduleWorkoutsInput) (any, error) {
	results := make([]any, 0, len(in.ScheduledWorkoutIDs))
	for _, id := range in.ScheduledWorkoutIDs {
		tracker := newWriteTracker("unschedule_workouts", s.logWriter()).withPrefix("Error unscheduling workout")
		tracker.Sent()
		if _, err := s.Client.UnscheduleWorkout(ctx, int64(id)); err != nil {
			results = append(results, map[string]any{
				"status": "error", "scheduled_workout_id": id, "message": writeFailureMessage(tracker, err),
			})
			continue
		}
		tracker.Committed()
		results = append(results, map[string]any{
			"status": "success", "scheduled_workout_id": id,
			"message": fmt.Sprintf("Scheduled workout %d removed from calendar", id),
		})
	}
	succeeded := 0
	for _, result := range results {
		if asMap(result)["status"] == "success" {
			succeeded++
		}
	}
	return map[string]any{
		"total": len(results), "succeeded": succeeded, "failed": len(results) - succeeded, "results": results,
	}, nil
}
