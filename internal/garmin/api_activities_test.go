package garmin

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestGetActivitiesActivityTypeFilter(t *testing.T) {
	for _, tc := range []struct{ typ, want string }{{"", ""}, {"running", "running"}} {
		c, _ := fixture(t, freshTokens("c1"), func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if r.URL.Path != "/activitylist-service/activities/search/activities" || q.Get("start") != "3" || q.Get("limit") != "7" {
				t.Errorf("url = %s", r.URL)
			}
			if _, has := q["activityType"]; has != (tc.want != "") || q.Get("activityType") != tc.want {
				t.Errorf("activityType in %s, want %q", r.URL.RawQuery, tc.want)
			}
			w.Write([]byte(`[]`))
		}, nil)
		if _, err := c.GetActivities(context.Background(), 3, 7, tc.typ); err != nil {
			t.Fatal(err)
		}
	}
}

func TestActivityIDMustBePositive(t *testing.T) {
	c, _ := fixture(t, freshTokens("c1"), func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected request %s", r.URL) }, nil)
	_, err := c.GetActivity(context.Background(), 0)
	if err == nil || err.Error() != "activity_id must be a positive integer, got: 0" {
		t.Errorf("err = %v", err)
	}
	if err := c.DeleteActivity(context.Background(), -5); err == nil {
		t.Error("DeleteActivity accepted -5")
	}
}

func TestGearUUIDCannotEscapeItsPathSegment(t *testing.T) {
	const evil = "x/../../foo?a=b#frag"
	calls := map[string]func(*Client) error{
		"/gear-service/gear/link/":   func(c *Client) error { _, err := c.AddGearToActivity(context.Background(), evil, 5); return err },
		"/gear-service/gear/unlink/": func(c *Client) error { _, err := c.RemoveGearFromActivity(context.Background(), evil, 5); return err },
		"/gear-service/gear/stats/":  func(c *Client) error { _, err := c.GetGearStats(context.Background(), evil); return err },
		"/gear-service/gear/user/":   func(c *Client) error { _, err := c.GetGearDefaults(context.Background(), evil); return err },
	}
	for prefix, call := range calls {
		var uri string
		c, _ := fixture(t, freshTokens("c1"), func(w http.ResponseWriter, r *http.Request) {
			uri = r.RequestURI
			w.Write([]byte(`{}`))
		}, nil)
		if err := call(c); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(uri, prefix+"x%2F..%2F..%2Ffoo%3Fa=b%23frag") || strings.Contains(uri, "?") || strings.Contains(uri, "#") {
			t.Errorf("%s: request URI %q escapes its segment", prefix, uri)
		}
	}
}

func TestPathSegEscapesDotSegments(t *testing.T) {
	for in, want := range map[string]string{"..": "%2E%2E", ".": "%2E", "a/b": "a%2Fb", "a b?": "a%20b%3F", "abc": "abc"} {
		if got := pathSeg(in); got != want {
			t.Errorf("pathSeg(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDownloadActivityFormatsAndErrors(t *testing.T) {
	var paths []string
	c, _ := fixture(t, freshTokens("c1"), func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch {
		case strings.HasSuffix(r.URL.Path, "/404"):
			w.WriteHeader(404)
			w.Write([]byte(`{"message":"gone"}`))
		case strings.HasSuffix(r.URL.Path, "/500"):
			w.WriteHeader(500)
		default:
			w.Write([]byte("bytes"))
		}
	}, nil)
	ctx := context.Background()
	for _, f := range []string{"ORIGINAL", "TCX", "GPX", "KML", "CSV"} {
		if b, err := c.DownloadActivity(ctx, 7, f); err != nil || string(b) != "bytes" {
			t.Errorf("%s: %q %v", f, b, err)
		}
	}
	want := []string{"/download-service/files/activity/7", "/download-service/export/tcx/activity/7",
		"/download-service/export/gpx/activity/7", "/download-service/export/kml/activity/7", "/download-service/export/csv/activity/7"}
	if strings.Join(paths, " ") != strings.Join(want, " ") {
		t.Errorf("paths = %v", paths)
	}
	if _, err := c.DownloadActivity(ctx, 404, "ORIGINAL"); err == nil || err.Error() != "Download client error (404): API Error 404 - gone" {
		t.Errorf("404: %v", err)
	}
	if _, err := c.DownloadActivity(ctx, 500, "ORIGINAL"); err == nil || err.Error() != "Download error: API Error 500" {
		t.Errorf("500: %v", err)
	}
	if _, err := c.DownloadActivity(ctx, 7, "PDF"); err == nil || err.Error() != "unexpected value PDF for dl_fmt" {
		t.Errorf("PDF: %v", err)
	}
}
