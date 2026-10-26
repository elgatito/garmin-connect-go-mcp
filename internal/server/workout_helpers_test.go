package server

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Ported from tests/integration/test_workouts_tools.py (the helper-level tests) and tests/unit/test_workout_preview.py.

func runningWorkoutWithSteps(steps ...any) map[string]any {
	return map[string]any{
		"workoutName": "Validation Workout",
		"sportType":   map[string]any{"sportTypeId": 1.0, "sportTypeKey": "running"},
		"workoutSegments": []any{map[string]any{
			"segmentOrder": 1.0,
			"sportType":    map[string]any{"sportTypeId": 1.0, "sportTypeKey": "running"},
			"workoutSteps": steps,
		}},
	}
}

func timedIntervalStep(targetType any) map[string]any {
	return map[string]any{
		"type":              "ExecutableStepDTO",
		"stepOrder":         1.0,
		"stepType":          map[string]any{"stepTypeId": 3.0, "stepTypeKey": "interval"},
		"endCondition":      map[string]any{"conditionTypeId": 2.0, "conditionTypeKey": "time"},
		"endConditionValue": 300.0,
		"targetType":        targetType,
		"targetValueOne":    143.0,
		"targetValueTwo":    157.0,
	}
}

func distancePaceStepWithNestedBounds() map[string]any {
	return map[string]any{
		"type":              "ExecutableStepDTO",
		"stepOrder":         1.0,
		"stepType":          map[string]any{"stepTypeId": 3.0, "stepTypeKey": "interval"},
		"endCondition":      map[string]any{"conditionTypeId": 3.0, "conditionTypeKey": "distance"},
		"endConditionValue": 400.0,
		"targetType": map[string]any{
			"workoutTargetTypeId":  6.0,
			"workoutTargetTypeKey": "pace.zone",
			"targetValueOne":       2.0833333,
			"targetValueTwo":       1.9607843,
		},
	}
}

func TestValidateDateRejectsInvalidValues(t *testing.T) {
	for _, v := range []any{"2024-02-30", "2024-13-01", "2024-01-15\n", "20240115", 20240115.0, nil} {
		_, err := validateWorkoutDate(v, "calendar_date")
		if err == nil || !strings.Contains(err.Error(), "expected YYYY-MM-DD") {
			t.Errorf("%v: err = %v", v, err)
		}
	}
	// The message quotes the value the way Python's repr does.
	_, err := validateWorkoutDate("2024-01-15-invalid", "calendar_date")
	if err == nil || err.Error() != "Invalid calendar_date '2024-01-15-invalid': expected YYYY-MM-DD" {
		t.Errorf("err = %v", err)
	}
}

func TestValidateDateAcceptsLeapDay(t *testing.T) {
	if got, err := validateWorkoutDate("2024-02-29", "calendar_date"); err != nil || got != "2024-02-29" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestNestedTargetConflictDoesNotPartiallyMutateStep(t *testing.T) {
	step := distancePaceStepWithNestedBounds()
	step["targetValueOne"] = 2.5
	err := normalizeWorkoutSteps(runningWorkoutWithSteps(step))
	if err == nil || !strings.Contains(err.Error(), "targetValueOne=2.5 conflicts") {
		t.Fatalf("err = %v", err)
	}
	tt := step["targetType"].(map[string]any)
	if step["targetValueOne"] != 2.5 || step["targetValueTwo"] != nil || tt["targetValueOne"] != 2.0833333 || tt["targetValueTwo"] != 1.9607843 {
		t.Errorf("step mutated: %v", step)
	}
}

func TestNestedTargetConflictDoesNotMutateEarlierWorkoutStep(t *testing.T) {
	first, second := distancePaceStepWithNestedBounds(), distancePaceStepWithNestedBounds()
	second["targetValueOne"] = 2.5
	err := normalizeWorkoutSteps(runningWorkoutWithSteps(first, second))
	if err == nil || !strings.Contains(err.Error(), "targetValueOne=2.5 conflicts") {
		t.Fatalf("err = %v", err)
	}
	tt := first["targetType"].(map[string]any)
	if _, has := first["targetValueOne"]; has || tt["targetValueOne"] != 2.0833333 || tt["targetValueTwo"] != 1.9607843 {
		t.Errorf("first step mutated: %v", first)
	}
}

func TestNestedTargetFieldsPromoteSingleBoundAndDeduplicateEqualValue(t *testing.T) {
	step := map[string]any{
		"targetValueOne": 2.0833333,
		"targetType":     map[string]any{"targetValueOne": 2.0833333, "targetValueTwo": 1.9607843},
	}
	if err := normalizeWorkoutSteps(runningWorkoutWithSteps(step)); err != nil {
		t.Fatal(err)
	}
	if step["targetValueOne"] != 2.0833333 || step["targetValueTwo"] != 1.9607843 || len(step["targetType"].(map[string]any)) != 0 {
		t.Errorf("step = %v", step)
	}
}

func TestNestedNullTargetFieldIsRemovedWithoutInjectingStepNull(t *testing.T) {
	step := map[string]any{"targetType": map[string]any{"targetValueOne": nil}}
	if err := normalizeWorkoutSteps(runningWorkoutWithSteps(step)); err != nil {
		t.Fatal(err)
	}
	if want := map[string]any{"targetType": map[string]any{}}; !reflect.DeepEqual(step, want) {
		t.Errorf("step = %v", step)
	}
}

func TestNestedValueReplacesExplicitStepNull(t *testing.T) {
	step := map[string]any{"targetValueOne": nil, "targetType": map[string]any{"targetValueOne": 2.0833333}}
	if err := normalizeWorkoutSteps(runningWorkoutWithSteps(step)); err != nil {
		t.Fatal(err)
	}
	if step["targetValueOne"] != 2.0833333 || len(step["targetType"].(map[string]any)) != 0 {
		t.Errorf("step = %v", step)
	}
}

func TestNestedTargetRepairIgnoresMissingOrNonDictTargetType(t *testing.T) {
	for name, targetType := range map[string]any{"missing": nil, "string": "pace.zone", "list": []any{}} {
		step := map[string]any{}
		if name != "missing" {
			step["targetType"] = targetType
		}
		original := map[string]any{}
		for k, v := range step {
			original[k] = v
		}
		if err := normalizeWorkoutSteps(runningWorkoutWithSteps(step)); err != nil || !reflect.DeepEqual(step, original) {
			t.Errorf("%s: step = %v, err = %v", name, step, err)
		}
	}
}

func TestFixRepeatGroupAddsMissingConditionTypeId(t *testing.T) {
	step := map[string]any{
		"type": "RepeatGroupDTO", "numberOfIterations": 5.0,
		"endCondition":      map[string]any{"conditionTypeKey": "iterations"},
		"endConditionValue": 5.0, "workoutSteps": []any{},
	}
	if err := fixRepeatGroupStep(step); err != nil {
		t.Fatal(err)
	}
	ec := step["endCondition"].(map[string]any)
	if ec["conditionTypeId"] != 7.0 || ec["conditionTypeKey"] != "iterations" {
		t.Errorf("endCondition = %v", ec)
	}
}

func TestFixRepeatGroupLeavesExistingConditionTypeIdUnchanged(t *testing.T) {
	step := map[string]any{
		"type": "RepeatGroupDTO", "numberOfIterations": 3.0,
		"endCondition":      map[string]any{"conditionTypeId": 7.0, "conditionTypeKey": "iterations"},
		"endConditionValue": 3.0, "workoutSteps": []any{},
	}
	if err := fixRepeatGroupStep(step); err != nil || step["endCondition"].(map[string]any)["conditionTypeId"] != 7.0 {
		t.Errorf("step = %v, err = %v", step, err)
	}
}

func TestFixRepeatGroupBackfillsNumberOfIterationsFromEndConditionValue(t *testing.T) {
	step := map[string]any{
		"type":              "RepeatGroupDTO",
		"endCondition":      map[string]any{"conditionTypeId": 7.0, "conditionTypeKey": "iterations"},
		"endConditionValue": 4.0, "workoutSteps": []any{},
	}
	if err := fixRepeatGroupStep(step); err != nil || step["numberOfIterations"] != 4.0 {
		t.Errorf("step = %v, err = %v", step, err)
	}
	// A value that is not a number reports Python's int() error.
	bad := map[string]any{"type": "RepeatGroupDTO", "endConditionValue": "many"}
	if err := fixRepeatGroupStep(bad); err == nil || err.Error() != "invalid literal for int() with base 10: 'many'" {
		t.Errorf("err = %v", err)
	}
}

func TestFixRepeatGroupDoesNotModifyNonRepeatSteps(t *testing.T) {
	step := map[string]any{
		"type": "ExecutableStepDTO", "endCondition": map[string]any{"conditionTypeKey": "time"}, "endConditionValue": 300.0,
	}
	if err := fixRepeatGroupStep(step); err != nil {
		t.Fatal(err)
	}
	if _, has := step["endCondition"].(map[string]any)["conditionTypeId"]; has {
		t.Errorf("step = %v", step)
	}
}

func TestFixRepeatGroupRecursesIntoNestedRepeatGroups(t *testing.T) {
	inner := map[string]any{
		"type": "RepeatGroupDTO", "numberOfIterations": 2.0,
		"endCondition": map[string]any{"conditionTypeKey": "iterations"}, "endConditionValue": 2.0, "workoutSteps": []any{},
	}
	outer := map[string]any{
		"type": "RepeatGroupDTO", "numberOfIterations": 3.0,
		"endCondition":      map[string]any{"conditionTypeId": 7.0, "conditionTypeKey": "iterations"},
		"endConditionValue": 3.0, "workoutSteps": []any{inner},
	}
	if err := fixRepeatGroupStep(outer); err != nil || inner["endCondition"].(map[string]any)["conditionTypeId"] != 7.0 {
		t.Errorf("inner = %v, err = %v", inner, err)
	}
}

// ---- test_workout_preview.py ----

type fakeWorkoutClient struct {
	workout  any
	workouts []any
	err      error
}

func (f *fakeWorkoutClient) GetWorkoutByID(context.Context, int64) (any, error) {
	return f.workout, f.err
}

// GetWorkouts is like Garmin: start is 1-based and 0 is treated as 1.
func (f *fakeWorkoutClient) GetWorkouts(_ context.Context, start, limit int) (any, error) {
	offset := max(start, 1) - 1
	if offset > len(f.workouts) {
		offset = len(f.workouts)
	}
	return f.workouts[offset:min(offset+limit, len(f.workouts))], nil
}

func TestPreviewFromWorkoutDetails(t *testing.T) {
	client := &fakeWorkoutClient{workout: map[string]any{
		"workoutId": 99.0, "workoutName": "Threshold Intervals", "sportType": map[string]any{"sportTypeKey": "running"},
		"workoutProvider": "garmin", "updatedDate": "2024-01-15",
	}}
	p := workoutPreview(context.Background(), client, 99)
	if p["workout_id"] != int64(99) || p["name"] != "Threshold Intervals" || p["sport"] != "running" {
		t.Errorf("preview = %v", p)
	}
}

func TestPreviewFallsBackToWorkoutListOnDetailsError(t *testing.T) {
	client := &fakeWorkoutClient{err: errors.New("403 Forbidden"), workouts: []any{
		map[string]any{"workoutId": 99.0, "workoutName": "Sweet Spot", "sportType": map[string]any{"sportTypeKey": "cycling"}},
	}}
	p := workoutPreview(context.Background(), client, 99)
	if p["name"] != "Sweet Spot" || p["sport"] != "cycling" {
		t.Errorf("preview = %v", p)
	}
}

func TestPreviewFallbackSearchesPastFirstPage(t *testing.T) {
	var ws []any
	for i := 1; i <= 150; i++ {
		ws = append(ws, map[string]any{"workoutId": float64(i), "workoutName": fmt.Sprintf("Workout %d", i)})
	}
	client := &fakeWorkoutClient{err: errors.New("403 Forbidden"), workouts: ws}
	if p := workoutPreview(context.Background(), client, 120); p["name"] != "Workout 120" {
		t.Errorf("preview = %v", p)
	}
}

func TestGetAllWorkoutsPagesALargeLibraryWithoutGapsOrDuplicates(t *testing.T) {
	var ws []any
	for i := 0; i < 250; i++ {
		ws = append(ws, map[string]any{"workoutId": float64(i)})
	}
	got, err := getAllWorkouts(context.Background(), &fakeWorkoutClient{workouts: ws})
	if err != nil || len(got) != 250 {
		t.Fatalf("len = %d, err = %v", len(got), err)
	}
	for i, w := range got {
		if w["workoutId"] != float64(i) {
			t.Fatalf("workout %d = %v", i, w["workoutId"])
		}
	}
}
