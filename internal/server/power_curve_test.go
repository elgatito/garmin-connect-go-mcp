package server

import "testing"

func TestPowerCurve(t *testing.T) {
	records := make([]powerRecord, 5)
	for i := range records {
		records[i] = powerRecord{power: 200, valid: true}
	}
	out := powerCurve(records)
	if out["5s"] != 200 || out["30s"] != 0 {
		t.Fatal(out)
	}
}
