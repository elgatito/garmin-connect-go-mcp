package garmin

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Port of the python-garminconnect methods behind the challenges, devices, weight, data management and
// women's health tools (GC/__init__.py).

const (
	goalsPath               = "/goal-service/goal/goals"
	personalRecordPath      = "/personalrecord-service/personalrecord/prs"
	earnedBadgesPath        = "/badge-service/badge/earned"
	adhocChallengesPath     = "/adhocchallenge-service/adHocChallenge/historical"
	badgeChallengesPath     = "/badgechallenge-service/badgeChallenge/completed"
	availableChallengesPath = "/badgechallenge-service/badgeChallenge/available"
	nonCompletedPath        = "/badgechallenge-service/badgeChallenge/non-completed"
	virtualChallengesPath   = "/badgechallenge-service/virtualChallenge/inProgress"
	racePredictorPath       = "/metrics-service/metrics/racepredictions"
	deviceServicePath       = "/device-service/deviceservice"
	primaryDevicePath       = "/web-gateway/device-info/primary-training-device"
	solarPath               = "/web-gateway/solar"
	deviceMessagesPath      = "/device-service/devicemessage/messages"
	weightPath              = "/weight-service"
	uploadPath              = "/upload-service/upload"
	bloodPressurePath       = "/bloodpressure-service/bloodpressure"
	hydrationLogPath        = "/usersummary-service/usersummary/hydration/log"
	menstrualCalendarPath   = "/periodichealth-service/menstrualcycle/calendar"
	menstrualDayviewPath    = "/periodichealth-service/menstrualcycle/dayview"
	pregnancySnapshotPath   = "/periodichealth-service/menstrualcycle/pregnancysnapshot"
	maxHydrationML          = 10000
)

// ValidateNonNegativeInt is _validate_non_negative_integer.
func ValidateNonNegativeInt(v int, name string) error {
	if v < 0 {
		return fmt.Errorf("%s must be non-negative, got: %d", name, v)
	}
	return nil
}

// ── goals, records, badges, challenges ─────────────────────────────────────

// GoalsPage is one request of the Goals page: Garmin only lists Connect UI goals when it sees the fetch-metadata
// header of a same-origin browser request, and its start is 1-based.
func (c *Client) GoalsPage(ctx context.Context, status string, start, limit int) (any, error) {
	return c.DoJSON(ctx, Req{Method: http.MethodGet, Path: goalsPath, Header: http.Header{"Sec-Fetch-Site": {"same-origin"}},
		Query: url.Values{"status": {status}, "start": {strconv.Itoa(start)}, "limit": {strconv.Itoa(limit)}, "sortOrder": {"asc"}}})
}

// GetGoals is get_goals(status): the library's own paging from start 0, 30 at a time, until a page comes back empty.
// A payload that is not a list (a dict) is returned as it is; Python would extend the list with its keys and loop forever.
func (c *Client) GetGoals(ctx context.Context, status string) (any, error) {
	if status != "active" && status != "future" && status != "past" {
		return nil, errors.New("status must be one of {'active', 'future', 'past'}")
	}
	const limit = 30
	goals := []any{}
	for start := 0; ; start += limit {
		page, err := c.DoJSON(ctx, Req{Method: http.MethodGet, Path: goalsPath,
			Query: url.Values{"status": {status}, "start": {strconv.Itoa(start)}, "limit": {strconv.Itoa(limit)}, "sortOrder": {"asc"}}})
		if err != nil {
			return nil, err
		}
		switch p := page.(type) {
		case nil:
			return goals, nil
		case []any:
			if len(p) == 0 {
				return goals, nil
			}
			goals = append(goals, p...)
		default:
			if falsy(p) {
				return goals, nil
			}
			return p, nil
		}
	}
}

func falsy(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case bool:
		return !x
	case float64:
		return x == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

// GetPersonalRecord returns the personal records of the display name.
func (c *Client) GetPersonalRecord(ctx context.Context) (any, error) {
	dn, err := c.displayName(ctx)
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, personalRecordPath+"/"+dn, nil)
}

func (c *Client) GetEarnedBadges(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, earnedBadgesPath, nil)
}

// challengePage validates start (non-negative, or positive when startPositive) and limit and GETs one page.
func (c *Client) challengePage(ctx context.Context, path string, start, limit int, startPositive bool) (any, error) {
	var err error
	if startPositive {
		_, err = ValidatePositiveInt(start, "start")
	} else {
		err = ValidateNonNegativeInt(start, "start")
	}
	if err != nil {
		return nil, err
	}
	if _, err := ValidatePositiveInt(limit, "limit"); err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, path, url.Values{"start": {strconv.Itoa(start)}, "limit": {strconv.Itoa(limit)}})
}

func (c *Client) GetAdhocChallenges(ctx context.Context, start, limit int) (any, error) {
	return c.challengePage(ctx, adhocChallengesPath, start, limit, false)
}
func (c *Client) GetBadgeChallenges(ctx context.Context, start, limit int) (any, error) {
	return c.challengePage(ctx, badgeChallengesPath, start, limit, false)
}
func (c *Client) GetAvailableBadgeChallenges(ctx context.Context, start, limit int) (any, error) {
	return c.challengePage(ctx, availableChallengesPath, start, limit, false)
}
func (c *Client) GetNonCompletedBadgeChallenges(ctx context.Context, start, limit int) (any, error) {
	return c.challengePage(ctx, nonCompletedPath, start, limit, false)
}

// GetInprogressVirtualChallenges needs a 1-based start.
func (c *Client) GetInprogressVirtualChallenges(ctx context.Context, start, limit int) (any, error) {
	return c.challengePage(ctx, virtualChallengesPath, start, limit, true)
}

// GetLatestRacePredictions is get_race_predictions() without arguments.
func (c *Client) GetLatestRacePredictions(ctx context.Context) (any, error) {
	dn, err := c.requireDisplayName(ctx)
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, racePredictorPath+"/latest/"+dn, nil)
}

// ── devices ────────────────────────────────────────────────────────────────

// deviceID is str(_validate_positive_integer(int(device_id), "device_id")).
func deviceID(v any) (string, error) {
	n, err := PyInt(v)
	if err != nil {
		return "", err
	}
	if n <= 0 {
		return "", fmt.Errorf("device_id must be a positive integer, got: %d", n)
	}
	return strconv.FormatInt(n, 10), nil
}

// GetDeviceSettings takes the Union[int, str] id the way int() does.
func (c *Client) GetDeviceSettings(ctx context.Context, id any) (any, error) {
	s, err := deviceID(id)
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, deviceServicePath+"/device-info/settings/"+s, nil)
}

func (c *Client) GetPrimaryTrainingDevice(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, primaryDevicePath, nil)
}

// GetDeviceSolarData is get_device_solar_data(device_id, date) for a single day: the deviceSolarInput of the response.
func (c *Client) GetDeviceSolarData(ctx context.Context, id any, date string) (any, error) {
	d, err := ValidateDate(date, "startdate")
	if err != nil {
		return nil, err
	}
	s, err := deviceID(id)
	if err != nil {
		return nil, err
	}
	resp, err := c.GetJSON(ctx, solarPath+"/"+s+"/"+d+"/"+d, url.Values{"singleDayView": {"True"}})
	if err != nil {
		return nil, err
	}
	m, _ := resp.(map[string]any)
	input, ok := m["deviceSolarInput"]
	if !ok {
		return nil, errors.New("No device solar input data received")
	}
	return input, nil
}

// GetDeviceAlarms collects the alarms of every device's settings.
func (c *Client) GetDeviceAlarms(ctx context.Context) ([]any, error) {
	devices, err := c.GetDevices(ctx)
	if err != nil {
		return nil, err
	}
	alarms := []any{}
	list, _ := devices.([]any)
	for _, d := range list {
		m, _ := d.(map[string]any)
		id, ok := m["deviceId"]
		if !ok {
			return nil, errors.New("'deviceId'")
		}
		settings, err := c.GetDeviceSettings(ctx, id)
		if err != nil {
			return nil, err
		}
		sm, _ := settings.(map[string]any)
		if more, ok := sm["alarms"].([]any); ok {
			alarms = append(alarms, more...)
		}
	}
	return alarms, nil
}

// PushWorkoutMessage queues payload in the device message queue and returns the HTTP status (Python: client.post(...).status_code).
func (c *Client) PushWorkoutMessage(ctx context.Context, payload any) (int, error) {
	status, _, err := c.DoStatus(ctx, Req{Method: http.MethodPost, Path: deviceMessagesPath, JSON: payload})
	return status, err
}

// ── weight ─────────────────────────────────────────────────────────────────

func (c *Client) GetWeighIns(ctx context.Context, startdate, enddate string) (any, error) {
	return c.getByRange(ctx, weightPath+"/weight/range", "startdate", "enddate", startdate, enddate, url.Values{"includeAll": {"True"}})
}

func (c *Client) GetDailyWeighIns(ctx context.Context, cdate string) (any, error) {
	d, err := ValidateDate(cdate, "cdate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, weightPath+"/weight/dayview/"+d, url.Values{"includeAll": {"True"}})
}

// DeleteWeighIn deletes one weigh-in version (weight_pk is the samplePk Garmin reported).
func (c *Client) DeleteWeighIn(ctx context.Context, weightPK, cdate string) error {
	d, err := ValidateDate(cdate, "cdate")
	if err != nil {
		return err
	}
	_, err = c.Do(ctx, Req{Method: http.MethodDelete, Path: weightPath + "/weight/" + d + "/byversion/" + pathSeg(weightPK)})
	return err
}

// DeleteWeighIns deletes the weigh-ins of cdate and returns how many it deleted; deleted=false is Python's None
// (nothing to delete, or several and delete_all is false).
func (c *Client) DeleteWeighIns(ctx context.Context, cdate string, deleteAll bool) (n int, deleted bool, err error) {
	day, err := c.GetDailyWeighIns(ctx, cdate)
	if err != nil {
		return 0, false, err
	}
	dm, _ := day.(map[string]any)
	list, _ := dm["dateWeightList"].([]any)
	if len(list) == 0 || (len(list) > 1 && !deleteAll) {
		return 0, false, nil
	}
	for _, w := range list {
		wm, _ := w.(map[string]any)
		pk, ok := wm["samplePk"]
		if !ok {
			return 0, false, errors.New("'samplePk'")
		}
		s := fmt.Sprint(pk)
		if f, isNum := pk.(float64); isNum {
			s = strconv.FormatFloat(f, 'f', -1, 64)
		}
		if err := c.DeleteWeighIn(ctx, s, cdate); err != nil {
			return 0, false, err
		}
	}
	return len(list), true, nil
}

func validWeightUnit(unitKey string) error {
	if unitKey != "kg" && unitKey != "lbs" {
		return errors.New("unitKey must be one of {'kg', 'lbs'}")
	}
	return nil
}

func validatePositiveNumber(v float64, name string) error {
	if v <= 0 {
		return fmt.Errorf("%s must be positive, got: %s", name, PyFloatRepr(v))
	}
	return nil
}

func (c *Client) postWeighIn(ctx context.Context, local, gmt time.Time, unitKey string, weight float64) (any, error) {
	return c.DoJSON(ctx, Req{Method: http.MethodPost, Path: weightPath + "/user-weight", JSON: map[string]any{
		"dateTimestamp": fmtTS(local), "gmtTimestamp": fmtTS(gmt), "unitKey": unitKey, "sourceType": "MANUAL", "value": weight}})
}

// AddWeighIn adds a weigh-in at the current time.
func (c *Client) AddWeighIn(ctx context.Context, weight float64, unitKey string) (any, error) {
	if err := validatePositiveNumber(weight, "weight"); err != nil {
		return nil, err
	}
	if err := validWeightUnit(unitKey); err != nil {
		return nil, err
	}
	now := time.Now()
	return c.postWeighIn(ctx, now, now.UTC(), unitKey, weight)
}

// AddWeighInWithTimestamps: dateTimestamp and gmtTimestamp may be empty (now / derived from the local one).
func (c *Client) AddWeighInWithTimestamps(ctx context.Context, weight float64, unitKey, dateTimestamp, gmtTimestamp string) (any, error) {
	if err := validWeightUnit(unitKey); err != nil {
		return nil, err
	}
	dt := time.Now()
	if dateTimestamp != "" {
		t, _, err := parseISODateTime(dateTimestamp)
		if err != nil {
			return nil, err
		}
		dt = t.In(time.Local) // astimezone(): a naive time is read as local, an aware one converted
	}
	dtGMT := dt.UTC()
	if gmtTimestamp != "" {
		g, naive, err := parseISODateTime(gmtTimestamp)
		if err != nil {
			return nil, err
		}
		if naive { // a naive GMT timestamp is taken as UTC
			g = time.Date(g.Year(), g.Month(), g.Day(), g.Hour(), g.Minute(), g.Second(), g.Nanosecond(), time.UTC)
		}
		dtGMT = g.UTC()
	}
	if err := validatePositiveNumber(weight, "weight"); err != nil {
		return nil, err
	}
	return c.postWeighIn(ctx, dt, dtGMT, unitKey, weight)
}

// ── data management ────────────────────────────────────────────────────────

// AddBodyComposition uploads a weight scale FIT file recorded at timestamp (an ISO date or datetime; empty is now).
func (c *Client) AddBodyComposition(ctx context.Context, timestamp string, comp BodyComposition) (any, error) {
	if err := validatePositiveNumber(comp.Weight, "weight"); err != nil {
		return nil, err
	}
	dt := time.Now()
	if timestamp != "" {
		t, _, err := parseISODateTime(timestamp)
		if err != nil {
			return nil, err
		}
		dt = t
	}
	fit, err := encodeWeightFIT(dt, time.Now(), comp)
	if err != nil {
		return nil, err
	}
	return c.Upload(ctx, uploadPath, "file", "body_composition.fit", fit)
}

// SetBloodPressure adds a reading taken now.
func (c *Client) SetBloodPressure(ctx context.Context, systolic, diastolic, pulse int, notes *string) (any, error) {
	now := time.Now()
	var n any
	if notes != nil {
		n = *notes
	}
	payload := map[string]any{"measurementTimestampLocal": fmtTS(now), "measurementTimestampGMT": fmtTS(now.UTC()), "systolic": systolic,
		"diastolic": diastolic, "pulse": pulse, "sourceType": "MANUAL", "notes": n}
	for _, r := range []struct {
		name        string
		val, lo, hi int
	}{{"systolic", systolic, 70, 260}, {"diastolic", diastolic, 40, 150}, {"pulse", pulse, 20, 250}} {
		if r.val < r.lo || r.val > r.hi {
			return nil, fmt.Errorf("%s must be an int in [%d, %d]", r.name, r.lo, r.hi)
		}
	}
	return c.DoJSON(ctx, Req{Method: http.MethodPost, Path: bloodPressurePath, JSON: payload})
}

// AddHydrationData logs valueInML (negative subtracts) at timestamp on cdate; both are required here and must agree.
func (c *Client) AddHydrationData(ctx context.Context, valueInML float64, timestamp, cdate string) (any, error) {
	if math.Abs(valueInML) > maxHydrationML {
		return nil, fmt.Errorf("value_in_ml seems unreasonably high (>%dml)", maxHydrationML)
	}
	d, err := ValidateDate(cdate, "cdate")
	if err != nil {
		return nil, err
	}
	raw, _, err := parseISODateTime(timestamp)
	if err != nil {
		m := strptimeRe.FindStringSubmatch(timestamp)
		if m == nil {
			return nil, fmt.Errorf("time data %s does not match format '%%Y-%%m-%%dT%%H:%%M:%%S'", PyRepr(timestamp))
		}
		var v [6]int
		for i := range v {
			v[i], _ = strconv.Atoi(m[i+1])
		}
		raw = time.Date(v[0], time.Month(v[1]), v[2], v[3], v[4], v[5], 0, time.UTC)
		if raw.Month() != time.Month(v[1]) || raw.Day() != v[2] || v[3] > 23 || v[4] > 59 || v[5] > 59 {
			return nil, fmt.Errorf("time data %s does not match format '%%Y-%%m-%%dT%%H:%%M:%%S'", PyRepr(timestamp))
		}
	}
	if tsDate := raw.Format(time.DateOnly); tsDate != d {
		return nil, fmt.Errorf("timestamp date (%s) doesn't match cdate (%s)", tsDate, d)
	}
	return c.DoJSON(ctx, Req{Method: http.MethodPut, Path: hydrationLogPath,
		JSON: map[string]any{"calendarDate": d, "timestampLocal": fmtTS(raw), "valueInML": valueInML}})
}

// ── women's health ─────────────────────────────────────────────────────────

func (c *Client) GetMenstrualDataForDate(ctx context.Context, fordate string) (any, error) {
	return c.getByDate(ctx, menstrualDayviewPath, "fordate", fordate)
}

func (c *Client) GetMenstrualCalendarData(ctx context.Context, startdate, enddate string) (any, error) {
	return c.getByRange(ctx, menstrualCalendarPath, "startdate", "enddate", startdate, enddate, nil)
}

func (c *Client) GetPregnancySummary(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, pregnancySnapshotPath, nil)
}
