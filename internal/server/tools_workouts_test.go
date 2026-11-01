package server

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// Ported from tests/integration/test_workouts_tools.py (read tools).

const (
	wUpload  = "POST /workout-service/workout"
	wGraphQL = "POST /graphql-gateway/graphql"
	wList    = "GET /workout-service/workouts"
)

func wRoute(id any) string { return fmt.Sprintf("GET /workout-service/workout/%v", id) }

// workoutsFake is the app_with_workouts fixture: the schedule pre-check sees no existing schedule.
func workoutsFake(t *testing.T) *fakeGarmin {
	t.Helper()
	f := newFakeGarmin(t)
	f.on(wGraphQL, map[string]any{"data": map[string]any{"workoutScheduleSummariesScalar": []any{}}})
	return f
}

func runTool(t *testing.T, f *fakeGarmin, tool string, args map[string]any) map[string]any {
	t.Helper()
	return resultMap(t, callOK(t, newSession(t, f, nil), tool, args))
}

func toolList(t *testing.T, key string, m map[string]any) []any {
	t.Helper()
	l, ok := m[key].([]any)
	if !ok {
		t.Fatalf("%s is not a list in %v", key, m)
	}
	return l
}

func TestGetWorkoutsKeepsGarminEstimatedMetrics(t *testing.T) {
	f := workoutsFake(t)
	w := fixture(t, "MOCK_SWIM_WORKOUT_DETAILS").(map[string]any)
	w["estimatedDurationInSecs"] = 3600.0
	f.on(wList, []any{w})
	s := asMap(toolList(t, "workouts", runTool(t, f, "get_workouts", nil))[0])
	if s["estimated_duration_seconds"] != 3600.0 || s["estimated_distance_meters"] != 3000.0 {
		t.Errorf("summary = %v", s)
	}
}

func TestGetWorkoutPaceTargetsIncludeMpsUnits(t *testing.T) {
	for _, secondary := range []bool{false, true} {
		prefix, out := "", ""
		if secondary {
			prefix, out = "secondary", "secondary_"
		}
		field := func(n string) string {
			if prefix == "" {
				return n
			}
			return prefix + strings.ToUpper(n[:1]) + n[1:]
		}
		step := map[string]any{
			field("targetType"):      map[string]any{"workoutTargetTypeId": 6.0, "workoutTargetTypeKey": "pace.zone"},
			field("targetValueOne"):  2.777,
			field("targetValueTwo"):  2.472,
			field("targetValueUnit"): nil,
		}
		f := workoutsFake(t)
		f.on(wRoute(123), runningWorkoutWithSteps(step))
		d := runTool(t, f, "get_workout_by_id", map[string]any{"workout_id": 123})
		c := asMap(asList(asMap(toolList(t, "segments", d)[0])["steps"])[0])
		if c[out+"target_value_unit"] != "meters_per_second" || c[out+"target_value_low"] != 2.777 || c[out+"target_value_high"] != 2.472 {
			t.Errorf("secondary=%v: %v", secondary, c)
		}
	}
}

func TestGetWorkoutsTool(t *testing.T) {
	f := workoutsFake(t)
	f.on(wList, fixture(t, "MOCK_WORKOUTS"))
	d := runTool(t, f, "get_workouts", nil)
	if d["count"] == nil || len(f.requests(wList)) != 1 {
		t.Errorf("d = %v, requests = %d", d, len(f.requests(wList)))
	}
}

func workoutPage(from, to int) []any {
	var out []any
	for i := from; i < to; i++ {
		out = append(out, map[string]any{"workoutId": float64(i), "workoutName": fmt.Sprintf("W%d", i)})
	}
	return out
}

func TestGetWorkoutsPagesThroughWholeLibrary(t *testing.T) {
	f := workoutsFake(t)
	f.on(wList, sequence(workoutPage(0, 100), workoutPage(100, 105)))
	d := runTool(t, f, "get_workouts", nil)
	ws := toolList(t, "workouts", d)
	if d["count"] != 105.0 || asMap(ws[len(ws)-1])["id"] != 104.0 {
		t.Errorf("count = %v", d["count"])
	}
	reqs := f.requests(wList)
	if len(reqs) != 2 || reqs[0].Query.Get("start") != "1" || reqs[0].Query.Get("limit") != "100" || reqs[1].Query.Get("start") != "101" {
		t.Errorf("requests = %v", reqs)
	}
}

func TestGetWorkoutsStopsWhenGarminRepeatsAPage(t *testing.T) {
	f := workoutsFake(t)
	f.on(wList, workoutPage(0, 100))
	d := runTool(t, f, "get_workouts", nil)
	if d["count"] != 100.0 || len(f.requests(wList)) != 2 {
		t.Errorf("count = %v, calls = %d", d["count"], len(f.requests(wList)))
	}
}

func TestGetWorkoutsHandlesNullSportType(t *testing.T) {
	f := workoutsFake(t)
	f.on(wList, []any{
		map[string]any{"workoutId": 1.0, "workoutName": "Easy Run", "sportType": map[string]any{"sportTypeKey": "running"}},
		map[string]any{"workoutId": 2.0, "workoutName": "Imported", "sportType": nil},
	})
	d := runTool(t, f, "get_workouts", nil)
	ws := toolList(t, "workouts", d)
	if d["count"] != 2.0 || asMap(ws[0])["sport"] != "running" || asMap(ws[1])["name"] != "Imported" {
		t.Errorf("d = %v", d)
	}
	if _, has := asMap(ws[1])["sport"]; has {
		t.Errorf("sport present: %v", ws[1])
	}
}

func TestGetWorkoutByIdTool(t *testing.T) {
	f := workoutsFake(t)
	f.on(wRoute(123456), fixture(t, "MOCK_WORKOUT_DETAILS"))
	d := runTool(t, f, "get_workout_by_id", map[string]any{"workout_id": 123456})
	if len(f.requests(wRoute(123456))) != 1 || len(f.requests("GET /workout-service/fbt-adaptive/123456")) != 0 {
		t.Error("wrong endpoint")
	}
	if d["id"] != 123456.0 || d["name"] != "5K Tempo Run" || d["sport"] != "running" {
		t.Errorf("d = %v", d)
	}
	seg := asMap(toolList(t, "segments", d)[0])
	if seg["step_count"] != 3.0 {
		t.Errorf("segment = %v", seg)
	}
	steps := toolList(t, "steps", seg)
	warm, interval := asMap(steps[0]), asMap(steps[1])
	if warm["type"] != "warmup" || warm["end_condition"] != "time" || warm["end_condition_value"] != 600.0 {
		t.Errorf("warmup = %v", warm)
	}
	if interval["type"] != "interval" || interval["target_type"] != "pace.zone" || interval["target_zone"] != 4.0 {
		t.Errorf("interval = %v", interval)
	}
}

func TestGetWorkoutByIdToolKeepsTargetUnits(t *testing.T) {
	power := map[string]any{"workoutTargetTypeId": 2.0, "workoutTargetTypeKey": "power.zone", "displayOrder": 2.0}
	cadence := map[string]any{"workoutTargetTypeId": 3.0, "workoutTargetTypeKey": "cadence", "displayOrder": 3.0}
	percent := map[string]any{"unitId": 253.0, "unitKey": "percent", "factor": 1.0}
	f := workoutsFake(t)
	f.on(wRoute(1), map[string]any{
		"workoutId": 1.0, "workoutName": "Units", "sportType": map[string]any{"sportTypeId": 2.0, "sportTypeKey": "cycling"},
		"poolLength": nil, "poolLengthUnit": nil,
		"workoutSegments": []any{map[string]any{
			"segmentOrder": 1.0, "sportType": map[string]any{"sportTypeId": 2.0, "sportTypeKey": "cycling"},
			"workoutSteps": []any{
				map[string]any{
					"type": "ExecutableStepDTO", "stepOrder": 1.0, "stepType": map[string]any{"stepTypeId": 3.0, "stepTypeKey": "interval"},
					"targetType": power, "targetValueOne": 76.0, "targetValueTwo": 81.0, "targetValueUnit": percent,
					"secondaryTargetType": cadence, "secondaryTargetValueOne": 85.0, "secondaryTargetValueTwo": 95.0, "secondaryTargetValueUnit": nil,
				},
				map[string]any{
					"type": "ExecutableStepDTO", "stepOrder": 2.0, "stepType": map[string]any{"stepTypeId": 3.0, "stepTypeKey": "interval"},
					"targetType": cadence, "targetValueOne": 85.0, "targetValueTwo": 95.0,
					"secondaryTargetType": power, "secondaryTargetValueOne": 76.0, "secondaryTargetValueTwo": 81.0, "secondaryTargetValueUnit": percent,
				},
			},
		}},
	})
	d := runTool(t, f, "get_workout_by_id", map[string]any{"workout_id": 1})
	if _, has := d["pool_length"]; has {
		t.Error("pool_length present")
	}
	steps := toolList(t, "steps", asMap(toolList(t, "segments", d)[0]))
	pf, cf := asMap(steps[0]), asMap(steps[1])
	if pf["target_value_unit"] != "percent" || cf["secondary_target_value_unit"] != "percent" {
		t.Errorf("steps = %v", steps)
	}
	if _, has := pf["secondary_target_value_unit"]; has {
		t.Error("secondary unit on power-first step")
	}
	if _, has := cf["target_value_unit"]; has {
		t.Error("target unit on cadence-first step")
	}
}

func TestGetWorkoutByIdToolKeepsPoolLength(t *testing.T) {
	f := workoutsFake(t)
	f.on(wRoute(1), map[string]any{
		"workoutId": 1.0, "workoutName": "Pool", "sportType": map[string]any{"sportTypeId": 4.0, "sportTypeKey": "swimming"},
		"poolLength": 50.0, "poolLengthUnit": map[string]any{"unitId": 1.0, "unitKey": "meter", "factor": 100.0},
		"workoutSegments": []any{map[string]any{
			"segmentOrder": 1.0, "sportType": map[string]any{"sportTypeId": 4.0, "sportTypeKey": "swimming"},
			"poolLength": nil, "poolLengthUnit": nil, "workoutSteps": []any{},
		}},
	})
	d := runTool(t, f, "get_workout_by_id", map[string]any{"workout_id": 1})
	if d["pool_length"] != 50.0 || d["pool_length_unit"] != "meter" {
		t.Errorf("d = %v", d)
	}
}

func TestGetWorkoutByIdToolRawReturnsGarminJson(t *testing.T) {
	f := workoutsFake(t)
	f.on(wRoute(123456), fixture(t, "MOCK_WORKOUT_DETAILS"))
	d := runTool(t, f, "get_workout_by_id", map[string]any{"workout_id": 123456, "raw": true})
	if !reflect.DeepEqual(d, fixture(t, "MOCK_WORKOUT_DETAILS")) {
		t.Errorf("raw = %v", d)
	}
}

func TestGetWorkoutByIdHandlesNullOptionalSections(t *testing.T) {
	w := fixture(t, "MOCK_WORKOUT_DETAILS").(map[string]any)
	w["sportType"] = nil
	seg := asMap(asList(w["workoutSegments"])[0])
	seg["sportType"], seg["workoutSteps"] = nil, nil
	f := workoutsFake(t)
	f.on(wRoute(123456), w)
	d := runTool(t, f, "get_workout_by_id", map[string]any{"workout_id": 123456})
	cs := asMap(toolList(t, "segments", d)[0])
	if d["id"] != 123456.0 || cs["order"] != 1.0 || cs["sport"] != nil || cs["steps"] != nil {
		t.Errorf("d = %v", d)
	}
}

func TestGetWorkoutByIdToolHandlesSwimSecondaryTargets(t *testing.T) {
	f := workoutsFake(t)
	f.on(wRoute(1528077786), fixture(t, "MOCK_SWIM_WORKOUT_DETAILS"))
	d := runTool(t, f, "get_workout_by_id", map[string]any{"workout_id": 1528077786})
	if d["id"] != 1528077786.0 || d["sport"] != "swimming" || d["estimated_distance_meters"] != 3000.0 {
		t.Errorf("d = %v", d)
	}
	seg := asMap(toolList(t, "segments", d)[0])
	if seg["step_count"] != 2.0 {
		t.Errorf("segment = %v", seg)
	}
	steps := toolList(t, "steps", seg)
	warm := asMap(steps[0])
	if warm["type"] != "warmup" || warm["secondary_target_type"] != "pace.zone" || warm["secondary_target_value_low"] != 0.45 ||
		warm["secondary_target_value_high"] != 0.6916667 || warm["target_type"] != nil {
		t.Errorf("warmup = %v", warm)
	}
	rep := asMap(steps[1])
	if rep["type"] != "repeat" || rep["repeat_count"] != 2.0 || rep["step_count"] != 2.0 {
		t.Errorf("repeat = %v", rep)
	}
	inner := toolList(t, "steps", rep)
	iv, rest := asMap(inner[0]), asMap(inner[1])
	if iv["type"] != "interval" || iv["secondary_target_type"] != "pace.zone" || iv["secondary_target_value_low"] != 0.7751938 || iv["secondary_target_value_high"] != 0.8583333 {
		t.Errorf("interval = %v", iv)
	}
	if rest["type"] != "rest" || rest["end_condition"] != "fixed.rest" || rest["end_condition_value"] != 60.0 {
		t.Errorf("rest = %v", rest)
	}
}

func TestGetWorkoutByIdToolIgnoresMalformedTargetBlocks(t *testing.T) {
	f := workoutsFake(t)
	f.on(wRoute(123457), map[string]any{
		"workoutId": 123457.0, "workoutName": "Malformed Swim Workout", "sportType": map[string]any{"sportTypeId": 4.0, "sportTypeKey": "swimming"},
		"workoutSegments": []any{map[string]any{
			"segmentOrder": 1.0, "sportType": map[string]any{"sportTypeId": 4.0, "sportTypeKey": "swimming"},
			"workoutSteps": []any{map[string]any{
				"type": "ExecutableStepDTO", "stepOrder": 1.0, "stepType": map[string]any{"stepTypeId": 1.0, "stepTypeKey": "warmup"},
				"endCondition": map[string]any{"conditionTypeId": 3.0, "conditionTypeKey": "distance"}, "endConditionValue": 100.0,
				"targetType": "pace.zone", "secondaryTargetType": []any{},
			}},
		}},
	})
	d := runTool(t, f, "get_workout_by_id", map[string]any{"workout_id": 123457})
	step := asMap(toolList(t, "steps", asMap(toolList(t, "segments", d)[0]))[0])
	if step["type"] != "warmup" || step["end_condition"] != "distance" || step["end_condition_value"] != 100.0 || step["target_type"] != nil || step["secondary_target_type"] != nil {
		t.Errorf("step = %v", step)
	}
}

func TestGetWorkoutByUuidTool(t *testing.T) {
	const uuid = "d7a5491b-42a5-4d2d-ba38-4e414fc03caf"
	f := workoutsFake(t)
	f.on("GET /workout-service/fbt-adaptive/"+uuid, map[string]any{
		"workoutId": nil, "workoutUuid": uuid, "workoutName": "Base", "description": "6:20/km",
		"sportType": map[string]any{"sportTypeId": 1.0, "sportTypeKey": "running"}, "estimatedDurationInSecs": 2160.0,
		"workoutPhrase": "AEROBIC_LOW_SHORTAGE_BASE", "trainingEffectLabel": "AEROBIC_BASE", "estimatedTrainingEffect": 2.3,
		"workoutSegments": []any{map[string]any{
			"segmentOrder": 1.0, "sportType": map[string]any{"sportTypeId": 1.0, "sportTypeKey": "running"},
			"workoutSteps": []any{map[string]any{
				"type": "ExecutableStepDTO", "stepOrder": 1.0, "stepType": map[string]any{"stepTypeId": 3.0, "stepTypeKey": "interval"},
				"endCondition": map[string]any{"conditionTypeId": 2.0, "conditionTypeKey": "time"}, "endConditionValue": 2160.0,
				"targetType":     map[string]any{"workoutTargetTypeId": 6.0, "workoutTargetTypeKey": "pace.zone"},
				"targetValueOne": 2.777, "targetValueTwo": 2.472,
			}},
		}},
	})
	d := runTool(t, f, "get_workout_by_id", map[string]any{"workout_id": uuid})
	if n := len(f.requests("GET /workout-service/fbt-adaptive/" + uuid)); n != 1 {
		t.Errorf("fbt-adaptive requests = %d", n)
	}
	if len(f.requests(wRoute("x"))) != 0 {
		t.Error("numeric endpoint used for a UUID")
	}
	if d["uuid"] != uuid || d["name"] != "Base" || d["sport"] != "running" || d["workout_type"] != "AEROBIC_LOW_SHORTAGE_BASE" ||
		d["training_effect_label"] != "AEROBIC_BASE" || d["estimated_training_effect"] != 2.3 || d["estimated_duration_seconds"] != 2160.0 {
		t.Errorf("d = %v", d)
	}
	if asMap(toolList(t, "segments", d)[0])["step_count"] != 1.0 {
		t.Errorf("d = %v", d)
	}
}

func TestGetWorkoutByIdFreeTextCannotEscapeItsEndpoint(t *testing.T) {
	var uri string
	f := workoutsFake(t)
	f.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.RequestURI, "fbt-adaptive") {
			uri = r.RequestURI
		}
		w.Write([]byte(`{"workoutName":"x"}`))
	})
	callTool(t, newSession(t, f, nil), "get_workout_by_id", map[string]any{"workout_id": "x-/../../foo?a=b#frag"})
	if !strings.HasPrefix(uri, "/workout-service/fbt-adaptive/x-%2F..%2F..%2Ffoo%3Fa=b%23frag") {
		t.Errorf("uri = %q", uri)
	}
}

func TestGetWorkoutByIdRejectsNonNumericId(t *testing.T) {
	text, isErr := callTool(t, newSession(t, workoutsFake(t), nil), "get_workout_by_id", map[string]any{"workout_id": "abc"})
	if !isErr || text != "Error retrieving workout: invalid literal for int() with base 10: 'abc'" {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestDownloadWorkoutTool(t *testing.T) {
	f := workoutsFake(t)
	f.on("GET /workout-service/workout/FIT/123456", rawBody("FITDATA"))
	d := runTool(t, f, "download_workout", map[string]any{"workout_id": 123456})
	if d["workout_id"] != 123456.0 || d["format"] != "FIT" || d["size_bytes"] != 7.0 || len(f.requests("GET /workout-service/workout/FIT/123456")) != 1 {
		t.Errorf("d = %v", d)
	}
	f.on("GET /workout-service/workout/FIT/5", rawBody(""))
	if text := callOK(t, newSession(t, f, nil), "download_workout", map[string]any{"workout_id": 5}); text != "No workout data found for workout with ID 5." {
		t.Errorf("text = %q", text)
	}
	if text, isErr := callTool(t, newSession(t, f, nil), "download_workout", map[string]any{"workout_id": 6}); !isErr || !strings.HasPrefix(text, "Error downloading workout: Download error: API Error 404") {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetWorkoutsNoData(t *testing.T) {
	f := workoutsFake(t)
	f.on(wList, nil)
	if text := callOK(t, newSession(t, f, nil), "get_workouts", nil); text != "No workouts found." {
		t.Errorf("text = %q", text)
	}
}

// ---- scheduled workouts and Garmin Coach ----

func scheduledQuery(t *testing.T, f *fakeGarmin) string {
	t.Helper()
	reqs := f.requests(wGraphQL)
	if len(reqs) != 1 {
		t.Fatalf("%d graphql requests", len(reqs))
	}
	return asMap(decode(t, string(reqs[0].Body)))["query"].(string)
}

func TestGetScheduledWorkoutsTool(t *testing.T) {
	f := workoutsFake(t)
	f.on(wGraphQL, map[string]any{"data": map[string]any{"workoutScheduleSummariesScalar": []any{map[string]any{
		"workoutUuid": "abc-123-def", "workoutId": 123456.0, "workoutName": "5K Tempo Run", "workoutType": "running",
		"scheduleDate": "2024-01-15", "tpPlanName": "5K Training Plan", "associatedActivityId": nil,
		"estimatedDurationInSecs": 1800.0, "estimatedDistanceInMeters": 5000.0,
	}}}})
	d := runTool(t, f, "get_scheduled_workouts", map[string]any{"start_date": "2024-01-08", "end_date": "2024-01-15"})
	w := asMap(toolList(t, "scheduled_workouts", d)[0])
	if d["count"] != 1.0 || w["name"] != "5K Tempo Run" || w["sport"] != "running" || w["completed"] != false ||
		w["training_plan"] != "5K Training Plan" || w["estimated_duration_seconds"] != 1800.0 {
		t.Errorf("d = %v", d)
	}
	if want := `query{workoutScheduleSummariesScalar(startDate:"2024-01-08", endDate:"2024-01-15")}`; scheduledQuery(t, f) != want {
		t.Errorf("query = %q", scheduledQuery(t, f))
	}
}

func TestGetScheduledWorkoutsPreservesManualShapeAndAddsPlanIds(t *testing.T) {
	f := workoutsFake(t)
	f.on(wGraphQL, map[string]any{"data": map[string]any{"workoutScheduleSummariesScalar": []any{
		map[string]any{
			"scheduledWorkoutId": 1001.0, "workoutUuid": nil, "workoutId": 2001.0, "workoutName": "Manual Ride", "workoutType": "cycling",
			"scheduleDate": "2024-01-15", "associatedActivityId": nil, "trainingPlanId": nil, "fbtAdaptivePlanId": nil, "tpType": nil,
		},
		map[string]any{
			"scheduledWorkoutId": nil, "workoutUuid": "abc-123-def", "workoutId": nil, "workoutName": "Coach Ride", "workoutType": "cycling",
			"scheduleDate": "2024-01-16", "associatedActivityId": nil, "trainingPlanId": 3001.0, "fbtAdaptivePlanId": 3001.0, "tpType": nil,
		},
	}}})
	sched := toolList(t, "scheduled_workouts", runTool(t, f, "get_scheduled_workouts", map[string]any{"start_date": "2024-01-15", "end_date": "2024-01-16"}))
	want := map[string]any{"date": "2024-01-15", "scheduled_workout_id": 1001.0, "workout_id": 2001.0, "name": "Manual Ride", "sport": "cycling", "completed": false}
	if !reflect.DeepEqual(sched[0], want) {
		t.Errorf("manual = %v", sched[0])
	}
	s1 := asMap(sched[1])
	if _, has := s1["tp_type"]; s1["training_plan_id"] != 3001.0 || s1["fbt_adaptive_plan_id"] != 3001.0 || has {
		t.Errorf("coach = %v", s1)
	}
}

func coachFixture() map[string]any {
	return map[string]any{"data": map[string]any{"trainingPlanScalar": map[string]any{"trainingPlanWorkoutScheduleDTOS": []any{map[string]any{
		"trainingPlanId": 12345.0, "planName": "5K Training Plan", "trainingPlanClassification": "FBT_ADAPTIVE",
		"trainingPlanDetailsDTO": map[string]any{
			"athletePlanId": 12345.0, "workoutsPerWeek": 4.0, "remainingAtpWorkoutsForWeek": 2.0, "trainingType": "RUNNING",
			"registrationDate": "2024-01-01", "planCompleted": false,
			"athleteRace": map[string]any{"raceName": "City 5K", "raceDay": "2024-03-10"},
		},
		"workoutScheduleSummaries": []any{
			map[string]any{
				"workoutUuid": "abc-123-def", "workoutId": nil, "workoutName": "Base Run", "workoutType": "running", "scheduleDate": "2024-01-15",
				"tpPlanName": "5K Training Plan", "associatedActivityId": nil, "estimatedDurationInSecs": 1800.0,
				"trainingPlanId": 12345.0, "fbtAdaptivePlanId": 12345.0, "tpType": nil,
			},
			map[string]any{
				"workoutUuid": "xyz-456-ghi", "workoutId": nil, "workoutName": "Strength", "workoutType": "strength_training", "scheduleDate": "2024-01-15",
				"tpPlanName": "5K Training Plan", "associatedActivityId": 987654.0, "estimatedDurationInSecs": 1200.0,
				"trainingPlanId": 12345.0, "fbtAdaptivePlanId": 12345.0, "tpType": nil,
			},
		},
	}}}}}
}

func TestGetGarminCoachWorkoutTools(t *testing.T) {
	for _, tool := range []string{"get_garmin_coach_workouts", "get_training_plan_workouts"} {
		f := workoutsFake(t)
		f.on(wGraphQL, coachFixture())
		d := runTool(t, f, tool, map[string]any{"calendar_date": "2024-01-15"})
		if want := `query{trainingPlanScalar(calendarDate:"2024-01-15", lang:"en-US", firstDayOfWeek:"monday")}`; scheduledQuery(t, f) != want {
			t.Errorf("%s: query = %q", tool, scheduledQuery(t, f))
		}
		wantPlans := []any{map[string]any{
			"name": "5K Training Plan", "training_plan_id": 12345.0, "classification": "FBT_ADAPTIVE", "training_type": "RUNNING",
			"race_name": "City 5K", "race_day": "2024-03-10", "workouts_per_week": 4.0, "remaining_workouts_this_week": 2.0,
			"registration_date": "2024-01-01", "plan_completed": false,
		}}
		if d["date"] != "2024-01-15" || !reflect.DeepEqual(d["training_plans"], []any{"5K Training Plan"}) || !reflect.DeepEqual(d["plans"], wantPlans) || d["count"] != 2.0 {
			t.Errorf("%s: d = %v", tool, d)
		}
		ws := toolList(t, "workouts", d)
		w0, w1 := asMap(ws[0]), asMap(ws[1])
		if _, has := w0["tp_type"]; w0["name"] != "Base Run" || w0["sport"] != "running" || w0["completed"] != false || w0["training_plan_id"] != 12345.0 || w0["fbt_adaptive_plan_id"] != 12345.0 || has {
			t.Errorf("%s: w0 = %v", tool, w0)
		}
		if w1["name"] != "Strength" || w1["sport"] != "strength_training" || w1["training_plan_id"] != 12345.0 || w1["fbt_adaptive_plan_id"] != 12345.0 || w1["completed"] != true || w1["activity_id"] != 987654.0 {
			t.Errorf("%s: w1 = %v", tool, w1)
		}
	}
}

func TestGetGarminCoachWorkoutsStpNumericIds(t *testing.T) {
	f := workoutsFake(t)
	f.on(wGraphQL, map[string]any{"data": map[string]any{"trainingPlanScalar": map[string]any{"trainingPlanWorkoutScheduleDTOS": []any{map[string]any{
		"trainingPlanId": 67890.0, "planName": "Push, Pull, Legs", "trainingPlanClassification": "STP",
		"trainingPlanDetailsDTO": map[string]any{"athletePlanId": 67890.0, "trainingType": "STRENGTH"},
		"workoutScheduleSummaries": []any{map[string]any{
			"scheduledWorkoutId": 111.0, "workoutUuid": nil, "workoutId": 222.0, "workoutName": "Leg Day", "workoutType": "strength_training",
			"scheduleDate": "2024-01-15", "tpPlanName": "Push, Pull, Legs", "associatedActivityId": nil, "trainingPlanId": 67890.0,
			"fbtAdaptivePlanId": nil, "tpType": "STP",
		}},
	}}}}})
	d := runTool(t, f, "get_garmin_coach_workouts", map[string]any{"calendar_date": "2024-01-15"})
	wantPlans := []any{map[string]any{"name": "Push, Pull, Legs", "training_plan_id": 67890.0, "classification": "STP", "training_type": "STRENGTH"}}
	w := asMap(toolList(t, "workouts", d)[0])
	_, hasUUID := w["workout_uuid"]
	_, hasFbt := w["fbt_adaptive_plan_id"]
	if !reflect.DeepEqual(d["training_plans"], []any{"Push, Pull, Legs"}) || !reflect.DeepEqual(d["plans"], wantPlans) ||
		w["workout_id"] != 222.0 || hasUUID || w["training_plan_id"] != 67890.0 || w["tp_type"] != "STP" || hasFbt {
		t.Errorf("d = %v", d)
	}
}

func TestGetGarminCoachWorkoutsIncludesRestDays(t *testing.T) {
	f := workoutsFake(t)
	f.on(wGraphQL, map[string]any{"data": map[string]any{"trainingPlanScalar": map[string]any{"trainingPlanWorkoutScheduleDTOS": []any{map[string]any{
		"trainingPlanId": 12345.0, "planName": "Adaptive Plan", "trainingPlanClassification": "FBT_ADAPTIVE",
		"trainingPlanDetailsDTO": map[string]any{"trainingType": "CYCLING"},
		"workoutScheduleSummaries": []any{map[string]any{
			"workoutUuid": "rest-123", "workoutId": nil, "workoutName": nil, "workoutType": nil, "scheduleDate": "2024-01-15",
			"associatedActivityId": nil, "trainingPlanId": 12345.0, "fbtAdaptivePlanId": 12345.0, "tpType": nil, "isRestDay": true,
		}},
	}}}}})
	d := runTool(t, f, "get_garmin_coach_workouts", map[string]any{"calendar_date": "2024-01-15"})
	r := asMap(toolList(t, "workouts", d)[0])
	_, hasName := r["name"]
	_, hasSport := r["sport"]
	if d["count"] != 1.0 || r["workout_uuid"] != "rest-123" || r["is_rest_day"] != true || r["training_plan_id"] != 12345.0 || r["fbt_adaptive_plan_id"] != 12345.0 || hasName || hasSport {
		t.Errorf("rest day = %v", r)
	}
}

func TestGetGarminCoachWorkoutsHandlesMalformedPlanEntries(t *testing.T) {
	f := workoutsFake(t)
	f.on(wGraphQL, map[string]any{"data": map[string]any{"trainingPlanScalar": map[string]any{"trainingPlanWorkoutScheduleDTOS": []any{
		nil,
		map[string]any{"planName": "Adaptive Plan", "trainingPlanDetailsDTO": []any{}, "workoutScheduleSummaries": []any{
			nil,
			map[string]any{"workoutUuid": "abc-123", "workoutName": "Base Run", "workoutType": "running", "scheduleDate": "2024-01-15", "associatedActivityId": nil},
		}},
	}}}})
	d := runTool(t, f, "get_garmin_coach_workouts", map[string]any{"calendar_date": "2024-01-15"})
	if !reflect.DeepEqual(d["training_plans"], []any{"Adaptive Plan"}) || d["count"] != 1.0 || asMap(toolList(t, "workouts", d)[0])["workout_uuid"] != "abc-123" {
		t.Errorf("d = %v", d)
	}
}

func TestGetGarminCoachWorkoutsHandlesMissingPlanData(t *testing.T) {
	const noData, noPlans = "No training plan data found or error querying data.", "No training plan workouts scheduled for 2024-01-15."
	plan := func(v any) any {
		return map[string]any{"data": map[string]any{"trainingPlanScalar": map[string]any{"trainingPlanWorkoutScheduleDTOS": v}}}
	}
	for i, tc := range []struct {
		response any
		want     string
	}{
		{nil, noData},
		{map[string]any{"data": nil}, noData},
		{map[string]any{"data": map[string]any{"trainingPlanScalar": nil}}, noPlans},
		{plan([]any{}), noPlans},
		{plan([]any{nil}), noPlans},
	} {
		f := workoutsFake(t)
		f.on(wGraphQL, tc.response)
		if text := callOK(t, newSession(t, f, nil), "get_garmin_coach_workouts", map[string]any{"calendar_date": "2024-01-15"}); text != tc.want {
			t.Errorf("case %d: %q, want %q", i, text, tc.want)
		}
	}
}

func TestGetGarminCoachWorkoutsRejectsInvalidDate(t *testing.T) {
	f := workoutsFake(t)
	text, isErr := callTool(t, newSession(t, f, nil), "get_garmin_coach_workouts", map[string]any{"calendar_date": "2024-01-15-invalid"})
	if !isErr || text != "Error retrieving Garmin Coach workouts: Invalid calendar_date '2024-01-15-invalid': expected YYYY-MM-DD" {
		t.Errorf("%v %q", isErr, text)
	}
	if n := len(f.requests(wGraphQL)); n != 0 {
		t.Errorf("%d graphql requests", n)
	}
}

func TestGetTrainingPlanWorkoutsNoActivePlan(t *testing.T) {
	f := workoutsFake(t)
	f.on(wGraphQL, map[string]any{"data": map[string]any{"trainingPlanScalar": nil}})
	if text := callOK(t, newSession(t, f, nil), "get_training_plan_workouts", map[string]any{"calendar_date": "2024-01-15"}); text != "No training plan workouts scheduled for 2024-01-15." {
		t.Errorf("text = %q", text)
	}
	if len(f.requests(wGraphQL)) != 1 {
		t.Error("graphql not called once")
	}
}

func TestGetTrainingPlanWorkoutsNullScheduleSummaries(t *testing.T) {
	f := workoutsFake(t)
	f.on(wGraphQL, map[string]any{"data": map[string]any{"trainingPlanScalar": map[string]any{"trainingPlanWorkoutScheduleDTOS": []any{
		map[string]any{"planName": "Completed Garmin Run Coach Plan", "workoutScheduleSummaries": nil},
	}}}})
	d := runTool(t, f, "get_training_plan_workouts", map[string]any{"calendar_date": "2024-01-15"})
	if !reflect.DeepEqual(d["training_plans"], []any{"Completed Garmin Run Coach Plan"}) || d["count"] != 0.0 || !reflect.DeepEqual(d["workouts"], []any{}) {
		t.Errorf("d = %v", d)
	}
}

func TestGetScheduledWorkoutsExposesScheduledId(t *testing.T) {
	f := workoutsFake(t)
	f.on(wGraphQL, map[string]any{"data": map[string]any{"workoutScheduleSummariesScalar": []any{map[string]any{
		"scheduledWorkoutId": 555.0, "workoutUuid": nil, "workoutId": 123456.0, "workoutName": "5K Tempo Run", "workoutType": "running",
		"scheduleDate": "2024-01-15", "associatedActivityId": nil,
	}}}})
	w := asMap(toolList(t, "scheduled_workouts", runTool(t, f, "get_scheduled_workouts", map[string]any{"start_date": "2024-01-08", "end_date": "2024-01-15"}))[0])
	if w["scheduled_workout_id"] != 555.0 || w["workout_id"] != 123456.0 {
		t.Errorf("w = %v", w)
	}
}

func TestGetScheduledWorkoutsHandlesNullGraphqlData(t *testing.T) {
	f := workoutsFake(t)
	f.on(wGraphQL, map[string]any{"data": nil})
	text := callOK(t, newSession(t, f, nil), "get_scheduled_workouts", map[string]any{"start_date": "2024-01-08", "end_date": "2024-01-15"})
	if strings.Contains(text, "NoneType") || !strings.Contains(text, "No workouts scheduled") {
		t.Errorf("text = %q", text)
	}
}
