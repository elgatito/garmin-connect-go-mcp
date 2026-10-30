package server

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"math"
)

func activityWeather(id int64, w map[string]any, units string) map[string]any {
	out := map[string]any{"activity_id": id}
	for k, src := range map[string]string{"humidity_percent": "relativeHumidity", "wind_speed": "windSpeed", "wind_direction_degrees": "windDirection", "wind_direction_compass": "windDirectionCompassPoint", "wind_gust": "windGust", "location": "issueLocation", "issue_time": "issueDate"} {
		if w[src] != nil {
			out[k] = w[src]
		}
	}
	for k, src := range map[string]string{"temperature": "temp", "apparent_temperature": "apparentTemp", "dew_point": "dewPoint"} {
		v := w[src]
		if n, ok := v.(float64); ok && units != "" && units != "statute_us" {
			v = math.RoundToEven((n-32)*5/9*10) / 10
		}
		if v != nil {
			out[k] = v
		}
	}
	if units == "statute_us" {
		out["temperature_unit"] = "F"
		out["wind_speed_unit"] = "mph"
	} else if units != "" {
		out["temperature_unit"] = "C"
		out["wind_speed_unit"] = "km/h"
	}
	typ, _ := w["weatherTypeDTO"].(map[string]any)
	station, _ := w["weatherStationDTO"].(map[string]any)
	if typ["weatherTypeName"] != nil {
		out["weather_type"] = typ["weatherTypeName"]
	}
	desc := typ["desc"]
	if !truthy(desc) {
		desc = typ["weatherTypeDesc"]
	}
	if desc != nil {
		out["weather_description"] = desc
	}
	if station["id"] != nil {
		out["station_id"] = station["id"]
	}
	if station["name"] != nil {
		out["station_name"] = station["name"]
	}
	return out
}
func registerActivityWeather(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_activity_weather", description("get_activity_weather"), map[string]any{"activity_id": map[string]any{"anyOf": []any{map[string]any{"type": "integer"}, map[string]any{"type": "string"}}}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		id, err := activityID(args["activity_id"])
		if err != nil {
			return nil, fmt.Errorf("Error retrieving activity weather data: %w", err)
		}
		v, err := s.Client.GetJSON(ctx, fmt.Sprintf("/activity-service/activity/%d/weather", id), nil)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving activity weather data: %w", err)
		}
		if empty(v) {
			return fmt.Sprintf("No weather data found for activity with ID %d", id), nil
		}
		w, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Error retrieving activity weather data: unexpected response")
		}
		units := ""
		if p, err := s.Client.Profile(ctx); err == nil {
			units = p.UnitSystem
		}
		return activityWeather(id, w, units), nil
	}, "activity_id")
}
