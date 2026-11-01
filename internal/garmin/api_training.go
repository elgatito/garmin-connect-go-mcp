package garmin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"time"
)

// Port of the python-garminconnect training and performance methods (GC/__init__.py).

// MaxMetricsPath is garmin_connect_metrics_url: the training tools append a date range to it themselves.
const MaxMetricsPath = "/metrics-service/metrics/maxmet/daily"

const (
	fitnessStatsPath     = "/fitnessstats-service/activity"
	hillScorePath        = "/metrics-service/metrics/hillscore"
	enduranceScorePath   = "/metrics-service/metrics/endurancescore"
	runningTolerancePath = "/metrics-service/metrics/runningtolerance/stats"
	hrvPath              = "/hrv-service/hrv"
	trainingStatusPath   = "/metrics-service/metrics/trainingstatus/aggregated"
	fitnessAgePath       = "/fitnessage-service/fitnessage"
	biometricPath        = "/biometric-service/biometric"
	biometricStatsPath   = "/biometric-service/stats"
	devicesPath          = "/device-service/deviceregistration/devices"
	requestReloadPath    = "/wellness-service/wellness/epoch/request"
)

// GetProgressSummaryBetweenDates is get_progress_summary_between_dates (groupbyactivities is always true).
func (c *Client) GetProgressSummaryBetweenDates(ctx context.Context, start, end, metric string) (any, error) {
	s, err := ValidateDate(start, "startdate")
	if err != nil {
		return nil, err
	}
	e, err := ValidateDate(end, "enddate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, fitnessStatsPath, url.Values{
		"startDate": {s}, "endDate": {e}, "aggregation": {"lifetime"},
		"groupByParentActivityType": {"True"}, "metric": {metric},
	})
}

// HillScore is the get_hill_score payload plus the key order of periodAvgScore (Python takes its first value).
type HillScore struct {
	Data    any
	AvgKeys []string
}

// GetHillScore is get_hill_score; a nil end (Python's None) is the single-day query, any string is validated.
func (c *Client) GetHillScore(ctx context.Context, start string, end *string) (*HillScore, error) {
	var raw []byte
	var err error
	if end == nil {
		s, verr := ValidateDate(start, "startdate")
		if verr != nil {
			return nil, verr
		}
		raw, err = c.Do(ctx, Req{Method: http.MethodGet, Path: hillScorePath, Query: url.Values{"calendarDate": {s}}})
	} else {
		s, verr := ValidateDate(start, "startdate")
		if verr != nil {
			return nil, verr
		}
		e, verr := ValidateDate(*end, "enddate")
		if verr != nil {
			return nil, verr
		}
		raw, err = c.Do(ctx, Req{Method: http.MethodGet, Path: hillScorePath + "/stats",
			Query: url.Values{"aggregation": {"daily"}, "startDate": {s}, "endDate": {e}}})
	}
	if err != nil {
		return nil, err
	}
	hs := &HillScore{}
	if len(bytes.TrimSpace(raw)) == 0 {
		return hs, nil
	}
	if err := json.Unmarshal(raw, &hs.Data); err != nil {
		return nil, fmt.Errorf("invalid JSON from %s: %w", hillScorePath, err)
	}
	var order struct {
		Keys objectKeys `json:"periodAvgScore"`
	}
	_ = json.Unmarshal(raw, &order) // only the order is lost on a surprising shape
	hs.AvgKeys = order.Keys
	return hs, nil
}

// GetEnduranceScore is get_endurance_score; a nil end (Python's None) is the single-day query, any string is validated.
func (c *Client) GetEnduranceScore(ctx context.Context, start string, end *string) (any, error) {
	if end == nil {
		s, err := ValidateDate(start, "startdate")
		if err != nil {
			return nil, err
		}
		return c.GetJSON(ctx, enduranceScorePath, url.Values{"calendarDate": {s}})
	}
	return c.statsRange(ctx, enduranceScorePath+"/stats", start, *end, url.Values{"aggregation": {"weekly"}})
}

// statsRange validates startdate/enddate and GETs path with startDate, endDate and the extra query.
func (c *Client) statsRange(ctx context.Context, path, start, end string, q url.Values) (any, error) {
	s, err := ValidateDate(start, "startdate")
	if err != nil {
		return nil, err
	}
	e, err := ValidateDate(end, "enddate")
	if err != nil {
		return nil, err
	}
	q.Set("startDate", s)
	q.Set("endDate", e)
	return c.GetJSON(ctx, path, q)
}

// GetRunningTolerance is get_running_tolerance.
func (c *Client) GetRunningTolerance(ctx context.Context, start, end, aggregation string) (any, error) {
	s, err := ValidateDate(start, "startdate")
	if err != nil {
		return nil, err
	}
	e, err := ValidateDate(end, "enddate")
	if err != nil {
		return nil, err
	}
	if aggregation != "daily" && aggregation != "weekly" {
		return nil, fmt.Errorf("invalid aggregation '%s', must be 'daily' or 'weekly'", aggregation)
	}
	return c.GetJSON(ctx, runningTolerancePath, url.Values{"startDate": {s}, "endDate": {e}, "aggregation": {aggregation}})
}

func (c *Client) GetHRVData(ctx context.Context, cdate string) (any, error) {
	return c.getByDate(ctx, hrvPath, "cdate", cdate)
}

func (c *Client) GetFitnessAgeData(ctx context.Context, cdate string) (any, error) {
	return c.getByDate(ctx, fitnessAgePath, "cdate", cdate)
}

// GetMaxMetrics returns the max metrics (VO2 max, acclimation) for one day.
func (c *Client) GetMaxMetrics(ctx context.Context, cdate string) (any, error) {
	d, err := ValidateDate(cdate, "cdate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, MaxMetricsPath+"/"+d+"/"+d, nil)
}

// GetCyclingFTP returns the latest cycling FTP (a dict or a list of dicts).
func (c *Client) GetCyclingFTP(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, biometricPath+"/latestFunctionalThresholdPower/CYCLING", nil)
}

// GetDevices returns the registered devices.
func (c *Client) GetDevices(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, devicesPath, nil)
}

// RequestReload asks Garmin to reload the epoch data of one day.
func (c *Client) RequestReload(ctx context.Context, cdate string) (any, error) {
	d, err := ValidateDate(cdate, "cdate")
	if err != nil {
		return nil, err
	}
	return c.DoJSON(ctx, Req{Method: http.MethodPost, Path: requestReloadPath + "/" + d})
}

// ListActivitiesRaw is connectapi(garmin_connect_activities, params=q): the activity search with arbitrary params.
func (c *Client) ListActivitiesRaw(ctx context.Context, q url.Values) (any, error) {
	return c.GetJSON(ctx, activitiesSearchPath, q)
}

// GetLactateThreshold is get_lactate_threshold. latest ignores start and end; otherwise end defaults to today
// (the tool always passes both) and the aggregation is "daily".
func (c *Client) GetLactateThreshold(ctx context.Context, latest bool, start, end string) (any, error) {
	if latest {
		return c.latestLactateThreshold(ctx)
	}
	if start == "" {
		return nil, fmt.Errorf("you must either specify 'latest=True' or a start_date")
	}
	if end == "" {
		end = time.Now().Format(time.DateOnly)
	}
	s, err := ValidateDate(start, "start_date")
	if err != nil {
		return nil, err
	}
	e, err := ValidateDate(end, "end_date")
	if err != nil {
		return nil, err
	}
	q := url.Values{"sport": {"RUNNING"}, "aggregation": {"daily"}, "aggregationStrategy": {"LATEST"}}
	get := func(metric string) (any, error) {
		return c.GetJSON(ctx, biometricStatsPath+"/"+metric+"/range/"+s+"/"+e, q)
	}
	speed, err := get("lactateThresholdSpeed")
	if err != nil {
		return nil, err
	}
	hr, err := get("lactateThresholdHeartRate")
	if err != nil {
		return nil, err
	}
	power, err := get("functionalThresholdPower")
	if err != nil {
		return nil, err
	}
	return map[string]any{"speed": speed, "heart_rate": hr, "power": power}, nil
}

func (c *Client) latestLactateThreshold(ctx context.Context) (any, error) {
	power, err := c.GetJSON(ctx, biometricPath+"/powerToWeight/latest/"+time.Now().Format(time.DateOnly), url.Values{"sport": {"Running"}})
	if err != nil {
		return nil, err
	}
	powerDict := map[string]any{}
	switch p := power.(type) {
	case []any:
		if len(p) > 0 { // a non-dict first entry reads as empty
			if m, ok := p[0].(map[string]any); ok {
				powerDict = m
			}
		}
	case map[string]any:
		powerDict = p
	}
	entries, err := c.GetJSON(ctx, biometricPath+"/latestLactateThreshold", nil)
	if err != nil {
		return nil, err
	}
	combined := map[string]any{"userProfilePK": nil, "version": nil, "calendarDate": nil, "sequence": nil, "speed": nil, "heartRate": nil, "heartRateCycling": nil}
	// The endpoint returns a list of near-identical dicts; combine them.
	list, _ := entries.([]any)
	for _, e := range list {
		entry, _ := e.(map[string]any)
		if speed := entry["speed"]; speed != nil {
			for _, k := range []string{"userProfilePK", "version", "calendarDate", "sequence"} {
				v, ok := entry[k]
				if !ok {
					return nil, fmt.Errorf("'%s'", k) // Python KeyError text
				}
				combined[k] = v
			}
			combined["speed"] = speed
		}
		// Prefer the correct key; fall back to Garmin's historical typo ("hearRate").
		hr := entry["heartRate"]
		if hr == nil || hr == false || hr == 0.0 || hr == "" {
			hr = entry["hearRate"]
		}
		if hr != nil {
			combined["heartRate"] = hr
		}
		if hrc := entry["heartRateCycling"]; hrc != nil {
			combined["heartRateCycling"] = hrc
		}
	}
	return map[string]any{"speed_and_heart_rate": combined, "power": powerDict}, nil
}

// TrainingStatus is the get_training_status payload plus the order in which Garmin listed the devices of its
// two device-keyed maps (Python dicts keep it; Go maps do not, and the device selection depends on it).
type TrainingStatus struct {
	Data       any
	StatusKeys []string // mostRecentTrainingStatus.latestTrainingStatusData
	LoadKeys   []string // mostRecentTrainingLoadBalance.metricsTrainingLoadBalanceDTOMap
}

// GetTrainingStatus returns the aggregated training status of one day.
func (c *Client) GetTrainingStatus(ctx context.Context, cdate string) (*TrainingStatus, error) {
	d, err := ValidateDate(cdate, "cdate")
	if err != nil {
		return nil, err
	}
	raw, err := c.Do(ctx, Req{Method: http.MethodGet, Path: trainingStatusPath + "/" + d})
	if err != nil {
		return nil, err
	}
	ts := &TrainingStatus{}
	if len(bytes.TrimSpace(raw)) == 0 {
		return ts, nil
	}
	if err := json.Unmarshal(raw, &ts.Data); err != nil {
		return nil, fmt.Errorf("invalid JSON from %s: %w", trainingStatusPath, err)
	}
	var order struct {
		Status struct {
			Keys objectKeys `json:"latestTrainingStatusData"`
		} `json:"mostRecentTrainingStatus"`
		Load struct {
			Keys objectKeys `json:"metricsTrainingLoadBalanceDTOMap"`
		} `json:"mostRecentTrainingLoadBalance"`
	}
	_ = json.Unmarshal(raw, &order) // only the order is lost on a surprising shape
	ts.StatusKeys, ts.LoadKeys = order.Status.Keys, order.Load.Keys
	return ts, nil
}

// objectKeys decodes a JSON object into its keys in document order (null or a non-object gives none).
type objectKeys []string

func (k *objectKeys) UnmarshalJSON(b []byte) error {
	*k = nil
	dec := json.NewDecoder(bytes.NewReader(b))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil
	}
	seen := map[string]bool{}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil
		}
		key, _ := t.(string)
		if !seen[key] {
			seen[key] = true
			*k = append(*k, key)
		}
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil
		}
	}
	return nil
}

// SortedKeys is the fallback device order when none was recorded.
func SortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
