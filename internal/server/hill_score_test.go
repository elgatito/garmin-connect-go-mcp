package server

import (
	"encoding/json"
	"testing"
)

func TestHillScoreUsesFirstAverageAndLatestDate(t *testing.T) {
	avg := firstJSONValue(json.RawMessage(`{"z":10,"a":20}`))
	out := hillSummary(map[string]any{"hillScoreDTOList": []any{map[string]any{"calendarDate": "2026-01-03", "overallScore": float64(50)}, map[string]any{"calendarDate": "2026-01-01", "overallScore": float64(30)}}}, avg, "2026-01-01", "2026-01-03")
	if out["period_avg_score"] != float64(10) || out["latest_overall_score"] != float64(50) {
		t.Fatal(out)
	}
}
