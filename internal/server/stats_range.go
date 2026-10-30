package server

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/url"
	"time"
)

func statsByDate(v any) map[string]map[string]any {
	out := map[string]map[string]any{}
	payload, _ := v.(map[string]any)
	values, _ := payload["values"].([]any)
	for _, v := range values {
		row, ok := v.(map[string]any)
		if !ok {
			continue
		}
		day, ok := row["calendarDate"].(string)
		if !ok {
			continue
		}
		data, _ := row["values"].(map[string]any)
		if data == nil {
			data = map[string]any{}
		}
		out[day] = data
	}
	return out
}
func registerStatsRange(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_stats_range", description("get_stats_range"), map[string]any{"start_date": map[string]any{"type": "string"}, "end_date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
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
		count := int(b.Sub(a).Hours()/24) + 1
		if count < 1 {
			return "end_date must be on or after start_date.", nil
		}
		if count > 28 {
			return fmt.Sprintf("Date range too large (%d days). Maximum is 28 days.", count), nil
		}
		path := "/usersummary-service/stats/daily/" + start + "/" + end
		cal, err := s.Client.GetJSON(ctx, path, url.Values{"statsType": {"CALORIES"}})
		if err != nil {
			return nil, fmt.Errorf("Error retrieving stats range: %w", err)
		}
		steps, err := s.Client.GetJSON(ctx, path, url.Values{"statsType": {"STEPS"}})
		if err != nil {
			return nil, fmt.Errorf("Error retrieving stats range: %w", err)
		}
		calories := statsByDate(cal)
		stepDays := statsByDate(steps)
		today := time.Now().Local().Format("2006-01-02")
		days := []any{}
		anyData := false
		for d := a; !d.After(b); d = d.AddDate(0, 0, 1) {
			date := d.Format("2006-01-02")
			c, hasCal := calories[date]
			s, hasSteps := stepDays[date]
			has := hasCal || hasSteps
			anyData = anyData || has
			days = append(days, map[string]any{"date": date, "total_calories": c["totalCalories"], "active_calories": c["activeCalories"], "resting_calories": c["restingCalories"], "steps": s["totalSteps"], "has_data": has, "is_partial": date == today})
		}
		if !anyData {
			return fmt.Sprintf("No stats found between %s and %s.", start, end), nil
		}
		return map[string]any{"start_date": start, "end_date": end, "days": days}, nil
	}, "start_date", "end_date")
}
