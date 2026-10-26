package server

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

const (
	getDevicesDescription = "Get all Garmin devices associated with the user account"

	getDeviceLastUsedDescription = "Get information about the last used Garmin device"

	getDeviceSettingsDescription = `Get settings for a specific Garmin device

Returns device configuration including time/date format, units,
activity tracking settings, and alarm information.`

	getPrimaryTrainingDeviceDescription = `Get information about the primary training device

Returns details about the device designated as primary for training
metrics, along with other wearable devices on the account.`

	getDeviceSolarDataDescription = `Get solar data for a specific device

Returns a per-day summary of solar charging for devices with solar panels
(e.g., Instinct Solar, Fenix Solar): activity time gained from solar, and
the average/peak solar utilization and minutes with solar input across the
day's per-minute readings. Only applicable to solar-capable devices.`

	getDeviceAlarmsDescription = `Get alarms from all Garmin devices

Returns all configured alarms with their schedules, sounds, and enabled status.`

	pushWorkoutToDeviceDescription = `Send a workout from the workout library to a Garmin device

Queues the workout in Garmin Connect's device message queue, the same
as "Send to Device" in the Garmin Connect app. The watch picks it up
at its next sync with Garmin Connect (phone app or Wi-Fi), so it does
not appear on the watch immediately. Scheduling a workout does not
need this: scheduled workouts reach the watch's calendar on their own.

Each call queues another copy, so do not push the same workout twice.`
)

type getDeviceSettingsInput struct {
	Account
	DeviceID any `json:"device_id,omitempty" jsonschema:"Device ID (optional; defaults to the most recently used device when omitted; can be obtained from get_devices or get_device_last_used)"`
}

type deviceSolarDataInput struct {
	Account
	DeviceID any    `json:"device_id" jsonschema:"Device ID (can be obtained from get_devices)"`
	Date     string `json:"date" jsonschema:"Date in YYYY-MM-DD format"`
}

type pushWorkoutToDeviceInput struct {
	Account
	WorkoutID int64 `json:"workout_id" jsonschema:"ID of the workout to send (from get_workouts or upload_workout)"`
	DeviceID  any   `json:"device_id,omitempty" jsonschema:"Device ID (optional; defaults to the most recently used device; can be obtained from get_devices or get_device_last_used)"`
}

func registerDeviceTools(r *registry) {
	addTool(r, "get_devices", getDevicesDescription, getDevices)
	addTool(r, "get_device_last_used", getDeviceLastUsedDescription, getDeviceLastUsed)
	addTool(r, "get_device_settings", getDeviceSettingsDescription, getDeviceSettings)
	addTool(r, "get_primary_training_device", getPrimaryTrainingDeviceDescription, getPrimaryTrainingDevice)
	addTool(r, "get_device_solar_data", getDeviceSolarDataDescription, getDeviceSolarData)
	addTool(r, "get_device_alarms", getDeviceAlarmsDescription, getDeviceAlarms)
	addTool(r, "push_workout_to_device", pushWorkoutToDeviceDescription, pushWorkoutToDevice)
}

func firstStringOf(values ...any) any {
	for _, value := range values {
		if s, ok := value.(string); ok && s != "" {
			return s
		}
	}
	return nil
}

func getDevices(ctx context.Context, s *Session, _ noArgsInput) (any, error) {
	devices, err := s.Client.GetDevices(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving devices: %w", err)
	}
	list := asList(devices)
	if len(list) == 0 {
		return "No devices found.", nil
	}
	curated := make([]map[string]any, 0, len(list))
	for _, item := range list {
		device := asMap(item)
		info := map[string]any{
			"device_id":        device["deviceId"],
			"device_name":      firstStringOf(device["displayName"], device["productDisplayName"]),
			"model":            device["partNumber"],
			"manufacturer":     device["manufacturerName"],
			"serial_number":    device["serialNumber"],
			"software_version": device["softwareVersionString"],
			"status":           device["deviceStatusName"],
			"last_sync_time":   device["lastSyncTime"],
			"battery_status":   device["batteryStatus"],
		}
		if truthy(device["deviceType"]) {
			info["device_type"] = device["deviceType"]
		}
		if device["primaryDevice"] != nil {
			info["is_primary"] = device["primaryDevice"]
		}
		curated = append(curated, pruneNil(info))
	}
	return curated, nil
}

func getDeviceLastUsed(ctx context.Context, s *Session, _ noArgsInput) (any, error) {
	deviceValue, err := s.Client.GetDeviceLastUsed(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving last used device: %w", err)
	}
	if !truthy(deviceValue) {
		return "No last used device found.", nil
	}
	device := asMap(deviceValue)
	curated := map[string]any{
		"user_device_id":  device["userDeviceId"],
		"device_name":     device["lastUsedDeviceName"],
		"device_key":      device["lastUsedDeviceApplicationKey"],
		"user_profile_id": device["userProfileNumber"],
	}
	if ms, ok := numberOf(device["lastUsedDeviceUploadTime"]); ok && ms != 0 {
		curated["last_upload_time"] = time.UnixMilli(int64(ms)).Local().Format("2006-01-02 15:04:05")
	}
	if truthy(device["imageUrl"]) {
		curated["image_url"] = device["imageUrl"]
	}
	return pruneNil(curated), nil
}

func getDeviceSettings(ctx context.Context, s *Session, in getDeviceSettingsInput) (any, error) {
	deviceID := in.DeviceID
	if deviceID == nil || !truthy(deviceID) {
		lastUsedValue, err := s.Client.GetDeviceLastUsed(ctx)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving device settings: %w", err)
		}
		if !truthy(lastUsedValue) {
			return "No default device found. Pass device_id explicitly or register a device with Garmin Connect.", nil
		}
		deviceID = asMap(lastUsedValue)["userDeviceId"]
		if !truthy(deviceID) {
			return "Default device has no userDeviceId. Pass device_id explicitly.", nil
		}
	}

	settingsValue, err := s.Client.GetDeviceSettings(ctx, deviceID)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving device settings: %w", err)
	}
	if !truthy(settingsValue) {
		return fmt.Sprintf("No settings found for device ID %s.", numToStr(deviceID)), nil
	}
	settings := asMap(settingsValue)

	curated := map[string]any{
		"device_id":         settings["deviceId"],
		"time_format":       settings["timeFormat"],
		"date_format":       settings["dateFormat"],
		"measurement_units": settings["measurementUnits"],
	}
	for key, target := range map[string]string{
		"keyTonesEnabled":     "key_tones_enabled",
		"keyVibrationEnabled": "key_vibration_enabled",
		"alertTonesEnabled":   "alert_tones_enabled",
	} {
		if settings[key] != nil {
			curated[target] = settings[key]
		}
	}

	if tracking := asMap(settings["activityTracking"]); len(tracking) > 0 {
		trackingInfo := map[string]any{}
		for key, target := range map[string]string{
			"moveAlertEnabled":            "move_alert_enabled",
			"pulseOxSleepTrackingEnabled": "pulse_ox_sleep_tracking",
			"highHrAlertEnabled":          "high_hr_alert_enabled",
			"lowHrAlertEnabled":           "low_hr_alert_enabled",
		} {
			if tracking[key] != nil {
				trackingInfo[target] = tracking[key]
			}
		}
		if len(trackingInfo) > 0 {
			curated["activity_tracking"] = trackingInfo
		}
	}

	if alarms := asList(settings["alarms"]); len(alarms) > 0 {
		enabled := 0
		for _, alarm := range alarms {
			if asMap(alarm)["alarmMode"] == "ON" {
				enabled++
			}
		}
		curated["alarm_count"] = len(alarms)
		curated["enabled_alarm_count"] = enabled
	}
	return pruneNil(curated), nil
}

func getPrimaryTrainingDevice(ctx context.Context, s *Session, _ noArgsInput) (any, error) {
	dataValue, err := s.Client.GetPrimaryTrainingDevice(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving primary training device: %w", err)
	}
	if !truthy(dataValue) {
		return "No primary training device found.", nil
	}
	data := asMap(dataValue)
	primaryDeviceID := asMap(data["PrimaryTrainingDevice"])["deviceId"]
	primaryDevices := asList(asMap(data["PrimaryTrainingDevices"])["deviceWeights"])

	curated := map[string]any{"primary_device_id": primaryDeviceID}
	if len(primaryDevices) > 0 {
		devicesList := make([]map[string]any, 0, len(primaryDevices))
		for _, item := range primaryDevices {
			device := asMap(item)
			info := map[string]any{
				"device_id":                device["deviceId"],
				"display_name":             device["displayName"],
				"is_primary_wearable":      device["primaryWearableDevice"],
				"primary_training_capable": device["primaryTrainingCapable"],
			}
			if truthy(device["imageUrl"]) {
				info["image_url"] = device["imageUrl"]
			}
			devicesList = append(devicesList, info)
		}
		curated["training_devices"] = devicesList
		curated["training_device_count"] = len(devicesList)
	}
	if wearable := asMap(data["WearableDevices"]); truthy(wearable["wearableDeviceCount"]) {
		curated["wearable_device_count"] = wearable["wearableDeviceCount"]
	}
	return curated, nil
}

func roundHalfUp(v float64, places int) float64 {
	pow := 1.0
	for i := 0; i < places; i++ {
		pow *= 10
	}
	return float64(int(v*pow+0.5)) / pow
}

func getDeviceSolarData(ctx context.Context, s *Session, in deviceSolarDataInput) (any, error) {
	noSolar := fmt.Sprintf(
		"No solar data available for device ID %s on %s. This device may not have solar capabilities.",
		numToStr(in.DeviceID), in.Date,
	)
	solarValue, err := s.Client.GetDeviceSolarData(ctx, in.DeviceID, in.Date)
	if err != nil {
		if err.Error() == "No device solar input data received" {
			return noSolar, nil
		}
		return nil, fmt.Errorf("Error retrieving solar data: %w", err)
	}
	if !truthy(solarValue) {
		return fmt.Sprintf("No solar data found for device ID %s on %s.", numToStr(in.DeviceID), in.Date), nil
	}
	daily := asList(asMap(solarValue)["solarDailyDataDTOs"])
	if len(daily) == 0 {
		return noSolar, nil
	}

	curatedDays := make([]map[string]any, 0, len(daily))
	for _, item := range daily {
		day := asMap(item)
		readings := asList(day["solarInputReadings"])
		utilization := make([]float64, 0, len(readings))
		for _, reading := range readings {
			if value, ok := numberOf(asMap(reading)["solarUtilization"]); ok {
				utilization = append(utilization, value)
			}
		}

		curated := map[string]any{"date": day["localConnectDate"]}
		if gained, ok := numberOf(day["totalActivityTimeGainedMs"]); ok {
			curated["activity_time_gained_minutes"] = roundHalfUp(gained/60000, 1)
		}
		if len(utilization) > 0 {
			sum := 0.0
			maxValue := utilization[0]
			minutes := 0
			for _, value := range utilization {
				sum += value
				if value > maxValue {
					maxValue = value
				}
				if value > 0 {
					minutes++
				}
			}
			curated["solar_utilization_avg_pct"] = roundHalfUp(sum/float64(len(utilization)), 2)
			curated["solar_utilization_max_pct"] = maxValue
			curated["minutes_with_solar_input"] = minutes
		}
		curatedDays = append(curatedDays, pruneNil(curated))
	}
	return map[string]any{"device_id": in.DeviceID, "solar_data": curatedDays}, nil
}

func formatAlarmTime(minutes any) any {
	f, ok := numberOf(minutes)
	if !ok || minutes == nil {
		return nil
	}
	total := int(f)
	return fmt.Sprintf("%02d:%02d", total/60, total%60)
}

func getDeviceAlarms(ctx context.Context, s *Session, _ noArgsInput) (any, error) {
	alarmsValue, err := s.Client.GetDeviceAlarms(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving device alarms: %w", err)
	}
	if len(alarmsValue) == 0 {
		return "No device alarms found.", nil
	}
	curated := make([]map[string]any, 0, len(alarmsValue))
	for _, item := range alarmsValue {
		alarm := asMap(item)
		alarmTimeMinutes := alarm["alarmTime"]
		days := any([]any{})
		if value, ok := alarm["alarmDays"]; ok {
			days = value
		}
		info := map[string]any{
			"alarm_id":     alarm["alarmId"],
			"time":         formatAlarmTime(alarmTimeMinutes),
			"time_minutes": alarmTimeMinutes,
			"enabled":      alarm["alarmMode"] == "ON",
			"days":         days,
			"sound":        alarm["alarmSound"],
		}
		if truthy(alarm["backlight"]) {
			info["backlight"] = alarm["backlight"]
		}
		if truthy(alarm["alarmMessage"]) {
			info["message"] = alarm["alarmMessage"]
		}
		curated = append(curated, info)
	}
	sort.SliceStable(curated, func(i, j int) bool {
		return asFloat(curated[i]["time_minutes"]) < asFloat(curated[j]["time_minutes"])
	})
	enabledCount := 0
	for _, alarm := range curated {
		if alarm["enabled"] == true {
			enabledCount++
		}
	}
	return map[string]any{
		"total_alarms":   len(curated),
		"enabled_alarms": enabledCount,
		"alarms":         curated,
	}, nil
}

func pushWorkoutToDevice(ctx context.Context, s *Session, in pushWorkoutToDeviceInput) (any, error) {
	deviceID := in.DeviceID
	if deviceID == nil {
		lastUsedValue, _ := s.Client.GetDeviceLastUsed(ctx)
		deviceID = asMap(lastUsedValue)["userDeviceId"]
		if !truthy(deviceID) {
			return map[string]any{
				"status":     "failed",
				"workout_id": in.WorkoutID,
				"message":    "No default device found. Pass device_id explicitly (see get_devices).",
			}, nil
		}
	}

	if _, err := garmin.ValidateDeviceID(deviceID); err != nil {
		return failedWorkoutPush(in.WorkoutID, deviceID, fmt.Sprintf("Failed to push workout: %v", err)), nil
	}

	workoutValue, err := s.Client.GetWorkoutByID(ctx, in.WorkoutID)
	if err != nil {
		return failedWorkoutPush(in.WorkoutID, deviceID, fmt.Sprintf("Failed to push workout: %v", err)), nil
	}
	workout := asMap(workoutValue)
	workoutName, _ := workout["workoutName"].(string)
	if workoutName == "" {
		workoutName = fmt.Sprintf("Workout %d", in.WorkoutID)
	}

	tracker := newWriteTracker("push_workout_to_device", s.logWriter()).
		withPrefix("Error pushing workout to device")
	tracker.Sent()
	if _, err := s.Client.PushWorkoutToDevice(ctx, deviceID, in.WorkoutID, workoutName); err != nil {
		var api *garmin.APIError
		if errors.As(err, &api) {
			_ = tracker.Fail(err)
			return map[string]any{
				"status":      "failed",
				"workout_id":  in.WorkoutID,
				"device_id":   deviceID,
				"http_status": api.Status,
				"message":     fmt.Sprintf("Failed to push workout: HTTP %d", api.Status),
			}, nil
		}
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	return map[string]any{
		"status":       "success",
		"workout_id":   in.WorkoutID,
		"workout_name": workoutName,
		"device_id":    deviceID,
		"message": fmt.Sprintf(
			"Queued workout %d for device %s; it arrives on the device at its next sync",
			in.WorkoutID, numToStr(deviceID),
		),
	}, nil
}

func failedWorkoutPush(workoutID int64, deviceID any, message string) map[string]any {
	return map[string]any{
		"status":     "failed",
		"workout_id": workoutID,
		"device_id":  deviceID,
		"message":    message,
	}
}
