package garmin

import (
	"context"
	"net/http"
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
