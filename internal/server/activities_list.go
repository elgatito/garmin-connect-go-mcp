package server

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func activityRows(rows []any, variant string) []any {
	out := make([]any, 0, len(rows))
	for _, v := range rows {
		a, ok := v.(map[string]any)
		if !ok {
			continue
		}
		fields := map[string]string{"id": "activityId", "name": "activityName", "start_time": "startTimeLocal", "distance_meters": "distance", "duration_seconds": "duration", "calories": "calories", "avg_hr_bpm": "averageHR", "steps": "steps"}
		if variant != "day" {
			fields["max_hr_bpm"] = "maxHR"
			fields["elevation_gain_meters"] = "elevationGain"
			fields["elevation_loss_meters"] = "elevationLoss"
		}
		if variant == "all" {
			fields["moving_duration_seconds"] = "movingDuration"
			fields["owner_display_name"] = "ownerDisplayName"
		}
		if variant == "day" {
			fields["lap_count"] = "lapCount"
			fields["moderate_intensity_minutes"] = "moderateIntensityMinutes"
			fields["vigorous_intensity_minutes"] = "vigorousIntensityMinutes"
		}
		entry := map[string]any{}
		for k, src := range fields {
			if a[src] != nil {
				entry[k] = a[src]
			}
		}
		for k, src := range map[string]string{"type": "activityType", "event_type": "eventType"} {
			if obj, ok := a[src].(map[string]any); ok && obj["typeKey"] != nil {
				entry[k] = obj["typeKey"]
			}
		}
		out = append(out, entry)
	}
	return out
}
func intArg(args map[string]any, key string, def int) (int, error) {
	v, ok := args[key]
	if !ok {
		return def, nil
	}
	n, ok := v.(float64)
	if !ok || n != float64(int(n)) {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return int(n), nil
}
func registerActivityLists(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_activities", description("get_activities"), map[string]any{"start": map[string]any{"type": "integer", "default": 0}, "limit": map[string]any{"type": "integer", "default": 20}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		start, err := intArg(args, "start", 0)
		if err != nil || start < 0 {
			return nil, fmt.Errorf("Error retrieving activities: start must be a non-negative integer")
		}
		limit, err := intArg(args, "limit", 20)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving activities: %w", err)
		}
		if limit < 1 {
			limit = 1
		}
		if limit > 100 {
			limit = 100
		}
		v, err := s.Client.GetJSON(ctx, "/activitylist-service/activities/search/activities", url.Values{"start": {strconv.Itoa(start)}, "limit": {strconv.Itoa(limit)}})
		if err != nil {
			return nil, fmt.Errorf("Error retrieving activities: %w", err)
		}
		if empty(v) {
			return fmt.Sprintf("No activities found at index %d", start), nil
		}
		rows, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("Error retrieving activities: unexpected response")
		}
		more := len(rows) == limit
		var next any
		if more {
			next = start + limit
		}
		return map[string]any{"start": start, "limit": limit, "count": len(rows), "has_more": more, "next_start": next, "activities": activityRows(rows, "all")}, nil
	})
	addTool(srv, m, "get_activities_by_date", description("get_activities_by_date"), map[string]any{"start_date": map[string]any{"type": "string"}, "end_date": map[string]any{"type": "string"}, "activity_type": map[string]any{"type": "string", "default": ""}, "page": map[string]any{"type": "integer", "default": 0}, "page_size": map[string]any{"type": "integer", "default": 100}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		start, err := dateArgument(args, "start_date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving activities by date: %w", err)
		}
		end, err := dateArgument(args, "end_date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving activities by date: %w", err)
		}
		page, err := intArg(args, "page", 0)
		if err != nil || page < 0 {
			return nil, fmt.Errorf("Error retrieving activities by date: page must be a non-negative integer")
		}
		size, err := intArg(args, "page_size", 100)
		if err != nil {
			return nil, err
		}
		if size < 1 {
			size = 1
		}
		if size > 200 {
			size = 200
		}
		q := url.Values{"startDate": {start}, "endDate": {end}, "start": {strconv.Itoa(page * size)}, "limit": {strconv.Itoa(size)}}
		if v, ok := args["activity_type"].(string); ok && v != "" {
			q.Set("activityType", v)
		}
		v, err := s.Client.GetJSON(ctx, "/activitylist-service/activities/search/activities", q)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving activities by date: %w", err)
		}
		var rows []any
		if v != nil {
			var ok bool
			rows, ok = v.([]any)
			if !ok {
				return nil, fmt.Errorf("Error retrieving activities by date: unexpected response")
			}
		}
		more := len(rows) == size
		result := map[string]any{"count": len(rows), "page": page, "page_size": size, "has_more": more, "date_range": map[string]any{"start": start, "end": end}, "activities": activityRows(rows, "date")}
		if more {
			result["next_page"] = page + 1
		}
		return result, nil
	}, "start_date", "end_date")
	addTool(srv, m, "count_activities", description("count_activities"), nil, func(ctx context.Context, s *Session, _ map[string]any) (any, error) {
		v, err := s.Client.GetJSON(ctx, "/activitylist-service/activities/count", nil)
		if err != nil {
			return nil, fmt.Errorf("Error counting activities: %w", err)
		}
		obj, ok := v.(map[string]any)
		if !ok || obj["totalCount"] == nil {
			return nil, fmt.Errorf("Error counting activities: No activities count data received")
		}
		return map[string]any{"total_activities": obj["totalCount"], "note": "Use get_activities() with pagination to retrieve activity details"}, nil
	})
	addTool(srv, m, "get_activities_fordate", description("get_activities_fordate"), map[string]any{"date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		date, err := dateArgument(args, "date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving activities for date: %w", err)
		}
		v, err := s.Client.GetJSON(ctx, "/mobile-gateway/heartRate/forDate/"+date, nil)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving activities for date: %w", err)
		}
		raw, _ := v.(map[string]any)
		group, _ := raw["ActivitiesForDay"].(map[string]any)
		rows, _ := group["payload"].([]any)
		if len(rows) == 0 {
			return fmt.Sprintf("No activities found for %s", date), nil
		}
		return map[string]any{"date": date, "count": len(rows), "activities": activityRows(rows, "day")}, nil
	}, "date")
	addTool(srv, m, "get_activity_types", description("get_activity_types"), nil, func(ctx context.Context, s *Session, _ map[string]any) (any, error) {
		v, err := s.Client.GetJSON(ctx, "/activity-service/activity/activityTypes", nil)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving activity types: %w", err)
		}
		if empty(v) {
			return "No activity types found", nil
		}
		rows, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("Error retrieving activity types: unexpected response")
		}
		result := make([]any, 0, len(rows))
		for _, v := range rows {
			raw, ok := v.(map[string]any)
			if !ok {
				continue
			}
			entry := map[string]any{}
			for k, source := range map[string]string{"type_id": "typeId", "type_key": "typeKey", "display_name": "displayName", "parent_type_id": "parentTypeId", "is_hidden": "isHidden"} {
				if raw[source] != nil {
					entry[k] = raw[source]
				}
			}
			result = append(result, entry)
		}
		return map[string]any{"count": len(rows), "activity_types": result}, nil
	})

}
