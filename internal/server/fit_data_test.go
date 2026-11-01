package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The goldens are the pinned Python _parse_fit (fitparse 1.2.0, CPython 3.12 sum() semantics) run on the
// testdata FIT files with include_records=True; records and rr_intervals_seconds are cut to the first 3,
// and <key>_total holds their full length.
func TestParseFitMatchesPythonGolden(t *testing.T) {
	for _, name := range []string{"Activity", "ActivityDevFields", "WithGearChangeData"} {
		t.Run(name, func(t *testing.T) {
			got, err := parseFit(readTestFIT(t, name+".fit"), true)
			if err != nil {
				t.Fatal(err)
			}
			for _, k := range []string{"records", "rr_intervals_seconds"} {
				if l, ok := got[k].([]map[string]any); ok {
					got[k+"_total"] = len(l)
					got[k] = l[:min(3, len(l))]
				}
			}
			b, err := os.ReadFile(filepath.Join("testdata", "fit_data_golden", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var want any
			if err := json.Unmarshal(b, &want); err != nil {
				t.Fatal(err)
			}
			diffJSON(t, name, jsonValue(t, got), want)
		})
	}
}

func TestParseFitRejectsNonFitBytes(t *testing.T) {
	if _, err := parseFit([]byte("PK\x03\x04garbage"), false); err == nil {
		t.Error("zip garbage parsed")
	}
	if _, err := parseFit([]byte("<html>error</html>"), false); err == nil {
		t.Error("html parsed")
	}
}
