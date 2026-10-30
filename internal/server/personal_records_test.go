package server

import "testing"

func TestPersonalRecords(t *testing.T) {
	rows := personalRecords([]any{map[string]any{"typeId": float64(3), "value": float64(1200), "prStartTimeLocalFormatted": "2026-01-02T23:00:00"}})
	if rows[0]["record_type"] != "Fastest 5K" || rows[0]["value"] != "20:00" || rows[0]["date"] != "2026-01-02" {
		t.Fatal(rows)
	}
}
