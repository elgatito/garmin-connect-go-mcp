package server

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"math"
	"net/url"
)

func solarSummary(data []any) []any {
	out := []any{}
	for _, v := range data {
		day, ok := v.(map[string]any)
		if !ok {
			continue
		}
		row := map[string]any{}
		if day["localConnectDate"] != nil {
			row["date"] = day["localConnectDate"]
		}
		if n, ok := day["totalActivityTimeGainedMs"].(float64); ok {
			row["activity_time_gained_minutes"] = math.RoundToEven(n/6000) / 10
		}
		readings, _ := day["solarInputReadings"].([]any)
		sum, max := float64(0), float64(0)
		count, positive := 0, 0
		for _, v := range readings {
			reading, _ := v.(map[string]any)
			n, ok := reading["solarUtilization"].(float64)
			if !ok {
				continue
			}
			if count == 0 || n > max {
				max = n
			}
			sum += n
			count++
			if n > 0 {
				positive++
			}
		}
		if count > 0 {
			row["solar_utilization_avg_pct"] = math.RoundToEven(sum/float64(count)*100) / 100
			row["solar_utilization_max_pct"] = max
			row["minutes_with_solar_input"] = positive
		}
		out = append(out, row)
	}
	return out
}
func registerSolar(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_device_solar_data", description("get_device_solar_data"), map[string]any{"device_id": map[string]any{"anyOf": []any{map[string]any{"type": "integer"}, map[string]any{"type": "string"}}}, "date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		id, err := activityID(args["device_id"])
		if err != nil {
			return nil, fmt.Errorf("Error retrieving solar data: %w", err)
		}
		date, err := dateArgument(args, "date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving solar data: %w", err)
		}
		v, err := s.Client.GetJSON(ctx, fmt.Sprintf("/web-gateway/solar/%d/%s/%s", id, date, date), url.Values{"singleDayView": {"True"}})
		if err != nil {
			return nil, fmt.Errorf("Error retrieving solar data: %w", err)
		}
		raw, _ := v.(map[string]any)
		solar, ok := raw["deviceSolarInput"].(map[string]any)
		if !ok {
			return fmt.Sprintf("No solar data available for device ID %v on %s. This device may not have solar capabilities.", args["device_id"], date), nil
		}
		if len(solar) == 0 {
			return fmt.Sprintf("No solar data found for device ID %v on %s.", args["device_id"], date), nil
		}
		days, _ := solar["solarDailyDataDTOs"].([]any)
		if len(days) == 0 {
			return fmt.Sprintf("No solar data available for device ID %v on %s. This device may not have solar capabilities.", args["device_id"], date), nil
		}
		return map[string]any{"device_id": args["device_id"], "solar_data": solarSummary(days)}, nil
	}, "device_id", "date")
}
