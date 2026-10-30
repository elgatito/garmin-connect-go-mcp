package server

import "testing"

func TestSolarSummary(t *testing.T) {
	days := solarSummary([]any{map[string]any{"solarInputReadings": []any{map[string]any{"solarUtilization": float64(10)}, map[string]any{"solarUtilization": float64(0)}}}})
	row := days[0].(map[string]any)
	if row["solar_utilization_avg_pct"] != float64(5) || row["minutes_with_solar_input"] != 1 {
		t.Fatal(row)
	}
}
