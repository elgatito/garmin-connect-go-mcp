package server

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

func normalizedGearUUID(v any) string {
	s, _ := v.(string)
	return strings.ToLower(strings.ReplaceAll(s, "-", ""))
}
func gearActivityName(v any) string {
	n, _ := v.(float64)
	names := map[int]string{1: "running", 2: "cycling", 3: "hiking", 4: "other", 5: "mountain_biking", 6: "trail_running", 7: "street_running", 8: "track_running", 9: "walking", 10: "road_biking", 13: "strength_training", 15: "casual_walking", 16: "speed_walking", 18: "treadmill_running", 19: "cyclocross", 20: "downhill_biking", 21: "track_cycling", 22: "recumbent_cycling", 25: "indoor_cycling", 26: "swimming", 27: "lap_swimming", 28: "open_water_swimming", 143: "gravel_cycling", 152: "virtual_ride", 153: "virtual_run", 154: "obstacle_run", 156: "indoor_running", 175: "e_bike_mountain", 176: "e_bike_fitness", 181: "ultra_run"}
	if name := names[int(n)]; name != "" {
		return name
	}
	return fmt.Sprintf("activity_%v", v)
}
func registerGear(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_gear", description("get_gear"), map[string]any{"include_stats": map[string]any{"type": "boolean", "default": true}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		v, err := s.Client.GetJSON(ctx, "/device-service/deviceservice/mylastused", nil)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving gear: %w", err)
		}
		if empty(v) {
			return "Could not retrieve user profile. Please ensure you have a synced device.", nil
		}
		device, _ := v.(map[string]any)
		profile := fmt.Sprint(device["userProfileNumber"])
		v, err = s.Client.GetJSON(ctx, "/gear-service/gear/filterGear", url.Values{"userProfilePk": {profile}})
		if err != nil {
			return nil, fmt.Errorf("Error retrieving gear: %w", err)
		}
		if empty(v) {
			return "No gear found.", nil
		}
		gears, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("Error retrieving gear: unexpected response")
		}
		notes := map[string]any{}
		if v, err := s.Client.GetJSON(ctx, "/gear-service/gear/v2/list", nil); err == nil {
			if list, ok := v.([]any); ok {
				for _, item := range list {
					g, _ := item.(map[string]any)
					if key := normalizedGearUUID(g["uuid"]); key != "" {
						notes[key] = g["notes"]
					}
				}
			}
		}
		v, err = s.Client.GetJSON(ctx, "/gear-service/gear/user/"+url.PathEscape(profile)+"/activityTypes", nil)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving gear: %w", err)
		}
		defaultList, _ := v.([]any)
		defaults := map[string][]string{}
		for _, v := range defaultList {
			d, _ := v.(map[string]any)
			uuid, _ := d["uuid"].(string)
			defaults[uuid] = append(defaults[uuid], gearActivityName(d["activityTypePk"]))
		}
		include := true
		if val, ok := args["include_stats"].(bool); ok {
			include = val
		}
		out := []map[string]any{}
		active, retired := 0, 0
		for _, v := range gears {
			g, ok := v.(map[string]any)
			if !ok {
				continue
			}
			uuid, _ := g["uuid"].(string)
			status, _ := g["gearStatusName"].(string)
			status = strings.ToLower(status)
			if status == "active" {
				active++
			}
			if status == "retired" {
				retired++
			}
			note, exists := notes[normalizedGearUUID(uuid)]
			if !exists {
				note = g["notes"]
			}
			item := map[string]any{"uuid": g["uuid"], "name": g["displayName"], "full_name": g["customMakeModel"], "type": g["gearTypeName"], "status": status, "date_begin": shortDate(g["dateBegin"]), "date_end": shortDate(g["dateEnd"]), "notes": note}
			if n, ok := g["maximumMeters"].(float64); ok && n > 0 {
				item["max_distance_km"] = math.RoundToEven(n/100) / 10
			}
			if list, ok := defaults[uuid]; ok {
				item["is_default_for"] = list
			}
			if include {
				if !regexpGearUUID(uuid) {
					item["stats_error"] = "invalid gear UUID"
				} else if v, err := s.Client.GetJSON(ctx, "/gear-service/gear/stats/"+uuid, nil); err != nil {
					item["stats_error"] = err.Error()
				} else if stats, ok := v.(map[string]any); ok && len(stats) > 0 {
					var distance any
					if n, ok := stats["totalDistance"].(float64); ok {
						distance = math.RoundToEven(n/100) / 10
					}
					item["stats"] = map[string]any{"total_activities": stats["totalActivities"], "total_distance_km": distance}
				}
			}
			out = append(out, item)
		}
		sort.SliceStable(out, func(i, j int) bool {
			a, b := out[i]["status"] == "active", out[j]["status"] == "active"
			if a != b {
				return a
			}
			x, _ := out[i]["date_begin"].(string)
			y, _ := out[j]["date_begin"].(string)
			return x > y
		})
		summary := map[string][]string{}
		for uuid, activities := range defaults {
			name := uuid
			for _, g := range out {
				if g["uuid"] == uuid {
					if v, ok := g["name"].(string); ok && v != "" {
						name = v
					} else if v, ok := g["full_name"].(string); ok && v != "" {
						name = v
					}
					break
				}
			}
			for _, a := range activities {
				summary[a] = append(summary[a], name)
			}
		}
		return map[string]any{"gear_count": len(out), "active_count": active, "retired_count": retired, "defaults": summary, "gear": out}, nil
	})
}

func regexpGearUUID(v string) bool { return regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(v) }
