package server

import "testing"

func TestStatsByDate(t *testing.T) {
	data := statsByDate(map[string]any{"values": []any{map[string]any{"calendarDate": "2026-01-01", "values": map[string]any{"totalSteps": float64(100)}}, map[string]any{"calendarDate": "2026-01-02", "values": nil}}})
	if data["2026-01-01"]["totalSteps"] != float64(100) || data["2026-01-02"] == nil {
		t.Fatal(data)
	}
}
