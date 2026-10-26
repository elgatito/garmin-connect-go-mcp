package server

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func gearByUUID(t *testing.T, payload map[string]any) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, item := range mutList(t, payload["gear"]) {
		m := mutMap(t, item)
		out[m["uuid"].(string)] = m
	}
	return out
}

func TestGetGearTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+deviceLastUsedPathTest, fixture(t, "MOCK_DEVICE_LAST_USED"))
	f.on("GET "+gearFilterPathTest, fixture(t, "MOCK_GEAR"))
	f.on("GET /gear-service/gear/v2/list", fixture(t, "MOCK_GEAR_V2"))
	f.on("GET "+gearBasePathTest+"/user/80653452/activityTypes", fixture(t, "MOCK_GEAR_DEFAULTS"))
	f.on("GET "+gearBasePathTest+"/stats/8abfc40d71fb4860bce19072b6c79644", fixture(t, "MOCK_GEAR_STATS"))
	f.on("GET "+gearBasePathTest+"/stats/6f27ed27397749ac9f6f450e039c2424", fixture(t, "MOCK_GEAR_STATS"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_gear", nil)
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	gear := gearByUUID(t, got)
	if gear["8abfc40d71fb4860bce19072b6c79644"]["notes"] != "Rotation shoe; blue laces." {
		t.Fatalf("notes = %v", gear["8abfc40d71fb4860bce19072b6c79644"]["notes"])
	}
	if notes, exists := gear["6f27ed27397749ac9f6f450e039c2424"]["notes"]; !exists || notes != nil {
		t.Fatalf("notes = %v", gear["6f27ed27397749ac9f6f450e039c2424"])
	}

	if calls := f.callsMatching("GET", deviceLastUsedPathTest); len(calls) != 1 {
		t.Fatalf("device calls = %d, want 1", len(calls))
	}
	gearCalls := f.callsMatching("GET", gearFilterPathTest)
	if len(gearCalls) != 1 {
		t.Fatalf("gear calls = %d, want 1", len(gearCalls))
	}
	if got := gearCalls[0].Query.Get("userProfilePk"); got != "80653452" {
		t.Fatalf("userProfilePk = %q", got)
	}
	if calls := f.callsMatching("GET", "/gear-service/gear/v2/list"); len(calls) != 1 {
		t.Fatalf("v2 calls = %d, want 1", len(calls))
	}
	if calls := f.callsMatching("GET", gearBasePathTest+"/user/80653452/activityTypes"); len(calls) != 1 {
		t.Fatalf("defaults calls = %d, want 1", len(calls))
	}
}

func TestGetGearPreservesActivityCountWhenDistanceIsNull(t *testing.T) {
	for _, total := range []int{0, 3} {
		t.Run(fmt.Sprintf("total_%d", total), func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on("GET "+deviceLastUsedPathTest, fixture(t, "MOCK_DEVICE_LAST_USED"))
			f.on("GET "+gearFilterPathTest, []any{mutList(t, fixture(t, "MOCK_GEAR"))[0]})
			f.on("GET /gear-service/gear/v2/list", []any{})
			f.on("GET "+gearBasePathTest+"/user/80653452/activityTypes", []any{})
			f.on("GET "+gearBasePathTest+"/stats/8abfc40d71fb4860bce19072b6c79644", map[string]any{
				"totalActivities": total, "totalDistance": nil,
			})
			cs := newSession(t, f, nil)

			text, isErr := callTool(t, cs, "get_gear", nil)
			if isErr {
				t.Fatalf("unexpected error: %q", text)
			}
			var got map[string]any
			decodeJSON(t, text, &got)
			gear := mutMap(t, mutList(t, got["gear"])[0])
			sameJSON(t, gear["stats"], map[string]any{
				"total_activities":  total,
				"total_distance_km": nil,
			})
		})
	}
}

func TestGetGearReportsPartialStatsFailures(t *testing.T) {
	gearList := mutList(t, fixture(t, "MOCK_GEAR"))
	for _, code := range []int{401, 429, 503} {
		t.Run(fmt.Sprintf("status_%d", code), func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on("GET "+deviceLastUsedPathTest, fixture(t, "MOCK_DEVICE_LAST_USED"))
			f.on("GET "+gearFilterPathTest, []any{gearList[0], gearList[1]})
			f.on("GET /gear-service/gear/v2/list", []any{})
			f.on("GET "+gearBasePathTest+"/user/80653452/activityTypes", []any{})
			f.on("GET "+gearBasePathTest+"/stats/8abfc40d71fb4860bce19072b6c79644", map[string]any{
				"totalActivities": 10, "totalDistance": 1000,
			})
			f.on("GET "+gearBasePathTest+"/stats/6f27ed27397749ac9f6f450e039c2424",
				status(code, map[string]any{"message": "stats failed"}))
			if code == 401 {
				f.on("POST /diauth", map[string]any{"access_token": "refreshed"})
			}
			cs := newSession(t, f, nil)

			text, isErr := callTool(t, cs, "get_gear", nil)
			if isErr {
				t.Fatalf("unexpected error: %q", text)
			}
			var got map[string]any
			decodeJSON(t, text, &got)
			if got["gear_count"] != float64(2) {
				t.Fatalf("gear_count = %v", got["gear_count"])
			}
			gear := gearByUUID(t, got)
			stats := mutMap(t, gear["8abfc40d71fb4860bce19072b6c79644"]["stats"])
			if stats["total_activities"] != float64(10) {
				t.Fatalf("stats = %v", stats)
			}
			want := fmt.Sprintf("API Error %d - stats failed", code)
			if gear["6f27ed27397749ac9f6f450e039c2424"]["stats_error"] != want {
				t.Fatalf("stats_error = %v, want %q", gear["6f27ed27397749ac9f6f450e039c2424"]["stats_error"], want)
			}
		})
	}
}

func TestGetGearToolDefaultActivityTypes(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+deviceLastUsedPathTest, fixture(t, "MOCK_DEVICE_LAST_USED"))
	f.on("GET "+gearFilterPathTest, []any{mutList(t, fixture(t, "MOCK_GEAR"))[0]})
	f.on("GET /gear-service/gear/v2/list", []any{})
	defaults := []any{}
	for _, pk := range []int{1, 3, 6, 9, 10, 13, 26, 143, 999} {
		defaults = append(defaults, map[string]any{
			"uuid":           "8abfc40d71fb4860bce19072b6c79644",
			"activityTypePk": pk,
			"defaultGear":    true,
		})
	}
	f.on("GET "+gearBasePathTest+"/user/80653452/activityTypes", defaults)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_gear", map[string]any{"include_stats": false})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	gear := mutMap(t, mutList(t, got["gear"])[0])
	sameJSON(t, gear["is_default_for"], []any{
		"running",
		"hiking",
		"trail_running",
		"walking",
		"road_biking",
		"strength_training",
		"swimming",
		"gravel_cycling",
		"activity_999",
	})
}

func TestGetGearToolDefaultsSummary(t *testing.T) {
	gearList := mutList(t, fixture(t, "MOCK_GEAR"))
	unnamed := func() map[string]any {
		m := cloneMap(mutMap(t, gearList[1]))
		m["gearStatusName"] = "active"
		m["displayName"] = nil
		return m
	}
	f := newFakeGarmin(t)
	f.on("GET "+deviceLastUsedPathTest, fixture(t, "MOCK_DEVICE_LAST_USED"))
	f.on("GET "+gearFilterPathTest, []any{gearList[0], unnamed()})
	f.on("GET /gear-service/gear/v2/list", []any{})
	f.on("GET "+gearBasePathTest+"/user/80653452/activityTypes", []any{
		map[string]any{"uuid": "8abfc40d71fb4860bce19072b6c79644", "activityTypePk": 1, "defaultGear": true},
		map[string]any{"uuid": "8abfc40d71fb4860bce19072b6c79644", "activityTypePk": 6, "defaultGear": true},
		map[string]any{"uuid": "6f27ed27397749ac9f6f450e039c2424", "activityTypePk": 1, "defaultGear": true},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_gear", map[string]any{"include_stats": false})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	sameJSON(t, got["defaults"], map[string]any{
		"running":       []any{"Nimbus 25", "ASICS Gel-Nimbus 24"},
		"trail_running": []any{"Nimbus 25"},
	})
}

func TestGetGearToolPreservesEmptyAndNullNotes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		notes any
	}{
		{"empty", ""},
		{"null", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on("GET "+deviceLastUsedPathTest, fixture(t, "MOCK_DEVICE_LAST_USED"))
			f.on("GET "+gearFilterPathTest, []any{mutList(t, fixture(t, "MOCK_GEAR"))[0]})
			v2 := mutMap(t, mutList(t, fixture(t, "MOCK_GEAR_V2"))[0])
			v2["notes"] = tc.notes
			f.on("GET /gear-service/gear/v2/list", []any{v2})
			f.on("GET "+gearBasePathTest+"/user/80653452/activityTypes", []any{})
			cs := newSession(t, f, nil)

			text, isErr := callTool(t, cs, "get_gear", map[string]any{"include_stats": false})
			if isErr {
				t.Fatalf("unexpected error: %q", text)
			}
			var got map[string]any
			decodeJSON(t, text, &got)
			gear := mutMap(t, mutList(t, got["gear"])[0])
			notes, exists := gear["notes"]
			if !exists || notes != tc.notes {
				t.Fatalf("notes = %v (exists %v), want %v", notes, exists, tc.notes)
			}
		})
	}
}

func TestGetGearToolWithoutStats(t *testing.T) {
	var log bytes.Buffer
	f := newFakeGarmin(t)
	f.on("GET "+deviceLastUsedPathTest, fixture(t, "MOCK_DEVICE_LAST_USED"))
	f.on("GET "+gearFilterPathTest, fixture(t, "MOCK_GEAR"))
	f.on("GET /gear-service/gear/v2/list", status(500, map[string]any{"message": "v2 unavailable"}))
	f.on("GET "+gearBasePathTest+"/user/80653452/activityTypes", fixture(t, "MOCK_GEAR_DEFAULTS"))
	cs := newSessionWithLog(t, f, &log)

	text, isErr := callTool(t, cs, "get_gear", map[string]any{"include_stats": false})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["gear_count"] != float64(2) {
		t.Fatalf("gear_count = %v", got["gear_count"])
	}
	for _, item := range mutList(t, got["gear"]) {
		m := mutMap(t, item)
		if m["notes"] != nil {
			t.Fatalf("notes = %v", m["notes"])
		}
	}
	if !strings.Contains(log.String(), "Garmin v2 gear list unavailable: API Error 500 - v2 unavailable") {
		t.Fatalf("log = %q", log.String())
	}
	if calls := f.callsMatching("GET", gearBasePathTest+"/stats/8abfc40d71fb4860bce19072b6c79644"); len(calls) != 0 {
		t.Fatalf("stats calls = %d, want 0", len(calls))
	}
}

func TestAddGearToActivityTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("PUT "+gearBasePathTest+"/link/abc123/activity/12345678901", map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "add_gear_to_activity", map[string]any{
		"activity_id": 12345678901,
		"gear_uuid":   "abc123",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["success"] != true || got["activity_id"] != float64(12345678901) || got["gear_uuid"] != "abc123" {
		t.Fatalf("result = %v", got)
	}
	if calls := f.callsMatching("PUT", gearBasePathTest+"/link/abc123/activity/12345678901"); len(calls) != 1 {
		t.Fatalf("link calls = %d, want 1", len(calls))
	}
}

func TestAddGearToActivityMissingGearIsUnavailable(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("PUT "+gearBasePathTest+"/link/missing/activity/12345678901",
		status(404, map[string]any{"message": "Gear with uuid: missing doesn't exist."}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "add_gear_to_activity", map[string]any{
		"activity_id": 12345678901,
		"gear_uuid":   "missing",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	want := "No gear association changed; gear missing was not found for activity 12345678901."
	if text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
}

func TestRemoveGearFromActivityTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("PUT "+gearBasePathTest+"/unlink/abc123/activity/12345678901", map[string]any{})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "remove_gear_from_activity", map[string]any{
		"activity_id": 12345678901,
		"gear_uuid":   "abc123",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	var got map[string]any
	decodeJSON(t, text, &got)
	if got["success"] != true || got["activity_id"] != float64(12345678901) || got["gear_uuid"] != "abc123" {
		t.Fatalf("result = %v", got)
	}
	if calls := f.callsMatching("PUT", gearBasePathTest+"/unlink/abc123/activity/12345678901"); len(calls) != 1 {
		t.Fatalf("unlink calls = %d, want 1", len(calls))
	}
}

func TestRemoveGearFromActivityMissingGearIsUnavailable(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("PUT "+gearBasePathTest+"/unlink/missing/activity/12345678901",
		status(404, map[string]any{"message": "Gear with uuid: missing doesn't exist."}))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "remove_gear_from_activity", map[string]any{
		"activity_id": 12345678901,
		"gear_uuid":   "missing",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	want := "No gear association changed; gear missing was not found for activity 12345678901."
	if text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
}
