package server

import (
	"strings"
	"testing"
)

const (
	devicesPath             = "/device-service/deviceregistration/devices"
	deviceLastUsedPath      = "/device-service/deviceservice/mylastused"
	deviceSettingsPath      = "/device-service/deviceservice/device-info/settings"
	primaryTrainingPath     = "/web-gateway/device-info/primary-training-device"
	deviceSolarPath         = "/web-gateway/solar"
	deviceMessageQueuePath  = "/device-service/devicemessage/messages"
	deviceWorkoutLookupPath = "/workout-service/workout/123456"
)

func TestGetDevicesTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+devicesPath, fixture(t, "MOCK_DEVICES"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_devices", nil)
	if isErr {
		t.Fatalf("error: %s", text)
	}
	var data []map[string]any
	decodeJSON(t, text, &data)
	if len(data) != 1 || data[0]["device_id"] != 123456789.0 {
		t.Fatalf("data = %v", data)
	}
	if data[0]["device_name"] != "Garmin Forerunner 945" {
		t.Fatalf("data = %v", data)
	}
	if calls := f.callsMatching("GET", devicesPath); len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
}

func TestGetDeviceLastUsedTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+deviceLastUsedPath, fixture(t, "MOCK_DEVICE_LAST_USED"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_device_last_used", nil)
	if isErr {
		t.Fatalf("error: %s", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["user_device_id"] != 4113247000.0 {
		t.Fatalf("data = %v", data)
	}
	if data["last_upload_time"] == nil || data["last_upload_time"] == "" {
		t.Fatalf("last_upload_time = %v", data["last_upload_time"])
	}
}

func TestGetDeviceSettingsTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+deviceSettingsPath+"/abc123456789", fixture(t, "MOCK_DEVICE_SETTINGS"))
	cs := newSession(t, f, nil)

	if _, isErr := callTool(t, cs, "get_device_settings", map[string]any{"device_id": "abc123456789"}); isErr {
		t.Fatal("get_device_settings returned an error")
	}
	if calls := f.callsMatching("GET", deviceSettingsPath+"/abc123456789"); len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
}

func TestGetDeviceSettingsDefaultDevice(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+deviceLastUsedPath, map[string]any{"userDeviceId": "abc123456789"})
	f.on("GET "+deviceSettingsPath+"/abc123456789", fixture(t, "MOCK_DEVICE_SETTINGS"))
	cs := newSession(t, f, nil)

	if _, isErr := callTool(t, cs, "get_device_settings", nil); isErr {
		t.Fatal("get_device_settings returned an error")
	}
	if calls := f.callsMatching("GET", deviceLastUsedPath); len(calls) != 1 {
		t.Fatalf("last_used calls = %d", len(calls))
	}
	if calls := f.callsMatching("GET", deviceSettingsPath+"/abc123456789"); len(calls) != 1 {
		t.Fatalf("settings calls = %d", len(calls))
	}
}

func TestGetDeviceSettingsDefaultDeviceMissing(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+deviceLastUsedPath, nil)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_device_settings", nil)
	if !strings.Contains(text, "No default device found") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("GET", deviceSettingsPath+"/"); len(calls) != 0 {
		t.Fatalf("settings calls = %d", len(calls))
	}
}

func TestGetPrimaryTrainingDeviceTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+primaryTrainingPath, fixture(t, "MOCK_DEVICES").([]any)[0])
	cs := newSession(t, f, nil)

	if _, isErr := callTool(t, cs, "get_primary_training_device", nil); isErr {
		t.Fatal("get_primary_training_device returned an error")
	}
	if calls := f.callsMatching("GET", primaryTrainingPath); len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
}

func TestGetPrimaryTrainingDeviceHandlesNullNestedSections(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+primaryTrainingPath, map[string]any{
		"PrimaryTrainingDevice":  nil,
		"PrimaryTrainingDevices": nil,
		"WearableDevices":        nil,
	})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_primary_training_device", nil)
	var data any
	decodeJSON(t, text, &data)
	sameJSON(t, data, map[string]any{"primary_device_id": nil})
}

func TestGetDeviceSolarDataTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+deviceSolarPath+"/123456789/2024-01-15/2024-01-15", map[string]any{
		"deviceSolarInput": fixture(t, "MOCK_DEVICE_SOLAR_DATA"),
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_device_solar_data", map[string]any{
		"device_id": 123456789,
		"date":      "2024-01-15",
	})
	if isErr {
		t.Fatalf("error: %s", text)
	}
	var data any
	decodeJSON(t, text, &data)
	want := map[string]any{
		"device_id": 123456789.0,
		"solar_data": []any{map[string]any{
			"date":                         "2024-01-15",
			"activity_time_gained_minutes": 30.0,
			"solar_utilization_avg_pct":    40.0,
			"solar_utilization_max_pct":    100.0,
			"minutes_with_solar_input":     3.0,
		}},
	}
	sameJSON(t, data, want)
	calls := f.callsMatching("GET", deviceSolarPath+"/123456789/2024-01-15/2024-01-15")
	if calls[0].Query.Get("singleDayView") != "True" {
		t.Fatalf("query = %v", calls[0].Query)
	}
}

func TestGetDeviceSolarDataNonSolarDevice(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+deviceSolarPath+"/123456789/2024-01-15/2024-01-15", map[string]any{})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_device_solar_data", map[string]any{
		"device_id": 123456789,
		"date":      "2024-01-15",
	})
	if !strings.Contains(text, "may not have solar capabilities") {
		t.Fatalf("text = %q", text)
	}
	if strings.HasPrefix(text, "Error") {
		t.Fatalf("text = %q", text)
	}
	if !strings.Contains(text, "device ID 123456789 on 2024-01-15") || strings.Contains(text, "e+") {
		t.Fatalf("device ID not rendered as a plain integer: %q", text)
	}
}

func TestGetDeviceSolarDataOtherError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+deviceSolarPath+"/123456789/2024-01-15/2024-01-15", status(500, nil))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_device_solar_data", map[string]any{
		"device_id": "123456789",
		"date":      "2024-01-15",
	})
	if text != "Error retrieving solar data: API Error 500" {
		t.Fatalf("text = %q", text)
	}
}

func TestGetDeviceAlarmsTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+devicesPath, []any{map[string]any{"deviceId": 123456789.0}})
	f.on("GET "+deviceSettingsPath+"/123456789", map[string]any{
		"alarms": []any{map[string]any{"alarmId": 1, "time": "07:00", "enabled": true}},
	})
	cs := newSession(t, f, nil)

	if _, isErr := callTool(t, cs, "get_device_alarms", nil); isErr {
		t.Fatal("get_device_alarms returned an error")
	}
	if calls := f.callsMatching("GET", deviceSettingsPath+"/123456789"); len(calls) != 1 {
		t.Fatalf("settings calls = %d", len(calls))
	}
}

func TestPushWorkoutToDeviceExplicitDevice(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+deviceWorkoutLookupPath, map[string]any{"workoutId": 123456.0, "workoutName": "Upper Body A"})
	f.on("POST "+deviceMessageQueuePath, map[string]any{})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "push_workout_to_device", map[string]any{
		"workout_id": 123456,
		"device_id":  3456789012,
	})
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["status"] != "success" || data["device_id"] != 3456789012.0 || data["workout_name"] != "Upper Body A" {
		t.Fatalf("data = %v", data)
	}
	if !strings.Contains(data["message"].(string), "device 3456789012") || strings.Contains(data["message"].(string), "e+") {
		t.Fatalf("message = %v", data["message"])
	}
	if calls := f.callsMatching("GET", deviceLastUsedPath); len(calls) != 0 {
		t.Fatalf("last used called %d times", len(calls))
	}
	calls := f.callsMatching("POST", deviceMessageQueuePath)
	if len(calls) != 1 {
		t.Fatalf("post calls = %d", len(calls))
	}
	var payload []any
	decodeJSON(t, string(calls[0].Body), &payload)
	sameJSON(t, payload, []any{map[string]any{
		"deviceId":    3456789012.0,
		"messageUrl":  "workout-service/workout/FIT/123456",
		"messageType": "workouts",
		"groupName":   nil,
		"messageName": "Upper Body A",
		"priority":    1.0,
		"fileType":    "FIT",
		"metaDataId":  123456.0,
	}})
}

func TestPushWorkoutToDeviceDefaultsToLastUsed(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+deviceLastUsedPath, fixture(t, "MOCK_DEVICE_LAST_USED"))
	f.on("GET "+deviceWorkoutLookupPath, map[string]any{"workoutName": "Easy Run"})
	f.on("POST "+deviceMessageQueuePath, map[string]any{})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "push_workout_to_device", map[string]any{"workout_id": 123456})
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["status"] != "success" || data["device_id"] != 4113247000.0 {
		t.Fatalf("data = %v", data)
	}
	calls := f.callsMatching("POST", deviceMessageQueuePath)
	var payload []any
	decodeJSON(t, string(calls[0].Body), &payload)
	if asMap(payload[0])["deviceId"] != 4113247000.0 {
		t.Fatalf("payload = %v", payload)
	}
}

func TestPushWorkoutToDeviceNoDefaultDevice(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+deviceLastUsedPath, nil)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "push_workout_to_device", map[string]any{"workout_id": 123456})
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["status"] != "failed" || !strings.Contains(data["message"].(string), "No default device") {
		t.Fatalf("data = %v", data)
	}
	if calls := f.callsMatching("POST", deviceMessageQueuePath); len(calls) != 0 {
		t.Fatalf("post calls = %d", len(calls))
	}
}

func TestPushWorkoutToDeviceError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+deviceWorkoutLookupPath, map[string]any{"workoutName": "Easy Run"})
	f.on("POST "+deviceMessageQueuePath, status(404, map[string]any{"message": "API Error 404"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "push_workout_to_device", map[string]any{
		"workout_id": 123456,
		"device_id":  3456789012,
	})
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["status"] != "failed" {
		t.Fatalf("data = %v", data)
	}
	if !strings.Contains(data["message"].(string), "404") {
		t.Fatalf("message = %v", data["message"])
	}
}
