package server

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const trainingStatusPath = "/metrics-service/metrics/trainingstatus/aggregated/"
const devicePath = "/device-service/deviceregistration/devices"

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
func deviceID(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func idValue(id string) any {
	if id == "" {
		return nil
	}
	if n, e := strconv.ParseInt(id, 10, 64); e == nil {
		return n
	}
	return id
}
func primaryDevice(devices any) string {
	for _, v := range slice(devices) {
		d := asMap(v)
		if d["primaryDevice"] == true {
			return deviceID(d["deviceId"])
		}
	}
	return ""
}
func slice(v any) []any { a, _ := v.([]any); return a }
func selectDevice(data any, devices any) (map[string]any, string, string, *bool) {
	m := asMap(data)
	if len(m) == 0 {
		return nil, "", "", nil
	}
	primary := primaryDevice(devices)
	// Garmin's JSON keys are device IDs. Sort for deterministic fallback.
	keys := sortedKeys(m)
	key := ""
	for _, k := range keys {
		d := asMap(m[k])
		if len(d) == 0 {
			continue
		}
		if key == "" {
			key = k
		}
		id := deviceID(d["deviceId"])
		if id == "" {
			id = k
		}
		if primary != "" && id == primary {
			key = k
			break
		}
	}
	if key == "" {
		return nil, "", "", nil
	}
	selected := asMap(m[key])
	id := deviceID(selected["deviceId"])
	if id == "" {
		id = key
	}
	if id != primary {
		for _, k := range keys {
			d := asMap(m[k])
			if d["primaryTrainingDevice"] == true {
				key = k
				selected = d
				id = deviceID(d["deviceId"])
				if id == "" {
					id = k
				}
				break
			}
		}
	}
	name := ""
	for _, v := range slice(devices) {
		d := asMap(v)
		if deviceID(d["deviceId"]) == id {
			name, _ = d["displayName"].(string)
			if name == "" {
				name, _ = d["productDisplayName"].(string)
			}
			break
		}
	}
	var isPrimary *bool
	if primary != "" && id != "" {
		b := primary == id
		isPrimary = &b
	}
	return selected, id, name, isPrimary
}
func trackerLoad(v any, date, id string) (any, string) {
	if id == "" {
		return nil, "tracker_id_unavailable"
	}
	rows := slice(v)
	if rows == nil {
		rows = []any{v}
	}
	var best map[string]any
	for _, x := range rows {
		d := asMap(x)
		if deviceID(d["deviceId"]) != id || d["calendarDate"] != date || d["acuteLoad"] == nil {
			continue
		}
		if best == nil || fmt.Sprint(d["timestampLocal"], d["timestamp"]) > fmt.Sprint(best["timestampLocal"], best["timestamp"]) {
			best = d
		}
	}
	if best == nil {
		return nil, "readiness_device_or_date_mismatch"
	}
	return best["acuteLoad"], ""
}
func copyPresent(dst, src map[string]any, fields map[string]string) {
	for target, key := range fields {
		if src[key] != nil {
			dst[target] = src[key]
		}
	}
}
func trainingMaps(data map[string]any) (map[string]any, map[string]any) {
	return asMap(asMap(data["mostRecentTrainingStatus"])["latestTrainingStatusData"]), asMap(asMap(data["mostRecentTrainingLoadBalance"])["metricsTrainingLoadBalanceDTOMap"])
}
func rounded(v any, places float64) any {
	n, ok := v.(float64)
	if !ok {
		return v
	}
	return math.RoundToEven(n*places) / places
}
func statusRange(args map[string]any) (string, string, time.Time, time.Time, error) {
	a, e := dateArgument(args, "start_date")
	if e != nil {
		return "", "", time.Time{}, time.Time{}, e
	}
	b, e := dateArgument(args, "end_date")
	if e != nil {
		return "", "", time.Time{}, time.Time{}, e
	}
	start, _ := time.Parse("2006-01-02", a)
	end, _ := time.Parse("2006-01-02", b)
	days := int(end.Sub(start).Hours()/24) + 1
	if days > 90 {
		return "", "", start, end, fmt.Errorf("Date range too large (%d days). Maximum is 90 days.", days)
	}
	if days < 1 {
		return "", "", start, end, fmt.Errorf("end_date must be on or after start_date.")
	}
	return a, b, start, end, nil
}
func registerTrainingStatus(srv *mcp.Server, m *manager) {
	dateSchema := map[string]any{"date": map[string]any{"type": "string"}}
	for _, name := range []string{"get_training_status", "get_training_load_balance"} {
		name := name
		addTool(srv, m, name, description(name), dateSchema, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
			date, e := dateArgument(args, "date")
			if e != nil {
				return nil, e
			}
			v, e := s.Client.GetJSON(ctx, trainingStatusPath+date, nil)
			if e != nil {
				return nil, fmt.Errorf("Error retrieving training status data: %w", e)
			}
			data := asMap(v)
			if len(data) == 0 {
				if name == "get_training_load_balance" {
					return fmt.Sprintf("No training load balance data found for %s.", date), nil
				}
				return fmt.Sprintf("No training status data found for %s.", date), nil
			}
			devices, _ := s.Client.GetJSON(ctx, devicePath, nil)
			latest, loads := trainingMaps(data)
			if name == "get_training_load_balance" {
				load, id, device, _ := selectDevice(loads, devices)
				if len(load) == 0 {
					return fmt.Sprintf("No training load balance data found for %s.", date), nil
				}
				out := map[string]any{"date": date}
				if load["calendarDate"] != nil {
					out["date"] = load["calendarDate"]
				}
				if id != "" {
					out["load_device_id"] = idValue(id)
				}
				if device != "" {
					out["load_device_name"] = device
				}
				if load["trainingBalanceFeedbackPhrase"] != nil {
					out["feedback"] = load["trainingBalanceFeedbackPhrase"]
				}
				for key, prefix := range map[string]string{"aerobic_low": "monthlyLoadAerobicLow", "aerobic_high": "monthlyLoadAerobicHigh", "anaerobic": "monthlyLoadAnaerobic"} {
					band := map[string]any{}
					copyPresent(band, load, map[string]string{"load": prefix, "target_min": prefix + "TargetMin", "target_max": prefix + "TargetMax"})
					if len(band) == 0 {
						continue
					}
					if n, ok := band["load"].(float64); ok {
						band["load"] = rounded(n, 10)
						low, lok := band["target_min"].(float64)
						high, hok := band["target_max"].(float64)
						if lok && hok {
							status := "within"
							if n < low {
								status = "below"
							} else if n > high {
								status = "above"
							}
							band["status"] = status
						}
					}
					out[key] = band
				}
				return out, nil
			}
			selected, id, device, primary := selectDevice(latest, devices)
			load := map[string]any{}
			loadID := ""
			if id == "" {
				load, loadID, _, _ = selectDevice(loads, devices)
			} else {
				for k, v := range loads {
					d := asMap(v)
					candidate := deviceID(d["deviceId"])
					if candidate == "" {
						candidate = k
					}
					if candidate == id {
						load = d
						loadID = id
						break
					}
				}
			}
			out := map[string]any{"date": date}
			if selected["calendarDate"] != nil {
				out["date"] = selected["calendarDate"]
			}
			if id != "" {
				out["reporting_device_id"] = idValue(id)
			}
			if device != "" {
				out["reporting_device_name"] = device
			}
			if loadID != "" {
				out["load_device_id"] = idValue(loadID)
			}
			copyPresent(out, selected, map[string]string{"training_status": "trainingStatus", "training_status_feedback": "trainingStatusFeedbackPhrase", "sport": "sport", "fitness_trend": "fitnessTrend"})
			vo := asMap(data["mostRecentVO2Max"])
			copyPresent(out, asMap(vo["generic"]), map[string]string{"vo2_max": "vo2MaxValue", "vo2_max_precise": "vo2MaxPreciseValue"})
			copyPresent(out, asMap(vo["cycling"]), map[string]string{"cycling_vo2_max": "vo2MaxValue", "cycling_vo2_max_precise": "vo2MaxPreciseValue"})
			copyPresent(out, load, map[string]string{"monthly_load_aerobic_low": "monthlyLoadAerobicLow", "monthly_load_aerobic_high": "monthlyLoadAerobicHigh", "monthly_load_anaerobic": "monthlyLoadAnaerobic", "training_balance_feedback": "trainingBalanceFeedbackPhrase"})
			if primary == nil || *primary {
				copyPresent(out, asMap(selected["acuteTrainingLoadDTO"]), map[string]string{"acute_load": "dailyTrainingLoadAcute", "chronic_load": "dailyTrainingLoadChronic", "load_ratio": "dailyAcuteChronicWorkloadRatio", "acwr_status": "acwrStatus", "acwr_percent": "acwrPercent", "optimal_chronic_load_min": "minTrainingLoadChronic", "optimal_chronic_load_max": "maxTrainingLoadChronic"})
			} else {
				ready, e := s.Client.GetJSON(ctx, "/metrics-service/metrics/trainingreadiness/"+date, nil)
				var value any
				reason := "readiness_unavailable"
				if e == nil {
					value, reason = trackerLoad(ready, date, primaryDevice(devices))
				}
				if value != nil {
					out["acute_load"] = value
					out["acute_load_source"] = "training_readiness"
				} else {
					out["acute_load_source"] = "unavailable"
					out["acute_load_unavailable_reason"] = reason
				}
			}
			if len(load) == 0 && len(loads) > 0 {
				out["load_source"] = "unavailable"
				out["load_unavailable_reason"] = "load_device_mismatch"
			}
			return out, nil
		}, "date")
	}
	addTool(srv, m, "get_training_load_trend", description("get_training_load_trend"), map[string]any{"start_date": map[string]any{"type": "string"}, "end_date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		start, end, a, b, e := statusRange(args)
		if e != nil {
			return nil, e
		}
		devices, deviceErr := s.Client.GetJSON(ctx, devicePath, nil)
		if deviceErr != nil && fatalTrendError(deviceErr) {
			return nil, fmt.Errorf("Error retrieving training load device data: %w", deviceErr)
		}
		trend := []any{}
		for day := a; !day.After(b); day = day.AddDate(0, 0, 1) {
			date := day.Format("2006-01-02")
			v, e := s.Client.GetJSON(ctx, trainingStatusPath+date, nil)
			if e != nil {
				return nil, fmt.Errorf("Error retrieving training load data for %s: %w", date, e)
			}
			data := asMap(v)
			if len(data) == 0 {
				continue
			}
			latest, _ := trainingMaps(data)
			selected, id, name, primary := selectDevice(latest, devices)
			dto := asMap(selected["acuteTrainingLoadDTO"])
			out := map[string]any{"date": date}
			if id != "" {
				out["reporting_device_id"] = idValue(id)
			}
			if name != "" {
				out["reporting_device_name"] = name
			}
			atl, ctl := dto["dailyTrainingLoadAcute"], dto["dailyTrainingLoadChronic"]
			if primary != nil && !*primary {
				ready, e := s.Client.GetJSON(ctx, "/metrics-service/metrics/trainingreadiness/"+date, nil)
				reason := "readiness_unavailable"
				atl = nil
				if e == nil {
					atl, reason = trackerLoad(ready, date, primaryDevice(devices))
				} else {
					if fatalTrendError(e) {
						return nil, fmt.Errorf("Error retrieving training load data for %s: %w", date, e)
					}
					out["readiness_error"] = e.Error()
				}
				ctl = nil
				if atl != nil {
					out["atl_source"] = "training_readiness"
				} else {
					out["atl_source"] = "unavailable"
					out["atl_unavailable_reason"] = reason
				}
			} else {
				for dst, src := range map[string]string{"acwr": "dailyAcuteChronicWorkloadRatio", "acwr_status": "acwrStatus", "acwr_percent": "acwrPercent", "optimal_chronic_load_min": "minTrainingLoadChronic", "optimal_chronic_load_max": "maxTrainingLoadChronic"} {
					if dto[src] != nil {
						scale := 10.0
						if dst == "acwr" {
							scale = 100
						}
						out[dst] = rounded(dto[src], scale)
					}
				}
			}
			if atl != nil {
				out["atl"] = rounded(atl, 10)
			}
			if ctl != nil {
				out["ctl"] = rounded(ctl, 10)
			}
			av, aok := atl.(float64)
			cv, cok := ctl.(float64)
			if aok && cok {
				out["tsb"] = rounded(cv-av, 10)
			}
			copyPresent(out, selected, map[string]string{"training_status": "trainingStatusFeedbackPhrase", "training_status_code": "trainingStatus", "fitness_trend": "fitnessTrend"})
			vo := asMap(asMap(data["mostRecentVO2Max"])["generic"])
			if vo["vo2MaxValue"] != nil {
				out["vo2_max"] = rounded(vo["vo2MaxValue"], 10)
			}
			if len(out) > 1 {
				trend = append(trend, out)
			}
		}
		if len(trend) == 0 {
			return fmt.Sprintf("No training load data found between %s and %s.", start, end), nil
		}
		return map[string]any{"start_date": start, "end_date": end, "days_with_data": len(trend), "trend": trend}, nil
	}, "start_date", "end_date")
}
