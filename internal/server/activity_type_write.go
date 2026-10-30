package server

import (
	"context"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"sort"
	"strings"
)

func registerActivityTypeWrites(srv *mcp.Server, m *manager) {
	for _, item := range []struct{ name, arg, label, listPath, field string }{{"set_activity_type", "type_key", "activity type", "/activity-service/activity/activityTypes", "activityTypeDTO"}, {"set_activity_event_type", "event_type", "activity event type", "/activity-service/activity/eventTypes", "eventTypeDTO"}} {
		item := item
		addTool(srv, m, item.name, description(item.name), map[string]any{"activity_id": map[string]any{"anyOf": []any{map[string]any{"type": "integer"}, map[string]any{"type": "string"}}}, item.arg: map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
			id, err := activityID(args["activity_id"])
			if err != nil {
				return nil, fmt.Errorf("Error updating %s: %w", item.label, err)
			}
			key, ok := args[item.arg].(string)
			if !ok {
				return nil, fmt.Errorf("Error updating %s: missing type", item.label)
			}
			key = strings.TrimSpace(key)
			tracker := newWriteTracker(item.name, "Error updating "+item.label, m.opt.Log)
			v, err := s.Client.GetJSON(ctx, item.listPath, nil)
			if err != nil {
				return nil, tracker.Fail(err)
			}
			list, _ := v.([]any)
			var match map[string]any
			valid := []string{}
			for _, v := range list {
				entry, ok := v.(map[string]any)
				if !ok {
					continue
				}
				k, ok := entry["typeKey"].(string)
				if !ok || k == "" {
					continue
				}
				valid = append(valid, k)
				if k == key {
					match = entry
				}
			}
			if match == nil {
				if item.name == "set_activity_type" {
					sort.Strings(valid)
					return fmt.Sprintf("Unknown activity type '%s'. Valid type keys: %s", key, strings.Join(valid, ", ")), nil
				}
				return fmt.Sprintf("Unknown event type '%s'. Valid event types: %s", key, strings.Join(valid, ", ")), nil
			}
			dto := map[string]any{"typeId": match["typeId"], "typeKey": match["typeKey"]}
			if item.name == "set_activity_type" {
				dto["parentTypeId"] = match["parentTypeId"]
			} else {
				dto["sortOrder"] = match["sortOrder"]
			}
			payload := map[string]any{"activityId": id, item.field: dto}
			tracker.Sent()
			_, err = s.Client.Do(ctx, garmin.Req{Method: http.MethodPut, Path: fmt.Sprintf("/activity-service/activity/%d", id), JSON: payload})
			if err != nil {
				return nil, tracker.Fail(err)
			}
			tracker.Committed()
			out := map[string]any{"success": true, "activity_id": id, item.arg: match["typeKey"]}
			if item.name == "set_activity_type" {
				out["type_id"] = match["typeId"]
				out["message"] = "Activity type successfully updated"
			} else {
				out["message"] = "Activity event type successfully updated"
			}
			return out, nil
		}, "activity_id", item.arg)
	}
}
