package server

import "testing"

func TestPredictionTime(t *testing.T) {
	for _, tc := range []struct {
		seconds any
		want    any
	}{{float64(3601), "1:00:01"}, {float64(301), "5:01"}, {nil, nil}} {
		if got := predictionTime(tc.seconds); got != tc.want {
			t.Fatalf("%v != %v", got, tc.want)
		}
	}
}
