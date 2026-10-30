package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/elgatito/garmin-connect-go-mcp/internal/accounts"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAccountValidationAndUnknown(t *testing.T) {
	ctx := context.Background()
	srv, err := New(Options{Store: accounts.Store{Base: t.TempDir()}, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	for _, tc := range []struct{ email, want string }{{"../x@y", `invalid email "../x@y"`}, {"no@x", "No Garmin account \"no@x\" is set up."}} {
		r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_full_name", Arguments: map[string]any{"email": tc.email}})
		if err != nil {
			t.Fatal(err)
		}
		if !r.IsError || len(r.Content) != 1 || !strings.Contains(r.Content[0].(*mcp.TextContent).Text, tc.want) {
			t.Fatalf("%+v", r)
		}
	}
}

func TestProfilesFiltersAndResources(t *testing.T) {
	ctx := context.Background()
	env := map[string]string{"GARMIN_TOOL_PROFILE": "health", "GARMIN_DISABLED_TOOLS": "get_floors", "GARMIN_ENABLED_TOOLS": "get_full_name"}
	srv, err := New(Options{Store: accounts.Store{Base: t.TempDir()}, Version: "test", Getenv: func(k string) string { return env[k] }})
	if err != nil {
		t.Fatal(err)
	}
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, tool := range tools.Tools {
		found[tool.Name] = true
	}
	if found["get_floors"] || !found["get_stats"] || !found["list_accounts"] {
		t.Fatal(found)
	}
	resources, err := cs.ListResources(ctx, nil)
	if err != nil || len(resources.Resources) != 5 {
		t.Fatal(resources, err)
	}
	resource, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "workout://templates/simple-run"})
	if err != nil || len(resource.Contents) != 1 || !strings.Contains(resource.Contents[0].Text, "Simple Run") {
		t.Fatal(resource, err)
	}
	env["GARMIN_TOOL_PROFILE"] = "does-not-exist"
	if _, err := New(Options{Getenv: func(k string) string { return env[k] }}); err == nil {
		t.Fatal("unknown profile accepted")
	}
}

func TestStatsUsesAccountAndCurates(t *testing.T) {
	ctx := context.Background()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer account-token" {
			t.Error("wrong account token")
		}
		switch r.URL.Path {
		case "/userprofile-service/socialProfile":
			fmt.Fprint(w, `{"displayName":"test"}`)
		case "/userprofile-service/userprofile/user-settings":
			fmt.Fprint(w, `{}`)
		case "/usersummary-service/usersummary/daily/test":
			if r.URL.Query().Get("calendarDate") != "2026-10-20" {
				t.Error("wrong date")
			}
			fmt.Fprint(w, `{"calendarDate":"2026-10-20","totalSteps":1234,"ignored":"value"}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer fake.Close()
	store := accounts.Store{Base: t.TempDir()}
	if err := store.SaveTokens("me@x", garmin.Tokens{DIToken: "account-token"}); err != nil {
		t.Fatal(err)
	}
	srv, err := New(Options{Store: store, Version: "test", Endpoints: func(string) garmin.Endpoints { return garmin.Endpoints{ConnectAPI: fake.URL} }})
	if err != nil {
		t.Fatal(err)
	}
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_stats", Arguments: map[string]any{"email": "ME@X", "date": "2026-10-20"}})
	if err != nil || res.IsError {
		t.Fatal(res, err)
	}
	var data map[string]any
	if err = json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &data); err != nil || data["total_steps"] != float64(1234) || data["ignored"] != nil {
		t.Fatal(data, err)
	}
}

func TestDailyStepsChunksDateRange(t *testing.T) {
	ctx := context.Background()
	var paths []string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		fmt.Fprint(w, `[{"totalSteps":1}]`)
	}))
	defer fake.Close()
	store := accounts.Store{Base: t.TempDir()}
	if err := store.SaveTokens("me@x", garmin.Tokens{DIToken: "token"}); err != nil {
		t.Fatal(err)
	}
	srv, _ := New(Options{Store: store, Version: "test", Endpoints: func(string) garmin.Endpoints { return garmin.Endpoints{ConnectAPI: fake.URL} }})
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_daily_steps", Arguments: map[string]any{"email": "me@x", "start_date": "2026-01-01", "end_date": "2026-02-01"}})
	if err != nil || res.IsError {
		t.Fatal(res, err)
	}
	if len(paths) != 2 || paths[0] != "/usersummary-service/stats/steps/daily/2026-01-01/2026-01-28" || paths[1] != "/usersummary-service/stats/steps/daily/2026-01-29/2026-02-01" {
		t.Fatal(paths)
	}
}

func TestWeeklyIntensityCuration(t *testing.T) {
	ctx := context.Background()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/usersummary-service/stats/im/weekly/2026-01-05/2026-02-01" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		fmt.Fprint(w, `[{"calendarDate":"2026-01-26","moderateValue":30,"vigorousValue":20,"weeklyGoal":150}]`)
	}))
	defer fake.Close()
	store := accounts.Store{Base: t.TempDir()}
	if err := store.SaveTokens("me@x", garmin.Tokens{DIToken: "token"}); err != nil {
		t.Fatal(err)
	}
	srv, _ := New(Options{Store: store, Version: "test", Endpoints: func(string) garmin.Endpoints { return garmin.Endpoints{ConnectAPI: fake.URL} }})
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_weekly_intensity_minutes", Arguments: map[string]any{"email": "me@x", "end_date": "2026-02-01"}})
	if err != nil || res.IsError {
		t.Fatal(res, err)
	}
	var data struct {
		WeeklyData []struct {
			Total float64 `json:"total_minutes"`
		} `json:"weekly_data"`
	}
	if err = json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &data); err != nil || len(data.WeeklyData) != 1 || data.WeeklyData[0].Total != 70 {
		t.Fatal(data, err)
	}
}

func TestActivityTypedSplits(t *testing.T) {
	ctx := context.Background()
	hits := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path != "/activity-service/activity/123/typedsplits" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"splits":[1]}`)
	}))
	defer fake.Close()
	store := accounts.Store{Base: t.TempDir()}
	if err := store.SaveTokens("me@x", garmin.Tokens{DIToken: "token"}); err != nil {
		t.Fatal(err)
	}
	srv, _ := New(Options{Store: store, Version: "test", Endpoints: func(string) garmin.Endpoints { return garmin.Endpoints{ConnectAPI: fake.URL} }})
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	for _, tc := range []struct {
		id  string
		bad bool
	}{{"123", false}, {"../bad", true}} {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_activity_typed_splits", Arguments: map[string]any{"email": "me@x", "activity_id": tc.id}})
		if err != nil || res.IsError != tc.bad {
			t.Fatal(res, err)
		}
	}
	if hits != 1 {
		t.Fatal(hits)
	}
}

func TestBodyCompositionDateRange(t *testing.T) {
	ctx := context.Background()
	hits := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path != "/weight-service/weight/dateRange" || r.URL.Query().Get("startDate") != "2026-01-01" || r.URL.Query().Get("endDate") != "2026-01-01" {
			t.Errorf("unexpected request %s", r.URL.String())
		}
		fmt.Fprint(w, `{"totalAverage":{"weight":70000}}`)
	}))
	defer fake.Close()
	store := accounts.Store{Base: t.TempDir()}
	if err := store.SaveTokens("me@x", garmin.Tokens{DIToken: "token"}); err != nil {
		t.Fatal(err)
	}
	srv, _ := New(Options{Store: store, Version: "test", Endpoints: func(string) garmin.Endpoints { return garmin.Endpoints{ConnectAPI: fake.URL} }})
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_body_composition", Arguments: map[string]any{"email": "me@x", "start_date": "2026-01-01"}})
	if err != nil || r.IsError || hits != 1 {
		t.Fatal(r, err, hits)
	}
}

func TestHRVDataCuration(t *testing.T) {
	ctx := context.Background()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hrv-service/hrv/2026-01-01" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"hrvSummary":{"lastNightAvg":42,"baseline":{"balancedLow":35}},"hrvReadings":[{"readingTimeLocal":"01:00","hrvValue":44}]}`)
	}))
	defer fake.Close()
	store := accounts.Store{Base: t.TempDir()}
	if err := store.SaveTokens("me@x", garmin.Tokens{DIToken: "token"}); err != nil {
		t.Fatal(err)
	}
	srv, _ := New(Options{Store: store, Version: "test", Endpoints: func(string) garmin.Endpoints { return garmin.Endpoints{ConnectAPI: fake.URL} }})
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_hrv_data", Arguments: map[string]any{"email": "me@x", "date": "2026-01-01", "return_timeseries": true}})
	if err != nil || r.IsError {
		t.Fatal(r, err)
	}
	var data map[string]any
	if err = json.Unmarshal([]byte(r.Content[0].(*mcp.TextContent).Text), &data); err != nil || data["last_night_avg_hrv_ms"] != float64(42) || data["readings_count"] != float64(1) {
		t.Fatal(data, err)
	}
}

func TestDeviceSettingsFallback(t *testing.T) {
	ctx := context.Background()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/device-service/deviceservice/mylastused":
			fmt.Fprint(w, `{"userDeviceId":123}`)
		case "/device-service/deviceservice/device-info/settings/123":
			fmt.Fprint(w, `{"deviceId":123,"alarms":[{"alarmMode":"ON"},{"alarmMode":"OFF"}],"keyTonesEnabled":false}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer fake.Close()
	store := accounts.Store{Base: t.TempDir()}
	if err := store.SaveTokens("me@x", garmin.Tokens{DIToken: "token"}); err != nil {
		t.Fatal(err)
	}
	srv, _ := New(Options{Store: store, Version: "test", Endpoints: func(string) garmin.Endpoints { return garmin.Endpoints{ConnectAPI: fake.URL} }})
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_device_settings", Arguments: map[string]any{"email": "me@x"}})
	if err != nil || r.IsError {
		t.Fatal(r, err)
	}
	var data map[string]any
	if err = json.Unmarshal([]byte(r.Content[0].(*mcp.TextContent).Text), &data); err != nil || data["enabled_alarm_count"] != float64(1) || data["key_tones_enabled"] != false {
		t.Fatal(data, err)
	}
}

func TestFitDirectoryIsPerAccount(t *testing.T) {
	ctx := context.Background()
	store := accounts.Store{Base: t.TempDir()}
	if err := store.SaveTokens("me@x", garmin.Tokens{DIToken: "token"}); err != nil {
		t.Fatal(err)
	}
	srv, _ := New(Options{Store: store, Version: "test"})
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	dir := filepath.Join(t.TempDir(), "fits")
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "set_fit_download_dir", Arguments: map[string]any{"email": "me@x", "path": dir}})
	if err != nil || r.IsError {
		t.Fatal(r, err)
	}
	settings, err := store.LoadSettings("me@x")
	if err != nil || settings.FitDownloadDir != dir || settings.Domain != "garmin.com" {
		t.Fatal(settings, err)
	}
}

func TestCourseDetails(t *testing.T) {
	ctx := context.Background()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/course-service/course/5" {
			t.Errorf("unexpected %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"courseId":5,"distanceMeter":123,"coursePoints":[{"name":"Water","lat":52}]}`)
	}))
	defer fake.Close()
	store := accounts.Store{Base: t.TempDir()}
	if err := store.SaveTokens("me@x", garmin.Tokens{DIToken: "token"}); err != nil {
		t.Fatal(err)
	}
	srv, _ := New(Options{Store: store, Version: "test", Endpoints: func(string) garmin.Endpoints {
		return garmin.Endpoints{Connect: "https://connect.garmin.cn", ConnectAPI: fake.URL}
	}})
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_course_details", Arguments: map[string]any{"email": "me@x", "course_id": 5}})
	if err != nil || r.IsError {
		t.Fatal(r, err)
	}
	var data map[string]any
	if err = json.Unmarshal([]byte(r.Content[0].(*mcp.TextContent).Text), &data); err != nil || data["distance_m"] != float64(123) || data["waypoints_count"] != float64(1) || data["url"] != "https://connect.garmin.cn/modern/course/5" {
		t.Fatal(data, err)
	}
}

func TestNutritionRangeLimit(t *testing.T) {
	ctx := context.Background()
	var calls atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprint(w, `{}`) }))
	defer fake.Close()
	store := accounts.Store{Base: t.TempDir()}
	if err := store.SaveTokens("me@x", garmin.Tokens{DIToken: "token"}); err != nil {
		t.Fatal(err)
	}
	srv, _ := New(Options{Store: store, Version: "test", Endpoints: func(string) garmin.Endpoints { return garmin.Endpoints{ConnectAPI: fake.URL} }})
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_nutrition_summary_between_dates", Arguments: map[string]any{"email": "me@x", "start_date": "2026-01-01", "end_date": "2026-04-01"}})
	if err != nil || r.IsError || !strings.Contains(r.Content[0].(*mcp.TextContent).Text, "Maximum is 61 days") || calls.Load() != 0 {
		t.Fatal(r, err, calls.Load())
	}
}

func TestFoodSearchCuration(t *testing.T) {
	ctx := context.Background()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("searchExpression") != "oat meal" {
			t.Error("incorrect search query")
		}
		fmt.Fprint(w, `{"moreDataAvailable":true,"results":[{"foodMetaData":{"foodId":12,"foodName":"Oats","source":"GARMIN"},"nutritionContents":[{"servingId":7,"calories":100}]}]}`)
	}))
	defer fake.Close()
	store := accounts.Store{Base: t.TempDir()}
	if err := store.SaveTokens("me@x", garmin.Tokens{DIToken: "token"}); err != nil {
		t.Fatal(err)
	}
	srv, _ := New(Options{Store: store, Version: "test", Endpoints: func(string) garmin.Endpoints { return garmin.Endpoints{ConnectAPI: fake.URL} }})
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "search_foods", Arguments: map[string]any{"email": "me@x", "query": "oat meal"}})
	if err != nil || r.IsError {
		t.Fatal(r, err)
	}
	var data map[string]any
	if err = json.Unmarshal([]byte(r.Content[0].(*mcp.TextContent).Text), &data); err != nil || data["count"] != float64(1) || data["has_more"] != true {
		t.Fatal(data, err)
	}
}
func TestHRVTrendAverage(t *testing.T) {
	ctx := context.Background()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "01") {
			fmt.Fprint(w, `{"hrvSummary":{"lastNightAvg":40}}`)
		} else {
			fmt.Fprint(w, `{"hrvSummary":{"lastNightAvg":50}}`)
		}
	}))
	defer fake.Close()
	store := accounts.Store{Base: t.TempDir()}
	if err := store.SaveTokens("me@x", garmin.Tokens{DIToken: "token"}); err != nil {
		t.Fatal(err)
	}
	srv, _ := New(Options{Store: store, Version: "test", Endpoints: func(string) garmin.Endpoints { return garmin.Endpoints{ConnectAPI: fake.URL} }})
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_hrv_trend", Arguments: map[string]any{"email": "me@x", "start_date": "2026-01-01", "end_date": "2026-01-02"}})
	if err != nil || r.IsError {
		t.Fatal(r, err)
	}
	var data map[string]any
	if err = json.Unmarshal([]byte(r.Content[0].(*mcp.TextContent).Text), &data); err != nil || data["period_avg_hrv_ms"] != float64(45) {
		t.Fatal(data, err)
	}
}
