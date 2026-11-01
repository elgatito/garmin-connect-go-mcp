package garmin

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Python helpers the API methods depend on: datetime.fromisoformat, int(), repr() and the _fmt_ts timestamp format.

// PyRepr is repr() of a str.
func PyRepr(s string) string {
	q := "'"
	if strings.Contains(s, "'") && !strings.Contains(s, `"`) {
		q = `"`
	}
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	if q == "'" {
		s = strings.ReplaceAll(s, "'", `\'`)
	}
	return q + s + q
}

// PyFloatRepr is repr() of a float.
func PyFloatRepr(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	case math.IsNaN(f):
		return "nan"
	}
	if a := math.Abs(f); a != 0 && (a >= 1e16 || a < 1e-4) {
		return strconv.FormatFloat(f, 'e', -1, 64)
	}
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

var pyIntRe = regexp.MustCompile(`^[+-]?[0-9]+(_[0-9]+)*$`)

// PyInt is int(v) for a decoded JSON value (the Union[int, str] ids the tools accept).
func PyInt(v any) (int64, error) {
	switch x := v.(type) {
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return 0, fmt.Errorf("cannot convert float %s to integer", PyFloatRepr(x))
		}
		return int64(x), nil
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case string:
		s := strings.TrimSpace(x)
		n, err := strconv.ParseInt(strings.ReplaceAll(s, "_", ""), 10, 64)
		if err != nil || !pyIntRe.MatchString(s) {
			return 0, fmt.Errorf("invalid literal for int() with base 10: %s", PyRepr(x))
		}
		return n, nil
	}
	name := "NoneType"
	switch v.(type) {
	case []any:
		name = "list"
	case map[string]any:
		name = "dict"
	}
	return 0, fmt.Errorf("int() argument must be a string, a bytes-like object or a real number, not '%s'", name)
}

var isoDateTimeRe = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})(?:.(\d{2})(?::(\d{2})(?::(\d{2})(?:[.,](\d{1,6}))?)?)?(Z|[+-]\d{2}(?::?\d{2}(?::?\d{2})?)?)?)?$`)

// parseISODateTime is datetime.fromisoformat. A naive result carries the wall clock in time.Local and naive=true;
// an aware one carries its own offset.
func parseISODateTime(s string) (t time.Time, naive bool, err error) {
	m := isoDateTimeRe.FindStringSubmatch(s)
	if m == nil {
		return t, false, fmt.Errorf("Invalid isoformat string: %s", PyRepr(s))
	}
	n := func(i int) int { v, _ := strconv.Atoi(m[i]); return v }
	year, month, day, hour, minute, sec := n(1), n(2), n(3), n(4), n(5), n(6)
	micro := 0
	if f := m[7]; f != "" {
		micro, _ = strconv.Atoi(f + strings.Repeat("0", 6-len(f)))
	}
	switch {
	case year < 1:
		return t, false, fmt.Errorf("year %d is out of range", year)
	case month < 1 || month > 12:
		return t, false, fmt.Errorf("month must be in 1..12")
	case day < 1 || day > time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day():
		return t, false, fmt.Errorf("day is out of range for month")
	case hour > 23:
		return t, false, fmt.Errorf("hour must be in 0..23")
	case minute > 59:
		return t, false, fmt.Errorf("minute must be in 0..59")
	case sec > 59:
		return t, false, fmt.Errorf("second must be in 0..59")
	}
	loc := time.Local
	if z := m[8]; z != "" {
		off := 0
		if z != "Z" {
			d := strings.ReplaceAll(z[1:], ":", "")
			h, _ := strconv.Atoi(d[:2])
			mi, se := 0, 0
			if len(d) >= 4 {
				mi, _ = strconv.Atoi(d[2:4])
			}
			if len(d) >= 6 {
				se, _ = strconv.Atoi(d[4:6])
			}
			if h > 23 || mi > 59 || se > 59 {
				return t, false, fmt.Errorf("offset must be a timedelta strictly between -timedelta(hours=24) and timedelta(hours=24)")
			}
			off = h*3600 + mi*60 + se
			if z[0] == '-' {
				off = -off
			}
		}
		loc = time.FixedZone("", off)
	}
	return time.Date(year, time.Month(month), day, hour, minute, sec, micro*1000, loc), m[8] == "", nil
}

var strptimeRe = regexp.MustCompile(`^(\d{1,4})-(\d{1,2})-(\d{1,2})T(\d{1,2}):(\d{1,2}):(\d{1,2})$`)

// fmtTS is _fmt_ts: the wall clock to millisecond precision, without a zone.
func fmtTS(t time.Time) string { return t.Format("2006-01-02T15:04:05.000") }
