package server

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func sleepSummary(data map[string]any) map[string]any {
	out := map[string]any{}
	daily, _ := data["dailySleepDTO"].(map[string]any)
	for k, src := range map[string]string{"sleep_seconds": "sleepTimeSeconds", "nap_seconds": "napTimeSeconds", "sleep_start": "sleepStartTimestampGMT", "sleep_end": "sleepEndTimestampGMT", "deep_sleep_seconds": "deepSleepSeconds", "light_sleep_seconds": "lightSleepSeconds", "rem_sleep_seconds": "remSleepSeconds", "awake_seconds": "awakeSleepSeconds", "awake_count": "awakeCount", "avg_sleep_stress": "avgSleepStress"} {
		if daily[src] != nil {
			out[k] = daily[src]
		}
	}
	scores, _ := daily["sleepScores"].(map[string]any)
	overall, _ := scores["overall"].(map[string]any)
	if overall["value"] != nil {
		out["sleep_score"] = overall["value"]
	}
	if overall["qualifierKey"] != nil {
		out["sleep_score_qualifier"] = overall["qualifierKey"]
	}
	for k, src := range map[string]string{"restless_moments_count": "restlessMomentsCount", "resting_heart_rate_bpm": "restingHeartRate"} {
		v, exists := data[src]
		if !exists {
			v = daily[src]
		}
		if v != nil {
			out[k] = v
		}
	}
	spo2, _ := data["wellnessSpO2SleepSummaryDTO"].(map[string]any)
	for k, keys := range map[string][]string{"avg_spo2_percent": {"averageSPO2", "averageSpo2"}, "lowest_spo2_percent": {"lowestSPO2", "lowestSpo2"}} {
		v, exists := spo2[keys[0]]
		if !exists {
			v = spo2[keys[1]]
		}
		if v != nil {
			out[k] = v
		}
	}
	if data["avgOvernightHrv"] != nil {
		out["avg_overnight_hrv"] = data["avgOvernightHrv"]
	}
	if total, ok := out["sleep_seconds"].(float64); ok && total > 0 {
		out["sleep_hours"] = math.RoundToEven(total/36) / 100
		for field, key := range map[string]string{"deep_sleep_seconds": "deep_sleep_percent", "light_sleep_seconds": "light_sleep_percent", "rem_sleep_seconds": "rem_sleep_percent"} {
			if n, ok := out[field].(float64); ok {
				out[key] = math.RoundToEven(n/total*1000) / 10
			}
		}
	}
	return out
}
func sleepData(ctx context.Context, s *Session, date string) (any, error) {
	p, err := s.Client.Profile(ctx)
	if err != nil {
		return nil, err
	}
	return s.Client.GetJSON(ctx, "/wellness-service/wellness/dailySleepData/"+url.PathEscape(p.DisplayName), url.Values{"date": {date}, "nonSleepBufferMinutes": {"60"}})
}
func registerSleep(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_sleep_summary", description("get_sleep_summary"), map[string]any{"date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		date, err := dateArgument(args, "date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving sleep summary: %w", err)
		}
		v, err := sleepData(ctx, s, date)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving sleep summary: %w", err)
		}
		if empty(v) {
			return fmt.Sprintf("No sleep summary found for %s", date), nil
		}
		data, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Error retrieving sleep summary: unexpected response")
		}
		return sleepSummary(data), nil
	}, "date")
	addTool(srv, m, "get_sleep_summary_range", description("get_sleep_summary_range"), map[string]any{"start_date": map[string]any{"type": "string"}, "end_date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
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
		if days > 90 {
			return fmt.Sprintf("Date range too large (%d days). Maximum is 90 days.", days), nil
		}
		if days < 1 {
			return "end_date must be on or after start_date.", nil
		}
		nights := []any{}
		for date := a; !date.After(b); date = date.AddDate(0, 0, 1) {
			day := date.Format("2006-01-02")
			v, err := sleepData(ctx, s, day)
			if err != nil {
				return nil, fmt.Errorf("Error retrieving sleep data for %s: %w", day, err)
			}
			if raw, ok := v.(map[string]any); ok && len(raw) > 0 {
				entry := sleepSummary(raw)
				if len(entry) > 0 {
					entry["date"] = day
					nights = append(nights, entry)
				}
			}
		}
		if len(nights) == 0 {
			return fmt.Sprintf("No sleep data found between %s and %s.", start, end), nil
		}
		return map[string]any{"start_date": start, "end_date": end, "nights_requested": days, "nights_returned": len(nights), "nights": nights}, nil
	}, "start_date", "end_date")
}
