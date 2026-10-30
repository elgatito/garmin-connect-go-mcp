package server

import "testing"

func TestRecoverySnapshotLatestAndZero(t *testing.T) {
	result := recoverySnapshot([]any{map[string]any{"timestampLocal": "2026-01-01T10:00:00", "recoveryTime": float64(600)}, map[string]any{"timestampLocal": "2026-01-01T11:00:00", "recoveryTime": float64(600), "recoveryTimeChangePhrase": "REACHED_ZERO"}}, "training_readiness", "2026-01-01")
	if result["remaining_hours"] != float64(0) || result["state"] != "recovered" {
		t.Fatal(result)
	}
}
