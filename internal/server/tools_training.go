package server

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

// Port of garmin_mcp/training.py (the helpers are in training_helpers.go and vo2_trend.go).

type noArgsIn struct{ Account }

type progressIn struct {
	Account
	StartDate string `json:"start_date" jsonschema:"Start date in YYYY-MM-DD format"`
	EndDate   string `json:"end_date" jsonschema:"End date in YYYY-MM-DD format"`
	Metric    string `json:"metric" jsonschema:"Metric to get progress for (e.g., \"elevationGain\", \"duration\", \"distance\", \"movingDuration\", \"calories\")"`
}

type trainingEffectIn struct {
	Account
	ActivityID int64 `json:"activity_id" jsonschema:"ID of the activity to retrieve training effect for"`
}

type hrvDataIn struct {
	Account
	Date             string `json:"date" jsonschema:"Date in YYYY-MM-DD format"`
	ReturnTimeseries bool   `json:"return_timeseries,omitempty" jsonschema:"If True, include detailed 5-minute HRV readings (can be large)"`
}

type fitnessAgeIn struct {
	Account
	Date    string `json:"date" jsonschema:"Date in YYYY-MM-DD format"`
	Details bool   `json:"details,omitempty" jsonschema:"If True, include component breakdown (BMI, RHR, vigorous activity) with targets and improvement suggestions"`
}

type lactateIn struct {
	Account
	StartDate *string `json:"start_date,omitempty" jsonschema:"Start date in YYYY-MM-DD format (optional; give with end_date for history, omit both for latest)"`
	EndDate   *string `json:"end_date,omitempty" jsonschema:"End date in YYYY-MM-DD format (optional; give with start_date for history, omit both for latest)"`
}

type toleranceTrendIn struct {
	Account
	StartDate   string  `json:"start_date" jsonschema:"Start date in YYYY-MM-DD format"`
	EndDate     string  `json:"end_date" jsonschema:"End date in YYYY-MM-DD format"`
	Aggregation *string `json:"aggregation,omitempty" jsonschema:"\"daily\" or \"weekly\" (default \"weekly\")"`
}

// devicesOrNil is `try: devices = get_devices() except Exception: devices = None`.
func devicesOrNil(ctx context.Context, c *garmin.Client) any {
	devices, err := c.GetDevices(ctx)
	if err != nil {
		return nil
	}
	return devices
}

// textOr is the Python idiom `value or default` for a string.
func textOr(v any, def string) string { return pyStr(firstTruthy(v, def)) }

func registerTrainingTools(r *registry) {
	types := &activityTypeCache{}

	addTool(r, "get_progress_summary_between_dates", progressSummaryDesc, func(ctx context.Context, s *Session, in progressIn) (any, error) {
		data, err := s.Client.GetProgressSummaryBetweenDates(ctx, in.StartDate, in.EndDate, in.Metric)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving progress summary: %w", err)
		}
		if falsy(data) {
			return fmt.Sprintf("No progress summary found for %s between %s and %s.", in.Metric, in.StartDate, in.EndDate), nil
		}
		list := asList(data)
		if len(list) == 0 {
			return "Unexpected response format from API", nil
		}
		// The raw `date` and `countOfActivities` are dropped/replaced: Garmin's fitnessstats endpoint echoes today's
		// date regardless of the queried range, and its countOfActivities matches neither the true count nor `stats`.
		byType := map[string]any{}
		total := 0.0
		for activityType, st := range asMap(asMap(list[0])["stats"]) {
			metricData := asMap(asMap(st)[in.Metric])
			count, ok := asNumber(getDefault(metricData, "count", 0.0))
			if len(metricData) == 0 || !ok || count <= 0 {
				continue
			}
			byType[activityType] = map[string]any{
				"count": metricData["count"],
				"sum":   convertProgressMetric(in.Metric, metricData["sum"]),
				"avg":   convertProgressMetric(in.Metric, metricData["avg"]),
				"min":   convertProgressMetric(in.Metric, metricData["min"]),
				"max":   convertProgressMetric(in.Metric, metricData["max"]),
			}
			total += count
		}
		return map[string]any{
			"metric": in.Metric, "start_date": in.StartDate, "end_date": in.EndDate,
			"stats_by_activity_type": byType, "count_of_activities": total,
		}, nil
	})

	addTool(r, "get_hill_score", "Get hill score data between dates", func(ctx context.Context, s *Session, in rangeIn) (any, error) {
		hs, err := s.Client.GetHillScore(ctx, in.StartDate, &in.EndDate)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving hill score data: %w", err)
		}
		data := hs.Data
		if falsy(data) {
			return fmt.Sprintf("No hill score data found between %s and %s.", in.StartDate, in.EndDate), nil
		}
		hill := asMap(data)
		var avgScore any
		if avg := asMap(hill["periodAvgScore"]); len(avg) > 0 {
			keys := hs.AvgKeys // Garmin's key order, as Python's next(iter(values))
			if len(keys) == 0 {
				keys = garmin.SortedKeys(avg)
			}
			avgScore = avg[keys[0]]
		}
		// Select the most recent score independently of upstream ordering.
		daily := []any{}
		latest := map[string]any{}
		latestKey := ""
		for _, sc := range asList(hill["hillScoreDTOList"]) {
			score, ok := sc.(map[string]any)
			if !ok {
				continue
			}
			if k := textOr(score["calendarDate"], ""); len(daily) == 0 || k > latestKey {
				latest, latestKey = score, k
			}
			daily = append(daily, map[string]any{
				"date": score["calendarDate"], "overall": score["overallScore"],
				"strength": score["strengthScore"], "endurance": score["enduranceScore"],
			})
		}
		return dropNil(map[string]any{
			"start_date": in.StartDate, "end_date": in.EndDate,
			"period_avg_score": avgScore, "max_score": hill["maxScore"],
			"latest_date": latest["calendarDate"], "latest_overall_score": latest["overallScore"],
			"latest_strength_score": latest["strengthScore"], "latest_endurance_score": latest["enduranceScore"],
			"latest_classification_id": latest["hillScoreClassificationId"],
			"daily_scores":             daily,
		}), nil
	})

	addTool(r, "get_endurance_score", "Get endurance score data between dates", func(ctx context.Context, s *Session, in rangeIn) (any, error) {
		data, err := s.Client.GetEnduranceScore(ctx, in.StartDate, &in.EndDate)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving endurance score data: %w", err)
		}
		if falsy(data) {
			return fmt.Sprintf("No endurance score data found between %s and %s.", in.StartDate, in.EndDate), nil
		}
		return enduranceScore(data, in, types.mapping(ctx, s.Client.GetActivityTypes)), nil
	})

	addTool(r, "get_training_effect", trainingEffectDesc, func(ctx context.Context, s *Session, in trainingEffectIn) (any, error) {
		source := "activity_details"
		var detailErr error
		activity, err := s.Client.GetActivity(ctx, in.ActivityID)
		if err != nil {
			if !isForbiddenError(err) {
				return nil, fmt.Errorf("Error retrieving training effect data: %w", err)
			}
			activity, detailErr = nil, err
		}
		if falsy(activity) {
			source = "activity_list"
			found := lookupActivityInList(ctx, liveActivityLister{s.Client}, in.ActivityID, 100)
			if found == nil {
				if detailErr != nil {
					return nil, fmt.Errorf("Error retrieving training effect data: Garmin returned HTTP 403 for activity %d details, "+
						"and the activity was not found in recent activity search.", in.ActivityID)
				}
				return fmt.Sprintf("No activity found with ID %d.", in.ActivityID), nil
			}
			activity = found
		}
		return curateTrainingEffect(activity, in.ActivityID, source), nil
	})

	addTool(r, "get_hrv_data", "Get Heart Rate Variability (HRV) data", func(ctx context.Context, s *Session, in hrvDataIn) (any, error) {
		data, err := s.Client.GetHRVData(ctx, in.Date)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving HRV data: %w", err)
		}
		if falsy(data) {
			return fmt.Sprintf("No HRV data found for %s.", in.Date), nil
		}
		hrv := asMap(data)
		summary := asMap(hrv["hrvSummary"])
		baseline := asMap(summary["baseline"])
		curated := map[string]any{
			"date":                        firstTruthy(summary["calendarDate"], in.Date),
			"last_night_avg_hrv_ms":       summary["lastNightAvg"],
			"last_night_5min_high_hrv_ms": summary["lastNight5MinHigh"],
			"weekly_avg_hrv_ms":           summary["weeklyAvg"],
			"baseline_balanced_low_ms":    baseline["balancedLow"],
			"baseline_balanced_upper_ms":  baseline["balancedUpper"],
			"baseline_low_upper_ms":       baseline["lowUpper"],
			"status":                      summary["status"],
			"feedback":                    summary["feedbackPhrase"],
			"sleep_start":                 hrv["sleepStartTimestampLocal"],
			"sleep_end":                   hrv["sleepEndTimestampLocal"],
		}
		if in.ReturnTimeseries {
			readings := []any{}
			for _, rd := range asList(hrv["hrvReadings"]) {
				if reading, ok := rd.(map[string]any); ok {
					readings = append(readings, map[string]any{"time": reading["readingTimeLocal"], "hrv_ms": reading["hrvValue"]})
				}
			}
			curated["hrv_readings"] = readings
			curated["readings_count"] = len(readings)
		}
		return dropNil(curated), nil
	})

	addTool(r, "get_fitnessage_data", "Get fitness age data", func(ctx context.Context, s *Session, in fitnessAgeIn) (any, error) {
		data, err := s.Client.GetFitnessAgeData(ctx, in.Date)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving fitness age data: %w", err)
		}
		if falsy(data) {
			return fmt.Sprintf("No fitness age data found for %s.", in.Date), nil
		}
		fa := asMap(data)
		chrono, fit := fa["chronologicalAge"], fa["fitnessAge"]
		var ageDiff any
		if c, ok := asNumber(chrono); ok {
			if f, ok := asNumber(fit); ok {
				ageDiff = round(c-f, 1)
			}
		}
		roundIfTruthy := func(v any) any {
			if falsy(v) {
				return nil
			}
			return roundAny(v, 1)
		}
		curated := map[string]any{
			"date":                         in.Date,
			"fitness_age_years":            roundIfTruthy(fit),
			"chronological_age_years":      chrono,
			"age_difference_years":         ageDiff,
			"achievable_fitness_age_years": roundIfTruthy(fa["achievableFitnessAge"]),
			"previous_fitness_age_years":   roundIfTruthy(fa["previousFitnessAge"]),
			"last_updated":                 fa["lastUpdated"],
		}
		if in.Details {
			components := map[string]any{}
			for name, cd := range asMap(fa["components"]) {
				comp, ok := cd.(map[string]any)
				if !ok {
					continue
				}
				info := map[string]any{"value": comp["value"]}
				for _, kv := range [][2]string{{"target", "targetValue"}, {"improvement_needed", "improvementValue"}, {"priority", "priority"},
					{"stale", "stale"}, {"last_measurement", "lastMeasurementDate"}} {
					if comp[kv[1]] != nil {
						info[kv[0]] = comp[kv[1]]
					}
				}
				if comp["potentialAge"] != nil {
					info["potential_age_if_improved"] = roundAny(comp["potentialAge"], 1)
				}
				components[name] = info
			}
			if len(components) > 0 {
				curated["components"] = components
			}
		}
		return dropNil(curated), nil
	})

	addTool(r, "get_training_status", trainingStatusDesc, func(ctx context.Context, s *Session, in dateIn) (any, error) {
		ts, err := s.Client.GetTrainingStatus(ctx, in.Date)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving training status data: %w", err)
		}
		return trainingStatus(ctx, s.Client, ts, in.Date)
	})

	addTool(r, "get_cycling_ftp", cyclingFTPDesc, func(ctx context.Context, s *Session, _ noArgsIn) (any, error) {
		data, err := s.Client.GetCyclingFTP(ctx)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving cycling FTP data: %w", err)
		}
		if list, ok := data.([]any); ok {
			data = nil
			for _, item := range list {
				if _, ok := item.(map[string]any); ok {
					data = item
					break
				}
			}
		}
		if falsy(data) {
			return "No cycling FTP data found", nil
		}
		ftp := asMap(data)
		return dropNil(map[string]any{
			"sport": ftp["sport"], "functional_threshold_power_watts": ftp["functionalThresholdPower"],
			"calendar_date": ftp["calendarDate"], "is_stale": ftp["isStale"], "biometric_source_type": ftp["biometricSourceType"],
		}), nil
	})

	addTool(r, "get_lactate_threshold", lactateThresholdDesc, lactateThreshold)

	addTool(r, "request_reload", "Request reload of epoch data", func(ctx context.Context, s *Session, in dateIn) (any, error) {
		result, err := s.Client.RequestReload(ctx, in.Date)
		if err != nil {
			return nil, fmt.Errorf("Error requesting data reload: %w", err)
		}
		return result, nil
	})

	addTool(r, "get_training_load_trend", trainingLoadTrendDesc, trainingLoadTrend)
	addTool(r, "get_training_load_balance", trainingLoadBalanceDesc, trainingLoadBalance)

	addTool(r, "get_hrv_trend", hrvTrendDesc, func(ctx context.Context, s *Session, in rangeIn) (any, error) {
		return dailyTrend(in, 30, "HRV", func(date string) (map[string]any, error) {
			data, err := s.Client.GetHRVData(ctx, date)
			if err != nil || falsy(data) {
				return nil, err
			}
			summary := asMap(asMap(data)["hrvSummary"])
			entry := map[string]any{"date": date}
			for _, kv := range [][2]string{{"last_night_avg_hrv_ms", "lastNightAvg"}, {"weekly_avg_hrv_ms", "weeklyAvg"}, {"last_night_5min_high_hrv_ms", "lastNight5MinHigh"}} {
				if v := summary[kv[1]]; v != nil {
					entry[kv[0]] = roundAny(v, 1)
				}
			}
			if v := summary["status"]; !falsy(v) {
				entry["status"] = v
			}
			if v := summary["feedbackPhrase"]; !falsy(v) {
				entry["feedback"] = v
			}
			return entry, nil
		}, func(trend []map[string]any) map[string]any {
			return map[string]any{"period_avg_hrv_ms": meanOf(trend, "last_night_avg_hrv_ms")}
		})
	})

	addTool(r, "get_vo2max_trend", vo2maxTrendDesc, func(ctx context.Context, s *Session, in rangeIn) (any, error) {
		return vo2maxTrend(ctx, liveVO2{s.Client}, in.StartDate, in.EndDate)
	})

	addTool(r, "get_respiration_trend", respirationTrendDesc, func(ctx context.Context, s *Session, in rangeIn) (any, error) {
		return dailyTrend(in, 30, "respiration", func(date string) (map[string]any, error) {
			data, err := s.Client.GetRespirationData(ctx, date)
			if err != nil || falsy(data) {
				return nil, err
			}
			d := asMap(data)
			entry := map[string]any{"date": date}
			for _, kv := range [][2]string{{"avg_waking_breaths_per_min", "avgWakingRespirationValue"}, {"avg_sleep_breaths_per_min", "avgSleepRespirationValue"},
				{"highest_breaths_per_min", "highestRespirationValue"}, {"lowest_breaths_per_min", "lowestRespirationValue"}} {
				if v := d[kv[1]]; v != nil {
					entry[kv[0]] = roundAny(v, 1)
				}
			}
			return entry, nil
		}, func(trend []map[string]any) map[string]any {
			return map[string]any{"period_avg_sleep_breaths_per_min": meanOf(trend, "avg_sleep_breaths_per_min")}
		})
	})

	addTool(r, "get_running_tolerance", runningToleranceDesc, func(ctx context.Context, s *Session, in dateIn) (any, error) {
		data, err := s.Client.GetRunningTolerance(ctx, in.Date, in.Date, "daily")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving running tolerance data: %w", err)
		}
		list := asList(data)
		if len(list) == 0 { // an empty list or null; Python also gives this text for other falsy values
			return fmt.Sprintf("No running tolerance data found for %s.", in.Date), nil
		}
		entry := asMap(list[0])
		curated := map[string]any{"date": getDefault(entry, "calendarDate", in.Date)}
		for k, v := range toleranceFields(entry["acuteTolerance"], entry["acuteImpactLoad"], entry["acuteDistance"]) {
			curated[k] = v
		}
		if f := entry["runningToleranceFeedBackPhrase"]; !falsy(f) {
			curated["feedback_phrase"] = f
		}
		return curated, nil
	})

	addTool(r, "get_running_tolerance_trend", runningToleranceTrendDesc, runningToleranceTrend)

	addTool(r, "get_acclimation", acclimationDesc, func(ctx context.Context, s *Session, in dateIn) (any, error) {
		data, err := s.Client.GetMaxMetrics(ctx, in.Date)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving acclimation data: %w", err)
		}
		if list, ok := data.([]any); ok {
			data = nil
			if len(list) > 0 {
				data = list[0]
			}
		}
		d, ok := data.(map[string]any)
		if !ok {
			return fmt.Sprintf("No acclimation data found for %s.", in.Date), nil
		}
		acc := asMap(d["heatAltitudeAcclimation"])
		if len(acc) == 0 {
			return fmt.Sprintf("No acclimation data found for %s. Garmin populates this only after outdoor activities in heat or at altitude.", in.Date), nil
		}
		result := map[string]any{"date": getDefault(acc, "calendarDate", in.Date)}
		for _, kv := range [][2]string{
			{"heat_acclimation_percent", "heatAcclimationPercentage"}, {"previous_heat_acclimation_percent", "previousHeatAcclimationPercentage"},
			{"heat_trend", "heatTrend"}, {"heat_acclimation_date", "heatAcclimationDate"}, {"previous_heat_acclimation_date", "previousHeatAcclimationDate"},
			{"altitude_acclimation_meters", "altitudeAcclimation"}, {"previous_altitude_acclimation_meters", "previousAltitudeAcclimation"},
			{"altitude_trend", "altitudeTrend"}, {"current_altitude_meters", "currentAltitude"},
		} {
			if v := acc[kv[1]]; v != nil {
				result[kv[0]] = v
			}
		}
		if heat, ok := asNumber(result["heat_acclimation_percent"]); ok {
			if prev, ok := asNumber(result["previous_heat_acclimation_percent"]); ok {
				result["heat_acclimation_change"] = round(heat-prev, 1)
			}
		}
		return result, nil
	})
}

// toleranceFields are the km figures of a running-tolerance record (raw values are in metres).
func toleranceFields(tolerance, acuteLoad, distance any) map[string]any {
	out := map[string]any{}
	if t, ok := asNumber(tolerance); ok {
		out["tolerance_km"] = round(t/1000, 2)
	}
	load, hasLoad := asNumber(acuteLoad)
	if hasLoad {
		out["acute_load_km"] = round(load/1000, 2)
	}
	if d, ok := asNumber(distance); ok {
		out["distance_km"] = round(d/1000, 2)
		if hasLoad && d != 0 {
			out["load_ratio"] = round(load/d, 2)
		}
	}
	return out
}

// dailyTrend runs the shared loop of the HRV and respiration trends: one request per day, empty days skipped,
// the first failed request ends the tool.
func dailyTrend(in rangeIn, maxDays int, label string, day func(date string) (map[string]any, error), summary func([]map[string]any) map[string]any) (any, error) {
	start, end, msg := trendRange(in.StartDate, in.EndDate, maxDays)
	if msg != "" {
		return msg, nil
	}
	trend := []map[string]any{}
	for cur := start; !cur.After(end); cur = cur.AddDate(0, 0, 1) {
		date := cur.Format(time.DateOnly)
		entry, err := day(date)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving %s data for %s: %w", label, date, err)
		}
		if len(entry) > 1 { // more than just the date
			trend = append(trend, entry)
		}
	}
	if len(trend) == 0 {
		return fmt.Sprintf("No %s data found between %s and %s.", label, in.StartDate, in.EndDate), nil
	}
	result := map[string]any{"start_date": in.StartDate, "end_date": in.EndDate, "days_with_data": len(trend), "trend": trend}
	for k, v := range summary(trend) {
		result[k] = v
	}
	return result, nil
}

// meanOf is round(sum(values) / len(values), 1) over the entries that have key, or nil when none do.
func meanOf(trend []map[string]any, key string) any {
	sum, n := 0.0, 0
	for _, e := range trend {
		if v, ok := asNumber(e[key]); ok {
			sum += v
			n++
		}
	}
	if n == 0 {
		return nil
	}
	return round(sum/float64(n), 1)
}

func enduranceScore(data any, in rangeIn, types map[int]string) any {
	end := asMap(data)
	dto := asMap(end["enduranceScoreDTO"])
	labels := map[int]string{1: "recreational", 2: "intermediate", 3: "trained", 4: "well_trained", 5: "expert", 6: "superior", 7: "elite"}
	classificationID := dto["classification"]
	var classification any
	if classificationID != nil {
		id, _ := asNumber(classificationID)
		label, ok := labels[int(id)]
		if !ok {
			label = "level_" + idString(classificationID)
		}
		classification = label
	}

	var contributors any
	if raw := asList(dto["contributors"]); len(raw) > 0 {
		list := make([]any, len(raw))
		for i, c := range raw {
			list[i] = mapContributor(c, types)
		}
		contributors = list
	}

	var weekly any
	groupMap := asMap(end["groupMap"])
	var breakdown []any
	for _, week := range garmin.SortedKeys(groupMap) {
		wd := asMap(groupMap[week])
		var wc any
		if raw := asList(wd["enduranceContributorDTOList"]); len(raw) > 0 {
			list := make([]any, len(raw))
			for i, c := range raw {
				list[i] = mapContributor(c, types)
			}
			wc = list
		}
		breakdown = append(breakdown, map[string]any{"week_start": week, "avg_score": wd["groupAverage"], "max_score": wd["groupMax"], "contributors": wc})
	}
	if len(breakdown) > 0 {
		weekly = breakdown
	}

	var thresholds any
	if !falsy(dto["classificationLowerLimitTrained"]) {
		thresholds = map[string]any{
			"intermediate": dto["classificationLowerLimitIntermediate"], "trained": dto["classificationLowerLimitTrained"],
			"well_trained": dto["classificationLowerLimitWellTrained"], "expert": dto["classificationLowerLimitExpert"],
			"superior": dto["classificationLowerLimitSuperior"], "elite": dto["classificationLowerLimitElite"],
		}
	}
	return removeNone(map[string]any{
		"start_date": in.StartDate, "end_date": in.EndDate,
		"period_avg_score": end["avg"], "period_max_score": end["max"],
		"current_score": dto["overallScore"], "current_date": dto["calendarDate"],
		"classification": classification, "classification_id": classificationID,
		"thresholds": thresholds, "contributors": contributors, "weekly_breakdown": weekly,
	})
}

// mapContributor is _map_contributor: a contributor with its activity type spelled out.
func mapContributor(c any, types map[int]string) map[string]any {
	contributor := asMap(c)
	var contribution any
	if !falsy(contributor["contribution"]) {
		contribution = roundAny(contributor["contribution"], 2)
	}
	result := map[string]any{"contribution_percent": contribution}
	if typeID := contributor["activityTypeId"]; typeID != nil {
		id, _ := asNumber(typeID)
		name, ok := types[int(id)]
		if !ok {
			name = "unknown_" + idString(typeID)
		}
		result["activity_type"] = name
		result["activity_type_id"] = typeID
	} else if group := contributor["group"]; group != nil {
		// TODO in Python: find a proper mapping for these groups
		id, _ := asNumber(group)
		names := map[int]string{0: "running (?)", 1: "biking (?)", 8: "Other Activities"}
		name, ok := names[int(id)]
		if !ok {
			name = "group_" + idString(group)
		}
		result["group"] = name
	}
	return result
}

// trainingStatus is the body of get_training_status.
func trainingStatus(ctx context.Context, c *garmin.Client, ts *garmin.TrainingStatus, date string) (any, error) {
	status := asMap(ts.Data)
	if len(status) == 0 {
		return fmt.Sprintf("No training status data found for %s.", date), nil
	}
	latest := asMap(asMap(status["mostRecentTrainingStatus"])["latestTrainingStatusData"])
	devices := devicesOrNil(ctx, c)
	sel := selectTrainingDevice(latest, ts.StatusKeys, devices)
	deviceData := sel.Data
	acwr := asMap(deviceData["acuteTrainingLoadDTO"])

	recentVO2 := asMap(status["mostRecentVO2Max"])
	vo2, cyclingVO2 := asMap(recentVO2["generic"]), asMap(recentVO2["cycling"])

	// Load balance is device-keyed too: never attach the first available record to a different reporting device.
	loadMap := asMap(asMap(status["mostRecentTrainingLoadBalance"])["metricsTrainingLoadBalanceDTOMap"])
	loadData := map[string]any{}
	var loadDeviceID *string
	if sel.ID == nil {
		// A day can have Load Focus without a training-status snapshot: select its own device.
		l := selectTrainingDevice(loadMap, ts.LoadKeys, devices)
		loadData, loadDeviceID = l.Data, l.ID
	} else {
		for _, key := range deviceEntries(loadMap, ts.LoadKeys) {
			data := asMap(loadMap[key])
			if id := deviceIDOr(data["deviceId"], key); id != nil && *id == *sel.ID {
				loadData, loadDeviceID = data, id
				break
			}
		}
	}

	curated := map[string]any{
		"date":                      getDefault(deviceData, "calendarDate", date),
		"reporting_device_id":       intIfDigitsPtr(sel.ID),
		"reporting_device_name":     strOrNil(sel.Name),
		"load_device_id":            intIfDigitsPtr(loadDeviceID),
		"training_status":           deviceData["trainingStatus"],
		"training_status_feedback":  deviceData["trainingStatusFeedbackPhrase"],
		"sport":                     deviceData["sport"],
		"fitness_trend":             deviceData["fitnessTrend"],
		"vo2_max":                   vo2["vo2MaxValue"],
		"vo2_max_precise":           vo2["vo2MaxPreciseValue"],
		"cycling_vo2_max":           cyclingVO2["vo2MaxValue"],
		"cycling_vo2_max_precise":   cyclingVO2["vo2MaxPreciseValue"],
		"monthly_load_aerobic_low":  loadData["monthlyLoadAerobicLow"],
		"monthly_load_aerobic_high": loadData["monthlyLoadAerobicHigh"],
		"monthly_load_anaerobic":    loadData["monthlyLoadAnaerobic"],
		"training_balance_feedback": loadData["trainingBalanceFeedbackPhrase"],
	}

	if sel.Primary == nil || *sel.Primary {
		curated["acute_load"] = acwr["dailyTrainingLoadAcute"]
		curated["chronic_load"] = acwr["dailyTrainingLoadChronic"]
		curated["load_ratio"] = acwr["dailyAcuteChronicWorkloadRatio"]
		curated["acwr_status"] = acwr["acwrStatus"]
		curated["acwr_percent"] = acwr["acwrPercent"]
		curated["optimal_chronic_load_min"] = acwr["minTrainingLoadChronic"]
		curated["optimal_chronic_load_max"] = acwr["maxTrainingLoadChronic"]
	} else {
		var acute any
		reason := "readiness_unavailable"
		if readiness, err := c.GetTrainingReadiness(ctx, date); err == nil {
			acute, reason = trackerAcuteLoad(readiness, date, primaryDeviceID(devices))
		}
		if acute != nil {
			curated["acute_load"] = acute
			curated["acute_load_source"] = "training_readiness"
		} else if reason != "" {
			curated["acute_load_source"] = "unavailable"
			curated["acute_load_unavailable_reason"] = reason
		}
	}

	if len(loadData) == 0 {
		for _, v := range loadMap {
			if len(asMap(v)) > 0 {
				curated["load_source"] = "unavailable"
				curated["load_unavailable_reason"] = "load_device_mismatch"
				break
			}
		}
	}
	return dropNil(curated), nil
}

func lactateThreshold(ctx context.Context, s *Session, in lactateIn) (any, error) {
	start, end := "", ""
	if in.StartDate != nil {
		start = *in.StartDate
	}
	if in.EndDate != nil {
		end = *in.EndDate
	}
	// A lone date would otherwise fall through to the latest-values query and silently ignore the requested range.
	if (start != "") != (end != "") {
		return "Provide both start_date and end_date for lactate threshold history, or omit both for the latest values.", nil
	}
	ranged := start != ""
	threshold, err := s.Client.GetLactateThreshold(ctx, !ranged, start, end)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving lactate threshold data: %w", err)
	}
	if falsy(threshold) {
		if ranged {
			return fmt.Sprintf("No lactate threshold data found between %s and %s", start, end), nil
		}
		return "No lactate threshold data found", nil
	}
	th := asMap(threshold)
	var curated map[string]any
	if ranged {
		// Historical range format: {speed: [...], heart_rate: [...], power: [...]}
		curated = map[string]any{"start_date": start, "end_date": end}
		history := func(key, field string, entries any, value func(any) any) {
			if falsy(entries) {
				return
			}
			list := []any{}
			for _, e := range asList(entries) {
				entry := asMap(e)
				list = append(list, map[string]any{"date": entry["from"], field: value(entry["value"]), "series": entry["series"]})
			}
			curated[key] = list
		}
		same := func(v any) any { return v }
		history("speed_history", "speed_mps", th["speed"], lactateSpeedMPS)
		// garminconnect returns it under "heart_rate"; accept the camelCase key too.
		history("heart_rate_history", "heart_rate_bpm", firstTruthy(th["heart_rate"], th["heartRate"]), same)
		history("power_history", "power_watts", th["power"], same)
	} else {
		// Latest format: {speed_and_heart_rate: {...}, power: {...}}
		speedHR, power := asMap(th["speed_and_heart_rate"]), asMap(th["power"])
		curated = map[string]any{
			"lactate_threshold_speed_mps":      lactateSpeedMPS(speedHR["speed"]),
			"lactate_threshold_heart_rate_bpm": speedHR["heartRate"],
			"heart_rate_cycling_bpm":           speedHR["heartRateCycling"],
			"speed_hr_date":                    speedHR["calendarDate"],
			"functional_threshold_power_watts": power["functionalThresholdPower"],
			"weight_kg":                        power["weight"],
			"power_to_weight":                  power["powerToWeight"],
			"sport":                            power["sport"],
			"power_date":                       power["calendarDate"],
			"is_stale":                         power["isStale"],
		}
	}
	return dropNil(curated), nil
}

func trainingLoadTrend(ctx context.Context, s *Session, in rangeIn) (any, error) {
	start, end, msg := trendRange(in.StartDate, in.EndDate, 90)
	if msg != "" {
		return msg, nil
	}
	devices, err := s.Client.GetDevices(ctx)
	if err != nil {
		if isFatalTrendError(err) {
			return nil, fmt.Errorf("Error retrieving training load device data: %w", err)
		}
		devices = nil
	}
	trend := []map[string]any{}
	for cur := start; !cur.After(end); cur = cur.AddDate(0, 0, 1) {
		date := cur.Format(time.DateOnly)
		entry, err := loadTrendEntry(ctx, s.Client, date, devices)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving training load data for %s: %w", date, err)
		}
		if len(entry) > 1 { // more than just the date
			trend = append(trend, entry)
		}
	}
	if len(trend) == 0 {
		return fmt.Sprintf("No training load data found between %s and %s.", in.StartDate, in.EndDate), nil
	}
	return map[string]any{"start_date": in.StartDate, "end_date": in.EndDate, "days_with_data": len(trend), "trend": trend}, nil
}

// loadTrendEntry is one day of get_training_load_trend.
func loadTrendEntry(ctx context.Context, c *garmin.Client, date string, devices any) (map[string]any, error) {
	ts, err := c.GetTrainingStatus(ctx, date)
	if err != nil {
		return nil, err
	}
	entry := map[string]any{"date": date}
	if falsy(ts.Data) {
		return entry, nil
	}
	data := asMap(ts.Data)
	// latestTrainingStatusData is keyed by device ID: pick the primary tracker's record (see selectTrainingDevice).
	latest := asMap(asMap(data["mostRecentTrainingStatus"])["latestTrainingStatusData"])
	sel := selectTrainingDevice(latest, ts.StatusKeys, devices)
	dto := asMap(sel.Data["acuteTrainingLoadDTO"])
	vo2 := asMap(asMap(data["mostRecentVO2Max"])["generic"])
	if sel.ID != nil {
		entry["reporting_device_id"] = intIfDigits(*sel.ID)
	}
	if sel.Name != nil {
		entry["reporting_device_name"] = *sel.Name
	}
	atl, ctl, acwr := dto["dailyTrainingLoadAcute"], dto["dailyTrainingLoadChronic"], dto["dailyAcuteChronicWorkloadRatio"]
	primary := sel.Primary == nil || *sel.Primary
	if !primary {
		var reason string
		readiness, err := c.GetTrainingReadiness(ctx, date)
		if err == nil {
			atl, reason = trackerAcuteLoad(readiness, date, primaryDeviceID(devices))
		} else {
			if isFatalTrendError(err) {
				return nil, err
			}
			atl, reason = nil, "readiness_unavailable"
			entry["readiness_error"] = err.Error()
		}
		ctl, acwr = nil, nil
		if atl != nil {
			entry["atl_source"] = "training_readiness"
		} else {
			entry["atl_source"] = "unavailable"
			entry["atl_unavailable_reason"] = reason
		}
	}
	if atl != nil {
		entry["atl"] = roundAny(atl, 1)
	}
	if ctl != nil {
		entry["ctl"] = roundAny(ctl, 1)
	}
	if a, ok := asNumber(atl); ok {
		if c, ok := asNumber(ctl); ok {
			entry["tsb"] = round(c-a, 1)
		}
	}
	if acwr != nil {
		entry["acwr"] = roundAny(acwr, 2)
	}
	if primary {
		if v := dto["acwrStatus"]; !falsy(v) {
			entry["acwr_status"] = v
		}
		if v := dto["acwrPercent"]; v != nil {
			entry["acwr_percent"] = v
		}
		if v := dto["minTrainingLoadChronic"]; v != nil {
			entry["optimal_chronic_load_min"] = roundAny(v, 1)
		}
		if v := dto["maxTrainingLoadChronic"]; v != nil {
			entry["optimal_chronic_load_max"] = roundAny(v, 1)
		}
	}
	// Device data has the training status fields flattened (no trainingStatusDTO sub-object).
	if v := sel.Data["trainingStatusFeedbackPhrase"]; !falsy(v) {
		entry["training_status"] = v
	}
	if v := sel.Data["trainingStatus"]; v != nil {
		entry["training_status_code"] = v
	}
	if v := sel.Data["fitnessTrend"]; v != nil {
		entry["fitness_trend"] = v
	}
	if v := vo2["vo2MaxValue"]; v != nil {
		entry["vo2_max"] = roundAny(v, 1)
	}
	return entry, nil
}

func trainingLoadBalance(ctx context.Context, s *Session, in dateIn) (any, error) {
	ts, err := s.Client.GetTrainingStatus(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving training load balance: %w", err)
	}
	noData := fmt.Sprintf("No training load balance data found for %s.", in.Date)
	if falsy(ts.Data) {
		return noData, nil
	}
	loadMap := asMap(asMap(asMap(ts.Data)["mostRecentTrainingLoadBalance"])["metricsTrainingLoadBalanceDTOMap"])
	// Device-keyed like the status map: select with the same helper as get_training_status.
	sel := selectTrainingDevice(loadMap, ts.LoadKeys, devicesOrNil(ctx, s.Client))
	load := sel.Data
	if len(load) == 0 {
		return noData, nil
	}
	band := func(loadKey, minKey, maxKey string) map[string]any {
		l, tmin, tmax := load[loadKey], load[minKey], load[maxKey]
		if l == nil && tmin == nil && tmax == nil {
			return nil
		}
		b := map[string]any{}
		if l != nil {
			b["load"] = roundAny(l, 1)
		}
		if tmin != nil {
			b["target_min"] = tmin
		}
		if tmax != nil {
			b["target_max"] = tmax
		}
		lf, ok1 := asNumber(l)
		lo, ok2 := asNumber(tmin)
		hi, ok3 := asNumber(tmax)
		if ok1 && ok2 && ok3 {
			switch {
			case lf < lo:
				b["status"] = "below"
			case lf > hi:
				b["status"] = "above"
			default:
				b["status"] = "within"
			}
		}
		return b
	}
	result := map[string]any{"date": getDefault(load, "calendarDate", in.Date)}
	if sel.ID != nil {
		result["load_device_id"] = intIfDigits(*sel.ID)
	}
	if sel.Name != nil {
		result["load_device_name"] = *sel.Name
	}
	if f := load["trainingBalanceFeedbackPhrase"]; !falsy(f) {
		result["feedback"] = f
	}
	for _, b := range []struct{ out, load, min, max string }{
		{"aerobic_low", "monthlyLoadAerobicLow", "monthlyLoadAerobicLowTargetMin", "monthlyLoadAerobicLowTargetMax"},
		{"aerobic_high", "monthlyLoadAerobicHigh", "monthlyLoadAerobicHighTargetMin", "monthlyLoadAerobicHighTargetMax"},
		{"anaerobic", "monthlyLoadAnaerobic", "monthlyLoadAnaerobicTargetMin", "monthlyLoadAnaerobicTargetMax"},
	} {
		if v := band(b.load, b.min, b.max); len(v) > 0 {
			result[b.out] = v
		}
	}
	return result, nil
}

func runningToleranceTrend(ctx context.Context, s *Session, in toleranceTrendIn) (any, error) {
	aggregation := "weekly"
	if in.Aggregation != nil {
		aggregation = *in.Aggregation
	}
	if aggregation != "daily" && aggregation != "weekly" {
		return nil, errors.New("Input should be 'daily' or 'weekly'")
	}
	maxDays := 366
	if aggregation == "daily" {
		maxDays = 90
	}
	_, _, msg := trendRange(in.StartDate, in.EndDate, maxDays)
	if msg != "" {
		if strings.HasPrefix(msg, "Date range too large") {
			msg = fmt.Sprintf("%s for %s aggregation.", strings.TrimSuffix(msg, "."), aggregation)
		}
		return msg, nil
	}

	data, err := s.Client.GetRunningTolerance(ctx, in.StartDate, in.EndDate, aggregation)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving running tolerance trend: %w", err)
	}
	if falsy(data) {
		return fmt.Sprintf("No running tolerance data found between %s and %s.", in.StartDate, in.EndDate), nil
	}
	trend := []map[string]any{}
	for _, e := range asList(data) {
		entry := asMap(e)
		var point map[string]any
		if aggregation == "daily" {
			point = toleranceFields(entry["acuteTolerance"], entry["acuteImpactLoad"], entry["acuteDistance"])
			if f := entry["runningToleranceFeedBackPhrase"]; !falsy(f) {
				point["feedback_phrase"] = f
			}
		} else {
			point = toleranceFields(entry["tolerance"], entry["totalImpactLoad"], entry["totalDistance"])
			for _, kv := range [][2]string{{"start_of_week", "startOfWeek"}, {"end_of_week", "endOfWeek"}, {"week_index", "weekIndex"}} {
				if v := entry[kv[1]]; v != nil {
					point[kv[0]] = v
				}
			}
		}
		point["date"] = entry["calendarDate"]
		trend = append(trend, point)
	}
	// The daily aggregation is not returned in chronological order by the API.
	sort.SliceStable(trend, func(i, j int) bool { return textOr(trend[i]["date"], "") < textOr(trend[j]["date"], "") })

	var first, latest, change any
	var values []float64
	for _, p := range trend {
		if v, ok := asNumber(p["tolerance_km"]); ok {
			values = append(values, v)
		}
	}
	if len(values) > 0 {
		first, latest, change = values[0], values[len(values)-1], round(values[len(values)-1]-values[0], 2)
	}
	return map[string]any{
		"start_date": in.StartDate, "end_date": in.EndDate, "aggregation": aggregation, "data_points": len(trend),
		"first_tolerance_km": first, "latest_tolerance_km": latest, "tolerance_change_km": change, "trend": trend,
	}, nil
}
