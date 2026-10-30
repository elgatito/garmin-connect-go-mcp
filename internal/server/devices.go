package server

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerDevices(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_devices", description("get_devices"), nil, func(ctx context.Context, s *Session, _ map[string]any) (any, error) {
		v, err := s.Client.GetJSON(ctx, "/device-service/deviceregistration/devices", nil)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving devices: %w", err)
		}
		if empty(v) {
			return "No devices found.", nil
		}
		rows, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("Error retrieving devices: unexpected response")
		}
		out := make([]any, 0, len(rows))
		for _, v := range rows {
			raw, ok := v.(map[string]any)
			if !ok {
				continue
			}
			row := map[string]any{}
			for k, source := range map[string]string{"device_id": "deviceId", "model": "partNumber", "manufacturer": "manufacturerName", "serial_number": "serialNumber", "software_version": "softwareVersionString", "status": "deviceStatusName", "last_sync_time": "lastSyncTime", "battery_status": "batteryStatus", "device_type": "deviceType", "is_primary": "primaryDevice"} {
				if raw[source] != nil {
					row[k] = raw[source]
				}
			}
			name := raw["displayName"]
			if name == nil || name == "" {
				name = raw["productDisplayName"]
			}
			if name != nil {
				row["device_name"] = name
			}
			out = append(out, row)
		}
		return out, nil
	})
	addTool(srv, m, "get_device_last_used", description("get_device_last_used"), nil, func(ctx context.Context, s *Session, _ map[string]any) (any, error) {
		v, err := s.Client.GetJSON(ctx, "/device-service/deviceservice/mylastused", nil)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving last used device: %w", err)
		}
		if empty(v) {
			return "No last used device found.", nil
		}
		raw, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Error retrieving last used device: unexpected response")
		}
		out := map[string]any{}
		for k, source := range map[string]string{"user_device_id": "userDeviceId", "device_name": "lastUsedDeviceName", "device_key": "lastUsedDeviceApplicationKey", "user_profile_id": "userProfileNumber", "image_url": "imageUrl"} {
			if raw[source] != nil && raw[source] != "" {
				out[k] = raw[source]
			}
		}
		if n, ok := raw["lastUsedDeviceUploadTime"].(float64); ok && n != 0 {
			out["last_upload_time"] = time.UnixMilli(int64(n)).Local().Format("2006-01-02 15:04:05")
		}
		return out, nil
	})
	addTool(srv, m, "get_primary_training_device", description("get_primary_training_device"), nil, func(ctx context.Context, s *Session, _ map[string]any) (any, error) {
		v, err := s.Client.GetJSON(ctx, "/web-gateway/device-info/primary-training-device", nil)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving primary training device: %w", err)
		}
		if empty(v) {
			return "No primary training device found.", nil
		}
		raw, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Error retrieving primary training device: unexpected response")
		}
		primary, _ := raw["PrimaryTrainingDevice"].(map[string]any)
		out := map[string]any{"primary_device_id": primary["deviceId"]}
		deviceGroup, _ := raw["PrimaryTrainingDevices"].(map[string]any)
		devices, _ := deviceGroup["deviceWeights"].([]any)
		if len(devices) > 0 {
			list := []any{}
			for _, v := range devices {
				d, ok := v.(map[string]any)
				if !ok {
					continue
				}
				entry := map[string]any{"device_id": d["deviceId"], "display_name": d["displayName"], "is_primary_wearable": d["primaryWearableDevice"], "primary_training_capable": d["primaryTrainingCapable"]}
				if d["imageUrl"] != nil && d["imageUrl"] != "" {
					entry["image_url"] = d["imageUrl"]
				}
				list = append(list, entry)
			}
			out["training_devices"] = list
			out["training_device_count"] = len(list)
		}
		wearable, _ := raw["WearableDevices"].(map[string]any)
		if count, ok := wearable["wearableDeviceCount"].(float64); ok && count != 0 {
			out["wearable_device_count"] = count
		}
		return out, nil
	})
	addTool(srv, m, "get_device_settings", description("get_device_settings"), map[string]any{"device_id": map[string]any{"anyOf": []any{map[string]any{"type": "integer"}, map[string]any{"type": "string"}}}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		id := args["device_id"]
		if id == nil {
			v, err := s.Client.GetJSON(ctx, "/device-service/deviceservice/mylastused", nil)
			if err != nil {
				return nil, fmt.Errorf("Error retrieving device settings: %w", err)
			}
			if empty(v) {
				return "No default device found. Pass device_id explicitly or register a device with Garmin Connect.", nil
			}
			data, _ := v.(map[string]any)
			id = data["userDeviceId"]
			if id == nil || id == float64(0) {
				return "Default device has no userDeviceId. Pass device_id explicitly.", nil
			}
		}
		var value int64
		var err error
		switch v := id.(type) {
		case string:
			value, err = strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		case float64:
			value = int64(v)
			if float64(value) != v {
				err = fmt.Errorf("invalid device ID")
			}
		default:
			err = fmt.Errorf("invalid device ID")
		}
		if err != nil || value <= 0 {
			return nil, fmt.Errorf("Error retrieving device settings: device_id must be a positive integer")
		}
		v, err := s.Client.GetJSON(ctx, fmt.Sprintf("/device-service/deviceservice/device-info/settings/%d", value), nil)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving device settings: %w", err)
		}
		if empty(v) {
			return fmt.Sprintf("No settings found for device ID %d.", value), nil
		}
		raw, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Error retrieving device settings: unexpected response")
		}
		out := map[string]any{}
		for k, src := range map[string]string{"device_id": "deviceId", "time_format": "timeFormat", "date_format": "dateFormat", "measurement_units": "measurementUnits", "key_tones_enabled": "keyTonesEnabled", "key_vibration_enabled": "keyVibrationEnabled", "alert_tones_enabled": "alertTonesEnabled"} {
			if raw[src] != nil {
				out[k] = raw[src]
			}
		}
		tracking, _ := raw["activityTracking"].(map[string]any)
		mapped := map[string]any{}
		for k, src := range map[string]string{"move_alert_enabled": "moveAlertEnabled", "pulse_ox_sleep_tracking": "pulseOxSleepTrackingEnabled", "high_hr_alert_enabled": "highHrAlertEnabled", "low_hr_alert_enabled": "lowHrAlertEnabled"} {
			if tracking[src] != nil {
				mapped[k] = tracking[src]
			}
		}
		if len(mapped) > 0 {
			out["activity_tracking"] = mapped
		}
		if alarms, ok := raw["alarms"].([]any); ok && len(alarms) > 0 {
			enabled := 0
			for _, v := range alarms {
				if alarm, ok := v.(map[string]any); ok && alarm["alarmMode"] == "ON" {
					enabled++
				}
			}
			out["alarm_count"] = len(alarms)
			out["enabled_alarm_count"] = enabled
		}
		return out, nil
	})

}
