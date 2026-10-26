package server

import (
	"archive/zip"
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/accounts"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/muktihari/fit/encoder"
	"github.com/muktihari/fit/profile/factory"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/profile/untyped/fieldnum"
	"github.com/muktihari/fit/profile/untyped/mesgnum"
	"github.com/muktihari/fit/proto"
)

const aaActivityID = 22041393449

// --- synthetic FIT fixtures -------------------------------------------------

type aaField struct {
	num   byte
	value any
}

type aaMsg struct {
	num    typedef.MesgNum
	fields []aaField
}

func aaFIT(t *testing.T, msgs ...aaMsg) []byte {
	t.Helper()
	out := make([]proto.Message, 0, len(msgs))
	for _, m := range msgs {
		fields := make([]proto.Field, 0, len(m.fields))
		for _, f := range m.fields {
			fields = append(fields, factory.CreateField(m.num, f.num).WithValue(f.value))
		}
		out = append(out, proto.Message{Num: m.num, Fields: fields})
	}
	var buf bytes.Buffer
	if err := encoder.New(&buf).Encode(&proto.FIT{Messages: out}); err != nil {
		t.Fatalf("encode FIT: %v", err)
	}
	return buf.Bytes()
}

func aaValidFIT(t *testing.T) []byte {
	t.Helper()
	return aaFIT(t, aaMsg{num: mesgnum.FileId, fields: []aaField{
		{fieldnum.FileIdType, typedef.FileActivity.Byte()},
		{fieldnum.FileIdManufacturer, uint16(typedef.ManufacturerGarmin)},
	}})
}

// aaTime encodes a wall-clock UTC time as a FIT timestamp.
func aaTime(t *testing.T, value string) uint32 {
	t.Helper()
	ts, err := time.Parse("2006-01-02 15:04:05", value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return uint32(ts.Unix() - 631065600)
}

func aaFITTime(t *testing.T, ts time.Time) uint32 {
	t.Helper()
	return uint32(ts.UTC().Unix() - 631065600)
}

func aaServeFIT(t *testing.T, f *fakeGarmin, id int64, raw []byte) {
	t.Helper()
	f.on(fmt.Sprintf("GET /download-service/files/activity/%d", id), rawBody(raw))
}

func aaDownloaded(t *testing.T, text string) map[string]any {
	t.Helper()
	var out map[string]any
	decodeJSON(t, text, &out)
	return out
}

func aaMap(t *testing.T, v any, key string) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%s is %T, want object", key, v)
	}
	return m
}

func aaList(t *testing.T, v any, key string) []any {
	t.Helper()
	l, ok := v.([]any)
	if !ok {
		t.Fatalf("%s is %T, want list", key, v)
	}
	return l
}

func aaNum(t *testing.T, v any) float64 {
	t.Helper()
	f, ok := numberOf(v)
	if !ok {
		t.Fatalf("value %v is not numeric", v)
	}
	return f
}

func aaFieldByName(t *testing.T, message map[string]any, name string) map[string]any {
	t.Helper()
	for _, item := range aaList(t, message["fields"], "fields") {
		field := aaMap(t, item, "field")
		if field["name"] == name {
			return field
		}
	}
	t.Fatalf("field %q not found", name)
	return nil
}

// --- generic FIT message tool ----------------------------------------------

func TestGetActivityFitMessagesPreservesAllMessageTypesAndFields(t *testing.T) {
	f := newFakeGarmin(t)
	raw := aaFIT(t,
		aaMsg{num: mesgnum.ExerciseTitle, fields: []aaField{
			{fieldnum.ExerciseTitleExerciseCategory, uint16(typedef.ExerciseCategoryBenchPress)},
			{fieldnum.ExerciseTitleExerciseName, uint16(0)},
		}},
		aaMsg{num: mesgnum.Set, fields: []aaField{
			{fieldnum.SetRepetitions, uint16(8)},
			{fieldnum.SetWeight, uint16(980)},
			{fieldnum.SetCategory, []uint16{uint16(typedef.ExerciseCategoryDeadlift), uint16(typedef.ExerciseCategoryPullUp)}},
			{fieldnum.SetSetType, typedef.SetTypeActive.Uint8()},
			{99, []byte{1, 2}},
		}},
	)
	aaServeFIT(t, f, aaActivityID, raw)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_activity_fit_messages", map[string]any{"activity_id": aaActivityID})
	if isErr {
		t.Fatalf("tool error: %s", text)
	}
	data := aaDownloaded(t, text)
	if data["source"] != "garmin_original_fit" {
		t.Fatalf("source = %v", data["source"])
	}
	counts := aaMap(t, data["message_counts"], "message_counts")
	if aaNum(t, counts["exercise_title"]) != 1 || aaNum(t, counts["set"]) != 1 {
		t.Fatalf("counts = %v", counts)
	}
	messages := aaList(t, data["messages"], "messages")
	if len(messages) != 2 {
		t.Fatalf("messages = %d", len(messages))
	}
	first := aaMap(t, messages[0], "message[0]")
	if first["type"] != "exercise_title" || aaNum(t, first["global_message_number"]) != 264 {
		t.Fatalf("first message = %v", first)
	}
	set := aaMap(t, messages[1], "message[1]")

	reps := aaFieldByName(t, set, "repetitions")
	if aaNum(t, reps["value"]) != 8 || aaNum(t, reps["definition_number"]) != 3 {
		t.Fatalf("repetitions = %v", reps)
	}
	weight := aaFieldByName(t, set, "weight")
	if math.Abs(aaNum(t, weight["value"])-61.25) > 0.01 || weight["units"] != "kg" {
		t.Fatalf("weight = %v", weight)
	}
	category := aaFieldByName(t, set, "category")
	names := aaList(t, category["value_names"], "value_names")
	if len(names) != 2 || names[0] != "deadlift" || names[1] != "pull_up" {
		t.Fatalf("category = %v", category)
	}
	setType := aaFieldByName(t, set, "set_type")
	if setType["value"] != "active" {
		t.Fatalf("set_type = %v", setType)
	}
	unknown := aaFieldByName(t, set, "unknown_99")
	if got := aaList(t, unknown["value"], "unknown value"); fmt.Sprint(got) != "[1 2]" {
		t.Fatalf("unknown = %v", unknown)
	}
}

func TestGetActivityFitMessagesOmitsRecordsByDefault(t *testing.T) {
	f := newFakeGarmin(t)
	raw := aaFIT(t,
		aaMsg{num: mesgnum.Record, fields: []aaField{{fieldnum.RecordHeartRate, uint8(140)}}},
		aaMsg{num: mesgnum.Set, fields: []aaField{{fieldnum.SetRepetitions, uint16(10)}}},
		aaMsg{num: mesgnum.Record, fields: []aaField{{fieldnum.RecordHeartRate, uint8(141)}}},
	)
	aaServeFIT(t, f, aaActivityID, raw)
	cs := newSession(t, f, nil)

	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_messages", map[string]any{"activity_id": aaActivityID}))
	counts := aaMap(t, data["message_counts"], "message_counts")
	if aaNum(t, counts["record"]) != 2 || aaNum(t, counts["set"]) != 1 {
		t.Fatalf("counts = %v", counts)
	}
	returned := aaMap(t, data["returned_message_counts"], "returned")
	if aaNum(t, returned["set"]) != 1 {
		t.Fatalf("returned = %v", returned)
	}
	messages := aaList(t, data["messages"], "messages")
	if len(messages) != 1 || aaMap(t, messages[0], "message")["type"] != "set" {
		t.Fatalf("messages = %v", messages)
	}
	stream := aaMap(t, data["record_stream"], "record_stream")
	if stream["included"] != false || aaNum(t, stream["total_count"]) != 2 || aaNum(t, stream["returned_count"]) != 0 {
		t.Fatalf("stream = %v", stream)
	}
}

func TestGetActivityFitMessagesPaginatesRecords(t *testing.T) {
	f := newFakeGarmin(t)
	raw := aaFIT(t,
		aaMsg{num: mesgnum.Record, fields: []aaField{{fieldnum.RecordHeartRate, uint8(140)}}},
		aaMsg{num: mesgnum.Record, fields: []aaField{{fieldnum.RecordHeartRate, uint8(141)}}},
		aaMsg{num: mesgnum.Record, fields: []aaField{{fieldnum.RecordHeartRate, uint8(142)}}},
	)
	aaServeFIT(t, f, aaActivityID, raw)
	cs := newSession(t, f, nil)

	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_messages", map[string]any{
		"activity_id": aaActivityID, "include_records": true, "message_offset": 1, "message_limit": 1,
	}))
	pagination := aaMap(t, data["pagination"], "pagination")
	if aaNum(t, pagination["total_eligible_count"]) != 3 ||
		aaNum(t, pagination["returned_count"]) != 1 ||
		aaNum(t, pagination["offset"]) != 1 ||
		aaNum(t, pagination["limit"]) != 1 ||
		aaNum(t, pagination["next_offset"]) != 2 {
		t.Fatalf("pagination = %v", pagination)
	}
	messages := aaList(t, data["messages"], "messages")
	if len(messages) != 1 {
		t.Fatalf("messages = %v", messages)
	}
	message := aaMap(t, messages[0], "message")
	if aaNum(t, message["type_index"]) != 1 {
		t.Fatalf("type_index = %v", message["type_index"])
	}
	fields := aaList(t, message["fields"], "fields")
	if aaNum(t, aaMap(t, fields[0], "field")["value"]) != 141 {
		t.Fatalf("field = %v", fields[0])
	}
}

func TestGetActivityFitMessagesFiltersReturnedTypesButKeepsInventory(t *testing.T) {
	f := newFakeGarmin(t)
	raw := aaFIT(t,
		aaMsg{num: mesgnum.Session, fields: []aaField{{fieldnum.SessionSport, typedef.SportTraining.Byte()}}},
		aaMsg{num: mesgnum.Set, fields: []aaField{{fieldnum.SetRepetitions, uint16(5)}}},
	)
	aaServeFIT(t, f, aaActivityID, raw)
	cs := newSession(t, f, nil)

	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_messages", map[string]any{
		"activity_id": aaActivityID, "message_types": []string{"SET"},
	}))
	counts := aaMap(t, data["message_counts"], "message_counts")
	if aaNum(t, counts["session"]) != 1 || aaNum(t, counts["set"]) != 1 {
		t.Fatalf("counts = %v", counts)
	}
	messages := aaList(t, data["messages"], "messages")
	if len(messages) != 1 || aaMap(t, messages[0], "message")["type"] != "set" {
		t.Fatalf("messages = %v", messages)
	}
}

func TestGetActivityFitMessagesRejectsUnboundedMessageLimit(t *testing.T) {
	f := newFakeGarmin(t)
	aaServeFIT(t, f, aaActivityID, aaValidFIT(t))
	cs := newSession(t, f, nil)

	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_messages", map[string]any{
		"activity_id": aaActivityID, "include_records": true, "message_limit": 5001,
	}))
	if data["error"] != "message_limit must be between 1 and 5000" {
		t.Fatalf("error = %v", data["error"])
	}
}

func TestGetActivityFitMessagesRejectsNegativeOffset(t *testing.T) {
	f := newFakeGarmin(t)
	aaServeFIT(t, f, aaActivityID, aaValidFIT(t))
	cs := newSession(t, f, nil)

	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_messages", map[string]any{
		"activity_id": aaActivityID, "message_offset": -1,
	}))
	if data["error"] != "message_offset must be at least 0" {
		t.Fatalf("error = %v", data["error"])
	}
}

func TestGetActivityFitMessagesOmitsOtherHighFrequencyTypesByDefault(t *testing.T) {
	f := newFakeGarmin(t)
	msgs := []aaMsg{{num: mesgnum.Session, fields: []aaField{{fieldnum.SessionSport, typedef.SportTraining.Byte()}}}}
	for i := 0; i < 101; i++ {
		msgs = append(msgs, aaMsg{num: typedef.MesgNum(233), fields: []aaField{{0, uint8(i)}}})
	}
	aaServeFIT(t, f, aaActivityID, aaFIT(t, msgs...))
	cs := newSession(t, f, nil)

	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_messages", map[string]any{"activity_id": aaActivityID}))
	messages := aaList(t, data["messages"], "messages")
	if len(messages) != 1 || aaMap(t, messages[0], "message")["type"] != "session" {
		t.Fatalf("messages = %v", messages)
	}
	counts := aaMap(t, data["message_counts"], "message_counts")
	if aaNum(t, counts["unknown_233"]) != 101 {
		t.Fatalf("counts = %v", counts)
	}
	omitted := aaMap(t, data["omitted_high_frequency_message_types"], "omitted")
	entry := aaMap(t, omitted["unknown_233"], "unknown_233")
	if aaNum(t, entry["count"]) != 101 {
		t.Fatalf("omitted = %v", omitted)
	}
}

func TestFitMessageNamesMatchFitparse(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "Activity.fit"))
	if err != nil {
		t.Fatalf("read Activity.fit: %v", err)
	}
	messages, err := decodeFIT(raw)
	if err != nil {
		t.Fatalf("decodeFIT: %v", err)
	}
	counts := map[string]int{}
	for _, message := range messages {
		counts[message.Name]++
	}
	for _, name := range []string{"file_id", "record", "session", "lap", "event", "device_info"} {
		if counts[name] == 0 {
			t.Fatalf("message %q missing from counts %v", name, counts)
		}
	}
	if counts["record"] < 1000 {
		t.Fatalf("record count = %d", counts["record"])
	}
}

func TestFitInvalidSentinelValuesAreNull(t *testing.T) {
	raw := aaFIT(t,
		aaMsg{num: mesgnum.Record, fields: []aaField{
			{fieldnum.RecordPower, uint16(0xFFFF)},
			{fieldnum.RecordHeartRate, uint8(140)},
		}},
	)
	messages, err := decodeFIT(raw)
	if err != nil {
		t.Fatalf("decodeFIT: %v", err)
	}
	power := aaFieldByName(t, map[string]any{"fields": func() []any {
		fields := []any{}
		for _, field := range messages[0].Fields {
			fields = append(fields, fitFieldMap(field))
		}
		return fields
	}()}, "power")
	if power["value"] != nil {
		t.Fatalf("power value = %v, want null", power["value"])
	}
	if power["raw_value"] != nil {
		t.Fatalf("power raw_value = %v, want null", power["raw_value"])
	}
	parsed, err := parseFIT(raw, true)
	if err != nil {
		t.Fatalf("parseFIT: %v", err)
	}
	records := fitMapList(parsed["records"])
	if len(records) != 1 {
		t.Fatalf("records = %v", records)
	}
	record := records[0]
	if _, ok := record["power_w"]; ok {
		t.Fatalf("invalid power leaked into curation: %v", record)
	}

	invalidRaw := aaFIT(t, aaMsg{num: mesgnum.FileId, fields: []aaField{
		{fieldnum.FileIdManufacturer, uint16(0xFFFF)},
	}})
	invalidMessages, err := decodeFIT(invalidRaw)
	if err != nil {
		t.Fatalf("decode invalid file_id: %v", err)
	}
	if got := fitFieldValue(invalidMessages[0], "manufacturer"); got != nil {
		t.Fatalf("invalid manufacturer = %v, want null", got)
	}

	// Activity.fit's manufacturer is 255 (development), a valid value, so it
	// must still decode as a label rather than being treated as a sentinel.
	activityRaw, err := os.ReadFile(filepath.Join("testdata", "Activity.fit"))
	if err != nil {
		t.Fatal(err)
	}
	activityMessages, err := decodeFIT(activityRaw)
	if err != nil {
		t.Fatalf("decode Activity.fit: %v", err)
	}
	found := false
	for _, message := range activityMessages {
		if message.Name != "file_id" {
			continue
		}
		for _, field := range message.Fields {
			if field.Name != "manufacturer" {
				continue
			}
			found = true
			if field.Value != "development" {
				t.Fatalf("file_id.manufacturer = %v, want development", field.Value)
			}
		}
	}
	if !found {
		t.Fatal("file_id.manufacturer not found")
	}
}

func TestFitProfileEnumCoverageIncludesGarminProduct(t *testing.T) {
	raw := aaFIT(t, aaMsg{num: mesgnum.FileId, fields: []aaField{
		{fieldnum.FileIdType, typedef.FileActivity.Byte()},
		{fieldnum.FileIdManufacturer, typedef.ManufacturerGarmin.Uint16()},
		{fieldnum.FileIdProduct, typedef.GarminProductFr255.Uint16()},
	}})
	messages, err := decodeFIT(raw)
	if err != nil {
		t.Fatalf("decodeFIT: %v", err)
	}
	var product *fitField
	for i := range messages[0].Fields {
		if messages[0].Fields[i].Name == "garmin_product" {
			product = &messages[0].Fields[i]
			break
		}
	}
	if product == nil {
		t.Fatalf("garmin_product not found: %v", messages[0].Fields)
	}
	if product.Value != "fr255" {
		t.Fatalf("product = %v, want fr255", product.Value)
	}
}

func TestFitInvalidArrayElementBecomesNull(t *testing.T) {
	raw := aaFIT(t, aaMsg{num: mesgnum.Record, fields: []aaField{
		{fieldnum.RecordLeftPowerPhase, []uint8{240, 0xFF}},
	}})
	messages, err := decodeFIT(raw)
	if err != nil {
		t.Fatalf("decodeFIT: %v", err)
	}
	var phase *fitField
	for i := range messages[0].Fields {
		if messages[0].Fields[i].Name == "left_power_phase" {
			phase = &messages[0].Fields[i]
			break
		}
	}
	if phase == nil {
		t.Fatal("left_power_phase not found")
	}
	values := aaList(t, jsonSafeFitValue(phase.Value), "phase value")
	if len(values) != 2 || math.Abs(aaNum(t, values[0])-337.5) > 0.001 || values[1] != nil {
		t.Fatalf("phase = %v", values)
	}
}

func TestGetActivityFitMessagesResolvesStandardContextualEnums(t *testing.T) {
	f := newFakeGarmin(t)
	raw := aaFIT(t,
		aaMsg{num: mesgnum.Set, fields: []aaField{
			{fieldnum.SetCategory, []uint16{uint16(typedef.ExerciseCategorySquat)}},
			{fieldnum.SetCategorySubtype, []uint16{uint16(typedef.SquatExerciseNameBarbellBackSquat)}},
		}},
		aaMsg{num: mesgnum.ExerciseTitle, fields: []aaField{
			{fieldnum.ExerciseTitleExerciseCategory, uint16(typedef.ExerciseCategorySquat)},
			{fieldnum.ExerciseTitleExerciseName, uint16(typedef.SquatExerciseNameBarbellBackSquat)},
		}},
	)
	aaServeFIT(t, f, aaActivityID, raw)
	cs := newSession(t, f, nil)

	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_messages", map[string]any{"activity_id": aaActivityID}))
	messages := aaList(t, data["messages"], "messages")
	if len(messages) != 2 {
		t.Fatalf("messages = %v", messages)
	}
	set := aaMap(t, messages[0], "set")
	title := aaMap(t, messages[1], "exercise_title")
	if got := aaFieldByName(t, set, "category")["value_name"]; got != "squat" {
		t.Fatalf("category = %v", got)
	}
	if got := aaFieldByName(t, set, "category_subtype")["value_name"]; got != "barbell_back_squat" {
		t.Fatalf("category_subtype = %v", got)
	}
	exerciseName := aaFieldByName(t, title, "exercise_name")
	if exerciseName["value_name"] != "barbell_back_squat" {
		t.Fatalf("exercise_name = %v", exerciseName)
	}
	if aaNum(t, exerciseName["raw_value"]) != float64(typedef.SquatExerciseNameBarbellBackSquat) {
		t.Fatalf("raw_value = %v", exerciseName["raw_value"])
	}
}

// --- FIT download calls -----------------------------------------------------

func TestGetActivityFitDataCallsDownload(t *testing.T) {
	f := newFakeGarmin(t)
	aaServeFIT(t, f, aaActivityID, aaValidFIT(t))
	cs := newSession(t, f, nil)

	mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID})
	calls := f.callsMatching("GET", fmt.Sprintf("/download-service/files/activity/%d", aaActivityID))
	if len(calls) != 1 {
		t.Fatalf("download calls = %d", len(calls))
	}
}

func TestGetActivityFitDataEmptyResponse(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(fmt.Sprintf("GET /download-service/files/activity/%d", aaActivityID), rawBody(nil))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID})
	if !strings.Contains(text, "No FIT data") {
		t.Fatalf("text = %q", text)
	}
}

func TestGetActivityFitDataNoneResponse(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(fmt.Sprintf("GET /download-service/files/activity/%d", aaActivityID), nil)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID})
	if !strings.Contains(text, "No FIT data") && !strings.Contains(text, "Error") {
		t.Fatalf("text = %q", text)
	}
}

func TestGetActivityFitDataErrorHandling(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(fmt.Sprintf("GET /download-service/files/activity/%d", aaActivityID), status(500, map[string]any{"message": "network error"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID})
	if !strings.Contains(text, "Error") {
		t.Fatalf("text = %q", text)
	}
}

// --- session parsing --------------------------------------------------------

func TestGetActivityFitDataSessionFields(t *testing.T) {
	f := newFakeGarmin(t)
	raw := aaFIT(t, aaMsg{num: mesgnum.Session, fields: []aaField{
		{fieldnum.SessionSport, typedef.SportCycling.Byte()},
		{fieldnum.SessionTotalElapsedTime, uint32(3120 * 1000)},
		{fieldnum.SessionTotalDistance, uint32(56320 * 100)},
		{fieldnum.SessionAvgPower, uint16(185)},
		{fieldnum.SessionNormalizedPower, uint16(210)},
		{fieldnum.SessionAvgCadence, uint8(83)},
		{fieldnum.SessionAvgHeartRate, uint8(148)},
		{fieldnum.SessionAvgLeftPco, int8(3)},
		{fieldnum.SessionAvgRightPco, int8(-8)},
	}})
	aaServeFIT(t, f, aaActivityID, raw)
	cs := newSession(t, f, nil)

	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	session := aaMap(t, data["session"], "session")
	if session["sport"] != "cycling" {
		t.Fatalf("sport = %v", session["sport"])
	}
	for key, want := range map[string]float64{
		"avg_power_w": 185, "normalized_power_w": 210, "avg_cadence_rpm": 83,
		"avg_left_pco_mm": 3, "avg_right_pco_mm": -8,
	} {
		if aaNum(t, session[key]) != want {
			t.Fatalf("%s = %v, want %v", key, session[key], want)
		}
	}
	if _, ok := data["multisport"]; ok {
		t.Fatal("multisport key present")
	}
}

var aaTriStart = time.Date(2024, 6, 1, 8, 0, 0, 0, time.UTC)

var aaBrickLegs = []struct {
	sport    typedef.Sport
	offset   int
	duration int
	power    any
}{
	{typedef.SportRunning, 0, 600, uint16(350)},
	{typedef.SportTransition, 600, 60, nil},
	{typedef.SportCycling, 660, 1200, uint16(200)},
	{typedef.SportRunning, 1860, 600, uint16(350)},
}

func aaMultisportFIT(t *testing.T, withTimestamps bool) []byte {
	msgs := []aaMsg{}
	for _, leg := range aaBrickLegs {
		for i := 0; i < leg.duration; i++ {
			fields := []aaField{{fieldnum.RecordHeartRate, uint8(150)}}
			if leg.power != nil {
				fields = append(fields, aaField{fieldnum.RecordPower, leg.power})
			}
			if withTimestamps {
				fields = append(fields, aaField{fieldnum.RecordTimestamp, aaFITTime(t, aaTriStart.Add(time.Duration(leg.offset+i)*time.Second))})
			}
			msgs = append(msgs, aaMsg{num: mesgnum.Record, fields: fields})
		}
	}
	for _, leg := range aaBrickLegs {
		fields := []aaField{
			{fieldnum.SessionSport, leg.sport.Byte()},
			{fieldnum.SessionStartTime, aaFITTime(t, aaTriStart.Add(time.Duration(leg.offset)*time.Second))},
			{fieldnum.SessionTotalElapsedTime, uint32(leg.duration * 1000)},
		}
		if leg.sport == typedef.SportCycling {
			fields = append(fields, aaField{fieldnum.SessionAvgPower, uint16(200)})
		}
		msgs = append(msgs, aaMsg{num: mesgnum.Session, fields: fields})
	}
	return aaFIT(t, msgs...)
}

func TestGetActivityFitDataMultisportListsEverySession(t *testing.T) {
	f := newFakeGarmin(t)
	aaServeFIT(t, f, aaActivityID, aaMultisportFIT(t, true))
	cs := newSession(t, f, nil)

	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	if data["multisport"] != true {
		t.Fatalf("multisport = %v", data["multisport"])
	}
	sessions := aaList(t, data["sessions"], "sessions")
	sports := []string{}
	for _, item := range sessions {
		sports = append(sports, fmt.Sprint(aaMap(t, item, "session")["sport"]))
	}
	if strings.Join(sports, ",") != "running,transition,cycling,running" {
		t.Fatalf("sports = %v", sports)
	}
	bike := aaMap(t, sessions[2], "bike session")
	if bike["start_time"] != "2024-06-01 08:11:00" {
		t.Fatalf("start_time = %v", bike["start_time"])
	}
	if aaNum(t, bike["total_elapsed_time_s"]) != 1200 {
		t.Fatalf("elapsed = %v", bike["total_elapsed_time_s"])
	}
	if aaNum(t, data["analyzed_session_index"]) != 2 {
		t.Fatalf("analyzed = %v", data["analyzed_session_index"])
	}
	session := aaMap(t, data["session"], "session")
	if session["sport"] != "cycling" || aaNum(t, session["avg_power_w"]) != 200 {
		t.Fatalf("session = %v", session)
	}
	if !strings.Contains(fmt.Sprint(data["analysis_note"]), "leg 2 (cycling)") {
		t.Fatalf("note = %v", data["analysis_note"])
	}
}

func TestGetActivityFitDataMultisportPowerCurveIsBikeOnly(t *testing.T) {
	f := newFakeGarmin(t)
	aaServeFIT(t, f, aaActivityID, aaMultisportFIT(t, true))
	cs := newSession(t, f, nil)

	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	pdc := aaMap(t, data["power_duration_curve"], "pdc")
	if aaNum(t, pdc["5s"]) != 200 || aaNum(t, pdc["20min"]) != 200 {
		t.Fatalf("pdc = %v", pdc)
	}
	if _, ok := pdc["60min"]; ok {
		t.Fatalf("60min present: %v", pdc)
	}
}

func TestGetActivityFitDataMultisportWithoutTimestampsFallsBack(t *testing.T) {
	f := newFakeGarmin(t)
	aaServeFIT(t, f, aaActivityID, aaMultisportFIT(t, false))
	cs := newSession(t, f, nil)

	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	if aaMap(t, data["session"], "session")["sport"] != "cycling" {
		t.Fatalf("session = %v", data["session"])
	}
	pdc := aaMap(t, data["power_duration_curve"], "pdc")
	if aaNum(t, pdc["5s"]) != 350 {
		t.Fatalf("pdc = %v", pdc)
	}
	if !strings.Contains(fmt.Sprint(data["analysis_note"]), "cover all legs") {
		t.Fatalf("note = %v", data["analysis_note"])
	}
}

func TestPrimarySessionIndexFallsBackToLongestLeg(t *testing.T) {
	sessions := []map[string]any{
		{"sport": "cycling", "total_elapsed_time_s": 900.0},
		{"sport": "running", "total_elapsed_time_s": 2400.0},
		{"sport": "cycling", "total_elapsed_time_s": 1800.0},
	}
	if got := primarySessionIndex(sessions); got != 1 {
		t.Fatalf("index = %d", got)
	}
	if got := primarySessionIndex([]map[string]any{
		{"sport": "swimming", "total_elapsed_time_s": 1200.0},
		{"sport": "running", "total_elapsed_time_s": 600.0},
	}); got != 0 {
		t.Fatalf("index = %d", got)
	}
}

// --- shift / DI2 ------------------------------------------------------------

func TestGetActivityFitDataShiftEvents(t *testing.T) {
	f := newFakeGarmin(t)
	gearData := uint32(53<<24 | 2<<16 | 16<<8 | 5)
	raw := aaFIT(t,
		aaMsg{num: mesgnum.Record, fields: []aaField{
			{fieldnum.RecordCadence, uint8(65)}, {fieldnum.RecordPower, uint16(200)},
			{fieldnum.RecordHeartRate, uint8(150)}, {fieldnum.RecordSpeed, uint16(8500)},
			{fieldnum.RecordAltitude, uint16((120 + 500) * 5)},
		}},
		aaMsg{num: mesgnum.Event, fields: []aaField{
			{fieldnum.EventEvent, typedef.EventRearGearChange.Byte()},
			{fieldnum.EventData, gearData},
			{fieldnum.EventTimestamp, aaTime(t, "2024-03-02 14:23:45")},
		}},
	)
	aaServeFIT(t, f, aaActivityID, raw)
	cs := newSession(t, f, nil)

	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	shifts := aaList(t, data["shifts"], "shifts")
	if len(shifts) != 1 {
		t.Fatalf("shifts = %v", shifts)
	}
	shift := aaMap(t, shifts[0], "shift")
	for key, want := range map[string]float64{
		"cadence_at_shift_rpm": 65, "front_teeth": 53, "rear_teeth": 16,
		"front_gear_num": 2, "rear_gear_num": 5,
	} {
		if aaNum(t, shift[key]) != want {
			t.Fatalf("%s = %v", key, shift[key])
		}
	}
	if shift["quality"] != "reactive" || shift["gear_combo"] != "53/16t" {
		t.Fatalf("shift = %v", shift)
	}
}

func TestDecodeGearChangeMatchesFitProfileLayout(t *testing.T) {
	packed := uint32(5) | uint32(21)<<8 | uint32(2)<<16 | uint32(50)<<24
	got := decodeGearChange(packed)
	want := map[string]any{"rear_gear_num": 5, "front_gear_num": 2, "rear_teeth": 21, "front_teeth": 50}
	sameJSON(t, got, want)
}

func TestGetActivityFitDataProactiveShift(t *testing.T) {
	f := newFakeGarmin(t)
	raw := aaFIT(t,
		aaMsg{num: mesgnum.Record, fields: []aaField{{fieldnum.RecordCadence, uint8(88)}}},
		aaMsg{num: mesgnum.Event, fields: []aaField{
			{fieldnum.EventEvent, typedef.EventRearGearChange.Byte()},
			{fieldnum.EventData, uint32(39<<24 | 1<<16 | 19<<8 | 4)},
			{fieldnum.EventTimestamp, aaTime(t, "2024-03-02 14:30:00")},
		}},
	)
	aaServeFIT(t, f, aaActivityID, raw)
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	shifts := aaList(t, data["shifts"], "shifts")
	if aaMap(t, shifts[0], "shift")["quality"] != "proactive" {
		t.Fatalf("shift = %v", shifts[0])
	}
}

func TestGetActivityFitDataShiftSummary(t *testing.T) {
	f := newFakeGarmin(t)
	msgs := []aaMsg{}
	for _, item := range []struct {
		cadence uint8
		ts      string
	}{{60, "14:20:00"}, {60, "14:21:00"}, {85, "14:25:00"}} {
		msgs = append(msgs,
			aaMsg{num: mesgnum.Record, fields: []aaField{{fieldnum.RecordCadence, item.cadence}}},
			aaMsg{num: mesgnum.Event, fields: []aaField{
				{fieldnum.EventEvent, typedef.EventRearGearChange.Byte()},
				{fieldnum.EventData, uint32(53<<24 | 2<<16 | 17<<8 | 4)},
				{fieldnum.EventTimestamp, aaTime(t, "2024-03-02 "+item.ts)},
			}},
		)
	}
	aaServeFIT(t, f, aaActivityID, aaFIT(t, msgs...))
	cs := newSession(t, f, nil)

	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	summary := aaMap(t, data["shift_summary"], "shift_summary")
	if aaNum(t, summary["total_shifts"]) != 3 || aaNum(t, summary["reactive_shifts"]) != 2 || aaNum(t, summary["proactive_shifts"]) != 1 {
		t.Fatalf("summary = %v", summary)
	}
	usage := aaMap(t, summary["gear_usage"], "gear_usage")
	if aaNum(t, usage["53/17t"]) != 3 {
		t.Fatalf("usage = %v", usage)
	}
}

func TestShiftSummarySpacedShiftsAreNotPanicBursts(t *testing.T) {
	shifts := []map[string]any{}
	for minute := 0; minute < 60; minute += 5 {
		shifts = append(shifts, map[string]any{
			"event": "rear_gear_change", "quality": "proactive",
			"timestamp": fmt.Sprintf("2024-06-01 10:%02d:00", minute),
		})
	}
	if got := aaNum(t, computeShiftSummary(shifts)["panic_burst_episodes"]); got != 0 {
		t.Fatalf("bursts = %v", got)
	}
}

func TestShiftSummaryCountsClusteredShiftsAsOneEpisodeEach(t *testing.T) {
	times := []string{"10:00:00", "10:00:02", "10:00:04", "10:00:06", "10:05:00", "10:05:03", "10:05:05"}
	shifts := []map[string]any{}
	for _, ts := range times {
		shifts = append(shifts, map[string]any{"event": "rear_gear_change", "quality": "proactive", "timestamp": "2024-06-01 " + ts})
	}
	if got := aaNum(t, computeShiftSummary(shifts)["panic_burst_episodes"]); got != 2 {
		t.Fatalf("bursts = %v", got)
	}
}

func TestShiftSummaryIgnoresShiftsWithoutTimestamps(t *testing.T) {
	shifts := []map[string]any{
		{"event": "rear_gear_change", "quality": "proactive", "timestamp": ""},
		{"event": "rear_gear_change", "quality": "proactive", "timestamp": "not a time"},
		{"event": "rear_gear_change", "quality": "proactive"},
	}
	summary := computeShiftSummary(shifts)
	if aaNum(t, summary["total_shifts"]) != 3 || aaNum(t, summary["panic_burst_episodes"]) != 0 {
		t.Fatalf("summary = %v", summary)
	}
}

// --- records ----------------------------------------------------------------

func TestGetActivityFitDataRecordsExcludedByDefault(t *testing.T) {
	f := newFakeGarmin(t)
	aaServeFIT(t, f, aaActivityID, aaFIT(t, aaMsg{num: mesgnum.Record, fields: []aaField{
		{fieldnum.RecordCadence, uint8(85)}, {fieldnum.RecordPower, uint16(210)}, {fieldnum.RecordHeartRate, uint8(145)},
	}}))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	if _, ok := data["records"]; ok {
		t.Fatal("records present")
	}
}

func TestGetActivityFitDataRecordsIncludedWhenRequested(t *testing.T) {
	f := newFakeGarmin(t)
	aaServeFIT(t, f, aaActivityID, aaFIT(t, aaMsg{num: mesgnum.Record, fields: []aaField{
		{fieldnum.RecordCadence, uint8(85)}, {fieldnum.RecordPower, uint16(210)},
		{fieldnum.RecordHeartRate, uint8(145)}, {fieldnum.RecordSpeed, uint16(9200)},
		{fieldnum.RecordAltitude, uint16((130 + 500) * 5)},
		{fieldnum.RecordTimestamp, aaTime(t, "2024-03-02 14:00:00")},
		{fieldnum.RecordLeftPco, int8(3)}, {fieldnum.RecordRightPco, int8(-9)},
	}}))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{
		"activity_id": aaActivityID, "include_records": true,
	}))
	records := aaList(t, data["records"], "records")
	if len(records) != 1 {
		t.Fatalf("records = %v", records)
	}
	rec := aaMap(t, records[0], "record")
	if aaNum(t, rec["cadence_rpm"]) != 85 || aaNum(t, rec["power_w"]) != 210 ||
		aaNum(t, rec["left_pco_mm"]) != 3 || aaNum(t, rec["right_pco_mm"]) != -9 {
		t.Fatalf("record = %v", rec)
	}
}

// --- left/right balance -----------------------------------------------------

func TestGetActivityFitDataPowerBalance(t *testing.T) {
	f := newFakeGarmin(t)
	aaServeFIT(t, f, aaActivityID, aaFIT(t, aaMsg{num: mesgnum.Session, fields: []aaField{
		{fieldnum.SessionSport, typedef.SportCycling.Byte()},
		{fieldnum.SessionLeftRightBalance, uint16(4700)},
	}}))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	session := aaMap(t, data["session"], "session")
	if aaNum(t, session["avg_left_power_pct"]) != 47 || aaNum(t, session["avg_right_power_pct"]) != 53 {
		t.Fatalf("session = %v", session)
	}
}

func TestGetActivityFitDataPowerBalanceUsesProfileFieldTypes(t *testing.T) {
	f := newFakeGarmin(t)
	aaServeFIT(t, f, aaActivityID, aaFIT(t,
		aaMsg{num: mesgnum.Record, fields: []aaField{{fieldnum.RecordLeftRightBalance, uint8(0x80 | 52)}}},
		aaMsg{num: mesgnum.Lap, fields: []aaField{{fieldnum.LapLeftRightBalance, uint16(0x8000 | 5100)}}},
		aaMsg{num: mesgnum.Session, fields: []aaField{{fieldnum.SessionLeftRightBalance, uint16(0x8000 | 5300)}}},
	))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{
		"activity_id": aaActivityID, "include_records": true,
	}))
	session := aaMap(t, data["session"], "session")
	if aaNum(t, session["avg_left_power_pct"]) != 47 || aaNum(t, session["avg_right_power_pct"]) != 53 {
		t.Fatalf("session = %v", session)
	}
	lap := aaMap(t, aaList(t, data["laps"], "laps")[0], "lap")
	if aaNum(t, lap["avg_left_power_pct"]) != 49 || aaNum(t, lap["avg_right_power_pct"]) != 51 {
		t.Fatalf("lap = %v", lap)
	}
	rec := aaMap(t, aaList(t, data["records"], "records")[0], "record")
	if aaNum(t, rec["left_power_pct"]) != 48 || aaNum(t, rec["right_power_pct"]) != 52 {
		t.Fatalf("record = %v", rec)
	}
}

func TestGetActivityFitDataSessionPowerPhaseUsesProfileIndexes(t *testing.T) {
	f := newFakeGarmin(t)
	aaServeFIT(t, f, aaActivityID, aaFIT(t, aaMsg{num: mesgnum.Session, fields: []aaField{
		{fieldnum.SessionAvgLeftPowerPhase, []uint8{240, 16}},
		{fieldnum.SessionAvgRightPowerPhase, []uint8{246, 11}},
	}}))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	session := aaMap(t, data["session"], "session")
	for key, want := range map[string]float64{
		"avg_left_power_phase_start_deg": 337.5, "avg_left_power_phase_end_deg": 22.5,
		"avg_right_power_phase_start_deg": 345.9375, "avg_right_power_phase_end_deg": 15.46875,
	} {
		if math.Abs(aaNum(t, session[key])-want) > 0.001 {
			t.Fatalf("%s = %v, want %v", key, session[key], want)
		}
	}
}

// --- no DI2 / variability / lap details ------------------------------------

func TestGetActivityFitDataNoShifts(t *testing.T) {
	f := newFakeGarmin(t)
	aaServeFIT(t, f, aaActivityID, aaFIT(t, aaMsg{num: mesgnum.Session, fields: []aaField{
		{fieldnum.SessionSport, typedef.SportCycling.Byte()},
	}}))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	summary := aaMap(t, data["shift_summary"], "shift_summary")
	if aaNum(t, summary["total_shifts"]) != 0 || summary["note"] == nil {
		t.Fatalf("summary = %v", summary)
	}
	if len(aaList(t, data["shifts"], "shifts")) != 0 {
		t.Fatalf("shifts = %v", data["shifts"])
	}
}

func TestGetActivityFitDataVariabilityIndexSession(t *testing.T) {
	f := newFakeGarmin(t)
	aaServeFIT(t, f, aaActivityID, aaFIT(t, aaMsg{num: mesgnum.Session, fields: []aaField{
		{fieldnum.SessionSport, typedef.SportCycling.Byte()},
		{fieldnum.SessionNormalizedPower, uint16(210)},
		{fieldnum.SessionAvgPower, uint16(175)},
	}}))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	session := aaMap(t, data["session"], "session")
	if math.Abs(aaNum(t, session["variability_index"])-1.2) > 0.001 {
		t.Fatalf("session = %v", session)
	}
}

func TestGetActivityFitDataVariabilityIndexLap(t *testing.T) {
	f := newFakeGarmin(t)
	aaServeFIT(t, f, aaActivityID, aaFIT(t, aaMsg{num: mesgnum.Lap, fields: []aaField{
		{fieldnum.LapNormalizedPower, uint16(240)},
		{fieldnum.LapAvgPower, uint16(200)},
		{fieldnum.LapTotalElapsedTime, uint32(1800 * 1000)},
	}}))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	laps := aaList(t, data["laps"], "laps")
	if len(laps) != 1 || math.Abs(aaNum(t, aaMap(t, laps[0], "lap")["variability_index"])-1.2) > 0.001 {
		t.Fatalf("laps = %v", laps)
	}
}

func TestGetActivityFitDataLapTorqueAndSmoothness(t *testing.T) {
	f := newFakeGarmin(t)
	aaServeFIT(t, f, aaActivityID, aaFIT(t, aaMsg{num: mesgnum.Lap, fields: []aaField{
		{fieldnum.LapAvgLeftTorqueEffectiveness, uint8(165)},
		{fieldnum.LapAvgRightTorqueEffectiveness, uint8(158)},
		{fieldnum.LapAvgLeftPedalSmoothness, uint8(62)},
		{fieldnum.LapAvgRightPedalSmoothness, uint8(57)},
		{fieldnum.LapTotalElapsedTime, uint32(3600 * 1000)},
	}}))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	lap := aaMap(t, aaList(t, data["laps"], "laps")[0], "lap")
	for key, want := range map[string]float64{
		"avg_left_torque_effectiveness_pct": 82.5, "avg_right_torque_effectiveness_pct": 79,
		"avg_left_pedal_smoothness_pct": 31, "avg_right_pedal_smoothness_pct": 28.5,
	} {
		if aaNum(t, lap[key]) != want {
			t.Fatalf("%s = %v", key, lap[key])
		}
	}
}

func TestGetActivityFitDataShiftTerrainClassification(t *testing.T) {
	f := newFakeGarmin(t)
	gearData := uint32(53<<24 | 2<<16 | 17<<8 | 4)
	aaServeFIT(t, f, aaActivityID, aaFIT(t,
		aaMsg{num: mesgnum.Record, fields: []aaField{{fieldnum.RecordCadence, uint8(75)}, {fieldnum.RecordGrade, int16(550)}}},
		aaMsg{num: mesgnum.Event, fields: []aaField{
			{fieldnum.EventEvent, typedef.EventRearGearChange.Byte()},
			{fieldnum.EventData, gearData},
			{fieldnum.EventTimestamp, aaTime(t, "2024-03-02 14:20:00")},
		}},
		aaMsg{num: mesgnum.Record, fields: []aaField{{fieldnum.RecordCadence, uint8(90)}, {fieldnum.RecordGrade, int16(100)}}},
		aaMsg{num: mesgnum.Event, fields: []aaField{
			{fieldnum.EventEvent, typedef.EventRearGearChange.Byte()},
			{fieldnum.EventData, gearData},
			{fieldnum.EventTimestamp, aaTime(t, "2024-03-02 14:21:00")},
		}},
	))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	shifts := aaList(t, data["shifts"], "shifts")
	if aaNum(t, aaMap(t, shifts[0], "shift")["grade_at_shift_pct"]) != 5.5 {
		t.Fatalf("shifts = %v", shifts)
	}
	if aaNum(t, aaMap(t, shifts[1], "shift")["grade_at_shift_pct"]) != 1 {
		t.Fatalf("shifts = %v", shifts)
	}
	byTerrain := aaMap(t, aaMap(t, data["shift_summary"], "summary")["by_terrain"], "by_terrain")
	if byTerrain["climbing"] == nil || byTerrain["flat"] == nil {
		t.Fatalf("by_terrain = %v", byTerrain)
	}
}

// --- power duration curve (unit) -------------------------------------------

func TestPowerDurationCurveBasic(t *testing.T) {
	records := []map[string]any{}
	for i := 0; i < 10; i++ {
		records = append(records, map[string]any{"power_w": 500})
	}
	for i := 0; i < 110; i++ {
		records = append(records, map[string]any{"power_w": 100})
	}
	pdc := computePowerDurationCurve(records)
	if pdc == nil || aaNum(t, pdc["5s"]) != 500 || aaNum(t, pdc["1min"]) >= 500 {
		t.Fatalf("pdc = %v", pdc)
	}
}

func TestPowerDurationCurveEmpty(t *testing.T) {
	records := make([]map[string]any, 100)
	for i := range records {
		records[i] = map[string]any{"cadence_rpm": 80}
	}
	if got := computePowerDurationCurve(records); got != nil {
		t.Fatalf("pdc = %v", got)
	}
}

func TestPowerDurationCurveShortRide(t *testing.T) {
	records := make([]map[string]any, 30)
	for i := range records {
		records[i] = map[string]any{"power_w": 300}
	}
	pdc := computePowerDurationCurve(records)
	if pdc == nil || pdc["5s"] == nil || pdc["1min"] != nil {
		t.Fatalf("pdc = %v", pdc)
	}
}

func TestPowerDurationCurveSlidingWindow(t *testing.T) {
	records := []map[string]any{}
	for i := 0; i < 10; i++ {
		records = append(records, map[string]any{"power_w": 100})
	}
	for i := 0; i < 5; i++ {
		records = append(records, map[string]any{"power_w": 400})
	}
	for i := 0; i < 10; i++ {
		records = append(records, map[string]any{"power_w": 100})
	}
	if got := aaNum(t, computePowerDurationCurve(records)["5s"]); got != 400 {
		t.Fatalf("5s = %v", got)
	}
}

func aaRecordsEvery(t *testing.T, step int, rows []map[string]any, start time.Time) []map[string]any {
	t.Helper()
	out := make([]map[string]any, 0, len(rows))
	for i, row := range rows {
		copyRow := map[string]any{"timestamp": fmt.Sprint(start.Add(time.Duration(i*step) * time.Second).Format("2006-01-02 15:04:05"))}
		for k, v := range row {
			copyRow[k] = v
		}
		out = append(out, copyRow)
	}
	return out
}

func TestPowerDurationCurveResamples3sRecordsTo1Hz(t *testing.T) {
	rows := []map[string]any{}
	for i := 0; i < 400; i++ {
		rows = append(rows, map[string]any{"power_w": 300})
	}
	for i := 0; i < 800; i++ {
		rows = append(rows, map[string]any{"power_w": 100})
	}
	pdc := computePowerDurationCurve(aaRecordsEvery(t, 3, rows, time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)))
	if aaNum(t, pdc["20min"]) != 300 || aaNum(t, pdc["60min"]) != 167 {
		t.Fatalf("pdc = %v", pdc)
	}
}

func TestPowerDurationCurveCountsPausesAsZero(t *testing.T) {
	rows := []map[string]any{}
	for i := 0; i < 60; i++ {
		rows = append(rows, map[string]any{"power_w": 400})
	}
	before := aaRecordsEvery(t, 1, rows, time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC))
	after := aaRecordsEvery(t, 1, rows, time.Date(2024, 6, 1, 10, 31, 0, 0, time.UTC))
	pdc := computePowerDurationCurve(append(before, after...))
	if aaNum(t, pdc["1min"]) != 400 || aaNum(t, pdc["5min"]) != 80 {
		t.Fatalf("pdc = %v", pdc)
	}
}

// --- climb detection --------------------------------------------------------

func aaClimbRecords(grade, power, cadence, hr, speed, altitudeStart float64, count int) []map[string]any {
	records := make([]map[string]any, 0, count)
	alt := altitudeStart
	for i := 0; i < count; i++ {
		alt += speed * grade / 100
		records = append(records, map[string]any{
			"grade_pct": grade, "power_w": power, "cadence_rpm": cadence,
			"heart_rate_bpm": hr, "speed_mps": speed, "altitude_m": alt,
			"timestamp": fmt.Sprintf("2024-03-02 14:%02d:%02d", i/60, i%60),
		})
	}
	return records
}

func TestDetectClimbsBasic(t *testing.T) {
	climbing := aaClimbRecords(5, 280, 75, 160, 5, 100, 200)
	flat := aaClimbRecords(0.5, 180, 90, 140, 8, 200, 100)
	climbs := detectClimbs(append(climbing, flat...), 30, 3, 60)
	if len(climbs) == 0 {
		t.Fatal("no climbs")
	}
	climb := climbs[0]
	if aaNum(t, climb["avg_grade_pct"]) != 5 || aaNum(t, climb["avg_power_w"]) != 280 || climb["vam_m_per_hr"] == nil {
		t.Fatalf("climb = %v", climb)
	}
}

func TestDetectClimbsNoneWhenFlat(t *testing.T) {
	if got := detectClimbs(aaClimbRecords(1, 200, 90, 140, 9, 50, 300), 50, 3, 120); len(got) != 0 {
		t.Fatalf("climbs = %v", got)
	}
}

func TestDetectClimbsVAMCalculation(t *testing.T) {
	climbs := detectClimbs(aaClimbRecords(5, 280, 75, 155, 5, 0, 300), 30, 3, 60)
	if len(climbs) == 0 || aaNum(t, climbs[0]["vam_m_per_hr"]) <= 500 {
		t.Fatalf("climbs = %v", climbs)
	}
}

func TestDetectClimbsUsesTimestampsFor3sRecords(t *testing.T) {
	climbing := []map[string]any{}
	for i := 0; i < 400; i++ {
		climbing = append(climbing, map[string]any{
			"grade_pct": 6.0, "power_w": 300, "speed_mps": 5.0,
			"altitude_m": 100 + 0.9*float64(i), "distance_m": 15.0 * float64(i),
		})
	}
	flat := []map[string]any{}
	for i := 0; i < 40; i++ {
		flat = append(flat, map[string]any{
			"grade_pct": 0.0, "power_w": 150, "speed_mps": 9.0,
			"altitude_m": 100 + 0.9*399, "distance_m": 5985 + 27*float64(i+1),
		})
	}
	climbs := detectClimbs(aaRecordsEvery(t, 3, append(climbing, flat...), time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)), 50, 3, 120)
	if len(climbs) != 1 {
		t.Fatalf("climbs = %v", climbs)
	}
	climb := climbs[0]
	if aaNum(t, climb["duration_s"]) != 1200 || math.Abs(aaNum(t, climb["elevation_gain_m"])-359.1) > 0.01 ||
		aaNum(t, climb["vam_m_per_hr"]) != 1077 || aaNum(t, climb["distance_m"]) != 5985 {
		t.Fatalf("climb = %v", climb)
	}
}

func TestDetectClimbsFlatToleranceIsInSeconds(t *testing.T) {
	climb := []map[string]any{}
	for i := 0; i < 200; i++ {
		climb = append(climb, map[string]any{"grade_pct": 6.0, "altitude_m": 100 + 0.9*float64(i)})
	}
	flat := make([]map[string]any, 15)
	for i := range flat {
		flat[i] = map[string]any{"grade_pct": 0.0, "altitude_m": 280.0}
	}
	second := []map[string]any{}
	for i := 0; i < 200; i++ {
		second = append(second, map[string]any{"grade_pct": 6.0, "altitude_m": 280 + 0.9*float64(i)})
	}
	all := append(append(climb, flat...), second...)
	climbs := detectClimbs(aaRecordsEvery(t, 3, all, time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)), 50, 3, 120)
	durations := []float64{}
	for _, climb := range climbs {
		durations = append(durations, aaNum(t, climb["duration_s"]))
	}
	if fmt.Sprint(durations) != "[600 600]" {
		t.Fatalf("durations = %v", durations)
	}
}

func TestDetectClimbsSpeedDistanceUsesRecordSpacing(t *testing.T) {
	climbing := []map[string]any{}
	for i := 0; i < 400; i++ {
		climbing = append(climbing, map[string]any{
			"grade_pct": 6.0, "speed_mps": 5.0, "altitude_m": 100 + 0.9*float64(i),
		})
	}
	climbs := detectClimbs(aaRecordsEvery(t, 3, climbing, time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)), 50, 3, 120)
	if len(climbs) != 1 || aaNum(t, climbs[0]["distance_m"]) != 6000 {
		t.Fatalf("climbs = %v", climbs)
	}
}

// --- HR drift ---------------------------------------------------------------

func TestHRDriftSignificant(t *testing.T) {
	records := []map[string]any{}
	for i := 0; i < 1900; i++ {
		records = append(records, map[string]any{"power_w": 250, "heart_rate_bpm": 140})
	}
	for i := 0; i < 1900; i++ {
		records = append(records, map[string]any{"power_w": 250, "heart_rate_bpm": 165})
	}
	drift := computeHRDrift(records)
	if drift == nil || aaNum(t, drift["hr_drift_pct"]) >= -10 || drift["interpretation"] != "significant_decoupling" {
		t.Fatalf("drift = %v", drift)
	}
}

func TestHRDriftInterpretationUsesDriftSign(t *testing.T) {
	cases := []struct {
		hr   float64
		sign float64
		want string
	}{
		{155, -1, "moderate_drift"},
		{132, 1, "power_increased_vs_hr"},
		{125, 1, "power_increased_vs_hr"},
	}
	for _, tc := range cases {
		records := []map[string]any{}
		for i := 0; i < 1900; i++ {
			records = append(records, map[string]any{"power_w": 200, "heart_rate_bpm": 140})
		}
		for i := 0; i < 1900; i++ {
			records = append(records, map[string]any{"power_w": 200, "heart_rate_bpm": tc.hr})
		}
		drift := computeHRDrift(records)
		if drift == nil || aaNum(t, drift["hr_drift_pct"])*tc.sign <= 5 || drift["interpretation"] != tc.want {
			t.Fatalf("hr %v: drift = %v", tc.hr, drift)
		}
	}
}

func TestHRDriftWellCoupled(t *testing.T) {
	records := make([]map[string]any, 4000)
	for i := range records {
		records[i] = map[string]any{"power_w": 200, "heart_rate_bpm": 145}
	}
	drift := computeHRDrift(records)
	if drift == nil || math.Abs(aaNum(t, drift["hr_drift_pct"])) >= 5 || drift["interpretation"] != "well_coupled" {
		t.Fatalf("drift = %v", drift)
	}
}

func TestHRDriftSkippedForShortRides(t *testing.T) {
	records := make([]map[string]any, 1000)
	for i := range records {
		records[i] = map[string]any{"power_w": 200, "heart_rate_bpm": 145}
	}
	if got := computeHRDrift(records); got != nil {
		t.Fatalf("drift = %v", got)
	}
}

func TestHRDriftRequires60MinutesNot3600Records(t *testing.T) {
	row := map[string]any{"power_w": 200, "heart_rate_bpm": 145}
	rows := make([]map[string]any, 1400)
	for i := range rows {
		rows[i] = row
	}
	drift := computeHRDrift(aaRecordsEvery(t, 3, rows, time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)))
	if drift == nil || drift["interpretation"] != "well_coupled" {
		t.Fatalf("drift = %v", drift)
	}
	short := make([]map[string]any, 1000)
	for i := range short {
		short[i] = row
	}
	if got := computeHRDrift(aaRecordsEvery(t, 3, short, time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC))); got != nil {
		t.Fatalf("short drift = %v", got)
	}
}

// --- temperature ------------------------------------------------------------

func TestTemperatureStatsBasic(t *testing.T) {
	records := []map[string]any{}
	for i := 0; i < 50; i++ {
		records = append(records, map[string]any{"temperature_c": 15, "heart_rate_bpm": 140, "power_w": 220})
	}
	for i := 0; i < 50; i++ {
		records = append(records, map[string]any{"temperature_c": 30, "heart_rate_bpm": 160, "power_w": 210})
	}
	stats := computeTemperatureStats(records)
	if stats == nil || aaNum(t, stats["min_temp_c"]) != 15 || aaNum(t, stats["max_temp_c"]) != 30 || aaNum(t, stats["temp_range_c"]) != 15 {
		t.Fatalf("stats = %v", stats)
	}
}

func TestTemperatureStatsNoneWhenNoData(t *testing.T) {
	records := make([]map[string]any, 50)
	for i := range records {
		records[i] = map[string]any{"power_w": 200, "heart_rate_bpm": 145}
	}
	if got := computeTemperatureStats(records); got != nil {
		t.Fatalf("stats = %v", got)
	}
}

func TestTemperatureStatsIncludeZeroPowerAndSkipMissingSamples(t *testing.T) {
	records := []map[string]any{
		{"temperature_c": 10, "power_w": 0, "heart_rate_bpm": 0},
		{"temperature_c": 10, "power_w": 200, "heart_rate_bpm": 140},
		{"temperature_c": 10},
	}
	for i := 0; i < 3; i++ {
		records = append(records, map[string]any{"temperature_c": 20})
	}
	records = append(records,
		map[string]any{"temperature_c": 30, "power_w": 0, "heart_rate_bpm": 0},
		map[string]any{"temperature_c": 30, "power_w": 0, "heart_rate_bpm": 150},
		map[string]any{"temperature_c": 30},
	)
	stats := computeTemperatureStats(records)
	if aaNum(t, stats["avg_power_coolest_third_w"]) != 100 ||
		aaNum(t, stats["avg_power_hottest_third_w"]) != 0 ||
		aaNum(t, stats["avg_hr_coolest_third_bpm"]) != 140 ||
		aaNum(t, stats["avg_hr_hottest_third_bpm"]) != 150 {
		t.Fatalf("stats = %v", stats)
	}
}

// --- grade analysis ---------------------------------------------------------

func TestGradeAnalysisBinsCorrectly(t *testing.T) {
	records := []map[string]any{}
	for i := 0; i < 60; i++ {
		records = append(records, map[string]any{"grade_pct": 0.5, "power_w": 180, "cadence_rpm": 90, "heart_rate_bpm": 140})
	}
	for i := 0; i < 60; i++ {
		records = append(records, map[string]any{"grade_pct": 4.5, "power_w": 280, "cadence_rpm": 75, "heart_rate_bpm": 165})
	}
	for i := 0; i < 60; i++ {
		records = append(records, map[string]any{"grade_pct": 7.5, "power_w": 320, "cadence_rpm": 65, "heart_rate_bpm": 175})
	}
	for i := 0; i < 60; i++ {
		records = append(records, map[string]any{"grade_pct": -4.0, "power_w": 50, "cadence_rpm": 95, "heart_rate_bpm": 120})
	}
	result := gradeAnalysis(records)
	if result == nil {
		t.Fatal("nil result")
	}
	for _, key := range []string{"flat", "gentle", "moderate", "descending"} {
		if result[key] == nil {
			t.Fatalf("missing %s: %v", key, result)
		}
	}
	if aaNum(t, aaMap(t, result["gentle"], "gentle")["avg_power_w"]) >= aaNum(t, aaMap(t, result["moderate"], "moderate")["avg_power_w"]) {
		t.Fatalf("result = %v", result)
	}
}

func TestGradeAnalysisTimeUsesRecordSpacing(t *testing.T) {
	rows := []map[string]any{}
	for i := 0; i < 100; i++ {
		rows = append(rows, map[string]any{"grade_pct": 0.5})
	}
	for i := 0; i < 50; i++ {
		rows = append(rows, map[string]any{"grade_pct": 4.5})
	}
	result := gradeAnalysis(aaRecordsEvery(t, 3, rows, time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)))
	if aaNum(t, aaMap(t, result["flat"], "flat")["time_s"]) != 300 || aaNum(t, aaMap(t, result["gentle"], "gentle")["time_s"]) != 150 {
		t.Fatalf("result = %v", result)
	}
}

// --- HRV metrics ------------------------------------------------------------

func TestComputeHRVMetricsReturnsNoneBelowMinimum(t *testing.T) {
	rr := make([]float64, 9)
	for i := range rr {
		rr[i] = 0.6
	}
	if got := computeHRVMetrics(rr); got != nil {
		t.Fatalf("metrics = %v", got)
	}
}

func TestComputeHRVMetricsCorrectValues(t *testing.T) {
	rr := []float64{}
	for i := 0; i < 10; i++ {
		rr = append(rr, 0.6, 0.7)
	}
	result := computeHRVMetrics(rr)
	if result == nil || aaNum(t, result["rmssd_ms"]) != 100 || aaNum(t, result["sdnn_ms"]) != 51.3 ||
		aaNum(t, result["pnn50_pct"]) != 100 || aaNum(t, result["mean_rr_ms"]) != 650 ||
		aaNum(t, result["mean_hr_bpm"]) != 92.3 || aaNum(t, result["rr_count"]) != 20 {
		t.Fatalf("result = %v", result)
	}
}

func TestComputeHRVMetricsPnn50ZeroWhenNoLargeDiffs(t *testing.T) {
	rr := make([]float64, 20)
	for i := range rr {
		rr[i] = 0.6
	}
	result := computeHRVMetrics(rr)
	if result == nil || aaNum(t, result["pnn50_pct"]) != 0 || aaNum(t, result["rmssd_ms"]) != 0 || aaNum(t, result["sdnn_ms"]) != 0 {
		t.Fatalf("result = %v", result)
	}
}

// --- HRV through the tool ---------------------------------------------------

func aaHRVFit(t *testing.T, rr []uint16, extra ...aaMsg) []byte {
	t.Helper()
	msgs := append([]aaMsg{}, extra...)
	msgs = append(msgs, aaMsg{num: mesgnum.Hrv, fields: []aaField{{fieldnum.HrvTime, rr}}})
	return aaFIT(t, msgs...)
}

func TestFitNoHrvMessagesProducesNoHrvKey(t *testing.T) {
	f := newFakeGarmin(t)
	aaServeFIT(t, f, aaActivityID, aaFIT(t, aaMsg{num: mesgnum.Record, fields: []aaField{{fieldnum.RecordHeartRate, uint8(140)}}}))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	if _, ok := data["hrv"]; ok {
		t.Fatal("hrv present")
	}
}

func TestFitHrvSummaryPresentWithEnoughIntervals(t *testing.T) {
	f := newFakeGarmin(t)
	rr := []uint16{}
	for i := 0; i < 10; i++ {
		rr = append(rr, 600, 700)
	}
	aaServeFIT(t, f, aaActivityID, aaHRVFit(t, rr,
		aaMsg{num: mesgnum.Record, fields: []aaField{{fieldnum.RecordTimestamp, aaTime(t, "2026-05-15 10:00:00")}}}))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	hrv := aaMap(t, data["hrv"], "hrv")
	if aaNum(t, hrv["rr_count"]) != 20 {
		t.Fatalf("hrv = %v", hrv)
	}
	for _, key := range []string{"rmssd_ms", "sdnn_ms", "pnn50_pct", "mean_rr_ms"} {
		if hrv[key] == nil {
			t.Fatalf("missing %s in %v", key, hrv)
		}
	}
}

func TestFitHrvSentinelValuesFiltered(t *testing.T) {
	f := newFakeGarmin(t)
	rr := make([]uint16, 20)
	for i := range rr {
		rr[i] = 600
	}
	rr = append(rr, 65535, 65535, 65535, 65535, 65535)
	aaServeFIT(t, f, aaActivityID, aaHRVFit(t, rr,
		aaMsg{num: mesgnum.Record, fields: []aaField{{fieldnum.RecordTimestamp, aaTime(t, "2026-05-15 10:00:00")}}}))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	if aaNum(t, aaMap(t, data["hrv"], "hrv")["rr_count"]) != 20 {
		t.Fatalf("hrv = %v", data["hrv"])
	}
}

func TestFitHrvNotProducedBelowMinimumSamples(t *testing.T) {
	f := newFakeGarmin(t)
	rr := make([]uint16, 9)
	for i := range rr {
		rr[i] = 600
	}
	aaServeFIT(t, f, aaActivityID, aaHRVFit(t, rr,
		aaMsg{num: mesgnum.Record, fields: []aaField{{fieldnum.RecordTimestamp, aaTime(t, "2026-05-15 10:00:00")}}}))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	if _, ok := data["hrv"]; ok {
		t.Fatal("hrv present")
	}
}

func TestFitHrvRrIntervalsExcludedByDefault(t *testing.T) {
	f := newFakeGarmin(t)
	rr := make([]uint16, 20)
	for i := range rr {
		rr[i] = 650
	}
	aaServeFIT(t, f, aaActivityID, aaHRVFit(t, rr,
		aaMsg{num: mesgnum.Record, fields: []aaField{{fieldnum.RecordTimestamp, aaTime(t, "2026-05-15 10:00:00")}}}))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	if _, ok := data["rr_intervals_seconds"]; ok {
		t.Fatal("rr intervals present")
	}
	if data["hrv"] == nil {
		t.Fatal("hrv missing")
	}
}

func TestFitHrvRrIntervalsIncludedWhenRecordsRequested(t *testing.T) {
	f := newFakeGarmin(t)
	rr := make([]uint16, 20)
	for i := range rr {
		rr[i] = 650
	}
	aaServeFIT(t, f, aaActivityID, aaHRVFit(t, rr,
		aaMsg{num: mesgnum.Record, fields: []aaField{{fieldnum.RecordTimestamp, aaTime(t, "2026-05-15 10:00:00")}}}))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{
		"activity_id": aaActivityID, "include_records": true,
	}))
	intervals := aaList(t, data["rr_intervals_seconds"], "rr_intervals_seconds")
	if len(intervals) != 20 || aaNum(t, aaMap(t, intervals[0], "interval")["rr_seconds"]) != 0.65 {
		t.Fatalf("intervals = %v", intervals)
	}
}

func TestFitHrvPerLapBucketing(t *testing.T) {
	f := newFakeGarmin(t)
	raw := aaFIT(t,
		aaMsg{num: mesgnum.Record, fields: []aaField{{fieldnum.RecordTimestamp, aaTime(t, "2026-05-15 10:05:00")}}},
		aaHRVMsg(t, []uint16{600, 700, 600, 700, 600, 700, 600, 700, 600, 700, 600, 700, 600, 700, 600, 700, 600, 700, 600, 700}),
		aaMsg{num: mesgnum.Record, fields: []aaField{{fieldnum.RecordTimestamp, aaTime(t, "2026-05-15 10:15:00")}}},
		aaHRVMsg(t, []uint16{600, 700, 600, 700, 600, 700, 600, 700, 600, 700, 600, 700, 600, 700, 600, 700, 600, 700, 600, 700}),
		aaMsg{num: mesgnum.Lap, fields: []aaField{
			{fieldnum.LapStartTime, aaTime(t, "2026-05-15 10:00:00")},
			{fieldnum.LapTotalElapsedTime, uint32(600 * 1000)},
		}},
		aaMsg{num: mesgnum.Lap, fields: []aaField{
			{fieldnum.LapStartTime, aaTime(t, "2026-05-15 10:10:00")},
			{fieldnum.LapTotalElapsedTime, uint32(600 * 1000)},
		}},
	)
	aaServeFIT(t, f, aaActivityID, raw)
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	laps := aaList(t, data["laps"], "laps")
	if len(laps) != 2 {
		t.Fatalf("laps = %v", laps)
	}
	for i := 0; i < 2; i++ {
		hrv := aaMap(t, aaMap(t, laps[i], "lap")["hrv"], "lap hrv")
		if aaNum(t, hrv["rr_count"]) != 20 {
			t.Fatalf("lap %d hrv = %v", i, hrv)
		}
	}
}

func TestFitHrvLapBelowMinimumGetsNoHrv(t *testing.T) {
	f := newFakeGarmin(t)
	raw := aaFIT(t,
		aaMsg{num: mesgnum.Record, fields: []aaField{{fieldnum.RecordTimestamp, aaTime(t, "2026-05-15 10:05:00")}}},
		aaHRVMsg(t, []uint16{600, 700, 600, 700, 600, 700, 600, 700, 600, 700, 600, 700, 600, 700, 600, 700, 600, 700, 600, 700}),
		aaMsg{num: mesgnum.Record, fields: []aaField{{fieldnum.RecordTimestamp, aaTime(t, "2026-05-15 10:15:00")}}},
		aaHRVMsg(t, []uint16{600, 600, 600, 600, 600}),
		aaMsg{num: mesgnum.Lap, fields: []aaField{
			{fieldnum.LapStartTime, aaTime(t, "2026-05-15 10:00:00")},
			{fieldnum.LapTotalElapsedTime, uint32(600 * 1000)},
		}},
		aaMsg{num: mesgnum.Lap, fields: []aaField{
			{fieldnum.LapStartTime, aaTime(t, "2026-05-15 10:10:00")},
			{fieldnum.LapTotalElapsedTime, uint32(600 * 1000)},
		}},
	)
	aaServeFIT(t, f, aaActivityID, raw)
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	laps := aaList(t, data["laps"], "laps")
	if len(laps) != 2 || aaMap(t, laps[0], "lap")["hrv"] == nil || aaMap(t, laps[1], "lap")["hrv"] != nil {
		t.Fatalf("laps = %v", laps)
	}
}

func aaHRVMsg(t *testing.T, rr []uint16) aaMsg {
	t.Helper()
	return aaMsg{num: mesgnum.Hrv, fields: []aaField{{fieldnum.HrvTime, rr}}}
}

// --- power duration curve tool ---------------------------------------------

func aaPowerFit(t *testing.T, watts uint16, count int) []byte {
	msgs := make([]aaMsg, 0, count)
	for i := 0; i < count; i++ {
		msgs = append(msgs, aaMsg{num: mesgnum.Record, fields: []aaField{{fieldnum.RecordPower, watts}}})
	}
	return aaFIT(t, msgs...)
}

func TestPowerDurationCurveFiltersByRequestedActivityType(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /activitylist-service/activities/search/activities", []any{
		map[string]any{"activityId": 1, "startTimeLocal": "2024-06-01 08:00:00", "activityType": map[string]any{"typeKey": "running", "parentTypeId": 17}},
		map[string]any{"activityId": 2, "startTimeLocal": "2024-06-02 08:00:00", "activityType": map[string]any{"typeKey": "road_biking", "parentTypeId": 2}},
	})
	aaServeFIT(t, f, 1, aaPowerFit(t, 250, 60))
	aaServeFIT(t, f, 2, aaPowerFit(t, 900, 60))
	cs := newSession(t, f, nil)

	for _, tc := range []struct {
		activityType string
		watts        float64
		id           float64
	}{{"running", 250, 1}, {"cycling", 900, 2}} {
		data := aaDownloaded(t, mustCallTool(t, cs, "get_power_duration_curve", map[string]any{"activity_type": tc.activityType}))
		if aaNum(t, data["activities_analyzed"]) != 1 {
			t.Fatalf("analyzed = %v", data)
		}
		best := aaMap(t, aaMap(t, data["season_bests"], "season_bests")["5s"], "5s")
		if aaNum(t, best["watts"]) != tc.watts || aaNum(t, best["activity_id"]) != tc.id {
			t.Fatalf("%s best = %v", tc.activityType, best)
		}
	}
}

func TestPowerDurationCurveIncludesCyclingParentSubtypes(t *testing.T) {
	f := newFakeGarmin(t)
	keys := []string{"virtual_ride", "cyclocross", "e_bike_fitness", "e_bike_mountain"}
	activities := []any{}
	for i, key := range keys {
		activities = append(activities, map[string]any{
			"activityId": i + 1, "activityType": map[string]any{"typeKey": key, "parentTypeId": 2},
		})
		aaServeFIT(t, f, int64(i+1), aaPowerFit(t, 300, 60))
	}
	f.on("GET /activitylist-service/activities/search/activities", activities)
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_power_duration_curve", map[string]any{}))
	if aaNum(t, data["activities_analyzed"]) != 4 {
		t.Fatalf("analyzed = %v", data)
	}
	calls := f.callsMatching("GET", "/download-service/files/activity/1")
	if len(calls) != 1 {
		t.Fatalf("calls = %v", calls)
	}
	for _, id := range []int64{2, 3, 4} {
		if len(f.callsMatching("GET", fmt.Sprintf("/download-service/files/activity/%d", id))) != 1 {
			t.Fatalf("missing download %d", id)
		}
	}
}

func TestPowerDurationCurveRequestsMatchingActivitiesBeforeLimiting(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /activitylist-service/activities/search/activities", []any{
		map[string]any{"activityId": 2, "activityType": map[string]any{"typeKey": "road_biking", "parentTypeId": 2}},
		map[string]any{"activityId": 4, "activityType": map[string]any{"typeKey": "road_biking", "parentTypeId": 2}},
	})
	aaServeFIT(t, f, 2, aaPowerFit(t, 300, 60))
	aaServeFIT(t, f, 4, aaPowerFit(t, 300, 60))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_power_duration_curve", map[string]any{
		"num_activities": 2, "activity_type": "CYCLING",
	}))
	if aaNum(t, data["activities_analyzed"]) != 2 {
		t.Fatalf("data = %v", data)
	}
	calls := f.callsMatching("GET", "/activitylist-service/activities/search/activities")
	if len(calls) != 1 {
		t.Fatalf("calls = %v", calls)
	}
	if calls[0].Query.Get("activityType") != "cycling" || calls[0].Query.Get("start") != "0" || calls[0].Query.Get("limit") != "2" {
		t.Fatalf("query = %v", calls[0].Query)
	}
}

func TestPowerDurationCurveToolUsesRecordTimestamps(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /activitylist-service/activities/search/activities", []any{
		map[string]any{"activityId": 2, "startTimeLocal": "2024-06-01 10:00:00", "activityType": map[string]any{"typeKey": "road_biking", "parentTypeId": 2}},
	})
	msgs := []aaMsg{}
	start := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 1200; i++ {
		watts := uint16(300)
		if i >= 400 {
			watts = 100
		}
		msgs = append(msgs, aaMsg{num: mesgnum.Record, fields: []aaField{
			{fieldnum.RecordPower, watts},
			{fieldnum.RecordTimestamp, aaFITTime(t, start.Add(time.Duration(3*i)*time.Second))},
		}})
	}
	aaServeFIT(t, f, 2, aaFIT(t, msgs...))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_power_duration_curve", map[string]any{}))
	bests := aaMap(t, data["season_bests"], "season_bests")
	if aaNum(t, aaMap(t, bests["20min"], "20min")["watts"]) != 300 ||
		aaNum(t, aaMap(t, bests["60min"], "60min")["watts"]) != 167 ||
		aaNum(t, data["ftp_estimate_w"]) != 285 {
		t.Fatalf("data = %v", data)
	}
}

// --- download directory / file tools ---------------------------------------

func TestResolveDownloadDirPrefersOutputDirArg(t *testing.T) {
	f := newFakeGarmin(t)
	dir := t.TempDir()
	f.on(fmt.Sprintf("GET /download-service/files/activity/%d", aaActivityID), rawBody([]byte{0x0e, 0x10, 'F', 'I', 'T'}))
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "download_activity_file", map[string]any{
		"activity_id": aaActivityID, "output_dir": dir,
	})
	data := aaDownloaded(t, text)
	if data["file_path"] != filepath.Join(dir, fmt.Sprintf("%d.fit", aaActivityID)) {
		t.Fatalf("data = %v", data)
	}
}

func TestDownloadActivityFileNeedsSetup(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "download_activity_file", map[string]any{"activity_id": aaActivityID}))
	if data["status"] != "needs_setup" || data["suggested_default"] == nil {
		t.Fatalf("data = %v", data)
	}
}

func TestSetFitDownloadDirPersists(t *testing.T) {
	f := newFakeGarmin(t)
	st := newTestStore(t)
	seedAccount(t, st, "me@x.com", testJWT(nil))
	cs := newSessionWithStore(t, f, nil, st)
	target := filepath.Join(t.TempDir(), "downloads")

	data := aaDownloaded(t, mustCallTool(t, cs, "set_fit_download_dir", map[string]any{"path": target}))
	if data["download_dir"] != target {
		t.Fatalf("data = %v", data)
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Fatalf("target not created: %v", err)
	}
	settings, err := st.LoadSettings("me@x.com")
	if err != nil {
		t.Fatal(err)
	}
	if settings.FitDownloadDir != target {
		t.Fatalf("settings = %+v", settings)
	}
}

func TestDownloadActivityFileUsesSavedSettings(t *testing.T) {
	f := newFakeGarmin(t)
	st := newTestStore(t)
	seedAccount(t, st, "me@x.com", testJWT(nil))
	dir := t.TempDir()
	if err := st.SaveSettings("me@x.com", accounts.Settings{Domain: "garmin.com", FitDownloadDir: dir}); err != nil {
		t.Fatal(err)
	}
	f.on(fmt.Sprintf("GET /download-service/files/activity/%d", aaActivityID), rawBody([]byte("fitdata")))
	cs := newSessionWithStore(t, f, nil, st)

	data := aaDownloaded(t, mustCallTool(t, cs, "download_activity_file", map[string]any{"activity_id": aaActivityID}))
	if data["file_path"] != filepath.Join(dir, fmt.Sprintf("%d.fit", aaActivityID)) {
		t.Fatalf("data = %v", data)
	}
}

func TestDownloadActivityFileUsesEnvDir(t *testing.T) {
	f := newFakeGarmin(t)
	dir := t.TempDir()
	t.Setenv("GARMIN_FIT_DOWNLOAD_DIR", dir)
	f.on(fmt.Sprintf("GET /download-service/files/activity/%d", aaActivityID), rawBody([]byte("fitdata")))
	cs := newSession(t, f, nil)

	data := aaDownloaded(t, mustCallTool(t, cs, "download_activity_file", map[string]any{"activity_id": aaActivityID}))
	if data["file_path"] != filepath.Join(dir, fmt.Sprintf("%d.fit", aaActivityID)) {
		t.Fatalf("data = %v", data)
	}
}

func TestDownloadActivityFileFitSavesFile(t *testing.T) {
	f := newFakeGarmin(t)
	dir := t.TempDir()
	fitBytes := []byte("\x0e\x10FITDATA")
	zipBytes := aaZip(t, "activity.fit", fitBytes)
	f.on(fmt.Sprintf("GET /download-service/files/activity/%d", aaActivityID), rawBody(zipBytes))
	cs := newSession(t, f, nil)

	data := aaDownloaded(t, mustCallTool(t, cs, "download_activity_file", map[string]any{
		"activity_id": aaActivityID, "output_dir": dir,
	}))
	expected := filepath.Join(dir, fmt.Sprintf("%d.fit", aaActivityID))
	got, err := os.ReadFile(expected)
	if err != nil || !bytes.Equal(got, fitBytes) {
		t.Fatalf("file = %q, %v", got, err)
	}
	if data["format"] != "fit" || aaNum(t, data["size_bytes"]) != float64(len(fitBytes)) {
		t.Fatalf("data = %v", data)
	}
}

func TestDownloadActivityFileOtherFormatsWriteRawBytes(t *testing.T) {
	for _, tc := range []struct{ format, path string }{
		{"gpx", "/download-service/export/gpx/activity"},
		{"tcx", "/download-service/export/tcx/activity"},
		{"csv", "/download-service/export/csv/activity"},
	} {
		f := newFakeGarmin(t)
		dir := t.TempDir()
		payload := []byte("<" + tc.format + ">data</" + tc.format + ">")
		f.on(fmt.Sprintf("GET %s/%d", tc.path, aaActivityID), rawBody(payload))
		cs := newSession(t, f, nil)
		data := aaDownloaded(t, mustCallTool(t, cs, "download_activity_file", map[string]any{
			"activity_id": aaActivityID, "format": tc.format, "output_dir": dir,
		}))
		expected := filepath.Join(dir, fmt.Sprintf("%d.%s", aaActivityID, tc.format))
		got, err := os.ReadFile(expected)
		if err != nil || !bytes.Equal(got, payload) || data["format"] != tc.format {
			t.Fatalf("%s: %q %v %v", tc.format, got, err, data)
		}
	}
}

func TestDownloadActivityFileInvalidFormat(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "download_activity_file", map[string]any{
		"activity_id": aaActivityID, "format": "pdf", "output_dir": t.TempDir(),
	}))
	if !strings.Contains(fmt.Sprint(data["error"]), "Invalid format") {
		t.Fatalf("data = %v", data)
	}
	valid := aaList(t, data["valid_formats"], "valid_formats")
	if fmt.Sprint(valid) != "[fit gpx tcx csv]" {
		t.Fatalf("valid = %v", valid)
	}
	if len(f.callsMatching("GET", fmt.Sprintf("/download-service/files/activity/%d", aaActivityID))) != 0 {
		t.Fatal("downloaded")
	}
}

func TestDownloadActivityFileNoneResponse(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(fmt.Sprintf("GET /download-service/files/activity/%d", aaActivityID), nil)
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "download_activity_file", map[string]any{
		"activity_id": aaActivityID, "output_dir": t.TempDir(),
	})
	if !strings.Contains(text, "No fit data returned") {
		t.Fatalf("text = %q", text)
	}
}

func TestDownloadActivityFileFitExtractionFailure(t *testing.T) {
	f := newFakeGarmin(t)
	dir := t.TempDir()
	f.on(fmt.Sprintf("GET /download-service/files/activity/%d", aaActivityID), rawBody(aaZip(t, "notes.txt", []byte("no fit here"))))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "download_activity_file", map[string]any{
		"activity_id": aaActivityID, "output_dir": dir,
	}))
	if data["error"] == nil || aaMap(t, data["debug"], "debug")["first_16_bytes_hex"] == nil {
		t.Fatalf("data = %v", data)
	}
	if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("%d.fit", aaActivityID))); err == nil {
		t.Fatal("file written")
	}
}

// --- enhanced fields --------------------------------------------------------

func TestGetActivityFitDataRecordsReadEnhancedAltitudeAndSpeed(t *testing.T) {
	f := newFakeGarmin(t)
	aaServeFIT(t, f, aaActivityID, aaFIT(t,
		aaMsg{num: mesgnum.Record, fields: []aaField{
			{fieldnum.RecordTimestamp, aaTime(t, "2026-10-07 20:15:00")},
			{fieldnum.RecordPower, uint16(228)},
			{fieldnum.RecordEnhancedAltitude, uint32((312.4 + 500) * 5)},
			{fieldnum.RecordEnhancedSpeed, uint32(2.1 * 1000)},
			{fieldnum.RecordDistance, uint32(7412.5 * 100)},
		}},
		aaMsg{num: mesgnum.Record, fields: []aaField{
			{fieldnum.RecordTimestamp, aaTime(t, "2026-10-07 20:15:01")},
			{fieldnum.RecordAltitude, uint16(4064)},
			{fieldnum.RecordSpeed, uint16(2.0 * 1000)},
		}},
	))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{
		"activity_id": aaActivityID, "include_records": true,
	}))
	records := aaList(t, data["records"], "records")
	first := aaMap(t, records[0], "record")
	second := aaMap(t, records[1], "record")
	if math.Abs(aaNum(t, first["altitude_m"])-312.4) > 0.2 || math.Abs(aaNum(t, first["speed_mps"])-2.1) > 0.001 ||
		math.Abs(aaNum(t, first["distance_m"])-7412.5) > 0.01 {
		t.Fatalf("first = %v", first)
	}
	if math.Abs(aaNum(t, second["altitude_m"])-312.9) > 0.2 || math.Abs(aaNum(t, second["speed_mps"])-2.0) > 0.001 {
		t.Fatalf("second = %v", second)
	}
}

func TestGetActivityFitDataSessionReadsEnhancedSpeed(t *testing.T) {
	f := newFakeGarmin(t)
	aaServeFIT(t, f, aaActivityID, aaFIT(t, aaMsg{num: mesgnum.Session, fields: []aaField{
		{fieldnum.SessionEnhancedAvgSpeed, uint32(5270)},
		{fieldnum.SessionEnhancedMaxSpeed, uint32(15100)},
	}}))
	cs := newSession(t, f, nil)
	session := aaMap(t, aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))["session"], "session")
	if math.Abs(aaNum(t, session["avg_speed_mps"])-5.27) > 0.001 || math.Abs(aaNum(t, session["max_speed_mps"])-15.1) > 0.001 {
		t.Fatalf("session = %v", session)
	}
}

// --- grade derivation -------------------------------------------------------

func aaRideFIT(t *testing.T, gradeField, altitude, distance bool) []byte {
	t.Helper()
	start := time.Date(2024, 3, 2, 14, 0, 0, 0, time.UTC)
	gearData := uint32(53<<24 | 2<<16 | 17<<8 | 4)
	msgs := []aaMsg{}
	dist := 0.0
	alt := 0.0
	for i := 0; i < 520; i++ {
		onClimb := i >= 60 && i < 460
		speed := 8.0
		grade := 0.0
		if onClimb {
			speed = 5.0
			grade = 6.0
		}
		dist += speed
		alt += speed * grade / 100
		fields := []aaField{
			{fieldnum.RecordTimestamp, aaFITTime(t, start.Add(time.Duration(i)*time.Second))},
			{fieldnum.RecordPower, uint16(180)},
			{fieldnum.RecordCadence, uint8(90)},
			{fieldnum.RecordHeartRate, uint8(140)},
			{fieldnum.RecordEnhancedSpeed, uint32(speed * 1000)},
		}
		if onClimb {
			fields[1].value = uint16(280)
			fields[2].value = uint8(75)
			fields[3].value = uint8(160)
		}
		if altitude {
			fields = append(fields, aaField{fieldnum.RecordEnhancedAltitude, uint32((100 + alt + 500) * 5)})
		}
		if distance {
			fields = append(fields, aaField{fieldnum.RecordDistance, uint32(dist * 100)})
		}
		if gradeField {
			fields = append(fields, aaField{fieldnum.RecordGrade, int16(grade * 100)})
		}
		msgs = append(msgs, aaMsg{num: mesgnum.Record, fields: fields})
		if i == 300 || i == 500 {
			msgs = append(msgs, aaMsg{num: mesgnum.Event, fields: []aaField{
				{fieldnum.EventEvent, typedef.EventRearGearChange.Byte()},
				{fieldnum.EventData, gearData},
				{fieldnum.EventTimestamp, aaFITTime(t, start.Add(time.Duration(i)*time.Second))},
			}})
		}
	}
	msgs = append(msgs, aaMsg{num: mesgnum.Session, fields: []aaField{
		{fieldnum.SessionSport, typedef.SportCycling.Byte()},
		{fieldnum.SessionStartTime, aaFITTime(t, start)},
		{fieldnum.SessionTotalElapsedTime, uint32(520 * 1000)},
	}})
	return aaFIT(t, msgs...)
}

func TestFitDataDerivesGradeWhenDeviceWritesNone(t *testing.T) {
	f := newFakeGarmin(t)
	aaServeFIT(t, f, aaActivityID, aaRideFIT(t, false, true, true))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	if data["grade_source"] != "derived" {
		t.Fatalf("grade_source = %v", data["grade_source"])
	}
	climbs := aaList(t, data["climbs"], "climbs")
	if len(climbs) != 1 {
		t.Fatalf("climbs = %v", climbs)
	}
	climb := aaMap(t, climbs[0], "climb")
	if math.Abs(aaNum(t, climb["avg_grade_pct"])-6) > 0.5 || math.Abs(aaNum(t, climb["elevation_gain_m"])-120) > 10 {
		t.Fatalf("climb = %v", climb)
	}
	bins := aaMap(t, aaMap(t, data["session"], "session")["grade_analysis"], "bins")
	climbing := 0.0
	if bins["gentle"] != nil {
		climbing += aaNum(t, aaMap(t, bins["gentle"], "gentle")["time_s"])
	}
	if bins["moderate"] != nil {
		climbing += aaNum(t, aaMap(t, bins["moderate"], "moderate")["time_s"])
	}
	if climbing <= 300 {
		t.Fatalf("bins = %v", bins)
	}
	shifts := aaList(t, data["shifts"], "shifts")
	if math.Abs(aaNum(t, aaMap(t, shifts[0], "shift")["grade_at_shift_pct"])-6) > 0.5 {
		t.Fatalf("shifts = %v", shifts)
	}
	if aaNum(t, aaMap(t, shifts[1], "shift")["grade_at_shift_pct"]) > 0.5 {
		t.Fatalf("shifts = %v", shifts)
	}
	byTerrain := aaMap(t, aaMap(t, data["shift_summary"], "summary")["by_terrain"], "by_terrain")
	if byTerrain["climbing"] == nil || byTerrain["flat"] == nil {
		t.Fatalf("by_terrain = %v", byTerrain)
	}
}

func TestFitDataKeepsDeviceGrade(t *testing.T) {
	f := newFakeGarmin(t)
	aaServeFIT(t, f, aaActivityID, aaRideFIT(t, true, true, true))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	if data["grade_source"] != "device" {
		t.Fatalf("grade_source = %v", data["grade_source"])
	}
	climb := aaMap(t, aaList(t, data["climbs"], "climbs")[0], "climb")
	if aaNum(t, climb["avg_grade_pct"]) != 6 || aaNum(t, climb["max_grade_pct"]) != 6 {
		t.Fatalf("climb = %v", climb)
	}
	if aaNum(t, aaMap(t, aaMap(t, aaMap(t, data["session"], "session")["grade_analysis"], "bins")["moderate"], "moderate")["time_s"]) != 400 {
		t.Fatalf("session = %v", data["session"])
	}
}

func TestFitDataNoGradeWithoutAltitudeOrDistance(t *testing.T) {
	for _, missing := range []string{"altitude", "distance"} {
		f := newFakeGarmin(t)
		aaServeFIT(t, f, aaActivityID, aaRideFIT(t, false, missing != "altitude", missing != "distance"))
		cs := newSession(t, f, nil)
		data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
		if _, ok := data["grade_source"]; ok {
			t.Fatalf("%s: grade_source = %v", missing, data["grade_source"])
		}
		if _, ok := data["climbs"]; ok {
			t.Fatalf("%s: climbs = %v", missing, data["climbs"])
		}
		if _, ok := aaMap(t, data["session"], "session")["grade_analysis"]; ok {
			t.Fatalf("%s: grade_analysis present", missing)
		}
		for _, item := range aaList(t, data["shifts"], "shifts") {
			if _, ok := aaMap(t, item, "shift")["grade_at_shift_pct"]; ok {
				t.Fatalf("%s: shift grade present", missing)
			}
		}
		if _, ok := aaMap(t, data["shift_summary"], "summary")["by_terrain"]; ok {
			t.Fatalf("%s: by_terrain present", missing)
		}
	}
}

// --- wrapped lap distance ---------------------------------------------------

func TestGetActivityFitDataWrappedLapDistanceIsNulled(t *testing.T) {
	f := newFakeGarmin(t)
	aaServeFIT(t, f, aaActivityID, aaFIT(t,
		aaMsg{num: mesgnum.Lap, fields: []aaField{
			{fieldnum.LapTotalElapsedTime, uint32(0)},
			{fieldnum.LapTotalDistance, uint32(20)},
		}},
		aaMsg{num: mesgnum.Lap, fields: []aaField{
			{fieldnum.LapTotalElapsedTime, uint32(0)},
			{fieldnum.LapTotalDistance, uint32(4294967292)},
			{fieldnum.LapAvgHeartRate, uint8(140)},
		}},
	))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	laps := aaList(t, data["laps"], "laps")
	first := aaMap(t, laps[0], "lap 0")
	second := aaMap(t, laps[1], "lap 1")
	if math.Abs(aaNum(t, first["total_distance_m"])-0.2) > 0.001 {
		t.Fatalf("first = %v", first)
	}
	if _, ok := second["total_distance_m"]; ok || second["distance_invalid"] != true || aaNum(t, second["avg_heart_rate_bpm"]) != 140 {
		t.Fatalf("second = %v", second)
	}
}

// --- W/kg -------------------------------------------------------------------

func TestGetActivityFitDataUsesLatestWeighInBeforeRide(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /weight-service/weight/dateRange", map[string]any{
		"dateWeightList": []any{
			map[string]any{"calendarDate": "2024-02-10", "weight": 72000},
			map[string]any{"calendarDate": "2024-02-27", "weight": 70000},
			map[string]any{"calendarDate": "2024-03-05", "weight": 68000},
		},
	})
	aaServeFIT(t, f, aaActivityID, aaFIT(t, aaMsg{num: mesgnum.Session, fields: []aaField{
		{fieldnum.SessionSport, typedef.SportCycling.Byte()},
		{fieldnum.SessionStartTime, aaTime(t, "2024-03-02 14:00:00")},
		{fieldnum.SessionAvgPower, uint16(210)},
	}}))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	if aaNum(t, data["rider_weight_kg"]) != 70 || data["rider_weight_date"] != "2024-02-27" {
		t.Fatalf("data = %v", data)
	}
	if aaNum(t, aaMap(t, data["session"], "session")["avg_w_per_kg"]) != 3 {
		t.Fatalf("session = %v", data["session"])
	}
	calls := f.callsMatching("GET", "/weight-service/weight/dateRange")
	if len(calls) != 1 || calls[0].Query.Get("startDate") != "2023-12-03" || calls[0].Query.Get("endDate") != "2024-03-02" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestGetActivityFitDataWithoutWeighInsOmitsWPerKg(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /weight-service/weight/dateRange", map[string]any{"dateWeightList": []any{}})
	aaServeFIT(t, f, aaActivityID, aaFIT(t, aaMsg{num: mesgnum.Session, fields: []aaField{
		{fieldnum.SessionSport, typedef.SportCycling.Byte()},
		{fieldnum.SessionStartTime, aaTime(t, "2024-03-02 14:00:00")},
		{fieldnum.SessionAvgPower, uint16(210)},
	}}))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	if _, ok := data["rider_weight_kg"]; ok {
		t.Fatalf("data = %v", data)
	}
	if _, ok := aaMap(t, data["session"], "session")["avg_w_per_kg"]; ok {
		t.Fatalf("session = %v", data["session"])
	}
}

func TestGetActivityFitDataAddsClimbWPerKg(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /weight-service/weight/dateRange", map[string]any{
		"dateWeightList": []any{
			map[string]any{"calendarDate": "2024-02-27", "weight": 70000},
		},
	})
	aaServeFIT(t, f, aaActivityID, aaRideFIT(t, false, true, true))
	cs := newSession(t, f, nil)
	data := aaDownloaded(t, mustCallTool(t, cs, "get_activity_fit_data", map[string]any{"activity_id": aaActivityID}))
	climbs := aaList(t, data["climbs"], "climbs")
	if len(climbs) == 0 {
		t.Fatal("no climbs")
	}
	climb := aaMap(t, climbs[0], "climb")
	if aaNum(t, climb["avg_power_w"]) != 280 || aaNum(t, climb["avg_w_per_kg"]) != 4 {
		t.Fatalf("climb = %v", climb)
	}
}

// --- small helpers ----------------------------------------------------------

func mustCallTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	text, isErr := callTool(t, cs, name, args)
	if isErr {
		t.Fatalf("tool %s error: %s", name, text)
	}
	return text
}

func aaZip(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entry, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
