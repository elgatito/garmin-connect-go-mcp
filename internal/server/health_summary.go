package server

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"math"
	"net/url"
)

func valuesFromSeries(v any) []float64 {
	list, _ := v.([]any)
	out := []float64{}
	for _, item := range list {
		row, ok := item.([]any)
		if !ok || len(row) < 2 {
			continue
		}
		n, ok := row[1].(float64)
		if ok && n > 0 {
			out = append(out, n)
		}
	}
	return out
}
func healthSummary(name string, data map[string]any) map[string]any {
	fields := map[string]string{}
	switch name {
	case "get_heart_rates_summary":
		fields = map[string]string{"date": "calendarDate", "max_heart_rate_bpm": "maxHeartRate", "min_heart_rate_bpm": "minHeartRate", "resting_heart_rate_bpm": "restingHeartRate", "last_7_days_avg_resting_hr": "lastSevenDaysAvgRestingHeartRate"}
	case "get_stress_summary":
		fields = map[string]string{"date": "calendarDate", "max_stress_level": "maxStressLevel", "avg_stress_level": "avgStressLevel"}
	case "get_respiration_summary":
		fields = map[string]string{"date": "calendarDate", "lowest_breaths_per_min": "lowestRespirationValue", "highest_breaths_per_min": "highestRespirationValue", "avg_waking_breaths_per_min": "avgWakingRespirationValue", "avg_sleep_breaths_per_min": "avgSleepRespirationValue"}
	}
	out := remapFields(data, fields)
	if name == "get_heart_rates_summary" {
		values := valuesFromSeries(data["heartRateValues"])
		if len(values) > 0 {
			sum := float64(0)
			for _, v := range values {
				sum += v
			}
			out["avg_heart_rate_bpm"] = math.RoundToEven(sum/float64(len(values))*10) / 10
			out["data_points_count"] = len(values)
		}
	}
	if name == "get_stress_summary" && !empty(data["stressValuesArray"]) {
		values := valuesFromSeries(data["stressValuesArray"])
		counts := [4]int{}
		for _, v := range values {
			switch {
			case v < 26:
				counts[0]++
			case v < 51:
				counts[1]++
			case v < 76:
				counts[2]++
			default:
				counts[3]++
			}
		}
		denom := len(values)
		if denom == 0 {
			denom = 1
		}
		for i, key := range []string{"rest_percent", "low_stress_percent", "medium_stress_percent", "high_stress_percent"} {
			out[key] = math.RoundToEven(float64(counts[i])*1000/float64(denom)) / 10
		}
		out["data_points_count"] = len(values)
	}
	return out
}
func registerHealthSummaries(srv *mcp.Server, m *manager) {
	for _, item := range []struct{ name, path, label string }{{"get_heart_rates_summary", "/wellness-service/wellness/dailyHeartRate/", "heart rate"}, {"get_stress_summary", "/wellness-service/wellness/dailyStress/", "stress"}, {"get_respiration_summary", "/wellness-service/wellness/daily/respiration/", "respiration"}} {
		item := item
		addTool(srv, m, item.name, description(item.name), map[string]any{"date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
			date, err := dateArgument(args, "date")
			if err != nil {
				return nil, fmt.Errorf("Error retrieving %s summary: %w", item.label, err)
			}
			path := item.path + date
			var query url.Values
			if item.name == "get_heart_rates_summary" {
				p, err := s.Client.Profile(ctx)
				if err != nil {
					return nil, fmt.Errorf("Error retrieving heart rate summary: %w", err)
				}
				path = item.path + url.PathEscape(p.DisplayName)
				query = url.Values{"date": {date}}
			}
			v, err := s.Client.GetJSON(ctx, path, query)
			if err != nil {
				return nil, fmt.Errorf("Error retrieving %s summary: %w", item.label, err)
			}
			if empty(v) {
				return fmt.Sprintf("No %s data found for %s", item.label, date), nil
			}
			raw, ok := v.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("Error retrieving %s summary: unexpected response", item.label)
			}
			return healthSummary(item.name, raw), nil
		}, "date")
	}
}
