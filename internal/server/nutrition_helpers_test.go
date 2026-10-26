package server

import (
	"reflect"
	"strings"
	"testing"
)

// Ported from tests/unit/test_nutrition_target_date.py.

func TestValidStoredTargetIsLeftUntouched(t *testing.T) {
	newTarget, errMsg := resolveWeightGoalTargetDate("2030-01-01", "2024-02-01", nil)
	if newTarget != "" || errMsg != "" {
		t.Errorf("got %q %q", newTarget, errMsg)
	}
}

func TestAbsentStoredTargetIsLeftUntouched(t *testing.T) {
	for _, stored := range []any{nil, ""} {
		newTarget, errMsg := resolveWeightGoalTargetDate(stored, "2024-02-01", nil)
		if newTarget != "" || errMsg != "" {
			t.Errorf("stored %v: got %q %q", stored, newTarget, errMsg)
		}
	}
}

func TestPassedTargetIsReportedNotSilentlyChanged(t *testing.T) {
	newTarget, errMsg := resolveWeightGoalTargetDate("2024-03-01", "2024-07-15", nil)
	if newTarget != "" || errMsg == "" {
		t.Fatalf("got %q %q", newTarget, errMsg)
	}
	for _, want := range []string{"targetDate", "2024-03-01", "2024-07-15", "target_date"} {
		if !strings.Contains(errMsg, want) {
			t.Errorf("message lacks %q: %s", want, errMsg)
		}
	}
	const full = "Cannot update nutrition settings for 2024-07-15: the weight goal's targetDate (2024-03-01) is not after 2024-07-15, " +
		"and Garmin rejects the whole settings update in that case.\n" +
		"Either set a new target date on the weight goal in Garmin Connect (or end the goal) and retry, or pass " +
		"target_date=YYYY-MM-DD (after 2024-07-15) to move the weight goal's target date as part of this update."
	if errMsg != full {
		t.Errorf("message = %q", errMsg)
	}
}

func TestTargetEqualToDateIsRejected(t *testing.T) {
	if _, errMsg := resolveWeightGoalTargetDate("2024-07-15", "2024-07-15", nil); errMsg == "" {
		t.Error("equal target accepted")
	}
}

func TestExplicitTargetDateOverridesAPassedOne(t *testing.T) {
	d := "2024-12-31"
	newTarget, errMsg := resolveWeightGoalTargetDate("2024-03-01", "2024-07-15", &d)
	if newTarget != "2024-12-31" || errMsg != "" {
		t.Errorf("got %q %q", newTarget, errMsg)
	}
}

func TestExplicitTargetDateMustBeAfterDate(t *testing.T) {
	for _, c := range []string{"2024-07-15", "2024-07-14"} {
		newTarget, errMsg := resolveWeightGoalTargetDate("2024-03-01", "2024-07-15", &c)
		if newTarget != "" || !strings.Contains(errMsg, "must be after") {
			t.Errorf("%s: got %q %q", c, newTarget, errMsg)
		}
	}
	c := "2024-07-14"
	_, errMsg := resolveWeightGoalTargetDate(nil, "2024-07-15", &c)
	want := "target_date (2024-07-14) must be after date (2024-07-15); Garmin requires the weight goal's target to be later than the day being written."
	if errMsg != want {
		t.Errorf("message = %q", errMsg)
	}
}

func TestExplicitTargetDateMustBeIso(t *testing.T) {
	for _, c := range []string{"31-12-2026", "not-a-date", "2024-13-01"} {
		newTarget, errMsg := resolveWeightGoalTargetDate("2024-03-01", "2024-07-15", &c)
		if newTarget != "" || !strings.Contains(errMsg, "YYYY-MM-DD") {
			t.Errorf("%s: got %q %q", c, newTarget, errMsg)
		}
	}
	bad := "it's"
	if _, errMsg := resolveWeightGoalTargetDate(nil, "2024-07-15", &bad); errMsg != `Invalid target_date "it's". Use YYYY-MM-DD.` {
		t.Errorf("message = %q", errMsg)
	}
}

// ── helpers without a Python test of their own ──────────────────────────────

func TestNumToStr(t *testing.T) {
	for in, want := range map[any]string{
		150.0: "150", 0.0: "0", 0.5: "0.5", 2.5: "2.5", 1e-05: "1e-05", 1.5e-05: "1.5e-05", 0.0001: "0.0001",
		1e16: "10000000000000000", -3.0: "-3", 1500000.5: "1500000.5", "100": "100", true: "1",
	} {
		got, err := numToStr(in)
		if err != nil || got != want {
			t.Errorf("numToStr(%v) = %q, %v; want %q", in, got, err, want)
		}
	}
	for in, want := range map[any]string{
		"abc": "invalid literal for int() with base 10: 'abc'",
		"1.5": "invalid literal for int() with base 10: '1.5'",
		nil:   "int() argument must be a string, a bytes-like object or a real number, not 'NoneType'",
	} {
		if _, err := numToStr(in); err == nil || err.Error() != want {
			t.Errorf("numToStr(%v) err = %v; want %q", in, err, want)
		}
	}
}

func TestFindFoodLogEntry(t *testing.T) {
	item := map[string]any{"logId": "target", "foodName": "Oats"}
	data := map[string]any{"mealDetails": []any{map[string]any{"loggedFoods": []any{map[string]any{"logId": "other"}, item}}}}
	if got := findFoodLogEntry(data, "target"); !reflect.DeepEqual(got, item) {
		t.Errorf("got %v", got)
	}
	if findFoodLogEntry(data, "missing") != nil || findFoodLogEntry(nil, "x") != nil || findFoodLogEntry("s", "x") != nil {
		t.Error("expected no entry")
	}
	if got := findFoodLogEntry(map[string]any{"logId": 99001.0}, "99001"); got == nil {
		t.Error("numeric logId not matched through str()")
	}
	if got := findFoodLogEntry(map[string]any{"logId": nil}, "None"); got != nil {
		t.Error("null logId matched")
	}
}

func TestCustomFoodIDs(t *testing.T) {
	for _, tc := range []struct {
		name          string
		food          any
		foodID, srvID string
	}{
		{"nil", nil, "", ""},
		{"empty", map[string]any{}, "", ""},
		{"meta", map[string]any{"foodMetaData": map[string]any{"foodId": "f1"}, "nutritionContents": []any{map[string]any{"servingId": "s1"}}}, "f1", "s1"},
		{"flat", map[string]any{"foodId": 7.0, "nutritionContents": []any{map[string]any{"servingId": 9.0}}}, "7", "9"},
		{"null contents", map[string]any{"foodMetaData": map[string]any{"foodId": "f1"}, "nutritionContents": nil}, "f1", ""},
		{"null first serving", map[string]any{"foodId": "f", "nutritionContents": []any{nil}}, "f", ""},
		{"meta without id falls back", map[string]any{"foodMetaData": map[string]any{"foodName": "x"}, "foodId": "top"}, "top", ""},
	} {
		f, s, err := customFoodIDs(tc.food)
		if err != nil || f != tc.foodID || s != tc.srvID {
			t.Errorf("%s: got %q %q %v", tc.name, f, s, err)
		}
	}
	if _, _, err := customFoodIDs([]any{1.0}); err == nil || err.Error() != "'list' object has no attribute 'get'" {
		t.Errorf("list err = %v", err)
	}
}

func TestValidateMealDatetime(t *testing.T) {
	if err := validateMealDatetime("2024-01-15", "08:30:00"); err != nil {
		t.Error(err)
	}
	const dateMsg, timeMsg = "meal_date must be a valid date in YYYY-MM-DD format", "meal_time must be a valid time in HH:MM:SS format"
	for _, tc := range [][3]string{
		{"2024-01-15", "8:30:00", timeMsg}, {"2024-01-15", "noon", timeMsg}, {"2024-01-15", "25:00:00", timeMsg},
		{"2024-01-15", "12:30 PM", timeMsg}, {"2024-01-15", "2024-01-15T08:30:00", timeMsg}, {"2024-01-15", "12:60:00", timeMsg},
		{"2024-02-30", "08:30:00", dateMsg}, {"2024-1-5", "08:30:00", dateMsg}, {"2024-01-15\n", "08:30:00", dateMsg},
	} {
		if err := validateMealDatetime(tc[0], tc[1]); err == nil || err.Error() != tc[2] {
			t.Errorf("%v: err = %v", tc[:2], err)
		}
	}
}
