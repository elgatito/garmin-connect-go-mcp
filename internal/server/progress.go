package server

import (
	"context"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/url"
)

func progressSummary(data map[string]any, metric, start, end string) map[string]any {
	stats, _ := data["stats"].(map[string]any)
	out := map[string]any{}
	total := float64(0)
	for sport, v := range stats {
		group, _ := v.(map[string]any)
		metricData, _ := group[metric].(map[string]any)
		count, ok := metricData["count"].(float64)
		if !ok || count <= 0 {
			continue
		}
		total += count
		entry := map[string]any{"count": count}
		for _, key := range []string{"sum", "avg", "min", "max"} {
			v := metricData[key]
			if metric == "calories" {
				if n, ok := v.(float64); ok {
					v = n / 4.19
				}
			}
			entry[key] = v
		}
		out[sport] = entry
	}
	return map[string]any{"metric": metric, "start_date": start, "end_date": end, "stats_by_activity_type": out, "count_of_activities": total}
}
func registerProgress(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_progress_summary_between_dates", description("get_progress_summary_between_dates"), map[string]any{"start_date": map[string]any{"type": "string"}, "end_date": map[string]any{"type": "string"}, "metric": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		start, err := dateArgument(args, "start_date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving progress summary: %w", err)
		}
		end, err := dateArgument(args, "end_date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving progress summary: %w", err)
		}
		metric, ok := args["metric"].(string)
		if !ok {
			return nil, fmt.Errorf("Error retrieving progress summary: metric required")
		}
		v, err := s.Client.GetJSON(ctx, "/fitnessstats-service/activity", url.Values{"startDate": {start}, "endDate": {end}, "aggregation": {"lifetime"}, "groupByParentActivityType": {"True"}, "metric": {metric}})
		if err != nil {
			return nil, fmt.Errorf("Error retrieving progress summary: %w", err)
		}
		if empty(v) {
			return fmt.Sprintf("No progress summary found for %s between %s and %s.", metric, start, end), nil
		}
		rows, ok := v.([]any)
		if !ok || len(rows) == 0 {
			return "Unexpected response format from API", nil
		}
		raw, ok := rows[0].(map[string]any)
		if !ok {
			return "Unexpected response format from API", nil
		}
		return progressSummary(raw, metric, start, end), nil
	}, "start_date", "end_date", "metric")
	addTool(srv, m, "request_reload", description("request_reload"), map[string]any{"date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		date, err := dateArgument(args, "date")
		if err != nil {
			return nil, fmt.Errorf("Error requesting data reload: %w", err)
		}
		tracker := newWriteTracker("request_reload", "Error requesting data reload", m.opt.Log)
		tracker.Sent()
		v, err := s.Client.DoJSON(ctx, garmin.Req{Method: http.MethodPost, Path: "/wellness-service/wellness/epoch/request/" + date})
		if err != nil {
			return nil, tracker.Fail(err)
		}
		tracker.Committed()
		return v, nil
	}, "date")
}
