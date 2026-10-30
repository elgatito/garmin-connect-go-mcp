package server

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"math"
	"time"
)

func registerRecoveryTrends(srv *mcp.Server, m *manager) {
	for _, item := range []struct{ name, path, label string }{{"get_hrv_trend", "/hrv-service/hrv/", "HRV"}, {"get_respiration_trend", "/wellness-service/wellness/daily/respiration/", "respiration"}} {
		item := item
		addTool(srv, m, item.name, description(item.name), map[string]any{"start_date": map[string]any{"type": "string"}, "end_date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
			start, err := dateArgument(args, "start_date")
			if err != nil {
				return nil, fmt.Errorf("Invalid date format: %w. Use YYYY-MM-DD.", err)
			}
			end, err := dateArgument(args, "end_date")
			if err != nil {
				return nil, fmt.Errorf("Invalid date format: %w. Use YYYY-MM-DD.", err)
			}
			a, _ := time.Parse("2006-01-02", start)
			b, _ := time.Parse("2006-01-02", end)
			days := int(b.Sub(a).Hours()/24) + 1
			if days > 30 {
				return fmt.Sprintf("Date range too large (%d days). Maximum is 30 days.", days), nil
			}
			if days < 1 {
				return "end_date must be on or after start_date.", nil
			}
			trend := []any{}
			sum := float64(0)
			count := 0
			for day := a; !day.After(b); day = day.AddDate(0, 0, 1) {
				date := day.Format("2006-01-02")
				v, err := s.Client.GetJSON(ctx, item.path+date, nil)
				if err != nil {
					return nil, fmt.Errorf("Error retrieving %s data for %s: %w", item.label, date, err)
				}
				data, _ := v.(map[string]any)
				if len(data) == 0 {
					continue
				}
				fields := map[string]string{}
				source := data
				averageKey := ""
				if item.name == "get_hrv_trend" {
					source, _ = data["hrvSummary"].(map[string]any)
					fields = map[string]string{"last_night_avg_hrv_ms": "lastNightAvg", "weekly_avg_hrv_ms": "weeklyAvg", "last_night_5min_high_hrv_ms": "lastNight5MinHigh", "status": "status", "feedback": "feedbackPhrase"}
					averageKey = "last_night_avg_hrv_ms"
				} else {
					fields = map[string]string{"avg_waking_breaths_per_min": "avgWakingRespirationValue", "avg_sleep_breaths_per_min": "avgSleepRespirationValue", "highest_breaths_per_min": "highestRespirationValue", "lowest_breaths_per_min": "lowestRespirationValue"}
					averageKey = "avg_sleep_breaths_per_min"
				}
				entry := map[string]any{"date": date}
				for k, src := range fields {
					if value := source[src]; value != nil {
						if n, ok := value.(float64); ok {
							value = math.RoundToEven(n*10) / 10
						}
						if (k == "status" || k == "feedback") && !truthy(value) {
							continue
						}
						entry[k] = value
					}
				}
				if len(entry) > 1 {
					trend = append(trend, entry)
					if n, ok := entry[averageKey].(float64); ok {
						sum += n
						count++
					}
				}
			}
			if len(trend) == 0 {
				return fmt.Sprintf("No %s data found between %s and %s.", item.label, start, end), nil
			}
			var average any
			if count > 0 {
				average = math.RoundToEven(sum/float64(count)*10) / 10
			}
			result := map[string]any{"start_date": start, "end_date": end, "days_with_data": len(trend), "trend": trend}
			if item.name == "get_hrv_trend" {
				result["period_avg_hrv_ms"] = average
			} else {
				result["period_avg_sleep_breaths_per_min"] = average
			}
			return result, nil
		}, "start_date", "end_date")
	}
}
