package server

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

// Ported from the gear_management cases in tests/integration/test_other_modules_tools.py.

const (
	deviceRoute   = "GET /device-service/deviceservice/mylastused"
	gearRoute     = "GET /gear-service/gear/filterGear"
	gearV2Route   = "GET /gear-service/gear/v2/list"
	gearDefRoute  = "GET /gear-service/gear/user/80653452/activityTypes"
	gearStatsBase = "/gear-service/gear/stats/"
	shoe1         = "8abfc40d71fb4860bce19072b6c79644"
	shoe2         = "6f27ed27397749ac9f6f450e039c2424"
)

// gearFake serves the device, gear list, v2 list and defaults the way the Python mocks do.
func gearFake(t *testing.T, gear, v2, defaults any) *fakeGarmin {
	t.Helper()
	f := newFakeGarmin(t)
	f.on(deviceRoute, fixture(t, "MOCK_DEVICE_LAST_USED"))
	f.on(gearRoute, gear)
	f.on(gearV2Route, v2)
	f.on(gearDefRoute, defaults)
	return f
}

func gearByUUID(t *testing.T, text string) (map[string]any, map[string]map[string]any) {
	t.Helper()
	d := resultMap(t, text)
	out := map[string]map[string]any{}
	for _, g := range asList(d["gear"]) {
		out[asMap(g)["uuid"].(string)] = asMap(g)
	}
	return d, out
}

func TestGetGearTool(t *testing.T) {
	f := gearFake(t, fixture(t, "MOCK_GEAR"), fixture(t, "MOCK_GEAR_V2"), fixture(t, "MOCK_GEAR_DEFAULTS"))
	f.on("GET "+gearStatsBase+shoe1, fixture(t, "MOCK_GEAR_STATS"))
	f.on("GET "+gearStatsBase+shoe2, fixture(t, "MOCK_GEAR_STATS"))
	d, gear := gearByUUID(t, callOK(t, newSession(t, f, nil), "get_gear", nil))
	if gear[shoe1]["notes"] != "Rotation shoe; blue laces." {
		t.Errorf("notes = %v", gear[shoe1]["notes"])
	}
	if n, has := gear[shoe2]["notes"]; n != nil || !has {
		t.Errorf("shoe2 notes = %v (present %v)", n, has)
	}
	if d["gear_count"] != 2.0 || d["active_count"] != 1.0 || d["retired_count"] != 1.0 {
		t.Errorf("counts: %v", d)
	}
	if !reflect.DeepEqual(d["defaults"], map[string]any{"running": []any{"Nimbus 25"}}) {
		t.Errorf("defaults = %v", d["defaults"])
	}
	g := gear[shoe1]
	wantStats := map[string]any{"total_activities": 137.0, "total_distance_km": 881.4}
	if !reflect.DeepEqual(g["stats"], wantStats) || g["max_distance_km"] != 643.7 || g["date_begin"] != "2024-03-15" ||
		g["full_name"] != "Asics Nimbus 25" || g["name"] != "Nimbus 25" || g["type"] != "Shoes" || g["status"] != "active" ||
		!reflect.DeepEqual(g["is_default_for"], []any{"running"}) {
		t.Errorf("shoe1 = %v", g)
	}
	if _, has := gear[shoe2]["is_default_for"]; has || gear[shoe2]["date_end"] != "2024-04-01" {
		t.Errorf("shoe2 = %v", gear[shoe2])
	}
	// active gear sorts first
	if first := asMap(asList(d["gear"])[0]); first["uuid"] != shoe1 {
		t.Errorf("first = %v", first["uuid"])
	}
	// the chain of calls
	if n := len(f.requests(deviceRoute)); n != 1 {
		t.Errorf("%d device calls", n)
	}
	wantQuery(t, query(t, f, gearRoute), map[string]string{"userProfilePk": "80653452"})
	if n := len(f.requests(gearV2Route)); n != 1 {
		t.Errorf("%d v2 calls", n)
	}
	if n := len(f.requests(gearDefRoute)); n != 1 {
		t.Errorf("%d defaults calls", n)
	}
}

func TestGetGearPreservesActivityCountWhenDistanceIsNull(t *testing.T) {
	for _, total := range []float64{0, 3} {
		f := gearFake(t, []any{asList(fixture(t, "MOCK_GEAR"))[0]}, []any{}, []any{})
		f.on("GET "+gearStatsBase+shoe1, map[string]any{"totalActivities": total, "totalDistance": nil})
		_, gear := gearByUUID(t, callOK(t, newSession(t, f, nil), "get_gear", nil))
		if want := map[string]any{"total_activities": total, "total_distance_km": nil}; !reflect.DeepEqual(gear[shoe1]["stats"], want) {
			t.Errorf("total %v: stats = %v", total, gear[shoe1]["stats"])
		}
	}
}

func TestGetGearReportsPartialStatsFailures(t *testing.T) {
	for _, code := range []int{401, 429, 503} {
		t.Run(pyStr(code), func(t *testing.T) {
			f := gearFake(t, asList(fixture(t, "MOCK_GEAR"))[:2], []any{}, []any{})
			f.on("GET "+gearStatsBase+shoe1, map[string]any{"totalActivities": 10, "totalDistance": 1000})
			f.on("GET "+gearStatsBase+shoe2, status(code, map[string]any{"message": "stats failed"}))
			text, isErr := callTool(t, newSession(t, f, nil), "get_gear", nil)
			if code == 401 {
				// a rejected token cannot be refreshed in the fake: the session is reported expired
				if !isErr || !strings.Contains(text, "has expired") {
					t.Errorf("%v %q", isErr, text)
				}
				return
			}
			if isErr {
				t.Fatal(text)
			}
			d, gear := gearByUUID(t, text)
			if d["gear_count"] != 2.0 || at(gear[shoe1], "stats", "total_activities") != 10.0 {
				t.Errorf("got %v", d)
			}
			if want := "API Error " + pyStr(code) + " - stats failed"; gear[shoe2]["stats_error"] != want {
				t.Errorf("stats_error = %v, want %q", gear[shoe2]["stats_error"], want)
			}
		})
	}
}

func TestGetGearStatsNotFoundIsEmpty(t *testing.T) {
	f := gearFake(t, []any{asList(fixture(t, "MOCK_GEAR"))[0]}, []any{}, []any{})
	f.on("GET "+gearStatsBase+shoe1, status(404, map[string]any{"message": "gone"}))
	_, gear := gearByUUID(t, callOK(t, newSession(t, f, nil), "get_gear", nil))
	g := gear[shoe1]
	if _, has := g["stats"]; has {
		t.Errorf("stats = %v", g["stats"])
	}
	if _, has := g["stats_error"]; has {
		t.Errorf("stats_error = %v", g["stats_error"])
	}
}

func TestGetGearToolDefaultActivityTypes(t *testing.T) {
	var defaults []any
	for _, pk := range []int{1, 3, 6, 9, 10, 13, 26, 143, 999} {
		defaults = append(defaults, map[string]any{"uuid": shoe1, "activityTypePk": pk, "defaultGear": true})
	}
	f := gearFake(t, []any{asList(fixture(t, "MOCK_GEAR"))[0]}, []any{}, defaults)
	d := resultMap(t, callOK(t, newSession(t, f, nil), "get_gear", map[string]any{"include_stats": false}))
	want := []any{"running", "hiking", "trail_running", "walking", "road_biking", "strength_training", "swimming", "gravel_cycling", "activity_999"}
	if got := asMap(asList(d["gear"])[0])["is_default_for"]; !reflect.DeepEqual(got, want) {
		t.Errorf("is_default_for = %v", got)
	}
}

func TestGetGearToolDefaultsSummary(t *testing.T) {
	g := asList(fixture(t, "MOCK_GEAR"))
	unnamed := withMap(g[1], map[string]any{"gearStatusName": "active", "displayName": nil})
	f := gearFake(t, []any{g[0], unnamed}, []any{}, []any{
		map[string]any{"uuid": shoe1, "activityTypePk": 1, "defaultGear": true},
		map[string]any{"uuid": shoe1, "activityTypePk": 6, "defaultGear": true},
		map[string]any{"uuid": shoe2, "activityTypePk": 1, "defaultGear": true},
	})
	d := resultMap(t, callOK(t, newSession(t, f, nil), "get_gear", map[string]any{"include_stats": false}))
	want := map[string]any{"running": []any{"Nimbus 25", "ASICS Gel-Nimbus 24"}, "trail_running": []any{"Nimbus 25"}}
	if !reflect.DeepEqual(d["defaults"], want) {
		t.Errorf("defaults = %v", d["defaults"])
	}
}

func withMap(m any, kv map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range asMap(m) {
		out[k] = v
	}
	for k, v := range kv {
		out[k] = v
	}
	return out
}

func TestGetGearToolPreservesEmptyAndNullNotes(t *testing.T) {
	for name, notes := range map[string]any{"empty": "", "null": nil} {
		t.Run(name, func(t *testing.T) {
			v2 := withMap(asList(fixture(t, "MOCK_GEAR_V2"))[0], map[string]any{"notes": notes})
			f := gearFake(t, []any{asList(fixture(t, "MOCK_GEAR"))[0]}, []any{v2}, []any{})
			d := resultMap(t, callOK(t, newSession(t, f, nil), "get_gear", map[string]any{"include_stats": false}))
			g := asMap(asList(d["gear"])[0])
			if got, has := g["notes"]; !has || got != notes {
				t.Errorf("notes = %#v (present %v), want %#v", got, has, notes)
			}
		})
	}
}

func TestGetGearToolWithoutStats(t *testing.T) {
	f := gearFake(t, fixture(t, "MOCK_GEAR"), status(500, map[string]any{"message": "v2 unavailable"}), fixture(t, "MOCK_GEAR_DEFAULTS"))
	var log bytes.Buffer
	srv, err := New(testOptions(f, newStore(t), nil, &log))
	if err != nil {
		t.Fatal(err)
	}
	d := resultMap(t, callOK(t, connect(t, srv), "get_gear", map[string]any{"include_stats": false}))
	if d["gear_count"] != 2.0 {
		t.Errorf("gear_count = %v", d["gear_count"])
	}
	for _, g := range asList(d["gear"]) {
		if n, has := asMap(g)["notes"]; n != nil || !has {
			t.Errorf("notes = %v", n)
		}
		if _, has := asMap(g)["stats"]; has {
			t.Errorf("stats present: %v", g)
		}
	}
	if !strings.Contains(log.String(), "Garmin v2 gear list unavailable: API Error 500 - v2 unavailable") {
		t.Errorf("log = %q", log.String())
	}
	for _, c := range f.requests("") {
		if strings.HasPrefix(c.Path, gearStatsBase) {
			t.Errorf("stats requested: %s", c.Path)
		}
	}
}

func TestGetGearWithoutDeviceOrGear(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(deviceRoute, map[string]any{})
	if text := callOK(t, newSession(t, f, nil), "get_gear", nil); text != "Could not retrieve user profile. Please ensure you have a synced device." {
		t.Error(text)
	}
	f = gearFake(t, []any{}, []any{}, []any{})
	if text := callOK(t, newSession(t, f, nil), "get_gear", nil); text != "No gear found." {
		t.Error(text)
	}
	f = gearFake(t, fixture(t, "MOCK_GEAR"), []any{}, status(500, map[string]any{"message": "boom"}))
	text, isErr := callTool(t, newSession(t, f, nil), "get_gear", nil)
	if !isErr || text != "Error retrieving gear: API Error 500 - boom" {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestAddGearToActivityTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("PUT /gear-service/gear/link/abc123/activity/12345678901", map[string]any{})
	text := callOK(t, newSession(t, f, nil), "add_gear_to_activity", map[string]any{"activity_id": 12345678901, "gear_uuid": "abc123"})
	want := map[string]any{"success": true, "activity_id": 12345678901.0, "gear_uuid": "abc123", "message": "Gear successfully added to activity"}
	if got := decode(t, text); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	if n := len(f.requests("PUT /gear-service/gear/link/abc123/activity/12345678901")); n != 1 {
		t.Errorf("%d PUTs", n)
	}
}

func TestAddGearToActivityMissingGearIsUnavailable(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("PUT /gear-service/gear/link/missing/activity/12345678901", status(404, map[string]any{"message": "Gear with uuid: missing doesn't exist."}))
	text := callOK(t, newSession(t, f, nil), "add_gear_to_activity", map[string]any{"activity_id": 12345678901, "gear_uuid": "missing"})
	if text != "No gear association changed; gear missing was not found for activity 12345678901." {
		t.Error(text)
	}
}

func TestAddGearToActivityOtherErrors(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("PUT /gear-service/gear/link/abc123/activity/12345678901", status(500, map[string]any{"message": "boom"}))
	text, isErr := callTool(t, newSession(t, f, nil), "add_gear_to_activity", map[string]any{"activity_id": 12345678901, "gear_uuid": "abc123"})
	if !isErr || text != "Error adding gear to activity: API Error 500 - boom" {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestRemoveGearFromActivityTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("PUT /gear-service/gear/unlink/abc123/activity/12345678901", map[string]any{})
	text := callOK(t, newSession(t, f, nil), "remove_gear_from_activity", map[string]any{"activity_id": 12345678901, "gear_uuid": "abc123"})
	want := map[string]any{"success": true, "activity_id": 12345678901.0, "gear_uuid": "abc123", "message": "Gear successfully removed from activity"}
	if got := decode(t, text); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	if n := len(f.requests("PUT /gear-service/gear/unlink/abc123/activity/12345678901")); n != 1 {
		t.Errorf("%d PUTs", n)
	}
}

func TestRemoveGearFromActivityMissingGearIsUnavailable(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("PUT /gear-service/gear/unlink/missing/activity/12345678901", status(404, map[string]any{"message": "Gear with uuid: missing doesn't exist."}))
	text := callOK(t, newSession(t, f, nil), "remove_gear_from_activity", map[string]any{"activity_id": 12345678901, "gear_uuid": "missing"})
	if text != "No gear association changed; gear missing was not found for activity 12345678901." {
		t.Error(text)
	}
}
