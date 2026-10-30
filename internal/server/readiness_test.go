package server

import "testing"

func TestReadinessRecovery(t *testing.T) {
	for _, tc := range []struct {
		raw  map[string]any
		want float64
	}{{map[string]any{"recoveryTime": float64(90)}, 1.5}, {map[string]any{"recoveryTime": float64(90), "recoveryTimeChangePhrase": "REACHED_ZERO"}, 0}} {
		if got := curateReadiness(tc.raw)["recovery_time_hours"]; got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
}
