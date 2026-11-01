package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

// Port of garmin_mcp/devices.py.

type deviceSettingsIn struct {
	Account
	DeviceID any `json:"device_id,omitempty" jsonschema:"Device ID (optional; defaults to the most recently used device when omitted; can be obtained from get_devices or get_device_last_used)"`
}

type deviceSolarIn struct {
	Account
	DeviceID any    `json:"device_id" jsonschema:"Device ID (can be obtained from get_devices)"`
	Date     string `json:"date" jsonschema:"Date in YYYY-MM-DD format"`
}

type pushWorkoutIn struct {
	Account
	WorkoutID int64  `json:"workout_id" jsonschema:"ID of the workout to send (from get_workouts or upload_workout)"`
	DeviceID  *int64 `json:"device_id,omitempty" jsonschema:"Device ID (optional; defaults to the most recently used device; can be obtained from get_devices or get_device_last_used)"`
}

const (
	deviceSettingsDesc = `Get settings for a specific Garmin device

Returns device configuration including time/date format, units,
activity tracking settings, and alarm information.`
	deviceSolarDesc = `Get solar data for a specific device

Returns a per-day summary of solar charging for devices with solar panels
(e.g., Instinct Solar, Fenix Solar): activity time gained from solar, and
the average/peak solar utilization and minutes with solar input across the
day's per-minute readings. Only applicable to solar-capable devices.`
	primaryDeviceDesc = `Get information about the primary training device

Returns details about the device designated as primary for training
metrics, along with other wearable devices on the account.`
	pushWorkoutDesc = `Send a workout from the workout library to a Garmin device

Queues the workout in Garmin Connect's device message queue, the same
as "Send to Device" in the Garmin Connect app. The watch picks it up
at its next sync with Garmin Connect (phone app or Wi-Fi), so it does
not appear on the watch immediately. Scheduling a workout does not
need this: scheduled workouts reach the watch's calendar on their own.

Each call queues another copy, so do not push the same workout twice.`
	noSolarFmt = "No solar data available for device ID %s on %s. This device may not have solar capabilities."
)

func registerDeviceTools(r *registry) {
	addTool(r, "get_devices", "Get all Garmin devices associated with the user account", getDevices)
	addTool(r, "get_device_last_used", "Get information about the last used Garmin device", getDeviceLastUsed)
	addTool(r, "get_device_settings", deviceSettingsDesc, getDeviceSettings)
	addTool(r, "get_primary_training_device", primaryDeviceDesc, getPrimaryTrainingDevice)
	addTool(r, "get_device_solar_data", deviceSolarDesc, getDeviceSolarData)
	addTool(r, "get_device_alarms", "Get alarms from all Garmin devices\n\nReturns all configured alarms with their schedules, sounds, and enabled status.", getDeviceAlarms)
	addTool(r, "push_workout_to_device", pushWorkoutDesc, pushWorkoutToDevice)
}

func getDevices(ctx context.Context, s *Session, _ noArgsIn) (any, error) {
	devices, err := s.Client.GetDevices(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving devices: %w", err)
	}
	if falsy(devices) {
		return "No devices found.", nil
	}
	curated := []map[string]any{}
	for _, d := range asList(devices) {
		dev := asMap(d)
		info := map[string]any{
			"device_id": dev["deviceId"], "device_name": firstTruthy(dev["displayName"], dev["productDisplayName"]),
			"model": dev["partNumber"], "manufacturer": dev["manufacturerName"], "serial_number": dev["serialNumber"],
			"software_version": dev["softwareVersionString"], "status": dev["deviceStatusName"],
			"last_sync_time": dev["lastSyncTime"], "battery_status": dev["batteryStatus"],
		}
		if !falsy(dev["deviceType"]) {
			info["device_type"] = dev["deviceType"]
		}
		if dev["primaryDevice"] != nil {
			info["is_primary"] = dev["primaryDevice"]
		}
		curated = append(curated, dropNil(info))
	}
	return curated, nil
}

func getDeviceLastUsed(ctx context.Context, s *Session, _ noArgsIn) (any, error) {
	device, err := s.Client.GetDeviceLastUsed(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving last used device: %w", err)
	}
	if falsy(device) {
		return "No last used device found.", nil
	}
	d := asMap(device)
	curated := map[string]any{"user_device_id": d["userDeviceId"], "device_name": d["lastUsedDeviceName"],
		"device_key": d["lastUsedDeviceApplicationKey"], "user_profile_id": d["userProfileNumber"]}
	if ms, ok := asNumber(d["lastUsedDeviceUploadTime"]); ok && ms != 0 {
		// Server-local time, as datetime.fromtimestamp.
		curated["last_upload_time"] = time.UnixMilli(int64(ms)).In(time.Local).Format("2006-01-02 15:04:05")
	}
	if !falsy(d["imageUrl"]) {
		curated["image_url"] = d["imageUrl"]
	}
	return dropNil(curated), nil
}

func getDeviceSettings(ctx context.Context, s *Session, in deviceSettingsIn) (any, error) {
	fail := func(err error) (any, error) { return nil, fmt.Errorf("Error retrieving device settings: %w", err) }
	deviceID := in.DeviceID
	if deviceID == nil {
		lastUsed, err := s.Client.GetDeviceLastUsed(ctx)
		if err != nil {
			return fail(err)
		}
		if falsy(lastUsed) {
			return "No default device found. Pass device_id explicitly or register a device with Garmin Connect.", nil
		}
		deviceID = asMap(lastUsed)["userDeviceId"]
		if falsy(deviceID) {
			return "Default device has no userDeviceId. Pass device_id explicitly.", nil
		}
	}
	settings, err := s.Client.GetDeviceSettings(ctx, deviceID)
	if err != nil {
		return fail(err)
	}
	if falsy(settings) {
		return fmt.Sprintf("No settings found for device ID %s.", idString(deviceID)), nil
	}
	st := asMap(settings)
	curated := map[string]any{"device_id": st["deviceId"], "time_format": st["timeFormat"], "date_format": st["dateFormat"],
		"measurement_units": st["measurementUnits"]}
	for _, k := range [][2]string{{"key_tones_enabled", "keyTonesEnabled"}, {"key_vibration_enabled", "keyVibrationEnabled"}, {"alert_tones_enabled", "alertTonesEnabled"}} {
		if st[k[1]] != nil {
			curated[k[0]] = st[k[1]]
		}
	}
	if tracking := asMap(st["activityTracking"]); len(tracking) > 0 {
		info := map[string]any{}
		for _, k := range [][2]string{{"move_alert_enabled", "moveAlertEnabled"}, {"pulse_ox_sleep_tracking", "pulseOxSleepTrackingEnabled"},
			{"high_hr_alert_enabled", "highHrAlertEnabled"}, {"low_hr_alert_enabled", "lowHrAlertEnabled"}} {
			if tracking[k[1]] != nil {
				info[k[0]] = tracking[k[1]]
			}
		}
		if len(info) > 0 {
			curated["activity_tracking"] = info
		}
	}
	if alarms := asList(st["alarms"]); len(alarms) > 0 {
		enabled := 0
		for _, a := range alarms {
			if asMap(a)["alarmMode"] == "ON" {
				enabled++
			}
		}
		curated["alarm_count"], curated["enabled_alarm_count"] = len(alarms), enabled
	}
	return dropNil(curated), nil
}

func getPrimaryTrainingDevice(ctx context.Context, s *Session, _ noArgsIn) (any, error) {
	data, err := s.Client.GetPrimaryTrainingDevice(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving primary training device: %w", err)
	}
	if falsy(data) {
		return "No primary training device found.", nil
	}
	d := asMap(data)
	curated := map[string]any{"primary_device_id": asMap(d["PrimaryTrainingDevice"])["deviceId"]}
	if weights := asList(asMap(d["PrimaryTrainingDevices"])["deviceWeights"]); len(weights) > 0 {
		list := []any{}
		for _, w := range weights {
			dev := asMap(w)
			info := map[string]any{"device_id": dev["deviceId"], "display_name": dev["displayName"],
				"is_primary_wearable": dev["primaryWearableDevice"], "primary_training_capable": dev["primaryTrainingCapable"]}
			if !falsy(dev["imageUrl"]) {
				info["image_url"] = dev["imageUrl"]
			}
			list = append(list, info)
		}
		curated["training_devices"], curated["training_device_count"] = list, len(list)
	}
	if n := asMap(d["WearableDevices"])["wearableDeviceCount"]; !falsy(n) {
		curated["wearable_device_count"] = n
	}
	return curated, nil
}

func getDeviceSolarData(ctx context.Context, s *Session, in deviceSolarIn) (any, error) {
	noSolar := fmt.Sprintf(noSolarFmt, idString(in.DeviceID), in.Date)
	solar, err := s.Client.GetDeviceSolarData(ctx, in.DeviceID, in.Date)
	if err != nil {
		// The library raises this when the response has no deviceSolarInput, which is what a non-solar device returns.
		if strings.Contains(err.Error(), "No device solar input data received") {
			return noSolar, nil
		}
		return nil, fmt.Errorf("Error retrieving solar data: %w", err)
	}
	if falsy(solar) {
		return fmt.Sprintf("No solar data found for device ID %s on %s.", idString(in.DeviceID), in.Date), nil
	}
	days := asList(asMap(solar)["solarDailyDataDTOs"])
	if len(days) == 0 {
		return noSolar, nil
	}
	// solarInputReadings is a per-minute series (~1440 entries a day), so summarise it rather than returning it.
	curatedDays := []any{}
	for _, d := range days {
		day := asMap(d)
		var util []float64
		for _, r := range asList(day["solarInputReadings"]) {
			if u, ok := asNumber(asMap(r)["solarUtilization"]); ok {
				util = append(util, u)
			}
		}
		c := map[string]any{"date": day["localConnectDate"]}
		if ms, ok := asNumber(day["totalActivityTimeGainedMs"]); ok {
			c["activity_time_gained_minutes"] = round(ms/60000, 1)
		}
		if len(util) > 0 {
			sum, max, with := 0.0, util[0], 0
			for _, u := range util {
				sum += u
				max = maxFloat(max, u)
				if u > 0 {
					with++
				}
			}
			c["solar_utilization_avg_pct"] = round(sum/float64(len(util)), 2)
			c["solar_utilization_max_pct"] = max
			c["minutes_with_solar_input"] = with
		}
		curatedDays = append(curatedDays, dropNil(c))
	}
	return map[string]any{"device_id": in.DeviceID, "solar_data": curatedDays}, nil
}

func maxFloat(a, b float64) float64 {
	if b > a {
		return b
	}
	return a
}

func getDeviceAlarms(ctx context.Context, s *Session, _ noArgsIn) (any, error) {
	alarms, err := s.Client.GetDeviceAlarms(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving device alarms: %w", err)
	}
	if len(alarms) == 0 {
		return "No device alarms found.", nil
	}
	curated := []map[string]any{}
	for _, a := range alarms {
		alarm := asMap(a)
		minutes, hasTime := asNumber(alarm["alarmTime"])
		info := map[string]any{"alarm_id": alarm["alarmId"], "time": nil, "time_minutes": alarm["alarmTime"],
			"enabled": alarm["alarmMode"] == "ON", "days": getDefault(alarm, "alarmDays", []any{}), "sound": alarm["alarmSound"]}
		if hasTime {
			m := int(minutes)
			info["time"] = fmt.Sprintf("%02d:%02d", m/60, m%60)
		}
		if !falsy(alarm["backlight"]) {
			info["backlight"] = alarm["backlight"]
		}
		if !falsy(alarm["alarmMessage"]) {
			info["message"] = alarm["alarmMessage"]
		}
		curated = append(curated, info)
	}
	sort.SliceStable(curated, func(i, j int) bool {
		a, _ := asNumber(curated[i]["time_minutes"])
		b, _ := asNumber(curated[j]["time_minutes"])
		return a < b
	})
	enabled := 0
	for _, a := range curated {
		if a["enabled"] == true {
			enabled++
		}
	}
	return map[string]any{"total_alarms": len(curated), "enabled_alarms": enabled, "alarms": curated}, nil
}

func pushWorkoutToDevice(ctx context.Context, s *Session, in pushWorkoutIn) (any, error) {
	var deviceID any
	if in.DeviceID != nil {
		deviceID = *in.DeviceID
	}
	failed := func(msg string) map[string]any {
		return map[string]any{"status": "failed", "workout_id": in.WorkoutID, "device_id": deviceID, "message": msg}
	}
	run := func() (any, error) {
		if deviceID == nil {
			lastUsed, err := s.Client.GetDeviceLastUsed(ctx)
			if err != nil {
				return nil, err
			}
			deviceID = asMap(lastUsed)["userDeviceId"]
			if falsy(deviceID) {
				return map[string]any{"status": "failed", "workout_id": in.WorkoutID,
					"message": "No default device found. Pass device_id explicitly (see get_devices)."}, nil
			}
		}
		// Garmin's message queue shows this name on the device.
		workout, err := s.Client.GetWorkoutByID(ctx, in.WorkoutID)
		if err != nil {
			return nil, err
		}
		name := firstTruthy(asMap(workout)["workoutName"], fmt.Sprintf("Workout %d", in.WorkoutID))
		payload := []any{map[string]any{
			"deviceId": deviceID, "messageUrl": fmt.Sprintf("workout-service/workout/FIT/%d", in.WorkoutID), "messageType": "workouts",
			"groupName": nil, "messageName": name, "priority": 1, "fileType": "FIT", "metaDataId": in.WorkoutID,
		}}
		code, err := s.Client.PushWorkoutMessage(ctx, payload)
		if err != nil {
			return nil, err
		}
		if code == http.StatusOK || code == http.StatusNoContent {
			return map[string]any{"status": "success", "workout_id": in.WorkoutID, "workout_name": name, "device_id": deviceID,
				"message": fmt.Sprintf("Queued workout %d for device %s; it arrives on the device at its next sync", in.WorkoutID, idString(deviceID))}, nil
		}
		return map[string]any{"status": "failed", "workout_id": in.WorkoutID, "device_id": deviceID, "http_status": code,
			"message": fmt.Sprintf("Failed to push workout: HTTP %d", code)}, nil
	}
	v, err := run()
	if err != nil {
		if errors.Is(err, garmin.ErrSessionExpired) {
			return nil, err
		}
		return failed("Failed to push workout: " + err.Error()), nil
	}
	return v, nil
}
