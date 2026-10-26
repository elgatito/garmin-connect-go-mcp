package server

import (
	"math"
	"strings"
	"testing"
)

const (
	dataUploadPath        = "/upload-service/upload"
	dataBloodPressurePath = "/bloodpressure-service/bloodpressure"
	dataHydrationPath     = "/usersummary-service/usersummary/hydration/log"
)

func TestAddBodyCompositionTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+dataUploadPath, map[string]any{"status": "success", "message": "Body composition added"})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "add_body_composition", map[string]any{
		"date": "2024-01-15", "weight": 70.0, "percent_fat": 15.0,
	})
	if isErr {
		t.Fatalf("error: %s", text)
	}
	calls := f.callsMatching("POST", dataUploadPath)
	if len(calls) != 1 {
		t.Fatalf("upload calls = %d", len(calls))
	}
	body := string(calls[0].Body)
	if !strings.Contains(body, `name="file"`) || !strings.Contains(body, `filename="body_composition.fit"`) {
		t.Fatalf("multipart body = %q", body)
	}
}

func TestBodyCompositionRejectsUnencodableMassInKg(t *testing.T) {
	fields := []string{"weight", "visceral_fat_mass", "bone_mass", "muscle_mass"}
	values := []float64{700, 655.35, -1, math.Inf(1)}
	for _, field := range fields {
		for _, value := range values {
			if math.IsInf(value, 0) {
				msg := bodyCompositionMassError(field, &value)
				if msg == "" || !strings.Contains(msg, field) || !strings.Contains(msg, "kg") || !strings.Contains(msg, "FIT") {
					t.Fatalf("%s inf: %q", field, msg)
				}
				continue
			}
			f := newFakeGarmin(t)
			cs := newSession(t, f, nil)
			args := map[string]any{"date": "2024-01-15", "weight": 70.0}
			args[field] = value
			text, _ := callTool(t, cs, "add_body_composition", args)
			if !strings.Contains(text, field) || !strings.Contains(text, "kg") || !strings.Contains(text, "FIT") {
				t.Fatalf("%s=%v: text = %q", field, value, text)
			}
			if calls := f.callsMatching("POST", dataUploadPath); len(calls) != 0 {
				t.Fatalf("%s=%v: upload calls = %d", field, value, len(calls))
			}
		}
	}
}

func TestBodyCompositionDocumentsMassAndEnergyUnits(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	var props map[string]any
	for _, tool := range listToolObjects(t, cs) {
		if tool.Name != "add_body_composition" {
			continue
		}
		schema, _ := tool.InputSchema.(map[string]any)
		props, _ = schema["properties"].(map[string]any)
	}
	if props == nil {
		t.Fatal("add_body_composition has no properties")
	}
	for _, field := range []string{"visceral_fat_mass", "bone_mass", "muscle_mass"} {
		desc, _ := asMap(props[field])["description"].(string)
		if !strings.Contains(desc, "kg") {
			t.Errorf("%s description = %q", field, desc)
		}
	}
	for _, field := range []string{"basal_met", "active_met"} {
		desc, _ := asMap(props[field])["description"].(string)
		if !strings.Contains(desc, "kcal/day") {
			t.Errorf("%s description = %q", field, desc)
		}
	}
}

func TestSetBloodPressureTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+dataBloodPressurePath, map[string]any{"status": "success", "message": "Blood pressure added"})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "set_blood_pressure", map[string]any{
		"systolic": 120, "diastolic": 80, "pulse": 65,
	})
	if isErr {
		t.Fatalf("error: %s", text)
	}
	calls := f.callsMatching("POST", dataBloodPressurePath)
	var payload map[string]any
	decodeJSON(t, string(calls[0].Body), &payload)
	if payload["systolic"] != 120.0 || payload["diastolic"] != 80.0 || payload["pulse"] != 65.0 {
		t.Fatalf("payload = %v", payload)
	}
	if payload["notes"] != nil {
		t.Fatalf("notes = %v", payload["notes"])
	}
}

func TestAddHydrationDataTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("PUT "+dataHydrationPath, map[string]any{"status": "success", "message": "Hydration data added"})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "add_hydration_data", map[string]any{
		"value_in_ml": 500, "cdate": "2024-01-15", "timestamp": "2024-01-15T10:00:00",
	})
	if isErr {
		t.Fatalf("error: %s", text)
	}
	calls := f.callsMatching("PUT", dataHydrationPath)
	var payload map[string]any
	decodeJSON(t, string(calls[0].Body), &payload)
	if payload["calendarDate"] != "2024-01-15" || payload["valueInML"] != 500.0 {
		t.Fatalf("payload = %v", payload)
	}
	if payload["timestampLocal"] != "2024-01-15T10:00:00.000" {
		t.Fatalf("timestampLocal = %v", payload["timestampLocal"])
	}
}
