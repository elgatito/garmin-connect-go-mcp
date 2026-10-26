package server

// Port of the ride-analysis helpers in activity_analysis.py: gear/balance decoding, record timing, the power
// duration curve, grade derivation, climbs, grade bins, HR drift, temperature, shift summary and HRV.
// Records and shifts are map[string]any like the Python dicts (shared by reference, as Python's are). Values
// keep Python's int/float split (int, int64 or uint64 = int; float64 = float) because sum() treats them apart.

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// pyNum reads a Python int or float.
func pyNum(v any) (f float64, isInt, ok bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true, true
	case int64:
		return float64(x), true, true
	case uint64:
		return float64(x), true, true
	case float64:
		return x, false, true
	}
	return 0, false, false
}

// recNum is r[key] as a number; false when it is missing or None.
func recNum(r map[string]any, key string) (float64, bool) {
	f, _, ok := pyNum(r[key])
	return f, ok
}

// pySum is CPython 3.12+ sum() (the Docker image runs 3.12): ints add exactly; the first float switches to
// float addition with Neumaier compensation for later floats, while later ints are added uncompensated.
func pySum(xs []any) float64 {
	var i int64
	var f, c float64
	inFloat := false
	for _, x := range xs {
		v, isInt, ok := pyNum(x)
		if !ok {
			continue
		}
		switch {
		case !inFloat && isInt:
			i += int64(v)
		case !inFloat:
			f, inFloat = float64(i)+v, true
		case isInt:
			f += v
		default:
			t := f + v
			if math.Abs(f) >= math.Abs(v) {
				c += (f - t) + v
			} else {
				c += (v - t) + f
			}
			f = t
		}
	}
	if !inFloat {
		return float64(i)
	}
	if c != 0 && !math.IsInf(c, 0) && !math.IsNaN(c) {
		f += c
	}
	return f
}

func pySumFloats(xs []float64) float64 {
	vals := make([]any, len(xs))
	for i, x := range xs {
		vals[i] = x
	}
	return pySum(vals)
}

// roundInt is Python's round(x) without ndigits (an int, so never -0).
func roundInt(x float64) float64 {
	if r := round(x, 0); r != 0 {
		return r
	}
	return 0
}

// safeAvg is _safe_avg over values that are already not None.
func safeAvg(vals []any) (float64, bool) {
	if len(vals) == 0 {
		return 0, false
	}
	return pySum(vals) / float64(len(vals)), true
}

// recValues is [r[key] for r in recs if r.get(key) is not None].
func recValues(recs []map[string]any, key string) []any {
	var out []any
	for _, r := range recs {
		if r[key] != nil {
			out = append(out, r[key])
		}
	}
	return out
}

// pyMinMax is Python's min()/max() of numbers: the first element holding the extreme value.
func pyMinMax(vals []any, wantMax bool) (any, float64) {
	var best any
	var bf float64
	for _, v := range vals {
		f, _, _ := pyNum(v)
		if best == nil || (wantMax && f > bf) || (!wantMax && f < bf) {
			best, bf = v, f
		}
	}
	return best, bf
}

// decodeGearChange is _decode_gear_change for a packed gear_change_data value.
func decodeGearChange(data int64) map[string]any {
	teeth := func(n int64) any {
		if n > 0 {
			return n
		}
		return nil
	}
	return map[string]any{
		"rear_gear_num":  data & 0xFF,
		"front_gear_num": (data >> 16) & 0xFF,
		"rear_teeth":     teeth((data >> 8) & 0xFF),
		"front_teeth":    teeth((data >> 24) & 0xFF),
	}
}

// decodeLeftRightBalance is _decode_left_right_balance: the left leg's power share, or nil.
func decodeLeftRightBalance(v any, perRecord bool) any {
	if v == nil {
		return nil
	}
	n, err := pyInt(v)
	if err != nil {
		return nil
	}
	var right bool
	var pct float64
	if perRecord {
		right, pct = n&0x80 != 0, float64(n&0x7F)
	} else {
		right, pct = n&0x8000 != 0, float64(n&0x3FFF)/100.0
	}
	if pct > 100 {
		return nil
	}
	if right {
		return round(100.0-pct, 1)
	}
	return round(pct, 1)
}

// pyTimeStr is str(datetime) for a naive UTC datetime.
func pyTimeStr(t time.Time) string {
	if us := t.Nanosecond() / 1000; us != 0 {
		return t.Format("2006-01-02 15:04:05") + "." + strconv.Itoa(1000000 + us)[1:]
	}
	return t.Format("2006-01-02 15:04:05")
}

// fitStr is str(v or "") for a FIT value.
func fitStr(v any) string {
	if t, ok := v.(time.Time); ok {
		return pyTimeStr(t)
	}
	if falsy(v) {
		return ""
	}
	return pyStr(v)
}

// shortcut: covers the datetime.fromisoformat forms str(datetime) and common ISO 8601 strings produce
// (YYYY-MM-DD, any separator, HH[:MM[:SS[.f+]]], ±HH:MM offset); extend if other forms ever reach it.
var isoDateTimeRe = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})(?:.(\d{2})(?::(\d{2})(?::(\d{2})(?:[.,](\d+))?)?)?(?:([+-])(\d{2}):(\d{2}))?)?$`)

// parseFitTimestamp is _parse_fit_timestamp: a datetime, or its str(), as naive UTC.
func parseFitTimestamp(v any) (time.Time, bool) {
	if t, ok := v.(time.Time); ok {
		return t.UTC(), true
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return time.Time{}, false
	}
	m := isoDateTimeRe.FindStringSubmatch(strings.ReplaceAll(s, "Z", "+00:00"))
	if m == nil {
		return time.Time{}, false
	}
	n := func(i int) int { x, _ := strconv.Atoi(m[i]); return x }
	frac := 0
	if m[7] != "" {
		digits := (m[7] + "000000")[:6]
		frac, _ = strconv.Atoi(digits)
	}
	y, mo, d, h, mi, sec := n(1), n(2), n(3), n(4), n(5), n(6)
	t := time.Date(y, time.Month(mo), d, h, mi, sec, frac*1000, time.UTC)
	if y < 1 || t.Year() != y || int(t.Month()) != mo || t.Day() != d || t.Hour() != h || t.Minute() != mi || t.Second() != sec {
		return time.Time{}, false
	}
	if m[8] != "" {
		oh, om := n(9), n(10)
		if oh > 23 || om > 59 {
			return time.Time{}, false
		}
		off := time.Duration(oh)*time.Hour + time.Duration(om)*time.Minute
		if m[8] == "+" {
			t = t.Add(-off)
		} else {
			t = t.Add(off)
		}
	}
	return t, true
}

// pyTimedelta is timedelta(seconds=s): rounded to whole microseconds, half to even.
func pyTimedelta(seconds float64) time.Duration {
	return time.Duration(math.RoundToEven(seconds*1e6)) * time.Microsecond
}

// ── Record timing (smart recording, pauses) ──────────────────────────────────

// maxRecordGapS is _MAX_RECORD_GAP_S: a longer gap between records is a pause, not slow sampling.
const maxRecordGapS = 10

// recordGapsS is _record_gaps_s: seconds from each record to the next (1 s each without usable timestamps).
func recordGapsS(records []map[string]any) []int {
	ones := func() []int {
		out := make([]int, len(records))
		for i := range out {
			out[i] = 1
		}
		return out
	}
	times := make([]time.Time, len(records))
	for i, r := range records {
		t, ok := parseFitTimestamp(r["timestamp"])
		if !ok {
			return ones()
		}
		times[i] = t
	}
	if len(records) == 0 {
		return []int{}
	}
	gaps := make([]int, 0, len(records))
	for i := 0; i+1 < len(times); i++ {
		g := int(times[i+1].Sub(times[i]).Seconds())
		if g < 0 {
			return ones()
		}
		gaps = append(gaps, g)
	}
	last := 1
	if len(gaps) > 0 {
		last = heldSeconds(gaps[len(gaps)-1])
	}
	return append(gaps, last)
}

// heldSeconds is _held_seconds: a record holds for its gap, or 1 s when the gap is a pause.
func heldSeconds(gap int) int {
	if gap <= maxRecordGapS {
		return gap
	}
	return 1
}

// recordSeconds is _record_seconds.
func recordSeconds(records []map[string]any) []int {
	gaps := recordGapsS(records)
	for i, g := range gaps {
		gaps[i] = heldSeconds(g)
	}
	return gaps
}

// ── Power Duration Curve ─────────────────────────────────────────────────────

var (
	pdcDurationsS = []int{5, 30, 60, 300, 600, 1200, 3600}
	pdcLabels     = []string{"5s", "30s", "1min", "5min", "10min", "20min", "60min"}
)

// computePowerDurationCurve is _compute_power_duration_curve: best mean power per duration, from power
// resampled to 1 Hz (each sample held until the next record; pauses count as 0 W). nil when there is none.
func computePowerDurationCurve(records []map[string]any) *ordered[float64] {
	hasPower := false
	for _, r := range records {
		hasPower = hasPower || r["power_w"] != nil
	}
	if !hasPower {
		return nil
	}
	var filled []float64
	for i, gap := range recordGapsS(records) {
		p, _, _ := pyNum(records[i]["power_w"])
		held := heldSeconds(gap)
		for k := 0; k < held; k++ {
			filled = append(filled, p)
		}
		for k := 0; k < min(gap-held, pdcDurationsS[len(pdcDurationsS)-1]); k++ {
			filled = append(filled, 0)
		}
	}
	n := len(filled)
	if n == 0 {
		return nil
	}
	prefix := make([]float64, n+1)
	for i, p := range filled {
		prefix[i+1] = prefix[i] + p
	}
	curve := newOrdered[float64]()
	for k, d := range pdcDurationsS {
		if n < d {
			continue
		}
		best := 0.0
		for i := 0; i+d <= n; i++ {
			if s := prefix[i+d] - prefix[i]; s > best {
				best = s
			}
		}
		if avg := best / float64(d); avg > 0 {
			curve.set(pdcLabels[k], roundInt(avg))
		}
	}
	if len(curve.keys) == 0 {
		return nil
	}
	return curve
}

// ── Grade derivation ─────────────────────────────────────────────────────────

const (
	gradeWindowM          = 100.0
	gradeMinSpanM         = 20.0
	gradeClampPct         = 40.0
	minDeviceGradeShare   = 0.05
	climbMinElevationGain = 50.0
	climbMinAvgGradePct   = 3.0
	climbMinDurationS     = 120
)

// deriveGrades is _derive_grades: fills missing grade_pct from altitude over a distance window centred on each
// record. Returns "device", "derived", or "" when no record ends up with a grade.
func deriveGrades(records []map[string]any) string {
	deviceCount := 0
	for _, r := range records {
		if r["grade_pct"] != nil {
			deviceCount++
		}
	}
	if len(records) > 0 && float64(deviceCount) >= minDeviceGradeShare*float64(len(records)) {
		return "device"
	}
	type point struct {
		idx       int
		dist, alt float64
	}
	var points []point
	for i, r := range records {
		d, okD := recNum(r, "distance_m")
		a, okA := recNum(r, "altitude_m")
		if okD && okA {
			points = append(points, point{i, d, a})
		}
	}
	half := gradeWindowM / 2
	derived, lo, hi := 0, 0, 0
	for _, p := range points {
		for lo < len(points)-1 && points[lo].dist < p.dist-half {
			lo++
		}
		hi = max(hi, lo)
		for hi+1 < len(points) && points[hi+1].dist <= p.dist+half {
			hi++
		}
		span := points[hi].dist - points[lo].dist
		if span < gradeMinSpanM || records[p.idx]["grade_pct"] != nil {
			continue
		}
		grade := (points[hi].alt - points[lo].alt) / span * 100
		records[p.idx]["grade_pct"] = round(math.Max(-gradeClampPct, math.Min(gradeClampPct, grade)), 2)
		derived++
	}
	switch {
	case derived > 0:
		return "derived"
	case deviceCount > 0:
		return "device"
	}
	return ""
}

// shiftAtRecord pairs a shift with how many records had been parsed when it occurred.
type shiftAtRecord struct {
	shift    map[string]any
	nRecords int
}

// fillShiftGrades is _fill_shift_grades: grade_at_shift_pct from the nearest preceding record with a grade.
func fillShiftGrades(pairs []shiftAtRecord, records []map[string]any) {
	lastGrades := make([]any, len(records))
	var last any
	for i, r := range records {
		if r["grade_pct"] != nil {
			last = r["grade_pct"]
		}
		lastGrades[i] = last
	}
	for _, p := range pairs {
		if _, ok := p.shift["grade_at_shift_pct"]; ok || p.nRecords == 0 {
			continue
		}
		if g, _, ok := pyNum(lastGrades[p.nRecords-1]); ok {
			p.shift["grade_at_shift_pct"] = round(g, 1)
		}
	}
}

// ── Climb detection + VAM ────────────────────────────────────────────────────

// detectClimbs is _detect_climbs: a climb starts at grade >= minAvgGradePct and ends once the grade has
// stayed below it for more than 30 s.
func detectClimbs(records []map[string]any, minElevationGainM, minAvgGradePct float64, minDurationS int) []map[string]any {
	climbs := []map[string]any{}
	if len(records) == 0 {
		return climbs
	}
	const flatToleranceS = 30
	held := recordSeconds(records)
	keep := func(c map[string]any) {
		gain, _, _ := pyNum(getDefault(c, "elevation_gain_m", 0))
		dur, _, _ := pyNum(getDefault(c, "duration_s", 0))
		if gain >= minElevationGainM && dur >= float64(minDurationS) {
			climbs = append(climbs, c)
		}
	}
	inClimb := false
	start, flatCount, flatS := 0, 0, 0
	for i, r := range records {
		grade, ok := recNum(r, "grade_pct")
		if !ok {
			continue
		}
		switch {
		case !inClimb:
			if grade >= minAvgGradePct {
				inClimb, start, flatCount, flatS = true, i, 0, 0
			}
		case grade < minAvgGradePct:
			flatCount++
			flatS += held[i]
			if flatS > flatToleranceS {
				keep(summarizeClimb(records, start, i-flatCount, held))
				inClimb, flatCount, flatS = false, 0, 0
			}
		default:
			flatCount, flatS = 0, 0
		}
	}
	if inClimb {
		keep(summarizeClimb(records, start, len(records)-1, held))
	}
	return climbs
}

// summarizeClimb is _summarize_climb over records[start:end+1]; held gives the seconds each record covers.
func summarizeClimb(records []map[string]any, start, end int, held []int) map[string]any {
	if end+1 <= start {
		return map[string]any{}
	}
	seg, segHeld := records[start:end+1], held[start:end+1]
	duration := 0
	for _, h := range segHeld {
		duration += h
	}
	result := map[string]any{
		"start_time": getDefault(seg[0], "timestamp", ""),
		"end_time":   getDefault(seg[len(seg)-1], "timestamp", ""),
		"duration_s": duration,
	}
	startAlt, okS := recNum(seg[0], "altitude_m")
	endAlt, okE := recNum(seg[len(seg)-1], "altitude_m")
	if okS && okE {
		gain := endAlt - startAlt
		result["elevation_gain_m"] = round(gain, 1)
		if duration > 0 {
			result["vam_m_per_hr"] = roundInt((gain / float64(duration)) * 3600)
		}
	}
	startDist, okS := recNum(seg[0], "distance_m")
	endDist, okE := recNum(seg[len(seg)-1], "distance_m")
	var dists []any // speed * held, per record with a speed
	for i, r := range seg {
		s, isInt, ok := pyNum(r["speed_mps"])
		switch {
		case !ok:
		case isInt:
			dists = append(dists, int64(s)*int64(segHeld[i]))
		default:
			dists = append(dists, s*float64(segHeld[i]))
		}
	}
	if okS && okE {
		result["distance_m"] = roundInt(endDist - startDist)
	} else if len(dists) > 0 {
		result["distance_m"] = roundInt(pySum(dists))
	}
	if grades := recValues(seg, "grade_pct"); len(grades) > 0 {
		avg, _ := safeAvg(grades)
		_, mx := pyMinMax(grades, true)
		result["avg_grade_pct"], result["max_grade_pct"] = round(avg, 1), round(mx, 1)
	}
	for _, kv := range [][2]string{{"power_w", "avg_power_w"}, {"cadence_rpm", "avg_cadence_rpm"}, {"heart_rate_bpm", "avg_hr_bpm"}} {
		if avg, ok := safeAvg(recValues(seg, kv[0])); ok {
			result[kv[1]] = roundInt(avg)
		}
	}
	return result
}

// ── Grade analysis ───────────────────────────────────────────────────────────

var gradeBins = []string{"descending", "flat", "gentle", "moderate", "steep"}

// gradeAnalysis is _grade_analysis: time and average power/cadence/HR per grade bin; nil without grades.
func gradeAnalysis(records []map[string]any) map[string]any {
	recs := map[string][]map[string]any{}
	timeS := map[string]int{}
	hasGrade := false
	held := recordSeconds(records)
	for i, r := range records {
		grade, ok := recNum(r, "grade_pct")
		if !ok {
			continue
		}
		hasGrade = true
		bucket := "steep"
		switch {
		case grade < -3:
			bucket = "descending"
		case grade < 3:
			bucket = "flat"
		case grade < 6:
			bucket = "gentle"
		case grade < 9:
			bucket = "moderate"
		}
		recs[bucket] = append(recs[bucket], r)
		timeS[bucket] += held[i]
	}
	if !hasGrade {
		return nil
	}
	result := map[string]any{}
	for _, key := range gradeBins {
		if len(recs[key]) == 0 {
			continue
		}
		entry := map[string]any{"time_s": timeS[key]}
		for _, kv := range [][2]string{{"power_w", "avg_power_w"}, {"cadence_rpm", "avg_cadence_rpm"}, {"heart_rate_bpm", "avg_hr_bpm"}} {
			if avg, ok := safeAvg(recValues(recs[key], kv[0])); ok {
				entry[kv[1]] = roundInt(avg)
			}
		}
		result[key] = entry
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// ── HR drift (aerobic decoupling) ────────────────────────────────────────────

// computeHRDrift is _compute_hr_drift: power:HR ratio change between the halves of the records with both
// power and HR; needs 60 min of such data by timestamps.
func computeHRDrift(records []map[string]any) map[string]any {
	const minDurationS = 3600
	var filtered []map[string]any
	filteredS := 0
	held := recordSeconds(records)
	for i, r := range records {
		hr, ok := recNum(r, "heart_rate_bpm")
		if r["power_w"] != nil && ok && hr > 0 {
			filtered = append(filtered, r)
			filteredS += held[i]
		}
	}
	if filteredS < minDurationS {
		return nil
	}
	mid := len(filtered) / 2
	ratio := func(recs []map[string]any) (float64, bool) {
		p, okP := safeAvg(recValues(recs, "power_w"))
		h, okH := safeAvg(recValues(recs, "heart_rate_bpm"))
		if !okP || !okH || h == 0 {
			return 0, false
		}
		return p / h, true
	}
	r1, ok1 := ratio(filtered[:mid])
	r2, ok2 := ratio(filtered[mid:])
	if !ok1 || !ok2 || r1 == 0 {
		return nil
	}
	drift := ((r2 - r1) / r1) * 100
	interpretation := "significant_decoupling"
	switch {
	case math.Abs(drift) < 5:
		interpretation = "well_coupled"
	case drift > 0:
		interpretation = "power_increased_vs_hr"
	case drift > -10:
		interpretation = "moderate_drift"
	}
	return map[string]any{
		"hr_drift_pct":               round(drift, 1),
		"first_half_power_hr_ratio":  round(r1, 3),
		"second_half_power_hr_ratio": round(r2, 3),
		"interpretation":             interpretation,
		"note": "Negative drift = HR increased vs power (decoupling); below -10% suggests " +
			"aerobic base is limiting factor. Positive drift = power increased vs HR " +
			"(not decoupling).",
	}
}

// ── Temperature correlation ──────────────────────────────────────────────────

// computeTemperatureStats is _compute_temperature_stats: temperature range and HR/power in the coolest vs
// hottest third of the records; nil without temperatures.
func computeTemperatureStats(records []map[string]any) map[string]any {
	temps := recValues(records, "temperature_c")
	if len(temps) == 0 {
		return nil
	}
	avgTemp, _ := safeAvg(temps)
	minRaw, minF := pyMinMax(temps, false)
	maxRaw, maxF := pyMinMax(temps, true)
	var byTemp []map[string]any
	for _, r := range records {
		if r["temperature_c"] != nil {
			byTemp = append(byTemp, r)
		}
	}
	sort.SliceStable(byTemp, func(i, j int) bool {
		a, _ := recNum(byTemp[i], "temperature_c")
		b, _ := recNum(byTemp[j], "temperature_c")
		return a < b
	})
	third := max(1, len(byTemp)/3)
	coolest, hottest := byTemp[:third], byTemp[len(byTemp)-third:]
	truthyHR := func(recs []map[string]any) []any {
		var out []any
		for _, r := range recs {
			if !falsy(r["heart_rate_bpm"]) {
				out = append(out, r["heart_rate_bpm"])
			}
		}
		return out
	}
	result := map[string]any{
		"avg_temp_c":   round(avgTemp, 1),
		"min_temp_c":   minRaw,
		"max_temp_c":   maxRaw,
		"temp_range_c": round(maxF-minF, 1),
	}
	for key, vals := range map[string][]any{
		"avg_hr_coolest_third_bpm":  truthyHR(coolest),
		"avg_hr_hottest_third_bpm":  truthyHR(hottest),
		"avg_power_coolest_third_w": recValues(coolest, "power_w"),
		"avg_power_hottest_third_w": recValues(hottest, "power_w"),
	} {
		if avg, ok := safeAvg(vals); ok {
			result[key] = round(avg, 1)
		}
	}
	return result
}

// ── Shift summary ────────────────────────────────────────────────────────────

var shiftQualities = []string{"proactive", "reactive", "coasting", "spun_out", "unknown"}

// computeShiftSummary is _compute_shift_summary.
func computeShiftSummary(shifts []map[string]any) map[string]any {
	if len(shifts) == 0 {
		return map[string]any{"total_shifts": 0, "note": "No DI2 shift events found in FIT file"}
	}
	qualityCounts := map[string]int{}
	terrainQuality := map[string]map[string]int{"climbing": {}, "flat": {}, "descending": {}}

	// Burst detection: 3+ shifts within 10 s are one episode; shifts without a parseable timestamp are ignored.
	const burstMinShifts, burstWindowS = 3, 10
	var times []time.Time
	for _, s := range shifts {
		if t, ok := parseFitTimestamp(s["timestamp"]); ok {
			times = append(times, t)
		}
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	bursts := 0
	for i := 0; i < len(times); {
		end := i
		for end+1 < len(times) && times[end+1].Sub(times[i]).Seconds() <= burstWindowS {
			end++
		}
		if end-i+1 >= burstMinShifts {
			bursts++
			i = end + 1
		} else {
			i++
		}
	}

	front, rear := 0, 0
	gearCounts := newOrdered[int]()
	var cadences []any
	hasTerrain := false
	for _, s := range shifts {
		quality := pyStr(getDefault(s, "quality", "unknown"))
		qualityCounts[quality]++
		terrain := "flat"
		if g, ok := recNum(s, "grade_at_shift_pct"); ok {
			hasTerrain = true
			if g >= 3 {
				terrain = "climbing"
			} else if g <= -3 {
				terrain = "descending"
			}
		}
		terrainQuality[terrain][quality]++
		if strings.Contains(pyStr(getDefault(s, "event", "")), "front") {
			front++
		} else {
			rear++
		}
		if combo := s["gear_combo"]; !falsy(combo) {
			key := pyStr(combo)
			gearCounts.set(key, gearCounts.vals[key]+1)
		}
		if s["cadence_at_shift_rpm"] != nil {
			cadences = append(cadences, s["cadence_at_shift_rpm"])
		}
	}
	sort.SliceStable(gearCounts.keys, func(i, j int) bool {
		return gearCounts.vals[gearCounts.keys[i]] > gearCounts.vals[gearCounts.keys[j]]
	})

	total := len(shifts)
	pct := func(n, of int) float64 { return round(float64(n)/float64(of)*100, 1) }
	summary := map[string]any{
		"total_shifts":         total,
		"front_shifts":         front,
		"rear_shifts":          rear,
		"proactive_shifts":     qualityCounts["proactive"],
		"reactive_shifts":      qualityCounts["reactive"],
		"coasting_shifts":      qualityCounts["coasting"],
		"spun_out_shifts":      qualityCounts["spun_out"],
		"proactive_pct":        pct(qualityCounts["proactive"], total),
		"reactive_pct":         pct(qualityCounts["reactive"], total),
		"panic_burst_episodes": bursts,
		"gear_usage":           gearCounts,
	}
	if hasTerrain {
		byTerrain := map[string]any{}
		for terrain, counts := range terrainQuality {
			tTotal := 0
			for _, n := range counts {
				tTotal += n
			}
			if tTotal > 0 {
				byTerrain[terrain] = map[string]any{"total": tTotal, "proactive": counts["proactive"],
					"reactive": counts["reactive"], "reactive_pct": pct(counts["reactive"], tTotal)}
			}
		}
		if len(byTerrain) > 0 {
			summary["by_terrain"] = byTerrain
		}
	}
	if len(cadences) > 0 {
		summary["avg_cadence_at_shift_rpm"] = round(pySum(cadences)/float64(len(cadences)), 1)
		summary["min_cadence_at_shift_rpm"], _ = pyMinMax(cadences, false)
		summary["max_cadence_at_shift_rpm"], _ = pyMinMax(cadences, true)
	}
	return summary
}

// ── HRV ──────────────────────────────────────────────────────────────────────

// computeHRVMetrics is _compute_hrv_metrics: RMSSD, SDNN, pNN50 and mean R-R from R-R intervals in seconds;
// nil below 10 intervals. x ** 0.5 is math.Sqrt (Python calls libm pow, correctly rounded for 0.5 in practice).
func computeHRVMetrics(rrS []float64) map[string]any {
	if len(rrS) < 10 {
		return nil
	}
	rrMs := make([]float64, len(rrS))
	for i, r := range rrS {
		rrMs[i] = r * 1000.0
	}
	diffs := make([]float64, len(rrMs)-1)
	squared := make([]float64, len(diffs))
	nn50 := 0
	for i := range diffs {
		diffs[i] = rrMs[i+1] - rrMs[i]
		squared[i] = diffs[i] * diffs[i]
		if math.Abs(diffs[i]) > 50.0 {
			nn50++
		}
	}
	rmssd, pnn50 := 0.0, 0.0
	if len(squared) > 0 {
		rmssd = math.Sqrt(pySumFloats(squared) / float64(len(squared)))
		pnn50 = 100.0 * float64(nn50) / float64(len(diffs))
	}
	meanRR := pySumFloats(rrMs) / float64(len(rrMs))
	dev := make([]float64, len(rrMs))
	for i, r := range rrMs {
		dev[i] = (r - meanRR) * (r - meanRR)
	}
	sdnn := math.Sqrt(pySumFloats(dev) / float64(len(rrMs)-1))
	var meanHR any
	if meanRR > 0 {
		meanHR = round(60000.0/meanRR, 1)
	}
	return map[string]any{
		"rmssd_ms":    round(rmssd, 1),
		"sdnn_ms":     round(sdnn, 1),
		"pnn50_pct":   round(pnn50, 2),
		"mean_rr_ms":  round(meanRR, 1),
		"mean_hr_bpm": meanHR,
		"rr_count":    len(rrMs),
	}
}

// ── Sessions (multisport) ────────────────────────────────────────────────────

// primarySessionIndex is _primary_session_index: the only cycling leg, else the longest (first on ties).
func primarySessionIndex(sessions []map[string]any) int {
	var cycling []int
	for i, s := range sessions {
		if s["sport"] == "cycling" {
			cycling = append(cycling, i)
		}
	}
	if len(cycling) == 1 {
		return cycling[0]
	}
	elapsed := func(i int) float64 {
		f, _, _ := pyNum(firstTruthy(sessions[i]["total_elapsed_time_s"], 0))
		return f
	}
	best := 0
	for i := 1; i < len(sessions); i++ {
		if elapsed(i) > elapsed(best) {
			best = i
		}
	}
	return best
}

// recordsInSession is _records_in_session: records in [start_time, start_time + elapsed); false when the
// window or any record timestamp cannot be determined.
func recordsInSession(records []map[string]any, session map[string]any) ([]map[string]any, bool) {
	start, ok := parseFitTimestamp(session["start_time"])
	elapsed, _, okE := pyNum(session["total_elapsed_time_s"])
	if !ok || !okE || elapsed == 0 {
		return nil, false
	}
	end := start.Add(pyTimedelta(elapsed))
	times := make([]time.Time, len(records))
	for i, r := range records {
		t, ok := parseFitTimestamp(r["timestamp"])
		if !ok {
			return nil, false
		}
		times[i] = t
	}
	out := []map[string]any{}
	for i, r := range records {
		if !times[i].Before(start) && times[i].Before(end) {
			out = append(out, r)
		}
	}
	return out, true
}
