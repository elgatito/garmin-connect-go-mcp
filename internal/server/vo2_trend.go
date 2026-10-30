package server

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type vo2Point struct {
	Date   string
	Value  float64
	Source string
}

func vo2Values(v any) map[string]float64 {
	out := map[string]float64{}
	if list, ok := v.([]any); ok {
		for _, item := range list {
			for sport, value := range vo2Values(item) {
				if _, exists := out[sport]; !exists {
					out[sport] = value
				}
			}
		}
		return out
	}
	m := asMap(v)
	paths := []struct {
		sport string
		path  []string
	}{{"running", []string{"vo2MaxRunning"}}, {"cycling", []string{"vo2MaxCycling"}}, {"running", []string{"vo2Max"}}, {"running", []string{"vo2MaxPreciseValue"}}, {"running", []string{"vo2MaxValue"}}, {"running", []string{"generic", "vo2MaxPreciseValue"}}, {"running", []string{"generic", "vo2MaxValue"}}, {"cycling", []string{"cycling", "vo2MaxPreciseValue"}}, {"cycling", []string{"cycling", "vo2MaxValue"}}, {"running", []string{"mostRecentVO2Max", "generic", "vo2MaxPreciseValue"}}, {"running", []string{"mostRecentVO2Max", "generic", "vo2MaxValue"}}, {"cycling", []string{"mostRecentVO2Max", "cycling", "vo2MaxPreciseValue"}}, {"cycling", []string{"mostRecentVO2Max", "cycling", "vo2MaxValue"}}, {"running", []string{"userData", "vo2MaxRunning"}}, {"cycling", []string{"userData", "vo2MaxCycling"}}}
	for _, p := range paths {
		if _, exists := out[p.sport]; exists {
			continue
		}
		var x any = m
		for _, key := range p.path {
			x = asMap(x)[key]
		}
		if n, ok := x.(float64); ok && !math.IsNaN(n) && !math.IsInf(n, 0) {
			out[p.sport] = n
		}
	}
	return out
}
func datedVO2(v any) map[string]map[string]float64 {
	out := map[string]map[string]float64{}
	var collect func(any)
	collect = func(item any) {
		if list, ok := item.([]any); ok {
			for _, x := range list {
				collect(x)
			}
			return
		}
		m := asMap(item)
		sections := asMap(m["mostRecentVO2Max"])
		if len(sections) == 0 {
			sections = m
		}
		for sport, value := range vo2Values(m) {
			key := "generic"
			if sport == "cycling" {
				key = "cycling"
			}
			date, _ := asMap(sections[key])["calendarDate"].(string)
			if date == "" {
				date, _ = sections["calendarDate"].(string)
			}
			if date == "" {
				date, _ = m["calendarDate"].(string)
			}
			if date != "" {
				if out[date] == nil {
					out[date] = map[string]float64{}
				}
				if _, exists := out[date][sport]; !exists {
					out[date][sport] = value
				}
			}
		}
	}
	collect(v)
	return out
}
func fatalTrendError(err error) bool {
	var api *garmin.APIError
	if errors.As(err, &api) {
		return api.Status == 401 || api.Status == 429 || api.Status >= 500
	}
	return true
}
func registerVO2Trend(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_vo2max_trend", description("get_vo2max_trend"), map[string]any{"start_date": map[string]any{"type": "string"}, "end_date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		start, end, a, b, e := statusRange(args)
		if e != nil {
			return nil, e
		}
		hist := map[string]map[string]vo2Point{"running": {}, "cycling": {}}
		errs := []any{}
		add := func(date, source string, values map[string]float64) {
			for sport, value := range values {
				if _, ok := hist[sport][date]; !ok {
					hist[sport][date] = vo2Point{date, math.RoundToEven(value*10) / 10, source}
				}
			}
		}
		rangeData, e := s.Client.GetJSON(ctx, "/metrics-service/metrics/maxmet/daily/"+start+"/"+end, nil)
		rangeMeasurements := map[string]map[string]float64{}
		if e != nil {
			if fatalTrendError(e) {
				return nil, fmt.Errorf("Error retrieving VO2 max data: %w", e)
			}
			errs = append(errs, map[string]any{"source": "get_max_metrics", "error": e.Error()})
		} else {
			rangeMeasurements = datedVO2(rangeData)
		}
		for day := a; !day.After(b); day = day.AddDate(0, 0, 1) {
			date := day.Format("2006-01-02")
			if values := rangeMeasurements[date]; len(values) > 0 {
				add(date, "get_max_metrics", values)
				continue
			}
			payload, e := s.Client.GetJSON(ctx, trainingStatusPath+date, nil)
			if e != nil {
				if fatalTrendError(e) {
					return nil, fmt.Errorf("Error retrieving VO2 max data for %s: %w", date, e)
				}
				errs = append(errs, map[string]any{"source": "get_training_status", "date": date, "error": e.Error()})
				continue
			}
			values := vo2Values(payload)
			for measured, dated := range datedVO2(payload) {
				for sport := range dated {
					delete(values, sport)
				}
				if measured <= date {
					add(measured, "get_training_status", dated)
				}
			}
			add(date, "get_training_status", values)
		}
		sport := ""
		if len(hist["running"]) > 0 || len(hist["cycling"]) > 0 {
			sport = "running"
			if len(hist["cycling"]) > len(hist["running"]) {
				sport = "cycling"
			}
		}
		trend := []any{}
		var last *vo2Point
		if sport != "" {
			for date, p := range hist[sport] {
				if date < start && (last == nil || date > last.Date) {
					q := p
					last = &q
				}
			}
			for day := a; !day.After(b); day = day.AddDate(0, 0, 1) {
				date := day.Format("2006-01-02")
				p, found := hist[sport][date]
				if found {
					q := p
					last = &q
				}
				if last != nil {
					entry := map[string]any{"date": date, "vo2_max": last.Value, "source": last.Source}
					if !found {
						entry["carried_forward"] = true
					}
					trend = append(trend, entry)
				}
			}
		}
		var current any
		if len(trend) == 0 {
			profile, e := s.Client.GetJSON(ctx, "/userprofile-service/userprofile/user-settings", nil)
			if e != nil {
				if fatalTrendError(e) {
					return nil, fmt.Errorf("Error retrieving VO2 max data: %w", e)
				}
				errs = append(errs, map[string]any{"source": "get_user_profile", "error": e.Error()})
			}
			values := vo2Values(profile)
			currentSport := "running"
			value, ok := values[currentSport]
			if !ok {
				currentSport = "cycling"
				value, ok = values[currentSport]
			}
			if !ok {
				if len(errs) > 0 {
					return nil, fmt.Errorf("Error retrieving VO2 max data: %v", asMap(errs[len(errs)-1])["error"])
				}
				return fmt.Sprintf("No VO2 max data found between %s and %s.", start, end), nil
			}
			current = map[string]any{"vo2_max": math.RoundToEven(value*10) / 10, "sport": currentSport, "source": "get_user_profile"}
		}
		out := map[string]any{"start_date": start, "end_date": end, "data_points": len(trend), "first_vo2_max": nil, "latest_vo2_max": nil, "change": nil, "trend": trend}
		if len(trend) > 0 {
			first := asMap(trend[0])["vo2_max"].(float64)
			last := asMap(trend[len(trend)-1])["vo2_max"].(float64)
			out["first_vo2_max"] = first
			out["latest_vo2_max"] = last
			out["change"] = math.RoundToEven((last-first)*10) / 10
		}
		if sport != "" {
			out["sport"] = sport
		}
		if len(errs) > 0 {
			out["request_errors"] = errs
		}
		if current != nil {
			out["current_vo2_max_estimate"] = current
			out["note"] = "Historical VO2 max values were not available from Garmin; returning the current profile estimate separately."
		}
		return out, nil
	}, "start_date", "end_date")
}
