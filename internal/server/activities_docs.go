package server

// Tool descriptions: the Python docstrings without their Args sections (the argument text is in the input structs).

const (
	activitiesByDateDesc = `Get activities between specified dates with pagination support.

For accounts with large activity histories, broad date ranges can return
thousands of activities in a single response. Use page and page_size to
retrieve activities in manageable chunks and avoid "result too large" errors.
Activities are ordered newest-first.

Pagination: when has_more is true the response includes next_page — pass
that value as page on the next call to retrieve the following page. Repeat
until has_more is false.

Note: total_count for a date range is not available from the Garmin API
without fetching all results. Use has_more / next_page to walk pages.

Each activity includes an event_type field with values such as:
  - "race"          — explicitly tagged as a race by the user
  - "training"      — explicitly tagged as a training activity
  - "uncategorized" — no event type set; common for Peloton imports and
                      untagged outdoor runs. Distinct from "training":
                      filter for races with event_type == "race" rather
                      than excluding "training", since many non-race
                      activities appear as "uncategorized" not "training"
  - field absent    — API returned no eventType for this activity; not
                      observed in practice in any activity back to 2012
                      (oldest activities sampled on this account)`

	getActivityDesc = `Get detailed information for a single activity.

Returns a comprehensive summary including timing, distance, heart rate,
elevation, training effect, and an event_type field. Common event_type
values: "race", "training", "uncategorized" (no event type set by the
user). The field is omitted when Garmin returns no eventTypeDTO.`

	setActivityTypeDesc = `Change the activity type (sport) of an activity.

Useful for reclassifying a mislabelled activity, e.g. flipping a run
logged as 'trail_running' to 'running', or a 'treadmill_running' walk to
'treadmill_walking'. Call get_activity_types to see all valid type keys.`

	setActivityDescriptionDesc = `Set or update the free-text description (notes) of an activity.

This is the notes field shown on the activity page — useful for
recording how a session felt, kit used, conditions, niggles, etc.
Pass an empty string to clear an existing description.`

	setActivityEventTypeDesc = `Set the event type of an activity.

Event type categorises the activity's purpose. Valid keys:
race, recreation, specialEvent, training, transportation, touring,
geocaching, fitness, uncategorized.`

	setPerceivedEffortDesc = `Set the perceived effort (RPE) for an activity.

Mirrors Garmin Connect's 'Perceived Effort' rating on a 0-10 scale,
where 0 clears the rating. Internally Garmin stores this multiplied by
10 (so RPE 7 is stored as 70); this tool handles the conversion.`

	setActivityFeelDesc = `Set how an activity felt ('How did you feel?').

Mirrors Garmin Connect's 5-point feel rating, stored as one of:
  0   = very tired / poor
  25  = tired
  50  = normal
  75  = good
  100 = strong
Higher is better.`

	activityWeatherDesc = `Get weather data for an activity.

Garmin's weather endpoint returns temperatures in Fahrenheit (from the
weather-station source) with no unit indicator, regardless of account
settings. This tool converts them to the account's display unit: metric
accounts get Celsius, statute_us accounts keep Fahrenheit. The
temperature_unit field ("F" or "C") states which unit was returned.

Wind speed, unlike temperature, is already returned in the account's
display unit (km/h for metric, mph for statute_us), so it is passed
through unconverted and labeled via the wind_speed_unit field.`

	setActivityExerciseSetsDesc = `Replace the exercise sets on a strength (or other rep-counting) activity.

Writes Garmin's structured per-set data, the same grid the Garmin Connect
"Edit sets" screen saves, so the sets feed Garmin's exercise history,
volume and muscle-map charts. This OVERWRITES every existing set,
including watch-detected sets and their recorded timing; to keep some,
read them with get_activity_exercise_sets and include them in the list.

When the watch already recorded the right sets but misidentified the
exercises, pass keep_timing=True: the existing sets keep their start
times, durations and rest periods, and only the exercise, reps and
weight are replaced, in order. The number of sets must match the
activity's recorded active sets, and duration_seconds/rest_seconds are
ignored.

Exercises must use Garmin's own keys. category is an exercise category
(e.g. DEADLIFT, BENCH_PRESS, PULL_UP, SQUAT) and exercise_name an
exercise within it (e.g. BARBELL_DEADLIFT, ROMANIAN_DEADLIFT). Garmin
rejects anything else with "Invalid Category" or "Invalid Sub-Category",
and rejects the whole request, so a typo leaves the activity unchanged.
Valid keys, grouped by category, are in
https://connect.garmin.com/web-data/exercises/Exercises.json`

	getActivitiesDesc = `Get activities with pagination support.

Retrieves a paginated list of activities ordered newest-first. Use this
for browsing through large activity lists when you do not need to filter
by date range, or as a complement to get_activities_by_date.

Each activity includes an event_type field. Common values: "race",
"training", "uncategorized" (no event type set by the user — common for
Peloton imports and untagged runs). Filter for races with
event_type == "race" rather than excluding "training", as many non-race
activities appear as "uncategorized" rather than "training".`

	createManualActivityDesc = `Log a manual activity in Garmin Connect — useful for activities done without a watch.

The type_key must match a Garmin activity type. Use get_activity_types to see
the full list. Common values: yoga, strength_training, meditation, indoor_cycling,
pilates, bouldering, fitness_equipment.

If a write fails with write_committed="unknown", check whether the
activity exists before retrying to avoid creating a duplicate.`

	deleteActivityDesc = `Permanently delete an activity from Garmin Connect.

This cannot be undone. Call once with confirm=false (the default) to
preview the activity, then call again with confirm=true to delete it.
confirm=true is the scripting equivalent of a force flag.`
)
