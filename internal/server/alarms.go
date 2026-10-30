package server

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"sort"
)

func curateAlarms(alarms []map[string]any) map[string]any {
	rows := make([]map[string]any, 0, len(alarms))
	enabled := 0
	for _, alarm := range alarms {
		var timeText any
		if n, ok := alarm["alarmTime"].(float64); ok {
			timeText = fmt.Sprintf("%02d:%02d", int(n)/60, int(n)%60)
		}
		on := alarm["alarmMode"] == "ON"
		if on {
			enabled++
		}
		days := alarm["alarmDays"]
		if days == nil {
			days = []any{}
		}
		row := map[string]any{"alarm_id": alarm["alarmId"], "time": timeText, "time_minutes": alarm["alarmTime"], "enabled": on, "days": days, "sound": alarm["alarmSound"]}
		if truthy(alarm["backlight"]) {
			row["backlight"] = alarm["backlight"]
		}
		if truthy(alarm["alarmMessage"]) {
			row["message"] = alarm["alarmMessage"]
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, _ := rows[i]["time_minutes"].(float64)
		b, _ := rows[j]["time_minutes"].(float64)
		return a < b
	})
	return map[string]any{"total_alarms": len(rows), "enabled_alarms": enabled, "alarms": rows}
}
func registerAlarms(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_device_alarms", description("get_device_alarms"), nil, func(ctx context.Context, s *Session, _ map[string]any) (any, error) {
		v, err := s.Client.GetJSON(ctx, "/device-service/deviceregistration/devices", nil)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving device alarms: %w", err)
		}
		devices, _ := v.([]any)
		alarms := []map[string]any{}
		for _, v := range devices {
			device, ok := v.(map[string]any)
			if !ok {
				continue
			}
			id, ok := device["deviceId"].(float64)
			if !ok || id <= 0 || id != float64(int64(id)) {
				return nil, fmt.Errorf("Error retrieving device alarms: invalid device ID")
			}
			v, err := s.Client.GetJSON(ctx, fmt.Sprintf("/device-service/deviceservice/device-info/settings/%d", int64(id)), nil)
			if err != nil {
				return nil, fmt.Errorf("Error retrieving device alarms: %w", err)
			}
			settings, _ := v.(map[string]any)
			list, _ := settings["alarms"].([]any)
			for _, v := range list {
				if alarm, ok := v.(map[string]any); ok {
					alarms = append(alarms, alarm)
				}
			}
		}
		if len(alarms) == 0 {
			return "No device alarms found.", nil
		}
		return curateAlarms(alarms), nil
	})
}
