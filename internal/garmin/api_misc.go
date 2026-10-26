package garmin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	goalsPath                       = "/goal-service/goal/goals"
	personalRecordPath              = "/personalrecord-service/personalrecord/prs"
	earnedBadgesPath                = "/badge-service/badge/earned"
	adhocChallengesPath             = "/adhocchallenge-service/adHocChallenge/historical"
	badgeChallengesPath             = "/badgechallenge-service/badgeChallenge/completed"
	availableBadgeChallengesPath    = "/badgechallenge-service/badgeChallenge/available"
	nonCompletedBadgeChallengesPath = "/badgechallenge-service/badgeChallenge/non-completed"
	virtualChallengesPath           = "/badgechallenge-service/virtualChallenge/inProgress"
	racePredictorPath               = "/metrics-service/metrics/racepredictions"
	deviceServicePath               = "/device-service/deviceservice"
	primaryTrainingDevicePath       = "/web-gateway/device-info/primary-training-device"
	solarPath                       = "/web-gateway/solar"
	uploadServicePath               = "/upload-service/upload"
	bloodPressureLogPath            = "/bloodpressure-service/bloodpressure"
	hydrationLogPath                = "/usersummary-service/usersummary/hydration/log"
	menstrualDayviewPath            = "/periodichealth-service/menstrualcycle/dayview"
	menstrualCalendarPath           = "/periodichealth-service/menstrualcycle/calendar"
	pregnancySnapshotPath           = "/periodichealth-service/menstrualcycle/pregnancysnapshot"
	courseServicePath               = "/course-service/course"
	courseImportPath                = "/course-service/course/import"
	deviceMessageQueuePath          = "device-service/devicemessage/messages"
)

func miscTruthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return x != ""
	case bool:
		return x
	case map[string]any:
		return len(x) > 0
	case []any:
		return len(x) > 0
	case float64:
		return x != 0
	case int:
		return x != 0
	}
	return true
}

func jsonMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// --- challenges ---

// GetGoalsPage reads one page of Connect UI goals with the browser header the
// endpoint needs.
func (c *Client) GetGoalsPage(ctx context.Context, status string, start, limit int) (any, error) {
	header := http.Header{}
	header.Set("Sec-Fetch-Site", "same-origin")
	return c.DoJSON(ctx, Req{
		Method: http.MethodGet,
		Path:   goalsPath,
		Query: url.Values{
			"status":    {status},
			"start":     {strconv.Itoa(start)},
			"limit":     {strconv.Itoa(limit)},
			"sortOrder": {"asc"},
		},
		Header: header,
	})
}

// GetGoals is python-garminconnect's get_goals: pages from index 0 until an
// empty page comes back.
func (c *Client) GetGoals(ctx context.Context, status string) ([]any, error) {
	var goals []any
	start := 0
	for {
		v, err := c.GetJSON(ctx, goalsPath, url.Values{
			"status":    {status},
			"start":     {strconv.Itoa(start)},
			"limit":     {"30"},
			"sortOrder": {"asc"},
		})
		if err != nil {
			return nil, err
		}
		list, _ := v.([]any)
		if len(list) == 0 {
			break
		}
		goals = append(goals, list...)
		start += 30
	}
	return goals, nil
}

// GetPersonalRecord returns the user's personal records.
func (c *Client) GetPersonalRecord(ctx context.Context) (any, error) {
	name, err := c.requireDisplayName(ctx)
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, personalRecordPath+"/"+name, nil)
}

// GetEarnedBadges returns the user's earned badges.
func (c *Client) GetEarnedBadges(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, earnedBadgesPath, nil)
}

func challengePage(ctx context.Context, c *Client, path string, start, limit int, positiveStart bool) (any, error) {
	var err error
	if positiveStart {
		start, err = ValidatePositiveInteger(start, "start")
	} else {
		start, err = ValidateNonNegativeInteger(start, "start")
	}
	if err != nil {
		return nil, err
	}
	if limit, err = ValidatePositiveInteger(limit, "limit"); err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, path, url.Values{
		"start": {strconv.Itoa(start)},
		"limit": {strconv.Itoa(limit)},
	})
}

// GetAdhocChallenges returns user-created social challenges.
func (c *Client) GetAdhocChallenges(ctx context.Context, start, limit int) (any, error) {
	return challengePage(ctx, c, adhocChallengesPath, start, limit, false)
}

// GetBadgeChallenges returns completed badge challenges.
func (c *Client) GetBadgeChallenges(ctx context.Context, start, limit int) (any, error) {
	return challengePage(ctx, c, badgeChallengesPath, start, limit, false)
}

// GetAvailableBadgeChallenges returns badge challenges the user can join.
func (c *Client) GetAvailableBadgeChallenges(ctx context.Context, start, limit int) (any, error) {
	return challengePage(ctx, c, availableBadgeChallengesPath, start, limit, false)
}

// GetNonCompletedBadgeChallenges returns in-progress badge challenges.
func (c *Client) GetNonCompletedBadgeChallenges(ctx context.Context, start, limit int) (any, error) {
	return challengePage(ctx, c, nonCompletedBadgeChallengesPath, start, limit, false)
}

// GetInprogressVirtualChallenges returns active virtual challenges.
func (c *Client) GetInprogressVirtualChallenges(ctx context.Context, start, limit int) (any, error) {
	return challengePage(ctx, c, virtualChallengesPath, start, limit, true)
}

// GetRacePredictions returns Garmin's latest race predictions.
func (c *Client) GetRacePredictions(ctx context.Context) (any, error) {
	name, err := c.requireDisplayName(ctx)
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, racePredictorPath+"/latest/"+name, nil)
}

// --- devices ---

// deviceIDString normalizes a device ID for a URL path. Garmin sometimes uses
// non-numeric application keys, so non-numeric values pass through unchanged.
func deviceIDString(v any) (string, error) {
	switch x := v.(type) {
	case string:
		raw := strings.TrimSpace(x)
		if raw == "" {
			return "", errors.New("device_id must not be empty")
		}
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			if n <= 0 {
				return "", fmt.Errorf("device_id must be a positive integer, got: %d", n)
			}
			return strconv.FormatInt(n, 10), nil
		}
		return raw, nil
	case int:
		if x <= 0 {
			return "", fmt.Errorf("device_id must be a positive integer, got: %d", x)
		}
		return strconv.Itoa(x), nil
	case int64:
		if x <= 0 {
			return "", fmt.Errorf("device_id must be a positive integer, got: %d", x)
		}
		return strconv.FormatInt(x, 10), nil
	case float64:
		if x != math.Trunc(x) || x <= 0 {
			return "", fmt.Errorf("device_id must be a positive integer, got: %v", x)
		}
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	case json.Number:
		n, err := x.Int64()
		if err != nil || n <= 0 {
			return "", fmt.Errorf("device_id must be a positive integer, got: %v", v)
		}
		return strconv.FormatInt(n, 10), nil
	}
	return "", fmt.Errorf("device_id must be a positive integer, got: %v", v)
}

// GetDeviceSettings returns configuration for one device.
func (c *Client) GetDeviceSettings(ctx context.Context, deviceID any) (any, error) {
	id, err := deviceIDString(deviceID)
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, deviceServicePath+"/device-info/settings/"+id, nil)
}

// GetPrimaryTrainingDevice returns the primary training device and its peers.
func (c *Client) GetPrimaryTrainingDevice(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, primaryTrainingDevicePath, nil)
}

// GetDeviceSolarData returns per-day solar charging data.
func (c *Client) GetDeviceSolarData(ctx context.Context, deviceID any, date string) (any, error) {
	d, err := ValidateDate(date, "startdate")
	if err != nil {
		return nil, err
	}
	id, err := deviceIDString(deviceID)
	if err != nil {
		return nil, err
	}
	resp, err := c.GetJSON(ctx, solarPath+"/"+id+"/"+d+"/"+d, url.Values{"singleDayView": {"True"}})
	if err != nil {
		return nil, err
	}
	m := jsonMap(resp)
	if m == nil || !miscTruthy(m["deviceSolarInput"]) {
		return nil, errors.New("No device solar input data received")
	}
	return m["deviceSolarInput"], nil
}

// GetDeviceAlarms returns alarms from every registered device.
func (c *Client) GetDeviceAlarms(ctx context.Context) ([]any, error) {
	devices, err := c.GetDevices(ctx)
	if err != nil {
		return nil, err
	}
	var alarms []any
	for _, item := range jsonList(devices) {
		settings, err := c.GetDeviceSettings(ctx, jsonMap(item)["deviceId"])
		if err != nil {
			return nil, err
		}
		if deviceAlarms, ok := jsonMap(settings)["alarms"]; ok && deviceAlarms != nil {
			alarms = append(alarms, jsonList(deviceAlarms)...)
		}
	}
	return alarms, nil
}

func jsonList(v any) []any {
	l, _ := v.([]any)
	return l
}

// PushWorkoutToDevice queues a workout FIT file on the device message queue.
func (c *Client) PushWorkoutToDevice(ctx context.Context, deviceID any, workoutID int64, workoutName string) (any, error) {
	id, err := deviceIDString(deviceID)
	if err != nil {
		return nil, err
	}
	payload := []any{map[string]any{
		"deviceId":    jsonNumberOrString(id),
		"messageUrl":  fmt.Sprintf("workout-service/workout/FIT/%d", workoutID),
		"messageType": "workouts",
		"groupName":   nil,
		"messageName": workoutName,
		"priority":    1,
		"fileType":    "FIT",
		"metaDataId":  workoutID,
	}}
	return c.DoJSON(ctx, Req{Method: http.MethodPost, Path: deviceMessageQueuePath, JSON: payload})
}

func jsonNumberOrString(s string) any {
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	return s
}

// --- weight management ---

// GetWeighIns returns weigh-ins between two dates.
func (c *Client) GetWeighIns(ctx context.Context, startDate, endDate string) (any, error) {
	start, err := ValidateDate(startDate, "startdate")
	if err != nil {
		return nil, err
	}
	end, err := ValidateDate(endDate, "enddate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, weightPath+"/weight/range/"+start+"/"+end, url.Values{"includeAll": {"true"}})
}

// GetDailyWeighIns returns weigh-ins for one day.
func (c *Client) GetDailyWeighIns(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "cdate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, weightPath+"/weight/dayview/"+d, url.Values{"includeAll": {"true"}})
}

// DeleteWeighIn deletes one weigh-in version. Callers read the day view first
// so validation happens before the write tracker is marked sent.
func (c *Client) DeleteWeighIn(ctx context.Context, date string, samplePk any) (any, error) {
	d, err := ValidateDate(date, "cdate")
	if err != nil {
		return nil, err
	}
	return c.DoJSON(ctx, Req{
		Method: http.MethodDelete,
		Path:   weightPath + "/weight/" + d + "/byversion/" + numericIDString(samplePk),
	})
}

func numericIDString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case json.Number:
		return x.String()
	}
	return fmt.Sprint(v)
}

func formatMillis(t time.Time) string {
	return t.Format("2006-01-02T15:04:05.000")
}

func parseSoftTime(s string, loc *time.Location) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid timestamp format: %s", s)
}

func weighInPayload(weight float64, unitKey string, dateTimestamp, gmtTimestamp time.Time) map[string]any {
	return map[string]any{
		"dateTimestamp": formatMillis(dateTimestamp),
		"gmtTimestamp":  formatMillis(gmtTimestamp),
		"unitKey":       unitKey,
		"sourceType":    "MANUAL",
		"value":         weight,
	}
}

// AddWeighIn adds a weigh-in timestamped now.
func (c *Client) AddWeighIn(ctx context.Context, weight float64, unitKey string) (any, error) {
	if err := ValidateWeight(weight); err != nil {
		return nil, err
	}
	if err := ValidateWeightUnit(unitKey); err != nil {
		return nil, err
	}
	now := time.Now()
	return c.DoJSON(ctx, Req{
		Method: http.MethodPost,
		Path:   weightPath + "/user-weight",
		JSON:   weighInPayload(weight, unitKey, now, now.UTC()),
	})
}

// weighInTimestamps resolves the local/GMT pair, deriving GMT from local when
// it is omitted.
func weighInTimestamps(dateTimestamp, gmtTimestamp string) (time.Time, time.Time, error) {
	var local time.Time
	if dateTimestamp != "" {
		parsed, err := parseSoftTime(dateTimestamp, time.Local)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		local = parsed.Local()
	} else {
		local = time.Now()
	}
	var gmt time.Time
	if gmtTimestamp != "" {
		parsed, err := parseSoftTime(gmtTimestamp, time.UTC)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		gmt = parsed.UTC()
	} else {
		gmt = local.UTC()
	}
	return local, gmt, nil
}

// AddWeighInWithTimestamps adds a weigh-in with explicit local/GMT timestamps.
func (c *Client) AddWeighInWithTimestamps(ctx context.Context, weight float64, unitKey, dateTimestamp, gmtTimestamp string) (any, error) {
	if err := ValidateWeightUnit(unitKey); err != nil {
		return nil, err
	}
	if err := ValidateWeight(weight); err != nil {
		return nil, err
	}
	local, gmt, err := weighInTimestamps(dateTimestamp, gmtTimestamp)
	if err != nil {
		return nil, err
	}
	return c.DoJSON(ctx, Req{
		Method: http.MethodPost,
		Path:   weightPath + "/user-weight",
		JSON:   weighInPayload(weight, unitKey, local, gmt),
	})
}

// --- data management ---

// BodyComposition is one body-composition measurement. Pointers distinguish
// "not provided" from a real zero.
type BodyComposition struct {
	Weight            float64
	PercentFat        *float64
	PercentHydration  *float64
	VisceralFatMass   *float64
	BoneMass          *float64
	MuscleMass        *float64
	BasalMet          *float64
	ActiveMet         *float64
	PhysiqueRating    *int
	MetabolicAge      *float64
	VisceralFatRating *int
	BMI               *float64
}

// AddBodyComposition uploads a weight-scale FIT file for a date.
func (c *Client) AddBodyComposition(ctx context.Context, date string, bc BodyComposition) (any, error) {
	d, err := ValidateDate(date, "date")
	if err != nil {
		return nil, err
	}
	if math.IsNaN(bc.Weight) || bc.Weight <= 0 {
		return nil, fmt.Errorf("weight must be positive, got: %v", bc.Weight)
	}
	dt, err := time.ParseInLocation("2006-01-02", d, time.Local)
	if err != nil {
		return nil, err
	}
	data := encodeWeightFIT(dt, bc)
	return c.Upload(ctx, uploadServicePath, "file", "body_composition.fit", data)
}

// SetBloodPressure records a manual blood pressure measurement.
func (c *Client) SetBloodPressure(ctx context.Context, systolic, diastolic, pulse int, notes *string) (any, error) {
	if err := ValidateBloodPressure(systolic, diastolic, pulse); err != nil {
		return nil, err
	}
	now := time.Now()
	var note any
	if notes != nil {
		note = *notes
	}
	return c.DoJSON(ctx, Req{
		Method: http.MethodPost,
		Path:   bloodPressureLogPath,
		JSON: map[string]any{
			"measurementTimestampLocal": formatMillis(now),
			"measurementTimestampGMT":   formatMillis(now.UTC()),
			"systolic":                  systolic,
			"diastolic":                 diastolic,
			"pulse":                     pulse,
			"sourceType":                "MANUAL",
			"notes":                     note,
		},
	})
}

// hydrationTimestamp validates a hydration amount, date and timestamp and
// returns the normalized date and parsed local timestamp.
func hydrationTimestamp(valueInML int, cdate, timestamp string) (string, time.Time, error) {
	if math.Abs(float64(valueInML)) > 10000 {
		return "", time.Time{}, fmt.Errorf("value_in_ml seems unreasonably high (>10000ml)")
	}
	d, err := ValidateDate(cdate, "cdate")
	if err != nil {
		return "", time.Time{}, err
	}
	ts, err := parseSoftTime(timestamp, time.Local)
	if err != nil {
		return "", time.Time{}, err
	}
	if ts.Format("2006-01-02") != d {
		return "", time.Time{}, fmt.Errorf("timestamp date (%s) doesn't match cdate (%s)", ts.Format("2006-01-02"), d)
	}
	return d, ts, nil
}

// AddHydrationData logs a hydration amount for a local timestamp.
func (c *Client) AddHydrationData(ctx context.Context, valueInML int, cdate, timestamp string) (any, error) {
	d, ts, err := hydrationTimestamp(valueInML, cdate, timestamp)
	if err != nil {
		return nil, err
	}
	return c.DoJSON(ctx, Req{
		Method: http.MethodPut,
		Path:   hydrationLogPath,
		JSON: map[string]any{
			"calendarDate":   d,
			"timestampLocal": formatMillis(ts),
			"valueInML":      valueInML,
		},
	})
}

// --- women's health ---

// GetPregnancySummary returns the pregnancy snapshot.
func (c *Client) GetPregnancySummary(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, pregnancySnapshotPath, nil)
}

// GetMenstrualDataForDate returns menstrual data for one day.
func (c *Client) GetMenstrualDataForDate(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "fordate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, menstrualDayviewPath+"/"+d, nil)
}

// GetMenstrualCalendarData returns cycle summaries overlapping a date range.
func (c *Client) GetMenstrualCalendarData(ctx context.Context, startDate, endDate string) (any, error) {
	start, err := ValidateDate(startDate, "startdate")
	if err != nil {
		return nil, err
	}
	end, err := ValidateDate(endDate, "enddate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, menstrualCalendarPath+"/"+start+"/"+end, nil)
}

// --- courses ---

// GetCourses returns every saved course.
func (c *Client) GetCourses(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, courseServicePath, nil)
}

// GetCourse returns one course's full details.
func (c *Client) GetCourse(ctx context.Context, id int64) (any, error) {
	return c.GetJSON(ctx, fmt.Sprintf("%s/%d", courseServicePath, id), nil)
}

// ImportCourse parses a GPX file server-side.
func (c *Client) ImportCourse(ctx context.Context, filename string, data []byte) (any, error) {
	return c.uploadFile(ctx, courseImportPath, "file", filename, "application/gpx+xml", data)
}

// SaveCourse creates a course from a parsed skeleton.
func (c *Client) SaveCourse(ctx context.Context, payload any) (any, error) {
	return c.DoJSON(ctx, Req{Method: http.MethodPost, Path: courseServicePath, JSON: payload})
}

// DeleteCourse permanently deletes a course.
func (c *Client) DeleteCourse(ctx context.Context, id int64) (any, error) {
	return c.DoJSON(ctx, Req{Method: http.MethodDelete, Path: fmt.Sprintf("%s/%d", courseServicePath, id)})
}

func (c *Client) uploadFile(ctx context.Context, path, field, filename, contentType string, data []byte) (any, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, field, filename))
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	part, err := mw.CreatePart(header)
	if err != nil {
		return nil, fmt.Errorf("garmin: build multipart form: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return nil, fmt.Errorf("garmin: write multipart form: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("garmin: finish multipart form: %w", err)
	}
	return c.DoJSON(ctx, Req{
		Method:      http.MethodPost,
		Path:        path,
		Body:        buf.Bytes(),
		ContentType: mw.FormDataContentType(),
	})
}

// --- FIT weight encoder (ported from garminconnect.fit) ---

const (
	fitHeaderSize     = 12
	fitProtocol       = 16
	fitProfileVersion = 108
	fitEpochOffset    = 631065600
)

type fitBaseType struct {
	size    int
	field   byte
	invalid uint64
}

var (
	fitEnum    = fitBaseType{size: 1, field: 0x00, invalid: 0xFF}
	fitUint8   = fitBaseType{size: 1, field: 0x02, invalid: 0xFF}
	fitUint16  = fitBaseType{size: 2, field: 0x84, invalid: 0xFFFF}
	fitUint32  = fitBaseType{size: 4, field: 0x86, invalid: 0xFFFFFFFF}
	fitUint32z = fitBaseType{size: 4, field: 0x8C, invalid: 0x00000000}
)

type fitValue struct {
	num   byte
	base  fitBaseType
	value *float64
	scale float64
}

func fitNumber(v float64) *float64 { return &v }

func encodeFitValue(v fitValue) []byte {
	raw := v.base.invalid
	if v.value != nil {
		scale := v.scale
		if scale == 0 {
			scale = 1
		}
		raw = uint64(int64(*v.value * scale))
	}
	out := make([]byte, v.base.size)
	for i := 0; i < v.base.size; i++ {
		out[i] = byte(raw >> (8 * i))
	}
	return out
}

func fitDefinition(msgType byte, msgNum uint16, fields []fitValue) []byte {
	var buf bytes.Buffer
	buf.WriteByte(0x40 | msgType)
	buf.Write([]byte{0, 0, byte(msgNum), byte(msgNum >> 8), byte(len(fields))})
	for _, field := range fields {
		buf.Write([]byte{field.num, byte(field.base.size), field.base.field})
	}
	return buf.Bytes()
}

func fitRecord(msgType byte, fields []fitValue) []byte {
	var buf bytes.Buffer
	buf.WriteByte(msgType)
	for _, field := range fields {
		buf.Write(encodeFitValue(field))
	}
	return buf.Bytes()
}

func fitTimestamp(t time.Time) float64 {
	return float64(t.Unix() - fitEpochOffset)
}

func encodeWeightFIT(timestamp time.Time, bc BodyComposition) []byte {
	var buf bytes.Buffer

	fileID := []fitValue{
		{num: 3, base: fitUint32z},
		{num: 4, base: fitUint32, value: fitNumber(fitTimestamp(timestamp))},
		{num: 1, base: fitUint16},
		{num: 2, base: fitUint16},
		{num: 5, base: fitUint16},
		{num: 0, base: fitEnum, value: fitNumber(9)}, // weight file type
	}
	buf.Write(fitDefinition(0, 0, fileID))
	buf.Write(fitRecord(0, fileID))

	creator := []fitValue{{num: 0, base: fitUint16}, {num: 1, base: fitUint8}}
	buf.Write(fitDefinition(1, 49, creator))
	buf.Write(fitRecord(1, creator))

	device := []fitValue{
		{num: 253, base: fitUint32, value: fitNumber(fitTimestamp(timestamp))},
		{num: 3, base: fitUint32z},
		{num: 7, base: fitUint32},
		{num: 8, base: fitUint32},
		{num: 2, base: fitUint16},
		{num: 4, base: fitUint16},
		{num: 5, base: fitUint16},
		{num: 10, base: fitUint16},
		{num: 0, base: fitUint8},
		{num: 1, base: fitUint8},
		{num: 6, base: fitUint8},
		{num: 11, base: fitUint8},
	}
	buf.Write(fitDefinition(2, 23, device))
	buf.Write(fitRecord(2, device))

	weight := []fitValue{
		{num: 253, base: fitUint32, value: fitNumber(fitTimestamp(timestamp))},
		{num: 0, base: fitUint16, value: fitNumber(bc.Weight), scale: 100},
		{num: 1, base: fitUint16, value: bc.PercentFat, scale: 100},
		{num: 2, base: fitUint16, value: bc.PercentHydration, scale: 100},
		{num: 3, base: fitUint16, value: bc.VisceralFatMass, scale: 100},
		{num: 4, base: fitUint16, value: bc.BoneMass, scale: 100},
		{num: 5, base: fitUint16, value: bc.MuscleMass, scale: 100},
		{num: 7, base: fitUint16, value: bc.BasalMet, scale: 4},
		{num: 9, base: fitUint16, value: bc.ActiveMet, scale: 4},
		{num: 8, base: fitUint8, value: intPointerAsFloat(bc.PhysiqueRating), scale: 1},
		{num: 10, base: fitUint8, value: bc.MetabolicAge, scale: 1},
		{num: 11, base: fitUint8, value: intPointerAsFloat(bc.VisceralFatRating), scale: 1},
		{num: 13, base: fitUint16, value: bc.BMI, scale: 10},
	}
	buf.Write(fitDefinition(3, 30, weight))
	buf.Write(fitRecord(3, weight))

	data := buf.Bytes()
	var out bytes.Buffer
	out.Write([]byte{fitHeaderSize, fitProtocol, byte(fitProfileVersion), byte(fitProfileVersion >> 8)})
	dataSize := uint32(len(data))
	out.Write([]byte{byte(dataSize), byte(dataSize >> 8), byte(dataSize >> 16), byte(dataSize >> 24)})
	out.WriteString(".FIT")
	out.Write(data)
	crc := uint16(0)
	for _, b := range out.Bytes() {
		crc = fitCRC(crc, b)
	}
	out.Write([]byte{byte(crc), byte(crc >> 8)})
	return out.Bytes()
}

func intPointerAsFloat(v *int) *float64 {
	if v == nil {
		return nil
	}
	f := float64(*v)
	return &f
}

func fitCRC(crc uint16, b byte) uint16 {
	table := [16]uint16{
		0x0000, 0xCC01, 0xD801, 0x1400, 0xF001, 0x3C00, 0x2800, 0xE401,
		0xA001, 0x6C00, 0x7800, 0xB401, 0x5000, 0x9C01, 0x8801, 0x4400,
	}
	tmp := table[crc&0xF]
	crc = (crc >> 4) & 0x0FFF
	crc = crc ^ tmp ^ table[b&0xF]
	tmp = table[crc&0xF]
	crc = (crc >> 4) & 0x0FFF
	return crc ^ tmp ^ table[(b>>4)&0xF]
}
