package server

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Ported from the data_management cases of tests/integration/test_other_modules_tools.py.

const (
	uploadRoute    = "POST /upload-service/upload"
	pressureRoute  = "POST /bloodpressure-service/bloodpressure"
	hydrationRoute = "PUT /usersummary-service/usersummary/hydration/log"
)

// fitCRC is the FIT file CRC-16 (independent of the encoder under test).
func fitCRC(b []byte) uint16 {
	table := [16]uint16{0x0000, 0xCC01, 0xD801, 0x1400, 0xF001, 0x3C00, 0x2800, 0xE401, 0xA001, 0x6C00, 0x7800, 0xB401, 0x5000, 0x9C01, 0x8801, 0x4400}
	var crc uint16
	for _, v := range b {
		crc = (crc>>4)&0x0FFF ^ table[crc&0xF] ^ table[v&0xF]
		crc = (crc>>4)&0x0FFF ^ table[crc&0xF] ^ table[v>>4]
	}
	return crc
}

// uploadedFIT returns the file part of the one upload request.
func uploadedFIT(t *testing.T, c recordedCall) (filename string, data []byte) {
	t.Helper()
	mt, params, err := mime.ParseMediaType(c.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/form-data" {
		t.Fatalf("content type %q", c.Header.Get("Content-Type"))
	}
	p, err := multipart.NewReader(bytes.NewReader(c.Body), params["boundary"]).NextPart()
	if err != nil || p.FormName() != "file" {
		t.Fatalf("part: %v %v", p, err)
	}
	data, _ = io.ReadAll(p)
	return p.FileName(), data
}

func TestAddBodyCompositionTool(t *testing.T) {
	setLocal(t, time.UTC)
	f := newFakeGarmin(t)
	resp := map[string]any{"status": "success", "message": "Body composition added"}
	f.on(uploadRoute, resp)
	got := decode(t, callOK(t, newSession(t, f, nil), "add_body_composition",
		map[string]any{"date": "2024-01-15", "weight": 70.0, "percent_fat": 15.0}))
	if !reflect.DeepEqual(got, resp) {
		t.Errorf("got %v", got)
	}
	name, data := uploadedFIT(t, theCall(t, f, uploadRoute))
	if name != "body_composition.fit" || string(data[8:12]) != ".FIT" || data[0] != 12 || data[1] != 16 {
		t.Fatalf("file %q header % x", name, data[:12])
	}
	if size := binary.LittleEndian.Uint32(data[4:8]); int(size) != len(data)-14 {
		t.Errorf("data size %d, file %d", size, len(data))
	}
	if crc := binary.LittleEndian.Uint16(data[len(data)-2:]); crc != fitCRC(data[:len(data)-2]) {
		t.Errorf("bad CRC %04x", crc)
	}
	// The last record is the weight scale message: timestamp (FIT epoch), weight 70.00 kg and 15.00 % fat, the rest invalid.
	rec := data[len(data)-2-25:]
	le := binary.LittleEndian
	if le.Uint32(rec[0:4]) != uint32(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC).Unix()-631065600) || le.Uint16(rec[4:6]) != 7000 ||
		le.Uint16(rec[6:8]) != 1500 || le.Uint16(rec[8:10]) != 0xFFFF {
		t.Errorf("weight record % x", rec)
	}
}

func TestAddBodyCompositionAllFieldsAndErrors(t *testing.T) {
	setLocal(t, time.UTC)
	f := newFakeGarmin(t)
	f.on(uploadRoute, map[string]any{})
	cs := newSession(t, f, nil)
	callOK(t, cs, "add_body_composition", map[string]any{"date": "2024-01-15T07:30:05", "weight": 19.99, "percent_fat": 22.5, "percent_hydration": 55.1,
		"visceral_fat_mass": 1.23, "bone_mass": 3.2, "muscle_mass": 32.5, "basal_met": 1600, "active_met": 800, "physique_rating": 5,
		"metabolic_age": 33, "visceral_fat_rating": 7, "bmi": 22.46})
	_, data := uploadedFIT(t, theCall(t, f, uploadRoute))
	rec := data[len(data)-2-25:]
	le := binary.LittleEndian
	got := []uint16{le.Uint16(rec[4:]), le.Uint16(rec[6:]), le.Uint16(rec[8:]), le.Uint16(rec[10:]), le.Uint16(rec[12:]), le.Uint16(rec[14:]),
		le.Uint16(rec[16:]), le.Uint16(rec[18:]), uint16(rec[20]), uint16(rec[21]), uint16(rec[22]), le.Uint16(rec[23:])}
	want := []uint16{1998, 2250, 5510, 123, 320, 3250, 6400, 3200, 5, 33, 7, 224}
	if !reflect.DeepEqual(got, want) { // int() truncates: 19.99*100 = 1998.999..., 22.46*10 = 224.6
		t.Errorf("fields %v, want %v", got, want)
	}
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"date": "nope", "weight": 70}, "Error adding body composition data: Invalid isoformat string: 'nope'"},
		{map[string]any{"date": "2024-01-15", "weight": 0}, "Error adding body composition data: weight must be positive, got: 0.0"},
		{map[string]any{"date": "2024-01-15", "weight": 70, "percent_fat": 700}, "Error adding body composition data: 'H' format requires 0 <= number <= 65535"},
		{map[string]any{"date": "2024-01-15", "weight": 70, "physique_rating": 300}, "Error adding body composition data: 'B' format requires 0 <= number <= 255"},
		{map[string]any{"date": "1980-01-15", "weight": 70}, "Error adding body composition data: 'I' format requires 0 <= number <= 4294967295"},
	} {
		if text, isErr := callTool(t, cs, "add_body_composition", c.args); !isErr || text != c.want {
			t.Errorf("%v: %v %q", c.args, isErr, text)
		}
	}
	if n := len(f.requests(uploadRoute)); n != 1 {
		t.Errorf("%d uploads", n)
	}
}

func TestBodyCompositionRejectsUnencodableMassInKg(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(uploadRoute, map[string]any{})
	cs := newSession(t, f, nil)
	for _, field := range []string{"weight", "visceral_fat_mass", "bone_mass", "muscle_mass"} {
		for _, value := range []float64{700, 655.35, -1} { // float("inf") cannot travel as JSON: see TestBodyCompositionMassCheckRejectsNonFinite
			t.Run(fmt.Sprintf("%s/%v", field, value), func(t *testing.T) {
				args := map[string]any{"date": "2024-01-15", "weight": 70, field: value}
				text, isErr := callTool(t, cs, "add_body_composition", args)
				if !isErr || !strings.Contains(text, field) || !strings.Contains(text, "kg") || !strings.Contains(text, "FIT") {
					t.Errorf("%v %q", isErr, text)
				}
			})
		}
	}
	if len(f.requests(uploadRoute)) != 0 {
		t.Error("an upload was sent")
	}
}

func TestBodyCompositionMassCheckRejectsNonFinite(t *testing.T) {
	for _, v := range []float64{math.Inf(1), math.Inf(-1), math.NaN()} {
		err := checkMassInKg("bone_mass", &v)
		if err == nil || err.Error() != "bone_mass must be a finite value in kg (not grams), at least 0 and below 655.35 for FIT encoding" {
			t.Errorf("%v: %v", v, err)
		}
	}
	ok := 655.34
	if err := checkMassInKg("weight", &ok); err != nil {
		t.Error(err)
	}
	if err := checkMassInKg("weight", nil); err != nil {
		t.Error(err)
	}
}

func TestBodyCompositionDocumentsMassAndEnergyUnits(t *testing.T) {
	cs := newSession(t, newFakeGarmin(t), nil)
	for _, tool := range listTools(t, cs) {
		if tool.Name != "add_body_composition" {
			continue
		}
		props := asMap(asMap(tool.InputSchema)["properties"])
		for field, unit := range map[string]string{"visceral_fat_mass": "kg", "bone_mass": "kg", "muscle_mass": "kg", "basal_met": "kcal/day", "active_met": "kcal/day"} {
			if d, _ := asMap(props[field])["description"].(string); !strings.Contains(d, unit) {
				t.Errorf("%s description %q lacks %s", field, d, unit)
			}
		}
		return
	}
	t.Fatal("add_body_composition not listed")
}

func TestSetBloodPressureTool(t *testing.T) {
	setLocal(t, time.FixedZone("LA", -8*3600))
	f := newFakeGarmin(t)
	resp := map[string]any{"status": "success", "message": "Blood pressure added"}
	f.on(pressureRoute, resp)
	got := decode(t, callOK(t, newSession(t, f, nil), "set_blood_pressure", map[string]any{"systolic": 120, "diastolic": 80, "pulse": 65}))
	if !reflect.DeepEqual(got, resp) {
		t.Errorf("got %v", got)
	}
	b := bodyOf(t, theCall(t, f, pressureRoute))
	local, gmt := b["measurementTimestampLocal"].(string), b["measurementTimestampGMT"].(string)
	l, _ := time.Parse("2006-01-02T15:04:05.000", local)
	g, _ := time.Parse("2006-01-02T15:04:05.000", gmt)
	if g.Sub(l) != 8*time.Hour || time.Since(g) > time.Minute {
		t.Errorf("timestamps %s / %s", local, gmt)
	}
	delete(b, "measurementTimestampLocal")
	delete(b, "measurementTimestampGMT")
	want := map[string]any{"systolic": 120.0, "diastolic": 80.0, "pulse": 65.0, "sourceType": "MANUAL", "notes": nil}
	if !reflect.DeepEqual(b, want) {
		t.Errorf("body = %v", b)
	}
}

func TestSetBloodPressureNotesAndRanges(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(pressureRoute, status(204, rawBody(nil)))
	cs := newSession(t, f, nil)
	if text := callOK(t, cs, "set_blood_pressure", map[string]any{"systolic": 120, "diastolic": 80, "pulse": 65, "notes": "after run"}); text != "{}" {
		t.Errorf("204 result = %q", text)
	}
	if bodyOf(t, theCall(t, f, pressureRoute))["notes"] != "after run" {
		t.Error("notes not sent")
	}
	for _, c := range []struct {
		sys, dia, pulse int
		want            string
	}{
		{69, 80, 65, "systolic must be an int in [70, 260]"},
		{120, 151, 65, "diastolic must be an int in [40, 150]"},
		{120, 80, 19, "pulse must be an int in [20, 250]"},
	} {
		text, isErr := callTool(t, cs, "set_blood_pressure", map[string]any{"systolic": c.sys, "diastolic": c.dia, "pulse": c.pulse})
		if !isErr || text != "Error setting blood pressure values: "+c.want {
			t.Errorf("%v %q", isErr, text)
		}
	}
	if len(f.requests(pressureRoute)) != 1 {
		t.Error("invalid readings were sent")
	}
}

func TestAddHydrationDataTool(t *testing.T) {
	f := newFakeGarmin(t)
	resp := map[string]any{"status": "success", "message": "Hydration data added"}
	f.on(hydrationRoute, resp)
	got := decode(t, callOK(t, newSession(t, f, nil), "add_hydration_data",
		map[string]any{"value_in_ml": 500, "cdate": "2024-01-15", "timestamp": "2024-01-15T10:00:00"}))
	want := map[string]any{"calendarDate": "2024-01-15", "timestampLocal": "2024-01-15T10:00:00.000", "valueInML": 500.0}
	if !reflect.DeepEqual(got, resp) || !reflect.DeepEqual(bodyOf(t, theCall(t, f, hydrationRoute)), want) {
		t.Errorf("got %v body %s", got, f.requests(hydrationRoute)[0].Body)
	}
}

func TestAddHydrationDataValidation(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(hydrationRoute, map[string]any{})
	cs := newSession(t, f, nil)
	for _, c := range []struct {
		ml            int
		cdate, ts     string
		wantErrSuffix string
	}{
		{20000, "2024-01-15", "2024-01-15T10:00:00", "value_in_ml seems unreasonably high (>10000ml)"},
		{-20000, "2024-01-15", "2024-01-15T10:00:00", "value_in_ml seems unreasonably high (>10000ml)"},
		{500, "2024-01-16", "2024-01-15T10:00:00", "timestamp date (2024-01-15) doesn't match cdate (2024-01-16)"},
		{500, "15-01-2024", "2024-01-15T10:00:00", "cdate must be in format 'YYYY-MM-DD', got: 15-01-2024"},
		{500, "2024-01-15", "yesterday", "time data 'yesterday' does not match format '%Y-%m-%dT%H:%M:%S'"},
	} {
		text, isErr := callTool(t, cs, "add_hydration_data", map[string]any{"value_in_ml": c.ml, "cdate": c.cdate, "timestamp": c.ts})
		if !isErr || text != "Error adding hydration data: "+c.wantErrSuffix {
			t.Errorf("%+v: %v %q", c, isErr, text)
		}
	}
	// A negative amount subtracts, and fractional seconds are kept to the millisecond.
	callOK(t, cs, "add_hydration_data", map[string]any{"value_in_ml": -250, "cdate": "2024-01-15", "timestamp": "2024-01-15T10:00:00.123456"})
	want := map[string]any{"calendarDate": "2024-01-15", "timestampLocal": "2024-01-15T10:00:00.123", "valueInML": -250.0}
	if b := bodyOf(t, theCall(t, f, hydrationRoute)); !reflect.DeepEqual(b, want) {
		t.Errorf("body = %v", b)
	}
}
