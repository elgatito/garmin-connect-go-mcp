package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

const (
	workoutsPath     = "/workout-service/workouts"
	workoutPath      = "/workout-service/workout"
	schedulePath     = "/workout-service/schedule"
	graphqlPath      = "/graphql-gateway/graphql"
	fbtAdaptivePath  = "/workout-service/fbt-adaptive/"
	workoutFITPrefix = "/workout-service/workout/FIT/"
)

func workoutText(t *testing.T, text string) map[string]any {
	t.Helper()
	var got map[string]any
	decodeJSON(t, text, &got)
	return got
}

func listOf(v any) []any { return v.([]any) }

func mapOf(v any) map[string]any { return v.(map[string]any) }

func deepCopy(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func runningWorkoutWithSteps(steps []any, name string) map[string]any {
	return map[string]any{
		"workoutName": name,
		"sportType":   map[string]any{"sportTypeId": 1, "sportTypeKey": "running"},
		"workoutSegments": []any{map[string]any{
			"segmentOrder": 1,
			"sportType":    map[string]any{"sportTypeId": 1, "sportTypeKey": "running"},
			"workoutSteps": steps,
		}},
	}
}

func cyclingWorkoutWithSteps(steps []any, name string) map[string]any {
	return map[string]any{
		"workoutName": name,
		"sportType":   map[string]any{"sportTypeId": 2, "sportTypeKey": "cycling"},
		"workoutSegments": []any{map[string]any{
			"segmentOrder": 1,
			"sportType":    map[string]any{"sportTypeId": 2, "sportTypeKey": "cycling"},
			"workoutSteps": steps,
		}},
	}
}

func timedIntervalStep(targetType any) map[string]any {
	return map[string]any{
		"type":              "ExecutableStepDTO",
		"stepOrder":         1,
		"stepType":          map[string]any{"stepTypeId": 3, "stepTypeKey": "interval"},
		"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
		"endConditionValue": 300,
		"targetType":        targetType,
		"targetValueOne":    143,
		"targetValueTwo":    157,
	}
}

func distancePaceStepWithNestedBounds() map[string]any {
	return map[string]any{
		"type":              "ExecutableStepDTO",
		"stepOrder":         1,
		"stepType":          map[string]any{"stepTypeId": 3, "stepTypeKey": "interval"},
		"endCondition":      map[string]any{"conditionTypeId": 3, "conditionTypeKey": "distance"},
		"endConditionValue": 400,
		"targetType": map[string]any{
			"workoutTargetTypeId":  6,
			"workoutTargetTypeKey": "pace.zone",
			"targetValueOne":       2.0833333,
			"targetValueTwo":       1.9607843,
		},
	}
}

func firstUploadBody(t *testing.T, f *fakeGarmin) map[string]any {
	t.Helper()
	calls := f.callsMatching("POST", workoutPath)
	if len(calls) == 0 {
		t.Fatal("no upload call")
	}
	var body map[string]any
	decodeJSON(t, string(calls[0].Body), &body)
	return body
}

func uploadStep(t *testing.T, f *fakeGarmin, index ...int) map[string]any {
	t.Helper()
	body := firstUploadBody(t, f)
	steps := listOf(mapOf(listOf(body["workoutSegments"])[0])["workoutSteps"])
	step := mapOf(steps[0])
	for _, i := range index {
		step = mapOf(listOf(step["workoutSteps"])[i])
	}
	return step
}

func emptyScheduleGraphQL(f *fakeGarmin) {
	f.on("POST "+graphqlPath, map[string]any{"data": map[string]any{"workoutScheduleSummariesScalar": []any{}}})
}

func doneWriting(t *testing.T, text string) map[string]any {
	t.Helper()
	return workoutText(t, text)
}

func newTestGarminClient(t *testing.T, f *fakeGarmin) *garmin.Client {
	t.Helper()
	st := newTestStore(t)
	seedAccount(t, st, "me@x.com", testJWT(nil))
	c, err := garmin.NewClient(garmin.Endpoints{ConnectAPI: f.URL, Connect: f.URL}, &tokenStore{store: st, email: "me@x.com"}, nil)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return c
}

// --- get_workouts ---

func TestGetWorkoutsTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+workoutsPath, fixture(t, "MOCK_WORKOUTS"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_workouts", nil)
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if calls := f.callsMatching("GET", workoutsPath); len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
	data := workoutText(t, text)
	if num(t, data["count"]) != 1 {
		t.Fatalf("data = %v", data)
	}
}

func TestGetWorkoutsPagesThroughWholeLibrary(t *testing.T) {
	first := make([]any, 100)
	for i := 0; i < 100; i++ {
		first[i] = map[string]any{"workoutId": i, "workoutName": fmt.Sprintf("W%d", i)}
	}
	second := make([]any, 5)
	for i := 100; i < 105; i++ {
		second[i-100] = map[string]any{"workoutId": i, "workoutName": fmt.Sprintf("W%d", i)}
	}
	f := newFakeGarmin(t)
	f.on("GET "+workoutsPath, sequence{first, second})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_workouts", nil)
	data := workoutText(t, text)
	if num(t, data["count"]) != 105 {
		t.Fatalf("count = %v", data["count"])
	}
	workouts := listOf(data["workouts"])
	if num(t, mapOf(workouts[len(workouts)-1])["id"]) != 104 {
		t.Fatalf("last = %v", workouts[len(workouts)-1])
	}
	calls := f.callsMatching("GET", workoutsPath)
	if len(calls) != 2 || calls[0].Query.Get("start") != "1" || calls[1].Query.Get("start") != "101" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestGetWorkoutsStopsWhenGarminRepeatsAPage(t *testing.T) {
	page := make([]any, 100)
	for i := 0; i < 100; i++ {
		page[i] = map[string]any{"workoutId": i, "workoutName": fmt.Sprintf("W%d", i)}
	}
	f := newFakeGarmin(t)
	f.on("GET "+workoutsPath, page)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_workouts", nil)
	if num(t, workoutText(t, text)["count"]) != 100 {
		t.Fatalf("count = %v", workoutText(t, text)["count"])
	}
	if calls := f.callsMatching("GET", workoutsPath); len(calls) != 2 {
		t.Fatalf("calls = %d", len(calls))
	}
}

func TestGetWorkoutsHandlesNullSportType(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+workoutsPath, []any{
		map[string]any{"workoutId": 1, "workoutName": "Easy Run", "sportType": map[string]any{"sportTypeKey": "running"}},
		map[string]any{"workoutId": 2, "workoutName": "Imported", "sportType": nil},
	})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_workouts", nil)
	data := workoutText(t, text)
	workouts := listOf(data["workouts"])
	if num(t, data["count"]) != 2 {
		t.Fatalf("count = %v", data["count"])
	}
	if mapOf(workouts[0])["sport"] != "running" || mapOf(workouts[1])["name"] != "Imported" {
		t.Fatalf("workouts = %v", workouts)
	}
	if _, ok := mapOf(workouts[1])["sport"]; ok {
		t.Fatal("sport present on null sportType")
	}
}

func TestGetWorkoutsKeepsGarminEstimatedMetrics(t *testing.T) {
	detail := deepCopy(t, fixture(t, "MOCK_SWIM_WORKOUT_DETAILS")).(map[string]any)
	detail["estimatedDurationInSecs"] = 3600
	f := newFakeGarmin(t)
	f.on("GET "+workoutsPath, []any{detail})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_workouts", nil)
	summary := mapOf(listOf(workoutText(t, text)["workouts"])[0])
	if num(t, summary["estimated_duration_seconds"]) != 3600 || num(t, summary["estimated_distance_meters"]) != 3000 {
		t.Fatalf("summary = %v", summary)
	}
}

func TestGetWorkoutsNoData(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+workoutsPath, nil)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_workouts", nil)
	if isErr || text != "No workouts found." {
		t.Fatalf("text = %q isErr=%v", text, isErr)
	}
}

// --- get_workout_by_id ---

func TestGetWorkoutByIdTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+workoutPath+"/123456", fixture(t, "MOCK_WORKOUT_DETAILS"))
	f.on("GET "+fbtAdaptivePath+"123456", nil)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_workout_by_id", map[string]any{"workout_id": 123456})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := workoutText(t, text)
	if num(t, data["id"]) != 123456 || data["name"] != "5K Tempo Run" || data["sport"] != "running" {
		t.Fatalf("data = %v", data)
	}
	segment := mapOf(listOf(data["segments"])[0])
	if num(t, segment["step_count"]) != 3 {
		t.Fatalf("segment = %v", segment)
	}
	steps := listOf(segment["steps"])
	warmup := mapOf(steps[0])
	if warmup["type"] != "warmup" || warmup["end_condition"] != "time" || num(t, warmup["end_condition_value"]) != 600 {
		t.Fatalf("warmup = %v", warmup)
	}
	interval := mapOf(steps[1])
	if interval["type"] != "interval" || interval["target_type"] != "pace.zone" || num(t, interval["target_zone"]) != 4 {
		t.Fatalf("interval = %v", interval)
	}
	if calls := f.callsMatching("GET", fbtAdaptivePath+"123456"); len(calls) != 0 {
		t.Fatalf("uuid endpoint called: %d", len(calls))
	}
}

func TestGetWorkoutPaceTargetsIncludeMpsUnits(t *testing.T) {
	for _, secondary := range []bool{false, true} {
		t.Run(fmt.Sprintf("secondary_%v", secondary), func(t *testing.T) {
			prefix := ""
			if secondary {
				prefix = "secondary"
			}
			field := func(name string) string {
				if prefix == "" {
					return name
				}
				return prefix + strings.ToUpper(name[:1]) + name[1:]
			}
			step := map[string]any{
				field("targetType"):      map[string]any{"workoutTargetTypeId": 6, "workoutTargetTypeKey": "pace.zone"},
				field("targetValueOne"):  2.777,
				field("targetValueTwo"):  2.472,
				field("targetValueUnit"): nil,
			}
			f := newFakeGarmin(t)
			f.on("GET "+workoutPath+"/123", runningWorkoutWithSteps([]any{step}, "Pace"))
			cs := newSession(t, f, nil)

			text, _ := callTool(t, cs, "get_workout_by_id", map[string]any{"workout_id": 123})
			curated := mapOf(listOf(mapOf(listOf(workoutText(t, text)["segments"])[0])["steps"])[0])
			outPrefix := ""
			if secondary {
				outPrefix = "secondary_"
			}
			if curated[outPrefix+"target_value_unit"] != "meters_per_second" {
				t.Fatalf("unit = %v", curated[outPrefix+"target_value_unit"])
			}
			if num(t, curated[outPrefix+"target_value_low"]) != 2.777 || num(t, curated[outPrefix+"target_value_high"]) != 2.472 {
				t.Fatalf("values = %v", curated)
			}
		})
	}
}

func TestGetWorkoutByIdToolKeepsTargetUnits(t *testing.T) {
	power := map[string]any{"workoutTargetTypeId": 2, "workoutTargetTypeKey": "power.zone", "displayOrder": 2}
	cadence := map[string]any{"workoutTargetTypeId": 3, "workoutTargetTypeKey": "cadence", "displayOrder": 3}
	percent := map[string]any{"unitId": 253, "unitKey": "percent", "factor": 1.0}
	f := newFakeGarmin(t)
	f.on("GET "+workoutPath+"/1", map[string]any{
		"workoutId": 1, "workoutName": "Units",
		"sportType":      map[string]any{"sportTypeId": 2, "sportTypeKey": "cycling"},
		"poolLength":     nil,
		"poolLengthUnit": nil,
		"workoutSegments": []any{map[string]any{
			"segmentOrder": 1,
			"sportType":    map[string]any{"sportTypeId": 2, "sportTypeKey": "cycling"},
			"workoutSteps": []any{
				map[string]any{
					"type": "ExecutableStepDTO", "stepOrder": 1,
					"stepType":                 map[string]any{"stepTypeId": 3, "stepTypeKey": "interval"},
					"targetType":               power,
					"targetValueOne":           76.0,
					"targetValueTwo":           81.0,
					"targetValueUnit":          percent,
					"secondaryTargetType":      cadence,
					"secondaryTargetValueOne":  85.0,
					"secondaryTargetValueTwo":  95.0,
					"secondaryTargetValueUnit": nil,
				},
				map[string]any{
					"type": "ExecutableStepDTO", "stepOrder": 2,
					"stepType":                 map[string]any{"stepTypeId": 3, "stepTypeKey": "interval"},
					"targetType":               cadence,
					"targetValueOne":           85.0,
					"targetValueTwo":           95.0,
					"secondaryTargetType":      power,
					"secondaryTargetValueOne":  76.0,
					"secondaryTargetValueTwo":  81.0,
					"secondaryTargetValueUnit": percent,
				},
			},
		}},
	})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_workout_by_id", map[string]any{"workout_id": 1})
	data := workoutText(t, text)
	if _, ok := data["pool_length"]; ok {
		t.Fatal("pool_length present")
	}
	steps := listOf(mapOf(listOf(data["segments"])[0])["steps"])
	powerFirst, cadenceFirst := mapOf(steps[0]), mapOf(steps[1])
	if powerFirst["target_value_unit"] != "percent" {
		t.Fatalf("power unit = %v", powerFirst["target_value_unit"])
	}
	if _, ok := powerFirst["secondary_target_value_unit"]; ok {
		t.Fatal("secondary unit present")
	}
	if _, ok := cadenceFirst["target_value_unit"]; ok {
		t.Fatal("cadence unit present")
	}
	if cadenceFirst["secondary_target_value_unit"] != "percent" {
		t.Fatalf("secondary unit = %v", cadenceFirst["secondary_target_value_unit"])
	}
}

func TestGetWorkoutByIdToolKeepsPoolLength(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+workoutPath+"/1", map[string]any{
		"workoutId": 1, "workoutName": "Pool",
		"sportType":      map[string]any{"sportTypeId": 4, "sportTypeKey": "swimming"},
		"poolLength":     50.0,
		"poolLengthUnit": map[string]any{"unitId": 1, "unitKey": "meter", "factor": 100.0},
		"workoutSegments": []any{map[string]any{
			"segmentOrder":   1,
			"sportType":      map[string]any{"sportTypeId": 4, "sportTypeKey": "swimming"},
			"poolLength":     nil,
			"poolLengthUnit": nil,
			"workoutSteps":   []any{},
		}},
	})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_workout_by_id", map[string]any{"workout_id": 1})
	data := workoutText(t, text)
	if num(t, data["pool_length"]) != 50 || data["pool_length_unit"] != "meter" {
		t.Fatalf("data = %v", data)
	}
}

func TestGetWorkoutByIdToolRawReturnsGarminJson(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+workoutPath+"/123456", fixture(t, "MOCK_WORKOUT_DETAILS"))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_workout_by_id", map[string]any{"workout_id": 123456, "raw": true})
	var got any
	decodeJSON(t, text, &got)
	sameJSON(t, got, fixture(t, "MOCK_WORKOUT_DETAILS"))
}

func TestGetWorkoutByIdHandlesNullOptionalSections(t *testing.T) {
	workout := deepCopy(t, fixture(t, "MOCK_WORKOUT_DETAILS")).(map[string]any)
	workout["sportType"] = nil
	segment := mapOf(listOf(workout["workoutSegments"])[0])
	segment["sportType"] = nil
	segment["workoutSteps"] = nil
	f := newFakeGarmin(t)
	f.on("GET "+workoutPath+"/123456", workout)
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_workout_by_id", map[string]any{"workout_id": 123456})
	data := workoutText(t, text)
	seg := mapOf(listOf(data["segments"])[0])
	if num(t, data["id"]) != 123456 || num(t, seg["order"]) != 1 {
		t.Fatalf("data = %v", data)
	}
	if _, ok := seg["sport"]; ok {
		t.Fatal("sport present")
	}
	if _, ok := seg["steps"]; ok {
		t.Fatal("steps present")
	}
}

func TestGetWorkoutByIdToolHandlesSwimSecondaryTargets(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+workoutPath+"/1528077786", fixture(t, "MOCK_SWIM_WORKOUT_DETAILS"))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_workout_by_id", map[string]any{"workout_id": 1528077786})
	data := workoutText(t, text)
	if num(t, data["id"]) != 1528077786 || data["sport"] != "swimming" || num(t, data["estimated_distance_meters"]) != 3000 {
		t.Fatalf("data = %v", data)
	}
	segment := mapOf(listOf(data["segments"])[0])
	if num(t, segment["step_count"]) != 2 {
		t.Fatalf("segment = %v", segment)
	}
	steps := listOf(segment["steps"])
	warmup := mapOf(steps[0])
	if warmup["type"] != "warmup" || warmup["secondary_target_type"] != "pace.zone" ||
		num(t, warmup["secondary_target_value_low"]) != 0.45 || num(t, warmup["secondary_target_value_high"]) != 0.6916667 {
		t.Fatalf("warmup = %v", warmup)
	}
	if _, ok := warmup["target_type"]; ok {
		t.Fatal("primary target present")
	}
	repeat := mapOf(steps[1])
	if repeat["type"] != "repeat" || num(t, repeat["repeat_count"]) != 2 || num(t, repeat["step_count"]) != 2 {
		t.Fatalf("repeat = %v", repeat)
	}
	inner := listOf(repeat["steps"])
	interval := mapOf(inner[0])
	if interval["secondary_target_value_low"] != 0.7751938 {
		t.Fatalf("interval = %v", interval)
	}
	rest := mapOf(inner[1])
	if rest["type"] != "rest" || rest["end_condition"] != "fixed.rest" || num(t, rest["end_condition_value"]) != 60 {
		t.Fatalf("rest = %v", rest)
	}
}

func TestGetWorkoutByIdToolIgnoresMalformedTargetBlocks(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+workoutPath+"/123457", map[string]any{
		"workoutId": 123457, "workoutName": "Malformed Swim Workout",
		"sportType": map[string]any{"sportTypeId": 4, "sportTypeKey": "swimming"},
		"workoutSegments": []any{map[string]any{
			"segmentOrder": 1,
			"sportType":    map[string]any{"sportTypeId": 4, "sportTypeKey": "swimming"},
			"workoutSteps": []any{map[string]any{
				"type": "ExecutableStepDTO", "stepOrder": 1,
				"stepType":            map[string]any{"stepTypeId": 1, "stepTypeKey": "warmup"},
				"endCondition":        map[string]any{"conditionTypeId": 3, "conditionTypeKey": "distance"},
				"endConditionValue":   100.0,
				"targetType":          "pace.zone",
				"secondaryTargetType": []any{},
			}},
		}},
	})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_workout_by_id", map[string]any{"workout_id": 123457})
	step := mapOf(listOf(mapOf(listOf(workoutText(t, text)["segments"])[0])["steps"])[0])
	if step["type"] != "warmup" || step["end_condition"] != "distance" || num(t, step["end_condition_value"]) != 100 {
		t.Fatalf("step = %v", step)
	}
	if _, ok := step["target_type"]; ok {
		t.Fatal("target_type present")
	}
	if _, ok := step["secondary_target_type"]; ok {
		t.Fatal("secondary_target_type present")
	}
}

func TestGetWorkoutByUuidTool(t *testing.T) {
	uuid := "d7a5491b-42a5-4d2d-ba38-4e414fc03caf"
	f := newFakeGarmin(t)
	f.on("GET "+fbtAdaptivePath+uuid, map[string]any{
		"workoutId": nil, "workoutUuid": uuid, "workoutName": "Base", "description": "6:20/km",
		"sportType":               map[string]any{"sportTypeId": 1, "sportTypeKey": "running"},
		"estimatedDurationInSecs": 2160,
		"workoutPhrase":           "AEROBIC_LOW_SHORTAGE_BASE",
		"trainingEffectLabel":     "AEROBIC_BASE",
		"estimatedTrainingEffect": 2.3,
		"workoutSegments": []any{map[string]any{
			"segmentOrder": 1,
			"sportType":    map[string]any{"sportTypeId": 1, "sportTypeKey": "running"},
			"workoutSteps": []any{map[string]any{
				"type": "ExecutableStepDTO", "stepOrder": 1,
				"stepType":          map[string]any{"stepTypeId": 3, "stepTypeKey": "interval"},
				"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
				"endConditionValue": 2160.0,
				"targetType":        map[string]any{"workoutTargetTypeId": 6, "workoutTargetTypeKey": "pace.zone"},
				"targetValueOne":    2.777,
				"targetValueTwo":    2.472,
			}},
		}},
	})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_workout_by_id", map[string]any{"workout_id": uuid})
	data := workoutText(t, text)
	if data["uuid"] != uuid || data["name"] != "Base" || data["sport"] != "running" ||
		data["workout_type"] != "AEROBIC_LOW_SHORTAGE_BASE" || data["training_effect_label"] != "AEROBIC_BASE" ||
		num(t, data["estimated_training_effect"]) != 2.3 || num(t, data["estimated_duration_seconds"]) != 2160 {
		t.Fatalf("data = %v", data)
	}
	if calls := f.callsMatching("GET", workoutPath+"/"+uuid); len(calls) != 0 {
		t.Fatalf("numeric endpoint called: %d", len(calls))
	}
}

func TestDownloadWorkoutTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+workoutFITPrefix+"123456", rawBody([]byte("FITDATA")))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "download_workout", map[string]any{"workout_id": 123456})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := workoutText(t, text)
	if num(t, data["workout_id"]) != 123456 || data["format"] != "FIT" || num(t, data["size_bytes"]) != 7 {
		t.Fatalf("data = %v", data)
	}
	if calls := f.callsMatching("GET", workoutFITPrefix+"123456"); len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
}

// --- shared upload/validation tests ---

func TestUploadRepairsNullHrZoneAndNumericStrings(t *testing.T) {
	for _, tool := range []string{"upload_workout", "upload_workouts", "schedule_workouts"} {
		for _, zone := range []any{3, "3"} {
			for _, includeNull := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/%v/%v", tool, zone, includeNull), func(t *testing.T) {
					step := timedIntervalStep(map[string]any{"workoutTargetTypeId": 4, "workoutTargetTypeKey": "heart.rate.zone"})
					step["targetValueOne"] = zone
					step["targetValueTwo"] = nil
					if includeNull {
						step["zoneNumber"] = nil
					}
					payload := runningWorkoutWithSteps([]any{step}, "Validation Workout")
					f := newFakeGarmin(t)
					emptyScheduleGraphQL(f)
					f.on("POST "+workoutPath, map[string]any{"workoutId": 123})
					f.on("POST "+schedulePath+"/123", status(200, nil))
					cs := newSession(t, f, nil)
					args := map[string]any{}
					switch tool {
					case "upload_workout":
						args["workout_data"] = payload
					case "upload_workouts":
						args["workouts"] = []any{payload}
					default:
						args["schedules"] = []any{map[string]any{"calendar_date": "2026-10-20", "workout_data": payload}}
					}
					text, isErr := callTool(t, cs, tool, args)
					if isErr {
						t.Fatalf("unexpected error: %q", text)
					}
					data := workoutText(t, text)
					status := data["status"]
					if status == nil {
						status = mapOf(listOf(data["results"])[0])["status"]
					}
					if status != "success" {
						t.Fatalf("data = %v", data)
					}
					sent := uploadStep(t, f)
					if num(t, sent["zoneNumber"]) != 3 {
						t.Fatalf("zoneNumber = %v", sent["zoneNumber"])
					}
					if _, ok := sent["targetValueOne"]; ok {
						t.Fatal("targetValueOne present")
					}
					if _, ok := sent["targetValueTwo"]; ok {
						t.Fatal("targetValueTwo present")
					}
				})
			}
		}
	}
}

func TestUploadBackfillsExplicitNullRepeatCount(t *testing.T) {
	group := map[string]any{
		"type": "RepeatGroupDTO", "numberOfIterations": nil,
		"endCondition":      map[string]any{"conditionTypeId": 7, "conditionTypeKey": "iterations"},
		"endConditionValue": 4, "workoutSteps": []any{},
	}
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, map[string]any{"workoutId": 123})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "upload_workout", map[string]any{"workout_data": runningWorkoutWithSteps([]any{group}, "Validation Workout")})
	if workoutText(t, text)["status"] != "success" {
		t.Fatalf("text = %q", text)
	}
	if num(t, uploadStep(t, f)["numberOfIterations"]) != 4 {
		t.Fatalf("iterations = %v", uploadStep(t, f)["numberOfIterations"])
	}
}

func TestUploadNormalizesStringConditionAndTargetIds(t *testing.T) {
	step := timedIntervalStep(map[string]any{"workoutTargetTypeId": "1", "workoutTargetTypeKey": "no.target"})
	step["endCondition"].(map[string]any)["conditionTypeId"] = "2"
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, map[string]any{"workoutId": 123})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "upload_workout", map[string]any{"workout_data": runningWorkoutWithSteps([]any{step}, "Validation Workout")})
	if workoutText(t, text)["status"] != "success" {
		t.Fatalf("text = %q", text)
	}
	sent := uploadStep(t, f)
	if num(t, mapOf(sent["endCondition"])["conditionTypeId"]) != 2 || num(t, mapOf(sent["targetType"])["workoutTargetTypeId"]) != 1 {
		t.Fatalf("sent = %v", sent)
	}
}

func TestUploadNullStepListsHaveAFieldError(t *testing.T) {
	cases := map[string]struct {
		mutate func(map[string]any)
		path   string
	}{
		"segments": {func(p map[string]any) { p["workoutSegments"] = nil }, "workoutSegments"},
		"steps": {func(p map[string]any) {
			p["workoutSegments"].([]any)[0].(map[string]any)["workoutSteps"] = nil
		}, "workoutSegments[0].workoutSteps"},
		"nested_steps": {func(p map[string]any) {
			p["workoutSegments"].([]any)[0].(map[string]any)["workoutSteps"] = []any{
				map[string]any{"type": "RepeatGroupDTO", "workoutSteps": nil},
			}
		}, "workoutSegments[0].workoutSteps[0].workoutSteps"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			payload := runningWorkoutWithSteps([]any{}, "Validation Workout")
			tc.mutate(payload)
			f := newFakeGarmin(t)
			cs := newSession(t, f, nil)
			text, isErr := callTool(t, cs, "upload_workout", map[string]any{"workout_data": payload})
			if isErr {
				t.Fatalf("unexpected error: %q", text)
			}
			if !strings.Contains(text, tc.path+" must be a list") {
				t.Fatalf("text = %q", text)
			}
			if calls := f.callsMatching("POST", workoutPath); len(calls) != 0 {
				t.Fatalf("upload calls = %d", len(calls))
			}
		})
	}
}

func TestUploadWorkoutTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, map[string]any{"workoutId": 123457, "workoutName": "New Workout"})
	cs := newSession(t, f, nil)

	payload := map[string]any{"workoutName": "New Workout", "sportType": map[string]any{"sportTypeId": 1}}
	text, isErr := callTool(t, cs, "upload_workout", map[string]any{"workout_data": payload})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := workoutText(t, text)
	if data["status"] != "success" || num(t, data["workout_id"]) != 123457 || data["name"] != "New Workout" {
		t.Fatalf("data = %v", data)
	}
	sameJSON(t, firstUploadBody(t, f), payload)
}

func TestUploadWorkoutPassesPercentFtpRangeThrough(t *testing.T) {
	payload := map[string]any{
		"workoutName": "Sweet spot",
		"sportType":   map[string]any{"sportTypeId": 2, "sportTypeKey": "cycling"},
		"workoutSegments": []any{map[string]any{
			"segmentOrder": 1,
			"sportType":    map[string]any{"sportTypeId": 2, "sportTypeKey": "cycling"},
			"workoutSteps": []any{map[string]any{
				"type": "ExecutableStepDTO", "stepOrder": 1,
				"stepType":          map[string]any{"stepTypeId": 3, "stepTypeKey": "interval"},
				"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
				"endConditionValue": 600.0,
				"targetType":        map[string]any{"workoutTargetTypeId": 2, "workoutTargetTypeKey": "power.zone"},
				"targetValueOne":    76.0,
				"targetValueTwo":    81.0,
				"targetValueUnit":   map[string]any{"unitKey": "percent"},
			}},
		}},
	}
	expected := deepCopy(t, payload)
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, map[string]any{"workoutId": 1, "workoutName": "Sweet spot"})
	cs := newSession(t, f, nil)

	_, _ = callTool(t, cs, "upload_workout", map[string]any{"workout_data": payload})
	sameJSON(t, firstUploadBody(t, f), expected)
}

func TestUploadWorkoutPromotesBoundsNestedInsideTargetType(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, map[string]any{"workoutId": 210001, "workoutName": "Issue 210"})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "upload_workout", map[string]any{
		"workout_data": runningWorkoutWithSteps([]any{distancePaceStepWithNestedBounds()}, "Issue 210"),
	})
	if workoutText(t, text)["status"] != "success" {
		t.Fatalf("text = %q", text)
	}
	called := uploadStep(t, f)
	if num(t, called["targetValueOne"]) != 2.0833333 || num(t, called["targetValueTwo"]) != 1.9607843 {
		t.Fatalf("called = %v", called)
	}
	if _, ok := mapOf(called["targetType"])["targetValueOne"]; ok {
		t.Fatal("nested bound left behind")
	}
}

func TestUploadWorkoutPromotesNestedBoundsInsideRepeatGroup(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, map[string]any{"workoutId": 210002, "workoutName": "Issue 210 Repeat"})
	cs := newSession(t, f, nil)

	group := map[string]any{
		"type": "RepeatGroupDTO", "stepOrder": 1, "numberOfIterations": 3,
		"endCondition":      map[string]any{"conditionTypeId": 7, "conditionTypeKey": "iterations"},
		"endConditionValue": 3,
		"workoutSteps":      []any{distancePaceStepWithNestedBounds()},
	}
	_, _ = callTool(t, cs, "upload_workout", map[string]any{
		"workout_data": runningWorkoutWithSteps([]any{group}, "Issue 210 Repeat"),
	})
	called := uploadStep(t, f, 0)
	if num(t, called["targetValueOne"]) != 2.0833333 || num(t, called["targetValueTwo"]) != 1.9607843 {
		t.Fatalf("called = %v", called)
	}
	if len(mapOf(called["targetType"])) != 2 {
		t.Fatalf("targetType = %v", called["targetType"])
	}
}

func TestUploadWorkoutRejectsConflictingNestedAndStepBounds(t *testing.T) {
	step := distancePaceStepWithNestedBounds()
	step["targetValueOne"] = 2.5
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "upload_workout", map[string]any{
		"workout_data": runningWorkoutWithSteps([]any{step}, "Conflicting Pace Bounds"),
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "workoutSegments[0].workoutSteps[0].targetValueOne=2.5 conflicts with workoutSegments[0].workoutSteps[0].targetType.targetValueOne=2.0833333") {
		t.Fatalf("text = %q", text)
	}
	if !strings.Contains(text, "keep only the step-level targetValueOne") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("POST", workoutPath); len(calls) != 0 {
		t.Fatalf("upload calls = %d", len(calls))
	}
}

func TestUploadWorkoutPromotesZoneNestedInsideTargetType(t *testing.T) {
	step := distancePaceStepWithNestedBounds()
	step["targetType"] = map[string]any{"workoutTargetTypeId": 4, "workoutTargetTypeKey": "heart.rate.zone", "zoneNumber": 3}
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, map[string]any{"workoutId": 210005, "workoutName": "Nested HR Zone"})
	cs := newSession(t, f, nil)

	_, _ = callTool(t, cs, "upload_workout", map[string]any{
		"workout_data": runningWorkoutWithSteps([]any{step}, "Nested HR Zone"),
	})
	called := uploadStep(t, f)
	if num(t, called["zoneNumber"]) != 3 {
		t.Fatalf("zone = %v", called["zoneNumber"])
	}
	if _, ok := mapOf(called["targetType"])["zoneNumber"]; ok {
		t.Fatal("nested zone left behind")
	}
}

func TestUploadWorkoutPromotesNestedHrZoneValueBeforeHrFix(t *testing.T) {
	step := distancePaceStepWithNestedBounds()
	step["targetType"] = map[string]any{"workoutTargetTypeId": 4, "workoutTargetTypeKey": "heart.rate.zone", "targetValueOne": 3}
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, map[string]any{"workoutId": 210006, "workoutName": "Nested HR Target Value"})
	cs := newSession(t, f, nil)

	_, _ = callTool(t, cs, "upload_workout", map[string]any{
		"workout_data": runningWorkoutWithSteps([]any{step}, "Nested HR Target Value"),
	})
	called := uploadStep(t, f)
	if num(t, called["zoneNumber"]) != 3 {
		t.Fatalf("zone = %v", called["zoneNumber"])
	}
	if _, ok := called["targetValueOne"]; ok {
		t.Fatal("step targetValueOne present")
	}
	if _, ok := mapOf(called["targetType"])["targetValueOne"]; ok {
		t.Fatal("nested targetValueOne present")
	}
}

func TestUploadWorkoutPromotesBoundsNestedInsideSecondaryTargetType(t *testing.T) {
	step := distancePaceStepWithNestedBounds()
	step["targetType"] = nil
	step["secondaryTargetType"] = map[string]any{
		"workoutTargetTypeId": 6, "workoutTargetTypeKey": "pace.zone",
		"secondaryTargetValueOne": 0.45, "secondaryTargetValueTwo": 0.6916667,
	}
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, map[string]any{"workoutId": 210007, "workoutName": "Nested Secondary Pace"})
	cs := newSession(t, f, nil)

	_, _ = callTool(t, cs, "upload_workout", map[string]any{
		"workout_data": runningWorkoutWithSteps([]any{step}, "Nested Secondary Pace"),
	})
	called := uploadStep(t, f)
	if num(t, called["secondaryTargetValueOne"]) != 0.45 || num(t, called["secondaryTargetValueTwo"]) != 0.6916667 {
		t.Fatalf("called = %v", called)
	}
	secondary := mapOf(called["secondaryTargetType"])
	if _, ok := secondary["secondaryTargetValueOne"]; ok {
		t.Fatal("nested secondary value left behind")
	}
}

func TestUploadWorkoutRejectsZoneMixedWithCustomRange(t *testing.T) {
	step := distancePaceStepWithNestedBounds()
	step["zoneNumber"] = 3
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "upload_workout", map[string]any{
		"workout_data": runningWorkoutWithSteps([]any{step}, "Ambiguous Pace Target"),
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	if !strings.Contains(text, "mixes zoneNumber=3 with custom range fields") || !strings.Contains(text, "use either a named zone or a custom range") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("POST", workoutPath); len(calls) != 0 {
		t.Fatalf("upload calls = %d", len(calls))
	}
}

func TestUploadWorkoutFixesHrZoneTarget(t *testing.T) {
	workout := map[string]any{
		"workoutName": "HR Zone Workout",
		"sportType":   map[string]any{"sportTypeId": 1, "sportTypeKey": "running"},
		"workoutSegments": []any{map[string]any{
			"segmentOrder": 1,
			"sportType":    map[string]any{"sportTypeId": 1, "sportTypeKey": "running"},
			"workoutSteps": []any{map[string]any{
				"type": "ExecutableStepDTO", "stepOrder": 1,
				"stepType":          map[string]any{"stepTypeId": 3, "stepTypeKey": "interval"},
				"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
				"endConditionValue": 600,
				"targetType":        map[string]any{"workoutTargetTypeId": 4, "workoutTargetTypeKey": "heart.rate.zone"},
				"targetValueOne":    3,
			}},
		}},
	}
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, map[string]any{"workoutId": 123458, "workoutName": "HR Zone Workout"})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "upload_workout", map[string]any{"workout_data": workout})
	sent := uploadStep(t, f)
	if num(t, sent["zoneNumber"]) != 3 {
		t.Fatalf("zone = %v", sent["zoneNumber"])
	}
	if _, ok := sent["targetValueOne"]; ok {
		t.Fatal("targetValueOne present")
	}
	if workoutText(t, text)["status"] != "success" {
		t.Fatalf("text = %q", text)
	}
}

func TestUploadWorkoutFixesHrZoneInRepeatGroup(t *testing.T) {
	workout := map[string]any{
		"workoutName": "Repeat HR Zone",
		"sportType":   map[string]any{"sportTypeId": 1, "sportTypeKey": "running"},
		"workoutSegments": []any{map[string]any{
			"segmentOrder": 1,
			"sportType":    map[string]any{"sportTypeId": 1, "sportTypeKey": "running"},
			"workoutSteps": []any{map[string]any{
				"type": "RepeatGroupDTO", "stepOrder": 1, "numberOfIterations": 2,
				"workoutSteps": []any{
					map[string]any{
						"type": "ExecutableStepDTO", "stepOrder": 1,
						"stepType":          map[string]any{"stepTypeId": 3, "stepTypeKey": "interval"},
						"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
						"endConditionValue": 600,
						"targetType":        map[string]any{"workoutTargetTypeId": 4, "workoutTargetTypeKey": "heart.rate.zone"},
						"targetValueOne":    3,
						"targetValueTwo":    3,
					},
					map[string]any{
						"type": "ExecutableStepDTO", "stepOrder": 2,
						"stepType":          map[string]any{"stepTypeId": 4, "stepTypeKey": "recovery"},
						"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
						"endConditionValue": 240,
						"targetType":        map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"},
					},
				},
			}},
		}},
	}
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, map[string]any{"workoutId": 123459, "workoutName": "Repeat HR Zone"})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "upload_workout", map[string]any{"workout_data": workout})
	interval := uploadStep(t, f, 0)
	if num(t, interval["zoneNumber"]) != 3 {
		t.Fatalf("zone = %v", interval["zoneNumber"])
	}
	if _, ok := interval["targetValueOne"]; ok {
		t.Fatal("targetValueOne present")
	}
	if workoutText(t, text)["status"] != "success" {
		t.Fatalf("text = %q", text)
	}
}

func TestUploadWorkoutRejectsMismatchedEndConditionId(t *testing.T) {
	step := map[string]any{
		"type": "ExecutableStepDTO", "stepOrder": 1,
		"stepType":          map[string]any{"stepTypeId": 4, "stepTypeKey": "recovery"},
		"endCondition":      map[string]any{"conditionTypeId": 4, "conditionTypeKey": "heart.rate"},
		"endConditionValue": 145.0,
		"targetType":        map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"},
	}
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "upload_workout", map[string]any{"workout_data": runningWorkoutWithSteps([]any{step}, "Validation Workout")})
	if !strings.Contains(text, "Error uploading workout") || !strings.Contains(text, "conditionTypeKey 'heart.rate' requires conditionTypeId 6") || !strings.Contains(text, "got 4 (calories)") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("POST", workoutPath); len(calls) != 0 {
		t.Fatalf("upload calls = %d", len(calls))
	}
}

func TestUploadWorkoutRejectsTargetTypeMismatch(t *testing.T) {
	step := timedIntervalStep(map[string]any{"workoutTargetTypeId": 6, "workoutTargetTypeKey": "heart.rate"})
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "upload_workout", map[string]any{"workout_data": runningWorkoutWithSteps([]any{step}, "Bad HR Target")})
	if !strings.Contains(text, "targetType mismatch") || !strings.Contains(text, "workoutTargetTypeId 6 is one of") || !strings.Contains(text, "not 'heart.rate'") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("POST", workoutPath); len(calls) != 0 {
		t.Fatalf("upload calls = %d", len(calls))
	}
}

func TestUploadWorkoutAcceptsHeartRateEndConditionId(t *testing.T) {
	workout := runningWorkoutWithSteps([]any{map[string]any{
		"type": "ExecutableStepDTO", "stepOrder": 1,
		"stepType":          map[string]any{"stepTypeId": 4, "stepTypeKey": "recovery"},
		"endCondition":      map[string]any{"conditionTypeId": 6, "conditionTypeKey": "heart.rate"},
		"endConditionValue": 145.0,
		"targetType":        map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"},
	}}, "Validation Workout")
	expected := deepCopy(t, workout)
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, map[string]any{"workoutId": 123460, "workoutName": "Validation Workout"})
	cs := newSession(t, f, nil)
	_, _ = callTool(t, cs, "upload_workout", map[string]any{"workout_data": workout})
	sameJSON(t, firstUploadBody(t, f), expected)
}

func TestUploadWorkoutAcceptsCustomHrRange(t *testing.T) {
	step := timedIntervalStep(map[string]any{"workoutTargetTypeId": 4, "workoutTargetTypeKey": "heart.rate.zone"})
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, map[string]any{"workoutId": 123460, "workoutName": "Custom HR Range"})
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "upload_workout", map[string]any{"workout_data": runningWorkoutWithSteps([]any{step}, "Custom HR Range")})
	sent := uploadStep(t, f)
	if num(t, sent["targetValueOne"]) != 143 || num(t, sent["targetValueTwo"]) != 157 {
		t.Fatalf("sent = %v", sent)
	}
	if _, ok := sent["zoneNumber"]; ok {
		t.Fatal("zoneNumber present")
	}
	if workoutText(t, text)["status"] != "success" {
		t.Fatalf("text = %q", text)
	}
}

func TestUploadWorkoutRejectsNestedEndConditionMismatch(t *testing.T) {
	group := map[string]any{
		"type": "RepeatGroupDTO", "stepOrder": 1, "numberOfIterations": 2,
		"endCondition": map[string]any{"conditionTypeId": 7, "conditionTypeKey": "iterations"},
		"workoutSteps": []any{map[string]any{
			"type": "ExecutableStepDTO", "stepOrder": 1,
			"stepType":          map[string]any{"stepTypeId": 4, "stepTypeKey": "recovery"},
			"endCondition":      map[string]any{"conditionTypeId": 4, "conditionTypeKey": "heart.rate"},
			"endConditionValue": 145.0,
			"targetType":        map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"},
		}},
	}
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "upload_workout", map[string]any{"workout_data": runningWorkoutWithSteps([]any{group}, "Validation Workout")})
	if !strings.Contains(text, "workoutSegments[0].workoutSteps[0].workoutSteps[0]") || !strings.Contains(text, "conditionTypeKey 'heart.rate' requires conditionTypeId 6") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("POST", workoutPath); len(calls) != 0 {
		t.Fatalf("upload calls = %d", len(calls))
	}
}

func TestUploadWorkoutRejectsNestedTargetTypeMismatch(t *testing.T) {
	bad := timedIntervalStep(map[string]any{"workoutTargetTypeId": 6, "workoutTargetTypeKey": "heart.rate"})
	group := map[string]any{"type": "RepeatGroupDTO", "stepOrder": 1, "numberOfIterations": 2, "workoutSteps": []any{bad}}
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "upload_workout", map[string]any{"workout_data": runningWorkoutWithSteps([]any{group}, "Nested Bad HR Target")})
	if !strings.Contains(text, "workoutSegments[0].workoutSteps[0].workoutSteps[0].targetType mismatch") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("POST", workoutPath); len(calls) != 0 {
		t.Fatalf("upload calls = %d", len(calls))
	}
}

func TestUploadWorkoutRejectsMissingEndConditionId(t *testing.T) {
	step := map[string]any{
		"type": "ExecutableStepDTO", "stepOrder": 1,
		"stepType":          map[string]any{"stepTypeId": 4, "stepTypeKey": "recovery"},
		"endCondition":      map[string]any{"conditionTypeKey": "heart.rate"},
		"endConditionValue": 145.0,
		"targetType":        map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"},
	}
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "upload_workout", map[string]any{"workout_data": runningWorkoutWithSteps([]any{step}, "Validation Workout")})
	if !strings.Contains(text, "conditionTypeKey 'heart.rate' requires conditionTypeId 6") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("POST", workoutPath); len(calls) != 0 {
		t.Fatalf("upload calls = %d", len(calls))
	}
}

func TestUploadWorkoutRejectsSecondaryTargetTypeMismatch(t *testing.T) {
	step := timedIntervalStep(nil)
	step["secondaryTargetType"] = map[string]any{"workoutTargetTypeId": 6, "workoutTargetTypeKey": "heart.rate"}
	step["secondaryTargetValueOne"] = 143
	step["secondaryTargetValueTwo"] = 157
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "upload_workout", map[string]any{"workout_data": runningWorkoutWithSteps([]any{step}, "Bad Secondary HR Target")})
	if !strings.Contains(text, "secondaryTargetType mismatch") || !strings.Contains(text, "workoutTargetTypeId 6 is one of") || !strings.Contains(text, "not 'heart.rate'") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("POST", workoutPath); len(calls) != 0 {
		t.Fatalf("upload calls = %d", len(calls))
	}
}

func TestUploadWorkoutAcceptsSecondaryTargetTypeWithNullPrimary(t *testing.T) {
	step := timedIntervalStep(nil)
	step["secondaryTargetType"] = map[string]any{"workoutTargetTypeId": 6, "workoutTargetTypeKey": "pace.zone"}
	step["secondaryTargetValueOne"] = 0.45
	step["secondaryTargetValueTwo"] = 0.6916667
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, map[string]any{"workoutId": 123461, "workoutName": "Secondary Pace Target"})
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "upload_workout", map[string]any{"workout_data": runningWorkoutWithSteps([]any{step}, "Secondary Pace Target")})
	sent := uploadStep(t, f)
	if sent["targetType"] != nil || mapOf(sent["secondaryTargetType"])["workoutTargetTypeKey"] != "pace.zone" {
		t.Fatalf("sent = %v", sent)
	}
	if workoutText(t, text)["status"] != "success" {
		t.Fatalf("text = %q", text)
	}
}

func TestUploadWorkoutRejectsNestedSecondaryTargetTypeMismatch(t *testing.T) {
	bad := timedIntervalStep(nil)
	bad["secondaryTargetType"] = map[string]any{"workoutTargetTypeId": 6, "workoutTargetTypeKey": "heart.rate"}
	group := map[string]any{"type": "RepeatGroupDTO", "stepOrder": 1, "numberOfIterations": 2, "workoutSteps": []any{bad}}
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "upload_workout", map[string]any{"workout_data": runningWorkoutWithSteps([]any{group}, "Nested Bad Secondary HR Target")})
	if !strings.Contains(text, "workoutSegments[0].workoutSteps[0].workoutSteps[0].secondaryTargetType mismatch") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("POST", workoutPath); len(calls) != 0 {
		t.Fatalf("upload calls = %d", len(calls))
	}
}

func TestUploadCyclingWorkoutPowerBetweenAccepted(t *testing.T) {
	workout := cyclingWorkoutWithSteps([]any{map[string]any{
		"type": "ExecutableStepDTO", "stepOrder": 1,
		"stepType":          map[string]any{"stepTypeId": 3, "stepTypeKey": "interval"},
		"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
		"endConditionValue": 600.0,
		"targetType":        map[string]any{"workoutTargetTypeId": 6, "workoutTargetTypeKey": "power.between"},
		"targetValueOne":    200,
		"targetValueTwo":    250,
	}}, "Cycling Power Between Test")
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, map[string]any{"workoutId": 200001, "workoutName": "Cycling Power Between Test"})
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "upload_workout", map[string]any{"workout_data": workout})
	data := workoutText(t, text)
	if data["status"] != "success" || num(t, data["workout_id"]) != 200001 {
		t.Fatalf("data = %v", data)
	}
	sent := uploadStep(t, f)
	if num(t, mapOf(sent["targetType"])["workoutTargetTypeId"]) != 6 || mapOf(sent["targetType"])["workoutTargetTypeKey"] != "power.between" ||
		num(t, sent["targetValueOne"]) != 200 || num(t, sent["targetValueTwo"]) != 250 {
		t.Fatalf("sent = %v", sent)
	}
}

func TestUploadCyclingWorkoutPowerZoneAccepted(t *testing.T) {
	workout := cyclingWorkoutWithSteps([]any{map[string]any{
		"type": "ExecutableStepDTO", "stepOrder": 1,
		"stepType":          map[string]any{"stepTypeId": 3, "stepTypeKey": "interval"},
		"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
		"endConditionValue": 1200.0,
		"targetType":        map[string]any{"workoutTargetTypeId": 2, "workoutTargetTypeKey": "power.zone"},
		"zoneNumber":        3,
	}}, "Cycling Power Zone Test")
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, map[string]any{"workoutId": 200002, "workoutName": "Cycling Power Zone Test"})
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "upload_workout", map[string]any{"workout_data": workout})
	if workoutText(t, text)["status"] != "success" {
		t.Fatalf("text = %q", text)
	}
	sent := uploadStep(t, f)
	if num(t, mapOf(sent["targetType"])["workoutTargetTypeId"]) != 2 || mapOf(sent["targetType"])["workoutTargetTypeKey"] != "power.zone" || num(t, sent["zoneNumber"]) != 3 {
		t.Fatalf("sent = %v", sent)
	}
}

func TestUploadCyclingWorkoutWrongIdForPowerBetweenRejected(t *testing.T) {
	workout := cyclingWorkoutWithSteps([]any{map[string]any{
		"type": "ExecutableStepDTO", "stepOrder": 1,
		"stepType":          map[string]any{"stepTypeId": 3, "stepTypeKey": "interval"},
		"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
		"endConditionValue": 600.0,
		"targetType":        map[string]any{"workoutTargetTypeId": 2, "workoutTargetTypeKey": "power.between"},
		"targetValueOne":    200,
		"targetValueTwo":    250,
	}}, "Bad Power Between")
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "upload_workout", map[string]any{"workout_data": workout})
	if !strings.Contains(text, "targetType mismatch") || !strings.Contains(text, "workoutTargetTypeId 2 is 'power.zone', not 'power.between'") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("POST", workoutPath); len(calls) != 0 {
		t.Fatalf("upload calls = %d", len(calls))
	}
}

func TestUploadCyclingWorkoutWrongIdForPowerZoneRejected(t *testing.T) {
	workout := cyclingWorkoutWithSteps([]any{map[string]any{
		"type": "ExecutableStepDTO", "stepOrder": 1,
		"stepType":          map[string]any{"stepTypeId": 3, "stepTypeKey": "interval"},
		"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
		"endConditionValue": 600.0,
		"targetType":        map[string]any{"workoutTargetTypeId": 6, "workoutTargetTypeKey": "power.zone"},
		"zoneNumber":        3,
	}}, "Bad Power Zone")
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "upload_workout", map[string]any{"workout_data": workout})
	if !strings.Contains(text, "targetType mismatch") || !strings.Contains(text, "workoutTargetTypeId 6 is one of") || !strings.Contains(text, "not 'power.zone'") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("POST", workoutPath); len(calls) != 0 {
		t.Fatalf("upload calls = %d", len(calls))
	}
}

func TestUploadCyclingWorkoutPowerBetweenInRepeatGroup(t *testing.T) {
	workout := cyclingWorkoutWithSteps([]any{
		map[string]any{
			"type": "ExecutableStepDTO", "stepOrder": 1,
			"stepType":          map[string]any{"stepTypeId": 1, "stepTypeKey": "warmup"},
			"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
			"endConditionValue": 600.0,
			"targetType":        map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"},
		},
		map[string]any{
			"type": "RepeatGroupDTO", "stepOrder": 2, "numberOfIterations": 4,
			"endCondition":      map[string]any{"conditionTypeId": 7, "conditionTypeKey": "iterations"},
			"endConditionValue": 4.0,
			"workoutSteps": []any{
				map[string]any{
					"type": "ExecutableStepDTO", "stepOrder": 1,
					"stepType":          map[string]any{"stepTypeId": 3, "stepTypeKey": "interval"},
					"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
					"endConditionValue": 300.0,
					"targetType":        map[string]any{"workoutTargetTypeId": 6, "workoutTargetTypeKey": "power.between"},
					"targetValueOne":    250,
					"targetValueTwo":    300,
				},
				map[string]any{
					"type": "ExecutableStepDTO", "stepOrder": 2,
					"stepType":          map[string]any{"stepTypeId": 4, "stepTypeKey": "recovery"},
					"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
					"endConditionValue": 120.0,
					"targetType":        map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"},
				},
			},
		},
		map[string]any{
			"type": "ExecutableStepDTO", "stepOrder": 3,
			"stepType":          map[string]any{"stepTypeId": 2, "stepTypeKey": "cooldown"},
			"endCondition":      map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time"},
			"endConditionValue": 600.0,
			"targetType":        map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"},
		},
	}, "Cycling Intervals Power Between")
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, map[string]any{"workoutId": 200003, "workoutName": "Cycling Intervals Power Between"})
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "upload_workout", map[string]any{"workout_data": workout})
	if workoutText(t, text)["status"] != "success" {
		t.Fatalf("text = %q", text)
	}
	body := firstUploadBody(t, f)
	topSteps := listOf(mapOf(listOf(body["workoutSegments"])[0])["workoutSteps"])
	interval := mapOf(listOf(mapOf(topSteps[1])["workoutSteps"])[0])
	if num(t, mapOf(interval["targetType"])["workoutTargetTypeId"]) != 6 || mapOf(interval["targetType"])["workoutTargetTypeKey"] != "power.between" ||
		num(t, interval["targetValueOne"]) != 250 || num(t, interval["targetValueTwo"]) != 300 {
		t.Fatalf("interval = %v", interval)
	}
}

func TestUploadWorkoutException(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, status(500, map[string]any{"message": "Upload failed"}))
	cs := newSession(t, f, nil)
	text, isErr := callTool(t, cs, "upload_workout", map[string]any{"workout_data": map[string]any{}})
	if !isErr || !strings.Contains(text, "Upload failed") {
		t.Fatalf("text = %q isErr=%v", text, isErr)
	}
}

// --- scheduled workouts ---

func TestGetScheduledWorkoutsTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+graphqlPath, map[string]any{"data": map[string]any{"workoutScheduleSummariesScalar": []any{
		map[string]any{
			"workoutUuid": "abc-123-def", "workoutId": 123456, "workoutName": "5K Tempo Run",
			"workoutType": "running", "scheduleDate": "2024-01-15", "tpPlanName": "5K Training Plan",
			"associatedActivityId": nil, "estimatedDurationInSecs": 1800, "estimatedDistanceInMeters": 5000.0,
		},
	}}})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_scheduled_workouts", map[string]any{"start_date": "2024-01-08", "end_date": "2024-01-15"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := workoutText(t, text)
	if num(t, data["count"]) != 1 {
		t.Fatalf("data = %v", data)
	}
	workout := mapOf(listOf(data["scheduled_workouts"])[0])
	if workout["name"] != "5K Tempo Run" || workout["sport"] != "running" || workout["completed"] != false ||
		workout["training_plan"] != "5K Training Plan" || num(t, workout["estimated_duration_seconds"]) != 1800 {
		t.Fatalf("workout = %v", workout)
	}
	if calls := f.callsMatching("POST", graphqlPath); len(calls) != 1 {
		t.Fatalf("graphql calls = %d", len(calls))
	}
}

func TestGetScheduledWorkoutsPreservesManualShapeAndAddsPlanIds(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+graphqlPath, map[string]any{"data": map[string]any{"workoutScheduleSummariesScalar": []any{
		map[string]any{
			"scheduledWorkoutId": 1001, "workoutUuid": nil, "workoutId": 2001, "workoutName": "Manual Ride",
			"workoutType": "cycling", "scheduleDate": "2024-01-15", "associatedActivityId": nil,
			"trainingPlanId": nil, "fbtAdaptivePlanId": nil, "tpType": nil,
		},
		map[string]any{
			"scheduledWorkoutId": nil, "workoutUuid": "abc-123-def", "workoutId": nil, "workoutName": "Coach Ride",
			"workoutType": "cycling", "scheduleDate": "2024-01-16", "associatedActivityId": nil,
			"trainingPlanId": 3001, "fbtAdaptivePlanId": 3001, "tpType": nil,
		},
	}}})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_scheduled_workouts", map[string]any{"start_date": "2024-01-15", "end_date": "2024-01-16"})
	scheduled := listOf(workoutText(t, text)["scheduled_workouts"])
	sameJSON(t, scheduled[0], map[string]any{
		"date": "2024-01-15", "scheduled_workout_id": 1001, "workout_id": 2001,
		"name": "Manual Ride", "sport": "cycling", "completed": false,
	})
	second := mapOf(scheduled[1])
	if num(t, second["training_plan_id"]) != 3001 || num(t, second["fbt_adaptive_plan_id"]) != 3001 {
		t.Fatalf("second = %v", second)
	}
	if _, ok := second["tp_type"]; ok {
		t.Fatal("tp_type present")
	}
}

func TestGetScheduledWorkoutsExposesScheduledId(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+graphqlPath, map[string]any{"data": map[string]any{"workoutScheduleSummariesScalar": []any{
		map[string]any{
			"scheduledWorkoutId": 555, "workoutUuid": nil, "workoutId": 123456, "workoutName": "5K Tempo Run",
			"workoutType": "running", "scheduleDate": "2024-01-15", "associatedActivityId": nil,
		},
	}}})
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "get_scheduled_workouts", map[string]any{"start_date": "2024-01-08", "end_date": "2024-01-15"})
	workout := mapOf(listOf(workoutText(t, text)["scheduled_workouts"])[0])
	if num(t, workout["scheduled_workout_id"]) != 555 || num(t, workout["workout_id"]) != 123456 {
		t.Fatalf("workout = %v", workout)
	}
}

func TestGetScheduledWorkoutsHandlesNullGraphqlData(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+graphqlPath, map[string]any{"data": nil})
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "get_scheduled_workouts", map[string]any{"start_date": "2024-01-08", "end_date": "2024-01-15"})
	if strings.Contains(text, "NoneType") || !strings.Contains(text, "No workouts scheduled") {
		t.Fatalf("text = %q", text)
	}
}

// --- Garmin Coach / training plan ---

func coachResponse() map[string]any {
	return map[string]any{"data": map[string]any{"trainingPlanScalar": map[string]any{
		"trainingPlanWorkoutScheduleDTOS": []any{map[string]any{
			"trainingPlanId": 12345, "planName": "5K Training Plan", "trainingPlanClassification": "FBT_ADAPTIVE",
			"trainingPlanDetailsDTO": map[string]any{
				"athletePlanId": 12345, "workoutsPerWeek": 4, "remainingAtpWorkoutsForWeek": 2,
				"trainingType": "RUNNING", "registrationDate": "2024-01-01", "planCompleted": false,
				"athleteRace": map[string]any{"raceName": "City 5K", "raceDay": "2024-03-10"},
			},
			"workoutScheduleSummaries": []any{
				map[string]any{
					"workoutUuid": "abc-123-def", "workoutId": nil, "workoutName": "Base Run",
					"workoutType": "running", "scheduleDate": "2024-01-15", "tpPlanName": "5K Training Plan",
					"associatedActivityId": nil, "estimatedDurationInSecs": 1800,
					"trainingPlanId": 12345, "fbtAdaptivePlanId": 12345, "tpType": nil,
				},
				map[string]any{
					"workoutUuid": "xyz-456-ghi", "workoutId": nil, "workoutName": "Strength",
					"workoutType": "strength_training", "scheduleDate": "2024-01-15", "tpPlanName": "5K Training Plan",
					"associatedActivityId": 987654, "estimatedDurationInSecs": 1200,
					"trainingPlanId": 12345, "fbtAdaptivePlanId": 12345, "tpType": nil,
				},
			},
		}},
	}}}
}

func TestGetGarminCoachWorkoutTools(t *testing.T) {
	for _, tool := range []string{"get_garmin_coach_workouts", "get_training_plan_workouts"} {
		t.Run(tool, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on("POST "+graphqlPath, coachResponse())
			cs := newSession(t, f, nil)
			text, isErr := callTool(t, cs, tool, map[string]any{"calendar_date": "2024-01-15"})
			if isErr {
				t.Fatalf("unexpected error: %q", text)
			}
			data := workoutText(t, text)
			if data["date"] != "2024-01-15" || num(t, data["count"]) != 2 {
				t.Fatalf("data = %v", data)
			}
			sameJSON(t, data["training_plans"], []any{"5K Training Plan"})
			plans := listOf(data["plans"])
			if len(plans) != 1 {
				t.Fatalf("plans = %v", plans)
			}
			plan := mapOf(plans[0])
			if plan["name"] != "5K Training Plan" || num(t, plan["training_plan_id"]) != 12345 ||
				plan["classification"] != "FBT_ADAPTIVE" || plan["training_type"] != "RUNNING" ||
				plan["race_name"] != "City 5K" || plan["race_day"] != "2024-03-10" ||
				num(t, plan["workouts_per_week"]) != 4 || num(t, plan["remaining_workouts_this_week"]) != 2 ||
				plan["registration_date"] != "2024-01-01" || plan["plan_completed"] != false {
				t.Fatalf("plan = %v", plan)
			}
			workouts := listOf(data["workouts"])
			first, second := mapOf(workouts[0]), mapOf(workouts[1])
			if first["name"] != "Base Run" || first["sport"] != "running" || first["completed"] != false ||
				num(t, first["training_plan_id"]) != 12345 || num(t, first["fbt_adaptive_plan_id"]) != 12345 {
				t.Fatalf("first = %v", first)
			}
			if second["name"] != "Strength" || second["sport"] != "strength_training" ||
				num(t, second["training_plan_id"]) != 12345 || num(t, second["fbt_adaptive_plan_id"]) != 12345 ||
				second["completed"] != true || num(t, second["activity_id"]) != 987654 {
				t.Fatalf("second = %v", second)
			}
			if _, ok := first["tp_type"]; ok {
				t.Fatal("tp_type present")
			}
			if calls := f.callsMatching("POST", graphqlPath); len(calls) != 1 {
				t.Fatalf("graphql calls = %d", len(calls))
			}
		})
	}
}

func TestGetGarminCoachWorkoutsStpNumericIds(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+graphqlPath, map[string]any{"data": map[string]any{"trainingPlanScalar": map[string]any{
		"trainingPlanWorkoutScheduleDTOS": []any{map[string]any{
			"trainingPlanId": 67890, "planName": "Push, Pull, Legs", "trainingPlanClassification": "STP",
			"trainingPlanDetailsDTO": map[string]any{"athletePlanId": 67890, "trainingType": "STRENGTH"},
			"workoutScheduleSummaries": []any{map[string]any{
				"scheduledWorkoutId": 111, "workoutUuid": nil, "workoutId": 222, "workoutName": "Leg Day",
				"workoutType": "strength_training", "scheduleDate": "2024-01-15", "tpPlanName": "Push, Pull, Legs",
				"associatedActivityId": nil, "trainingPlanId": 67890, "fbtAdaptivePlanId": nil, "tpType": "STP",
			}},
		}},
	}}})
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "get_garmin_coach_workouts", map[string]any{"calendar_date": "2024-01-15"})
	data := workoutText(t, text)
	sameJSON(t, data["training_plans"], []any{"Push, Pull, Legs"})
	sameJSON(t, data["plans"], []any{map[string]any{
		"name": "Push, Pull, Legs", "training_plan_id": 67890,
		"classification": "STP", "training_type": "STRENGTH",
	}})
	workout := mapOf(listOf(data["workouts"])[0])
	if num(t, workout["workout_id"]) != 222 || workout["tp_type"] != "STP" || num(t, workout["training_plan_id"]) != 67890 {
		t.Fatalf("workout = %v", workout)
	}
	if _, ok := workout["workout_uuid"]; ok {
		t.Fatal("workout_uuid present")
	}
	if _, ok := workout["fbt_adaptive_plan_id"]; ok {
		t.Fatal("fbt_adaptive_plan_id present")
	}
}

func TestGetGarminCoachWorkoutsIncludesRestDays(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+graphqlPath, map[string]any{"data": map[string]any{"trainingPlanScalar": map[string]any{
		"trainingPlanWorkoutScheduleDTOS": []any{map[string]any{
			"trainingPlanId": 12345, "planName": "Adaptive Plan", "trainingPlanClassification": "FBT_ADAPTIVE",
			"trainingPlanDetailsDTO": map[string]any{"trainingType": "CYCLING"},
			"workoutScheduleSummaries": []any{map[string]any{
				"workoutUuid": "rest-123", "workoutId": nil, "workoutName": nil, "workoutType": nil,
				"scheduleDate": "2024-01-15", "associatedActivityId": nil,
				"trainingPlanId": 12345, "fbtAdaptivePlanId": 12345, "tpType": nil, "isRestDay": true,
			}},
		}},
	}}})
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "get_garmin_coach_workouts", map[string]any{"calendar_date": "2024-01-15"})
	data := workoutText(t, text)
	if num(t, data["count"]) != 1 {
		t.Fatalf("count = %v", data["count"])
	}
	rest := mapOf(listOf(data["workouts"])[0])
	if rest["workout_uuid"] != "rest-123" || rest["is_rest_day"] != true ||
		num(t, rest["training_plan_id"]) != 12345 || num(t, rest["fbt_adaptive_plan_id"]) != 12345 {
		t.Fatalf("rest = %v", rest)
	}
	if _, ok := rest["name"]; ok {
		t.Fatal("name present")
	}
	if _, ok := rest["sport"]; ok {
		t.Fatal("sport present")
	}
}

func TestGetGarminCoachWorkoutsHandlesMalformedPlanEntries(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+graphqlPath, map[string]any{"data": map[string]any{"trainingPlanScalar": map[string]any{
		"trainingPlanWorkoutScheduleDTOS": []any{
			nil,
			map[string]any{
				"planName": "Adaptive Plan", "trainingPlanDetailsDTO": []any{},
				"workoutScheduleSummaries": []any{
					nil,
					map[string]any{
						"workoutUuid": "abc-123", "workoutName": "Base Run", "workoutType": "running",
						"scheduleDate": "2024-01-15", "associatedActivityId": nil,
					},
				},
			},
		},
	}}})
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "get_garmin_coach_workouts", map[string]any{"calendar_date": "2024-01-15"})
	data := workoutText(t, text)
	sameJSON(t, data["training_plans"], []any{"Adaptive Plan"})
	if num(t, data["count"]) != 1 || mapOf(listOf(data["workouts"])[0])["workout_uuid"] != "abc-123" {
		t.Fatalf("data = %v", data)
	}
}

func TestGetGarminCoachWorkoutsHandlesMissingPlanData(t *testing.T) {
	cases := []struct {
		name     string
		response any
		want     string
	}{
		{"null response", nil, "No training plan data found or error querying data."},
		{"null data", map[string]any{"data": nil}, "No training plan data found or error querying data."},
		{"null plan", map[string]any{"data": map[string]any{"trainingPlanScalar": nil}}, "No training plan workouts scheduled for 2024-01-15."},
		{"empty list", map[string]any{"data": map[string]any{"trainingPlanScalar": map[string]any{"trainingPlanWorkoutScheduleDTOS": []any{}}}}, "No training plan workouts scheduled for 2024-01-15."},
		{"null entry", map[string]any{"data": map[string]any{"trainingPlanScalar": map[string]any{"trainingPlanWorkoutScheduleDTOS": []any{nil}}}}, "No training plan workouts scheduled for 2024-01-15."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on("POST "+graphqlPath, tc.response)
			cs := newSession(t, f, nil)
			text, _ := callTool(t, cs, "get_garmin_coach_workouts", map[string]any{"calendar_date": "2024-01-15"})
			if text != tc.want {
				t.Fatalf("text = %q, want %q", text, tc.want)
			}
		})
	}
}

func TestGetGarminCoachWorkoutsRejectsInvalidDate(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "get_garmin_coach_workouts", map[string]any{"calendar_date": "2024-01-15-invalid"})
	want := "Error retrieving Garmin Coach workouts: Invalid calendar_date '2024-01-15-invalid': expected YYYY-MM-DD"
	if text != want {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("POST", graphqlPath); len(calls) != 0 {
		t.Fatalf("graphql calls = %d", len(calls))
	}
}

func TestGetTrainingPlanWorkoutsNoActivePlan(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+graphqlPath, map[string]any{"data": map[string]any{"trainingPlanScalar": nil}})
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "get_training_plan_workouts", map[string]any{"calendar_date": "2024-01-15"})
	if text != "No training plan workouts scheduled for 2024-01-15." {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("POST", graphqlPath); len(calls) != 1 {
		t.Fatalf("graphql calls = %d", len(calls))
	}
}

func TestGetTrainingPlanWorkoutsNullScheduleSummaries(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+graphqlPath, map[string]any{"data": map[string]any{"trainingPlanScalar": map[string]any{
		"trainingPlanWorkoutScheduleDTOS": []any{map[string]any{
			"planName": "Completed Garmin Run Coach Plan", "workoutScheduleSummaries": nil,
		}},
	}}})
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "get_training_plan_workouts", map[string]any{"calendar_date": "2024-01-15"})
	data := workoutText(t, text)
	sameJSON(t, data["training_plans"], []any{"Completed Garmin Run Coach Plan"})
	if num(t, data["count"]) != 0 {
		t.Fatalf("data = %v", data)
	}
	if len(listOf(data["workouts"])) != 0 {
		t.Fatalf("workouts = %v", data["workouts"])
	}
}

// --- delete_workout ---

func previewWorkoutRoute(f *fakeGarmin, id int) {
	f.on("GET "+fmt.Sprintf("%s/%d", workoutPath, id), map[string]any{
		"workoutId": id, "workoutName": "Easy Run", "sportType": map[string]any{"sportTypeKey": "running"},
	})
}

func TestDeleteWorkoutRequiresConfirmation(t *testing.T) {
	f := newFakeGarmin(t)
	previewWorkoutRoute(f, 123456)
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "delete_workout", map[string]any{"workout_id": 123456})
	data := workoutText(t, text)
	if data["status"] != "needs_confirmation" || mapOf(data["workout"])["name"] != "Easy Run" {
		t.Fatalf("data = %v", data)
	}
	if calls := f.callsMatching("DELETE", workoutPath+"/123456"); len(calls) != 0 {
		t.Fatalf("delete calls = %d", len(calls))
	}
}

func TestDeleteWorkoutSuccess(t *testing.T) {
	f := newFakeGarmin(t)
	previewWorkoutRoute(f, 123456)
	f.on("DELETE "+workoutPath+"/123456", map[string]any{})
	cs := newSession(t, f, nil)
	text, isErr := callTool(t, cs, "delete_workout", map[string]any{"workout_id": 123456, "confirm": true})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := workoutText(t, text)
	if data["status"] != "success" || num(t, data["workout_id"]) != 123456 || !strings.Contains(data["message"].(string), "deleted successfully") {
		t.Fatalf("data = %v", data)
	}
	if calls := f.callsMatching("DELETE", workoutPath+"/123456"); len(calls) != 1 {
		t.Fatalf("delete calls = %d", len(calls))
	}
}

func TestDeleteWorkoutFailure(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+workoutPath+"/999999", map[string]any{"workoutId": 999999})
	f.on("DELETE "+workoutPath+"/999999", status(404, map[string]any{"message": "API Error 404"}))
	cs := newSession(t, f, nil)
	text, isErr := callTool(t, cs, "delete_workout", map[string]any{"workout_id": 999999, "confirm": true})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	data := workoutText(t, text)
	if data["tool"] != "delete_workout" || data["write_committed"] != false || !strings.Contains(data["message"].(string), "404") {
		t.Fatalf("data = %v", data)
	}
}

func TestDeleteWorkoutException(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+workoutPath+"/123456", map[string]any{"workoutId": 123456})
	f.on("DELETE "+workoutPath+"/123456", status(500, map[string]any{"message": "Network error"}))
	cs := newSession(t, f, nil)
	text, isErr := callTool(t, cs, "delete_workout", map[string]any{"workout_id": 123456, "confirm": true})
	if !isErr || !strings.Contains(text, "Network error") {
		t.Fatalf("text = %q isErr=%v", text, isErr)
	}
}

func TestDeleteWorkoutHandlesNullSportType(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+workoutPath+"/123456", map[string]any{"workoutId": 123456, "workoutName": "Imported", "sportType": nil})
	f.on("DELETE "+workoutPath+"/123456", map[string]any{})
	cs := newSession(t, f, nil)
	text, isErr := callTool(t, cs, "delete_workout", map[string]any{"workout_id": 123456, "confirm": true})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := workoutText(t, text)
	if data["status"] != "success" || mapOf(data["workout"])["name"] != "Imported" {
		t.Fatalf("data = %v", data)
	}
}

// --- delete_workouts ---

func TestDeleteWorkoutsRequiresConfirmation(t *testing.T) {
	f := newFakeGarmin(t)
	previewWorkoutRoute(f, 123456)
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "delete_workouts", map[string]any{"workout_ids": []any{123456}})
	data := workoutText(t, text)
	if data["status"] != "needs_confirmation" || num(t, data["total"]) != 1 || mapOf(listOf(data["workouts"])[0])["name"] != "Easy Run" {
		t.Fatalf("data = %v", data)
	}
	if calls := f.callsMatching("DELETE", workoutPath+"/123456"); len(calls) != 0 {
		t.Fatalf("delete calls = %d", len(calls))
	}
}

func TestDeleteWorkoutsSingle(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+workoutPath+"/123456", map[string]any{"workoutId": 123456})
	f.on("DELETE "+workoutPath+"/123456", map[string]any{})
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "delete_workouts", map[string]any{"workout_ids": []any{123456}, "confirm": true})
	data := workoutText(t, text)
	if num(t, data["total"]) != 1 || num(t, data["succeeded"]) != 1 || num(t, data["failed"]) != 0 {
		t.Fatalf("data = %v", data)
	}
	entry := mapOf(listOf(data["results"])[0])
	if entry["status"] != "success" || num(t, entry["workout_id"]) != 123456 {
		t.Fatalf("entry = %v", entry)
	}
}

func TestDeleteWorkoutsMultiple(t *testing.T) {
	f := newFakeGarmin(t)
	for _, id := range []int{111, 222, 333} {
		f.on("GET "+fmt.Sprintf("%s/%d", workoutPath, id), map[string]any{"workoutId": id})
		f.on("DELETE "+fmt.Sprintf("%s/%d", workoutPath, id), map[string]any{})
	}
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "delete_workouts", map[string]any{"workout_ids": []any{111, 222, 333}, "confirm": true})
	data := workoutText(t, text)
	if num(t, data["total"]) != 3 || num(t, data["succeeded"]) != 3 || num(t, data["failed"]) != 0 {
		t.Fatalf("data = %v", data)
	}
}

func TestDeleteWorkoutsPartialFailure(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+workoutPath+"/111", map[string]any{"workoutId": 111})
	f.on("GET "+workoutPath+"/999", map[string]any{"workoutId": 999})
	f.on("DELETE "+workoutPath+"/111", map[string]any{})
	f.on("DELETE "+workoutPath+"/999", status(404, map[string]any{"message": "API Error 404"}))
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "delete_workouts", map[string]any{"workout_ids": []any{111, 999}, "confirm": true})
	data := workoutText(t, text)
	if num(t, data["total"]) != 2 || num(t, data["succeeded"]) != 1 || num(t, data["failed"]) != 1 {
		t.Fatalf("data = %v", data)
	}
	results := listOf(data["results"])
	if mapOf(results[0])["status"] != "success" || mapOf(results[1])["status"] != "error" ||
		!strings.Contains(mapOf(results[1])["message"].(string), "404") {
		t.Fatalf("results = %v", results)
	}
}

func TestDeleteWorkoutsException(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+workoutPath+"/123456", map[string]any{"workoutId": 123456})
	f.on("DELETE "+workoutPath+"/123456", status(500, map[string]any{"message": "Network error"}))
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "delete_workouts", map[string]any{"workout_ids": []any{123456}, "confirm": true})
	data := workoutText(t, text)
	if num(t, data["total"]) != 1 || num(t, data["succeeded"]) != 0 || num(t, data["failed"]) != 1 {
		t.Fatalf("data = %v", data)
	}
	entry := mapOf(listOf(data["results"])[0])
	if entry["status"] != "error" || !strings.Contains(entry["message"].(string), "Network error") {
		t.Fatalf("entry = %v", entry)
	}
}

// --- upload_workouts ---

func TestUploadWorkoutsSingle(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, map[string]any{"workoutId": 111, "workoutName": "Easy Run"})
	cs := newSession(t, f, nil)
	text, isErr := callTool(t, cs, "upload_workouts", map[string]any{"workouts": []any{
		map[string]any{"workoutName": "Easy Run", "sportType": map[string]any{"sportTypeId": 1, "sportTypeKey": "running"}},
	}})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := workoutText(t, text)
	if num(t, data["total"]) != 1 || num(t, data["succeeded"]) != 1 || num(t, data["failed"]) != 0 {
		t.Fatalf("data = %v", data)
	}
	entry := mapOf(listOf(data["results"])[0])
	if entry["status"] != "success" || num(t, entry["workout_id"]) != 111 || entry["name"] != "Easy Run" {
		t.Fatalf("entry = %v", entry)
	}
}

func TestUploadWorkoutsPromotesBoundsNestedInsideTargetType(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, map[string]any{"workoutId": 210003, "workoutName": "Issue 210 Batch"})
	cs := newSession(t, f, nil)
	_, _ = callTool(t, cs, "upload_workouts", map[string]any{"workouts": []any{
		runningWorkoutWithSteps([]any{distancePaceStepWithNestedBounds()}, "Issue 210 Batch"),
	}})
	called := uploadStep(t, f)
	if num(t, called["targetValueOne"]) != 2.0833333 || num(t, called["targetValueTwo"]) != 1.9607843 {
		t.Fatalf("called = %v", called)
	}
	if _, ok := mapOf(called["targetType"])["targetValueOne"]; ok {
		t.Fatal("nested bound left behind")
	}
}

func TestUploadWorkoutsMultiple(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, sequence{
		map[string]any{"workoutId": 111, "workoutName": "Easy Run"},
		map[string]any{"workoutId": 222, "workoutName": "Tempo Run"},
		map[string]any{"workoutId": 333, "workoutName": "Long Run"},
	})
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "upload_workouts", map[string]any{"workouts": []any{
		map[string]any{"workoutName": "Easy Run"},
		map[string]any{"workoutName": "Tempo Run"},
		map[string]any{"workoutName": "Long Run"},
	}})
	data := workoutText(t, text)
	if num(t, data["total"]) != 3 || num(t, data["succeeded"]) != 3 || num(t, data["failed"]) != 0 {
		t.Fatalf("data = %v", data)
	}
}

func TestUploadWorkoutsPartialFailure(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, sequence{
		map[string]any{"workoutId": 111, "workoutName": "Easy Run"},
		status(500, map[string]any{"message": "API error"}),
	})
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "upload_workouts", map[string]any{"workouts": []any{
		map[string]any{"workoutName": "Easy Run"},
		map[string]any{"workoutName": "Bad Workout"},
	}})
	data := workoutText(t, text)
	if num(t, data["total"]) != 2 || num(t, data["succeeded"]) != 1 || num(t, data["failed"]) != 1 {
		t.Fatalf("data = %v", data)
	}
	results := listOf(data["results"])
	second := mapOf(results[1])
	if mapOf(results[0])["status"] != "success" || second["status"] != "error" ||
		!strings.Contains(second["message"].(string), "API error") || second["name"] != "Bad Workout" {
		t.Fatalf("results = %v", results)
	}
}

func TestUploadWorkoutsReportsEndConditionValidationError(t *testing.T) {
	valid := runningWorkoutWithSteps([]any{map[string]any{
		"type": "ExecutableStepDTO", "stepOrder": 1,
		"stepType":          map[string]any{"stepTypeId": 4, "stepTypeKey": "recovery"},
		"endCondition":      map[string]any{"conditionTypeId": 6, "conditionTypeKey": "heart.rate"},
		"endConditionValue": 145.0,
		"targetType":        map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"},
	}}, "Valid HR Workout")
	invalid := runningWorkoutWithSteps([]any{map[string]any{
		"type": "ExecutableStepDTO", "stepOrder": 1,
		"stepType":          map[string]any{"stepTypeId": 4, "stepTypeKey": "recovery"},
		"endCondition":      map[string]any{"conditionTypeId": 4, "conditionTypeKey": "heart.rate"},
		"endConditionValue": 145.0,
		"targetType":        map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"},
	}}, "Invalid HR Workout")
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, map[string]any{"workoutId": 111, "workoutName": "Valid HR Workout"})
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "upload_workouts", map[string]any{"workouts": []any{valid, invalid}})
	data := workoutText(t, text)
	if num(t, data["total"]) != 2 || num(t, data["succeeded"]) != 1 || num(t, data["failed"]) != 1 {
		t.Fatalf("data = %v", data)
	}
	second := mapOf(listOf(data["results"])[1])
	if second["status"] != "error" || second["name"] != "Invalid HR Workout" ||
		!strings.Contains(second["message"].(string), "conditionTypeKey 'heart.rate' requires conditionTypeId 6") {
		t.Fatalf("second = %v", second)
	}
	sameJSON(t, firstUploadBody(t, f), valid)
}

func TestUploadWorkoutsRejectsTargetTypeMismatch(t *testing.T) {
	good := runningWorkoutWithSteps([]any{timedIntervalStep(map[string]any{"workoutTargetTypeId": 4, "workoutTargetTypeKey": "heart.rate.zone"})}, "Good HR Range")
	bad := runningWorkoutWithSteps([]any{timedIntervalStep(map[string]any{"workoutTargetTypeId": 6, "workoutTargetTypeKey": "heart.rate"})}, "Bad HR Target")
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, map[string]any{"workoutId": 111, "workoutName": "Good HR Range"})
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "upload_workouts", map[string]any{"workouts": []any{good, bad}})
	data := workoutText(t, text)
	if num(t, data["succeeded"]) != 1 || num(t, data["failed"]) != 1 {
		t.Fatalf("data = %v", data)
	}
	second := mapOf(listOf(data["results"])[1])
	if second["status"] != "error" || !strings.Contains(second["message"].(string), "targetType mismatch") {
		t.Fatalf("second = %v", second)
	}
}

// --- schedule_workout / schedule_workouts ---

func TestScheduleWorkoutAcceptsAllSuccessStatuses(t *testing.T) {
	for _, tool := range []string{"schedule_workout", "schedule_workouts"} {
		for _, code := range []int{201, 202, 204} {
			t.Run(fmt.Sprintf("%s/%d", tool, code), func(t *testing.T) {
				f := newFakeGarmin(t)
				emptyScheduleGraphQL(f)
				f.on("POST "+schedulePath+"/123", status(code, nil))
				cs := newSession(t, f, nil)
				var args map[string]any
				if tool == "schedule_workout" {
					args = map[string]any{"workout_id": 123, "calendar_date": "2026-10-20"}
				} else {
					args = map[string]any{"schedules": []any{map[string]any{"workout_id": 123, "calendar_date": "2026-10-20"}}}
				}
				text, isErr := callTool(t, cs, tool, args)
				if isErr {
					t.Fatalf("unexpected error: %q", text)
				}
				data := workoutText(t, text)
				status := data["status"]
				if status == nil {
					status = mapOf(listOf(data["results"])[0])["status"]
				}
				if status != "success" {
					t.Fatalf("data = %v", data)
				}
				if calls := f.callsMatching("POST", schedulePath+"/123"); len(calls) != 1 {
					t.Fatalf("post calls = %d", len(calls))
				}
			})
		}
	}
}

func TestScheduleWorkoutsSingle(t *testing.T) {
	f := newFakeGarmin(t)
	emptyScheduleGraphQL(f)
	f.on("POST "+schedulePath+"/123456", status(200, nil))
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "schedule_workouts", map[string]any{"schedules": []any{
		map[string]any{"workout_id": 123456, "calendar_date": "2024-01-15"},
	}})
	data := workoutText(t, text)
	if num(t, data["total"]) != 1 || num(t, data["succeeded"]) != 1 || num(t, data["failed"]) != 0 {
		t.Fatalf("data = %v", data)
	}
	entry := mapOf(listOf(data["results"])[0])
	if entry["status"] != "success" || num(t, entry["workout_id"]) != 123456 || entry["scheduled_date"] != "2024-01-15" {
		t.Fatalf("entry = %v", entry)
	}
}

func TestScheduleWorkoutsMultiple(t *testing.T) {
	f := newFakeGarmin(t)
	emptyScheduleGraphQL(f)
	for _, id := range []int{111, 222, 333} {
		f.on("POST "+fmt.Sprintf("%s/%d", schedulePath, id), status(200, nil))
	}
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "schedule_workouts", map[string]any{"schedules": []any{
		map[string]any{"workout_id": 111, "calendar_date": "2024-01-15"},
		map[string]any{"workout_id": 222, "calendar_date": "2024-01-17"},
		map[string]any{"workout_id": 333, "calendar_date": "2024-01-19"},
	}})
	data := workoutText(t, text)
	if num(t, data["total"]) != 3 || num(t, data["succeeded"]) != 3 || num(t, data["failed"]) != 0 {
		t.Fatalf("data = %v", data)
	}
}

func TestScheduleWorkoutsPartialFailure(t *testing.T) {
	f := newFakeGarmin(t)
	emptyScheduleGraphQL(f)
	f.on("POST "+schedulePath+"/111", status(200, nil))
	f.on("POST "+schedulePath+"/999", status(404, map[string]any{"message": "API Error 404 - Workout not found"}))
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "schedule_workouts", map[string]any{"schedules": []any{
		map[string]any{"workout_id": 111, "calendar_date": "2024-01-15"},
		map[string]any{"workout_id": 999, "calendar_date": "2024-01-17"},
	}})
	data := workoutText(t, text)
	if num(t, data["total"]) != 2 || num(t, data["succeeded"]) != 1 || num(t, data["failed"]) != 1 {
		t.Fatalf("data = %v", data)
	}
	results := listOf(data["results"])
	second := mapOf(results[1])
	if mapOf(results[0])["status"] != "success" || second["status"] != "error" ||
		!strings.Contains(second["message"].(string), "API Error 404 - Workout not found") {
		t.Fatalf("results = %v", results)
	}
}

func TestScheduleWorkoutsMissingFields(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "schedule_workouts", map[string]any{"schedules": []any{
		map[string]any{"workout_id": 123456},
	}})
	data := workoutText(t, text)
	if num(t, data["total"]) != 1 || num(t, data["failed"]) != 1 {
		t.Fatalf("data = %v", data)
	}
	entry := mapOf(listOf(data["results"])[0])
	if entry["status"] != "failed" || !strings.Contains(entry["message"].(string), "Missing required field") {
		t.Fatalf("entry = %v", entry)
	}
	if calls := f.callsMatching("POST", schedulePath+"/123456"); len(calls) != 0 {
		t.Fatalf("post calls = %d", len(calls))
	}
}

func TestScheduleWorkoutsRejectsInvalidDate(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "schedule_workouts", map[string]any{"schedules": []any{
		map[string]any{"workout_id": 123456, "calendar_date": "not-a-date"},
	}})
	data := workoutText(t, text)
	entry := mapOf(listOf(data["results"])[0])
	if num(t, data["failed"]) != 1 || entry["status"] != "failed" || !strings.Contains(entry["message"].(string), "YYYY-MM-DD") {
		t.Fatalf("data = %v", data)
	}
	if calls := f.callsMatching("POST", schedulePath+"/123456"); len(calls) != 0 {
		t.Fatalf("post calls = %d", len(calls))
	}
}

func TestScheduleWorkoutsInvalidDateSkipsInlineUpload(t *testing.T) {
	inline := runningWorkoutWithSteps([]any{timedIntervalStep(map[string]any{"workoutTargetTypeId": 4, "workoutTargetTypeKey": "heart.rate.zone"})}, "Inline")
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "schedule_workouts", map[string]any{"schedules": []any{
		map[string]any{"workout_data": inline, "calendar_date": "2024/02/01"},
	}})
	data := workoutText(t, text)
	entry := mapOf(listOf(data["results"])[0])
	if num(t, data["failed"]) != 1 || entry["status"] != "failed" || !strings.Contains(entry["message"].(string), "YYYY-MM-DD") {
		t.Fatalf("data = %v", data)
	}
	if calls := f.callsMatching("POST", workoutPath); len(calls) != 0 {
		t.Fatalf("upload calls = %d", len(calls))
	}
}

func TestScheduleWorkoutRejectsInvalidDate(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "schedule_workout", map[string]any{"workout_id": 123456, "calendar_date": "01-15-2024"})
	data := workoutText(t, text)
	if data["status"] != "failed" || num(t, data["workout_id"]) != 123456 || !strings.Contains(data["message"].(string), "YYYY-MM-DD") {
		t.Fatalf("data = %v", data)
	}
	if calls := f.callsMatching("POST", schedulePath+"/123456"); len(calls) != 0 {
		t.Fatalf("post calls = %d", len(calls))
	}
}

func TestScheduleWorkoutsException(t *testing.T) {
	f := newFakeGarmin(t)
	emptyScheduleGraphQL(f)
	f.on("POST "+schedulePath+"/123456", status(500, map[string]any{"message": "Network error"}))
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "schedule_workouts", map[string]any{"schedules": []any{
		map[string]any{"workout_id": 123456, "calendar_date": "2024-01-15"},
	}})
	data := workoutText(t, text)
	entry := mapOf(listOf(data["results"])[0])
	if num(t, data["failed"]) != 1 || entry["status"] != "error" || !strings.Contains(entry["message"].(string), "Network error") {
		t.Fatalf("data = %v", data)
	}
}

func TestScheduleWorkoutsIdempotent(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+graphqlPath, map[string]any{"data": map[string]any{"workoutScheduleSummariesScalar": []any{
		map[string]any{"workoutId": 123456, "scheduleDate": "2024-01-15", "workoutName": "Easy Run"},
	}}})
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "schedule_workouts", map[string]any{"schedules": []any{
		map[string]any{"workout_id": 123456, "calendar_date": "2024-01-15"},
	}})
	data := workoutText(t, text)
	entry := mapOf(listOf(data["results"])[0])
	if entry["status"] != "success" || entry["idempotent"] != true {
		t.Fatalf("entry = %v", entry)
	}
	if calls := f.callsMatching("POST", schedulePath+"/123456"); len(calls) != 0 {
		t.Fatalf("post calls = %d", len(calls))
	}
}

func TestScheduleWorkoutsStringWorkoutIdIsIdempotent(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+graphqlPath, map[string]any{"data": map[string]any{"workoutScheduleSummariesScalar": []any{
		map[string]any{"workoutId": 123456, "scheduleDate": "2024-01-15"},
	}}})
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "schedule_workouts", map[string]any{"schedules": []any{
		map[string]any{"workout_id": "123456", "calendar_date": "2024-01-15"},
	}})
	entry := mapOf(listOf(workoutText(t, text)["results"])[0])
	if entry["status"] != "success" || entry["idempotent"] != true || num(t, entry["workout_id"]) != 123456 {
		t.Fatalf("entry = %v", entry)
	}
	if calls := f.callsMatching("POST", schedulePath+"/123456"); len(calls) != 0 {
		t.Fatalf("post calls = %d", len(calls))
	}
}

func TestScheduleWorkoutsNonStringDateKeepsEarlierResults(t *testing.T) {
	f := newFakeGarmin(t)
	emptyScheduleGraphQL(f)
	f.on("POST "+schedulePath+"/111", status(200, nil))
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "schedule_workouts", map[string]any{"schedules": []any{
		map[string]any{"workout_id": 111, "calendar_date": "2024-01-15"},
		map[string]any{"workout_id": 222, "calendar_date": 20240117},
	}})
	data := workoutText(t, text)
	results := listOf(data["results"])
	if num(t, data["total"]) != 2 || num(t, data["succeeded"]) != 1 {
		t.Fatalf("data = %v", data)
	}
	if mapOf(results[0])["status"] != "success" || mapOf(results[1])["status"] != "failed" ||
		!strings.Contains(mapOf(results[1])["message"].(string), "YYYY-MM-DD") {
		t.Fatalf("results = %v", results)
	}
}

func TestScheduleWorkoutsRejectsNonexistentOrPaddedDate(t *testing.T) {
	for _, bad := range []string{"2024-02-30", "2024-13-01", "2024-01-15\n"} {
		f := newFakeGarmin(t)
		cs := newSession(t, f, nil)
		text, _ := callTool(t, cs, "schedule_workouts", map[string]any{"schedules": []any{
			map[string]any{"workout_id": 123456, "calendar_date": bad},
		}})
		entry := mapOf(listOf(workoutText(t, text)["results"])[0])
		if entry["status"] != "failed" || !strings.Contains(entry["message"].(string), "YYYY-MM-DD") {
			t.Fatalf("entry = %v", entry)
		}
		if calls := f.callsMatching("POST", schedulePath+"/123456"); len(calls) != 0 {
			t.Fatalf("post calls = %d", len(calls))
		}
	}
}

func TestScheduleWorkoutsInlineUpload(t *testing.T) {
	inline := map[string]any{"workoutName": "Easy Run", "sportType": map[string]any{"sportTypeId": 1, "sportTypeKey": "running"}}
	f := newFakeGarmin(t)
	emptyScheduleGraphQL(f)
	f.on("POST "+workoutPath, map[string]any{"workoutId": 999001, "workoutName": "Easy Run"})
	f.on("POST "+schedulePath+"/999001", status(200, nil))
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "schedule_workouts", map[string]any{"schedules": []any{
		map[string]any{"workout_data": inline, "calendar_date": "2024-02-01"},
	}})
	entry := mapOf(listOf(workoutText(t, text)["results"])[0])
	if entry["status"] != "success" || num(t, entry["workout_id"]) != 999001 ||
		entry["scheduled_date"] != "2024-02-01" || entry["workout_name"] != "Easy Run" {
		t.Fatalf("entry = %v", entry)
	}
	sameJSON(t, firstUploadBody(t, f), inline)
}

func TestScheduleWorkoutsInlinePromotesNestedTargetBounds(t *testing.T) {
	f := newFakeGarmin(t)
	emptyScheduleGraphQL(f)
	f.on("POST "+workoutPath, map[string]any{"workoutId": 210004, "workoutName": "Issue 210 Inline"})
	f.on("POST "+schedulePath+"/210004", status(200, nil))
	cs := newSession(t, f, nil)
	_, _ = callTool(t, cs, "schedule_workouts", map[string]any{"schedules": []any{
		map[string]any{"workout_data": runningWorkoutWithSteps([]any{distancePaceStepWithNestedBounds()}, "Issue 210 Inline"), "calendar_date": "2024-02-01"},
	}})
	called := uploadStep(t, f)
	if num(t, called["targetValueOne"]) != 2.0833333 || num(t, called["targetValueTwo"]) != 1.9607843 {
		t.Fatalf("called = %v", called)
	}
}

func TestScheduleWorkoutsInlineUploadRejectsEndConditionMismatch(t *testing.T) {
	inline := runningWorkoutWithSteps([]any{map[string]any{
		"type": "ExecutableStepDTO", "stepOrder": 1,
		"stepType":          map[string]any{"stepTypeId": 4, "stepTypeKey": "recovery"},
		"endCondition":      map[string]any{"conditionTypeId": 4, "conditionTypeKey": "heart.rate"},
		"endConditionValue": 145.0,
		"targetType":        map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"},
	}}, "Invalid Inline HR Workout")
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "schedule_workouts", map[string]any{"schedules": []any{
		map[string]any{"workout_data": inline, "calendar_date": "2024-02-01"},
	}})
	data := workoutText(t, text)
	entry := mapOf(listOf(data["results"])[0])
	if num(t, data["failed"]) != 1 || entry["status"] != "error" ||
		!strings.Contains(entry["message"].(string), "conditionTypeKey 'heart.rate' requires conditionTypeId 6") {
		t.Fatalf("data = %v", data)
	}
	if calls := f.callsMatching("POST", workoutPath); len(calls) != 0 {
		t.Fatalf("upload calls = %d", len(calls))
	}
}

func TestScheduleWorkoutsRejectsInlineTargetTypeMismatch(t *testing.T) {
	inline := runningWorkoutWithSteps([]any{timedIntervalStep(map[string]any{"workoutTargetTypeId": 6, "workoutTargetTypeKey": "heart.rate"})}, "Bad Inline HR Target")
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "schedule_workouts", map[string]any{"schedules": []any{
		map[string]any{"workout_data": inline, "calendar_date": "2024-02-01"},
	}})
	data := workoutText(t, text)
	entry := mapOf(listOf(data["results"])[0])
	if num(t, data["failed"]) != 1 || entry["status"] != "error" || !strings.Contains(entry["message"].(string), "targetType mismatch") {
		t.Fatalf("data = %v", data)
	}
	if calls := f.callsMatching("POST", workoutPath); len(calls) != 0 {
		t.Fatalf("upload calls = %d", len(calls))
	}
}

func TestScheduleWorkoutsMixedInlineAndId(t *testing.T) {
	inline := map[string]any{"workoutName": "Tempo Run", "sportType": map[string]any{"sportTypeId": 1, "sportTypeKey": "running"}}
	f := newFakeGarmin(t)
	emptyScheduleGraphQL(f)
	f.on("POST "+workoutPath, map[string]any{"workoutId": 999002, "workoutName": "Tempo Run"})
	f.on("POST "+schedulePath+"/111", status(200, nil))
	f.on("POST "+schedulePath+"/999002", status(200, nil))
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "schedule_workouts", map[string]any{"schedules": []any{
		map[string]any{"workout_id": 111, "calendar_date": "2024-02-05"},
		map[string]any{"workout_data": inline, "calendar_date": "2024-02-07"},
	}})
	data := workoutText(t, text)
	results := listOf(data["results"])
	if num(t, data["total"]) != 2 || num(t, data["succeeded"]) != 2 {
		t.Fatalf("data = %v", data)
	}
	if num(t, mapOf(results[0])["workout_id"]) != 111 || num(t, mapOf(results[1])["workout_id"]) != 999002 {
		t.Fatalf("results = %v", results)
	}
}

func TestScheduleWorkoutsMissingBothIdAndData(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "schedule_workouts", map[string]any{"schedules": []any{
		map[string]any{"calendar_date": "2024-02-01"},
	}})
	data := workoutText(t, text)
	entry := mapOf(listOf(data["results"])[0])
	message, _ := entry["message"].(string)
	if num(t, data["failed"]) != 1 || (!strings.Contains(message, "workout_id") && !strings.Contains(message, "workout_data")) {
		t.Fatalf("data = %v", data)
	}
}

func TestScheduleWorkoutsInlineUploadNoIdReturned(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST "+workoutPath, map[string]any{"workoutName": "Bad Response"})
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "schedule_workouts", map[string]any{"schedules": []any{
		map[string]any{"workout_data": map[string]any{"workoutName": "Bad Response"}, "calendar_date": "2024-02-01"},
	}})
	data := workoutText(t, text)
	entry := mapOf(listOf(data["results"])[0])
	if num(t, data["failed"]) != 1 || entry["status"] != "failed" {
		t.Fatalf("data = %v", data)
	}
	if calls := f.callsMatching("POST", schedulePath+"/999"); len(calls) != 0 {
		t.Fatalf("post calls = %d", len(calls))
	}
}

// --- unschedule / reschedule ---

func TestUnscheduleWorkoutSuccess(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("DELETE "+schedulePath+"/1677275789", map[string]any{})
	cs := newSession(t, f, nil)
	text, isErr := callTool(t, cs, "unschedule_workout", map[string]any{"scheduled_workout_id": 1677275789})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := workoutText(t, text)
	if data["status"] != "success" || num(t, data["scheduled_workout_id"]) != 1677275789 ||
		!strings.Contains(data["message"].(string), "removed from calendar") {
		t.Fatalf("data = %v", data)
	}
}

func TestUnscheduleWorkoutError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("DELETE "+schedulePath+"/999", status(500, map[string]any{"message": "Network error"}))
	cs := newSession(t, f, nil)
	text, isErr := callTool(t, cs, "unschedule_workout", map[string]any{"scheduled_workout_id": 999})
	if !isErr || !strings.Contains(text, "Network error") {
		t.Fatalf("text = %q isErr=%v", text, isErr)
	}
}

func scheduledEntry() map[string]any {
	return map[string]any{
		"workoutScheduleId": 1677275789,
		"calendarDate":      "2024-01-15",
		"workout": map[string]any{
			"workoutId": 123456, "workoutName": "5K Tempo Run",
			"workoutSegments": []any{map[string]any{"segmentOrder": 1}},
		},
		"ownerId":     80653452,
		"createdDate": "2024-01-10T08:00:00.0",
	}
}

func TestRescheduleWorkoutSuccess(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+schedulePath+"/1677275789", scheduledEntry())
	f.on("PUT "+schedulePath+"/1677275789", map[string]any{})
	cs := newSession(t, f, nil)
	text, isErr := callTool(t, cs, "reschedule_workout", map[string]any{"scheduled_workout_id": 1677275789, "calendar_date": "2024-01-17"})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := workoutText(t, text)
	if data["status"] != "success" || data["previous_date"] != "2024-01-15" || data["scheduled_date"] != "2024-01-17" ||
		num(t, data["workout_id"]) != 123456 {
		t.Fatalf("data = %v", data)
	}
	calls := f.callsMatching("PUT", schedulePath+"/1677275789")
	if len(calls) != 1 {
		t.Fatalf("put calls = %d", len(calls))
	}
	var body map[string]any
	decodeJSON(t, string(calls[0].Body), &body)
	sameJSON(t, body, map[string]any{
		"workoutScheduleId": 1677275789,
		"calendarDate":      "2024-01-17",
		"workout":           map[string]any{"workoutId": 123456},
		"ownerId":           80653452,
		"createdDate":       "2024-01-10T08:00:00.0",
	})
}

func TestRescheduleWorkoutInvalidDate(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "reschedule_workout", map[string]any{"scheduled_workout_id": 1677275789, "calendar_date": "2024-02-30"})
	if workoutText(t, text)["status"] != "failed" {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("GET", schedulePath+"/1677275789"); len(calls) != 0 {
		t.Fatalf("get calls = %d", len(calls))
	}
	if calls := f.callsMatching("PUT", schedulePath+"/1677275789"); len(calls) != 0 {
		t.Fatalf("put calls = %d", len(calls))
	}
}

func TestRescheduleWorkoutNotFound(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+schedulePath+"/999", nil)
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "reschedule_workout", map[string]any{"scheduled_workout_id": 999, "calendar_date": "2024-01-17"})
	data := workoutText(t, text)
	if data["status"] != "failed" || !strings.Contains(data["message"].(string), "not found") {
		t.Fatalf("data = %v", data)
	}
	if calls := f.callsMatching("PUT", schedulePath+"/999"); len(calls) != 0 {
		t.Fatalf("put calls = %d", len(calls))
	}
}

func TestRescheduleWorkoutError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+schedulePath+"/1677275789", scheduledEntry())
	f.on("PUT "+schedulePath+"/1677275789", status(400, map[string]any{"message": "API Error 400"}))
	cs := newSession(t, f, nil)
	text, isErr := callTool(t, cs, "reschedule_workout", map[string]any{"scheduled_workout_id": 1677275789, "calendar_date": "2024-01-17"})
	data := workoutText(t, text)
	if isErr || data["status"] != "failed" || !strings.Contains(data["message"].(string), "400") {
		t.Fatalf("text = %q isErr=%v", text, isErr)
	}
}

func TestUnscheduleWorkoutsMultiple(t *testing.T) {
	f := newFakeGarmin(t)
	for _, id := range []int{111, 222, 333} {
		f.on("DELETE "+fmt.Sprintf("%s/%d", schedulePath, id), map[string]any{})
	}
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "unschedule_workouts", map[string]any{"scheduled_workout_ids": []any{111, 222, 333}})
	data := workoutText(t, text)
	if num(t, data["total"]) != 3 || num(t, data["succeeded"]) != 3 || num(t, data["failed"]) != 0 {
		t.Fatalf("data = %v", data)
	}
}

func TestUnscheduleWorkoutsPartialFailure(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("DELETE "+schedulePath+"/111", map[string]any{})
	f.on("DELETE "+schedulePath+"/999", status(404, map[string]any{"message": "API Error 404"}))
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "unschedule_workouts", map[string]any{"scheduled_workout_ids": []any{111, 999}})
	data := workoutText(t, text)
	results := listOf(data["results"])
	if num(t, data["total"]) != 2 || num(t, data["succeeded"]) != 1 || num(t, data["failed"]) != 1 {
		t.Fatalf("data = %v", data)
	}
	if mapOf(results[0])["status"] != "success" || mapOf(results[1])["status"] != "error" ||
		!strings.Contains(mapOf(results[1])["message"].(string), "404") {
		t.Fatalf("results = %v", results)
	}
}

// --- helper unit tests ---

func TestValidateDateRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"2024-02-30", "2024-13-01", "2024-01-15\n", "20240115", ""} {
		if _, err := validateWorkoutDate(value, "calendar_date"); err == nil || !strings.Contains(err.Error(), "expected YYYY-MM-DD") {
			t.Errorf("%q: err = %v", value, err)
		}
	}
}

func TestValidateDateAcceptsLeapDay(t *testing.T) {
	got, err := validateWorkoutDate("2024-02-29", "calendar_date")
	if err != nil || got != "2024-02-29" {
		t.Fatalf("got = %q err = %v", got, err)
	}
}

func TestNestedTargetConflictDoesNotPartiallyMutateStep(t *testing.T) {
	step := distancePaceStepWithNestedBounds()
	step["targetValueOne"] = 2.5
	workout := runningWorkoutWithSteps([]any{step}, "Workout")
	err := normalizeWorkoutSteps(workout)
	if err == nil || !strings.Contains(err.Error(), "targetValueOne=2.5 conflicts") {
		t.Fatalf("err = %v", err)
	}
	if step["targetValueOne"] != 2.5 {
		t.Fatalf("step value = %v", step["targetValueOne"])
	}
	if _, ok := step["targetValueTwo"]; ok {
		t.Fatal("step targetValueTwo injected")
	}
	target := mapOf(step["targetType"])
	if target["targetValueOne"] != 2.0833333 || target["targetValueTwo"] != 1.9607843 {
		t.Fatalf("target = %v", target)
	}
}

func TestNestedTargetConflictDoesNotMutateEarlierWorkoutStep(t *testing.T) {
	first := distancePaceStepWithNestedBounds()
	second := distancePaceStepWithNestedBounds()
	second["targetValueOne"] = 2.5
	err := normalizeWorkoutSteps(runningWorkoutWithSteps([]any{first, second}, "Workout"))
	if err == nil || !strings.Contains(err.Error(), "targetValueOne=2.5 conflicts") {
		t.Fatalf("err = %v", err)
	}
	if _, ok := first["targetValueOne"]; ok {
		t.Fatal("first mutated")
	}
	target := mapOf(first["targetType"])
	if target["targetValueOne"] != 2.0833333 || target["targetValueTwo"] != 1.9607843 {
		t.Fatalf("target = %v", target)
	}
}

func TestNestedTargetFieldsPromoteSingleBoundAndDeduplicateEqualValue(t *testing.T) {
	step := map[string]any{
		"targetValueOne": 2.0833333,
		"targetType": map[string]any{
			"targetValueOne": 2.0833333,
			"targetValueTwo": 1.9607843,
		},
	}
	if err := normalizeWorkoutSteps(runningWorkoutWithSteps([]any{step}, "Workout")); err != nil {
		t.Fatal(err)
	}
	if step["targetValueOne"] != 2.0833333 || step["targetValueTwo"] != 1.9607843 {
		t.Fatalf("step = %v", step)
	}
	if len(mapOf(step["targetType"])) != 0 {
		t.Fatalf("targetType = %v", step["targetType"])
	}
}

func TestNestedNullTargetFieldIsRemovedWithoutInjectingStepNull(t *testing.T) {
	step := map[string]any{"targetType": map[string]any{"targetValueOne": nil}}
	if err := normalizeWorkoutSteps(runningWorkoutWithSteps([]any{step}, "Workout")); err != nil {
		t.Fatal(err)
	}
	if len(step) != 1 || len(mapOf(step["targetType"])) != 0 {
		t.Fatalf("step = %v", step)
	}
}

func TestNestedValueReplacesExplicitStepNull(t *testing.T) {
	step := map[string]any{
		"targetValueOne": nil,
		"targetType":     map[string]any{"targetValueOne": 2.0833333},
	}
	if err := normalizeWorkoutSteps(runningWorkoutWithSteps([]any{step}, "Workout")); err != nil {
		t.Fatal(err)
	}
	if step["targetValueOne"] != 2.0833333 || len(mapOf(step["targetType"])) != 0 {
		t.Fatalf("step = %v", step)
	}
}

func TestNestedTargetRepairIgnoresMissingOrNonDictTargetType(t *testing.T) {
	cases := []map[string]any{{}, {"targetType": "pace.zone"}, {"targetType": []any{}}}
	for _, step := range cases {
		original := deepCopy(t, step)
		workout := runningWorkoutWithSteps([]any{step}, "Workout")
		if err := normalizeWorkoutSteps(workout); err != nil {
			t.Fatal(err)
		}
		sameJSON(t, step, original)
	}
}

func TestFixRepeatGroupAddsMissingConditionTypeId(t *testing.T) {
	step := map[string]any{
		"type": "RepeatGroupDTO", "numberOfIterations": 5,
		"endCondition":      map[string]any{"conditionTypeKey": "iterations"},
		"endConditionValue": 5, "workoutSteps": []any{},
	}
	fixRepeatGroupStep(step)
	if num(t, mapOf(step["endCondition"])["conditionTypeId"]) != 7 || mapOf(step["endCondition"])["conditionTypeKey"] != "iterations" {
		t.Fatalf("endCondition = %v", step["endCondition"])
	}
}

func TestFixRepeatGroupLeavesExistingConditionTypeIdUnchanged(t *testing.T) {
	step := map[string]any{
		"type": "RepeatGroupDTO", "numberOfIterations": 3,
		"endCondition":      map[string]any{"conditionTypeId": 7, "conditionTypeKey": "iterations"},
		"endConditionValue": 3, "workoutSteps": []any{},
	}
	fixRepeatGroupStep(step)
	if num(t, mapOf(step["endCondition"])["conditionTypeId"]) != 7 {
		t.Fatalf("endCondition = %v", step["endCondition"])
	}
}

func TestFixRepeatGroupBackfillsNumberOfIterationsFromEndConditionValue(t *testing.T) {
	step := map[string]any{
		"type":              "RepeatGroupDTO",
		"endCondition":      map[string]any{"conditionTypeId": 7, "conditionTypeKey": "iterations"},
		"endConditionValue": 4, "workoutSteps": []any{},
	}
	fixRepeatGroupStep(step)
	if num(t, step["numberOfIterations"]) != 4 {
		t.Fatalf("iterations = %v", step["numberOfIterations"])
	}
}

func TestFixRepeatGroupDoesNotModifyNonRepeatSteps(t *testing.T) {
	step := map[string]any{
		"type":              "ExecutableStepDTO",
		"endCondition":      map[string]any{"conditionTypeKey": "time"},
		"endConditionValue": 300.0,
	}
	fixRepeatGroupStep(step)
	if _, ok := mapOf(step["endCondition"])["conditionTypeId"]; ok {
		t.Fatal("conditionTypeId injected")
	}
}

func TestFixRepeatGroupRecursesIntoNestedRepeatGroups(t *testing.T) {
	inner := map[string]any{
		"type": "RepeatGroupDTO", "numberOfIterations": 2,
		"endCondition":      map[string]any{"conditionTypeKey": "iterations"},
		"endConditionValue": 2, "workoutSteps": []any{},
	}
	outer := map[string]any{
		"type": "RepeatGroupDTO", "numberOfIterations": 3,
		"endCondition":      map[string]any{"conditionTypeId": 7, "conditionTypeKey": "iterations"},
		"endConditionValue": 3, "workoutSteps": []any{inner},
	}
	fixRepeatGroupStep(outer)
	if num(t, mapOf(inner["endCondition"])["conditionTypeId"]) != 7 {
		t.Fatalf("inner = %v", inner)
	}
}

// --- preview helpers ---

func TestPreviewFromWorkoutDetails(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+workoutPath+"/99", map[string]any{
		"workoutId": 99, "workoutName": "Threshold Intervals",
		"sportType":       map[string]any{"sportTypeKey": "running"},
		"workoutProvider": "garmin", "updatedDate": "2024-01-15",
	})
	preview := workoutPreview(context.Background(), newTestGarminClient(t, f), 99)
	if num(t, preview["workout_id"]) != 99 || preview["name"] != "Threshold Intervals" || preview["sport"] != "running" {
		t.Fatalf("preview = %v", preview)
	}
}

func TestPreviewFallsBackToWorkoutListOnDetailsError(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+workoutPath+"/99", status(403, map[string]any{"message": "Forbidden"}))
	f.on("GET "+workoutsPath, []any{map[string]any{
		"workoutId": 99, "workoutName": "Sweet Spot", "sportType": map[string]any{"sportTypeKey": "cycling"},
	}})
	preview := workoutPreview(context.Background(), newTestGarminClient(t, f), 99)
	if preview["name"] != "Sweet Spot" || preview["sport"] != "cycling" {
		t.Fatalf("preview = %v", preview)
	}
}

func TestPreviewFallbackSearchesPastFirstPage(t *testing.T) {
	first := make([]any, 100)
	for i := 1; i <= 100; i++ {
		first[i-1] = map[string]any{"workoutId": i, "workoutName": fmt.Sprintf("Workout %d", i)}
	}
	second := make([]any, 50)
	for i := 101; i <= 150; i++ {
		second[i-101] = map[string]any{"workoutId": i, "workoutName": fmt.Sprintf("Workout %d", i)}
	}
	f := newFakeGarmin(t)
	f.on("GET "+workoutPath+"/120", status(403, map[string]any{"message": "Forbidden"}))
	f.on("GET "+workoutsPath, sequence{first, second})
	preview := workoutPreview(context.Background(), newTestGarminClient(t, f), 120)
	if preview["name"] != "Workout 120" {
		t.Fatalf("preview = %v", preview)
	}
}

func TestGetAllWorkoutsPagesALargeLibraryWithoutGapsOrDuplicates(t *testing.T) {
	var pages []any
	for start := 0; start < 250; start += 100 {
		end := start + 100
		if end > 250 {
			end = 250
		}
		page := make([]any, 0, end-start)
		for i := start; i < end; i++ {
			page = append(page, map[string]any{"workoutId": i})
		}
		pages = append(pages, page)
	}
	f := newFakeGarmin(t)
	f.on("GET "+workoutsPath, sequence(pages))
	workouts, err := getAllWorkouts(context.Background(), newTestGarminClient(t, f))
	if err != nil {
		t.Fatal(err)
	}
	if len(workouts) != 250 {
		t.Fatalf("len = %d", len(workouts))
	}
	for i, w := range workouts {
		if num(t, w["workoutId"]) != float64(i) {
			t.Fatalf("workout %d = %v", i, w)
		}
	}
}
