package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

const trainingActivitiesSearchPath = "/activitylist-service/activities/search/activities"

const (
	getProgressSummaryDescription = `Get progress summary for a metric between dates`

	getHillScoreDescription      = `Get hill score data between dates`
	getEnduranceScoreDescription = `Get endurance score data between dates`
	getTrainingEffectDescription = `Get training effect data for a specific activity.

Uses activity details first. If Garmin returns HTTP 403 for that
endpoint (seen on valid, recent activities), falls back to activity
list search, which still includes aerobic/anaerobic training effect.`
	getHrvDataDescription        = `Get Heart Rate Variability (HRV) data`
	getFitnessAgeDataDescription = `Get fitness age data`
	getTrainingStatusDescription = `Get training status with curated metrics

Returns comprehensive training status including load, VO2 max, recovery,
and training readiness indicators.`
	getCyclingFTPDescription = `Get the latest cycling Functional Threshold Power (FTP) data.

Returns the most recent cycling FTP estimate available from Garmin.`
	getLactateThresholdDescription = `Get lactate threshold data

Returns lactate threshold information, which is the exercise intensity at
which lactate starts to accumulate in the blood. This is a key metric for
endurance training.`
	requestReloadDescription        = `Request reload of epoch data`
	getTrainingLoadTrendDescription = "Get Garmin's daily acute and chronic training load over a date range.\n\nReturns, per day, Garmin's acute load (`atl`, the 7-day load), chronic load (`ctl`), their\ndifference (`tsb` = ctl - atl), and Garmin's Acute:Chronic Workload Ratio (`acwr`,\n`acwr_status`). These are Garmin's dailyTrainingLoadAcute/Chronic values, not TrainingPeaks\nCTL/ATL: they are on Garmin's weekly load scale, so `tsb` is not comparable to TrainingPeaks\nTSB and TSB form thresholds do not apply. Use `acwr`/`acwr_status` (and the optimal chronic\nload range) to judge whether load is building steadily or spiking.\n\nRecommended range: 4-8 weeks. Maximum: 90 days.\nEmpty days are skipped; a failed daily request returns an error."

	getTrainingLoadBalanceDescription = `Get Garmin's Load Focus — the distribution of the trailing-month
training load across Aerobic Low, Aerobic High, and Anaerobic intensity
bands, plus the system's feedback phrase (e.g. AEROBIC_HIGH_SHORTAGE,
BALANCED, ANAEROBIC_SHORTAGE).

Use this to assess whether the athlete's training mix is balanced or
deficient in a particular intensity band. Each band reports its load
alongside Garmin's target range; a ` + "`status`" + ` of "below", "within", or
"above" is computed from the load relative to that range.

Garmin reports Load Focus per device. The primary activity tracker's
record is used when it can be identified; ` + "`load_device_id`" + ` and
` + "`load_device_name`" + ` say which device the numbers came from.`
	getHrvTrendDescription = `Get HRV (Heart Rate Variability) trend over a date range.

Returns daily HRV values and weekly rolling averages. Single-day HRV is too noisy
to act on — use this tool to identify baseline shifts that signal accumulated fatigue
or recovery. A drop of >10ms from the 7-day baseline warrants reducing training load.

Recommended range: 7-21 days. Maximum: 30 days.
Empty days are skipped; a failed daily request returns an error.`
	getVo2maxTrendDescription = `Get VO2 max trend over a date range.

Returns daily VO2 max estimates from Garmin's FirstBeat algorithm. Use this to track
whether training is producing fitness gains over weeks or months. Flat or declining
VO2 max over 4+ weeks suggests insufficient training stimulus or overreaching.

Note: VO2 max estimates are smoothed and update gradually — daily changes of <0.5
are within normal noise. Focus on the 4-6 week trend direction.

Garmin records a new VO2 max value only on days with a recompute (after an
activity). Days in between carry the last known value forward and are marked
with "carried_forward": true, matching the trend chart in Garmin Connect.
Dated status values retain their measurement date. data_points counts
displayed days, including carried-forward values.

If historical values are unavailable, the current profile estimate is returned
separately and is not represented as a historical trend point.
Endpoint-specific failures remain visible in request_errors when a
fallback succeeds. Authentication, rate-limit, and connection failures
stop the request.

Recommended range: 4-12 weeks. Maximum: 90 days.`
	getRespirationTrendDescription = `Get overnight respiration rate trend over a date range.

Elevated resting respiration rate (compared to personal baseline) is an early
warning sign for overreaching, illness, or poor recovery. Use this alongside HRV
trend for a complete recovery picture.

Recommended range: 7-21 days. Maximum: 30 days.
Empty days are skipped; a failed daily request returns an error.`
	getRunningToleranceDescription = "Get Running Tolerance for a single day.\n\nReturns Garmin's running load capacity model: how much running load the\nathlete can currently absorb (tolerance), the intensity-adjusted load\ntheir recent runs have produced (acute load), and the raw distance behind\nthat load. All three are expressed in km so they're directly comparable —\n`load_ratio` (acute_load_km / distance_km) quantifies how much intensity\nis inflating the cost of each kilometer run."

	getRunningToleranceTrendDescription = `Get Running Tolerance trend over a date range.

Running Tolerance moves slowly — its value is in the trajectory, not any
single day. Returns, per period: tolerance_km (current load capacity),
acute_load_km (intensity-adjusted load), distance_km (actual distance
run), and load_ratio (acute_load_km / distance_km — how much intensity
inflates the cost of each kilometer). Weekly aggregation (default) gives
a compact multi-month view; daily gives day-to-day resolution for a
shorter window.

Recommended range: 4-12 weeks. Maximum: 90 days for daily aggregation,
366 days for weekly (this endpoint returns the whole range in one call,
so the limit protects output size, not request volume).`
	getAcclimationDescription = `Get heat and altitude acclimation status for a given date.

Garmin tracks how adapted the athlete currently is to training in heat
and at altitude. heat_acclimation_percent runs 0-100 and decays without
continued exposure; use it to judge readiness for a warm-weather race.

heat_trend reports Garmin's own label (e.g. ACCLIMATIZED). The
previous_* fields hold the prior reading so direction of travel is
visible without a second call.

VO2 max is not returned here; use get_training_status or get_vo2max_trend.`
)

type progressSummaryInput struct {
	Account
	StartDate string `json:"start_date" jsonschema:"Start date in YYYY-MM-DD format"`
	EndDate   string `json:"end_date" jsonschema:"End date in YYYY-MM-DD format"`
	Metric    string `json:"metric" jsonschema:"Metric to get progress for (e.g., \"elevationGain\", \"duration\", \"distance\", \"movingDuration\", \"calories\"). Calories are returned in kcal; Garmin's endpoint stores this metric pre-scaled by ~4.19 and this tool undoes that scaling."`
}

type trRangeInput struct {
	Account
	StartDate string `json:"start_date" jsonschema:"Start date in YYYY-MM-DD format"`
	EndDate   string `json:"end_date" jsonschema:"End date in YYYY-MM-DD format"`
}

type trainingEffectInput struct {
	Account
	ActivityID any `json:"activity_id" jsonschema:"ID of the activity to retrieve training effect for"`
}

type hrvDataInput struct {
	Account
	Date             string `json:"date" jsonschema:"Date in YYYY-MM-DD format"`
	ReturnTimeseries bool   `json:"return_timeseries,omitempty" jsonschema:"If True, include detailed 5-minute HRV readings (can be large)"`
}

type fitnessAgeInput struct {
	Account
	Date    string `json:"date" jsonschema:"Date in YYYY-MM-DD format"`
	Details bool   `json:"details,omitempty" jsonschema:"If True, include component breakdown (BMI, RHR, vigorous activity) with targets and improvement suggestions"`
}

type trDateInput struct {
	Account
	Date string `json:"date" jsonschema:"Date in YYYY-MM-DD format"`
}

type cyclingFTPInput struct {
	Account
}

type lactateThresholdInput struct {
	Account
	StartDate *string `json:"start_date,omitempty" jsonschema:"Start date in YYYY-MM-DD format (optional; give with end_date for history, omit both for latest)"`
	EndDate   *string `json:"end_date,omitempty" jsonschema:"End date in YYYY-MM-DD format (optional; give with start_date for history, omit both for latest)"`
}

type runningToleranceTrendInput struct {
	Account
	StartDate   string `json:"start_date" jsonschema:"Start date in YYYY-MM-DD format"`
	EndDate     string `json:"end_date" jsonschema:"End date in YYYY-MM-DD format"`
	Aggregation string `json:"aggregation,omitempty" jsonschema:"\"daily\" or \"weekly\" (default \"weekly\")"`
}

func registerTrainingTools(r *registry) {
	addTool(r, "get_progress_summary_between_dates", getProgressSummaryDescription, getProgressSummaryBetweenDates)
	addTool(r, "get_hill_score", getHillScoreDescription, getHillScore)
	addTool(r, "get_endurance_score", getEnduranceScoreDescription, getEnduranceScore)
	addTool(r, "get_training_effect", getTrainingEffectDescription, getTrainingEffect)
	addTool(r, "get_hrv_data", getHrvDataDescription, getHrvData)
	addTool(r, "get_fitnessage_data", getFitnessAgeDataDescription, getFitnessageData)
	addTool(r, "get_training_status", getTrainingStatusDescription, getTrainingStatus)
	addTool(r, "get_cycling_ftp", getCyclingFTPDescription, getCyclingFTP)
	addTool(r, "get_lactate_threshold", getLactateThresholdDescription, getLactateThreshold)
	addTool(r, "request_reload", requestReloadDescription, requestReload)
	addTool(r, "get_training_load_trend", getTrainingLoadTrendDescription, getTrainingLoadTrend)
	addTool(r, "get_training_load_balance", getTrainingLoadBalanceDescription, getTrainingLoadBalance)
	addTool(r, "get_hrv_trend", getHrvTrendDescription, getHrvTrend)
	addTool(r, "get_vo2max_trend", getVo2maxTrendDescription, getVo2maxTrend)
	addTool(r, "get_respiration_trend", getRespirationTrendDescription, getRespirationTrend)
	addTool(r, "get_running_tolerance", getRunningToleranceDescription, getRunningTolerance)
	addTool(r, "get_running_tolerance_trend", getRunningToleranceTrendDescription, getRunningToleranceTrend)
	addTool(r, "get_acclimation", getAcclimationDescription, getAcclimation)
}

// --- shared training helpers ---

// trainingActivityCache mirrors the Python module cache, keyed per account client.
var trainingActivityCache sync.Map

func numberString(v any) string {
	switch x := v.(type) {
	case float64:
		if x == math.Trunc(x) && !math.IsInf(x, 0) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case json.Number:
		return x.String()
	}
	return fmt.Sprint(v)
}

func deviceIDOf(v any) *string {
	if v == nil {
		return nil
	}
	s := numberString(v)
	return &s
}

func firstNonEmptyString(values ...any) string {
	for _, v := range values {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func primaryDeviceID(devices any) *string {
	for _, item := range asList(devices) {
		device, ok := item.(map[string]any)
		if !ok || device["primaryDevice"] != true {
			continue
		}
		return deviceIDOf(device["deviceId"])
	}
	return nil
}

// selectTrainingDevice picks one status record and reports whether it belongs
// to the verified primary tracker.
func selectTrainingDevice(latestData any, devices any) (map[string]any, *string, *string, *bool) {
	latest := asMap(latestData)
	keys := make([]string, 0, len(latest))
	for key, value := range latest {
		if m, ok := value.(map[string]any); ok && len(m) > 0 {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return map[string]any{}, nil, nil, nil
	}
	// Python relies on insertion order; Go maps do not preserve it, so sort for
	// a deterministic fallback.
	sort.Strings(keys)

	deviceRecords := []map[string]any{}
	for _, item := range asList(devices) {
		if m, ok := item.(map[string]any); ok {
			deviceRecords = append(deviceRecords, m)
		}
	}

	var primaryID *string
	for _, device := range deviceRecords {
		if device["primaryDevice"] == true {
			primaryID = deviceIDOf(device["deviceId"])
			break
		}
	}

	selectedKey := keys[0]
	selected := asMap(latest[selectedKey])
	if primaryID != nil {
		matchedPrimary := false
		for _, key := range keys {
			value := asMap(latest[key])
			recordID := deviceIDOf(value["deviceId"])
			if recordID == nil {
				recordID = deviceIDOf(key)
			}
			if recordID != nil && *recordID == *primaryID {
				selectedKey, selected = key, value
				matchedPrimary = true
				break
			}
		}
		if !matchedPrimary {
			for _, key := range keys {
				value := asMap(latest[key])
				if value["primaryTrainingDevice"] == true {
					selectedKey, selected = key, value
					break
				}
			}
		}
	} else {
		for _, key := range keys {
			value := asMap(latest[key])
			if value["primaryTrainingDevice"] == true {
				selectedKey, selected = key, value
				break
			}
		}
	}

	reportingID := deviceIDOf(selected["deviceId"])
	if reportingID == nil {
		reportingID = deviceIDOf(selectedKey)
	}

	var reportingName *string
	for _, device := range deviceRecords {
		id := deviceIDOf(device["deviceId"])
		if id == nil || reportingID == nil || *id != *reportingID {
			continue
		}
		if name := firstNonEmptyString(device["displayName"], device["productDisplayName"]); name != "" {
			reportingName = &name
		}
		break
	}

	var isPrimary *bool
	if primaryID != nil && reportingID != nil {
		value := *reportingID == *primaryID
		isPrimary = &value
	}
	return selected, reportingID, reportingName, isPrimary
}

func timestampOf(m map[string]any) string {
	if v := m["timestampLocal"]; v != nil {
		return fmt.Sprint(v)
	}
	if v := m["timestamp"]; v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

func trackerAcuteLoad(readiness any, date string, trackerID *string) (*float64, *string) {
	if trackerID == nil {
		reason := "tracker_id_unavailable"
		return nil, &reason
	}
	candidates := []any{readiness}
	if list, ok := readiness.([]any); ok {
		candidates = list
	}
	var matching []map[string]any
	for _, candidate := range candidates {
		m, ok := candidate.(map[string]any)
		if !ok {
			continue
		}
		id := deviceIDOf(m["deviceId"])
		if id != nil && *id == *trackerID && m["calendarDate"] == date && m["acuteLoad"] != nil {
			matching = append(matching, m)
		}
	}
	if len(matching) == 0 {
		reason := "readiness_device_or_date_mismatch"
		return nil, &reason
	}
	sort.SliceStable(matching, func(i, j int) bool {
		return timestampOf(matching[i]) < timestampOf(matching[j])
	})
	value, ok := numberOf(matching[len(matching)-1]["acuteLoad"])
	if !ok {
		reason := "readiness_device_or_date_mismatch"
		return nil, &reason
	}
	return &value, nil
}

func progressMetricValue(metric string, value any) any {
	if value == nil || metric != "calories" {
		return value
	}
	f, ok := numberOf(value)
	if !ok {
		return value
	}
	return f / 4.19
}

func lactateThresholdSpeedMPS(value any) any {
	if !truthy(value) {
		return nil
	}
	f, ok := numberOf(value)
	if !ok {
		return nil
	}
	return f * 10
}

func firstPresent(mapping map[string]any, keys ...string) any {
	for _, key := range keys {
		if value := mapping[key]; value != nil {
			return value
		}
	}
	return nil
}

func extractVO2Measurements(data any) map[string]float64 {
	measurements := map[string]float64{}
	if list, ok := data.([]any); ok {
		for _, item := range list {
			for sport, value := range extractVO2Measurements(item) {
				if _, exists := measurements[sport]; !exists {
					measurements[sport] = value
				}
			}
		}
		return measurements
	}

	root := asMap(data)
	candidates := []struct {
		path  []string
		sport string
	}{
		{[]string{"vo2MaxRunning"}, "running"},
		{[]string{"vo2MaxCycling"}, "cycling"},
		{[]string{"vo2Max"}, "running"},
		{[]string{"vo2MaxPreciseValue"}, "running"},
		{[]string{"vo2MaxValue"}, "running"},
		{[]string{"generic", "vo2MaxPreciseValue"}, "running"},
		{[]string{"generic", "vo2MaxValue"}, "running"},
		{[]string{"cycling", "vo2MaxPreciseValue"}, "cycling"},
		{[]string{"cycling", "vo2MaxValue"}, "cycling"},
		{[]string{"mostRecentVO2Max", "generic", "vo2MaxPreciseValue"}, "running"},
		{[]string{"mostRecentVO2Max", "generic", "vo2MaxValue"}, "running"},
		{[]string{"mostRecentVO2Max", "cycling", "vo2MaxPreciseValue"}, "cycling"},
		{[]string{"mostRecentVO2Max", "cycling", "vo2MaxValue"}, "cycling"},
		{[]string{"userData", "vo2MaxRunning"}, "running"},
		{[]string{"userData", "vo2MaxCycling"}, "cycling"},
	}
	for _, candidate := range candidates {
		if _, exists := measurements[candidate.sport]; exists {
			continue
		}
		var current any = root
		for _, key := range candidate.path {
			current = asMap(current)[key]
		}
		if value, ok := numberOf(current); ok {
			measurements[candidate.sport] = value
		}
	}
	return measurements
}

func extractDatedVO2Measurements(data any) map[string]map[string]float64 {
	byDate := map[string]map[string]float64{}
	var collect func(any)
	collect = func(item any) {
		if list, ok := item.([]any); ok {
			for _, nested := range list {
				collect(nested)
			}
			return
		}

		root := asMap(item)
		measurements := extractVO2Measurements(root)
		sections := asMap(root["mostRecentVO2Max"])
		if len(sections) == 0 {
			sections = root
		}
		for sport, value := range measurements {
			sectionName := "cycling"
			if sport == "running" {
				sectionName = "generic"
			}
			section := asMap(sections[sectionName])
			calendarDate := section["calendarDate"]
			if calendarDate == nil {
				calendarDate = sections["calendarDate"]
			}
			if calendarDate == nil {
				calendarDate = root["calendarDate"]
			}
			date, ok := calendarDate.(string)
			if !ok {
				continue
			}
			if byDate[date] == nil {
				byDate[date] = map[string]float64{}
			}
			if _, exists := byDate[date][sport]; !exists {
				byDate[date][sport] = value
			}
		}
	}
	collect(data)
	return byDate
}

func buildVO2TrendSeries(history []map[string]any, end time.Time) []map[string]any {
	series := []map[string]any{}
	if len(history) == 0 {
		return series
	}

	measured := map[string]map[string]any{}
	var firstDay time.Time
	first := true
	for _, entry := range history {
		date, _ := entry["date"].(string)
		measured[date] = entry
		day, err := time.Parse(dateLayout, date)
		if err != nil {
			continue
		}
		if first || day.Before(firstDay) {
			firstDay = day
			first = false
		}
	}
	if first {
		return series
	}

	var last map[string]any
	for current := firstDay; !current.After(end); current = current.AddDate(0, 0, 1) {
		date := current.Format(dateLayout)
		if entry, ok := measured[date]; ok {
			last = map[string]any{
				"date":    entry["date"],
				"vo2_max": entry["vo2_max"],
				"source":  entry["source"],
			}
			series = append(series, last)
		} else if last != nil {
			series = append(series, map[string]any{
				"date":            date,
				"vo2_max":         last["vo2_max"],
				"source":          last["source"],
				"carried_forward": true,
			})
		}
	}
	return series
}

func trainingActivityTypes(ctx context.Context, c *garmin.Client) map[int]string {
	if cached, ok := trainingActivityCache.Load(c); ok {
		return cached.(map[int]string)
	}
	payload, err := c.GetActivityTypes(ctx)
	if err != nil {
		return map[int]string{}
	}
	mapping := map[int]string{}
	for _, item := range asList(payload) {
		record, ok := item.(map[string]any)
		if !ok || record["typeId"] == nil {
			continue
		}
		id, ok := numberOf(record["typeId"])
		if !ok {
			continue
		}
		key := "unknown"
		if value, exists := record["typeKey"]; exists {
			if s, ok := value.(string); ok && s != "" {
				key = s
			}
		}
		mapping[int(id)] = key
	}
	trainingActivityCache.Store(c, mapping)
	return mapping
}

func mapContributor(contributor map[string]any, mapping map[int]string) map[string]any {
	result := map[string]any{"contribution_percent": nil}
	if contribution := contributor["contribution"]; truthy(contribution) {
		if value, ok := numberOf(contribution); ok {
			result["contribution_percent"] = roundTo(value, 2)
		} else {
			result["contribution_percent"] = contribution
		}
	}

	if activityTypeID := contributor["activityTypeId"]; activityTypeID != nil {
		name := "unknown_" + numberString(activityTypeID)
		if id, ok := numberOf(activityTypeID); ok {
			if mapped, exists := mapping[int(id)]; exists {
				name = mapped
			}
		}
		result["activity_type"] = name
		result["activity_type_id"] = activityTypeID
	} else if group := contributor["group"]; group != nil {
		groupNames := map[int]string{0: "running (?)", 1: "biking (?)", 8: "Other Activities"}
		name := "group_" + numberString(group)
		if id, ok := numberOf(group); ok {
			if mapped, exists := groupNames[int(id)]; exists {
				name = mapped
			}
		}
		result["group"] = name
	}
	return result
}

func isForbiddenError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *garmin.APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "403") || strings.Contains(text, "forbidden")
}

func isFatalTrendError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, garmin.ErrSessionExpired) {
		return true
	}
	var apiErr *garmin.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status == http.StatusUnauthorized ||
			apiErr.Status == http.StatusTooManyRequests ||
			apiErr.Status >= http.StatusInternalServerError
	}
	// garminconnect treats connection failures as fatal; non-HTTP errors here
	// are transport or decode failures and get the same treatment.
	return true
}

func activityIDMatches(item map[string]any, activityID int64) bool {
	raw := item["activityId"]
	if value, ok := numberOf(raw); ok {
		return int64(value) == activityID
	}
	return fmt.Sprint(raw) == strconv.FormatInt(activityID, 10)
}

func asActivityList(payload any) []map[string]any {
	out := []map[string]any{}
	if list, ok := payload.([]any); ok {
		for _, item := range list {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	data := asMap(payload)
	for _, key := range []string{"activityList", "activities"} {
		if list, ok := data[key].([]any); ok {
			for _, item := range list {
				if m, ok := item.(map[string]any); ok {
					out = append(out, m)
				}
			}
			return out
		}
	}
	return out
}

func lookupActivityInList(ctx context.Context, c *garmin.Client, activityID int64, limit int) (map[string]any, error) {
	attempts := []url.Values{
		{"activityIds": {strconv.FormatInt(activityID, 10)}, "start": {"0"}, "limit": {"1"}},
		{"start": {"0"}, "limit": {strconv.Itoa(limit)}},
	}
	for _, query := range attempts {
		payload, err := c.GetJSON(ctx, trainingActivitiesSearchPath, query)
		if err != nil {
			continue
		}
		for _, item := range asActivityList(payload) {
			if activityIDMatches(item, activityID) {
				return item, nil
			}
		}
	}
	payload, err := c.GetActivities(ctx, 0, limit)
	if err != nil {
		return nil, nil
	}
	for _, item := range asActivityList(payload) {
		if activityIDMatches(item, activityID) {
			return item, nil
		}
	}
	return nil, nil
}

func minutesToHours(value any) *float64 {
	minutes, ok := numberOf(value)
	if !ok || minutes <= 0 {
		return nil
	}
	hours := roundTo(minutes/60.0, 1)
	return &hours
}

func curateTrainingEffect(activity any, activityID int64, source string) map[string]any {
	record := asMap(activity)
	summary := asMap(record["summaryDTO"])

	aerobic := firstPresent(summary, "trainingEffect", "aerobicTrainingEffect")
	if aerobic == nil {
		aerobic = firstPresent(record, "aerobicTrainingEffect", "trainingEffect")
	}
	anaerobic := firstPresent(summary, "anaerobicTrainingEffect")
	if anaerobic == nil {
		anaerobic = firstPresent(record, "anaerobicTrainingEffect")
	}
	label := firstPresent(summary, "trainingEffectLabel")
	if label == nil {
		label = firstPresent(record, "trainingEffectLabel", "aerobicTrainingEffectMessage")
	}
	recovery := firstPresent(summary, "recoveryTime")
	if recovery == nil {
		recovery = firstPresent(record, "recoveryTime")
	}
	load := firstPresent(summary, "activityTrainingLoad")
	if load == nil {
		load = firstPresent(record, "activityTrainingLoad")
	}
	performance := firstPresent(summary, "performanceCondition")
	if performance == nil {
		performance = firstPresent(record, "performanceCondition")
	}

	var recoveryHours any
	if hours := minutesToHours(recovery); hours != nil {
		recoveryHours = *hours
	}

	return pruneNil(map[string]any{
		"activity_id":           activityID,
		"source":                source,
		"training_effect":       aerobic,
		"aerobic_effect":        aerobic,
		"anaerobic_effect":      anaerobic,
		"training_effect_label": label,
		"recovery_time_hours":   recoveryHours,
		"training_load":         load,
		"performance_condition": performance,
	})
}

func pruneNilDeep(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, item := range v {
			if item == nil {
				continue
			}
			out[key] = pruneNilDeep(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = pruneNilDeep(item)
		}
		return out
	}
	return value
}

func intIfDigits(value *string) any {
	if value == nil {
		return nil
	}
	s := *value
	if s == "" {
		return s
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return s
		}
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return s
}

func valueOrDefault(m map[string]any, key string, fallback any) any {
	if value, ok := m[key]; ok {
		return value
	}
	return fallback
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func dateString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// --- tool handlers ---

func getProgressSummaryBetweenDates(ctx context.Context, s *Session, in progressSummaryInput) (any, error) {
	summaryData, err := s.Client.GetProgressSummaryBetweenDates(ctx, in.StartDate, in.EndDate, in.Metric)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving progress summary: %w", err)
	}
	if !truthy(summaryData) {
		return fmt.Sprintf("No progress summary found for %s between %s and %s.", in.Metric, in.StartDate, in.EndDate), nil
	}
	list, ok := summaryData.([]any)
	if !ok || len(list) == 0 {
		return "Unexpected response format from API", nil
	}

	data := asMap(list[0])
	curated := map[string]any{
		"metric":                 in.Metric,
		"start_date":             in.StartDate,
		"end_date":               in.EndDate,
		"stats_by_activity_type": map[string]any{},
	}
	byType := curated["stats_by_activity_type"].(map[string]any)
	count := 0
	for activityType, raw := range asMap(data["stats"]) {
		activityStats := asMap(raw)
		metricData := asMap(activityStats[in.Metric])
		metricCount, _ := numberOf(metricData["count"])
		if !truthy(metricData) || metricCount <= 0 {
			continue
		}
		byType[activityType] = map[string]any{
			"count": metricData["count"],
			"sum":   progressMetricValue(in.Metric, metricData["sum"]),
			"avg":   progressMetricValue(in.Metric, metricData["avg"]),
			"min":   progressMetricValue(in.Metric, metricData["min"]),
			"max":   progressMetricValue(in.Metric, metricData["max"]),
		}
		count += int(metricCount)
	}
	curated["count_of_activities"] = count
	return pruneNil(curated), nil
}

func getHillScore(ctx context.Context, s *Session, in trRangeInput) (any, error) {
	data, err := s.Client.GetHillScore(ctx, in.StartDate, in.EndDate)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving hill score data: %w", err)
	}
	if !truthy(data) {
		return fmt.Sprintf("No hill score data found between %s and %s.", in.StartDate, in.EndDate), nil
	}

	record := asMap(data)
	periodAvgScore := asMap(record["periodAvgScore"])
	var periodAvg any
	if len(periodAvgScore) > 0 {
		periodAvg = periodAvgScore[sortedMapKeys(periodAvgScore)[0]]
	}

	dailyScores := []map[string]any{}
	for _, item := range asList(record["hillScoreDTOList"]) {
		if score, ok := item.(map[string]any); ok {
			dailyScores = append(dailyScores, score)
		}
	}

	latestScore := map[string]any{}
	if len(dailyScores) > 0 {
		latestScore = dailyScores[0]
		latestDate := dateString(latestScore["calendarDate"])
		for _, score := range dailyScores[1:] {
			if date := dateString(score["calendarDate"]); date > latestDate {
				latestScore = score
				latestDate = date
			}
		}
	}

	daily := make([]any, 0, len(dailyScores))
	for _, score := range dailyScores {
		daily = append(daily, map[string]any{
			"date":      score["calendarDate"],
			"overall":   score["overallScore"],
			"strength":  score["strengthScore"],
			"endurance": score["enduranceScore"],
		})
	}

	curated := map[string]any{
		"start_date":               in.StartDate,
		"end_date":                 in.EndDate,
		"period_avg_score":         periodAvg,
		"max_score":                record["maxScore"],
		"latest_date":              latestScore["calendarDate"],
		"latest_overall_score":     latestScore["overallScore"],
		"latest_strength_score":    latestScore["strengthScore"],
		"latest_endurance_score":   latestScore["enduranceScore"],
		"latest_classification_id": latestScore["hillScoreClassificationId"],
		"daily_scores":             daily,
	}
	return pruneNil(curated), nil
}

func getEnduranceScore(ctx context.Context, s *Session, in trRangeInput) (any, error) {
	data, err := s.Client.GetEnduranceScore(ctx, in.StartDate, in.EndDate)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving endurance score data: %w", err)
	}
	if !truthy(data) {
		return fmt.Sprintf("No endurance score data found between %s and %s.", in.StartDate, in.EndDate), nil
	}

	record := asMap(data)
	mapping := trainingActivityTypes(ctx, s.Client)
	scoreDTO := asMap(record["enduranceScoreDTO"])

	classificationLabels := map[int]string{
		1: "recreational", 2: "intermediate", 3: "trained", 4: "well_trained",
		5: "expert", 6: "superior", 7: "elite",
	}
	classificationID := scoreDTO["classification"]
	var classification any
	if classificationID != nil {
		label := "level_" + numberString(classificationID)
		if id, ok := numberOf(classificationID); ok {
			if mapped, exists := classificationLabels[int(id)]; exists {
				label = mapped
			}
		}
		classification = label
	}

	var contributors any
	rawContributors := asList(scoreDTO["contributors"])
	if len(rawContributors) > 0 {
		mapped := make([]any, 0, len(rawContributors))
		for _, item := range rawContributors {
			mapped = append(mapped, mapContributor(asMap(item), mapping))
		}
		contributors = mapped
	}

	weeklyBreakdown := []any{}
	groupMap := asMap(record["groupMap"])
	for _, weekDate := range sortedMapKeys(groupMap) {
		weekData := asMap(groupMap[weekDate])
		weekContributors := []any{}
		for _, item := range asList(weekData["enduranceContributorDTOList"]) {
			weekContributors = append(weekContributors, mapContributor(asMap(item), mapping))
		}
		var weekContributorsOut any
		if len(weekContributors) > 0 {
			weekContributorsOut = weekContributors
		}
		weeklyBreakdown = append(weeklyBreakdown, map[string]any{
			"week_start":   weekDate,
			"avg_score":    weekData["groupAverage"],
			"max_score":    weekData["groupMax"],
			"contributors": weekContributorsOut,
		})
	}

	var thresholds any
	if truthy(scoreDTO["classificationLowerLimitTrained"]) {
		thresholds = map[string]any{
			"intermediate": scoreDTO["classificationLowerLimitIntermediate"],
			"trained":      scoreDTO["classificationLowerLimitTrained"],
			"well_trained": scoreDTO["classificationLowerLimitWellTrained"],
			"expert":       scoreDTO["classificationLowerLimitExpert"],
			"superior":     scoreDTO["classificationLowerLimitSuperior"],
			"elite":        scoreDTO["classificationLowerLimitElite"],
		}
	}

	var weeklyOut any
	if len(weeklyBreakdown) > 0 {
		weeklyOut = weeklyBreakdown
	}

	curated := map[string]any{
		"start_date":        in.StartDate,
		"end_date":          in.EndDate,
		"period_avg_score":  record["avg"],
		"period_max_score":  record["max"],
		"current_score":     scoreDTO["overallScore"],
		"current_date":      scoreDTO["calendarDate"],
		"classification":    classification,
		"classification_id": classificationID,
		"thresholds":        thresholds,
		"contributors":      contributors,
		"weekly_breakdown":  weeklyOut,
	}
	return pruneNilDeep(curated), nil
}

func getTrainingEffect(ctx context.Context, s *Session, in trainingEffectInput) (any, error) {
	activityID, err := activityIDValue(in.ActivityID)
	if err != nil {
		return fmt.Sprintf("Invalid activity ID: %v", in.ActivityID), nil
	}

	source := "activity_details"
	var detailErr error
	activity, err := s.Client.GetActivity(ctx, activityID)
	if err != nil {
		if !isForbiddenError(err) {
			return nil, fmt.Errorf("Error retrieving training effect data: %w", err)
		}
		activity = nil
		detailErr = err
	}

	if !truthy(activity) {
		source = "activity_list"
		found, lookupErr := lookupActivityInList(ctx, s.Client, activityID, 100)
		if lookupErr != nil {
			return nil, fmt.Errorf("Error retrieving training effect data: %w", lookupErr)
		}
		activity = found
		if !truthy(activity) {
			if detailErr != nil {
				return fmt.Sprintf(
					"Error retrieving training effect data: Garmin returned HTTP 403 for activity %d details, and the activity was not found in recent activity search.",
					activityID,
				), nil
			}
			return fmt.Sprintf("No activity found with ID %d.", activityID), nil
		}
	}
	return curateTrainingEffect(activity, activityID, source), nil
}

func getHrvData(ctx context.Context, s *Session, in hrvDataInput) (any, error) {
	data, err := s.Client.GetHRVData(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving HRV data: %w", err)
	}
	if !truthy(data) {
		return fmt.Sprintf("No HRV data found for %s.", in.Date), nil
	}

	record := asMap(data)
	summary := asMap(record["hrvSummary"])
	baseline := asMap(summary["baseline"])

	date := any(in.Date)
	if value := summary["calendarDate"]; truthy(value) {
		date = value
	}
	curated := map[string]any{
		"date":                        date,
		"last_night_avg_hrv_ms":       summary["lastNightAvg"],
		"last_night_5min_high_hrv_ms": summary["lastNight5MinHigh"],
		"weekly_avg_hrv_ms":           summary["weeklyAvg"],
		"baseline_balanced_low_ms":    baseline["balancedLow"],
		"baseline_balanced_upper_ms":  baseline["balancedUpper"],
		"baseline_low_upper_ms":       baseline["lowUpper"],
		"status":                      summary["status"],
		"feedback":                    summary["feedbackPhrase"],
		"sleep_start":                 record["sleepStartTimestampLocal"],
		"sleep_end":                   record["sleepEndTimestampLocal"],
	}

	if in.ReturnTimeseries {
		readings := []any{}
		for _, item := range asList(record["hrvReadings"]) {
			reading, ok := item.(map[string]any)
			if !ok {
				continue
			}
			readings = append(readings, map[string]any{
				"time":   reading["readingTimeLocal"],
				"hrv_ms": reading["hrvValue"],
			})
		}
		curated["hrv_readings"] = readings
		curated["readings_count"] = len(readings)
	}
	return pruneNil(curated), nil
}

func getFitnessageData(ctx context.Context, s *Session, in fitnessAgeInput) (any, error) {
	fitnessAge, err := s.Client.GetFitnessAgeData(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving fitness age data: %w", err)
	}
	if !truthy(fitnessAge) {
		return fmt.Sprintf("No fitness age data found for %s.", in.Date), nil
	}

	record := asMap(fitnessAge)
	chronologicalAge := record["chronologicalAge"]
	fitAge := record["fitnessAge"]
	var ageDiff any
	if chronologicalAge != nil && fitAge != nil {
		chrono, okChrono := numberOf(chronologicalAge)
		fit, okFit := numberOf(fitAge)
		if okChrono && okFit {
			ageDiff = roundTo(chrono-fit, 1)
		}
	}

	roundedAge := func(value any) any {
		if !truthy(value) {
			return nil
		}
		if f, ok := numberOf(value); ok {
			return roundTo(f, 1)
		}
		return nil
	}

	curated := map[string]any{
		"date":                         in.Date,
		"fitness_age_years":            roundedAge(fitAge),
		"chronological_age_years":      chronologicalAge,
		"age_difference_years":         ageDiff,
		"achievable_fitness_age_years": roundedAge(record["achievableFitnessAge"]),
		"previous_fitness_age_years":   roundedAge(record["previousFitnessAge"]),
		"last_updated":                 record["lastUpdated"],
	}

	if in.Details {
		components := map[string]any{}
		for name, raw := range asMap(record["components"]) {
			component, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			info := map[string]any{"value": component["value"]}
			if component["targetValue"] != nil {
				info["target"] = component["targetValue"]
			}
			if component["improvementValue"] != nil {
				info["improvement_needed"] = component["improvementValue"]
			}
			if component["potentialAge"] != nil {
				if f, ok := numberOf(component["potentialAge"]); ok {
					info["potential_age_if_improved"] = roundTo(f, 1)
				}
			}
			if component["priority"] != nil {
				info["priority"] = component["priority"]
			}
			if component["stale"] != nil {
				info["stale"] = component["stale"]
			}
			if component["lastMeasurementDate"] != nil {
				info["last_measurement"] = component["lastMeasurementDate"]
			}
			components[name] = info
		}
		if len(components) > 0 {
			curated["components"] = components
		}
	}
	return pruneNil(curated), nil
}

func getTrainingStatus(ctx context.Context, s *Session, in trDateInput) (any, error) {
	payload, err := s.Client.GetTrainingStatus(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving training status data: %w", err)
	}
	status := asMap(payload)
	if len(status) == 0 {
		return fmt.Sprintf("No training status data found for %s.", in.Date), nil
	}

	recentStatus := asMap(status["mostRecentTrainingStatus"])
	latestData := asMap(recentStatus["latestTrainingStatusData"])

	devices, err := s.Client.GetDevices(ctx)
	if err != nil {
		devices = nil
	}
	deviceData, reportingID, reportingName, isPrimaryTracker := selectTrainingDevice(latestData, devices)
	acwrData := asMap(deviceData["acuteTrainingLoadDTO"])

	mostRecentVO2 := asMap(status["mostRecentVO2Max"])
	vo2Data := asMap(mostRecentVO2["generic"])
	cyclingVO2Data := asMap(mostRecentVO2["cycling"])

	loadBalance := asMap(status["mostRecentTrainingLoadBalance"])
	loadMap := asMap(loadBalance["metricsTrainingLoadBalanceDTOMap"])
	loadData := map[string]any{}
	var loadDeviceID *string
	if reportingID == nil {
		loadData, loadDeviceID, _, _ = selectTrainingDevice(loadMap, devices)
	} else {
		for _, key := range sortedMapKeys(loadMap) {
			data := asMap(loadMap[key])
			if len(data) == 0 {
				continue
			}
			candidateID := deviceIDOf(data["deviceId"])
			if candidateID == nil {
				candidateID = deviceIDOf(key)
			}
			if candidateID != nil && *candidateID == *reportingID {
				loadData = data
				loadDeviceID = candidateID
				break
			}
		}
	}

	curated := map[string]any{
		"date":                      valueOrDefault(deviceData, "calendarDate", in.Date),
		"reporting_device_id":       intIfDigits(reportingID),
		"reporting_device_name":     stringPtr(reportingName),
		"load_device_id":            intIfDigits(loadDeviceID),
		"training_status":           deviceData["trainingStatus"],
		"training_status_feedback":  deviceData["trainingStatusFeedbackPhrase"],
		"sport":                     deviceData["sport"],
		"fitness_trend":             deviceData["fitnessTrend"],
		"vo2_max":                   vo2Data["vo2MaxValue"],
		"vo2_max_precise":           vo2Data["vo2MaxPreciseValue"],
		"cycling_vo2_max":           cyclingVO2Data["vo2MaxValue"],
		"cycling_vo2_max_precise":   cyclingVO2Data["vo2MaxPreciseValue"],
		"monthly_load_aerobic_low":  loadData["monthlyLoadAerobicLow"],
		"monthly_load_aerobic_high": loadData["monthlyLoadAerobicHigh"],
		"monthly_load_anaerobic":    loadData["monthlyLoadAnaerobic"],
		"training_balance_feedback": loadData["trainingBalanceFeedbackPhrase"],
	}

	if isPrimaryTracker == nil || *isPrimaryTracker {
		curated["acute_load"] = acwrData["dailyTrainingLoadAcute"]
		curated["chronic_load"] = acwrData["dailyTrainingLoadChronic"]
		curated["load_ratio"] = acwrData["dailyAcuteChronicWorkloadRatio"]
		curated["acwr_status"] = acwrData["acwrStatus"]
		curated["acwr_percent"] = acwrData["acwrPercent"]
		curated["optimal_chronic_load_min"] = acwrData["minTrainingLoadChronic"]
		curated["optimal_chronic_load_max"] = acwrData["maxTrainingLoadChronic"]
	} else {
		var acuteLoad *float64
		var reason *string
		readiness, readinessErr := s.Client.GetTrainingReadiness(ctx, in.Date)
		if readinessErr != nil {
			value := "readiness_unavailable"
			reason = &value
		} else {
			acuteLoad, reason = trackerAcuteLoad(readiness, in.Date, primaryDeviceID(devices))
		}
		if acuteLoad != nil {
			curated["acute_load"] = *acuteLoad
			curated["acute_load_source"] = "training_readiness"
		} else if reason != nil {
			curated["acute_load_source"] = "unavailable"
			curated["acute_load_unavailable_reason"] = *reason
		}
	}

	if len(loadData) == 0 {
		hasLoad := false
		for _, raw := range loadMap {
			if data, ok := raw.(map[string]any); ok && len(data) > 0 {
				hasLoad = true
				break
			}
		}
		if hasLoad {
			curated["load_source"] = "unavailable"
			curated["load_unavailable_reason"] = "load_device_mismatch"
		}
	}
	return pruneNil(curated), nil
}

func stringPtr(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func getCyclingFTP(ctx context.Context, s *Session, _ cyclingFTPInput) (any, error) {
	ftpData, err := s.Client.GetCyclingFTP(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving cycling FTP data: %w", err)
	}
	if list, ok := ftpData.([]any); ok {
		ftpData = nil
		for _, item := range list {
			if m, ok := item.(map[string]any); ok {
				ftpData = m
				break
			}
		}
	}
	if !truthy(ftpData) {
		return "No cycling FTP data found", nil
	}
	record := asMap(ftpData)
	return pruneNil(map[string]any{
		"sport":                            record["sport"],
		"functional_threshold_power_watts": record["functionalThresholdPower"],
		"calendar_date":                    record["calendarDate"],
		"is_stale":                         record["isStale"],
		"biometric_source_type":            record["biometricSourceType"],
	}), nil
}

func getLactateThreshold(ctx context.Context, s *Session, in lactateThresholdInput) (any, error) {
	startDate := ""
	if in.StartDate != nil {
		startDate = *in.StartDate
	}
	endDate := ""
	if in.EndDate != nil {
		endDate = *in.EndDate
	}
	if (startDate != "") != (endDate != "") {
		return "Provide both start_date and end_date for lactate threshold history, or omit both for the latest values.", nil
	}

	var threshold any
	var err error
	if startDate != "" && endDate != "" {
		threshold, err = s.Client.GetLactateThresholdRange(ctx, startDate, endDate, "")
	} else {
		threshold, err = s.Client.GetLactateThresholdLatest(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("Error retrieving lactate threshold data: %w", err)
	}
	if !truthy(threshold) {
		if startDate != "" && endDate != "" {
			return fmt.Sprintf("No lactate threshold data found between %s and %s", startDate, endDate), nil
		}
		return "No lactate threshold data found", nil
	}

	record := asMap(threshold)
	var curated map[string]any
	if startDate != "" && endDate != "" {
		curated = map[string]any{
			"start_date": startDate,
			"end_date":   endDate,
		}
		if speedHistory := asList(record["speed"]); len(speedHistory) > 0 {
			entries := make([]any, 0, len(speedHistory))
			for _, item := range speedHistory {
				entry := asMap(item)
				entries = append(entries, map[string]any{
					"date":      entry["from"],
					"speed_mps": lactateThresholdSpeedMPS(entry["value"]),
					"series":    entry["series"],
				})
			}
			curated["speed_history"] = entries
		}
		hrHistory := asList(record["heart_rate"])
		if len(hrHistory) == 0 {
			hrHistory = asList(record["heartRate"])
		}
		if len(hrHistory) > 0 {
			entries := make([]any, 0, len(hrHistory))
			for _, item := range hrHistory {
				entry := asMap(item)
				entries = append(entries, map[string]any{
					"date":           entry["from"],
					"heart_rate_bpm": entry["value"],
					"series":         entry["series"],
				})
			}
			curated["heart_rate_history"] = entries
		}
		if powerHistory := asList(record["power"]); len(powerHistory) > 0 {
			entries := make([]any, 0, len(powerHistory))
			for _, item := range powerHistory {
				entry := asMap(item)
				entries = append(entries, map[string]any{
					"date":        entry["from"],
					"power_watts": entry["value"],
					"series":      entry["series"],
				})
			}
			curated["power_history"] = entries
		}
	} else {
		speedAndHeartRate := asMap(record["speed_and_heart_rate"])
		power := asMap(record["power"])
		curated = map[string]any{
			"lactate_threshold_speed_mps":      lactateThresholdSpeedMPS(speedAndHeartRate["speed"]),
			"lactate_threshold_heart_rate_bpm": speedAndHeartRate["heartRate"],
			"heart_rate_cycling_bpm":           speedAndHeartRate["heartRateCycling"],
			"speed_hr_date":                    speedAndHeartRate["calendarDate"],
			"functional_threshold_power_watts": power["functionalThresholdPower"],
			"weight_kg":                        power["weight"],
			"power_to_weight":                  power["powerToWeight"],
			"sport":                            power["sport"],
			"power_date":                       power["calendarDate"],
			"is_stale":                         power["isStale"],
		}
	}
	return pruneNil(curated), nil
}

func requestReload(ctx context.Context, s *Session, in trDateInput) (any, error) {
	result, err := s.Client.RequestReload(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error requesting data reload: %w", err)
	}
	return result, nil
}

func getTrainingLoadTrend(ctx context.Context, s *Session, in trRangeInput) (any, error) {
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

	devices, err := s.Client.GetDevices(ctx)
	if err != nil {
		if isFatalTrendError(err) {
			if errors.Is(err, garmin.ErrSessionExpired) {
				return nil, err
			}
			return fmt.Sprintf("Error retrieving training load device data: %v", err), nil
		}
		devices = nil
	}

	trend := []any{}
	for current := start; !current.After(end); current = current.AddDate(0, 0, 1) {
		dateStr := current.Format(dateLayout)
		payload, err := s.Client.GetTrainingStatus(ctx, dateStr)
		if err != nil {
			if errors.Is(err, garmin.ErrSessionExpired) {
				return nil, err
			}
			return fmt.Sprintf("Error retrieving training load data for %s: %v", dateStr, err), nil
		}
		if !truthy(payload) {
			continue
		}

		record := asMap(payload)
		recentStatus := asMap(record["mostRecentTrainingStatus"])
		latestData := asMap(recentStatus["latestTrainingStatusData"])
		statusData, reportingID, reportingName, isPrimary := selectTrainingDevice(latestData, devices)
		atlDTO := asMap(statusData["acuteTrainingLoadDTO"])
		mostRecentVO2 := asMap(record["mostRecentVO2Max"])
		vo2Data := asMap(mostRecentVO2["generic"])

		entry := map[string]any{"date": dateStr}
		if reportingID != nil {
			entry["reporting_device_id"] = intIfDigits(reportingID)
		}
		if reportingName != nil {
			entry["reporting_device_name"] = *reportingName
		}

		atl := atlDTO["dailyTrainingLoadAcute"]
		ctl := atlDTO["dailyTrainingLoadChronic"]
		acwr := atlDTO["dailyAcuteChronicWorkloadRatio"]
		if isPrimary != nil && !*isPrimary {
			var readinessReason *string
			readiness, readinessErr := s.Client.GetTrainingReadiness(ctx, dateStr)
			if readinessErr != nil {
				if isFatalTrendError(readinessErr) {
					return fmt.Sprintf("Error retrieving training load data for %s: %v", dateStr, readinessErr), nil
				}
				atl = nil
				reason := "readiness_unavailable"
				readinessReason = &reason
				entry["readiness_error"] = readinessErr.Error()
			} else {
				acute, reason := trackerAcuteLoad(readiness, dateStr, primaryDeviceID(devices))
				if acute != nil {
					atl = *acute
				} else {
					atl = nil
				}
				readinessReason = reason
			}
			ctl = nil
			acwr = nil
			if atl != nil {
				entry["atl_source"] = "training_readiness"
			} else {
				entry["atl_source"] = "unavailable"
				if readinessReason != nil {
					entry["atl_unavailable_reason"] = *readinessReason
				}
			}
		}

		if atl != nil {
			if value, ok := numberOf(atl); ok {
				entry["atl"] = roundTo(value, 1)
			}
		}
		if ctl != nil {
			if value, ok := numberOf(ctl); ok {
				entry["ctl"] = roundTo(value, 1)
			}
		}
		if atl != nil && ctl != nil {
			if atlValue, okATL := numberOf(atl); okATL {
				if ctlValue, okCTL := numberOf(ctl); okCTL {
					entry["tsb"] = roundTo(ctlValue-atlValue, 1)
				}
			}
		}
		if acwr != nil {
			if value, ok := numberOf(acwr); ok {
				entry["acwr"] = roundTo(value, 2)
			}
		}
		primary := isPrimary == nil || *isPrimary
		if acwrStatus := atlDTO["acwrStatus"]; truthy(acwrStatus) && primary {
			entry["acwr_status"] = acwrStatus
		}
		if acwrPercent := atlDTO["acwrPercent"]; acwrPercent != nil && primary {
			entry["acwr_percent"] = acwrPercent
		}
		if chronicMin := atlDTO["minTrainingLoadChronic"]; chronicMin != nil && primary {
			if value, ok := numberOf(chronicMin); ok {
				entry["optimal_chronic_load_min"] = roundTo(value, 1)
			}
		}
		if chronicMax := atlDTO["maxTrainingLoadChronic"]; chronicMax != nil && primary {
			if value, ok := numberOf(chronicMax); ok {
				entry["optimal_chronic_load_max"] = roundTo(value, 1)
			}
		}
		if phrase := statusData["trainingStatusFeedbackPhrase"]; truthy(phrase) {
			entry["training_status"] = phrase
		}
		if code := statusData["trainingStatus"]; code != nil {
			entry["training_status_code"] = code
		}
		if fitnessTrend := statusData["fitnessTrend"]; fitnessTrend != nil {
			entry["fitness_trend"] = fitnessTrend
		}
		if vo2 := vo2Data["vo2MaxValue"]; vo2 != nil {
			if value, ok := numberOf(vo2); ok {
				entry["vo2_max"] = roundTo(value, 1)
			}
		}
		if len(entry) > 1 {
			trend = append(trend, entry)
		}
	}

	if len(trend) == 0 {
		return fmt.Sprintf("No training load data found between %s and %s.", in.StartDate, in.EndDate), nil
	}
	return map[string]any{
		"start_date":     in.StartDate,
		"end_date":       in.EndDate,
		"days_with_data": len(trend),
		"trend":          trend,
	}, nil
}

func getTrainingLoadBalance(ctx context.Context, s *Session, in trDateInput) (any, error) {
	data, err := s.Client.GetTrainingStatus(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving training load balance: %w", err)
	}
	if !truthy(data) {
		return fmt.Sprintf("No training load balance data found for %s.", in.Date), nil
	}

	record := asMap(data)
	loadBalance := asMap(record["mostRecentTrainingLoadBalance"])
	loadMap := asMap(loadBalance["metricsTrainingLoadBalanceDTOMap"])

	devices, err := s.Client.GetDevices(ctx)
	if err != nil {
		devices = nil
	}
	loadData, loadDeviceID, loadDeviceName, _ := selectTrainingDevice(loadMap, devices)
	if len(loadData) == 0 {
		return fmt.Sprintf("No training load balance data found for %s.", in.Date), nil
	}

	band := func(loadKey, minKey, maxKey string) map[string]any {
		load := loadData[loadKey]
		targetMin := loadData[minKey]
		targetMax := loadData[maxKey]
		if load == nil && targetMin == nil && targetMax == nil {
			return nil
		}
		result := map[string]any{}
		if load != nil {
			if value, ok := numberOf(load); ok {
				result["load"] = roundTo(value, 1)
			}
		}
		if targetMin != nil {
			result["target_min"] = targetMin
		}
		if targetMax != nil {
			result["target_max"] = targetMax
		}
		if load != nil && targetMin != nil && targetMax != nil {
			loadValue, okLoad := numberOf(load)
			minValue, okMin := numberOf(targetMin)
			maxValue, okMax := numberOf(targetMax)
			if okLoad && okMin && okMax {
				switch {
				case loadValue < minValue:
					result["status"] = "below"
				case loadValue > maxValue:
					result["status"] = "above"
				default:
					result["status"] = "within"
				}
			}
		}
		return result
	}

	result := map[string]any{
		"date": valueOrDefault(loadData, "calendarDate", in.Date),
	}
	if loadDeviceID != nil {
		result["load_device_id"] = intIfDigits(loadDeviceID)
	}
	if loadDeviceName != nil {
		result["load_device_name"] = *loadDeviceName
	}
	if feedback := loadData["trainingBalanceFeedbackPhrase"]; truthy(feedback) {
		result["feedback"] = feedback
	}
	if aerobicLow := band("monthlyLoadAerobicLow", "monthlyLoadAerobicLowTargetMin", "monthlyLoadAerobicLowTargetMax"); aerobicLow != nil {
		result["aerobic_low"] = aerobicLow
	}
	if aerobicHigh := band("monthlyLoadAerobicHigh", "monthlyLoadAerobicHighTargetMin", "monthlyLoadAerobicHighTargetMax"); aerobicHigh != nil {
		result["aerobic_high"] = aerobicHigh
	}
	if anaerobic := band("monthlyLoadAnaerobic", "monthlyLoadAnaerobicTargetMin", "monthlyLoadAnaerobicTargetMax"); anaerobic != nil {
		result["anaerobic"] = anaerobic
	}
	return result, nil
}

func getHrvTrend(ctx context.Context, s *Session, in trRangeInput) (any, error) {
	const maxDays = 30
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

	trend := []any{}
	for current := start; !current.After(end); current = current.AddDate(0, 0, 1) {
		dateStr := current.Format(dateLayout)
		data, err := s.Client.GetHRVData(ctx, dateStr)
		if err != nil {
			return fmt.Sprintf("Error retrieving HRV data for %s: %v", dateStr, err), nil
		}
		if !truthy(data) {
			continue
		}
		summary := asMap(asMap(data)["hrvSummary"])
		entry := map[string]any{"date": dateStr}
		if value := summary["lastNightAvg"]; value != nil {
			if f, ok := numberOf(value); ok {
				entry["last_night_avg_hrv_ms"] = roundTo(f, 1)
			}
		}
		if value := summary["weeklyAvg"]; value != nil {
			if f, ok := numberOf(value); ok {
				entry["weekly_avg_hrv_ms"] = roundTo(f, 1)
			}
		}
		if value := summary["lastNight5MinHigh"]; value != nil {
			if f, ok := numberOf(value); ok {
				entry["last_night_5min_high_hrv_ms"] = roundTo(f, 1)
			}
		}
		if status := summary["status"]; truthy(status) {
			entry["status"] = status
		}
		if feedback := summary["feedbackPhrase"]; truthy(feedback) {
			entry["feedback"] = feedback
		}
		if len(entry) > 1 {
			trend = append(trend, entry)
		}
	}

	if len(trend) == 0 {
		return fmt.Sprintf("No HRV data found between %s and %s.", in.StartDate, in.EndDate), nil
	}

	values := []float64{}
	for _, item := range trend {
		if value, ok := numberOf(asMap(item)["last_night_avg_hrv_ms"]); ok {
			values = append(values, value)
		}
	}
	var periodAvg any
	if len(values) > 0 {
		sum := 0.0
		for _, value := range values {
			sum += value
		}
		periodAvg = roundTo(sum/float64(len(values)), 1)
	}

	return map[string]any{
		"start_date":        in.StartDate,
		"end_date":          in.EndDate,
		"days_with_data":    len(trend),
		"period_avg_hrv_ms": periodAvg,
		"trend":             trend,
	}, nil
}

func getVo2maxTrend(ctx context.Context, s *Session, in trRangeInput) (any, error) {
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

	histories := map[string][]map[string]any{"running": {}, "cycling": {}}
	requestErrors := []any{}

	addMeasurements := func(measurementDate string, measurements map[string]float64, source string) {
		for sport, vo2 := range measurements {
			exists := false
			for _, entry := range histories[sport] {
				if entry["date"] == measurementDate {
					exists = true
					break
				}
			}
			if !exists {
				histories[sport] = append(histories[sport], map[string]any{
					"date":    measurementDate,
					"vo2_max": roundTo(vo2, 1),
					"source":  source,
				})
			}
		}
	}

	rangeMeasurements := map[string]map[string]float64{}
	rangeData, rangeErr := s.Client.GetMaxMetricsRange(ctx, in.StartDate, in.EndDate)
	if rangeErr != nil {
		if isFatalTrendError(rangeErr) {
			return fmt.Sprintf("Error retrieving VO2 max data: %v", rangeErr), nil
		}
		requestErrors = append(requestErrors, map[string]any{
			"source": "get_max_metrics",
			"error":  rangeErr.Error(),
		})
	} else {
		rangeMeasurements = extractDatedVO2Measurements(rangeData)
	}

	for current := start; !current.After(end); current = current.AddDate(0, 0, 1) {
		dateStr := current.Format(dateLayout)
		source := ""
		measurements := rangeMeasurements[dateStr]
		if measurements == nil {
			measurements = map[string]float64{}
		}
		if len(measurements) > 0 {
			source = "get_max_metrics"
		} else {
			// The Go client always supports the range endpoint, so unlike
			// garminconnect's daily fallback, only training status is queried
			// for days the range response leaves empty.
			payload, methodErr := s.Client.GetTrainingStatus(ctx, dateStr)
			if methodErr != nil {
				if isFatalTrendError(methodErr) {
					return fmt.Sprintf("Error retrieving VO2 max data for %s: %v", dateStr, methodErr), nil
				}
				requestErrors = append(requestErrors, map[string]any{
					"source": "get_training_status",
					"date":   dateStr,
					"error":  methodErr.Error(),
				})
			} else {
				measurements = extractVO2Measurements(payload)
				dated := extractDatedVO2Measurements(payload)
				if len(dated) > 0 {
					datedSports := map[string]bool{}
					for _, measurementDate := range sortedFloatMapKeys(dated) {
						values := dated[measurementDate]
						for sport := range values {
							datedSports[sport] = true
						}
						if measurementDate <= dateStr {
							addMeasurements(measurementDate, values, "get_training_status")
						}
					}
					filtered := map[string]float64{}
					for sport, value := range measurements {
						if !datedSports[sport] {
							filtered[sport] = value
						}
					}
					measurements = filtered
				}
				if len(measurements) > 0 {
					source = "get_training_status"
				}
			}
		}
		addMeasurements(dateStr, measurements, source)
	}

	selectedSport := ""
	if len(histories["running"]) > 0 || len(histories["cycling"]) > 0 {
		if len(histories["running"]) >= len(histories["cycling"]) {
			selectedSport = "running"
		} else {
			selectedSport = "cycling"
		}
	}

	var history []map[string]any
	if selectedSport != "" {
		history = histories[selectedSport]
	}
	series := buildVO2TrendSeries(history, end)
	trend := []any{}
	for _, entry := range series {
		if dateString(entry["date"]) >= in.StartDate {
			trend = append(trend, entry)
		}
	}

	var currentEstimate float64
	currentSport := ""
	hasEstimate := false
	if len(trend) == 0 {
		profile, profileErr := s.Client.GetUserProfile(ctx)
		if profileErr != nil {
			if isFatalTrendError(profileErr) {
				return fmt.Sprintf("Error retrieving VO2 max data: %v", profileErr), nil
			}
			requestErrors = append(requestErrors, map[string]any{
				"source": "get_user_profile",
				"error":  profileErr.Error(),
			})
		}
		profileMeasurements := extractVO2Measurements(profile)
		if len(profileMeasurements) > 0 {
			currentSport = "cycling"
			if _, ok := profileMeasurements["running"]; ok {
				currentSport = "running"
			}
			currentEstimate = profileMeasurements[currentSport]
			hasEstimate = true
		} else {
			if len(requestErrors) > 0 {
				last := asMap(requestErrors[len(requestErrors)-1])
				return fmt.Sprintf("Error retrieving VO2 max data: %v", last["error"]), nil
			}
			return fmt.Sprintf("No VO2 max data found between %s and %s.", in.StartDate, in.EndDate), nil
		}
	}

	var firstVO2 any
	var latestVO2 any
	if len(trend) > 0 {
		firstVO2 = asMap(trend[0])["vo2_max"]
		latestVO2 = asMap(trend[len(trend)-1])["vo2_max"]
	}
	var change any
	if firstVO2 != nil && latestVO2 != nil {
		firstValue, okFirst := numberOf(firstVO2)
		latestValue, okLatest := numberOf(latestVO2)
		if okFirst && okLatest {
			change = roundTo(latestValue-firstValue, 1)
		}
	}

	response := map[string]any{
		"start_date":     in.StartDate,
		"end_date":       in.EndDate,
		"data_points":    len(trend),
		"first_vo2_max":  firstVO2,
		"latest_vo2_max": latestVO2,
		"change":         change,
		"trend":          trend,
	}
	if selectedSport != "" {
		response["sport"] = selectedSport
	}
	if len(requestErrors) > 0 {
		response["request_errors"] = requestErrors
	}
	if hasEstimate {
		response["current_vo2_max_estimate"] = map[string]any{
			"vo2_max": roundTo(currentEstimate, 1),
			"sport":   currentSport,
			"source":  "get_user_profile",
		}
		response["note"] = "Historical VO2 max values were not available from Garmin; " +
			"returning the current profile estimate separately."
	}
	return response, nil
}

func sortedFloatMapKeys(m map[string]map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func getRespirationTrend(ctx context.Context, s *Session, in trRangeInput) (any, error) {
	const maxDays = 30
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

	trend := []any{}
	for current := start; !current.After(end); current = current.AddDate(0, 0, 1) {
		dateStr := current.Format(dateLayout)
		data, err := s.Client.GetRespirationData(ctx, dateStr)
		if err != nil {
			return fmt.Sprintf("Error retrieving respiration data for %s: %v", dateStr, err), nil
		}
		if !truthy(data) {
			continue
		}
		record := asMap(data)
		entry := map[string]any{"date": dateStr}
		fields := []struct {
			key  string
			from string
		}{
			{"avg_waking_breaths_per_min", "avgWakingRespirationValue"},
			{"avg_sleep_breaths_per_min", "avgSleepRespirationValue"},
			{"highest_breaths_per_min", "highestRespirationValue"},
			{"lowest_breaths_per_min", "lowestRespirationValue"},
		}
		for _, field := range fields {
			if value := record[field.from]; value != nil {
				if f, ok := numberOf(value); ok {
					entry[field.key] = roundTo(f, 1)
				}
			}
		}
		if len(entry) > 1 {
			trend = append(trend, entry)
		}
	}

	if len(trend) == 0 {
		return fmt.Sprintf("No respiration data found between %s and %s.", in.StartDate, in.EndDate), nil
	}

	values := []float64{}
	for _, item := range trend {
		if value, ok := numberOf(asMap(item)["avg_sleep_breaths_per_min"]); ok {
			values = append(values, value)
		}
	}
	var periodAvg any
	if len(values) > 0 {
		sum := 0.0
		for _, value := range values {
			sum += value
		}
		periodAvg = roundTo(sum/float64(len(values)), 1)
	}
	return map[string]any{
		"start_date":                       in.StartDate,
		"end_date":                         in.EndDate,
		"days_with_data":                   len(trend),
		"period_avg_sleep_breaths_per_min": periodAvg,
		"trend":                            trend,
	}, nil
}

func getRunningTolerance(ctx context.Context, s *Session, in trDateInput) (any, error) {
	data, err := s.Client.GetRunningTolerance(ctx, in.Date, in.Date, "daily")
	if err != nil {
		return nil, fmt.Errorf("Error retrieving running tolerance data: %w", err)
	}
	if !truthy(data) {
		return fmt.Sprintf("No running tolerance data found for %s.", in.Date), nil
	}
	list := asList(data)
	if len(list) == 0 {
		return fmt.Sprintf("No running tolerance data found for %s.", in.Date), nil
	}

	entry := asMap(list[0])
	curated := map[string]any{
		"date": valueOrDefault(entry, "calendarDate", in.Date),
	}
	if tolerance := entry["acuteTolerance"]; tolerance != nil {
		if f, ok := numberOf(tolerance); ok {
			curated["tolerance_km"] = roundTo(f/1000, 2)
		}
	}
	acuteLoad := entry["acuteImpactLoad"]
	if acuteLoad != nil {
		if f, ok := numberOf(acuteLoad); ok {
			curated["acute_load_km"] = roundTo(f/1000, 2)
		}
	}
	distance := entry["acuteDistance"]
	if distance != nil {
		if f, ok := numberOf(distance); ok {
			curated["distance_km"] = roundTo(f/1000, 2)
		}
	}
	if acuteLoad != nil && truthy(distance) {
		if acuteValue, okAcute := numberOf(acuteLoad); okAcute {
			if distanceValue, okDistance := numberOf(distance); okDistance {
				curated["load_ratio"] = roundTo(acuteValue/distanceValue, 2)
			}
		}
	}
	if feedback := entry["runningToleranceFeedBackPhrase"]; truthy(feedback) {
		curated["feedback_phrase"] = feedback
	}
	return curated, nil
}

func getRunningToleranceTrend(ctx context.Context, s *Session, in runningToleranceTrendInput) (any, error) {
	start, err := time.Parse(dateLayout, in.StartDate)
	if err != nil {
		return fmt.Sprintf("Invalid date format: %v. Use YYYY-MM-DD.", err), nil
	}
	end, err := time.Parse(dateLayout, in.EndDate)
	if err != nil {
		return fmt.Sprintf("Invalid date format: %v. Use YYYY-MM-DD.", err), nil
	}
	days := int(end.Sub(start).Hours()/24) + 1
	if days < 1 {
		return "end_date must be on or after start_date.", nil
	}

	aggregation := in.Aggregation
	if aggregation == "" {
		aggregation = "weekly"
	}
	maxDays := 366
	if aggregation == "daily" {
		maxDays = 90
	}
	if days > maxDays {
		return fmt.Sprintf("Date range too large (%d days). Maximum is %d days for %s aggregation.", days, maxDays, aggregation), nil
	}

	data, err := s.Client.GetRunningTolerance(ctx, in.StartDate, in.EndDate, aggregation)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving running tolerance trend: %w", err)
	}
	if !truthy(data) {
		return fmt.Sprintf("No running tolerance data found between %s and %s.", in.StartDate, in.EndDate), nil
	}

	trend := []map[string]any{}
	for _, item := range asList(data) {
		entry := asMap(item)
		var tolerance, acuteLoad, distance any
		if aggregation == "daily" {
			tolerance = entry["acuteTolerance"]
			acuteLoad = entry["acuteImpactLoad"]
			distance = entry["acuteDistance"]
		} else {
			tolerance = entry["tolerance"]
			acuteLoad = entry["totalImpactLoad"]
			distance = entry["totalDistance"]
		}

		point := map[string]any{"date": entry["calendarDate"]}
		if tolerance != nil {
			if f, ok := numberOf(tolerance); ok {
				point["tolerance_km"] = roundTo(f/1000, 2)
			}
		}
		if acuteLoad != nil {
			if f, ok := numberOf(acuteLoad); ok {
				point["acute_load_km"] = roundTo(f/1000, 2)
			}
		}
		if distance != nil {
			if f, ok := numberOf(distance); ok {
				point["distance_km"] = roundTo(f/1000, 2)
			}
		}
		if acuteLoad != nil && truthy(distance) {
			if acuteValue, okAcute := numberOf(acuteLoad); okAcute {
				if distanceValue, okDistance := numberOf(distance); okDistance {
					point["load_ratio"] = roundTo(acuteValue/distanceValue, 2)
				}
			}
		}

		if aggregation == "daily" {
			if feedback := entry["runningToleranceFeedBackPhrase"]; truthy(feedback) {
				point["feedback_phrase"] = feedback
			}
		} else {
			if entry["startOfWeek"] != nil {
				point["start_of_week"] = entry["startOfWeek"]
			}
			if entry["endOfWeek"] != nil {
				point["end_of_week"] = entry["endOfWeek"]
			}
			if entry["weekIndex"] != nil {
				point["week_index"] = entry["weekIndex"]
			}
		}
		trend = append(trend, point)
	}

	sort.SliceStable(trend, func(i, j int) bool {
		return dateString(trend[i]["date"]) < dateString(trend[j]["date"])
	})

	toleranceValues := []float64{}
	for _, point := range trend {
		if value, ok := numberOf(point["tolerance_km"]); ok {
			toleranceValues = append(toleranceValues, value)
		}
	}
	var firstTolerance, latestTolerance, change any
	if len(toleranceValues) > 0 {
		firstTolerance = toleranceValues[0]
		latestTolerance = toleranceValues[len(toleranceValues)-1]
		change = roundTo(toleranceValues[len(toleranceValues)-1]-toleranceValues[0], 2)
	}

	out := make([]any, 0, len(trend))
	for _, point := range trend {
		out = append(out, point)
	}
	return map[string]any{
		"start_date":          in.StartDate,
		"end_date":            in.EndDate,
		"aggregation":         aggregation,
		"data_points":         len(trend),
		"first_tolerance_km":  firstTolerance,
		"latest_tolerance_km": latestTolerance,
		"tolerance_change_km": change,
		"trend":               out,
	}, nil
}

func getAcclimation(ctx context.Context, s *Session, in trDateInput) (any, error) {
	data, err := s.Client.GetMaxMetrics(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving acclimation data: %w", err)
	}
	if list, ok := data.([]any); ok {
		if len(list) == 0 {
			data = nil
		} else {
			data = list[0]
		}
	}
	record, ok := data.(map[string]any)
	if !ok {
		return fmt.Sprintf("No acclimation data found for %s.", in.Date), nil
	}

	acclimation := asMap(record["heatAltitudeAcclimation"])
	if len(acclimation) == 0 {
		return fmt.Sprintf(
			"No acclimation data found for %s. Garmin populates this only after outdoor activities in heat or at altitude.",
			in.Date,
		), nil
	}

	result := map[string]any{
		"date": valueOrDefault(acclimation, "calendarDate", in.Date),
	}
	fields := []struct {
		outKey string
		inKey  string
	}{
		{"heat_acclimation_percent", "heatAcclimationPercentage"},
		{"previous_heat_acclimation_percent", "previousHeatAcclimationPercentage"},
		{"heat_trend", "heatTrend"},
		{"heat_acclimation_date", "heatAcclimationDate"},
		{"previous_heat_acclimation_date", "previousHeatAcclimationDate"},
		{"altitude_acclimation_meters", "altitudeAcclimation"},
		{"previous_altitude_acclimation_meters", "previousAltitudeAcclimation"},
		{"altitude_trend", "altitudeTrend"},
		{"current_altitude_meters", "currentAltitude"},
	}
	for _, field := range fields {
		if value := acclimation[field.inKey]; value != nil {
			result[field.outKey] = value
		}
	}
	heat, okHeat := numberOf(result["heat_acclimation_percent"])
	previous, okPrevious := numberOf(result["previous_heat_acclimation_percent"])
	if okHeat && okPrevious {
		result["heat_acclimation_change"] = roundTo(heat-previous, 1)
	}
	return result, nil
}
