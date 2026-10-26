package server

import (
	"context"
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

// Port of garmin_mcp/courses.py.

const getCoursesDesc = `List all courses saved on Garmin Connect.

Returns a curated list of courses with id, name, distance, activity type
and creation date.`

const getCourseDetailsDesc = `Get full details of a Garmin Connect course by ID.

Returns course metadata, elevation gain/loss, total distance,
and all custom course waypoints (shops, water, food, campgrounds, hazards).`

const downloadCourseGPXDesc = `Download a Garmin Connect course as a GPX file.

The GPX is rebuilt from the course's track points (lat/lon/elevation)
and course points (as waypoints); it is not the originally uploaded file.`

const uploadCourseDesc = `Upload a GPX file as a Garmin Connect Course.

The course can then be loaded onto the watch (sync or "Send to Device")
and used as a navigation course or to build a PacePro strategy.`

const deleteCourseDesc = `Delete a course from Garmin Connect.

This cannot be undone. Call once with confirm=false (the default) to
preview the course, then call again with confirm=true to delete it.`

type courseIDIn struct {
	Account
	CourseID int64 `json:"course_id" jsonschema:"ID of the course (from get_courses)."`
}

type downloadCourseGPXIn struct {
	Account
	CourseID   int64   `json:"course_id" jsonschema:"ID of the course to download."`
	OutputPath *string `json:"output_path,omitempty" jsonschema:"Optional destination directory (saved as {course_id}.gpx) or file path ending in .gpx; any other file path is rejected. Defaults to the download_activity_file directory (GARMIN_FIT_DOWNLOAD_DIR, then set_fit_download_dir), else ./courses/{course_id}.gpx."`
}

type uploadCourseIn struct {
	Account
	GPXPath      string  `json:"gpx_path" jsonschema:"Absolute path to the .gpx file on disk."`
	CourseName   *string `json:"course_name,omitempty" jsonschema:"Override the course name. Defaults to the name parsed from the GPX file. Max 128 characters; Garmin rejects longer."`
	ActivityType *string `json:"activity_type,omitempty" jsonschema:"One of running, cycling, hiking, walking, trail_running, mountain_biking, road_biking, gravel_cycling. Defaults to running."`
	Description  *string `json:"description,omitempty" jsonschema:"Optional description shown on the course detail page. Max 512 characters; Garmin rejects longer."`
}

type deleteCourseIn struct {
	Account
	CourseID int64 `json:"course_id" jsonschema:"ID of the course to delete (get IDs from get_courses)."`
	Confirm  bool  `json:"confirm,omitempty" jsonschema:"Must be true to actually delete. Default false returns a preview."`
}

func registerCourseTools(r *registry) {
	addTool(r, "get_courses", getCoursesDesc, getCourses)
	addTool(r, "get_course_details", getCourseDetailsDesc, getCourseDetails)
	addTool(r, "download_course_gpx", downloadCourseGPXDesc, downloadCourseGPX)
	addTool(r, "upload_course", uploadCourseDesc, uploadCourse)
	addTool(r, "delete_course", deleteCourseDesc, deleteCourse)
}

func getCourses(ctx context.Context, s *Session, _ noArgsIn) (any, error) {
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
		c := asMap(item)
		curated = append(curated, map[string]any{
			"course_id": c["courseId"], "name": c["courseName"], "distance_m": c["distanceInMeters"],
			"elevation_gain_m": c["elevationGainInMeters"], "elevation_loss_m": c["elevationLossInMeters"],
			"activity": asMap(c["activityType"])["typeKey"], "has_pace_band": c["hasPaceBand"], "created": c["createdDateFormatted"],
		})
	}
	return map[string]any{"count": len(curated), "courses": curated}, nil
}

func courseURL(s *Session, id any) string {
	return "https://connect." + s.Client.Domain() + "/modern/course/" + pyStr(id)
}

func getCourseDetails(ctx context.Context, s *Session, in courseIDIn) (any, error) {
	data, err := s.Client.GetCourse(ctx, in.CourseID)
	if err != nil {
		return nil, fmt.Errorf("Error fetching course details: %w", err)
	}
	d, ok := data.(map[string]any)
	if !ok {
		return data, nil
	}
	points := []map[string]any{}
	for _, cp := range asList(d["coursePoints"]) {
		m := asMap(cp)
		points = append(points, map[string]any{"name": m["name"], "type": m["pointType"], "lat": m["lat"], "lon": m["lon"], "distance_m": m["distance"]})
	}
	return map[string]any{
		"course_id": d["courseId"], "name": d["courseName"],
		"distance_m":       firstTruthy(d["distanceInMeters"], d["distanceMeter"]),
		"elevation_gain_m": firstTruthy(d["elevationGainInMeters"], d["elevationGainMeter"]),
		"elevation_loss_m": firstTruthy(d["elevationLossInMeters"], d["elevationLossMeter"]),
		"activity":         asMap(d["activityType"])["typeKey"],
		"waypoints_count":  len(points), "waypoints": points, "geo_points_count": len(asList(d["geoPoints"])),
		"url": courseURL(s, in.CourseID),
	}, nil
}

// --- GPX download ---

// gpxDecimal is _gpx_decimal: format(Decimal(str(value)), "f"). Python's str() of a float is the shortest
// repr with a ".0" on whole numbers below 1e16; Decimal then expands any exponent without rounding.
// shortcut: JSON integers decode as float64, so a whole number prints as "5.0" (Python would print "5").
func gpxDecimal(v any) (string, error) {
	f, ok := asNumber(v)
	if !ok {
		return "", fmt.Errorf("[<class 'decimal.ConversionSyntax'>]")
	}
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") && math.Abs(f) < 1e16 {
		s += ".0"
	}
	return s, nil
}

var xmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;") // xml.sax.saxutils.escape

// expandUser is os.path.expanduser for the "~" and "~/..." forms.
func expandUser(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + p[1:]
		}
	}
	return p
}

// resolveDownloadDir is activity_analysis._resolve_download_dir(None): GARMIN_FIT_DOWNLOAD_DIR, then the
// account's saved download directory; "" when nothing is configured.
func resolveDownloadDir(s *Session) string {
	dir := ""
	if s.Getenv != nil {
		dir = s.Getenv("GARMIN_FIT_DOWNLOAD_DIR")
	}
	if dir == "" {
		if st, err := s.Settings(); err == nil {
			dir = st.FitDownloadDir
		}
	}
	if dir == "" {
		return ""
	}
	abs, err := filepath.Abs(expandUser(dir))
	if err != nil {
		return dir
	}
	return abs
}

// resolveGPXOutputPath is _resolve_gpx_output_path; configuredDir is resolveDownloadDir's result ("" if none).
// A directory (existing, or written with a trailing slash) gets {course_id}.gpx; a file must end in .gpx.
func resolveGPXOutputPath(courseID int64, outputPath, configuredDir string) (string, error) {
	name := strconv.FormatInt(courseID, 10) + ".gpx"
	if outputPath != "" {
		p, err := filepath.Abs(expandUser(outputPath))
		if err != nil {
			return "", err
		}
		if fi, err := os.Stat(p); (err == nil && fi.IsDir()) || strings.HasSuffix(outputPath, "/") || strings.HasSuffix(outputPath, `\`) {
			return filepath.Join(p, name), nil
		}
		if !strings.HasSuffix(strings.ToLower(p), ".gpx") {
			return "", fmt.Errorf("output_path must be a directory or a .gpx file, got: %s", outputPath)
		}
		return p, nil
	}
	base := configuredDir
	if base == "" {
		var err error
		if base, err = filepath.Abs("./courses"); err != nil {
			return "", err
		}
	}
	return filepath.Join(base, name), nil
}

func downloadCourseGPX(ctx context.Context, s *Session, in downloadCourseGPXIn) (any, error) {
	fail := func(err error) (any, error) { return nil, fmt.Errorf("Error downloading course GPX: %w", err) }
	out := ""
	if in.OutputPath != nil {
		out = *in.OutputPath
	}
	target, err := resolveGPXOutputPath(in.CourseID, out, resolveDownloadDir(s))
	if err != nil {
		return fail(err)
	}
	raw, err := s.Client.GetCourse(ctx, in.CourseID)
	if err != nil {
		return fail(err)
	}
	data := asMap(raw)
	geoPoints := asList(data["geoPoints"])
	if len(geoPoints) == 0 {
		return nil, fmt.Errorf("Error: course %d not found or missing geoPoints.", in.CourseID)
	}
	name := pyStr(firstTruthy(data["courseName"], fmt.Sprintf("Garmin Course %d", in.CourseID)))
	coursePoints := asList(data["coursePoints"])

	lines := []string{
		"<?xml version='1.0' encoding='UTF-8'?>",
		"<gpx version='1.1' creator='GarminConnectMCP' xmlns='http://www.topografix.com/GPX/1/1'>",
		"  <metadata>", "    <name>" + xmlEscaper.Replace(name) + "</name>", "  </metadata>",
	}
	for _, c := range coursePoints {
		cp := asMap(c)
		if cp["lat"] == nil || cp["lon"] == nil {
			continue
		}
		lat, err := gpxDecimal(cp["lat"])
		if err != nil {
			return fail(err)
		}
		lon, err := gpxDecimal(cp["lon"])
		if err != nil {
			return fail(err)
		}
		cpName := pyStr(firstTruthy(firstTruthy(cp["name"], cp["pointType"]), "Waypoint"))
		cpType := pyStr(firstTruthy(cp["pointType"], "GENERIC"))
		lines = append(lines, "  <wpt lat='"+lat+"' lon='"+lon+"'>", "    <name>"+xmlEscaper.Replace(cpName)+"</name>",
			"    <type>"+xmlEscaper.Replace(cpType)+"</type>", "  </wpt>")
	}
	lines = append(lines, "  <trk>", "    <name>"+xmlEscaper.Replace(name)+"</name>", "    <trkseg>")
	for _, g := range geoPoints {
		p := asMap(g)
		if p["latitude"] == nil || p["longitude"] == nil {
			continue
		}
		lat, err := gpxDecimal(p["latitude"])
		if err != nil {
			return fail(err)
		}
		lon, err := gpxDecimal(p["longitude"])
		if err != nil {
			return fail(err)
		}
		ele := ""
		if p["elevation"] != nil {
			e, err := gpxDecimal(p["elevation"])
			if err != nil {
				return fail(err)
			}
			ele = "<ele>" + e + "</ele>"
		}
		lines = append(lines, "      <trkpt lat='"+lat+"' lon='"+lon+"'>"+ele+"</trkpt>")
	}
	lines = append(lines, "    </trkseg>", "  </trk>", "</gpx>")

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fail(err)
	}
	if err := os.WriteFile(target, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return fail(err)
	}
	return map[string]any{"status": "success", "course_id": in.CourseID, "name": name, "gpx_path": target,
		"waypoints_count": len(coursePoints), "track_points_count": len(geoPoints)}, nil
}

// --- upload ---

// activityTypeIDs are Garmin Connect activity typeIds (see get_activity_types).
var activityTypeIDs = map[string]int{
	"running": 1, "cycling": 2, "hiking": 3, "walking": 9, "trail_running": 6, "mountain_biking": 5, "road_biking": 10, "gravel_cycling": 143,
}

const earthRadiusM = 6371000.0

func degPoint(p map[string]any, key string) float64 {
	f, _ := asNumber(p[key])
	return f * math.Pi / 180
}

// haversine is _haversine. Coordinates are checked by buildCoursePayload before it is called.
func haversine(p1, p2 map[string]any) float64 {
	lat1, lon1 := degPoint(p1, "latitude"), degPoint(p1, "longitude")
	lat2, lon2 := degPoint(p2, "latitude"), degPoint(p2, "longitude")
	dlat, dlon := lat2-lat1, lon2-lon1
	a := math.Pow(math.Sin(dlat/2), 2) + math.Cos(lat1)*math.Cos(lat2)*math.Pow(math.Sin(dlon/2), 2)
	return 2 * earthRadiusM * math.Asin(math.Sqrt(a))
}

// initialBearing is _initial_bearing.
func initialBearing(p1, p2 map[string]any) float64 {
	lat1, lat2 := degPoint(p1, "latitude"), degPoint(p2, "latitude")
	l1, _ := asNumber(p1["longitude"])
	l2, _ := asNumber(p2["longitude"])
	dlon := (l2 - l1) * math.Pi / 180
	x := math.Sin(dlon) * math.Cos(lat2)
	y := math.Cos(lat1)*math.Sin(lat2) - math.Sin(lat1)*math.Cos(lat2)*math.Cos(dlon)
	b := math.Atan2(x, y) * 180 / math.Pi
	return math.Mod(math.Mod(b+360, 360)+360, 360) // Python's % is always non-negative here
}

// buildCoursePayload is _build_course_payload. It fills in missing elevations in parsed's geoPoints, as Python does.
func buildCoursePayload(parsed map[string]any, courseName string, activityTypeID int, description *string) (map[string]any, error) {
	geo := asList(parsed["geoPoints"])
	if len(geo) == 0 {
		return nil, fmt.Errorf("GPX parsed skeleton contains no geoPoints")
	}
	pts := make([]map[string]any, len(geo))
	lats, lons := make([]float64, len(geo)), make([]float64, len(geo))
	for i, g := range geo {
		p := asMap(g)
		la, lo := p["latitude"], p["longitude"]
		lat, ok1 := asNumber(la)
		lon, ok2 := asNumber(lo)
		if !ok1 || !ok2 {
			key := "latitude"
			if ok1 {
				key = "longitude"
			}
			return nil, fmt.Errorf("'%s'", key) // Python KeyError / TypeError text, close enough for a malformed skeleton
		}
		pts[i], lats[i], lons[i] = p, lat, lon
	}
	total := 0.0
	for i := 1; i < len(pts); i++ {
		total += haversine(pts[i-1], pts[i])
	}
	for _, p := range pts {
		if p["elevation"] == nil {
			p["elevation"] = 0.0
		}
	}
	minLat, maxLat := slicesMinMax(lats)
	minLon, maxLon := slicesMinMax(lons)
	bbox := map[string]any{
		"center":            map[string]any{"latitude": (minLat + maxLat) / 2, "longitude": (minLon + maxLon) / 2},
		"lowerLeft":         map[string]any{"latitude": minLat, "longitude": minLon},
		"upperRight":        map[string]any{"latitude": maxLat, "longitude": maxLon},
		"lowerLeftLatIsSet": true, "lowerLeftLongIsSet": true, "upperRightLatIsSet": true, "upperRightLongIsSet": true,
	}
	start := map[string]any{"latitude": pts[0]["latitude"], "longitude": pts[0]["longitude"],
		"elevation": firstTruthy(pts[0]["elevation"], 0.0), "distance": nil, "timestamp": nil}
	var desc any
	if description != nil {
		desc = *description
	}
	return map[string]any{
		"courseName": courseName, "description": desc, "openStreetMap": false, "matchedToSegments": false,
		"userProfilePk": nil, "userGroupPk": nil, "rulePK": 2 /* private */, "geoRoutePk": nil,
		"sourceTypeId": 3 /* GPX */, "sourcePk": nil, "distanceMeter": total, "elevationGainMeter": 0.0, "elevationLossMeter": 0.0,
		"startPoint": start, "coursePoints": []any{}, "boundingBox": bbox, "hasShareableEvent": false,
		"hasTurnDetectionDisabled": false, "activityTypePk": activityTypeID, "virtualPartnerId": nil, "includeLaps": false,
		"elapsedSeconds": nil, "startBearing": initialBearing(pts[0], pts[len(pts)-1]), "endBearing": nil, "geoPoints": geo,
	}, nil
}

// pySuffix is pathlib.PurePath(p).suffix.
func pySuffix(p string) string {
	name := filepath.Base(p)
	if i := strings.LastIndex(name, "."); i > 0 && i < len(name)-1 {
		return name[i:]
	}
	return ""
}

func uploadCourse(ctx context.Context, s *Session, in uploadCourseIn) (any, error) {
	fail := func(format string, a ...any) (any, error) { return nil, fmt.Errorf(format, a...) }
	failWrap := func(err error) (any, error) { return nil, fmt.Errorf("Error uploading course: %w", err) }
	if suffix := pySuffix(in.GPXPath); strings.ToLower(suffix) != ".gpx" {
		if suffix == "" {
			suffix = "(no extension)"
		}
		return fail("Error: only .gpx files are allowed, got: %s", suffix)
	}
	path, err := filepath.Abs(in.GPXPath)
	if err != nil {
		return failWrap(err)
	}
	if real, err := filepath.EvalSymlinks(path); err == nil { // Path.resolve(): follow symlinks when the path exists
		path = real
	}
	if fi, err := os.Stat(path); err != nil || !fi.Mode().IsRegular() {
		return fail("Error: GPX file not found: %s", path)
	}
	activityType := "running"
	if in.ActivityType != nil {
		activityType = *in.ActivityType
	}
	typeID, ok := activityTypeIDs[strings.ToLower(activityType)]
	if !ok {
		names := make([]string, 0, len(activityTypeIDs))
		for k := range activityTypeIDs {
			names = append(names, k)
		}
		sort.Strings(names)
		return fail("Error: unknown activity_type '%s'. Supported: %s.", activityType, strings.Join(names, ", "))
	}
	gpx, err := os.ReadFile(path)
	if err != nil {
		return failWrap(err)
	}
	// Step 1: parse the GPX server-side
	base := filepath.Base(path)
	raw, err := s.Client.ImportCourseGPX(ctx, base, gpx)
	if err != nil {
		return failWrap(err)
	}
	parsed, ok := raw.(map[string]any)
	if !ok {
		return failWrap(fmt.Errorf("'%s' object has no attribute 'get'", pyTypeName(raw)))
	}
	name := ""
	if in.CourseName != nil {
		name = *in.CourseName
	}
	if name == "" {
		name = pyStr(firstTruthy(parsed["courseName"], strings.TrimSuffix(base, filepath.Ext(base))))
	}
	// Step 2: build the create payload and save
	payload, err := buildCoursePayload(parsed, name, typeID, in.Description)
	if err != nil {
		return failWrap(err)
	}
	savedRaw, err := s.Client.SaveCourse(ctx, payload)
	if err != nil {
		return failWrap(err)
	}
	saved, ok := savedRaw.(map[string]any)
	if !ok {
		return failWrap(fmt.Errorf("'%s' object has no attribute 'get'", pyTypeName(savedRaw)))
	}
	return map[string]any{
		"status": "success", "course_id": saved["courseId"], "name": saved["courseName"], "distance_m": saved["distanceMeter"],
		"elevation_gain_m": saved["elevationGainMeter"], "elevation_loss_m": saved["elevationLossMeter"],
		"activity_type_id": saved["activityTypePk"], "url": courseURL(s, saved["courseId"]),
	}, nil
}

// --- delete ---

// courseSource is the part of the client coursePreview needs.
type courseSource interface {
	GetCourse(ctx context.Context, id int64) (any, error)
	GetCourses(ctx context.Context) (any, error)
}

var _ courseSource = (*garmin.Client)(nil)

// coursePreview is _course_preview: a compact description of a course before deleting it.
func coursePreview(ctx context.Context, c courseSource, id int64) map[string]any {
	preview := map[string]any{"course_id": id}
	course, _ := c.GetCourse(ctx, id) // any failure just means "no details"
	cm := asMap(course)
	if len(cm) == 0 {
		// Fall back to the course list if the details call fails.
		items, err := c.GetCourses(ctx)
		list, isList := items.([]any)
		if err != nil || !isList {
			return preview
		}
		cm = nil
		for _, item := range list {
			if m, ok := item.(map[string]any); ok && pyStr(m["courseId"]) == strconv.FormatInt(id, 10) {
				cm = m
				break
			}
		}
		if cm == nil {
			return preview
		}
	}
	preview["name"] = cm["courseName"]
	preview["activity"] = asMap(cm["activityType"])["typeKey"]
	preview["distance_m"] = firstTruthy(cm["distanceInMeters"], cm["distanceMeter"])
	preview["created"] = cm["createdDateFormatted"]
	return dropNil(preview)
}

func deleteCourse(ctx context.Context, s *Session, in deleteCourseIn) (any, error) {
	preview := coursePreview(ctx, s.Client, in.CourseID)
	if !in.Confirm {
		return map[string]any{"status": "needs_confirmation", "course": preview,
			"message": "This permanently deletes the course from Garmin Connect and cannot be undone. Call again with confirm=true to proceed."}, nil
	}
	if err := s.Client.DeleteCourse(ctx, in.CourseID); err != nil {
		return nil, fmt.Errorf("Error deleting course: %w", err)
	}
	if s.Log != nil {
		fmt.Fprintf(s.Log, "delete_course: deleted course %d name=%s at %s\n", in.CourseID, pyReprValue(preview["name"]), time.Now().UTC().Format("2006-01-02T15:04:05.000000+00:00"))
	}
	return map[string]any{"status": "success", "course_id": in.CourseID, "course": preview, "message": fmt.Sprintf("Course %d deleted", in.CourseID)}, nil
}
