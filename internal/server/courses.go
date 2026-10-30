package server

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerCourses(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_courses", description("get_courses"), nil, func(ctx context.Context, s *Session, _ map[string]any) (any, error) {
		v, err := s.Client.GetJSON(ctx, "/course-service/course", nil)
		if err != nil {
			return nil, fmt.Errorf("Error listing courses: %w", err)
		}
		rows, ok := v.([]any)
		if !ok {
			return v, nil
		}
		out := []any{}
		for _, v := range rows {
			c, ok := v.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("Error listing courses: unexpected response")
			}
			activity, _ := c["activityType"].(map[string]any)
			out = append(out, map[string]any{"course_id": c["courseId"], "name": c["courseName"], "distance_m": c["distanceInMeters"], "elevation_gain_m": c["elevationGainInMeters"], "elevation_loss_m": c["elevationLossInMeters"], "activity": activity["typeKey"], "has_pace_band": c["hasPaceBand"], "created": c["createdDateFormatted"]})
		}
		return map[string]any{"count": len(out), "courses": out}, nil
	})
	addTool(srv, m, "get_course_details", description("get_course_details"), map[string]any{"course_id": map[string]any{"type": "integer"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		id, err := intArg(args, "course_id", 0)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("Error fetching course details: course_id must be a positive integer")
		}
		v, err := s.Client.GetJSON(ctx, fmt.Sprintf("/course-service/course/%d", id), nil)
		if err != nil {
			return nil, fmt.Errorf("Error fetching course details: %w", err)
		}
		raw, ok := v.(map[string]any)
		if !ok {
			return v, nil
		}
		points, _ := raw["coursePoints"].([]any)
		waypoints := []any{}
		for _, v := range points {
			cp, ok := v.(map[string]any)
			if !ok {
				continue
			}
			waypoints = append(waypoints, map[string]any{"name": cp["name"], "type": cp["pointType"], "lat": cp["lat"], "lon": cp["lon"], "distance_m": cp["distance"]})
		}
		geo, _ := raw["geoPoints"].([]any)
		activity, _ := raw["activityType"].(map[string]any)
		fallback := func(a, b string) any {
			v := raw[a]
			if v == nil || v == float64(0) {
				return raw[b]
			}
			return v
		}
		return map[string]any{"course_id": raw["courseId"], "name": raw["courseName"], "distance_m": fallback("distanceInMeters", "distanceMeter"), "elevation_gain_m": fallback("elevationGainInMeters", "elevationGainMeter"), "elevation_loss_m": fallback("elevationLossInMeters", "elevationLossMeter"), "activity": activity["typeKey"], "waypoints_count": len(waypoints), "waypoints": waypoints, "geo_points_count": len(geo), "url": fmt.Sprintf("https://connect.%s/modern/course/%d", s.Client.Domain(), id)}, nil
	}, "course_id")
}
