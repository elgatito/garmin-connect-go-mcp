package server

import (
	"strings"
	"testing"
)

func TestWeightGoalTargetDate(t *testing.T) {
	if targetDateError("2026-01-01", "2026-02-01", nil) == "" {
		t.Fatal("expired target allowed")
	}
	target := "2027-01-01"
	if got := targetDateError("2026-01-01", "2026-02-01", &target); got != "" {
		t.Fatal(got)
	}
	target = "2026-02-01"
	if got := targetDateError(nil, "2026-02-01", &target); !strings.Contains(got, "must be after") {
		t.Fatal(got)
	}
}
