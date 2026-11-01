package server

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Ported from tests/unit/test_workout_builders.py.

func ip(n int) *int { return &n }

// rt round-trips a built workout through JSON so numbers are float64 like a decoded request body.
func rt(t *testing.T, w map[string]any, err error) map[string]any {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	b, e := json.Marshal(w)
	if e != nil {
		t.Fatal(e)
	}
	var out map[string]any
	if e := json.Unmarshal(b, &out); e != nil {
		t.Fatal(e)
	}
	return out
}

func stepsOf(w map[string]any) []any {
	return asList(asMap(asList(w["workoutSegments"])[0])["workoutSteps"])
}

func keyOf(step any, field, key string) any { return asMap(asMap(step)[field])[key] }

func wantErr(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), substr) {
		t.Errorf("err = %v, want containing %q", err, substr)
	}
}

func TestBuildWalkRunJsonMatchesPocSnapshot(t *testing.T) {
	w, err := buildWalkRunJSON("POC Walk/Run 7x1m/3m Z3", 60, 180, 7, 10, 8, "Z3")
	result := rt(t, w, err)
	b, e := os.ReadFile("testdata/poc_walk_run.json")
	if e != nil {
		t.Fatal(e)
	}
	var expected map[string]any
	if e := json.Unmarshal(b, &expected); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("got %v\nwant %v", result, expected)
	}
}

func TestBuildZ2WalkJsonStructure(t *testing.T) {
	w, err := buildZ2WalkJSON("Z2 Walk 30m", 30, 110, 130)
	r := rt(t, w, err)
	steps := stepsOf(r)
	if r["workoutName"] != "Z2 Walk 30m" || keyOf(r, "sportType", "sportTypeKey") != "walking" || keyOf(r, "sportType", "sportTypeId") != 12.0 ||
		len(steps) != 3 || asMap(steps[1])["zoneNumber"] != 2.0 || asMap(steps[1])["endConditionValue"] != 1800.0 {
		t.Errorf("r = %v", r)
	}
}

func TestBuildRunJsonStructure(t *testing.T) {
	w, err := buildRunJSON("Step 8 - 30min continuous", 1800, 5, 5, "Z3", nil, nil)
	r := rt(t, w, err)
	steps := stepsOf(r)
	if r["workoutName"] != "Step 8 - 30min continuous" || keyOf(r, "sportType", "sportTypeKey") != "running" || keyOf(r, "sportType", "sportTypeId") != 1.0 || len(steps) != 3 {
		t.Fatalf("r = %v", r)
	}
	if keyOf(steps[0], "stepType", "stepTypeKey") != "warmup" || asMap(steps[0])["endConditionValue"] != 300.0 ||
		keyOf(steps[1], "stepType", "stepTypeKey") != "interval" || asMap(steps[1])["endConditionValue"] != 1800.0 || asMap(steps[1])["zoneNumber"] != 3.0 ||
		keyOf(steps[2], "stepType", "stepTypeKey") != "cooldown" || asMap(steps[2])["endConditionValue"] != 300.0 {
		t.Errorf("steps = %v", steps)
	}
}

func TestBuildRunJsonCustomHrRange(t *testing.T) {
	w, err := buildRunJSON("Base run - custom range", 1440, 5, 5, "Z3", ip(136), ip(148))
	r := rt(t, w, err)
	step := asMap(stepsOf(r)[1])
	_, hasZone := step["zoneNumber"]
	if keyOf(step, "targetType", "workoutTargetTypeKey") != "heart.rate.zone" || step["targetValueOne"] != 136.0 || step["targetValueTwo"] != 148.0 || hasZone ||
		!strings.Contains(r["description"].(string), "136-148bpm") {
		t.Errorf("r = %v", r)
	}
}

func TestBuildRunJsonCustomHrRangeRequiresBothBounds(t *testing.T) {
	_, err := buildRunJSON("Bad range", 1440, 5, 5, "Z3", ip(136), nil)
	wantErr(t, err, "hr_min and hr_max must both be provided together")
}

func TestBuildRunJsonCustomHrRangeRejectsInvertedBounds(t *testing.T) {
	_, err := buildRunJSON("Bad range", 1440, 5, 5, "Z3", ip(148), ip(136))
	wantErr(t, err, "must be less than")
}

func TestBuildRunIntervalJsonStructure(t *testing.T) {
	w, err := buildRunIntervalJSON("VO2 5x3min", 5, 180, 120, 10, 8, "Z5", nil, nil, nil, nil)
	r := rt(t, w, err)
	steps := stepsOf(r)
	if r["workoutName"] != "VO2 5x3min" || keyOf(r, "sportType", "sportTypeKey") != "running" || len(steps) != 3 {
		t.Fatalf("r = %v", r)
	}
	warmup, group, cooldown := asMap(steps[0]), asMap(steps[1]), asMap(steps[2])
	if keyOf(warmup, "stepType", "stepTypeKey") != "warmup" || warmup["endConditionValue"] != 600.0 {
		t.Errorf("warmup = %v", warmup)
	}
	// The group occupies stepOrder 2 and its children continue the numbering.
	kids := asList(group["workoutSteps"])
	if group["type"] != "RepeatGroupDTO" || group["stepOrder"] != 2.0 || group["numberOfIterations"] != 5.0 ||
		keyOf(group, "endCondition", "conditionTypeKey") != "iterations" || group["endConditionValue"] != 5.0 || len(kids) != 2 {
		t.Fatalf("group = %v", group)
	}
	work, recovery := asMap(kids[0]), asMap(kids[1])
	if work["stepOrder"] != 3.0 || keyOf(work, "stepType", "stepTypeKey") != "interval" || work["endConditionValue"] != 180.0 || work["zoneNumber"] != 5.0 ||
		recovery["stepOrder"] != 4.0 || keyOf(recovery, "stepType", "stepTypeKey") != "recovery" || recovery["endConditionValue"] != 120.0 ||
		keyOf(recovery, "targetType", "workoutTargetTypeKey") != "no.target" {
		t.Errorf("kids = %v", kids)
	}
	if cooldown["stepOrder"] != 5.0 || keyOf(cooldown, "stepType", "stepTypeKey") != "cooldown" || cooldown["endConditionValue"] != 480.0 ||
		!strings.Contains(r["description"].(string), "5x(3m Z5 / 2m jog)") {
		t.Errorf("cooldown = %v, desc = %v", cooldown, r["description"])
	}
}

func TestBuildRunIntervalJsonCustomHrRange(t *testing.T) {
	w, err := buildRunIntervalJSON("Threshold 3x6min", 3, 360, 180, 12, 8, "Z4", ip(172), ip(180), nil, nil)
	r := rt(t, w, err)
	work := asMap(asList(asMap(stepsOf(r)[1])["workoutSteps"])[0])
	_, hasZone := work["zoneNumber"]
	if keyOf(work, "targetType", "workoutTargetTypeKey") != "heart.rate.zone" || work["targetValueOne"] != 172.0 || work["targetValueTwo"] != 180.0 || hasZone ||
		!strings.Contains(r["description"].(string), "172-180bpm") {
		t.Errorf("work = %v", work)
	}
}

func TestBuildRunIntervalJsonRecoveryHrTarget(t *testing.T) {
	w, err := buildRunIntervalJSON("Controlled recoveries", 4, 240, 120, 10, 5, "Z4", ip(165), ip(175), ip(120), ip(140))
	r := rt(t, w, err)
	rec := asMap(asList(asMap(stepsOf(r)[1])["workoutSteps"])[1])
	if keyOf(rec, "targetType", "workoutTargetTypeKey") != "heart.rate.zone" || rec["targetValueOne"] != 120.0 || rec["targetValueTwo"] != 140.0 ||
		!strings.Contains(rec["description"].(string), "120-140bpm") {
		t.Errorf("rec = %v", rec)
	}
}

func TestBuildRunIntervalJsonRecoveryHrRequiresBothBounds(t *testing.T) {
	for _, b := range [][2]*int{{ip(120), nil}, {nil, ip(140)}} {
		_, err := buildRunIntervalJSON("Half a recovery range", 4, 240, 120, 10, 5, "Z4", nil, nil, b[0], b[1])
		wantErr(t, err, "recovery_hr_min and recovery_hr_max must both be provided together")
	}
}

func TestBuildRunIntervalJsonRecoveryHrRejectsInvertedBounds(t *testing.T) {
	_, err := buildRunIntervalJSON("Inverted recovery range", 4, 240, 120, 10, 5, "Z4", nil, nil, ip(140), ip(120))
	wantErr(t, err, "recovery_hr_min (140) must be less than recovery_hr_max (120)")
}

func TestBuildRunIntervalJsonZeroRecoveryOmitsStep(t *testing.T) {
	w, err := buildRunIntervalJSON("Strides", 6, 20, 0, 10, 5, "Z4", nil, nil, nil, nil)
	r := rt(t, w, err)
	steps := stepsOf(r)
	group, cooldown := asMap(steps[1]), asMap(steps[2])
	kids := asList(group["workoutSteps"])
	// Cooldown numbering shifts up when there is no recovery child.
	if len(kids) != 1 || keyOf(kids[0], "stepType", "stepTypeKey") != "interval" || cooldown["stepOrder"] != 4.0 || strings.Contains(r["description"].(string), "jog") {
		t.Errorf("r = %v", r)
	}
}

func TestBuildRunIntervalJsonRejectsBadInputs(t *testing.T) {
	_, err := buildRunIntervalJSON("Bad", 0, 60, 60, 10, 5, "Z4", nil, nil, nil, nil)
	wantErr(t, err, "repeats")
	_, err = buildRunIntervalJSON("Bad", 3, 0, 60, 10, 5, "Z4", nil, nil, nil, nil)
	wantErr(t, err, "rep_seconds")
	_, err = buildRunIntervalJSON("Bad", 3, 60, -1, 10, 5, "Z4", nil, nil, nil, nil)
	wantErr(t, err, "recovery_seconds")
}

func ex(name string, sets, reps, rest int) map[string]any {
	return map[string]any{"name": name, "sets": float64(sets), "reps": float64(reps), "rest_seconds": float64(rest)}
}

func strength(t *testing.T, name string, exercises ...map[string]any) map[string]any {
	t.Helper()
	w, err := buildStrengthJSON(name, exercises)
	return rt(t, w, err)
}

func types(steps []any) []any {
	out := []any{}
	for _, s := range steps {
		out = append(out, asMap(s)["type"])
	}
	return out
}

func TestBuildStrengthJsonStructure(t *testing.T) {
	r := strength(t, "Full Body A", ex("Sentadillas", 3, 12, 90), ex("Flexiones", 3, 15, 60))
	steps := stepsOf(r)
	// Multi-set exercises each become one repeat group; rest lives inside the group.
	if r["workoutName"] != "Full Body A" || keyOf(r, "sportType", "sportTypeKey") != "strength_training" || keyOf(r, "sportType", "sportTypeId") != 5.0 ||
		!reflect.DeepEqual(types(steps), []any{"RepeatGroupDTO", "RepeatGroupDTO"}) || asMap(steps[0])["numberOfIterations"] != 3.0 ||
		asMap(asList(asMap(steps[0])["workoutSteps"])[0])["exerciseName"] != "Sentadillas" ||
		asMap(asList(asMap(steps[1])["workoutSteps"])[0])["exerciseName"] != "Flexiones" {
		t.Errorf("r = %v", r)
	}
}

func TestStrengthSingleSetStaysFlat(t *testing.T) {
	steps := stepsOf(strength(t, "Singles", ex("Plank Hold", 1, 1, 60), ex("Dead Hang", 1, 1, 0)))
	if !reflect.DeepEqual(types(steps), []any{"ExecutableStepDTO", "ExecutableStepDTO", "ExecutableStepDTO"}) ||
		asMap(steps[0])["exerciseName"] != "Plank Hold" || keyOf(steps[1], "stepType", "stepTypeKey") != "recovery" {
		t.Errorf("steps = %v", steps)
	}
}

func TestStrengthMultiSetRepeatGroupShape(t *testing.T) {
	steps := stepsOf(strength(t, "Sets", ex("Goblet Squat", 4, 8, 90)))
	if len(steps) != 1 {
		t.Fatalf("steps = %v", steps)
	}
	group := asMap(steps[0])
	kids := asList(group["workoutSteps"])
	if len(kids) != 2 {
		t.Fatalf("group = %v", group)
	}
	work, rest := asMap(kids[0]), asMap(kids[1])
	if group["type"] != "RepeatGroupDTO" || !reflect.DeepEqual(group["stepType"], map[string]any{"stepTypeId": 6.0, "stepTypeKey": "repeat"}) ||
		group["numberOfIterations"] != 4.0 || keyOf(group, "endCondition", "conditionTypeKey") != "iterations" ||
		keyOf(work, "endCondition", "conditionTypeKey") != "reps" || work["endConditionValue"] != 8.0 ||
		!strings.Contains(work["description"].(string), "4 sets x 8 reps") ||
		keyOf(rest, "stepType", "stepTypeKey") != "recovery" || rest["endConditionValue"] != 90.0 {
		t.Errorf("group = %v", group)
	}
}

func TestStrengthSingleSetIdenticalExercisesKeepMiddleRest(t *testing.T) {
	steps := stepsOf(strength(t, "Duplicates", ex("Plank Hold", 1, 1, 45), ex("Plank Hold", 1, 1, 45)))
	if !reflect.DeepEqual(types(steps), []any{"ExecutableStepDTO", "ExecutableStepDTO", "ExecutableStepDTO"}) || keyOf(steps[1], "stepType", "stepTypeKey") != "recovery" {
		t.Errorf("steps = %v", steps)
	}
}

func TestStrengthMultiSetWithoutRestHasNoRecoveryStep(t *testing.T) {
	steps := stepsOf(strength(t, "No rest", ex("Push Up", 3, 12, 0)))
	group := asMap(steps[0])
	kids := asList(group["workoutSteps"])
	if len(steps) != 1 || group["numberOfIterations"] != 3.0 || len(kids) != 1 || keyOf(kids[0], "stepType", "stepTypeKey") != "interval" {
		t.Errorf("steps = %v", steps)
	}
}

// workSteps are the exercise steps; multi-set exercises nest theirs inside a repeat group.
func workSteps(w map[string]any) []map[string]any {
	var flat []map[string]any
	for _, s := range stepsOf(w) {
		kids := asList(asMap(s)["workoutSteps"])
		if kids == nil {
			kids = []any{s}
		}
		for _, c := range kids {
			if keyOf(c, "stepType", "stepTypeKey") == "interval" {
				flat = append(flat, asMap(c))
			}
		}
	}
	return flat
}

func TestStrengthOmitsCategoryWhenNotSupplied(t *testing.T) {
	r := strength(t, "No categories", ex("Back Squat", 3, 5, 120), ex("Zercher Whatever", 3, 8, 60))
	ws := workSteps(r)
	for _, s := range ws {
		if _, has := s["category"]; has {
			t.Errorf("category present: %v", s)
		}
	}
	if !strings.HasPrefix(ws[0]["description"].(string), "Back Squat:") {
		t.Errorf("ws = %v", ws[0])
	}
}

func TestStrengthPassesThroughSuppliedCategory(t *testing.T) {
	r := strength(t, "Mixed",
		map[string]any{"name": "Farmers Carry 40m", "sets": 3.0, "reps": 1.0, "category": "carry"},
		map[string]any{"name": "Back Squat", "sets": 3.0, "reps": 5.0})
	ws := workSteps(r)
	_, has := ws[1]["category"]
	if ws[0]["category"] != "CARRY" || ws[0]["exerciseName"] != "Farmers Carry 40m" || has {
		t.Errorf("ws = %v", ws)
	}
}

func TestStrengthRejectsEmptyCategory(t *testing.T) {
	for _, bad := range []any{"", "   ", 5.0} {
		_, err := buildStrengthJSON("Bad", []map[string]any{{"name": "Back Squat", "sets": 1.0, "reps": 1.0, "category": bad}})
		if err == nil {
			t.Errorf("category %v accepted", bad)
		}
	}
}

func walkSteps(steps []any, out *[]map[string]any) {
	for _, s := range steps {
		*out = append(*out, asMap(s))
		walkSteps(asList(asMap(s)["workoutSteps"]), out)
	}
}

func stepOrders(w map[string]any) []any {
	var all []map[string]any
	for _, seg := range asList(w["workoutSegments"]) {
		walkSteps(asList(asMap(seg)["workoutSteps"]), &all)
	}
	out := []any{}
	for _, s := range all {
		out = append(out, s["stepOrder"])
	}
	return out
}

func assertDepthFirstNumbering(t *testing.T, w map[string]any) {
	t.Helper()
	orders := stepOrders(w)
	want := []any{}
	for i := range orders {
		want = append(want, float64(i+1))
	}
	if !reflect.DeepEqual(orders, want) {
		t.Errorf("orders = %v", orders)
	}
	var all []map[string]any
	for _, seg := range asList(w["workoutSegments"]) {
		walkSteps(asList(asMap(seg)["workoutSteps"]), &all)
	}
	for _, g := range all {
		if g["type"] != "RepeatGroupDTO" {
			continue
		}
		if !reflect.DeepEqual(g["stepType"], map[string]any{"stepTypeId": 6.0, "stepTypeKey": "repeat"}) ||
			!reflect.DeepEqual(g["endCondition"], map[string]any{"conditionTypeId": 7.0, "conditionTypeKey": "iterations"}) ||
			g["endConditionValue"] != g["numberOfIterations"] {
			t.Errorf("group = %v", g)
		}
	}
}

func TestBuildWalkRunJsonRepeatGroupShape(t *testing.T) {
	w, err := buildWalkRunJSON("2:2 x5", 120, 120, 5, 10, 8, "Z3")
	r := rt(t, w, err)
	steps := stepsOf(r)
	warmup, group, cooldown := asMap(steps[0]), asMap(steps[1]), asMap(steps[2])
	kids := asList(group["workoutSteps"])
	run, walk := asMap(kids[0]), asMap(kids[1])
	if warmup["stepOrder"] != 1.0 || group["stepOrder"] != 2.0 || run["stepOrder"] != 3.0 || walk["stepOrder"] != 4.0 || cooldown["stepOrder"] != 5.0 ||
		group["numberOfIterations"] != 5.0 || group["endConditionValue"] != 5.0 || group["smartRepeat"] != false {
		t.Errorf("r = %v", r)
	}
	assertDepthFirstNumbering(t, r)
}

func TestBuildStrengthJsonNumbersStepsDepthFirst(t *testing.T) {
	r := strength(t, "Mixed", ex("Squat", 3, 5, 90), ex("Plank", 1, 1, 60), ex("Push Up", 2, 12, 0))
	steps := stepsOf(r)
	// group 1 (squat 2, rest 3), plank 4, rest 5, group 6 (push-up 7)
	if !reflect.DeepEqual(types(steps), []any{"RepeatGroupDTO", "ExecutableStepDTO", "ExecutableStepDTO", "RepeatGroupDTO"}) ||
		!reflect.DeepEqual(stepOrders(r), []any{1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0}) ||
		asMap(steps[0])["endConditionValue"] != 3.0 || asMap(steps[3])["endConditionValue"] != 2.0 {
		t.Errorf("r = %v", r)
	}
	assertDepthFirstNumbering(t, r)
}

func TestBuildStrengthJsonTwoMultiSetExercisesHaveDistinctOrders(t *testing.T) {
	r := strength(t, "Two groups", ex("A", 3, 10, 60), ex("B", 3, 10, 60))
	if !reflect.DeepEqual(stepOrders(r), []any{1.0, 2.0, 3.0, 4.0, 5.0, 6.0}) {
		t.Errorf("orders = %v", stepOrders(r))
	}
}

func TestEveryBuilderAndTemplateNumbersStepsDepthFirst(t *testing.T) {
	built := map[string]func() (map[string]any, error){
		"walk_run": func() (map[string]any, error) { return buildWalkRunJSON("w", 60, 60, 5, 5, 5, "Z3") },
		"z2_walk":  func() (map[string]any, error) { return buildZ2WalkJSON("z", 30, 110, 130) },
		"run":      func() (map[string]any, error) { return buildRunJSON("r", 1800, 5, 5, "Z3", nil, nil) },
		"run_interval": func() (map[string]any, error) {
			return buildRunIntervalJSON("i", 5, 180, 120, 10, 8, "Z4", nil, nil, nil, nil)
		},
		"run_interval_no_recovery": func() (map[string]any, error) {
			return buildRunIntervalJSON("strides", 6, 20, 0, 10, 5, "Z4", nil, nil, nil, nil)
		},
		"strength": func() (map[string]any, error) {
			return buildStrengthJSON("s", []map[string]any{ex("A", 3, 10, 60), ex("B", 1, 10, 60), ex("C", 2, 10, 0)})
		},
	}
	for name, f := range built {
		t.Run(name, func(t *testing.T) {
			w, err := f()
			assertDepthFirstNumbering(t, rt(t, w, err))
		})
	}
	for _, tmpl := range []string{"get_simple_run_template", "get_interval_template", "get_tempo_template", "get_strength_template"} {
		t.Run(tmpl, func(t *testing.T) {
			for _, r := range workoutResources {
				if r.Name == tmpl {
					var w map[string]any
					if err := json.Unmarshal([]byte(r.Text), &w); err != nil {
						t.Fatal(err)
					}
					assertDepthFirstNumbering(t, w)
					return
				}
			}
			t.Fatalf("no template %s", tmpl)
		})
	}
}
