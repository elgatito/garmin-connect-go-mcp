package server

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"math"
	"net/url"
	"sort"
	"time"
)

func toleranceTrend(rows []any, aggregation string) []map[string]any {
	out := []map[string]any{}
	for _, v := range rows {
		raw, ok := v.(map[string]any)
		if !ok {
			continue
		}
		data := raw
		if aggregation == "weekly" {
			data = map[string]any{"calendarDate": raw["calendarDate"], "acuteTolerance": raw["tolerance"], "acuteImpactLoad": raw["totalImpactLoad"], "acuteDistance": raw["totalDistance"]}
		}
		entry := toleranceSummary(data, "")
		if aggregation == "weekly" {
			for k, src := range map[string]string{"start_of_week": "startOfWeek", "end_of_week": "endOfWeek", "week_index": "weekIndex"} {
				if raw[src] != nil {
					entry[k] = raw[src]
				}
			}
		}
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool { a, _ := out[i]["date"].(string); b, _ := out[j]["date"].(string); return a < b })
	return out
}
func registerToleranceTrend(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_running_tolerance_trend", description("get_running_tolerance_trend"), map[string]any{"start_date": map[string]any{"type": "string"}, "end_date": map[string]any{"type": "string"}, "aggregation": map[string]any{"type": "string", "default": "weekly"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		start, err := dateArgument(args, "start_date")
		if err != nil {
			return nil, fmt.Errorf("Invalid date format: %w. Use YYYY-MM-DD.", err)
		}
		end, err := dateArgument(args, "end_date")
		if err != nil {
			return nil, fmt.Errorf("Invalid date format: %w. Use YYYY-MM-DD.", err)
		}
		aggregation := "weekly"
		if v, ok := args["aggregation"].(string); ok {
			aggregation = v
		}
		a, _ := time.Parse("2006-01-02", start)
		b, _ := time.Parse("2006-01-02", end)
		days := int(b.Sub(a).Hours()/24) + 1
		if days < 1 {
			return "end_date must be on or after start_date.", nil
		}
		maxDays := 366
		if aggregation == "daily" {
			maxDays = 90
		}
		if days > maxDays {
			return fmt.Sprintf("Date range too large (%d days). Maximum is %d days for %s aggregation.", days, maxDays, aggregation), nil
		}
		if aggregation != "daily" && aggregation != "weekly" {
			return nil, fmt.Errorf("Error retrieving running tolerance trend: invalid aggregation '%s', must be 'daily' or 'weekly'", aggregation)
		}
		v, err := s.Client.GetJSON(ctx, "/metrics-service/metrics/runningtolerance/stats", url.Values{"startDate": {start}, "endDate": {end}, "aggregation": {aggregation}})
		if err != nil {
			return nil, fmt.Errorf("Error retrieving running tolerance trend: %w", err)
		}
		list, _ := v.([]any)
		if len(list) == 0 {
			return fmt.Sprintf("No running tolerance data found between %s and %s.", start, end), nil
		}
		trend := toleranceTrend(list, aggregation)
		var first, last, change any
		for _, row := range trend {
			if n, ok := row["tolerance_km"].(float64); ok {
				if first == nil {
					first = n
				}
				last = n
			}
		}
		if first != nil && last != nil {
			change = math.RoundToEven((last.(float64)-first.(float64))*100) / 100
		}
		return map[string]any{"start_date": start, "end_date": end, "aggregation": aggregation, "data_points": len(trend), "first_tolerance_km": first, "latest_tolerance_km": last, "tolerance_change_km": change, "trend": trend}, nil
	}, "start_date", "end_date")
}
