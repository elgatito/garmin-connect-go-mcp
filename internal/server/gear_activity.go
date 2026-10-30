package server

import (
	"context"
	"errors"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
)

func registerGearActivity(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_activity_gear", description("get_activity_gear"), map[string]any{"activity_id": map[string]any{"anyOf": []any{map[string]any{"type": "integer"}, map[string]any{"type": "string"}}}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		id, err := activityID(args["activity_id"])
		if err != nil {
			return nil, fmt.Errorf("Error retrieving activity gear data: %w", err)
		}
		v, err := s.Client.GetJSON(ctx, "/gear-service/gear/filterGear", url.Values{"activityId": {strconv.FormatInt(id, 10)}})
		if err != nil {
			return nil, fmt.Errorf("Error retrieving activity gear data: %w", err)
		}
		if empty(v) {
			return fmt.Sprintf("No gear data found for activity with ID %d", id), nil
		}
		return v, nil
	}, "activity_id")
	for _, item := range []struct{ name, action, verb string }{{"add_gear_to_activity", "link", "added"}, {"remove_gear_from_activity", "unlink", "removed"}} {
		item := item
		addTool(srv, m, item.name, description(item.name), map[string]any{"activity_id": map[string]any{"type": "integer"}, "gear_uuid": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
			id, err := activityID(args["activity_id"])
			if err != nil {
				return nil, err
			}
			uuid, ok := args["gear_uuid"].(string)
			if !ok || !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(uuid) {
				return nil, fmt.Errorf("invalid gear UUID")
			}
			label := "adding"
			if item.action == "unlink" {
				label = "removing"
			}
			prep := "to"
			if item.action == "unlink" {
				prep = "from"
			}
			tracker := newWriteTracker(item.name, "Error "+label+" gear "+prep+" activity", m.opt.Log)
			tracker.Sent()
			_, err = s.Client.Do(ctx, garmin.Req{Method: http.MethodPut, Path: fmt.Sprintf("/gear-service/gear/%s/%s/activity/%d", item.action, uuid, id)})
			var api *garmin.APIError
			if errors.As(err, &api) && api.Status == 404 {
				return fmt.Sprintf("No gear association changed; gear %s was not found for activity %d.", uuid, id), nil
			}
			if err != nil {
				return nil, tracker.Fail(err)
			}
			tracker.Committed()
			return map[string]any{"success": true, "activity_id": id, "gear_uuid": uuid, "message": fmt.Sprintf("Gear successfully %s %s activity", item.verb, map[bool]string{true: "from", false: "to"}[item.action == "unlink"])}, nil
		}, "activity_id", "gear_uuid")
	}
}
