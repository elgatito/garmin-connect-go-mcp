package server

import (
	"bytes"
	"context"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
)

func coursePayload(skeleton map[string]any, name string, typeID int, description any) (map[string]any, error) {
	raw, _ := skeleton["geoPoints"].([]any)
	if len(raw) == 0 {
		return nil, fmt.Errorf("GPX parsed skeleton contains no geoPoints")
	}
	points := []any{}
	minLat, maxLat, minLon, maxLon := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
	var lastLat, lastLon, firstLat, firstLon float64
	distance := float64(0)
	for i, v := range raw {
		p, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid geoPoint")
		}
		lat, lok := p["latitude"].(float64)
		lon, ook := p["longitude"].(float64)
		if !lok || !ook || math.Abs(lat) > 90 || math.Abs(lon) > 180 {
			return nil, fmt.Errorf("invalid geoPoint coordinates")
		}
		copyPoint := map[string]any{}
		for k, v := range p {
			copyPoint[k] = v
		}
		if copyPoint["elevation"] == nil {
			copyPoint["elevation"] = float64(0)
		}
		points = append(points, copyPoint)
		minLat = math.Min(minLat, lat)
		maxLat = math.Max(maxLat, lat)
		minLon = math.Min(minLon, lon)
		maxLon = math.Max(maxLon, lon)
		if i == 0 {
			firstLat, firstLon = lat, lon
		} else {
			lat1, lat2 := lastLat*math.Pi/180, lat*math.Pi/180
			dlat, dlon := (lat-lastLat)*math.Pi/180, (lon-lastLon)*math.Pi/180
			a := math.Pow(math.Sin(dlat/2), 2) + math.Cos(lat1)*math.Cos(lat2)*math.Pow(math.Sin(dlon/2), 2)
			distance += 2 * 6371000 * math.Asin(math.Sqrt(math.Min(1, a)))
		}
		lastLat, lastLon = lat, lon
	}
	lat1, lat2 := firstLat*math.Pi/180, lastLat*math.Pi/180
	dlon := (lastLon - firstLon) * math.Pi / 180
	bearing := math.Mod(math.Atan2(math.Sin(dlon)*math.Cos(lat2), math.Cos(lat1)*math.Sin(lat2)-math.Sin(lat1)*math.Cos(lat2)*math.Cos(dlon))*180/math.Pi+360, 360)
	first := points[0].(map[string]any)
	return map[string]any{"courseName": name, "description": description, "openStreetMap": false, "matchedToSegments": false, "userProfilePk": nil, "userGroupPk": nil, "rulePK": 2, "geoRoutePk": nil, "sourceTypeId": 3, "sourcePk": nil, "distanceMeter": distance, "elevationGainMeter": 0.0, "elevationLossMeter": 0.0, "startPoint": map[string]any{"latitude": firstLat, "longitude": firstLon, "elevation": first["elevation"], "distance": nil, "timestamp": nil}, "coursePoints": []any{}, "boundingBox": map[string]any{"center": map[string]any{"latitude": (minLat + maxLat) / 2, "longitude": (minLon + maxLon) / 2}, "lowerLeft": map[string]any{"latitude": minLat, "longitude": minLon}, "upperRight": map[string]any{"latitude": maxLat, "longitude": maxLon}, "lowerLeftLatIsSet": true, "lowerLeftLongIsSet": true, "upperRightLatIsSet": true, "upperRightLongIsSet": true}, "hasShareableEvent": false, "hasTurnDetectionDisabled": false, "activityTypePk": typeID, "virtualPartnerId": nil, "includeLaps": false, "elapsedSeconds": nil, "startBearing": bearing, "endBearing": nil, "geoPoints": points}, nil
}
func registerCourseUpload(srv *mcp.Server, m *manager) {
	addTool(srv, m, "upload_course", description("upload_course"), map[string]any{"gpx_path": map[string]any{"type": "string"}, "course_name": map[string]any{"type": "string"}, "activity_type": map[string]any{"type": "string", "default": "running"}, "description": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		path, ok := args["gpx_path"].(string)
		if !ok {
			return nil, fmt.Errorf("gpx_path is required")
		}
		ext := filepath.Ext(path)
		if strings.ToLower(ext) != ".gpx" {
			if ext == "" {
				ext = "(no extension)"
			}
			return fmt.Sprintf("Error: only .gpx files are allowed, got: %s", ext), nil
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(abs)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Sprintf("Error: GPX file not found: %s", abs), nil
		}
		kind := "running"
		if v, ok := args["activity_type"].(string); ok {
			kind = v
		}
		ids := map[string]int{"running": 1, "cycling": 2, "hiking": 3, "walking": 9, "trail_running": 6, "mountain_biking": 5, "road_biking": 10, "gravel_cycling": 143}
		id, ok := ids[strings.ToLower(kind)]
		if !ok {
			return fmt.Sprintf("Error: unknown activity_type '%s'. Supported: cycling, gravel_cycling, hiking, mountain_biking, road_biking, running, trail_running, walking.", kind), nil
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return nil, fmt.Errorf("Error uploading course: %w", err)
		}
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": filepath.Base(abs)}))
		header.Set("Content-Type", "application/gpx+xml")
		part, err := writer.CreatePart(header)
		if err != nil {
			return nil, err
		}
		if _, err = part.Write(data); err != nil {
			return nil, err
		}
		if err = writer.Close(); err != nil {
			return nil, err
		}
		tracker := newWriteTracker("upload_course", "Error uploading course", m.opt.Log)
		v, err := s.Client.DoJSON(ctx, garmin.Req{Method: http.MethodPost, Path: "/course-service/course/import", Body: body.Bytes(), ContentType: writer.FormDataContentType()})
		if err != nil {
			return nil, tracker.Fail(err)
		}
		parsed, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Error uploading course: invalid import response")
		}
		name, _ := args["course_name"].(string)
		if name == "" {
			name, _ = parsed["courseName"].(string)
		}
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))
		}
		payload, err := coursePayload(parsed, name, id, args["description"])
		if err != nil {
			return nil, fmt.Errorf("Error uploading course: %w", err)
		}
		tracker.Sent()
		v, err = s.Client.DoJSON(ctx, garmin.Req{Method: http.MethodPost, Path: "/course-service/course", JSON: payload})
		if err != nil {
			return nil, tracker.Fail(err)
		}
		tracker.Committed()
		saved, _ := v.(map[string]any)
		return map[string]any{"status": "success", "course_id": saved["courseId"], "name": saved["courseName"], "distance_m": saved["distanceMeter"], "elevation_gain_m": saved["elevationGainMeter"], "elevation_loss_m": saved["elevationLossMeter"], "activity_type_id": saved["activityTypePk"], "url": fmt.Sprintf("https://connect.%s/modern/course/%v", s.Client.Domain(), saved["courseId"])}, nil
	}, "gpx_path")
}
