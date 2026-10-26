package server

// Tool descriptions: the Python docstrings without their Args sections (the argument text is in the input structs).
const (
	getWorkoutsDesc = `Get all workouts with curated summary list

Returns a count and list of workout summaries with essential metadata only.
For detailed workout information including segments, use get_workout_by_id.`

	getWorkoutByIdDesc = `Get detailed information for a specific workout

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

	downloadWorkoutDesc = `Download a workout as a FIT file

Downloads the workout in FIT format. The binary data cannot be returned
directly through the MCP interface, but this confirms the workout is available.`

	uploadWorkoutDesc = `Upload a workout from JSON data

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

	uploadWorkoutsDesc = `Upload multiple workouts from JSON data in a single call

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

	deleteWorkoutDesc = `Delete a workout from Garmin Connect

Permanently removes a workout from your Garmin Connect workout library.
This cannot be undone. Call once with confirm=false (the default) to
preview the workout, then call again with confirm=true to delete it.`

	deleteWorkoutsDesc = `Delete multiple workouts from Garmin Connect in a single call

Permanently removes multiple workouts from your Garmin Connect workout library.
This cannot be undone. Call once with confirm=false (the default) to
preview the workouts, then call again with confirm=true to delete them.`

	getScheduledWorkoutsDesc = `Get scheduled workouts between two dates with curated summary list

Returns workouts that have been scheduled on the Garmin Connect calendar,
including their scheduled dates and completion status.`

	getGarminCoachWorkoutsDesc = `Get Garmin Coach workouts around the given date

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

	getTrainingPlanWorkoutsDesc = `Compatibility alias for get_garmin_coach_workouts

Prefer get_garmin_coach_workouts for new requests. This legacy tool
returns the same Garmin Coach/training-plan data; do not call both for
one request. Adaptive plans expose only Garmin's currently generated
window, typically the current week; future dates may return no workouts
even while a plan is active.

Adaptive training plans typically expose workout_uuid; other plan
families may expose numeric workout_id. Pass whichever identifier is
present to get_workout_by_id. The returned count includes rest days.`

	scheduleWorkoutDesc = `Schedule a workout to a specific calendar date

This adds an existing workout from your Garmin workout library
to your Garmin Connect calendar on the specified date.

Idempotent: if the workout is already scheduled for that date, this
is a no-op that reports success without creating a duplicate entry.`

	scheduleWorkoutsDesc = `Schedule multiple workouts to specific calendar dates

This adds workouts to your Garmin Connect calendar in a single call.
Each item can either reference an existing workout by ID, or provide
inline workout_data to upload-and-schedule in one step.

Args:
    schedules: List of workout schedules, each with:
        - calendar_date (str): Date to schedule the workout in YYYY-MM-DD format (required)
        - workout_id (int): ID of an existing workout to schedule (required unless workout_data is provided)
        - workout_data (dict): Inline workout JSON to upload first, then schedule (optional).
          When provided, workout_id is not required. Uses the same structure and
          target-value rules as upload_workout; pace targets are in m/s, not
          min/km (5:00/km = 3.333 m/s).

Examples:
    Schedule existing workouts by ID:
    [{"workout_id": 123456, "calendar_date": "2024-01-15"},
     {"workout_id": 789012, "calendar_date": "2024-01-17"}]

    Upload and schedule inline:
    [{"calendar_date": "2024-01-15", "workout_data": {"workoutName": "Easy Run", ...}},
     {"workout_id": 789012, "calendar_date": "2024-01-17"}]`

	rescheduleWorkoutDesc = `Move a scheduled workout to another date on the Garmin Connect calendar

Moves the existing calendar entry in place, so its id stays the same
and no duplicate entry is created. Prefer this over unschedule +
schedule. Safe to repeat: moving an entry to the date it is already
on changes nothing.

IMPORTANT: scheduled_workout_id is the calendar-entry id, which is
different from the workout's id. Get it from get_scheduled_workouts
(the "scheduled_workout_id" field), not from get_workouts.`

	unscheduleWorkoutDesc = `Remove a scheduled workout from the Garmin Connect calendar

Deletes a calendar entry without deleting the underlying workout
template — the workout stays in your library and can be re-scheduled.

IMPORTANT: scheduled_workout_id is the calendar-entry id, which is
different from the workout's id. Get it from get_scheduled_workouts
(the "scheduled_workout_id" field), not from get_workouts.

Note: the scheduled-workouts listing is an eventually-consistent index.
If you just scheduled this workout, allow a moment before unscheduling
so the id is available.`

	unscheduleWorkoutsDesc = `Remove multiple scheduled workouts from the Garmin Connect calendar

Deletes multiple calendar entries in a single call. The underlying
workout templates are left intact in your library.

IMPORTANT: each id is a calendar-entry id (the "scheduled_workout_id"
field from get_scheduled_workouts), not a workout id.`

	getCalendarEventsDesc = `Get races and events on the Garmin Connect calendar between two dates

Returns calendar entries the user added or subscribed to in Garmin
Connect, such as upcoming races. Use this to answer questions about
which events or races are scheduled.

These entries are not returned by get_scheduled_workouts, which covers
only workouts, nor by get_goals. This is the only tool that exposes
them.

Each event reports its distance in meters when Garmin stores one,
the user's custom goal time in seconds when set, the local start
time when the organiser published it, and two flags: is_race marks
the entry as a race rather than a general event, and primary_event
marks the goal race that an active training plan targets. Goal time
comes from a per-event detail request.`
)
