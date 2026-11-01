package server

// Ports of the pure ride-analysis unit tests in test_activity_analysis_tools.py. Beyond the Python assertions,
// each case also checks the full output the pinned Python code gives for the same input (run with CPython
// 3.12 sum() semantics).

import (
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/factory"
	"github.com/muktihari/fit/profile/untyped/fieldnum"
	"github.com/muktihari/fit/profile/untyped/mesgnum"
)

// repeat is Python's [row] * n (the same dict n times).
func repeat(row map[string]any, n int) []map[string]any {
	out := make([]map[string]any, n)
	for i := range out {
		out[i] = row
	}
	return out
}

func concat(parts ...[]map[string]any) []map[string]any {
	var out []map[string]any
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// makeRecords is the Python test's _make_records.
func makeRecords(grade float64, power, cadence, hr int, speed float64, altitudeStart float64, count int) []map[string]any {
	var out []map[string]any
	alt := altitudeStart
	for i := 0; i < count; i++ {
		alt += speed * grade / 100
		out = append(out, map[string]any{"grade_pct": grade, "power_w": power, "cadence_rpm": cadence,
			"heart_rate_bpm": hr, "speed_mps": speed, "altitude_m": alt,
			"timestamp": fmt.Sprintf("2024-03-02 14:%02d:%02d", i/60, i%60)})
	}
	return out
}

var rideStart = time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)

// recordsEvery is the Python test's _records_every: rows step seconds apart, timestamped like _parse_fit.
func recordsEvery(step int, rows []map[string]any, start time.Time) []map[string]any {
	out := make([]map[string]any, len(rows))
	for i, row := range rows {
		r := map[string]any{"timestamp": pyTimeStr(start.Add(time.Duration(i*step) * time.Second))}
		for k, v := range row {
			r[k] = v
		}
		out[i] = r
	}
	return out
}

func wantJSON(t *testing.T, label string, got any, want string) {
	t.Helper()
	diffJSON(t, label, jsonValue(t, got), decode(t, want))
}

func TestPrimarySessionIndexFallsBackToLongestLeg(t *testing.T) {
	sessions := []map[string]any{
		{"sport": "cycling", "total_elapsed_time_s": 900.0},
		{"sport": "running", "total_elapsed_time_s": 2400.0},
		{"sport": "cycling", "total_elapsed_time_s": 1800.0},
	}
	if got := primarySessionIndex(sessions); got != 1 {
		t.Errorf("index = %d", got)
	}
	if got := primarySessionIndex([]map[string]any{
		{"sport": "swimming", "total_elapsed_time_s": 1200.0}, {"sport": "running", "total_elapsed_time_s": 600.0},
	}); got != 0 {
		t.Errorf("index = %d", got)
	}
}

func TestDecodeGearChangeMatchesFitProfileLayout(t *testing.T) {
	offsets := map[string]int{}
	for _, sf := range factory.CreateField(mesgnum.Event, fieldnum.EventData).SubFields {
		if sf.Name != "gear_change_data" {
			continue
		}
		off := 0
		for _, c := range sf.Components {
			offsets[factory.CreateField(mesgnum.Event, c.FieldNum).Name] = off
			off += int(c.Bits)
		}
	}
	packed := int64(5)<<offsets["rear_gear_num"] | int64(21)<<offsets["rear_gear"] |
		int64(2)<<offsets["front_gear_num"] | int64(50)<<offsets["front_gear"]
	want := map[string]any{"rear_gear_num": int64(5), "front_gear_num": int64(2), "rear_teeth": int64(21), "front_teeth": int64(50)}
	if got := decodeGearChange(packed); !reflect.DeepEqual(got, want) {
		t.Errorf("decode = %v (offsets %v)", got, offsets)
	}
	if got := decodeGearChange(5); got["rear_teeth"] != nil || got["front_teeth"] != nil {
		t.Errorf("zero teeth = %v", got)
	}
}

func shiftAt(ts string) map[string]any {
	return map[string]any{"event": "rear_gear_change", "quality": "proactive", "timestamp": ts}
}

func TestShiftSummarySpacedShiftsAreNotPanicBursts(t *testing.T) {
	var shifts []map[string]any
	for minute := 0; minute < 60; minute += 5 {
		shifts = append(shifts, shiftAt(fmt.Sprintf("2024-06-01 10:%02d:00", minute)))
	}
	if got := computeShiftSummary(shifts)["panic_burst_episodes"]; got != 0 {
		t.Errorf("episodes = %v", got)
	}
}

func TestShiftSummaryCountsClusteredShiftsAsOneEpisodeEach(t *testing.T) {
	var shifts []map[string]any
	for _, ts := range []string{"10:00:00", "10:00:02", "10:00:04", "10:00:06", "10:05:00", "10:05:03", "10:05:05"} {
		shifts = append(shifts, shiftAt("2024-06-01 "+ts))
	}
	summary := computeShiftSummary(shifts)
	if got := summary["panic_burst_episodes"]; got != 2 {
		t.Errorf("episodes = %v", got)
	}
	wantJSON(t, "summary", summary, `{"total_shifts": 7, "front_shifts": 0, "rear_shifts": 7, "proactive_shifts": 7,
		"reactive_shifts": 0, "coasting_shifts": 0, "spun_out_shifts": 0, "proactive_pct": 100.0, "reactive_pct": 0.0,
		"panic_burst_episodes": 2, "gear_usage": {}}`)
}

func TestShiftSummaryIgnoresShiftsWithoutTimestamps(t *testing.T) {
	summary := computeShiftSummary([]map[string]any{shiftAt(""), shiftAt("not a time"), {"event": "rear_gear_change"}})
	if summary["total_shifts"] != 3 || summary["panic_burst_episodes"] != 0 {
		t.Errorf("summary = %v", summary)
	}
	wantJSON(t, "summary", summary, `{"total_shifts": 3, "front_shifts": 0, "rear_shifts": 3, "proactive_shifts": 2,
		"reactive_shifts": 0, "coasting_shifts": 0, "spun_out_shifts": 0, "proactive_pct": 66.7, "reactive_pct": 0.0,
		"panic_burst_episodes": 0, "gear_usage": {}}`)
}

// The shift summary's gear_usage and by_terrain follow Python: usage sorted by count (stable), terrain by grade.
func TestShiftSummaryGearUsageOrderAndTerrain(t *testing.T) {
	shifts := []map[string]any{
		{"event": "front_gear_change", "quality": "reactive", "gear_combo": "39/21t", "grade_at_shift_pct": 5.0, "cadence_at_shift_rpm": int64(60)},
		{"event": "rear_gear_change", "quality": "proactive", "gear_combo": "53/17t", "grade_at_shift_pct": -3.0, "cadence_at_shift_rpm": int64(85)},
		{"event": "rear_gear_change", "quality": "proactive", "gear_combo": "53/17t", "cadence_at_shift_rpm": int64(90)},
		{"event": "rear_gear_change", "quality": "spun_out", "gear_combo": "39/21t", "grade_at_shift_pct": 2.9},
		{"event": "rear_gear_change", "quality": "coasting", "gear_combo": "53/11t", "grade_at_shift_pct": 3.0},
	}
	summary := computeShiftSummary(shifts)
	usage, _ := summary["gear_usage"].(*ordered[int])
	if usage == nil || !reflect.DeepEqual(usage.keys, []string{"39/21t", "53/17t", "53/11t"}) {
		t.Errorf("gear_usage = %#v", summary["gear_usage"])
	}
	wantJSON(t, "summary", summary, `{"total_shifts": 5, "front_shifts": 1, "rear_shifts": 4, "proactive_shifts": 2,
		"reactive_shifts": 1, "coasting_shifts": 1, "spun_out_shifts": 1, "proactive_pct": 40.0, "reactive_pct": 20.0,
		"panic_burst_episodes": 0, "gear_usage": {"39/21t": 2, "53/17t": 2, "53/11t": 1},
		"by_terrain": {"climbing": {"total": 2, "proactive": 0, "reactive": 1, "reactive_pct": 50.0},
			"flat": {"total": 2, "proactive": 1, "reactive": 0, "reactive_pct": 0.0},
			"descending": {"total": 1, "proactive": 1, "reactive": 0, "reactive_pct": 0.0}},
		"avg_cadence_at_shift_rpm": 78.3, "min_cadence_at_shift_rpm": 60, "max_cadence_at_shift_rpm": 90}`)
	wantJSON(t, "empty", computeShiftSummary(nil), `{"total_shifts": 0, "note": "No DI2 shift events found in FIT file"}`)
}

func powerRows(w, n int) []map[string]any { return repeat(map[string]any{"power_w": w}, n) }

func TestPowerDurationCurveBasic(t *testing.T) {
	pdc := computePowerDurationCurve(concat(powerRows(500, 10), powerRows(100, 110)))
	if pdc == nil || pdc.vals["5s"] != 500 || pdc.vals["1min"] >= 500 {
		t.Fatalf("pdc = %v", pdc)
	}
	wantJSON(t, "pdc", pdc, `{"5s": 500, "30s": 233, "1min": 167}`)
	if !reflect.DeepEqual(pdc.keys, []string{"5s", "30s", "1min"}) {
		t.Errorf("order = %v", pdc.keys)
	}
}

func TestPowerDurationCurveEmpty(t *testing.T) {
	if pdc := computePowerDurationCurve(repeat(map[string]any{"cadence_rpm": 80}, 100)); pdc != nil {
		t.Errorf("pdc = %v", pdc)
	}
}

func TestPowerDurationCurveShortRide(t *testing.T) {
	pdc := computePowerDurationCurve(powerRows(300, 30))
	if pdc == nil {
		t.Fatal("nil")
	}
	if _, ok := pdc.vals["5s"]; !ok {
		t.Error("5s missing")
	}
	if _, ok := pdc.vals["1min"]; ok {
		t.Error("1min present")
	}
	wantJSON(t, "pdc", pdc, `{"5s": 300, "30s": 300}`)
}

func TestPowerDurationCurveSlidingWindow(t *testing.T) {
	pdc := computePowerDurationCurve(concat(powerRows(100, 10), powerRows(400, 5), powerRows(100, 10)))
	if pdc == nil || pdc.vals["5s"] != 400 {
		t.Errorf("pdc = %v", pdc)
	}
}

func TestDetectClimbsBasic(t *testing.T) {
	records := concat(makeRecords(5.0, 280, 75, 160, 5.0, 100, 200), makeRecords(0.5, 180, 90, 140, 8.0, 200, 100))
	climbs := detectClimbs(records, 30, 3.0, 60)
	if len(climbs) < 1 || climbs[0]["avg_grade_pct"] != 5.0 || climbs[0]["avg_power_w"] != 280.0 || climbs[0]["vam_m_per_hr"] == nil {
		t.Fatalf("climbs = %v", climbs)
	}
	wantJSON(t, "climbs", climbs, `[{"start_time": "2024-03-02 14:00:00", "end_time": "2024-03-02 14:03:19", "duration_s": 200,
		"elevation_gain_m": 49.8, "vam_m_per_hr": 896, "distance_m": 1000, "avg_grade_pct": 5.0, "max_grade_pct": 5.0,
		"avg_power_w": 280, "avg_cadence_rpm": 75, "avg_hr_bpm": 160}]`)
}

func TestDetectClimbsNoneWhenFlat(t *testing.T) {
	if climbs := detectClimbs(makeRecords(1.0, 200, 90, 140, 9.0, 50, 300), 50, 3, 120); len(climbs) != 0 {
		t.Errorf("climbs = %v", climbs)
	}
	if climbs := detectClimbs(nil, 50, 3, 120); climbs == nil || len(climbs) != 0 {
		t.Errorf("empty = %#v", climbs)
	}
}

func TestDetectClimbsVamCalculation(t *testing.T) {
	climbs := detectClimbs(makeRecords(5.0, 280, 75, 155, 5.0, 0, 300), 30, 3, 60)
	if len(climbs) < 1 {
		t.Fatal("no climb")
	}
	if v, _ := climbs[0]["vam_m_per_hr"].(float64); v <= 500 {
		t.Errorf("vam = %v", climbs[0]["vam_m_per_hr"])
	}
	wantJSON(t, "climbs", climbs, `[{"start_time": "2024-03-02 14:00:00", "end_time": "2024-03-02 14:04:59", "duration_s": 300,
		"elevation_gain_m": 74.8, "vam_m_per_hr": 897, "distance_m": 1500, "avg_grade_pct": 5.0, "max_grade_pct": 5.0,
		"avg_power_w": 280, "avg_cadence_rpm": 75, "avg_hr_bpm": 155}]`)
}

const hrDriftNote = "Negative drift = HR increased vs power (decoupling); below -10% suggests aerobic base is limiting factor. Positive drift = power increased vs HR (not decoupling)."

func hrRows(power, hr, n int) []map[string]any {
	return repeat(map[string]any{"power_w": power, "heart_rate_bpm": hr}, n)
}

func TestHrDriftSignificant(t *testing.T) {
	drift := computeHRDrift(concat(hrRows(250, 140, 1900), hrRows(250, 165, 1900)))
	if drift == nil {
		t.Fatal("nil")
	}
	if p, _ := drift["hr_drift_pct"].(float64); p >= -10 || drift["interpretation"] != "significant_decoupling" {
		t.Errorf("drift = %v", drift)
	}
	wantJSON(t, "drift", drift, `{"hr_drift_pct": -15.2, "first_half_power_hr_ratio": 1.786, "second_half_power_hr_ratio": 1.515,
		"interpretation": "significant_decoupling", "note": "`+hrDriftNote+`"}`)
}

func TestHrDriftInterpretationUsesDriftSign(t *testing.T) {
	for _, tc := range []struct {
		hr       int
		sign     float64
		interp   string
		pct, r2  float64
		r1String string
	}{
		{155, -1, "moderate_drift", -9.7, 1.29, "1.429"},
		{132, 1, "power_increased_vs_hr", 6.1, 1.515, "1.429"},
		{125, 1, "power_increased_vs_hr", 12.0, 1.6, "1.429"},
	} {
		t.Run(fmt.Sprint(tc.hr), func(t *testing.T) {
			drift := computeHRDrift(concat(hrRows(200, 140, 1900), hrRows(200, tc.hr, 1900)))
			if drift == nil {
				t.Fatal("nil")
			}
			if p, _ := drift["hr_drift_pct"].(float64); p*tc.sign <= 5 || drift["interpretation"] != tc.interp {
				t.Errorf("drift = %v", drift)
			}
			wantJSON(t, "drift", drift, fmt.Sprintf(`{"hr_drift_pct": %v, "first_half_power_hr_ratio": %s,
				"second_half_power_hr_ratio": %v, "interpretation": %q, "note": %q}`, tc.pct, tc.r1String, tc.r2, tc.interp, hrDriftNote))
		})
	}
}

func TestHrDriftWellCoupled(t *testing.T) {
	drift := computeHRDrift(hrRows(200, 145, 4000))
	if drift == nil {
		t.Fatal("nil")
	}
	if p, _ := drift["hr_drift_pct"].(float64); math.Abs(p) >= 5 || drift["interpretation"] != "well_coupled" {
		t.Errorf("drift = %v", drift)
	}
	wantJSON(t, "drift", drift, `{"hr_drift_pct": 0.0, "first_half_power_hr_ratio": 1.379, "second_half_power_hr_ratio": 1.379,
		"interpretation": "well_coupled", "note": "`+hrDriftNote+`"}`)
}

func TestHrDriftSkippedForShortRides(t *testing.T) {
	if drift := computeHRDrift(hrRows(200, 145, 1000)); drift != nil {
		t.Errorf("drift = %v", drift)
	}
}

func TestTemperatureStatsBasic(t *testing.T) {
	records := concat(repeat(map[string]any{"temperature_c": 15, "heart_rate_bpm": 140, "power_w": 220}, 50),
		repeat(map[string]any{"temperature_c": 30, "heart_rate_bpm": 160, "power_w": 210}, 50))
	stats := computeTemperatureStats(records)
	if stats == nil {
		t.Fatal("nil")
	}
	wantJSON(t, "stats", stats, `{"avg_temp_c": 22.5, "min_temp_c": 15, "max_temp_c": 30, "temp_range_c": 15,
		"avg_hr_coolest_third_bpm": 140.0, "avg_hr_hottest_third_bpm": 160.0,
		"avg_power_coolest_third_w": 220.0, "avg_power_hottest_third_w": 210.0}`)
}

func TestTemperatureStatsNoneWhenNoData(t *testing.T) {
	if stats := computeTemperatureStats(hrRows(200, 145, 50)); stats != nil {
		t.Errorf("stats = %v", stats)
	}
}

func TestTemperatureStatsIncludeZeroPowerAndSkipMissingSamples(t *testing.T) {
	var records []map[string]any
	for _, ph := range [][2]any{{0, 0}, {200, 140}, {nil, nil}} {
		records = append(records, map[string]any{"temperature_c": 10, "power_w": ph[0], "heart_rate_bpm": ph[1]})
	}
	records = append(records, repeat(map[string]any{"temperature_c": 20}, 3)...)
	for _, ph := range [][2]any{{0, 0}, {0, 150}, {nil, nil}} {
		records = append(records, map[string]any{"temperature_c": 30, "power_w": ph[0], "heart_rate_bpm": ph[1]})
	}
	stats := computeTemperatureStats(records)
	wantJSON(t, "stats", stats, `{"avg_temp_c": 20.0, "min_temp_c": 10, "max_temp_c": 30, "temp_range_c": 20,
		"avg_hr_coolest_third_bpm": 140.0, "avg_hr_hottest_third_bpm": 150.0,
		"avg_power_coolest_third_w": 100.0, "avg_power_hottest_third_w": 0.0}`)
}

func TestGradeAnalysisBinsCorrectly(t *testing.T) {
	row := func(g float64, p, c, h int) []map[string]any {
		return repeat(map[string]any{"grade_pct": g, "power_w": p, "cadence_rpm": c, "heart_rate_bpm": h}, 60)
	}
	result := gradeAnalysis(concat(row(0.5, 180, 90, 140), row(4.5, 280, 75, 165), row(7.5, 320, 65, 175), row(-4.0, 50, 95, 120)))
	if result == nil {
		t.Fatal("nil")
	}
	gentle, _ := result["gentle"].(map[string]any)
	moderate, _ := result["moderate"].(map[string]any)
	if gp, _ := gentle["avg_power_w"].(float64); gp >= moderate["avg_power_w"].(float64) {
		t.Errorf("gentle %v moderate %v", gentle, moderate)
	}
	wantJSON(t, "bins", result, `{"descending": {"time_s": 60, "avg_power_w": 50, "avg_cadence_rpm": 95, "avg_hr_bpm": 120},
		"flat": {"time_s": 60, "avg_power_w": 180, "avg_cadence_rpm": 90, "avg_hr_bpm": 140},
		"gentle": {"time_s": 60, "avg_power_w": 280, "avg_cadence_rpm": 75, "avg_hr_bpm": 165},
		"moderate": {"time_s": 60, "avg_power_w": 320, "avg_cadence_rpm": 65, "avg_hr_bpm": 175}}`)
	if gradeAnalysis(powerRows(200, 10)) != nil {
		t.Error("no grade should give nil")
	}
}

func TestPowerDurationCurveResamples3sRecordsTo1hz(t *testing.T) {
	pdc := computePowerDurationCurve(recordsEvery(3, concat(powerRows(300, 400), powerRows(100, 800)), rideStart))
	wantJSON(t, "pdc", pdc, `{"5s": 300, "30s": 300, "1min": 300, "5min": 300, "10min": 300, "20min": 300, "60min": 167}`)
}

func TestPowerDurationCurveCountsPausesAsZero(t *testing.T) {
	before := recordsEvery(1, powerRows(400, 60), rideStart)
	after := recordsEvery(1, powerRows(400, 60), rideStart.Add(31*time.Minute))
	pdc := computePowerDurationCurve(concat(before, after))
	wantJSON(t, "pdc", pdc, `{"5s": 400, "30s": 400, "1min": 400, "5min": 80, "10min": 40, "20min": 20}`)
}

func TestDetectClimbsUsesTimestampsFor3sRecords(t *testing.T) {
	var rows []map[string]any
	for i := 0; i < 400; i++ {
		rows = append(rows, map[string]any{"grade_pct": 6.0, "power_w": 300, "speed_mps": 5.0,
			"altitude_m": 100 + 0.9*float64(i), "distance_m": 15.0 * float64(i)})
	}
	for i := 0; i < 40; i++ {
		rows = append(rows, map[string]any{"grade_pct": 0.0, "power_w": 150, "speed_mps": 9.0,
			"altitude_m": 100 + 0.9*399, "distance_m": 5985.0 + 27.0*float64(i+1)})
	}
	climbs := detectClimbs(recordsEvery(3, rows, rideStart), 50, 3, 120)
	wantJSON(t, "climbs", climbs, `[{"start_time": "2024-06-01 10:00:00", "end_time": "2024-06-01 10:19:57", "duration_s": 1200,
		"elevation_gain_m": 359.1, "vam_m_per_hr": 1077, "distance_m": 5985, "avg_grade_pct": 6.0, "max_grade_pct": 6.0,
		"avg_power_w": 300}]`)
}

func TestDetectClimbsFlatToleranceIsInSeconds(t *testing.T) {
	var rows []map[string]any
	for i := 0; i < 200; i++ {
		rows = append(rows, map[string]any{"grade_pct": 6.0, "altitude_m": 100 + 0.9*float64(i)})
	}
	rows = append(rows, repeat(map[string]any{"grade_pct": 0.0, "altitude_m": 280.0}, 15)...)
	for i := 0; i < 200; i++ {
		rows = append(rows, map[string]any{"grade_pct": 6.0, "altitude_m": 280 + 0.9*float64(i)})
	}
	climbs := detectClimbs(recordsEvery(3, rows, rideStart), 50, 3, 120)
	wantJSON(t, "climbs", climbs, `[
		{"start_time": "2024-06-01 10:00:00", "end_time": "2024-06-01 10:09:57", "duration_s": 600, "elevation_gain_m": 179.1,
		 "vam_m_per_hr": 1075, "avg_grade_pct": 6.0, "max_grade_pct": 6.0},
		{"start_time": "2024-06-01 10:10:45", "end_time": "2024-06-01 10:20:42", "duration_s": 600, "elevation_gain_m": 179.1,
		 "vam_m_per_hr": 1075, "avg_grade_pct": 6.0, "max_grade_pct": 6.0}]`)
}

func TestDetectClimbsSpeedDistanceUsesRecordSpacing(t *testing.T) {
	var rows []map[string]any
	for i := 0; i < 400; i++ {
		rows = append(rows, map[string]any{"grade_pct": 6.0, "speed_mps": 5.0, "altitude_m": 100 + 0.9*float64(i)})
	}
	climbs := detectClimbs(recordsEvery(3, rows, rideStart), 50, 3, 120)
	wantJSON(t, "climbs", climbs, `[{"start_time": "2024-06-01 10:00:00", "end_time": "2024-06-01 10:19:57", "duration_s": 1200,
		"elevation_gain_m": 359.1, "vam_m_per_hr": 1077, "distance_m": 6000, "avg_grade_pct": 6.0, "max_grade_pct": 6.0}]`)
}

func TestHrDriftRequires60MinutesNot3600Records(t *testing.T) {
	drift := computeHRDrift(recordsEvery(3, hrRows(200, 145, 1400), rideStart))
	if drift == nil || drift["interpretation"] != "well_coupled" {
		t.Errorf("drift = %v", drift)
	}
	if drift := computeHRDrift(recordsEvery(3, hrRows(200, 145, 1000), rideStart)); drift != nil {
		t.Errorf("50 min drift = %v", drift)
	}
}

func TestGradeAnalysisTimeUsesRecordSpacing(t *testing.T) {
	result := gradeAnalysis(recordsEvery(3, concat(repeat(map[string]any{"grade_pct": 0.5}, 100),
		repeat(map[string]any{"grade_pct": 4.5}, 50)), rideStart))
	wantJSON(t, "bins", result, `{"flat": {"time_s": 300}, "gentle": {"time_s": 150}}`)
}

func rrSeconds(pattern []float64, times int) []float64 {
	var out []float64
	for i := 0; i < times; i++ {
		out = append(out, pattern...)
	}
	return out
}

func TestComputeHrvMetricsReturnsNoneBelowMinimum(t *testing.T) {
	if got := computeHRVMetrics(rrSeconds([]float64{0.6}, 9)); got != nil {
		t.Errorf("got %v", got)
	}
}

func TestComputeHrvMetricsCorrectValues(t *testing.T) {
	got := computeHRVMetrics(rrSeconds([]float64{0.6, 0.7}, 10))
	want := map[string]any{"rmssd_ms": 100.0, "sdnn_ms": 51.3, "pnn50_pct": 100.0, "mean_rr_ms": 650.0, "mean_hr_bpm": 92.3, "rr_count": 20}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestComputeHrvMetricsPnn50ZeroWhenNoLargeDiffs(t *testing.T) {
	got := computeHRVMetrics(rrSeconds([]float64{0.6}, 20))
	want := map[string]any{"rmssd_ms": 0.0, "sdnn_ms": 0.0, "pnn50_pct": 0.0, "mean_rr_ms": 600.0, "mean_hr_bpm": 100.0, "rr_count": 20}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

// pySum follows CPython 3.12+ sum(): exact ints, then Neumaier-compensated floats.
func TestPySumMatchesCPython312(t *testing.T) {
	tenths := make([]any, 10)
	for i := range tenths {
		tenths[i] = 0.1
	}
	for _, tc := range []struct {
		in   []any
		want float64
	}{
		{tenths, 1.0}, // 3.10 gives 0.9999999999999999
		{[]any{1, 2, int64(3)}, 6},
		{[]any{1e100, 1.0, -1e100}, 1.0},
		{[]any{}, 0},
		{[]any{0.1, 0.2, 0.3}, 0.6},
	} {
		if got := pySum(tc.in); got != tc.want {
			t.Errorf("pySum(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseFitTimestamp(t *testing.T) {
	want := time.Date(2024, 3, 2, 14, 23, 45, 0, time.UTC)
	for _, in := range []any{"2024-03-02 14:23:45", "2024-03-02T14:23:45", "2024-03-02T14:23:45Z", "2024-03-02T16:23:45+02:00", want} {
		if got, ok := parseFitTimestamp(in); !ok || !got.Equal(want) {
			t.Errorf("%v -> %v %v", in, got, ok)
		}
	}
	if got, ok := parseFitTimestamp("2024-03-02 14:23:45.500000"); !ok || got.Nanosecond() != 5e8 {
		t.Errorf("fraction -> %v %v", got, ok)
	}
	if got, ok := parseFitTimestamp("2024-03-02"); !ok || !got.Equal(time.Date(2024, 3, 2, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("date -> %v %v", got, ok)
	}
	for _, in := range []any{nil, "", "not a time", "2024-13-02 10:00:00", "2024-02-30", 12345} {
		if got, ok := parseFitTimestamp(in); ok {
			t.Errorf("%v -> %v", in, got)
		}
	}
	if got := pyTimeStr(time.Date(2024, 3, 2, 14, 0, 0, 1500, time.UTC)); got != "2024-03-02 14:00:00.000001" {
		t.Errorf("pyTimeStr = %s", got)
	}
}

func TestDecodeLeftRightBalance(t *testing.T) {
	for _, tc := range []struct {
		in        any
		perRecord bool
		want      any
	}{
		{int64(4700), false, 47.0},
		{int64(0x8000 | 5300), false, 47.0},
		{int64(0x80 | 52), true, 48.0},
		{int64(48), true, 48.0},
		{int64(10001), false, nil}, // over 100 %
		{"right", false, nil},      // fitparse renders 0x8000 as its label
		{nil, false, nil},
		{47.9, false, 0.5},          // int() truncates
		{int64(0x3FFF), false, nil}, // 163.83 %
	} {
		if got := decodeLeftRightBalance(tc.in, tc.perRecord); got != tc.want {
			t.Errorf("decode(%v, %v) = %v, want %v", tc.in, tc.perRecord, got, tc.want)
		}
	}
}

func TestRecordGapsS(t *testing.T) {
	ts := func(s ...string) []map[string]any {
		out := make([]map[string]any, len(s))
		for i := range s {
			out[i] = map[string]any{"timestamp": s[i]}
		}
		return out
	}
	for _, tc := range []struct {
		recs []map[string]any
		want []int
	}{
		{ts("2024-01-01 10:00:00", "2024-01-01 10:00:03", "2024-01-01 10:00:30"), []int{3, 27, 1}},
		{ts("2024-01-01 10:00:00", "2024-01-01 10:00:04"), []int{4, 4}},
		{ts("2024-01-01 10:00:00"), []int{1}},
		{ts("2024-01-01 10:00:05", "2024-01-01 10:00:00"), []int{1, 1}},
		{ts("2024-01-01 10:00:00", ""), []int{1, 1}},
		{nil, []int{}},
	} {
		if got := recordGapsS(tc.recs); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("gaps = %v, want %v", got, tc.want)
		}
	}
	if got := recordSeconds(ts("2024-01-01 10:00:00", "2024-01-01 10:00:30", "2024-01-01 10:00:33")); !reflect.DeepEqual(got, []int{1, 3, 3}) {
		t.Errorf("seconds = %v", got)
	}
}

func TestDeriveGradesAndFillShiftGrades(t *testing.T) {
	var records []map[string]any
	for i := 0; i < 50; i++ {
		records = append(records, map[string]any{"distance_m": 5.0 * float64(i), "altitude_m": 100 + 0.3*float64(i)})
	}
	shiftNone := map[string]any{"event": "rear_gear_change"}
	shiftKept := map[string]any{"event": "rear_gear_change", "grade_at_shift_pct": 1.0}
	shiftFirst := map[string]any{"event": "rear_gear_change"}
	if src := deriveGrades(records); src != "derived" {
		t.Fatalf("source = %q", src)
	}
	if records[0]["grade_pct"] != 6.0 || records[25]["grade_pct"] != 6.0 {
		t.Errorf("grades = %v %v", records[0]["grade_pct"], records[25]["grade_pct"])
	}
	if _, ok := records[1]["grade_pct"]; !ok {
		t.Error("record 1 (10 m span... 55 m) should have a grade")
	}
	fillShiftGrades([]shiftAtRecord{{shiftNone, 30}, {shiftKept, 30}, {shiftFirst, 0}}, records)
	if shiftNone["grade_at_shift_pct"] != 6.0 || shiftKept["grade_at_shift_pct"] != 1.0 {
		t.Errorf("shifts = %v %v", shiftNone, shiftKept)
	}
	if _, ok := shiftFirst["grade_at_shift_pct"]; ok {
		t.Error("shift before any record got a grade")
	}

	device := []map[string]any{{"grade_pct": 2.0}, {"altitude_m": 1.0, "distance_m": 0.0}}
	if src := deriveGrades(device); src != "device" {
		t.Errorf("device source = %q", src)
	}
	if src := deriveGrades([]map[string]any{{"power_w": 1}}); src != "" {
		t.Errorf("none source = %q", src)
	}
}
