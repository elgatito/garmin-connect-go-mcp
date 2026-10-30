package server

import "testing"

func TestGearUUIDNormalization(t *testing.T) {
	if normalizedGearUUID("AB-CD") != "abcd" || gearActivityName(float64(152)) != "virtual_ride" {
		t.Fatal("bad gear mapping")
	}
}
