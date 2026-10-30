package server

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerActivities(srv *mcp.Server, m *manager) {
	for _, item := range []struct{ name, path, label, emptyMessage string }{
		{"get_activity_typed_splits", "typedsplits", "activity typed splits", "No typed splits found for activity with ID %d"},
		{"get_activity_split_summaries", "split_summaries", "activity split summaries", "No split summaries found for activity with ID %d"},
		{"get_activity_hr_in_timezones", "hrTimeInZones", "activity heart rate time zone data", "No heart rate time zone data found for activity with ID %d"},
		{"get_activity_power_in_timezones", "powerTimeInZones", "activity power zone data", "No power zone data found for activity %d. Ensure the activity was recorded with a power meter."},
		{"get_activity_exercise_sets", "exerciseSets", "activity exercise sets", "No exercise sets found for activity with ID %d"},
	} {
		item := item
		addTool(srv, m, item.name, description(item.name), map[string]any{"activity_id": map[string]any{"anyOf": []any{map[string]any{"type": "integer"}, map[string]any{"type": "string"}}}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
			raw, ok := args["activity_id"]
			if !ok {
				return nil, fmt.Errorf("Error retrieving %s: activity_id is required", item.label)
			}
			var id int64
			var err error
			switch v := raw.(type) {
			case string:
				id, err = strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			case float64:
				id = int64(v)
				if v != float64(id) {
					err = fmt.Errorf("invalid activity ID")
				}
			default:
				err = fmt.Errorf("invalid activity ID")
			}
			if err != nil || id <= 0 {
				return nil, fmt.Errorf("Error retrieving %s: activity_id must be a positive integer", item.label)
			}
			value, err := s.Client.GetJSON(ctx, fmt.Sprintf("/activity-service/activity/%d/%s", id, item.path), nil)
			if err != nil {
				return nil, fmt.Errorf("Error retrieving %s: %w", item.label, err)
			}
			if empty(value) {
				return fmt.Sprintf(item.emptyMessage, id), nil
			}
			return value, nil
		}, "activity_id")
	}
}
