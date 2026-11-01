package server

// Ports of the get_activity_fit_data and get_power_duration_curve tests. The Python tests patch fitparse
// with fake messages; these encode the same messages as real FIT bytes (physical values converted to the
// field's raw units) and serve them from the fake Garmin, so the real decode path runs.

import (
	"fmt"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/factory"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/profile/untyped/fieldnum"
	"github.com/muktihari/fit/profile/untyped/mesgnum"
	"github.com/muktihari/fit/proto"
)

// fitRaw converts a physical value to the field's raw integer: (v + offset) * scale.
func fitRawOf(f proto.Field, v float64) any {
	raw := math.Round((v + f.Offset) * f.Scale)
	switch f.BaseType {
	case basetype.Sint8:
		return int8(raw)
	case basetype.Uint16, basetype.Uint16z:
		return uint16(raw)
	case basetype.Sint16:
		return int16(raw)
	case basetype.Uint32, basetype.Uint32z:
		return uint32(raw)
	case basetype.Sint32:
		return int32(raw)
	}
	return uint8(raw)
}

// fitP is a field set from its physical value (one value) or values (an array field).
func fitP(mesg typedef.MesgNum, field byte, vs ...float64) proto.Field {
	f := factory.CreateField(mesg, field)
	if len(vs) == 1 && !f.Array {
		return f.WithValue(fitRawOf(f, vs[0]))
	}
	switch f.BaseType {
	case basetype.Uint16:
		out := make([]uint16, len(vs))
		for i, v := range vs {
			out[i], _ = fitRawOf(f, v).(uint16)
		}
		return f.WithValue(out)
	default:
		out := make([]uint8, len(vs))
		for i, v := range vs {
			out[i], _ = fitRawOf(f, v).(uint8)
		}
		return f.WithValue(out)
	}
}

func fitTimeField(mesg typedef.MesgNum, field byte, t time.Time) proto.Field {
	return fitF(mesg, field, uint32(t.Unix()-fitUTCReference))
}

func fitTS(mesg typedef.MesgNum, t time.Time) proto.Field {
	return fitTimeField(mesg, proto.FieldNumTimestamp, t)
}

func ts(s string) time.Time {
	t, err := time.Parse(time.DateTime, s)
	if err != nil {
		panic(err)
	}
	return t
}

func record(fields ...proto.Field) proto.Message { return fitMesg(mesgnum.Record, fields...) }

func rearShift(gear uint32, at time.Time) proto.Message {
	return fitMesg(mesgnum.Event,
		fitTS(mesgnum.Event, at),
		fitF(mesgnum.Event, fieldnum.EventEvent, typedef.EventRearGearChange),
		fitF(mesgnum.Event, fieldnum.EventData, gear))
}

func fileID() proto.Message {
	return fitMesg(mesgnum.FileId, fitF(mesgnum.FileId, fieldnum.FileIdType, typedef.FileActivity))
}

// callFitData serves fit for the activity and calls get_activity_fit_data.
func callFitData(t *testing.T, fit []byte, args map[string]any, setup func(f *fakeGarmin)) (map[string]any, *fakeGarmin) {
	t.Helper()
	f := newFakeGarmin(t)
	f.on(fitOriginalRoute, rawBody(fit))
	if setup != nil {
		setup(f)
	}
	if args == nil {
		args = map[string]any{}
	}
	args["activity_id"] = fitActivityID
	data, _ := decode(t, callOK(t, newSession(t, f, nil), "get_activity_fit_data", args)).(map[string]any)
	return data, f
}

func fitDataOf(t *testing.T, args map[string]any, msgs ...proto.Message) map[string]any {
	t.Helper()
	data, _ := callFitData(t, fitFile(t, append([]proto.Message{fileID()}, msgs...)...), args, nil)
	return data
}

func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }
func arr(v any) []any          { l, _ := v.([]any); return l }

func TestGetActivityFitDataCallsDownload(t *testing.T) {
	_, f := callFitData(t, fitFile(t, fileID()), nil, nil)
	calls := f.requests(fitOriginalRoute)
	if len(calls) != 1 {
		t.Fatalf("downloads = %d", len(calls))
	}
	for _, c := range f.requests("") {
		if strings.HasPrefix(c.Path, "/download-service/") && c.Path != "/download-service/files/activity/22041393449" {
			t.Errorf("unexpected download %s", c.Path)
		}
	}
}

func TestGetActivityFitDataEmptyResponse(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(fitOriginalRoute, rawBody{})
	text := callOK(t, newSession(t, f, nil), "get_activity_fit_data", map[string]any{"activity_id": fitActivityID})
	if text != "No FIT data returned for activity 22041393449" {
		t.Errorf("text = %q", text)
	}
}

// Python's mocked None download is a 204 here, which downloads as b"" in Python.
func TestGetActivityFitDataNoneResponse(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(fitOriginalRoute, status(http.StatusNoContent, rawBody{}))
	text, _ := callTool(t, newSession(t, f, nil), "get_activity_fit_data", map[string]any{"activity_id": fitActivityID})
	if !strings.Contains(text, "No FIT data") && !strings.Contains(text, "Error") {
		t.Errorf("text = %q", text)
	}
}

func TestGetActivityFitDataErrorHandling(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(fitOriginalRoute, status(http.StatusInternalServerError, map[string]any{"message": "network error"}))
	text, isErr := callTool(t, newSession(t, f, nil), "get_activity_fit_data", map[string]any{"activity_id": fitActivityID})
	if !isErr || !strings.HasPrefix(text, "Error downloading FIT data for activity 22041393449: ") {
		t.Errorf("text = %q (isErr %v)", text, isErr)
	}
	text, isErr = callTool(t, newSession(t, f, nil), "get_activity_fit_data", map[string]any{"activity_id": "abc"})
	if !isErr || text != "Error downloading FIT data for activity abc: invalid literal for int() with base 10: 'abc'" {
		t.Errorf("text = %q (isErr %v)", text, isErr)
	}
}

func TestGetActivityFitDataParseFailureReturnsDebug(t *testing.T) {
	data, _ := callFitData(t, []byte("<html>oops</html>"), nil, nil)
	debug := obj(data["debug"])
	if data["error"] == nil || data["error"] == "" || data["activity_id"] != nil ||
		debug["total_bytes"] != 17.0 || debug["first_16_bytes_hex"] != "3c68746d6c3e6f6f70733c2f68746d6c" || debug["hint"] != fitDebugHint {
		t.Errorf("data = %v", data)
	}
}

func TestGetActivityFitDataSessionFields(t *testing.T) {
	data := fitDataOf(t, nil, fitMesg(mesgnum.Session,
		fitF(mesgnum.Session, fieldnum.SessionSport, typedef.SportCycling),
		fitP(mesgnum.Session, fieldnum.SessionTotalElapsedTime, 3120.0),
		fitP(mesgnum.Session, fieldnum.SessionTotalDistance, 56320.0),
		fitP(mesgnum.Session, fieldnum.SessionAvgPower, 185),
		fitP(mesgnum.Session, fieldnum.SessionNormalizedPower, 210),
		fitP(mesgnum.Session, fieldnum.SessionAvgCadence, 83),
		fitP(mesgnum.Session, fieldnum.SessionAvgHeartRate, 148),
		fitP(mesgnum.Session, fieldnum.SessionAvgLeftPco, 3),
		fitP(mesgnum.Session, fieldnum.SessionAvgRightPco, -8),
	))
	s := obj(data["session"])
	want := map[string]any{"sport": "cycling", "start_time": "", "total_elapsed_time_s": 3120.0, "total_distance_m": 56320.0,
		"avg_power_w": 185.0, "normalized_power_w": 210.0, "avg_cadence_rpm": 83.0, "avg_heart_rate_bpm": 148.0,
		"avg_left_pco_mm": 3.0, "avg_right_pco_mm": -8.0, "variability_index": 1.135}
	if !reflect.DeepEqual(s, want) {
		t.Errorf("session = %v", s)
	}
	for _, k := range []string{"multisport", "sessions", "analyzed_session_index"} {
		if _, ok := data[k]; ok {
			t.Errorf("%s present", k)
		}
	}
}

var triStart = time.Date(2024, 6, 1, 8, 0, 0, 0, time.UTC)

// brickLegs is the Python test's _BRICK_LEGS: (sport, start offset s, duration s, record power W or -1).
var brickLegs = []struct {
	sport            typedef.Sport
	offset, duration int
	power            int
}{
	{typedef.SportRunning, 0, 600, 350},
	{typedef.SportTransition, 600, 60, -1},
	{typedef.SportCycling, 660, 1200, 200},
	{typedef.SportRunning, 1860, 600, 350},
}

// multisportMessages is _multisport_messages: records for every leg, then one session per leg.
func multisportMessages(withTimestamps bool) []proto.Message {
	var msgs []proto.Message
	for _, leg := range brickLegs {
		for i := 0; i < leg.duration; i++ {
			fields := []proto.Field{fitP(mesgnum.Record, fieldnum.RecordHeartRate, 150)}
			if leg.power >= 0 {
				fields = append(fields, fitP(mesgnum.Record, fieldnum.RecordPower, float64(leg.power)))
			}
			if withTimestamps {
				fields = append(fields, fitTS(mesgnum.Record, triStart.Add(time.Duration(leg.offset+i)*time.Second)))
			}
			msgs = append(msgs, record(fields...))
		}
	}
	for _, leg := range brickLegs {
		fields := []proto.Field{
			fitF(mesgnum.Session, fieldnum.SessionSport, leg.sport),
			fitTimeField(mesgnum.Session, fieldnum.SessionStartTime, triStart.Add(time.Duration(leg.offset)*time.Second)),
			fitP(mesgnum.Session, fieldnum.SessionTotalElapsedTime, float64(leg.duration)),
		}
		if leg.sport == typedef.SportCycling {
			fields = append(fields, fitP(mesgnum.Session, fieldnum.SessionAvgPower, 200))
		}
		msgs = append(msgs, fitMesg(mesgnum.Session, fields...))
	}
	return msgs
}

func TestGetActivityFitDataMultisportListsEverySession(t *testing.T) {
	data := fitDataOf(t, nil, multisportMessages(true)...)
	if data["multisport"] != true {
		t.Fatalf("multisport = %v", data["multisport"])
	}
	var sports []any
	for _, s := range arr(data["sessions"]) {
		sports = append(sports, obj(s)["sport"])
	}
	if !reflect.DeepEqual(sports, []any{"running", "transition", "cycling", "running"}) {
		t.Errorf("sports = %v", sports)
	}
	bike := obj(arr(data["sessions"])[2])
	if bike["start_time"] != "2024-06-01 08:11:00" || bike["total_elapsed_time_s"] != 1200.0 {
		t.Errorf("bike = %v", bike)
	}
	if _, ok := bike["grade_analysis"]; ok {
		t.Error("sessions entry got analyses")
	}
	if data["analyzed_session_index"] != 2.0 || obj(data["session"])["sport"] != "cycling" || obj(data["session"])["avg_power_w"] != 200.0 {
		t.Errorf("analyzed = %v session = %v", data["analyzed_session_index"], data["session"])
	}
	if note, _ := data["analysis_note"].(string); note != "Multisport file: session and record-based analyses (power curve, climbs, "+
		"grade, HR drift, temperature) cover leg 2 (cycling) only. See sessions for every leg." {
		t.Errorf("note = %q", note)
	}
}

func TestGetActivityFitDataMultisportPowerCurveIsBikeOnly(t *testing.T) {
	data := fitDataOf(t, nil, multisportMessages(true)...)
	pdc := obj(data["power_duration_curve"])
	if pdc["5s"] != 200.0 || pdc["20min"] != 200.0 {
		t.Errorf("pdc = %v", pdc)
	}
	if _, ok := pdc["60min"]; ok {
		t.Error("60min present")
	}
}

func TestGetActivityFitDataMultisportWithoutTimestampsFallsBack(t *testing.T) {
	data := fitDataOf(t, nil, multisportMessages(false)...)
	if obj(data["session"])["sport"] != "cycling" || obj(data["power_duration_curve"])["5s"] != 350.0 {
		t.Errorf("session = %v pdc = %v", data["session"], data["power_duration_curve"])
	}
	if note, _ := data["analysis_note"].(string); note != "Multisport file: session summarises leg 2 (cycling), but record "+
		"timestamps or the session window are missing, so record-based analyses cover all legs." {
		t.Errorf("note = %q", note)
	}
}

func packedGear(front, frontNum, rear, rearNum uint32) uint32 {
	return front<<24 | frontNum<<16 | rear<<8 | rearNum
}

func TestGetActivityFitDataShiftEvents(t *testing.T) {
	data := fitDataOf(t, nil,
		record(fitP(mesgnum.Record, fieldnum.RecordCadence, 65), fitP(mesgnum.Record, fieldnum.RecordPower, 200),
			fitP(mesgnum.Record, fieldnum.RecordHeartRate, 150), fitP(mesgnum.Record, fieldnum.RecordSpeed, 8.5),
			fitP(mesgnum.Record, fieldnum.RecordAltitude, 120.0)),
		rearShift(packedGear(53, 2, 16, 5), ts("2024-03-02 14:23:45")),
	)
	shifts := arr(data["shifts"])
	if len(shifts) != 1 {
		t.Fatalf("shifts = %v", shifts)
	}
	want := map[string]any{"timestamp": "2024-03-02 14:23:45", "event": "rear_gear_change", "cadence_at_shift_rpm": 65.0,
		"quality": "reactive", "front_teeth": 53.0, "rear_teeth": 16.0, "front_gear_num": 2.0, "rear_gear_num": 5.0,
		"gear_combo": "53/16t"}
	if !reflect.DeepEqual(shifts[0], want) {
		t.Errorf("shift = %v", shifts[0])
	}
}

func TestGetActivityFitDataProactiveShift(t *testing.T) {
	data := fitDataOf(t, nil, record(fitP(mesgnum.Record, fieldnum.RecordCadence, 88)),
		rearShift(packedGear(39, 1, 19, 4), ts("2024-03-02 14:30:00")))
	if q := obj(arr(data["shifts"])[0])["quality"]; q != "proactive" {
		t.Errorf("quality = %v", q)
	}
}

func TestGetActivityFitDataShiftSummary(t *testing.T) {
	var msgs []proto.Message
	for _, c := range []struct {
		cadence float64
		at      string
	}{{60, "14:20:00"}, {60, "14:21:00"}, {85, "14:25:00"}} {
		msgs = append(msgs, record(fitP(mesgnum.Record, fieldnum.RecordCadence, c.cadence)),
			rearShift(packedGear(53, 2, 17, 4), ts("2024-03-02 "+c.at)))
	}
	summary := obj(fitDataOf(t, nil, msgs...)["shift_summary"])
	if summary["total_shifts"] != 3.0 || summary["reactive_shifts"] != 2.0 || summary["proactive_shifts"] != 1.0 ||
		obj(summary["gear_usage"])["53/17t"] != 3.0 {
		t.Errorf("summary = %v", summary)
	}
}

func TestGetActivityFitDataRecordsExcludedByDefault(t *testing.T) {
	data := fitDataOf(t, nil, record(fitP(mesgnum.Record, fieldnum.RecordCadence, 85),
		fitP(mesgnum.Record, fieldnum.RecordPower, 210), fitP(mesgnum.Record, fieldnum.RecordHeartRate, 145)))
	if _, ok := data["records"]; ok {
		t.Error("records present")
	}
}

func TestGetActivityFitDataRecordsIncludedWhenRequested(t *testing.T) {
	data := fitDataOf(t, map[string]any{"include_records": true}, record(
		fitP(mesgnum.Record, fieldnum.RecordCadence, 85), fitP(mesgnum.Record, fieldnum.RecordPower, 210),
		fitP(mesgnum.Record, fieldnum.RecordHeartRate, 145), fitP(mesgnum.Record, fieldnum.RecordSpeed, 9.2),
		fitP(mesgnum.Record, fieldnum.RecordAltitude, 130.0), fitTS(mesgnum.Record, ts("2024-03-02 14:00:00")),
		fitP(mesgnum.Record, fieldnum.RecordLeftPco, 3), fitP(mesgnum.Record, fieldnum.RecordRightPco, -9)))
	records := arr(data["records"])
	if len(records) != 1 {
		t.Fatalf("records = %v", records)
	}
	want := map[string]any{"timestamp": "2024-03-02 14:00:00", "cadence_rpm": 85.0, "power_w": 210.0, "heart_rate_bpm": 145.0,
		"speed_mps": 9.2, "altitude_m": 130.0, "left_pco_mm": 3.0, "right_pco_mm": -9.0}
	if !reflect.DeepEqual(records[0], want) {
		t.Errorf("record = %v", records[0])
	}
}

func TestGetActivityFitDataPowerBalance(t *testing.T) {
	data := fitDataOf(t, nil, fitMesg(mesgnum.Session, fitF(mesgnum.Session, fieldnum.SessionSport, typedef.SportCycling),
		fitF(mesgnum.Session, fieldnum.SessionLeftRightBalance, typedef.LeftRightBalance100(4700))))
	s := obj(data["session"])
	if s["avg_left_power_pct"] != 47.0 || s["avg_right_power_pct"] != 53.0 {
		t.Errorf("session = %v", s)
	}
}

func TestGetActivityFitDataPowerBalanceUsesProfileFieldTypes(t *testing.T) {
	data := fitDataOf(t, map[string]any{"include_records": true},
		record(fitF(mesgnum.Record, fieldnum.RecordLeftRightBalance, typedef.LeftRightBalance(0x80|52))),
		fitMesg(mesgnum.Lap, fitF(mesgnum.Lap, fieldnum.LapLeftRightBalance, typedef.LeftRightBalance100(0x8000|5100))),
		fitMesg(mesgnum.Session, fitF(mesgnum.Session, fieldnum.SessionLeftRightBalance, typedef.LeftRightBalance100(0x8000|5300))),
	)
	s, lap, rec := obj(data["session"]), obj(arr(data["laps"])[0]), obj(arr(data["records"])[0])
	if s["avg_left_power_pct"] != 47.0 || s["avg_right_power_pct"] != 53.0 ||
		lap["avg_left_power_pct"] != 49.0 || lap["avg_right_power_pct"] != 51.0 ||
		rec["left_power_pct"] != 48.0 || rec["right_power_pct"] != 52.0 {
		t.Errorf("session %v lap %v record %v", s, lap, rec)
	}
}

// Real FIT stores power phase as uint8 at scale 0.7111111; fitparse yields raw / 0.7111111 (the Python
// test's mock passed 337.5 directly).
func TestGetActivityFitDataSessionPowerPhaseUsesProfileIndexes(t *testing.T) {
	data := fitDataOf(t, nil, fitMesg(mesgnum.Session,
		fitF(mesgnum.Session, fieldnum.SessionAvgLeftPowerPhase, []uint8{240, 16}),
		fitF(mesgnum.Session, fieldnum.SessionAvgRightPowerPhase, []uint8{246, 11})))
	s := obj(data["session"])
	scale := 0.7111111
	if s["avg_left_power_phase_start_deg"] != 240/scale || s["avg_left_power_phase_end_deg"] != 16/scale ||
		s["avg_right_power_phase_start_deg"] != 246/scale || s["avg_right_power_phase_end_deg"] != 11/scale {
		t.Errorf("session = %v", s)
	}
}

func TestGetActivityFitDataNoShifts(t *testing.T) {
	data := fitDataOf(t, nil, fitMesg(mesgnum.Session, fitF(mesgnum.Session, fieldnum.SessionSport, typedef.SportCycling)))
	summary := obj(data["shift_summary"])
	if summary["total_shifts"] != 0.0 || summary["note"] != "No DI2 shift events found in FIT file" {
		t.Errorf("summary = %v", summary)
	}
	if shifts, ok := data["shifts"].([]any); !ok || len(shifts) != 0 {
		t.Errorf("shifts = %#v", data["shifts"])
	}
}

func TestGetActivityFitDataVariabilityIndexSession(t *testing.T) {
	data := fitDataOf(t, nil, fitMesg(mesgnum.Session, fitF(mesgnum.Session, fieldnum.SessionSport, typedef.SportCycling),
		fitP(mesgnum.Session, fieldnum.SessionNormalizedPower, 210), fitP(mesgnum.Session, fieldnum.SessionAvgPower, 175)))
	if vi := obj(data["session"])["variability_index"]; vi != round(210.0/175, 3) {
		t.Errorf("vi = %v", vi)
	}
}

func TestGetActivityFitDataVariabilityIndexLap(t *testing.T) {
	data := fitDataOf(t, nil, fitMesg(mesgnum.Lap, fitP(mesgnum.Lap, fieldnum.LapNormalizedPower, 240),
		fitP(mesgnum.Lap, fieldnum.LapAvgPower, 200), fitP(mesgnum.Lap, fieldnum.LapTotalElapsedTime, 1800.0)))
	laps := arr(data["laps"])
	if len(laps) != 1 || obj(laps[0])["variability_index"] != round(240.0/200, 3) {
		t.Errorf("laps = %v", laps)
	}
}

// Pedal smoothness is uint8 at scale 2, so 31.2/28.4 (the Python mock values) become 31.5/28.5.
func TestGetActivityFitDataLapTorqueAndSmoothness(t *testing.T) {
	data := fitDataOf(t, nil, fitMesg(mesgnum.Lap,
		fitP(mesgnum.Lap, fieldnum.LapAvgLeftTorqueEffectiveness, 82.5), fitP(mesgnum.Lap, fieldnum.LapAvgRightTorqueEffectiveness, 79.0),
		fitP(mesgnum.Lap, fieldnum.LapAvgLeftPedalSmoothness, 31.5), fitP(mesgnum.Lap, fieldnum.LapAvgRightPedalSmoothness, 28.5),
		fitP(mesgnum.Lap, fieldnum.LapTotalElapsedTime, 3600.0)))
	lap := obj(arr(data["laps"])[0])
	if lap["avg_left_torque_effectiveness_pct"] != 82.5 || lap["avg_right_torque_effectiveness_pct"] != 79.0 ||
		lap["avg_left_pedal_smoothness_pct"] != 31.5 || lap["avg_right_pedal_smoothness_pct"] != 28.5 {
		t.Errorf("lap = %v", lap)
	}
}

func TestGetActivityFitDataShiftTerrainClassification(t *testing.T) {
	gear := packedGear(53, 2, 17, 4)
	data := fitDataOf(t, nil,
		record(fitP(mesgnum.Record, fieldnum.RecordCadence, 75), fitP(mesgnum.Record, fieldnum.RecordGrade, 5.5)),
		rearShift(gear, ts("2024-03-02 14:20:00")),
		record(fitP(mesgnum.Record, fieldnum.RecordCadence, 90), fitP(mesgnum.Record, fieldnum.RecordGrade, 1.0)),
		rearShift(gear, ts("2024-03-02 14:21:00")),
	)
	shifts := arr(data["shifts"])
	if obj(shifts[0])["grade_at_shift_pct"] != 5.5 || obj(shifts[1])["grade_at_shift_pct"] != 1.0 {
		t.Errorf("shifts = %v", shifts)
	}
	terrain := obj(obj(data["shift_summary"])["by_terrain"])
	if terrain["climbing"] == nil || terrain["flat"] == nil {
		t.Errorf("by_terrain = %v", terrain)
	}
}

func tsRecord(at string, fields ...proto.Field) proto.Message {
	return record(append(fields, fitTS(mesgnum.Record, ts(at)))...)
}

func hrvMesg(rr ...float64) proto.Message {
	return fitMesg(mesgnum.Hrv, fitP(mesgnum.Hrv, fieldnum.HrvTime, rr...))
}

func TestFitNoHrvMessagesProducesNoHrvKey(t *testing.T) {
	data := fitDataOf(t, nil, record(fitP(mesgnum.Record, fieldnum.RecordHeartRate, 140)))
	if _, ok := data["hrv"]; ok {
		t.Error("hrv present")
	}
}

func TestFitHrvSummaryPresentWithEnoughIntervals(t *testing.T) {
	data := fitDataOf(t, nil, tsRecord("2026-05-15 10:00:00"), hrvMesg(rrSeconds([]float64{0.6, 0.7}, 10)...))
	want := map[string]any{"rmssd_ms": 100.0, "sdnn_ms": 51.3, "pnn50_pct": 100.0, "mean_rr_ms": 650.0, "mean_hr_bpm": 92.3, "rr_count": 20.0}
	if !reflect.DeepEqual(data["hrv"], want) {
		t.Errorf("hrv = %v", data["hrv"])
	}
}

// 65.535 s is FIT's invalid uint16 (decoded as None); 65.534 s is valid and dropped by the range check.
func TestFitHrvSentinelValuesFiltered(t *testing.T) {
	rr := append(rrSeconds([]float64{0.6}, 20), 65.535, 65.535, 65.535, 65.534, 65.534)
	data := fitDataOf(t, nil, tsRecord("2026-05-15 10:00:00"), hrvMesg(rr...))
	if obj(data["hrv"])["rr_count"] != 20.0 {
		t.Errorf("hrv = %v", data["hrv"])
	}
}

func TestFitHrvNotProducedBelowMinimumSamples(t *testing.T) {
	data := fitDataOf(t, nil, tsRecord("2026-05-15 10:00:00"), hrvMesg(rrSeconds([]float64{0.6}, 9)...))
	if _, ok := data["hrv"]; ok {
		t.Error("hrv present")
	}
}

func TestFitHrvRrIntervalsExcludedByDefault(t *testing.T) {
	data := fitDataOf(t, nil, tsRecord("2026-05-15 10:00:00"), hrvMesg(rrSeconds([]float64{0.65}, 20)...))
	if _, ok := data["rr_intervals_seconds"]; ok {
		t.Error("rr_intervals_seconds present")
	}
	if _, ok := data["hrv"]; !ok {
		t.Error("hrv missing")
	}
}

func TestFitHrvRrIntervalsIncludedWhenRecordsRequested(t *testing.T) {
	data := fitDataOf(t, map[string]any{"include_records": true}, tsRecord("2026-05-15 10:00:00"),
		hrvMesg(rrSeconds([]float64{0.65}, 20)...))
	stream := arr(data["rr_intervals_seconds"])
	if len(stream) != 20 || !reflect.DeepEqual(stream[0], map[string]any{"timestamp": "2026-05-15 10:00:00", "rr_seconds": 0.65}) {
		t.Errorf("stream = %v", stream)
	}
}

func hrvLaps(lap2 []float64) []proto.Message {
	lap := func(start string) proto.Message {
		return fitMesg(mesgnum.Lap, fitTimeField(mesgnum.Lap, fieldnum.LapStartTime, ts(start)),
			fitP(mesgnum.Lap, fieldnum.LapTotalElapsedTime, 600.0))
	}
	return []proto.Message{
		tsRecord("2026-05-15 10:05:00"), hrvMesg(rrSeconds([]float64{0.6, 0.7}, 10)...),
		tsRecord("2026-05-15 10:15:00"), hrvMesg(lap2...),
		lap("2026-05-15 10:00:00"), lap("2026-05-15 10:10:00"),
	}
}

func TestFitHrvPerLapBucketing(t *testing.T) {
	data := fitDataOf(t, nil, hrvLaps(rrSeconds([]float64{0.6, 0.7}, 10))...)
	laps := arr(data["laps"])
	if len(laps) != 2 || obj(obj(laps[0])["hrv"])["rr_count"] != 20.0 || obj(obj(laps[1])["hrv"])["rr_count"] != 20.0 {
		t.Errorf("laps = %v", laps)
	}
}

func TestFitHrvLapBelowMinimumGetsNoHrv(t *testing.T) {
	data := fitDataOf(t, nil, hrvLaps(rrSeconds([]float64{0.6}, 5))...)
	laps := arr(data["laps"])
	if len(laps) != 2 || obj(laps[0])["hrv"] == nil || obj(laps[1])["hrv"] != nil {
		t.Errorf("laps = %v", laps)
	}
}

// Legacy altitude is uint16 at 0.2 m (scale 5, offset 500), so the Python mock's 312.9 becomes raw 4064,
// which fitparse and decodeFIT both read as 4064/5 - 500 = 312.79999999999995.
func TestGetActivityFitDataRecordsReadEnhancedAltitudeAndSpeed(t *testing.T) {
	data := fitDataOf(t, map[string]any{"include_records": true},
		tsRecord("2026-10-07 20:15:00", fitP(mesgnum.Record, fieldnum.RecordPower, 228),
			fitP(mesgnum.Record, fieldnum.RecordEnhancedAltitude, 312.4), fitP(mesgnum.Record, fieldnum.RecordEnhancedSpeed, 2.1),
			fitP(mesgnum.Record, fieldnum.RecordDistance, 7412.5)),
		tsRecord("2026-10-07 20:15:01", fitP(mesgnum.Record, fieldnum.RecordAltitude, 312.8),
			fitP(mesgnum.Record, fieldnum.RecordSpeed, 2.0)),
	)
	records := arr(data["records"])
	r0, r1 := obj(records[0]), obj(records[1])
	legacyRaw := 4064.0 // a variable, so the division is float64 arithmetic, not an exact constant
	if r0["altitude_m"] != 312.4 || r0["speed_mps"] != 2.1 || r0["distance_m"] != 7412.5 ||
		r1["altitude_m"] != legacyRaw/5-500 || r1["speed_mps"] != 2.0 {
		t.Errorf("records = %v", records)
	}
}

func TestGetActivityFitDataSessionReadsEnhancedSpeed(t *testing.T) {
	data := fitDataOf(t, nil, fitMesg(mesgnum.Session, fitP(mesgnum.Session, fieldnum.SessionEnhancedAvgSpeed, 5.27),
		fitP(mesgnum.Session, fieldnum.SessionEnhancedMaxSpeed, 15.1)))
	s := obj(data["session"])
	if s["avg_speed_mps"] != 5.27 || s["max_speed_mps"] != 15.1 {
		t.Errorf("session = %v", s)
	}
}

// rideMessages is _ride_messages: 60 s flat, 400 s steady 6 % climb at 5 m/s, 60 s flat, 1 Hz, with a rear
// shift mid-climb and another on the closing flat.
func rideMessages(gradeField, altitude, distance bool) []proto.Message {
	start := time.Date(2024, 3, 2, 14, 0, 0, 0, time.UTC)
	gear := packedGear(53, 2, 17, 4)
	var msgs []proto.Message
	dist, alt := 0.0, 0.0
	for s := 0; s < 520; s++ {
		onClimb := s >= 60 && s < 460
		speed, grade, power, cadence, hr := 8.0, 0.0, 180.0, 90.0, 140.0
		if onClimb {
			speed, grade, power, cadence, hr = 5.0, 6.0, 280, 75, 160
		}
		dist += speed
		alt += speed * grade / 100
		at := start.Add(time.Duration(s) * time.Second)
		fields := []proto.Field{fitTS(mesgnum.Record, at), fitP(mesgnum.Record, fieldnum.RecordPower, power),
			fitP(mesgnum.Record, fieldnum.RecordCadence, cadence), fitP(mesgnum.Record, fieldnum.RecordHeartRate, hr),
			fitP(mesgnum.Record, fieldnum.RecordEnhancedSpeed, speed)}
		if altitude {
			fields = append(fields, fitP(mesgnum.Record, fieldnum.RecordEnhancedAltitude, 100.0+alt))
		}
		if distance {
			fields = append(fields, fitP(mesgnum.Record, fieldnum.RecordDistance, dist))
		}
		if gradeField {
			fields = append(fields, fitP(mesgnum.Record, fieldnum.RecordGrade, grade))
		}
		msgs = append(msgs, record(fields...))
		if s == 300 || s == 500 {
			msgs = append(msgs, rearShift(gear, at))
		}
	}
	return msgs
}

func near(v any, want, tol float64) bool {
	f, ok := v.(float64)
	return ok && math.Abs(f-want) <= tol
}

func TestFitDataDerivesGradeWhenDeviceWritesNone(t *testing.T) {
	data := fitDataOf(t, nil, rideMessages(false, true, true)...)
	if data["grade_source"] != "derived" {
		t.Errorf("grade_source = %v", data["grade_source"])
	}
	climbs := arr(data["climbs"])
	if len(climbs) != 1 || !near(obj(climbs[0])["avg_grade_pct"], 6.0, 0.5) || !near(obj(climbs[0])["elevation_gain_m"], 120, 10) {
		t.Errorf("climbs = %v", climbs)
	}
	bins := obj(obj(data["session"])["grade_analysis"])
	climbing, _ := obj(bins["gentle"])["time_s"].(float64)
	moderate, _ := obj(bins["moderate"])["time_s"].(float64)
	if bins["flat"] == nil || climbing+moderate <= 300 {
		t.Errorf("bins = %v", bins)
	}
	shifts := arr(data["shifts"])
	if !near(obj(shifts[0])["grade_at_shift_pct"], 6.0, 0.5) || !near(obj(shifts[1])["grade_at_shift_pct"], 0.0, 0.5) {
		t.Errorf("shifts = %v", shifts)
	}
	terrain := obj(obj(data["shift_summary"])["by_terrain"])
	if terrain["climbing"] == nil || terrain["flat"] == nil {
		t.Errorf("by_terrain = %v", terrain)
	}
}

func TestFitDataKeepsDeviceGrade(t *testing.T) {
	data := fitDataOf(t, nil, rideMessages(true, true, true)...)
	climb := obj(arr(data["climbs"])[0])
	shifts := arr(data["shifts"])
	if data["grade_source"] != "device" || climb["avg_grade_pct"] != 6.0 || climb["max_grade_pct"] != 6.0 ||
		obj(obj(obj(data["session"])["grade_analysis"])["moderate"])["time_s"] != 400.0 ||
		obj(shifts[0])["grade_at_shift_pct"] != 6.0 || obj(shifts[1])["grade_at_shift_pct"] != 0.0 {
		t.Errorf("data = %v", data)
	}
}

func TestFitDataNoGradeWithoutAltitudeOrDistance(t *testing.T) {
	for _, missing := range []string{"altitude", "distance"} {
		t.Run(missing, func(t *testing.T) {
			data := fitDataOf(t, nil, rideMessages(false, missing != "altitude", missing != "distance")...)
			for _, k := range []string{"grade_source", "climbs"} {
				if _, ok := data[k]; ok {
					t.Errorf("%s present", k)
				}
			}
			if _, ok := obj(data["session"])["grade_analysis"]; ok {
				t.Error("grade_analysis present")
			}
			for _, s := range arr(data["shifts"]) {
				if _, ok := obj(s)["grade_at_shift_pct"]; ok {
					t.Errorf("shift grade %v", s)
				}
			}
			if _, ok := obj(data["shift_summary"])["by_terrain"]; ok {
				t.Error("by_terrain present")
			}
		})
	}
}

func TestGetActivityFitDataWrappedLapDistanceIsNulled(t *testing.T) {
	data := fitDataOf(t, nil,
		fitMesg(mesgnum.Lap, fitP(mesgnum.Lap, fieldnum.LapTotalElapsedTime, 0), fitP(mesgnum.Lap, fieldnum.LapTotalDistance, 0.2)),
		fitMesg(mesgnum.Lap, fitP(mesgnum.Lap, fieldnum.LapTotalElapsedTime, 0),
			fitF(mesgnum.Lap, fieldnum.LapTotalDistance, uint32(0xFFFFFFFC)), // -4 cm wrapped: 42949672.92 m
			fitP(mesgnum.Lap, fieldnum.LapAvgHeartRate, 140)),
	)
	laps := arr(data["laps"])
	l0, l1 := obj(laps[0]), obj(laps[1])
	if len(laps) != 2 || l0["total_distance_m"] != 0.2 || l0["distance_invalid"] != nil ||
		l1["total_distance_m"] != nil || l1["distance_invalid"] != true || l1["avg_heart_rate_bpm"] != 140.0 {
		t.Errorf("laps = %v", laps)
	}
	if _, ok := l1["total_distance_m"]; ok {
		t.Error("wrapped distance key kept")
	}
}

const weightRoute = "GET /weight-service/weight/dateRange"

func weighInSession(extra ...proto.Field) proto.Message {
	return fitMesg(mesgnum.Session, append([]proto.Field{fitF(mesgnum.Session, fieldnum.SessionSport, typedef.SportCycling),
		fitTimeField(mesgnum.Session, fieldnum.SessionStartTime, ts("2024-03-02 14:00:00")),
		fitP(mesgnum.Session, fieldnum.SessionAvgPower, 210)}, extra...)...)
}

func TestGetActivityFitDataUsesLatestWeighInBeforeRide(t *testing.T) {
	data, f := callFitData(t, fitFile(t, fileID(), weighInSession()), nil, func(f *fakeGarmin) {
		f.on(weightRoute, map[string]any{"dateWeightList": []any{
			map[string]any{"calendarDate": "2024-02-10", "weight": 72000},
			map[string]any{"calendarDate": "2024-02-27", "weight": 70000},
			map[string]any{"calendarDate": "2024-03-05", "weight": 68000}, // after the ride: must not be used
		}})
	})
	calls := f.requests(weightRoute)
	if len(calls) != 1 || calls[0].Query.Get("startDate") != "2023-12-03" || calls[0].Query.Get("endDate") != "2024-03-02" {
		t.Fatalf("weight calls = %v", calls)
	}
	if data["rider_weight_kg"] != 70.0 || data["rider_weight_date"] != "2024-02-27" || obj(data["session"])["avg_w_per_kg"] != 3.0 {
		t.Errorf("data = %v", data)
	}
	if data["activity_id"] != float64(fitActivityID) {
		t.Errorf("activity_id = %v", data["activity_id"])
	}
}

func TestGetActivityFitDataWithoutWeighInsOmitsWPerKg(t *testing.T) {
	data, _ := callFitData(t, fitFile(t, fileID(), weighInSession()), nil, func(f *fakeGarmin) {
		f.on(weightRoute, map[string]any{"dateWeightList": []any{}})
	})
	if _, ok := data["rider_weight_kg"]; ok {
		t.Error("rider_weight_kg present")
	}
	if _, ok := obj(data["session"])["avg_w_per_kg"]; ok {
		t.Error("avg_w_per_kg present")
	}
}

// W/kg also covers normalized power and every climb with power.
func TestGetActivityFitDataWPerKgForNormalizedPowerAndClimbs(t *testing.T) {
	msgs := append([]proto.Message{fileID(), weighInSession(fitP(mesgnum.Session, fieldnum.SessionNormalizedPower, 245))},
		rideMessages(true, true, true)...)
	data, _ := callFitData(t, fitFile(t, msgs...), nil, func(f *fakeGarmin) {
		f.on(weightRoute, map[string]any{"dateWeightList": []any{map[string]any{"calendarDate": "2024-03-02", "weight": 70000.0}}})
	})
	s := obj(data["session"])
	climb := obj(arr(data["climbs"])[0])
	if data["rider_weight_date"] != "2024-03-02" || s["normalized_w_per_kg"] != 3.5 || climb["avg_w_per_kg"] != round(280.0/70, 2) {
		t.Errorf("session %v climb %v", s, climb)
	}
}

func TestRiderWeightKg(t *testing.T) {
	for _, tc := range []struct {
		name string
		body any
		want []any
	}{
		{"same-day ties keep the heavier reading", map[string]any{"dateWeightList": []any{
			map[string]any{"calendarDate": "2024-03-02", "weight": 70000}, map[string]any{"calendarDate": "2024-03-02", "weight": 71234}}},
			[]any{71.23, "2024-03-02"}},
		{"falsy entries skipped", map[string]any{"dateWeightList": []any{"x", map[string]any{"calendarDate": "", "weight": 1},
			map[string]any{"calendarDate": "2024-03-01", "weight": 0}, map[string]any{"calendarDate": "2024-02-01", "weight": 65000}}},
			[]any{65.0, "2024-02-01"}},
		{"not a dict", []any{1}, nil},
		{"null", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on(weightRoute, tc.body)
			f.on(fitOriginalRoute, rawBody(fitFile(t, fileID(), weighInSession())))
			data, _ := decode(t, callOK(t, newSession(t, f, nil), "get_activity_fit_data", map[string]any{"activity_id": fitActivityID})).(map[string]any)
			var got []any
			if w, ok := data["rider_weight_kg"]; ok {
				got = []any{w, data["rider_weight_date"]}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// ── get_power_duration_curve ────────────────────────────────────────────────

const activitiesRoute = "GET /activitylist-service/activities/search/activities"

func downloadRoute(id int) string { return fmt.Sprintf("GET /download-service/files/activity/%d", id) }

// powerFit is _mock_power_fitfile: count records all carrying the given power (no timestamps).
func powerFit(t *testing.T, watts float64, count int) []byte {
	msgs := []proto.Message{fileID()}
	for i := 0; i < count; i++ {
		msgs = append(msgs, record(fitP(mesgnum.Record, fieldnum.RecordPower, watts)))
	}
	return fitFile(t, msgs...)
}

func downloadedIDs(f *fakeGarmin) []string {
	var ids []string
	for _, c := range f.requests("") {
		if id, ok := strings.CutPrefix(c.Path, "/download-service/files/activity/"); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

func TestPowerDurationCurveFiltersByRequestedActivityType(t *testing.T) {
	for _, tc := range []struct {
		activityType string
		watts, id    float64
	}{{"running", 250, 1}, {"cycling", 900, 2}} {
		t.Run(tc.activityType, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on(activitiesRoute, []any{
				map[string]any{"activityId": 1, "startTimeLocal": "2024-06-01 08:00:00", "activityType": map[string]any{"typeKey": "running", "parentTypeId": 17}},
				map[string]any{"activityId": 2, "startTimeLocal": "2024-06-02 08:00:00", "activityType": map[string]any{"typeKey": "road_biking", "parentTypeId": 2}},
			})
			f.on(downloadRoute(1), rawBody(powerFit(t, 250, 60)))
			f.on(downloadRoute(2), rawBody(powerFit(t, 900, 60)))
			data, _ := decode(t, callOK(t, newSession(t, f, nil), "get_power_duration_curve", map[string]any{"activity_type": tc.activityType})).(map[string]any)
			best := obj(obj(data["season_bests"])["5s"])
			if data["activities_analyzed"] != 1.0 || best["watts"] != tc.watts || best["activity_id"] != tc.id {
				t.Errorf("data = %v", data)
			}
			if q := f.requests(activitiesRoute)[0].Query; q.Get("activityType") != tc.activityType || q.Get("limit") != "20" || q.Get("start") != "0" {
				t.Errorf("query = %v", q)
			}
		})
	}
}

func TestPowerDurationCurveIncludesCyclingParentSubtypes(t *testing.T) {
	f := newFakeGarmin(t)
	var acts []any
	for i, key := range []string{"virtual_ride", "cyclocross", "e_bike_fitness", "e_bike_mountain"} {
		acts = append(acts, map[string]any{"activityId": i + 1, "activityType": map[string]any{"typeKey": key, "parentTypeId": 2}})
		f.on(downloadRoute(i+1), rawBody(powerFit(t, 300, 60)))
	}
	f.on(activitiesRoute, acts)
	data, _ := decode(t, callOK(t, newSession(t, f, nil), "get_power_duration_curve", map[string]any{})).(map[string]any)
	if data["activities_analyzed"] != 4.0 || obj(obj(data["season_bests"])["5s"])["watts"] != 300.0 {
		t.Errorf("data = %v", data)
	}
	if ids := downloadedIDs(f); !reflect.DeepEqual(ids, []string{"1", "2", "3", "4"}) {
		t.Errorf("downloads = %v", ids)
	}
	if date := obj(obj(data["season_bests"])["5s"])["date"]; date != "" {
		t.Errorf("date = %v", date)
	}
}

func TestPowerDurationCurveRequestsMatchingActivitiesBeforeLimiting(t *testing.T) {
	activities := []any{}
	for _, a := range []struct {
		id     int
		key    string
		parent int
	}{{1, "running", 17}, {2, "road_biking", 2}, {3, "running", 17}, {4, "road_biking", 2}} {
		activities = append(activities, map[string]any{"activityId": a.id, "activityType": map[string]any{"typeKey": a.key, "parentTypeId": a.parent}})
	}
	f := newFakeGarmin(t)
	f.on(activitiesRoute, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		list := activities
		if r.URL.Query().Get("activityType") == "cycling" {
			list = nil
			for _, a := range activities {
				if obj(obj(a)["activityType"])["parentTypeId"] == 2 {
					list = append(list, a)
				}
			}
		}
		start, _ := strconv.Atoi(r.URL.Query().Get("start"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		writeBody(w, r, http.StatusOK, list[start:min(start+limit, len(list))])
	}))
	f.on(downloadRoute(2), rawBody(powerFit(t, 300, 60)))
	f.on(downloadRoute(4), rawBody(powerFit(t, 300, 60)))
	data, _ := decode(t, callOK(t, newSession(t, f, nil), "get_power_duration_curve",
		map[string]any{"num_activities": 2, "activity_type": "CYCLING"})).(map[string]any)
	if data["activities_analyzed"] != 2.0 {
		t.Errorf("data = %v", data)
	}
	if ids := downloadedIDs(f); !reflect.DeepEqual(ids, []string{"2", "4"}) {
		t.Errorf("downloads = %v", ids)
	}
	calls := f.requests(activitiesRoute)
	if len(calls) != 1 || calls[0].Query.Encode() != "activityType=cycling&limit=2&start=0" {
		t.Errorf("calls = %v", calls)
	}
}

func TestPowerDurationCurveToolUsesRecordTimestamps(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(activitiesRoute, []any{map[string]any{"activityId": 2, "startTimeLocal": "2024-06-01 10:00:00",
		"activityType": map[string]any{"typeKey": "road_biking", "parentTypeId": 2}}})
	msgs := []proto.Message{fileID()}
	for i := 0; i < 1200; i++ {
		w := 100.0
		if i < 400 {
			w = 300
		}
		msgs = append(msgs, record(fitP(mesgnum.Record, fieldnum.RecordPower, w),
			fitTS(mesgnum.Record, rideStart.Add(time.Duration(3*i)*time.Second))))
	}
	f.on(downloadRoute(2), rawBody(fitFile(t, msgs...)))
	text := callOK(t, newSession(t, f, nil), "get_power_duration_curve", map[string]any{})
	want := `{
  "activities_analyzed": 1,
  "activities_skipped": 0,
  "ftp_estimate_w": 285,
  "ftp_note": "Estimated as 95% of 20-minute best power",
  "season_bests": {
    "5s": {
      "watts": 300,
      "activity_id": 2,
      "date": "2024-06-01"
    },
    "30s": {
      "watts": 300,
      "activity_id": 2,
      "date": "2024-06-01"
    },
    "1min": {
      "watts": 300,
      "activity_id": 2,
      "date": "2024-06-01"
    },
    "5min": {
      "watts": 300,
      "activity_id": 2,
      "date": "2024-06-01"
    },
    "10min": {
      "watts": 300,
      "activity_id": 2,
      "date": "2024-06-01"
    },
    "20min": {
      "watts": 300,
      "activity_id": 2,
      "date": "2024-06-01"
    },
    "60min": {
      "watts": 167,
      "activity_id": 2,
      "date": "2024-06-01"
    }
  }
}`
	if text != want {
		t.Errorf("text = %s", text)
	}
}

func TestGetPowerDurationCurveMessages(t *testing.T) {
	run := map[string]any{"activityId": 1, "activityType": map[string]any{"typeKey": "running", "parentTypeId": 17}}
	ride := map[string]any{"activityId": 2, "activityType": map[string]any{"typeKey": "road_biking", "parentTypeId": 2}}
	for _, tc := range []struct {
		name   string
		args   map[string]any
		acts   any
		dl     any
		want   string
		isErr  bool
		limitQ string
	}{
		{"no activities", nil, []any{}, nil, "No activities found.", false, "20"},
		{"none matching", map[string]any{"activity_type": "Swimming", "num_activities": 80}, []any{run, ride}, nil,
			"No Swimming activities found in the last 50 activities.", false, "50"},
		{"no power", nil, []any{ride}, rawBody(fitFile(t, fileID(), hrRecord(120))), "No power data found across 1 activities.", false, "20"},
		{"broken file counts as skipped", nil, []any{ride}, rawBody("<html>"), "No power data found across 0 activities.", false, "20"},
		{"empty download is not processed", nil, []any{ride}, rawBody{}, "No power data found across 0 activities.", false, "20"},
		{"limit validation", map[string]any{"num_activities": 0}, nil, nil,
			"Error computing power duration curve: limit must be a positive integer, got: 0", true, ""},
		{"non-dict activity", nil, []any{"x"}, nil, "Error computing power duration curve: 'str' object has no attribute 'get'", true, "20"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on(activitiesRoute, tc.acts)
			if tc.dl != nil {
				f.on(downloadRoute(2), tc.dl)
			}
			text, isErr := callTool(t, newSession(t, f, nil), "get_power_duration_curve", tc.args)
			if text != tc.want || isErr != tc.isErr {
				t.Errorf("got %q (isErr %v)", text, isErr)
			}
			if calls := f.requests(activitiesRoute); tc.limitQ != "" && (len(calls) != 1 || calls[0].Query.Get("limit") != tc.limitQ) {
				t.Errorf("calls = %v", calls)
			}
		})
	}
}

func TestGetPowerDurationCurveSkipsFailedDownloads(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(activitiesRoute, []any{
		map[string]any{"activityId": 1, "startTimeLocal": "2024-06-01 08:00:00", "activityType": map[string]any{"typeKey": "road_biking"}},
		map[string]any{"activityId": nil, "activityType": map[string]any{"typeKey": "road_biking"}},
		map[string]any{"activityId": 2, "startTimeLocal": "2024-06-02 08:00:00", "activityType": map[string]any{"typeKey": "road_biking"}},
	})
	f.on(downloadRoute(1), status(http.StatusInternalServerError, "boom"))
	f.on(downloadRoute(2), rawBody(powerFit(t, 400, 30)))
	data, _ := decode(t, callOK(t, newSession(t, f, nil), "get_power_duration_curve", nil)).(map[string]any)
	if data["activities_analyzed"] != 1.0 || data["activities_skipped"] != 1.0 || data["ftp_estimate_w"] != nil {
		t.Errorf("data = %v", data)
	}
	if keys := len(obj(data["season_bests"])); keys != 2 {
		t.Errorf("season_bests = %v", data["season_bests"])
	}
}

// A 204 download is b"" in Python (garth returns resp.content), so every download tool takes its empty branch.
func TestDownloadToolsTreat204AsEmpty(t *testing.T) {
	noContent := status(http.StatusNoContent, rawBody{})
	for _, tc := range []struct {
		tool, route string
		args        map[string]any
		want        string
	}{
		{"get_activity_fit_data", fitOriginalRoute, map[string]any{"activity_id": fitActivityID},
			"No FIT data returned for activity 22041393449"},
		{"get_activity_fit_messages", fitOriginalRoute, map[string]any{"activity_id": fitActivityID},
			"No FIT data returned for activity 22041393449"},
		{"download_activity_file", fitOriginalRoute, map[string]any{"activity_id": fitActivityID, "output_dir": "<tmp>"},
			"No fit data returned for activity 22041393449"},
		{"download_activity_file", "GET /download-service/export/gpx/activity/22041393449",
			map[string]any{"activity_id": fitActivityID, "format": "gpx", "output_dir": "<tmp>"},
			"No gpx data returned for activity 22041393449"},
		{"download_workout", "GET /workout-service/workout/FIT/42", map[string]any{"workout_id": 42},
			"No workout data found for workout with ID 42."},
	} {
		t.Run(tc.tool+" "+tc.route, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on(tc.route, noContent)
			if tc.args["output_dir"] == "<tmp>" {
				tc.args["output_dir"] = t.TempDir()
			}
			text, isErr := callTool(t, newSession(t, f, nil), tc.tool, tc.args)
			if text != tc.want || isErr {
				t.Errorf("got %q (isErr %v)", text, isErr)
			}
			if n := len(f.requests(tc.route)); n != 1 {
				t.Errorf("requests = %d", n)
			}
		})
	}
}

// Python skips an activity whose download is empty without counting it as skipped.
func TestPowerDurationCurveSkips204DownloadSilently(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(activitiesRoute, []any{
		map[string]any{"activityId": 1, "activityType": map[string]any{"typeKey": "road_biking"}},
		map[string]any{"activityId": 2, "activityType": map[string]any{"typeKey": "road_biking"}},
	})
	f.on(downloadRoute(1), status(http.StatusNoContent, rawBody{}))
	f.on(downloadRoute(2), rawBody(powerFit(t, 400, 30)))
	data, _ := decode(t, callOK(t, newSession(t, f, nil), "get_power_duration_curve", nil)).(map[string]any)
	if data["activities_analyzed"] != 1.0 || data["activities_skipped"] != 0.0 {
		t.Errorf("data = %v", data)
	}
}
