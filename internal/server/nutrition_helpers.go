package server

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Pure helpers of garmin_mcp/nutrition.py.

// numToStr is _num_to_str: whole numbers lose their ".0" ("160", not "160.0"). Strings keep their text when int()
// accepts them (Python compares "100" == 100, which is False, and falls through to str()), otherwise int() raises.
func numToStr(v any) (string, error) {
	switch x := v.(type) {
	case float64:
		if x == math.Trunc(x) {
			n, _ := new(big.Float).SetFloat64(x).Int(nil)
			return n.String(), nil
		}
		if a := math.Abs(x); a < 1e-4 { // Python's repr switches to exponent form below 1e-4
			return strconv.FormatFloat(x, 'e', -1, 64), nil
		}
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	case bool:
		if x {
			return "1", nil
		}
		return "0", nil
	case string:
		if _, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64); err != nil {
			return "", fmt.Errorf("invalid literal for int() with base 10: %s", pyRepr(x))
		}
		return x, nil
	}
	return "", fmt.Errorf("int() argument must be a string, a bytes-like object or a real number, not '%s'", pyTypeName(v))
}

// resolveWeightGoalTargetDate is resolve_weight_goal_target_date. At most one result is non-empty; both empty means
// the stored value is valid and must be left untouched.
func resolveWeightGoalTargetDate(stored any, date string, targetDate *string) (newTarget, errMsg string) {
	if targetDate != nil {
		if _, err := parseISODate(*targetDate); err != nil {
			return "", fmt.Sprintf("Invalid target_date %s. Use YYYY-MM-DD.", pyRepr(*targetDate))
		}
		if *targetDate <= date {
			return "", fmt.Sprintf("target_date (%s) must be after date (%s); Garmin requires the weight goal's target to be later than the day being written.", *targetDate, date)
		}
		return *targetDate, ""
	}
	if !falsy(stored) && pyStrValue(stored) <= date {
		return "", fmt.Sprintf("Cannot update nutrition settings for %s: the weight goal's targetDate (%s) is not after %s, "+
			"and Garmin rejects the whole settings update in that case.\n"+
			"Either set a new target date on the weight goal in Garmin Connect (or end the goal) and retry, "+
			"or pass target_date=YYYY-MM-DD (after %s) to move the weight goal's target date as part of this update.",
			date, pyStrValue(stored), date, date)
	}
	return "", ""
}

var (
	mealDateRe = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	mealTimeRe = regexp.MustCompile(`^([0-9]{2}):([0-9]{2}):([0-9]{2})$`)
)

// validateMealDatetime is _validate_meal_datetime: the local clock fields are checked before meal windows are matched.
func validateMealDatetime(mealDate, mealTime string) error {
	if _, err := parseISODate(mealDate); err != nil || !mealDateRe.MatchString(mealDate) {
		return errors.New("meal_date must be a valid date in YYYY-MM-DD format")
	}
	m := mealTimeRe.FindStringSubmatch(mealTime)
	if m == nil {
		return errors.New("meal_time must be a valid time in HH:MM:SS format")
	}
	for i, max := range []int{23, 59, 59} {
		if n, _ := strconv.Atoi(m[i+1]); n > max {
			return errors.New("meal_time must be a valid time in HH:MM:SS format")
		}
	}
	return nil
}

// findFoodLogEntry is _find_food_log_entry: the dict whose logId (compared as a string) is logID, searched
// depth-first through nested JSON without assuming a meal layout. Keys are visited in sorted order because a
// decoded map has no insertion order.
func findFoodLogEntry(data any, logID string) map[string]any {
	var children []any
	switch x := data.(type) {
	case map[string]any:
		if id, ok := x["logId"]; ok && id != nil && pyStrValue(id) == logID {
			return x
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			children = append(children, x[k])
		}
	case []any:
		children = x
	default:
		return nil
	}
	for _, c := range children {
		if e := findFoodLogEntry(c, logID); e != nil {
			return e
		}
	}
	return nil
}

// customFoodIDs is _custom_food_ids: the food and first-serving IDs (as strings, "" for None) of a search or
// create response.
func customFoodIDs(food any) (foodID, servingID string, err error) {
	if falsy(food) {
		return "", "", nil
	}
	m, ok := food.(map[string]any)
	if !ok {
		return "", "", fmt.Errorf("'%s' object has no attribute 'get'", pyTypeName(food))
	}
	meta := asMap(firstTruthy(m["foodMetaData"], m))
	var serving map[string]any
	if contents := asList(m["nutritionContents"]); len(contents) > 0 {
		serving = asMap(contents[0])
	}
	var fid, sid string
	if id := firstTruthy(meta["foodId"], m["foodId"]); id != nil {
		fid = idString(id)
	}
	if id := serving["servingId"]; id != nil {
		sid = idString(id)
	}
	return fid, sid, nil
}
