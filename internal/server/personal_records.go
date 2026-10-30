package server

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/url"
	"sort"
	"time"
)

func personalRecords(rows []any) []map[string]any {
	names := []string{"", "Fastest 1K", "Fastest Mile", "Fastest 5K", "Fastest 10K", "Fastest Half Marathon", "Fastest Marathon", "Longest Run", "Longest Ride", "Most Elevation Gain Cycling", "Fastest 100K Cycling", "Fastest 40K Cycling", "Most Steps Day", "Most Steps Week", "Most Steps Month", "Longest Daily Goal Streak", "Longest Weekly Goal Streak", "Longest Pool Swim", "Fastest 100m Pool Swim", "Fastest 400m Pool Swim", "Fastest 500m Pool Swim", "Fastest 800m Pool Swim", "Fastest 1500m Pool Swim", "Fastest 1 Mile Pool Swim"}
	out := []map[string]any{}
	for _, v := range rows {
		raw, ok := v.(map[string]any)
		if !ok {
			continue
		}
		id, _ := raw["typeId"].(float64)
		n := int(id)
		name := fmt.Sprintf("Unknown Record (typeId=%v)", raw["typeId"])
		if n > 0 && n < len(names) {
			name = names[n]
		}
		var formatted any
		val := raw["value"]
		if value, ok := val.(float64); ok {
			switch {
			case n >= 1 && n <= 6 || n == 10 || n == 11 || n >= 18 && n <= 23:
				formatted = predictionTime(value)
			case n == 7 || n == 8 || n == 17:
				formatted = badgeValue(value, 1)
			case n == 9:
				formatted = badgeValue(value, 2)
			case n >= 12 && n <= 14:
				formatted = badgeValue(value, 3)
			case n == 15 || n == 16:
				formatted = fmt.Sprintf("%d days", int(value))
			default:
				formatted = fmt.Sprint(value)
			}
		}
		date := shortDate(raw["prStartTimeLocalFormatted"])
		if date == nil {
			if ms, ok := raw["prStartTimeGmt"].(float64); ok {
				date = time.UnixMilli(int64(ms)).UTC().Format("2006-01-02")
			}
		}
		entry := map[string]any{"record_type": name, "type_id": raw["typeId"], "value": formatted, "raw_value": val, "date": date}
		if truthy(raw["activityId"]) {
			entry["activity_id"] = raw["activityId"]
		}
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, _ := out[i]["type_id"].(float64)
		b, _ := out[j]["type_id"].(float64)
		return a < b
	})
	return out
}
func registerPersonalRecords(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_personal_record", description("get_personal_record"), nil, func(ctx context.Context, s *Session, _ map[string]any) (any, error) {
		p, err := s.Client.Profile(ctx)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving personal records: %w", err)
		}
		v, err := s.Client.GetJSON(ctx, "/personalrecord-service/personalrecord/prs/"+url.PathEscape(p.DisplayName), nil)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving personal records: %w", err)
		}
		if empty(v) {
			return "No personal records found.", nil
		}
		rows, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("Error retrieving personal records: unexpected response")
		}
		return personalRecords(rows), nil
	})
}
