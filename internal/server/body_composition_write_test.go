package server

import (
	"testing"
	"time"
)

func TestBodyCompositionFITRoundTrip(t *testing.T) {
	b, err := bodyCompositionFIT(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), map[string]any{"weight": float64(70.5), "percent_fat": float64(12)})
	if err != nil {
		t.Fatal(err)
	}
	messages, err := decodeFIT(b)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	creator := false
	for _, m := range messages {
		if m.Name == "file_creator" {
			creator = true
		}
		if m.Name != "weight_scale" {
			continue
		}
		for _, f := range m.Fields {
			if f.Name == "weight" && f.Value == float64(70.5) {
				found = true
			}
		}
	}
	if !found || !creator {
		t.Fatal("missing scaled weight", messages)
	}
}
