package server

import (
	"context"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"math"
	"net/http"
	"net/url"
)

func registerWeightDelete(srv *mcp.Server, m *manager) {
	addTool(srv, m, "delete_weigh_ins", description("delete_weigh_ins"), map[string]any{"date": map[string]any{"type": "string"}, "delete_all": map[string]any{"type": "boolean", "default": true}, "confirm": map[string]any{"type": "boolean", "default": false}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		date, err := dateArgument(args, "date")
		if err != nil {
			return nil, fmt.Errorf("Error deleting weight measurements: %w", err)
		}
		all := true
		if v, ok := args["delete_all"].(bool); ok {
			all = v
		}
		confirm, _ := args["confirm"].(bool)
		tracker := newWriteTracker("delete_weigh_ins", "Error deleting weight measurements", m.opt.Log)
		v, err := s.Client.GetJSON(ctx, "/weight-service/weight/dayview/"+date, url.Values{"includeAll": {"True"}})
		if err != nil {
			return nil, tracker.Fail(err)
		}
		data, _ := v.(map[string]any)
		rows, _ := data["dateWeightList"].([]any)
		if len(rows) == 0 || len(rows) > 1 && !all {
			message := fmt.Sprintf("No weight measurements found for %s; nothing was deleted", date)
			if len(rows) > 1 {
				message = fmt.Sprintf("Nothing was deleted for %s: deleting multiple measurements requires delete_all=true", date)
			}
			if confirm && !all {
				message = fmt.Sprintf("Nothing was deleted for %s: the day has either no weight measurements or more than one, and deleting more than one requires delete_all=true", date)
			}
			return map[string]any{"status": "nothing_deleted", "date": date, "deleted_count": 0, "message": message}, nil
		}
		if !confirm {
			preview := []any{}
			for _, v := range rows {
				item, _ := v.(map[string]any)
				row := remapFields(item, map[string]string{"id": "samplePk", "source_type": "sourceType", "timestamp_gmt": "timestampGMT"})
				if n, ok := item["weight"].(float64); ok {
					row["weight_kg"] = math.RoundToEven(n/10) / 100
				}
				preview = append(preview, row)
			}
			return map[string]any{"status": "needs_confirmation", "date": date, "delete_all": all, "measurement_count": len(preview), "measurements": preview, "message": "This permanently deletes the listed weight measurements and cannot be undone. Call again with confirm=true to proceed."}, nil
		}
		ids := make([]string, 0, len(rows))
		for _, v := range rows {
			item, _ := v.(map[string]any)
			var id string
			switch value := item["samplePk"].(type) {
			case string:
				id = value
			case float64:
				if value > 0 && value == math.Trunc(value) {
					id = fmt.Sprintf("%.0f", value)
				}
			}
			if !regexpGearUUID(id) {
				return nil, fmt.Errorf("Error deleting weight measurements: invalid sample ID")
			}
			ids = append(ids, id)
		}
		for _, id := range ids {
			tracker.Sent()
			_, err = s.Client.Do(ctx, garmin.Req{Method: http.MethodDelete, Path: "/weight-service/weight/" + date + "/byversion/" + id})
			if err != nil {
				return nil, tracker.Fail(err)
			}
			tracker.Committed()
		}
		return map[string]any{"status": "success", "date": date, "deleted_count": len(rows), "message": fmt.Sprintf("Weight measurements deleted for %s", date)}, nil
	}, "date")
}
