package server

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
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

	"github.com/elgatito/garmin-connect-go-mcp/internal/accounts"
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

// ── download_activity_file / set_fit_download_dir ─────────────────────────

func TestResolveDownloadDirPrefersOutputDirArg(t *testing.T) {
	dir := t.TempDir()
	st := newStore(t)
	if err := st.SaveSettings(testEmail, accounts.Settings{FitDownloadDir: filepath.Join(dir, "cfg")}); err != nil {
		t.Fatal(err)
	}
	s := &Session{Email: testEmail, Store: st, Getenv: envOf(map[string]string{"GARMIN_FIT_DOWNLOAD_DIR": filepath.Join(dir, "env")})}
	if got := resolveDownloadDir(s, filepath.Join(dir, "arg")); got != filepath.Join(dir, "arg") {
		t.Errorf("got %q", got)
	}
}

func TestResolveDownloadDirUsesEnvVar(t *testing.T) {
	dir := t.TempDir()
	s := &Session{Email: testEmail, Store: newStore(t), Getenv: envOf(map[string]string{"GARMIN_FIT_DOWNLOAD_DIR": filepath.Join(dir, "env")})}
	if got := resolveDownloadDir(s, ""); got != filepath.Join(dir, "env") {
		t.Errorf("got %q", got)
	}
}

// Python reads a global config file; here the saved directory is the account's settings.json fit_download_dir.
func TestResolveDownloadDirUsesConfigFile(t *testing.T) {
	dir := t.TempDir()
	st := newStore(t)
	if err := st.SaveSettings(testEmail, accounts.Settings{FitDownloadDir: filepath.Join(dir, "saved")}); err != nil {
		t.Fatal(err)
	}
	s := &Session{Email: testEmail, Store: st, Getenv: envOf(nil)}
	if got := resolveDownloadDir(s, ""); got != filepath.Join(dir, "saved") {
		t.Errorf("got %q", got)
	}
}

func TestResolveDownloadDirReturnsNoneWhenUnconfigured(t *testing.T) {
	s := &Session{Email: testEmail, Store: newStore(t), Getenv: envOf(nil)}
	if got := resolveDownloadDir(s, ""); got != "" {
		t.Errorf("got %q", got)
	}
}

func writeSettingsFile(t *testing.T, st accounts.Store, content string) {
	t.Helper()
	dir, err := st.Dir(testEmail)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Python: a missing config file reads as {}. Here: no settings.json reads as defaults with no download dir.
func TestReadFitConfigMissingReturnsEmpty(t *testing.T) {
	st := newStore(t)
	got, err := st.LoadSettings(testEmail)
	if err != nil || got.FitDownloadDir != "" {
		t.Errorf("got %+v %v", got, err)
	}
}

func TestResolveDownloadDirConfigMissingKeyReturnsNone(t *testing.T) {
	st := newStore(t)
	writeSettingsFile(t, st, `{"other": "value"}`)
	s := &Session{Email: testEmail, Store: st, Getenv: envOf(nil)}
	if got := resolveDownloadDir(s, ""); got != "" {
		t.Errorf("got %q", got)
	}
}

// Python: a non-dict config reads as {}. Here: an unreadable settings.json means no download dir, and
// set_fit_download_dir replaces it.
func TestReadFitConfigNonDictReturnsEmpty(t *testing.T) {
	st := newStore(t)
	writeSettingsFile(t, st, "[1, 2, 3]")
	s := &Session{Email: testEmail, Store: st, Getenv: envOf(nil)}
	if got := resolveDownloadDir(s, ""); got != "" {
		t.Errorf("got %q", got)
	}
	target := filepath.Join(t.TempDir(), "dl")
	callOK(t, newSessionWithStore(t, newFakeGarmin(t), st, nil), "set_fit_download_dir", map[string]any{"path": target})
	if got, err := st.LoadSettings(testEmail); err != nil || got.FitDownloadDir != target {
		t.Errorf("after set: %+v %v", got, err)
	}
}

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestSetFitDownloadDirPersists(t *testing.T) {
	st := newStore(t)
	if err := st.SaveSettings(testEmail, accounts.Settings{Domain: "garmin.cn"}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "downloads")
	data, _ := decode(t, callOK(t, newSessionWithStore(t, newFakeGarmin(t), st, nil), "set_fit_download_dir", map[string]any{"path": target})).(map[string]any)

	dir, _ := st.Dir(testEmail)
	want := map[string]any{"download_dir": target, "config_path": filepath.Join(dir, "settings.json"),
		"message": "Default FIT download directory configured."}
	if !reflect.DeepEqual(data, want) {
		t.Errorf("got %v", data)
	}
	if fi, err := os.Stat(target); err != nil || !fi.IsDir() {
		t.Errorf("directory not created: %v", err)
	}
	got, err := st.LoadSettings(testEmail)
	if err != nil || got.FitDownloadDir != target || got.Domain != "garmin.cn" {
		t.Errorf("settings = %+v %v", got, err)
	}
}

func TestSetFitDownloadDirResolvesRelativePath(t *testing.T) {
	t.Chdir(t.TempDir())
	cwd, _ := os.Getwd()
	data, _ := decode(t, callOK(t, newSession(t, newFakeGarmin(t), nil), "set_fit_download_dir", map[string]any{"path": "rel/dir"})).(map[string]any)
	if data["download_dir"] != filepath.Join(cwd, "rel", "dir") {
		t.Errorf("got %v", data)
	}
	// A path that cannot be a directory is reported, not persisted.
	file := filepath.Join(cwd, "afile")
	os.WriteFile(file, nil, 0o600)
	text, isErr := callTool(t, newSession(t, newFakeGarmin(t), nil), "set_fit_download_dir", map[string]any{"path": file})
	if !isErr || !strings.HasPrefix(text, "Error setting FIT download directory: ") {
		t.Errorf("file path: %v %q", isErr, text)
	}
}

func TestDownloadActivityFileFitSavesFile(t *testing.T) {
	dir := t.TempDir()
	fitBytes := "\x0e\x10FITDATA"
	f := newFakeGarmin(t)
	f.on(fitOriginalRoute, rawBody(zipOf(t, [2]string{"activity.fit", fitBytes})))
	data, _ := decode(t, callOK(t, newSession(t, f, nil), "download_activity_file",
		map[string]any{"activity_id": fitActivityID, "output_dir": dir})).(map[string]any)

	path := filepath.Join(dir, "22041393449.fit")
	if b, err := os.ReadFile(path); err != nil || string(b) != fitBytes {
		t.Errorf("file = %q %v", b, err)
	}
	want := map[string]any{"activity_id": float64(fitActivityID), "format": "fit", "file_path": path,
		"size_bytes": float64(len(fitBytes)), "message": "Activity file saved."}
	if !reflect.DeepEqual(data, want) {
		t.Errorf("got %v", data)
	}
	if n := len(f.requests(fitOriginalRoute)); n != 1 {
		t.Errorf("downloads = %d", n)
	}
}

func TestDownloadActivityFileOtherFormatsWriteRawBytes(t *testing.T) {
	for _, fmtName := range []string{"gpx", "tcx", "csv"} {
		t.Run(fmtName, func(t *testing.T) {
			dir := t.TempDir()
			payload := "<" + fmtName + ">data</" + fmtName + ">"
			route := "GET /download-service/export/" + fmtName + "/activity/22041393449"
			f := newFakeGarmin(t)
			f.on(route, rawBody(payload))
			data, _ := decode(t, callOK(t, newSession(t, f, nil), "download_activity_file",
				map[string]any{"activity_id": fitActivityID, "format": fmtName, "output_dir": dir})).(map[string]any)
			if b, err := os.ReadFile(filepath.Join(dir, "22041393449."+fmtName)); err != nil || string(b) != payload {
				t.Errorf("file = %q %v", b, err)
			}
			if data["format"] != fmtName || len(f.requests(route)) != 1 {
				t.Errorf("got %v", data)
			}
		})
	}
}

func TestDownloadActivityFileInvalidFormat(t *testing.T) {
	f := newFakeGarmin(t)
	data, _ := decode(t, callOK(t, newSession(t, f, nil), "download_activity_file",
		map[string]any{"activity_id": fitActivityID, "format": "pdf", "output_dir": t.TempDir()})).(map[string]any)
	want := map[string]any{"error": "Invalid format 'pdf'.", "valid_formats": []any{"fit", "gpx", "tcx", "csv"}}
	if !reflect.DeepEqual(data, want) {
		t.Errorf("got %v", data)
	}
	for _, c := range f.requests("") {
		if strings.HasPrefix(c.Path, "/download-service/") {
			t.Errorf("unexpected download %s", c.Path)
		}
	}
}

func TestDownloadActivityFileNeedsSetup(t *testing.T) {
	f := newFakeGarmin(t)
	st := newStore(t)
	data, _ := decode(t, callOK(t, newSessionWithStore(t, f, st, nil), "download_activity_file",
		map[string]any{"activity_id": fitActivityID})).(map[string]any)
	cwd, _ := os.Getwd()
	dir, _ := st.Dir(testEmail)
	want := map[string]any{
		"status":            "needs_setup",
		"suggested_default": cwd,
		"config_path":       filepath.Join(dir, "settings.json"),
		"message": "No download directory configured. Ask the user where to save activity files (offer the " +
			"current working directory as the default), then call set_fit_download_dir(path) before downloading.",
	}
	if !reflect.DeepEqual(data, want) {
		t.Errorf("got %v", data)
	}
	if len(f.requests(fitOriginalRoute)) != 0 {
		t.Error("downloaded before setup")
	}
}

func TestDownloadActivityFileNoneResponse(t *testing.T) {
	dir := t.TempDir()
	f := newFakeGarmin(t)
	f.on(fitOriginalRoute, rawBody(nil))
	text, isErr := callTool(t, newSession(t, f, nil), "download_activity_file", map[string]any{"activity_id": fitActivityID, "output_dir": dir})
	if isErr || text != "No fit data returned for activity 22041393449" {
		t.Errorf("got %v %q", isErr, text)
	}
}

func TestDownloadActivityFileFitExtractionFailure(t *testing.T) {
	dir := t.TempDir()
	raw := zipOf(t, [2]string{"notes.txt", "no fit here"})
	f := newFakeGarmin(t)
	f.on(fitOriginalRoute, rawBody(raw))
	data, _ := decode(t, callOK(t, newSession(t, f, nil), "download_activity_file",
		map[string]any{"activity_id": fitActivityID, "output_dir": dir})).(map[string]any)
	dbg, _ := data["debug"].(map[string]any)
	if data["error"] != "ZIP archive contains no .fit files" || dbg["total_bytes"] != float64(len(raw)) ||
		dbg["first_16_bytes_hex"] != hexPrefix(raw) {
		t.Errorf("got %v", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "22041393449.fit")); !os.IsNotExist(err) {
		t.Errorf("file written: %v", err)
	}
}

func hexPrefix(b []byte) string {
	return hex.EncodeToString(b[:min(16, len(b))])
}

func TestDownloadActivityFileDirectoryPrecedence(t *testing.T) {
	base := t.TempDir()
	st := newStore(t)
	if err := st.SaveSettings(testEmail, accounts.Settings{FitDownloadDir: filepath.Join(base, "saved")}); err != nil {
		t.Fatal(err)
	}
	f := newFakeGarmin(t)
	f.on("GET /download-service/export/gpx/activity/5", rawBody("<gpx/>"))
	args := map[string]any{"activity_id": "5", "format": " GPX "}
	// Saved settings, then the environment, then output_dir; each directory is created.
	for _, c := range []struct {
		env  map[string]string
		out  string
		want string
	}{
		{nil, "", filepath.Join(base, "saved")},
		{map[string]string{"GARMIN_FIT_DOWNLOAD_DIR": filepath.Join(base, "env")}, "", filepath.Join(base, "env")},
		{map[string]string{"GARMIN_FIT_DOWNLOAD_DIR": filepath.Join(base, "env")}, filepath.Join(base, "arg"), filepath.Join(base, "arg")},
	} {
		if c.out != "" {
			args["output_dir"] = c.out
		}
		data, _ := decode(t, callOK(t, newSessionWithStore(t, f, st, c.env), "download_activity_file", args)).(map[string]any)
		if data["file_path"] != filepath.Join(c.want, "5.gpx") || data["format"] != "gpx" || data["size_bytes"] != 6.0 {
			t.Errorf("%v: %v", c, data)
		}
	}
}

func TestDownloadActivityFileErrors(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	text, isErr := callTool(t, cs, "download_activity_file", map[string]any{"activity_id": "x1", "output_dir": t.TempDir()})
	if !isErr || text != "Error downloading activity x1: invalid literal for int() with base 10: 'x1'" {
		t.Errorf("bad id: %v %q", isErr, text)
	}
	f.on(fitOriginalRoute, status(404, map[string]any{"message": "gone"}))
	text, isErr = callTool(t, cs, "download_activity_file", map[string]any{"activity_id": fitActivityID, "output_dir": t.TempDir()})
	if !isErr || text != "Error downloading activity 22041393449: Download client error (404): API Error 404 - gone" {
		t.Errorf("404: %v %q", isErr, text)
	}
}
