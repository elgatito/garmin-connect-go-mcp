package server

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

// Port of garmin_mcp/health_wellness.py (the recovery and energy-balance logic is in health_recovery.go and health_energy.go).

const maxSleepRangeDays = 90

type dateIn struct {
	Account
	Date string `json:"date" jsonschema:"Date in YYYY-MM-DD format"`
}

type rangeIn struct {
	Account
	StartDate string `json:"start_date" jsonschema:"Start date in YYYY-MM-DD format"`
	EndDate   string `json:"end_date" jsonschema:"End date in YYYY-MM-DD format"`
}

type bodyCompositionIn struct {
	Account
	StartDate string  `json:"start_date" jsonschema:"Date in YYYY-MM-DD format or start date if end_date provided"`
	EndDate   *string `json:"end_date,omitempty" jsonschema:"Optional end date in YYYY-MM-DD format for date range"`
}

type weeksIn struct {
	Account
	EndDate string `json:"end_date" jsonschema:"End date in YYYY-MM-DD format"`
	Weeks   *int   `json:"weeks,omitempty" jsonschema:"Number of weeks to fetch (default 4, max 52)"`
}

type recoveryIn struct {
	Account
	Date *string `json:"date,omitempty" jsonschema:"Date in YYYY-MM-DD format. Defaults to today."`
}

type dayFetch = func(c *garmin.Client, ctx context.Context, date string) (any, error)

// addDayTool registers a one-date tool: fetch, "no data" text when the payload is falsy, then optional curation.
// errPrefix is Python's "Error retrieving X" and noData its "No X found for %s" format.
func addDayTool(r *registry, name, desc, errPrefix, noData string, fetch dayFetch, curate func(data any, date string) any) {
	addTool(r, name, desc, func(ctx context.Context, s *Session, in dateIn) (any, error) {
		data, err := fetch(s.Client, ctx, in.Date)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", errPrefix, err)
		}
		if falsy(data) {
			return fmt.Sprintf(noData, in.Date), nil
		}
		if curate != nil {
			return curate(data, in.Date), nil
		}
		return data, nil
	})
}

type rangeFetch = func(c *garmin.Client, ctx context.Context, start, end string) (any, error)

// addRangeTool is addDayTool for a start/end pair; noData takes both dates.
func addRangeTool(r *registry, name, desc, errPrefix, noData string, fetch rangeFetch, curate func(data any) any) {
	addTool(r, name, desc, func(ctx context.Context, s *Session, in rangeIn) (any, error) {
		data, err := fetch(s.Client, ctx, in.StartDate, in.EndDate)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", errPrefix, err)
		}
		if falsy(data) {
			return fmt.Sprintf(noData, in.StartDate, in.EndDate), nil
		}
		if curate != nil {
			return curate(data), nil
		}
		return data, nil
	})
}

// addWeeklyTool registers a "N weeks ending end_date" tool; each week is curated by week and the result is
// sorted most recent first. label names the data in messages ("steps" -> "Error retrieving weekly steps data").
func addWeeklyTool(r *registry, name, desc, label string,
	fetch func(c *garmin.Client, ctx context.Context, end string, weeks int) (any, error), week func(map[string]any) map[string]any) {
	addTool(r, name, desc, func(ctx context.Context, s *Session, in weeksIn) (any, error) {
		weeks := 4
		if in.Weeks != nil {
			weeks = *in.Weeks
		}
		weeks = min(weeks, 52)
		data, err := fetch(s.Client, ctx, in.EndDate, weeks)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving weekly %s data: %w", label, err)
		}
		if falsy(data) {
			return fmt.Sprintf("No weekly %s data found for %d weeks ending %s", label, weeks, in.EndDate), nil
		}
		curated := []map[string]any{}
		for _, w := range asList(data) {
			curated = append(curated, week(asMap(w)))
		}
		startOf := func(m map[string]any) string { return pyStr(firstTruthy(m["week_start"], "")) }
		sort.SliceStable(curated, func(i, j int) bool { return startOf(curated[i]) > startOf(curated[j]) })
		return map[string]any{
			"end_date": in.EndDate, "weeks_requested": weeks, "weeks_returned": len(curated), "weekly_data": curated,
		}, nil
	})
}

func registerHealthTools(r *registry) {
	registerHealthSummaryTools(r)
	registerHealthSeriesTools(r)
	registerHealthRangeTools(r)

	addTool(r, "get_energy_balance", energyBalanceSummary, func(ctx context.Context, s *Session, in rangeIn) (any, error) {
		return getEnergyBalance(ctx, s, in.StartDate, in.EndDate)
	})
	addTool(r, "get_recovery_time_remaining", recoveryDescription, func(ctx context.Context, s *Session, in recoveryIn) (any, error) {
		date := ""
		if in.Date != nil {
			date = strings.TrimSpace(*in.Date)
		}
		if date == "" {
			date = time.Now().Format(time.DateOnly)
		}
		if _, err := time.Parse(time.DateOnly, date); err != nil {
			return fmt.Sprintf("Invalid date '%s'. Use YYYY-MM-DD.", date), nil
		}
		curated, err := collectRecoveryTime(ctx, s.Client, date, time.Time{})
		if err != nil {
			return nil, fmt.Errorf("Error retrieving recovery time remaining: %w", err)
		}
		return curated, nil
	})
}

const recoveryDescription = `Get remaining recovery time in hours for a day.

This is the Firstbeat recovery clock shown on-device, distinct from
Training Readiness score (missing on some devices such as Forerunner 255)
and from Training Status ACWR. Training Readiness values are snapshots:
remaining_hours and state describe the snapshot at as_of (Garmin's local
timestamp when available), not a live countdown. remaining_hours_at_snapshot
retains the same value explicitly. For today only, when snapshots are
absent, the tool estimates remaining recovery by decaying recent activity
assignments and reports the estimate's as_of time. Today uses the server's
local date. Request failures are reported as errors, or source_errors when
another source supplies usable data.`

// registerHealthSummaryTools: the curated one-day summaries and plain one-day reads.
func registerHealthSummaryTools(r *registry) {
	addDayTool(r, "get_stats", `Get daily activity stats with curated essential metrics

Returns a summary of daily health and activity data including steps,
calories, heart rate, stress, body battery, and sleep metrics.

Note: bmr_calories is Garmin's bmrKilocalories field -- per Garmin's
own documentation this is RMR (BMR plus sedentary-to-light movement),
not true basal metabolic rate. For the current date, all calorie and
duration fields are a partial-day accumulator (elapsed hours so far),
not a full-day total.`, "Error retrieving stats", "No stats found for %s", (*garmin.Client).GetStats, curateStats)

	addDayTool(r, "get_user_summary", "Get user summary data (compatible with garminconnect-ha)",
		"Error retrieving user summary", "No user summary found for %s", (*garmin.Client).GetUserSummary, nil)

	addDayTool(r, "get_stats_and_body", "Get stats and body composition data",
		"Error retrieving stats and body composition data", "No stats and body composition data found for %s", (*garmin.Client).GetStatsAndBody, nil)

	addDayTool(r, "get_steps_data", `Get detailed steps data with 15-minute intervals

Note: This returns full interval data (~14KB). For a compact summary,
use get_stats() which includes total_steps.`, "Error retrieving steps data", "No steps data found for %s", (*garmin.Client).GetStepsData, nil)

	addDayTool(r, "get_training_readiness", `Get training readiness data with curated metrics

Returns training readiness score and contributing factors.`, "Error retrieving training readiness data",
		"No training readiness data found for %s", func(c *garmin.Client, ctx context.Context, date string) (any, error) {
			v, err := c.GetTrainingReadiness(ctx, date)
			snapshots := []any{}
			for _, s := range asSnapshotList(v) {
				snapshots = append(snapshots, s)
			}
			return snapshots, err
		}, func(data any, _ string) any {
			curated := []map[string]any{}
			for _, s := range asList(data) {
				curated = append(curated, curateTrainingReadiness(asMap(s)))
			}
			return curated
		})

	addDayTool(r, "get_morning_training_readiness", `Get morning training readiness score

Returns the morning training readiness assessment, which evaluates
recovery status and readiness to train based on overnight metrics.
Fields match one get_training_readiness entry (the snapshot taken
right after waking).`, "Error retrieving morning training readiness", "No morning training readiness data found for %s",
		(*garmin.Client).GetMorningTrainingReadiness, func(data any, date string) any {
			curated := curateTrainingReadiness(asMap(data))
			if _, ok := curated["date"]; !ok {
				curated["date"] = date
			}
			return curated
		})

	addDayTool(r, "get_body_battery_events", "Get body battery events data", "Error retrieving body battery events",
		"No body battery events found for %s", (*garmin.Client).GetBodyBatteryEvents, nil)
	addDayTool(r, "get_floors", "Get floors climbed data", "Error retrieving floors data", "No floors data found for %s",
		(*garmin.Client).GetFloors, nil)
	addDayTool(r, "get_rhr_day", "Get resting heart rate data", "Error retrieving resting heart rate data",
		"No resting heart rate data found for %s", (*garmin.Client).GetRHRDay, nil)
	addDayTool(r, "get_hydration_data", "Get hydration data", "Error retrieving hydration data", "No hydration data found for %s",
		(*garmin.Client).GetHydrationData, nil)
	addDayTool(r, "get_all_day_stress", "Get all-day stress data", "Error retrieving all-day stress data",
		"No all-day stress data found for %s", (*garmin.Client).GetAllDayStress, nil)
	addDayTool(r, "get_all_day_events", "Get daily wellness events data", "Error retrieving daily wellness events",
		"No daily wellness events found for %s", (*garmin.Client).GetAllDayEvents, nil)
	addDayTool(r, "get_lifestyle_logging_data", `Get lifestyle logging data for a specific date

Returns lifestyle logging data which allows users to track behaviors
and their impact on health metrics.`, "Error retrieving lifestyle logging data", "No lifestyle logging data found for %s",
		(*garmin.Client).GetLifestyleLoggingData, nil)

	addDayTool(r, "get_spo2_data", "Get SpO2 (blood oxygen) data", "Error retrieving SpO2 data", "No SpO2 data found for %s",
		(*garmin.Client).GetSpO2Data, func(data any, _ string) any {
			d := asMap(data)
			summary := pick(d,
				"date", "calendarDate", "avg_spo2_percent", "averageSpO2", "lowest_spo2_percent", "lowestSpO2",
				"latest_spo2_percent", "latestSpO2", "latest_reading_time", "latestSpO2TimestampLocal",
				"last_7_days_avg_spo2", "lastSevenDaysAvgSpO2", "avg_sleep_spo2_percent", "avgSleepSpO2")
			if hourly := d["spO2HourlyAverages"]; !falsy(hourly) {
				summary["hourly_averages"] = hourly
			}
			return dropNil(summary)
		})
}

// seriesValues returns the positive values at index 1 of [timestamp, value] pairs (gaps are null or negative).
func seriesValues(series any) []float64 {
	var out []float64
	for _, p := range asList(series) {
		if pair := asList(p); len(pair) > 1 {
			if v, ok := asNumber(pair[1]); ok && v > 0 {
				out = append(out, v)
			}
		}
	}
	return out
}

// registerHealthSeriesTools: heart rate, sleep, stress and respiration (full series and lightweight summaries).
func registerHealthSeriesTools(r *registry) {
	addDayTool(r, "get_heart_rates", `Get full heart rate time-series data

Note: This returns detailed 2-minute interval data (~25KB).
For a compact summary, use get_heart_rates_summary().`, "Error retrieving heart rate data", "No heart rate data found for %s",
		(*garmin.Client).GetHeartRates, nil)

	addDayTool(r, "get_heart_rates_summary", `Get heart rate summary with essential metrics (lightweight version)

Returns a compact summary (~500 bytes) instead of full time-series data (~25KB).
Ideal for daily health checkups and LLM integrations.`, "Error retrieving heart rate summary", "No heart rate data found for %s",
		(*garmin.Client).GetHeartRates, func(data any, _ string) any {
			d := asMap(data)
			summary := pick(d, "date", "calendarDate", "max_heart_rate_bpm", "maxHeartRate", "min_heart_rate_bpm", "minHeartRate",
				"resting_heart_rate_bpm", "restingHeartRate", "last_7_days_avg_resting_hr", "lastSevenDaysAvgRestingHeartRate")
			if values := seriesValues(d["heartRateValues"]); len(values) > 0 {
				summary["avg_heart_rate_bpm"] = round(sum(values)/float64(len(values)), 1)
				summary["data_points_count"] = len(values)
			}
			return dropNil(summary)
		})

	addDayTool(r, "get_sleep_data", `Get full sleep data with all details

Note: This returns detailed sleep data (~50KB).
For a compact summary, use get_sleep_summary().`, "Error retrieving sleep data", "No sleep data found for %s",
		(*garmin.Client).GetSleepData, nil)

	addDayTool(r, "get_sleep_summary", `Get sleep summary with only essential metrics (lightweight version)

This endpoint returns a compact summary of sleep data (~350 bytes) instead of
the full granular data (~50KB). Ideal for daily health checkups and LLM integrations
where the full time-series data would overwhelm the context window.`, "Error retrieving sleep summary",
		"No sleep summary found for %s", (*garmin.Client).GetSleepData, func(data any, _ string) any {
			return extractSleepSummary(asMap(data))
		})

	addTool(r, "get_sleep_summary_range", `Get lightweight sleep summaries for every night in a date range.

Returns the same curated metrics as get_sleep_summary (sleep score, duration,
sleep stages, HRV, resting HR, etc.) for each night between start_date and
end_date, inclusive. Use this instead of calling get_sleep_summary once per
night when analyzing sleep trends over weeks or months.

Note: Garmin Connect does not expose a native range endpoint for sleep, so
this makes one request per night internally. Recommended range: up to a
few weeks for quick checks. Maximum: 90 nights per call.
Empty nights are skipped; a failed request stops the range and returns
an error identifying the failed date.`, sleepSummaryRange)

	addDayTool(r, "get_stress_data", `Get full stress time-series data

Note: This returns detailed interval data (~35KB) including body battery.
For a compact summary, use get_stress_summary().`, "Error retrieving stress data", "No stress data found for %s",
		(*garmin.Client).GetStressData, nil)

	addDayTool(r, "get_stress_summary", `Get stress summary with essential metrics (lightweight version)

Returns a compact summary (~400 bytes) instead of full time-series data (~35KB).
Ideal for daily health checkups and LLM integrations.`, "Error retrieving stress summary", "No stress data found for %s",
		(*garmin.Client).GetStressData, func(data any, _ string) any {
			d := asMap(data)
			summary := pick(d, "date", "calendarDate", "max_stress_level", "maxStressLevel", "avg_stress_level", "avgStressLevel")
			if series := asList(d["stressValuesArray"]); len(series) > 0 {
				// Readings of -1 and -2 are gaps/activity and are excluded.
				var rest, low, medium, high int
				values := seriesValues(series)
				for _, v := range values {
					switch {
					case v < 26:
						rest++
					case v < 51:
						low++
					case v < 76:
						medium++
					default:
						high++
					}
				}
				total := float64(max(len(values), 1))
				summary["rest_percent"] = round(float64(rest)/total*100, 1)
				summary["low_stress_percent"] = round(float64(low)/total*100, 1)
				summary["medium_stress_percent"] = round(float64(medium)/total*100, 1)
				summary["high_stress_percent"] = round(float64(high)/total*100, 1)
				summary["data_points_count"] = len(values)
			}
			return dropNil(summary)
		})

	addDayTool(r, "get_respiration_data", `Get full respiration time-series data

Note: This returns detailed interval data (~20KB).
For a compact summary, use get_respiration_summary().`, "Error retrieving respiration data", "No respiration data found for %s",
		(*garmin.Client).GetRespirationData, nil)

	addDayTool(r, "get_respiration_summary", `Get respiration summary with essential metrics (lightweight version)

Returns a compact summary (~300 bytes) instead of full time-series data (~20KB).`, "Error retrieving respiration summary",
		"No respiration data found for %s", (*garmin.Client).GetRespirationData, func(data any, _ string) any {
			return dropNil(pick(asMap(data), "date", "calendarDate", "lowest_breaths_per_min", "lowestRespirationValue",
				"highest_breaths_per_min", "highestRespirationValue", "avg_waking_breaths_per_min", "avgWakingRespirationValue",
				"avg_sleep_breaths_per_min", "avgSleepRespirationValue"))
		})
}

func sum(xs []float64) (t float64) {
	for _, x := range xs {
		t += x
	}
	return
}

// extractSleepSummary curates one night's raw sleep payload (shared by the single-night and range tools).
func extractSleepSummary(sleep map[string]any) map[string]any {
	summary := map[string]any{}
	daily := asMap(sleep["dailySleepDTO"])
	if len(daily) > 0 {
		for k, v := range pick(daily,
			"sleep_seconds", "sleepTimeSeconds", "nap_seconds", "napTimeSeconds", "sleep_start", "sleepStartTimestampGMT",
			"sleep_end", "sleepEndTimestampGMT", "deep_sleep_seconds", "deepSleepSeconds", "light_sleep_seconds", "lightSleepSeconds",
			"rem_sleep_seconds", "remSleepSeconds", "awake_seconds", "awakeSleepSeconds", "awake_count", "awakeCount",
			"avg_sleep_stress", "avgSleepStress") {
			summary[k] = v
		}
		// sleepScores (or its overall block) can be an explicit null while the record exists.
		overall := asMap(asMap(daily["sleepScores"])["overall"])
		summary["sleep_score"] = overall["value"]
		summary["sleep_score_qualifier"] = overall["qualifierKey"]
	}
	// Accept top-level physiological metrics while retaining the nested shape.
	summary["restless_moments_count"] = getDefault(sleep, "restlessMomentsCount", daily["restlessMomentsCount"])
	summary["resting_heart_rate_bpm"] = getDefault(sleep, "restingHeartRate", daily["restingHeartRate"])

	if spo2 := asMap(sleep["wellnessSpO2SleepSummaryDTO"]); len(spo2) > 0 {
		summary["avg_spo2_percent"] = getDefault(spo2, "averageSPO2", spo2["averageSpo2"])
		summary["lowest_spo2_percent"] = getDefault(spo2, "lowestSPO2", spo2["lowestSpo2"])
	}
	if v, ok := sleep["avgOvernightHrv"]; ok {
		summary["avg_overnight_hrv"] = v
	}

	// Unmeasured phases (null) are skipped rather than reported as 0%.
	total, _ := asNumber(summary["sleep_seconds"])
	if total > 0 {
		for _, p := range [][2]string{{"deep_sleep_seconds", "deep_sleep_percent"}, {"light_sleep_seconds", "light_sleep_percent"}, {"rem_sleep_seconds", "rem_sleep_percent"}} {
			if secs, ok := asNumber(summary[p[0]]); ok {
				summary[p[1]] = round(secs/total*100, 1)
			}
		}
	}
	if total != 0 {
		summary["sleep_hours"] = round(total/3600, 2)
	}
	return dropNil(summary)
}

func sleepSummaryRange(ctx context.Context, s *Session, in rangeIn) (any, error) {
	start, err := parseISODate(in.StartDate)
	var end time.Time
	if err == nil {
		end, err = parseISODate(in.EndDate)
	}
	if err != nil {
		return fmt.Sprintf("Invalid date format: %s. Use YYYY-MM-DD.", err), nil
	}
	days := daysBetween(start, end) + 1
	if days > maxSleepRangeDays {
		return fmt.Sprintf("Date range too large (%d days). Maximum is %d days.", days, maxSleepRangeDays), nil
	}
	if days < 1 {
		return "end_date must be on or after start_date.", nil
	}

	nights := []map[string]any{}
	for cur := start; !cur.After(end); cur = cur.AddDate(0, 0, 1) {
		date := cur.Format(time.DateOnly)
		data, err := s.Client.GetSleepData(ctx, date)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving sleep data for %s: %w", date, err)
		}
		if falsy(data) {
			continue
		}
		if entry := extractSleepSummary(asMap(data)); len(entry) > 0 { // a night with only a date carries no data
			entry["date"] = date
			nights = append(nights, entry)
		}
	}
	if len(nights) == 0 {
		return fmt.Sprintf("No sleep data found between %s and %s.", in.StartDate, in.EndDate), nil
	}
	return map[string]any{
		"start_date": in.StartDate, "end_date": in.EndDate, "nights_requested": days, "nights_returned": len(nights), "nights": nights,
	}, nil
}

var statsFields = []string{
	"date", "calendarDate",
	"total_steps", "totalSteps", "daily_step_goal", "dailyStepGoal", "distance_meters", "totalDistanceMeters",
	"total_calories", "totalKilocalories", "active_calories", "activeKilocalories", "bmr_calories", "bmrKilocalories",
	"highly_active_seconds", "highlyActiveSeconds", "active_seconds", "activeSeconds", "sedentary_seconds", "sedentarySeconds",
	"sleeping_seconds", "sleepingSeconds",
	"moderate_intensity_minutes", "moderateIntensityMinutes", "vigorous_intensity_minutes", "vigorousIntensityMinutes",
	"intensity_minutes_goal", "intensityMinutesGoal",
	"min_heart_rate_bpm", "minHeartRate", "max_heart_rate_bpm", "maxHeartRate", "resting_heart_rate_bpm", "restingHeartRate",
	"last_7_days_avg_resting_hr", "lastSevenDaysAvgRestingHeartRate",
	"avg_stress_level", "averageStressLevel", "max_stress_level", "maxStressLevel", "stress_qualifier", "stressQualifier",
	"body_battery_charged", "bodyBatteryChargedValue", "body_battery_drained", "bodyBatteryDrainedValue",
	"body_battery_highest", "bodyBatteryHighestValue", "body_battery_lowest", "bodyBatteryLowestValue",
	"body_battery_current", "bodyBatteryMostRecentValue",
	"avg_spo2_percent", "averageSpo2", "lowest_spo2_percent", "lowestSpo2",
	"avg_waking_respiration", "avgWakingRespirationValue", "highest_respiration", "highestRespirationValue",
	"lowest_respiration", "lowestRespirationValue",
}

func curateStats(data any, _ string) any {
	stats := asMap(data)
	summary := pick(stats, statsFields...)
	// Floors keep a measured zero (only a missing measurement is dropped) and are rounded to 1 decimal.
	for _, f := range [][2]string{{"floors_ascended", "floorsAscended"}, {"floors_descended", "floorsDescended"}} {
		if v, ok := asNumber(stats[f[1]]); ok {
			summary[f[0]] = round(v, 1)
		}
	}
	return dropNil(summary)
}

func registerHealthRangeTools(r *registry) {
	addTool(r, "get_stats_range", `Get lightweight per-day calorie and step totals for a date range.

Returns total/active/resting calories and steps for each day between
start_date and end_date, inclusive, instead of the full get_stats
payload per day. Use this for multi-day analysis (e.g. comparing
measured intake/expenditure against Garmin's calorie model) instead
of calling get_stats once per day.

resting_calories is Garmin's bmrKilocalories field -- per Garmin's
own documentation this is RMR (BMR plus sedentary-to-light movement),
not true basal metabolic rate.

is_partial is true for the current date, whose accumulator only
covers elapsed hours so far -- treating it as a full day will
overstate a mean. Days with no device data (e.g. before the account
existed, or future dates within the range) return has_data: false
and null values rather than zeros, so a missing day never silently
enters an average as zero.

Maximum range: 28 days per call (Garmin's own limit for this endpoint).`, getStatsRange)

	addTool(r, "get_body_composition", "Get body composition data for a single date or date range",
		func(ctx context.Context, s *Session, in bodyCompositionIn) (any, error) {
			end, noData := "", fmt.Sprintf("No body composition data found for %s", in.StartDate)
			if in.EndDate != nil && *in.EndDate != "" {
				end = *in.EndDate
				noData = fmt.Sprintf("No body composition data found between %s and %s", in.StartDate, end)
			}
			data, err := s.Client.GetBodyComposition(ctx, in.StartDate, end)
			if err != nil {
				return nil, fmt.Errorf("Error retrieving body composition data: %w", err)
			}
			if falsy(data) {
				return noData, nil
			}
			return data, nil
		})

	addRangeTool(r, "get_daily_steps", "Get steps data for a date range", "Error retrieving daily steps data",
		"No daily steps data found between %s and %s", (*garmin.Client).GetDailySteps, nil)

	addRangeTool(r, "get_blood_pressure", "Get blood pressure data", "Error retrieving blood pressure data",
		"No blood pressure data found between %s and %s", (*garmin.Client).GetBloodPressure, nil)

	addRangeTool(r, "get_body_battery", "Get body battery data with events", "Error retrieving body battery data",
		"No body battery data found between %s and %s", (*garmin.Client).GetBodyBattery, func(data any) any {
			curated := []map[string]any{}
			for _, d := range asList(data) {
				curated = append(curated, curateBodyBatteryDay(asMap(d)))
			}
			return curated
		})

	addWeeklyTool(r, "get_weekly_steps", `Get weekly step data aggregates

Returns weekly step totals for the specified number of weeks ending at end_date.`, "steps",
		(*garmin.Client).GetWeeklySteps, func(w map[string]any) map[string]any {
			values := asMap(w["values"])
			return dropNil(map[string]any{
				"week_start": w["calendarDate"], "total_steps": values["totalSteps"], "average_steps": values["averageSteps"],
				"total_distance_meters": values["totalDistance"], "average_distance_meters": values["averageDistance"],
				"days_with_data": values["wellnessDataDaysCount"],
			})
		})

	addWeeklyTool(r, "get_weekly_stress", `Get weekly stress data aggregates

Returns weekly stress values for the specified number of weeks ending at end_date.`, "stress",
		(*garmin.Client).GetWeeklyStress, func(w map[string]any) map[string]any {
			return dropNil(map[string]any{"week_start": w["calendarDate"], "stress_value": w["value"]})
		})

	addWeeklyTool(r, "get_weekly_intensity_minutes", `Get weekly intensity minutes data aggregates

Returns weekly intensity minutes (moderate and vigorous) for the specified
number of weeks ending at end_date. total_minutes counts each vigorous
minute twice (moderate + 2 x vigorous), as Garmin does, so it is the
figure to compare against weekly_goal; moderate_minutes and
vigorous_minutes are the raw, unweighted minutes.`, "intensity minutes",
		func(c *garmin.Client, ctx context.Context, end string, weeks int) (any, error) {
			endDate, err := time.Parse(time.DateOnly, end)
			if err != nil {
				return nil, fmt.Errorf("time data '%s' does not match format '%%Y-%%m-%%d'", end)
			}
			start := endDate.AddDate(0, 0, -(weeks*7 - 1)).Format(time.DateOnly)
			return c.GetWeeklyIntensityMinutes(ctx, start, end)
		}, func(w map[string]any) map[string]any {
			moderate, _ := asNumber(w["moderateValue"]) // a missing/null value counts as 0 in the total
			vigorous, _ := asNumber(w["vigorousValue"])
			return dropNil(map[string]any{
				"week_start": w["calendarDate"], "weekly_goal": w["weeklyGoal"], "moderate_minutes": w["moderateValue"],
				"vigorous_minutes": w["vigorousValue"], "total_minutes": moderate + 2*vigorous,
			})
		})
}

func curateBodyBatteryDay(day map[string]any) map[string]any {
	events := []map[string]any{}
	for _, e := range asList(day["bodyBatteryActivityEvent"]) {
		event := asMap(e)
		curated := pick(event, "type", "eventType", "start_time", "eventStartTimeGmt", "body_battery_impact", "bodyBatteryImpact",
			"feedback", "shortFeedback")
		if ms, ok := asNumber(event["durationInMilliseconds"]); ok {
			curated["duration_minutes"] = round(ms/60000, 1)
		}
		events = append(events, curated)
	}
	entry := pick(day, "date", "date", "charged", "charged", "drained", "drained")
	entry["events"] = events
	if feedback := asMap(day["bodyBatteryDynamicFeedbackEvent"]); len(feedback) > 0 {
		entry["current_feedback"] = feedback["feedbackShortType"]
		entry["body_battery_level"] = feedback["bodyBatteryLevel"]
	}
	return entry
}

func getStatsRange(ctx context.Context, s *Session, in rangeIn) (any, error) {
	const maxDays = 28
	start, err := parseISODate(in.StartDate)
	var end time.Time
	if err == nil {
		end, err = parseISODate(in.EndDate)
	}
	if err != nil {
		return fmt.Sprintf("Invalid date format: %s. Use YYYY-MM-DD.", err), nil
	}
	requested := daysBetween(start, end) + 1
	if requested < 1 {
		return "end_date must be on or after start_date.", nil
	}
	if requested > maxDays {
		return fmt.Sprintf("Date range too large (%d days). Maximum is %d days.", requested, maxDays), nil
	}

	path := "/usersummary-service/stats/daily/" + in.StartDate + "/" + in.EndDate
	var byType [2]map[string]map[string]any
	for i, statsType := range []string{"CALORIES", "STEPS"} {
		resp, err := s.Client.GetJSON(ctx, path, map[string][]string{"statsType": {statsType}})
		if err != nil {
			return nil, fmt.Errorf("Error retrieving stats range: %w", err)
		}
		byType[i] = map[string]map[string]any{}
		for _, e := range asList(asMap(resp)["values"]) {
			entry := asMap(e)
			date, _ := entry["calendarDate"].(string)
			byType[i][date] = asMap(entry["values"])
		}
	}

	today := time.Now().Format(time.DateOnly)
	var daily []map[string]any
	hasAny := false
	for cur := start; !cur.After(end); cur = cur.AddDate(0, 0, 1) {
		date := cur.Format(time.DateOnly)
		cal, hasCal := byType[0][date]
		steps, hasSteps := byType[1][date]
		hasAny = hasAny || hasCal || hasSteps
		daily = append(daily, map[string]any{
			"date": date, "total_calories": cal["totalCalories"], "active_calories": cal["activeCalories"],
			"resting_calories": cal["restingCalories"], "steps": steps["totalSteps"],
			"has_data": hasCal || hasSteps, "is_partial": date == today,
		})
	}
	if !hasAny {
		return fmt.Sprintf("No stats found between %s and %s.", in.StartDate, in.EndDate), nil
	}
	return map[string]any{"start_date": in.StartDate, "end_date": in.EndDate, "days": daily}, nil
}
