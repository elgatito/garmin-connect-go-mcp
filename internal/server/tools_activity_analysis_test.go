package server

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/muktihari/fit/encoder"
	"github.com/muktihari/fit/profile"
	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/factory"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/profile/untyped/fieldnum"
	"github.com/muktihari/fit/profile/untyped/mesgnum"
	"github.com/muktihari/fit/proto"
)

const fitActivityID = 22041393449

const fitOriginalRoute = "GET /download-service/files/activity/22041393449"

// fitFile encodes messages with the library's encoder, so tool tests run the real decode path
// (the Python tests patch fitparse with fake messages instead).
func fitFile(t *testing.T, msgs ...proto.Message) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := encoder.New(&buf).Encode(&proto.FIT{Messages: msgs}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func fitMesg(num typedef.MesgNum, fields ...proto.Field) proto.Message {
	return proto.Message{Num: num, Fields: fields}
}

func fitF(num typedef.MesgNum, field byte, v any) proto.Field {
	return factory.CreateField(num, field).WithValue(v)
}

// fitUnknownField is a field the profile does not define.
func fitUnknownField(num byte, bt basetype.BaseType, array bool, v any) proto.Field {
	return proto.Field{FieldBase: &proto.FieldBase{Name: factory.NameUnknown, Num: num, BaseType: bt,
		Type: profile.ProfileTypeFromBaseType(bt), Array: array, Scale: 1}, Value: proto.Any(v)}
}

func callFitMessages(t *testing.T, fit []byte, args map[string]any) (map[string]any, *fakeGarmin) {
	t.Helper()
	f := newFakeGarmin(t)
	f.on(fitOriginalRoute, rawBody(fit))
	if args == nil {
		args = map[string]any{}
	}
	args["activity_id"] = fitActivityID
	data, _ := decode(t, callOK(t, newSession(t, f, nil), "get_activity_fit_messages", args)).(map[string]any)
	return data, f
}

func messageTypes(data map[string]any) []any {
	var out []any
	msgs, _ := data["messages"].([]any)
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		out = append(out, mm["type"])
	}
	return out
}

func TestGetActivityFitMessagesPreservesAllMessageTypesAndFields(t *testing.T) {
	fit := fitFile(t,
		fitMesg(mesgnum.ExerciseTitle,
			fitF(mesgnum.ExerciseTitle, fieldnum.ExerciseTitleMessageIndex, typedef.MessageIndex(0)),
			fitF(mesgnum.ExerciseTitle, fieldnum.ExerciseTitleExerciseCategory, typedef.ExerciseCategoryBenchPress),
			fitF(mesgnum.ExerciseTitle, fieldnum.ExerciseTitleExerciseName, uint16(typedef.BenchPressExerciseNameBarbellBenchPress)),
		),
		fitMesg(mesgnum.Set,
			fitF(mesgnum.Set, fieldnum.SetRepetitions, uint16(8)),
			fitF(mesgnum.Set, fieldnum.SetWeight, uint16(980)), // 61.25 kg at scale 16
			fitF(mesgnum.Set, fieldnum.SetCategory, []typedef.ExerciseCategory{typedef.ExerciseCategoryDeadlift, typedef.ExerciseCategoryPullUp}),
			fitF(mesgnum.Set, fieldnum.SetCategorySubtype, []uint16{0, 0xFFFF}),
			fitF(mesgnum.Set, fieldnum.SetSetType, typedef.SetTypeActive),
			fitUnknownField(99, basetype.Byte, true, []byte{1, 2}),
		),
	)
	data, f := callFitMessages(t, fit, nil)

	if n := len(f.requests(fitOriginalRoute)); n != 1 {
		t.Fatalf("downloads = %d", n)
	}
	if data["source"] != "garmin_original_fit" || data["activity_id"] != float64(fitActivityID) {
		t.Errorf("source/id: %v %v", data["source"], data["activity_id"])
	}
	if got := data["message_counts"]; !reflect.DeepEqual(got, map[string]any{"exercise_title": 1.0, "set": 1.0}) {
		t.Errorf("message_counts = %v", got)
	}
	if got := messageTypes(data); !reflect.DeepEqual(got, []any{"exercise_title", "set"}) {
		t.Errorf("types = %v", got)
	}
	msgs, _ := data["messages"].([]any)
	first, _ := msgs[0].(map[string]any)
	if first["global_message_number"] != 264.0 {
		t.Errorf("global_message_number = %v", first["global_message_number"])
	}
	second, _ := msgs[1].(map[string]any)
	want := decode(t, `[
		{"name": "repetitions", "value": 8, "definition_number": 3, "base_type": "uint16", "profile_type": "uint16", "field_type": "field", "raw_value": 8},
		{"name": "weight", "value": 61.25, "units": "kg", "definition_number": 4, "base_type": "uint16", "profile_type": "uint16", "field_type": "field", "raw_value": 980},
		{"name": "category", "value": [8, 21], "definition_number": 7, "base_type": "uint16", "profile_type": "exercise_category", "field_type": "field", "value_names": ["deadlift", "pull_up"], "raw_value": [8, 21]},
		{"name": "category_subtype", "value": [0, null], "definition_number": 8, "base_type": "uint16", "profile_type": "uint16", "field_type": "field", "value_names": ["barbell_deadlift", null], "raw_value": [0, null]},
		{"name": "set_type", "value": "active", "definition_number": 5, "base_type": "uint8", "profile_type": "set_type", "field_type": "field", "value_name": "active", "raw_value": 1},
		{"name": "unknown_99", "value": [1, 2], "definition_number": 99, "base_type": "byte", "profile_type": "byte", "field_type": "field", "raw_value": [1, 2]}
	]`)
	diffJSON(t, "set.fields", second["fields"], want)
}

func TestGetActivityFitMessagesResolvesStandardContextualEnums(t *testing.T) {
	fit := fitFile(t,
		fitMesg(mesgnum.Set,
			fitF(mesgnum.Set, fieldnum.SetCategory, []typedef.ExerciseCategory{typedef.ExerciseCategorySquat}),
			fitF(mesgnum.Set, fieldnum.SetCategorySubtype, []uint16{6}),
		),
		fitMesg(mesgnum.ExerciseTitle,
			fitF(mesgnum.ExerciseTitle, fieldnum.ExerciseTitleExerciseCategory, typedef.ExerciseCategorySquat),
			fitF(mesgnum.ExerciseTitle, fieldnum.ExerciseTitleExerciseName, uint16(6)),
		),
	)
	data, _ := callFitMessages(t, fit, nil)
	msgs, _ := data["messages"].([]any)
	byName := make([]map[string]map[string]any, len(msgs))
	for i, m := range msgs {
		byName[i] = map[string]map[string]any{}
		fields, _ := m.(map[string]any)["fields"].([]any)
		for _, fl := range fields {
			fm, _ := fl.(map[string]any)
			name, _ := fm["name"].(string)
			byName[i][name] = fm
		}
	}
	if got := byName[0]["category"]["value_name"]; got != "squat" {
		t.Errorf("category = %v", got)
	}
	if got := byName[0]["category_subtype"]["value_name"]; got != "barbell_back_squat" {
		t.Errorf("category_subtype = %v", got)
	}
	if got := byName[1]["exercise_name"]["value_name"]; got != "barbell_back_squat" {
		t.Errorf("exercise_name = %v", got)
	}
	if got := byName[1]["exercise_name"]["raw_value"]; got != 6.0 {
		t.Errorf("exercise_name raw = %v", got)
	}
}

func hrRecord(bpm uint8) proto.Message {
	return fitMesg(mesgnum.Record, fitF(mesgnum.Record, fieldnum.RecordHeartRate, bpm))
}

func repsSet(n uint16) proto.Message {
	return fitMesg(mesgnum.Set, fitF(mesgnum.Set, fieldnum.SetRepetitions, n))
}

func TestGetActivityFitMessagesOmitsRecordsByDefault(t *testing.T) {
	data, _ := callFitMessages(t, fitFile(t, hrRecord(140), repsSet(10), hrRecord(141)), nil)
	if got := data["message_counts"]; !reflect.DeepEqual(got, map[string]any{"record": 2.0, "set": 1.0}) {
		t.Errorf("message_counts = %v", got)
	}
	if got := data["returned_message_counts"]; !reflect.DeepEqual(got, map[string]any{"set": 1.0}) {
		t.Errorf("returned_message_counts = %v", got)
	}
	if got := messageTypes(data); !reflect.DeepEqual(got, []any{"set"}) {
		t.Errorf("types = %v", got)
	}
	rs, _ := data["record_stream"].(map[string]any)
	if rs["included"] != false || rs["total_count"] != 2.0 || rs["returned_count"] != 0.0 ||
		rs["hint"] != "Set include_records=true and use message pagination to retrieve records." {
		t.Errorf("record_stream = %v", rs)
	}
}

func TestGetActivityFitMessagesPaginatesRecords(t *testing.T) {
	data, _ := callFitMessages(t, fitFile(t, hrRecord(140), hrRecord(141), hrRecord(142)),
		map[string]any{"include_records": true, "message_offset": 1, "message_limit": 1})
	if got := data["record_stream"]; !reflect.DeepEqual(got, map[string]any{"included": true, "total_count": 3.0, "returned_count": 1.0}) {
		t.Errorf("record_stream = %v", got)
	}
	wantPage := map[string]any{"total_eligible_count": 3.0, "returned_count": 1.0, "offset": 1.0, "limit": 1.0, "next_offset": 2.0}
	if got := data["pagination"]; !reflect.DeepEqual(got, wantPage) {
		t.Errorf("pagination = %v", got)
	}
	msgs, _ := data["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages = %v", msgs)
	}
	m, _ := msgs[0].(map[string]any)
	fields, _ := m["fields"].([]any)
	f0, _ := fields[0].(map[string]any)
	if m["type_index"] != 1.0 || f0["value"] != 141.0 {
		t.Errorf("message = %v", m)
	}
}

func TestGetActivityFitMessagesFiltersReturnedTypesButKeepsInventory(t *testing.T) {
	fit := fitFile(t, fitMesg(mesgnum.Session, fitF(mesgnum.Session, fieldnum.SessionSport, typedef.SportTraining)), repsSet(5))
	data, _ := callFitMessages(t, fit, map[string]any{"message_types": []any{"SET"}})
	if got := data["message_counts"]; !reflect.DeepEqual(got, map[string]any{"session": 1.0, "set": 1.0}) {
		t.Errorf("message_counts = %v", got)
	}
	if got := data["returned_message_counts"]; !reflect.DeepEqual(got, map[string]any{"set": 1.0}) {
		t.Errorf("returned_message_counts = %v", got)
	}
	if got := messageTypes(data); !reflect.DeepEqual(got, []any{"set"}) {
		t.Errorf("types = %v", got)
	}
}

func TestGetActivityFitMessagesRejectsUnboundedMessageLimit(t *testing.T) {
	fit := fitFile(t, repsSet(1))
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"include_records": true, "message_limit": 5001}, "message_limit must be between 1 and 5000"},
		{map[string]any{"message_limit": 0}, "message_limit must be between 1 and 5000"},
		{map[string]any{"message_offset": -1}, "message_offset must be at least 0"},
	} {
		data, f := callFitMessages(t, fit, c.args)
		if data["error"] != c.want || data["activity_id"] != float64(fitActivityID) {
			t.Errorf("%v: %v", c.args, data)
		}
		dbg, _ := data["debug"].(map[string]any)
		if dbg["download_size_bytes"] != float64(len(fit)) || dbg["first_16_bytes_hex"] == nil || dbg["hint"] == nil {
			t.Errorf("debug = %v", dbg)
		}
		if len(f.requests(fitOriginalRoute)) != 1 { // Python validates after downloading
			t.Error("expected one download")
		}
	}
}

func TestGetActivityFitMessagesOmitsOtherHighFrequencyTypesByDefault(t *testing.T) {
	msgs := []proto.Message{fitMesg(mesgnum.Session, fitF(mesgnum.Session, fieldnum.SessionSport, typedef.SportTraining))}
	for i := range 101 {
		msgs = append(msgs, fitMesg(233, fitUnknownField(0, basetype.Uint8, false, uint8(i))))
	}
	data, _ := callFitMessages(t, fitFile(t, msgs...), nil)
	if got := messageTypes(data); !reflect.DeepEqual(got, []any{"session"}) {
		t.Errorf("types = %v", got)
	}
	counts, _ := data["message_counts"].(map[string]any)
	omitted, _ := data["omitted_high_frequency_message_types"].(map[string]any)
	want := map[string]any{"count": 101.0, "hint": "Request message_types=['unknown_233'] and follow pagination."}
	if counts["unknown_233"] != 101.0 || !reflect.DeepEqual(omitted["unknown_233"], want) {
		t.Errorf("counts %v omitted %v", counts, omitted)
	}
	// Explicitly requested, the omitted type is paginated like any other.
	data, _ = callFitMessages(t, fitFile(t, msgs...), map[string]any{"message_types": []any{"unknown_233"}, "message_limit": 100})
	page, _ := data["pagination"].(map[string]any)
	if page["returned_count"] != 100.0 || page["next_offset"] != 100.0 || data["omitted_high_frequency_message_types"] != nil {
		t.Errorf("filtered: %v", data)
	}
}

func TestGetActivityFitMessagesReadsZippedOriginalWithStringID(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(fitOriginalRoute, rawBody(zipOf(t, [2]string{"22041393449_ACTIVITY.fit", string(readTestFIT(t, "Activity.fit"))})))
	text := callOK(t, newSession(t, f, nil), "get_activity_fit_messages",
		map[string]any{"activity_id": "22041393449", "message_types": []any{"session"}})
	data, _ := decode(t, text).(map[string]any)
	if data["activity_id"] != float64(fitActivityID) || data["fit_size_bytes"] != 94096.0 || !reflect.DeepEqual(messageTypes(data), []any{"session"}) {
		t.Errorf("data = %.300s", text)
	}
	// Inventory keys keep the file's order, like the Python dict.
	if !strings.Contains(text, `"message_counts": {
    "file_id": 1,
    "device_info": 1,`) {
		t.Errorf("order: %.400s", text)
	}
}

func TestGetActivityFitMessagesDownloadProblems(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(fitOriginalRoute, rawBody(nil))
	cs := newSession(t, f, nil)
	if text, isErr := callTool(t, cs, "get_activity_fit_messages", map[string]any{"activity_id": fitActivityID}); isErr || text != "No FIT data returned for activity 22041393449" {
		t.Errorf("empty: %v %q", isErr, text)
	}

	f.on(fitOriginalRoute, rawBody("<html>nope</html>"))
	data, _ := decode(t, callOK(t, cs, "get_activity_fit_messages", map[string]any{"activity_id": fitActivityID})).(map[string]any)
	dbg, _ := data["debug"].(map[string]any)
	if data["error"] == nil || dbg["first_16_bytes_hex"] != "3c68746d6c3e6e6f70653c2f68746d6c" || dbg["download_size_bytes"] != 17.0 ||
		dbg["hint"] != "1f8b = gzip, 504b = ZIP, 0e10/0c10 = raw FIT, 3c or 7b = HTML/JSON error from Garmin" {
		t.Errorf("parse error: %v", data)
	}

	f.on(fitOriginalRoute, status(500, map[string]any{"message": "boom"}))
	text, isErr := callTool(t, cs, "get_activity_fit_messages", map[string]any{"activity_id": fitActivityID})
	if !isErr || !strings.HasPrefix(text, "Error downloading FIT data for activity 22041393449: Download error: API Error 500") {
		t.Errorf("download error: %v %q", isErr, text)
	}

	before := len(f.requests(""))
	text, isErr = callTool(t, cs, "get_activity_fit_messages", map[string]any{"activity_id": "abc"})
	if !isErr || text != "Error downloading FIT data for activity abc: invalid literal for int() with base 10: 'abc'" {
		t.Errorf("bad id: %v %q", isErr, text)
	}
	if len(f.requests("")) != before {
		t.Error("bad id must not download")
	}
}
