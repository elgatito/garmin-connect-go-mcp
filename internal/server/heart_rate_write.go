package server

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerHeartRateWrite(srv *mcp.Server, m *manager) {
	schema := map[string]any{"sport": map[string]any{"type": "string"}, "max_hr": map[string]any{"type": "integer"}, "resting_hr": map[string]any{"type": "integer"}, "lactate_threshold_hr": map[string]any{"type": "integer"}, "calculation_method": map[string]any{"type": "string"}, "zone_boundaries": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}}}
	addTool(srv, m, "set_heart_rate_zones", description("set_heart_rate_zones"), schema, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		keys := []string{"max_hr", "resting_hr", "lactate_threshold_hr", "calculation_method", "zone_boundaries"}
		supplied := false
		for _, k := range keys {
			if args[k] != nil {
				supplied = true
			}
		}
		if !supplied {
			return "No fields to update — supply at least one of max_hr, resting_hr, lactate_threshold_hr, calculation_method, or zone_boundaries.", nil
		}
		invalid := func(message string) (any, error) { return "Invalid heart-rate zone settings: " + message, nil }
		sport, ok := args["sport"].(string)
		if !ok {
			return invalid("sport must be a non-empty Garmin sport key such as DEFAULT, RUNNING, or CYCLING")
		}
		sport = strings.NewReplacer("-", "_", " ", "_").Replace(strings.ToUpper(strings.TrimSpace(sport)))
		if sport == "GENERIC" {
			sport = "DEFAULT"
		}
		if !regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`).MatchString(sport) {
			return invalid("sport must be a non-empty Garmin sport key such as DEFAULT, RUNNING, or CYCLING")
		}
		tracker := newWriteTracker("set_heart_rate_zones", "Error updating heart-rate zones", m.opt.Log)
		v, err := s.Client.GetJSON(ctx, "/biometric-service/heartRateZones", nil)
		if err != nil {
			return nil, tracker.Fail(err)
		}
		zones, ok := v.([]any)
		if !ok {
			return invalid("Garmin returned an unexpected heart-rate-zone response")
		}
		var current, def map[string]any
		for _, v := range zones {
			z, ok := v.(map[string]any)
			if !ok {
				continue
			}
			if z["sport"] == sport {
				current = z
			}
			if z["sport"] == "DEFAULT" {
				def = z
			}
		}
		if current == nil {
			current = def
		}
		if current == nil {
			return fmt.Sprintf("Could not read current heart-rate zones for %s, and no DEFAULT profile is available to inherit — cannot apply update.", sport), nil
		}
		config := map[string]any{}
		for k, v := range current {
			config[k] = v
		}
		config["sport"] = sport
		for k, field := range map[string]string{"max_hr": "maxHeartRateUsed", "resting_hr": "restingHeartRateUsed", "lactate_threshold_hr": "lactateThresholdHeartRateUsed"} {
			if val := args[k]; val != nil {
				config[field] = val
				if k == "resting_hr" {
					config["restingHrAutoUpdateUsed"] = false
				}
			}
		}
		method := ""
		if value, ok := args["calculation_method"].(string); ok {
			method = strings.NewReplacer("-", "_", " ", "_").Replace(strings.ToUpper(strings.TrimSpace(value)))
			aliases := map[string]string{"HR_MAX": "HR_MAX", "MAX_HR": "HR_MAX", "MAXHR": "HR_MAX", "%MAX_HR": "HR_MAX", "HR_RESERVE": "HR_RESERVE", "HRR": "HR_RESERVE", "KARVONEN": "HR_RESERVE", "%HRR": "HR_RESERVE", "LACTATE_THRESHOLD": "LACTATE_THRESHOLD", "LTHR": "LACTATE_THRESHOLD", "%LTHR": "LACTATE_THRESHOLD", "CUSTOM": "CUSTOM_BPM", "CUSTOM_BPM": "CUSTOM_BPM", "BPM": "CUSTOM_BPM"}
			method = aliases[method]
			if method == "" {
				return invalid("calculation_method must be one of max_hr, hrr/karvonen, lthr, or custom_bpm")
			}
			apiMethod := method
			if method == "CUSTOM_BPM" {
				apiMethod = "HR_MAX"
			}
			config["trainingMethod"] = apiMethod
		}
		bounds, present := args["zone_boundaries"].([]any)
		if present {
			if len(bounds) != 5 {
				return invalid("zone_boundaries must contain exactly five BPM floors")
			}
			for i, v := range bounds {
				config[fmt.Sprintf("zone%dFloor", i+1)] = v
			}
		}
		if present && method != "CUSTOM_BPM" {
			return invalid("manual zone_boundaries require calculation_method='custom_bpm'")
		}
		if method == "CUSTOM_BPM" && !present {
			return invalid("calculation_method='custom_bpm' requires all five zone_boundaries")
		}
		valid := func(v any) bool { n, ok := v.(float64); return ok && n > 0 && n <= 300 && n == float64(int(n)) }
		for field, name := range map[string]string{"maxHeartRateUsed": "max_hr", "restingHeartRateUsed": "resting_hr", "lactateThresholdHeartRateUsed": "lactate_threshold_hr"} {
			if config[field] != nil && !valid(config[field]) {
				return invalid(name + " must be an integer BPM value from 1 to 300")
			}
		}
		if !valid(config["maxHeartRateUsed"]) {
			return invalid("the current or supplied max_hr must be an integer BPM value from 1 to 300")
		}
		max := config["maxHeartRateUsed"].(float64)
		if n, ok := config["restingHeartRateUsed"].(float64); ok && n >= max {
			return invalid("resting_hr must be lower than max_hr")
		}
		if n, ok := config["lactateThresholdHeartRateUsed"].(float64); ok && n > max {
			return invalid("lactate_threshold_hr must not exceed max_hr")
		}
		prev := float64(0)
		for i := 1; i <= 5; i++ {
			v := config[fmt.Sprintf("zone%dFloor", i)]
			if !valid(v) {
				return invalid("all five zone boundaries must be integer BPM values from 1 to 300")
			}
			n := v.(float64)
			if n <= prev {
				return invalid("zone boundaries must be strictly monotonically increasing")
			}
			prev = n
		}
		if prev > max {
			return invalid("zone boundaries must not exceed max_hr")
		}
		config["changeState"] = "CHANGED"
		tracker.Sent()
		_, err = s.Client.Do(ctx, garmin.Req{Method: http.MethodPut, Path: "/biometric-service/heartRateZones", JSON: []any{config}})
		if err != nil {
			return nil, tracker.Fail(err)
		}
		tracker.Committed()
		v, err = s.Client.GetJSON(ctx, "/biometric-service/heartRateZones", nil)
		if err == nil {
			if list, ok := v.([]any); ok {
				for _, v := range list {
					if z, ok := v.(map[string]any); ok && z["sport"] == sport {
						return z, nil
					}
				}
			}
			err = fmt.Errorf("No %s heart-rate zone profile was present on read-back", sport)
		}
		return map[string]any{"status": "success", "write_committed": true, "message": "Garmin accepted the update, but the saved profile could not be confirmed. Re-read the profile before another update.", "confirmation_error": err.Error(), "submitted_configuration": config}, nil
	}, "sport")
}
