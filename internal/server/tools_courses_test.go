package server

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	coursesListPath   = "/course-service/course"
	coursesDetailPath = "/course-service/course/777"
	coursesImportPath = "/course-service/course/import"
)

type gpxPoint struct {
	Lat string  `xml:"lat,attr"`
	Lon string  `xml:"lon,attr"`
	Ele *string `xml:"ele"`
}

type gpxWaypoint struct {
	Lat  string `xml:"lat,attr"`
	Lon  string `xml:"lon,attr"`
	Name string `xml:"name"`
	Type string `xml:"type"`
}

type gpxDoc struct {
	XMLName  xml.Name `xml:"gpx"`
	Metadata struct {
		Name string `xml:"name"`
	} `xml:"metadata"`
	Waypoints []gpxWaypoint `xml:"wpt"`
	Track     struct {
		Name string `xml:"name"`
		Segs []struct {
			Points []gpxPoint `xml:"trkpt"`
		} `xml:"trkseg"`
	} `xml:"trk"`
}

func parseGPXFile(t *testing.T, path string) gpxDoc {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read gpx: %v", err)
	}
	var doc gpxDoc
	if err := xml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse gpx: %v\n%s", err, raw)
	}
	return doc
}

func TestGetCoursesCuratesFields(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+coursesListPath, []any{map[string]any{
		"courseId":              111.0,
		"courseName":            "River Loop",
		"distanceInMeters":      10250.5,
		"elevationGainInMeters": 120.0,
		"elevationLossInMeters": 118.0,
		"activityType":          map[string]any{"typeKey": "running"},
		"hasPaceBand":           false,
		"createdDateFormatted":  "2024-03-01",
	}})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_courses", nil)
	if isErr {
		t.Fatalf("error: %s", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["count"] != 1.0 {
		t.Fatalf("data = %v", data)
	}
	course := asMap(asList(data["courses"])[0])
	if course["course_id"] != 111.0 || course["name"] != "River Loop" || course["distance_m"] != 10250.5 || course["activity"] != "running" {
		t.Fatalf("course = %v", course)
	}
	if calls := f.callsMatching("GET", coursesListPath); len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
}

func TestGetCoursesEmpty(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+coursesListPath, []any{})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_courses", nil)
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["count"] != 0.0 || len(asList(data["courses"])) != 0 {
		t.Fatalf("data = %v", data)
	}
}

func TestGetCoursesErrorIsCaught(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+coursesListPath, status(500, map[string]any{"message": "boom"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_courses", nil)
	if !strings.Contains(text, "Error listing courses") {
		t.Fatalf("text = %q", text)
	}
}

func TestGetCourseDetailsSuccess(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+coursesDetailPath, map[string]any{
		"courseId":              777.0,
		"courseName":            "Bikepack Day 1",
		"distanceInMeters":      45000.0,
		"elevationGainInMeters": 350.0,
		"elevationLossInMeters": 300.0,
		"activityType":          map[string]any{"typeKey": "gravel_cycling"},
		"coursePoints": []any{map[string]any{
			"name": "Water Fountain", "pointType": "WATER", "lat": 52.2, "lon": 21.0, "distance": 12000.0,
		}},
		"geoPoints": []any{
			map[string]any{"latitude": 52.1, "longitude": 20.9},
			map[string]any{"latitude": 52.2, "longitude": 21.0},
		},
	})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_course_details", map[string]any{"course_id": 777})
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["course_id"] != 777.0 || data["name"] != "Bikepack Day 1" || data["distance_m"] != 45000.0 {
		t.Fatalf("data = %v", data)
	}
	if data["waypoints_count"] != 1.0 || data["geo_points_count"] != 2.0 {
		t.Fatalf("data = %v", data)
	}
	if asMap(asList(data["waypoints"])[0])["type"] != "WATER" {
		t.Fatalf("waypoints = %v", data["waypoints"])
	}
	if !strings.Contains(data["url"].(string), "/modern/course/777") {
		t.Fatalf("url = %v", data["url"])
	}
}

func TestGetCourseDetailsTreatsNullCoursePointsAsEmpty(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /course-service/course/778", map[string]any{
		"courseId":     778.0,
		"courseName":   "Track Only",
		"coursePoints": nil,
		"geoPoints": []any{
			map[string]any{"latitude": 52.1, "longitude": 20.9},
			map[string]any{"latitude": 52.2, "longitude": 21.0},
		},
	})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_course_details", map[string]any{"course_id": 778})
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["waypoints_count"] != 0.0 || len(asList(data["waypoints"])) != 0 {
		t.Fatalf("data = %v", data)
	}
	if data["geo_points_count"] != 2.0 {
		t.Fatalf("data = %v", data)
	}
}

func TestGetCourseDetailsTreatsNullGeoPointsAsEmpty(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /course-service/course/779", map[string]any{
		"courseId":     779.0,
		"courseName":   "Metadata Only",
		"coursePoints": nil,
		"geoPoints":    nil,
	})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_course_details", map[string]any{"course_id": 779})
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["waypoints_count"] != 0.0 || data["geo_points_count"] != 0.0 {
		t.Fatalf("data = %v", data)
	}
}

func TestGetCourseDetailsErrorIsCaught(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /course-service/course/999", status(404, map[string]any{"message": "Not found"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_course_details", map[string]any{"course_id": 999})
	if !strings.Contains(text, "Error fetching course details") {
		t.Fatalf("text = %q", text)
	}
}

func TestDownloadCourseGpxExpandsSmallNumbersToDecimal(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /course-service/course/888", map[string]any{
		"courseName": "Near the equator and prime meridian",
		"geoPoints":  []any{map[string]any{"latitude": 0.00005, "longitude": -0.00005, "elevation": 0.000001}},
		"coursePoints": []any{map[string]any{
			"lat": -0.00000001, "lon": 0.00000001,
		}},
	})
	cs := newSession(t, f, nil)
	path := filepath.Join(t.TempDir(), "small_numbers.gpx")

	text, _ := callTool(t, cs, "download_course_gpx", map[string]any{"course_id": 888, "output_path": path})
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["status"] != "success" {
		t.Fatalf("data = %v", data)
	}
	doc := parseGPXFile(t, path)
	wpt := doc.Waypoints[0]
	if wpt.Lat != "-0.00000001" || wpt.Lon != "0.00000001" {
		t.Fatalf("wpt = %+v", wpt)
	}
	pt := doc.Track.Segs[0].Points[0]
	if pt.Lat != "0.00005" || pt.Lon != "-0.00005" {
		t.Fatalf("pt = %+v", pt)
	}
	if pt.Ele == nil || *pt.Ele != "0.000001" {
		t.Fatalf("ele = %v", pt.Ele)
	}
}

func TestDownloadCourseGpxSuccess(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /course-service/course/888", map[string]any{
		"courseId":   888.0,
		"courseName": "Mountain Pass",
		"geoPoints": []any{
			map[string]any{"latitude": 46.1, "longitude": 8.1, "elevation": 1200.0},
			map[string]any{"latitude": 46.2, "longitude": 8.2, "elevation": 1400.0},
		},
		"coursePoints": []any{map[string]any{
			"name": "Summit Rest", "pointType": "SUMMIT", "lat": 46.2, "lon": 8.2,
		}},
	})
	cs := newSession(t, f, nil)
	out := filepath.Join(t.TempDir(), "mountain_pass.gpx")

	text, _ := callTool(t, cs, "download_course_gpx", map[string]any{"course_id": 888, "output_path": out})
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["status"] != "success" || data["course_id"] != 888.0 || data["waypoints_count"] != 1.0 || data["track_points_count"] != 2.0 {
		t.Fatalf("data = %v", data)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(raw)
	for _, want := range []string{
		"<gpx version='1.1'",
		"<name>Mountain Pass</name>",
		"<wpt lat='46.2' lon='8.2'>",
		"<name>Summit Rest</name>",
		"<trkpt lat='46.1' lon='8.1'><ele>1200.0</ele></trkpt>",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("content missing %q:\n%s", want, content)
		}
	}
}

func TestDownloadCourseGpxEscapesXmlSpecialCharacters(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /course-service/course/891", map[string]any{
		"courseId":   891.0,
		"courseName": "Rock & Roll <Loop>",
		"geoPoints": []any{
			map[string]any{"latitude": 46.1, "longitude": 8.1, "elevation": 1200.0},
			map[string]any{"latitude": 46.2, "longitude": 8.2, "elevation": 1400.0},
		},
		"coursePoints": []any{map[string]any{
			"name": "Café & Water", "pointType": "WATER", "lat": 46.2, "lon": 8.2,
		}},
	})
	cs := newSession(t, f, nil)
	out := filepath.Join(t.TempDir(), "rock_and_roll.gpx")

	text, _ := callTool(t, cs, "download_course_gpx", map[string]any{"course_id": 891, "output_path": out})
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["status"] != "success" || data["name"] != "Rock & Roll <Loop>" {
		t.Fatalf("data = %v", data)
	}
	doc := parseGPXFile(t, out)
	if doc.Metadata.Name != "Rock & Roll <Loop>" || doc.Track.Name != "Rock & Roll <Loop>" {
		t.Fatalf("names = %q, %q", doc.Metadata.Name, doc.Track.Name)
	}
	if len(doc.Waypoints) != 1 || doc.Waypoints[0].Name != "Café & Water" || doc.Waypoints[0].Type != "WATER" {
		t.Fatalf("waypoints = %+v", doc.Waypoints)
	}
	if len(doc.Track.Segs[0].Points) != 2 {
		t.Fatalf("points = %d", len(doc.Track.Segs[0].Points))
	}
}

func TestDownloadCourseGpxNullCourseNameUsesDefault(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /course-service/course/892", map[string]any{
		"courseId":     892.0,
		"courseName":   nil,
		"geoPoints":    []any{map[string]any{"latitude": 46.1, "longitude": 8.1, "elevation": nil}},
		"coursePoints": []any{map[string]any{"name": nil, "pointType": nil, "lat": 46.1, "lon": 8.1}},
	})
	cs := newSession(t, f, nil)
	out := filepath.Join(t.TempDir(), "unnamed.gpx")

	text, _ := callTool(t, cs, "download_course_gpx", map[string]any{"course_id": 892, "output_path": out})
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["name"] != "Garmin Course 892" {
		t.Fatalf("data = %v", data)
	}
	raw, _ := os.ReadFile(out)
	if strings.Contains(string(raw), "None") {
		t.Fatalf("content contains None:\n%s", raw)
	}
	doc := parseGPXFile(t, out)
	if doc.Metadata.Name != "Garmin Course 892" {
		t.Fatalf("metadata name = %q", doc.Metadata.Name)
	}
	if doc.Waypoints[0].Name != "Waypoint" || doc.Waypoints[0].Type != "GENERIC" {
		t.Fatalf("waypoint = %+v", doc.Waypoints[0])
	}
}

func TestDownloadCourseGpxPreservesTrackWhenCoursePointsAreNull(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /course-service/course/889", map[string]any{
		"courseId":   889.0,
		"courseName": "Track Only",
		"geoPoints": []any{
			map[string]any{"latitude": 46.1, "longitude": 8.1, "elevation": 1200.0},
			map[string]any{"latitude": 46.2, "longitude": 8.2, "elevation": 1400.0},
		},
		"coursePoints": nil,
	})
	cs := newSession(t, f, nil)
	out := filepath.Join(t.TempDir(), "track_only.gpx")

	text, _ := callTool(t, cs, "download_course_gpx", map[string]any{"course_id": 889, "output_path": out})
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["status"] != "success" || data["waypoints_count"] != 0.0 || data["track_points_count"] != 2.0 {
		t.Fatalf("data = %v", data)
	}
	raw, _ := os.ReadFile(out)
	if !strings.Contains(string(raw), "<trkpt lat='46.1' lon='8.1'><ele>1200.0</ele></trkpt>") {
		t.Fatalf("content =\n%s", raw)
	}
}

func TestDownloadCourseGpxReportsNullGeoPointsWithoutWriting(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /course-service/course/890", map[string]any{
		"courseId": 890.0, "courseName": "Metadata Only", "geoPoints": nil, "coursePoints": nil,
	})
	cs := newSession(t, f, nil)
	out := filepath.Join(t.TempDir(), "metadata_only.gpx")

	text, _ := callTool(t, cs, "download_course_gpx", map[string]any{"course_id": 890, "output_path": out})
	if !strings.Contains(text, "Error: course 890 not found or missing geoPoints") {
		t.Fatalf("text = %q", text)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("output file was written")
	}
}

func TestDownloadCourseGpxMissingCourse(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /course-service/course/404", map[string]any{})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "download_course_gpx", map[string]any{"course_id": 404})
	if !strings.Contains(text, "Error: course 404 not found") {
		t.Fatalf("text = %q", text)
	}
}

var trackOnlyCourse = map[string]any{
	"courseId":     893.0,
	"courseName":   "Track Only",
	"geoPoints":    []any{map[string]any{"latitude": 46.1, "longitude": 8.1, "elevation": 1200.0}},
	"coursePoints": nil,
}

func copyTrackOnlyCourse() map[string]any {
	out := map[string]any{}
	for k, v := range trackOnlyCourse {
		out[k] = v
	}
	return out
}

func TestDownloadCourseGpxRejectsNonGpxOutputFile(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /course-service/course/893", copyTrackOnlyCourse())
	cs := newSession(t, f, nil)
	target := filepath.Join(t.TempDir(), "x", ".zshrc")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("export PATH=/usr/bin\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	text, _ := callTool(t, cs, "download_course_gpx", map[string]any{"course_id": 893, "output_path": target})
	if !strings.Contains(text, "output_path must be a directory or a .gpx file") {
		t.Fatalf("text = %q", text)
	}
	raw, _ := os.ReadFile(target)
	if string(raw) != "export PATH=/usr/bin\n" {
		t.Fatalf("target changed: %q", raw)
	}
	if calls := f.callsMatching("GET", "/course-service/course/893"); len(calls) != 0 {
		t.Fatalf("course fetched %d times", len(calls))
	}
}

func TestDownloadCourseGpxAcceptsGpxOutputFile(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /course-service/course/893", copyTrackOnlyCourse())
	cs := newSession(t, f, nil)
	target := filepath.Join(t.TempDir(), "Route.GPX")

	text, _ := callTool(t, cs, "download_course_gpx", map[string]any{"course_id": 893, "output_path": target})
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["gpx_path"] != target {
		t.Fatalf("gpx_path = %v", data["gpx_path"])
	}
	doc := parseGPXFile(t, target)
	if doc.XMLName.Space != "http://www.topografix.com/GPX/1/1" {
		t.Fatalf("namespace = %q", doc.XMLName.Space)
	}
}

func TestDownloadCourseGpxDefaultsToConfiguredDownloadDir(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /course-service/course/893", copyTrackOnlyCourse())
	cs := newSession(t, f, nil)

	root := t.TempDir()
	downloadDir := filepath.Join(root, "downloads")
	configPath := filepath.Join(root, "fit_config.json")
	raw, _ := json.Marshal(map[string]any{"download_dir": downloadDir})
	if err := os.WriteFile(configPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GARMIN_FIT_CONFIG", configPath)
	t.Setenv("GARMIN_FIT_DOWNLOAD_DIR", "")
	cwd := filepath.Join(root, "cwd")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(previous) }()

	text, _ := callTool(t, cs, "download_course_gpx", map[string]any{"course_id": 893})
	var data map[string]any
	decodeJSON(t, text, &data)
	want := filepath.Join(downloadDir, "893.gpx")
	if data["gpx_path"] != want {
		t.Fatalf("gpx_path = %v, want %v", data["gpx_path"], want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("download missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "courses")); err == nil {
		t.Fatal("./courses was created")
	}
}

func TestUploadCourseRejectsNonGpx(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "upload_course", map[string]any{"gpx_path": "/tmp/route.tcx"})
	if !strings.Contains(text, "only .gpx files are allowed") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("POST", coursesImportPath); len(calls) != 0 {
		t.Fatalf("import calls = %d", len(calls))
	}
}

func TestUploadCourseMissingFile(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "upload_course", map[string]any{"gpx_path": "/tmp/nonexistent-route-12345.gpx"})
	if !strings.Contains(text, "GPX file not found") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("POST", coursesImportPath); len(calls) != 0 {
		t.Fatalf("import calls = %d", len(calls))
	}
}

func TestUploadCourseRejectsUnknownActivityType(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	gpxFile := filepath.Join(t.TempDir(), "valid.gpx")
	if err := os.WriteFile(gpxFile, []byte("<gpx></gpx>"), 0o644); err != nil {
		t.Fatal(err)
	}

	text, _ := callTool(t, cs, "upload_course", map[string]any{
		"gpx_path": gpxFile, "activity_type": "paragliding",
	})
	if !strings.Contains(text, "unknown activity_type 'paragliding'") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("POST", coursesImportPath); len(calls) != 0 {
		t.Fatalf("import calls = %d", len(calls))
	}
}

func TestUploadCourseSuccess(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+coursesImportPath, map[string]any{
		"courseName": "River Loop",
		"geoPoints": []any{
			map[string]any{"latitude": 40.0, "longitude": -105.0, "elevation": 1600.0},
			map[string]any{"latitude": 40.01, "longitude": -105.01, "elevation": 1610.0},
		},
	})
	f.on("POST "+coursesListPath, map[string]any{
		"courseId": 999.0, "courseName": "River Loop", "distanceMeter": 1250.0,
		"elevationGainMeter": 10.0, "elevationLossMeter": 0.0, "activityTypePk": 1.0,
	})
	cs := newSession(t, f, nil)
	gpxFile := filepath.Join(t.TempDir(), "test.gpx")
	if err := os.WriteFile(gpxFile, []byte("<gpx><trk><trkseg></trkseg></trk></gpx>"), 0o644); err != nil {
		t.Fatal(err)
	}

	text, _ := callTool(t, cs, "upload_course", map[string]any{
		"gpx_path": gpxFile, "course_name": "River Loop", "activity_type": "running",
	})
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["status"] != "success" || data["course_id"] != 999.0 || data["name"] != "River Loop" {
		t.Fatalf("data = %v", data)
	}
	if data["activity_type_id"] != 1.0 {
		t.Fatalf("data = %v", data)
	}
	if !strings.Contains(data["url"].(string), "course/999") {
		t.Fatalf("url = %v", data["url"])
	}
	imports := f.callsMatching("POST", coursesImportPath)
	saves := f.callsMatching("POST", coursesListPath)
	if len(imports) != 1 || len(saves) != 1 {
		t.Fatalf("imports = %d, saves = %d", len(imports), len(saves))
	}
	if !strings.Contains(string(imports[0].Body), `filename="test.gpx"`) {
		t.Fatalf("import body = %q", imports[0].Body)
	}
}

func TestUploadCoursePostsGarminActivityTypeID(t *testing.T) {
	cases := []struct {
		activityType string
		typeID       float64
	}{
		{"running", 1}, {"cycling", 2}, {"hiking", 3}, {"walking", 9},
		{"mountain_biking", 5}, {"trail_running", 6}, {"road_biking", 10}, {"gravel_cycling", 143},
	}
	for _, tc := range cases {
		t.Run(tc.activityType, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on("POST "+coursesImportPath, map[string]any{
				"courseName": "Route",
				"geoPoints": []any{
					map[string]any{"latitude": 40.0, "longitude": -105.0, "elevation": 1600.0},
					map[string]any{"latitude": 40.01, "longitude": -105.01, "elevation": 1610.0},
				},
			})
			f.on("POST "+coursesListPath, map[string]any{
				"courseId": 1000.0, "courseName": "Route", "activityTypePk": tc.typeID,
			})
			cs := newSession(t, f, nil)
			gpxFile := filepath.Join(t.TempDir(), "route.gpx")
			if err := os.WriteFile(gpxFile, []byte("<gpx><trk><trkseg></trkseg></trk></gpx>"), 0o644); err != nil {
				t.Fatal(err)
			}

			text, _ := callTool(t, cs, "upload_course", map[string]any{
				"gpx_path": gpxFile, "activity_type": tc.activityType,
			})
			var data map[string]any
			decodeJSON(t, text, &data)
			if data["status"] != "success" {
				t.Fatalf("data = %v", data)
			}
			saves := f.callsMatching("POST", coursesListPath)
			var payload map[string]any
			decodeJSON(t, string(saves[0].Body), &payload)
			if payload["activityTypePk"] != tc.typeID {
				t.Fatalf("payload = %v", payload)
			}
		})
	}
}

func TestDeleteCourseRequiresConfirmation(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /course-service/course/555", map[string]any{
		"courseId": 555.0, "courseName": "River Loop", "distanceMeter": 10250.5,
		"activityType": map[string]any{"typeKey": "running"},
	})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "delete_course", map[string]any{"course_id": 555})
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["status"] != "needs_confirmation" {
		t.Fatalf("data = %v", data)
	}
	course := asMap(data["course"])
	if course["name"] != "River Loop" || course["distance_m"] != 10250.5 {
		t.Fatalf("course = %v", course)
	}
	if calls := f.callsMatching("GET", "/course-service/course/555"); len(calls) != 1 {
		t.Fatalf("detail calls = %d", len(calls))
	}
	for _, call := range f.recorded() {
		if call.Method == "DELETE" {
			t.Fatalf("unexpected delete %s", call.Path)
		}
	}
}

func TestDeleteCourseSuccess(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /course-service/course/555", map[string]any{"courseId": 555.0, "courseName": "River Loop"})
	f.on("DELETE /course-service/course/555", nil)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "delete_course", map[string]any{"course_id": 555, "confirm": true})
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["status"] != "success" || data["course_id"] != 555.0 {
		t.Fatalf("data = %v", data)
	}
	if asMap(data["course"])["name"] != "River Loop" {
		t.Fatalf("course = %v", data["course"])
	}
	if calls := f.callsMatching("DELETE", "/course-service/course/555"); len(calls) != 1 {
		t.Fatalf("delete calls = %d", len(calls))
	}
}

func TestDeleteCourseErrorIsCaught(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /course-service/course/555", map[string]any{"courseId": 555.0})
	f.on("DELETE /course-service/course/555", status(500, map[string]any{"message": "nope"}))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "delete_course", map[string]any{"course_id": 555, "confirm": true})
	if !strings.Contains(text, "Error deleting course") {
		t.Fatalf("text = %q", text)
	}
}

func TestBuildCoursePayloadComputesDistanceAndDefaults(t *testing.T) {
	parsed := map[string]any{
		"geoPoints": []any{
			map[string]any{"latitude": 40.0, "longitude": -105.0, "elevation": 1600.0},
			map[string]any{"latitude": 40.0, "longitude": -105.0, "elevation": nil},
		},
	}
	payload, err := buildCoursePayload(parsed, "X", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if payload["courseName"] != "X" || payload["activityTypePk"] != 1 {
		t.Fatalf("payload = %v", payload)
	}
	if payload["distanceMeter"] != 0.0 {
		t.Fatalf("distanceMeter = %v", payload["distanceMeter"])
	}
	points := asList(payload["geoPoints"])
	if asMap(points[1])["elevation"] != 0.0 {
		t.Fatalf("elevation = %v", asMap(points[1])["elevation"])
	}
	if asMap(asMap(payload["boundingBox"])["lowerLeft"])["latitude"] != 40.0 {
		t.Fatalf("bbox = %v", payload["boundingBox"])
	}
}

func TestBuildCoursePayloadRejectsTooFewPoints(t *testing.T) {
	_, err := buildCoursePayload(map[string]any{}, "X", 1, nil)
	if err == nil || !strings.Contains(err.Error(), "no geoPoints") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveGpxOutputPath(t *testing.T) {
	if got, err := resolveGPXOutputPath(123, "/tmp/custom.gpx"); err != nil || got != "/tmp/custom.gpx" {
		t.Fatalf("custom file = %q, %v", got, err)
	}
	if got, err := resolveGPXOutputPath(123, "/tmp/dir/"); err != nil || got != "/tmp/dir/123.gpx" {
		t.Fatalf("custom dir = %q, %v", got, err)
	}
	if got, err := resolveGPXOutputPath(123, ""); err != nil || !strings.HasSuffix(got, "courses/123.gpx") {
		t.Fatalf("default = %q, %v", got, err)
	}
}

// --- ported from tests/unit/test_course_preview.py ---

func TestPreviewFromCourseDetails(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /course-service/course/99", map[string]any{
		"courseId": 99.0, "courseName": "River Loop", "distanceMeter": 10250.5,
		"activityType": map[string]any{"typeKey": "running"},
	})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "delete_course", map[string]any{"course_id": 99})
	var data map[string]any
	decodeJSON(t, text, &data)
	preview := asMap(data["course"])
	if preview["course_id"] != 99.0 || preview["name"] != "River Loop" {
		t.Fatalf("preview = %v", preview)
	}
	if preview["activity"] != "running" || preview["distance_m"] != 10250.5 {
		t.Fatalf("preview = %v", preview)
	}
}

func TestPreviewFallsBackToCourseListOnDetailsError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /course-service/course/99", status(403, map[string]any{"message": "403 Forbidden"}))
	f.on("GET "+coursesListPath, []any{
		map[string]any{"courseId": 98.0, "courseName": "Other Course"},
		map[string]any{
			"courseId": 99.0, "courseName": "Gravel Loop", "distanceInMeters": 45000.0,
			"activityType": map[string]any{"typeKey": "gravel_cycling"}, "createdDateFormatted": "2024-03-01",
		},
	})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "delete_course", map[string]any{"course_id": 99})
	var data map[string]any
	decodeJSON(t, text, &data)
	preview := asMap(data["course"])
	if preview["name"] != "Gravel Loop" || preview["activity"] != "gravel_cycling" || preview["created"] != "2024-03-01" {
		t.Fatalf("preview = %v", preview)
	}
}

func TestPreviewWithNoCourseDataKeepsOnlyID(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /course-service/course/99", status(404, map[string]any{"message": "404 Not Found"}))
	f.on("GET "+coursesListPath, []any{})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "delete_course", map[string]any{"course_id": 99})
	var data map[string]any
	decodeJSON(t, text, &data)
	preview := asMap(data["course"])
	sameJSON(t, preview, map[string]any{"course_id": 99.0})
}

func TestUploadCourseFormatsLargeCourseIDInURL(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+coursesImportPath, map[string]any{
		"courseName": "Long Course",
		"geoPoints": []any{
			map[string]any{"latitude": 40.0, "longitude": -105.0, "elevation": 1600.0},
			map[string]any{"latitude": 40.01, "longitude": -105.01, "elevation": 1610.0},
		},
	})
	f.on("POST "+coursesListPath, map[string]any{
		"courseId": 38474628.0, "courseName": "Long Course", "activityTypePk": 1.0,
	})
	cs := newSession(t, f, nil)
	gpxFile := filepath.Join(t.TempDir(), "long.gpx")
	if err := os.WriteFile(gpxFile, []byte("<gpx><trk><trkseg></trkseg></trk></gpx>"), 0o644); err != nil {
		t.Fatal(err)
	}

	text, _ := callTool(t, cs, "upload_course", map[string]any{"gpx_path": gpxFile})
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["course_id"] != 38474628.0 {
		t.Fatalf("course_id = %v", data["course_id"])
	}
	url, _ := data["url"].(string)
	if !strings.HasSuffix(url, "/modern/course/38474628") {
		t.Fatalf("url = %q", url)
	}
	idPart := url[strings.LastIndex(url, "/")+1:]
	if idPart != "38474628" {
		t.Fatalf("url id uses non-integer formatting: %q", url)
	}
}
