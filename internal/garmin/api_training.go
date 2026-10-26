package garmin

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

func trainingList(value any) []any {
	list, _ := value.([]any)
	return list
}

func trainingTruthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case string:
		return v != ""
	case bool:
		return v
	case map[string]any:
		return len(v) > 0
	case []any:
		return len(v) > 0
	case float64:
		return v != 0
	case int:
		return v != 0
	}
	return true
}

const (
	progressSummaryPath  = "/fitnessstats-service/activity"
	hillScorePath        = "/metrics-service/metrics/hillscore"
	enduranceScorePath   = "/metrics-service/metrics/endurancescore"
	hrvPath              = "/hrv-service/hrv"
	fitnessAgePath       = "/fitnessage-service/fitnessage"
	trainingStatusPath   = "/metrics-service/metrics/trainingstatus/aggregated"
	biometricPath        = "/biometric-service/biometric"
	biometricStatsPath   = "/biometric-service/stats"
	maxMetricsPath       = "/metrics-service/metrics/maxmet/daily"
	runningTolerancePath = "/metrics-service/metrics/runningtolerance/stats"
	devicesPath          = "/device-service/deviceregistration/devices"
	requestReloadPath    = "/wellness-service/wellness/epoch/request"
)

// GetProgressSummaryBetweenDates returns the fitness-stats summary for a metric.
func (c *Client) GetProgressSummaryBetweenDates(ctx context.Context, startDate, endDate, metric string) (any, error) {
	start, err := ValidateDate(startDate, "startdate")
	if err != nil {
		return nil, err
	}
	end, err := ValidateDate(endDate, "enddate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, progressSummaryPath, url.Values{
		"startDate":                 {start},
		"endDate":                   {end},
		"aggregation":               {"lifetime"},
		"groupByParentActivityType": {"True"},
		"metric":                    {metric},
	})
}

// GetHillScore returns daily hill scores between two dates.
func (c *Client) GetHillScore(ctx context.Context, startDate, endDate string) (any, error) {
	start, err := ValidateDate(startDate, "startdate")
	if err != nil {
		return nil, err
	}
	end, err := ValidateDate(endDate, "enddate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, hillScorePath+"/stats", url.Values{
		"startDate":   {start},
		"endDate":     {end},
		"aggregation": {"daily"},
	})
}

// GetEnduranceScore returns weekly endurance scores between two dates.
func (c *Client) GetEnduranceScore(ctx context.Context, startDate, endDate string) (any, error) {
	start, err := ValidateDate(startDate, "startdate")
	if err != nil {
		return nil, err
	}
	end, err := ValidateDate(endDate, "enddate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, enduranceScorePath+"/stats", url.Values{
		"startDate":   {start},
		"endDate":     {end},
		"aggregation": {"weekly"},
	})
}

// GetHRVData returns the heart-rate variability payload for a date.
func (c *Client) GetHRVData(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "cdate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, hrvPath+"/"+d, nil)
}

// GetFitnessAgeData returns the fitness-age payload for a date.
func (c *Client) GetFitnessAgeData(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "cdate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, fitnessAgePath+"/"+d, nil)
}

// GetTrainingStatus returns the aggregated training-status payload for a date.
func (c *Client) GetTrainingStatus(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "cdate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, trainingStatusPath+"/"+d, nil)
}

// GetDevices returns the account's registered devices.
func (c *Client) GetDevices(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, devicesPath, nil)
}

// GetCyclingFTP returns the latest cycling Functional Threshold Power estimate.
func (c *Client) GetCyclingFTP(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, biometricPath+"/latestFunctionalThresholdPower/CYCLING", nil)
}

// GetLactateThresholdLatest combines the latest speed/heart-rate and power
// readings the same way garminconnect does.
func (c *Client) GetLactateThresholdLatest(ctx context.Context) (any, error) {
	power, err := c.GetJSON(ctx, biometricPath+"/powerToWeight/latest/"+time.Now().Format("2006-01-02"), url.Values{
		"sport": {"Running"},
	})
	if err != nil {
		return nil, err
	}
	speedAndHeartRate, err := c.GetJSON(ctx, biometricPath+"/latestLactateThreshold", nil)
	if err != nil {
		return nil, err
	}

	powerData := map[string]any{}
	if list, ok := power.([]any); ok && len(list) > 0 {
		if m, ok := list[0].(map[string]any); ok {
			powerData = m
		}
	} else if m, ok := power.(map[string]any); ok {
		powerData = m
	}

	combined := map[string]any{
		"userProfilePK":    nil,
		"version":          nil,
		"calendarDate":     nil,
		"sequence":         nil,
		"speed":            nil,
		"heartRate":        nil,
		"heartRateCycling": nil,
	}
	for _, item := range trainingList(speedAndHeartRate) {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if speed := entry["speed"]; speed != nil {
			combined["userProfilePK"] = entry["userProfilePK"]
			combined["version"] = entry["version"]
			combined["calendarDate"] = entry["calendarDate"]
			combined["sequence"] = entry["sequence"]
			combined["speed"] = speed
		}
		hr := entry["heartRate"]
		if !trainingTruthy(hr) {
			hr = entry["hearRate"]
		}
		if hr != nil {
			combined["heartRate"] = hr
		}
		if hrc := entry["heartRateCycling"]; hrc != nil {
			combined["heartRateCycling"] = hrc
		}
	}
	return map[string]any{
		"speed_and_heart_rate": combined,
		"power":                powerData,
	}, nil
}

// GetLactateThresholdRange returns speed, heart-rate and power history.
func (c *Client) GetLactateThresholdRange(ctx context.Context, startDate, endDate, aggregation string) (any, error) {
	start, err := ValidateDate(startDate, "start_date")
	if err != nil {
		return nil, err
	}
	end, err := ValidateDate(endDate, "end_date")
	if err != nil {
		return nil, err
	}
	if aggregation == "" {
		aggregation = "daily"
	}
	switch aggregation {
	case "daily", "weekly", "monthly", "yearly":
	default:
		return nil, fmt.Errorf("aggregation must be one of daily, weekly, monthly, yearly")
	}
	query := func() url.Values {
		return url.Values{
			"sport":               {"RUNNING"},
			"aggregation":         {aggregation},
			"aggregationStrategy": {"LATEST"},
		}
	}
	speed, err := c.GetJSON(ctx, biometricStatsPath+"/lactateThresholdSpeed/range/"+start+"/"+end, query())
	if err != nil {
		return nil, err
	}
	heartRate, err := c.GetJSON(ctx, biometricStatsPath+"/lactateThresholdHeartRate/range/"+start+"/"+end, query())
	if err != nil {
		return nil, err
	}
	power, err := c.GetJSON(ctx, biometricStatsPath+"/functionalThresholdPower/range/"+start+"/"+end, query())
	if err != nil {
		return nil, err
	}
	return map[string]any{"speed": speed, "heart_rate": heartRate, "power": power}, nil
}

// RequestReload asks Garmin to reload epoch data for a date.
func (c *Client) RequestReload(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "cdate")
	if err != nil {
		return nil, err
	}
	return c.DoJSON(ctx, Req{Method: http.MethodPost, Path: requestReloadPath + "/" + d})
}

// GetRunningTolerance returns the running-tolerance series for a range.
func (c *Client) GetRunningTolerance(ctx context.Context, startDate, endDate, aggregation string) (any, error) {
	start, err := ValidateDate(startDate, "startdate")
	if err != nil {
		return nil, err
	}
	end, err := ValidateDate(endDate, "enddate")
	if err != nil {
		return nil, err
	}
	if aggregation != "daily" && aggregation != "weekly" {
		return nil, fmt.Errorf("invalid aggregation '%s', must be 'daily' or 'weekly'", aggregation)
	}
	return c.GetJSON(ctx, runningTolerancePath, url.Values{
		"startDate":   {start},
		"endDate":     {end},
		"aggregation": {aggregation},
	})
}

// GetMaxMetrics returns the max-metrics payload for one date.
func (c *Client) GetMaxMetrics(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "cdate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, maxMetricsPath+"/"+d+"/"+d, nil)
}

// GetMaxMetricsRange returns the max-metrics payload for a date range.
func (c *Client) GetMaxMetricsRange(ctx context.Context, startDate, endDate string) (any, error) {
	start, err := ValidateDate(startDate, "startdate")
	if err != nil {
		return nil, err
	}
	end, err := ValidateDate(endDate, "enddate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, maxMetricsPath+"/"+start+"/"+end, nil)
}
