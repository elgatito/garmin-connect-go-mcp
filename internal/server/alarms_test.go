package server

import "testing"

func TestCurateAlarms(t *testing.T) {
	out := curateAlarms([]map[string]any{{"alarmTime": float64(480), "alarmMode": "ON"}, {"alarmTime": float64(360), "alarmMode": "OFF"}})
	rows := out["alarms"].([]map[string]any)
	if out["enabled_alarms"] != 1 || rows[0]["time"] != "06:00" {
		t.Fatal(out)
	}
}
