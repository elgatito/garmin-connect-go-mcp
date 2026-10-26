package server

import (
	"context"
	"errors"
	"math"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

// Helpers of garmin_mcp/training.py. The functions keep the Python names (snake_case -> camelCase) so the ported
// unit tests read the same.

// deviceID is _device_id: str(value), or nothing for a missing value.
func deviceID(v any) (string, bool) {
	if v == nil {
		return "", false
	}
	return idString(v), true
}

// deviceIDOr is `_device_id(a) or _device_id(b)`: an empty id falls back to the key.
func deviceIDOr(v any, key string) *string {
	if id, ok := deviceID(v); ok && id != "" {
		return &id
	}
	if key == "" {
		return nil
	}
	return &key
}

// primaryDeviceID is _primary_device_id: the verified primary tracker, or unknown.
func primaryDeviceID(devices any) *string {
	for _, d := range asList(devices) {
		if dev := asMap(d); dev["primaryDevice"] == true {
			id, ok := deviceID(dev["deviceId"])
			if !ok {
				return nil
			}
			return &id
		}
	}
	return nil
}

type deviceSelection struct {
	Data    map[string]any
	ID      *string // reporting device id
	Name    *string
	Primary *bool // nil: unknown
}

// deviceEntries is the non-empty dict values of a device-keyed map in Garmin's order (order lists the keys as
// Garmin sent them; keys it does not mention follow in sorted order).
func deviceEntries(m map[string]any, order []string) (keys []string) {
	seen := map[string]bool{}
	for _, k := range order {
		if _, ok := m[k]; ok && !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	var rest []string
	for k := range m {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	keys = append(keys, rest...)
	out := keys[:0]
	for _, k := range keys {
		if v := asMap(m[k]); len(v) > 0 {
			out = append(out, k)
		}
	}
	return out
}

// selectTrainingDevice is _select_training_device: it selects one device record and says whether it is the
// primary tracker (nil: unknown; never inferred as false).
func selectTrainingDevice(latest any, order []string, devices any) deviceSelection {
	l := asMap(latest)
	keys := deviceEntries(l, order)
	if len(keys) == 0 {
		return deviceSelection{Data: map[string]any{}}
	}
	var records []map[string]any
	for _, d := range asList(devices) {
		if dev, ok := d.(map[string]any); ok {
			records = append(records, dev)
		}
	}
	var primaryID *string
	for _, dev := range records {
		if dev["primaryDevice"] == true {
			if id, ok := deviceID(dev["deviceId"]); ok {
				primaryID = &id
			}
			break
		}
	}

	selectedKey := keys[0]
	marked := func() (string, bool) {
		for _, k := range keys {
			if asMap(l[k])["primaryTrainingDevice"] == true {
				return k, true
			}
		}
		return "", false
	}
	if primaryID != nil {
		matched := false
		for _, k := range keys {
			if id := deviceIDOr(asMap(l[k])["deviceId"], k); id != nil && *id == *primaryID {
				selectedKey, matched = k, true
				break
			}
		}
		// The primary tracker may be absent from this day's status map: use Garmin's explicit marker first.
		if !matched {
			if k, ok := marked(); ok {
				selectedKey = k
			}
		}
	} else if k, ok := marked(); ok {
		selectedKey = k
	}

	selected := asMap(l[selectedKey])
	sel := deviceSelection{Data: selected, ID: deviceIDOr(selected["deviceId"], selectedKey)}
	for _, dev := range records {
		if id, _ := deviceID(dev["deviceId"]); sel.ID != nil && id == *sel.ID {
			if name := firstTruthy(dev["displayName"], dev["productDisplayName"]); name != nil {
				s := pyStr(name)
				sel.Name = &s
			}
			break
		}
	}
	if primaryID != nil && sel.ID != nil {
		p := *sel.ID == *primaryID
		sel.Primary = &p
	}
	return sel
}

// trackerAcuteLoad is _tracker_acute_load: the acute load of an identified tracker/date readiness row, or the
// reason there is none.
func trackerAcuteLoad(readiness any, date string, trackerID *string) (any, string) {
	if trackerID == nil {
		return nil, "tracker_id_unavailable"
	}
	candidates, ok := readiness.([]any)
	if !ok {
		candidates = []any{readiness}
	}
	var matching []map[string]any
	for _, c := range candidates {
		cand, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if id, ok := deviceID(cand["deviceId"]); ok && id == *trackerID && cand["calendarDate"] == date && cand["acuteLoad"] != nil {
			matching = append(matching, cand)
		}
	}
	if len(matching) == 0 {
		return nil, "readiness_device_or_date_mismatch"
	}
	key := func(c map[string]any) string {
		return pyStr(firstTruthy(firstTruthy(c["timestampLocal"], c["timestamp"]), ""))
	}
	sort.SliceStable(matching, func(i, j int) bool { return key(matching[i]) < key(matching[j]) })
	return matching[len(matching)-1]["acuteLoad"], ""
}

// intIfDigits is `int(x) if x.isdigit() else x` for a device id.
func intIfDigits(id string) any {
	if id != "" && strings.Trim(id, "0123456789") == "" {
		if n, err := strconv.ParseInt(id, 10, 64); err == nil {
			return n
		}
	}
	return id
}

func strOrNil(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func intIfDigitsPtr(s *string) any {
	if s == nil || *s == "" {
		return strOrNil(s)
	}
	return intIfDigits(*s)
}

// progressCaloriesFactor: the fitnessstats "calories" metric is stored pre-multiplied by this factor (a Garmin
// unit quirk of that endpoint, not a kJ conversion).
const progressCaloriesFactor = 4.19

// convertProgressMetric is _convert_progress_metric.
func convertProgressMetric(metric string, value any) any {
	v, ok := asNumber(value)
	if !ok || metric != "calories" {
		return value
	}
	return v / progressCaloriesFactor
}

// cmPerMsToMps: Garmin returns the lactate threshold speed in cm/ms (Connect's web app multiplies by 10).
const cmPerMsToMps = 10

// lactateSpeedMPS is _lactate_threshold_speed_mps.
func lactateSpeedMPS(value any) any {
	v, ok := asNumber(value)
	if !ok || v == 0 {
		return nil
	}
	return v * cmPerMsToMps
}

var vo2Paths = []struct {
	path  []string
	sport string
}{
	{[]string{"vo2MaxRunning"}, "running"},
	{[]string{"vo2MaxCycling"}, "cycling"},
	{[]string{"vo2Max"}, "running"},
	{[]string{"vo2MaxPreciseValue"}, "running"},
	{[]string{"vo2MaxValue"}, "running"},
	{[]string{"generic", "vo2MaxPreciseValue"}, "running"},
	{[]string{"generic", "vo2MaxValue"}, "running"},
	{[]string{"cycling", "vo2MaxPreciseValue"}, "cycling"},
	{[]string{"cycling", "vo2MaxValue"}, "cycling"},
	{[]string{"mostRecentVO2Max", "generic", "vo2MaxPreciseValue"}, "running"},
	{[]string{"mostRecentVO2Max", "generic", "vo2MaxValue"}, "running"},
	{[]string{"mostRecentVO2Max", "cycling", "vo2MaxPreciseValue"}, "cycling"},
	{[]string{"mostRecentVO2Max", "cycling", "vo2MaxValue"}, "cycling"},
	{[]string{"userData", "vo2MaxRunning"}, "running"},
	{[]string{"userData", "vo2MaxCycling"}, "cycling"},
}

// extractVO2Measurements is _extract_vo2_measurements: all VO2 max values by sport, the first usable per sport.
func extractVO2Measurements(data any) map[string]float64 {
	if list, ok := data.([]any); ok {
		out := map[string]float64{}
		for _, item := range list {
			for sport, v := range extractVO2Measurements(item) {
				if _, ok := out[sport]; !ok {
					out[sport] = v
				}
			}
		}
		return out
	}
	out := map[string]float64{}
	d := asMap(data)
	for _, p := range vo2Paths {
		var cur any = d
		for _, k := range p.path {
			cur = asMap(cur)[k]
		}
		if _, have := out[p.sport]; have {
			continue
		}
		if v, ok := asNumber(cur); ok && !math.IsNaN(v) && !math.IsInf(v, 0) {
			out[p.sport] = v
		}
	}
	return out
}

// extractDatedVO2Measurements is _extract_dated_vo2_measurements: max-metrics values by calendar date and sport.
func extractDatedVO2Measurements(data any) map[string]map[string]float64 {
	byDate := map[string]map[string]float64{}
	var collect func(item any)
	collect = func(item any) {
		if list, ok := item.([]any); ok {
			for _, n := range list {
				collect(n)
			}
			return
		}
		it := asMap(item)
		sections := asMap(it["mostRecentVO2Max"])
		if len(sections) == 0 {
			sections = it
		}
		for sport, value := range extractVO2Measurements(it) {
			name := "cycling"
			if sport == "running" {
				name = "generic"
			}
			section := asMap(sections[name])
			date, ok := firstTruthy(firstTruthy(section["calendarDate"], sections["calendarDate"]), it["calendarDate"]).(string)
			if !ok {
				continue
			}
			if byDate[date] == nil {
				byDate[date] = map[string]float64{}
			}
			if _, have := byDate[date][sport]; !have {
				byDate[date][sport] = value
			}
		}
	}
	collect(data)
	return byDate
}

// metricsEndpoint is the part of the client getMaxMetricsRange needs (garmin_connect_metrics_url and connectapi).
type metricsEndpoint struct {
	URL string
	Get func(ctx context.Context, path string) (any, error)
}

// getMaxMetricsRange is _get_max_metrics_range: false when the client cannot fetch a range.
func getMaxMetricsRange(ctx context.Context, ep metricsEndpoint, start, end string) (bool, any, error) {
	if ep.Get == nil || ep.URL == "" {
		return false, nil, nil
	}
	v, err := ep.Get(ctx, ep.URL+"/"+start+"/"+end)
	return true, v, err
}

var apiStatusRe = regexp.MustCompile(`\bAPI Error (\d{3})\b`)

// isFatalTrendError is _is_fatal_trend_error: authentication, rate limits and outages stop the endpoint fallbacks.
func isFatalTrendError(err error) bool {
	if errors.Is(err, garmin.ErrSessionExpired) {
		return true
	}
	if m := apiStatusRe.FindStringSubmatch(err.Error()); m != nil {
		status, _ := strconv.Atoi(m[1])
		return status == 401 || status == 429 || status >= 500
	}
	// Transport failures (*url.Error is a net.Error) are python-garminconnect's connection errors.
	var netErr net.Error
	return errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded)
}

// isForbiddenError is _is_forbidden_error.
func isForbiddenError(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "403") || strings.Contains(text, "forbidden")
}

// firstPresent is _first_present: the first non-null value of keys.
func firstPresent(m any, keys ...string) any {
	d := asMap(m)
	for _, k := range keys {
		if v := d[k]; v != nil {
			return v
		}
	}
	return nil
}

// activityIDMatches is _activity_id_matches: activityId may arrive as a number or a string.
func activityIDMatches(item any, id int64) bool {
	raw := asMap(item)["activityId"]
	switch x := raw.(type) {
	case float64:
		return x == float64(id)
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err == nil {
			return n == id
		}
		return x == strconv.FormatInt(id, 10)
	case nil:
		return false // str(None) never equals the id
	}
	return pyStr(raw) == strconv.FormatInt(id, 10)
}

// asActivityList is _as_activity_list.
func asActivityList(payload any) []map[string]any {
	dicts := func(l []any) []map[string]any {
		out := []map[string]any{}
		for _, i := range l {
			if m, ok := i.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	if l, ok := payload.([]any); ok {
		return dicts(l)
	}
	d := asMap(payload)
	for _, k := range []string{"activityList", "activities"} {
		if l, ok := d[k].([]any); ok {
			return dicts(l)
		}
	}
	return []map[string]any{}
}

// activityLister is what _lookup_activity_in_list needs from the client.
type activityLister interface {
	SearchList(ctx context.Context, q url.Values) (any, error)
	Activities(ctx context.Context, start, limit int) (any, error)
}

// lookupActivityInList is _lookup_activity_in_list: finds an activity in the list-search endpoint, which often
// works when the details endpoint answers 403. Failed searches are skipped.
func lookupActivityInList(ctx context.Context, c activityLister, id int64, limit int) map[string]any {
	find := func(payload any) map[string]any {
		for _, item := range asActivityList(payload) {
			if activityIDMatches(item, id) {
				return item
			}
		}
		return nil
	}
	for _, q := range []url.Values{
		{"activityIds": {strconv.FormatInt(id, 10)}, "start": {"0"}, "limit": {"1"}},
		{"start": {"0"}, "limit": {strconv.Itoa(limit)}},
	} {
		payload, err := c.SearchList(ctx, q)
		if err != nil {
			continue
		}
		if match := find(payload); match != nil {
			return match
		}
	}
	payload, err := c.Activities(ctx, 0, limit)
	if err != nil {
		return nil
	}
	return find(payload)
}

type liveActivityLister struct{ c *garmin.Client }

func (l liveActivityLister) SearchList(ctx context.Context, q url.Values) (any, error) {
	return l.c.ListActivitiesRaw(ctx, q)
}

func (l liveActivityLister) Activities(ctx context.Context, start, limit int) (any, error) {
	return l.c.GetActivities(ctx, start, limit, "")
}

// minutesToHours is _minutes_to_hours: Garmin recovery minutes as hours, or nil when missing or invalid.
func minutesToHours(v any) any {
	n, ok := asNumber(v)
	if !ok || n <= 0 || math.IsInf(n, 0) || math.IsNaN(n) {
		return nil
	}
	return round(n/60.0, 1)
}

// curateTrainingEffect is _curate_training_effect: training-effect fields from the details or the list-search shape.
func curateTrainingEffect(activity any, activityID int64, source string) map[string]any {
	act := asMap(activity)
	summary := asMap(act["summaryDTO"])
	either := func(sumKeys, actKeys []string) any {
		if v := firstPresent(summary, sumKeys...); v != nil {
			return v
		}
		return firstPresent(act, actKeys...)
	}
	aerobic := either([]string{"trainingEffect", "aerobicTrainingEffect"}, []string{"aerobicTrainingEffect", "trainingEffect"})
	return dropNil(map[string]any{
		"activity_id":           activityID,
		"source":                source,
		"training_effect":       aerobic,
		"aerobic_effect":        aerobic,
		"anaerobic_effect":      either([]string{"anaerobicTrainingEffect"}, []string{"anaerobicTrainingEffect"}),
		"training_effect_label": either([]string{"trainingEffectLabel"}, []string{"trainingEffectLabel", "aerobicTrainingEffectMessage"}),
		"recovery_time_hours":   minutesToHours(either([]string{"recoveryTime"}, []string{"recoveryTime"})),
		"training_load":         either([]string{"activityTrainingLoad"}, []string{"activityTrainingLoad"}),
		"performance_condition": either([]string{"performanceCondition"}, []string{"performanceCondition"}),
	})
}

type vo2Point struct {
	Date   string
	VO2    float64
	Source string
}

// buildVO2TrendSeries is _build_vo2_trend_series: sparse max-metrics days as a dense daily series, carrying the
// last value forward (marked carried_forward) until end.
func buildVO2TrendSeries(history []vo2Point, end time.Time) ([]map[string]any, error) {
	series := []map[string]any{}
	if len(history) == 0 {
		return series, nil
	}
	measured := map[string]vo2Point{}
	first := history[0].Date
	for _, p := range history {
		measured[p.Date] = p
		if p.Date < first {
			first = p.Date
		}
	}
	cur, err := parseISODate(first)
	if err != nil {
		return nil, err
	}
	var last *vo2Point
	for ; !cur.After(end); cur = cur.AddDate(0, 0, 1) {
		if p, ok := measured[cur.Format(time.DateOnly)]; ok {
			last = &p
			series = append(series, map[string]any{"date": p.Date, "vo2_max": p.VO2, "source": p.Source})
		} else if last != nil {
			series = append(series, map[string]any{"date": cur.Format(time.DateOnly), "vo2_max": last.VO2, "source": last.Source, "carried_forward": true})
		}
	}
	return series, nil
}

// activityTypeCache is the _activity_type_cache: typeId -> typeKey, filled by the first lookup that succeeds.
type activityTypeCache struct {
	mu sync.Mutex
	m  map[int]string
}

// mapping is _get_activity_type_mapping; a failed lookup gives an empty mapping and is retried next time.
func (c *activityTypeCache) mapping(ctx context.Context, fetch func(context.Context) (any, error)) map[int]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m != nil {
		return c.m
	}
	types, err := fetch(ctx)
	if err != nil {
		return map[int]string{}
	}
	m := map[int]string{}
	for _, t := range asList(types) {
		at := asMap(t)
		if id, ok := asNumber(at["typeId"]); ok {
			m[int(id)] = pyStr(getDefault(at, "typeKey", "unknown"))
		}
	}
	c.m = m
	return m
}

// removeNone is the recursive remove_none of get_endurance_score: nulls vanish from objects, lists keep their items.
func removeNone(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			if e != nil {
				out[k] = removeNone(e)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = removeNone(e)
		}
		return out
	}
	return v
}

// roundAny is round(v, digits) for numbers; anything else is returned as it is.
func roundAny(v any, digits int) any {
	if n, ok := asNumber(v); ok {
		return round(n, digits)
	}
	return v
}
