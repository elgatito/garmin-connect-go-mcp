package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

const (
	getCoursesDescription = `List all courses saved on Garmin Connect.

Returns a curated list of courses with id, name, distance, activity type
and creation date.`

	getCourseDetailsDescription = `Get full details of a Garmin Connect course by ID.

Returns course metadata, elevation gain/loss, total distance,
and all custom course waypoints (shops, water, food, campgrounds, hazards).`

	downloadCourseGpxDescription = `Download a Garmin Connect course as a GPX file.

The GPX is rebuilt from the course's track points (lat/lon/elevation)
and course points (as waypoints); it is not the originally uploaded file.`

	uploadCourseDescription = `Upload a GPX file as a Garmin Connect Course.

The course can then be loaded onto the watch (sync or "Send to Device")
and used as a navigation course or to build a PacePro strategy.`

	deleteCourseDescription = `Delete a course from Garmin Connect.

This cannot be undone. Call once with confirm=false (the default) to
preview the course, then call again with confirm=true to delete it.`

	earthRadiusM = 6371000.0
)

var courseActivityTypeIDs = map[string]int{
	"running":         1,
	"cycling":         2,
	"hiking":          3,
	"walking":         9,
	"trail_running":   6,
	"mountain_biking": 5,
	"road_biking":     10,
	"gravel_cycling":  143,
}

type coursesInput struct {
	Account
}

type courseIDInput struct {
	Account
	CourseID int64 `json:"course_id" jsonschema:"ID of the course (from get_courses)."`
}

type downloadCourseGpxInput struct {
	Account
	CourseID   int64  `json:"course_id" jsonschema:"ID of the course to download."`
	OutputPath string `json:"output_path,omitempty" jsonschema:"Optional destination directory (saved as {course_id}.gpx) or file path ending in .gpx; any other file path is rejected. Defaults to the download_activity_file directory (GARMIN_FIT_DOWNLOAD_DIR, then set_fit_download_dir), else ./courses/{course_id}.gpx."`
}

type uploadCourseInput struct {
	Account
	GpxPath      string  `json:"gpx_path" jsonschema:"Absolute path to the .gpx file on disk."`
	CourseName   string  `json:"course_name,omitempty" jsonschema:"Override the course name. Defaults to the name parsed from the GPX file. Max 128 characters; Garmin rejects longer."`
	ActivityType string  `json:"activity_type,omitempty" jsonschema:"One of running, cycling, hiking, walking, trail_running, mountain_biking, road_biking, gravel_cycling. Defaults to running."`
	Description  *string `json:"description,omitempty" jsonschema:"Optional description shown on the course detail page. Max 512 characters; Garmin rejects longer."`
}

type deleteCourseInput struct {
	Account
	CourseID int64 `json:"course_id" jsonschema:"ID of the course to delete (get IDs from get_courses)."`
	Confirm  bool  `json:"confirm,omitempty" jsonschema:"Must be true to actually delete. Default false returns a preview."`
}

func registerCourseTools(r *registry) {
	addTool(r, "get_courses", getCoursesDescription, getCourses)
	addTool(r, "get_course_details", getCourseDetailsDescription, getCourseDetails)
	addTool(r, "download_course_gpx", downloadCourseGpxDescription, downloadCourseGpx)
	addTool(r, "upload_course", uploadCourseDescription, uploadCourse)
	addTool(r, "delete_course", deleteCourseDescription, deleteCourse)
}

func getCourses(ctx context.Context, s *Session, _ coursesInput) (any, error) {
	data, err := s.Client.GetCourses(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error listing courses: %w", err)
	}
	list, ok := data.([]any)
	if !ok {
		return data, nil
	}
	curated := make([]map[string]any, 0, len(list))
	for _, item := range list {
		course := asMap(item)
		activity := asMap(course["activityType"])["typeKey"]
		curated = append(curated, map[string]any{
			"course_id":        course["courseId"],
			"name":             course["courseName"],
			"distance_m":       course["distanceInMeters"],
			"elevation_gain_m": course["elevationGainInMeters"],
			"elevation_loss_m": course["elevationLossInMeters"],
			"activity":         activity,
			"has_pace_band":    course["hasPaceBand"],
			"created":          course["createdDateFormatted"],
		})
	}
	return map[string]any{"count": len(curated), "courses": curated}, nil
}

func firstTruthyValue(values ...any) any {
	for _, value := range values {
		if truthy(value) {
			return value
		}
	}
	return nil
}

func getCourseDetails(ctx context.Context, s *Session, in courseIDInput) (any, error) {
	dataValue, err := s.Client.GetCourse(ctx, in.CourseID)
	if err != nil {
		return nil, fmt.Errorf("Error fetching course details: %w", err)
	}
	data, ok := dataValue.(map[string]any)
	if !ok {
		return dataValue, nil
	}
	coursePoints := make([]map[string]any, 0)
	for _, item := range asList(data["coursePoints"]) {
		point := asMap(item)
		coursePoints = append(coursePoints, map[string]any{
			"name":       point["name"],
			"type":       point["pointType"],
			"lat":        point["lat"],
			"lon":        point["lon"],
			"distance_m": point["distance"],
		})
	}
	geoPoints := asList(data["geoPoints"])
	return map[string]any{
		"course_id":        data["courseId"],
		"name":             data["courseName"],
		"distance_m":       firstTruthyValue(data["distanceInMeters"], data["distanceMeter"]),
		"elevation_gain_m": firstTruthyValue(data["elevationGainInMeters"], data["elevationGainMeter"]),
		"elevation_loss_m": firstTruthyValue(data["elevationLossInMeters"], data["elevationLossMeter"]),
		"activity":         asMap(data["activityType"])["typeKey"],
		"waypoints_count":  len(coursePoints),
		"waypoints":        coursePoints,
		"geo_points_count": len(geoPoints),
		"url":              fmt.Sprintf("https://connect.%s/modern/course/%d", s.Client.Domain(), in.CourseID),
	}, nil
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

func resolveDownloadDir() string {
	if dir := os.Getenv("GARMIN_FIT_DOWNLOAD_DIR"); dir != "" {
		abs, err := filepath.Abs(expandHome(dir))
		if err == nil {
			return abs
		}
	}
	configPath := os.Getenv("GARMIN_FIT_CONFIG")
	if configPath == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			configPath = filepath.Join(home, ".garminconnect_fit_config.json")
		}
	}
	raw, err := os.ReadFile(expandHome(configPath))
	if err == nil {
		var config map[string]any
		if json.Unmarshal(raw, &config) == nil {
			if dir, ok := config["download_dir"].(string); ok && dir != "" {
				abs, err := filepath.Abs(expandHome(dir))
				if err == nil {
					return abs
				}
			}
		}
	}
	return ""
}

func resolveGPXOutputPath(courseID int64, outputPath string) (string, error) {
	if outputPath != "" {
		expanded := expandHome(outputPath)
		path, err := filepath.Abs(expanded)
		if err != nil {
			return "", err
		}
		isDir := false
		if info, err := os.Stat(path); err == nil {
			isDir = info.IsDir()
		}
		if isDir || strings.HasSuffix(outputPath, "/") || strings.HasSuffix(outputPath, "\\") {
			return filepath.Join(path, fmt.Sprintf("%d.gpx", courseID)), nil
		}
		if !strings.HasSuffix(strings.ToLower(path), ".gpx") {
			return "", fmt.Errorf("output_path must be a directory or a .gpx file, got: %s", outputPath)
		}
		return path, nil
	}
	base := resolveDownloadDir()
	if base == "" {
		abs, err := filepath.Abs("./courses")
		if err != nil {
			return "", err
		}
		base = abs
	}
	return filepath.Join(base, fmt.Sprintf("%d.gpx", courseID)), nil
}

func gpxDecimal(value any) string {
	f, ok := numberOf(value)
	if !ok {
		if s, isString := value.(string); isString {
			if parsed, err := strconv.ParseFloat(s, 64); err == nil {
				f = parsed
				ok = true
			}
		}
	}
	if !ok {
		return fmt.Sprint(value)
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e16 {
		return strconv.FormatFloat(f, 'f', 1, 64)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func escapeXML(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func downloadCourseGpx(ctx context.Context, s *Session, in downloadCourseGpxInput) (any, error) {
	targetPath, err := resolveGPXOutputPath(in.CourseID, in.OutputPath)
	if err != nil {
		return fmt.Sprintf("Error downloading course GPX: %v", err), nil
	}

	dataValue, err := s.Client.GetCourse(ctx, in.CourseID)
	if err != nil {
		return nil, fmt.Errorf("Error downloading course GPX: %w", err)
	}
	data, ok := dataValue.(map[string]any)
	if !ok || !truthy(data["geoPoints"]) {
		return fmt.Sprintf("Error: course %d not found or missing geoPoints.", in.CourseID), nil
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Sprintf("Error downloading course GPX: %v", err), nil
	}

	name := stringFieldOf(data, "courseName")
	if name == "" {
		name = fmt.Sprintf("Garmin Course %d", in.CourseID)
	}
	geoPoints := asList(data["geoPoints"])
	coursePoints := asList(data["coursePoints"])

	lines := []string{
		"<?xml version='1.0' encoding='UTF-8'?>",
		"<gpx version='1.1' creator='GarminConnectMCP' xmlns='http://www.topografix.com/GPX/1/1'>",
		"  <metadata>",
		fmt.Sprintf("    <name>%s</name>", escapeXML(name)),
		"  </metadata>",
	}
	for _, item := range coursePoints {
		point := asMap(item)
		lat, lon := point["lat"], point["lon"]
		if lat == nil || lon == nil {
			continue
		}
		pointName := firstTruthyValue(point["name"], point["pointType"], "Waypoint")
		pointType := firstTruthyValue(point["pointType"], "GENERIC")
		lines = append(lines,
			fmt.Sprintf("  <wpt lat='%s' lon='%s'>", gpxDecimal(lat), gpxDecimal(lon)),
			fmt.Sprintf("    <name>%s</name>", escapeXML(fmt.Sprint(pointName))),
			fmt.Sprintf("    <type>%s</type>", escapeXML(fmt.Sprint(pointType))),
			"  </wpt>",
		)
	}
	lines = append(lines, "  <trk>", fmt.Sprintf("    <name>%s</name>", escapeXML(name)), "    <trkseg>")
	for _, item := range geoPoints {
		point := asMap(item)
		lat, lon := point["latitude"], point["longitude"]
		if lat == nil || lon == nil {
			continue
		}
		ele := ""
		if point["elevation"] != nil {
			ele = fmt.Sprintf("<ele>%s</ele>", gpxDecimal(point["elevation"]))
		}
		lines = append(lines, fmt.Sprintf("      <trkpt lat='%s' lon='%s'>%s</trkpt>",
			gpxDecimal(lat), gpxDecimal(lon), ele))
	}
	lines = append(lines, "    </trkseg>", "  </trk>", "</gpx>")

	if err := os.WriteFile(targetPath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return fmt.Sprintf("Error downloading course GPX: %v", err), nil
	}

	return map[string]any{
		"status":             "success",
		"course_id":          in.CourseID,
		"name":               name,
		"gpx_path":           targetPath,
		"waypoints_count":    len(coursePoints),
		"track_points_count": len(geoPoints),
	}, nil
}

// --- course payload helpers ---

func haversine(p1, p2 map[string]any) float64 {
	lat1, _ := numberOf(p1["latitude"])
	lon1, _ := numberOf(p1["longitude"])
	lat2, _ := numberOf(p2["latitude"])
	lon2, _ := numberOf(p2["longitude"])
	rlat1, rlon1 := lat1*math.Pi/180, lon1*math.Pi/180
	rlat2, rlon2 := lat2*math.Pi/180, lon2*math.Pi/180
	dlat, dlon := rlat2-rlat1, rlon2-rlon1
	a := math.Sin(dlat/2)*math.Sin(dlat/2) + math.Cos(rlat1)*math.Cos(rlat2)*math.Sin(dlon/2)*math.Sin(dlon/2)
	return 2 * earthRadiusM * math.Asin(math.Sqrt(a))
}

func initialBearing(p1, p2 map[string]any) float64 {
	lat1, _ := numberOf(p1["latitude"])
	lon1, _ := numberOf(p1["longitude"])
	lat2, _ := numberOf(p2["latitude"])
	lon2, _ := numberOf(p2["longitude"])
	rlat1, rlat2 := lat1*math.Pi/180, lat2*math.Pi/180
	dlon := (lon2 - lon1) * math.Pi / 180
	x := math.Sin(dlon) * math.Cos(rlat2)
	y := math.Cos(rlat1)*math.Sin(rlat2) - math.Sin(rlat1)*math.Cos(rlat2)*math.Cos(dlon)
	bearing := math.Atan2(x, y) * 180 / math.Pi
	return math.Mod(bearing+360, 360)
}

func buildCoursePayload(parsed map[string]any, courseName string, activityTypeID int, description *string) (map[string]any, error) {
	geoPoints := asList(parsed["geoPoints"])
	if len(geoPoints) == 0 {
		return nil, errors.New("GPX parsed skeleton contains no geoPoints")
	}

	totalDistance := 0.0
	for i := 1; i < len(geoPoints); i++ {
		totalDistance += haversine(asMap(geoPoints[i-1]), asMap(geoPoints[i]))
	}

	lats := make([]float64, 0, len(geoPoints))
	lons := make([]float64, 0, len(geoPoints))
	for _, item := range geoPoints {
		point := asMap(item)
		if point["elevation"] == nil {
			point["elevation"] = 0.0
		}
		lat, _ := numberOf(point["latitude"])
		lon, _ := numberOf(point["longitude"])
		lats = append(lats, lat)
		lons = append(lons, lon)
	}
	sort.Float64s(lats)
	sort.Float64s(lons)
	minLat, maxLat := lats[0], lats[len(lats)-1]
	minLon, maxLon := lons[0], lons[len(lons)-1]
	bbox := map[string]any{
		"center": map[string]any{
			"latitude":  (minLat + maxLat) / 2,
			"longitude": (minLon + maxLon) / 2,
		},
		"lowerLeft":           map[string]any{"latitude": minLat, "longitude": minLon},
		"upperRight":          map[string]any{"latitude": maxLat, "longitude": maxLon},
		"lowerLeftLatIsSet":   true,
		"lowerLeftLongIsSet":  true,
		"upperRightLatIsSet":  true,
		"upperRightLongIsSet": true,
	}

	firstPoint := asMap(geoPoints[0])
	startElevation := 0.0
	if elevation, ok := numberOf(firstPoint["elevation"]); ok {
		startElevation = elevation
	}
	startPoint := map[string]any{
		"latitude":  firstPoint["latitude"],
		"longitude": firstPoint["longitude"],
		"elevation": startElevation,
		"distance":  nil,
		"timestamp": nil,
	}
	bearing := initialBearing(firstPoint, asMap(geoPoints[len(geoPoints)-1]))

	var descriptionValue any
	if description != nil {
		descriptionValue = *description
	}
	return map[string]any{
		"courseName":               courseName,
		"description":              descriptionValue,
		"openStreetMap":            false,
		"matchedToSegments":        false,
		"userProfilePk":            nil,
		"userGroupPk":              nil,
		"rulePK":                   2,
		"geoRoutePk":               nil,
		"sourceTypeId":             3,
		"sourcePk":                 nil,
		"distanceMeter":            totalDistance,
		"elevationGainMeter":       0.0,
		"elevationLossMeter":       0.0,
		"startPoint":               startPoint,
		"coursePoints":             []any{},
		"boundingBox":              bbox,
		"hasShareableEvent":        false,
		"hasTurnDetectionDisabled": false,
		"activityTypePk":           activityTypeID,
		"virtualPartnerId":         nil,
		"includeLaps":              false,
		"elapsedSeconds":           nil,
		"startBearing":             bearing,
		"endBearing":               nil,
		"geoPoints":                geoPoints,
	}, nil
}

func uploadCourse(ctx context.Context, s *Session, in uploadCourseInput) (any, error) {
	extension := filepath.Ext(in.GpxPath)
	if strings.ToLower(extension) != ".gpx" {
		shown := extension
		if shown == "" {
			shown = "(no extension)"
		}
		return fmt.Sprintf("Error: only .gpx files are allowed, got: %s", shown), nil
	}
	gpxPath, err := filepath.Abs(expandHome(in.GpxPath))
	if err != nil {
		return fmt.Sprintf("Error uploading course: %v", err), nil
	}
	info, err := os.Stat(gpxPath)
	if err != nil || info.IsDir() {
		return fmt.Sprintf("Error: GPX file not found: %s", gpxPath), nil
	}

	activityType := strings.ToLower(in.ActivityType)
	if activityType == "" {
		activityType = "running"
	}
	activityTypeID, ok := courseActivityTypeIDs[activityType]
	if !ok {
		supported := make([]string, 0, len(courseActivityTypeIDs))
		for key := range courseActivityTypeIDs {
			supported = append(supported, key)
		}
		sort.Strings(supported)
		return fmt.Sprintf("Error: unknown activity_type '%s'. Supported: %s.", in.ActivityType, strings.Join(supported, ", ")), nil
	}

	gpxBytes, err := os.ReadFile(gpxPath)
	if err != nil {
		return fmt.Sprintf("Error uploading course: %v", err), nil
	}

	tracker := newWriteTracker("upload_course", s.logWriter()).
		withPrefix("Error uploading course")
	tracker.Sent()
	parsedValue, err := s.Client.ImportCourse(ctx, filepath.Base(gpxPath), gpxBytes)
	if err != nil {
		return nil, tracker.Fail(err)
	}
	parsed := asMap(parsedValue)

	effectiveName := in.CourseName
	if effectiveName == "" {
		effectiveName = stringFieldOf(parsed, "courseName")
	}
	if effectiveName == "" {
		base := filepath.Base(gpxPath)
		effectiveName = strings.TrimSuffix(base, filepath.Ext(base))
	}
	payload, err := buildCoursePayload(parsed, effectiveName, activityTypeID, in.Description)
	if err != nil {
		return fmt.Sprintf("Error uploading course: %v", err), nil
	}
	savedValue, err := s.Client.SaveCourse(ctx, payload)
	if err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	saved := asMap(savedValue)
	return map[string]any{
		"status":           "success",
		"course_id":        saved["courseId"],
		"name":             saved["courseName"],
		"distance_m":       saved["distanceMeter"],
		"elevation_gain_m": saved["elevationGainMeter"],
		"elevation_loss_m": saved["elevationLossMeter"],
		"activity_type_id": saved["activityTypePk"],
		"url":              fmt.Sprintf("https://connect.%s/modern/course/%s", s.Client.Domain(), numToStr(saved["courseId"])),
	}, nil
}

func coursePreview(ctx context.Context, c *garmin.Client, courseID int64) map[string]any {
	preview := map[string]any{"course_id": courseID}

	var course map[string]any
	if value, err := c.GetCourse(ctx, courseID); err == nil {
		if m, ok := value.(map[string]any); ok && len(m) > 0 {
			course = m
		}
	}
	if course == nil {
		items, err := c.GetCourses(ctx)
		if err != nil {
			return preview
		}
		list, ok := items.([]any)
		if !ok {
			return preview
		}
		want := strconv.FormatInt(courseID, 10)
		for _, item := range list {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if fmt.Sprint(m["courseId"]) == want {
				course = m
				break
			}
		}
		if course == nil {
			return preview
		}
	}

	activityType := asMap(course["activityType"])
	preview["name"] = course["courseName"]
	preview["activity"] = activityType["typeKey"]
	preview["distance_m"] = firstTruthyValue(course["distanceInMeters"], course["distanceMeter"])
	preview["created"] = course["createdDateFormatted"]
	return pruneNil(preview)
}

func deleteCourse(ctx context.Context, s *Session, in deleteCourseInput) (any, error) {
	preview := coursePreview(ctx, s.Client, in.CourseID)
	if !in.Confirm {
		return map[string]any{
			"status": "needs_confirmation",
			"course": preview,
			"message": "This permanently deletes the course from Garmin Connect and cannot be undone. " +
				"Call again with confirm=true to proceed.",
		}, nil
	}

	tracker := newWriteTracker("delete_course", s.logWriter()).
		withPrefix("Error deleting course")
	tracker.Sent()
	if _, err := s.Client.DeleteCourse(ctx, in.CourseID); err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	fmt.Fprintf(s.logWriter(), "delete_course: deleted course %d name=%s at %s\n",
		in.CourseID, pythonRepr(preview["name"]),
		time.Now().UTC().Format("2006-01-02T15:04:05.000000-07:00"))
	return map[string]any{
		"status":    "success",
		"course_id": in.CourseID,
		"course":    preview,
		"message":   fmt.Sprintf("Course %d deleted", in.CourseID),
	}, nil
}
