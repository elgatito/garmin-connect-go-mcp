package server

import (
	"maps"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// Ported from the user_profile tests in tests/integration/test_other_modules_tools.py.

const hrZonesRoute = "/biometric-service/heartRateZones"

func heartRateZones() []any {
	return []any{
		map[string]any{
			"trainingMethod": "LACTATE_THRESHOLD", "restingHeartRateUsed": 54.0, "lactateThresholdHeartRateUsed": 186.0,
			"zone1Floor": 105.0, "zone2Floor": 126.0, "zone3Floor": 147.0, "zone4Floor": 168.0, "zone5Floor": 189.0,
			"maxHeartRateUsed": 210.0, "restingHrAutoUpdateUsed": false, "sport": "DEFAULT", "changeState": "UNCHANGED",
		},
		map[string]any{
			"trainingMethod": "LACTATE_THRESHOLD", "restingHeartRateUsed": 54.0, "lactateThresholdHeartRateUsed": 187.0,
			"zone1Floor": 135.0, "zone2Floor": 136.0, "zone3Floor": 166.0, "zone4Floor": 178.0, "zone5Floor": 198.0,
			"maxHeartRateUsed": 203.0, "restingHrAutoUpdateUsed": false, "sport": "CYCLING", "changeState": "UNCHANGED",
		},
	}
}

func with(m any, kv map[string]any) map[string]any {
	out := maps.Clone(m.(map[string]any))
	maps.Copy(out, kv)
	return out
}

// putBody returns the JSON body of the single heart-rate-zone PUT.
func putBody(t *testing.T, f *fakeGarmin) []any {
	t.Helper()
	puts := f.requests("PUT " + hrZonesRoute)
	if len(puts) != 1 {
		t.Fatalf("%d PUTs, want 1", len(puts))
	}
	return decode(t, string(puts[0].Body)).([]any)
}

func TestGetFullNameTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /userprofile-service/socialProfile", map[string]any{"displayName": "testuser", "fullName": "Test User Full Name"})
	text := callOK(t, newSession(t, f, nil), "get_full_name", nil)
	if got := decode(t, text); !reflect.DeepEqual(got, map[string]any{"full_name": "Test User Full Name"}) {
		t.Fatalf("got %v", got)
	}
	if n := len(f.requests("GET /userprofile-service/socialProfile")); n != 1 {
		t.Errorf("%d profile calls", n)
	}
}

func TestGetUnitSystemTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /userprofile-service/userprofile/user-settings", map[string]any{"userData": map[string]any{"measurementSystem": "statute_us"}})
	text := callOK(t, newSession(t, f, nil), "get_unit_system", nil)
	if got := decode(t, text); !reflect.DeepEqual(got, map[string]any{"unit_system": "statute_us"}) {
		t.Fatalf("got %v", got)
	}
}

func TestGetUserProfileTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /userprofile-service/userprofile/user-settings", fixture(t, "MOCK_USER_PROFILE"))
	text := callOK(t, newSession(t, f, nil), "get_user_profile", nil)
	if got := decode(t, text); !reflect.DeepEqual(got, fixture(t, "MOCK_USER_PROFILE")) {
		t.Fatalf("got %v", got)
	}
	if n := len(f.requests("GET /userprofile-service/userprofile/user-settings")); n != 1 {
		t.Errorf("%d calls", n)
	}
}

func TestGetUserprofileSettingsTool(t *testing.T) {
	f := newFakeGarmin(t)
	settings := map[string]any{"emailNotifications": true, "privacySettings": "PUBLIC"}
	f.on("GET /userprofile-service/userprofile/settings", settings)
	text := callOK(t, newSession(t, f, nil), "get_userprofile_settings", nil)
	if got := decode(t, text); !reflect.DeepEqual(got, settings) {
		t.Fatalf("got %v", got)
	}
}

func TestGetHeartRateZonesForSport(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+hrZonesRoute, heartRateZones())
	text := callOK(t, newSession(t, f, nil), "get_heart_rate_zones", map[string]any{"sport": "cycling"})
	if got := decode(t, text); !reflect.DeepEqual(got, heartRateZones()[1]) {
		t.Fatalf("got %v", got)
	}
	if n := len(f.requests("")); n != 1 {
		t.Errorf("%d calls, want 1", n)
	}
}

func TestSetHeartRateZonesCustomCyclingReadModifyWrite(t *testing.T) {
	zones := heartRateZones()
	confirmed := with(zones[1], map[string]any{
		"trainingMethod": "HR_MAX", "maxHeartRateUsed": 204.0, "lactateThresholdHeartRateUsed": 188.0,
		"zone1Floor": 100.0, "zone2Floor": 136.0, "zone3Floor": 150.0, "zone4Floor": 170.0, "zone5Floor": 188.0,
	})
	f := newFakeGarmin(t)
	f.on("GET "+hrZonesRoute, sequence(zones, []any{zones[0], confirmed}))
	f.on("PUT "+hrZonesRoute, status(204, rawBody(nil)))

	text := callOK(t, newSession(t, f, nil), "set_heart_rate_zones", map[string]any{
		"sport": "cycling", "max_hr": 204, "resting_hr": 54, "lactate_threshold_hr": 188,
		"calculation_method": "custom_bpm", "zone_boundaries": []int{100, 136, 150, 170, 188},
	})
	if got := decode(t, text); !reflect.DeepEqual(got, confirmed) {
		t.Fatalf("got %v", got)
	}
	payload := putBody(t, f)
	if want := []any{with(confirmed, map[string]any{"changeState": "CHANGED"})}; !reflect.DeepEqual(payload, want) {
		t.Fatalf("payload %v\nwant %v", payload, want)
	}
	if payload[0].(map[string]any)["sport"] != "CYCLING" {
		t.Error("sport")
	}
	if n := len(f.requests("GET " + hrZonesRoute)); n != 2 {
		t.Errorf("%d GETs, want 2", n)
	}
}

func TestSetHeartRateZonesPartialUpdatePreservesOtherFields(t *testing.T) {
	zones := heartRateZones()
	confirmed := with(zones[1], map[string]any{"maxHeartRateUsed": 204.0})
	f := newFakeGarmin(t)
	f.on("GET "+hrZonesRoute, sequence(zones, []any{zones[0], confirmed}))
	f.on("PUT "+hrZonesRoute, status(204, rawBody(nil)))

	text := callOK(t, newSession(t, f, nil), "set_heart_rate_zones", map[string]any{"sport": "cycling", "max_hr": 204})
	if got := decode(t, text); !reflect.DeepEqual(got, confirmed) {
		t.Fatalf("got %v", got)
	}
	written := putBody(t, f)[0].(map[string]any)
	cycling := zones[1].(map[string]any)
	if written["zone1Floor"] != cycling["zone1Floor"] || written["trainingMethod"] != cycling["trainingMethod"] {
		t.Errorf("written %v", written)
	}
}

func TestSetHeartRateZonesRejectsInvalidBoundaries(t *testing.T) {
	cases := []struct {
		args  map[string]any
		error string
	}{
		{map[string]any{"calculation_method": "custom_bpm", "zone_boundaries": []int{100, 150, 140, 170, 188}}, "strictly monotonically increasing"},
		{map[string]any{"max_hr": 180, "calculation_method": "custom_bpm", "zone_boundaries": []int{100, 136, 150, 170, 188}}, "must not exceed max_hr"},
		{map[string]any{"zone_boundaries": []int{100, 136, 150, 170, 188}}, "require calculation_method='custom_bpm'"},
		{map[string]any{"calculation_method": "custom_bpm"}, "requires all five zone_boundaries"},
		{map[string]any{"max_hr": 301}, "from 1 to 300"},
	}
	for _, c := range cases {
		t.Run(c.error, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on("GET "+hrZonesRoute, heartRateZones())
			args := map[string]any{"sport": "cycling"}
			maps.Copy(args, c.args)
			text, _ := callTool(t, newSession(t, f, nil), "set_heart_rate_zones", args)
			if !strings.Contains(text, c.error) {
				t.Errorf("got %q", text)
			}
			if n := len(f.requests("PUT " + hrZonesRoute)); n != 0 {
				t.Errorf("%d PUTs", n)
			}
		})
	}
}

func TestSetHeartRateZonesNewSportInheritsDefault(t *testing.T) {
	zones := heartRateZones()
	confirmed := with(zones[0], map[string]any{"sport": "RUNNING", "maxHeartRateUsed": 205.0})
	f := newFakeGarmin(t)
	f.on("GET "+hrZonesRoute, sequence([]any{zones[0]}, []any{zones[0], confirmed}))
	f.on("PUT "+hrZonesRoute, status(204, rawBody(nil)))

	text := callOK(t, newSession(t, f, nil), "set_heart_rate_zones", map[string]any{"sport": "running", "max_hr": 205})
	if got := decode(t, text); !reflect.DeepEqual(got, confirmed) {
		t.Fatalf("got %v", got)
	}
	written := putBody(t, f)[0].(map[string]any)
	if written["sport"] != "RUNNING" || written["zone1Floor"] != zones[0].(map[string]any)["zone1Floor"] {
		t.Errorf("written %v", written)
	}
}

func TestSetHeartRateZonesRequiresAnUpdate(t *testing.T) {
	f := newFakeGarmin(t)
	text, _ := callTool(t, newSession(t, f, nil), "set_heart_rate_zones", map[string]any{"sport": "cycling"})
	if !strings.Contains(text, "No fields to update") {
		t.Errorf("got %q", text)
	}
	if n := len(f.requests("")); n != 0 {
		t.Errorf("%d calls", n)
	}
}

func TestHeartRateZoneUpdateReportsCommittedWhenConfirmationFails(t *testing.T) {
	for name, readback := range map[string]any{
		"rate limited":        status(429, map[string]any{"message": "rate limited"}),
		"unexpected response": map[string]any{"not": "a list"},
		"empty list":          []any{},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on("GET "+hrZonesRoute, sequence(heartRateZones(), readback))
			f.on("PUT "+hrZonesRoute, status(204, rawBody(nil)))
			data := decode(t, callOK(t, newSession(t, f, nil), "set_heart_rate_zones", map[string]any{"sport": "cycling", "max_hr": 204})).(map[string]any)
			sub, _ := data["submitted_configuration"].(map[string]any)
			if data["status"] != "success" || data["write_committed"] != true || data["confirmation_error"] == "" || data["confirmation_error"] == nil ||
				sub["sport"] != "CYCLING" || sub["maxHeartRateUsed"] != 204.0 {
				t.Fatalf("got %v", data)
			}
			if n := len(f.requests("PUT " + hrZonesRoute)); n != 1 {
				t.Errorf("%d PUTs", n)
			}
		})
	}
}

func TestHeartRateZoneUpdateTracksFailedWrite(t *testing.T) {
	cases := []struct {
		name      string
		put       any
		env       map[string]string
		committed any
		errType   string
		inMessage string
	}{
		{"503", status(503, map[string]any{"message": "gateway failed"}), nil, "unknown", "GarminConnectConnectionError", "API Error 503 - gateway failed"},
		{"read timeout", func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }, map[string]string{"GARMIN_MCP_CALL_TIMEOUT": "0.3"}, "unknown", "ReadTimeout", "deadline exceeded"},
		{"400", status(400, map[string]any{"message": "invalid profile"}), nil, false, "GarminConnectConnectionError", "API Error 400 - invalid profile"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on("GET "+hrZonesRoute, heartRateZones())
			f.on("PUT "+hrZonesRoute, c.put)
			text, isErr := callTool(t, newSession(t, f, c.env), "set_heart_rate_zones", map[string]any{"sport": "cycling", "max_hr": 204})
			if !isErr {
				t.Fatalf("not an error: %s", text)
			}
			data := decode(t, text).(map[string]any)
			msg, _ := data["message"].(string)
			if data["write_committed"] != c.committed || data["stage"] != "garmin_request" || data["error_type"] != c.errType ||
				!strings.HasPrefix(msg, "Error updating heart-rate zones: ") || !strings.Contains(msg, c.inMessage) || data["correlation_id"] == nil {
				t.Fatalf("got %v", data)
			}
			if n := len(f.requests("GET " + hrZonesRoute)); n != 1 {
				t.Errorf("%d GETs", n)
			}
		})
	}
}

// Also pins that a writeFailure caused by an expired session keeps its payload instead of becoming the expired-session copy.
func TestHeartRateZoneUpdateLookupFailureReportsNoWrite(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+hrZonesRoute, status(401, map[string]any{"message": "expired token"}))
	f.on("POST /di-oauth2-service/oauth/token", status(400, map[string]any{"error": "invalid_grant"}))
	text, isErr := callTool(t, newSession(t, f, nil), "set_heart_rate_zones", map[string]any{"sport": "cycling", "max_hr": 204})
	if !isErr || strings.HasPrefix(text, "The Garmin session") {
		t.Fatalf("got %v %q", isErr, text)
	}
	data := decode(t, text).(map[string]any)
	if data["write_committed"] != false || data["stage"] != "auth_refresh" {
		t.Fatalf("got %v", data)
	}
	if n := len(f.requests("PUT " + hrZonesRoute)); n != 0 {
		t.Errorf("%d PUTs", n)
	}
}
