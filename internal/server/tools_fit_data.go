package server

// Port of activity_analysis.py: get_activity_fit_data.

import (
	"context"
	"errors"
	"fmt"
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

type fitDataIn struct {
	Account
	ActivityID     any  `json:"activity_id" jsonschema:"Garmin activity ID"`
	IncludeRecords bool `json:"include_records,omitempty" jsonschema:"Include full per-second time series (default False). Warning: adds significant data volume for long rides."`
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
