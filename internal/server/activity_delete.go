package server

import (
	"context"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/url"
	"time"
)

func activityPreview(ctx context.Context, s *Session, id int64) map[string]any {
	out := map[string]any{"activity_id": id}
	v, err := s.Client.GetJSON(ctx, fmt.Sprintf("/activity-service/activity/%d", id), nil)
	if err == nil {
		if raw, ok := v.(map[string]any); ok && len(raw) > 0 {
			summary, _ := raw["summaryDTO"].(map[string]any)
			typ, _ := raw["activityTypeDTO"].(map[string]any)
			values := map[string]any{"name": raw["activityName"], "type": typ["typeKey"], "start_time": summary["startTimeLocal"], "distance_meters": summary["distance"], "duration_seconds": summary["duration"]}
			for _, field := range []struct{ key, source string }{{"start_time", "startTimeLocal"}, {"distance_meters", "distance"}, {"duration_seconds", "duration"}} {
				if !truthy(values[field.key]) {
					values[field.key] = raw[field.source]
				}
			}
			for k, v := range values {
				if v != nil {
					out[k] = v
				}
			}
			return out
		}
	}
	v, err = s.Client.GetJSON(ctx, "/activitylist-service/activities/search/activities", url.Values{"start": {"0"}, "limit": {"50"}})
	if err != nil {
		return out
	}
	items, _ := v.([]any)
	for _, item := range items {
		raw, ok := item.(map[string]any)
		if !ok || fmt.Sprint(raw["activityId"]) != fmt.Sprint(id) {
			continue
		}
		typ, _ := raw["activityType"].(map[string]any)
		for k, v := range map[string]any{"name": raw["activityName"], "type": typ["typeKey"], "start_time": raw["startTimeLocal"], "distance_meters": raw["distance"], "duration_seconds": raw["duration"]} {
			if v != nil {
				out[k] = v
			}
		}
		break
	}
	return out
}
func registerActivityDelete(srv *mcp.Server, m *manager) {
	addTool(srv, m, "delete_activity", description("delete_activity"), map[string]any{"activity_id": map[string]any{"anyOf": []any{map[string]any{"type": "integer"}, map[string]any{"type": "string"}}}, "confirm": map[string]any{"type": "boolean", "default": false}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		id, err := activityID(args["activity_id"])
		if err != nil {
			return fmt.Sprintf("Invalid activity ID: %v", args["activity_id"]), nil
		}
		preview := activityPreview(ctx, s, id)
		confirm, _ := args["confirm"].(bool)
		if !confirm {
			return map[string]any{"status": "needs_confirmation", "activity": preview, "message": "This permanently deletes the activity from Garmin Connect and cannot be undone. Call again with confirm=true to proceed."}, nil
		}
		tracker := newWriteTracker("delete_activity", "Error deleting activity", m.opt.Log)
		tracker.Sent()
		_, err = s.Client.Do(ctx, garmin.Req{Method: http.MethodDelete, Path: fmt.Sprintf("/activity-service/activity/%d", id)})
		if err != nil {
			return nil, tracker.Fail(err)
		}
		tracker.Committed()
		fmt.Fprintf(m.opt.Log, "delete_activity: deleted activity %d name=%q at %s\n", id, preview["name"], time.Now().UTC().Format(time.RFC3339))
		return map[string]any{"status": "deleted", "activity": preview, "message": fmt.Sprintf("Activity %d permanently deleted.", id)}, nil
	}, "activity_id")
}
