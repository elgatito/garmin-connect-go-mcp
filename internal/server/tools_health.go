package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

const (
	dateLayout = "2006-01-02"

	getStatsDescription = `Get daily activity stats with curated essential metrics

Returns a summary of daily health and activity data including steps,
calories, heart rate, stress, body battery, and sleep metrics.

Note: bmr_calories is Garmin's bmrKilocalories field -- per Garmin's
own documentation this is RMR (BMR plus sedentary-to-light movement),
not true basal metabolic rate. For the current date, all calorie and
duration fields are a partial-day accumulator (elapsed hours so far),
not a full-day total.`
	getStatsRangeDescription = `Get lightweight per-day calorie and step totals for a date range.

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

Maximum range: 28 days per call (Garmin's own limit for this endpoint).`
	getEnergyBalanceDescription = `Estimate a composition-derived TDEE from logged intake, Garmin's
claimed expenditure, and body-composition change over a date range.

Combines three signals Garmin exposes separately -- mean logged intake
(nutrition log), mean claimed expenditure (Garmin's total_calories from
daily stats), and a body-composition trend (weight and body-fat % from
smart-scale readings) -- into composition_derived_tdee, an estimate
independent of Garmin's calorie model:
composition_derived_tdee = mean_intake + implied_deficit, where
implied_deficit comes from the fitted rate of fat and fat-free mass
change, using the standard energy-density approximations
7700 kcal/kg fat and 1800 kcal/kg fat-free mass. These are body
composition literature estimates, not Garmin-provided or measured
values -- treat the result as a rough estimate, not a lab measurement.
difference_kcal_per_day (Garmin's claim minus this estimate) has at
least two explanations -- Garmin overestimating expenditure, or
under-logged intake, which is common -- and this tool cannot
distinguish between them.

The body-composition trend is fit by least-squares regression across
every qualifying scale reading in range, not just the first and last --
an endpoint-to-endpoint difference is fully determined by two
individual measurements and inherits all of their noise (a 0.5%
body-fat wobble, well within normal scale repeatability, can swing a
two-point estimate by over a hundred kcal/day). weight_change_kg,
fat_mass_change_kg, and lean_mass_change_kg in body_composition are
all fitted values (the fitted line's value at the last reading's day
offset minus its value at the first), not raw differences between the
observed_first_*/observed_last_* fields, which are the actual
readings for reference/sanity-checking, not what the change figures
are computed from -- fitted_first_weight_kg/fitted_last_weight_kg are
also included so fitted_last_weight_kg - fitted_first_weight_kg can be
checked against weight_change_kg directly.

implied_deficit_kcal_per_day is the slope of a single regression: for
each reading, compute -(fat_kg * 7700 + lean_kg * 1800) where
lean_kg = weight_kg - fat_kg, then fit that value against day offset.
This is mathematically identical to combining separately-fit fat and
lean slopes, but its standard error -- implied_deficit_uncertainty_kcal_per_day,
returned whenever more than 2 readings are used -- is that single
fit's own residual standard error, which is well-defined and
reproducible from the raw readings alone (fat_kg and lean_kg are both
derived from the same weight/body-fat pair per reading, so treating
their separately-fit slopes' errors as independent and combining them
after the fact, instead of fitting the combined series directly,
would not be reproducible from outside without also knowing that
assumption). Treat a deficit within roughly one standard error of
zero as indistinguishable from no change.

Below a minimum span, or when most of the fit's statistical leverage
sits in one tight cluster of readings taken within about 48 hours of
each other, the fitted trend is not reliable even with 3+ readings --
a close, low-noise pair inside that cluster makes the fit look
precise while the slope is still mostly an extrapolation from that
cluster to wherever the other reading(s) fall. When this gate fails,
body_composition is still returned (it's just the fitted line, always
checkable against the observed readings) but derived is omitted and
gate_note explains which condition failed. Prefer windows of 3+ weeks
with several readings spread across them, not clustered at one end.

Short windows are also dominated by water/glycogen shifts (which
carry little caloric weight, unlike true lean tissue), which biases
the fat-free-mass term; see assumptions.note.

Exclusions applied automatically, matching get_nutrition_summary_between_dates
and get_stats_range:
- Intake: days with no logged food (item_count 0) are excluded from
  the mean, not counted as zero. Days with a low but nonzero
  item_count remain included (excluding them requires judgment this
  tool doesn't make unilaterally) but are called out in
  low_item_count_dates so low-confidence days are visible rather than
  silently blended into the mean. A window with many such days (or
  many item_count-0 days) biases composition_derived_tdee downward --
  check days_excluded_unlogged and low_item_count_dates before
  comparing two windows against each other.
- Expenditure: days with no device data, and the current (partial)
  day, are excluded from the mean.
- Body composition: only scale readings with both weight and body-fat
  percentage are used (skips weight-only manual entries).

Needs at least 2 qualifying body-composition readings to derive a
trend, and to pass the span/leverage gate above to derive a TDEE from
it. With 0 or 1 readings, returns the intake/expenditure means and
whatever body composition data exists, but omits the derived TDEE.

Maximum range: 61 days per call.`
	getUserSummaryDescription     = "Get user summary data (compatible with garminconnect-ha)"
	getBodyCompositionDescription = "Get body composition data for a single date or date range"
	getStatsAndBodyDescription    = "Get stats and body composition data"
	getStepsDataDescription       = `Get detailed steps data with 15-minute intervals

Note: This returns full interval data (~14KB). For a compact summary,
use get_stats() which includes total_steps.`
	getDailyStepsDescription        = "Get steps data for a date range"
	getTrainingReadinessDescription = `Get training readiness data with curated metrics

Returns training readiness score and contributing factors.`
	getBodyBatteryDescription       = "Get body battery data with events"
	getBodyBatteryEventsDescription = "Get body battery events data"
	getBloodPressureDescription     = "Get blood pressure data"
	getFloorsDescription            = "Get floors climbed data"
	getRHRDayDescription            = "Get resting heart rate data"
	getHeartRatesDescription        = `Get full heart rate time-series data

Note: This returns detailed 2-minute interval data (~25KB).
For a compact summary, use get_heart_rates_summary().`
	getHeartRatesSummaryDescription = `Get heart rate summary with essential metrics (lightweight version)

Returns a compact summary (~500 bytes) instead of full time-series data (~25KB).
Ideal for daily health checkups and LLM integrations.`
	getHydrationDataDescription = "Get hydration data"
	getSleepDataDescription     = `Get full sleep data with all details

Note: This returns detailed sleep data (~50KB).
For a compact summary, use get_sleep_summary().`
	getSleepSummaryDescription = `Get sleep summary with only essential metrics (lightweight version)

This endpoint returns a compact summary of sleep data (~350 bytes) instead of
the full granular data (~50KB). Ideal for daily health checkups and LLM integrations
where the full time-series data would overwhelm the context window.`
	getSleepSummaryRangeDescription = `Get lightweight sleep summaries for every night in a date range.

Returns the same curated metrics as get_sleep_summary (sleep score, duration,
sleep stages, HRV, resting HR, etc.) for each night between start_date and
end_date, inclusive. Use this instead of calling get_sleep_summary once per
night when analyzing sleep trends over weeks or months.

Note: Garmin Connect does not expose a native range endpoint for sleep, so
this makes one request per night internally. Recommended range: up to a
few weeks for quick checks. Maximum: 90 nights per call.
Empty nights are skipped; a failed request stops the range and returns
an error identifying the failed date.`
	getStressDataDescription = `Get full stress time-series data

Note: This returns detailed interval data (~35KB) including body battery.
For a compact summary, use get_stress_summary().`
	getStressSummaryDescription = `Get stress summary with essential metrics (lightweight version)

Returns a compact summary (~400 bytes) instead of full time-series data (~35KB).
Ideal for daily health checkups and LLM integrations.`
	getRespirationDataDescription = `Get full respiration time-series data

Note: This returns detailed interval data (~20KB).
For a compact summary, use get_respiration_summary().`
	getRespirationSummaryDescription = `Get respiration summary with essential metrics (lightweight version)

Returns a compact summary (~300 bytes) instead of full time-series data (~20KB).`
	getSpo2DataDescription             = "Get SpO2 (blood oxygen) data"
	getAllDayStressDescription         = "Get all-day stress data"
	getAllDayEventsDescription         = "Get daily wellness events data"
	getLifestyleLoggingDataDescription = `Get lifestyle logging data for a specific date

Returns lifestyle logging data which allows users to track behaviors
and their impact on health metrics.`
	getWeeklyStepsDescription = `Get weekly step data aggregates

Returns weekly step totals for the specified number of weeks ending at end_date.`
	getWeeklyStressDescription = `Get weekly stress data aggregates

Returns weekly stress values for the specified number of weeks ending at end_date.`
	getWeeklyIntensityMinutesDescription = `Get weekly intensity minutes data aggregates

Returns weekly intensity minutes (moderate and vigorous) for the specified
number of weeks ending at end_date. total_minutes counts each vigorous
minute twice (moderate + 2 x vigorous), as Garmin does, so it is the
figure to compare against weekly_goal; moderate_minutes and
vigorous_minutes are the raw, unweighted minutes.`
	getMorningTrainingReadinessDescription = `Get morning training readiness score

Returns the morning training readiness assessment, which evaluates
recovery status and readiness to train based on overnight metrics.
Fields match one get_training_readiness entry (the snapshot taken
right after waking).`
	getRecoveryTimeRemainingDescription = `Get remaining recovery time in hours for a day.

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
)

type healthDateInput struct {
	Account
	Date string `json:"date" jsonschema:"Date in YYYY-MM-DD format"`
}

type healthRangeInput struct {
	Account
	StartDate string `json:"start_date" jsonschema:"Start date in YYYY-MM-DD format"`
	EndDate   string `json:"end_date" jsonschema:"End date in YYYY-MM-DD format"`
}

type bodyCompositionInput struct {
	Account
	StartDate string  `json:"start_date" jsonschema:"Date in YYYY-MM-DD format or start date if end_date provided"`
	EndDate   *string `json:"end_date,omitempty" jsonschema:"Optional end date in YYYY-MM-DD format for date range"`
}

type weeklyInput struct {
	Account
	EndDate string `json:"end_date" jsonschema:"End date in YYYY-MM-DD format"`
	Weeks   *int   `json:"weeks,omitempty" jsonschema:"Number of weeks to fetch (default 4, max 52)"`
}

type recoveryTimeInput struct {
	Account
	Date string `json:"date,omitempty" jsonschema:"Date in YYYY-MM-DD format. Defaults to today."`
}

func registerHealthTools(r *registry) {
	addTool(r, "get_stats", getStatsDescription, getStats)
	addTool(r, "get_stats_range", getStatsRangeDescription, getStatsRange)
	addTool(r, "get_energy_balance", getEnergyBalanceDescription, getEnergyBalance)
	addTool(r, "get_user_summary", getUserSummaryDescription, getUserSummary)
	addTool(r, "get_body_composition", getBodyCompositionDescription, getBodyComposition)
	addTool(r, "get_stats_and_body", getStatsAndBodyDescription, getStatsAndBody)
	addTool(r, "get_steps_data", getStepsDataDescription, getStepsData)
	addTool(r, "get_daily_steps", getDailyStepsDescription, getDailySteps)
	addTool(r, "get_training_readiness", getTrainingReadinessDescription, getTrainingReadiness)
	addTool(r, "get_body_battery", getBodyBatteryDescription, getBodyBattery)
	addTool(r, "get_body_battery_events", getBodyBatteryEventsDescription, getBodyBatteryEvents)
	addTool(r, "get_blood_pressure", getBloodPressureDescription, getBloodPressure)
	addTool(r, "get_floors", getFloorsDescription, getFloors)
	addTool(r, "get_rhr_day", getRHRDayDescription, getRHRDay)
	addTool(r, "get_heart_rates", getHeartRatesDescription, getHeartRates)
	addTool(r, "get_heart_rates_summary", getHeartRatesSummaryDescription, getHeartRatesSummary)
	addTool(r, "get_hydration_data", getHydrationDataDescription, getHydrationData)
	addTool(r, "get_sleep_data", getSleepDataDescription, getSleepData)
	addTool(r, "get_sleep_summary", getSleepSummaryDescription, getSleepSummary)
	addTool(r, "get_sleep_summary_range", getSleepSummaryRangeDescription, getSleepSummaryRange)
	addTool(r, "get_stress_data", getStressDataDescription, getStressData)
	addTool(r, "get_stress_summary", getStressSummaryDescription, getStressSummary)
	addTool(r, "get_respiration_data", getRespirationDataDescription, getRespirationData)
	addTool(r, "get_respiration_summary", getRespirationSummaryDescription, getRespirationSummary)
	addTool(r, "get_spo2_data", getSpo2DataDescription, getSpo2Data)
	addTool(r, "get_all_day_stress", getAllDayStressDescription, getAllDayStress)
	addTool(r, "get_all_day_events", getAllDayEventsDescription, getAllDayEvents)
	addTool(r, "get_lifestyle_logging_data", getLifestyleLoggingDataDescription, getLifestyleLoggingData)
	addTool(r, "get_weekly_steps", getWeeklyStepsDescription, getWeeklySteps)
	addTool(r, "get_weekly_stress", getWeeklyStressDescription, getWeeklyStress)
	addTool(r, "get_weekly_intensity_minutes", getWeeklyIntensityMinutesDescription, getWeeklyIntensityMinutes)
	addTool(r, "get_morning_training_readiness", getMorningTrainingReadinessDescription, getMorningTrainingReadiness)
	addTool(r, "get_recovery_time_remaining", getRecoveryTimeRemainingDescription, getRecoveryTimeRemaining)
}

// --- helpers ---

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func numberOf(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return 0, false
		}
		return x, true
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

func roundTo(v float64, places int) float64 {
	pow := math.Pow10(places)
	return math.Round(v*pow) / pow
}

func pruneNil(m map[string]any) map[string]any {
	for k, v := range m {
		if v == nil {
			delete(m, k)
		}
	}
	return m
}

func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return x != ""
	case bool:
		return x
	case map[string]any:
		return len(x) > 0
	case []any:
		return len(x) > 0
	case float64:
		return x != 0
	case int:
		return x != 0
	}
	return true
}

func firstTruthy(a, b any) any {
	if truthy(a) {
		return a
	}
	return b
}

func snapshotList(payload any) []map[string]any {
	switch x := payload.(type) {
	case []any:
		out := make([]map[string]any, 0, len(x))
		for _, item := range x {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case map[string]any:
		if len(x) > 0 {
			return []map[string]any{x}
		}
	}
	return nil
}

// --- get_stats ---

func getStats(ctx context.Context, s *Session, in healthDateInput) (any, error) {
	stats, err := s.Client.GetStats(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving stats: %w", err)
	}
	if isEmptyValue(stats) {
		return fmt.Sprintf("No stats found for %s", in.Date), nil
	}
	m := asMap(stats)
	summary := map[string]any{
		"date": m["calendarDate"],

		"total_steps":      m["totalSteps"],
		"daily_step_goal":  m["dailyStepGoal"],
		"distance_meters":  m["totalDistanceMeters"],
		"floors_ascended":  nil,
		"floors_descended": nil,

		"total_calories":  m["totalKilocalories"],
		"active_calories": m["activeKilocalories"],
		"bmr_calories":    m["bmrKilocalories"],

		"highly_active_seconds": m["highlyActiveSeconds"],
		"active_seconds":        m["activeSeconds"],
		"sedentary_seconds":     m["sedentarySeconds"],
		"sleeping_seconds":      m["sleepingSeconds"],

		"moderate_intensity_minutes": m["moderateIntensityMinutes"],
		"vigorous_intensity_minutes": m["vigorousIntensityMinutes"],
		"intensity_minutes_goal":     m["intensityMinutesGoal"],

		"min_heart_rate_bpm":         m["minHeartRate"],
		"max_heart_rate_bpm":         m["maxHeartRate"],
		"resting_heart_rate_bpm":     m["restingHeartRate"],
		"last_7_days_avg_resting_hr": m["lastSevenDaysAvgRestingHeartRate"],

		"avg_stress_level": m["averageStressLevel"],
		"max_stress_level": m["maxStressLevel"],
		"stress_qualifier": m["stressQualifier"],

		"body_battery_charged": m["bodyBatteryChargedValue"],
		"body_battery_drained": m["bodyBatteryDrainedValue"],
		"body_battery_highest": m["bodyBatteryHighestValue"],
		"body_battery_lowest":  m["bodyBatteryLowestValue"],
		"body_battery_current": m["bodyBatteryMostRecentValue"],

		"avg_spo2_percent":    m["averageSpo2"],
		"lowest_spo2_percent": m["lowestSpo2"],

		"avg_waking_respiration": m["avgWakingRespirationValue"],
		"highest_respiration":    m["highestRespirationValue"],
		"lowest_respiration":     m["lowestRespirationValue"],
	}
	if v, ok := numberOf(m["floorsAscended"]); ok {
		summary["floors_ascended"] = roundTo(v, 1)
	}
	if v, ok := numberOf(m["floorsDescended"]); ok {
		summary["floors_descended"] = roundTo(v, 1)
	}
	return pruneNil(summary), nil
}

// --- get_stats_range ---

func getStatsRange(ctx context.Context, s *Session, in healthRangeInput) (any, error) {
	const maxDays = 28
	start, err := time.Parse(dateLayout, in.StartDate)
	if err != nil {
		return fmt.Sprintf("Invalid date format: %v. Use YYYY-MM-DD.", err), nil
	}
	end, err := time.Parse(dateLayout, in.EndDate)
	if err != nil {
		return fmt.Sprintf("Invalid date format: %v. Use YYYY-MM-DD.", err), nil
	}

	daysRequested := int(end.Sub(start).Hours()/24) + 1
	if daysRequested < 1 {
		return "end_date must be on or after start_date.", nil
	}
	if daysRequested > maxDays {
		return fmt.Sprintf("Date range too large (%d days). Maximum is %d days.", daysRequested, maxDays), nil
	}

	caloriesResp, err := s.Client.GetDailyStatsRange(ctx, in.StartDate, in.EndDate, "CALORIES")
	if err != nil {
		return nil, fmt.Errorf("Error retrieving stats range: %w", err)
	}
	stepsResp, err := s.Client.GetDailyStatsRange(ctx, in.StartDate, in.EndDate, "STEPS")
	if err != nil {
		return nil, fmt.Errorf("Error retrieving stats range: %w", err)
	}

	caloriesByDate := map[string]map[string]any{}
	for _, entry := range asList(asMap(caloriesResp)["values"]) {
		m := asMap(entry)
		if dateStr, ok := m["calendarDate"].(string); ok && dateStr != "" {
			caloriesByDate[dateStr] = asMap(m["values"])
		}
	}
	stepsByDate := map[string]map[string]any{}
	for _, entry := range asList(asMap(stepsResp)["values"]) {
		m := asMap(entry)
		if dateStr, ok := m["calendarDate"].(string); ok && dateStr != "" {
			stepsByDate[dateStr] = asMap(m["values"])
		}
	}

	today := time.Now().Format(dateLayout)
	daily := []any{}
	hasData := false
	for current := start; !current.After(end); current = current.AddDate(0, 0, 1) {
		dateStr := current.Format(dateLayout)
		cal, calOK := caloriesByDate[dateStr]
		steps, stepsOK := stepsByDate[dateStr]
		dayHasData := calOK || stepsOK
		if dayHasData {
			hasData = true
		}
		daily = append(daily, map[string]any{
			"date":             dateStr,
			"total_calories":   cal["totalCalories"],
			"active_calories":  cal["activeCalories"],
			"resting_calories": cal["restingCalories"],
			"steps":            steps["totalSteps"],
			"has_data":         dayHasData,
			"is_partial":       dateStr == today,
		})
	}

	if !hasData {
		return fmt.Sprintf("No stats found between %s and %s.", in.StartDate, in.EndDate), nil
	}
	return map[string]any{
		"start_date": in.StartDate,
		"end_date":   in.EndDate,
		"days":       daily,
	}, nil
}

// --- get_energy_balance ---

type intakeDay struct {
	calories  any
	itemCount int
}

func fetchDailyIntake(ctx context.Context, c *garmin.Client, start, end time.Time) (map[string]intakeDay, error) {
	resp, err := c.GetNutritionLogsRange(ctx, start.Format(dateLayout), end.Format(dateLayout))
	if err != nil {
		return nil, err
	}
	byDate := map[string]intakeDay{}
	for _, item := range asList(asMap(resp)["dailyNutritionSummaries"]) {
		day := asMap(item)
		dateStr, ok := day["mealDate"].(string)
		if !ok || dateStr == "" {
			continue
		}
		content := asMap(day["dailyNutritionContent"])
		itemCount := 0
		for _, meal := range asList(day["mealDetails"]) {
			itemCount += len(asList(asMap(meal)["loggedFoods"]))
		}
		byDate[dateStr] = intakeDay{calories: content["calories"], itemCount: itemCount}
	}
	return byDate, nil
}

func fetchDailyCalories(ctx context.Context, c *garmin.Client, start, end time.Time) (map[string]map[string]any, error) {
	byDate := map[string]map[string]any{}
	for chunkStart := start; !chunkStart.After(end); chunkStart = chunkStart.AddDate(0, 0, 28) {
		chunkEnd := chunkStart.AddDate(0, 0, 27)
		if chunkEnd.After(end) {
			chunkEnd = end
		}
		resp, err := c.GetDailyStatsRange(ctx, chunkStart.Format(dateLayout), chunkEnd.Format(dateLayout), "CALORIES")
		if err != nil {
			return nil, err
		}
		for _, item := range asList(asMap(resp)["values"]) {
			entry := asMap(item)
			dateStr, ok := entry["calendarDate"].(string)
			if !ok || dateStr == "" {
				continue
			}
			byDate[dateStr] = asMap(entry["values"])
		}
	}
	return byDate, nil
}

type linearFit struct {
	slope     float64
	intercept float64
	seSlope   *float64
	n         int
}

func linearFitOf(xs, ys []float64) *linearFit {
	n := len(xs)
	if n < 2 {
		return nil
	}
	var sumX, sumY float64
	for i := range xs {
		sumX += xs[i]
		sumY += ys[i]
	}
	meanX := sumX / float64(n)
	meanY := sumY / float64(n)
	var sxx, sxy float64
	for i := range xs {
		sxx += (xs[i] - meanX) * (xs[i] - meanX)
		sxy += (xs[i] - meanX) * (ys[i] - meanY)
	}
	if sxx == 0 {
		return nil
	}
	slope := sxy / sxx
	intercept := meanY - slope*meanX
	fit := &linearFit{slope: slope, intercept: intercept, n: n}
	if n > 2 {
		var sse float64
		for i := range xs {
			residual := ys[i] - (slope*xs[i] + intercept)
			sse += residual * residual
		}
		if dof := n - 2; dof > 0 {
			se := math.Sqrt(sse / float64(dof) / sxx)
			fit.seSlope = &se
		}
	}
	return fit
}

func bodyCompGateFailure(xs []float64) string {
	const minSpanDays = 21
	if len(xs) == 0 {
		return ""
	}
	minX, maxX := xs[0], xs[0]
	for _, x := range xs {
		minX = math.Min(minX, x)
		maxX = math.Max(maxX, x)
	}
	span := maxX - minX
	if span < minSpanDays {
		return fmt.Sprintf(
			"readings span only %v days; need at least %d to separate a real trend from water-weight noise",
			span, minSpanDays,
		)
	}

	var sumX float64
	for _, x := range xs {
		sumX += x
	}
	meanX := sumX / float64(len(xs))
	var sxx float64
	for _, x := range xs {
		sxx += (x - meanX) * (x - meanX)
	}
	if sxx == 0 {
		return "all readings fall on the same day"
	}

	leverages := make([]float64, len(xs))
	var total float64
	for i, x := range xs {
		leverages[i] = 1/float64(len(xs)) + (x-meanX)*(x-meanX)/sxx
		total += leverages[i]
	}

	var maxCluster float64
	for _, anchor := range xs {
		var cluster float64
		for i, x := range xs {
			if offset := x - anchor; offset >= 0 && offset <= 2 {
				cluster += leverages[i]
			}
		}
		maxCluster = math.Max(maxCluster, cluster)
	}
	if maxCluster > total/2 {
		return fmt.Sprintf(
			"a single cluster of readings within 48h of each other accounts for %.0f%% of the fit's statistical leverage -- the trend across the full window is not reliably distinguishable from noise in that cluster alone",
			math.Round(maxCluster/total*100),
		)
	}
	return ""
}

func getEnergyBalance(ctx context.Context, s *Session, in healthRangeInput) (any, error) {
	const (
		maxDays               = 61
		fatKcalPerKg          = 7700
		leanMassKcalPerKg     = 1800
		lowItemCountThreshold = 3
	)
	start, err := time.Parse(dateLayout, in.StartDate)
	if err != nil {
		return fmt.Sprintf("Invalid date format: %v. Use YYYY-MM-DD.", err), nil
	}
	end, err := time.Parse(dateLayout, in.EndDate)
	if err != nil {
		return fmt.Sprintf("Invalid date format: %v. Use YYYY-MM-DD.", err), nil
	}

	daysRequested := int(end.Sub(start).Hours()/24) + 1
	if daysRequested < 1 {
		return "end_date must be on or after start_date.", nil
	}
	if daysRequested > maxDays {
		return fmt.Sprintf("Date range too large (%d days). Maximum is %d days.", daysRequested, maxDays), nil
	}

	intakeByDate, err := fetchDailyIntake(ctx, s.Client, start, end)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving energy balance data: %w", err)
	}
	expenditureByDate, err := fetchDailyCalories(ctx, s.Client, start, end)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving energy balance data: %w", err)
	}
	bodyComp, err := s.Client.GetBodyComposition(ctx, in.StartDate, in.EndDate)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving energy balance data: %w", err)
	}

	today := time.Now().Format(dateLayout)

	var loggedCalories []float64
	lowItemCountDates := []string{}
	for dateStr, v := range intakeByDate {
		if v.itemCount > 0 && v.calories != nil {
			if c, ok := numberOf(v.calories); ok {
				loggedCalories = append(loggedCalories, c)
			}
		}
		if v.itemCount > 0 && v.itemCount < lowItemCountThreshold {
			lowItemCountDates = append(lowItemCountDates, dateStr)
		}
	}
	sort.Strings(lowItemCountDates)
	intakeDaysIncluded := len(loggedCalories)
	var meanIntake *float64
	if intakeDaysIncluded > 0 {
		var sum float64
		for _, c := range loggedCalories {
			sum += c
		}
		mean := sum / float64(intakeDaysIncluded)
		meanIntake = &mean
	}

	var expenditureValues []float64
	for dateStr, v := range expenditureByDate {
		if dateStr == today {
			continue
		}
		if total, ok := numberOf(v["totalCalories"]); ok {
			expenditureValues = append(expenditureValues, total)
		}
	}
	expenditureDaysIncluded := len(expenditureValues)
	var meanExpenditure *float64
	if expenditureDaysIncluded > 0 {
		var sum float64
		for _, v := range expenditureValues {
			sum += v
		}
		mean := sum / float64(expenditureDaysIncluded)
		meanExpenditure = &mean
	}

	type reading struct {
		date    string
		weight  float64
		bodyFat float64
	}
	var readings []reading
	for _, item := range asList(asMap(bodyComp)["dateWeightList"]) {
		entry := asMap(item)
		dateStr, _ := entry["calendarDate"].(string)
		weight, weightOK := numberOf(entry["weight"])
		bodyFat, fatOK := numberOf(entry["bodyFat"])
		if dateStr != "" && weightOK && fatOK {
			readings = append(readings, reading{date: dateStr, weight: weight, bodyFat: bodyFat})
		}
	}
	sort.Slice(readings, func(i, j int) bool { return readings[i].date < readings[j].date })

	result := map[string]any{
		"start_date":     in.StartDate,
		"end_date":       in.EndDate,
		"days_requested": daysRequested,
		"intake": map[string]any{
			"mean_calories_per_day":  nil,
			"days_included":          intakeDaysIncluded,
			"days_excluded_unlogged": daysRequested - intakeDaysIncluded,
			"low_item_count_dates":   lowItemCountDates,
		},
		"expenditure_garmin": map[string]any{
			"mean_total_calories_per_day": nil,
			"days_included":               expenditureDaysIncluded,
			"days_excluded":               daysRequested - expenditureDaysIncluded,
		},
	}
	if meanIntake != nil {
		result["intake"].(map[string]any)["mean_calories_per_day"] = roundTo(*meanIntake, 1)
	}
	if meanExpenditure != nil {
		result["expenditure_garmin"].(map[string]any)["mean_total_calories_per_day"] = roundTo(*meanExpenditure, 1)
	}

	if len(readings) >= 2 {
		firstDate := readings[0].date
		lastDate := readings[len(readings)-1].date
		dayZero, _ := time.Parse(dateLayout, firstDate)
		xs := make([]float64, len(readings))
		weights := make([]float64, len(readings))
		fats := make([]float64, len(readings))
		fatSeries := make([]float64, len(readings))
		for i, r := range readings {
			day, _ := time.Parse(dateLayout, r.date)
			xs[i] = math.Round(day.Sub(dayZero).Hours() / 24)
			weights[i] = r.weight / 1000
			fats[i] = r.bodyFat
			fatSeries[i] = weights[i] * r.bodyFat / 100
		}
		spanDays := xs[len(xs)-1] - xs[0]

		weightFit := linearFitOf(xs, weights)
		fatFit := linearFitOf(xs, fatSeries)

		bodyComposition := map[string]any{
			"readings_used":                   len(readings),
			"first_reading_date":              firstDate,
			"last_reading_date":               lastDate,
			"days_between_readings":           spanDays,
			"observed_first_weight_kg":        roundTo(weights[0], 2),
			"observed_last_weight_kg":         roundTo(weights[len(weights)-1], 2),
			"observed_first_body_fat_percent": fats[0],
			"observed_last_body_fat_percent":  fats[len(fats)-1],
		}
		result["body_composition"] = bodyComposition
		gateFailure := bodyCompGateFailure(xs)

		if weightFit != nil && fatFit != nil && spanDays >= 1 {
			fatChange := fatFit.slope * spanDays
			weightChange := weightFit.slope * spanDays
			leanChange := weightChange - fatChange
			bodyComposition["fitted_first_weight_kg"] = roundTo(weightFit.intercept+weightFit.slope*xs[0], 2)
			bodyComposition["fitted_last_weight_kg"] = roundTo(weightFit.intercept+weightFit.slope*xs[len(xs)-1], 2)
			bodyComposition["weight_change_kg"] = roundTo(weightChange, 2)
			bodyComposition["fat_mass_change_kg"] = roundTo(fatChange, 3)
			bodyComposition["lean_mass_change_kg"] = roundTo(leanChange, 3)

			if gateFailure != "" {
				result["gate_note"] = "Derived TDEE suppressed: " + gateFailure
			} else if meanIntake != nil {
				deficitSeries := make([]float64, len(readings))
				for i := range readings {
					deficitSeries[i] = -(fatSeries[i]*fatKcalPerKg + (weights[i]-fatSeries[i])*leanMassKcalPerKg)
				}
				deficitFit := linearFitOf(xs, deficitSeries)
				impliedDeficit := deficitFit.slope
				measuredTdee := *meanIntake + impliedDeficit

				derived := map[string]any{
					"implied_deficit_kcal_per_day":             roundTo(impliedDeficit, 1),
					"implied_deficit_uncertainty_kcal_per_day": nil,
					"composition_derived_tdee_kcal_per_day":    roundTo(measuredTdee, 1),
					"garmin_tdee_kcal_per_day":                 nil,
					"difference_kcal_per_day":                  nil,
				}
				if deficitFit.seSlope != nil {
					derived["implied_deficit_uncertainty_kcal_per_day"] = roundTo(*deficitFit.seSlope, 1)
				}
				if meanExpenditure != nil {
					derived["garmin_tdee_kcal_per_day"] = roundTo(*meanExpenditure, 1)
					derived["difference_kcal_per_day"] = roundTo(*meanExpenditure-measuredTdee, 1)
				}
				if deficitFit.seSlope == nil {
					derived["uncertainty_note"] = "Only 2 readings -- no residual variance to estimate uncertainty from. More readings in range would let this be quantified."
				}
				derived["note"] = "composition_derived_tdee is a model estimate, not ground truth. difference_kcal_per_day is not attributable to Garmin's model or to under-logged intake without independent validation of either. A window with a high days_excluded_unlogged count biases this estimate downward, since under-logged days pull down the intake mean it's built from."
				result["derived"] = derived
				result["assumptions"] = map[string]any{
					"fat_kcal_per_kg":       fatKcalPerKg,
					"lean_mass_kcal_per_kg": leanMassKcalPerKg,
					"note":                  "Standard energy-density approximations from body-composition literature, not Garmin-provided or measured constants. Fat-free mass change over short windows is often water/glycogen rather than structural tissue, which carries little caloric weight -- this biases lean_mass_kcal_per_kg high for short windows.",
				}
			} else {
				result["derived_note"] = "Body composition trend fit but no qualifying intake days to derive a TDEE from."
			}
		} else {
			result["derived_note"] = "Body composition readings found but a trend could not be fit (readings span zero days)."
		}
	} else if len(readings) > 0 {
		result["body_composition_note"] = fmt.Sprintf(
			"Only 1 qualifying body-composition reading (weight + body fat %%) in range, on %s. Need at least 2 to fit a trend.",
			readings[0].date,
		)
	} else {
		result["body_composition_note"] = "No body-composition readings with both weight and body fat % in range."
	}

	if intakeDaysIncluded == 0 && expenditureDaysIncluded == 0 && len(readings) == 0 {
		return fmt.Sprintf("No usable data found between %s and %s.", in.StartDate, in.EndDate), nil
	}
	return result, nil
}

// --- pass-through tools ---

func getUserSummary(ctx context.Context, s *Session, in healthDateInput) (any, error) {
	summary, err := s.Client.GetUserSummary(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving user summary: %w", err)
	}
	if isEmptyValue(summary) {
		return fmt.Sprintf("No user summary found for %s", in.Date), nil
	}
	return summary, nil
}

func getBodyComposition(ctx context.Context, s *Session, in bodyCompositionInput) (any, error) {
	endDate := ""
	if in.EndDate != nil {
		endDate = *in.EndDate
	}
	composition, err := s.Client.GetBodyComposition(ctx, in.StartDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving body composition data: %w", err)
	}
	if isEmptyValue(composition) {
		if endDate != "" {
			return fmt.Sprintf("No body composition data found between %s and %s", in.StartDate, endDate), nil
		}
		return fmt.Sprintf("No body composition data found for %s", in.StartDate), nil
	}
	return composition, nil
}

func getStatsAndBody(ctx context.Context, s *Session, in healthDateInput) (any, error) {
	data, err := s.Client.GetStatsAndBody(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving stats and body composition data: %w", err)
	}
	if isEmptyValue(data) {
		return fmt.Sprintf("No stats and body composition data found for %s", in.Date), nil
	}
	return data, nil
}

func getStepsData(ctx context.Context, s *Session, in healthDateInput) (any, error) {
	steps, err := s.Client.GetStepsData(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving steps data: %w", err)
	}
	if isEmptyValue(steps) {
		return fmt.Sprintf("No steps data found for %s", in.Date), nil
	}
	return steps, nil
}

func getDailySteps(ctx context.Context, s *Session, in healthRangeInput) (any, error) {
	steps, err := s.Client.GetDailySteps(ctx, in.StartDate, in.EndDate)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving daily steps data: %w", err)
	}
	if isEmptyValue(steps) {
		return fmt.Sprintf("No daily steps data found between %s and %s", in.StartDate, in.EndDate), nil
	}
	return steps, nil
}

func getFloors(ctx context.Context, s *Session, in healthDateInput) (any, error) {
	data, err := s.Client.GetFloors(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving floors data: %w", err)
	}
	if isEmptyValue(data) {
		return fmt.Sprintf("No floors data found for %s", in.Date), nil
	}
	return data, nil
}

func getRHRDay(ctx context.Context, s *Session, in healthDateInput) (any, error) {
	data, err := s.Client.GetRHRDay(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving resting heart rate data: %w", err)
	}
	if isEmptyValue(data) {
		return fmt.Sprintf("No resting heart rate data found for %s", in.Date), nil
	}
	return data, nil
}

func getHydrationData(ctx context.Context, s *Session, in healthDateInput) (any, error) {
	data, err := s.Client.GetHydrationData(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving hydration data: %w", err)
	}
	if isEmptyValue(data) {
		return fmt.Sprintf("No hydration data found for %s", in.Date), nil
	}
	return data, nil
}

func getBloodPressure(ctx context.Context, s *Session, in healthRangeInput) (any, error) {
	data, err := s.Client.GetBloodPressure(ctx, in.StartDate, in.EndDate)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving blood pressure data: %w", err)
	}
	if isEmptyValue(data) {
		return fmt.Sprintf("No blood pressure data found between %s and %s", in.StartDate, in.EndDate), nil
	}
	return data, nil
}

func getBodyBatteryEvents(ctx context.Context, s *Session, in healthDateInput) (any, error) {
	data, err := s.Client.GetBodyBatteryEvents(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving body battery events: %w", err)
	}
	if isEmptyValue(data) {
		return fmt.Sprintf("No body battery events found for %s", in.Date), nil
	}
	return data, nil
}

func getAllDayStress(ctx context.Context, s *Session, in healthDateInput) (any, error) {
	data, err := s.Client.GetAllDayStress(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving all-day stress data: %w", err)
	}
	if isEmptyValue(data) {
		return fmt.Sprintf("No all-day stress data found for %s", in.Date), nil
	}
	return data, nil
}

func getAllDayEvents(ctx context.Context, s *Session, in healthDateInput) (any, error) {
	data, err := s.Client.GetAllDayEvents(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving daily wellness events: %w", err)
	}
	if isEmptyValue(data) {
		return fmt.Sprintf("No daily wellness events found for %s", in.Date), nil
	}
	return data, nil
}

func getLifestyleLoggingData(ctx context.Context, s *Session, in healthDateInput) (any, error) {
	data, err := s.Client.GetLifestyleLoggingData(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving lifestyle logging data: %w", err)
	}
	if isEmptyValue(data) {
		return fmt.Sprintf("No lifestyle logging data found for %s", in.Date), nil
	}
	return data, nil
}

func getStressData(ctx context.Context, s *Session, in healthDateInput) (any, error) {
	data, err := s.Client.GetStressData(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving stress data: %w", err)
	}
	if isEmptyValue(data) {
		return fmt.Sprintf("No stress data found for %s", in.Date), nil
	}
	return data, nil
}

func getRespirationData(ctx context.Context, s *Session, in healthDateInput) (any, error) {
	data, err := s.Client.GetRespirationData(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving respiration data: %w", err)
	}
	if isEmptyValue(data) {
		return fmt.Sprintf("No respiration data found for %s", in.Date), nil
	}
	return data, nil
}

func getSleepData(ctx context.Context, s *Session, in healthDateInput) (any, error) {
	data, err := s.Client.GetSleepData(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving sleep data: %w", err)
	}
	if isEmptyValue(data) {
		return fmt.Sprintf("No sleep data found for %s", in.Date), nil
	}
	return data, nil
}

func getHeartRates(ctx context.Context, s *Session, in healthDateInput) (any, error) {
	data, err := s.Client.GetHeartRates(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving heart rate data: %w", err)
	}
	if isEmptyValue(data) {
		return fmt.Sprintf("No heart rate data found for %s", in.Date), nil
	}
	return data, nil
}

// --- curated tools ---

func getTrainingReadiness(ctx context.Context, s *Session, in healthDateInput) (any, error) {
	payload, err := s.Client.GetTrainingReadiness(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving training readiness data: %w", err)
	}
	snapshots := snapshotList(payload)
	if len(snapshots) == 0 {
		return fmt.Sprintf("No training readiness data found for %s", in.Date), nil
	}
	curated := make([]any, 0, len(snapshots))
	for _, r := range snapshots {
		curated = append(curated, curateTrainingReadiness(r))
	}
	return curated, nil
}

func curateTrainingReadiness(r map[string]any) map[string]any {
	entry := map[string]any{
		"date":      r["calendarDate"],
		"timestamp": r["timestampLocal"],
		"context":   r["inputContext"],

		"level":    r["level"],
		"score":    r["score"],
		"feedback": r["feedbackShort"],

		"sleep_score":           r["sleepScore"],
		"sleep_factor_percent":  r["sleepScoreFactorPercent"],
		"sleep_factor_feedback": r["sleepScoreFactorFeedback"],

		"recovery_time_hours":      nil,
		"recovery_factor_percent":  r["recoveryTimeFactorPercent"],
		"recovery_factor_feedback": r["recoveryTimeFactorFeedback"],

		"training_load_factor_percent": r["acwrFactorPercent"],
		"training_load_feedback":       r["acwrFactorFeedback"],
		"acute_load":                   r["acuteLoad"],

		"hrv_factor_percent":  r["hrvFactorPercent"],
		"hrv_factor_feedback": r["hrvFactorFeedback"],
		"hrv_weekly_avg":      r["hrvWeeklyAverage"],

		"stress_history_factor_percent": r["stressHistoryFactorPercent"],
		"stress_history_feedback":       r["stressHistoryFactorFeedback"],

		"sleep_history_factor_percent": r["sleepHistoryFactorPercent"],
		"sleep_history_feedback":       r["sleepHistoryFactorFeedback"],
	}
	if hours, ok := hoursFromRecoveryMinutes(r["recoveryTime"], r["recoveryTimeChangePhrase"]); ok {
		entry["recovery_time_hours"] = hours
	}
	return pruneNil(entry)
}

func getMorningTrainingReadiness(ctx context.Context, s *Session, in healthDateInput) (any, error) {
	readiness, err := s.Client.GetMorningTrainingReadiness(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving morning training readiness: %w", err)
	}
	if isEmptyValue(readiness) {
		return fmt.Sprintf("No morning training readiness data found for %s", in.Date), nil
	}
	curated := curateTrainingReadiness(asMap(readiness))
	if _, ok := curated["date"]; !ok {
		curated["date"] = in.Date
	}
	return curated, nil
}

func getBodyBattery(ctx context.Context, s *Session, in healthRangeInput) (any, error) {
	payload, err := s.Client.GetBodyBattery(ctx, in.StartDate, in.EndDate)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving body battery data: %w", err)
	}
	if isEmptyValue(payload) {
		return fmt.Sprintf("No body battery data found between %s and %s", in.StartDate, in.EndDate), nil
	}

	curated := []any{}
	for _, item := range asList(payload) {
		day := asMap(item)
		entry := map[string]any{
			"date":    day["date"],
			"charged": day["charged"],
			"drained": day["drained"],
			"events":  []any{},
		}
		events := []any{}
		for _, eventItem := range asList(day["bodyBatteryActivityEvent"]) {
			event := asMap(eventItem)
			curatedEvent := map[string]any{
				"type":                event["eventType"],
				"start_time":          event["eventStartTimeGmt"],
				"body_battery_impact": event["bodyBatteryImpact"],
				"feedback":            event["shortFeedback"],
			}
			if duration, ok := numberOf(event["durationInMilliseconds"]); ok {
				curatedEvent["duration_minutes"] = roundTo(duration/60000, 1)
			}
			events = append(events, curatedEvent)
		}
		entry["events"] = events
		if feedback := asMap(day["bodyBatteryDynamicFeedbackEvent"]); len(feedback) > 0 {
			entry["current_feedback"] = feedback["feedbackShortType"]
			entry["body_battery_level"] = feedback["bodyBatteryLevel"]
		}
		curated = append(curated, entry)
	}
	return curated, nil
}

func getHeartRatesSummary(ctx context.Context, s *Session, in healthDateInput) (any, error) {
	data, err := s.Client.GetHeartRates(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving heart rate summary: %w", err)
	}
	if isEmptyValue(data) {
		return fmt.Sprintf("No heart rate data found for %s", in.Date), nil
	}
	m := asMap(data)
	summary := map[string]any{
		"date":                       m["calendarDate"],
		"max_heart_rate_bpm":         m["maxHeartRate"],
		"min_heart_rate_bpm":         m["minHeartRate"],
		"resting_heart_rate_bpm":     m["restingHeartRate"],
		"last_7_days_avg_resting_hr": m["lastSevenDaysAvgRestingHeartRate"],
	}

	var valid []float64
	for _, pair := range asList(m["heartRateValues"]) {
		values := asList(pair)
		if len(values) < 2 {
			continue
		}
		if v, ok := numberOf(values[1]); ok && v > 0 {
			valid = append(valid, v)
		}
	}
	if len(valid) > 0 {
		var sum float64
		for _, v := range valid {
			sum += v
		}
		summary["avg_heart_rate_bpm"] = roundTo(sum/float64(len(valid)), 1)
		summary["data_points_count"] = len(valid)
	}
	return pruneNil(summary), nil
}

func getStressSummary(ctx context.Context, s *Session, in healthDateInput) (any, error) {
	data, err := s.Client.GetStressData(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving stress summary: %w", err)
	}
	if isEmptyValue(data) {
		return fmt.Sprintf("No stress data found for %s", in.Date), nil
	}
	m := asMap(data)
	summary := map[string]any{
		"date":             m["calendarDate"],
		"max_stress_level": m["maxStressLevel"],
		"avg_stress_level": m["avgStressLevel"],
	}

	var valid []float64
	stressValues := asList(m["stressValuesArray"])
	for _, pair := range stressValues {
		values := asList(pair)
		if len(values) < 2 {
			continue
		}
		if v, ok := numberOf(values[1]); ok && v > 0 {
			valid = append(valid, v)
		}
	}
	if len(stressValues) > 0 {
		var rest, low, medium, high int
		for _, v := range valid {
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
		total := 1.0
		if len(valid) > 0 {
			total = float64(len(valid))
		}
		summary["rest_percent"] = roundTo(float64(rest)/total*100, 1)
		summary["low_stress_percent"] = roundTo(float64(low)/total*100, 1)
		summary["medium_stress_percent"] = roundTo(float64(medium)/total*100, 1)
		summary["high_stress_percent"] = roundTo(float64(high)/total*100, 1)
		summary["data_points_count"] = len(valid)
	}
	return pruneNil(summary), nil
}

func getRespirationSummary(ctx context.Context, s *Session, in healthDateInput) (any, error) {
	data, err := s.Client.GetRespirationData(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving respiration summary: %w", err)
	}
	if isEmptyValue(data) {
		return fmt.Sprintf("No respiration data found for %s", in.Date), nil
	}
	m := asMap(data)
	return pruneNil(map[string]any{
		"date":                       m["calendarDate"],
		"lowest_breaths_per_min":     m["lowestRespirationValue"],
		"highest_breaths_per_min":    m["highestRespirationValue"],
		"avg_waking_breaths_per_min": m["avgWakingRespirationValue"],
		"avg_sleep_breaths_per_min":  m["avgSleepRespirationValue"],
	}), nil
}

func getSpo2Data(ctx context.Context, s *Session, in healthDateInput) (any, error) {
	data, err := s.Client.GetSpo2Data(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving SpO2 data: %w", err)
	}
	if isEmptyValue(data) {
		return fmt.Sprintf("No SpO2 data found for %s", in.Date), nil
	}
	m := asMap(data)
	summary := map[string]any{
		"date":                   m["calendarDate"],
		"avg_spo2_percent":       m["averageSpO2"],
		"lowest_spo2_percent":    m["lowestSpO2"],
		"latest_spo2_percent":    m["latestSpO2"],
		"latest_reading_time":    m["latestSpO2TimestampLocal"],
		"last_7_days_avg_spo2":   m["lastSevenDaysAvgSpO2"],
		"avg_sleep_spo2_percent": m["avgSleepSpO2"],
	}
	if hourly := m["spO2HourlyAverages"]; truthy(hourly) {
		summary["hourly_averages"] = hourly
	}
	return pruneNil(summary), nil
}

// --- sleep ---

func extractSleepSummary(sleepData map[string]any) map[string]any {
	summary := map[string]any{}

	dailySleep := asMap(sleepData["dailySleepDTO"])
	if len(dailySleep) > 0 {
		summary["sleep_seconds"] = dailySleep["sleepTimeSeconds"]
		summary["nap_seconds"] = dailySleep["napTimeSeconds"]
		summary["sleep_start"] = dailySleep["sleepStartTimestampGMT"]
		summary["sleep_end"] = dailySleep["sleepEndTimestampGMT"]

		sleepScores := asMap(dailySleep["sleepScores"])
		overallScore := asMap(sleepScores["overall"])
		summary["sleep_score"] = overallScore["value"]
		summary["sleep_score_qualifier"] = overallScore["qualifierKey"]

		summary["deep_sleep_seconds"] = dailySleep["deepSleepSeconds"]
		summary["light_sleep_seconds"] = dailySleep["lightSleepSeconds"]
		summary["rem_sleep_seconds"] = dailySleep["remSleepSeconds"]
		summary["awake_seconds"] = dailySleep["awakeSleepSeconds"]

		summary["awake_count"] = dailySleep["awakeCount"]
		summary["avg_sleep_stress"] = dailySleep["avgSleepStress"]
	}

	if v, ok := sleepData["restlessMomentsCount"]; ok {
		summary["restless_moments_count"] = v
	} else {
		summary["restless_moments_count"] = dailySleep["restlessMomentsCount"]
	}
	if v, ok := sleepData["restingHeartRate"]; ok {
		summary["resting_heart_rate_bpm"] = v
	} else {
		summary["resting_heart_rate_bpm"] = dailySleep["restingHeartRate"]
	}

	spo2Summary := asMap(sleepData["wellnessSpO2SleepSummaryDTO"])
	if len(spo2Summary) > 0 {
		summary["avg_spo2_percent"] = firstTruthy(spo2Summary["averageSPO2"], spo2Summary["averageSpo2"])
		summary["lowest_spo2_percent"] = firstTruthy(spo2Summary["lowestSPO2"], spo2Summary["lowestSpo2"])
	}

	if v, ok := sleepData["avgOvernightHrv"]; ok {
		summary["avg_overnight_hrv"] = v
	}

	if totalSleep, ok := numberOf(summary["sleep_seconds"]); ok && totalSleep > 0 {
		for _, phase := range []struct {
			secondsKey string
			percentKey string
		}{
			{"deep_sleep_seconds", "deep_sleep_percent"},
			{"light_sleep_seconds", "light_sleep_percent"},
			{"rem_sleep_seconds", "rem_sleep_percent"},
		} {
			if phaseSeconds, ok := numberOf(summary[phase.secondsKey]); ok {
				summary[phase.percentKey] = roundTo(phaseSeconds/totalSleep*100, 1)
			}
		}
	}
	if totalSleep, ok := numberOf(summary["sleep_seconds"]); ok && totalSleep != 0 {
		summary["sleep_hours"] = roundTo(totalSleep/3600, 2)
	}
	return pruneNil(summary)
}

func getSleepSummary(ctx context.Context, s *Session, in healthDateInput) (any, error) {
	data, err := s.Client.GetSleepData(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving sleep summary: %w", err)
	}
	if isEmptyValue(data) {
		return fmt.Sprintf("No sleep summary found for %s", in.Date), nil
	}
	return extractSleepSummary(asMap(data)), nil
}

func getSleepSummaryRange(ctx context.Context, s *Session, in healthRangeInput) (any, error) {
	const maxDays = 90
	start, err := time.Parse(dateLayout, in.StartDate)
	if err != nil {
		return fmt.Sprintf("Invalid date format: %v. Use YYYY-MM-DD.", err), nil
	}
	end, err := time.Parse(dateLayout, in.EndDate)
	if err != nil {
		return fmt.Sprintf("Invalid date format: %v. Use YYYY-MM-DD.", err), nil
	}

	days := int(end.Sub(start).Hours()/24) + 1
	if days > maxDays {
		return fmt.Sprintf("Date range too large (%d days). Maximum is %d days.", days, maxDays), nil
	}
	if days < 1 {
		return "end_date must be on or after start_date.", nil
	}

	nights := []any{}
	for current := start; !current.After(end); current = current.AddDate(0, 0, 1) {
		dateStr := current.Format(dateLayout)
		data, err := s.Client.GetSleepData(ctx, dateStr)
		if err != nil {
			return fmt.Sprintf("Error retrieving sleep data for %s: %v", dateStr, err), nil
		}
		if !isEmptyValue(data) {
			entry := map[string]any{"date": dateStr}
			for k, v := range extractSleepSummary(asMap(data)) {
				entry[k] = v
			}
			if len(entry) > 1 {
				nights = append(nights, entry)
			}
		}
	}

	if len(nights) == 0 {
		return fmt.Sprintf("No sleep data found between %s and %s.", in.StartDate, in.EndDate), nil
	}
	return map[string]any{
		"start_date":       in.StartDate,
		"end_date":         in.EndDate,
		"nights_requested": days,
		"nights_returned":  len(nights),
		"nights":           nights,
	}, nil
}

// --- weekly aggregates ---

func weeklyWeeks(in *int) int {
	weeks := 4
	if in != nil {
		weeks = *in
	}
	if weeks > 52 {
		weeks = 52
	}
	return weeks
}

func weekStartOf(week any) string {
	start, _ := asMap(week)["calendarDate"].(string)
	return start
}

func sortWeeksDesc(weeks []any) {
	sort.SliceStable(weeks, func(i, j int) bool {
		return weekStartOf(weeks[i]) > weekStartOf(weeks[j])
	})
}

func getWeeklySteps(ctx context.Context, s *Session, in weeklyInput) (any, error) {
	weeks := weeklyWeeks(in.Weeks)
	data, err := s.Client.GetWeeklySteps(ctx, in.EndDate, weeks)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving weekly steps data: %w", err)
	}
	if isEmptyValue(data) {
		return fmt.Sprintf("No weekly steps data found for %d weeks ending %s", weeks, in.EndDate), nil
	}
	curated := []any{}
	for _, item := range asList(data) {
		week := asMap(item)
		values := asMap(week["values"])
		entry := map[string]any{
			"week_start":              week["calendarDate"],
			"total_steps":             values["totalSteps"],
			"average_steps":           values["averageSteps"],
			"total_distance_meters":   values["totalDistance"],
			"average_distance_meters": values["averageDistance"],
			"days_with_data":          values["wellnessDataDaysCount"],
		}
		curated = append(curated, pruneNil(entry))
	}
	sortWeeksDesc(curated)
	return map[string]any{
		"end_date":        in.EndDate,
		"weeks_requested": weeks,
		"weeks_returned":  len(curated),
		"weekly_data":     curated,
	}, nil
}

func getWeeklyStress(ctx context.Context, s *Session, in weeklyInput) (any, error) {
	weeks := weeklyWeeks(in.Weeks)
	data, err := s.Client.GetWeeklyStress(ctx, in.EndDate, weeks)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving weekly stress data: %w", err)
	}
	if isEmptyValue(data) {
		return fmt.Sprintf("No weekly stress data found for %d weeks ending %s", weeks, in.EndDate), nil
	}
	curated := []any{}
	for _, item := range asList(data) {
		week := asMap(item)
		entry := map[string]any{
			"week_start":   week["calendarDate"],
			"stress_value": week["value"],
		}
		curated = append(curated, pruneNil(entry))
	}
	sortWeeksDesc(curated)
	return map[string]any{
		"end_date":        in.EndDate,
		"weeks_requested": weeks,
		"weeks_returned":  len(curated),
		"weekly_data":     curated,
	}, nil
}

func getWeeklyIntensityMinutes(ctx context.Context, s *Session, in weeklyInput) (any, error) {
	weeks := weeklyWeeks(in.Weeks)
	endDate, err := time.Parse(dateLayout, in.EndDate)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving weekly intensity minutes data: %w", err)
	}
	startDate := endDate.AddDate(0, 0, -(weeks*7 - 1)).Format(dateLayout)

	data, err := s.Client.GetWeeklyIntensityMinutes(ctx, startDate, in.EndDate)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving weekly intensity minutes data: %w", err)
	}
	if isEmptyValue(data) {
		return fmt.Sprintf("No weekly intensity minutes data found for %d weeks ending %s", weeks, in.EndDate), nil
	}
	curated := []any{}
	for _, item := range asList(data) {
		week := asMap(item)
		entry := map[string]any{
			"week_start":       week["calendarDate"],
			"weekly_goal":      week["weeklyGoal"],
			"moderate_minutes": week["moderateValue"],
			"vigorous_minutes": week["vigorousValue"],
		}
		var moderate, vigorous float64
		if v, ok := numberOf(week["moderateValue"]); ok {
			moderate = v
		}
		if v, ok := numberOf(week["vigorousValue"]); ok {
			vigorous = v
		}
		entry["total_minutes"] = moderate + 2*vigorous
		curated = append(curated, pruneNil(entry))
	}
	sortWeeksDesc(curated)
	return map[string]any{
		"end_date":        in.EndDate,
		"weeks_requested": weeks,
		"weeks_returned":  len(curated),
		"weekly_data":     curated,
	}, nil
}

// --- recovery time ---

func hoursFromRecoveryMinutes(minutes any, phrase any) (float64, bool) {
	if phrase == "REACHED_ZERO" {
		return 0.0, true
	}
	v, ok := numberOf(minutes)
	if !ok || v < 0 {
		return 0, false
	}
	return roundTo(v/60.0, 1), true
}

func recoveryState(hours *float64, phrase any) string {
	if phrase == "REACHED_ZERO" || (hours != nil && *hours == 0) {
		return "recovered"
	}
	if hours == nil {
		return "unknown"
	}
	if *hours <= 6 {
		return "nearly_recovered"
	}
	if *hours <= 24 {
		return "recovering"
	}
	return "not_recovered"
}

func snapshotKey(m map[string]any) string {
	if v := firstTruthy(m["timestampLocal"], m["timestamp"]); v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

func recoveryFromReadiness(payload any, source string) map[string]any {
	snapshots := snapshotList(payload)
	if len(snapshots) == 0 {
		return nil
	}
	latest := snapshots[0]
	bestKey := snapshotKey(latest)
	for _, snapshot := range snapshots[1:] {
		key := snapshotKey(snapshot)
		if key > bestKey {
			latest = snapshot
			bestKey = key
		}
	}

	phrase := latest["recoveryTimeChangePhrase"]
	hours, ok := hoursFromRecoveryMinutes(latest["recoveryTime"], phrase)
	if !ok {
		return nil
	}
	score := latest["score"]
	if score == nil {
		score = latest["readinessScore"]
	}
	asOf := firstTruthy(latest["timestampLocal"], latest["timestamp"])
	curated := map[string]any{
		"remaining_hours":             hours,
		"remaining_hours_at_snapshot": hours,
		"value_type":                  "snapshot",
		"as_of":                       asOf,
		"recovery_score":              score,
		"state":                       recoveryState(&hours, phrase),
		"source":                      source,
		"date":                        latest["calendarDate"],
		"timestamp":                   asOf,
		"change_phrase":               phrase,
		"level":                       firstTruthy(latest["level"], latest["readinessLevel"]),
	}
	return pruneNil(curated)
}

func activityEndUTC(activity map[string]any) (time.Time, bool) {
	begin, ok := numberOf(activity["beginTimestamp"])
	if !ok || begin <= 0 {
		return time.Time{}, false
	}
	var duration float64
	for _, key := range []string{"duration", "elapsedDuration"} {
		if v, ok := numberOf(activity[key]); ok && v != 0 {
			duration = v
			break
		}
	}
	start := time.UnixMilli(int64(begin)).UTC()
	return start.Add(time.Duration(duration * float64(time.Second))), true
}

func recoveryFromRecentActivities(ctx context.Context, c *garmin.Client, now time.Time, sourceErrors *[]map[string]any) map[string]any {
	items, err := c.GetActivities(ctx, 0, 20)
	if err != nil {
		*sourceErrors = append(*sourceErrors, map[string]any{"source": "get_activities", "error": err.Error()})
		return nil
	}
	list, ok := items.([]any)
	if !ok {
		return nil
	}

	var best map[string]any
	var bestEnd time.Time
	for _, item := range list {
		activity, ok := item.(map[string]any)
		if !ok {
			continue
		}
		recovery, ok := numberOf(activity["recoveryTime"])
		if !ok {
			continue
		}
		end, ok := activityEndUTC(activity)
		if !ok {
			continue
		}
		elapsedMin := math.Max(0, now.Sub(end).Minutes())
		remainingHours := roundTo(math.Max(0, recovery-elapsedMin)/60.0, 1)
		if best == nil || end.After(bestEnd) {
			bestEnd = end
			best = map[string]any{
				"remaining_hours": remainingHours,
				"recovery_score":  nil,
				"state":           recoveryState(&remainingHours, nil),
				"source":          "recent_activity",
				"value_type":      "estimate",
				"as_of":           now.Format("2006-01-02T15:04:05-07:00"),
				"activity_id":     activity["activityId"],
				"activity_name":   activity["activityName"],
			}
		}
	}
	if best == nil {
		return nil
	}
	return pruneNil(best)
}

func collectRecoveryTime(ctx context.Context, c *garmin.Client, dateStr string, now time.Time) (map[string]any, error) {
	sourceErrors := []map[string]any{}
	setDefaults := func(curated map[string]any) {
		if _, ok := curated["date"]; !ok {
			curated["date"] = dateStr
		}
		if len(sourceErrors) > 0 {
			curated["source_errors"] = sourceErrors
		}
	}

	readiness, err := c.GetTrainingReadiness(ctx, dateStr)
	if err != nil {
		sourceErrors = append(sourceErrors, map[string]any{"source": "get_training_readiness", "error": err.Error()})
	}
	if curated := recoveryFromReadiness(readiness, "training_readiness"); curated != nil {
		setDefaults(curated)
		return curated, nil
	}

	morning, err := c.GetMorningTrainingReadiness(ctx, dateStr)
	if err != nil {
		sourceErrors = append(sourceErrors, map[string]any{"source": "get_morning_training_readiness", "error": err.Error()})
	}
	if curated := recoveryFromReadiness(morning, "morning_training_readiness"); curated != nil {
		setDefaults(curated)
		return curated, nil
	}

	isToday := dateStr == now.In(time.Local).Format(dateLayout)
	if isToday {
		if curated := recoveryFromRecentActivities(ctx, c, now, &sourceErrors); curated != nil {
			setDefaults(curated)
			return curated, nil
		}
	}

	if len(sourceErrors) > 0 {
		parts := make([]string, 0, len(sourceErrors))
		for _, sourceError := range sourceErrors {
			parts = append(parts, fmt.Sprintf("%s: %s", sourceError["source"], sourceError["error"]))
		}
		return nil, errors.New(strings.Join(parts, "; "))
	}

	var message string
	if !isToday {
		message = "No Training Readiness recovery snapshot found for this date. The activity-based recovery estimate is only available for today."
	} else {
		message = "No recovery time remaining found for this date. Training Readiness snapshots were empty and recent activities did not include recoveryTime. Some devices (for example Forerunner 255) show recovery on-device without publishing a Connect Training Readiness feed."
	}
	return map[string]any{
		"remaining_hours": nil,
		"recovery_score":  nil,
		"state":           "unavailable",
		"date":            dateStr,
		"message":         message,
	}, nil
}

func getRecoveryTimeRemaining(ctx context.Context, s *Session, in recoveryTimeInput) (any, error) {
	dateStr := strings.TrimSpace(in.Date)
	if dateStr == "" {
		dateStr = time.Now().Format(dateLayout)
	}
	if _, err := time.Parse(dateLayout, dateStr); err != nil {
		return fmt.Sprintf("Invalid date %q. Use YYYY-MM-DD.", dateStr), nil
	}
	curated, err := collectRecoveryTime(ctx, s.Client, dateStr, time.Now())
	if err != nil {
		return nil, fmt.Errorf("Error retrieving recovery time remaining: %w", err)
	}
	return curated, nil
}
