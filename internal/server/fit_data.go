package server

// Port of activity_analysis._curate_session and _parse_fit: the curated cycling view of a FIT file used by
// get_activity_fit_data. Field values come from decodeFIT, which reproduces fitparse's values.

import (
	"fmt"
	"maps"
	"time"
)

// fitFieldPair is an output key and the FIT field names _get_field tries for it, in order.
type fitFieldPair struct {
	key   string
	names []string
}

func ff(key string, names ...string) fitFieldPair { return fitFieldPair{key, names} }

var (
	sessionFields = []fitFieldPair{
		ff("total_elapsed_time_s", "total_elapsed_time"),
		ff("total_timer_time_s", "total_timer_time"),
		ff("total_distance_m", "total_distance"),
		ff("total_calories", "total_calories"),
		ff("avg_speed_mps", "enhanced_avg_speed", "avg_speed"),
		ff("max_speed_mps", "enhanced_max_speed", "max_speed"),
		ff("avg_power_w", "avg_power"),
		ff("max_power_w", "max_power"),
		ff("normalized_power_w", "normalized_power"),
		ff("avg_cadence_rpm", "avg_cadence"),
		ff("max_cadence_rpm", "max_cadence"),
		ff("avg_heart_rate_bpm", "avg_heart_rate"),
		ff("max_heart_rate_bpm", "max_heart_rate"),
		ff("total_ascent_m", "total_ascent"),
		ff("total_descent_m", "total_descent"),
		ff("total_training_effect", "total_training_effect"),
		ff("avg_left_pco_mm", "avg_left_pco"),
		ff("avg_right_pco_mm", "avg_right_pco"),
		ff("avg_left_torque_effectiveness_pct", "avg_left_torque_effectiveness"),
		ff("avg_right_torque_effectiveness_pct", "avg_right_torque_effectiveness"),
		ff("avg_left_pedal_smoothness_pct", "avg_left_pedal_smoothness"),
		ff("avg_right_pedal_smoothness_pct", "avg_right_pedal_smoothness"),
	}
	lapFields = []fitFieldPair{
		ff("total_elapsed_time_s", "total_elapsed_time"),
		ff("total_distance_m", "total_distance"),
		ff("avg_speed_mps", "enhanced_avg_speed", "avg_speed"),
		ff("avg_power_w", "avg_power"),
		ff("normalized_power_w", "normalized_power"),
		ff("avg_cadence_rpm", "avg_cadence"),
		ff("avg_heart_rate_bpm", "avg_heart_rate"),
		ff("avg_left_pco_mm", "avg_left_pco"),
		ff("avg_right_pco_mm", "avg_right_pco"),
		ff("avg_left_torque_effectiveness_pct", "avg_left_torque_effectiveness"),
		ff("avg_right_torque_effectiveness_pct", "avg_right_torque_effectiveness"),
		ff("avg_left_pedal_smoothness_pct", "avg_left_pedal_smoothness"),
		ff("avg_right_pedal_smoothness_pct", "avg_right_pedal_smoothness"),
		ff("total_ascent_m", "total_ascent"),
		ff("total_descent_m", "total_descent"),
	}
	recordFields = []fitFieldPair{
		ff("heart_rate_bpm", "heart_rate"),
		// Modern Edge/Forerunner units write only the 32-bit enhanced_* fields.
		ff("speed_mps", "enhanced_speed", "speed"),
		ff("altitude_m", "enhanced_altitude", "altitude"),
		ff("distance_m", "distance"),
		ff("temperature_c", "temperature"),
		ff("left_pco_mm", "left_pco"),
		ff("right_pco_mm", "right_pco"),
		ff("left_torque_effectiveness_pct", "left_torque_effectiveness"),
		ff("right_torque_effectiveness_pct", "right_torque_effectiveness"),
		ff("left_pedal_smoothness_pct", "left_pedal_smoothness"),
		ff("right_pedal_smoothness_pct", "right_pedal_smoothness"),
	}
)

func setFields(dst map[string]any, m fitMessage, fields []fitFieldPair) {
	for _, p := range fields {
		dst[p.key] = getField(m, p.names...)
	}
}

// variabilityIndex sets NP / avg power when both are truthy and avg power is positive.
func variabilityIndex(m map[string]any) {
	np, _, _ := pyNum(m["normalized_power_w"])
	avg, _, _ := pyNum(m["avg_power_w"])
	if !falsy(m["normalized_power_w"]) && !falsy(m["avg_power_w"]) && avg > 0 {
		m["variability_index"] = round(np/avg, 3)
	}
}

// curateSession is _curate_session.
func curateSession(m fitMessage) map[string]any {
	session := map[string]any{
		"sport":      getField(m, "sport"),
		"sub_sport":  getField(m, "sub_sport"),
		"start_time": fitStr(getField(m, "start_time")),
	}
	setFields(session, m, sessionFields)
	// FIT power_phase_type indexes 0/1 are the start/end angles.
	for _, side := range []string{"left", "right"} {
		phase := getField(m, "avg_"+side+"_power_phase")
		if l, ok := phase.([]any); ok {
			if len(l) > 0 {
				session["avg_"+side+"_power_phase_start_deg"] = l[0]
			}
			if len(l) > 1 {
				session["avg_"+side+"_power_phase_end_deg"] = l[1]
			}
		} else if phase != nil {
			session["avg_"+side+"_power_phase_start_deg"] = phase
		}
	}
	left := decodeLeftRightBalance(getField(m, "left_right_balance"), false)
	session["avg_left_power_pct"] = left
	if l, ok := left.(float64); ok {
		session["avg_right_power_pct"] = round(100.0-l, 1)
	}
	dropNil(session)
	variabilityIndex(session)
	return session
}

// curateLap is the lap branch of _parse_fit.
func curateLap(m fitMessage, number int) map[string]any {
	lap := map[string]any{"lap_number": number, "start_time": fitStr(getField(m, "start_time"))}
	setFields(lap, m, lapFields)
	if d, _, ok := pyNum(lap["total_distance_m"]); ok && d >= uint32DistanceWrapM-maxWrappedNegativeM && d <= uint32DistanceWrapM {
		lap["total_distance_m"] = nil
		lap["distance_invalid"] = true
	}
	if l, ok := decodeLeftRightBalance(getField(m, "left_right_balance"), false).(float64); ok {
		lap["avg_left_power_pct"] = l
		lap["avg_right_power_pct"] = round(100.0-l, 1)
	}
	dropNil(lap)
	variabilityIndex(lap)
	return lap
}

// rrPair is an R-R interval with the timestamp of the record message before it.
type rrPair struct {
	ts any
	rr float64
}

// parseFit is _parse_fit: the curated sessions, laps, DI2 shifts, records and record-based analyses.
func parseFit(raw []byte, includeRecords bool) (map[string]any, error) {
	fitBytes, err := extractFitBytes(raw)
	if err != nil {
		return nil, err
	}
	msgs, err := decodeFIT(fitBytes)
	if err != nil {
		return nil, err
	}

	sessions := []map[string]any{}
	laps := []map[string]any{}
	shifts := []map[string]any{}
	records := []map[string]any{}
	var shiftCounts []shiftAtRecord
	var rrPairs []rrPair
	var lastRecordTS, lastCadence, lastGrade any

	for _, m := range msgs {
		switch m.Name {
		case "session":
			sessions = append(sessions, curateSession(m))
		case "lap":
			laps = append(laps, curateLap(m, len(laps)+1))
		case "event":
			eventType := getField(m, "event")
			if eventType != "rear_gear_change" && eventType != "front_gear_change" && eventType != "gear_change" {
				continue
			}
			gearData := getField(m, "gear_change_data", "data")
			shift := map[string]any{
				"timestamp":            fitStr(getField(m, "timestamp")),
				"event":                pyStr(eventType),
				"cadence_at_shift_rpm": lastCadence,
			}
			if g, _, ok := pyNum(lastGrade); ok {
				shift["grade_at_shift_pct"] = round(g, 1)
			}
			if gearData != nil {
				if n, err := pyInt(gearData); err == nil {
					decoded := decodeGearChange(n)
					maps.Copy(shift, decoded)
					if ft, rt := decoded["front_teeth"], decoded["rear_teeth"]; ft != nil && rt != nil {
						shift["gear_combo"] = fmt.Sprintf("%d/%dt", ft, rt)
					}
				} else {
					shift["gear_change_data_raw"] = pyStr(gearData)
				}
			}
			shift["quality"] = "unknown"
			if cad, _, ok := pyNum(lastCadence); ok {
				switch {
				case cad == 0:
					shift["quality"] = "coasting"
				case cad < 70:
					shift["quality"] = "reactive"
				case cad > 100:
					shift["quality"] = "spun_out"
				default:
					shift["quality"] = "proactive"
				}
			}
			dropNil(shift)
			shifts = append(shifts, shift)
			shiftCounts = append(shiftCounts, shiftAtRecord{shift, len(records)})
		case "record":
			cadence := getField(m, "cadence")
			if cadence != nil {
				lastCadence = cadence
			}
			grade := getField(m, "grade")
			if grade != nil {
				lastGrade = grade
			}
			ts := getField(m, "timestamp")
			if ts != nil {
				lastRecordTS = ts
			}
			record := map[string]any{
				"timestamp":   fitStr(ts),
				"power_w":     getField(m, "power"),
				"cadence_rpm": cadence,
				"grade_pct":   grade,
				"lat_deg":     semicirclesToDegrees(getField(m, "position_lat")),
				"lon_deg":     semicirclesToDegrees(getField(m, "position_long")),
			}
			setFields(record, m, recordFields)
			if l, ok := decodeLeftRightBalance(getField(m, "left_right_balance"), true).(float64); ok {
				record["left_power_pct"] = l
				record["right_power_pct"] = round(100.0-l, 1)
			}
			for _, side := range []string{"left", "right"} {
				// Python indexes [0] then [1]; a scalar raises TypeError before either is set.
				if phase, ok := getField(m, side+"_power_phase").([]any); ok && len(phase) > 0 {
					record[side+"_power_phase_start_deg"] = phase[0]
					if len(phase) > 1 {
						record[side+"_power_phase_end_deg"] = phase[1]
					}
				}
			}
			records = append(records, dropNil(record))
		case "hrv":
			rrField := getField(m, "time")
			if rrField == nil {
				continue
			}
			rrs, ok := rrField.([]any)
			if !ok {
				rrs = []any{rrField}
			}
			for _, v := range rrs {
				// ~65.535 s is FIT's "no R-R interval" filler in fixed-size arrays.
				if rr, _, ok := pyNum(v); ok && 0.2 < rr && rr < 3.0 {
					rrPairs = append(rrPairs, rrPair{lastRecordTS, rr})
				}
			}
		}
	}

	// A multisport file has one session per leg while records and laps span every leg: summarise one leg and
	// run the record-based analytics on its records only.
	multisport := len(sessions) > 1
	analysisRecords := records
	var session map[string]any
	var analyzedIndex int
	var analysisNote string
	if multisport {
		analyzedIndex = primarySessionIndex(sessions)
		session = maps.Clone(sessions[analyzedIndex])
		leg := fmt.Sprintf("leg %d (%s)", analyzedIndex, pyStr(getDefault(session, "sport", "unknown")))
		inLeg, ok := recordsInSession(records, session)
		if ok {
			analysisRecords = inLeg
			analysisNote = "Multisport file: session and record-based analyses (power curve, climbs, grade, HR drift, " +
				"temperature) cover " + leg + " only. See sessions for every leg."
		} else {
			analysisNote = "Multisport file: session summarises " + leg + ", but record timestamps or the session " +
				"window are missing, so record-based analyses cover all legs."
		}
	} else if len(sessions) > 0 {
		session = sessions[0]
	} else {
		session = map[string]any{}
	}

	// Devices that don't write the record grade field: derive it from altitude + distance.
	gradeSource := ""
	if len(records) > 0 {
		gradeSource = deriveGrades(records)
	}
	if gradeSource == "derived" {
		fillShiftGrades(shiftCounts, records)
	}
	shiftSummary := computeShiftSummary(shifts)

	climbs := []map[string]any{}
	var pdc *ordered[float64]
	if len(analysisRecords) > 0 {
		if v := gradeAnalysis(analysisRecords); len(v) > 0 {
			session["grade_analysis"] = v
		}
		if v := computeHRDrift(analysisRecords); len(v) > 0 {
			session["hr_drift"] = v
		}
		if v := computeTemperatureStats(analysisRecords); len(v) > 0 {
			session["temperature_stats"] = v
		}
		climbs = detectClimbs(analysisRecords, climbMinElevationGain, climbMinAvgGradePct, climbMinDurationS)
		pdc = computePowerDurationCurve(analysisRecords)
	}

	result := map[string]any{"session": session, "laps": laps, "shift_summary": shiftSummary, "shifts": shifts}
	if multisport {
		result["multisport"] = true
		result["sessions"] = sessions
		result["analyzed_session_index"] = analyzedIndex
		result["analysis_note"] = analysisNote
	}
	if len(climbs) > 0 {
		result["climbs"] = climbs
	}
	if pdc != nil {
		result["power_duration_curve"] = pdc
	}
	if gradeSource != "" {
		result["grade_source"] = gradeSource
	}

	lapPowerFromRecords(laps, records)

	if len(rrPairs) > 0 {
		all := make([]float64, len(rrPairs))
		for i, p := range rrPairs {
			all[i] = p.rr
		}
		if hrv := computeHRVMetrics(all); hrv != nil {
			result["hrv"] = hrv
		}
		// Per-lap HRV: R-R pairs whose record timestamp falls in the lap's [start, start + elapsed) window.
		for _, lap := range laps {
			start, end, ok := lapWindow(lap)
			if !ok {
				continue
			}
			var lapRR []float64
			for _, p := range rrPairs {
				if t, ok := parseFitTimestamp(p.ts); ok && !t.Before(start) && t.Before(end) {
					lapRR = append(lapRR, p.rr)
				}
			}
			if hrv := computeHRVMetrics(lapRR); hrv != nil {
				lap["hrv"] = hrv
			}
		}
		if includeRecords {
			stream := make([]map[string]any, len(rrPairs))
			for i, p := range rrPairs {
				var ts any
				if !falsy(p.ts) {
					ts = fitStr(p.ts)
				}
				stream[i] = map[string]any{"timestamp": ts, "rr_seconds": p.rr}
			}
			result["rr_intervals_seconds"] = stream
		}
	}
	if includeRecords {
		result["records"] = records
	}
	return result, nil
}

// lapWindow is a lap's [start_time, start_time + total_elapsed_time_s) window.
func lapWindow(lap map[string]any) (time.Time, time.Time, bool) {
	start, ok := parseFitTimestamp(lap["start_time"])
	elapsed, _, okE := pyNum(lap["total_elapsed_time_s"])
	if !ok || !okE || elapsed == 0 {
		return time.Time{}, time.Time{}, false
	}
	return start, start.Add(pyTimedelta(elapsed)), true
}

// lapPowerFromRecords sets avg/max power for laps without a native avg_power (e.g. running with a wrist or
// Connect IQ power source) from the records in the lap's time window.
func lapPowerFromRecords(laps, records []map[string]any) {
	missing := false
	for _, lap := range laps {
		_, has := lap["avg_power_w"]
		missing = missing || !has
	}
	if len(records) == 0 || !missing {
		return
	}
	type tsPower struct {
		t time.Time
		w float64
	}
	var recPower []tsPower
	for _, r := range records {
		w, ok := recNum(r, "power_w")
		if !ok {
			continue
		}
		if t, ok := parseFitTimestamp(r["timestamp"]); ok {
			recPower = append(recPower, tsPower{t, w})
		}
	}
	if len(recPower) == 0 {
		return
	}
	for _, lap := range laps {
		if _, has := lap["avg_power_w"]; has {
			continue
		}
		start, end, ok := lapWindow(lap)
		if !ok {
			continue
		}
		var vals []float64
		for _, p := range recPower {
			if !p.t.Before(start) && p.t.Before(end) {
				vals = append(vals, p.w)
			}
		}
		if len(vals) > 0 {
			mx := vals[0]
			for _, v := range vals[1:] {
				mx = max(mx, v)
			}
			lap["avg_power_w"] = roundInt(pySumFloats(vals) / float64(len(vals)))
			lap["max_power_w"] = roundInt(mx)
		}
	}
}
