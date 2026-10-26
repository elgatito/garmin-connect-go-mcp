package server

// Port of activity_analysis.py: get_activity_fit_data and get_power_duration_curve.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const getActivityFitDataDesc = `Download and parse FIT file for an activity to expose advanced cycling data.

Returns data not available through the standard REST API, including:
- DI2 / electronic shifting events with cadence at time of shift, grade at shift,
  gear combinations, shift quality classification, and terrain-grouped shift analysis
- Cycling dynamics per session and lap: platform center offset (PCO), left/right power
  balance, torque effectiveness, pedal smoothness
- Variability Index (NP / avg_power) per session and lap
- Climb detection with VAM (vertical ascent rate), avg power/cadence/HR per climb,
  and W/kg per climb (using the latest Garmin weigh-in on or before the ride,
  up to 90 days back; rider_weight_date says which weigh-in was used)
- Grade-correlated stats: avg power, cadence, HR broken down by terrain steepness
- Grade comes from the device's per-record grade field when present; otherwise
  it is derived from altitude and distance (~100 m window). grade_source
  ("device" | "derived") says which
- HR drift / cardiac drift coefficient (aerobic decoupling for rides ≥60 min)
- Temperature correlation: avg HR/power in hottest vs. coolest portions of ride
- Power Duration Curve: best mean maximal power at 5s, 30s, 1min, 5min, 10min, 20min, 60min
- Optional full per-second time series when include_records=True

Multisport files (triathlon, duathlon) contain one session per leg. For
these, the response adds multisport=true and a sessions list with every
leg; session and the record-based analyses (power curve, climbs, grade,
HR drift, temperature) describe one leg only — the cycling leg if there
is exactly one, else the longest — given by analyzed_session_index and
explained in analysis_note. Laps and records still cover all legs.

Shift quality:
- proactive: shifted at 70-100 rpm (ideal cadence range)
- reactive: shifted below 70 rpm (already grinding before shifting)
- coasting: shifted at 0 rpm (mid-stop or freewheeling)
- spun_out: shifted above 100 rpm (waited too long in easy gear)

Note: DI2 data requires Shimano Di2 / SRAM eTap. Cycling dynamics require a
compatible power meter (e.g., Garmin Rally, Favero Assioma, PowerTap P1 pedals).`

const getPowerDurationCurveDesc = `Get season-best Power Duration Curve across recent activities.

Downloads FIT files for recent activities of the requested type and computes best mean maximal
power at each standard duration. Returns season bests with which activity and date
each best came from.

Durations: 5s (sprint), 30s, 1min, 5min (VO2 max proxy), 10min, 20min (FTP proxy), 60min

Use the 20-minute best × 0.95 as a strong FTP estimate without a formal test.

Warning: downloads multiple FIT files — may take 30-60 seconds for 20 activities.`

type fitDataIn struct {
	Account
	ActivityID     any  `json:"activity_id" jsonschema:"Garmin activity ID"`
	IncludeRecords bool `json:"include_records,omitempty" jsonschema:"Include full per-second time series (default False). Warning: adds significant data volume for long rides."`
}

type powerDurationCurveIn struct {
	Account
	NumActivities *int    `json:"num_activities,omitempty" jsonschema:"Number of recent activities matching activity_type to request (default 20, max 50)"`
	ActivityType  *string `json:"activity_type,omitempty" jsonschema:"Activity type to filter (default \"cycling\")"`
}

func getActivityFitData(ctx context.Context, s *Session, in fitDataIn) (any, error) {
	id, err := parseActivityID(in.ActivityID)
	if err != nil {
		return nil, fmt.Errorf("Error downloading FIT data for activity %s: %w", pyStr(in.ActivityID), err)
	}
	fail := func(err error) (any, error) {
		return nil, fmt.Errorf("Error downloading FIT data for activity %d: %w", id, err)
	}
	raw, err := s.Client.DownloadActivity(ctx, id, "ORIGINAL")
	if err != nil {
		return fail(err)
	}
	if len(raw) == 0 {
		return fmt.Sprintf("No FIT data returned for activity %d", id), nil
	}
	parsed, err := parseFit(raw, in.IncludeRecords)
	if err != nil {
		return newFitBytesFailure(err, raw), nil
	}
	parsed["activity_id"] = id

	// W/kg: latest weigh-in on or before the activity date.
	session, _ := parsed["session"].(map[string]any)
	start, _ := session["start_time"].(string)
	if start == "" {
		return parsed, nil
	}
	kg, day, ok := riderWeightKg(ctx, s, string([]rune(start)[:min(10, len([]rune(start)))]))
	if !ok {
		return parsed, nil
	}
	parsed["rider_weight_kg"], parsed["rider_weight_date"] = kg, day
	perKg := func(dst map[string]any, key string, w any) error {
		if falsy(w) {
			return nil
		}
		if kg == 0 {
			return errors.New("float division by zero")
		}
		f, _, _ := pyNum(w)
		dst[key] = round(f/kg, 2)
		return nil
	}
	if err := perKg(session, "avg_w_per_kg", session["avg_power_w"]); err != nil {
		return fail(err)
	}
	if err := perKg(session, "normalized_w_per_kg", session["normalized_power_w"]); err != nil {
		return fail(err)
	}
	climbs, _ := parsed["climbs"].([]map[string]any)
	for _, climb := range climbs {
		if err := perKg(climb, "avg_w_per_kg", climb["avg_power_w"]); err != nil {
			return fail(err)
		}
	}
	return parsed, nil
}

// weightLookbackDays is _WEIGHT_LOOKBACK_DAYS: most people don't weigh in on ride days.
const weightLookbackDays = 90

// riderWeightKg is _get_rider_weight_kg: the latest weigh-in (kg, date) on or before the activity date, up
// to 90 days back. Any failure means no weight, as Python's blanket except does.
func riderWeightKg(ctx context.Context, s *Session, activityDate string) (float64, string, bool) {
	d, err := parseISODate(activityDate)
	if err != nil {
		return 0, "", false
	}
	end := d.Format(time.DateOnly)
	data, err := s.Client.GetBodyComposition(ctx, d.AddDate(0, 0, -weightLookbackDays).Format(time.DateOnly), end)
	if err != nil {
		return 0, "", false
	}
	var bestDay string
	var bestWeight float64
	found := false
	for _, e := range asList(asMap(data)["dateWeightList"]) {
		entry, ok := e.(map[string]any)
		if !ok || falsy(entry["calendarDate"]) || falsy(entry["weight"]) {
			continue
		}
		day, okDay := entry["calendarDate"].(string)
		w, okW := asNumber(entry["weight"])
		if !okDay || !okW { // Python raises TypeError comparing or dividing them
			return 0, "", false
		}
		if day > end || (found && (day < bestDay || (day == bestDay && w <= bestWeight))) {
			continue
		}
		bestDay, bestWeight, found = day, w, true
	}
	if !found {
		return 0, "", false
	}
	return round(bestWeight/1000.0, 2), bestDay, true
}

type seasonBest struct {
	Watts      float64 `json:"watts"`
	ActivityID any     `json:"activity_id"`
	Date       string  `json:"date"`
}

// isRequestedActivity is get_power_duration_curve's local sport guard. Cycling's parent type includes
// virtual_ride, cyclocross and e-bike types without either substring.
func isRequestedActivity(a any, requested string) (bool, error) {
	m, ok := a.(map[string]any)
	if !ok {
		return false, fmt.Errorf("'%s' object has no attribute 'get'", pyTypeName(a))
	}
	at := firstTruthy(m["activityType"], map[string]any{})
	atMap, ok := at.(map[string]any)
	if !ok {
		return false, fmt.Errorf("'%s' object has no attribute 'get'", pyTypeName(at))
	}
	typeKey := strings.ToLower(pyStrValue(getDefault(atMap, "typeKey", "")))
	parent := pyStrValue(getDefault(atMap, "parentTypeId", ""))
	return strings.Contains(typeKey, requested) || requested == parent ||
		(requested == "cycling" && (parent == "2" || strings.Contains(typeKey, "biking"))), nil
}

func getPowerDurationCurve(ctx context.Context, s *Session, in powerDurationCurveIn) (any, error) {
	const maxActivities = 50
	num := 20
	if in.NumActivities != nil {
		num = *in.NumActivities
	}
	num = min(num, maxActivities)
	activityType := "cycling"
	if in.ActivityType != nil {
		activityType = *in.ActivityType
	}
	fail := func(err error) (any, error) { return nil, fmt.Errorf("Error computing power duration curve: %w", err) }

	// Filter before applying the count so other sports do not consume the requested window.
	requested := strings.ToLower(activityType)
	v, err := s.Client.GetActivities(ctx, 0, num, requested)
	if err != nil {
		return fail(err)
	}
	if falsy(v) {
		return "No activities found.", nil
	}
	activities, ok := v.([]any)
	if !ok {
		if _, isMap := v.(map[string]any); !isMap && pyTypeName(v) != "str" {
			return fail(fmt.Errorf("'%s' object is not iterable", pyTypeName(v)))
		}
		return fail(errors.New("'str' object has no attribute 'get'")) // a dict or str iterates as str items
	}
	var matching []map[string]any
	for _, a := range activities {
		match, err := isRequestedActivity(a, requested)
		if err != nil {
			return fail(err)
		}
		if match {
			matching = append(matching, asMap(a))
		}
	}
	if len(matching) == 0 {
		return fmt.Sprintf("No %s activities found in the last %d activities.", activityType, num), nil
	}

	bests := map[string]seasonBest{}
	processed, skipped := 0, 0
	for _, a := range matching {
		actID := a["activityId"]
		date := []rune(pyStrValue(getDefault(a, "startTimeLocal", "")))
		actDate := string(date[:min(10, len(date))])
		if falsy(actID) {
			continue
		}
		pdc, done, err := activityPowerCurve(ctx, s, actID)
		if err != nil {
			skipped++
			continue
		}
		if !done {
			continue
		}
		if pdc != nil {
			for _, label := range pdc.keys {
				if best, seen := bests[label]; !seen || pdc.vals[label] > best.Watts {
					bests[label] = seasonBest{pdc.vals[label], actID, actDate}
				}
			}
		}
		processed++
	}
	if len(bests) == 0 {
		return fmt.Sprintf("No power data found across %d activities.", processed), nil
	}
	season := newOrdered[seasonBest]()
	for _, label := range pdcLabels {
		if b, ok := bests[label]; ok {
			season.set(label, b)
		}
	}
	var ftp any
	if b, ok := bests["20min"]; ok {
		ftp = roundInt(b.Watts * 0.95)
	}
	return struct {
		ActivitiesAnalyzed int                  `json:"activities_analyzed"`
		ActivitiesSkipped  int                  `json:"activities_skipped"`
		FTPEstimateW       any                  `json:"ftp_estimate_w"`
		FTPNote            string               `json:"ftp_note"`
		SeasonBests        *ordered[seasonBest] `json:"season_bests"`
	}{processed, skipped, ftp, "Estimated as 95% of 20-minute best power", season}, nil
}

// activityPowerCurve downloads one activity's original FIT file and computes its power curve from the record
// power and timestamps. done is false for an empty download (neither processed nor skipped); an error counts
// the activity as skipped.
func activityPowerCurve(ctx context.Context, s *Session, actID any) (pdc *ordered[float64], done bool, err error) {
	id, err := parseActivityID(actID) // Python puts str(activityId) in the URL; a non-integer id cannot download
	if err != nil {
		return nil, false, err
	}
	raw, err := s.Client.DownloadActivity(ctx, id, "ORIGINAL")
	if err != nil || len(raw) == 0 {
		return nil, false, err
	}
	fitBytes, err := extractFitBytes(raw)
	if err != nil {
		return nil, false, err
	}
	msgs, err := decodeFIT(fitBytes)
	if err != nil {
		return nil, false, err
	}
	var records []map[string]any
	for _, m := range msgs {
		if m.Name == "record" {
			records = append(records, map[string]any{"power_w": getField(m, "power"), "timestamp": getField(m, "timestamp")})
		}
	}
	return computePowerDurationCurve(records), true, nil
}
