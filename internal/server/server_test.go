package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const emailDescription = "Email of the Garmin Connect account to use. Call list_accounts to see configured accounts."

func TestEveryToolRequiresEmail(t *testing.T) {
	cs := newSession(t, newFakeGarmin(t), nil)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range res.Tools {
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("%s: input schema is %T", tool.Name, tool.InputSchema)
		}
		required, _ := schema["required"].([]any)
		hasRequired := false
		for _, r := range required {
			if r == "email" {
				hasRequired = true
			}
		}
		if tool.Name == "list_accounts" {
			if hasRequired {
				t.Errorf("list_accounts must not require email")
			}
			continue
		}
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s: no properties", tool.Name)
		}
		email, ok := props["email"].(map[string]any)
		if !ok {
			t.Fatalf("%s: no email property", tool.Name)
		}
		if email["description"] != emailDescription {
			t.Errorf("%s: email description = %q", tool.Name, email["description"])
		}
		if !hasRequired {
			t.Errorf("%s: email is not required", tool.Name)
		}
	}
}

func TestEveryToolAnnotated(t *testing.T) {
	cs := newSession(t, newFakeGarmin(t), nil)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Annotations == nil {
			t.Errorf("%s has no annotations", tool.Name)
		}
	}
}

func TestUnknownAccountMessage(t *testing.T) {
	cs := newSession(t, newFakeGarmin(t), nil)
	text, isErr := callTool(t, cs, "get_full_name", map[string]any{"email": "nobody@x.com"})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	want := `No Garmin account "nobody@x.com" is set up. Ask the user to run "garmin-mcp auth --email nobody@x.com" in a terminal. Configured accounts: me@x.com.`
	if text != want {
		t.Fatalf("message = %q, want %q", text, want)
	}
}

func TestToolRejectsBadEmail(t *testing.T) {
	f := newFakeGarmin(t)
	st := newTestStore(t)
	seedAccount(t, st, "me@x.com", testJWT(nil))
	cs := newSessionWithStore(t, f, nil, st)

	text, isErr := callTool(t, cs, "get_full_name", map[string]any{"email": "../../etc@x"})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	if text != `invalid email "../../etc@x"` {
		t.Fatalf("message = %q", text)
	}
	if calls := f.recorded(); len(calls) != 0 {
		t.Fatalf("fake saw %d calls, want 0", len(calls))
	}
	emails, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(emails) != 1 || emails[0] != "me@x.com" {
		t.Fatalf("accounts changed: %v", emails)
	}
	if _, err := os.Stat(filepath.Join(st.Base, "..", "etc@x")); !os.IsNotExist(err) {
		t.Fatalf("unexpected file outside base: %v", err)
	}
}

func TestEmailCaseInsensitive(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	text, isErr := callTool(t, cs, "get_full_name", map[string]any{"email": "ME@X.COM"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["full_name"] != "Test User" {
		t.Fatalf("full_name = %v", got["full_name"])
	}
}

func TestCorruptTokensMessage(t *testing.T) {
	st := newTestStore(t)
	seedAccount(t, st, "me@x.com", testJWT(nil))
	dir, err := st.Dir("me@x.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tokens.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	cs := newSessionWithStore(t, newFakeGarmin(t), nil, st)

	text, isErr := callTool(t, cs, "get_full_name", nil)
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	want := `The Garmin session for "me@x.com" has expired. Ask the user to run "garmin-mcp auth --email me@x.com --force" in a terminal.`
	if text != want {
		t.Fatalf("message = %q, want %q", text, want)
	}
}

func TestExpiredRefreshMessage(t *testing.T) {
	st := newTestStore(t)
	seedAccount(t, st, "me@x.com", expiringJWT())
	f := newFakeGarmin(t)
	f.on("POST /diauth", respStatus{code: 400, body: map[string]any{"error": "invalid_grant"}})
	cs := newSessionWithStore(t, f, nil, st)

	text, isErr := callTool(t, cs, "get_full_name", nil)
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	want := `The Garmin session for "me@x.com" has expired. Ask the user to run "garmin-mcp auth --email me@x.com --force" in a terminal.`
	if text != want {
		t.Fatalf("message = %q, want %q", text, want)
	}
}

func TestTwoAccountsIsolated(t *testing.T) {
	st := newTestStore(t)
	tokenA := testJWT(map[string]any{"sub": "a"})
	tokenB := testJWT(map[string]any{"sub": "b"})
	seedAccount(t, st, "a@x.com", tokenA)
	seedAccount(t, st, "b@x.com", tokenB)
	f := newFakeGarmin(t)
	cs := newSessionWithStore(t, f, nil, st)

	for _, email := range []string{"a@x.com", "b@x.com"} {
		text, isErr := callTool(t, cs, "get_full_name", map[string]any{"email": email})
		if isErr {
			t.Fatalf("%s: %q", email, text)
		}
	}

	var auths []string
	for _, call := range f.recorded() {
		if call.Path == "/userprofile-service/socialProfile" {
			auths = append(auths, call.Header.Get("Authorization"))
		}
	}
	if len(auths) != 2 {
		t.Fatalf("socialProfile calls = %d, want 2", len(auths))
	}
	if auths[0] != "Bearer "+tokenA || auths[1] != "Bearer "+tokenB {
		t.Fatalf("bearer tokens not isolated: %v", auths)
	}
}

func TestListAccounts(t *testing.T) {
	cs := newSession(t, newFakeGarmin(t), nil)
	text, isErr := callTool(t, cs, "list_accounts", nil)
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got struct {
		Accounts []struct {
			Email       string `json:"email"`
			Domain      string `json:"domain"`
			DisplayName string `json:"display_name"`
			TokenStatus string `json:"token_status"`
		} `json:"accounts"`
	}
	decodeJSON(t, text, &got)
	if len(got.Accounts) != 1 {
		t.Fatalf("accounts = %v", got.Accounts)
	}
	a := got.Accounts[0]
	if a.Email != "me@x.com" || a.Domain != "garmin.com" || a.DisplayName != "" || a.TokenStatus != "ok" {
		t.Fatalf("account = %+v", a)
	}
}

func TestProfileFiltering(t *testing.T) {
	core := []string{"get_full_name", "get_heart_rate_zones", "get_unit_system", "get_user_profile", "get_userprofile_settings"}
	health := []string{
		"get_all_day_events", "get_all_day_stress", "get_blood_pressure",
		"get_body_battery", "get_body_battery_events", "get_body_composition",
		"get_daily_steps", "get_energy_balance", "get_floors",
		"get_heart_rates", "get_heart_rates_summary", "get_hydration_data",
		"get_lifestyle_logging_data", "get_morning_training_readiness",
		"get_recovery_time_remaining", "get_respiration_data",
		"get_respiration_summary", "get_rhr_day", "get_sleep_data",
		"get_sleep_summary", "get_sleep_summary_range", "get_spo2_data",
		"get_stats", "get_stats_and_body", "get_stats_range", "get_steps_data",
		"get_stress_data", "get_stress_summary", "get_training_readiness",
		"get_user_summary", "get_weekly_intensity_minutes", "get_weekly_steps",
		"get_weekly_stress",
	}
	minimalHealth := []string{
		"get_body_battery", "get_daily_steps", "get_heart_rates_summary",
		"get_hrv_data", "get_sleep_data", "get_sleep_summary", "get_stats",
		"get_stress_summary", "get_training_readiness", "get_training_status",
		"get_user_summary",
	}
	minimalActivities := []string{
		"get_activities", "get_activities_by_date", "get_activity", "get_activity_splits",
	}
	activities := []string{
		"add_gear_to_activity", "count_activities", "create_manual_activity",
		"delete_activity", "get_activities", "get_activities_by_date",
		"get_activities_fordate", "get_activity", "get_activity_exercise_sets",
		"get_activity_gear", "get_activity_hr_in_timezones",
		"get_activity_power_in_timezones", "get_activity_split_summaries",
		"get_activity_splits", "get_activity_typed_splits", "get_activity_types",
		"get_activity_weather", "get_gear", "remove_gear_from_activity",
		"set_activity_description", "set_activity_event_type",
		"set_activity_exercise_sets", "set_activity_feel", "set_activity_name",
		"set_activity_type", "set_perceived_effort",
	}
	training := []string{
		"get_acclimation", "get_cycling_ftp", "get_endurance_score",
		"get_fitnessage_data", "get_hill_score", "get_hrv_data", "get_hrv_trend",
		"get_lactate_threshold", "get_progress_summary_between_dates",
		"get_respiration_trend", "get_running_tolerance",
		"get_running_tolerance_trend", "get_training_effect",
		"get_training_load_balance", "get_training_load_trend",
		"get_training_status", "get_vo2max_trend", "request_reload",
	}
	workouts := []string{
		"create_run_interval_workout", "create_run_workout", "create_strength_workout",
		"create_walk_run_workout", "create_z2_walk_workout", "delete_workout",
		"delete_workouts", "download_workout", "get_calendar_events",
		"get_garmin_coach_workouts", "get_scheduled_workouts",
		"get_training_plan_workouts", "get_workout_by_id", "get_workouts",
		"reschedule_workout", "schedule_week", "schedule_workout",
		"schedule_workouts", "unschedule_workout", "unschedule_workouts",
		"upload_workout", "upload_workouts",
	}
	all := append(append(append(append(append(append([]string{}, core...), health...), activities...), training...), workouts...), "list_accounts", "set_heart_rate_zones")
	minimal := append(append(append(append([]string{}, core...), minimalHealth...), minimalActivities...), "list_accounts")

	cases := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{
			name: "all profile",
			env:  map[string]string{"GARMIN_TOOL_PROFILE": "all"},
			want: all,
		},
		{
			name: "minimal profile",
			env:  map[string]string{"GARMIN_TOOL_PROFILE": "minimal"},
			want: minimal,
		},
		{
			name: "profile plus allowlist adds",
			env: map[string]string{
				"GARMIN_TOOL_PROFILE":  "minimal",
				"GARMIN_ENABLED_TOOLS": "set_heart_rate_zones",
			},
			want: append(append([]string{}, minimal...), "set_heart_rate_zones"),
		},
		{
			name: "profile plus denylist removes",
			env: map[string]string{
				"GARMIN_TOOL_PROFILE":   "minimal",
				"GARMIN_DISABLED_TOOLS": "get_heart_rate_zones",
			},
			want: []string{
				"get_full_name", "get_unit_system", "get_user_profile", "get_userprofile_settings",
				"get_body_battery", "get_daily_steps", "get_heart_rates_summary", "get_hrv_data",
				"get_sleep_data", "get_sleep_summary", "get_stats", "get_stress_summary",
				"get_training_readiness", "get_training_status", "get_user_summary",
				"get_activities", "get_activities_by_date", "get_activity",
				"get_activity_splits", "list_accounts",
			},
		},
		{
			name: "profile applies both lists",
			env: map[string]string{
				"GARMIN_TOOL_PROFILE":   "minimal",
				"GARMIN_ENABLED_TOOLS":  "set_heart_rate_zones",
				"GARMIN_DISABLED_TOOLS": "get_full_name,get_heart_rate_zones",
			},
			want: []string{
				"get_unit_system", "get_user_profile", "get_userprofile_settings",
				"get_body_battery", "get_daily_steps", "get_heart_rates_summary", "get_hrv_data",
				"get_sleep_data", "get_sleep_summary", "get_stats", "get_stress_summary",
				"get_training_readiness", "get_training_status", "get_user_summary",
				"get_activities", "get_activities_by_date", "get_activity",
				"get_activity_splits", "list_accounts", "set_heart_rate_zones",
			},
		},
		{
			name: "allowlist only registers listed",
			env:  map[string]string{"GARMIN_ENABLED_TOOLS": "get_full_name"},
			want: []string{"get_full_name"},
		},
		{
			name: "blank allowlist keeps denylist behavior",
			env: map[string]string{
				"GARMIN_ENABLED_TOOLS":  "  \t",
				"GARMIN_DISABLED_TOOLS": "get_full_name",
			},
			want: append(append(append(append([]string{}, health...), activities...),
				append(training, workouts...)...),
				"get_heart_rate_zones", "get_unit_system", "get_user_profile", "get_userprofile_settings",
				"list_accounts", "set_heart_rate_zones"),
		},
		{
			name: "allowlist takes precedence over denylist",
			env: map[string]string{
				"GARMIN_ENABLED_TOOLS":  "get_full_name",
				"GARMIN_DISABLED_TOOLS": "get_full_name",
			},
			want: []string{"get_full_name"},
		},
		{
			name: "matching is case insensitive",
			env:  map[string]string{"GARMIN_ENABLED_TOOLS": "GET_FULL_NAME"},
			want: []string{"get_full_name"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertNames(t, callToolNames(t, newSession(t, newFakeGarmin(t), tc.env)), tc.want)
		})
	}
}

func TestResolveToolProfile(t *testing.T) {
	if p, err := resolveToolProfile(""); err != nil || p != nil {
		t.Fatalf("empty = %v, %v", p, err)
	}
	if p, err := resolveToolProfile("   "); err != nil || p != nil {
		t.Fatalf("blank = %v, %v", p, err)
	}
	if p, err := resolveToolProfile("all"); err != nil || p != nil {
		t.Fatalf("all = %v, %v", p, err)
	}
	if p, err := resolveToolProfile("health,all"); err != nil || p != nil {
		t.Fatalf("health,all = %v, %v", p, err)
	}
	if p, err := resolveToolProfile(" Health "); err != nil || !sameSet(p, toolProfiles["health"]) {
		t.Fatalf("Health = %v, %v", p, err)
	}
	combined, err := resolveToolProfile("training, workouts")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"get_training_status", "create_run_workout"} {
		if !combined[name] {
			t.Errorf("%s missing from union", name)
		}
	}
	if combined["log_food"] {
		t.Error("log_food unexpectedly in union")
	}

	_, err = resolveToolProfile("helth")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Unknown GARMIN_TOOL_PROFILE 'helth'") {
		t.Fatalf("message = %q", msg)
	}
	if !strings.Contains(msg, "Known profiles: activities, all, health, minimal, nutrition, training, workouts") {
		t.Fatalf("message = %q", msg)
	}

	if _, err := resolveToolProfile(",, ,"); err == nil || !strings.Contains(err.Error(), "expected at least one profile name") {
		t.Fatalf("separators error = %v", err)
	}
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func TestUnknownProfileFails(t *testing.T) {
	_, err := New(Options{
		Getenv: func(k string) string {
			if k == "GARMIN_TOOL_PROFILE" {
				return "helth"
			}
			return ""
		},
		Log: io.Discard,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "Unknown GARMIN_TOOL_PROFILE 'helth'") {
		t.Fatalf("message = %q", err)
	}
}

func TestCallTimeout(t *testing.T) {
	t.Run("deadline", func(t *testing.T) {
		f := newFakeGarmin(t)
		f.on("GET /userprofile-service/socialProfile", delay{wait: time.Second, body: map[string]any{"fullName": "late"}})
		cs := newSession(t, f, map[string]string{"GARMIN_MCP_CALL_TIMEOUT": "0.2"})

		text, isErr := callTool(t, cs, "get_full_name", nil)
		if !isErr {
			t.Fatalf("expected error, got %q", text)
		}
		if !strings.Contains(text, "Garmin request 'get_full_name' did not return within 0.2s") {
			t.Fatalf("message = %q", text)
		}
		if !strings.Contains(text, "GARMIN_MCP_CALL_TIMEOUT") {
			t.Fatalf("message = %q", text)
		}
	})

	t.Run("zero disables", func(t *testing.T) {
		f := newFakeGarmin(t)
		f.on("GET /userprofile-service/socialProfile", delay{wait: 150 * time.Millisecond, body: map[string]any{"fullName": "slow but fine"}})
		cs := newSession(t, f, map[string]string{"GARMIN_MCP_CALL_TIMEOUT": "0"})

		text, isErr := callTool(t, cs, "get_full_name", nil)
		if isErr {
			t.Fatalf("unexpected error: %q", text)
		}
		var got map[string]any
		decodeJSON(t, text, &got)
		if got["full_name"] != "slow but fine" {
			t.Fatalf("full_name = %v", got["full_name"])
		}
	})
}

func failurePayload(t *testing.T, err error, sent, committed bool, prefix string) map[string]any {
	t.Helper()
	tr := newWriteTracker("log_food", io.Discard)
	if prefix != "" {
		tr = tr.withPrefix(prefix)
	}
	if sent {
		tr.Sent()
	}
	if committed {
		tr.Committed()
	}
	ferr := tr.Fail(err)
	var wf *writeFailure
	if !errors.As(ferr, &wf) {
		t.Fatalf("Fail returned %T, want *writeFailure", ferr)
	}
	return wf.payload
}

func TestWriteFailurePayload(t *testing.T) {
	t.Run("garmin 401 is rejected request", func(t *testing.T) {
		got := failurePayload(t, &garmin.APIError{Status: 401}, true, false, "")
		if got["stage"] != "garmin_request" || got["upstream_status"] != 401 || got["upstream_body"] != nil || got["write_committed"] != false {
			t.Fatalf("payload = %v", got)
		}
		if got["error_type"] != "APIError" {
			t.Fatalf("error_type = %v", got["error_type"])
		}
	})

	t.Run("garmin 400 carries upstream body", func(t *testing.T) {
		got := failurePayload(t, &garmin.APIError{Status: 400, Message: "Required Parameter meal id is null"}, true, false, "")
		if got["upstream_status"] != 400 || got["upstream_body"] != "Required Parameter meal id is null" || got["write_committed"] != false {
			t.Fatalf("payload = %v", got)
		}
	})

	for _, status := range []int{400, 404} {
		t.Run("garmin 4xx after send is rejected", func(t *testing.T) {
			got := failurePayload(t, &garmin.APIError{Status: status, Message: "rejected"}, true, false, "")
			if got["write_committed"] != false {
				t.Fatalf("payload = %v", got)
			}
			if hint, _ := got["hint"].(string); !strings.HasPrefix(hint, "Nothing was written") {
				t.Fatalf("hint = %v", got["hint"])
			}
		})
	}

	for _, status := range []int{500, 502, 504} {
		t.Run("garmin 5xx after send is unknown", func(t *testing.T) {
			got := failurePayload(t, &garmin.APIError{Status: status, Message: "<html>Gateway Time-out</html>"}, true, false, "")
			if got["stage"] != "garmin_request" || got["upstream_status"] != status || got["write_committed"] != "unknown" {
				t.Fatalf("payload = %v", got)
			}
			if hint, _ := got["hint"].(string); !strings.HasPrefix(hint, "The request may have reached Garmin") {
				t.Fatalf("hint = %v", got["hint"])
			}
		})
	}

	t.Run("garmin 5xx before send is not committed", func(t *testing.T) {
		got := failurePayload(t, &garmin.APIError{Status: 504, Message: "Gateway Time-out"}, false, false, "")
		if got["upstream_status"] != 504 || got["write_committed"] != false {
			t.Fatalf("payload = %v", got)
		}
	})

	t.Run("upstream body is trimmed", func(t *testing.T) {
		got := failurePayload(t, &garmin.APIError{Status: 500, Message: strings.Repeat("x", 2000)}, true, false, "")
		body, _ := got["upstream_body"].(string)
		if len(body) != 500 {
			t.Fatalf("body length = %d", len(body))
		}
	})

	t.Run("rate limit is 429", func(t *testing.T) {
		got := failurePayload(t, &garmin.APIError{Status: 429, Message: "slow down"}, true, false, "")
		if got["upstream_status"] != 429 || got["write_committed"] != false {
			t.Fatalf("payload = %v", got)
		}
	})

	t.Run("auth error is auth refresh", func(t *testing.T) {
		got := failurePayload(t, garmin.ErrSessionExpired, true, false, "")
		if got["stage"] != "auth_refresh" || got["write_committed"] != false {
			t.Fatalf("payload = %v", got)
		}
	})

	t.Run("dns failure never committed", func(t *testing.T) {
		got := failurePayload(t, &net.DNSError{Name: "connectapi.garmin.com", Err: "nodename nor servname"}, true, false, "")
		if got["stage"] != "garmin_request" || got["write_committed"] != false || got["error_type"] != "DNSError" {
			t.Fatalf("payload = %v", got)
		}
	})

	t.Run("connect failure never committed", func(t *testing.T) {
		got := failurePayload(t, &net.OpError{Op: "dial", Err: errors.New("connection refused")}, true, false, "")
		if got["write_committed"] != false {
			t.Fatalf("payload = %v", got)
		}
	})

	t.Run("timeout after send is unknown", func(t *testing.T) {
		got := failurePayload(t, context.DeadlineExceeded, true, false, "")
		if got["stage"] != "garmin_request" || got["write_committed"] != "unknown" {
			t.Fatalf("payload = %v", got)
		}
		if hint, _ := got["hint"].(string); !strings.Contains(hint, "duplicate") {
			t.Fatalf("hint = %v", got["hint"])
		}
	})

	t.Run("timeout before send is not committed", func(t *testing.T) {
		got := failurePayload(t, context.DeadlineExceeded, false, false, "")
		if got["write_committed"] != false {
			t.Fatalf("payload = %v", got)
		}
	})

	t.Run("non json 2xx body is response parse committed", func(t *testing.T) {
		got := failurePayload(t, &json.SyntaxError{}, true, false, "")
		if got["stage"] != "response_parse" || got["write_committed"] != true {
			t.Fatalf("payload = %v", got)
		}
	})

	t.Run("failure after commit is committed", func(t *testing.T) {
		got := failurePayload(t, errors.New("not JSON serializable"), true, true, "")
		if got["stage"] != "response_parse" || got["write_committed"] != true {
			t.Fatalf("payload = %v", got)
		}
	})

	t.Run("unexpected error before send", func(t *testing.T) {
		got := failurePayload(t, errors.New("mealId"), false, false, "")
		if got["stage"] != "internal" || got["write_committed"] != false {
			t.Fatalf("payload = %v", got)
		}
	})

	t.Run("message keeps tool prefix", func(t *testing.T) {
		got := failurePayload(t, &garmin.APIError{Status: 401}, true, false, "Error logging food")
		if got["status"] != "error" || got["tool"] != "log_food" {
			t.Fatalf("payload = %v", got)
		}
		if msg, _ := got["message"].(string); !strings.HasPrefix(msg, "Error logging food: API Error 401") {
			t.Fatalf("message = %v", got["message"])
		}
	})

	t.Run("correlation id is logged", func(t *testing.T) {
		var buf bytes.Buffer
		tr := newWriteTracker("log_food", &buf)
		tr.Sent()
		ferr := tr.Fail(context.DeadlineExceeded)
		var wf *writeFailure
		if !errors.As(ferr, &wf) {
			t.Fatalf("Fail returned %T", ferr)
		}
		cid, _ := wf.payload["correlation_id"].(string)
		if len(cid) != 12 {
			t.Fatalf("correlation_id = %q", cid)
		}
		logged := buf.String()
		if !strings.Contains(logged, cid) || !strings.Contains(logged, "log_food failed") || !strings.Contains(logged, "write_committed=unknown") {
			t.Fatalf("log = %q", logged)
		}
	})

	t.Run("correlation ids are unique", func(t *testing.T) {
		a := failurePayload(t, &garmin.APIError{Status: 401}, false, false, "")
		b := failurePayload(t, &garmin.APIError{Status: 401}, false, false, "")
		if a["correlation_id"] == b["correlation_id"] {
			t.Fatal("correlation ids match")
		}
	})
}

func TestWorkoutResources(t *testing.T) {
	cs := newSession(t, newFakeGarmin(t), nil)
	res, err := cs.ListResources(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	uris := map[string]bool{}
	for _, r := range res.Resources {
		uris[r.URI] = true
	}
	for _, want := range []string{
		"workout://templates/simple-run",
		"workout://templates/interval-running",
		"workout://templates/tempo-run",
		"workout://templates/strength-circuit",
		"workout://reference/structure",
	} {
		if !uris[want] {
			t.Errorf("missing resource %s", want)
		}
	}
	if len(res.Resources) != 5 {
		t.Errorf("resources = %d, want 5", len(res.Resources))
	}

	read, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "workout://templates/simple-run"})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(read.Contents) == 0 || read.Contents[0].MIMEType != "application/json" {
		t.Fatalf("contents = %+v", read.Contents)
	}
	var template map[string]any
	decodeJSON(t, read.Contents[0].Text, &template)
	if template["workoutName"] != "Simple Run" {
		t.Fatalf("workoutName = %v", template["workoutName"])
	}
}

func TestMalformedEnabledToolsFails(t *testing.T) {
	_, err := New(Options{
		Getenv: func(k string) string {
			if k == "GARMIN_ENABLED_TOOLS" {
				return ",,  ,"
			}
			return ""
		},
		Log: io.Discard,
	})
	if err == nil || err.Error() != "Invalid GARMIN_ENABLED_TOOLS: expected at least one tool name" {
		t.Fatalf("error = %v", err)
	}
}

func TestUnknownFilterNamesFlagTypos(t *testing.T) {
	r := &registry{
		enabled:     map[string]bool{"get_full_name": true, "get_typo": true},
		disabled:    map[string]bool{},
		seen:        map[string]bool{},
		unannotated: map[string]bool{},
	}
	if !r.register("get_full_name") {
		t.Fatal("get_full_name was filtered out")
	}
	unknown := r.unknownFilterNames()
	if len(unknown) != 1 || unknown[0] != "get_typo" {
		t.Fatalf("unknown = %v", unknown)
	}
}
