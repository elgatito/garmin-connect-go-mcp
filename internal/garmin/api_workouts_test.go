package garmin

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func serveWorkouts(t *testing.T, body string) (*Client, *[]string) {
	t.Helper()
	var seen []string
	c, _ := fixture(t, freshTokens("c1"), func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.RequestURI()+" "+string(b))
		w.Write([]byte(body))
	}, nil)
	return c, &seen
}

func TestWorkoutRequests(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		call func(c *Client) error
		want string
	}{
		{"list", func(c *Client) error { _, err := c.GetWorkouts(ctx, 1, 100); return err }, "GET /workout-service/workouts?limit=100&start=1 "},
		{"by id", func(c *Client) error { _, err := c.GetWorkoutByID(ctx, 123456); return err }, "GET /workout-service/workout/123456 "},
		{"fbt", func(c *Client) error { _, err := c.GetFbtAdaptiveWorkout(ctx, "d7a5-42"); return err }, "GET /workout-service/fbt-adaptive/d7a5-42 "},
		{"delete", func(c *Client) error { _, err := c.DeleteWorkout(ctx, 7); return err }, "DELETE /workout-service/workout/7 "},
		{"download", func(c *Client) error { _, err := c.DownloadWorkout(ctx, 7); return err }, "GET /workout-service/workout/FIT/7 "},
		{"upload", func(c *Client) error { _, err := c.UploadWorkout(ctx, map[string]any{"a": 1}); return err }, `POST /workout-service/workout {"a":1}`},
		{"schedule", func(c *Client) error { _, err := c.ScheduleWorkout(ctx, 7, "2024-01-15"); return err }, `POST /workout-service/schedule/7 {"date":"2024-01-15"}`},
		{"scheduled by id", func(c *Client) error { _, err := c.GetScheduledWorkoutByID(ctx, 9); return err }, "GET /workout-service/schedule/9 "},
		{"update scheduled", func(c *Client) error { _, err := c.UpdateScheduledWorkout(ctx, 9, map[string]any{"a": 1}); return err }, `PUT /workout-service/schedule/9 {"a":1}`},
		{"unschedule", func(c *Client) error { _, err := c.UnscheduleWorkout(ctx, 9); return err }, "DELETE /workout-service/schedule/9 "},
		{"month", func(c *Client) error { _, err := c.GetScheduledWorkouts(ctx, 2026, 10); return err }, "GET /calendar-service/year/2026/month/9 "},
		{"graphql", func(c *Client) error { _, err := c.QueryGraphQL(ctx, map[string]any{"query": "q"}); return err }, `POST /graphql-gateway/graphql {"query":"q"}`},
		{"event", func(c *Client) error { _, err := c.GetCalendarEvent(ctx, "12345", false); return err }, "GET /calendar-service/event/12345 "},
		{"shareable", func(c *Client) error { _, err := c.GetCalendarEvent(ctx, "u-1", true); return err }, "GET /calendar-service/event/u-1/shareable "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, seen := serveWorkouts(t, `{}`)
			if err := tc.call(c); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(*seen, []string{tc.want}) {
				t.Errorf("requests = %q, want %q", *seen, tc.want)
			}
		})
	}
}

func TestWorkoutValidationNeverReachesGarmin(t *testing.T) {
	ctx := context.Background()
	c, seen := serveWorkouts(t, `{}`)
	for name, tc := range map[string]struct {
		call func() error
		want string
	}{
		"id":        {func() error { _, err := c.GetWorkoutByID(ctx, 0); return err }, "workout_id must be a positive integer, got: 0"},
		"delete":    {func() error { _, err := c.DeleteWorkout(ctx, -1); return err }, "workout_id must be a positive integer, got: -1"},
		"download":  {func() error { _, err := c.DownloadWorkout(ctx, 0); return err }, "workout_id must be a positive integer, got: 0"},
		"schedule":  {func() error { _, err := c.ScheduleWorkout(ctx, 0, "2024-01-15"); return err }, "workout_id must be a positive integer, got: 0"},
		"date":      {func() error { _, err := c.ScheduleWorkout(ctx, 1, "nope"); return err }, "date_str must be in format 'YYYY-MM-DD', got: nope"},
		"scheduled": {func() error { _, err := c.GetScheduledWorkoutByID(ctx, 0); return err }, "scheduled_workout_id must be a positive integer, got: 0"},
		"unsched":   {func() error { _, err := c.UnscheduleWorkout(ctx, -3); return err }, "scheduled_workout_id must be a positive integer, got: -3"},
		"year":      {func() error { _, err := c.GetScheduledWorkouts(ctx, 1999, 5); return err }, "year must be 2000 or later, got: 1999"},
		"month":     {func() error { _, err := c.GetScheduledWorkouts(ctx, 2024, 13); return err }, "month must be between 1 and 12, got: 13"},
		"month0":    {func() error { _, err := c.GetScheduledWorkouts(ctx, 2024, 0); return err }, "month must be a positive integer, got: 0"},
		"start":     {func() error { _, err := c.GetWorkouts(ctx, -1, 100); return err }, "start must be non-negative, got: -1"},
		"limit":     {func() error { _, err := c.GetWorkouts(ctx, 0, 0); return err }, "limit must be a positive integer, got: 0"},
	} {
		if err := tc.call(); err == nil || err.Error() != tc.want {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
	if len(*seen) != 0 {
		t.Errorf("requests = %v", *seen)
	}
}

func TestDownloadWorkoutReturnsBytesAndWrapsErrors(t *testing.T) {
	var accept string
	c, _ := fixture(t, freshTokens("c1"), func(w http.ResponseWriter, r *http.Request) {
		accept = r.Header.Get("Accept")
		if strings.HasSuffix(r.URL.Path, "/9") {
			w.WriteHeader(404)
			w.Write([]byte(`{"message":"gone"}`))
			return
		}
		w.Write([]byte{0x0e, 0x10, 0x00})
	}, nil)
	b, err := c.DownloadWorkout(context.Background(), 5)
	if err != nil || !reflect.DeepEqual(b, []byte{0x0e, 0x10, 0x00}) || accept != "*/*" {
		t.Errorf("got %v %v accept %q", b, err, accept)
	}
	if _, err := c.DownloadWorkout(context.Background(), 9); err == nil || err.Error() != "Download error: API Error 404 - gone" {
		t.Errorf("err = %v", err)
	}
}

func TestWorkoutFreeTextCannotEscapeItsPathSegment(t *testing.T) {
	const evil = "x/../../foo?a=b#frag"
	for prefix, call := range map[string]func(*Client) error{
		"/workout-service/fbt-adaptive/": func(c *Client) error { _, err := c.GetFbtAdaptiveWorkout(context.Background(), evil); return err },
		"/calendar-service/event/":       func(c *Client) error { _, err := c.GetCalendarEvent(context.Background(), evil, false); return err },
	} {
		var uri string
		c, _ := fixture(t, freshTokens("c1"), func(w http.ResponseWriter, r *http.Request) {
			uri = r.RequestURI
			w.Write([]byte(`{}`))
		}, nil)
		if err := call(c); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(uri, prefix+"x%2F..%2F..%2Ffoo%3Fa=b%23frag") || strings.Contains(uri, "#") || strings.Count(uri, "?") != 0 {
			t.Errorf("%s: request URI %q escapes its segment", prefix, uri)
		}
	}
}
