package server

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func dateArgument(args map[string]any, key string) (string, error) {
	v, ok := args[key].(string)
	if !ok {
		return "", fmt.Errorf("%s is required", key)
	}
	parsed, err := time.Parse("2006-01-02", v)
	if err != nil || parsed.Format("2006-01-02") != v {
		return "", fmt.Errorf("invalid date %q: expected YYYY-MM-DD", v)
	}
	return v, nil
}
func empty(v any) bool {
	if v == nil {
		return true
	}
	switch x := v.(type) {
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	case string:
		return x == ""
	}
	return false
}
func registerHealth(srv *mcp.Server, m *manager) {
	type daily struct {
		name, path, label string
		query             bool
	}
	items := []daily{
		{"get_floors", "/wellness-service/wellness/floorsChartData/daily/", "floors data", false},
		{"get_body_battery_events", "/wellness-service/wellness/bodyBattery/events/", "body battery events", false},
		{"get_hydration_data", "/usersummary-service/usersummary/hydration/daily/", "hydration data", false},
		{"get_respiration_data", "/wellness-service/wellness/daily/respiration/", "respiration data", false},
		{"get_stress_data", "/wellness-service/wellness/dailyStress/", "stress data", false},
		{"get_all_day_stress", "/wellness-service/wellness/dailyStress/", "all-day stress data", false},
		{"get_all_day_events", "/wellness-service/wellness/dailyEvents", "daily wellness events", true},
		{"get_lifestyle_logging_data", "/lifestylelogging-service/dailyLog/", "lifestyle logging data", false},
	}
	for _, item := range items {
		item := item
		addTool(srv, m, item.name, description(item.name), map[string]any{"date": map[string]any{"type": "string", "description": "Date in YYYY-MM-DD format"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
			date, err := dateArgument(args, "date")
			if err != nil {
				return nil, fmt.Errorf("Error retrieving %s: %w", item.label, err)
			}
			path := item.path + date
			var query url.Values
			if item.query {
				path = item.path
				query = url.Values{"calendarDate": {date}}
			}
			v, err := s.Client.GetJSON(ctx, path, query)
			if err != nil {
				return nil, fmt.Errorf("Error retrieving %s: %w", item.label, err)
			}
			if empty(v) {
				return fmt.Sprintf("No %s found for %s", item.label, date), nil
			}
			return v, nil
		}, "date")
	}
	for _, item := range []struct{ name, path, label, key string }{{"get_user_summary", "/usersummary-service/usersummary/daily/", "user summary", "calendarDate"}, {"get_steps_data", "/wellness-service/wellness/dailySummaryChart/", "steps data", "date"}, {"get_heart_rates", "/wellness-service/wellness/dailyHeartRate/", "heart rate data", "date"}, {"get_sleep_data", "/wellness-service/wellness/dailySleepData/", "sleep data", "date"}} {
		item := item
		addTool(srv, m, item.name, description(item.name), map[string]any{"date": map[string]any{"type": "string", "description": "Date in YYYY-MM-DD format"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
			date, err := dateArgument(args, "date")
			if err != nil {
				return nil, fmt.Errorf("Error retrieving %s: %w", item.label, err)
			}
			profile, err := s.Client.Profile(ctx)
			if err != nil {
				return nil, fmt.Errorf("Error retrieving %s: %w", item.label, err)
			}
			query := url.Values{item.key: {date}}
			if item.name == "get_sleep_data" {
				query.Set("nonSleepBufferMinutes", "60")
			}
			v, err := s.Client.GetJSON(ctx, item.path+url.PathEscape(profile.DisplayName), query)
			if err != nil {
				return nil, fmt.Errorf("Error retrieving %s: %w", item.label, err)
			}
			if empty(v) {
				return fmt.Sprintf("No %s found for %s", item.label, date), nil
			}
			return v, nil
		}, "date")
	}
	addTool(srv, m, "get_stats", description("get_stats"), map[string]any{"date": map[string]any{"type": "string", "description": "Date in YYYY-MM-DD format"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		date, err := dateArgument(args, "date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving stats: %w", err)
		}
		p, err := s.Client.Profile(ctx)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving stats: %w", err)
		}
		v, err := s.Client.GetJSON(ctx, "/usersummary-service/usersummary/daily/"+url.PathEscape(p.DisplayName), url.Values{"calendarDate": {date}})
		if err != nil {
			return nil, fmt.Errorf("Error retrieving stats: %w", err)
		}
		if empty(v) {
			return fmt.Sprintf("No stats found for %s", date), nil
		}
		raw, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Error retrieving stats: unexpected response")
		}
		fields := map[string]string{"date": "calendarDate", "total_steps": "totalSteps", "daily_step_goal": "dailyStepGoal", "distance_meters": "totalDistanceMeters", "floors_ascended": "floorsAscended", "floors_descended": "floorsDescended", "total_calories": "totalKilocalories", "active_calories": "activeKilocalories", "bmr_calories": "bmrKilocalories", "highly_active_seconds": "highlyActiveSeconds", "active_seconds": "activeSeconds", "sedentary_seconds": "sedentarySeconds", "sleeping_seconds": "sleepingSeconds", "moderate_intensity_minutes": "moderateIntensityMinutes", "vigorous_intensity_minutes": "vigorousIntensityMinutes", "intensity_minutes_goal": "intensityMinutesGoal", "min_heart_rate_bpm": "minHeartRate", "max_heart_rate_bpm": "maxHeartRate", "resting_heart_rate_bpm": "restingHeartRate", "last_7_days_avg_resting_hr": "lastSevenDaysAvgRestingHeartRate", "avg_stress_level": "averageStressLevel", "max_stress_level": "maxStressLevel", "stress_qualifier": "stressQualifier", "body_battery_charged": "bodyBatteryChargedValue", "body_battery_drained": "bodyBatteryDrainedValue", "body_battery_highest": "bodyBatteryHighestValue", "body_battery_lowest": "bodyBatteryLowestValue", "body_battery_current": "bodyBatteryMostRecentValue", "avg_spo2_percent": "averageSpo2", "lowest_spo2_percent": "lowestSpo2", "avg_waking_respiration": "avgWakingRespirationValue", "highest_respiration": "highestRespirationValue", "lowest_respiration": "lowestRespirationValue"}
		out := map[string]any{}
		for key, source := range fields {
			value := raw[source]
			if value != nil {
				if key == "floors_ascended" || key == "floors_descended" {
					if n, ok := value.(float64); ok {
						value = math.RoundToEven(n*10) / 10
					}
				}
				out[key] = value
			}
		}
		return out, nil
	}, "date")
	addTool(srv, m, "get_spo2_data", description("get_spo2_data"), map[string]any{"date": map[string]any{"type": "string", "description": "Date in YYYY-MM-DD format"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		date, err := dateArgument(args, "date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving SpO2 data: %w", err)
		}
		v, err := s.Client.GetJSON(ctx, "/wellness-service/wellness/daily/spo2/"+date, nil)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving SpO2 data: %w", err)
		}
		if empty(v) {
			return fmt.Sprintf("No SpO2 data found for %s", date), nil
		}
		raw, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Error retrieving SpO2 data: unexpected response")
		}
		out := map[string]any{}
		for k, source := range map[string]string{"date": "calendarDate", "avg_spo2_percent": "averageSpO2", "lowest_spo2_percent": "lowestSpO2", "latest_spo2_percent": "latestSpO2", "latest_reading_time": "latestSpO2TimestampLocal", "last_7_days_avg_spo2": "lastSevenDaysAvgSpO2", "avg_sleep_spo2_percent": "avgSleepSpO2"} {
			if raw[source] != nil {
				out[k] = raw[source]
			}
		}
		if !empty(raw["spO2HourlyAverages"]) {
			out["hourly_averages"] = raw["spO2HourlyAverages"]
		}
		return out, nil
	}, "date")

}
