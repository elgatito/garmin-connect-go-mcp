package server

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"math"
	"net/url"
)

func acclimationSummary(data map[string]any, date string) map[string]any {
	out := map[string]any{"date": date}
	if value, ok := data["calendarDate"]; ok {
		out["date"] = value
	}
	for k, src := range map[string]string{"heat_acclimation_percent": "heatAcclimationPercentage", "previous_heat_acclimation_percent": "previousHeatAcclimationPercentage", "heat_trend": "heatTrend", "heat_acclimation_date": "heatAcclimationDate", "previous_heat_acclimation_date": "previousHeatAcclimationDate", "altitude_acclimation_meters": "altitudeAcclimation", "previous_altitude_acclimation_meters": "previousAltitudeAcclimation", "altitude_trend": "altitudeTrend", "current_altitude_meters": "currentAltitude"} {
		if data[src] != nil {
			out[k] = data[src]
		}
	}
	heat, ok := data["heatAcclimationPercentage"].(float64)
	prev, ok2 := data["previousHeatAcclimationPercentage"].(float64)
	if ok && ok2 {
		out["heat_acclimation_change"] = math.RoundToEven((heat-prev)*10) / 10
	}
	return out
}
func toleranceSummary(data map[string]any, date string) map[string]any {
	out := map[string]any{"date": date}
	if value, ok := data["calendarDate"]; ok {
		out["date"] = value
	}
	for k, src := range map[string]string{"tolerance_km": "acuteTolerance", "acute_load_km": "acuteImpactLoad", "distance_km": "acuteDistance"} {
		if n, ok := data[src].(float64); ok {
			out[k] = math.RoundToEven(n/10) / 100
		}
	}
	load, lok := data["acuteImpactLoad"].(float64)
	dist, dok := data["acuteDistance"].(float64)
	if lok && dok && dist != 0 {
		out["load_ratio"] = math.RoundToEven(load/dist*100) / 100
	}
	if truthy(data["runningToleranceFeedBackPhrase"]) {
		out["feedback_phrase"] = data["runningToleranceFeedBackPhrase"]
	}
	return out
}
func registerAcclimation(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_acclimation", description("get_acclimation"), map[string]any{"date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		date, err := dateArgument(args, "date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving acclimation data: %w", err)
		}
		v, err := s.Client.GetJSON(ctx, "/metrics-service/metrics/maxmet/daily/"+date+"/"+date, nil)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving acclimation data: %w", err)
		}
		if rows, ok := v.([]any); ok {
			v = nil
			if len(rows) > 0 {
				v = rows[0]
			}
		}
		data, ok := v.(map[string]any)
		if !ok {
			return fmt.Sprintf("No acclimation data found for %s.", date), nil
		}
		acc, _ := data["heatAltitudeAcclimation"].(map[string]any)
		if len(acc) == 0 {
			return fmt.Sprintf("No acclimation data found for %s. Garmin populates this only after outdoor activities in heat or at altitude.", date), nil
		}
		return acclimationSummary(acc, date), nil
	}, "date")
	addTool(srv, m, "get_running_tolerance", description("get_running_tolerance"), map[string]any{"date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		date, err := dateArgument(args, "date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving running tolerance data: %w", err)
		}
		v, err := s.Client.GetJSON(ctx, "/metrics-service/metrics/runningtolerance/stats", url.Values{"startDate": {date}, "endDate": {date}, "aggregation": {"daily"}})
		if err != nil {
			return nil, fmt.Errorf("Error retrieving running tolerance data: %w", err)
		}
		list, _ := v.([]any)
		if len(list) == 0 {
			return fmt.Sprintf("No running tolerance data found for %s.", date), nil
		}
		raw, ok := list[0].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Error retrieving running tolerance data: unexpected response")
		}
		return toleranceSummary(raw, date), nil
	}, "date")
}
