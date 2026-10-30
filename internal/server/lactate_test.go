package server

import "testing"

func TestLactateSpeed(t *testing.T) {
	if lactateSpeed(float64(0.4)) != float64(4) || lactateSpeed(float64(0)) != nil {
		t.Fatal("incorrect conversion")
	}
}
