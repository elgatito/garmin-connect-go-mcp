package garmin

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// Port of the python-garminconnect health & wellness methods (GC/__init__.py).

// displayName is the profile display name; "None" mimics Python interpolating a missing one into the URL.
func (c *Client) displayName(ctx context.Context) (string, error) {
	p, err := c.Profile(ctx)
	if err != nil {
		return "", err
	}
	if p.DisplayName == "" {
		return "None", nil
	}
	return p.DisplayName, nil
}

// requireDisplayName is _require_display_name.
func (c *Client) requireDisplayName(ctx context.Context) (string, error) {
	p, err := c.Profile(ctx)
	if err != nil {
		return "", err
	}
	if p.DisplayName == "" {
		return "", errors.New("Display name is not set. This usually means your Garmin profile is incomplete " +
			"(new account with no display name configured). Please set a display name at https://connect.garmin.com and try again.")
	}
	return p.DisplayName, nil
}

// getByDate validates date (reported as name) and GETs prefix/date.
func (c *Client) getByDate(ctx context.Context, prefix, name, date string) (any, error) {
	d, err := ValidateDate(date, name)
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, prefix+"/"+d, nil)
}

// getByRange validates start and end and GETs prefix/start/end.
func (c *Client) getByRange(ctx context.Context, prefix, startName, endName, start, end string, q url.Values) (any, error) {
	s, err := ValidateDate(start, startName)
	if err != nil {
		return nil, err
	}
	e, err := ValidateDate(end, endName)
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, prefix+"/"+s+"/"+e, q)
}

// GetUserSummary returns the user's activity summary for cdate.
func (c *Client) GetUserSummary(ctx context.Context, cdate string) (any, error) {
	d, err := ValidateDate(cdate, "cdate")
	if err != nil {
		return nil, err
	}
	dn, err := c.requireDisplayName(ctx)
	if err != nil {
		return nil, err
	}
	v, err := c.GetJSON(ctx, "/usersummary-service/usersummary/daily/"+dn, url.Values{"calendarDate": {d}})
	if err != nil {
		return nil, err
	}
	m, _ := v.(map[string]any)
	if len(m) == 0 {
		return nil, errors.New("No data received from server")
	}
	if m["privacyProtected"] == true {
		return nil, errors.New("Authentication error")
	}
	return m, nil
}

// GetStats is GetUserSummary (kept for parity with the Python method names).
func (c *Client) GetStats(ctx context.Context, cdate string) (any, error) {
	return c.GetUserSummary(ctx, cdate)
}

// GetStepsData returns the 15-minute steps chart; an empty response is [].
func (c *Client) GetStepsData(ctx context.Context, cdate string) (any, error) {
	d, err := ValidateDate(cdate, "cdate")
	if err != nil {
		return nil, err
	}
	dn, err := c.requireDisplayName(ctx)
	if err != nil {
		return nil, err
	}
	v, err := c.GetJSON(ctx, "/wellness-service/wellness/dailySummaryChart/"+dn, url.Values{"date": {d}})
	if v == nil && err == nil {
		return []any{}, nil
	}
	return v, err
}

func (c *Client) GetFloors(ctx context.Context, cdate string) (any, error) {
	v, err := c.getByDate(ctx, "/wellness-service/wellness/floorsChartData/daily", "cdate", cdate)
	if v == nil && err == nil {
		return nil, errors.New("No floors data received")
	}
	return v, err
}

// GetDailySteps splits ranges over 28 days into 28-day requests and concatenates the lists.
func (c *Client) GetDailySteps(ctx context.Context, start, end string) (any, error) {
	const prefix = "/usersummary-service/stats/steps/daily"
	s, err := ValidateDate(start, "start")
	if err != nil {
		return nil, err
	}
	e, err := ValidateDate(end, "end")
	if err != nil {
		return nil, err
	}
	sd, _ := time.Parse(time.DateOnly, s)
	ed, _ := time.Parse(time.DateOnly, e)
	if sd.After(ed) {
		return nil, errors.New("start date cannot be after end date")
	}
	if int(ed.Sub(sd).Hours()/24)+1 <= 28 {
		return c.GetJSON(ctx, prefix+"/"+s+"/"+e, nil)
	}
	all := []any{}
	for cur := sd; !cur.After(ed); {
		chunkEnd := cur.AddDate(0, 0, 27)
		if chunkEnd.After(ed) {
			chunkEnd = ed
		}
		v, err := c.GetJSON(ctx, prefix+"/"+cur.Format(time.DateOnly)+"/"+chunkEnd.Format(time.DateOnly), nil)
		if err != nil {
			return nil, err
		}
		if l, ok := v.([]any); ok {
			all = append(all, l...)
		}
		cur = chunkEnd.AddDate(0, 0, 1)
	}
	return all, nil
}

func (c *Client) weekly(ctx context.Context, prefix, end string, weeks int) (any, error) {
	e, err := ValidateDate(end, "end")
	if err != nil {
		return nil, err
	}
	if _, err := ValidatePositiveInt(weeks, "weeks"); err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, prefix+"/"+e+"/"+strconv.Itoa(weeks), nil)
}

func (c *Client) GetWeeklySteps(ctx context.Context, end string, weeks int) (any, error) {
	return c.weekly(ctx, "/usersummary-service/stats/steps/weekly", end, weeks)
}

func (c *Client) GetWeeklyStress(ctx context.Context, end string, weeks int) (any, error) {
	return c.weekly(ctx, "/usersummary-service/stats/stress/weekly", end, weeks)
}

func (c *Client) GetWeeklyIntensityMinutes(ctx context.Context, start, end string) (any, error) {
	return c.getByRange(ctx, "/usersummary-service/stats/im/weekly", "start", "end", start, end, nil)
}

// GetHeartRates returns the day's heart-rate time series; an empty response is an error.
func (c *Client) GetHeartRates(ctx context.Context, cdate string) (any, error) {
	d, err := ValidateDate(cdate, "cdate")
	if err != nil {
		return nil, err
	}
	dn, err := c.displayName(ctx)
	if err != nil {
		return nil, err
	}
	v, err := c.GetJSON(ctx, "/wellness-service/wellness/dailyHeartRate/"+dn, url.Values{"date": {d}})
	if v == nil && err == nil {
		return nil, errors.New("No heart rate data received")
	}
	return v, err
}

// GetBodyComposition covers startdate..enddate; an empty enddate means the same day.
func (c *Client) GetBodyComposition(ctx context.Context, startdate, enddate string) (any, error) {
	s, err := ValidateDate(startdate, "startdate")
	if err != nil {
		return nil, err
	}
	e := s
	if enddate != "" {
		if e, err = ValidateDate(enddate, "enddate"); err != nil {
			return nil, err
		}
	}
	if s > e { // ISO dates compare lexically
		return nil, errors.New("startdate cannot be after enddate")
	}
	return c.GetJSON(ctx, "/weight-service/weight/dateRange", url.Values{"startDate": {s}, "endDate": {e}})
}

// GetStatsAndBody merges the day's stats with the body composition totalAverage.
func (c *Client) GetStatsAndBody(ctx context.Context, cdate string) (any, error) {
	d, err := ValidateDate(cdate, "cdate")
	if err != nil {
		return nil, err
	}
	stats, err := c.GetStats(ctx, d)
	if err != nil {
		return nil, err
	}
	body, err := c.GetBodyComposition(ctx, d, "")
	if err != nil {
		return nil, err
	}
	statsMap, _ := stats.(map[string]any)
	out := map[string]any{}
	for k, v := range statsMap {
		out[k] = v
	}
	bm, _ := body.(map[string]any)
	avg, _ := bm["totalAverage"].(map[string]any)
	for k, v := range avg {
		out[k] = v
	}
	return out, nil
}

func (c *Client) GetTrainingReadiness(ctx context.Context, cdate string) (any, error) {
	return c.getByDate(ctx, "/metrics-service/metrics/trainingreadiness", "cdate", cdate)
}

// GetMorningTrainingReadiness picks the AFTER_WAKEUP_RESET snapshot, else the first one; nil when there is none.
func (c *Client) GetMorningTrainingReadiness(ctx context.Context, cdate string) (any, error) {
	data, err := c.GetTrainingReadiness(ctx, cdate)
	if err != nil || isEmptyJSON(data) {
		return nil, err
	}
	list, ok := data.([]any)
	if !ok {
		return data, nil
	}
	for _, e := range list {
		if m, ok := e.(map[string]any); ok && m["inputContext"] == "AFTER_WAKEUP_RESET" {
			return m, nil
		}
	}
	return list[0], nil
}

// isEmptyJSON is Python falsiness for the shapes a readiness payload can take.
func isEmptyJSON(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

// GetBodyBattery returns body battery reports; an empty enddate means the same day.
func (c *Client) GetBodyBattery(ctx context.Context, startdate, enddate string) (any, error) {
	s, e, err := dayRange(startdate, enddate)
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, "/wellness-service/wellness/bodyBattery/reports/daily", url.Values{"startDate": {s}, "endDate": {e}})
}

// dayRange validates startdate and enddate (empty enddate defaults to startdate).
func dayRange(startdate, enddate string) (string, string, error) {
	s, err := ValidateDate(startdate, "startdate")
	if err != nil {
		return "", "", err
	}
	if enddate == "" {
		return s, s, nil
	}
	e, err := ValidateDate(enddate, "enddate")
	return s, e, err
}

func (c *Client) GetBodyBatteryEvents(ctx context.Context, cdate string) (any, error) {
	return c.getByDate(ctx, "/wellness-service/wellness/bodyBattery/events", "cdate", cdate)
}

// GetBloodPressure returns readings for startdate..enddate (empty enddate: same day).
func (c *Client) GetBloodPressure(ctx context.Context, startdate, enddate string) (any, error) {
	s, e, err := dayRange(startdate, enddate)
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, "/bloodpressure-service/bloodpressure/range/"+s+"/"+e, url.Values{"includeAll": {"True"}})
}

func (c *Client) GetRHRDay(ctx context.Context, cdate string) (any, error) {
	d, err := ValidateDate(cdate, "cdate")
	if err != nil {
		return nil, err
	}
	dn, err := c.requireDisplayName(ctx)
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, "/userstats-service/wellness/daily/"+dn,
		url.Values{"fromDate": {d}, "untilDate": {d}, "metricId": {"60"}})
}

func (c *Client) GetHydrationData(ctx context.Context, cdate string) (any, error) {
	return c.getByDate(ctx, "/usersummary-service/usersummary/hydration/daily", "cdate", cdate)
}

func (c *Client) GetSleepData(ctx context.Context, cdate string) (any, error) {
	d, err := ValidateDate(cdate, "cdate")
	if err != nil {
		return nil, err
	}
	dn, err := c.displayName(ctx)
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, "/wellness-service/wellness/dailySleepData/"+dn,
		url.Values{"date": {d}, "nonSleepBufferMinutes": {"60"}})
}

func (c *Client) GetStressData(ctx context.Context, cdate string) (any, error) {
	return c.getByDate(ctx, "/wellness-service/wellness/dailyStress", "cdate", cdate)
}

// GetAllDayStress hits the same endpoint as GetStressData.
func (c *Client) GetAllDayStress(ctx context.Context, cdate string) (any, error) {
	return c.GetStressData(ctx, cdate)
}

func (c *Client) GetRespirationData(ctx context.Context, cdate string) (any, error) {
	return c.getByDate(ctx, "/wellness-service/wellness/daily/respiration", "cdate", cdate)
}

func (c *Client) GetSpO2Data(ctx context.Context, cdate string) (any, error) {
	return c.getByDate(ctx, "/wellness-service/wellness/daily/spo2", "cdate", cdate)
}

func (c *Client) GetAllDayEvents(ctx context.Context, cdate string) (any, error) {
	d, err := ValidateDate(cdate, "cdate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, "/wellness-service/wellness/dailyEvents", url.Values{"calendarDate": {d}})
}

func (c *Client) GetLifestyleLoggingData(ctx context.Context, cdate string) (any, error) {
	return c.getByDate(ctx, "/lifestylelogging-service/dailyLog", "cdate", cdate)
}

// GetActivities lists recent activities (start offset, limit up to 1000); an empty response is [].
func (c *Client) GetActivities(ctx context.Context, start, limit int) (any, error) {
	if start < 0 {
		return nil, fmt.Errorf("start must be non-negative, got: %d", start)
	}
	if _, err := ValidatePositiveInt(limit, "limit"); err != nil {
		return nil, err
	}
	if limit > 1000 {
		return nil, errors.New("limit cannot exceed 1000")
	}
	v, err := c.GetJSON(ctx, "/activitylist-service/activities/search/activities",
		url.Values{"start": {strconv.Itoa(start)}, "limit": {strconv.Itoa(limit)}})
	if v == nil && err == nil {
		return []any{}, nil
	}
	return v, err
}
