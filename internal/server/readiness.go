package server

import (
	"context"
	"fmt"
	"math"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func curateReadiness(raw map[string]any) map[string]any {
	out := map[string]any{}
	for k, source := range map[string]string{"date": "calendarDate", "timestamp": "timestampLocal", "context": "inputContext", "level": "level", "score": "score", "feedback": "feedbackShort", "sleep_score": "sleepScore", "sleep_factor_percent": "sleepScoreFactorPercent", "sleep_factor_feedback": "sleepScoreFactorFeedback", "recovery_factor_percent": "recoveryTimeFactorPercent", "recovery_factor_feedback": "recoveryTimeFactorFeedback", "training_load_factor_percent": "acwrFactorPercent", "training_load_feedback": "acwrFactorFeedback", "acute_load": "acuteLoad", "hrv_factor_percent": "hrvFactorPercent", "hrv_factor_feedback": "hrvFactorFeedback", "hrv_weekly_avg": "hrvWeeklyAverage", "stress_history_factor_percent": "stressHistoryFactorPercent", "stress_history_feedback": "stressHistoryFactorFeedback", "sleep_history_factor_percent": "sleepHistoryFactorPercent", "sleep_history_feedback": "sleepHistoryFactorFeedback"} {
		if raw[source] != nil {
			out[k] = raw[source]
		}
	}
	if raw["recoveryTimeChangePhrase"] == "REACHED_ZERO" {
		out["recovery_time_hours"] = 0.0
	} else if v, ok := raw["recoveryTime"].(float64); ok && v >= 0 {
		out["recovery_time_hours"] = math.RoundToEven(v/6) / 10
	}
	return out
}
func registerReadiness(srv *mcp.Server, m *manager) {
	for _, item := range []struct{ name, label string }{{"get_training_readiness", "training readiness data"}, {"get_morning_training_readiness", "morning training readiness"}} {
		item := item
		addTool(srv, m, item.name, description(item.name), map[string]any{"date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
			date, err := dateArgument(args, "date")
			if err != nil {
				return nil, fmt.Errorf("Error retrieving %s: %w", item.label, err)
			}
			v, err := s.Client.GetJSON(ctx, "/metrics-service/metrics/trainingreadiness/"+date, nil)
			if err != nil {
				return nil, fmt.Errorf("Error retrieving %s: %w", item.label, err)
			}
			var rows []map[string]any
			switch data := v.(type) {
			case map[string]any:
				if len(data) > 0 {
					rows = append(rows, data)
				}
			case []any:
				for _, item := range data {
					if row, ok := item.(map[string]any); ok {
						rows = append(rows, row)
					}
				}
			}
			if len(rows) == 0 {
				if item.name == "get_morning_training_readiness" {
					return fmt.Sprintf("No morning training readiness data found for %s", date), nil
				}
				return fmt.Sprintf("No training readiness data found for %s", date), nil
			}
			if item.name == "get_morning_training_readiness" {
				row := rows[0]
				for _, r := range rows {
					if r["inputContext"] == "AFTER_WAKEUP_RESET" {
						row = r
						break
					}
				}
				out := curateReadiness(row)
				if out["date"] == nil {
					out["date"] = date
				}
				return out, nil
			}
			out := make([]any, 0, len(rows))
			for _, row := range rows {
				out = append(out, curateReadiness(row))
			}
			return out, nil
		}, "date")
	}
}
