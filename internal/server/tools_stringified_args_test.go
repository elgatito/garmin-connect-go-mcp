package server

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Object and array arguments may arrive as JSON strings (FastMCP's pre_parse_json); the registry parses them.

func stringify(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestStringifiedWorkoutDataUploadsLikeTheObject(t *testing.T) {
	data := runningWorkoutWithSteps(timedIntervalStep(hrZoneTarget()))
	var bodies []map[string]any
	for _, arg := range []any{data, stringify(t, data)} {
		f := uploadFake(t, map[string]any{"workoutId": 1.0, "workoutName": "x"})
		d := runTool(t, f, "upload_workout", map[string]any{"workout_data": arg})
		if d["status"] != "success" {
			t.Fatalf("d = %v", d)
		}
		bodies = append(bodies, uploadedWorkout(t, f))
	}
	if !reflect.DeepEqual(bodies[0], bodies[1]) {
		t.Errorf("object body %v != string body %v", bodies[0], bodies[1])
	}
}

func TestStringifiedWorkoutsListUploads(t *testing.T) {
	f := uploadFake(t, map[string]any{"workoutId": 1.0, "workoutName": "x"})
	list := []any{map[string]any{"workoutName": "A"}, map[string]any{"workoutName": "B"}}
	d := runTool(t, f, "upload_workouts", map[string]any{"workouts": stringify(t, list)})
	if d["total"] != 2.0 || d["succeeded"] != 2.0 || len(f.requests(wUpload)) != 2 {
		t.Errorf("d = %v", d)
	}
}

func TestStringifiedExerciseSets(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(actPut+"/exerciseSets", rawBody(""))
	sets := []any{map[string]any{"category": "PULL_UP", "exercise_name": "PULL_UP", "reps": 10}}
	d := runTool(t, f, "set_activity_exercise_sets", map[string]any{"activity_id": 12345678901, "sets": stringify(t, sets)})
	if d["success"] != true || d["sets_written"] != 1.0 {
		t.Errorf("d = %v", d)
	}
}

func TestStringifiedArgumentsThatDoNotFitStillFailValidation(t *testing.T) {
	for name, arg := range map[string]any{
		"invalid JSON":  `{"workoutName": `,
		"wrong kind":    `[1, 2]`,
		"plain string":  `hello`,
		"JSON string":   `"x"`,
		"JSON number":   `5`,
		"empty string":  ``,
		"object-ish nu": `null`,
	} {
		f := uploadFake(t, map[string]any{"workoutId": 1.0})
		text, isErr := callTool(t, newSession(t, f, nil), "upload_workout", map[string]any{"workout_data": arg})
		if !isErr || text == "" || len(f.requests(wUpload)) != 0 {
			t.Errorf("%s: isErr=%v text=%q uploads=%d", name, isErr, text, len(f.requests(wUpload)))
		}
	}
	f := uploadFake(t, map[string]any{"workoutId": 1.0})
	text, isErr := callTool(t, newSession(t, f, nil), "upload_workouts", map[string]any{"workouts": `{"workoutName": "A"}`})
	if !isErr || text == "" || len(f.requests(wUpload)) != 0 {
		t.Errorf("object for list: isErr=%v text=%q", isErr, text)
	}
}

func TestPlainObjectArgumentStillWorks(t *testing.T) {
	f := uploadFake(t, map[string]any{"workoutId": 1.0, "workoutName": "x"})
	if d := runTool(t, f, "upload_workout", map[string]any{"workout_data": map[string]any{"workoutName": "x"}}); d["status"] != "success" {
		t.Errorf("d = %v", d)
	}
}
