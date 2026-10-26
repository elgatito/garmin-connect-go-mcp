package garmin

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"
)

const (
	dailySummaryPath       = "/usersummary-service/usersummary/daily"
	dailyStatsPath         = "/usersummary-service/stats/daily"
	dailyStepsStatsPath    = "/usersummary-service/stats/steps/daily"
	weeklyStepsStatsPath   = "/usersummary-service/stats/steps/weekly"
	weeklyStressStatsPath  = "/usersummary-service/stats/stress/weekly"
	weeklyIntensityPath    = "/usersummary-service/stats/im/weekly"
	stepsChartPath         = "/wellness-service/wellness/dailySummaryChart"
	floorsChartPath        = "/wellness-service/wellness/floorsChartData/daily"
	heartRatesPath         = "/wellness-service/wellness/dailyHeartRate"
	weightPath             = "/weight-service"
	bodyBatteryPath        = "/wellness-service/wellness/bodyBattery/reports/daily"
	bodyBatteryEventsPath  = "/wellness-service/wellness/bodyBattery/events"
	bloodPressurePath      = "/bloodpressure-service/bloodpressure/range"
	hydrationPath          = "/usersummary-service/usersummary/hydration/daily"
	respirationPath        = "/wellness-service/wellness/daily/respiration"
	spo2Path               = "/wellness-service/wellness/daily/spo2"
	dailyStressPath        = "/wellness-service/wellness/dailyStress"
	dailyEventsPath        = "/wellness-service/wellness/dailyEvents"
	dailySleepPath         = "/wellness-service/wellness/dailySleepData"
	lifestyleLoggingPath   = "/lifestylelogging-service/dailyLog"
	rhrPath                = "/userstats-service/wellness/daily"
	trainingReadinessPath  = "/metrics-service/metrics/trainingreadiness"
	activitiesSearchPath   = "/activitylist-service/activities/search/activities"
	nutritionLogsRangePath = "/nutrition-service/food/logs/range"

	maxActivityLimit = 1000
)

// isEmptyJSON mirrors Python truthiness for decoded Garmin responses.
func isEmptyJSON(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case map[string]any:
		return len(x) == 0
	case []any:
		return len(x) == 0
	}
	return false
}

// requireDisplayName returns the profile display name Garmin paths need.
func (c *Client) requireDisplayName(ctx context.Context) (string, error) {
	social, err := c.socialProfile(ctx)
	if err != nil {
		return "", err
	}
	name := stringField(social, "displayName")
	if name == "" {
		return "", errors.New("Display name is not set. This usually means your Garmin profile is incomplete (new account with no display name configured). Please set a display name at https://connect.garmin.com and try again.")
	}
	return name, nil
}

func parseGarminDate(s string) time.Time {
	d, _ := time.Parse("2006-01-02", s)
	return d
}

// GetStats returns the daily activity summary, the same payload as
// GetUserSummary.
func (c *Client) GetStats(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "cdate")
	if err != nil {
		return nil, err
	}
	return c.GetUserSummary(ctx, d)
}

// GetUserSummary returns the daily activity summary.
func (c *Client) GetUserSummary(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "cdate")
	if err != nil {
		return nil, err
	}
	name, err := c.requireDisplayName(ctx)
	if err != nil {
		return nil, err
	}
	v, err := c.GetJSON(ctx, dailySummaryPath+"/"+name, url.Values{"calendarDate": {d}})
	if err != nil {
		return nil, err
	}
	if isEmptyJSON(v) {
		return nil, errors.New("No data received from server")
	}
	if m, ok := v.(map[string]any); ok && m["privacyProtected"] == true {
		return nil, errors.New("Authentication error")
	}
	return v, nil
}

// GetStepsData returns the 15-minute step intervals for a date; nil means no
// data and is returned as an empty list.
func (c *Client) GetStepsData(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "cdate")
	if err != nil {
		return nil, err
	}
	name, err := c.requireDisplayName(ctx)
	if err != nil {
		return nil, err
	}
	v, err := c.GetJSON(ctx, stepsChartPath+"/"+name, url.Values{"date": {d}})
	if err != nil {
		return nil, err
	}
	if v == nil {
		return []any{}, nil
	}
	return v, nil
}

// GetFloors returns floors climbed for a date.
func (c *Client) GetFloors(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "cdate")
	if err != nil {
		return nil, err
	}
	v, err := c.GetJSON(ctx, floorsChartPath+"/"+d, nil)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, errors.New("No floors data received")
	}
	return v, nil
}

// GetDailySteps returns per-day step totals, chunked under Garmin's 28-day cap.
func (c *Client) GetDailySteps(ctx context.Context, start, end string) (any, error) {
	startDate, err := ValidateDate(start, "start")
	if err != nil {
		return nil, err
	}
	endDate, err := ValidateDate(end, "end")
	if err != nil {
		return nil, err
	}
	startDay := parseGarminDate(startDate)
	endDay := parseGarminDate(endDate)
	if startDay.After(endDay) {
		return nil, errors.New("start date cannot be after end date")
	}

	if int(endDay.Sub(startDay).Hours()/24)+1 <= 28 {
		return c.GetJSON(ctx, dailyStepsStatsPath+"/"+startDate+"/"+endDate, nil)
	}

	var all []any
	for current := startDay; !current.After(endDay); current = current.AddDate(0, 0, 28) {
		chunkEnd := current.AddDate(0, 0, 27)
		if chunkEnd.After(endDay) {
			chunkEnd = endDay
		}
		v, err := c.GetJSON(ctx, dailyStepsStatsPath+"/"+current.Format("2006-01-02")+"/"+chunkEnd.Format("2006-01-02"), nil)
		if err != nil {
			return nil, err
		}
		if list, ok := v.([]any); ok && len(list) > 0 {
			all = append(all, list...)
		}
	}
	return all, nil
}

// GetWeeklySteps returns weekly step aggregates ending at end.
func (c *Client) GetWeeklySteps(ctx context.Context, end string, weeks int) (any, error) {
	endDate, err := ValidateDate(end, "end")
	if err != nil {
		return nil, err
	}
	if weeks, err = ValidatePositiveInteger(weeks, "weeks"); err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, fmt.Sprintf("%s/%s/%d", weeklyStepsStatsPath, endDate, weeks), nil)
}

// GetWeeklyStress returns weekly stress aggregates ending at end.
func (c *Client) GetWeeklyStress(ctx context.Context, end string, weeks int) (any, error) {
	endDate, err := ValidateDate(end, "end")
	if err != nil {
		return nil, err
	}
	if weeks, err = ValidatePositiveInteger(weeks, "weeks"); err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, fmt.Sprintf("%s/%s/%d", weeklyStressStatsPath, endDate, weeks), nil)
}

// GetWeeklyIntensityMinutes returns weekly intensity-minute aggregates.
func (c *Client) GetWeeklyIntensityMinutes(ctx context.Context, start, end string) (any, error) {
	startDate, err := ValidateDate(start, "start")
	if err != nil {
		return nil, err
	}
	endDate, err := ValidateDate(end, "end")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, weeklyIntensityPath+"/"+startDate+"/"+endDate, nil)
}

// GetHeartRates returns the full heart-rate time series for a date.
func (c *Client) GetHeartRates(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "cdate")
	if err != nil {
		return nil, err
	}
	name, err := c.requireDisplayName(ctx)
	if err != nil {
		return nil, err
	}
	v, err := c.GetJSON(ctx, heartRatesPath+"/"+name, url.Values{"date": {d}})
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, errors.New("No heart rate data received")
	}
	return v, nil
}

// GetStatsAndBody merges the daily stats with the body composition total
// averages.
func (c *Client) GetStatsAndBody(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "cdate")
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
	merged, ok := stats.(map[string]any)
	if !ok {
		return stats, nil
	}
	out := make(map[string]any, len(merged))
	for k, v := range merged {
		out[k] = v
	}
	if bodyMap, ok := body.(map[string]any); ok {
		if avg, ok := bodyMap["totalAverage"].(map[string]any); ok {
			for k, v := range avg {
				out[k] = v
			}
		}
	}
	return out, nil
}

// GetBodyComposition returns body composition for a start date through an
// optional end date.
func (c *Client) GetBodyComposition(ctx context.Context, startDate, endDate string) (any, error) {
	start, err := ValidateDate(startDate, "startdate")
	if err != nil {
		return nil, err
	}
	end := start
	if endDate != "" {
		if end, err = ValidateDate(endDate, "enddate"); err != nil {
			return nil, err
		}
	}
	if parseGarminDate(start).After(parseGarminDate(end)) {
		return nil, errors.New("startdate cannot be after enddate")
	}
	return c.GetJSON(ctx, weightPath+"/weight/dateRange", url.Values{
		"startDate": {start},
		"endDate":   {end},
	})
}

// GetBodyBattery returns body battery values per day.
func (c *Client) GetBodyBattery(ctx context.Context, startDate, endDate string) (any, error) {
	start, err := ValidateDate(startDate, "startdate")
	if err != nil {
		return nil, err
	}
	end := start
	if endDate != "" {
		if end, err = ValidateDate(endDate, "enddate"); err != nil {
			return nil, err
		}
	}
	return c.GetJSON(ctx, bodyBatteryPath, url.Values{
		"startDate": {start},
		"endDate":   {end},
	})
}

// GetBodyBatteryEvents returns body battery events for a date.
func (c *Client) GetBodyBatteryEvents(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "cdate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, bodyBatteryEventsPath+"/"+d, nil)
}

// GetBloodPressure returns blood pressure readings for a date range.
func (c *Client) GetBloodPressure(ctx context.Context, startDate, endDate string) (any, error) {
	start, err := ValidateDate(startDate, "startdate")
	if err != nil {
		return nil, err
	}
	end := start
	if endDate != "" {
		if end, err = ValidateDate(endDate, "enddate"); err != nil {
			return nil, err
		}
	}
	return c.GetJSON(ctx, bloodPressurePath+"/"+start+"/"+end, url.Values{"includeAll": {"true"}})
}

// GetHydrationData returns the hydration log for a date.
func (c *Client) GetHydrationData(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "cdate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, hydrationPath+"/"+d, nil)
}

// GetRespirationData returns the respiration time series for a date.
func (c *Client) GetRespirationData(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "cdate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, respirationPath+"/"+d, nil)
}

// GetSpo2Data returns the SpO2 payload for a date.
func (c *Client) GetSpo2Data(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "cdate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, spo2Path+"/"+d, nil)
}

// GetAllDayStress returns the all-day stress payload for a date.
func (c *Client) GetAllDayStress(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "cdate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, dailyStressPath+"/"+d, nil)
}

// GetAllDayEvents returns daily wellness events for a date.
func (c *Client) GetAllDayEvents(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "cdate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, dailyEventsPath, url.Values{"calendarDate": {d}})
}

// GetSleepData returns the full sleep payload for a date.
func (c *Client) GetSleepData(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "cdate")
	if err != nil {
		return nil, err
	}
	name, err := c.requireDisplayName(ctx)
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, dailySleepPath+"/"+name, url.Values{
		"date":                  {d},
		"nonSleepBufferMinutes": {"60"},
	})
}

// GetStressData returns the stress time series for a date.
func (c *Client) GetStressData(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "cdate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, dailyStressPath+"/"+d, nil)
}

// GetLifestyleLoggingData returns lifestyle logging data for a date.
func (c *Client) GetLifestyleLoggingData(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "cdate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, lifestyleLoggingPath+"/"+d, nil)
}

// GetRHRDay returns resting heart rate for a date.
func (c *Client) GetRHRDay(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "cdate")
	if err != nil {
		return nil, err
	}
	name, err := c.requireDisplayName(ctx)
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, rhrPath+"/"+name, url.Values{
		"fromDate":  {d},
		"untilDate": {d},
		"metricId":  {"60"},
	})
}

// GetTrainingReadiness returns the training-readiness snapshots for a date.
func (c *Client) GetTrainingReadiness(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "cdate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, trainingReadinessPath+"/"+d, nil)
}

// GetMorningTrainingReadiness returns the AFTER_WAKEUP_RESET snapshot, falling
// back to the first entry when the device does not populate inputContext.
func (c *Client) GetMorningTrainingReadiness(ctx context.Context, date string) (any, error) {
	data, err := c.GetTrainingReadiness(ctx, date)
	if err != nil {
		return nil, err
	}
	if isEmptyJSON(data) {
		return nil, nil
	}
	if list, ok := data.([]any); ok {
		for _, item := range list {
			if m, ok := item.(map[string]any); ok && m["inputContext"] == "AFTER_WAKEUP_RESET" {
				return m, nil
			}
		}
		if len(list) > 0 {
			return list[0], nil
		}
		return nil, nil
	}
	return data, nil
}

// GetActivities returns recent activities.
func (c *Client) GetActivities(ctx context.Context, start, limit int) (any, error) {
	return c.GetActivitiesByType(ctx, start, limit, "")
}

// GetDailyStatsRange fetches per-day stats of statsType from the daily stats
// endpoint. The endpoint accepts at most 28 days per request.
func (c *Client) GetDailyStatsRange(ctx context.Context, start, end, statsType string) (any, error) {
	return c.GetJSON(ctx, dailyStatsPath+"/"+start+"/"+end, url.Values{"statsType": {statsType}})
}

// GetNutritionLogsRange fetches the nutrition log range payload.
func (c *Client) GetNutritionLogsRange(ctx context.Context, start, end string) (any, error) {
	return c.GetJSON(ctx, nutritionLogsRangePath, url.Values{
		"startDate": {start},
		"endDate":   {end},
	})
}
