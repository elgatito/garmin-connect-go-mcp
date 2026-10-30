package server

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/url"
	"time"
)

func lactateSpeed(v any) any {
	if n, ok := v.(float64); ok && n != 0 {
		return n * 10
	}
	return nil
}
func registerLactate(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_lactate_threshold", description("get_lactate_threshold"), map[string]any{"start_date": map[string]any{"type": "string"}, "end_date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		start, _ := args["start_date"].(string)
		end, _ := args["end_date"].(string)
		if (start != "") != (end != "") {
			return "Provide both start_date and end_date for lactate threshold history, or omit both for the latest values.", nil
		}
		if start != "" {
			if _, err := dateArgument(args, "start_date"); err != nil {
				return nil, fmt.Errorf("Error retrieving lactate threshold data: %w", err)
			}
			if _, err := dateArgument(args, "end_date"); err != nil {
				return nil, fmt.Errorf("Error retrieving lactate threshold data: %w", err)
			}
			out := map[string]any{"start_date": start, "end_date": end}
			for _, item := range []struct{ source, output, value string }{{"lactateThresholdSpeed", "speed_history", "speed_mps"}, {"lactateThresholdHeartRate", "heart_rate_history", "heart_rate_bpm"}, {"functionalThresholdPower", "power_history", "power_watts"}} {
				path := fmt.Sprintf("/biometric-service/stats/%s/range/%s/%s", item.source, start, end)
				v, err := s.Client.GetJSON(ctx, path, url.Values{"sport": {"RUNNING"}, "aggregation": {"daily"}, "aggregationStrategy": {"LATEST"}})
				if err != nil {
					return nil, fmt.Errorf("Error retrieving lactate threshold data: %w", err)
				}
				rows, _ := v.([]any)
				if len(rows) > 0 {
					history := []any{}
					for _, v := range rows {
						r, _ := v.(map[string]any)
						value := r["value"]
						if item.value == "speed_mps" {
							value = lactateSpeed(value)
						}
						history = append(history, map[string]any{"date": r["from"], item.value: value, "series": r["series"]})
					}
					out[item.output] = history
				}
			}
			return out, nil
		}
		today := time.Now().Local().Format("2006-01-02")
		p, err := s.Client.GetJSON(ctx, "/biometric-service/biometric/powerToWeight/latest/"+today, url.Values{"sport": {"Running"}})
		if err != nil {
			return nil, fmt.Errorf("Error retrieving lactate threshold data: %w", err)
		}
		if list, ok := p.([]any); ok {
			p = nil
			if len(list) > 0 {
				p = list[0]
			}
		}
		power, _ := p.(map[string]any)
		v, err := s.Client.GetJSON(ctx, "/biometric-service/biometric/latestLactateThreshold", nil)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving lactate threshold data: %w", err)
		}
		rows, _ := v.([]any)
		hr := map[string]any{}
		for _, v := range rows {
			row, _ := v.(map[string]any)
			if row["speed"] != nil {
				hr["speed"] = row["speed"]
				hr["calendarDate"] = row["calendarDate"]
			}
			heart := row["heartRate"]
			if !truthy(heart) {
				heart = row["hearRate"]
			}
			if heart != nil {
				hr["heartRate"] = heart
			}
			if row["heartRateCycling"] != nil {
				hr["heartRateCycling"] = row["heartRateCycling"]
			}
		}
		out := map[string]any{}
		for k, v := range map[string]any{"lactate_threshold_speed_mps": lactateSpeed(hr["speed"]), "lactate_threshold_heart_rate_bpm": hr["heartRate"], "heart_rate_cycling_bpm": hr["heartRateCycling"], "speed_hr_date": hr["calendarDate"], "functional_threshold_power_watts": power["functionalThresholdPower"], "weight_kg": power["weight"], "power_to_weight": power["powerToWeight"], "sport": power["sport"], "power_date": power["calendarDate"], "is_stale": power["isStale"]} {
			if v != nil {
				out[k] = v
			}
		}
		return out, nil
	})
}
