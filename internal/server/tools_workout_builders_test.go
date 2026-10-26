package server

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// Ported from tests/integration/test_workout_builders_tools.py.

func TestBuildersRejectInvalidCountsAndDurationsBeforeUpload(t *testing.T) {
	defaults := map[string]map[string]any{
		"create_run_workout":          {"run_seconds": 300, "warmup_min": 5, "cooldown_min": 5},
		"create_run_interval_workout": {"repeats": 3, "rep_seconds": 60, "recovery_seconds": 60, "warmup_min": 5, "cooldown_min": 5},
		"create_walk_run_workout":     {"run_seconds": 60, "walk_seconds": 60, "repeats": 3, "warmup_min": 5, "cooldown_min": 5},
		"create_z2_walk_workout":      {"duration_min": 30, "hr_min": 110, "hr_max": 130},
		"create_strength_workout":     {"exercises": []any{map[string]any{"name": "Squat", "reps": 5}}},
	}
	prefix := map[string]string{
		"create_run_workout": "Error creating run workout", "create_run_interval_workout": "Error creating run interval workout",
		"create_walk_run_workout": "Error creating walk/run workout", "create_z2_walk_workout": "Error creating Z2 walk workout",
		"create_strength_workout": "Error creating strength workout",
	}
	ex1 := func(k string, v int) any { return []any{map[string]any{"name": "Squat", k: v}} }
	for _, tc := range []struct {
		tool  string
		args  map[string]any
		field string
	}{
		{"create_run_workout", map[string]any{"run_seconds": 0}, "run_seconds"},
		{"create_run_workout", map[string]any{"run_seconds": -1}, "run_seconds"},
		{"create_run_workout", map[string]any{"warmup_min": -1}, "warmup_min"},
		{"create_run_workout", map[string]any{"cooldown_min": -1}, "cooldown_min"},
		{"create_run_interval_workout", map[string]any{"warmup_min": -1}, "warmup_min"},
		{"create_run_interval_workout", map[string]any{"cooldown_min": -1}, "cooldown_min"},
		{"create_walk_run_workout", map[string]any{"run_seconds": 0}, "run_seconds"},
		{"create_walk_run_workout", map[string]any{"walk_seconds": 0}, "walk_seconds"},
		{"create_walk_run_workout", map[string]any{"repeats": 0}, "repeats"},
		{"create_walk_run_workout", map[string]any{"warmup_min": -1}, "warmup_min"},
		{"create_walk_run_workout", map[string]any{"cooldown_min": -1}, "cooldown_min"},
		{"create_z2_walk_workout", map[string]any{"duration_min": 0}, "duration_min"},
		{"create_z2_walk_workout", map[string]any{"duration_min": -1}, "duration_min"},
		{"create_strength_workout", map[string]any{"exercises": ex1("sets", 0)}, "sets"},
		{"create_strength_workout", map[string]any{"exercises": ex1("sets", -1)}, "sets"},
		{"create_strength_workout", map[string]any{"exercises": ex1("reps", 0)}, "reps"},
		{"create_strength_workout", map[string]any{"exercises": ex1("reps", -1)}, "reps"},
		{"create_strength_workout", map[string]any{"exercises": ex1("rest_seconds", -1)}, "rest_seconds"},
	} {
		f := uploadFake(t, map[string]any{"workoutId": 123.0})
		args := map[string]any{"name": "Invalid workout"}
		for k, v := range defaults[tc.tool] {
			args[k] = v
		}
		for k, v := range tc.args {
			args[k] = v
		}
		text, isErr := callTool(t, newSession(t, f, nil), tc.tool, args)
		if !isErr || !strings.HasPrefix(text, prefix[tc.tool]) || !strings.Contains(text, tc.field) || len(f.requests(wUpload)) != 0 {
			t.Errorf("%s %v: isErr=%v text=%q uploads=%d", tc.tool, tc.args, isErr, text, len(f.requests(wUpload)))
		}
	}
}

func TestZeroRecoveryRejectsUnusedHrBounds(t *testing.T) {
	for _, bounds := range []map[string]any{
		{"recovery_hr_min": 120}, {"recovery_hr_max": 140},
		{"recovery_hr_min": 140, "recovery_hr_max": 120}, {"recovery_hr_min": 120, "recovery_hr_max": 140},
	} {
		f := uploadFake(t, map[string]any{"workoutId": 123.0})
		args := map[string]any{"name": "Strides", "repeats": 6, "rep_seconds": 20, "recovery_seconds": 0, "warmup_min": 10, "cooldown_min": 5}
		for k, v := range bounds {
			args[k] = v
		}
		text, isErr := callTool(t, newSession(t, f, nil), "create_run_interval_workout", args)
		if !isErr || !strings.HasPrefix(text, "Error creating run interval workout") || !strings.Contains(text, "recovery_hr_") || len(f.requests(wUpload)) != 0 {
			t.Errorf("%v: isErr=%v text=%q", bounds, isErr, text)
		}
	}
}

func weekFake(t *testing.T) *fakeGarmin { return workoutsFake(t) }

func scheduled(t *testing.T, f *fakeGarmin, week ...any) []any {
	t.Helper()
	d := runTool(t, f, "schedule_week", map[string]any{"week": week})
	if d["status"] != "complete" {
		t.Fatalf("d = %v", d)
	}
	return toolList(t, "scheduled", d)
}

func wk(date, id any) map[string]any { return map[string]any{"date": date, "workout_id": id} }

func TestScheduleWeekInvalidIdNamesFieldAndContinues(t *testing.T) {
	for _, id := range []any{nil, "abc", 0, -1} {
		f := weekFake(t)
		f.on("POST "+wSchedule+"123", map[string]any{})
		entries := scheduled(t, f, wk("2026-10-20", id), wk("2026-10-21", 123))
		e0, e1 := asMap(entries[0]), asMap(entries[1])
		if e0["status"] != "failed" || !strings.Contains(e0["message"].(string), "workout_id") || e1["status"] != "scheduled" ||
			len(f.requests("POST "+wSchedule+"123")) != 1 {
			t.Errorf("id %v: entries = %v", id, entries)
		}
		wantID := id
		if n, ok := id.(int); ok {
			wantID = float64(n)
		}
		if e0["workout_id"] != wantID || e0["date"] != "2026-10-20" {
			t.Errorf("id %v: e0 = %v", id, e0)
		}
	}
}

func TestScheduleWeekAcceptsSuccessStatuses(t *testing.T) {
	for _, code := range []int{201, 202, 204} {
		f := weekFake(t)
		f.on("POST "+wSchedule+"123", status(code, rawBody("")))
		entry := asMap(scheduled(t, f, wk("2026-10-20", 123))[0])
		if entry["status"] != "scheduled" {
			t.Errorf("%d: entry = %v", code, entry)
		}
	}
}

func TestScheduleWeekKeepsReturnedCalendarEntryId(t *testing.T) {
	f := weekFake(t)
	f.on("POST "+wSchedule+"123", map[string]any{"workoutScheduleId": 456, "workoutId": 123})
	entry := asMap(scheduled(t, f, wk("2026-10-20", 123))[0])
	if entry["status"] != "scheduled" || entry["scheduled_workout_id"] != 456.0 {
		t.Errorf("entry = %v", entry)
	}
}

func TestScheduleWeekEmptySuccessBodyStillSucceeds(t *testing.T) {
	f := weekFake(t)
	f.on("POST "+wSchedule+"123", status(200, rawBody("not json")))
	entry := asMap(scheduled(t, f, wk("2026-10-20", 123))[0])
	if entry["status"] != "scheduled" {
		t.Errorf("entry = %v", entry)
	}
	if _, has := entry["scheduled_workout_id"]; has {
		t.Errorf("entry = %v", entry)
	}
}

func TestScheduleWeekUsesClientPostNotGarth(t *testing.T) {
	f := weekFake(t)
	f.on("POST "+wSchedule+"1234567890", map[string]any{})
	d := runTool(t, f, "schedule_week", map[string]any{"week": []any{wk("2026-05-12", 1234567890)}})
	e := asMap(toolList(t, "scheduled", d)[0])
	if d["status"] != "complete" || e["status"] != "scheduled" || e["workout_id"] != 1234567890.0 || len(f.requests("POST "+wSchedule+"1234567890")) != 1 {
		t.Errorf("d = %v", d)
	}
}

func graphQLReturning(byDate map[string][]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		list := []any{}
		for date, entries := range byDate {
			if strings.Contains(string(body[:n]), date) {
				list = entries
			}
		}
		writeBody(w, r, 200, map[string]any{"data": map[string]any{"workoutScheduleSummariesScalar": list}})
	}
}

func TestScheduleWeekIsIdempotent(t *testing.T) {
	f := weekFake(t)
	f.on(wGraphQL, map[string]any{"data": map[string]any{"workoutScheduleSummariesScalar": []any{
		map[string]any{"workoutId": 1234567890, "scheduleDate": "2026-05-12", "workoutName": "Easy Run"},
	}}})
	d := runTool(t, f, "schedule_week", map[string]any{"week": []any{wk("2026-05-12", 1234567890)}})
	e := asMap(toolList(t, "scheduled", d)[0])
	if d["status"] != "complete" || e["status"] != "already_scheduled" || e["idempotent"] != true || len(f.requests("POST "+wSchedule+"1234567890")) != 0 {
		t.Errorf("d = %v", d)
	}
}

func TestScheduleWeekPartialIdempotency(t *testing.T) {
	f := weekFake(t)
	f.on(wGraphQL, graphQLReturning(map[string][]any{"2026-05-12": {
		map[string]any{"workoutId": 111, "scheduleDate": "2026-05-12", "workoutName": "Easy Run"},
	}}))
	f.on("POST "+wSchedule+"111", map[string]any{})
	f.on("POST "+wSchedule+"222", map[string]any{})
	entries := scheduled(t, f, wk("2026-05-12", 111), wk("2026-05-14", 222))
	if asMap(entries[0])["status"] != "already_scheduled" || asMap(entries[1])["status"] != "scheduled" ||
		len(f.requests("POST "+wSchedule+"111")) != 0 || len(f.requests("POST "+wSchedule+"222")) != 1 {
		t.Errorf("entries = %v", entries)
	}
}

func TestScheduleWeekKeepsResultsWhenAnItemFails(t *testing.T) {
	f := weekFake(t)
	f.on("POST "+wSchedule+"111", map[string]any{})
	f.on("POST "+wSchedule+"222", status(404, map[string]any{"message": "workout not found"}))
	f.on("POST "+wSchedule+"333", map[string]any{})
	entries := scheduled(t, f, wk("2026-05-12", 111), wk("2026-05-13", 222), wk("2026-05-14", 333))
	got := []any{}
	for _, e := range entries {
		got = append(got, asMap(e)["status"])
	}
	e1 := asMap(entries[1])
	if !reflect.DeepEqual(got, []any{"scheduled", "error", "scheduled"}) || e1["workout_id"] != 222.0 ||
		!strings.Contains(e1["message"].(string), "API Error 404") || !strings.HasPrefix(e1["message"].(string), "Error scheduling workout: ") ||
		len(f.requests("")) < 3 {
		t.Errorf("entries = %v", entries)
	}
}

func TestScheduleWeekRejectsInvalidDate(t *testing.T) {
	for _, bad := range []string{"10/14/2026", "2026-02-30"} {
		f := weekFake(t)
		f.on("POST "+wSchedule+"111", map[string]any{})
		e := asMap(scheduled(t, f, wk(bad, 111))[0])
		if e["status"] != "failed" || !strings.Contains(e["message"].(string), "YYYY-MM-DD") || e["date"] != bad || len(f.requests("POST "+wSchedule+"111")) != 0 {
			t.Errorf("%s: e = %v", bad, e)
		}
	}
}

func TestScheduleWeekMissingDateDoesNotAbortBatch(t *testing.T) {
	f := weekFake(t)
	f.on("POST "+wSchedule+"222", map[string]any{})
	entries := scheduled(t, f, map[string]any{"workout_id": 111}, wk("2026-05-14", 222))
	reqs := f.requests("POST " + wSchedule + "222")
	if asMap(entries[0])["status"] != "failed" || asMap(entries[1])["status"] != "scheduled" || len(reqs) != 1 ||
		!reflect.DeepEqual(asMap(decode(t, string(reqs[0].Body))), map[string]any{"date": "2026-05-14"}) || len(f.requests("POST "+wSchedule+"111")) != 0 {
		t.Errorf("entries = %v", entries)
	}
}

func TestCreateRunWorkoutSuccess(t *testing.T) {
	f := uploadFake(t, map[string]any{"workoutId": 9876543210, "workoutName": "Step 8 - 30min continuous"})
	d := runTool(t, f, "create_run_workout", map[string]any{"name": "Step 8 - 30min continuous", "run_seconds": 1800, "warmup_min": 5, "cooldown_min": 5, "hr_zone": "Z3"})
	want := map[string]any{"status": "success", "workout_id": 9876543210.0, "name": "Step 8 - 30min continuous", "message": "Workout uploaded successfully"}
	if !reflect.DeepEqual(d, want) || len(f.requests(wUpload)) != 1 {
		t.Errorf("d = %v", d)
	}
	if step := sentStep(uploadedWorkout(t, f), 1); step["zoneNumber"] != 3.0 || step["endConditionValue"] != 1800.0 {
		t.Errorf("step = %v", step)
	}
}

func TestCreateRunWorkoutCustomHrRange(t *testing.T) {
	f := uploadFake(t, map[string]any{"workoutId": 1111111111, "workoutName": "Base run - 7/20"})
	d := runTool(t, f, "create_run_workout", map[string]any{"name": "Base run - 7/20", "run_seconds": 1440, "warmup_min": 5, "cooldown_min": 5, "hr_min": 136, "hr_max": 148})
	step := sentStep(uploadedWorkout(t, f), 1)
	_, hasZone := step["zoneNumber"]
	if d["status"] != "success" || asMap(step["targetType"])["workoutTargetTypeKey"] != "heart.rate.zone" || step["targetValueOne"] != 136.0 || step["targetValueTwo"] != 148.0 || hasZone {
		t.Errorf("d = %v, step = %v", d, step)
	}
}

func TestCreateRunWorkoutException(t *testing.T) {
	f := workoutsFake(t)
	f.on(wUpload, status(500, map[string]any{"message": "Upload failed"}))
	text, isErr := callTool(t, newSession(t, f, nil), "create_run_workout", map[string]any{"name": "Step 8 - 30min continuous", "run_seconds": 1800, "warmup_min": 5, "cooldown_min": 5})
	if !isErr || text != "Error creating run workout: API Error 500 - Upload failed" {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestCreateRunIntervalWorkoutSuccess(t *testing.T) {
	f := uploadFake(t, map[string]any{"workoutId": 2222222222, "workoutName": "Threshold 3x6min"})
	d := runTool(t, f, "create_run_interval_workout", map[string]any{
		"name": "Threshold 3x6min", "repeats": 3, "rep_seconds": 360, "recovery_seconds": 180, "warmup_min": 12, "cooldown_min": 8, "hr_min": 172, "hr_max": 180,
	})
	steps := stepsOf(uploadedWorkout(t, f))
	if d["status"] != "success" || d["workout_id"] != 2222222222.0 || d["message"] != "Interval workout uploaded successfully" ||
		keyOf(steps[0], "stepType", "stepTypeKey") != "warmup" || asMap(steps[1])["type"] != "RepeatGroupDTO" || asMap(steps[1])["numberOfIterations"] != 3.0 ||
		keyOf(steps[2], "stepType", "stepTypeKey") != "cooldown" {
		t.Errorf("d = %v, steps = %v", d, steps)
	}
}

func TestCreateRunIntervalWorkoutException(t *testing.T) {
	f := workoutsFake(t)
	f.on(wUpload, status(500, map[string]any{"message": "Upload failed"}))
	text, isErr := callTool(t, newSession(t, f, nil), "create_run_interval_workout", map[string]any{
		"name": "Threshold 3x6min", "repeats": 3, "rep_seconds": 360, "recovery_seconds": 180, "warmup_min": 12, "cooldown_min": 8,
	})
	if !isErr || text != "Error creating run interval workout: API Error 500 - Upload failed" {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestCreateRunIntervalWorkoutRejectsSingleRecoveryBound(t *testing.T) {
	f := uploadFake(t, map[string]any{"workoutId": 1})
	text, isErr := callTool(t, newSession(t, f, nil), "create_run_interval_workout", map[string]any{
		"name": "Threshold 3x6min", "repeats": 3, "rep_seconds": 360, "recovery_seconds": 180, "warmup_min": 12, "cooldown_min": 8,
		"hr_min": 172, "hr_max": 180, "recovery_hr_min": 120,
	})
	if !isErr || !strings.Contains(text, "Error creating run interval workout") ||
		!strings.Contains(text, "recovery_hr_min and recovery_hr_max must both be provided together") || len(f.requests(wUpload)) != 0 {
		t.Errorf("%v %q", isErr, text)
	}
}

// Extra: the tools without a Python integration test (walk/run, Z2 walk, strength) upload what the builders build,
// and the schema defaults (hr_zone Z3 / Z4) apply when the argument is omitted.
func TestBuilderToolsUploadBuiltWorkout(t *testing.T) {
	for _, tc := range []struct {
		tool, msg string
		args      map[string]any
		build     func() (map[string]any, error)
	}{
		{"create_walk_run_workout", "Workout uploaded successfully",
			map[string]any{"name": "W", "run_seconds": 60, "walk_seconds": 180, "repeats": 7, "warmup_min": 10, "cooldown_min": 8},
			func() (map[string]any, error) { return buildWalkRunJSON("W", 60, 180, 7, 10, 8, "Z3") }},
		{"create_z2_walk_workout", "Workout uploaded successfully",
			map[string]any{"name": "Z", "duration_min": 30, "hr_min": 110, "hr_max": 130},
			func() (map[string]any, error) { return buildZ2WalkJSON("Z", 30, 110, 130) }},
		{"create_strength_workout", "Workout uploaded successfully",
			map[string]any{"name": "S", "exercises": []any{map[string]any{"name": "Squat", "sets": 3, "reps": 5}, map[string]any{"name": "Plank"}}},
			func() (map[string]any, error) {
				return buildStrengthJSON("S", []map[string]any{{"name": "Squat", "sets": 3.0, "reps": 5.0}, {"name": "Plank"}})
			}},
		{"create_run_interval_workout", "Interval workout uploaded successfully",
			map[string]any{"name": "I", "repeats": 3, "rep_seconds": 60, "recovery_seconds": 30, "warmup_min": 5, "cooldown_min": 5},
			func() (map[string]any, error) {
				return buildRunIntervalJSON("I", 3, 60, 30, 5, 5, "Z4", nil, nil, nil, nil)
			}},
	} {
		f := uploadFake(t, map[string]any{"workoutId": 7, "workoutName": "n"})
		d := runTool(t, f, tc.tool, tc.args)
		w, err := tc.build()
		if got, want := uploadedWorkout(t, f), rt(t, w, err); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: sent %v, want %v", tc.tool, got, want)
		}
		if d["message"] != tc.msg || d["workout_id"] != 7.0 || d["name"] != "n" {
			t.Errorf("%s: d = %v", tc.tool, d)
		}
	}
}

func TestBuilderToolsReturnNonObjectUploadResultAsIs(t *testing.T) {
	f := uploadFake(t, []any{1, 2})
	text := callOK(t, newSession(t, f, nil), "create_z2_walk_workout", map[string]any{"name": "Z", "duration_min": 30, "hr_min": 110, "hr_max": 130})
	if !reflect.DeepEqual(decode(t, text), []any{1.0, 2.0}) {
		t.Errorf("text = %s", text)
	}
}
