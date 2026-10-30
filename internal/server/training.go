package server

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerTraining(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_cycling_ftp", description("get_cycling_ftp"), nil, func(ctx context.Context, s *Session, _ map[string]any) (any, error) {
		v, err := s.Client.GetJSON(ctx, "/biometric-service/biometric/latestFunctionalThresholdPower/CYCLING", nil)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving cycling FTP data: %w", err)
		}
		if list, ok := v.([]any); ok {
			v = nil
			for _, item := range list {
				if _, ok := item.(map[string]any); ok {
					v = item
					break
				}
			}
		}
		if empty(v) {
			return "No cycling FTP data found", nil
		}
		raw, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Error retrieving cycling FTP data: unexpected response")
		}
		out := map[string]any{}
		for k, source := range map[string]string{"sport": "sport", "functional_threshold_power_watts": "functionalThresholdPower", "calendar_date": "calendarDate", "is_stale": "isStale", "biometric_source_type": "biometricSourceType"} {
			if raw[source] != nil {
				out[k] = raw[source]
			}
		}
		return out, nil
	})
	addTool(srv, m, "get_hrv_data", description("get_hrv_data"), map[string]any{"date": map[string]any{"type": "string"}, "return_timeseries": map[string]any{"type": "boolean", "default": false}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		date, err := dateArgument(args, "date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving HRV data: %w", err)
		}
		v, err := s.Client.GetJSON(ctx, "/hrv-service/hrv/"+date, nil)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving HRV data: %w", err)
		}
		if empty(v) {
			return fmt.Sprintf("No HRV data found for %s.", date), nil
		}
		raw, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Error retrieving HRV data: unexpected response")
		}
		summary, _ := raw["hrvSummary"].(map[string]any)
		baseline, _ := summary["baseline"].(map[string]any)
		out := map[string]any{"date": date}
		if summary["calendarDate"] != nil && summary["calendarDate"] != "" {
			out["date"] = summary["calendarDate"]
		}
		for k, source := range map[string]string{"last_night_avg_hrv_ms": "lastNightAvg", "last_night_5min_high_hrv_ms": "lastNight5MinHigh", "weekly_avg_hrv_ms": "weeklyAvg", "status": "status", "feedback": "feedbackPhrase"} {
			if summary[source] != nil {
				out[k] = summary[source]
			}
		}
		for k, source := range map[string]string{"baseline_balanced_low_ms": "balancedLow", "baseline_balanced_upper_ms": "balancedUpper", "baseline_low_upper_ms": "lowUpper"} {
			if baseline[source] != nil {
				out[k] = baseline[source]
			}
		}
		for k, source := range map[string]string{"sleep_start": "sleepStartTimestampLocal", "sleep_end": "sleepEndTimestampLocal"} {
			if raw[source] != nil {
				out[k] = raw[source]
			}
		}
		if include, _ := args["return_timeseries"].(bool); include {
			readings := []any{}
			if list, ok := raw["hrvReadings"].([]any); ok {
				for _, v := range list {
					if r, ok := v.(map[string]any); ok {
						readings = append(readings, map[string]any{"time": r["readingTimeLocal"], "hrv_ms": r["hrvValue"]})
					}
				}
			}
			out["hrv_readings"] = readings
			out["readings_count"] = len(readings)
		}
		return out, nil
	}, "date")
}
