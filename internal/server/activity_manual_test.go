package server

import "testing"

func TestTitleActivity(t *testing.T) {
	if titleActivity("indoor_cycling") != "Indoor Cycling" {
		t.Fatal(titleActivity("indoor_cycling"))
	}
}
