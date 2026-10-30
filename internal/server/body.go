package server

import (
	"context"
	"fmt"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func bodyComposition(ctx context.Context, s *Session, start, end string) (any, error) {
	if start > end {
		return nil, fmt.Errorf("startdate cannot be after enddate")
	}
	return s.Client.GetJSON(ctx, "/weight-service/weight/dateRange", url.Values{"startDate": {start}, "endDate": {end}})
}
func registerBody(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_body_composition", description("get_body_composition"), map[string]any{"start_date": map[string]any{"type": "string"}, "end_date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		start, err := dateArgument(args, "start_date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving body composition data: %w", err)
		}
		end := start
		endArg, hasEnd := args["end_date"].(string)
		if hasEnd && endArg != "" {
			end, err = dateArgument(args, "end_date")
			if err != nil {
				return nil, fmt.Errorf("Error retrieving body composition data: %w", err)
			}
		}
		v, err := bodyComposition(ctx, s, start, end)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving body composition data: %w", err)
		}
		if empty(v) {
			if hasEnd && endArg != "" {
				return fmt.Sprintf("No body composition data found between %s and %s", start, end), nil
			}
			return fmt.Sprintf("No body composition data found for %s", start), nil
		}
		return v, nil
	}, "start_date")
	addTool(srv, m, "get_stats_and_body", description("get_stats_and_body"), map[string]any{"date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		date, err := dateArgument(args, "date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving stats and body composition data: %w", err)
		}
		p, err := s.Client.Profile(ctx)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving stats and body composition data: %w", err)
		}
		v, err := s.Client.GetJSON(ctx, "/usersummary-service/usersummary/daily/"+url.PathEscape(p.DisplayName), url.Values{"calendarDate": {date}})
		if err != nil {
			return nil, fmt.Errorf("Error retrieving stats and body composition data: %w", err)
		}
		stats, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Error retrieving stats and body composition data: unexpected response")
		}
		v, err = bodyComposition(ctx, s, date, date)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving stats and body composition data: %w", err)
		}
		if body, ok := v.(map[string]any); ok {
			if avg, ok := body["totalAverage"].(map[string]any); ok {
				for k, v := range avg {
					stats[k] = v
				}
			}
		}
		if len(stats) == 0 {
			return fmt.Sprintf("No stats and body composition data found for %s", date), nil
		}
		return stats, nil
	}, "date")
	addTool(srv, m, "get_rhr_day", description("get_rhr_day"), map[string]any{"date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		date, err := dateArgument(args, "date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving resting heart rate data: %w", err)
		}
		p, err := s.Client.Profile(ctx)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving resting heart rate data: %w", err)
		}
		v, err := s.Client.GetJSON(ctx, "/userstats-service/wellness/daily/"+url.PathEscape(p.DisplayName), url.Values{"fromDate": {date}, "untilDate": {date}, "metricId": {"60"}})
		if err != nil {
			return nil, fmt.Errorf("Error retrieving resting heart rate data: %w", err)
		}
		if empty(v) {
			return fmt.Sprintf("No resting heart rate data found for %s", date), nil
		}
		return v, nil
	}, "date")
}
