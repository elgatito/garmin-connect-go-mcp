package server

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func calendarEvent(item, detail map[string]any) map[string]any {
	target, _ := item["completionTarget"].(map[string]any)
	var distance any
	if target["unitType"] == "distance" {
		value, ok := target["value"].(float64)
		scale, found := map[string]float64{"meter": 1, "kilometer": 1000, "yard": 0.9144, "mile": 1609.344}[fmt.Sprint(target["unit"])]
		if ok && found {
			distance = value * scale
		}
	}
	customization, _ := detail["eventCustomization"].(map[string]any)
	goal, _ := customization["customGoal"].(map[string]any)
	var goalTime any
	if goal["unitType"] == "time" && goal["unit"] == "second" {
		if v, ok := goal["value"].(float64); ok {
			goalTime = v
		}
	}
	eventTime, _ := item["eventTimeLocal"].(map[string]any)
	var location any
	if v, ok := item["location"].(string); ok && strings.TrimSpace(v) != "" {
		location = strings.TrimSpace(v)
	}
	return map[string]any{"title": item["title"], "date": item["date"], "is_race": truthy(item["isRace"]), "primary_event": truthy(item["primaryEvent"]), "subscribed": truthy(item["subscribed"]), "distance_meters": distance, "goal_time_seconds": goalTime, "start_time_local": eventTime["startTimeHhMm"], "time_zone": eventTime["timeZoneId"], "location": location, "url": item["url"], "event_uuid": item["shareableEventUuid"]}
}
func registerCalendar(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_calendar_events", description("get_calendar_events"), map[string]any{"start_date": map[string]any{"type": "string"}, "end_date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		start, err := dateArgument(args, "start_date")
		if err != nil {
			return err.Error(), nil
		}
		end, err := dateArgument(args, "end_date")
		if err != nil {
			return err.Error(), nil
		}
		if start > end {
			return "start_date must be on or before end_date.", nil
		}
		a, _ := time.Parse("2006-01-02", start)
		b, _ := time.Parse("2006-01-02", end)
		month := time.Date(a.Year(), a.Month(), 1, 0, 0, 0, 0, time.UTC)
		last := time.Date(b.Year(), b.Month(), 1, 0, 0, 0, 0, time.UTC)
		events := []map[string]any{}
		seen := map[string]bool{}
		for !month.After(last) {
			v, err := s.Client.GetJSON(ctx, fmt.Sprintf("/calendar-service/year/%d/month/%d", month.Year(), int(month.Month())-1), nil)
			if err != nil {
				return nil, fmt.Errorf("Error retrieving calendar events: %w", err)
			}
			data, _ := v.(map[string]any)
			items, _ := data["calendarItems"].([]any)
			for _, v := range items {
				item, ok := v.(map[string]any)
				if !ok || item["itemType"] != "event" {
					continue
				}
				day, ok := item["date"].(string)
				if !ok || day < start || day > end {
					continue
				}
				key := fmt.Sprintf("%v\x00%v\x00%s", item["shareableEventUuid"], item["title"], day)
				if seen[key] {
					continue
				}
				seen[key] = true
				detail := map[string]any{}
				var path string
				if id := item["id"]; id != nil {
					path = "/calendar-service/event/" + url.PathEscape(fmt.Sprint(id))
				} else if uuid, ok := item["shareableEventUuid"].(string); ok && strings.TrimSpace(uuid) != "" {
					path = "/calendar-service/event/" + url.PathEscape(strings.TrimSpace(uuid)) + "/shareable"
				}
				if path != "" {
					v, err := s.Client.GetJSON(ctx, path, nil)
					if err == nil {
						detail, _ = v.(map[string]any)
					}
				}
				events = append(events, calendarEvent(item, detail))
			}
			month = month.AddDate(0, 1, 0)
		}
		sort.SliceStable(events, func(i, j int) bool {
			a, _ := events[i]["date"].(string)
			b, _ := events[j]["date"].(string)
			if a != b {
				return a < b
			}
			x, _ := events[i]["title"].(string)
			y, _ := events[j]["title"].(string)
			return x < y
		})
		return map[string]any{"count": len(events), "date_range": map[string]any{"start": start, "end": end}, "events": events}, nil
	}, "start_date", "end_date")
}
