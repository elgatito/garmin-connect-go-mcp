package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

// Port of garmin_mcp/activity_management.py.

// The activity_id of most tools is Union[int, str] in Python (some MCP clients stringify large ids), so it is
// declared as any and converted by parseActivityID.

type getActivityIn struct {
	Account
	ActivityID any `json:"activity_id" jsonschema:"ID of the activity to retrieve"`
}

type splitsIn struct {
	Account
	ActivityID any `json:"activity_id" jsonschema:"ID of the activity to retrieve splits for"`
}

type typedSplitsIn struct {
	Account
	ActivityID any `json:"activity_id" jsonschema:"ID of the activity to retrieve typed splits for"`
}

type splitSummariesIn struct {
	Account
	ActivityID any `json:"activity_id" jsonschema:"ID of the activity to retrieve split summaries for"`
}

type weatherIn struct {
	Account
	ActivityID any `json:"activity_id" jsonschema:"ID of the activity to retrieve weather data for"`
}

type hrZonesActivityIn struct {
	Account
	ActivityID any `json:"activity_id" jsonschema:"ID of the activity to retrieve heart rate time zone data for"`
}

type powerZonesActivityIn struct {
	Account
	ActivityID any `json:"activity_id" jsonschema:"ID of the activity to retrieve power zone data for"`
}

type activityGearIn struct {
	Account
	ActivityID any `json:"activity_id" jsonschema:"ID of the activity to retrieve gear data for"`
}

type exerciseSetsIn struct {
	Account
	ActivityID any `json:"activity_id" jsonschema:"ID of the activity to retrieve exercise sets for"`
}

type activitiesByDateIn struct {
	Account
	StartDate    string `json:"start_date" jsonschema:"Start date in YYYY-MM-DD format"`
	EndDate      string `json:"end_date" jsonschema:"End date in YYYY-MM-DD format"`
	ActivityType string `json:"activity_type,omitempty" jsonschema:"Optional activity type filter (e.g., cycling, running, swimming)"`
	Page         int    `json:"page,omitempty" jsonschema:"Zero-based page number (default 0)"`
	PageSize     *int   `json:"page_size,omitempty" jsonschema:"Number of activities per page, max 200 (default 100)"`
}

type activitiesForDateIn struct {
	Account
	Date string `json:"date" jsonschema:"Date in YYYY-MM-DD format"`
}

type setNameIn struct {
	Account
	ActivityID   any    `json:"activity_id" jsonschema:"ID of the activity to update"`
	ActivityName string `json:"activity_name" jsonschema:"New activity name. Max 200 characters; Garmin rejects longer."`
}

type setTypeIn struct {
	Account
	ActivityID any    `json:"activity_id" jsonschema:"ID of the activity to update"`
	TypeKey    string `json:"type_key" jsonschema:"Target activity type key (e.g. 'running', 'trail_running', 'treadmill_running', 'cycling', 'lap_swimming')"`
}

type setDescriptionIn struct {
	Account
	ActivityID  any    `json:"activity_id" jsonschema:"ID of the activity to update"`
	Description string `json:"description" jsonschema:"New description text (empty string clears it). Max 2000 characters; Garmin rejects longer. Emoji and other non-BMP characters count as two."`
}

type setEventTypeIn struct {
	Account
	ActivityID any    `json:"activity_id" jsonschema:"ID of the activity to update"`
	EventType  string `json:"event_type" jsonschema:"Target event type key (e.g. 'race', 'training')"`
}

type setEffortIn struct {
	Account
	ActivityID any     `json:"activity_id" jsonschema:"ID of the activity to update"`
	RPE        float64 `json:"rpe" jsonschema:"Perceived effort from 0 to 10 (0 clears the rating)"`
}

type setFeelIn struct {
	Account
	ActivityID any `json:"activity_id" jsonschema:"ID of the activity to update"`
	Feel       int `json:"feel" jsonschema:"One of 0, 25, 50, 75, 100"`
}

type setExerciseSetsIn struct {
	Account
	ActivityID any              `json:"activity_id" jsonschema:"ID of the activity to update"`
	Sets       []map[string]any `json:"sets" jsonschema:"Active sets in the order performed. Each is a dict with: category (required): Garmin exercise category key; exercise_name: Garmin exercise key within the category, omit to record just the category; reps: Repetition count, omit for timed exercises; weight_kg or weight_lb: Load lifted, omit for bodyweight; duration_seconds: Time under work for the set; rest_seconds: Rest after this set, recorded as a REST set"`
	KeepTiming bool             `json:"keep_timing,omitempty" jsonschema:"Keep the recorded set timing and rest periods, replacing only exercise, reps and weight (default False)"`
}

type listActivitiesIn struct {
	Account
	Start *int `json:"start,omitempty" jsonschema:"Starting index (default 0)"`
	Limit *int `json:"limit,omitempty" jsonschema:"Maximum number of activities to return (default 20, max 100)"`
}

type createManualIn struct {
	Account
	TypeKey         string  `json:"type_key" jsonschema:"Activity type key (e.g. \"yoga\", \"strength_training\")"`
	Date            string  `json:"date" jsonschema:"Date of the activity in YYYY-MM-DD format"`
	DurationMinutes int     `json:"duration_minutes" jsonschema:"Duration of the activity in minutes"`
	StartTime       *string `json:"start_time,omitempty" jsonschema:"Start time as HH:MM (24-hour, default 09:00)"`
	ActivityName    string  `json:"activity_name,omitempty" jsonschema:"Optional title; defaults to the type_key if not provided"`
	DistanceKm      float64 `json:"distance_km,omitempty" jsonschema:"Distance in kilometres (default 0.0 for non-distance activities)"`
	TimeZone        *string `json:"time_zone,omitempty" jsonschema:"IANA time zone for the activity (default UTC)"`
}

type deleteActivityIn struct {
	Account
	ActivityID any  `json:"activity_id" jsonschema:"ID of the activity to delete"`
	Confirm    bool `json:"confirm,omitempty" jsonschema:"Must be true to actually delete. Default false returns a preview."`
}

// ── helpers ────────────────────────────────────────────────────────────────

// parseActivityID is Python's int(activity_id) for a Union[int, str] argument.
func parseActivityID(v any) (int64, error) {
	switch x := v.(type) {
	case float64:
		if x == math.Trunc(x) && math.Abs(x) <= 1<<53 {
			return int64(x), nil
		}
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid literal for int() with base 10: %s", pyRepr(x))
		}
		return n, nil
	}
	return 0, errors.New("activity_id must be an integer")
}

// pyRepr is repr() of a str.
func pyRepr(s string) string {
	q := "'"
	if strings.Contains(s, "'") && !strings.Contains(s, `"`) {
		q = `"`
	}
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	if q == "'" {
		s = strings.ReplaceAll(s, "'", `\'`)
	}
	return q + s + q
}

// idString is str(x) for ids that Garmin sends as JSON numbers (an int in Python): whole numbers without an exponent.
func idString(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return pyStr(v)
}

// listActivity curates one entry of an activity list: the given out/src pairs plus type and event_type.
func listActivity(a map[string]any, outSrc ...string) map[string]any {
	m := pick(a, outSrc...)
	m["type"] = asMap(a["activityType"])["typeKey"]
	m["event_type"] = asMap(a["eventType"])["typeKey"]
	return dropNil(m)
}

type activityFetch = func(c *garmin.Client, ctx context.Context, id int64) (any, error)

// addActivityTool registers a one-activity read tool: parse the id, fetch, "no data" text when the payload is falsy,
// then optional curation. errPrefix is Python's "Error retrieving X" and noData its message format taking the id.
func addActivityTool[In interface{ account() string }](r *registry, name, desc, errPrefix, noData string, id func(In) any,
	fetch activityFetch, curate func(ctx context.Context, s *Session, id int64, data any) any) {
	addTool(r, name, desc, func(ctx context.Context, s *Session, in In) (any, error) {
		aid, err := parseActivityID(id(in))
		var data any
		if err == nil {
			data, err = fetch(s.Client, ctx, aid)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", errPrefix, err)
		}
		if falsy(data) {
			return fmt.Sprintf(noData, aid), nil
		}
		if curate != nil {
			return curate(ctx, s, aid, data), nil
		}
		return data, nil
	})
}

// updateActivity runs one write of a set_* tool: parse the id, run do, and report the Python "Error updating X" text.
func updateActivity[In interface{ account() string }](r *registry, name, desc, errPrefix string, id func(In) any,
	do func(ctx context.Context, s *Session, in In, aid int64) (any, error)) {
	addTool(r, name, desc, func(ctx context.Context, s *Session, in In) (any, error) {
		aid, err := parseActivityID(id(in))
		var v any
		if err == nil {
			v, err = do(ctx, s, in, aid)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", errPrefix, err)
		}
		return v, nil
	})
}

func keyError(m map[string]any, key string) (any, error) {
	v, ok := m[key]
	if !ok {
		return nil, fmt.Errorf("'%s'", key)
	}
	return v, nil
}

// typeKeys lists the non-empty typeKey values of a list of type dicts.
func typeKeys(types []any) []string {
	var keys []string
	for _, t := range types {
		if k := asMap(t)["typeKey"]; !falsy(k) {
			keys = append(keys, pyStr(k))
		}
	}
	return keys
}

func findByTypeKey(types []any, key string) map[string]any {
	for _, t := range types {
		if m := asMap(t); m != nil && m["typeKey"] == key {
			return m
		}
	}
	return nil
}

func registerActivityTools(r *registry) {
	registerActivityListTools(r)
	registerActivityReadTools(r)
	registerActivityWriteTools(r)
}

func registerActivityListTools(r *registry) {
	addTool(r, "get_activities_by_date", activitiesByDateDesc, func(ctx context.Context, s *Session, in activitiesByDateIn) (any, error) {
		pageSize := 100
		if in.PageSize != nil {
			pageSize = *in.PageSize
		}
		pageSize = min(max(1, pageSize), 200)
		start := in.Page * pageSize

		data, err := s.Client.SearchActivities(ctx, in.StartDate, in.EndDate, in.ActivityType, start, pageSize)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving activities by date: %w", err)
		}
		activities := asList(data)
		curated := map[string]any{
			"count": len(activities), "page": in.Page, "page_size": pageSize, "has_more": false,
			"date_range": map[string]any{"start": in.StartDate, "end": in.EndDate}, "activities": []any{},
		}
		if falsy(data) {
			return curated, nil
		}
		hasMore := len(activities) == pageSize
		curated["has_more"] = hasMore
		if hasMore {
			curated["next_page"] = in.Page + 1
		}
		out := []any{}
		for _, a := range activities {
			out = append(out, listActivity(asMap(a),
				"id", "activityId", "name", "activityName", "start_time", "startTimeLocal", "distance_meters", "distance",
				"duration_seconds", "duration", "calories", "calories", "avg_hr_bpm", "averageHR", "max_hr_bpm", "maxHR",
				"steps", "steps", "elevation_gain_meters", "elevationGain", "elevation_loss_meters", "elevationLoss"))
		}
		curated["activities"] = out
		return curated, nil
	})

	addTool(r, "get_activities_fordate", "Get activities for a specific date", func(ctx context.Context, s *Session, in activitiesForDateIn) (any, error) {
		data, err := s.Client.GetActivitiesForDate(ctx, in.Date)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving activities for date: %w", err)
		}
		noData := fmt.Sprintf("No activities found for %s", in.Date)
		if falsy(data) {
			return noData, nil
		}
		payload := asList(asMap(asMap(data)["ActivitiesForDay"])["payload"])
		if len(payload) == 0 {
			return noData, nil
		}
		out := []any{}
		for _, a := range payload {
			out = append(out, listActivity(asMap(a),
				"id", "activityId", "name", "activityName", "start_time", "startTimeLocal", "distance_meters", "distance",
				"duration_seconds", "duration", "calories", "calories", "avg_hr_bpm", "averageHR", "steps", "steps",
				"lap_count", "lapCount", "moderate_intensity_minutes", "moderateIntensityMinutes",
				"vigorous_intensity_minutes", "vigorousIntensityMinutes"))
		}
		return map[string]any{"date": in.Date, "count": len(payload), "activities": out}, nil
	})

	addTool(r, "count_activities", "Get total count of activities in the user's Garmin account\n\nReturns the total number of activities recorded.",
		func(ctx context.Context, s *Session, _ noArgs) (any, error) {
			count, err := s.Client.CountActivities(ctx)
			if err != nil {
				return nil, fmt.Errorf("Error counting activities: %w", err)
			}
			if count == nil {
				return "Unable to retrieve activity count", nil
			}
			return map[string]any{"total_activities": count, "note": "Use get_activities() with pagination to retrieve activity details"}, nil
		})

	addTool(r, "get_activities", getActivitiesDesc, func(ctx context.Context, s *Session, in listActivitiesIn) (any, error) {
		start, limit := 0, 20
		if in.Start != nil {
			start = *in.Start
		}
		if in.Limit != nil {
			limit = *in.Limit
		}
		limit = min(max(1, limit), 100)
		data, err := s.Client.GetActivities(ctx, start, limit, "")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving activities: %w", err)
		}
		if falsy(data) {
			return fmt.Sprintf("No activities found at index %d", start), nil
		}
		activities := asList(data)
		hasMore := len(activities) == limit
		var nextStart any
		if hasMore {
			nextStart = start + limit
		}
		out := []any{}
		for _, a := range activities {
			out = append(out, listActivity(asMap(a),
				"id", "activityId", "name", "activityName", "start_time", "startTimeLocal", "distance_meters", "distance",
				"duration_seconds", "duration", "moving_duration_seconds", "movingDuration", "calories", "calories",
				"avg_hr_bpm", "averageHR", "max_hr_bpm", "maxHR", "steps", "steps", "elevation_gain_meters", "elevationGain",
				"elevation_loss_meters", "elevationLoss", "owner_display_name", "ownerDisplayName"))
		}
		return map[string]any{
			"start": start, "limit": limit, "count": len(activities), "has_more": hasMore, "next_start": nextStart, "activities": out,
		}, nil
	})

	addTool(r, "get_activity_types", "Get all available activity types\n\nReturns a list of all activity types supported by Garmin Connect,\nuseful for filtering activities by type.",
		func(ctx context.Context, s *Session, _ noArgs) (any, error) {
			data, err := s.Client.GetActivityTypes(ctx)
			if err != nil {
				return nil, fmt.Errorf("Error retrieving activity types: %w", err)
			}
			if falsy(data) {
				return "No activity types found", nil
			}
			types := asList(data)
			out := []any{}
			for _, t := range types {
				out = append(out, dropNil(pick(asMap(t), "type_id", "typeId", "type_key", "typeKey", "display_name", "displayName",
					"parent_type_id", "parentTypeId", "is_hidden", "isHidden")))
			}
			return map[string]any{"count": len(types), "activity_types": out}, nil
		})
}

func registerActivityReadTools(r *registry) {
	addActivityTool(r, "get_activity", getActivityDesc, "Error retrieving activity", "No activity found with ID %d",
		func(in getActivityIn) any { return in.ActivityID },
		(*garmin.Client).GetActivity, func(_ context.Context, _ *Session, _ int64, data any) any { return curateActivity(asMap(data)) })

	addActivityTool(r, "get_activity_splits", "Get splits for an activity\n\nLap and nested length start_time_gmt timestamps are GMT (UTC).",
		"Error retrieving activity splits", "No splits found for activity with ID %d",
		func(in splitsIn) any { return in.ActivityID },
		(*garmin.Client).GetActivitySplits, func(_ context.Context, _ *Session, _ int64, data any) any { return curateSplits(asMap(data)) })

	addActivityTool(r, "get_activity_typed_splits", "Get typed splits for an activity",
		"Error retrieving activity typed splits", "No typed splits found for activity with ID %d",
		func(in typedSplitsIn) any { return in.ActivityID }, (*garmin.Client).GetActivityTypedSplits, nil)

	addActivityTool(r, "get_activity_split_summaries", "Get split summaries for an activity",
		"Error retrieving activity split summaries", "No split summaries found for activity with ID %d",
		func(in splitSummariesIn) any { return in.ActivityID }, (*garmin.Client).GetActivitySplitSummaries, nil)

	addActivityTool(r, "get_activity_weather", activityWeatherDesc,
		"Error retrieving activity weather data", "No weather data found for activity with ID %d",
		func(in weatherIn) any { return in.ActivityID }, (*garmin.Client).GetActivityWeather, curateWeather)

	addActivityTool(r, "get_activity_hr_in_timezones", "Get heart rate data in different time zones for an activity",
		"Error retrieving activity heart rate time zone data", "No heart rate time zone data found for activity with ID %d",
		func(in hrZonesActivityIn) any { return in.ActivityID }, (*garmin.Client).GetActivityHRInTimezones, nil)

	addActivityTool(r, "get_activity_power_in_timezones", "Get power distribution across training zones for an activity.\n\n"+
		"Returns time spent in each power zone with watt thresholds and duration.\nRequires a power meter. Zones are based on the athlete's FTP configured in Garmin Connect.",
		"Error retrieving activity power zone data", "No power zone data found for activity %d. Ensure the activity was recorded with a power meter.",
		func(in powerZonesActivityIn) any { return in.ActivityID }, (*garmin.Client).GetActivityPowerInTimezones, nil)

	addActivityTool(r, "get_activity_gear", "Get gear data used for an activity",
		"Error retrieving activity gear data", "No gear data found for activity with ID %d",
		func(in activityGearIn) any { return in.ActivityID }, (*garmin.Client).GetActivityGear, nil)

	addActivityTool(r, "get_activity_exercise_sets", "Get exercise sets for strength training activities",
		"Error retrieving activity exercise sets", "No exercise sets found for activity with ID %d",
		func(in exerciseSetsIn) any { return in.ActivityID }, (*garmin.Client).GetActivityExerciseSets, nil)
}

func curateActivity(activity map[string]any) map[string]any {
	summary := asMap(activity["summaryDTO"])
	typeDTO := asMap(activity["activityTypeDTO"])
	metadata := asMap(activity["metadataDTO"])
	curated := pick(activity, "id", "activityId", "name", "activityName", "description", "description")
	curated["type"] = typeDTO["typeKey"]
	curated["event_type"] = asMap(activity["eventTypeDTO"])["typeKey"]
	curated["parent_type"] = typeDTO["parentTypeId"]
	for k, v := range pick(summary,
		"start_time_local", "startTimeLocal", "start_time_gmt", "startTimeGMT", "duration_seconds", "duration",
		"moving_duration_seconds", "movingDuration", "elapsed_duration_seconds", "elapsedDuration",
		"distance_meters", "distance", "avg_speed_mps", "averageSpeed", "max_speed_mps", "maxSpeed",
		"avg_hr_bpm", "averageHR", "max_hr_bpm", "maxHR", "min_hr_bpm", "minHR", "calories", "calories", "bmr_calories", "bmrCalories",
		"avg_cadence", "averageRunCadence", "max_cadence", "maxRunCadence", "avg_stride_length_cm", "strideLength",
		"avg_ground_contact_time_ms", "groundContactTime", "avg_vertical_oscillation_cm", "verticalOscillation", "steps", "steps",
		"avg_power_watts", "averagePower", "max_power_watts", "maxPower", "normalized_power_watts", "normalizedPower",
		"training_effect", "trainingEffect", "anaerobic_training_effect", "anaerobicTrainingEffect",
		"training_effect_label", "trainingEffectLabel", "training_load", "activityTrainingLoad",
		"moderate_intensity_minutes", "moderateIntensityMinutes", "vigorous_intensity_minutes", "vigorousIntensityMinutes",
		"elevation_gain_meters", "elevationGain", "elevation_loss_meters", "elevationLoss",
		"max_elevation_meters", "maxElevation", "min_elevation_meters", "minElevation",
		"recovery_hr_bpm", "recoveryHeartRate", "body_battery_impact", "differenceBodyBattery",
		"workout_feel", "directWorkoutFeel", "workout_rpe", "directWorkoutRpe") {
		curated[k] = v
	}
	for k, v := range pick(metadata, "lap_count", "lapCount", "has_splits", "hasSplits", "device_manufacturer", "manufacturer") {
		curated[k] = v
	}
	return dropNil(curated)
}

// Garmin's splits API passes a wrapped negative lap distance through unchanged (see sanitize_lap_distance).
const (
	uint32DistanceWrapM = (1 << 32) / 100.0
	maxWrappedNegativeM = 1000.0
)

func curateSplits(splits map[string]any) map[string]any {
	laps := asList(splits["lapDTOs"])
	curatedLaps := []any{}
	for _, l := range laps {
		lap := asMap(l)
		lapData := pick(lap,
			"lap_number", "lapIndex", "start_time_gmt", "startTimeGMT", "distance_meters", "distance", "duration_seconds", "duration",
			"moving_duration_seconds", "movingDuration", "elapsed_duration_seconds", "elapsedDuration",
			"avg_speed_mps", "averageSpeed", "avg_moving_speed_mps", "averageMovingSpeed", "max_speed_mps", "maxSpeed",
			"avg_hr_bpm", "averageHR", "max_hr_bpm", "maxHR", "calories", "calories", "bmr_calories", "bmrCalories",
			"avg_cadence", "averageRunCadence", "avg_power_watts", "averagePower", "avg_swim_cadence", "averageSwimCadence",
			"active_length_count", "numberOfActiveLengths", "total_strokes", "totalNumberOfStrokes", "avg_strokes", "averageStrokes",
			"avg_swolf", "averageSWOLF", "avg_stroke_distance", "averageStrokeDistance", "intensity_type", "intensityType",
			"elevation_gain_meters", "elevationGain", "elevation_loss_meters", "elevationLoss", "workout_step_index", "wktStepIndex")
		if d, ok := asNumber(lap["distance"]); ok && d >= uint32DistanceWrapM-maxWrappedNegativeM && d <= uint32DistanceWrapM {
			lapData["distance_meters"] = nil
			lapData["distance_invalid"] = true
		}
		if lengthDTOs := asList(lap["lengthDTOs"]); len(lengthDTOs) > 0 {
			lengths := []any{}
			for _, le := range lengthDTOs {
				lengths = append(lengths, dropNil(pick(asMap(le),
					"length_number", "lengthIndex", "start_time_gmt", "startTimeGMT", "distance_meters", "distance",
					"duration_seconds", "duration", "avg_speed_mps", "averageSpeed", "max_speed_mps", "maxSpeed", "calories", "calories",
					"avg_hr_bpm", "averageHR", "max_hr_bpm", "maxHR", "total_strokes", "totalNumberOfStrokes",
					"avg_swolf", "averageSWOLF", "stroke", "swimStroke")))
			}
			lapData["lengths"] = lengths
		}
		curatedLaps = append(curatedLaps, dropNil(lapData))
	}
	return map[string]any{"activity_id": splits["activityId"], "lap_count": len(laps), "laps": curatedLaps}
}

// curateWeather converts Garmin's Fahrenheit temperatures to the account's display unit (see the tool description).
func curateWeather(ctx context.Context, s *Session, id int64, data any) any {
	weather := asMap(data)
	unit, err := s.Client.GetUnitSystem(ctx)
	if err != nil {
		unit = ""
	}
	known := unit != "" // unit_system is not None
	fToC := func(v any) any {
		f, ok := asNumber(v)
		if !ok {
			return v
		}
		return round((f-32)*5/9, 1)
	}
	temp, apparent, dew := weather["temp"], weather["apparentTemp"], weather["dewPoint"]
	var tempUnit, windUnit any
	if known && unit != "statute_us" {
		tempUnit, temp, apparent, dew = "C", fToC(temp), fToC(apparent), fToC(dew)
	} else if unit == "statute_us" {
		tempUnit = "F"
	}
	switch {
	case unit == "statute_us":
		windUnit = "mph"
	case known:
		windUnit = "km/h"
	}
	typeDTO := asMap(weather["weatherTypeDTO"])
	station := asMap(weather["weatherStationDTO"])
	return dropNil(map[string]any{
		"activity_id": id, "temperature": temp, "temperature_unit": tempUnit, "apparent_temperature": apparent, "dew_point": dew,
		"humidity_percent": weather["relativeHumidity"], "wind_speed": weather["windSpeed"], "wind_speed_unit": windUnit,
		"wind_direction_degrees": weather["windDirection"], "wind_direction_compass": weather["windDirectionCompassPoint"],
		"wind_gust": weather["windGust"], "weather_type": typeDTO["weatherTypeName"],
		"weather_description": firstTruthy(typeDTO["desc"], typeDTO["weatherTypeDesc"]),
		"location":            weather["issueLocation"], "station_id": station["id"], "station_name": station["name"], "issue_time": weather["issueDate"],
	})
}

func registerActivityWriteTools(r *registry) {
	updateActivity(r, "set_activity_name", "Set or update the name of an activity.", "Error updating activity name",
		func(in setNameIn) any { return in.ActivityID },
		func(ctx context.Context, s *Session, in setNameIn, id int64) (any, error) {
			name := strings.TrimSpace(in.ActivityName)
			if name == "" {
				return "Activity name cannot be empty", nil
			}
			if _, err := s.Client.SetActivityName(ctx, id, name); err != nil {
				return nil, err
			}
			return map[string]any{"success": true, "activity_id": id, "activity_name": name, "message": "Activity name successfully updated"}, nil
		})

	updateActivity(r, "set_activity_type", setActivityTypeDesc, "Error updating activity type",
		func(in setTypeIn) any { return in.ActivityID },
		func(ctx context.Context, s *Session, in setTypeIn, id int64) (any, error) {
			typeKey := strings.TrimSpace(in.TypeKey)
			data, err := s.Client.GetActivityTypes(ctx)
			if err != nil {
				return nil, err
			}
			types := asList(data)
			match := findByTypeKey(types, typeKey)
			if match == nil {
				valid := typeKeys(types)
				sort.Strings(valid)
				return fmt.Sprintf("Unknown activity type '%s'. Valid type keys: %s", typeKey, strings.Join(valid, ", ")), nil
			}
			typeID, err := keyError(match, "typeId")
			if err != nil {
				return nil, err
			}
			if _, err := s.Client.SetActivityType(ctx, id, typeID, typeKey, match["parentTypeId"]); err != nil {
				return nil, err
			}
			return map[string]any{"success": true, "activity_id": id, "type_key": typeKey, "type_id": typeID, "message": "Activity type successfully updated"}, nil
		})

	updateActivity(r, "set_activity_description", setActivityDescriptionDesc, "Error updating activity description",
		func(in setDescriptionIn) any { return in.ActivityID },
		func(ctx context.Context, s *Session, in setDescriptionIn, id int64) (any, error) {
			if _, err := s.Client.UpdateActivity(ctx, id, map[string]any{"description": in.Description}); err != nil {
				return nil, err
			}
			return map[string]any{"success": true, "activity_id": id, "description": in.Description, "message": "Activity description successfully updated"}, nil
		})

	updateActivity(r, "set_activity_event_type", setActivityEventTypeDesc, "Error updating activity event type",
		func(in setEventTypeIn) any { return in.ActivityID },
		func(ctx context.Context, s *Session, in setEventTypeIn, id int64) (any, error) {
			eventType := strings.TrimSpace(in.EventType)
			data, err := s.Client.GetActivityEventTypes(ctx)
			if err != nil {
				return nil, err
			}
			eventTypes := asList(data)
			match := findByTypeKey(eventTypes, eventType)
			if match == nil {
				return fmt.Sprintf("Unknown event type '%s'. Valid event types: %s", eventType, strings.Join(typeKeys(eventTypes), ", ")), nil
			}
			typeID, err := keyError(match, "typeId")
			if err != nil {
				return nil, err
			}
			dto := map[string]any{"typeId": typeID, "typeKey": match["typeKey"], "sortOrder": match["sortOrder"]}
			if _, err := s.Client.UpdateActivity(ctx, id, map[string]any{"eventTypeDTO": dto}); err != nil {
				return nil, err
			}
			return map[string]any{"success": true, "activity_id": id, "event_type": match["typeKey"], "message": "Activity event type successfully updated"}, nil
		})

	updateActivity(r, "set_perceived_effort", setPerceivedEffortDesc, "Error updating perceived effort",
		func(in setEffortIn) any { return in.ActivityID },
		func(ctx context.Context, s *Session, in setEffortIn, id int64) (any, error) {
			if !(0 <= in.RPE && in.RPE <= 10) {
				return "rpe must be between 0 and 10", nil
			}
			rpe10 := int64(math.RoundToEven(in.RPE * 10))
			if err := updateSummary(ctx, s, id, map[string]any{"directWorkoutRpe": rpe10}); err != nil {
				return nil, err
			}
			return map[string]any{"success": true, "activity_id": id, "rpe": in.RPE, "message": "Perceived effort successfully updated"}, nil
		})

	updateActivity(r, "set_activity_feel", setActivityFeelDesc, "Error updating activity feel",
		func(in setFeelIn) any { return in.ActivityID },
		func(ctx context.Context, s *Session, in setFeelIn, id int64) (any, error) {
			switch in.Feel {
			case 0, 25, 50, 75, 100:
			default:
				return "feel must be one of 0, 25, 50, 75, 100", nil
			}
			if err := updateSummary(ctx, s, id, map[string]any{"directWorkoutFeel": in.Feel}); err != nil {
				return nil, err
			}
			return map[string]any{"success": true, "activity_id": id, "feel": in.Feel, "message": "Activity feel successfully updated"}, nil
		})

	updateActivity(r, "set_activity_exercise_sets", setActivityExerciseSetsDesc, "Error updating activity exercise sets",
		func(in setExerciseSetsIn) any { return in.ActivityID }, setActivityExerciseSets)

	addTool(r, "create_manual_activity", createManualActivityDesc, createManualActivity)
	addTool(r, "delete_activity", deleteActivityDesc, deleteActivity)
}

// updateSummary sends a partial summaryDTO: Garmin merges it into the stored summary. A read-modify-write is avoided
// on purpose: PUTting both halves of the GPS start coordinate together is rejected with a 400.
func updateSummary(ctx context.Context, s *Session, id int64, fields map[string]any) error {
	_, err := s.Client.UpdateActivity(ctx, id, map[string]any{"summaryDTO": fields})
	return err
}

// ── exercise sets ──────────────────────────────────────────────────────────

// pyFloat is float(x) for a decoded JSON value.
func pyFloat(v any) (float64, error) {
	switch x := v.(type) {
	case float64:
		return x, nil
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return 0, fmt.Errorf("could not convert string to float: %s", pyRepr(x))
		}
		return f, nil
	}
	return 0, fmt.Errorf("float() argument must be a string or a real number, not '%T'", v)
}

// buildExerciseSets converts friendly set dicts into Garmin's exerciseSets wire format (_build_exercise_sets).
func buildExerciseSets(sets []map[string]any) ([]any, error) {
	out := []any{}
	for i, s := range sets {
		n := i + 1
		category := ""
		if c := s["category"]; !falsy(c) {
			category = strings.ToUpper(strings.TrimSpace(pyStr(c)))
		}
		if category == "" {
			return nil, fmt.Errorf("set %d: category is required", n)
		}
		var name any
		if e := s["exercise_name"]; !falsy(e) {
			if up := strings.ToUpper(strings.TrimSpace(pyStr(e))); up != "" && up != category {
				name = up
			}
		}

		if s["weight_kg"] != nil && s["weight_lb"] != nil {
			return nil, fmt.Errorf("set %d: give weight_kg or weight_lb, not both", n)
		}
		var kg *float64
		switch {
		case s["weight_kg"] != nil:
			f, err := pyFloat(s["weight_kg"])
			if err != nil {
				return nil, err
			}
			kg = &f
		case s["weight_lb"] != nil:
			f, err := pyFloat(s["weight_lb"])
			if err != nil {
				return nil, err
			}
			f *= 0.45359237
			kg = &f
		}
		if kg != nil && *kg < 0 {
			return nil, fmt.Errorf("set %d: weight cannot be negative", n)
		}

		var reps any
		if r := s["reps"]; r != nil {
			f, ok := asNumber(r)
			if !ok || f != math.Trunc(f) || f < 0 {
				return nil, fmt.Errorf("set %d: reps must be a non-negative whole number", n)
			}
			reps = int64(f)
		}

		weight := int64(-1)
		if kg != nil {
			weight = int64(math.RoundToEven(*kg * 1000))
		}
		out = append(out, map[string]any{
			"setType":         "ACTIVE",
			"exercises":       []any{map[string]any{"category": category, "name": name, "probability": 100}},
			"repetitionCount": reps,
			"weight":          weight,
			"duration":        s["duration_seconds"],
		})
		if !falsy(s["rest_seconds"]) {
			out = append(out, map[string]any{
				"setType": "REST", "exercises": []any{}, "repetitionCount": nil, "weight": -1, "duration": s["rest_seconds"],
			})
		}
	}
	return out, nil
}

func setActivityExerciseSets(ctx context.Context, s *Session, in setExerciseSetsIn, id int64) (any, error) {
	if len(in.Sets) == 0 {
		return "sets cannot be empty", nil
	}
	exerciseSets, err := buildExerciseSets(in.Sets)
	if err != nil {
		return nil, err
	}

	var payload any = exerciseSets
	if in.KeepTiming {
		data, err := s.Client.GetActivityExerciseSets(ctx, id)
		if err != nil {
			return nil, err
		}
		existing := asList(asMap(data)["exerciseSets"])
		recorded := 0
		for _, e := range existing {
			if asMap(e)["setType"] == "ACTIVE" {
				recorded++
			}
		}
		if recorded != len(in.Sets) {
			return fmt.Sprintf("keep_timing needs one set per recorded set: activity %d has %d active sets, got %d", id, recorded, len(in.Sets)), nil
		}
		var newActive []map[string]any
		for _, e := range exerciseSets {
			if m := asMap(e); m["setType"] == "ACTIVE" {
				newActive = append(newActive, m)
			}
		}
		next := 0
		for _, e := range existing {
			if m := asMap(e); m["setType"] == "ACTIVE" {
				repl := newActive[next]
				next++
				m["exercises"], m["repetitionCount"], m["weight"] = repl["exercises"], repl["repetitionCount"], repl["weight"]
			}
		}
		payload = existing
	}

	if _, err := s.Client.SetActivityExerciseSets(ctx, id, payload); err != nil {
		return nil, err
	}
	return map[string]any{
		"success": true, "activity_id": id, "sets_written": len(in.Sets), "message": "Activity exercise sets successfully replaced",
	}, nil
}

// ── create_manual_activity / delete_activity ───────────────────────────────

var startTimeRe = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// pyTitle is str.title(): upper-case a letter that follows a non-letter, lower-case the rest.
func pyTitle(s string) string {
	prevLetter := false
	return strings.Map(func(r rune) rune {
		isLetter := unicode.IsLetter(r)
		defer func() { prevLetter = isLetter }()
		switch {
		case isLetter && prevLetter:
			return unicode.ToLower(r)
		case isLetter:
			return unicode.ToUpper(r)
		}
		return r
	}, s)
}

func createManualActivity(ctx context.Context, s *Session, in createManualIn) (any, error) {
	tr := newWriteTracker("create_manual_activity", "Error creating manual activity", s.Log)
	typeKey := strings.TrimSpace(in.TypeKey)
	if typeKey == "" {
		return nil, errors.New("Error: type_key is required")
	}
	if in.DurationMinutes <= 0 {
		return nil, errors.New("Error: duration_minutes must be greater than 0")
	}
	if in.DistanceKm < 0 {
		return nil, errors.New("Error: distance_km must be non-negative")
	}
	if _, err := parseISODate(in.Date); err != nil {
		return nil, errors.New("Error: date must be a valid date in YYYY-MM-DD format")
	}
	startTime := "09:00"
	if in.StartTime != nil {
		startTime = *in.StartTime
	}
	if !startTimeRe.MatchString(startTime) {
		return nil, errors.New("Error: start_time must be in HH:MM (24-hour) format")
	}
	timeZone := "UTC"
	if in.TimeZone != nil {
		timeZone = *in.TimeZone
	}

	name := strings.TrimSpace(in.ActivityName)
	if name == "" {
		name = pyTitle(strings.ReplaceAll(typeKey, "_", " "))
	}
	tr.Sent()
	result, err := s.Client.CreateManualActivity(ctx, in.Date+"T"+startTime+":00.000", timeZone, typeKey, in.DistanceKm, in.DurationMinutes, name)
	if err != nil {
		return nil, tr.Fail(err)
	}
	tr.Committed()
	return map[string]any{"success": true, "activity": result}, nil
}

// previewClient is the part of garmin.Client that activityPreview needs.
type previewClient interface {
	GetActivity(ctx context.Context, id int64) (any, error)
	GetActivities(ctx context.Context, start, limit int, activityType string) (any, error)
}

// activityPreview builds a compact preview of an activity before deleting it (_activity_preview). Lookup failures
// are swallowed: the preview then holds just the id.
func activityPreview(ctx context.Context, c previewClient, id int64) map[string]any {
	preview := map[string]any{"activity_id": id}

	if activity, err := c.GetActivity(ctx, id); err == nil && len(asMap(activity)) > 0 {
		a := asMap(activity)
		summary := asMap(a["summaryDTO"])
		preview["name"] = a["activityName"]
		preview["type"] = asMap(a["activityTypeDTO"])["typeKey"]
		preview["start_time"] = firstTruthy(summary["startTimeLocal"], a["startTimeLocal"])
		preview["distance_meters"] = firstTruthy(summary["distance"], a["distance"])
		preview["duration_seconds"] = firstTruthy(summary["duration"], a["duration"])
		return dropNil(preview)
	}

	items, err := c.GetActivities(ctx, 0, 50, "")
	list, ok := items.([]any)
	if err != nil || !ok {
		return preview
	}
	for _, item := range list {
		m := asMap(item)
		if m == nil || idString(m["activityId"]) != strconv.FormatInt(id, 10) {
			continue
		}
		preview["name"] = m["activityName"]
		preview["type"] = asMap(m["activityType"])["typeKey"]
		preview["start_time"] = m["startTimeLocal"]
		preview["distance_meters"] = m["distance"]
		preview["duration_seconds"] = m["duration"]
		break
	}
	return dropNil(preview)
}

func deleteActivity(ctx context.Context, s *Session, in deleteActivityIn) (any, error) {
	id, err := parseActivityID(in.ActivityID)
	if err != nil {
		return fmt.Sprintf("Invalid activity ID: %s", pyStr(in.ActivityID)), nil
	}
	preview := activityPreview(ctx, s.Client, id)
	if !in.Confirm {
		return map[string]any{
			"status":   "needs_confirmation",
			"activity": preview,
			"message":  "This permanently deletes the activity from Garmin Connect and cannot be undone. Call again with confirm=true to proceed.",
		}, nil
	}

	if err := s.Client.DeleteActivity(ctx, id); err != nil {
		return nil, fmt.Errorf("Error deleting activity: %w", err)
	}
	name := "None"
	if n, ok := preview["name"].(string); ok {
		name = pyRepr(n)
	}
	fmt.Fprintf(s.Log, "delete_activity: deleted activity %d name=%s at %s\n", id, name, time.Now().UTC().Format("2006-01-02T15:04:05.000000+00:00"))
	return map[string]any{
		"status": "deleted", "activity": preview, "message": fmt.Sprintf("Activity %d permanently deleted.", id),
	}, nil
}
