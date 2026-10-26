package garmin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// serve answers every request with body and records "METHOD URI".
func serveTraining(t *testing.T, body string) (*Client, *[]string) {
	t.Helper()
	var seen []string
	c, _ := fixture(t, freshTokens("c1"), func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.RequestURI()+string(b))
		w.Write([]byte(body))
	}, nil)
	return c, &seen
}

func TestTrainingRequestsAndValidation(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		call func(c *Client) (any, error)
		want string
	}{
		{"progress", func(c *Client) (any, error) {
			return c.GetProgressSummaryBetweenDates(ctx, "2024-01-08", "2024-01-15", "duration")
		}, "GET /fitnessstats-service/activity?aggregation=lifetime&endDate=2024-01-15&groupByParentActivityType=True&metric=duration&startDate=2024-01-08"},
		{"hill day", func(c *Client) (any, error) { return c.GetHillScore(ctx, "2024-01-08", "") },
			"GET /metrics-service/metrics/hillscore?calendarDate=2024-01-08"},
		{"hill range", func(c *Client) (any, error) { return c.GetHillScore(ctx, "2024-01-08", "2024-01-15") },
			"GET /metrics-service/metrics/hillscore/stats?aggregation=daily&endDate=2024-01-15&startDate=2024-01-08"},
		{"endurance day", func(c *Client) (any, error) { return c.GetEnduranceScore(ctx, "2024-01-08", "") },
			"GET /metrics-service/metrics/endurancescore?calendarDate=2024-01-08"},
		{"endurance range", func(c *Client) (any, error) { return c.GetEnduranceScore(ctx, "2024-01-08", "2024-01-15") },
			"GET /metrics-service/metrics/endurancescore/stats?aggregation=weekly&endDate=2024-01-15&startDate=2024-01-08"},
		{"tolerance", func(c *Client) (any, error) { return c.GetRunningTolerance(ctx, "2024-01-08", "2024-01-15", "weekly") },
			"GET /metrics-service/metrics/runningtolerance/stats?aggregation=weekly&endDate=2024-01-15&startDate=2024-01-08"},
		{"hrv", func(c *Client) (any, error) { return c.GetHRVData(ctx, "2024-01-15") }, "GET /hrv-service/hrv/2024-01-15"},
		{"fitness age", func(c *Client) (any, error) { return c.GetFitnessAgeData(ctx, "2024-01-15") }, "GET /fitnessage-service/fitnessage/2024-01-15"},
		{"max metrics", func(c *Client) (any, error) { return c.GetMaxMetrics(ctx, "2024-01-15") }, "GET /metrics-service/metrics/maxmet/daily/2024-01-15/2024-01-15"},
		{"ftp", func(c *Client) (any, error) { return c.GetCyclingFTP(ctx) }, "GET /biometric-service/biometric/latestFunctionalThresholdPower/CYCLING"},
		{"devices", func(c *Client) (any, error) { return c.GetDevices(ctx) }, "GET /device-service/deviceregistration/devices"},
		{"reload", func(c *Client) (any, error) { return c.RequestReload(ctx, "2024-01-15") }, "POST /wellness-service/wellness/epoch/request/2024-01-15"},
		{"activity search", func(c *Client) (any, error) { return c.ListActivitiesRaw(ctx, map[string][]string{"start": {"0"}}) },
			"GET /activitylist-service/activities/search/activities?start=0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, seen := serveTraining(t, `{}`)
			if _, err := tc.call(c); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(*seen, []string{tc.want}) {
				t.Errorf("requests = %v, want %v", *seen, tc.want)
			}
		})
	}

	// Malformed dates and aggregation never reach Garmin.
	c, seen := serveTraining(t, `{}`)
	for name, call := range map[string]func() error{
		"progress start": func() error { _, err := c.GetProgressSummaryBetweenDates(ctx, "nope", "2024-01-15", "x"); return err },
		"hill":           func() error { _, err := c.GetHillScore(ctx, "2024-02-30", ""); return err },
		"endurance end":  func() error { _, err := c.GetEnduranceScore(ctx, "2024-01-01", "2024-1-5"); return err },
		"tolerance agg":  func() error { _, err := c.GetRunningTolerance(ctx, "2024-01-01", "2024-01-02", "monthly"); return err },
		"reload":         func() error { _, err := c.RequestReload(ctx, "../x"); return err },
		"lactate":        func() error { _, err := c.GetLactateThreshold(ctx, false, "2024-01-01", "x"); return err },
		"lactate start":  func() error { _, err := c.GetLactateThreshold(ctx, false, "", ""); return err },
	} {
		if call() == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if len(*seen) != 0 {
		t.Errorf("requests = %v", *seen)
	}
}

func TestGetLactateThresholdRangeRequests(t *testing.T) {
	c, seen := serveTraining(t, `[]`)
	if _, err := c.GetLactateThreshold(context.Background(), false, "2024-01-08", "2024-01-15"); err != nil {
		t.Fatal(err)
	}
	q := "?aggregation=daily&aggregationStrategy=LATEST&sport=RUNNING"
	var want []string
	for _, m := range []string{"lactateThresholdSpeed", "lactateThresholdHeartRate", "functionalThresholdPower"} {
		want = append(want, "GET /biometric-service/stats/"+m+"/range/2024-01-08/2024-01-15"+q)
	}
	if !reflect.DeepEqual(*seen, want) {
		t.Errorf("requests = %v", *seen)
	}
}

// The latest query combines the endpoint's list of near-identical records; Garmin's "hearRate" typo is a fallback.
func TestGetLactateThresholdLatestCombinesRecords(t *testing.T) {
	c, _ := fixture(t, freshTokens("c1"), func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "powerToWeight/latest/"):
			w.Write([]byte(`{"functionalThresholdPower": 300}`))
		default:
			w.Write([]byte(`[{"userProfilePK":1,"version":2,"calendarDate":"d","sequence":3,"speed":0.4},
				{"hearRate":150},{"heartRate":160,"heartRateCycling":140},{"speed":null}]`))
		}
	}, nil)
	got, err := c.GetLactateThreshold(context.Background(), true, "", "")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	want := `{"power":{"functionalThresholdPower":300},"speed_and_heart_rate":{"calendarDate":"d","heartRate":160,"heartRateCycling":140,"sequence":3,"speed":0.4,"userProfilePK":1,"version":2}}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
}

func TestGetTrainingStatusKeepsDeviceOrder(t *testing.T) {
	c, _ := serveTraining(t, `{"mostRecentTrainingStatus":{"latestTrainingStatusData":{"b":{"x":1},"a":null,"c":{}}},
		"mostRecentTrainingLoadBalance":{"metricsTrainingLoadBalanceDTOMap":{"z":{},"y":{}}}}`)
	ts, err := c.GetTrainingStatus(context.Background(), "2024-01-15")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ts.StatusKeys, []string{"b", "a", "c"}) || !reflect.DeepEqual(ts.LoadKeys, []string{"z", "y"}) || ts.Data == nil {
		t.Errorf("%+v", ts)
	}
	for _, body := range []string{`null`, `[1]`, `{"mostRecentTrainingStatus":null}`, `{"mostRecentTrainingStatus":{"latestTrainingStatusData":[1]}}`} {
		c, _ := serveTraining(t, body)
		ts, err := c.GetTrainingStatus(context.Background(), "2024-01-15")
		if err != nil || len(ts.StatusKeys) != 0 || len(ts.LoadKeys) != 0 {
			t.Errorf("%s: %+v %v", body, ts, err)
		}
	}
}
