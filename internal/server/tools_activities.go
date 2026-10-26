package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

const (
	activitiesByDateDescription = `Get activities between specified dates with pagination support.

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

	activitiesFordateDescription = "Get activities for a specific date"

	getActivityDescription = `Get detailed information for a single activity.

Returns a comprehensive summary including timing, distance, heart rate,
elevation, training effect, and an event_type field. Common event_type
values: "race", "training", "uncategorized" (no event type set by the
user). The field is omitted when Garmin returns no eventTypeDTO.`

	setActivityNameDescription = `Set or update the name of an activity.`

	setActivityTypeDescription = `Change the activity type (sport) of an activity.

Useful for reclassifying a mislabelled activity, e.g. flipping a run
logged as 'trail_running' to 'running', or a 'treadmill_running' walk to
'treadmill_walking'. Call get_activity_types to see all valid type keys.`

	setActivityDescriptionDescription = `Set or update the free-text description (notes) of an activity.

This is the notes field shown on the activity page — useful for
recording how a session felt, kit used, conditions, niggles, etc.
Pass an empty string to clear an existing description.`

	setActivityEventTypeDescription = `Set the event type of an activity.

Event type categorises the activity's purpose. Valid keys:
race, recreation, specialEvent, training, transportation, touring,
geocaching, fitness, uncategorized.`

	setPerceivedEffortDescription = `Set the perceived effort (RPE) for an activity.

Mirrors Garmin Connect's 'Perceived Effort' rating on a 0-10 scale,
where 0 clears the rating. Internally Garmin stores this multiplied by
10 (so RPE 7 is stored as 70); this tool handles the conversion.`

	setActivityFeelDescription = `Set how an activity felt ('How did you feel?').

Mirrors Garmin Connect's 5-point feel rating, stored as one of:
  0   = very tired / poor
  25  = tired
  50  = normal
  75  = good
  100 = strong
Higher is better.`

	getActivitySplitsDescription = `Get splits for an activity

Lap and nested length start_time_gmt timestamps are GMT (UTC).`

	getActivityTypedSplitsDescription = "Get typed splits for an activity"

	getActivitySplitSummariesDescription = "Get split summaries for an activity"

	getActivityWeatherDescription = `Get weather data for an activity.

Garmin's weather endpoint returns temperatures in Fahrenheit (from the
weather-station source) with no unit indicator, regardless of account
settings. This tool converts them to the account's display unit: metric
accounts get Celsius, statute_us accounts keep Fahrenheit. The
temperature_unit field ("F" or "C") states which unit was returned.

Wind speed, unlike temperature, is already returned in the account's
display unit (km/h for metric, mph for statute_us), so it is passed
through unconverted and labeled via the wind_speed_unit field.`

	getActivityHRInTimezonesDescription = "Get heart rate data in different time zones for an activity"

	getActivityPowerInTimezonesDescription = `Get power distribution across training zones for an activity.

Returns time spent in each power zone with watt thresholds and duration.
Requires a power meter. Zones are based on the athlete's FTP configured in Garmin Connect.`

	getActivityGearDescription = "Get gear data used for an activity"

	getActivityExerciseSetsDescription = "Get exercise sets for strength training activities"

	setActivityExerciseSetsDescription = `Replace the exercise sets on a strength (or other rep-counting) activity.

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

	countActivitiesDescription = `Get total count of activities in the user's Garmin account

Returns the total number of activities recorded.`

	getActivitiesDescription = `Get activities with pagination support.

Retrieves a paginated list of activities ordered newest-first. Use this
for browsing through large activity lists when you do not need to filter
by date range, or as a complement to get_activities_by_date.

Each activity includes an event_type field. Common values: "race",
"training", "uncategorized" (no event type set by the user — common for
Peloton imports and untagged runs). Filter for races with
event_type == "race" rather than excluding "training", as many non-race
activities appear as "uncategorized" rather than "training".`

	createManualActivityDescription = `Log a manual activity in Garmin Connect — useful for activities done without a watch.

The type_key must match a Garmin activity type. Use get_activity_types to see
the full list. Common values: yoga, strength_training, meditation, indoor_cycling,
pilates, bouldering, fitness_equipment.

If a write fails with write_committed="unknown", check whether the
activity exists before retrying to avoid creating a duplicate.`

	deleteActivityDescription = `Permanently delete an activity from Garmin Connect.

This cannot be undone. Call once with confirm=false (the default) to
preview the activity, then call again with confirm=true to delete it.
confirm=true is the scripting equivalent of a force flag.`

	getActivityTypesDescription = `Get all available activity types

Returns a list of all activity types supported by Garmin Connect,
useful for filtering activities by type.`

	uint32DistanceWrapM = float64(uint64(1)<<32) / 100
	maxWrappedNegativeM = 1000.0
)

// activityIDValue converts a Python Union[int, str] activity_id argument.
// The schema is untyped because MCP clients may stringify large IDs.
func activityIDValue(v any) (int64, error) {
	switch x := v.(type) {
	case int:
		return int64(x), nil
	case int64:
		return x, nil
	case float64:
		if x != math.Trunc(x) {
			return 0, fmt.Errorf("activity_id must be an integer, got: %v", x)
		}
		return int64(x), nil
	case json.Number:
		return x.Int64()
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("activity_id must be an integer, got: %q", x)
		}
		return n, nil
	}
	return 0, fmt.Errorf("activity_id must be an integer, got: %v", v)
}

func activityIDOrErr(v any, prefix string) (int64, error) {
	id, err := activityIDValue(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", prefix, err)
	}
	return id, nil
}

type activitiesByDateInput struct {
	Account
	StartDate    string `json:"start_date" jsonschema:"Start date in YYYY-MM-DD format"`
	EndDate      string `json:"end_date" jsonschema:"End date in YYYY-MM-DD format"`
	ActivityType string `json:"activity_type,omitempty" jsonschema:"Optional activity type filter (e.g., cycling, running, swimming)"`
	Page         int    `json:"page,omitempty" jsonschema:"Zero-based page number (default 0)"`
	PageSize     *int   `json:"page_size,omitempty" jsonschema:"Number of activities per page, max 200 (default 100)"`
}

type activitiesFordateInput struct {
	Account
	Date string `json:"date" jsonschema:"Date in YYYY-MM-DD format"`
}

type activityInput struct {
	Account
	ActivityID any `json:"activity_id" jsonschema:"ID of the activity to retrieve"`
}

type activitySplitsInput struct {
	Account
	ActivityID any `json:"activity_id" jsonschema:"ID of the activity to retrieve splits for"`
}

type activityTypedSplitsInput struct {
	Account
	ActivityID any `json:"activity_id" jsonschema:"ID of the activity to retrieve typed splits for"`
}

type activitySplitSummariesInput struct {
	Account
	ActivityID any `json:"activity_id" jsonschema:"ID of the activity to retrieve split summaries for"`
}

type activityWeatherInput struct {
	Account
	ActivityID any `json:"activity_id" jsonschema:"ID of the activity to retrieve weather data for"`
}

type activityHRZonesInput struct {
	Account
	ActivityID any `json:"activity_id" jsonschema:"ID of the activity to retrieve heart rate time zone data for"`
}

type activityPowerZonesInput struct {
	Account
	ActivityID any `json:"activity_id" jsonschema:"ID of the activity to retrieve power zone data for"`
}

type activityGearInput struct {
	Account
	ActivityID any `json:"activity_id" jsonschema:"ID of the activity to retrieve gear data for"`
}

type activityExerciseSetsInput struct {
	Account
	ActivityID any `json:"activity_id" jsonschema:"ID of the activity to retrieve exercise sets for"`
}

type activityTypesInput struct {
	Account
}

type setActivityNameInput struct {
	Account
	ActivityID   any    `json:"activity_id" jsonschema:"ID of the activity to update"`
	ActivityName string `json:"activity_name" jsonschema:"New activity name. Max 200 characters; Garmin rejects longer."`
}

type setActivityTypeInput struct {
	Account
	ActivityID any    `json:"activity_id" jsonschema:"ID of the activity to update"`
	TypeKey    string `json:"type_key" jsonschema:"Target activity type key (e.g. 'running', 'trail_running', 'treadmill_running', 'cycling', 'lap_swimming')"`
}

type setActivityDescriptionInput struct {
	Account
	ActivityID  any    `json:"activity_id" jsonschema:"ID of the activity to update"`
	Description string `json:"description" jsonschema:"New description text (empty string clears it). Max 2000 characters; Garmin rejects longer. Emoji and other non-BMP characters count as two."`
}

type setActivityEventTypeInput struct {
	Account
	ActivityID any    `json:"activity_id" jsonschema:"ID of the activity to update"`
	EventType  string `json:"event_type" jsonschema:"Target event type key (e.g. 'race', 'training')"`
}

type setPerceivedEffortInput struct {
	Account
	ActivityID any     `json:"activity_id" jsonschema:"ID of the activity to update"`
	RPE        float64 `json:"rpe" jsonschema:"Perceived effort from 0 to 10 (0 clears the rating)"`
}

type setActivityFeelInput struct {
	Account
	ActivityID any `json:"activity_id" jsonschema:"ID of the activity to update"`
	Feel       int `json:"feel" jsonschema:"One of 0, 25, 50, 75, 100"`
}

type setActivityExerciseSetsInput struct {
	Account
	ActivityID any              `json:"activity_id" jsonschema:"ID of the activity to update"`
	Sets       []map[string]any `json:"sets" jsonschema:"Active sets in the order performed. Each is a dict with: category (required): Garmin exercise category key; exercise_name: Garmin exercise key within the category; omit to record just the category; reps: Repetition count; omit for timed exercises; weight_kg or weight_lb: Load lifted; omit for bodyweight; duration_seconds: Time under work for the set; rest_seconds: Rest after this set, recorded as a REST set"`
	KeepTiming bool             `json:"keep_timing,omitempty" jsonschema:"Keep the recorded set timing and rest periods, replacing only exercise, reps and weight (default False)"`
}

type countActivitiesInput struct {
	Account
}

type getActivitiesInput struct {
	Account
	Start int  `json:"start,omitempty" jsonschema:"Starting index (default 0)"`
	Limit *int `json:"limit,omitempty" jsonschema:"Maximum number of activities to return (default 20, max 100)"`
}

type createManualActivityInput struct {
	Account
	TypeKey         string  `json:"type_key" jsonschema:"Activity type key (e.g. \"yoga\", \"strength_training\")"`
	Date            string  `json:"date" jsonschema:"Date of the activity in YYYY-MM-DD format"`
	DurationMinutes int     `json:"duration_minutes" jsonschema:"Duration of the activity in minutes"`
	StartTime       string  `json:"start_time,omitempty" jsonschema:"Start time as HH:MM (24-hour, default 09:00)"`
	ActivityName    string  `json:"activity_name,omitempty" jsonschema:"Optional title; defaults to the type_key if not provided"`
	DistanceKm      float64 `json:"distance_km,omitempty" jsonschema:"Distance in kilometres (default 0.0 for non-distance activities)"`
	TimeZone        string  `json:"time_zone,omitempty" jsonschema:"IANA time zone for the activity (default UTC)"`
}

type deleteActivityInput struct {
	Account
	ActivityID any  `json:"activity_id" jsonschema:"ID of the activity to delete"`
	Confirm    bool `json:"confirm,omitempty" jsonschema:"Must be true to actually delete. Default false returns a preview."`
}

func registerActivityTools(r *registry) {
	addTool(r, "get_activities_by_date", activitiesByDateDescription, getActivitiesByDate)
	addTool(r, "get_activities_fordate", activitiesFordateDescription, getActivitiesFordate)
	addTool(r, "get_activity", getActivityDescription, getActivity)
	addTool(r, "set_activity_name", setActivityNameDescription, setActivityName)
	addTool(r, "set_activity_type", setActivityTypeDescription, setActivityType)
	addTool(r, "set_activity_description", setActivityDescriptionDescription, setActivityDescription)
	addTool(r, "set_activity_event_type", setActivityEventTypeDescription, setActivityEventType)
	addTool(r, "set_perceived_effort", setPerceivedEffortDescription, setPerceivedEffort)
	addTool(r, "set_activity_feel", setActivityFeelDescription, setActivityFeel)
	addTool(r, "get_activity_splits", getActivitySplitsDescription, getActivitySplits)
	addTool(r, "get_activity_typed_splits", getActivityTypedSplitsDescription, getActivityTypedSplits)
	addTool(r, "get_activity_split_summaries", getActivitySplitSummariesDescription, getActivitySplitSummaries)
	addTool(r, "get_activity_weather", getActivityWeatherDescription, getActivityWeather)
	addTool(r, "get_activity_hr_in_timezones", getActivityHRInTimezonesDescription, getActivityHRInTimezones)
	addTool(r, "get_activity_power_in_timezones", getActivityPowerInTimezonesDescription, getActivityPowerInTimezones)
	addTool(r, "get_activity_gear", getActivityGearDescription, getActivityGear)
	addTool(r, "get_activity_exercise_sets", getActivityExerciseSetsDescription, getActivityExerciseSets)
	addTool(r, "set_activity_exercise_sets", setActivityExerciseSetsDescription, setActivityExerciseSets)
	addTool(r, "count_activities", countActivitiesDescription, countActivities)
	addTool(r, "get_activities", getActivitiesDescription, getActivities)
	addTool(r, "create_manual_activity", createManualActivityDescription, createManualActivity)
	addTool(r, "delete_activity", deleteActivityDescription, deleteActivity)
	addTool(r, "get_activity_types", getActivityTypesDescription, getActivityTypes)
}

// --- get_activities_by_date ---

func getActivitiesByDate(ctx context.Context, s *Session, in activitiesByDateInput) (any, error) {
	pageSize := 100
	if in.PageSize != nil {
		pageSize = *in.PageSize
	}
	pageSize = min(max(1, pageSize), 200)
	start := in.Page * pageSize

	v, err := s.Client.GetActivitiesByDatePage(ctx, in.StartDate, in.EndDate, in.ActivityType, start, pageSize)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving activities by date: %w", err)
	}
	list := asList(v)
	curated := map[string]any{
		"count":      len(list),
		"page":       in.Page,
		"page_size":  pageSize,
		"has_more":   len(list) == pageSize,
		"date_range": map[string]any{"start": in.StartDate, "end": in.EndDate},
		"activities": []any{},
	}
	if len(list) == pageSize {
		curated["next_page"] = in.Page + 1
	}
	activities := make([]any, 0, len(list))
	for _, item := range list {
		activities = append(activities, pruneNil(byDateActivity(asMap(item))))
	}
	curated["activities"] = activities
	return curated, nil
}

func byDateActivity(a map[string]any) map[string]any {
	return map[string]any{
		"id":                    a["activityId"],
		"name":                  a["activityName"],
		"type":                  asMap(a["activityType"])["typeKey"],
		"event_type":            asMap(a["eventType"])["typeKey"],
		"start_time":            a["startTimeLocal"],
		"distance_meters":       a["distance"],
		"duration_seconds":      a["duration"],
		"calories":              a["calories"],
		"avg_hr_bpm":            a["averageHR"],
		"max_hr_bpm":            a["maxHR"],
		"steps":                 a["steps"],
		"elevation_gain_meters": a["elevationGain"],
		"elevation_loss_meters": a["elevationLoss"],
	}
}

// --- get_activities_fordate ---

func getActivitiesFordate(ctx context.Context, s *Session, in activitiesFordateInput) (any, error) {
	data, err := s.Client.GetActivitiesFordate(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving activities for date: %w", err)
	}
	if !truthy(data) {
		return fmt.Sprintf("No activities found for %s", in.Date), nil
	}
	payload := asList(asMap(asMap(data)["ActivitiesForDay"])["payload"])
	if len(payload) == 0 {
		return fmt.Sprintf("No activities found for %s", in.Date), nil
	}
	activities := make([]any, 0, len(payload))
	for _, item := range payload {
		activities = append(activities, pruneNil(forDateActivity(asMap(item))))
	}
	return map[string]any{
		"date":       in.Date,
		"count":      len(payload),
		"activities": activities,
	}, nil
}

func forDateActivity(a map[string]any) map[string]any {
	return map[string]any{
		"id":                         a["activityId"],
		"name":                       a["activityName"],
		"type":                       asMap(a["activityType"])["typeKey"],
		"event_type":                 asMap(a["eventType"])["typeKey"],
		"start_time":                 a["startTimeLocal"],
		"distance_meters":            a["distance"],
		"duration_seconds":           a["duration"],
		"calories":                   a["calories"],
		"avg_hr_bpm":                 a["averageHR"],
		"steps":                      a["steps"],
		"lap_count":                  a["lapCount"],
		"moderate_intensity_minutes": a["moderateIntensityMinutes"],
		"vigorous_intensity_minutes": a["vigorousIntensityMinutes"],
	}
}

// --- get_activity ---

func getActivity(ctx context.Context, s *Session, in activityInput) (any, error) {
	id, err := activityIDOrErr(in.ActivityID, "Error retrieving activity")
	if err != nil {
		return nil, err
	}
	activity, err := s.Client.GetActivity(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving activity: %w", err)
	}
	if !truthy(activity) {
		return fmt.Sprintf("No activity found with ID %d", id), nil
	}
	a := asMap(activity)
	summary := asMap(a["summaryDTO"])
	activityType := asMap(a["activityTypeDTO"])
	metadata := asMap(a["metadataDTO"])

	curated := map[string]any{
		"id":          a["activityId"],
		"name":        a["activityName"],
		"description": a["description"],
		"type":        activityType["typeKey"],
		"event_type":  asMap(a["eventTypeDTO"])["typeKey"],
		"parent_type": activityType["parentTypeId"],

		"start_time_local":         summary["startTimeLocal"],
		"start_time_gmt":           summary["startTimeGMT"],
		"duration_seconds":         summary["duration"],
		"moving_duration_seconds":  summary["movingDuration"],
		"elapsed_duration_seconds": summary["elapsedDuration"],

		"distance_meters": summary["distance"],
		"avg_speed_mps":   summary["averageSpeed"],
		"max_speed_mps":   summary["maxSpeed"],

		"avg_hr_bpm": summary["averageHR"],
		"max_hr_bpm": summary["maxHR"],
		"min_hr_bpm": summary["minHR"],

		"calories":     summary["calories"],
		"bmr_calories": summary["bmrCalories"],

		"avg_cadence":                 summary["averageRunCadence"],
		"max_cadence":                 summary["maxRunCadence"],
		"avg_stride_length_cm":        summary["strideLength"],
		"avg_ground_contact_time_ms":  summary["groundContactTime"],
		"avg_vertical_oscillation_cm": summary["verticalOscillation"],
		"steps":                       summary["steps"],

		"avg_power_watts":        summary["averagePower"],
		"max_power_watts":        summary["maxPower"],
		"normalized_power_watts": summary["normalizedPower"],

		"training_effect":           summary["trainingEffect"],
		"anaerobic_training_effect": summary["anaerobicTrainingEffect"],
		"training_effect_label":     summary["trainingEffectLabel"],
		"training_load":             summary["activityTrainingLoad"],

		"moderate_intensity_minutes": summary["moderateIntensityMinutes"],
		"vigorous_intensity_minutes": summary["vigorousIntensityMinutes"],

		"elevation_gain_meters": summary["elevationGain"],
		"elevation_loss_meters": summary["elevationLoss"],
		"max_elevation_meters":  summary["maxElevation"],
		"min_elevation_meters":  summary["minElevation"],

		"recovery_hr_bpm":     summary["recoveryHeartRate"],
		"body_battery_impact": summary["differenceBodyBattery"],

		"workout_feel": summary["directWorkoutFeel"],
		"workout_rpe":  summary["directWorkoutRpe"],

		"lap_count":           metadata["lapCount"],
		"has_splits":          metadata["hasSplits"],
		"device_manufacturer": metadata["manufacturer"],
	}
	return pruneNil(curated), nil
}

// --- set_activity_name ---

func setActivityName(ctx context.Context, s *Session, in setActivityNameInput) (any, error) {
	id, err := activityIDOrErr(in.ActivityID, "Error updating activity name")
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.ActivityName)
	if name == "" {
		return "Activity name cannot be empty", nil
	}
	tracker := newWriteTracker("set_activity_name", s.logWriter()).
		withPrefix("Error updating activity name")
	tracker.Sent()
	if _, err := s.Client.PutActivityUpdate(ctx, id, map[string]any{"activityName": name}); err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	return map[string]any{
		"success":       true,
		"activity_id":   id,
		"activity_name": name,
		"message":       "Activity name successfully updated",
	}, nil
}

// --- set_activity_type ---

func setActivityType(ctx context.Context, s *Session, in setActivityTypeInput) (any, error) {
	id, err := activityIDOrErr(in.ActivityID, "Error updating activity type")
	if err != nil {
		return nil, err
	}
	typeKey := strings.TrimSpace(in.TypeKey)
	tracker := newWriteTracker("set_activity_type", s.logWriter()).
		withPrefix("Error updating activity type")

	types, err := s.Client.GetActivityTypes(ctx)
	if err != nil {
		return nil, tracker.Fail(err)
	}
	var match map[string]any
	keys := []string{}
	for _, item := range asList(types) {
		t := asMap(item)
		if key, _ := t["typeKey"].(string); key != "" {
			keys = append(keys, key)
		}
		if t["typeKey"] == typeKey && match == nil {
			match = t
		}
	}
	if match == nil {
		sort.Strings(keys)
		return fmt.Sprintf("Unknown activity type '%s'. Valid type keys: %s", typeKey, strings.Join(keys, ", ")), nil
	}

	tracker.Sent()
	if _, err := s.Client.PutActivityUpdate(ctx, id, map[string]any{
		"activityTypeDTO": map[string]any{
			"typeId":       match["typeId"],
			"typeKey":      match["typeKey"],
			"parentTypeId": match["parentTypeId"],
		},
	}); err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	return map[string]any{
		"success":     true,
		"activity_id": id,
		"type_key":    match["typeKey"],
		"type_id":     match["typeId"],
		"message":     "Activity type successfully updated",
	}, nil
}

// --- set_activity_description ---

func setActivityDescription(ctx context.Context, s *Session, in setActivityDescriptionInput) (any, error) {
	id, err := activityIDOrErr(in.ActivityID, "Error updating activity description")
	if err != nil {
		return nil, err
	}
	tracker := newWriteTracker("set_activity_description", s.logWriter()).
		withPrefix("Error updating activity description")
	tracker.Sent()
	if _, err := s.Client.PutActivityUpdate(ctx, id, map[string]any{"description": in.Description}); err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	return map[string]any{
		"success":     true,
		"activity_id": id,
		"description": in.Description,
		"message":     "Activity description successfully updated",
	}, nil
}

// --- set_activity_event_type ---

func setActivityEventType(ctx context.Context, s *Session, in setActivityEventTypeInput) (any, error) {
	id, err := activityIDOrErr(in.ActivityID, "Error updating activity event type")
	if err != nil {
		return nil, err
	}
	eventType := strings.TrimSpace(in.EventType)
	tracker := newWriteTracker("set_activity_event_type", s.logWriter()).
		withPrefix("Error updating activity event type")

	types, err := s.Client.GetActivityEventTypes(ctx)
	if err != nil {
		return nil, tracker.Fail(err)
	}
	var match map[string]any
	keys := []string{}
	for _, item := range asList(types) {
		t := asMap(item)
		if key, _ := t["typeKey"].(string); key != "" {
			keys = append(keys, key)
		}
		if t["typeKey"] == eventType && match == nil {
			match = t
		}
	}
	if match == nil {
		return fmt.Sprintf("Unknown event type '%s'. Valid event types: %s", eventType, strings.Join(keys, ", ")), nil
	}

	tracker.Sent()
	if _, err := s.Client.PutActivityUpdate(ctx, id, map[string]any{
		"eventTypeDTO": map[string]any{
			"typeId":    match["typeId"],
			"typeKey":   match["typeKey"],
			"sortOrder": match["sortOrder"],
		},
	}); err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	return map[string]any{
		"success":     true,
		"activity_id": id,
		"event_type":  match["typeKey"],
		"message":     "Activity event type successfully updated",
	}, nil
}

// --- set_perceived_effort ---

func setPerceivedEffort(ctx context.Context, s *Session, in setPerceivedEffortInput) (any, error) {
	id, err := activityIDOrErr(in.ActivityID, "Error updating perceived effort")
	if err != nil {
		return nil, err
	}
	if in.RPE < 0 || in.RPE > 10 {
		return "rpe must be between 0 and 10", nil
	}
	tracker := newWriteTracker("set_perceived_effort", s.logWriter()).
		withPrefix("Error updating perceived effort")
	tracker.Sent()
	if _, err := s.Client.PutActivityUpdate(ctx, id, map[string]any{
		"summaryDTO": map[string]any{"directWorkoutRpe": int(math.Round(in.RPE * 10))},
	}); err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	return map[string]any{
		"success":     true,
		"activity_id": id,
		"rpe":         in.RPE,
		"message":     "Perceived effort successfully updated",
	}, nil
}

// --- set_activity_feel ---

func setActivityFeel(ctx context.Context, s *Session, in setActivityFeelInput) (any, error) {
	id, err := activityIDOrErr(in.ActivityID, "Error updating activity feel")
	if err != nil {
		return nil, err
	}
	switch in.Feel {
	case 0, 25, 50, 75, 100:
	default:
		return "feel must be one of 0, 25, 50, 75, 100", nil
	}
	tracker := newWriteTracker("set_activity_feel", s.logWriter()).
		withPrefix("Error updating activity feel")
	tracker.Sent()
	if _, err := s.Client.PutActivityUpdate(ctx, id, map[string]any{
		"summaryDTO": map[string]any{"directWorkoutFeel": in.Feel},
	}); err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	return map[string]any{
		"success":     true,
		"activity_id": id,
		"feel":        in.Feel,
		"message":     "Activity feel successfully updated",
	}, nil
}

// --- get_activity_splits ---

func wrappedLapDistance(v any) bool {
	f, ok := numberOf(v)
	return ok && f >= uint32DistanceWrapM-maxWrappedNegativeM && f <= uint32DistanceWrapM
}

func getActivitySplits(ctx context.Context, s *Session, in activitySplitsInput) (any, error) {
	id, err := activityIDOrErr(in.ActivityID, "Error retrieving activity splits")
	if err != nil {
		return nil, err
	}
	splits, err := s.Client.GetActivitySplits(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving activity splits: %w", err)
	}
	if !truthy(splits) {
		return fmt.Sprintf("No splits found for activity with ID %d", id), nil
	}
	laps := asList(asMap(splits)["lapDTOs"])
	curated := map[string]any{
		"activity_id": asMap(splits)["activityId"],
		"lap_count":   len(laps),
		"laps":        []any{},
	}
	for _, item := range laps {
		lap := asMap(item)
		lapData := map[string]any{
			"lap_number":               lap["lapIndex"],
			"start_time_gmt":           lap["startTimeGMT"],
			"distance_meters":          lap["distance"],
			"duration_seconds":         lap["duration"],
			"moving_duration_seconds":  lap["movingDuration"],
			"elapsed_duration_seconds": lap["elapsedDuration"],
			"avg_speed_mps":            lap["averageSpeed"],
			"avg_moving_speed_mps":     lap["averageMovingSpeed"],
			"max_speed_mps":            lap["maxSpeed"],
			"avg_hr_bpm":               lap["averageHR"],
			"max_hr_bpm":               lap["maxHR"],
			"calories":                 lap["calories"],
			"bmr_calories":             lap["bmrCalories"],
			"avg_cadence":              lap["averageRunCadence"],
			"avg_power_watts":          lap["averagePower"],
			"avg_swim_cadence":         lap["averageSwimCadence"],
			"active_length_count":      lap["numberOfActiveLengths"],
			"total_strokes":            lap["totalNumberOfStrokes"],
			"avg_strokes":              lap["averageStrokes"],
			"avg_swolf":                lap["averageSWOLF"],
			"avg_stroke_distance":      lap["averageStrokeDistance"],
			"intensity_type":           lap["intensityType"],
			"elevation_gain_meters":    lap["elevationGain"],
			"elevation_loss_meters":    lap["elevationLoss"],
			"workout_step_index":       lap["wktStepIndex"],
		}
		if lap["distance"] != nil && wrappedLapDistance(lap["distance"]) {
			lapData["distance_meters"] = nil
			lapData["distance_invalid"] = true
		}
		if lengths := asList(lap["lengthDTOs"]); len(lengths) > 0 {
			curatedLengths := make([]any, 0, len(lengths))
			for _, lengthItem := range lengths {
				length := asMap(lengthItem)
				curatedLengths = append(curatedLengths, pruneNil(map[string]any{
					"length_number":    length["lengthIndex"],
					"start_time_gmt":   length["startTimeGMT"],
					"distance_meters":  length["distance"],
					"duration_seconds": length["duration"],
					"avg_speed_mps":    length["averageSpeed"],
					"max_speed_mps":    length["maxSpeed"],
					"calories":         length["calories"],
					"avg_hr_bpm":       length["averageHR"],
					"max_hr_bpm":       length["maxHR"],
					"total_strokes":    length["totalNumberOfStrokes"],
					"avg_swolf":        length["averageSWOLF"],
					"stroke":           length["swimStroke"],
				}))
			}
			lapData["lengths"] = curatedLengths
		}
		curated["laps"] = append(curated["laps"].([]any), pruneNil(lapData))
	}
	return curated, nil
}

// --- raw passthrough readers ---

func getActivityTypedSplits(ctx context.Context, s *Session, in activityTypedSplitsInput) (any, error) {
	id, err := activityIDOrErr(in.ActivityID, "Error retrieving activity typed splits")
	if err != nil {
		return nil, err
	}
	v, err := s.Client.GetActivityTypedSplits(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving activity typed splits: %w", err)
	}
	if !truthy(v) {
		return fmt.Sprintf("No typed splits found for activity with ID %d", id), nil
	}
	return v, nil
}

func getActivitySplitSummaries(ctx context.Context, s *Session, in activitySplitSummariesInput) (any, error) {
	id, err := activityIDOrErr(in.ActivityID, "Error retrieving activity split summaries")
	if err != nil {
		return nil, err
	}
	v, err := s.Client.GetActivitySplitSummaries(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving activity split summaries: %w", err)
	}
	if !truthy(v) {
		return fmt.Sprintf("No split summaries found for activity with ID %d", id), nil
	}
	return v, nil
}

func getActivityHRInTimezones(ctx context.Context, s *Session, in activityHRZonesInput) (any, error) {
	id, err := activityIDOrErr(in.ActivityID, "Error retrieving activity heart rate time zone data")
	if err != nil {
		return nil, err
	}
	v, err := s.Client.GetActivityHRInTimezones(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving activity heart rate time zone data: %w", err)
	}
	if !truthy(v) {
		return fmt.Sprintf("No heart rate time zone data found for activity with ID %d", id), nil
	}
	return v, nil
}

func getActivityPowerInTimezones(ctx context.Context, s *Session, in activityPowerZonesInput) (any, error) {
	id, err := activityIDOrErr(in.ActivityID, "Error retrieving activity power zone data")
	if err != nil {
		return nil, err
	}
	v, err := s.Client.GetActivityPowerInTimezones(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving activity power zone data: %w", err)
	}
	if !truthy(v) {
		return fmt.Sprintf("No power zone data found for activity %d. Ensure the activity was recorded with a power meter.", id), nil
	}
	return v, nil
}

func getActivityGear(ctx context.Context, s *Session, in activityGearInput) (any, error) {
	id, err := activityIDOrErr(in.ActivityID, "Error retrieving activity gear data")
	if err != nil {
		return nil, err
	}
	v, err := s.Client.GetActivityGear(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving activity gear data: %w", err)
	}
	if !truthy(v) {
		return fmt.Sprintf("No gear data found for activity with ID %d", id), nil
	}
	return v, nil
}

func getActivityExerciseSets(ctx context.Context, s *Session, in activityExerciseSetsInput) (any, error) {
	id, err := activityIDOrErr(in.ActivityID, "Error retrieving activity exercise sets")
	if err != nil {
		return nil, err
	}
	v, err := s.Client.GetActivityExerciseSets(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving activity exercise sets: %w", err)
	}
	if !truthy(v) {
		return fmt.Sprintf("No exercise sets found for activity with ID %d", id), nil
	}
	return v, nil
}

// --- get_activity_weather ---

func fahrenheitToCelsius(v any) any {
	f, ok := numberOf(v)
	if !ok {
		return nil
	}
	return roundTo((f-32)*5/9, 1)
}

func getActivityWeather(ctx context.Context, s *Session, in activityWeatherInput) (any, error) {
	id, err := activityIDOrErr(in.ActivityID, "Error retrieving activity weather data")
	if err != nil {
		return nil, err
	}
	weather, err := s.Client.GetActivityWeather(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving activity weather data: %w", err)
	}
	if !truthy(weather) {
		return fmt.Sprintf("No weather data found for activity with ID %d", id), nil
	}

	unitSystem, err := s.Client.GetUnitSystem(ctx)
	if err != nil {
		unitSystem = nil
	}

	w := asMap(weather)
	rawTemp := w["temp"]
	rawApparent := w["apparentTemp"]
	rawDew := w["dewPoint"]

	var tempUnit any
	var outTemp, outApparent, outDew any
	if unitSystem != nil && unitSystem != "statute_us" {
		tempUnit = "C"
		outTemp, outApparent, outDew = fahrenheitToCelsius(rawTemp), fahrenheitToCelsius(rawApparent), fahrenheitToCelsius(rawDew)
	} else {
		if unitSystem == "statute_us" {
			tempUnit = "F"
		}
		outTemp, outApparent, outDew = rawTemp, rawApparent, rawDew
	}

	var windUnit any
	switch {
	case unitSystem == "statute_us":
		windUnit = "mph"
	case unitSystem != nil:
		windUnit = "km/h"
	}

	weatherTypeDTO := asMap(w["weatherTypeDTO"])
	stationDTO := asMap(w["weatherStationDTO"])
	curated := map[string]any{
		"activity_id":            id,
		"temperature":            outTemp,
		"temperature_unit":       tempUnit,
		"apparent_temperature":   outApparent,
		"dew_point":              outDew,
		"humidity_percent":       w["relativeHumidity"],
		"wind_speed":             w["windSpeed"],
		"wind_speed_unit":        windUnit,
		"wind_direction_degrees": w["windDirection"],
		"wind_direction_compass": w["windDirectionCompassPoint"],
		"wind_gust":              w["windGust"],
		"weather_type":           weatherTypeDTO["weatherTypeName"],
		"weather_description":    firstTruthy(weatherTypeDTO["desc"], weatherTypeDTO["weatherTypeDesc"]),
		"location":               w["issueLocation"],
		"station_id":             stationDTO["id"],
		"station_name":           stationDTO["name"],
		"issue_time":             w["issueDate"],
	}
	return pruneNil(curated), nil
}

// --- set_activity_exercise_sets ---

func buildExerciseSets(sets []map[string]any) ([]any, error) {
	out := make([]any, 0, len(sets))
	for i, set := range sets {
		n := i + 1
		category := strings.ToUpper(strings.TrimSpace(fmt.Sprint(stringValueOrEmpty(set, "category"))))
		if category == "" {
			return nil, fmt.Errorf("set %d: category is required", n)
		}
		name := strings.ToUpper(strings.TrimSpace(fmt.Sprint(stringValueOrEmpty(set, "exercise_name"))))
		var nameValue any
		if name != "" {
			nameValue = name
		}

		weightKg, hasKg := set["weight_kg"]
		weightLb, hasLb := set["weight_lb"]
		if hasKg && weightKg != nil && hasLb && weightLb != nil {
			return nil, fmt.Errorf("set %d: give weight_kg or weight_lb, not both", n)
		}
		var kg *float64
		if hasKg && weightKg != nil {
			f, ok := numberOf(weightKg)
			if !ok {
				return nil, fmt.Errorf("set %d: weight_kg must be a number", n)
			}
			kg = &f
		} else if hasLb && weightLb != nil {
			f, ok := numberOf(weightLb)
			if !ok {
				return nil, fmt.Errorf("set %d: weight_lb must be a number", n)
			}
			converted := f * 0.45359237
			kg = &converted
		}
		if kg != nil && *kg < 0 {
			return nil, fmt.Errorf("set %d: weight cannot be negative", n)
		}

		var reps any
		if raw, exists := set["reps"]; exists && raw != nil {
			f, ok := numberOf(raw)
			if !ok || f != math.Trunc(f) || f < 0 {
				return nil, fmt.Errorf("set %d: reps must be a non-negative whole number", n)
			}
			reps = int(f)
		}

		weight := -1
		if kg != nil {
			weight = int(math.Round(*kg * 1000))
		}

		if nameValue == category {
			nameValue = nil
		}

		out = append(out, map[string]any{
			"setType": "ACTIVE",
			"exercises": []any{map[string]any{
				"category":    category,
				"name":        nameValue,
				"probability": 100,
			}},
			"repetitionCount": reps,
			"weight":          weight,
			"duration":        set["duration_seconds"],
		})
		if rest := set["rest_seconds"]; truthy(rest) {
			out = append(out, map[string]any{
				"setType":         "REST",
				"exercises":       []any{},
				"repetitionCount": nil,
				"weight":          -1,
				"duration":        rest,
			})
		}
	}
	return out, nil
}

func stringValueOrEmpty(m map[string]any, key string) any {
	v := m[key]
	if !truthy(v) {
		return ""
	}
	return v
}

func setActivityExerciseSets(ctx context.Context, s *Session, in setActivityExerciseSetsInput) (any, error) {
	id, err := activityIDOrErr(in.ActivityID, "Error updating activity exercise sets")
	if err != nil {
		return nil, err
	}
	if len(in.Sets) == 0 {
		return "sets cannot be empty", nil
	}
	exerciseSets, err := buildExerciseSets(in.Sets)
	if err != nil {
		return fmt.Sprintf("Error updating activity exercise sets: %v", err), nil
	}

	if in.KeepTiming {
		existingPayload, err := s.Client.GetActivityExerciseSets(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("Error updating activity exercise sets: %w", err)
		}
		existing := asList(asMap(existingPayload)["exerciseSets"])
		recorded := 0
		for _, item := range existing {
			if asMap(item)["setType"] == "ACTIVE" {
				recorded++
			}
		}
		if recorded != len(in.Sets) {
			return fmt.Sprintf("keep_timing needs one set per recorded set: activity %d has %d active sets, got %d",
				id, recorded, len(in.Sets)), nil
		}
		active := []map[string]any{}
		for _, item := range exerciseSets {
			m := asMap(item)
			if m["setType"] == "ACTIVE" {
				active = append(active, m)
			}
		}
		next := 0
		for _, item := range existing {
			m := asMap(item)
			if m["setType"] != "ACTIVE" {
				continue
			}
			replacement := active[next]
			next++
			m["exercises"] = replacement["exercises"]
			m["repetitionCount"] = replacement["repetitionCount"]
			m["weight"] = replacement["weight"]
		}
		exerciseSets = existing
	}

	tracker := newWriteTracker("set_activity_exercise_sets", s.logWriter()).
		withPrefix("Error updating activity exercise sets")
	tracker.Sent()
	if _, err := s.Client.SetActivityExerciseSets(ctx, id, exerciseSets); err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	return map[string]any{
		"success":      true,
		"activity_id":  id,
		"sets_written": len(in.Sets),
		"message":      "Activity exercise sets successfully replaced",
	}, nil
}

// --- count_activities ---

func countActivities(ctx context.Context, s *Session, in countActivitiesInput) (any, error) {
	count, err := s.Client.CountActivities(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error counting activities: %w", err)
	}
	if count == nil {
		return "Unable to retrieve activity count", nil
	}
	return map[string]any{
		"total_activities": count,
		"note":             "Use get_activities() with pagination to retrieve activity details",
	}, nil
}

// --- get_activities ---

func getActivities(ctx context.Context, s *Session, in getActivitiesInput) (any, error) {
	limit := 20
	if in.Limit != nil {
		limit = *in.Limit
	}
	limit = min(max(1, limit), 100)

	v, err := s.Client.GetActivities(ctx, in.Start, limit)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving activities: %w", err)
	}
	if !truthy(v) {
		return fmt.Sprintf("No activities found at index %d", in.Start), nil
	}
	list := asList(v)
	hasMore := len(list) == limit
	curated := map[string]any{
		"start":      in.Start,
		"limit":      limit,
		"count":      len(list),
		"has_more":   hasMore,
		"next_start": nil,
		"activities": []any{},
	}
	if hasMore {
		curated["next_start"] = in.Start + limit
	}
	activities := make([]any, 0, len(list))
	for _, item := range list {
		activities = append(activities, pruneNil(pageActivity(asMap(item))))
	}
	curated["activities"] = activities
	return curated, nil
}

func pageActivity(a map[string]any) map[string]any {
	return map[string]any{
		"id":                      a["activityId"],
		"name":                    a["activityName"],
		"type":                    asMap(a["activityType"])["typeKey"],
		"event_type":              asMap(a["eventType"])["typeKey"],
		"start_time":              a["startTimeLocal"],
		"distance_meters":         a["distance"],
		"duration_seconds":        a["duration"],
		"moving_duration_seconds": a["movingDuration"],
		"calories":                a["calories"],
		"avg_hr_bpm":              a["averageHR"],
		"max_hr_bpm":              a["maxHR"],
		"steps":                   a["steps"],
		"elevation_gain_meters":   a["elevationGain"],
		"elevation_loss_meters":   a["elevationLoss"],
		"owner_display_name":      a["ownerDisplayName"],
	}
}

// --- create_manual_activity ---

func createManualActivity(ctx context.Context, s *Session, in createManualActivityInput) (any, error) {
	typeKey := strings.TrimSpace(in.TypeKey)
	if typeKey == "" {
		return "Error: type_key is required", nil
	}
	if in.DurationMinutes <= 0 {
		return "Error: duration_minutes must be greater than 0", nil
	}
	if in.DistanceKm < 0 {
		return "Error: distance_km must be non-negative", nil
	}
	if _, err := garmin.ValidateDate(in.Date, "date"); err != nil {
		return "Error: date must be a valid date in YYYY-MM-DD format", nil
	}
	startTime := in.StartTime
	if startTime == "" {
		startTime = "09:00"
	}
	if !validClockTime(startTime) {
		return "Error: start_time must be in HH:MM (24-hour) format", nil
	}
	name := strings.TrimSpace(in.ActivityName)
	if name == "" {
		name = titleCase(strings.ReplaceAll(typeKey, "_", " "))
	}
	timeZone := in.TimeZone
	if timeZone == "" {
		timeZone = "UTC"
	}

	tracker := newWriteTracker("create_manual_activity", s.logWriter()).
		withPrefix("Error creating manual activity")
	tracker.Sent()
	result, err := s.Client.CreateManualActivity(ctx,
		in.Date+"T"+startTime+":00.000", timeZone, typeKey, in.DistanceKm, in.DurationMinutes, name)
	if err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	return map[string]any{"success": true, "activity": result}, nil
}

func validClockTime(s string) bool {
	if len(s) != 5 || s[2] != ':' {
		return false
	}
	hour, err := strconv.Atoi(s[0:2])
	if err != nil {
		return false
	}
	minute, err := strconv.Atoi(s[3:5])
	if err != nil {
		return false
	}
	return hour >= 0 && hour <= 23 && minute >= 0 && minute <= 59
}

func titleCase(s string) string {
	words := strings.Fields(s)
	for i, word := range words {
		runes := []rune(strings.ToLower(word))
		if len(runes) > 0 {
			runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
		}
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}

// --- delete_activity ---

func activityPreview(ctx context.Context, c *garmin.Client, id int64) map[string]any {
	preview := map[string]any{"activity_id": id}

	activity, err := c.GetActivity(ctx, id)
	if err != nil {
		activity = nil
	}
	if a := asMap(activity); len(a) > 0 {
		summary := asMap(a["summaryDTO"])
		typeDTO := asMap(a["activityTypeDTO"])
		preview["name"] = a["activityName"]
		preview["type"] = typeDTO["typeKey"]
		preview["start_time"] = firstTruthy(summary["startTimeLocal"], a["startTimeLocal"])
		preview["distance_meters"] = firstTruthy(summary["distance"], a["distance"])
		preview["duration_seconds"] = firstTruthy(summary["duration"], a["duration"])
		return pruneNil(preview)
	}

	items, err := c.GetActivities(ctx, 0, 50)
	if err != nil {
		return pruneNil(preview)
	}
	for _, item := range asList(items) {
		entry := asMap(item)
		if !matchesActivityID(entry["activityId"], id) {
			continue
		}
		preview["name"] = entry["activityName"]
		preview["type"] = asMap(entry["activityType"])["typeKey"]
		preview["start_time"] = entry["startTimeLocal"]
		preview["distance_meters"] = entry["distance"]
		preview["duration_seconds"] = entry["duration"]
		break
	}
	return pruneNil(preview)
}

func matchesActivityID(v any, id int64) bool {
	if s, ok := v.(string); ok {
		return s == strconv.FormatInt(id, 10)
	}
	f, ok := numberOf(v)
	return ok && f == float64(id)
}

func deleteActivity(ctx context.Context, s *Session, in deleteActivityInput) (any, error) {
	id, err := activityIDValue(in.ActivityID)
	if err != nil {
		return fmt.Sprintf("Invalid activity ID: %v", in.ActivityID), nil
	}
	preview := activityPreview(ctx, s.Client, id)
	if !in.Confirm {
		return map[string]any{
			"status":   "needs_confirmation",
			"activity": preview,
			"message":  "This permanently deletes the activity from Garmin Connect and cannot be undone. Call again with confirm=true to proceed.",
		}, nil
	}

	tracker := newWriteTracker("delete_activity", s.logWriter()).
		withPrefix("Error deleting activity")
	tracker.Sent()
	if _, err := s.Client.DeleteActivity(ctx, id); err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	fmt.Fprintf(s.logWriter(), "delete_activity: deleted activity %d name=%s at %s\n",
		id, pythonRepr(preview["name"]), time.Now().UTC().Format("2006-01-02T15:04:05.000000-07:00"))
	return map[string]any{
		"status":   "deleted",
		"activity": preview,
		"message":  fmt.Sprintf("Activity %d permanently deleted.", id),
	}, nil
}

func pythonRepr(v any) string {
	if s, ok := v.(string); ok {
		return "'" + s + "'"
	}
	if v == nil {
		return "None"
	}
	return fmt.Sprint(v)
}

// --- get_activity_types ---

func getActivityTypes(ctx context.Context, s *Session, in activityTypesInput) (any, error) {
	types, err := s.Client.GetActivityTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving activity types: %w", err)
	}
	if !truthy(types) {
		return "No activity types found", nil
	}
	list := asList(types)
	curatedTypes := make([]any, 0, len(list))
	for _, item := range list {
		t := asMap(item)
		curatedTypes = append(curatedTypes, pruneNil(map[string]any{
			"type_id":        t["typeId"],
			"type_key":       t["typeKey"],
			"display_name":   t["displayName"],
			"parent_type_id": t["parentTypeId"],
			"is_hidden":      t["isHidden"],
		}))
	}
	return map[string]any{"count": len(list), "activity_types": curatedTypes}, nil
}
