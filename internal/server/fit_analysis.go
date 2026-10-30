package server

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"net/url"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/muktihari/fit/profile/typedef"
)

func fitFieldValue(msg fitMessage, names ...string) any {
	for _, name := range names {
		for _, f := range msg.Fields {
			if f.Name == name && f.Value != nil {
				return f.Value
			}
		}
	}
	return nil
}
func fitFields(msg fitMessage, fields map[string][]string) map[string]any {
	out := map[string]any{}
	for dst, names := range fields {
		if value := fitFieldValue(msg, names...); value != nil {
			out[dst] = value
		}
	}
	return out
}
func fitEnum(v any, kind string) any {
	n, ok := fitNumber(v)
	if !ok {
		return v
	}
	switch kind {
	case "sport":
		return typedef.Sport(n).String()
	case "sub_sport":
		return typedef.SubSport(n).String()
	case "event":
		return typedef.Event(n).String()
	}
	return v
}
func fitBalance(v any, record bool) any {
	num, ok := fitNumber(v)
	if !ok {
		return nil
	}
	n := int(num)
	pct := float64(n&0x3fff) / 100
	right := n&0x8000 != 0
	if record {
		pct = float64(n & 0x7f)
		right = n&0x80 != 0
	}
	if pct > 100 {
		return nil
	}
	if right {
		pct = 100 - pct
	}
	return math.RoundToEven(pct*10) / 10
}
func addBalance(out map[string]any, msg fitMessage, record bool) {
	left := fitBalance(fitFieldValue(msg, "left_right_balance"), record)
	if left == nil {
		return
	}
	prefix := "avg_"
	if record {
		prefix = ""
	}
	out[prefix+"left_power_pct"] = left
	out[prefix+"right_power_pct"] = rounded(100-left.(float64), 10)
}
func addPhases(out map[string]any, msg fitMessage, prefix string) {
	for _, side := range []string{"left", "right"} {
		v := fitFieldValue(msg, prefix+side+"_power_phase")
		values, ok := v.([]any)
		if ok && len(values) > 0 {
			out[prefix+side+"_power_phase_start_deg"] = values[0]
			if len(values) > 1 {
				out[prefix+side+"_power_phase_end_deg"] = values[1]
			}
		} else if v != nil && prefix != "" {
			out[prefix+side+"_power_phase_start_deg"] = v
		}
	}
}

var fitCommonSummaryFields = map[string][]string{"start_time": {"start_time"}, "total_elapsed_time_s": {"total_elapsed_time"}, "total_distance_m": {"total_distance"}, "avg_speed_mps": {"enhanced_avg_speed", "avg_speed"}, "avg_power_w": {"avg_power"}, "normalized_power_w": {"normalized_power"}, "avg_cadence_rpm": {"avg_cadence"}, "avg_heart_rate_bpm": {"avg_heart_rate"}, "avg_left_pco_mm": {"avg_left_pco"}, "avg_right_pco_mm": {"avg_right_pco"}, "avg_left_torque_effectiveness_pct": {"avg_left_torque_effectiveness"}, "avg_right_torque_effectiveness_pct": {"avg_right_torque_effectiveness"}, "avg_left_pedal_smoothness_pct": {"avg_left_pedal_smoothness"}, "avg_right_pedal_smoothness_pct": {"avg_right_pedal_smoothness"}, "total_ascent_m": {"total_ascent"}, "total_descent_m": {"total_descent"}}
var fitSessionExtraFields = map[string][]string{"sport": {"sport"}, "sub_sport": {"sub_sport"}, "total_timer_time_s": {"total_timer_time"}, "total_calories": {"total_calories"}, "max_speed_mps": {"enhanced_max_speed", "max_speed"}, "max_power_w": {"max_power"}, "max_cadence_rpm": {"max_cadence"}, "max_heart_rate_bpm": {"max_heart_rate"}, "total_training_effect": {"total_training_effect"}}
var fitRecordFields = map[string][]string{"timestamp": {"timestamp"}, "power_w": {"power"}, "cadence_rpm": {"cadence"}, "heart_rate_bpm": {"heart_rate"}, "speed_mps": {"enhanced_speed", "speed"}, "altitude_m": {"enhanced_altitude", "altitude"}, "distance_m": {"distance"}, "grade_pct": {"grade"}, "temperature_c": {"temperature"}, "left_pco_mm": {"left_pco"}, "right_pco_mm": {"right_pco"}, "left_torque_effectiveness_pct": {"left_torque_effectiveness"}, "right_torque_effectiveness_pct": {"right_torque_effectiveness"}, "left_pedal_smoothness_pct": {"left_pedal_smoothness"}, "right_pedal_smoothness_pct": {"right_pedal_smoothness"}}

func addVariability(v map[string]any) {
	avg, a := fitNumber(v["avg_power_w"])
	np, n := fitNumber(v["normalized_power_w"])
	if a && n && avg > 0 && np != 0 {
		v["variability_index"] = rounded(np/avg, 1000)
	}
}

type rrPair struct {
	timestamp string
	value     float64
}

func parseFITAnalysis(messages []fitMessage, includeRecords bool) map[string]any {
	sessions, laps, shifts, records := []map[string]any{}, []map[string]any{}, []map[string]any{}, []map[string]any{}
	shiftCounts := []int{}
	rr := []rrPair{}
	var lastCadence, lastGrade any
	lastTimestamp := ""
	for _, msg := range messages {
		switch msg.Name {
		case "session":
			v := fitFields(msg, fitCommonSummaryFields)
			for k, x := range fitFields(msg, fitSessionExtraFields) {
				v[k] = x
			}
			v["start_time"] = fitString(v["start_time"])
			if v["sport"] != nil {
				v["sport"] = fitEnum(v["sport"], "sport")
			}
			if v["sub_sport"] != nil {
				v["sub_sport"] = fitEnum(v["sub_sport"], "sub_sport")
			}
			addPhases(v, msg, "avg_")
			addBalance(v, msg, false)
			addVariability(v)
			sessions = append(sessions, v)
		case "lap":
			v := fitFields(msg, fitCommonSummaryFields)
			v["start_time"] = fitString(v["start_time"])
			v["lap_number"] = len(laps) + 1
			if n, ok := fitNumber(v["total_distance_m"]); ok && n >= 42949672.96-1000 && n <= 42949672.96 {
				delete(v, "total_distance_m")
				v["distance_invalid"] = true
			}
			addBalance(v, msg, false)
			addVariability(v)
			laps = append(laps, v)
		case "record":
			v := fitFields(msg, fitRecordFields)
			v["timestamp"] = fitString(v["timestamp"])
			if v["timestamp"] != "" {
				lastTimestamp = v["timestamp"].(string)
			}
			if v["cadence_rpm"] != nil {
				lastCadence = v["cadence_rpm"]
			}
			if v["grade_pct"] != nil {
				lastGrade = v["grade_pct"]
			}
			for dst, src := range map[string]string{"lat_deg": "position_lat", "lon_deg": "position_long"} {
				if n, ok := fitNumber(fitFieldValue(msg, src)); ok {
					v[dst] = rounded(n*180/2147483648, 1000000)
				}
			}
			addBalance(v, msg, true)
			addPhases(v, msg, "")
			records = append(records, v)
		case "event":
			event := fmt.Sprint(fitEnum(fitFieldValue(msg, "event"), "event"))
			if event != "rear_gear_change" && event != "front_gear_change" && event != "gear_change" {
				continue
			}
			shift := map[string]any{"timestamp": fitString(fitFieldValue(msg, "timestamp")), "event": event, "quality": "unknown"}
			if lastCadence != nil {
				shift["cadence_at_shift_rpm"] = lastCadence
				cad, _ := fitNumber(lastCadence)
				quality := "proactive"
				if cad == 0 {
					quality = "coasting"
				} else if cad < 70 {
					quality = "reactive"
				} else if cad > 100 {
					quality = "spun_out"
				}
				shift["quality"] = quality
			}
			if lastGrade != nil {
				shift["grade_at_shift_pct"] = rounded(lastGrade, 10)
			}
			if raw, ok := fitNumber(fitFieldValue(msg, "gear_change_data", "data")); ok {
				n := uint32(raw)
				shift["rear_gear_num"] = n & 255
				shift["front_gear_num"] = (n >> 16) & 255
				rear, front := (n>>8)&255, (n>>24)&255
				if rear > 0 {
					shift["rear_teeth"] = rear
				}
				if front > 0 {
					shift["front_teeth"] = front
				}
				if rear > 0 && front > 0 {
					shift["gear_combo"] = fmt.Sprintf("%d/%dt", front, rear)
				}
			}
			shifts = append(shifts, shift)
			shiftCounts = append(shiftCounts, len(records))
		case "hrv":
			value := fitFieldValue(msg, "time")
			values, ok := value.([]any)
			if !ok {
				values = []any{value}
			}
			for _, v := range values {
				if n, ok := fitNumber(v); ok && n > 0.2 && n < 3 {
					rr = append(rr, rrPair{lastTimestamp, n})
				}
			}
		}
	}
	analysis := records
	session := map[string]any{}
	index := 0
	if len(sessions) > 0 {
		index = primaryFITSession(sessions)
		for k, v := range sessions[index] {
			session[k] = v
		}
	}
	out := map[string]any{"session": session, "laps": laps, "shifts": shifts}
	if len(sessions) > 1 {
		analysis = recordsInSession(records, session)
		sport := fmt.Sprint(session["sport"])
		if sport == "<nil>" {
			sport = "unknown"
		}
		leg := fmt.Sprintf("leg %d (%s)", index, sport)
		if analysis == nil {
			analysis = records
			out["analysis_note"] = fmt.Sprintf("Multisport file: session summarises %s, but record timestamps or the session window are missing, so record-based analyses cover all legs.", leg)
		} else {
			out["analysis_note"] = fmt.Sprintf("Multisport file: session and record-based analyses (power curve, climbs, grade, HR drift, temperature) cover %s only. See sessions for every leg.", leg)
		}
		out["multisport"] = true
		out["sessions"] = sessions
		out["analyzed_session_index"] = index
	}
	grade := deriveFITGrades(records)
	if grade != "" {
		out["grade_source"] = grade
	}
	if grade == "derived" {
		for i, shift := range shifts {
			if shift["grade_at_shift_pct"] != nil || shiftCounts[i] == 0 {
				continue
			}
			for j := shiftCounts[i] - 1; j >= 0; j-- {
				if n, ok := fitNumber(records[j]["grade_pct"]); ok {
					shift["grade_at_shift_pct"] = rounded(n, 10)
					break
				}
			}
		}
	}
	out["shift_summary"] = fitShiftSummary(shifts)
	if gradeStats := fitGradeAnalysis(analysis); len(gradeStats) > 0 {
		session["grade_analysis"] = gradeStats
	}
	if drift := fitHRDrift(analysis); len(drift) > 0 {
		session["hr_drift"] = drift
	}
	if temperature := fitTemperature(analysis); len(temperature) > 0 {
		session["temperature_stats"] = temperature
	}
	if climbs := fitClimbs(analysis); len(climbs) > 0 {
		out["climbs"] = climbs
	}
	powerRecords := []powerRecord{}
	for _, r := range analysis {
		p, ok := fitNumber(r["power_w"])
		powerRecords = append(powerRecords, powerRecord{power: p, valid: ok, timestamp: fitString(r["timestamp"])})
	}
	if curve := powerCurve(powerRecords); len(curve) > 0 {
		out["power_duration_curve"] = curve
	}
	if metrics := fitHRVMetrics(rr); metrics != nil {
		out["hrv"] = metrics
	}
	for _, lap := range laps {
		start, ok := parseFITTime(lap["start_time"])
		elapsed, eok := fitNumber(lap["total_elapsed_time_s"])
		if !ok || !eok || elapsed == 0 {
			continue
		}
		end := start.Add(time.Duration(elapsed * float64(time.Second)))
		if lap["avg_power_w"] == nil {
			values := []float64{}
			for _, r := range records {
				ts, tok := parseFITTime(r["timestamp"])
				p, pok := fitNumber(r["power_w"])
				if tok && pok && !ts.Before(start) && ts.Before(end) {
					values = append(values, p)
				}
			}
			if len(values) > 0 {
				sum, max := 0.0, values[0]
				for _, v := range values {
					sum += v
					if v > max {
						max = v
					}
				}
				lap["avg_power_w"] = rounded(sum/float64(len(values)), 1)
				lap["max_power_w"] = rounded(max, 1)
			}
		}
		lapRR := []rrPair{}
		for _, r := range rr {
			ts, ok := parseFITTime(r.timestamp)
			if ok && !ts.Before(start) && ts.Before(end) {
				lapRR = append(lapRR, r)
			}
		}
		if metrics := fitHRVMetrics(lapRR); metrics != nil {
			lap["hrv"] = metrics
		}
	}
	if includeRecords {
		out["records"] = records
		if len(rr) > 0 {
			pairs := []any{}
			for _, p := range rr {
				var ts any
				if p.timestamp != "" {
					ts = p.timestamp
				}
				pairs = append(pairs, map[string]any{"timestamp": ts, "rr_seconds": p.value})
			}
			out["rr_intervals_seconds"] = pairs
		}
	}
	return out
}
func fitString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func parseFITTime(v any) (time.Time, bool) {
	s := fitString(v)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		t, e := time.Parse(layout, s)
		if e == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
func primaryFITSession(sessions []map[string]any) int {
	cycling, index := 0, 0
	for i, s := range sessions {
		if s["sport"] == "cycling" {
			cycling++
			index = i
		}
	}
	if cycling == 1 {
		return index
	}
	index = 0
	for i, s := range sessions {
		n, _ := fitNumber(s["total_elapsed_time_s"])
		best, _ := fitNumber(sessions[index]["total_elapsed_time_s"])
		if n > best {
			index = i
		}
	}
	return index
}
func recordsInSession(records []map[string]any, session map[string]any) []map[string]any {
	start, ok := parseFITTime(session["start_time"])
	elapsed, eok := fitNumber(session["total_elapsed_time_s"])
	if !ok || !eok || elapsed == 0 {
		return nil
	}
	end := start.Add(time.Duration(elapsed * float64(time.Second)))
	out := []map[string]any{}
	for _, r := range records {
		ts, ok := parseFITTime(r["timestamp"])
		if !ok {
			return nil
		}
		if !ts.Before(start) && ts.Before(end) {
			out = append(out, r)
		}
	}
	return out
}
func registerFITAnalysis(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_activity_fit_data", description("get_activity_fit_data"), map[string]any{"activity_id": map[string]any{"anyOf": []any{map[string]any{"type": "integer"}, map[string]any{"type": "string"}}}, "include_records": map[string]any{"type": "boolean", "default": false}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		id, e := activityID(args["activity_id"])
		if e != nil {
			return nil, e
		}
		raw, e := s.Client.Download(ctx, fmt.Sprintf("/download-service/files/activity/%d", id), nil)
		if e != nil {
			return nil, fmt.Errorf("Error downloading FIT data for activity %d: %w", id, e)
		}
		if len(raw) == 0 {
			return fmt.Sprintf("No FIT data returned for activity %d", id), nil
		}
		msgs, e := decodeFIT(raw)
		if e != nil {
			n := len(raw)
			if n > 16 {
				n = 16
			}
			return map[string]any{"error": e.Error(), "debug": map[string]any{"total_bytes": len(raw), "first_16_bytes_hex": hex.EncodeToString(raw[:n]), "hint": "1f8b = gzip, 504b = ZIP, 0e10/0c10 = raw FIT, 3c or 7b = HTML/JSON error from Garmin"}}, nil
		}
		include, _ := args["include_records"].(bool)
		result := parseFITAnalysis(msgs, include)
		result["activity_id"] = id
		session := asMap(result["session"])
		start := fitString(session["start_time"])
		if len(start) >= 10 {
			date := start[:10]
			if day, e := time.Parse("2006-01-02", date); e == nil {
				from := day.AddDate(0, 0, -90).Format("2006-01-02")
				body, e := s.Client.GetJSON(ctx, "/weight-service/weight/dateRange", url.Values{"startDate": {from}, "endDate": {date}})
				if e == nil {
					latest := ""
					weight := 0.0
					for _, v := range slice(asMap(body)["dateWeightList"]) {
						r := asMap(v)
						d, _ := r["calendarDate"].(string)
						w, ok := fitNumber(r["weight"])
						if ok && w > 0 && d <= date && d >= latest {
							latest = d
							weight = math.RoundToEven(w/10) / 100
						}
					}
					if latest != "" {
						result["rider_weight_kg"] = weight
						result["rider_weight_date"] = latest
						for dst, src := range map[string]string{"avg_w_per_kg": "avg_power_w", "normalized_w_per_kg": "normalized_power_w"} {
							if p, ok := fitNumber(session[src]); ok && p != 0 {
								session[dst] = rounded(p/weight, 100)
							}
						}
						if climbs, ok := result["climbs"].([]map[string]any); ok {
							for _, climb := range climbs {
								if p, ok := fitNumber(climb["avg_power_w"]); ok && p != 0 {
									climb["avg_w_per_kg"] = rounded(p/weight, 100)
								}
							}
						}
					}
				}
			}
		}
		return result, nil
	}, "activity_id")
}
