package server

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerHealthRanges(srv *mcp.Server, m *manager) {
	schema := map[string]any{"start_date": map[string]any{"type": "string", "description": "Start date in YYYY-MM-DD format"}, "end_date": map[string]any{"type": "string", "description": "End date in YYYY-MM-DD format"}}
	for _, item := range []struct{ name, label string }{{"get_daily_steps", "daily steps data"}, {"get_blood_pressure", "blood pressure data"}} {
		item := item
		addTool(srv, m, item.name, description(item.name), schema, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
			start, err := dateArgument(args, "start_date")
			if err != nil {
				return nil, fmt.Errorf("Error retrieving %s: %w", item.label, err)
			}
			end, err := dateArgument(args, "end_date")
			if err != nil {
				return nil, fmt.Errorf("Error retrieving %s: %w", item.label, err)
			}
			a, _ := time.Parse("2006-01-02", start)
			b, _ := time.Parse("2006-01-02", end)
			if a.After(b) {
				return nil, fmt.Errorf("Error retrieving %s: start date cannot be after end date", item.label)
			}
			if item.name == "get_blood_pressure" {
				v, err := s.Client.GetJSON(ctx, "/bloodpressure-service/bloodpressure/range/"+start+"/"+end, url.Values{"includeAll": {"True"}})
				if err != nil {
					return nil, fmt.Errorf("Error retrieving %s: %w", item.label, err)
				}
				if empty(v) {
					return fmt.Sprintf("No %s found between %s and %s", item.label, start, end), nil
				}
				return v, nil
			}
			var all []any
			for current := a; !current.After(b); {
				last := current.AddDate(0, 0, 27)
				if last.After(b) {
					last = b
				}
				v, err := s.Client.GetJSON(ctx, "/usersummary-service/stats/steps/daily/"+current.Format("2006-01-02")+"/"+last.Format("2006-01-02"), nil)
				if err != nil {
					return nil, fmt.Errorf("Error retrieving %s: %w", item.label, err)
				}
				if v != nil {
					data, ok := v.([]any)
					if !ok {
						return nil, fmt.Errorf("Error retrieving %s: unexpected response", item.label)
					}
					all = append(all, data...)
				}
				current = last.AddDate(0, 0, 1)
			}
			if len(all) == 0 {
				return fmt.Sprintf("No %s found between %s and %s", item.label, start, end), nil
			}
			return all, nil
		}, "start_date", "end_date")
	}
	addTool(srv, m, "get_body_battery", description("get_body_battery"), schema, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		start, err := dateArgument(args, "start_date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving body battery data: %w", err)
		}
		end, err := dateArgument(args, "end_date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving body battery data: %w", err)
		}
		v, err := s.Client.GetJSON(ctx, "/wellness-service/wellness/bodyBattery/reports/daily", url.Values{"startDate": {start}, "endDate": {end}})
		if err != nil {
			return nil, fmt.Errorf("Error retrieving body battery data: %w", err)
		}
		if empty(v) {
			return fmt.Sprintf("No body battery data found between %s and %s", start, end), nil
		}
		days, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("Error retrieving body battery data: unexpected response")
		}
		result := make([]any, 0, len(days))
		for _, v := range days {
			day, ok := v.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("Error retrieving body battery data: unexpected response")
			}
			events := []any{}
			if list, ok := day["bodyBatteryActivityEvent"].([]any); ok {
				for _, v := range list {
					e, ok := v.(map[string]any)
					if !ok {
						continue
					}
					event := map[string]any{"type": e["eventType"], "start_time": e["eventStartTimeGmt"], "body_battery_impact": e["bodyBatteryImpact"], "feedback": e["shortFeedback"]}
					if n, ok := e["durationInMilliseconds"].(float64); ok {
						event["duration_minutes"] = math.RoundToEven(n/6000) / 10
					}
					events = append(events, event)
				}
			}
			entry := map[string]any{"date": day["date"], "charged": day["charged"], "drained": day["drained"], "events": events}
			if f, ok := day["bodyBatteryDynamicFeedbackEvent"].(map[string]any); ok && len(f) > 0 {
				entry["current_feedback"] = f["feedbackShortType"]
				entry["body_battery_level"] = f["bodyBatteryLevel"]
			}
			result = append(result, entry)
		}
		return result, nil
	}, "start_date", "end_date")

}
