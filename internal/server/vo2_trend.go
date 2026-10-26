package server

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

// vo2Source is the part of the Garmin client get_vo2max_trend uses; the tests substitute a client that lacks the
// max-metrics range endpoint (Python: garmin_connect_metrics_url = None).
type vo2Source interface {
	MaxMetricsEndpoint() metricsEndpoint
	TrainingStatus(ctx context.Context, date string) (any, error)
	MaxMetrics(ctx context.Context, date string) (any, error)
	UserProfile(ctx context.Context) (any, error)
}

type liveVO2 struct{ c *garmin.Client }

func (l liveVO2) MaxMetricsEndpoint() metricsEndpoint {
	return metricsEndpoint{URL: garmin.MaxMetricsPath, Get: func(ctx context.Context, path string) (any, error) {
		return l.c.GetJSON(ctx, path, nil)
	}}
}

func (l liveVO2) TrainingStatus(ctx context.Context, date string) (any, error) {
	ts, err := l.c.GetTrainingStatus(ctx, date)
	if err != nil {
		return nil, err
	}
	return ts.Data, nil
}

func (l liveVO2) MaxMetrics(ctx context.Context, date string) (any, error) {
	return l.c.GetMaxMetrics(ctx, date)
}

func (l liveVO2) UserProfile(ctx context.Context) (any, error) { return l.c.GetUserProfile(ctx) }

var sports = []string{"running", "cycling"}

// vo2maxTrend is the get_vo2max_trend tool.
func vo2maxTrend(ctx context.Context, src vo2Source, startDate, endDate string) (any, error) {
	start, end, msg := trendRange(startDate, endDate, 90)
	if msg != "" {
		return msg, nil
	}

	histories := map[string][]vo2Point{}
	requestErrors := []any{}
	add := func(date string, measurements map[string]float64, source string) {
		for _, sport := range sports {
			vo2, ok := measurements[sport]
			if !ok {
				continue
			}
			dup := false
			for _, p := range histories[sport] {
				dup = dup || p.Date == date
			}
			if !dup {
				histories[sport] = append(histories[sport], vo2Point{date, round(vo2, 1), source})
			}
		}
	}

	var rangeMeasurements map[string]map[string]float64
	supported, rangeData, err := getMaxMetricsRange(ctx, src.MaxMetricsEndpoint(), startDate, endDate)
	switch {
	case err != nil && isFatalTrendError(err):
		return nil, fmt.Errorf("Error retrieving VO2 max data: %w", err)
	case err != nil:
		requestErrors = append(requestErrors, map[string]any{"source": "get_max_metrics", "error": err.Error()})
		// The range endpoint exists but failed. Continue with training status without retrying the same
		// max-metrics endpoint once per day.
		rangeMeasurements = map[string]map[string]float64{}
	case supported:
		rangeMeasurements = extractDatedVO2Measurements(rangeData)
	}

	for cur := start; !cur.After(end); cur = cur.AddDate(0, 0, 1) {
		dateStr := cur.Format(time.DateOnly)
		var measurements map[string]float64
		source := ""
		var methods []string
		if rangeMeasurements != nil {
			measurements = rangeMeasurements[dateStr]
			if len(measurements) > 0 {
				source = "get_max_metrics"
			} else {
				methods = []string{"get_training_status"}
			}
		} else {
			methods = []string{"get_training_status", "get_max_metrics"}
		}

		for _, method := range methods {
			var payload any
			var err error
			if method == "get_training_status" {
				payload, err = src.TrainingStatus(ctx, dateStr)
			} else {
				payload, err = src.MaxMetrics(ctx, dateStr)
			}
			if err != nil {
				if isFatalTrendError(err) {
					return nil, fmt.Errorf("Error retrieving VO2 max data for %s: %w", dateStr, err)
				}
				requestErrors = append(requestErrors, map[string]any{"source": method, "date": dateStr, "error": err.Error()})
				continue
			}
			measurements = extractVO2Measurements(payload)
			if dated := extractDatedVO2Measurements(payload); len(dated) > 0 {
				datedSports := map[string]bool{}
				dates := make([]string, 0, len(dated))
				for d := range dated {
					dates = append(dates, d)
				}
				sort.Strings(dates)
				for _, d := range dates {
					for sport := range dated[d] {
						datedSports[sport] = true
					}
					if d <= dateStr {
						add(d, dated[d], method)
					}
				}
				// mostRecentVO2Max may repeat an earlier measurement; its own date, rather than the requested date,
				// is authoritative.
				for sport := range measurements {
					if datedSports[sport] {
						delete(measurements, sport)
					}
				}
				if len(measurements) == 0 {
					break
				}
			}
			if len(measurements) == 0 {
				continue
			}
			source = method
			break
		}
		add(dateStr, measurements, source)
	}

	selected := ""
	if len(histories["running"]) > 0 || len(histories["cycling"]) > 0 {
		// Prefer the sport with the best coverage; ties go to running.
		selected = "running"
		if len(histories["cycling"]) > len(histories["running"]) {
			selected = "cycling"
		}
	}
	series, err := buildVO2TrendSeries(histories[selected], end)
	if err != nil {
		return nil, err
	}
	trend := []map[string]any{}
	for _, e := range series {
		if d, _ := e["date"].(string); d >= startDate {
			trend = append(trend, e)
		}
	}

	var currentEstimate float64
	currentSport := ""
	if len(trend) == 0 {
		profile, err := src.UserProfile(ctx)
		if err != nil {
			if isFatalTrendError(err) {
				return nil, fmt.Errorf("Error retrieving VO2 max data: %w", err)
			}
			requestErrors = append(requestErrors, map[string]any{"source": "get_user_profile", "error": err.Error()})
			profile = nil
		}
		pm := extractVO2Measurements(profile)
		if len(pm) == 0 {
			if len(requestErrors) > 0 {
				return nil, fmt.Errorf("Error retrieving VO2 max data: %s", asMap(requestErrors[len(requestErrors)-1])["error"])
			}
			return fmt.Sprintf("No VO2 max data found between %s and %s.", startDate, endDate), nil
		}
		currentSport = "cycling"
		if _, ok := pm["running"]; ok {
			currentSport = "running"
		}
		currentEstimate = pm[currentSport]
	}

	var first, latest, change any
	if len(trend) > 0 {
		f, _ := asNumber(trend[0]["vo2_max"])
		l, _ := asNumber(trend[len(trend)-1]["vo2_max"])
		first, latest, change = f, l, round(l-f, 1)
	}
	response := map[string]any{
		"start_date": startDate, "end_date": endDate, "data_points": len(trend),
		"first_vo2_max": first, "latest_vo2_max": latest, "change": change, "trend": trend,
	}
	if selected != "" {
		response["sport"] = selected
	}
	if len(requestErrors) > 0 {
		response["request_errors"] = requestErrors
	}
	if currentSport != "" {
		response["current_vo2_max_estimate"] = map[string]any{"vo2_max": round(currentEstimate, 1), "sport": currentSport, "source": "get_user_profile"}
		response["note"] = "Historical VO2 max values were not available from Garmin; returning the current profile estimate separately."
	}
	return response, nil
}

// trendRange parses the dates of a trend tool and enforces its day cap; a non-empty message is the tool's answer.
func trendRange(startDate, endDate string, maxDays int) (start, end time.Time, msg string) {
	var err error
	if start, err = parseISODate(startDate); err == nil {
		end, err = parseISODate(endDate)
	}
	if err != nil {
		return start, end, fmt.Sprintf("Invalid date format: %s. Use YYYY-MM-DD.", err)
	}
	days := daysBetween(start, end) + 1
	if days > maxDays {
		return start, end, fmt.Sprintf("Date range too large (%d days). Maximum is %d days.", days, maxDays)
	}
	if days < 1 {
		return start, end, "end_date must be on or after start_date."
	}
	return start, end, ""
}
