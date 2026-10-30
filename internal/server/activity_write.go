package server

import (
	"context"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"math"
	"net/http"
	"strconv"
	"strings"
)

func activityID(v any) (int64, error) {
	switch v := v.(type) {
	case string:
		id, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err == nil && id > 0 {
			return id, nil
		}
	case float64:
		id := int64(v)
		if v == float64(id) && id > 0 {
			return id, nil
		}
	}
	return 0, fmt.Errorf("activity_id must be a positive integer")
}
func registerActivityWrites(srv *mcp.Server, m *manager) {
	for _, item := range []struct {
		name, arg, label, field string
		numeric                 bool
	}{{"set_activity_name", "activity_name", "activity name", "activityName", false}, {"set_activity_description", "description", "activity description", "description", false}, {"set_perceived_effort", "rpe", "perceived effort", "directWorkoutRpe", true}, {"set_activity_feel", "feel", "activity feel", "directWorkoutFeel", true}} {
		item := item
		schema := map[string]any{"activity_id": map[string]any{"anyOf": []any{map[string]any{"type": "integer"}, map[string]any{"type": "string"}}}, item.arg: map[string]any{"type": "string"}}
		if item.numeric {
			schema[item.arg] = map[string]any{"type": "number"}
		}
		addTool(srv, m, item.name, description(item.name), schema, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
			id, err := activityID(args["activity_id"])
			if err != nil {
				return nil, fmt.Errorf("Error updating %s: %w", item.label, err)
			}
			value := args[item.arg]
			wire := value
			if item.numeric {
				v, ok := value.(float64)
				if !ok {
					return nil, fmt.Errorf("Error updating %s: invalid value", item.label)
				}
				if item.name == "set_perceived_effort" {
					if v < 0 || v > 10 {
						return "rpe must be between 0 and 10", nil
					}
					wire = math.RoundToEven(v * 10)
				} else {
					if v != 0 && v != 25 && v != 50 && v != 75 && v != 100 {
						return "feel must be one of 0, 25, 50, 75, 100", nil
					}
				}
			} else {
				v, ok := value.(string)
				if !ok {
					return nil, fmt.Errorf("Error updating %s: invalid value", item.label)
				}
				if item.name == "set_activity_name" {
					v = strings.TrimSpace(v)
					if v == "" {
						return "Activity name cannot be empty", nil
					}
					value = v
					wire = v
				}
			}
			payload := map[string]any{"activityId": id}
			if item.numeric {
				payload["summaryDTO"] = map[string]any{item.field: wire}
			} else {
				payload[item.field] = wire
			}
			tracker := newWriteTracker(item.name, "Error updating "+item.label, m.opt.Log)
			tracker.Sent()
			_, err = s.Client.Do(ctx, garmin.Req{Method: http.MethodPut, Path: fmt.Sprintf("/activity-service/activity/%d", id), JSON: payload})
			if err != nil {
				return nil, tracker.Fail(err)
			}
			tracker.Committed()
			label := strings.ToUpper(item.label[:1]) + item.label[1:]
			return map[string]any{"success": true, "activity_id": id, item.arg: value, "message": label + " successfully updated"}, nil
		}, "activity_id", item.arg)
	}
}
