package server

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// Safe accessors for decoded Garmin JSON (any / map[string]any / []any / float64). They never panic:
// a value of the wrong shape reads as missing, which is what Python's `.get(...)`/`or {}` idioms give on None.

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

// asNumber returns v as a float64 when it is a JSON number (booleans are not numbers, as in the Python checks).
func asNumber(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

// firstTruthy is Python's `a or b`.
func firstTruthy(a, b any) any {
	if falsy(a) {
		return b
	}
	return a
}

// getDefault is Python's dict.get(key, def): def applies only when the key is absent (a null value stays null).
func getDefault(m map[string]any, key string, def any) any {
	if v, ok := m[key]; ok {
		return v
	}
	return def
}

// pick builds {out: m[src]} from alternating out/src names; absent keys are nil.
func pick(m map[string]any, outSrc ...string) map[string]any {
	res := make(map[string]any, len(outSrc)/2)
	for i := 0; i+1 < len(outSrc); i += 2 {
		res[outSrc[i]] = m[outSrc[i+1]]
	}
	return res
}

// dropNil removes nil values ({k: v for k, v in d.items() if v is not None}).
func dropNil(m map[string]any) map[string]any {
	for k, v := range m {
		if v == nil {
			delete(m, k)
		}
	}
	return m
}

// round is Python's round(x, digits): correctly rounded from the exact binary value, ties to even.
func round(x float64, digits int) float64 {
	r, err := strconv.ParseFloat(strconv.FormatFloat(x, 'f', digits, 64), 64)
	if err != nil {
		return x
	}
	return r
}

// pyStr is str() for the scalars a Garmin payload holds.
func pyStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

var isoDateRe = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

// parseISODate is datetime.date.fromisoformat for YYYY-MM-DD, with Python's error texts.
func parseISODate(s string) (time.Time, error) {
	if !isoDateRe.MatchString(s) {
		return time.Time{}, fmt.Errorf("Invalid isoformat string: '%s'", s)
	}
	t, err := time.Parse(time.DateOnly, s)
	switch {
	case err == nil && t.Year() >= 1:
		return t, nil
	case s[5:7] < "01" || s[5:7] > "12":
		return time.Time{}, fmt.Errorf("month must be in 1..12")
	case err == nil:
		return time.Time{}, fmt.Errorf("year %d is out of range", t.Year())
	}
	return time.Time{}, fmt.Errorf("day is out of range for month")
}

// daysBetween is (end - start).days for dates parsed by parseISODate.
func daysBetween(start, end time.Time) int {
	return int(end.Sub(start).Hours() / 24)
}
