package server

import (
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Port of the workout step repair and validation helpers of garmin_mcp/workouts.py. The workout builders (and
// schedule_week) reuse them: prepareWorkoutForUpload for inline workouts and validateWorkoutDate for dates.

var endConditionTypeIDs = map[string]int64{
	"lap.button": 1, "time": 2, "distance": 3, "calories": 4, "power": 5, "heart.rate": 6,
	"iterations": 7, "fixed.rest": 8, "fixed.repetition": 9, "reps": 10, "training.peaks.tss": 11,
}

var endConditionTypeKeys = func() map[int64]string {
	m := make(map[int64]string, len(endConditionTypeIDs))
	for k, id := range endConditionTypeIDs {
		m[id] = k
	}
	return m
}()

// knownTargetTypeIDs maps a workoutTargetTypeId to its canonical keys (sorted). Unknown ids are allowed. Id 6 is
// sport dependent: pace.zone for running/swimming, power.between for cycling.
var knownTargetTypeIDs = map[int64][]string{
	1: {"no.target"},
	2: {"power.zone"},
	4: {"heart.rate.zone"},
	6: {"pace.zone", "power.between"},
}

var knownTargetTypeKeys = func() map[string]int64 {
	m := map[string]int64{}
	for id, keys := range knownTargetTypeIDs {
		for _, k := range keys {
			m[k] = id
		}
	}
	return m
}()

var targetFieldLayouts = []struct {
	field  string
	bounds [2]string
	zone   string
}{
	{"targetType", [2]string{"targetValueOne", "targetValueTwo"}, "zoneNumber"},
	{"secondaryTargetType", [2]string{"secondaryTargetValueOne", "secondaryTargetValueTwo"}, "secondaryZoneNumber"},
}

var workoutDateRe = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

// validateWorkoutDate is workouts._validate_date: a real YYYY-MM-DD string, nothing else (no padding, no other ISO forms).
func validateWorkoutDate(v any, field string) (string, error) {
	if s, ok := v.(string); ok && workoutDateRe.MatchString(s) {
		if _, err := parseISODate(s); err == nil {
			return s, nil
		}
	}
	return "", fmt.Errorf("Invalid %s %s: expected YYYY-MM-DD", field, pyReprValue(v))
}

// prepareWorkoutForUpload repairs, then validates, a workout DTO in place (the three calls every upload path makes).
func prepareWorkoutForUpload(w map[string]any) error {
	if err := normalizeWorkoutSteps(w); err != nil {
		return err
	}
	if err := validateEndConditionSteps(w); err != nil {
		return err
	}
	return validateTargetTypeSteps(w)
}

type stepAt struct {
	step map[string]any
	path string
}

// listField is dict.get(key, []) for a field that must be a list: absent is empty, a present non-list is not ok.
func listField(m map[string]any, key string) ([]any, bool) {
	v, present := m[key]
	if !present {
		return nil, true
	}
	l, ok := v.([]any)
	return l, ok
}

// iterStepTree collects a step and every nested step with its request path (_iter_step_tree).
func iterStepTree(step any, path string, out *[]stepAt) error {
	m, ok := step.(map[string]any)
	if !ok {
		return fmt.Errorf("%s must be an object", path)
	}
	*out = append(*out, stepAt{m, path})
	nested, ok := listField(m, "workoutSteps")
	if !ok {
		return fmt.Errorf("%s.workoutSteps must be a list", path)
	}
	for i, n := range nested {
		if err := iterStepTree(n, fmt.Sprintf("%s.workoutSteps[%d]", path, i), out); err != nil {
			return err
		}
	}
	return nil
}

// iterWorkoutSteps collects every step of the workout with a stable path (_iter_workout_steps).
func iterWorkoutSteps(w map[string]any) ([]stepAt, error) {
	segments, ok := listField(w, "workoutSegments")
	if !ok {
		return nil, fmt.Errorf("workoutSegments must be a list")
	}
	var out []stepAt
	for si, seg := range segments {
		segPath := fmt.Sprintf("workoutSegments[%d]", si)
		sm, ok := seg.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s must be an object", segPath)
		}
		steps, ok := listField(sm, "workoutSteps")
		if !ok {
			return nil, fmt.Errorf("%s.workoutSteps must be a list", segPath)
		}
		for i, st := range steps {
			if err := iterStepTree(st, fmt.Sprintf("%s.workoutSteps[%d]", segPath, i), &out); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// topLevelSteps lists the maps of segments[].workoutSteps (only called after iterWorkoutSteps accepted the shape).
func topLevelSteps(w map[string]any) []map[string]any {
	var out []map[string]any
	segments, _ := listField(w, "workoutSegments")
	for _, seg := range segments {
		sm, _ := seg.(map[string]any)
		steps, _ := listField(sm, "workoutSteps")
		for _, st := range steps {
			if m, ok := st.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}

// validateNestedTargetFields rejects conflicting or ambiguous target fields before any repair.
func validateNestedTargetFields(step map[string]any, path string) error {
	for _, l := range targetFieldLayouts {
		tt, ok := step[l.field].(map[string]any)
		if !ok {
			continue
		}
		fields := []string{l.bounds[0], l.bounds[1], l.zone}
		for _, f := range fields {
			nested := tt[f]
			if nested != nil && step[f] != nil && !reflect.DeepEqual(step[f], nested) {
				return fmt.Errorf("%s.%s=%s conflicts with %s.%s.%s=%s; keep only the step-level %s",
					path, f, pyReprValue(step[f]), path, l.field, f, pyReprValue(nested), f)
			}
		}
		zone := step[l.zone]
		if zone == nil {
			zone = tt[l.zone]
		}
		var bounds []string
		for _, f := range l.bounds {
			v := step[f]
			if v == nil {
				v = tt[f]
			}
			if v != nil {
				bounds = append(bounds, f+"="+pyReprValue(v))
			}
		}
		if zone != nil && len(bounds) > 0 {
			return fmt.Errorf("%s mixes %s=%s with custom range fields (%s); use either a named zone or a custom range",
				path, l.zone, pyReprValue(zone), strings.Join(bounds, ", "))
		}
	}
	return nil
}

// moveNestedTargetFields moves target fields found inside targetType to the step level, where Garmin reads them.
func moveNestedTargetFields(step map[string]any) {
	for _, l := range targetFieldLayouts {
		tt, ok := step[l.field].(map[string]any)
		if !ok {
			continue
		}
		for _, f := range []string{l.bounds[0], l.bounds[1], l.zone} {
			value, has := tt[f]
			if !has {
				continue
			}
			delete(tt, f)
			if value != nil && step[f] == nil {
				step[f] = value
			}
		}
	}
}

// fixHRZoneStep turns a small integer targetValueOne of a heart.rate.zone target into zoneNumber, recursively.
func fixHRZoneStep(step map[string]any) {
	key := ""
	if tt, ok := step["targetType"].(map[string]any); ok {
		key, _ = tt["workoutTargetTypeKey"].(string)
	}
	if key == "heart.rate.zone" && step["zoneNumber"] == nil {
		if z, err := pyFloat(step["targetValueOne"]); err == nil && z == math.Trunc(z) && z >= 1 && z <= 5 {
			step["zoneNumber"] = z
			delete(step, "targetValueOne")
			delete(step, "targetValueTwo")
		}
	}
	nested, _ := step["workoutSteps"].([]any)
	for _, n := range nested {
		if m, ok := n.(map[string]any); ok {
			fixHRZoneStep(m)
		}
	}
}

// fixRepeatGroupStep gives RepeatGroupDTO steps the conditionTypeId and numberOfIterations Garmin needs, recursively.
func fixRepeatGroupStep(step map[string]any) error {
	nested, _ := step["workoutSteps"].([]any)
	if step["type"] != "RepeatGroupDTO" {
		for _, n := range nested {
			if m, ok := n.(map[string]any); ok {
				if err := fixRepeatGroupStep(m); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if ec, ok := step["endCondition"].(map[string]any); ok {
		if _, has := ec["conditionTypeId"]; ec["conditionTypeKey"] == "iterations" && !has {
			ec["conditionTypeId"] = float64(7)
		}
	}
	if step["numberOfIterations"] == nil {
		if value := step["endConditionValue"]; value != nil {
			n, err := pyInt(value)
			if err != nil {
				return err
			}
			step["numberOfIterations"] = float64(n)
		}
	}
	for _, n := range nested {
		if m, ok := n.(map[string]any); ok {
			if err := fixRepeatGroupStep(m); err != nil {
				return err
			}
		}
	}
	return nil
}

// normalizeWorkoutSteps repairs recoverable step-shape mistakes before validation and upload. The whole workout is
// checked first, so a conflict in a later step cannot leave an earlier step half repaired.
func normalizeWorkoutSteps(w map[string]any) error {
	steps, err := iterWorkoutSteps(w)
	if err != nil {
		return err
	}
	for _, s := range steps {
		if err := validateNestedTargetFields(s.step, s.path); err != nil {
			return err
		}
	}
	for _, s := range steps {
		moveNestedTargetFields(s.step)
	}
	// The helpers recurse, so they run on top-level steps only.
	for _, step := range topLevelSteps(w) {
		fixHRZoneStep(step)
		if err := fixRepeatGroupStep(step); err != nil {
			return err
		}
	}
	return nil
}

// validateEndConditionStep rejects endCondition id/key pairs Garmin would silently reinterpret.
func validateEndConditionStep(step map[string]any, path string) error {
	if ec, ok := step["endCondition"].(map[string]any); ok {
		key, id := ec["conditionTypeKey"], ec["conditionTypeId"]
		if s, isStr := id.(string); isStr {
			n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
			if err != nil {
				return fmt.Errorf("%s.endCondition.conditionTypeId must be numeric", path)
			}
			id = float64(n)
			ec["conditionTypeId"] = id
		}
		keyStr, _ := key.(string)
		expectedID, haveID := endConditionTypeIDs[keyStr]
		expectedKey, haveKey := "", false
		if f, ok := id.(float64); ok && f == math.Trunc(f) {
			expectedKey, haveKey = endConditionTypeKeys[int64(f)]
		}
		switch {
		case haveID:
			if id == nil {
				return fmt.Errorf("%s.endCondition conditionTypeKey '%s' requires conditionTypeId %d", path, keyStr, expectedID)
			}
			if f, ok := id.(float64); !ok || f != float64(expectedID) {
				actual := "unknown"
				if haveKey {
					actual = expectedKey
				}
				return fmt.Errorf("%s.endCondition conditionTypeKey '%s' requires conditionTypeId %d, got %s (%s)",
					path, keyStr, expectedID, pyStrValue(id), actual)
			}
		case haveKey && key != nil:
			return fmt.Errorf("%s.endCondition conditionTypeId %s requires conditionTypeKey '%s', got '%s'",
				path, pyStrValue(id), expectedKey, pyStrValue(key))
		}
	}
	nested, _ := step["workoutSteps"].([]any)
	for i, n := range nested {
		if m, ok := n.(map[string]any); ok {
			if err := validateEndConditionStep(m, fmt.Sprintf("%s.workoutSteps[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateEndConditionSteps validates every step's endCondition block before upload.
func validateEndConditionSteps(w map[string]any) error {
	return walkTopLevel(w, validateEndConditionStep)
}

// validateTargetTypeBlock rejects a target type id/key pair Garmin would silently reinterpret.
func validateTargetTypeBlock(step map[string]any, path, field string) error {
	tt, ok := step[field].(map[string]any)
	if !ok {
		return nil
	}
	key := tt["workoutTargetTypeKey"]
	keyStr, keyIsStr := key.(string)
	var id int64
	haveID := false
	if raw := tt["workoutTargetTypeId"]; raw != nil {
		n, err := pyInt(raw)
		if err != nil {
			return fmt.Errorf("%s.%s.workoutTargetTypeId must be numeric", path, field)
		}
		id, haveID = n, true
		tt["workoutTargetTypeId"] = float64(n)
	}
	if valid, known := knownTargetTypeIDs[id]; haveID && known && key != nil && !(keyIsStr && contains(valid, keyStr)) {
		if len(valid) == 1 {
			return fmt.Errorf("%s.%s mismatch: workoutTargetTypeId %d is %s, not %s", path, field, id, pyRepr(valid[0]), pyReprValue(key))
		}
		reprs := make([]string, len(valid))
		for i, k := range valid {
			reprs[i] = pyRepr(k)
		}
		sort.Strings(reprs)
		return fmt.Errorf("%s.%s mismatch: workoutTargetTypeId %d is one of (%s), not %s", path, field, id, strings.Join(reprs, ", "), pyReprValue(key))
	}
	if expected, known := knownTargetTypeKeys[keyStr]; keyIsStr && known && haveID && id != expected {
		return fmt.Errorf("%s.%s mismatch: workoutTargetTypeKey %s requires workoutTargetTypeId %d, not %d", path, field, pyReprValue(key), expected, id)
	}
	return nil
}

func validateTargetTypeStep(step map[string]any, path string) error {
	if err := validateTargetTypeBlock(step, path, "targetType"); err != nil {
		return err
	}
	if err := validateTargetTypeBlock(step, path, "secondaryTargetType"); err != nil {
		return err
	}
	nested, _ := step["workoutSteps"].([]any)
	for i, n := range nested {
		if m, ok := n.(map[string]any); ok {
			if err := validateTargetTypeStep(m, fmt.Sprintf("%s.workoutSteps[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateTargetTypeSteps walks all workout steps and validates the known targetType id/key pairs.
func validateTargetTypeSteps(w map[string]any) error {
	return walkTopLevel(w, validateTargetTypeStep)
}

// walkTopLevel calls fn on each top-level step with its path (workoutSegments[i].workoutSteps[j]).
func walkTopLevel(w map[string]any, fn func(step map[string]any, path string) error) error {
	segments, _ := listField(w, "workoutSegments")
	for si, seg := range segments {
		sm, _ := seg.(map[string]any)
		steps, _ := listField(sm, "workoutSteps")
		for i, st := range steps {
			if m, ok := st.(map[string]any); ok {
				if err := fn(m, fmt.Sprintf("workoutSegments[%d].workoutSteps[%d]", si, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// pyInt is Python's int(v) for a decoded JSON value, with Python's error texts.
func pyInt(v any) (int64, error) {
	switch x := v.(type) {
	case float64:
		return int64(x), nil
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid literal for int() with base 10: %s", pyRepr(x))
		}
		return n, nil
	}
	return 0, fmt.Errorf("int() argument must be a string, a bytes-like object or a real number, not '%s'", pyTypeName(v))
}

// pyTypeName is type(v).__name__ for a decoded JSON value (whole numbers read as int).
func pyTypeName(v any) string {
	switch x := v.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case float64:
		if x == math.Trunc(x) {
			return "int"
		}
		return "float"
	case string:
		return "str"
	case []any:
		return "list"
	}
	return "dict"
}

// pyStrValue is str(v): a string as-is, anything else as its repr.
func pyStrValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return pyReprValue(v)
}

// pyReprValue is repr(v) for a decoded JSON value. JSON does not keep 1 and 1.0 apart, so whole numbers print as ints;
// dict keys print sorted because the decoded map has no order.
func pyReprValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case bool:
		if x {
			return "True"
		}
		return "False"
	case string:
		return pyRepr(x)
	case float64:
		switch {
		case x == math.Trunc(x) && math.Abs(x) < 1e16:
			return strconv.FormatInt(int64(x), 10)
		case math.Abs(x) >= 1e16 || math.Abs(x) < 1e-4:
			return strconv.FormatFloat(x, 'g', -1, 64)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = pyReprValue(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = pyRepr(k) + ": " + pyReprValue(x[k])
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return fmt.Sprint(v)
}
