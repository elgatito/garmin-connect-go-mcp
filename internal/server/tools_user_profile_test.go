package server

import (
	"strings"
	"testing"
	"time"
)

var heartRateZones = []any{
	map[string]any{
		"trainingMethod":                "LACTATE_THRESHOLD",
		"restingHeartRateUsed":          54,
		"lactateThresholdHeartRateUsed": 186,
		"zone1Floor":                    105,
		"zone2Floor":                    126,
		"zone3Floor":                    147,
		"zone4Floor":                    168,
		"zone5Floor":                    189,
		"maxHeartRateUsed":              210,
		"restingHrAutoUpdateUsed":       false,
		"sport":                         "DEFAULT",
		"changeState":                   "UNCHANGED",
	},
	map[string]any{
		"trainingMethod":                "LACTATE_THRESHOLD",
		"restingHeartRateUsed":          54,
		"lactateThresholdHeartRateUsed": 187,
		"zone1Floor":                    135,
		"zone2Floor":                    136,
		"zone3Floor":                    166,
		"zone4Floor":                    178,
		"zone5Floor":                    198,
		"maxHeartRateUsed":              203,
		"restingHrAutoUpdateUsed":       false,
		"sport":                         "CYCLING",
		"changeState":                   "UNCHANGED",
	},
}

const zonesPath = "/biometric-service/heartRateZones"

func TestGetFullNameTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /userprofile-service/socialProfile", map[string]any{
		"displayName": "testuser",
		"fullName":    "Test User Full Name",
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_full_name", nil)
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	sameJSON(t, got, map[string]any{"full_name": "Test User Full Name"})
	if calls := f.callsMatching("GET", "/userprofile-service/socialProfile"); len(calls) != 1 {
		t.Fatalf("socialProfile calls = %d, want 1", len(calls))
	}
}

func TestGetUnitSystemTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /userprofile-service/userprofile/user-settings", map[string]any{
		"userData": map[string]any{"measurementSystem": "METRIC"},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_unit_system", nil)
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	sameJSON(t, got, map[string]any{"unit_system": "METRIC"})
	if calls := f.callsMatching("GET", "/userprofile-service/userprofile/user-settings"); len(calls) != 1 {
		t.Fatalf("user-settings calls = %d, want 1", len(calls))
	}
}

func TestGetUserProfileTool(t *testing.T) {
	profile := fixture(t, "MOCK_USER_PROFILE")
	f := newFakeGarmin(t)
	f.on("GET /userprofile-service/userprofile/user-settings", profile)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_user_profile", nil)
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, profile)
	if calls := f.callsMatching("GET", "/userprofile-service/userprofile/user-settings"); len(calls) != 1 {
		t.Fatalf("user-settings calls = %d, want 1", len(calls))
	}
}

func TestGetUserprofileSettingsTool(t *testing.T) {
	settings := map[string]any{"emailNotifications": true, "privacySettings": "PUBLIC"}
	f := newFakeGarmin(t)
	f.on("GET /userprofile-service/userprofile/settings", settings)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_userprofile_settings", nil)
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, settings)
	if calls := f.callsMatching("GET", "/userprofile-service/userprofile/settings"); len(calls) != 1 {
		t.Fatalf("settings calls = %d, want 1", len(calls))
	}
}

func TestGetHeartRateZonesForSport(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+zonesPath, heartRateZones)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_heart_rate_zones", map[string]any{"sport": "cycling"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, heartRateZones[1])
	if calls := f.callsMatching("GET", zonesPath); len(calls) != 1 {
		t.Fatalf("zone calls = %d, want 1", len(calls))
	}
}

func TestSetHeartRateZonesCustomCyclingReadModifyWrite(t *testing.T) {
	confirmed := map[string]any{
		"trainingMethod":                "HR_MAX",
		"restingHeartRateUsed":          54,
		"lactateThresholdHeartRateUsed": 188,
		"zone1Floor":                    100,
		"zone2Floor":                    136,
		"zone3Floor":                    150,
		"zone4Floor":                    170,
		"zone5Floor":                    188,
		"maxHeartRateUsed":              204,
		"restingHrAutoUpdateUsed":       false,
		"sport":                         "CYCLING",
		"changeState":                   "UNCHANGED",
	}
	f := newFakeGarmin(t)
	f.on("GET "+zonesPath, sequence{heartRateZones, []any{heartRateZones[0], confirmed}})
	f.on("PUT "+zonesPath, respStatus{code: 204})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_heart_rate_zones", map[string]any{
		"sport":                "cycling",
		"max_hr":               204,
		"resting_hr":           54,
		"lactate_threshold_hr": 188,
		"calculation_method":   "custom_bpm",
		"zone_boundaries":      []any{100, 136, 150, 170, 188},
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, confirmed)

	puts := f.callsMatching("PUT", zonesPath)
	if len(puts) != 1 {
		t.Fatalf("PUT calls = %d, want 1", len(puts))
	}
	var payload []map[string]any
	decodeJSON(t, string(puts[0].Body), &payload)
	want := map[string]any{}
	for k, v := range confirmed {
		want[k] = v
	}
	want["changeState"] = "CHANGED"
	sameJSON(t, payload[0], want)
	if payload[0]["sport"] != "CYCLING" {
		t.Fatalf("sport = %v", payload[0]["sport"])
	}
	if calls := f.callsMatching("GET", zonesPath); len(calls) != 2 {
		t.Fatalf("GET zone calls = %d, want 2", len(calls))
	}
}

func TestSetHeartRateZonesPartialUpdatePreservesOtherFields(t *testing.T) {
	confirmed := map[string]any{}
	for k, v := range heartRateZones[1].(map[string]any) {
		confirmed[k] = v
	}
	confirmed["maxHeartRateUsed"] = 204

	f := newFakeGarmin(t)
	f.on("GET "+zonesPath, sequence{heartRateZones, []any{heartRateZones[0], confirmed}})
	f.on("PUT "+zonesPath, respStatus{code: 204})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_heart_rate_zones", map[string]any{
		"sport":  "cycling",
		"max_hr": 204,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, confirmed)

	puts := f.callsMatching("PUT", zonesPath)
	if len(puts) != 1 {
		t.Fatalf("PUT calls = %d", len(puts))
	}
	var payload []map[string]any
	decodeJSON(t, string(puts[0].Body), &payload)
	written := payload[0]
	original := heartRateZones[1].(map[string]any)
	if written["zone1Floor"] != float64(original["zone1Floor"].(int)) {
		t.Fatalf("zone1Floor = %v", written["zone1Floor"])
	}
	if written["trainingMethod"] != original["trainingMethod"] {
		t.Fatalf("trainingMethod = %v", written["trainingMethod"])
	}
}

func TestSetHeartRateZonesRejectsInvalidBoundaries(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "non-monotonic",
			args: map[string]any{
				"calculation_method": "custom_bpm",
				"zone_boundaries":    []any{100, 150, 140, 170, 188},
			},
			want: "strictly monotonically increasing",
		},
		{
			name: "exceeds max",
			args: map[string]any{
				"max_hr":             180,
				"calculation_method": "custom_bpm",
				"zone_boundaries":    []any{100, 136, 150, 170, 188},
			},
			want: "must not exceed max_hr",
		},
		{
			name: "boundaries need custom method",
			args: map[string]any{
				"zone_boundaries": []any{100, 136, 150, 170, 188},
			},
			want: "require calculation_method='custom_bpm'",
		},
		{
			name: "custom needs boundaries",
			args: map[string]any{
				"calculation_method": "custom_bpm",
			},
			want: "requires all five zone_boundaries",
		},
		{
			name: "max out of range",
			args: map[string]any{
				"max_hr": 301,
			},
			want: "from 1 to 300",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on("GET "+zonesPath, heartRateZones)
			cs := newSession(t, f, nil)

			args := map[string]any{"sport": "cycling"}
			for k, v := range tc.args {
				args[k] = v
			}
			text, _ := callTool(t, cs, "set_heart_rate_zones", args)
			if !strings.Contains(text, tc.want) {
				t.Fatalf("text = %q, want substring %q", text, tc.want)
			}
			if puts := f.callsMatching("PUT", zonesPath); len(puts) != 0 {
				t.Fatalf("PUT calls = %d, want 0", len(puts))
			}
		})
	}
}

func TestSetHeartRateZonesNewSportInheritsDefault(t *testing.T) {
	confirmed := map[string]any{}
	for k, v := range heartRateZones[0].(map[string]any) {
		confirmed[k] = v
	}
	confirmed["sport"] = "RUNNING"
	confirmed["maxHeartRateUsed"] = 205

	f := newFakeGarmin(t)
	f.on("GET "+zonesPath, sequence{[]any{heartRateZones[0]}, []any{heartRateZones[0], confirmed}})
	f.on("PUT "+zonesPath, respStatus{code: 204})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_heart_rate_zones", map[string]any{
		"sport":  "running",
		"max_hr": 205,
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, confirmed)

	puts := f.callsMatching("PUT", zonesPath)
	if len(puts) != 1 {
		t.Fatalf("PUT calls = %d", len(puts))
	}
	var payload []map[string]any
	decodeJSON(t, string(puts[0].Body), &payload)
	if payload[0]["sport"] != "RUNNING" {
		t.Fatalf("sport = %v", payload[0]["sport"])
	}
	if payload[0]["zone1Floor"] != float64(heartRateZones[0].(map[string]any)["zone1Floor"].(int)) {
		t.Fatalf("zone1Floor = %v", payload[0]["zone1Floor"])
	}
}

func TestSetHeartRateZonesRequiresAnUpdate(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_heart_rate_zones", map[string]any{"sport": "cycling"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.HasPrefix(text, "No fields to update") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("GET", zonesPath); len(calls) != 0 {
		t.Fatalf("zone calls = %d, want 0", len(calls))
	}
}

func TestHeartRateZoneUpdateReportsCommittedWhenConfirmationFails(t *testing.T) {
	readbacks := []struct {
		name string
		body any
	}{
		{name: "rate limited", body: respStatus{code: 429, body: map[string]any{"message": "rate limited"}}},
		{name: "unexpected response", body: map[string]any{}},
		{name: "empty list", body: []any{}},
	}
	for _, tc := range readbacks {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on("GET "+zonesPath, sequence{heartRateZones, tc.body})
			f.on("PUT "+zonesPath, respStatus{code: 204})
			cs := newSession(t, f, nil)

			text, isErr := callTool(t, cs, "set_heart_rate_zones", map[string]any{
				"sport":  "cycling",
				"max_hr": 204,
			})
			if isErr {
				t.Fatalf("unexpected error: %q", text)
			}
			var got map[string]any
			decodeJSON(t, text, &got)
			if got["status"] != "success" {
				t.Fatalf("status = %v", got["status"])
			}
			if got["write_committed"] != true {
				t.Fatalf("write_committed = %v", got["write_committed"])
			}
			if s, _ := got["confirmation_error"].(string); s == "" {
				t.Fatalf("confirmation_error = %v", got["confirmation_error"])
			}
			submitted, _ := got["submitted_configuration"].(map[string]any)
			if submitted["sport"] != "CYCLING" || submitted["maxHeartRateUsed"] != float64(204) {
				t.Fatalf("submitted_configuration = %v", submitted)
			}
			if puts := f.callsMatching("PUT", zonesPath); len(puts) != 1 {
				t.Fatalf("PUT calls = %d, want 1", len(puts))
			}
		})
	}
}

func TestHeartRateZoneUpdateTracksFailedWrite(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      map[string]any
		committed any
	}{
		{name: "server error", status: 503, body: map[string]any{"message": "gateway failed"}, committed: "unknown"},
		{name: "rejected", status: 400, body: map[string]any{"message": "invalid profile"}, committed: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on("GET "+zonesPath, heartRateZones)
			f.on("PUT "+zonesPath, respStatus{code: tc.status, body: tc.body})
			cs := newSession(t, f, nil)

			text, isErr := callTool(t, cs, "set_heart_rate_zones", map[string]any{
				"sport":  "cycling",
				"max_hr": 204,
			})
			if !isErr {
				t.Fatalf("expected tool error, got %q", text)
			}
			var got map[string]any
			decodeJSON(t, text, &got)
			if got["write_committed"] != tc.committed {
				t.Fatalf("write_committed = %v", got["write_committed"])
			}
			if got["stage"] != "garmin_request" {
				t.Fatalf("stage = %v", got["stage"])
			}
			if got["error_type"] != "APIError" {
				t.Fatalf("error_type = %v", got["error_type"])
			}
			if msg, _ := got["message"].(string); !strings.Contains(msg, "Error updating heart-rate zones:") {
				t.Fatalf("message = %q", msg)
			}
			if s, _ := got["correlation_id"].(string); s == "" {
				t.Fatalf("correlation_id = %v", got["correlation_id"])
			}
			if calls := f.callsMatching("GET", zonesPath); len(calls) != 1 {
				t.Fatalf("zone calls = %d, want 1", len(calls))
			}
		})
	}
}

func TestHeartRateZoneUpdateLookupFailureReportsNoWrite(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+zonesPath, respStatus{code: 401, body: map[string]any{"message": "expired token"}})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_heart_rate_zones", map[string]any{
		"sport":  "cycling",
		"max_hr": 204,
	})
	if !isErr {
		t.Fatalf("expected tool error, got %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["write_committed"] != false {
		t.Fatalf("write_committed = %v", got["write_committed"])
	}
	if got["stage"] != "auth_refresh" {
		t.Fatalf("stage = %v", got["stage"])
	}
	if puts := f.callsMatching("PUT", zonesPath); len(puts) != 0 {
		t.Fatalf("PUT calls = %d, want 0", len(puts))
	}
}

func TestHeartRateZoneUpdateTimeoutTracksUnknownWrite(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+zonesPath, heartRateZones)
	f.on("PUT "+zonesPath, delay{wait: 400 * time.Millisecond, body: status(204, nil)})
	cs := newSession(t, f, map[string]string{"GARMIN_MCP_CALL_TIMEOUT": "0.2"})

	text, isErr := callTool(t, cs, "set_heart_rate_zones", map[string]any{
		"sport":  "cycling",
		"max_hr": 204,
	})
	if !isErr {
		t.Fatalf("expected tool error, got %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["write_committed"] != "unknown" {
		t.Fatalf("write_committed = %v", got["write_committed"])
	}
	if got["stage"] != "garmin_request" {
		t.Fatalf("stage = %v", got["stage"])
	}
	if hint, _ := got["hint"].(string); !strings.Contains(hint, "duplicate") {
		t.Fatalf("hint = %v", got["hint"])
	}
	if cid, _ := got["correlation_id"].(string); cid == "" {
		t.Fatalf("correlation_id = %v", got["correlation_id"])
	}
	if puts := f.callsMatching("PUT", zonesPath); len(puts) != 1 {
		t.Fatalf("PUT calls = %d, want 1", len(puts))
	}
}
