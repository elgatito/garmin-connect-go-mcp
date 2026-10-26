package server

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func intPtr(v int) *int { return &v }

func mustBuild(t *testing.T, build func() (map[string]any, error)) map[string]any {
	t.Helper()
	workout, err := build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return workout
}

func buildSteps(t *testing.T, workout map[string]any) []any {
	t.Helper()
	return mutList(t, mutMap(t, mutList(t, workout["workoutSegments"])[0])["workoutSteps"])
}

func stepAt(t *testing.T, steps []any, i int) map[string]any {
	t.Helper()
	return mutMap(t, steps[i])
}

func workoutUploaded(t *testing.T, f *fakeGarmin) map[string]any {
	t.Helper()
	calls := f.callsMatching("POST", "/workout-service/workout")
	if len(calls) != 1 {
		t.Fatalf("upload calls = %d, want 1", len(calls))
	}
	var body map[string]any
	decodeJSON(t, string(calls[0].Body), &body)
	return body
}

// --- unit: builders ---

func TestBuildWalkRunJsonMatchesPocSnapshot(t *testing.T) {
	got, err := buildWalkRunJSON("POC Walk/Run 7x1m/3m Z3", 60, 180, 7, 10, 8, "Z3")
	if err != nil {
		t.Fatal(err)
	}
	sameJSON(t, got, fixture(t, "POC_WALK_RUN"))
}

func TestBuildZ2WalkJsonStructure(t *testing.T) {
	got := mustBuild(t, func() (map[string]any, error) { return buildZ2WalkJSON("Z2 Walk 30m", 30, 110, 130) })
	if got["workoutName"] != "Z2 Walk 30m" {
		t.Fatalf("name = %v", got["workoutName"])
	}
	sport := mutMap(t, got["sportType"])
	if sport["sportTypeKey"] != "walking" || num(t, sport["sportTypeId"]) != 12 {
		t.Fatalf("sport = %v", sport)
	}
	steps := buildSteps(t, got)
	if len(steps) != 3 || num(t, stepAt(t, steps, 1)["zoneNumber"]) != 2 || num(t, stepAt(t, steps, 1)["endConditionValue"]) != 1800 {
		t.Fatalf("steps = %v", steps)
	}
}

func TestBuildRunJsonStructure(t *testing.T) {
	got := mustBuild(t, func() (map[string]any, error) {
		return buildRunJSON("Step 8 - 30min continuous", 1800, 5, 5, "Z3", nil, nil)
	})
	sport := mutMap(t, got["sportType"])
	if got["workoutName"] != "Step 8 - 30min continuous" || sport["sportTypeKey"] != "running" || num(t, sport["sportTypeId"]) != 1 {
		t.Fatalf("workout = %v", got)
	}
	steps := buildSteps(t, got)
	if len(steps) != 3 {
		t.Fatalf("steps = %d", len(steps))
	}
	if mutMap(t, stepAt(t, steps, 0)["stepType"])["stepTypeKey"] != "warmup" || num(t, stepAt(t, steps, 0)["endConditionValue"]) != 300 {
		t.Fatalf("warmup = %v", steps[0])
	}
	if mutMap(t, stepAt(t, steps, 1)["stepType"])["stepTypeKey"] != "interval" || num(t, stepAt(t, steps, 1)["endConditionValue"]) != 1800 || num(t, stepAt(t, steps, 1)["zoneNumber"]) != 3 {
		t.Fatalf("interval = %v", steps[1])
	}
	if mutMap(t, stepAt(t, steps, 2)["stepType"])["stepTypeKey"] != "cooldown" || num(t, stepAt(t, steps, 2)["endConditionValue"]) != 300 {
		t.Fatalf("cooldown = %v", steps[2])
	}
}

func TestBuildRunJsonCustomHrRange(t *testing.T) {
	got := mustBuild(t, func() (map[string]any, error) {
		return buildRunJSON("Base run - custom range", 1440, 5, 5, "Z3", intPtr(136), intPtr(148))
	})
	interval := stepAt(t, buildSteps(t, got), 1)
	if mutMap(t, interval["targetType"])["workoutTargetTypeKey"] != "heart.rate.zone" ||
		num(t, interval["targetValueOne"]) != 136 || num(t, interval["targetValueTwo"]) != 148 {
		t.Fatalf("interval = %v", interval)
	}
	if _, ok := interval["zoneNumber"]; ok {
		t.Fatal("zoneNumber present")
	}
	if !strings.Contains(got["description"].(string), "136-148bpm") {
		t.Fatalf("description = %v", got["description"])
	}
}

func TestBuildRunJsonCustomHrRangeRequiresBothBounds(t *testing.T) {
	_, err := buildRunJSON("Bad range", 1440, 5, 5, "Z3", intPtr(136), nil)
	if err == nil || !strings.Contains(err.Error(), "hr_min and hr_max must both be provided together") {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildRunJsonCustomHrRangeRejectsInvertedBounds(t *testing.T) {
	_, err := buildRunJSON("Bad range", 1440, 5, 5, "Z3", intPtr(148), intPtr(136))
	if err == nil || !strings.Contains(err.Error(), "must be less than") {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildRunIntervalJsonStructure(t *testing.T) {
	got := mustBuild(t, func() (map[string]any, error) {
		return buildRunIntervalJSON("VO2 5x3min", 5, 180, 120, 10, 8, "Z5", nil, nil, nil, nil)
	})
	steps := buildSteps(t, got)
	warmup, group, cooldown := stepAt(t, steps, 0), stepAt(t, steps, 1), stepAt(t, steps, 2)
	if warmup["stepType"].(map[string]any)["stepTypeKey"] != "warmup" || num(t, warmup["endConditionValue"]) != 600 {
		t.Fatalf("warmup = %v", warmup)
	}
	if group["type"] != "RepeatGroupDTO" || num(t, group["stepOrder"]) != 2 || num(t, group["numberOfIterations"]) != 5 ||
		mutMap(t, group["endCondition"])["conditionTypeKey"] != "iterations" || num(t, group["endConditionValue"]) != 5 {
		t.Fatalf("group = %v", group)
	}
	work := mutMap(t, mutList(t, group["workoutSteps"])[0])
	recovery := mutMap(t, mutList(t, group["workoutSteps"])[1])
	if num(t, work["stepOrder"]) != 3 || mutMap(t, work["stepType"])["stepTypeKey"] != "interval" ||
		num(t, work["endConditionValue"]) != 180 || num(t, work["zoneNumber"]) != 5 {
		t.Fatalf("work = %v", work)
	}
	if num(t, recovery["stepOrder"]) != 4 || mutMap(t, recovery["stepType"])["stepTypeKey"] != "recovery" ||
		num(t, recovery["endConditionValue"]) != 120 || mutMap(t, recovery["targetType"])["workoutTargetTypeKey"] != "no.target" {
		t.Fatalf("recovery = %v", recovery)
	}
	if num(t, cooldown["stepOrder"]) != 5 || mutMap(t, cooldown["stepType"])["stepTypeKey"] != "cooldown" || num(t, cooldown["endConditionValue"]) != 480 {
		t.Fatalf("cooldown = %v", cooldown)
	}
	if !strings.Contains(got["description"].(string), "5x(3m Z5 / 2m jog)") {
		t.Fatalf("description = %v", got["description"])
	}
}

func TestBuildRunIntervalJsonCustomHrRange(t *testing.T) {
	got := mustBuild(t, func() (map[string]any, error) {
		return buildRunIntervalJSON("Threshold 3x6min", 3, 360, 180, 12, 8, "Z4", intPtr(172), intPtr(180), nil, nil)
	})
	group := stepAt(t, buildSteps(t, got), 1)
	work := mutMap(t, mutList(t, group["workoutSteps"])[0])
	if mutMap(t, work["targetType"])["workoutTargetTypeKey"] != "heart.rate.zone" ||
		num(t, work["targetValueOne"]) != 172 || num(t, work["targetValueTwo"]) != 180 {
		t.Fatalf("work = %v", work)
	}
	if _, ok := work["zoneNumber"]; ok {
		t.Fatal("zoneNumber present")
	}
	if !strings.Contains(got["description"].(string), "172-180bpm") {
		t.Fatalf("description = %v", got["description"])
	}
}

func TestBuildRunIntervalJsonRecoveryHrTarget(t *testing.T) {
	got := mustBuild(t, func() (map[string]any, error) {
		return buildRunIntervalJSON("Controlled recoveries", 4, 240, 120, 10, 5, "Z4", intPtr(165), intPtr(175), intPtr(120), intPtr(140))
	})
	group := stepAt(t, buildSteps(t, got), 1)
	recovery := mutMap(t, mutList(t, group["workoutSteps"])[1])
	if mutMap(t, recovery["targetType"])["workoutTargetTypeKey"] != "heart.rate.zone" ||
		num(t, recovery["targetValueOne"]) != 120 || num(t, recovery["targetValueTwo"]) != 140 ||
		!strings.Contains(recovery["description"].(string), "120-140bpm") {
		t.Fatalf("recovery = %v", recovery)
	}
}

func TestBuildRunIntervalJsonRecoveryHrRequiresBothBounds(t *testing.T) {
	for _, bounds := range []struct{ min, max *int }{{intPtr(120), nil}, {nil, intPtr(140)}} {
		_, err := buildRunIntervalJSON("Half a recovery range", 4, 240, 120, 10, 5, "Z4", nil, nil, bounds.min, bounds.max)
		if err == nil || !strings.Contains(err.Error(), "recovery_hr_min and recovery_hr_max must both be provided together") {
			t.Fatalf("err = %v", err)
		}
	}
}

func TestBuildRunIntervalJsonRecoveryHrRejectsInvertedBounds(t *testing.T) {
	_, err := buildRunIntervalJSON("Inverted recovery range", 4, 240, 120, 10, 5, "Z4", nil, nil, intPtr(140), intPtr(120))
	if err == nil || !strings.Contains(err.Error(), "recovery_hr_min (140) must be less than recovery_hr_max (120)") {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildRunIntervalJsonZeroRecoveryOmitsStep(t *testing.T) {
	got := mustBuild(t, func() (map[string]any, error) {
		return buildRunIntervalJSON("Strides", 6, 20, 0, 10, 5, "Z4", nil, nil, nil, nil)
	})
	steps := buildSteps(t, got)
	group := stepAt(t, steps, 1)
	cooldown := stepAt(t, steps, 2)
	if len(mutList(t, group["workoutSteps"])) != 1 || mutMap(t, mutMap(t, mutList(t, group["workoutSteps"])[0])["stepType"])["stepTypeKey"] != "interval" {
		t.Fatalf("group = %v", group)
	}
	if num(t, cooldown["stepOrder"]) != 4 {
		t.Fatalf("cooldown order = %v", cooldown["stepOrder"])
	}
	if strings.Contains(got["description"].(string), "jog") {
		t.Fatalf("description = %v", got["description"])
	}
}

func TestBuildRunIntervalJsonRejectsBadInputs(t *testing.T) {
	if _, err := buildRunIntervalJSON("Bad", 0, 60, 60, 10, 5, "Z4", nil, nil, nil, nil); err == nil || !strings.Contains(err.Error(), "repeats") {
		t.Fatalf("repeats err = %v", err)
	}
	if _, err := buildRunIntervalJSON("Bad", 3, 0, 60, 10, 5, "Z4", nil, nil, nil, nil); err == nil || !strings.Contains(err.Error(), "rep_seconds") {
		t.Fatalf("rep err = %v", err)
	}
	if _, err := buildRunIntervalJSON("Bad", 3, 60, -1, 10, 5, "Z4", nil, nil, nil, nil); err == nil || !strings.Contains(err.Error(), "recovery_seconds") {
		t.Fatalf("recovery err = %v", err)
	}
}

func TestBuildStrengthJsonStructure(t *testing.T) {
	got := mustBuild(t, func() (map[string]any, error) {
		return buildStrengthJSON("Full Body A", []map[string]any{
			{"name": "Sentadillas", "sets": 3, "reps": 12, "rest_seconds": 90},
			{"name": "Flexiones", "sets": 3, "reps": 15, "rest_seconds": 60},
		})
	})
	sport := mutMap(t, got["sportType"])
	if got["workoutName"] != "Full Body A" || sport["sportTypeKey"] != "strength_training" || num(t, sport["sportTypeId"]) != 5 {
		t.Fatalf("workout = %v", got)
	}
	steps := buildSteps(t, got)
	if len(steps) != 2 || stepAt(t, steps, 0)["type"] != "RepeatGroupDTO" || stepAt(t, steps, 1)["type"] != "RepeatGroupDTO" {
		t.Fatalf("steps = %v", steps)
	}
	if num(t, stepAt(t, steps, 0)["numberOfIterations"]) != 3 {
		t.Fatalf("sets = %v", stepAt(t, steps, 0)["numberOfIterations"])
	}
	if mutMap(t, mutList(t, stepAt(t, steps, 0)["workoutSteps"])[0])["exerciseName"] != "Sentadillas" {
		t.Fatal("exercise name lost")
	}
	if mutMap(t, mutList(t, stepAt(t, steps, 1)["workoutSteps"])[0])["exerciseName"] != "Flexiones" {
		t.Fatal("exercise name lost")
	}
}

func TestStrengthSingleSetStaysFlat(t *testing.T) {
	got := mustBuild(t, func() (map[string]any, error) {
		return buildStrengthJSON("Singles", []map[string]any{
			{"name": "Plank Hold", "sets": 1, "reps": 1, "rest_seconds": 60},
			{"name": "Dead Hang", "sets": 1, "reps": 1, "rest_seconds": 0},
		})
	})
	steps := buildSteps(t, got)
	if len(steps) != 3 {
		t.Fatalf("steps = %d", len(steps))
	}
	for _, s := range steps {
		if mutMap(t, s)["type"] != "ExecutableStepDTO" {
			t.Fatalf("step = %v", s)
		}
	}
	if stepAt(t, steps, 0)["exerciseName"] != "Plank Hold" || mutMap(t, stepAt(t, steps, 1)["stepType"])["stepTypeKey"] != "recovery" {
		t.Fatalf("steps = %v", steps)
	}
}

func TestStrengthMultiSetRepeatGroupShape(t *testing.T) {
	got := mustBuild(t, func() (map[string]any, error) {
		return buildStrengthJSON("Sets", []map[string]any{
			{"name": "Goblet Squat", "sets": 4, "reps": 8, "rest_seconds": 90},
		})
	})
	steps := buildSteps(t, got)
	if len(steps) != 1 {
		t.Fatalf("steps = %d", len(steps))
	}
	group := stepAt(t, steps, 0)
	if group["type"] != "RepeatGroupDTO" || num(t, group["numberOfIterations"]) != 4 ||
		mutMap(t, group["endCondition"])["conditionTypeKey"] != "iterations" ||
		mutMap(t, group["stepType"])["stepTypeKey"] != "repeat" {
		t.Fatalf("group = %v", group)
	}
	work := mutMap(t, mutList(t, group["workoutSteps"])[0])
	rest := mutMap(t, mutList(t, group["workoutSteps"])[1])
	if mutMap(t, work["endCondition"])["conditionTypeKey"] != "reps" || num(t, work["endConditionValue"]) != 8 ||
		!strings.Contains(work["description"].(string), "4 sets x 8 reps") {
		t.Fatalf("work = %v", work)
	}
	if mutMap(t, rest["stepType"])["stepTypeKey"] != "recovery" || num(t, rest["endConditionValue"]) != 90 {
		t.Fatalf("rest = %v", rest)
	}
}

func TestStrengthSingleSetIdenticalExercisesKeepMiddleRest(t *testing.T) {
	got := mustBuild(t, func() (map[string]any, error) {
		return buildStrengthJSON("Duplicates", []map[string]any{
			{"name": "Plank Hold", "sets": 1, "reps": 1, "rest_seconds": 45},
			{"name": "Plank Hold", "sets": 1, "reps": 1, "rest_seconds": 45},
		})
	})
	steps := buildSteps(t, got)
	if len(steps) != 3 || mutMap(t, stepAt(t, steps, 1)["stepType"])["stepTypeKey"] != "recovery" {
		t.Fatalf("steps = %v", steps)
	}
}

func TestStrengthMultiSetWithoutRestHasNoRecoveryStep(t *testing.T) {
	got := mustBuild(t, func() (map[string]any, error) {
		return buildStrengthJSON("No rest", []map[string]any{
			{"name": "Push Up", "sets": 3, "reps": 12, "rest_seconds": 0},
		})
	})
	steps := buildSteps(t, got)
	group := stepAt(t, steps, 0)
	if num(t, group["numberOfIterations"]) != 3 || len(mutList(t, group["workoutSteps"])) != 1 ||
		mutMap(t, mutMap(t, mutList(t, group["workoutSteps"])[0])["stepType"])["stepTypeKey"] != "interval" {
		t.Fatalf("group = %v", group)
	}
}

func strengthWorkSteps(workout map[string]any) []map[string]any {
	var flat []map[string]any
	for _, s := range buildStepsNoT(workout) {
		m := s.(map[string]any)
		children := []any{m}
		if nested, ok := m["workoutSteps"].([]any); ok {
			children = nested
		}
		for _, c := range children {
			cm := c.(map[string]any)
			if cm["stepType"].(map[string]any)["stepTypeKey"] == "interval" {
				flat = append(flat, cm)
			}
		}
	}
	return flat
}

func buildStepsNoT(workout map[string]any) []any {
	return workout["workoutSegments"].([]any)[0].(map[string]any)["workoutSteps"].([]any)
}

func TestStrengthOmitsCategoryWhenNotSupplied(t *testing.T) {
	got := mustBuild(t, func() (map[string]any, error) {
		return buildStrengthJSON("No categories", []map[string]any{
			{"name": "Back Squat", "sets": 3, "reps": 5, "rest_seconds": 120},
			{"name": "Zercher Whatever", "sets": 3, "reps": 8, "rest_seconds": 60},
		})
	})
	works := strengthWorkSteps(got)
	if len(works) != 2 {
		t.Fatalf("work steps = %d", len(works))
	}
	for _, step := range works {
		if _, ok := step["category"]; ok {
			t.Fatalf("category present: %v", step)
		}
	}
	if !strings.HasPrefix(works[0]["description"].(string), "Back Squat:") {
		t.Fatalf("description = %v", works[0]["description"])
	}
}

func TestStrengthPassesThroughSuppliedCategory(t *testing.T) {
	got := mustBuild(t, func() (map[string]any, error) {
		return buildStrengthJSON("Mixed", []map[string]any{
			{"name": "Farmers Carry 40m", "sets": 3, "reps": 1, "category": "carry"},
			{"name": "Back Squat", "sets": 3, "reps": 5},
		})
	})
	works := strengthWorkSteps(got)
	if works[0]["category"] != "CARRY" || works[0]["exerciseName"] != "Farmers Carry 40m" {
		t.Fatalf("first = %v", works[0])
	}
	if _, ok := works[1]["category"]; ok {
		t.Fatal("category leaked to second exercise")
	}
}

func TestStrengthRejectsEmptyCategory(t *testing.T) {
	for _, bad := range []any{"", "   ", 5} {
		_, err := buildStrengthJSON("Bad", []map[string]any{
			{"name": "Back Squat", "sets": 1, "reps": 1, "category": bad},
		})
		if err == nil {
			t.Fatalf("category %v accepted", bad)
		}
	}
}

func TestBuildWalkRunJsonRepeatGroupShape(t *testing.T) {
	got := mustBuild(t, func() (map[string]any, error) { return buildWalkRunJSON("2:2 x5", 120, 120, 5, 10, 8, "Z3") })
	steps := buildSteps(t, got)
	warmup, group, cooldown := stepAt(t, steps, 0), stepAt(t, steps, 1), stepAt(t, steps, 2)
	if num(t, warmup["stepOrder"]) != 1 || num(t, group["stepOrder"]) != 2 {
		t.Fatalf("orders: %v %v", warmup["stepOrder"], group["stepOrder"])
	}
	run := mutMap(t, mutList(t, group["workoutSteps"])[0])
	walk := mutMap(t, mutList(t, group["workoutSteps"])[1])
	if num(t, run["stepOrder"]) != 3 || num(t, walk["stepOrder"]) != 4 || num(t, cooldown["stepOrder"]) != 5 {
		t.Fatalf("child orders = %v %v %v", run["stepOrder"], walk["stepOrder"], cooldown["stepOrder"])
	}
	if num(t, group["numberOfIterations"]) != 5 || num(t, group["endConditionValue"]) != 5 || group["smartRepeat"] != false {
		t.Fatalf("group = %v", group)
	}
	assertDepthFirstNumbering(t, got)
}

func TestBuildStrengthJsonNumbersStepsDepthFirst(t *testing.T) {
	got := mustBuild(t, func() (map[string]any, error) {
		return buildStrengthJSON("Mixed", []map[string]any{
			{"name": "Squat", "sets": 3, "reps": 5, "rest_seconds": 90},
			{"name": "Plank", "sets": 1, "reps": 1, "rest_seconds": 60},
			{"name": "Push Up", "sets": 2, "reps": 12, "rest_seconds": 0},
		})
	})
	steps := buildSteps(t, got)
	if len(steps) != 4 {
		t.Fatalf("steps = %d", len(steps))
	}
	if stepAt(t, steps, 0)["type"] != "RepeatGroupDTO" || stepAt(t, steps, 1)["type"] != "ExecutableStepDTO" ||
		stepAt(t, steps, 2)["type"] != "ExecutableStepDTO" || stepAt(t, steps, 3)["type"] != "RepeatGroupDTO" {
		t.Fatalf("types = %v", steps)
	}
	if got := stepOrders(got); !sameInts(got, []int{1, 2, 3, 4, 5, 6, 7}) {
		t.Fatalf("orders = %v", got)
	}
	if num(t, stepAt(t, steps, 0)["endConditionValue"]) != 3 || num(t, stepAt(t, steps, 3)["endConditionValue"]) != 2 {
		t.Fatalf("group values = %v %v", stepAt(t, steps, 0)["endConditionValue"], stepAt(t, steps, 3)["endConditionValue"])
	}
	assertDepthFirstNumbering(t, got)
}

func TestBuildStrengthJsonTwoMultiSetExercisesHaveDistinctOrders(t *testing.T) {
	got := mustBuild(t, func() (map[string]any, error) {
		return buildStrengthJSON("Two groups", []map[string]any{
			{"name": "A", "sets": 3, "reps": 10, "rest_seconds": 60},
			{"name": "B", "sets": 3, "reps": 10, "rest_seconds": 60},
		})
	})
	if orders := stepOrders(got); !sameInts(orders, []int{1, 2, 3, 4, 5, 6}) {
		t.Fatalf("orders = %v", orders)
	}
}

func walkSteps(steps []any) []any {
	var out []any
	for _, s := range steps {
		m := s.(map[string]any)
		out = append(out, m)
		if nested, ok := m["workoutSteps"].([]any); ok {
			out = append(out, walkSteps(nested)...)
		}
	}
	return out
}

func stepOrders(workout map[string]any) []int {
	var orders []int
	for _, segment := range workout["workoutSegments"].([]any) {
		for _, s := range walkSteps(segment.(map[string]any)["workoutSteps"].([]any)) {
			m := s.(map[string]any)
			order, _ := numberOf(m["stepOrder"])
			orders = append(orders, int(order))
		}
	}
	return orders
}

func sameInts(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func assertDepthFirstNumbering(t *testing.T, workout map[string]any) {
	t.Helper()
	orders := stepOrders(workout)
	for i := range orders {
		if orders[i] != i+1 {
			t.Fatalf("orders = %v", orders)
		}
	}
	for _, segment := range workout["workoutSegments"].([]any) {
		for _, s := range walkSteps(segment.(map[string]any)["workoutSteps"].([]any)) {
			m := s.(map[string]any)
			if m["type"] != "RepeatGroupDTO" {
				continue
			}
			if mutMap(t, m["stepType"])["stepTypeKey"] != "repeat" ||
				mutMap(t, m["endCondition"])["conditionTypeKey"] != "iterations" ||
				num(t, m["endConditionValue"]) != num(t, m["numberOfIterations"]) {
				t.Fatalf("bad repeat group %v", m)
			}
		}
	}
}

func TestEveryBuilderAndTemplateNumbersStepsDepthFirst(t *testing.T) {
	cases := map[string]map[string]any{}
	cases["walk_run"] = mustBuild(t, func() (map[string]any, error) { return buildWalkRunJSON("w", 60, 60, 5, 5, 5, "Z3") })
	cases["z2_walk"] = mustBuild(t, func() (map[string]any, error) { return buildZ2WalkJSON("z", 30, 110, 130) })
	cases["run"] = mustBuild(t, func() (map[string]any, error) { return buildRunJSON("r", 1800, 5, 5, "Z3", nil, nil) })
	cases["run_interval"] = mustBuild(t, func() (map[string]any, error) {
		return buildRunIntervalJSON("i", 5, 180, 120, 10, 8, "Z4", nil, nil, nil, nil)
	})
	cases["run_interval_no_recovery"] = mustBuild(t, func() (map[string]any, error) {
		return buildRunIntervalJSON("strides", 6, 20, 0, 10, 5, "Z4", nil, nil, nil, nil)
	})
	cases["strength"] = mustBuild(t, func() (map[string]any, error) {
		return buildStrengthJSON("s", []map[string]any{
			{"name": "A", "sets": 3, "reps": 10, "rest_seconds": 60},
			{"name": "B", "sets": 1, "reps": 10, "rest_seconds": 60},
			{"name": "C", "sets": 2, "reps": 10, "rest_seconds": 0},
		})
	})
	for name, body := range map[string]string{
		"simple_run_template":       simpleRunBody,
		"interval_running_template": intervalRunningBody,
		"tempo_run_template":        tempoRunBody,
		"strength_circuit_template": strengthCircuitBody,
	} {
		var v map[string]any
		decodeJSON(t, body, &v)
		cases[name] = v
	}
	for name, workout := range cases {
		t.Run(name, func(t *testing.T) { assertDepthFirstNumbering(t, workout) })
	}
}

// --- integration: validation ---

func TestBuildersRejectInvalidCountsAndDurationsBeforeUpload(t *testing.T) {
	cases := []struct {
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
		{"create_strength_workout", map[string]any{"exercises": []any{map[string]any{"name": "Squat", "sets": 0}}}, "sets"},
		{"create_strength_workout", map[string]any{"exercises": []any{map[string]any{"name": "Squat", "sets": -1}}}, "sets"},
		{"create_strength_workout", map[string]any{"exercises": []any{map[string]any{"name": "Squat", "reps": 0}}}, "reps"},
		{"create_strength_workout", map[string]any{"exercises": []any{map[string]any{"name": "Squat", "reps": -1}}}, "reps"},
		{"create_strength_workout", map[string]any{"exercises": []any{map[string]any{"name": "Squat", "rest_seconds": -1}}}, "rest_seconds"},
	}
	defaults := map[string]map[string]any{
		"create_run_workout":          {"run_seconds": 300, "warmup_min": 5, "cooldown_min": 5},
		"create_run_interval_workout": {"repeats": 3, "rep_seconds": 60, "recovery_seconds": 60, "warmup_min": 5, "cooldown_min": 5},
		"create_walk_run_workout":     {"run_seconds": 60, "walk_seconds": 60, "repeats": 3, "warmup_min": 5, "cooldown_min": 5},
		"create_z2_walk_workout":      {"duration_min": 30, "hr_min": 110, "hr_max": 130},
		"create_strength_workout":     {"exercises": []any{map[string]any{"name": "Squat", "reps": 5}}},
	}
	for _, tc := range cases {
		t.Run(tc.tool+"/"+tc.field, func(t *testing.T) {
			f := newFakeGarmin(t)
			cs := newSession(t, f, nil)
			args := map[string]any{"name": "Invalid workout"}
			for k, v := range defaults[tc.tool] {
				args[k] = v
			}
			for k, v := range tc.args {
				args[k] = v
			}
			text, _ := callTool(t, cs, tc.tool, args)
			if !strings.Contains(text, "Error creating") || !strings.Contains(text, tc.field) {
				t.Fatalf("text = %q", text)
			}
			if calls := f.callsMatching("POST", "/workout-service/workout"); len(calls) != 0 {
				t.Fatalf("upload calls = %d", len(calls))
			}
		})
	}
}

func TestZeroRecoveryRejectsUnusedHrBounds(t *testing.T) {
	cases := []map[string]any{
		{"recovery_hr_min": 120},
		{"recovery_hr_max": 140},
		{"recovery_hr_min": 140, "recovery_hr_max": 120},
		{"recovery_hr_min": 120, "recovery_hr_max": 140},
	}
	for _, bounds := range cases {
		f := newFakeGarmin(t)
		cs := newSession(t, f, nil)
		args := map[string]any{
			"name": "Strides", "repeats": 6, "rep_seconds": 20,
			"recovery_seconds": 0, "warmup_min": 10, "cooldown_min": 5,
		}
		for k, v := range bounds {
			args[k] = v
		}
		text, _ := callTool(t, cs, "create_run_interval_workout", args)
		if !strings.Contains(text, "Error creating run interval workout") || !strings.Contains(text, "recovery_hr_") {
			t.Fatalf("text = %q", text)
		}
		if calls := f.callsMatching("POST", "/workout-service/workout"); len(calls) != 0 {
			t.Fatalf("upload calls = %d", len(calls))
		}
	}
}

// --- integration: schedule_week ---

func TestScheduleWeekInvalidIdNamesFieldAndContinues(t *testing.T) {
	for _, workoutID := range []any{nil, "abc", 0, -1} {
		t.Run(fmt.Sprintf("id_%v", workoutID), func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on("POST /graphql-gateway/graphql", map[string]any{"data": map[string]any{"workoutScheduleSummariesScalar": []any{}}})
			f.on("POST /workout-service/schedule/123", status(200, nil))
			cs := newSession(t, f, nil)

			text, isErr := callTool(t, cs, "schedule_week", map[string]any{"week": []any{
				map[string]any{"date": "2026-10-20", "workout_id": workoutID},
				map[string]any{"date": "2026-10-21", "workout_id": 123},
			}})
			if isErr {
				t.Fatalf("unexpected error: %q", text)
			}
			entries := mutList(t, calendarText(t, text)["scheduled"])
			first := mutMap(t, entries[0])
			if first["status"] != "failed" || !strings.Contains(first["message"].(string), "workout_id") {
				t.Fatalf("first = %v", first)
			}
			if mutMap(t, entries[1])["status"] != "scheduled" {
				t.Fatalf("second = %v", entries[1])
			}
			if calls := f.callsMatching("POST", "/workout-service/schedule/123"); len(calls) != 1 {
				t.Fatalf("post calls = %d", len(calls))
			}
		})
	}
}

func TestScheduleWeekAcceptsSuccessStatuses(t *testing.T) {
	for _, code := range []int{201, 202, 204} {
		f := newFakeGarmin(t)
		f.on("POST /graphql-gateway/graphql", map[string]any{"data": map[string]any{"workoutScheduleSummariesScalar": []any{}}})
		f.on("POST /workout-service/schedule/123", status(code, map[string]any{}))
		cs := newSession(t, f, nil)
		text, isErr := callTool(t, cs, "schedule_week", map[string]any{"week": []any{
			map[string]any{"date": "2026-10-20", "workout_id": 123},
		}})
		if isErr {
			t.Fatalf("unexpected error: %q", text)
		}
		entry := mutMap(t, mutList(t, calendarText(t, text)["scheduled"])[0])
		if entry["status"] != "scheduled" {
			t.Fatalf("entry = %v", entry)
		}
	}
}

func TestScheduleWeekKeepsReturnedCalendarEntryId(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST /graphql-gateway/graphql", map[string]any{"data": map[string]any{"workoutScheduleSummariesScalar": []any{}}})
	f.on("POST /workout-service/schedule/123", map[string]any{"workoutScheduleId": 456, "workoutId": 123})
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "schedule_week", map[string]any{"week": []any{
		map[string]any{"date": "2026-10-20", "workout_id": 123},
	}})
	entry := mutMap(t, mutList(t, calendarText(t, text)["scheduled"])[0])
	if entry["status"] != "scheduled" || num(t, entry["scheduled_workout_id"]) != 456 {
		t.Fatalf("entry = %v", entry)
	}
}

func TestScheduleWeekEmptySuccessBodyStillSucceeds(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST /graphql-gateway/graphql", map[string]any{"data": map[string]any{"workoutScheduleSummariesScalar": []any{}}})
	f.on("POST /workout-service/schedule/123", rawBody(nil))
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "schedule_week", map[string]any{"week": []any{
		map[string]any{"date": "2026-10-20", "workout_id": 123},
	}})
	entry := mutMap(t, mutList(t, calendarText(t, text)["scheduled"])[0])
	if entry["status"] != "scheduled" {
		t.Fatalf("entry = %v", entry)
	}
}

func TestScheduleWeekUsesClientPostNotGarth(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST /graphql-gateway/graphql", map[string]any{"data": map[string]any{"workoutScheduleSummariesScalar": []any{}}})
	f.on("POST /workout-service/schedule/1234567890", status(200, nil))
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "schedule_week", map[string]any{"week": []any{
		map[string]any{"date": "2026-05-12", "workout_id": 1234567890},
	}})
	data := calendarText(t, text)
	if data["status"] != "complete" {
		t.Fatalf("data = %v", data)
	}
	entry := mutMap(t, mutList(t, data["scheduled"])[0])
	if entry["status"] != "scheduled" || num(t, entry["workout_id"]) != 1234567890 {
		t.Fatalf("entry = %v", entry)
	}
}

func TestScheduleWeekIsIdempotent(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST /graphql-gateway/graphql", map[string]any{"data": map[string]any{"workoutScheduleSummariesScalar": []any{
		map[string]any{"workoutId": 1234567890, "scheduleDate": "2026-05-12", "workoutName": "Easy Run"},
	}}})
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "schedule_week", map[string]any{"week": []any{
		map[string]any{"date": "2026-05-12", "workout_id": 1234567890},
	}})
	entry := mutMap(t, mutList(t, calendarText(t, text)["scheduled"])[0])
	if entry["status"] != "already_scheduled" || entry["idempotent"] != true {
		t.Fatalf("entry = %v", entry)
	}
	if calls := f.callsMatching("POST", "/workout-service/schedule/1234567890"); len(calls) != 0 {
		t.Fatalf("post calls = %d", len(calls))
	}
}

func TestScheduleWeekPartialIdempotency(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST /graphql-gateway/graphql", sequence{
		map[string]any{"data": map[string]any{"workoutScheduleSummariesScalar": []any{
			map[string]any{"workoutId": 111, "scheduleDate": "2026-05-12"},
		}}},
		map[string]any{"data": map[string]any{"workoutScheduleSummariesScalar": []any{}}},
	})
	f.on("POST /workout-service/schedule/222", status(200, nil))
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "schedule_week", map[string]any{"week": []any{
		map[string]any{"date": "2026-05-12", "workout_id": 111},
		map[string]any{"date": "2026-05-14", "workout_id": 222},
	}})
	entries := mutList(t, calendarText(t, text)["scheduled"])
	if mutMap(t, entries[0])["status"] != "already_scheduled" || mutMap(t, entries[1])["status"] != "scheduled" {
		t.Fatalf("entries = %v", entries)
	}
}

func TestScheduleWeekKeepsResultsWhenAnItemFails(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST /graphql-gateway/graphql", map[string]any{"data": map[string]any{"workoutScheduleSummariesScalar": []any{}}})
	f.on("POST /workout-service/schedule/111", status(200, nil))
	f.on("POST /workout-service/schedule/222", status(404, map[string]any{"message": "API Error 404 - workout not found"}))
	f.on("POST /workout-service/schedule/333", status(200, nil))
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "schedule_week", map[string]any{"week": []any{
		map[string]any{"date": "2026-05-12", "workout_id": 111},
		map[string]any{"date": "2026-05-13", "workout_id": 222},
		map[string]any{"date": "2026-05-14", "workout_id": 333},
	}})
	entries := mutList(t, calendarText(t, text)["scheduled"])
	statuses := []any{mutMap(t, entries[0])["status"], mutMap(t, entries[1])["status"], mutMap(t, entries[2])["status"]}
	sameJSON(t, statuses, []any{"scheduled", "error", "scheduled"})
	if num(t, mutMap(t, entries[1])["workout_id"]) != 222 || !strings.Contains(mutMap(t, entries[1])["message"].(string), "API Error 404") {
		t.Fatalf("second = %v", entries[1])
	}
}

func TestScheduleWeekRejectsInvalidDate(t *testing.T) {
	for _, bad := range []string{"10/14/2026", "2026-02-30"} {
		f := newFakeGarmin(t)
		cs := newSession(t, f, nil)
		text, _ := callTool(t, cs, "schedule_week", map[string]any{"week": []any{
			map[string]any{"date": bad, "workout_id": 111},
		}})
		entry := mutMap(t, mutList(t, calendarText(t, text)["scheduled"])[0])
		if entry["status"] != "failed" || !strings.Contains(entry["message"].(string), "YYYY-MM-DD") {
			t.Fatalf("entry = %v", entry)
		}
		if calls := f.callsMatching("POST", "/workout-service/schedule/111"); len(calls) != 0 {
			t.Fatalf("post calls = %d", len(calls))
		}
	}
}

func TestScheduleWeekMissingDateDoesNotAbortBatch(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST /graphql-gateway/graphql", map[string]any{"data": map[string]any{"workoutScheduleSummariesScalar": []any{}}})
	f.on("POST /workout-service/schedule/222", status(200, nil))
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "schedule_week", map[string]any{"week": []any{
		map[string]any{"workout_id": 111},
		map[string]any{"date": "2026-05-14", "workout_id": 222},
	}})
	entries := mutList(t, calendarText(t, text)["scheduled"])
	if mutMap(t, entries[0])["status"] != "failed" || mutMap(t, entries[1])["status"] != "scheduled" {
		t.Fatalf("entries = %v", entries)
	}
	if calls := f.callsMatching("POST", "/workout-service/schedule/222"); len(calls) != 1 {
		t.Fatalf("post calls = %d", len(calls))
	}
}

// --- integration: create_* ---

func TestCreateRunWorkoutSuccess(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST /workout-service/workout", map[string]any{"workoutId": 9876543210, "workoutName": "Step 8 - 30min continuous"})
	cs := newSession(t, f, nil)
	text, isErr := callTool(t, cs, "create_run_workout", map[string]any{
		"name": "Step 8 - 30min continuous", "run_seconds": 1800, "warmup_min": 5, "cooldown_min": 5, "hr_zone": "Z3",
	})
	if isErr {
		t.Fatalf("unexpected error: %q", text)
	}
	data := calendarText(t, text)
	if data["status"] != "success" || num(t, data["workout_id"]) != 9876543210 {
		t.Fatalf("data = %v", data)
	}
}

func TestCreateRunWorkoutCustomHrRange(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST /workout-service/workout", map[string]any{"workoutId": 1111111111, "workoutName": "Base run - 7/20"})
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "create_run_workout", map[string]any{
		"name": "Base run - 7/20", "run_seconds": 1440, "warmup_min": 5, "cooldown_min": 5,
		"hr_min": 136, "hr_max": 148,
	})
	if calendarText(t, text)["status"] != "success" {
		t.Fatalf("text = %q", text)
	}
	uploaded := workoutUploaded(t, f)
	interval := mutMap(t, mutList(t, mutMap(t, mutList(t, uploaded["workoutSegments"])[0])["workoutSteps"])[1])
	if mutMap(t, interval["targetType"])["workoutTargetTypeKey"] != "heart.rate.zone" ||
		num(t, interval["targetValueOne"]) != 136 || num(t, interval["targetValueTwo"]) != 148 {
		t.Fatalf("interval = %v", interval)
	}
	if _, ok := interval["zoneNumber"]; ok {
		t.Fatal("zoneNumber present")
	}
}

func TestCreateRunWorkoutException(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST /workout-service/workout", status(500, map[string]any{"message": "Upload failed"}))
	cs := newSession(t, f, nil)
	text, isErr := callTool(t, cs, "create_run_workout", map[string]any{
		"name": "Step 8 - 30min continuous", "run_seconds": 1800, "warmup_min": 5, "cooldown_min": 5,
	})
	if !isErr || !strings.Contains(text, "Error creating run workout") || !strings.Contains(text, "Upload failed") {
		t.Fatalf("text = %q isErr=%v", text, isErr)
	}
}

func TestCreateRunIntervalWorkoutSuccess(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST /workout-service/workout", map[string]any{"workoutId": 2222222222, "workoutName": "Threshold 3x6min"})
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "create_run_interval_workout", map[string]any{
		"name": "Threshold 3x6min", "repeats": 3, "rep_seconds": 360, "recovery_seconds": 180,
		"warmup_min": 12, "cooldown_min": 8, "hr_min": 172, "hr_max": 180,
	})
	if calendarText(t, text)["status"] != "success" {
		t.Fatalf("text = %q", text)
	}
	uploaded := workoutUploaded(t, f)
	steps := mutList(t, mutMap(t, mutList(t, uploaded["workoutSegments"])[0])["workoutSteps"])
	if mutMap(t, stepAt(t, steps, 0)["stepType"])["stepTypeKey"] != "warmup" ||
		stepAt(t, steps, 1)["type"] != "RepeatGroupDTO" ||
		num(t, stepAt(t, steps, 1)["numberOfIterations"]) != 3 ||
		mutMap(t, stepAt(t, steps, 2)["stepType"])["stepTypeKey"] != "cooldown" {
		t.Fatalf("steps = %v", steps)
	}
}

func TestCreateRunIntervalWorkoutException(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("POST /workout-service/workout", status(500, map[string]any{"message": "Upload failed"}))
	cs := newSession(t, f, nil)
	text, isErr := callTool(t, cs, "create_run_interval_workout", map[string]any{
		"name": "Threshold 3x6min", "repeats": 3, "rep_seconds": 360, "recovery_seconds": 180,
		"warmup_min": 12, "cooldown_min": 8,
	})
	if !isErr || !strings.Contains(text, "Error creating run interval workout") || !strings.Contains(text, "Upload failed") {
		t.Fatalf("text = %q isErr=%v", text, isErr)
	}
}

func TestCreateRunIntervalWorkoutRejectsSingleRecoveryBound(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	text, _ := callTool(t, cs, "create_run_interval_workout", map[string]any{
		"name": "Threshold 3x6min", "repeats": 3, "rep_seconds": 360, "recovery_seconds": 180,
		"warmup_min": 12, "cooldown_min": 8, "hr_min": 172, "hr_max": 180,
		"recovery_hr_min": 120,
	})
	if !strings.Contains(text, "Error creating run interval workout") ||
		!strings.Contains(text, "recovery_hr_min and recovery_hr_max must both be provided together") {
		t.Fatalf("text = %q", text)
	}
	if calls := f.callsMatching("POST", "/workout-service/workout"); len(calls) != 0 {
		t.Fatalf("upload calls = %d", len(calls))
	}
}

// --- templates / schema contract ---

func TestSwimmingReferenceMatchesCapturedGarminWorkout(t *testing.T) {
	fixtureWorkout := mutMap(t, fixture(t, "MOCK_SWIM_WORKOUT_DETAILS"))
	sport := mutMap(t, fixtureWorkout["sportType"])
	var reference map[string]any
	decodeJSON(t, workoutStructureReferenceBody, &reference)
	sports := mutMap(t, reference["sportType_values"])
	if mutMap(t, sports[formatID(sport["sportTypeId"])])["sportTypeKey"] != sport["sportTypeKey"] {
		t.Fatal("swimming reference mismatch")
	}
}

func formatID(v any) string {
	f, _ := numberOf(v)
	return strconv.FormatInt(int64(f), 10)
}

func TestStrengthResourceDescriptionMatchesRepsBasedGroups(t *testing.T) {
	var resource workoutResource
	for _, r := range workoutResources {
		if r.uri == "workout://templates/strength-circuit" {
			resource = r
		}
	}
	var template map[string]any
	decodeJSON(t, resource.body, &template)
	steps := mutList(t, mutMap(t, mutList(t, template["workoutSegments"])[0])["workoutSteps"])
	groups := steps[1:3]
	if num(t, mutMap(t, groups[0])["numberOfIterations"]) != 3 || num(t, mutMap(t, groups[1])["numberOfIterations"]) != 3 {
		t.Fatalf("groups = %v", groups)
	}
	first := mutMap(t, mutList(t, mutMap(t, groups[0])["workoutSteps"])[0])
	second := mutMap(t, mutList(t, mutMap(t, groups[1])["workoutSteps"])[0])
	if num(t, first["endConditionValue"]) != 10 || num(t, second["endConditionValue"]) != 8 {
		t.Fatalf("reps = %v %v", first["endConditionValue"], second["endConditionValue"])
	}
	if !strings.Contains(resource.description, "10 reps") || !strings.Contains(resource.description, "8 reps") || strings.Contains(resource.description, "10min work") {
		t.Fatalf("description = %q", resource.description)
	}
}

func TestUploadWorkoutSportTypeIdsMatchReference(t *testing.T) {
	parts := strings.SplitN(uploadWorkoutDescription, "Sport type IDs for workouts", 2)
	if len(parts) != 2 {
		t.Fatal("description missing sport type section")
	}
	lines := strings.Split(parts[1], "\n")
	if len(lines) < 2 {
		t.Fatal("description missing sport type line")
	}
	pairs := regexp.MustCompile(`(\d+) = (\w+)`).FindAllStringSubmatch(lines[1], -1)
	if len(pairs) == 0 {
		t.Fatalf("sport line = %q", lines[1])
	}
	var reference map[string]any
	decodeJSON(t, workoutStructureReferenceBody, &reference)
	sports := mutMap(t, reference["sportType_values"])
	foundWalking := false
	for _, pair := range pairs {
		if pair[1] == "12" && pair[2] == "walking" {
			foundWalking = true
		}
		if mutMap(t, sports[pair[1]])["sportTypeKey"] != pair[2] {
			t.Fatalf("%s = %s not in reference", pair[1], pair[2])
		}
	}
	if !foundWalking {
		t.Fatal("walking sport type missing")
	}
}

func TestWorkoutUploadToolsDocumentPaceUnits(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	descriptions := map[string]string{}
	for _, tool := range listToolObjects(t, cs) {
		descriptions[tool.Name] = tool.Description
	}
	for _, name := range []string{"upload_workout", "upload_workouts"} {
		if !strings.Contains(descriptions[name], "3.333 m/s") {
			t.Errorf("%s description missing pace units", name)
		}
	}
	for _, tool := range listToolObjects(t, cs) {
		if tool.Name != "schedule_workouts" {
			continue
		}
		schema, _ := tool.InputSchema.(map[string]any)
		props, _ := schema["properties"].(map[string]any)
		schedules, _ := props["schedules"].(map[string]any)
		if !strings.Contains(fmt.Sprint(schedules["description"]), "3.333 m/s") {
			t.Errorf("schedule_workouts schedules schema missing pace units")
		}
	}
}

func TestParametersDefaultingToNoneAcceptNull(t *testing.T) {
	f := newFakeGarmin(t)
	cs := newSession(t, f, nil)
	for _, tool := range listToolObjects(t, cs) {
		schema, _ := tool.InputSchema.(map[string]any)
		props, _ := schema["properties"].(map[string]any)
		for name, raw := range props {
			prop := mutMap(t, raw)
			if v, ok := prop["default"]; ok && v == nil && !acceptsNull(prop) {
				t.Errorf("%s.%s rejects null default", tool.Name, name)
			}
		}
	}
}

func acceptsNull(schema map[string]any) bool {
	if schema["type"] == "null" {
		return true
	}
	if types, ok := schema["type"].([]any); ok {
		for _, t := range types {
			if t == "null" {
				return true
			}
		}
	}
	if options, ok := schema["anyOf"].([]any); ok {
		for _, option := range options {
			if m, ok := option.(map[string]any); ok && acceptsNull(m) {
				return true
			}
		}
	}
	return false
}

func listToolObjects(t *testing.T, cs *mcp.ClientSession) []*mcp.Tool {
	t.Helper()
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	return res.Tools
}

func TestBuildersRejectNonNumericStrengthFields(t *testing.T) {
	cases := []struct {
		name  string
		field string
		value any
	}{
		{"sets non-numeric", "sets", "three"},
		{"sets null", "sets", nil},
		{"reps non-numeric", "reps", "three"},
		{"rest non-numeric", "rest_seconds", "soon"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGarmin(t)
			cs := newSession(t, f, nil)
			exercise := map[string]any{"name": "Squat", "reps": 5}
			if tc.field == "reps" {
				delete(exercise, "reps")
			}
			exercise[tc.field] = tc.value
			text, _ := callTool(t, cs, "create_strength_workout", map[string]any{
				"name": "Bad strength", "exercises": []any{exercise},
			})
			if !strings.Contains(text, "Error creating strength workout") ||
				!strings.Contains(text, tc.field) || !strings.Contains(text, "must be an integer") {
				t.Fatalf("text = %q", text)
			}
			if calls := f.callsMatching("POST", "/workout-service/workout"); len(calls) != 0 {
				t.Fatalf("upload calls = %d", len(calls))
			}
		})
	}
}
