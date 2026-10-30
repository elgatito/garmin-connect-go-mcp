package server

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func weightMeasurement(w map[string]any, includeDate bool) map[string]any {
	out := map[string]any{}
	for k, src := range map[string]string{"weight_grams": "weight", "bmi": "bmi", "body_fat_percent": "bodyFat", "body_water_percent": "bodyWater", "bone_mass_grams": "boneMass", "muscle_mass_grams": "muscleMass", "source_type": "sourceType", "timestamp_gmt": "timestampGMT"} {
		if w[src] != nil {
			out[k] = w[src]
		}
	}
	if includeDate && w["calendarDate"] != nil {
		out["date"] = w["calendarDate"]
	}
	if n, ok := w["weight"].(float64); ok && n != 0 {
		out["weight_kg"] = math.RoundToEven(n/10) / 100
	}
	return out
}
func weightAverage(out map[string]any, data map[string]any) {
	avg, _ := data["totalAverage"].(map[string]any)
	if n, ok := avg["weight"].(float64); ok && n != 0 {
		out["average_weight_grams"] = n
		out["average_weight_kg"] = math.RoundToEven(n/10) / 100
	}
}
func registerWeight(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_daily_weigh_ins", description("get_daily_weigh_ins"), map[string]any{"date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		date, err := dateArgument(args, "date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving daily weight measurements: %w", err)
		}
		v, err := s.Client.GetJSON(ctx, "/weight-service/weight/dayview/"+date, url.Values{"includeAll": {"True"}})
		if err != nil {
			return nil, fmt.Errorf("Error retrieving daily weight measurements: %w", err)
		}
		data, _ := v.(map[string]any)
		rows, _ := data["dateWeightList"].([]any)
		if len(rows) == 0 {
			return fmt.Sprintf("No weight measurements found for %s.", date), nil
		}
		outRows := []any{}
		for _, v := range rows {
			if w, ok := v.(map[string]any); ok {
				outRows = append(outRows, weightMeasurement(w, false))
			}
		}
		out := map[string]any{"date": date, "measurement_count": len(rows), "measurements": outRows}
		weightAverage(out, data)
		return out, nil
	}, "date")
	addTool(srv, m, "get_weigh_ins", description("get_weigh_ins"), map[string]any{"start_date": map[string]any{"type": "string"}, "end_date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		start, err := dateArgument(args, "start_date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving weight measurements: %w", err)
		}
		end, err := dateArgument(args, "end_date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving weight measurements: %w", err)
		}
		v, err := s.Client.GetJSON(ctx, "/weight-service/weight/range/"+start+"/"+end, url.Values{"includeAll": {"True"}})
		if err != nil {
			return nil, fmt.Errorf("Error retrieving weight measurements: %w", err)
		}
		data, _ := v.(map[string]any)
		days, _ := data["dailyWeightSummaries"].([]any)
		if len(days) == 0 {
			return fmt.Sprintf("No weight measurements found between %s and %s.", start, end), nil
		}
		rows := []map[string]any{}
		for _, v := range days {
			day, _ := v.(map[string]any)
			metrics, _ := day["allWeightMetrics"].([]any)
			for _, v := range metrics {
				if w, ok := v.(map[string]any); ok {
					rows = append(rows, weightMeasurement(w, true))
				}
			}
		}
		sort.SliceStable(rows, func(i, j int) bool { a, _ := rows[i]["date"].(string); b, _ := rows[j]["date"].(string); return a > b })
		out := map[string]any{"date_range": map[string]any{"start": start, "end": end}, "measurement_count": len(rows), "days_with_data": len(days), "measurements": rows}
		weightAverage(out, data)
		return out, nil
	}, "start_date", "end_date")
}
