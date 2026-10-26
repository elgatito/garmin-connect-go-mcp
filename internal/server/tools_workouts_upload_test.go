package server

import (
	"regexp"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"reflect"
	"strings"
	"testing"
)

// Ported from tests/integration/test_workouts_tools.py (upload tools).

// uploadedWorkout is the decoded body of the single upload request.
func uploadedWorkout(t *testing.T, f *fakeGarmin) map[string]any { return putJSON(t, f, wUpload) }

func sentStep(w map[string]any, idx ...int) map[string]any {
	steps := asList(asMap(asList(w["workoutSegments"])[0])["workoutSteps"])
	step := asMap(steps[idx[0]])
	for _, i := range idx[1:] {
		step = asMap(asList(step["workoutSteps"])[i])
	}
	return step
}

func uploadFake(t *testing.T, result any) *fakeGarmin {
	f := workoutsFake(t)
	f.on(wUpload, result)
	return f
}

func hrZoneTarget() map[string]any {
	return map[string]any{"workoutTargetTypeId": 4.0, "workoutTargetTypeKey": "heart.rate.zone"}
}

func TestUploadRepairsNullHrZoneAndNumericStrings(t *testing.T) {
	for _, tool := range []string{"upload_workout", "upload_workouts", "schedule_workouts"} {
		for _, tc := range []struct {
			zone     any
			nullZone bool
		}{{3.0, true}, {"3", true}, {"3", false}} {
			step := timedIntervalStep(hrZoneTarget())
			step["targetValueOne"], step["targetValueTwo"] = tc.zone, nil
			if tc.nullZone {
				step["zoneNumber"] = nil
			}
			payload := runningWorkoutWithSteps(step)
			f := uploadFake(t, map[string]any{"workoutId": 123.0})
			f.on("POST /workout-service/schedule/123", map[string]any{})
			args := map[string]map[string]any{
				"upload_workout":    {"workout_data": payload},
				"upload_workouts":   {"workouts": []any{payload}},
				"schedule_workouts": {"schedules": []any{map[string]any{"calendar_date": "2026-10-20", "workout_data": payload}}},
			}[tool]
			d := runTool(t, f, tool, args)
			status := d["status"]
			if status == nil {
				status = asMap(toolList(t, "results", d)[0])["status"]
			}
			ss := sentStep(uploadedWorkout(t, f), 0)
			_, has1 := ss["targetValueOne"]
			_, has2 := ss["targetValueTwo"]
			if status != "success" || ss["zoneNumber"] != 3.0 || has1 || has2 {
				t.Errorf("%s %v: status %v step %v", tool, tc, status, ss)
			}
		}
	}
}

func TestUploadBackfillsExplicitNullRepeatCount(t *testing.T) {
	group := map[string]any{
		"type": "RepeatGroupDTO", "numberOfIterations": nil,
		"endCondition": map[string]any{"conditionTypeId": 7.0, "conditionTypeKey": "iterations"}, "endConditionValue": 4.0, "workoutSteps": []any{},
	}
	f := uploadFake(t, map[string]any{"workoutId": 123.0})
	d := runTool(t, f, "upload_workout", map[string]any{"workout_data": runningWorkoutWithSteps(group)})
	if d["status"] != "success" || sentStep(uploadedWorkout(t, f), 0)["numberOfIterations"] != 4.0 {
		t.Errorf("d = %v", d)
	}
}

func TestUploadNormalizesStringConditionAndTargetIds(t *testing.T) {
	step := timedIntervalStep(map[string]any{"workoutTargetTypeId": "1", "workoutTargetTypeKey": "no.target"})
	step["endCondition"].(map[string]any)["conditionTypeId"] = "2"
	f := uploadFake(t, map[string]any{"workoutId": 123.0})
	d := runTool(t, f, "upload_workout", map[string]any{"workout_data": runningWorkoutWithSteps(step)})
	ss := sentStep(uploadedWorkout(t, f), 0)
	if d["status"] != "success" || asMap(ss["endCondition"])["conditionTypeId"] != 2.0 || asMap(ss["targetType"])["workoutTargetTypeId"] != 1.0 {
		t.Errorf("step = %v", ss)
	}
}

func TestUploadNullStepListsHaveAFieldError(t *testing.T) {
	for _, shape := range []string{"segments", "steps", "nested_steps"} {
		payload := runningWorkoutWithSteps()
		path := "workoutSegments"
		seg := asMap(asList(payload["workoutSegments"])[0])
		switch shape {
		case "segments":
			payload["workoutSegments"] = nil
		case "steps":
			seg["workoutSteps"] = nil
			path += "[0].workoutSteps"
		default:
			seg["workoutSteps"] = []any{map[string]any{"type": "RepeatGroupDTO", "workoutSteps": nil}}
			path += "[0].workoutSteps[0].workoutSteps"
		}
		f := uploadFake(t, map[string]any{})
		text, isErr := callTool(t, newSession(t, f, nil), "upload_workout", map[string]any{"workout_data": payload})
		if !isErr || !strings.Contains(text, path+" must be a list") || len(f.requests(wUpload)) != 0 {
			t.Errorf("%s: %v %q", shape, isErr, text)
		}
	}
}

func TestUploadWorkoutTool(t *testing.T) {
	f := uploadFake(t, map[string]any{"workoutId": 123457.0, "workoutName": "New Workout"})
	data := map[string]any{"workoutName": "New Workout", "sportType": map[string]any{"sportTypeId": 1.0}}
	d := runTool(t, f, "upload_workout", map[string]any{"workout_data": data})
	want := map[string]any{"status": "success", "workout_id": 123457.0, "name": "New Workout", "message": "Workout uploaded successfully"}
	if !reflect.DeepEqual(d, want) || !reflect.DeepEqual(uploadedWorkout(t, f), data) {
		t.Errorf("d = %v", d)
	}
}

func toolDescriptions(t *testing.T) map[string]string {
	t.Helper()
	res, err := newSession(t, newFakeGarmin(t), nil).ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, tool := range res.Tools {
		out[tool.Name] = tool.Description
	}
	return out
}

func TestUploadWorkoutSportTypeIdsMatchReference(t *testing.T) {
	desc := toolDescriptions(t)["upload_workout"]
	line := strings.Split(strings.SplitN(desc, "Sport type IDs for workouts", 2)[1], "\n")[1]
	ref := templateReference(t)["sportType_values"].(map[string]any)
	pairs := regexpFindPairs(line)
	found12 := false
	for _, p := range pairs {
		found12 = found12 || (p[0] == "12" && p[1] == "walking")
		if asMap(ref[p[0]])["sportTypeKey"] != p[1] {
			t.Errorf("sport %s = %s, reference %v", p[0], p[1], ref[p[0]])
		}
	}
	if !found12 || len(pairs) == 0 {
		t.Errorf("pairs = %v", pairs)
	}
}

func TestWorkoutUploadToolsDocumentPaceUnits(t *testing.T) {
	d := toolDescriptions(t)
	for _, name := range []string{"upload_workout", "upload_workouts", "schedule_workouts"} {
		if !strings.Contains(d[name], "3.333 m/s") {
			t.Errorf("%s does not document m/s pace", name)
		}
	}
}

func cyclingWorkoutWithSteps(name string, steps ...any) map[string]any {
	return map[string]any{
		"workoutName": name, "sportType": map[string]any{"sportTypeId": 2.0, "sportTypeKey": "cycling"},
		"workoutSegments": []any{map[string]any{
			"segmentOrder": 1.0, "sportType": map[string]any{"sportTypeId": 2.0, "sportTypeKey": "cycling"}, "workoutSteps": steps,
		}},
	}
}

func TestUploadWorkoutPassesPercentFtpRangeThrough(t *testing.T) {
	data := cyclingWorkoutWithSteps("Sweet spot", map[string]any{
		"type": "ExecutableStepDTO", "stepOrder": 1.0, "stepType": map[string]any{"stepTypeId": 3.0, "stepTypeKey": "interval"},
		"endCondition": map[string]any{"conditionTypeId": 2.0, "conditionTypeKey": "time"}, "endConditionValue": 600.0,
		"targetType":     map[string]any{"workoutTargetTypeId": 2.0, "workoutTargetTypeKey": "power.zone"},
		"targetValueOne": 76.0, "targetValueTwo": 81.0, "targetValueUnit": map[string]any{"unitKey": "percent"},
	})
	f := uploadFake(t, map[string]any{"workoutId": 1.0, "workoutName": "Sweet spot"})
	runTool(t, f, "upload_workout", map[string]any{"workout_data": data})
	if !reflect.DeepEqual(uploadedWorkout(t, f), jsonValue(t, data)) {
		t.Errorf("sent = %v", uploadedWorkout(t, f))
	}
}

// uploadOK uploads a repairable workout and returns the step the server sent.
func uploadStepVia(t *testing.T, tool string, workout map[string]any, idx ...int) (map[string]any, map[string]any) {
	t.Helper()
	f := uploadFake(t, map[string]any{"workoutId": 210001.0, "workoutName": "x"})
	f.on("POST /workout-service/schedule/210001", map[string]any{})
	args := map[string]any{"workout_data": workout}
	switch tool {
	case "upload_workouts":
		args = map[string]any{"workouts": []any{workout}}
	case "schedule_workouts":
		args = map[string]any{"schedules": []any{map[string]any{"workout_data": workout, "calendar_date": "2024-02-01"}}}
	}
	d := runTool(t, f, tool, args)
	return sentStep(uploadedWorkout(t, f), idx...), d
}

func TestUploadWorkoutPromotesBoundsNestedInsideTargetType(t *testing.T) {
	for _, tool := range []string{"upload_workout", "upload_workouts", "schedule_workouts"} { // also the batch / inline variants
		step, d := uploadStepVia(t, tool, runningWorkoutWithSteps(distancePaceStepWithNestedBounds()), 0)
		tt := asMap(step["targetType"])
		_, a := tt["targetValueOne"]
		_, b := tt["targetValueTwo"]
		if step["targetValueOne"] != 2.0833333 || step["targetValueTwo"] != 1.9607843 || a || b {
			t.Errorf("%s: step = %v", tool, step)
		}
		if tool == "upload_workout" && d["status"] != "success" {
			t.Errorf("d = %v", d)
		}
	}
}

func TestUploadWorkoutPromotesNestedBoundsInsideRepeatGroup(t *testing.T) {
	group := map[string]any{
		"type": "RepeatGroupDTO", "stepOrder": 1.0, "numberOfIterations": 3.0,
		"endCondition": map[string]any{"conditionTypeId": 7.0, "conditionTypeKey": "iterations"}, "endConditionValue": 3.0,
		"workoutSteps": []any{distancePaceStepWithNestedBounds()},
	}
	step, _ := uploadStepVia(t, "upload_workout", runningWorkoutWithSteps(group), 0, 0)
	keys := []string{}
	for k := range asMap(step["targetType"]) {
		keys = append(keys, k)
	}
	if step["targetValueOne"] != 2.0833333 || step["targetValueTwo"] != 1.9607843 || len(keys) != 2 {
		t.Errorf("step = %v", step)
	}
}

func uploadError(t *testing.T, workout map[string]any) string {
	t.Helper()
	f := uploadFake(t, map[string]any{"workoutId": 1.0})
	text, isErr := callTool(t, newSession(t, f, nil), "upload_workout", map[string]any{"workout_data": workout})
	if !isErr || len(f.requests(wUpload)) != 0 {
		t.Fatalf("expected a rejection before upload: %v %q", isErr, text)
	}
	return text
}

func TestUploadWorkoutRejectsConflictingNestedAndStepBounds(t *testing.T) {
	step := distancePaceStepWithNestedBounds()
	step["targetValueOne"] = 2.5
	msg := uploadError(t, runningWorkoutWithSteps(step))
	if !strings.Contains(msg, "workoutSegments[0].workoutSteps[0].targetValueOne=2.5 conflicts with workoutSegments[0].workoutSteps[0].targetType.targetValueOne=2.0833333") ||
		!strings.Contains(msg, "keep only the step-level targetValueOne") {
		t.Errorf("msg = %q", msg)
	}
}

func TestUploadWorkoutPromotesZoneNestedInsideTargetType(t *testing.T) {
	step := distancePaceStepWithNestedBounds()
	step["targetType"] = map[string]any{"workoutTargetTypeId": 4.0, "workoutTargetTypeKey": "heart.rate.zone", "zoneNumber": 3.0}
	s, _ := uploadStepVia(t, "upload_workout", runningWorkoutWithSteps(step), 0)
	if _, has := asMap(s["targetType"])["zoneNumber"]; s["zoneNumber"] != 3.0 || has {
		t.Errorf("step = %v", s)
	}
}

func TestUploadWorkoutPromotesNestedHrZoneValueBeforeHrFix(t *testing.T) {
	step := distancePaceStepWithNestedBounds()
	step["targetType"] = map[string]any{"workoutTargetTypeId": 4.0, "workoutTargetTypeKey": "heart.rate.zone", "targetValueOne": 3.0}
	s, _ := uploadStepVia(t, "upload_workout", runningWorkoutWithSteps(step), 0)
	_, a := s["targetValueOne"]
	_, b := asMap(s["targetType"])["targetValueOne"]
	if s["zoneNumber"] != 3.0 || a || b {
		t.Errorf("step = %v", s)
	}
}

func TestUploadWorkoutPromotesBoundsNestedInsideSecondaryTargetType(t *testing.T) {
	step := distancePaceStepWithNestedBounds()
	step["targetType"] = nil
	step["secondaryTargetType"] = map[string]any{
		"workoutTargetTypeId": 6.0, "workoutTargetTypeKey": "pace.zone", "secondaryTargetValueOne": 0.45, "secondaryTargetValueTwo": 0.6916667,
	}
	s, _ := uploadStepVia(t, "upload_workout", runningWorkoutWithSteps(step), 0)
	st := asMap(s["secondaryTargetType"])
	_, a := st["secondaryTargetValueOne"]
	_, b := st["secondaryTargetValueTwo"]
	if s["secondaryTargetValueOne"] != 0.45 || s["secondaryTargetValueTwo"] != 0.6916667 || a || b {
		t.Errorf("step = %v", s)
	}
}

func TestUploadWorkoutRejectsZoneMixedWithCustomRange(t *testing.T) {
	step := distancePaceStepWithNestedBounds()
	step["zoneNumber"] = 3.0
	msg := uploadError(t, runningWorkoutWithSteps(step))
	if !strings.Contains(msg, "mixes zoneNumber=3 with custom range fields") || !strings.Contains(msg, "use either a named zone or a custom range") {
		t.Errorf("msg = %q", msg)
	}
}

func TestUploadWorkoutFixesHrZoneTarget(t *testing.T) {
	step := map[string]any{
		"type": "ExecutableStepDTO", "stepOrder": 1.0, "stepType": map[string]any{"stepTypeId": 3.0, "stepTypeKey": "interval"},
		"endCondition": map[string]any{"conditionTypeId": 2.0, "conditionTypeKey": "time"}, "endConditionValue": 600.0,
		"targetType": hrZoneTarget(), "targetValueOne": 3.0,
	}
	s, d := uploadStepVia(t, "upload_workout", runningWorkoutWithSteps(step), 0)
	_, a := s["targetValueOne"]
	_, b := s["targetValueTwo"]
	if s["zoneNumber"] != 3.0 || a || b || d["status"] != "success" {
		t.Errorf("step = %v, d = %v", s, d)
	}
}

func TestUploadWorkoutFixesHrZoneInRepeatGroup(t *testing.T) {
	group := map[string]any{
		"type": "RepeatGroupDTO", "stepOrder": 1.0, "numberOfIterations": 2.0,
		"workoutSteps": []any{
			map[string]any{
				"type": "ExecutableStepDTO", "stepOrder": 1.0, "stepType": map[string]any{"stepTypeId": 3.0, "stepTypeKey": "interval"},
				"endCondition": map[string]any{"conditionTypeId": 2.0, "conditionTypeKey": "time"}, "endConditionValue": 600.0,
				"targetType": hrZoneTarget(), "targetValueOne": 3.0, "targetValueTwo": 3.0,
			},
			map[string]any{
				"type": "ExecutableStepDTO", "stepOrder": 2.0, "stepType": map[string]any{"stepTypeId": 4.0, "stepTypeKey": "recovery"},
				"endCondition": map[string]any{"conditionTypeId": 2.0, "conditionTypeKey": "time"}, "endConditionValue": 240.0,
				"targetType": map[string]any{"workoutTargetTypeId": 1.0, "workoutTargetTypeKey": "no.target"},
			},
		},
	}
	s, d := uploadStepVia(t, "upload_workout", runningWorkoutWithSteps(group), 0, 0)
	_, a := s["targetValueOne"]
	_, b := s["targetValueTwo"]
	if s["zoneNumber"] != 3.0 || a || b || d["status"] != "success" {
		t.Errorf("step = %v", s)
	}
}

func recoveryStep(conditionID any, key string) map[string]any {
	ec := map[string]any{"conditionTypeKey": key}
	if conditionID != nil {
		ec["conditionTypeId"] = conditionID
	}
	return map[string]any{
		"type": "ExecutableStepDTO", "stepOrder": 1.0, "stepType": map[string]any{"stepTypeId": 4.0, "stepTypeKey": "recovery"},
		"endCondition": ec, "endConditionValue": 145.0,
		"targetType": map[string]any{"workoutTargetTypeId": 1.0, "workoutTargetTypeKey": "no.target"},
	}
}

func TestUploadWorkoutRejectsMismatchedEndConditionId(t *testing.T) {
	msg := uploadError(t, runningWorkoutWithSteps(recoveryStep(4.0, "heart.rate")))
	for _, want := range []string{"Error uploading workout", "conditionTypeKey 'heart.rate' requires conditionTypeId 6", "got 4 (calories)"} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in %q", want, msg)
		}
	}
}

func TestUploadWorkoutRejectsTargetTypeMismatch(t *testing.T) {
	msg := uploadError(t, runningWorkoutWithSteps(timedIntervalStep(map[string]any{"workoutTargetTypeId": 6.0, "workoutTargetTypeKey": "heart.rate"})))
	// ID 6 is valid for 'pace.zone' (running) and 'power.between' (cycling), not 'heart.rate'
	for _, want := range []string{"targetType mismatch", "workoutTargetTypeId 6 is one of", "not 'heart.rate'", "('pace.zone', 'power.between')"} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in %q", want, msg)
		}
	}
}

func TestUploadWorkoutAcceptsHeartRateEndConditionId(t *testing.T) {
	data := runningWorkoutWithSteps(recoveryStep(6.0, "heart.rate"))
	f := uploadFake(t, map[string]any{"workoutId": 123460.0, "workoutName": "Validation Workout"})
	runTool(t, f, "upload_workout", map[string]any{"workout_data": data})
	if !reflect.DeepEqual(uploadedWorkout(t, f), jsonValue(t, data)) {
		t.Errorf("sent = %v", uploadedWorkout(t, f))
	}
}

func TestUploadWorkoutAcceptsCustomHrRange(t *testing.T) {
	s, d := uploadStepVia(t, "upload_workout", runningWorkoutWithSteps(timedIntervalStep(hrZoneTarget())), 0)
	if _, has := s["zoneNumber"]; s["targetValueOne"] != 143.0 || s["targetValueTwo"] != 157.0 || has || d["status"] != "success" {
		t.Errorf("step = %v", s)
	}
}

func TestUploadWorkoutRejectsNestedEndConditionMismatch(t *testing.T) {
	group := map[string]any{
		"type": "RepeatGroupDTO", "stepOrder": 1.0, "numberOfIterations": 2.0,
		"endCondition": map[string]any{"conditionTypeId": 7.0, "conditionTypeKey": "iterations"},
		"workoutSteps": []any{recoveryStep(4.0, "heart.rate")},
	}
	msg := uploadError(t, runningWorkoutWithSteps(group))
	if !strings.Contains(msg, "workoutSegments[0].workoutSteps[0].workoutSteps[0]") || !strings.Contains(msg, "conditionTypeKey 'heart.rate' requires conditionTypeId 6") {
		t.Errorf("msg = %q", msg)
	}
}

func TestUploadWorkoutRejectsNestedTargetTypeMismatch(t *testing.T) {
	bad := timedIntervalStep(map[string]any{"workoutTargetTypeId": 6.0, "workoutTargetTypeKey": "heart.rate"})
	group := map[string]any{"type": "RepeatGroupDTO", "stepOrder": 1.0, "numberOfIterations": 2.0, "workoutSteps": []any{bad}}
	if msg := uploadError(t, runningWorkoutWithSteps(group)); !strings.Contains(msg, "workoutSegments[0].workoutSteps[0].workoutSteps[0].targetType mismatch") {
		t.Errorf("msg = %q", msg)
	}
}

func TestUploadWorkoutRejectsMissingEndConditionId(t *testing.T) {
	msg := uploadError(t, runningWorkoutWithSteps(recoveryStep(nil, "heart.rate")))
	if !strings.Contains(msg, "conditionTypeKey 'heart.rate' requires conditionTypeId 6") {
		t.Errorf("msg = %q", msg)
	}
}

func secondaryHRMismatchStep() map[string]any {
	step := timedIntervalStep(nil)
	step["secondaryTargetType"] = map[string]any{"workoutTargetTypeId": 6.0, "workoutTargetTypeKey": "heart.rate"}
	step["secondaryTargetValueOne"], step["secondaryTargetValueTwo"] = 143.0, 157.0
	return step
}

func TestUploadWorkoutRejectsSecondaryTargetTypeMismatch(t *testing.T) {
	msg := uploadError(t, runningWorkoutWithSteps(secondaryHRMismatchStep()))
	for _, want := range []string{"secondaryTargetType mismatch", "workoutTargetTypeId 6 is one of", "not 'heart.rate'"} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in %q", want, msg)
		}
	}
}

func TestUploadWorkoutAcceptsSecondaryTargetTypeWithNullPrimary(t *testing.T) {
	step := timedIntervalStep(nil)
	step["secondaryTargetType"] = map[string]any{"workoutTargetTypeId": 6.0, "workoutTargetTypeKey": "pace.zone"}
	step["secondaryTargetValueOne"], step["secondaryTargetValueTwo"] = 0.45, 0.6916667
	s, d := uploadStepVia(t, "upload_workout", runningWorkoutWithSteps(step), 0)
	if _, has := s["targetType"]; !has || s["targetType"] != nil || asMap(s["secondaryTargetType"])["workoutTargetTypeKey"] != "pace.zone" || d["status"] != "success" {
		t.Errorf("step = %v", s)
	}
}

func TestUploadWorkoutRejectsNestedSecondaryTargetTypeMismatch(t *testing.T) {
	bad := timedIntervalStep(nil)
	bad["secondaryTargetType"] = map[string]any{"workoutTargetTypeId": 6.0, "workoutTargetTypeKey": "heart.rate"}
	group := map[string]any{"type": "RepeatGroupDTO", "stepOrder": 1.0, "numberOfIterations": 2.0, "workoutSteps": []any{bad}}
	if msg := uploadError(t, runningWorkoutWithSteps(group)); !strings.Contains(msg, "workoutSegments[0].workoutSteps[0].workoutSteps[0].secondaryTargetType mismatch") {
		t.Errorf("msg = %q", msg)
	}
}

func cyclingStep(target map[string]any, extra map[string]any) map[string]any {
	s := map[string]any{
		"type": "ExecutableStepDTO", "stepOrder": 1.0, "stepType": map[string]any{"stepTypeId": 3.0, "stepTypeKey": "interval"},
		"endCondition": map[string]any{"conditionTypeId": 2.0, "conditionTypeKey": "time"}, "endConditionValue": 600.0, "targetType": target,
	}
	for k, v := range extra {
		s[k] = v
	}
	return s
}

func TestUploadCyclingWorkoutPowerBetweenAccepted(t *testing.T) {
	step := cyclingStep(map[string]any{"workoutTargetTypeId": 6.0, "workoutTargetTypeKey": "power.between"}, map[string]any{"targetValueOne": 200.0, "targetValueTwo": 250.0})
	f := uploadFake(t, map[string]any{"workoutId": 200001.0, "workoutName": "Cycling Power Between Test"})
	d := runTool(t, f, "upload_workout", map[string]any{"workout_data": cyclingWorkoutWithSteps("Cycling Power Between Test", step)})
	s := sentStep(uploadedWorkout(t, f), 0)
	tt := asMap(s["targetType"])
	if d["status"] != "success" || d["workout_id"] != 200001.0 || tt["workoutTargetTypeId"] != 6.0 || tt["workoutTargetTypeKey"] != "power.between" || s["targetValueOne"] != 200.0 || s["targetValueTwo"] != 250.0 {
		t.Errorf("d = %v, step = %v", d, s)
	}
}

func TestUploadCyclingWorkoutPowerZoneAccepted(t *testing.T) {
	step := cyclingStep(map[string]any{"workoutTargetTypeId": 2.0, "workoutTargetTypeKey": "power.zone"}, map[string]any{"zoneNumber": 3.0, "endConditionValue": 1200.0})
	f := uploadFake(t, map[string]any{"workoutId": 200002.0})
	d := runTool(t, f, "upload_workout", map[string]any{"workout_data": cyclingWorkoutWithSteps("Cycling Power Zone Test", step)})
	s := sentStep(uploadedWorkout(t, f), 0)
	tt := asMap(s["targetType"])
	if d["status"] != "success" || tt["workoutTargetTypeId"] != 2.0 || tt["workoutTargetTypeKey"] != "power.zone" || s["zoneNumber"] != 3.0 {
		t.Errorf("d = %v, step = %v", d, s)
	}
}

func TestUploadCyclingWorkoutWrongIdForPowerBetweenRejected(t *testing.T) {
	step := cyclingStep(map[string]any{"workoutTargetTypeId": 2.0, "workoutTargetTypeKey": "power.between"}, map[string]any{"targetValueOne": 200.0, "targetValueTwo": 250.0})
	msg := uploadError(t, cyclingWorkoutWithSteps("Bad Power Between", step))
	if !strings.Contains(msg, "targetType mismatch") || !strings.Contains(msg, "workoutTargetTypeId 2 is 'power.zone', not 'power.between'") {
		t.Errorf("msg = %q", msg)
	}
}

func TestUploadCyclingWorkoutWrongIdForPowerZoneRejected(t *testing.T) {
	step := cyclingStep(map[string]any{"workoutTargetTypeId": 6.0, "workoutTargetTypeKey": "power.zone"}, map[string]any{"zoneNumber": 3.0})
	msg := uploadError(t, cyclingWorkoutWithSteps("Bad Power Zone", step))
	if !strings.Contains(msg, "targetType mismatch") || !strings.Contains(msg, "workoutTargetTypeId 6 is one of") || !strings.Contains(msg, "not 'power.zone'") {
		t.Errorf("msg = %q", msg)
	}
}

func TestUploadCyclingWorkoutPowerBetweenInRepeatGroup(t *testing.T) {
	noTarget := map[string]any{"workoutTargetTypeId": 1.0, "workoutTargetTypeKey": "no.target"}
	timeEnd := map[string]any{"conditionTypeId": 2.0, "conditionTypeKey": "time"}
	group := map[string]any{
		"type": "RepeatGroupDTO", "stepOrder": 2.0, "numberOfIterations": 4.0,
		"endCondition": map[string]any{"conditionTypeId": 7.0, "conditionTypeKey": "iterations"}, "endConditionValue": 4.0,
		"workoutSteps": []any{
			cyclingStep(map[string]any{"workoutTargetTypeId": 6.0, "workoutTargetTypeKey": "power.between"}, map[string]any{"endConditionValue": 300.0, "targetValueOne": 250.0, "targetValueTwo": 300.0}),
			map[string]any{"type": "ExecutableStepDTO", "stepOrder": 2.0, "stepType": map[string]any{"stepTypeId": 4.0, "stepTypeKey": "recovery"}, "endCondition": timeEnd, "endConditionValue": 120.0, "targetType": noTarget},
		},
	}
	warm := map[string]any{"type": "ExecutableStepDTO", "stepOrder": 1.0, "stepType": map[string]any{"stepTypeId": 1.0, "stepTypeKey": "warmup"}, "endCondition": timeEnd, "endConditionValue": 600.0, "targetType": noTarget}
	cool := map[string]any{"type": "ExecutableStepDTO", "stepOrder": 3.0, "stepType": map[string]any{"stepTypeId": 2.0, "stepTypeKey": "cooldown"}, "endCondition": timeEnd, "endConditionValue": 600.0, "targetType": noTarget}
	f := uploadFake(t, map[string]any{"workoutId": 200003.0})
	d := runTool(t, f, "upload_workout", map[string]any{"workout_data": cyclingWorkoutWithSteps("Cycling Intervals Power Between", warm, group, cool)})
	s := sentStep(uploadedWorkout(t, f), 1, 0)
	tt := asMap(s["targetType"])
	if d["status"] != "success" || tt["workoutTargetTypeId"] != 6.0 || tt["workoutTargetTypeKey"] != "power.between" || s["targetValueOne"] != 250.0 || s["targetValueTwo"] != 300.0 {
		t.Errorf("d = %v, step = %v", d, s)
	}
}

func TestUploadWorkoutException(t *testing.T) {
	f := workoutsFake(t)
	f.on(wUpload, status(500, map[string]any{"message": "Upload failed"}))
	text, isErr := callTool(t, newSession(t, f, nil), "upload_workout", map[string]any{"workout_data": map[string]any{}})
	if !isErr || text != "Error uploading workout: API Error 500 - Upload failed" {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestUploadWorkoutsSingle(t *testing.T) {
	f := uploadFake(t, map[string]any{"workoutId": 111.0, "workoutName": "Easy Run"})
	d := runTool(t, f, "upload_workouts", map[string]any{"workouts": []any{map[string]any{"workoutName": "Easy Run", "sportType": map[string]any{"sportTypeId": 1.0, "sportTypeKey": "running"}}}})
	r := asMap(toolList(t, "results", d)[0])
	if d["total"] != 1.0 || d["succeeded"] != 1.0 || d["failed"] != 0.0 || r["status"] != "success" || r["workout_id"] != 111.0 || r["name"] != "Easy Run" || len(f.requests(wUpload)) != 1 {
		t.Errorf("d = %v", d)
	}
}

func TestUploadWorkoutsMultiple(t *testing.T) {
	f := uploadFake(t, sequence(
		map[string]any{"workoutId": 111.0, "workoutName": "Easy Run"},
		map[string]any{"workoutId": 222.0, "workoutName": "Tempo Run"},
		map[string]any{"workoutId": 333.0, "workoutName": "Long Run"}))
	d := runTool(t, f, "upload_workouts", map[string]any{"workouts": []any{
		map[string]any{"workoutName": "Easy Run"}, map[string]any{"workoutName": "Tempo Run"}, map[string]any{"workoutName": "Long Run"}}})
	if d["total"] != 3.0 || d["succeeded"] != 3.0 || d["failed"] != 0.0 || len(f.requests(wUpload)) != 3 {
		t.Errorf("d = %v", d)
	}
}

func TestUploadWorkoutsPartialFailure(t *testing.T) {
	f := uploadFake(t, sequence(map[string]any{"workoutId": 111.0, "workoutName": "Easy Run"}, status(400, map[string]any{"message": "API error"})))
	d := runTool(t, f, "upload_workouts", map[string]any{"workouts": []any{map[string]any{"workoutName": "Easy Run"}, map[string]any{"workoutName": "Bad Workout"}}})
	rs := toolList(t, "results", d)
	r1 := asMap(rs[1])
	if d["total"] != 2.0 || d["succeeded"] != 1.0 || d["failed"] != 1.0 || asMap(rs[0])["status"] != "success" || r1["status"] != "error" ||
		!strings.Contains(r1["message"].(string), "API error") || r1["name"] != "Bad Workout" {
		t.Errorf("d = %v", d)
	}
}

func TestUploadWorkoutsReportsEndConditionValidationError(t *testing.T) {
	valid := runningWorkoutWithSteps(recoveryStep(6.0, "heart.rate"))
	valid["workoutName"] = "Valid HR Workout"
	invalid := runningWorkoutWithSteps(recoveryStep(4.0, "heart.rate"))
	invalid["workoutName"] = "Invalid HR Workout"
	f := uploadFake(t, map[string]any{"workoutId": 111.0, "workoutName": "Valid HR Workout"})
	d := runTool(t, f, "upload_workouts", map[string]any{"workouts": []any{valid, invalid}})
	rs := toolList(t, "results", d)
	r1 := asMap(rs[1])
	if d["total"] != 2.0 || d["succeeded"] != 1.0 || d["failed"] != 1.0 || asMap(rs[0])["status"] != "success" || r1["status"] != "error" ||
		r1["name"] != "Invalid HR Workout" || !strings.Contains(r1["message"].(string), "conditionTypeKey 'heart.rate' requires conditionTypeId 6") {
		t.Errorf("d = %v", d)
	}
	if !reflect.DeepEqual(uploadedWorkout(t, f), jsonValue(t, valid)) {
		t.Error("the valid workout was not the one uploaded")
	}
}

func TestUploadWorkoutsRejectsTargetTypeMismatch(t *testing.T) {
	good := runningWorkoutWithSteps(timedIntervalStep(hrZoneTarget()))
	good["workoutName"] = "Good HR Range"
	bad := runningWorkoutWithSteps(timedIntervalStep(map[string]any{"workoutTargetTypeId": 6.0, "workoutTargetTypeKey": "heart.rate"}))
	bad["workoutName"] = "Bad HR Target"
	f := uploadFake(t, map[string]any{"workoutId": 111.0, "workoutName": "Good HR Range"})
	d := runTool(t, f, "upload_workouts", map[string]any{"workouts": []any{good, bad}})
	rs := toolList(t, "results", d)
	r1 := asMap(rs[1])
	if d["total"] != 2.0 || d["succeeded"] != 1.0 || d["failed"] != 1.0 || r1["status"] != "error" || !strings.Contains(r1["message"].(string), "targetType mismatch") {
		t.Errorf("d = %v", d)
	}
	if !reflect.DeepEqual(uploadedWorkout(t, f), jsonValue(t, good)) {
		t.Error("the valid workout was not the one uploaded")
	}
}

// templateReference reads the workout://reference/structure resource.
func templateReference(t *testing.T) map[string]any {
	t.Helper()
	r, err := newSession(t, newFakeGarmin(t), nil).ReadResource(t.Context(), &mcp.ReadResourceParams{URI: "workout://reference/structure"})
	if err != nil {
		t.Fatal(err)
	}
	return resultMap(t, r.Contents[0].Text)
}

var sportPairRe = regexp.MustCompile(`(\d+) = (\w+)`)

func regexpFindPairs(line string) [][2]string {
	var out [][2]string
	for _, m := range sportPairRe.FindAllStringSubmatch(line, -1) {
		out = append(out, [2]string{m[1], m[2]})
	}
	return out
}
