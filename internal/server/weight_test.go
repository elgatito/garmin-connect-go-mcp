package server

import (
	"testing"
	"time"
)

func TestWeightMeasurement(t *testing.T) {
	got := weightMeasurement(map[string]any{"weight": float64(72450), "bodyFat": float64(12)}, false)
	if got["weight_kg"] != 72.45 || got["body_fat_percent"] != float64(12) {
		t.Fatal(got)
	}
}
func TestParseWeightTimestamp(t *testing.T) {
	local, err := parseTimestamp("2026-01-02T10:30:00", time.UTC)
	if err != nil || local.UTC().Format(time.RFC3339) != "2026-01-02T10:30:00Z" {
		t.Fatal(local, err)
	}
}
