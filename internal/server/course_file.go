package server

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func coursePreview(ctx context.Context, s *Session, id int64) map[string]any {
	out := map[string]any{"course_id": id}
	v, err := s.Client.GetJSON(ctx, fmt.Sprintf("/course-service/course/%d", id), nil)
	data, _ := v.(map[string]any)
	if err != nil || len(data) == 0 {
		v, err = s.Client.GetJSON(ctx, "/course-service/course", nil)
		if err != nil {
			return out
		}
		list, _ := v.([]any)
		for _, item := range list {
			c, _ := item.(map[string]any)
			if fmt.Sprint(c["courseId"]) == fmt.Sprint(id) {
				data = c
				break
			}
		}
	}
	if len(data) == 0 {
		return out
	}
	typ, _ := data["activityType"].(map[string]any)
	distance := data["distanceInMeters"]
	if !truthy(distance) {
		distance = data["distanceMeter"]
	}
	for k, v := range map[string]any{"name": data["courseName"], "activity": typ["typeKey"], "distance_m": distance, "created": data["createdDateFormatted"]} {
		if v != nil {
			out[k] = v
		}
	}
	return out
}
func xmlText(value any) string {
	var b bytes.Buffer
	xml.EscapeText(&b, []byte(fmt.Sprint(value)))
	return b.String()
}
func decimal(v any) string {
	n, ok := v.(float64)
	if !ok {
		return ""
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}
func buildGPX(id int64, data map[string]any) ([]byte, string, int, int) {
	name, _ := data["courseName"].(string)
	if name == "" {
		name = fmt.Sprintf("Garmin Course %d", id)
	}
	points, _ := data["coursePoints"].([]any)
	geo, _ := data["geoPoints"].([]any)
	lines := []string{"<?xml version='1.0' encoding='UTF-8'?>", "<gpx version='1.1' creator='GarminConnectMCP' xmlns='http://www.topografix.com/GPX/1/1'>", "  <metadata>", "    <name>" + xmlText(name) + "</name>", "  </metadata>"}
	for _, v := range points {
		p, _ := v.(map[string]any)
		if decimal(p["lat"]) == "" || decimal(p["lon"]) == "" {
			continue
		}
		n := p["name"]
		if !truthy(n) {
			n = p["pointType"]
		}
		if !truthy(n) {
			n = "Waypoint"
		}
		typ := p["pointType"]
		if !truthy(typ) {
			typ = "GENERIC"
		}
		lines = append(lines, fmt.Sprintf("  <wpt lat='%s' lon='%s'>", decimal(p["lat"]), decimal(p["lon"])), "    <name>"+xmlText(n)+"</name>", "    <type>"+xmlText(typ)+"</type>", "  </wpt>")
	}
	lines = append(lines, "  <trk>", "    <name>"+xmlText(name)+"</name>", "    <trkseg>")
	for _, v := range geo {
		p, _ := v.(map[string]any)
		if decimal(p["latitude"]) == "" || decimal(p["longitude"]) == "" {
			continue
		}
		ele := ""
		if decimal(p["elevation"]) != "" {
			ele = "<ele>" + decimal(p["elevation"]) + "</ele>"
		}
		lines = append(lines, fmt.Sprintf("      <trkpt lat='%s' lon='%s'>%s</trkpt>", decimal(p["latitude"]), decimal(p["longitude"]), ele))
	}
	lines = append(lines, "    </trkseg>", "  </trk>", "</gpx>")
	return []byte(strings.Join(lines, "\n")), name, len(points), len(geo)
}
func registerCourseFile(srv *mcp.Server, m *manager) {
	addTool(srv, m, "delete_course", description("delete_course"), map[string]any{"course_id": map[string]any{"type": "integer"}, "confirm": map[string]any{"type": "boolean", "default": false}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		id, err := activityID(args["course_id"])
		if err != nil {
			return nil, fmt.Errorf("Error deleting course: %w", err)
		}
		preview := coursePreview(ctx, s, id)
		confirm, _ := args["confirm"].(bool)
		if !confirm {
			return map[string]any{"status": "needs_confirmation", "course": preview, "message": "This permanently deletes the course from Garmin Connect and cannot be undone. Call again with confirm=true to proceed."}, nil
		}
		tracker := newWriteTracker("delete_course", "Error deleting course", m.opt.Log)
		tracker.Sent()
		_, err = s.Client.Do(ctx, garmin.Req{Method: http.MethodDelete, Path: fmt.Sprintf("/course-service/course/%d", id)})
		if err != nil {
			return nil, tracker.Fail(err)
		}
		tracker.Committed()
		fmt.Fprintf(m.opt.Log, "delete_course: deleted course %d at %s\n", id, time.Now().UTC().Format(time.RFC3339))
		return map[string]any{"status": "success", "course_id": id, "course": preview, "message": fmt.Sprintf("Course %d deleted", id)}, nil
	}, "course_id")
	addTool(srv, m, "download_course_gpx", description("download_course_gpx"), map[string]any{"course_id": map[string]any{"type": "integer"}, "output_path": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		id, err := activityID(args["course_id"])
		if err != nil {
			return nil, fmt.Errorf("Error downloading course GPX: %w", err)
		}
		given, _ := args["output_path"].(string)
		target := ""
		if given != "" {
			target, err = absoluteDir(given)
			if err != nil {
				return nil, err
			}
			info, e := os.Stat(target)
			if (e == nil && info.IsDir()) || strings.HasSuffix(given, "/") || strings.HasSuffix(given, `\`) {
				target = filepath.Join(target, fmt.Sprintf("%d.gpx", id))
			} else if !strings.HasSuffix(strings.ToLower(target), ".gpx") {
				return nil, fmt.Errorf("Error downloading course GPX: output_path must be a directory or a .gpx file, got: %s", given)
			}
		} else {
			dir := m.opt.Getenv("GARMIN_FIT_DOWNLOAD_DIR")
			if dir == "" {
				settings, e := s.Settings()
				if e != nil {
					return nil, e
				}
				dir = settings.FitDownloadDir
			}
			if dir == "" {
				dir = "./courses"
			}
			dir, err = absoluteDir(dir)
			if err != nil {
				return nil, err
			}
			target = filepath.Join(dir, fmt.Sprintf("%d.gpx", id))
		}
		v, err := s.Client.GetJSON(ctx, fmt.Sprintf("/course-service/course/%d", id), nil)
		if err != nil {
			return nil, fmt.Errorf("Error downloading course GPX: %w", err)
		}
		data, _ := v.(map[string]any)
		if empty(data["geoPoints"]) {
			return fmt.Sprintf("Error: course %d not found or missing geoPoints.", id), nil
		}
		body, name, waypoints, points := buildGPX(id, data)
		if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return nil, fmt.Errorf("Error downloading course GPX: %w", err)
		}
		if err = saveDownloadedFile(target, body); err != nil {
			return nil, fmt.Errorf("Error downloading course GPX: %w", err)
		}
		return map[string]any{"status": "success", "course_id": id, "name": name, "gpx_path": target, "waypoints_count": waypoints, "track_points_count": points}, nil
	}, "course_id")
}
