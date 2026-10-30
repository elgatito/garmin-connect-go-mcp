package server

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"math"
	"net/url"
	"strings"
	"time"
)

func recoveryState(hours float64, phrase any) string {
	if phrase == "REACHED_ZERO" || hours == 0 {
		return "recovered"
	}
	if hours <= 6 {
		return "nearly_recovered"
	}
	if hours <= 24 {
		return "recovering"
	}
	return "not_recovered"
}
func recoverySnapshot(v any, source, date string) map[string]any {
	rows := []map[string]any{}
	switch v := v.(type) {
	case map[string]any:
		if len(v) > 0 {
			rows = append(rows, v)
		}
	case []any:
		for _, item := range v {
			if row, ok := item.(map[string]any); ok {
				rows = append(rows, row)
			}
		}
	}
	if len(rows) == 0 {
		return nil
	}
	latest := rows[0]
	timestamp := func(r map[string]any) string {
		v := r["timestampLocal"]
		if !truthy(v) {
			v = r["timestamp"]
		}
		if v == nil {
			return ""
		}
		return fmt.Sprint(v)
	}
	for _, r := range rows[1:] {
		if timestamp(r) > timestamp(latest) {
			latest = r
		}
	}
	phrase := latest["recoveryTimeChangePhrase"]
	n, ok := latest["recoveryTime"].(float64)
	if phrase == "REACHED_ZERO" {
		n = 0
		ok = true
	}
	if !ok || n < 0 {
		return nil
	}
	hours := math.RoundToEven(n/6) / 10
	score := latest["score"]
	if score == nil {
		score = latest["readinessScore"]
	}
	ts := latest["timestampLocal"]
	if !truthy(ts) {
		ts = latest["timestamp"]
	}
	level := latest["level"]
	if !truthy(level) {
		level = latest["readinessLevel"]
	}
	result := map[string]any{"remaining_hours": hours, "remaining_hours_at_snapshot": hours, "value_type": "snapshot", "as_of": ts, "recovery_score": score, "state": recoveryState(hours, phrase), "source": source, "date": latest["calendarDate"], "timestamp": ts, "change_phrase": phrase, "level": level}
	if result["date"] == nil {
		result["date"] = date
	}
	for k, v := range result {
		if v == nil {
			delete(result, k)
		}
	}
	return result
}
func registerRecovery(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_recovery_time_remaining", description("get_recovery_time_remaining"), map[string]any{"date": map[string]any{"type": "string", "default": ""}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		date, _ := args["date"].(string)
		date = strings.TrimSpace(date)
		if date == "" {
			date = time.Now().Local().Format("2006-01-02")
		}
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return fmt.Sprintf("Invalid date '%s'. Use YYYY-MM-DD.", date), nil
		}
		path := "/metrics-service/metrics/trainingreadiness/" + date
		sourceErrors := []map[string]string{}
		v, err := s.Client.GetJSON(ctx, path, nil)
		if err != nil {
			sourceErrors = append(sourceErrors, map[string]string{"source": "get_training_readiness", "error": err.Error()})
		} else if result := recoverySnapshot(v, "training_readiness", date); result != nil {
			return result, nil
		}
		v, err = s.Client.GetJSON(ctx, path, nil)
		if err != nil {
			sourceErrors = append(sourceErrors, map[string]string{"source": "get_morning_training_readiness", "error": err.Error()})
		} else {
			var morning any
			switch rows := v.(type) {
			case []any:
				if len(rows) > 0 {
					morning = rows[0]
					for _, v := range rows {
						if r, ok := v.(map[string]any); ok && r["inputContext"] == "AFTER_WAKEUP_RESET" {
							morning = r
							break
						}
					}
				}
			case map[string]any:
				morning = rows
			}
			if result := recoverySnapshot(morning, "morning_training_readiness", date); result != nil {
				if len(sourceErrors) > 0 {
					result["source_errors"] = sourceErrors
				}
				return result, nil
			}
		}
		now := time.Now()
		today := date == now.Local().Format("2006-01-02")
		if today {
			v, err = s.Client.GetJSON(ctx, "/activitylist-service/activities/search/activities", url.Values{"start": {"0"}, "limit": {"20"}})
			if err != nil {
				sourceErrors = append(sourceErrors, map[string]string{"source": "get_activities", "error": err.Error()})
			} else if rows, ok := v.([]any); ok {
				var best map[string]any
				var bestEnd time.Time
				for _, v := range rows {
					r, ok := v.(map[string]any)
					if !ok {
						continue
					}
					recovery, ok := r["recoveryTime"].(float64)
					if !ok {
						continue
					}
					begin, ok := r["beginTimestamp"].(float64)
					if !ok || begin <= 0 {
						continue
					}
					duration, _ := r["duration"].(float64)
					if duration == 0 {
						duration, _ = r["elapsedDuration"].(float64)
					}
					end := time.UnixMilli(int64(begin)).Add(time.Duration(duration * float64(time.Second)))
					if best == nil || end.After(bestEnd) {
						minutes := recovery - math.Max(0, now.Sub(end).Minutes())
						hours := math.RoundToEven(math.Max(0, minutes)/6) / 10
						best = map[string]any{"remaining_hours": hours, "state": recoveryState(hours, nil), "source": "recent_activity", "value_type": "estimate", "as_of": now.UTC().Format(time.RFC3339Nano), "activity_id": r["activityId"], "activity_name": r["activityName"], "date": date}
						for k, v := range best {
							if v == nil {
								delete(best, k)
							}
						}
						bestEnd = end
					}
				}
				if best != nil {
					if len(sourceErrors) > 0 {
						best["source_errors"] = sourceErrors
					}
					return best, nil
				}
			}
		}
		if len(sourceErrors) > 0 {
			parts := []string{}
			for _, e := range sourceErrors {
				parts = append(parts, e["source"]+": "+e["error"])
			}
			return nil, fmt.Errorf("Error retrieving recovery time remaining: %s", strings.Join(parts, "; "))
		}
		message := "No Training Readiness recovery snapshot found for this date. The activity-based recovery estimate is only available for today."
		if today {
			message = "No recovery time remaining found for this date. Training Readiness snapshots were empty and recent activities did not include recoveryTime. Some devices (for example Forerunner 255) show recovery on-device without publishing a Connect Training Readiness feed."
		}
		return map[string]any{"remaining_hours": nil, "recovery_score": nil, "state": "unavailable", "date": date, "message": message}, nil
	})
}
