package server

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// Ported from the devices cases of tests/integration/test_other_modules_tools.py. The Python mocks bypass
// garminconnect's int(device_id), so the string device ids here are numeric (see TestGetDeviceSettingsRejectsNonNumericID).

const (
	deviceSettingsRoute = "GET /device-service/deviceservice/device-info/settings/123456789"
	primaryRoute        = "GET /web-gateway/device-info/primary-training-device"
	solarRoute          = "GET /web-gateway/solar/123456789/2024-01-15/2024-01-15"
	pushRoute           = "POST /device-service/devicemessage/messages"
	workoutRoute        = "GET /workout-service/workout/123456"
	lastUsedRoute2      = "GET /device-service/deviceservice/mylastused"
)

func TestGetDevicesTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(devicesRoute, fixture(t, "MOCK_DEVICES"))
	got := decode(t, callOK(t, newSession(t, f, nil), "get_devices", nil))
	want := []any{map[string]any{"device_id": 123456789.0, "device_name": "Garmin Forerunner 945", "battery_status": "GOOD"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	theCall(t, f, devicesRoute)
}

func TestGetDevicesCuratesOptionalFields(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(devicesRoute, []any{map[string]any{"deviceId": 1.0, "productDisplayName": "Watch", "partNumber": "006", "manufacturerName": "GARMIN",
		"serialNumber": 5.0, "softwareVersionString": "1.0", "deviceStatusName": "ACTIVE", "lastSyncTime": "2024", "deviceType": "WATCH",
		"primaryDevice": false, "capabilityFlag1": true}})
	got := asList(decode(t, callOK(t, newSession(t, f, nil), "get_devices", nil)))[0]
	want := map[string]any{"device_id": 1.0, "device_name": "Watch", "model": "006", "manufacturer": "GARMIN", "serial_number": 5.0,
		"software_version": "1.0", "status": "ACTIVE", "last_sync_time": "2024", "device_type": "WATCH", "is_primary": false}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestGetDeviceLastUsedTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(lastUsedRoute2, map[string]any{"deviceId": 123456789.0, "lastUsed": "2024-01-15"})
	text := callOK(t, newSession(t, f, nil), "get_device_last_used", nil)
	theCall(t, f, lastUsedRoute2)
	if !reflect.DeepEqual(decode(t, text), map[string]any{}) {
		t.Errorf("text = %s", text)
	}
}

func TestGetDeviceLastUsedFormatsUploadTimeInServerLocalTime(t *testing.T) {
	setLocal(t, time.FixedZone("X", 2*3600))
	f := newFakeGarmin(t)
	f.on(lastUsedRoute2, fixture(t, "MOCK_DEVICE_LAST_USED"))
	got := resultMap(t, callOK(t, newSession(t, f, nil), "get_device_last_used", nil))
	want := map[string]any{"user_device_id": 4113247000.0, "device_name": "Garmin Forerunner 945", "device_key": "123456789",
		"user_profile_id": 80653452.0, "last_upload_time": "2024-01-28 18:00:00", "image_url": "https://example.com/device.png"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestGetDeviceSettingsTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(deviceSettingsRoute, fixture(t, "MOCK_DEVICE_SETTINGS"))
	got := resultMap(t, callOK(t, newSession(t, f, nil), "get_device_settings", map[string]any{"device_id": "123456789"}))
	if !reflect.DeepEqual(got, map[string]any{"device_id": 123456789.0, "time_format": "24_HOUR"}) {
		t.Errorf("got %v", got)
	}
	theCall(t, f, deviceSettingsRoute)
}

func TestGetDeviceSettingsAcceptsIntegerID(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(deviceSettingsRoute, fixture(t, "MOCK_DEVICE_SETTINGS"))
	callOK(t, newSession(t, f, nil), "get_device_settings", map[string]any{"device_id": 123456789})
	theCall(t, f, deviceSettingsRoute)
}

func TestGetDeviceSettingsRejectsNonNumericID(t *testing.T) {
	f := newFakeGarmin(t)
	text, isErr := callTool(t, newSession(t, f, nil), "get_device_settings", map[string]any{"device_id": "../../x?y#z"})
	if !isErr || text != "Error retrieving device settings: invalid literal for int() with base 10: '../../x?y#z'" || len(f.requests("")) != 0 {
		t.Errorf("%v %q requests=%v", isErr, text, f.requests(""))
	}
	text, isErr = callTool(t, newSession(t, f, nil), "get_device_settings", map[string]any{"device_id": 0})
	if !isErr || text != "Error retrieving device settings: device_id must be a positive integer, got: 0" {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetDeviceSettingsDefaultDevice(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(lastUsedRoute2, map[string]any{"userDeviceId": "123456789"})
	f.on(deviceSettingsRoute, fixture(t, "MOCK_DEVICE_SETTINGS"))
	callOK(t, newSession(t, f, nil), "get_device_settings", nil)
	theCall(t, f, lastUsedRoute2)
	theCall(t, f, deviceSettingsRoute)
}

func TestGetDeviceSettingsDefaultDeviceMissing(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(lastUsedRoute2, nil)
	text := callOK(t, newSession(t, f, nil), "get_device_settings", nil)
	if !strings.Contains(text, "No default device found") || len(f.requests(deviceSettingsRoute)) != 0 {
		t.Errorf("text = %q", text)
	}
	f.on(lastUsedRoute2, map[string]any{"userDeviceId": nil})
	text = callOK(t, newSession(t, f, nil), "get_device_settings", nil)
	if text != "Default device has no userDeviceId. Pass device_id explicitly." {
		t.Errorf("text = %q", text)
	}
}

func TestGetDeviceSettingsCuration(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(deviceSettingsRoute, map[string]any{"deviceId": 123456789.0, "timeFormat": "12_HOUR", "dateFormat": "MDY", "measurementUnits": "STATUTE",
		"keyTonesEnabled": false, "keyVibrationEnabled": true, "alertTonesEnabled": nil,
		"activityTracking": map[string]any{"moveAlertEnabled": true, "pulseOxSleepTrackingEnabled": false, "highHrAlertEnabled": nil},
		"alarms":           []any{map[string]any{"alarmMode": "ON"}, map[string]any{"alarmMode": "OFF"}}})
	got := resultMap(t, callOK(t, newSession(t, f, nil), "get_device_settings", map[string]any{"device_id": 123456789}))
	want := map[string]any{"device_id": 123456789.0, "time_format": "12_HOUR", "date_format": "MDY", "measurement_units": "STATUTE",
		"key_tones_enabled": false, "key_vibration_enabled": true,
		"activity_tracking": map[string]any{"move_alert_enabled": true, "pulse_ox_sleep_tracking": false},
		"alarm_count":       2.0, "enabled_alarm_count": 1.0}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestGetPrimaryTrainingDeviceTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(primaryRoute, fixture(t, "MOCK_DEVICES").([]any)[0])
	got := resultMap(t, callOK(t, newSession(t, f, nil), "get_primary_training_device", nil))
	if !reflect.DeepEqual(got, map[string]any{"primary_device_id": nil}) {
		t.Errorf("got %v", got)
	}
	theCall(t, f, primaryRoute)
}

func TestGetPrimaryTrainingDeviceCuration(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(primaryRoute, map[string]any{
		"PrimaryTrainingDevice": map[string]any{"deviceId": 7.0},
		"PrimaryTrainingDevices": map[string]any{"deviceWeights": []any{
			map[string]any{"deviceId": 7.0, "displayName": "Fenix", "primaryWearableDevice": true, "primaryTrainingCapable": true, "imageUrl": "u"},
			map[string]any{"deviceId": 8.0, "displayName": "Edge"}}},
		"WearableDevices": map[string]any{"wearableDeviceCount": 2.0}})
	got := resultMap(t, callOK(t, newSession(t, f, nil), "get_primary_training_device", nil))
	want := map[string]any{"primary_device_id": 7.0, "training_device_count": 2.0, "wearable_device_count": 2.0, "training_devices": []any{
		map[string]any{"device_id": 7.0, "display_name": "Fenix", "is_primary_wearable": true, "primary_training_capable": true, "image_url": "u"},
		map[string]any{"device_id": 8.0, "display_name": "Edge", "is_primary_wearable": nil, "primary_training_capable": nil}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestGetPrimaryTrainingDeviceHandlesNullNestedSections(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(primaryRoute, map[string]any{"PrimaryTrainingDevice": nil, "PrimaryTrainingDevices": nil, "WearableDevices": nil})
	got := decode(t, callOK(t, newSession(t, f, nil), "get_primary_training_device", nil))
	if !reflect.DeepEqual(got, map[string]any{"primary_device_id": nil}) {
		t.Errorf("got %v", got)
	}
}

func TestGetDeviceSolarDataTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(solarRoute, map[string]any{"deviceSolarInput": fixture(t, "MOCK_DEVICE_SOLAR_DATA")})
	got := decode(t, callOK(t, newSession(t, f, nil), "get_device_solar_data", map[string]any{"device_id": 123456789, "date": "2024-01-15"}))
	want := map[string]any{"device_id": 123456789.0, "solar_data": []any{map[string]any{
		"date": "2024-01-15", "activity_time_gained_minutes": 30.0, "solar_utilization_avg_pct": 40.0,
		"solar_utilization_max_pct": 100.0, "minutes_with_solar_input": 3.0}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	if c := theCall(t, f, solarRoute); c.Query.Get("singleDayView") != "True" || len(c.Query) != 1 {
		t.Errorf("query = %v", c.Query)
	}
}

func TestGetDeviceSolarDataNonSolarDevice(t *testing.T) {
	for name, body := range map[string]any{"no_key": map[string]any{}, "empty_days": map[string]any{"deviceSolarInput": map[string]any{"solarDailyDataDTOs": []any{}}}} {
		t.Run(name, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on(solarRoute, body)
			text, isErr := callTool(t, newSession(t, f, nil), "get_device_solar_data", map[string]any{"device_id": "123456789", "date": "2024-01-15"})
			if isErr || !strings.Contains(text, "may not have solar capabilities") || strings.HasPrefix(text, "Error") {
				t.Errorf("%v %q", isErr, text)
			}
		})
	}
}

func TestGetDeviceSolarDataOtherError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(solarRoute, status(500, rawBody(nil)))
	text, isErr := callTool(t, newSession(t, f, nil), "get_device_solar_data", map[string]any{"device_id": "123456789", "date": "2024-01-15"})
	if !isErr || text != "Error retrieving solar data: API Error 500" {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetDeviceSolarDataValidatesDate(t *testing.T) {
	f := newFakeGarmin(t)
	text, isErr := callTool(t, newSession(t, f, nil), "get_device_solar_data", map[string]any{"device_id": 1, "date": "15/01/2024"})
	if !isErr || text != "Error retrieving solar data: startdate must be in format 'YYYY-MM-DD', got: 15/01/2024" {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetDeviceAlarmsTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(devicesRoute, []any{map[string]any{"deviceId": 123456789.0}, map[string]any{"deviceId": 5.0}, map[string]any{"deviceId": 6.0}})
	f.on(deviceSettingsRoute, map[string]any{"alarms": []any{
		map[string]any{"alarmId": 2.0, "alarmTime": 1125.0, "alarmMode": "OFF", "alarmDays": []any{"SATURDAY"}, "alarmSound": "VIBRATION", "backlight": "ON"},
		map[string]any{"alarmId": 1.0, "alarmTime": 420.0, "alarmMode": "ON", "alarmDays": []any{"MONDAY", "TUESDAY"}, "alarmSound": "TONE", "alarmMessage": "Wake"},
	}})
	f.on("GET /device-service/deviceservice/device-info/settings/5", map[string]any{"alarms": nil})
	f.on("GET /device-service/deviceservice/device-info/settings/6", map[string]any{"alarms": []any{map[string]any{"alarmId": 3.0}}})
	got := resultMap(t, callOK(t, newSession(t, f, nil), "get_device_alarms", nil))
	want := map[string]any{"total_alarms": 3.0, "enabled_alarms": 1.0, "alarms": []any{
		map[string]any{"alarm_id": 3.0, "time": nil, "time_minutes": nil, "enabled": false, "days": []any{}, "sound": nil},
		map[string]any{"alarm_id": 1.0, "time": "07:00", "time_minutes": 420.0, "enabled": true, "days": []any{"MONDAY", "TUESDAY"}, "sound": "TONE", "message": "Wake"},
		map[string]any{"alarm_id": 2.0, "time": "18:45", "time_minutes": 1125.0, "enabled": false, "days": []any{"SATURDAY"}, "sound": "VIBRATION", "backlight": "ON"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestGetDeviceAlarmsNone(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(devicesRoute, []any{})
	if text := callOK(t, newSession(t, f, nil), "get_device_alarms", nil); text != "No device alarms found." {
		t.Errorf("text = %q", text)
	}
}

func pushPayload(deviceID any, name string) []any {
	return []any{map[string]any{"deviceId": deviceID, "messageUrl": "workout-service/workout/FIT/123456", "messageType": "workouts",
		"groupName": nil, "messageName": name, "priority": 1.0, "fileType": "FIT", "metaDataId": 123456.0}}
}

func TestPushWorkoutToDeviceExplicitDevice(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(workoutRoute, map[string]any{"workoutId": 123456.0, "workoutName": "Upper Body A"})
	f.on(pushRoute, map[string]any{})
	d := resultMap(t, callOK(t, newSession(t, f, nil), "push_workout_to_device", map[string]any{"workout_id": 123456, "device_id": 3456789012}))
	if d["status"] != "success" || d["device_id"] != 3456789012.0 || d["workout_name"] != "Upper Body A" || d["workout_id"] != 123456.0 ||
		d["message"] != "Queued workout 123456 for device 3456789012; it arrives on the device at its next sync" {
		t.Errorf("got %v", d)
	}
	if len(f.requests(lastUsedRoute2)) != 0 {
		t.Error("last used device was looked up")
	}
	c := theCall(t, f, pushRoute)
	if !reflect.DeepEqual(decode(t, string(c.Body)), any(pushPayload(3456789012.0, "Upper Body A"))) {
		t.Errorf("body = %s", c.Body)
	}
}

func TestPushWorkoutToDeviceDefaultsToLastUsed(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(lastUsedRoute2, fixture(t, "MOCK_DEVICE_LAST_USED"))
	f.on(workoutRoute, map[string]any{"workoutName": "Easy Run"})
	f.on(pushRoute, status(204, rawBody(nil)))
	d := resultMap(t, callOK(t, newSession(t, f, nil), "push_workout_to_device", map[string]any{"workout_id": 123456}))
	if d["status"] != "success" || d["device_id"] != 4113247000.0 {
		t.Errorf("got %v", d)
	}
	if !reflect.DeepEqual(decode(t, string(theCall(t, f, pushRoute).Body)), any(pushPayload(4113247000.0, "Easy Run"))) {
		t.Error("payload mismatch")
	}
}

func TestPushWorkoutToDeviceNameFallback(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(workoutRoute, map[string]any{"workoutName": ""})
	f.on(pushRoute, map[string]any{})
	callOK(t, newSession(t, f, nil), "push_workout_to_device", map[string]any{"workout_id": 123456, "device_id": 9})
	if !reflect.DeepEqual(decode(t, string(theCall(t, f, pushRoute).Body)), any(pushPayload(9.0, "Workout 123456"))) {
		t.Error("payload mismatch")
	}
}

func TestPushWorkoutToDeviceNoDefaultDevice(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(lastUsedRoute2, nil)
	d := resultMap(t, callOK(t, newSession(t, f, nil), "push_workout_to_device", map[string]any{"workout_id": 123456}))
	if d["status"] != "failed" || !strings.Contains(d["message"].(string), "No default device") || d["workout_id"] != 123456.0 {
		t.Errorf("got %v", d)
	}
	if len(f.requests(pushRoute)) != 0 {
		t.Error("a message was queued")
	}
}

func TestPushWorkoutToDeviceError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(workoutRoute, map[string]any{"workoutName": "Easy Run"})
	f.on(pushRoute, status(404, map[string]any{"message": "gone"}))
	text, isErr := callTool(t, newSession(t, f, nil), "push_workout_to_device", map[string]any{"workout_id": 123456, "device_id": 3456789012})
	d := resultMap(t, text)
	if isErr || d["status"] != "failed" || !strings.Contains(d["message"].(string), "404") || d["device_id"] != 3456789012.0 {
		t.Errorf("%v %v", isErr, d)
	}
}

func TestPushWorkoutToDeviceUnexpectedSuccessStatus(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(workoutRoute, map[string]any{"workoutName": "Easy Run"})
	f.on(pushRoute, status(202, map[string]any{}))
	d := resultMap(t, callOK(t, newSession(t, f, nil), "push_workout_to_device", map[string]any{"workout_id": 123456, "device_id": 9}))
	want := map[string]any{"status": "failed", "workout_id": 123456.0, "device_id": 9.0, "http_status": 202.0, "message": "Failed to push workout: HTTP 202"}
	if !reflect.DeepEqual(d, want) {
		t.Errorf("got %v", d)
	}
}

func TestPushWorkoutToDeviceWorkoutLookupFailure(t *testing.T) {
	f := newFakeGarmin(t)
	d := resultMap(t, callOK(t, newSession(t, f, nil), "push_workout_to_device", map[string]any{"workout_id": 123456, "device_id": 9}))
	if d["status"] != "failed" || !strings.HasPrefix(d["message"].(string), "Failed to push workout: API Error 404") || len(f.requests(pushRoute)) != 0 {
		t.Errorf("got %v", d)
	}
}
