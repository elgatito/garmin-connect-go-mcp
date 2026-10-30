package server

import "testing"

func TestStressSummary(t *testing.T) {
	data := map[string]any{"stressValuesArray": []any{[]any{1.0, 10.0}, []any{2.0, 60.0}, []any{3.0, -1.0}}}
	out := healthSummary("get_stress_summary", data)
	if out["rest_percent"] != float64(50) || out["medium_stress_percent"] != float64(50) || out["data_points_count"] != 2 {
		t.Fatal(out)
	}
}
