package server

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/elgatito/garmin-connect-go-mcp/internal/accounts"
)

// Ported from tests/integration/test_courses_tools.py and tests/unit/test_course_preview.py.

const (
	cList   = "GET /course-service/course"
	cImport = "POST /course-service/course/import"
	cSave   = "POST /course-service/course"
)

func cGet(id int) string { return fmt.Sprintf("GET /course-service/course/%d", id) }

func coursesFake(t *testing.T, routes map[string]any) *fakeGarmin {
	t.Helper()
	f := newFakeGarmin(t)
	for k, v := range routes {
		f.on(k, v)
	}
	return f
}

// courseBase is https://connect.<Client.Domain()>; the fake's endpoints make the domain its host:port.
func courseBase(f *fakeGarmin) string {
	return "https://connect." + strings.TrimPrefix(f.URL, "http://")
}

func courseText(t *testing.T, f *fakeGarmin, tool string, args map[string]any) (string, bool) {
	t.Helper()
	return callTool(t, newSession(t, f, nil), tool, args)
}

func courseOK(t *testing.T, f *fakeGarmin, tool string, args map[string]any) map[string]any {
	t.Helper()
	return resultMap(t, callOK(t, newSession(t, f, nil), tool, args))
}

func wantCourseErr(t *testing.T, text string, isErr bool, sub string) {
	t.Helper()
	if !isErr || !strings.Contains(text, sub) {
		t.Fatalf("want error containing %q, got isErr=%v %q", sub, isErr, text)
	}
}

// --- get_courses ---

func TestGetCoursesCuratesFields(t *testing.T) {
	f := coursesFake(t, map[string]any{cList: []any{map[string]any{
		"courseId": 111.0, "courseName": "River Loop", "distanceInMeters": 10250.5, "elevationGainInMeters": 120.0,
		"elevationLossInMeters": 118.0, "activityType": map[string]any{"typeKey": "running"}, "hasPaceBand": false,
		"createdDateFormatted": "2024-03-01",
	}}})
	d := courseOK(t, f, "get_courses", nil)
	if d["count"] != 1.0 {
		t.Errorf("count = %v", d["count"])
	}
	want := map[string]any{"course_id": 111.0, "name": "River Loop", "distance_m": 10250.5, "elevation_gain_m": 120.0,
		"elevation_loss_m": 118.0, "activity": "running", "has_pace_band": false, "created": "2024-03-01"}
	if c := toolList(t, "courses", d)[0]; !reflect.DeepEqual(c, want) {
		t.Errorf("course = %v", c)
	}
	if n := len(f.requests(cList)); n != 1 {
		t.Errorf("GET /course-service/course called %d times", n)
	}
}

func TestGetCoursesEmpty(t *testing.T) {
	d := courseOK(t, coursesFake(t, map[string]any{cList: []any{}}), "get_courses", nil)
	if d["count"] != 0.0 || len(toolList(t, "courses", d)) != 0 {
		t.Errorf("got %v", d)
	}
}

func TestGetCoursesErrorIsCaught(t *testing.T) {
	f := coursesFake(t, map[string]any{cList: status(500, map[string]any{"message": "boom"})})
	text, isErr := courseText(t, f, "get_courses", nil)
	wantCourseErr(t, text, isErr, "Error listing courses")
}

// --- get_course_details ---

func TestGetCourseDetailsSuccess(t *testing.T) {
	f := coursesFake(t, map[string]any{cGet(777): map[string]any{
		"courseId": 777.0, "courseName": "Bikepack Day 1", "distanceInMeters": 45000.0, "elevationGainInMeters": 350.0,
		"elevationLossInMeters": 300.0, "activityType": map[string]any{"typeKey": "gravel_cycling"},
		"coursePoints": []any{map[string]any{"name": "Water Fountain", "pointType": "WATER", "lat": 52.2, "lon": 21.0, "distance": 12000.0}},
		"geoPoints":    []any{map[string]any{"latitude": 52.1, "longitude": 20.9}, map[string]any{"latitude": 52.2, "longitude": 21.0}},
	}})
	d := courseOK(t, f, "get_course_details", map[string]any{"course_id": 777})
	if d["course_id"] != 777.0 || d["name"] != "Bikepack Day 1" || d["distance_m"] != 45000.0 || d["waypoints_count"] != 1.0 ||
		d["geo_points_count"] != 2.0 || d["elevation_gain_m"] != 350.0 || d["elevation_loss_m"] != 300.0 || d["activity"] != "gravel_cycling" {
		t.Errorf("got %v", d)
	}
	if w := asMap(toolList(t, "waypoints", d)[0]); w["type"] != "WATER" || w["name"] != "Water Fountain" || w["lat"] != 52.2 || w["lon"] != 21.0 || w["distance_m"] != 12000.0 {
		t.Errorf("waypoint %v", w)
	}
	if d["url"] != courseBase(f)+"/modern/course/777" {
		t.Errorf("url = %v", d["url"])
	}
	if n := len(f.requests(cGet(777))); n != 1 {
		t.Errorf("details requested %d times", n)
	}
}

func TestGetCourseDetailsTreatsNullCoursePointsAsEmpty(t *testing.T) {
	f := coursesFake(t, map[string]any{cGet(778): map[string]any{
		"courseId": 778.0, "courseName": "Track Only", "coursePoints": nil,
		"geoPoints": []any{map[string]any{"latitude": 52.1, "longitude": 20.9}, map[string]any{"latitude": 52.2, "longitude": 21.0}},
	}})
	d := courseOK(t, f, "get_course_details", map[string]any{"course_id": 778})
	if d["waypoints_count"] != 0.0 || len(toolList(t, "waypoints", d)) != 0 || d["geo_points_count"] != 2.0 {
		t.Errorf("got %v", d)
	}
}

func TestGetCourseDetailsTreatsNullGeoPointsAsEmpty(t *testing.T) {
	f := coursesFake(t, map[string]any{cGet(779): map[string]any{"courseId": 779.0, "courseName": "Metadata Only", "coursePoints": nil, "geoPoints": nil}})
	d := courseOK(t, f, "get_course_details", map[string]any{"course_id": 779})
	if d["waypoints_count"] != 0.0 || d["geo_points_count"] != 0.0 {
		t.Errorf("got %v", d)
	}
}

func TestGetCourseDetailsErrorIsCaught(t *testing.T) {
	f := coursesFake(t, map[string]any{cGet(999): status(404, map[string]any{"message": "Not found"})})
	text, isErr := courseText(t, f, "get_course_details", map[string]any{"course_id": 999})
	wantCourseErr(t, text, isErr, "Error fetching course details")
}

func TestGetCourseDetailsFallsBackToMeterFields(t *testing.T) {
	f := coursesFake(t, map[string]any{cGet(5): map[string]any{"courseId": 5.0, "distanceMeter": 12.5, "elevationGainMeter": 3.0, "elevationLossMeter": 4.0}})
	d := courseOK(t, f, "get_course_details", map[string]any{"course_id": 5})
	if d["distance_m"] != 12.5 || d["elevation_gain_m"] != 3.0 || d["elevation_loss_m"] != 4.0 {
		t.Errorf("got %v", d)
	}
}

func TestGetCourseDetailsNonDictIsReturnedAsIs(t *testing.T) {
	f := coursesFake(t, map[string]any{cGet(6): []any{1.0}})
	text, isErr := courseText(t, f, "get_course_details", map[string]any{"course_id": 6})
	if isErr || !reflect.DeepEqual(decode(t, text), []any{1.0}) {
		t.Errorf("got %v %q", isErr, text)
	}
}

// --- download_course_gpx ---

type gpxDoc struct {
	Metadata struct {
		Name string `xml:"name"`
	} `xml:"metadata"`
	Wpt []struct {
		Lat  string `xml:"lat,attr"`
		Lon  string `xml:"lon,attr"`
		Name string `xml:"name"`
		Type string `xml:"type"`
	} `xml:"wpt"`
	Trk struct {
		Name string `xml:"name"`
		Pts  []struct {
			Lat string  `xml:"lat,attr"`
			Lon string  `xml:"lon,attr"`
			Ele *string `xml:"ele"`
		} `xml:"trkseg>trkpt"`
	} `xml:"trk"`
}

func parseGPX(t *testing.T, data []byte) gpxDoc {
	t.Helper()
	var g gpxDoc
	if err := xml.Unmarshal(data, &g); err != nil {
		t.Fatalf("GPX is not well-formed: %v\n%s", err, data)
	}
	return g
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func downloadGPX(t *testing.T, course map[string]any, id int, path string) map[string]any {
	t.Helper()
	f := coursesFake(t, map[string]any{cGet(id): course})
	d := courseOK(t, f, "download_course_gpx", map[string]any{"course_id": id, "output_path": path})
	if n := len(f.requests(cGet(id))); n != 1 {
		t.Errorf("course requested %d times", n)
	}
	return d
}

func TestDownloadCourseGpxExpandsSmallNumbersToDecimal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "small_numbers.gpx")
	d := downloadGPX(t, map[string]any{
		"courseName":   "Near the equator and prime meridian",
		"geoPoints":    []any{map[string]any{"latitude": 0.00005, "longitude": -0.00005, "elevation": 0.000001}},
		"coursePoints": []any{map[string]any{"lat": -0.00000001, "lon": 0.00000001}},
	}, 888, path)
	if d["status"] != "success" {
		t.Fatal(d)
	}
	g := parseGPX(t, []byte(readFile(t, path)))
	if w := g.Wpt[0]; w.Lat != "-0.00000001" || w.Lon != "0.00000001" {
		t.Errorf("wpt %+v", w)
	}
	if p := g.Trk.Pts[0]; p.Lat != "0.00005" || p.Lon != "-0.00005" || p.Ele == nil || *p.Ele != "0.000001" {
		t.Errorf("trkpt %+v", p)
	}
}

func TestDownloadCourseGpxSuccess(t *testing.T) {
	out := filepath.Join(t.TempDir(), "mountain_pass.gpx")
	d := downloadGPX(t, map[string]any{
		"courseId": 888.0, "courseName": "Mountain Pass",
		"geoPoints": []any{map[string]any{"latitude": 46.1, "longitude": 8.1, "elevation": 1200.0},
			map[string]any{"latitude": 46.2, "longitude": 8.2, "elevation": 1400.0}},
		"coursePoints": []any{map[string]any{"name": "Summit Rest", "pointType": "SUMMIT", "lat": 46.2, "lon": 8.2}},
	}, 888, out)
	if d["status"] != "success" || d["course_id"] != 888.0 || d["waypoints_count"] != 1.0 || d["track_points_count"] != 2.0 ||
		d["name"] != "Mountain Pass" || d["gpx_path"] != out {
		t.Errorf("got %v", d)
	}
	content := readFile(t, out)
	for _, want := range []string{"<gpx version='1.1'", "<name>Mountain Pass</name>", "<wpt lat='46.2' lon='8.2'>", "<name>Summit Rest</name>",
		"<trkpt lat='46.1' lon='8.1'><ele>1200.0</ele></trkpt>"} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in\n%s", want, content)
		}
	}
	want := strings.Join([]string{
		"<?xml version='1.0' encoding='UTF-8'?>",
		"<gpx version='1.1' creator='GarminConnectMCP' xmlns='http://www.topografix.com/GPX/1/1'>",
		"  <metadata>", "    <name>Mountain Pass</name>", "  </metadata>",
		"  <wpt lat='46.2' lon='8.2'>", "    <name>Summit Rest</name>", "    <type>SUMMIT</type>", "  </wpt>",
		"  <trk>", "    <name>Mountain Pass</name>", "    <trkseg>",
		"      <trkpt lat='46.1' lon='8.1'><ele>1200.0</ele></trkpt>",
		"      <trkpt lat='46.2' lon='8.2'><ele>1400.0</ele></trkpt>",
		"    </trkseg>", "  </trk>", "</gpx>"}, "\n")
	if content != want {
		t.Errorf("content =\n%s", content)
	}
}

func TestDownloadCourseGpxEscapesXmlSpecialCharacters(t *testing.T) {
	out := filepath.Join(t.TempDir(), "rock_and_roll.gpx")
	d := downloadGPX(t, map[string]any{
		"courseId": 891.0, "courseName": "Rock & Roll <Loop>",
		"geoPoints": []any{map[string]any{"latitude": 46.1, "longitude": 8.1, "elevation": 1200.0},
			map[string]any{"latitude": 46.2, "longitude": 8.2, "elevation": 1400.0}},
		"coursePoints": []any{map[string]any{"name": "Café & Water", "pointType": "WATER", "lat": 46.2, "lon": 8.2}},
	}, 891, out)
	if d["status"] != "success" || d["name"] != "Rock & Roll <Loop>" {
		t.Errorf("got %v", d)
	}
	g := parseGPX(t, []byte(readFile(t, out)))
	if g.Metadata.Name != "Rock & Roll <Loop>" || g.Trk.Name != "Rock & Roll <Loop>" || g.Wpt[0].Name != "Café & Water" || g.Wpt[0].Type != "WATER" || len(g.Trk.Pts) != 2 {
		t.Errorf("parsed %+v", g)
	}
}

func TestDownloadCourseGpxNullCourseNameUsesDefault(t *testing.T) {
	out := filepath.Join(t.TempDir(), "unnamed.gpx")
	d := downloadGPX(t, map[string]any{
		"courseId": 892.0, "courseName": nil,
		"geoPoints":    []any{map[string]any{"latitude": 46.1, "longitude": 8.1, "elevation": nil}},
		"coursePoints": []any{map[string]any{"name": nil, "pointType": nil, "lat": 46.1, "lon": 8.1}},
	}, 892, out)
	if d["name"] != "Garmin Course 892" {
		t.Errorf("name = %v", d["name"])
	}
	content := readFile(t, out)
	if strings.Contains(content, "None") || strings.Contains(content, "<ele>") {
		t.Errorf("content =\n%s", content)
	}
	g := parseGPX(t, []byte(content))
	if g.Metadata.Name != "Garmin Course 892" || g.Wpt[0].Name != "Waypoint" || g.Wpt[0].Type != "GENERIC" {
		t.Errorf("parsed %+v", g)
	}
}

func TestDownloadCoursePointNameFallsBackToPointType(t *testing.T) {
	out := filepath.Join(t.TempDir(), "p.gpx")
	downloadGPX(t, map[string]any{"courseName": "N", "geoPoints": []any{map[string]any{"latitude": 1.5, "longitude": 2.0}},
		"coursePoints": []any{map[string]any{"name": "", "pointType": "FOOD", "lat": 1.0, "lon": 2.0}, map[string]any{"name": "no coords", "lat": 1.0}}}, 1, out)
	g := parseGPX(t, []byte(readFile(t, out)))
	if len(g.Wpt) != 1 || g.Wpt[0].Name != "FOOD" || g.Wpt[0].Type != "FOOD" || g.Wpt[0].Lon != "2.0" {
		t.Errorf("parsed %+v", g.Wpt)
	}
	if p := g.Trk.Pts[0]; p.Lon != "2.0" || p.Ele != nil {
		t.Errorf("trkpt %+v", p)
	}
}

func TestDownloadCourseGpxPreservesTrackWhenCoursePointsAreNull(t *testing.T) {
	out := filepath.Join(t.TempDir(), "track_only.gpx")
	d := downloadGPX(t, map[string]any{
		"courseId": 889.0, "courseName": "Track Only", "coursePoints": nil,
		"geoPoints": []any{map[string]any{"latitude": 46.1, "longitude": 8.1, "elevation": 1200.0},
			map[string]any{"latitude": 46.2, "longitude": 8.2, "elevation": 1400.0}},
	}, 889, out)
	if d["status"] != "success" || d["waypoints_count"] != 0.0 || d["track_points_count"] != 2.0 {
		t.Errorf("got %v", d)
	}
	if !strings.Contains(readFile(t, out), "<trkpt lat='46.1' lon='8.1'><ele>1200.0</ele></trkpt>") {
		t.Error("track point missing")
	}
}

func TestDownloadCourseGpxReportsNullGeoPointsWithoutWriting(t *testing.T) {
	out := filepath.Join(t.TempDir(), "metadata_only.gpx")
	f := coursesFake(t, map[string]any{cGet(890): map[string]any{"courseId": 890.0, "courseName": "Metadata Only", "geoPoints": nil, "coursePoints": nil}})
	text, isErr := courseText(t, f, "download_course_gpx", map[string]any{"course_id": 890, "output_path": out})
	wantCourseErr(t, text, isErr, "Error: course 890 not found or missing geoPoints")
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file written: %v", err)
	}
}

func TestDownloadCourseGpxMissingCourse(t *testing.T) {
	// no output_path: the default ./courses directory must not be created either
	t.Chdir(t.TempDir())
	f := coursesFake(t, map[string]any{cGet(404): map[string]any{}})
	text, isErr := courseText(t, f, "download_course_gpx", map[string]any{"course_id": 404})
	wantCourseErr(t, text, isErr, "Error: course 404 not found")
	if _, err := os.Stat("courses"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("courses dir created: %v", err)
	}
}

var trackOnlyCourse = func() map[string]any {
	return map[string]any{"courseId": 893.0, "courseName": "Track Only",
		"geoPoints": []any{map[string]any{"latitude": 46.1, "longitude": 8.1, "elevation": 1200.0}}, "coursePoints": nil}
}

func TestDownloadCourseGpxRejectsNonGpxOutputFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "x")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, ".zshrc")
	if err := os.WriteFile(target, []byte("export PATH=/usr/bin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := coursesFake(t, map[string]any{cGet(893): trackOnlyCourse()})
	text, isErr := courseText(t, f, "download_course_gpx", map[string]any{"course_id": 893, "output_path": target})
	wantCourseErr(t, text, isErr, "output_path must be a directory or a .gpx file")
	if got := readFile(t, target); got != "export PATH=/usr/bin\n" {
		t.Errorf("file changed: %q", got)
	}
	if n := len(f.requests(cGet(893))); n != 0 {
		t.Errorf("course requested %d times, want 0", n)
	}
}

func TestDownloadCourseGpxAcceptsGpxOutputFile(t *testing.T) {
	target := filepath.Join(t.TempDir(), "Route.GPX")
	d := downloadGPX(t, trackOnlyCourse(), 893, target)
	if d["gpx_path"] != target {
		t.Errorf("gpx_path = %v", d["gpx_path"])
	}
	var g struct{ XMLName xml.Name }
	if err := xml.Unmarshal([]byte(readFile(t, target)), &g); err != nil || g.XMLName.Space != "http://www.topografix.com/GPX/1/1" || g.XMLName.Local != "gpx" {
		t.Errorf("root = %+v err=%v", g.XMLName, err)
	}
}

func TestDownloadCourseGpxDirectoryOutputPathCreatesIt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new", "sub") + "/"
	d := downloadGPX(t, trackOnlyCourse(), 893, dir)
	want := filepath.Join(filepath.Clean(dir), "893.gpx")
	if d["gpx_path"] != want {
		t.Errorf("gpx_path = %v, want %v", d["gpx_path"], want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Error(err)
	}
}

func TestDownloadCourseGpxDefaultsToConfiguredDownloadDir(t *testing.T) {
	root := t.TempDir()
	downloadDir := filepath.Join(root, "downloads")
	cwd := filepath.Join(root, "cwd")
	if err := os.Mkdir(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	st := newStore(t)
	if err := st.SaveSettings(testEmail, accounts.Settings{Domain: "garmin.com", FitDownloadDir: downloadDir}); err != nil {
		t.Fatal(err)
	}
	f := coursesFake(t, map[string]any{cGet(893): trackOnlyCourse()})
	d := resultMap(t, callOK(t, newSessionWithStore(t, f, st, nil), "download_course_gpx", map[string]any{"course_id": 893}))
	want := filepath.Join(downloadDir, "893.gpx")
	if d["gpx_path"] != want {
		t.Errorf("gpx_path = %v, want %v", d["gpx_path"], want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "courses")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("courses dir exists: %v", err)
	}
}

func TestDownloadCourseGpxEnvDownloadDirBeatsSettingsAndFallsBackToCoursesDir(t *testing.T) {
	root := t.TempDir()
	envDir := filepath.Join(root, "env")
	st := newStore(t)
	if err := st.SaveSettings(testEmail, accounts.Settings{Domain: "garmin.com", FitDownloadDir: filepath.Join(root, "cfg")}); err != nil {
		t.Fatal(err)
	}
	f := coursesFake(t, map[string]any{cGet(893): trackOnlyCourse()})
	d := resultMap(t, callOK(t, newSessionWithStore(t, f, st, map[string]string{"GARMIN_FIT_DOWNLOAD_DIR": envDir}), "download_course_gpx", map[string]any{"course_id": 893}))
	if d["gpx_path"] != filepath.Join(envDir, "893.gpx") {
		t.Errorf("gpx_path = %v", d["gpx_path"])
	}
	cwd := filepath.Join(root, "cwd")
	if err := os.Mkdir(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	d = resultMap(t, callOK(t, newSession(t, f, nil), "download_course_gpx", map[string]any{"course_id": 893}))
	if d["gpx_path"] != filepath.Join(cwd, "courses", "893.gpx") {
		t.Errorf("gpx_path = %v", d["gpx_path"])
	}
}

// --- upload_course ---

func gpxFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func noUploads(t *testing.T, f *fakeGarmin) {
	t.Helper()
	if n := len(f.requests(cImport)) + len(f.requests(cSave)); n != 0 {
		t.Errorf("%d upload requests, want 0", n)
	}
}

func TestUploadCourseRejectsNonGpx(t *testing.T) {
	f := newFakeGarmin(t)
	text, isErr := courseText(t, f, "upload_course", map[string]any{"gpx_path": "/tmp/route.tcx"})
	wantCourseErr(t, text, isErr, "only .gpx files are allowed")
	noUploads(t, f)
}

func TestUploadCourseMissingFile(t *testing.T) {
	f := newFakeGarmin(t)
	text, isErr := courseText(t, f, "upload_course", map[string]any{"gpx_path": "/tmp/nonexistent-route-12345.gpx"})
	wantCourseErr(t, text, isErr, "GPX file not found")
	noUploads(t, f)
}

func TestUploadCourseRejectsUnknownActivityType(t *testing.T) {
	f := newFakeGarmin(t)
	p := gpxFile(t, "valid.gpx", "<gpx></gpx>")
	text, isErr := courseText(t, f, "upload_course", map[string]any{"gpx_path": p, "activity_type": "paragliding"})
	wantCourseErr(t, text, isErr, "unknown activity_type 'paragliding'")
	if !strings.Contains(text, "Supported: cycling, gravel_cycling, hiking, mountain_biking, road_biking, running, trail_running, walking.") {
		t.Errorf("text = %q", text)
	}
	noUploads(t, f)
}

func importSkeleton() map[string]any {
	return map[string]any{"courseName": "River Loop", "geoPoints": []any{
		map[string]any{"latitude": 40.0, "longitude": -105.0, "elevation": 1600.0},
		map[string]any{"latitude": 40.01, "longitude": -105.01, "elevation": 1610.0}}}
}

func TestUploadCourseSuccess(t *testing.T) {
	const gpx = "<gpx><trk><trkseg></trkseg></trk></gpx>"
	p := gpxFile(t, "test.gpx", gpx)
	f := coursesFake(t, map[string]any{cImport: importSkeleton(), cSave: map[string]any{
		"courseId": 999.0, "courseName": "River Loop", "distanceMeter": 1250.0, "elevationGainMeter": 10.0, "elevationLossMeter": 0.0, "activityTypePk": 1.0}})
	d := courseOK(t, f, "upload_course", map[string]any{"gpx_path": p, "course_name": "River Loop", "activity_type": "running"})
	want := map[string]any{"status": "success", "course_id": 999.0, "name": "River Loop", "distance_m": 1250.0, "elevation_gain_m": 10.0,
		"elevation_loss_m": 0.0, "activity_type_id": 1.0, "url": courseBase(f) + "/modern/course/999"}
	if !reflect.DeepEqual(d, want) {
		t.Errorf("got %v", d)
	}

	// Two-step flow: POST /import (multipart) then POST /course (JSON).
	imp, sv := f.requests(cImport), f.requests(cSave)
	if len(imp) != 1 || len(sv) != 1 {
		t.Fatalf("import=%d save=%d", len(imp), len(sv))
	}
	mt, params, err := mime.ParseMediaType(imp[0].Header.Get("Content-Type"))
	if err != nil || mt != "multipart/form-data" {
		t.Fatalf("content type %q", imp[0].Header.Get("Content-Type"))
	}
	part, err := multipart.NewReader(bytes.NewReader(imp[0].Body), params["boundary"]).NextPart()
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(part)
	if part.FormName() != "file" || part.FileName() != "test.gpx" || part.Header.Get("Content-Type") != "application/gpx+xml" || string(body) != gpx {
		t.Errorf("part %q %q %q %q", part.FormName(), part.FileName(), part.Header.Get("Content-Type"), body)
	}
	var payload map[string]any
	if err := json.Unmarshal(sv[0].Body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["courseName"] != "River Loop" || payload["activityTypePk"] != 1.0 || payload["description"] != nil || payload["rulePK"] != 2.0 || payload["sourceTypeId"] != 3.0 {
		t.Errorf("payload %v", payload)
	}
	if len(asList(payload["geoPoints"])) != 2 || asMap(payload["boundingBox"])["lowerLeftLatIsSet"] != true {
		t.Errorf("payload %v", payload)
	}
}

func TestUploadCoursePostsGarminActivityTypeId(t *testing.T) {
	for _, c := range []struct {
		name string
		id   float64
	}{{"running", 1}, {"cycling", 2}, {"hiking", 3}, {"walking", 9}, {"mountain_biking", 5}, {"trail_running", 6}, {"road_biking", 10}, {"gravel_cycling", 143}} {
		t.Run(c.name, func(t *testing.T) {
			p := gpxFile(t, "route.gpx", "<gpx><trk><trkseg></trkseg></trk></gpx>")
			f := coursesFake(t, map[string]any{cImport: importSkeleton(), cSave: map[string]any{"courseId": 1000.0, "courseName": "Route", "activityTypePk": c.id}})
			d := courseOK(t, f, "upload_course", map[string]any{"gpx_path": p, "activity_type": strings.ToUpper(c.name)})
			if d["status"] != "success" {
				t.Fatal(d)
			}
			var payload map[string]any
			if err := json.Unmarshal(f.requests(cSave)[0].Body, &payload); err != nil {
				t.Fatal(err)
			}
			if payload["activityTypePk"] != c.id {
				t.Errorf("activityTypePk = %v, want %v", payload["activityTypePk"], c.id)
			}
		})
	}
}

func TestUploadCourseNameAndDescriptionDefaults(t *testing.T) {
	p := gpxFile(t, "my_ride.gpx", "<gpx/>")
	skel := importSkeleton()
	delete(skel, "courseName")
	f := coursesFake(t, map[string]any{cImport: skel, cSave: map[string]any{"courseId": 1.0}})
	courseOK(t, f, "upload_course", map[string]any{"gpx_path": p, "description": "Nice"})
	var payload map[string]any
	if err := json.Unmarshal(f.requests(cSave)[0].Body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["courseName"] != "my_ride" || payload["description"] != "Nice" {
		t.Errorf("payload %v", payload)
	}
}

func TestUploadCourseNoGeoPointsIsReported(t *testing.T) {
	p := gpxFile(t, "empty.gpx", "<gpx/>")
	f := coursesFake(t, map[string]any{cImport: map[string]any{"courseName": "x"}})
	text, isErr := courseText(t, f, "upload_course", map[string]any{"gpx_path": p})
	wantCourseErr(t, text, isErr, "Error uploading course: GPX parsed skeleton contains no geoPoints")
	if len(f.requests(cSave)) != 0 {
		t.Error("save must not be called")
	}
}

func TestUploadCourseImportErrorIsCaught(t *testing.T) {
	p := gpxFile(t, "bad.gpx", "<gpx/>")
	f := coursesFake(t, map[string]any{cImport: status(400, map[string]any{"message": "bad gpx"})})
	text, isErr := courseText(t, f, "upload_course", map[string]any{"gpx_path": p})
	wantCourseErr(t, text, isErr, "Error uploading course: ")
}

func TestPySuffix(t *testing.T) {
	for in, want := range map[string]string{"a.gpx": ".gpx", "/x/A.GPX": ".GPX", ".gpx": "", "a.": "", "a": "", "a.tar.gz": ".gz", "/d.d/a": ""} {
		if got := pySuffix(in); got != want {
			t.Errorf("pySuffix(%q) = %q, want %q", in, got, want)
		}
	}
}

// --- delete_course ---

func TestDeleteCourseRequiresConfirmation(t *testing.T) {
	f := coursesFake(t, map[string]any{cGet(555): map[string]any{"courseId": 555.0, "courseName": "River Loop", "distanceMeter": 10250.5, "activityType": map[string]any{"typeKey": "running"}}})
	d := courseOK(t, f, "delete_course", map[string]any{"course_id": 555})
	c := asMap(d["course"])
	if d["status"] != "needs_confirmation" || c["name"] != "River Loop" || c["distance_m"] != 10250.5 || c["activity"] != "running" || c["course_id"] != 555.0 {
		t.Errorf("got %v", d)
	}
	if d["message"] != "This permanently deletes the course from Garmin Connect and cannot be undone. Call again with confirm=true to proceed." {
		t.Errorf("message = %v", d["message"])
	}
	if n := len(f.requests(cGet(555))); n != 1 {
		t.Errorf("details requested %d times", n)
	}
	if n := len(f.requests("DELETE /course-service/course/555")); n != 0 {
		t.Errorf("DELETE called %d times", n)
	}
}

func TestDeleteCourseSuccess(t *testing.T) {
	f := coursesFake(t, map[string]any{cGet(555): map[string]any{"courseId": 555.0, "courseName": "River Loop"}, "DELETE /course-service/course/555": map[string]any{}})
	var log bytes.Buffer
	st := newStore(t)
	srv, err := New(testOptions(f, st, nil, &log))
	if err != nil {
		t.Fatal(err)
	}
	d := resultMap(t, callOK(t, connect(t, srv), "delete_course", map[string]any{"course_id": 555, "confirm": true}))
	if d["status"] != "success" || d["course_id"] != 555.0 || asMap(d["course"])["name"] != "River Loop" || d["message"] != "Course 555 deleted" {
		t.Errorf("got %v", d)
	}
	if n := len(f.requests("DELETE /course-service/course/555")); n != 1 {
		t.Errorf("DELETE called %d times", n)
	}
	if got := log.String(); !strings.HasPrefix(got, "delete_course: deleted course 555 name='River Loop' at ") {
		t.Errorf("log = %q", got)
	}
}

func TestDeleteCourseErrorIsCaught(t *testing.T) {
	f := coursesFake(t, map[string]any{cGet(555): map[string]any{"courseId": 555.0}, "DELETE /course-service/course/555": status(500, map[string]any{"message": "nope"})})
	text, isErr := courseText(t, f, "delete_course", map[string]any{"course_id": 555, "confirm": true})
	wantCourseErr(t, text, isErr, "Error deleting course")
}

// --- pure helpers ---

func TestBuildCoursePayloadComputesDistanceAndDefaults(t *testing.T) {
	parsed := map[string]any{"geoPoints": []any{
		map[string]any{"latitude": 40.0, "longitude": -105.0, "elevation": 1600.0},
		map[string]any{"latitude": 40.0, "longitude": -105.0, "elevation": nil}}}
	p, err := buildCoursePayload(parsed, "X", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p["courseName"] != "X" || p["activityTypePk"] != 1 {
		t.Errorf("payload %v", p)
	}
	if p["distanceMeter"] != 0.0 { // identical points -> zero total distance
		t.Errorf("distanceMeter = %v", p["distanceMeter"])
	}
	if e := asMap(asList(p["geoPoints"])[1])["elevation"]; e != 0.0 { // missing elevation is backfilled
		t.Errorf("elevation = %v", e)
	}
	if ll := asMap(asMap(p["boundingBox"])["lowerLeft"]); ll["latitude"] != 40.0 {
		t.Errorf("lowerLeft = %v", ll)
	}
}

func TestBuildCoursePayloadRejectsTooFewPoints(t *testing.T) {
	_, err := buildCoursePayload(map[string]any{}, "X", 1, nil)
	if err == nil || !strings.Contains(err.Error(), "no geoPoints") {
		t.Errorf("err = %v", err)
	}
}

func TestBuildCoursePayloadBoundingBoxStartPointAndBearing(t *testing.T) {
	parsed := map[string]any{"geoPoints": []any{
		map[string]any{"latitude": 10.0, "longitude": 20.0, "elevation": 5.0},
		map[string]any{"latitude": 12.0, "longitude": 18.0},
		map[string]any{"latitude": 11.0, "longitude": 22.0, "elevation": 7.0}}}
	desc := "d"
	p, err := buildCoursePayload(parsed, "X", 2, &desc)
	if err != nil {
		t.Fatal(err)
	}
	bb := asMap(p["boundingBox"])
	if c := asMap(bb["center"]); c["latitude"] != 11.0 || c["longitude"] != 20.0 {
		t.Errorf("center %v", c)
	}
	if ur := asMap(bb["upperRight"]); ur["latitude"] != 12.0 || ur["longitude"] != 22.0 {
		t.Errorf("upperRight %v", ur)
	}
	if sp := asMap(p["startPoint"]); sp["latitude"] != 10.0 || sp["elevation"] != 5.0 || sp["distance"] != nil || sp["timestamp"] != nil {
		t.Errorf("startPoint %v", sp)
	}
	if p["description"] != "d" || p["endBearing"] != nil || p["elevationGainMeter"] != 0.0 || len(asList(p["coursePoints"])) != 0 {
		t.Errorf("payload %v", p)
	}
	// Python: _initial_bearing((10,20),(11,22)) = 62.8663..., rounded to compare
	if b, _ := p["startBearing"].(float64); round(b, 2) != 62.87 {
		t.Errorf("startBearing = %v", p["startBearing"])
	}
	want := haversine(asMap(asList(p["geoPoints"])[0]), asMap(asList(p["geoPoints"])[1])) + haversine(asMap(asList(p["geoPoints"])[1]), asMap(asList(p["geoPoints"])[2]))
	if p["distanceMeter"] != want {
		t.Errorf("distanceMeter = %v want %v", p["distanceMeter"], want)
	}
}

func TestHaversineOneDegreeOfLatitude(t *testing.T) {
	// Python: _haversine({"latitude":0,"longitude":0},{"latitude":1,"longitude":0}) == 111194.92664455873
	got := haversine(map[string]any{"latitude": 0.0, "longitude": 0.0}, map[string]any{"latitude": 1.0, "longitude": 0.0})
	if round(got, 6) != 111194.926645 {
		t.Errorf("got %v", got)
	}
}

func TestResolveGpxOutputPath(t *testing.T) {
	// 1. Custom file path
	if got, err := resolveGPXOutputPath(123, "/tmp/custom.gpx", ""); err != nil || got != "/tmp/custom.gpx" {
		t.Errorf("file: %q %v", got, err)
	}
	// 2. Custom directory path
	if got, err := resolveGPXOutputPath(123, "/tmp/dir/", ""); err != nil || got != "/tmp/dir/123.gpx" {
		t.Errorf("dir: %q %v", got, err)
	}
	// 3. Default path
	if got, err := resolveGPXOutputPath(123, "", ""); err != nil || !strings.HasSuffix(got, "courses/123.gpx") {
		t.Errorf("default: %q %v", got, err)
	}
	if got, _ := resolveGPXOutputPath(123, "", "/dl"); got != "/dl/123.gpx" {
		t.Errorf("configured: %q", got)
	}
	// existing directory without trailing slash, relative path resolved against cwd, ~ expanded, .gpx case-insensitive
	d := t.TempDir()
	if got, _ := resolveGPXOutputPath(7, d, ""); got != filepath.Join(d, "7.gpx") {
		t.Errorf("existing dir: %q", got)
	}
	t.Chdir(d)
	if got, _ := resolveGPXOutputPath(7, "a/../b.GPX", ""); got != filepath.Join(d, "b.GPX") {
		t.Errorf("relative: %q", got)
	}
	t.Setenv("HOME", "/home/u")
	if got, _ := resolveGPXOutputPath(7, "~/r.gpx", ""); got != "/home/u/r.gpx" {
		t.Errorf("tilde: %q", got)
	}
	if _, err := resolveGPXOutputPath(7, "/tmp/x/.zshrc", ""); err == nil || err.Error() != "output_path must be a directory or a .gpx file, got: /tmp/x/.zshrc" {
		t.Errorf("err = %v", err)
	}
}

func TestGpxDecimal(t *testing.T) {
	for _, c := range []struct {
		in   any
		want string
	}{{46.2, "46.2"}, {1200.0, "1200.0"}, {0.00005, "0.00005"}, {-0.00005, "-0.00005"}, {1e-8, "0.00000001"}, {1.5e-7, "0.00000015"},
		{1e16, "10000000000000000"}, {1.5e22, "15000000000000000000000"}, {0.0, "0.0"}, {-12.345, "-12.345"}, {123456789.125, "123456789.125"}} {
		if got, err := gpxDecimal(c.in); err != nil || got != c.want {
			t.Errorf("gpxDecimal(%v) = %q %v, want %q", c.in, got, err, c.want)
		}
	}
	if _, err := gpxDecimal("abc"); err == nil {
		t.Error("string must be rejected")
	}
}

// --- unit/test_course_preview.py ---

type fakeConnect struct {
	course, courses any
	err             error
}

func (c fakeConnect) GetCourse(context.Context, int64) (any, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.course, nil
}
func (c fakeConnect) GetCourses(context.Context) (any, error) { return c.courses, nil }

func TestPreviewFromCourseDetails(t *testing.T) {
	p := coursePreview(context.Background(), fakeConnect{course: map[string]any{"courseId": 99.0, "courseName": "River Loop", "distanceMeter": 10250.5,
		"activityType": map[string]any{"typeKey": "running"}}}, 99)
	if p["course_id"] != int64(99) || p["name"] != "River Loop" || p["activity"] != "running" || p["distance_m"] != 10250.5 {
		t.Errorf("preview %v", p)
	}
}

func TestPreviewFallsBackToCourseListOnDetailsError(t *testing.T) {
	p := coursePreview(context.Background(), fakeConnect{err: errors.New("403 Forbidden"), courses: []any{
		map[string]any{"courseId": 98.0, "courseName": "Other Course"},
		map[string]any{"courseId": 99.0, "courseName": "Gravel Loop", "distanceInMeters": 45000.0, "activityType": map[string]any{"typeKey": "gravel_cycling"}, "createdDateFormatted": "2024-03-01"}}}, 99)
	if p["name"] != "Gravel Loop" || p["activity"] != "gravel_cycling" || p["created"] != "2024-03-01" || p["distance_m"] != 45000.0 {
		t.Errorf("preview %v", p)
	}
}

func TestPreviewWithNoCourseDataKeepsOnlyId(t *testing.T) {
	p := coursePreview(context.Background(), fakeConnect{err: errors.New("404 Not Found"), courses: []any{}}, 99)
	if !reflect.DeepEqual(p, map[string]any{"course_id": int64(99)}) {
		t.Errorf("preview %v", p)
	}
}
