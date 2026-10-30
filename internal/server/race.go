package server

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/url"
)

func predictionTime(v any) any {
	seconds, ok := v.(float64)
	if !ok {
		return nil
	}
	n := int(seconds)
	hours := n / 3600
	minutes := (n % 3600) / 60
	secs := n % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, secs)
	}
	return fmt.Sprintf("%d:%02d", minutes, secs)
}
func registerRace(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_race_predictions", description("get_race_predictions"), nil, func(ctx context.Context, s *Session, _ map[string]any) (any, error) {
		p, err := s.Client.Profile(ctx)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving race predictions: %w", err)
		}
		v, err := s.Client.GetJSON(ctx, "/metrics-service/metrics/racepredictions/latest/"+url.PathEscape(p.DisplayName), nil)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving race predictions: %w", err)
		}
		if empty(v) {
			return "No race predictions found.", nil
		}
		raw, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Error retrieving race predictions: unexpected response")
		}
		predictions := map[string]any{}
		for key, source := range map[string]string{"5K": "time5K", "10K": "time10K", "half_marathon": "timeHalfMarathon", "marathon": "timeMarathon"} {
			predictions[key] = map[string]any{"time": predictionTime(raw[source]), "time_seconds": raw[source]}
		}
		return map[string]any{"prediction_date": raw["calendarDate"], "predictions": predictions}, nil
	})
}
