package server

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"math"
)

func fitnessAge(data map[string]any, date string, details bool) map[string]any {
	out := map[string]any{"date": date}
	for key, src := range map[string]string{"chronological_age_years": "chronologicalAge", "last_updated": "lastUpdated"} {
		if data[src] != nil {
			out[key] = data[src]
		}
	}
	for key, src := range map[string]string{"fitness_age_years": "fitnessAge", "achievable_fitness_age_years": "achievableFitnessAge", "previous_fitness_age_years": "previousFitnessAge"} {
		if n, ok := data[src].(float64); ok && n != 0 {
			out[key] = math.RoundToEven(n*10) / 10
		}
	}
	if chronological, ok := data["chronologicalAge"].(float64); ok {
		if fitness, ok := data["fitnessAge"].(float64); ok {
			out["age_difference_years"] = math.RoundToEven((chronological-fitness)*10) / 10
		}
	}
	if details {
		components, _ := data["components"].(map[string]any)
		result := map[string]any{}
		for name, v := range components {
			c, ok := v.(map[string]any)
			if !ok {
				continue
			}
			entry := map[string]any{"value": c["value"]}
			for k, src := range map[string]string{"target": "targetValue", "improvement_needed": "improvementValue", "priority": "priority", "stale": "stale", "last_measurement": "lastMeasurementDate"} {
				if c[src] != nil {
					entry[k] = c[src]
				}
			}
			if n, ok := c["potentialAge"].(float64); ok {
				entry["potential_age_if_improved"] = math.RoundToEven(n*10) / 10
			}
			result[name] = entry
		}
		if len(result) > 0 {
			out["components"] = result
		}
	}
	return out
}
func registerFitnessAge(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_fitnessage_data", description("get_fitnessage_data"), map[string]any{"date": map[string]any{"type": "string"}, "details": map[string]any{"type": "boolean", "default": false}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		date, err := dateArgument(args, "date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving fitness age data: %w", err)
		}
		v, err := s.Client.GetJSON(ctx, "/fitnessage-service/fitnessage/"+date, nil)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving fitness age data: %w", err)
		}
		if empty(v) {
			return fmt.Sprintf("No fitness age data found for %s.", date), nil
		}
		raw, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Error retrieving fitness age data: unexpected response")
		}
		details, _ := args["details"].(bool)
		return fitnessAge(raw, date, details), nil
	}, "date")
}
