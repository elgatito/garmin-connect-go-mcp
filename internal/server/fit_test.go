package server

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func readTestFIT(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFitMessageNamesMatchFitparse(t *testing.T) {
	msgs, err := decodeFIT(readTestFIT(t, "Activity.fit"))
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, m := range msgs {
		counts[m.Name]++
	}
	// fitparse 1.2.0 inventory of the same file.
	want := map[string]int{"file_id": 1, "device_info": 1, "event": 2, "developer_data_id": 1,
		"field_description": 2, "record": 3601, "lap": 1, "session": 1, "activity": 1}
	if !reflect.DeepEqual(counts, want) {
		t.Fatalf("counts = %v\nwant %v", counts, want)
	}
	if fitMessageName(233) != "unknown_233" || fitMessageName(0xFF00) != "unknown_65280" || fitMessageName(20) != "record" {
		t.Fatal(fitMessageName(233), fitMessageName(0xFF00), fitMessageName(20))
	}
}

// The goldens are _parse_fit_messages output from the pinned Python (fitparse 1.2.0) on the same files.
func TestParseFitMessagesMatchesFitparseGolden(t *testing.T) {
	cases := []struct {
		golden, file  string
		types         []string
		records       bool
		offset, limit int
	}{
		{"Activity.default", "Activity.fit", nil, false, 0, 1000},
		{"Activity.records_0_10", "Activity.fit", []string{"record"}, true, 0, 10},
		{"Activity.records_1500_5", "Activity.fit", []string{"record"}, true, 1500, 5},
		{"ActivityDevFields.default", "ActivityDevFields.fit", nil, false, 0, 1000},
		{"ActivityDevFields.records_0_10", "ActivityDevFields.fit", []string{"record"}, true, 0, 10},
		{"ActivityDevFields.records_1500_5", "ActivityDevFields.fit", []string{"record"}, true, 1500, 5},
		{"WithGearChangeData.default", "WithGearChangeData.fit", nil, false, 0, 1000},
		{"WithGearChangeData.records_0_10", "WithGearChangeData.fit", []string{"record"}, true, 0, 10},
		{"WithGearChangeData.records_1500_5", "WithGearChangeData.fit", []string{"record"}, true, 1500, 5},
		{"WithGearChangeData.file_id", "WithGearChangeData.fit", []string{" FILE_ID ", ""}, false, 0, 1},
	}
	for _, c := range cases {
		t.Run(c.golden, func(t *testing.T) {
			res, err := parseFitMessages(readTestFIT(t, c.file), c.types, c.records, c.offset, c.limit)
			if err != nil {
				t.Fatal(err)
			}
			got := jsonValue(t, res)
			want := decode(t, string(readTestFIT(t, filepath.Join("fitparse_golden", c.golden+".json"))))
			patchNewerProfile(want)
			diffJSON(t, "", got, want)
		})
	}
}

// patchNewerProfile rewrites the golden where fitparse's FIT profile (SDK 20.8) predates the library's (21.218):
// record field 108 (enhanced_respiration_rate) and the Edge 1040 product label are unknown to fitparse.
func patchNewerProfile(golden any) {
	msgs, _ := golden.(map[string]any)["messages"].([]any)
	for _, m := range msgs {
		for _, f := range m.(map[string]any)["fields"].([]any) {
			f := f.(map[string]any)
			switch {
			case f["name"] == "unknown_108" && m.(map[string]any)["type"] == "record":
				f["name"], f["profile_type"], f["units"] = "enhanced_respiration_rate", "uint16", "Breaths/min"
				f["value"] = f["raw_value"].(float64) / 100
			case f["name"] == "garmin_product" && f["raw_value"] == 3843.0:
				f["value"], f["value_name"] = "edge_1040", "edge_1040"
			}
		}
	}
}

// diffJSON reports every differing path (DeepEqual alone would only say "not equal").
func diffJSON(t *testing.T, path string, got, want any) {
	t.Helper()
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			t.Errorf("%s: got %v, want object", path, got)
			return
		}
		for k := range w {
			if _, ok := g[k]; !ok {
				t.Errorf("%s.%s: missing, want %v", path, k, w[k])
			}
		}
		for k := range g {
			if _, ok := w[k]; !ok {
				t.Errorf("%s.%s: unexpected %v", path, k, g[k])
				continue
			}
			diffJSON(t, path+"."+k, g[k], w[k])
		}
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			t.Errorf("%s: got %v, want %v", path, got, want)
			return
		}
		for i := range w {
			diffJSON(t, fmt.Sprintf("%s[%d]", path, i), g[i], w[i])
		}
	default:
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %#v, want %#v", path, got, want)
		}
	}
}

func TestParseFitMessagesKeepsFileOrderInInventory(t *testing.T) {
	res, err := parseFitMessages(readTestFIT(t, "Activity.fit"), nil, false, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(res.MessageCounts)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"file_id":1,"device_info":1,"event":2,"developer_data_id":1,"field_description":2,"record":3601,"lap":1,"session":1,"activity":1}`
	if string(b) != want {
		t.Fatalf("got %s", b)
	}
}

func TestParseFitMessagesValidatesPagination(t *testing.T) {
	for _, c := range []struct {
		offset, limit int
		want          string
	}{
		{-1, 10, "message_offset must be at least 0"},
		{0, 0, "message_limit must be between 1 and 5000"},
		{0, 5001, "message_limit must be between 1 and 5000"},
	} {
		if _, err := parseFitMessages(nil, nil, false, c.offset, c.limit); err == nil || err.Error() != c.want {
			t.Errorf("(%d,%d): %v", c.offset, c.limit, err)
		}
	}
}

func zipOf(t *testing.T, files ...[2]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		w, err := zw.Create(f[0])
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(f[1]))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractFitBytes(t *testing.T) {
	got, err := extractFitBytes(zipOf(t, [2]string{"notes.txt", "x"}, [2]string{"A.FIT", "first"}, [2]string{"b.fit", "second"}))
	if err != nil || string(got) != "first" {
		t.Fatalf("zip: %q %v", got, err)
	}
	if _, err := extractFitBytes(zipOf(t, [2]string{"notes.txt", "no fit here"})); err == nil || err.Error() != "ZIP archive contains no .fit files" {
		t.Fatalf("zip without fit: %v", err)
	}
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write([]byte("\x0e\x10gz"))
	zw.Close()
	if got, err := extractFitBytes(gz.Bytes()); err != nil || string(got) != "\x0e\x10gz" {
		t.Fatalf("gzip: %q %v", got, err)
	}
	if got, err := extractFitBytes([]byte("\x0e\x10raw")); err != nil || string(got) != "\x0e\x10raw" {
		t.Fatalf("raw: %q %v", got, err)
	}
}

func TestJSONSafeFitValue(t *testing.T) {
	ts := time.Date(2021, 7, 20, 21, 11, 20, 0, time.UTC)
	got := jsonSafeFitValue([]any{nil, true, int64(3), 1.5, "s", []byte{1, 2}, ts, []any{int64(1), nil},
		map[string]any{"k": []byte{0xff}}, math.Inf(1), math.Inf(-1), "08:30:00"})
	want := []any{nil, true, int64(3), 1.5, "s", map[string]any{"encoding": "hex", "value": "0102"},
		"2021-07-20T21:11:20", []any{int64(1), nil}, map[string]any{"k": map[string]any{"encoding": "hex", "value": "ff"}},
		"Infinity", "-Infinity", "08:30:00"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
}

func TestGetFieldReturnsFirstNonNullValue(t *testing.T) {
	m := fitMessage{Name: "event", Fields: []fitField{
		{Name: "speed", Value: nil},
		{Name: "gear_change_data", Parent: "data", Value: int64(7)},
		{Name: "enhanced_speed", Value: 2.5},
	}}
	if got := getField(m, "speed", "enhanced_speed"); got != 2.5 {
		t.Errorf("got %v", got)
	}
	if got := getField(m, "data"); got != int64(7) {
		t.Errorf("subfield by parent name: %v", got)
	}
	if got := getField(m, "missing"); got != nil {
		t.Errorf("missing: %v", got)
	}
}

func TestSemicirclesToDegrees(t *testing.T) {
	if got := semicirclesToDegrees(int64(477503889)); got != 40.023914 {
		t.Errorf("got %v", got)
	}
	if got := semicirclesToDegrees(nil); got != nil {
		t.Errorf("nil: %v", got)
	}
}
