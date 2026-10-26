package server

import (
	"io"

	"bytes"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"reflect"
	"strings"
	"testing"
)

// Ported from tests/integration/test_workouts_tools.py (delete, schedule and unschedule tools).

const (
	wSchedule = "/workout-service/schedule/"
	wDelete   = "DELETE /workout-service/workout/"
)

func easyRun(id float64) map[string]any {
	return map[string]any{"workoutId": id, "workoutName": "Easy Run", "sportType": map[string]any{"sportTypeKey": "running"}}
}

func TestDeleteWorkoutRequiresConfirmation(t *testing.T) {
	f := workoutsFake(t)
	f.on(wRoute(123456), easyRun(123456))
	d := runTool(t, f, "delete_workout", map[string]any{"workout_id": 123456})
	if d["status"] != "needs_confirmation" || asMap(d["workout"])["name"] != "Easy Run" || len(f.requests(wDelete+"123456")) != 0 {
		t.Errorf("d = %v", d)
	}
	if !strings.Contains(d["message"].(string), "Call again with confirm=true") {
		t.Errorf("message = %v", d["message"])
	}
}

func TestDeleteWorkoutSuccess(t *testing.T) {
	f := workoutsFake(t)
	f.on(wDelete+"123456", map[string]any{})
	f.on(wRoute(123456), easyRun(123456))
	var log bytes.Buffer
	d := resultMap(t, callOK(t, newSessionLog(t, f, &log), "delete_workout", map[string]any{"workout_id": 123456, "confirm": true}))
	if d["status"] != "success" || d["workout_id"] != 123456.0 || !strings.Contains(d["message"].(string), "deleted successfully") || len(f.requests(wDelete+"123456")) != 1 {
		t.Errorf("d = %v", d)
	}
	if !strings.Contains(log.String(), "delete_workout: deleted workout 123456 name='Easy Run' at ") {
		t.Errorf("log = %q", log.String())
	}
}

func TestDeleteWorkoutFailure(t *testing.T) {
	f := workoutsFake(t)
	f.on(wDelete+"999999", status(404, map[string]any{"message": "gone"}))
	f.on(wRoute(999999), map[string]any{"workoutId": 999999.0})
	d := runTool(t, f, "delete_workout", map[string]any{"workout_id": 999999, "confirm": true})
	if d["status"] != "failed" || d["workout_id"] != 999999.0 || !strings.Contains(d["message"].(string), "404") {
		t.Errorf("d = %v", d)
	}
}

func TestDeleteWorkoutException(t *testing.T) {
	f := workoutsFake(t)
	f.on(wDelete+"123456", status(503, map[string]any{"message": "Network error"}))
	f.on(wRoute(123456), map[string]any{"workoutId": 123456.0})
	d := runTool(t, f, "delete_workout", map[string]any{"workout_id": 123456, "confirm": true})
	if d["status"] != "failed" || !strings.Contains(d["message"].(string), "Network error") {
		t.Errorf("d = %v", d)
	}
}

func TestDeleteWorkoutHandlesNullSportType(t *testing.T) {
	f := workoutsFake(t)
	f.on(wDelete+"123456", map[string]any{})
	f.on(wRoute(123456), map[string]any{"workoutId": 123456.0, "workoutName": "Imported", "sportType": nil})
	d := runTool(t, f, "delete_workout", map[string]any{"workout_id": 123456, "confirm": true})
	if d["status"] != "success" || asMap(d["workout"])["name"] != "Imported" || len(f.requests(wDelete+"123456")) != 1 {
		t.Errorf("d = %v", d)
	}
}

func TestDeleteWorkoutsRequiresConfirmation(t *testing.T) {
	f := workoutsFake(t)
	f.on(wRoute(123456), easyRun(123456))
	d := runTool(t, f, "delete_workouts", map[string]any{"workout_ids": []any{123456}})
	if d["status"] != "needs_confirmation" || d["total"] != 1.0 || asMap(toolList(t, "workouts", d)[0])["name"] != "Easy Run" || len(f.requests(wDelete+"123456")) != 0 {
		t.Errorf("d = %v", d)
	}
}

func TestDeleteWorkoutsSingle(t *testing.T) {
	f := workoutsFake(t)
	f.on(wDelete+"123456", map[string]any{})
	f.on(wRoute(123456), map[string]any{"workoutId": 123456.0})
	d := runTool(t, f, "delete_workouts", map[string]any{"workout_ids": []any{123456}, "confirm": true})
	r := asMap(toolList(t, "results", d)[0])
	if d["total"] != 1.0 || d["succeeded"] != 1.0 || d["failed"] != 0.0 || r["status"] != "success" || r["workout_id"] != 123456.0 {
		t.Errorf("d = %v", d)
	}
}

func TestDeleteWorkoutsMultiple(t *testing.T) {
	f := workoutsFake(t)
	for _, id := range []string{"111", "222", "333"} {
		f.on(wDelete+id, map[string]any{})
		f.on("GET /workout-service/workout/"+id, map[string]any{"workoutId": 1.0})
	}
	d := runTool(t, f, "delete_workouts", map[string]any{"workout_ids": []any{111, 222, 333}, "confirm": true})
	if d["total"] != 3.0 || d["succeeded"] != 3.0 || d["failed"] != 0.0 || len(f.requests("")) < 3 {
		t.Errorf("d = %v", d)
	}
	n := 0
	for _, c := range f.requests("") {
		if c.Method == "DELETE" {
			n++
		}
	}
	if n != 3 {
		t.Errorf("%d deletes", n)
	}
}

func TestDeleteWorkoutsPartialFailure(t *testing.T) {
	f := workoutsFake(t)
	f.on(wDelete+"111", map[string]any{})
	f.on(wDelete+"999", status(404, map[string]any{"message": "API Error 404"}))
	f.on(wRoute(111), map[string]any{"workoutId": 111.0})
	f.on(wRoute(999), map[string]any{"workoutId": 999.0})
	d := runTool(t, f, "delete_workouts", map[string]any{"workout_ids": []any{111, 999}, "confirm": true})
	rs := toolList(t, "results", d)
	if d["total"] != 2.0 || d["succeeded"] != 1.0 || d["failed"] != 1.0 || asMap(rs[0])["status"] != "success" ||
		asMap(rs[1])["status"] != "error" || !strings.Contains(asMap(rs[1])["message"].(string), "404") {
		t.Errorf("d = %v", d)
	}
	if _, has := asMap(rs[1])["workout"]; has {
		t.Error("an error entry carries no preview")
	}
}

func TestDeleteWorkoutsException(t *testing.T) {
	f := workoutsFake(t)
	f.on(wDelete+"123456", status(503, map[string]any{"message": "Network error"}))
	f.on(wRoute(123456), map[string]any{"workoutId": 123456.0})
	d := runTool(t, f, "delete_workouts", map[string]any{"workout_ids": []any{123456}, "confirm": true})
	r := asMap(toolList(t, "results", d)[0])
	if d["total"] != 1.0 || d["succeeded"] != 0.0 || d["failed"] != 1.0 || r["status"] != "error" || !strings.Contains(r["message"].(string), "Network error") {
		t.Errorf("d = %v", d)
	}
}

// ---- schedule ----

func scheduleFake(t *testing.T, ids ...string) *fakeGarmin {
	f := workoutsFake(t)
	for _, id := range ids {
		f.on("POST "+wSchedule+id, map[string]any{})
	}
	return f
}

func scheduleBodies(t *testing.T, f *fakeGarmin, id string) []any {
	var out []any
	for _, c := range f.requests("POST " + wSchedule + id) {
		out = append(out, decode(t, string(c.Body)))
	}
	return out
}

func TestScheduleWorkoutAcceptsAllSuccessStatuses(t *testing.T) {
	for _, tool := range []string{"schedule_workout", "schedule_workouts"} {
		for _, code := range []int{201, 202, 204} {
			f := workoutsFake(t)
			f.on("POST "+wSchedule+"123", status(code, rawBody("")))
			sch := map[string]any{"workout_id": 123, "calendar_date": "2026-10-20"}
			args := sch
			if tool == "schedule_workouts" {
				args = map[string]any{"schedules": []any{sch}}
			}
			d := runTool(t, f, tool, args)
			st := d["status"]
			if st == nil {
				st = asMap(toolList(t, "results", d)[0])["status"]
			}
			if st != "success" || len(f.requests("POST "+wSchedule+"123")) != 1 {
				t.Errorf("%s %d: d = %v", tool, code, d)
			}
		}
	}
}

func TestScheduleWorkoutsSingle(t *testing.T) {
	f := scheduleFake(t, "123456")
	d := runTool(t, f, "schedule_workouts", map[string]any{"schedules": []any{map[string]any{"workout_id": 123456, "calendar_date": "2024-01-15"}}})
	r := asMap(toolList(t, "results", d)[0])
	if d["total"] != 1.0 || d["succeeded"] != 1.0 || d["failed"] != 0.0 || r["status"] != "success" || r["workout_id"] != 123456.0 || r["scheduled_date"] != "2024-01-15" {
		t.Errorf("d = %v", d)
	}
	if got := scheduleBodies(t, f, "123456"); !reflect.DeepEqual(got, []any{map[string]any{"date": "2024-01-15"}}) {
		t.Errorf("bodies = %v", got)
	}
}

func TestScheduleWorkoutsMultiple(t *testing.T) {
	f := scheduleFake(t, "111", "222", "333")
	d := runTool(t, f, "schedule_workouts", map[string]any{"schedules": []any{
		map[string]any{"workout_id": 111, "calendar_date": "2024-01-15"},
		map[string]any{"workout_id": 222, "calendar_date": "2024-01-17"},
		map[string]any{"workout_id": 333, "calendar_date": "2024-01-19"}}})
	posts := 0
	for _, c := range f.requests("") {
		if c.Method == "POST" && strings.HasPrefix(c.Path, wSchedule) {
			posts++
		}
	}
	if d["total"] != 3.0 || d["succeeded"] != 3.0 || d["failed"] != 0.0 || posts != 3 {
		t.Errorf("d = %v, posts %d", d, posts)
	}
}

func TestScheduleWorkoutsPartialFailure(t *testing.T) {
	f := scheduleFake(t, "111")
	f.on("POST "+wSchedule+"999", status(404, map[string]any{"message": "Workout not found"}))
	d := runTool(t, f, "schedule_workouts", map[string]any{"schedules": []any{
		map[string]any{"workout_id": 111, "calendar_date": "2024-01-15"}, map[string]any{"workout_id": 999, "calendar_date": "2024-01-17"}}})
	rs := toolList(t, "results", d)
	if d["total"] != 2.0 || d["succeeded"] != 1.0 || d["failed"] != 1.0 || asMap(rs[0])["status"] != "success" || asMap(rs[1])["status"] != "error" ||
		!strings.Contains(asMap(rs[1])["message"].(string), "API Error 404 - Workout not found") {
		t.Errorf("d = %v", d)
	}
}

func scheduleOne(t *testing.T, f *fakeGarmin, item map[string]any) (map[string]any, map[string]any) {
	t.Helper()
	d := runTool(t, f, "schedule_workouts", map[string]any{"schedules": []any{item}})
	return d, asMap(toolList(t, "results", d)[0])
}

func noSchedulePosts(t *testing.T, f *fakeGarmin) {
	t.Helper()
	for _, c := range f.requests("") {
		if c.Method == "POST" && strings.HasPrefix(c.Path, wSchedule) {
			t.Errorf("unexpected schedule POST %s", c.Path)
		}
	}
}

func TestScheduleWorkoutsMissingFields(t *testing.T) {
	f := workoutsFake(t)
	d, r := scheduleOne(t, f, map[string]any{"workout_id": 123456})
	if d["total"] != 1.0 || d["succeeded"] != 0.0 || d["failed"] != 1.0 || r["status"] != "failed" || !strings.Contains(r["message"].(string), "Missing required field") {
		t.Errorf("d = %v", d)
	}
	noSchedulePosts(t, f)
}

func TestScheduleWorkoutsRejectsInvalidDate(t *testing.T) {
	f := workoutsFake(t)
	d, r := scheduleOne(t, f, map[string]any{"workout_id": 123456, "calendar_date": "not-a-date"})
	if d["succeeded"] != 0.0 || d["failed"] != 1.0 || r["status"] != "failed" || !strings.Contains(r["message"].(string), "YYYY-MM-DD") {
		t.Errorf("d = %v", d)
	}
	noSchedulePosts(t, f)
}

func TestScheduleWorkoutsInvalidDateSkipsInlineUpload(t *testing.T) {
	f := uploadFake(t, map[string]any{"workoutId": 1.0})
	inline := runningWorkoutWithSteps(timedIntervalStep(hrZoneTarget()))
	d, r := scheduleOne(t, f, map[string]any{"workout_data": inline, "calendar_date": "2024/02/01"})
	if d["failed"] != 1.0 || r["status"] != "failed" || !strings.Contains(r["message"].(string), "YYYY-MM-DD") || len(f.requests(wUpload)) != 0 {
		t.Errorf("d = %v", d)
	}
	noSchedulePosts(t, f)
}

func TestScheduleWorkoutRejectsInvalidDate(t *testing.T) {
	f := workoutsFake(t)
	d := runTool(t, f, "schedule_workout", map[string]any{"workout_id": 123456, "calendar_date": "01-15-2024"})
	if d["status"] != "failed" || d["workout_id"] != 123456.0 || !strings.Contains(d["message"].(string), "YYYY-MM-DD") || len(f.requests(wGraphQL)) != 0 {
		t.Errorf("d = %v", d)
	}
	noSchedulePosts(t, f)
}

func TestScheduleWorkoutsException(t *testing.T) {
	f := workoutsFake(t)
	f.on("POST "+wSchedule+"123456", status(503, map[string]any{"message": "Network error"}))
	d, r := scheduleOne(t, f, map[string]any{"workout_id": 123456, "calendar_date": "2024-01-15"})
	if d["total"] != 1.0 || d["succeeded"] != 0.0 || d["failed"] != 1.0 || r["status"] != "error" || !strings.Contains(r["message"].(string), "Network error") {
		t.Errorf("d = %v", d)
	}
}

func scheduledOn(id float64, date string) map[string]any {
	return map[string]any{"data": map[string]any{"workoutScheduleSummariesScalar": []any{
		map[string]any{"workoutId": id, "scheduleDate": date, "workoutName": "Easy Run"}}}}
}

func TestScheduleWorkoutsIdempotent(t *testing.T) {
	f := workoutsFake(t)
	f.on(wGraphQL, scheduledOn(123456, "2024-01-15"))
	d, r := scheduleOne(t, f, map[string]any{"workout_id": 123456, "calendar_date": "2024-01-15"})
	if d["total"] != 1.0 || d["succeeded"] != 1.0 || d["failed"] != 0.0 || r["status"] != "success" || r["idempotent"] != true {
		t.Errorf("d = %v", d)
	}
	noSchedulePosts(t, f)
}

func TestScheduleWorkoutIdempotent(t *testing.T) {
	f := workoutsFake(t)
	f.on(wGraphQL, scheduledOn(123456, "2024-01-15"))
	d := runTool(t, f, "schedule_workout", map[string]any{"workout_id": 123456, "calendar_date": "2024-01-15"})
	if d["status"] != "success" || d["idempotent"] != true || d["message"] != "Workout 123456 already scheduled for 2024-01-15 — no action taken" {
		t.Errorf("d = %v", d)
	}
	noSchedulePosts(t, f)
}

func TestScheduleWorkoutsStringWorkoutIdIsIdempotent(t *testing.T) {
	f := workoutsFake(t)
	f.on(wGraphQL, scheduledOn(123456, "2024-01-15"))
	_, r := scheduleOne(t, f, map[string]any{"workout_id": "123456", "calendar_date": "2024-01-15"})
	if r["status"] != "success" || r["idempotent"] != true || r["workout_id"] != 123456.0 {
		t.Errorf("r = %v", r)
	}
	noSchedulePosts(t, f)
}

func TestScheduleWorkoutsNonStringDateKeepsEarlierResults(t *testing.T) {
	f := scheduleFake(t, "111", "222")
	d := runTool(t, f, "schedule_workouts", map[string]any{"schedules": []any{
		map[string]any{"workout_id": 111, "calendar_date": "2024-01-15"}, map[string]any{"workout_id": 222, "calendar_date": 20240117}}})
	rs := toolList(t, "results", d)
	r1 := asMap(rs[1])
	if d["total"] != 2.0 || d["succeeded"] != 1.0 || asMap(rs[0])["status"] != "success" || r1["status"] != "failed" ||
		r1["message"] != "Invalid calendar_date 20240117: expected YYYY-MM-DD" {
		t.Errorf("d = %v", d)
	}
	if len(f.requests("POST "+wSchedule+"222")) != 0 || len(scheduleBodies(t, f, "111")) != 1 {
		t.Error("wrong POSTs")
	}
}

func TestScheduleWorkoutsRejectsNonexistentOrPaddedDate(t *testing.T) {
	for _, date := range []string{"2024-02-30", "2024-13-01", "2024-01-15\n"} {
		f := scheduleFake(t, "123456")
		_, r := scheduleOne(t, f, map[string]any{"workout_id": 123456, "calendar_date": date})
		if r["status"] != "failed" || !strings.Contains(r["message"].(string), "YYYY-MM-DD") {
			t.Errorf("%q: r = %v", date, r)
		}
		noSchedulePosts(t, f)
	}
}

func TestScheduleWorkoutsInlineUpload(t *testing.T) {
	f := uploadFake(t, map[string]any{"workoutId": 999001.0, "workoutName": "Easy Run"})
	f.on("POST "+wSchedule+"999001", map[string]any{})
	inline := map[string]any{"workoutName": "Easy Run", "sportType": map[string]any{"sportTypeId": 1.0, "sportTypeKey": "running"}}
	d, r := scheduleOne(t, f, map[string]any{"workout_data": inline, "calendar_date": "2024-02-01"})
	if d["total"] != 1.0 || d["succeeded"] != 1.0 || d["failed"] != 0.0 || r["status"] != "success" || r["workout_id"] != 999001.0 ||
		r["scheduled_date"] != "2024-02-01" || r["workout_name"] != "Easy Run" {
		t.Errorf("d = %v", d)
	}
	if !reflect.DeepEqual(uploadedWorkout(t, f), inline) || !reflect.DeepEqual(scheduleBodies(t, f, "999001"), []any{map[string]any{"date": "2024-02-01"}}) {
		t.Error("wrong upload or schedule body")
	}
}

func TestScheduleWorkoutsInlinePromotesNestedTargetBounds(t *testing.T) {
	s, _ := uploadStepVia(t, "schedule_workouts", runningWorkoutWithSteps(distancePaceStepWithNestedBounds()), 0)
	tt := asMap(s["targetType"])
	_, a := tt["targetValueOne"]
	_, b := tt["targetValueTwo"]
	if s["targetValueOne"] != 2.0833333 || s["targetValueTwo"] != 1.9607843 || a || b {
		t.Errorf("step = %v", s)
	}
}

func TestScheduleWorkoutsInlineUploadRejectsEndConditionMismatch(t *testing.T) {
	f := uploadFake(t, map[string]any{"workoutId": 1.0})
	inline := runningWorkoutWithSteps(recoveryStep(4.0, "heart.rate"))
	d, r := scheduleOne(t, f, map[string]any{"workout_data": inline, "calendar_date": "2024-02-01"})
	if d["total"] != 1.0 || d["succeeded"] != 0.0 || d["failed"] != 1.0 || r["status"] != "error" ||
		!strings.Contains(r["message"].(string), "conditionTypeKey 'heart.rate' requires conditionTypeId 6") || len(f.requests(wUpload)) != 0 {
		t.Errorf("d = %v", d)
	}
	noSchedulePosts(t, f)
}

func TestScheduleWorkoutsRejectsInlineTargetTypeMismatch(t *testing.T) {
	f := uploadFake(t, map[string]any{"workoutId": 1.0})
	inline := runningWorkoutWithSteps(timedIntervalStep(map[string]any{"workoutTargetTypeId": 6.0, "workoutTargetTypeKey": "heart.rate"}))
	d, r := scheduleOne(t, f, map[string]any{"workout_data": inline, "calendar_date": "2024-02-01"})
	if d["failed"] != 1.0 || r["status"] != "error" || !strings.Contains(r["message"].(string), "targetType mismatch") || len(f.requests(wUpload)) != 0 {
		t.Errorf("d = %v", d)
	}
	noSchedulePosts(t, f)
}

func TestScheduleWorkoutsMixedInlineAndId(t *testing.T) {
	f := uploadFake(t, map[string]any{"workoutId": 999002.0, "workoutName": "Tempo Run"})
	f.on("POST "+wSchedule+"111", map[string]any{})
	f.on("POST "+wSchedule+"999002", map[string]any{})
	inline := map[string]any{"workoutName": "Tempo Run", "sportType": map[string]any{"sportTypeId": 1.0, "sportTypeKey": "running"}}
	d := runTool(t, f, "schedule_workouts", map[string]any{"schedules": []any{
		map[string]any{"workout_id": 111, "calendar_date": "2024-02-05"}, map[string]any{"workout_data": inline, "calendar_date": "2024-02-07"}}})
	rs := toolList(t, "results", d)
	if d["total"] != 2.0 || d["succeeded"] != 2.0 || d["failed"] != 0.0 || asMap(rs[0])["workout_id"] != 111.0 || asMap(rs[1])["workout_id"] != 999002.0 {
		t.Errorf("d = %v", d)
	}
}

func TestScheduleWorkoutsMissingBothIdAndData(t *testing.T) {
	f := workoutsFake(t)
	d, r := scheduleOne(t, f, map[string]any{"calendar_date": "2024-02-01"})
	msg := r["message"].(string)
	if d["total"] != 1.0 || d["succeeded"] != 0.0 || d["failed"] != 1.0 || (!strings.Contains(msg, "workout_id") && !strings.Contains(msg, "workout_data")) {
		t.Errorf("d = %v", d)
	}
	noSchedulePosts(t, f)
}

func TestScheduleWorkoutsInlineUploadNoIdReturned(t *testing.T) {
	f := uploadFake(t, map[string]any{"workoutName": "Bad Response"})
	d, r := scheduleOne(t, f, map[string]any{"workout_data": map[string]any{"workoutName": "Bad Response"}, "calendar_date": "2024-02-01"})
	if d["total"] != 1.0 || d["succeeded"] != 0.0 || d["failed"] != 1.0 || r["status"] != "failed" {
		t.Errorf("d = %v", d)
	}
	noSchedulePosts(t, f)
}

// ---- unschedule / reschedule ----

func TestUnscheduleWorkoutSuccess(t *testing.T) {
	f := workoutsFake(t)
	f.on("DELETE "+wSchedule+"1677275789", map[string]any{})
	d := runTool(t, f, "unschedule_workout", map[string]any{"scheduled_workout_id": 1677275789})
	if d["status"] != "success" || d["scheduled_workout_id"] != 1677275789.0 || !strings.Contains(d["message"].(string), "removed from calendar") ||
		len(f.requests("DELETE "+wSchedule+"1677275789")) != 1 {
		t.Errorf("d = %v", d)
	}
}

func TestUnscheduleWorkoutError(t *testing.T) {
	f := workoutsFake(t)
	f.on("DELETE "+wSchedule+"999", status(503, map[string]any{"message": "Network error"}))
	d := runTool(t, f, "unschedule_workout", map[string]any{"scheduled_workout_id": 999})
	if d["status"] != "failed" || d["scheduled_workout_id"] != 999.0 || !strings.Contains(d["message"].(string), "Network error") {
		t.Errorf("d = %v", d)
	}
}

func scheduledEntry() map[string]any {
	return map[string]any{
		"workoutScheduleId": 1677275789.0, "calendarDate": "2024-01-15",
		"workout":     map[string]any{"workoutId": 123456.0, "workoutName": "5K Tempo Run", "workoutSegments": []any{map[string]any{"segmentOrder": 1.0}}},
		"ownerId":     80653452.0,
		"createdDate": "2024-01-10T08:00:00.0",
	}
}

func TestRescheduleWorkoutSuccess(t *testing.T) {
	f := workoutsFake(t)
	f.on("GET "+wSchedule+"1677275789", scheduledEntry())
	f.on("PUT "+wSchedule+"1677275789", map[string]any{})
	d := runTool(t, f, "reschedule_workout", map[string]any{"scheduled_workout_id": 1677275789, "calendar_date": "2024-01-17"})
	if d["status"] != "success" || d["previous_date"] != "2024-01-15" || d["scheduled_date"] != "2024-01-17" || d["workout_id"] != 123456.0 {
		t.Errorf("d = %v", d)
	}
	want := map[string]any{
		"workoutScheduleId": 1677275789.0, "calendarDate": "2024-01-17", "workout": map[string]any{"workoutId": 123456.0},
		"ownerId": 80653452.0, "createdDate": "2024-01-10T08:00:00.0",
	}
	if got := putJSON(t, f, "PUT "+wSchedule+"1677275789"); !reflect.DeepEqual(got, want) {
		t.Errorf("PUT body = %v", got)
	}
	if len(f.requests("GET "+wSchedule+"1677275789")) != 1 {
		t.Error("entry not read exactly once")
	}
}

func TestRescheduleWorkoutInvalidDate(t *testing.T) {
	f := workoutsFake(t)
	d := runTool(t, f, "reschedule_workout", map[string]any{"scheduled_workout_id": 1677275789, "calendar_date": "2024-02-30"})
	if d["status"] != "failed" || len(f.requests("GET "+wSchedule+"1677275789")) != 0 || len(f.requests("PUT "+wSchedule+"1677275789")) != 0 {
		t.Errorf("d = %v", d)
	}
}

func TestRescheduleWorkoutNotFound(t *testing.T) {
	f := workoutsFake(t)
	f.on("GET "+wSchedule+"999", nil)
	d := runTool(t, f, "reschedule_workout", map[string]any{"scheduled_workout_id": 999, "calendar_date": "2024-01-17"})
	if d["status"] != "failed" || !strings.Contains(d["message"].(string), "not found") || len(f.requests("PUT "+wSchedule+"999")) != 0 {
		t.Errorf("d = %v", d)
	}
}

func TestRescheduleWorkoutError(t *testing.T) {
	f := workoutsFake(t)
	f.on("GET "+wSchedule+"1677275789", scheduledEntry())
	f.on("PUT "+wSchedule+"1677275789", status(400, map[string]any{"message": "bad"}))
	d := runTool(t, f, "reschedule_workout", map[string]any{"scheduled_workout_id": 1677275789, "calendar_date": "2024-01-17"})
	if d["status"] != "failed" || !strings.Contains(d["message"].(string), "400") {
		t.Errorf("d = %v", d)
	}
}

func TestUnscheduleWorkoutsMultiple(t *testing.T) {
	f := workoutsFake(t)
	for _, id := range []string{"111", "222", "333"} {
		f.on("DELETE "+wSchedule+id, map[string]any{})
	}
	d := runTool(t, f, "unschedule_workouts", map[string]any{"scheduled_workout_ids": []any{111, 222, 333}})
	if d["total"] != 3.0 || d["succeeded"] != 3.0 || d["failed"] != 0.0 || len(f.requests("DELETE "+wSchedule+"222")) != 1 {
		t.Errorf("d = %v", d)
	}
}

func TestUnscheduleWorkoutsPartialFailure(t *testing.T) {
	f := workoutsFake(t)
	f.on("DELETE "+wSchedule+"111", map[string]any{})
	f.on("DELETE "+wSchedule+"999", status(404, map[string]any{"message": "API Error 404"}))
	d := runTool(t, f, "unschedule_workouts", map[string]any{"scheduled_workout_ids": []any{111, 999}})
	rs := toolList(t, "results", d)
	if d["total"] != 2.0 || d["succeeded"] != 1.0 || d["failed"] != 1.0 || asMap(rs[0])["status"] != "success" || asMap(rs[1])["status"] != "error" ||
		!strings.Contains(asMap(rs[1])["message"].(string), "404") {
		t.Errorf("d = %v", d)
	}
}

// Python test_tool_input_schemas.py: an optional parameter must accept null when it advertises a null default.
// The workout tools have no null-default parameter, so the check is that optional ones are exactly confirm/raw.
func TestParametersDefaultingToNoneAcceptNull(t *testing.T) {
	res, err := newSession(t, newFakeGarmin(t), nil).ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if !workoutToolNames[tool.Name] {
			continue
		}
		schema := asMap(jsonValue(t, tool.InputSchema))
		required := map[string]bool{}
		for _, r := range asList(schema["required"]) {
			required[r.(string)] = true
		}
		for name, p := range asMap(schema["properties"]) {
			if required[name] || name == "confirm" || name == "raw" {
				continue
			}
			if !acceptsNull(asMap(p)) {
				t.Errorf("%s.%s is optional and rejects null", tool.Name, name)
			}
		}
	}
}

func acceptsNull(schema map[string]any) bool {
	switch ty := schema["type"].(type) {
	case string:
		if ty == "null" {
			return true
		}
	case []any:
		for _, x := range ty {
			if x == "null" {
				return true
			}
		}
	}
	for _, o := range asList(schema["anyOf"]) {
		if acceptsNull(asMap(o)) {
			return true
		}
	}
	return false
}

func newSessionLog(t *testing.T, f *fakeGarmin, log io.Writer) *mcp.ClientSession {
	t.Helper()
	srv, err := New(testOptions(f, newStore(t), nil, log))
	if err != nil {
		t.Fatal(err)
	}
	return connect(t, srv)
}

var workoutToolNames = map[string]bool{
	"get_workouts": true, "get_workout_by_id": true, "download_workout": true, "upload_workout": true, "upload_workouts": true,
	"delete_workout": true, "delete_workouts": true, "get_scheduled_workouts": true, "get_garmin_coach_workouts": true,
	"get_training_plan_workouts": true, "schedule_workout": true, "schedule_workouts": true, "reschedule_workout": true,
	"unschedule_workout": true, "unschedule_workouts": true, "get_calendar_events": true,
}
