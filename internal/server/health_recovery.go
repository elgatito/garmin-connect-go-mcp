package server

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Port of the recovery-time and training-readiness helpers in health_wellness.py.

// asSnapshotList normalizes a training-readiness payload (list, single dict, or nothing) to a list of dicts.
func asSnapshotList(payload any) []map[string]any {
	switch p := payload.(type) {
	case []any:
		var out []map[string]any
		for _, item := range p {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case map[string]any:
		if len(p) > 0 {
			return []map[string]any{p}
		}
	}
	return nil
}

// recoveryState maps remaining hours (nil: unknown) and Garmin's change phrase to a coarse state.
func recoveryState(hours *float64, phrase any) string {
	switch {
	case phrase == "REACHED_ZERO" || (hours != nil && *hours == 0):
		return "recovered"
	case hours == nil:
		return "unknown"
	case *hours <= 6:
		return "nearly_recovered"
	case *hours <= 24:
		return "recovering"
	}
	return "not_recovered"
}

// hoursFromRecoveryMinutes converts Garmin recovery minutes to hours (nil when unusable). REACHED_ZERO wins
// because Garmin keeps the last assigned minutes after the clock has drained.
func hoursFromRecoveryMinutes(minutes, phrase any) *float64 {
	if phrase == "REACHED_ZERO" {
		h := 0.0
		return &h
	}
	m, ok := asNumber(minutes)
	if !ok || m < 0 {
		return nil
	}
	h := round(m/60, 1)
	return &h
}

// hoursValue is the hours as a JSON value (nil when unknown).
func hoursValue(h *float64) any {
	if h == nil {
		return nil
	}
	return *h
}

// recoveryFromReadiness builds a recovery payload from the latest readiness snapshot; nil when there is none.
func recoveryFromReadiness(payload any, source string) map[string]any {
	snapshots := asSnapshotList(payload)
	if len(snapshots) == 0 {
		return nil
	}
	key := func(m map[string]any) string {
		if v := firstTruthy(m["timestampLocal"], m["timestamp"]); !falsy(v) {
			return pyStr(v)
		}
		return ""
	}
	latest := snapshots[0]
	for _, s := range snapshots[1:] {
		if key(s) > key(latest) { // max() keeps the first of equal keys
			latest = s
		}
	}
	phrase := latest["recoveryTimeChangePhrase"]
	hours := hoursFromRecoveryMinutes(latest["recoveryTime"], phrase)
	if hours == nil {
		return nil
	}
	score := latest["score"]
	if score == nil {
		score = latest["readinessScore"]
	}
	asOf := firstTruthy(latest["timestampLocal"], latest["timestamp"])
	return dropNil(map[string]any{
		"remaining_hours":             *hours,
		"remaining_hours_at_snapshot": *hours,
		"value_type":                  "snapshot",
		"as_of":                       asOf,
		"recovery_score":              score,
		"state":                       recoveryState(hours, phrase),
		"source":                      source,
		"date":                        latest["calendarDate"],
		"timestamp":                   asOf,
		"change_phrase":               phrase,
		"level":                       firstTruthy(latest["level"], latest["readinessLevel"]),
	})
}

// curateTrainingReadiness curates one snapshot (shared by get_training_readiness and get_morning_training_readiness).
func curateTrainingReadiness(r map[string]any) map[string]any {
	e := pick(r,
		"date", "calendarDate", "timestamp", "timestampLocal", "context", "inputContext",
		"level", "level", "score", "score", "feedback", "feedbackShort",
		"sleep_score", "sleepScore", "sleep_factor_percent", "sleepScoreFactorPercent", "sleep_factor_feedback", "sleepScoreFactorFeedback",
		"recovery_factor_percent", "recoveryTimeFactorPercent", "recovery_factor_feedback", "recoveryTimeFactorFeedback",
		"training_load_factor_percent", "acwrFactorPercent", "training_load_feedback", "acwrFactorFeedback", "acute_load", "acuteLoad",
		"hrv_factor_percent", "hrvFactorPercent", "hrv_factor_feedback", "hrvFactorFeedback", "hrv_weekly_avg", "hrvWeeklyAverage",
		"stress_history_factor_percent", "stressHistoryFactorPercent", "stress_history_feedback", "stressHistoryFactorFeedback",
		"sleep_history_factor_percent", "sleepHistoryFactorPercent", "sleep_history_feedback", "sleepHistoryFactorFeedback",
	)
	e["recovery_time_hours"] = hoursValue(hoursFromRecoveryMinutes(r["recoveryTime"], r["recoveryTimeChangePhrase"]))
	return dropNil(e)
}

// recoveryClient is the part of garmin.Client the recovery lookup needs (a seam for tests).
type recoveryClient interface {
	GetTrainingReadiness(ctx context.Context, date string) (any, error)
	GetMorningTrainingReadiness(ctx context.Context, date string) (any, error)
	GetActivities(ctx context.Context, start, limit int) (any, error)
}

// activityEndUTC estimates an activity's end from its list-search fields.
func activityEndUTC(a map[string]any) (time.Time, bool) {
	begin, ok := asNumber(a["beginTimestamp"])
	if !ok || begin <= 0 {
		return time.Time{}, false
	}
	dur, _ := asNumber(firstTruthy(a["duration"], a["elapsedDuration"]))
	return time.UnixMicro(int64(begin * 1000)).UTC().Add(time.Duration(dur * float64(time.Second))), true
}

// recoveryFromRecentActivities decays the latest activity-assigned recoveryTime to now.
func recoveryFromRecentActivities(ctx context.Context, c recoveryClient, now time.Time, srcErrs *[]map[string]string) map[string]any {
	items, err := c.GetActivities(ctx, 0, 20)
	if err != nil {
		*srcErrs = append(*srcErrs, map[string]string{"source": "get_activities", "error": err.Error()})
		return nil
	}
	var best map[string]any
	var bestEnd time.Time
	for _, it := range asList(items) {
		item := asMap(it)
		recovery, ok := asNumber(item["recoveryTime"])
		if !ok {
			continue
		}
		end, ok := activityEndUTC(item)
		if !ok {
			continue
		}
		elapsedMin := max(0, now.Sub(end).Minutes())
		remaining := round(max(0, recovery-elapsedMin)/60, 1)
		if best == nil || end.After(bestEnd) {
			bestEnd = end
			best = dropNil(map[string]any{
				"remaining_hours": remaining,
				"state":           recoveryState(&remaining, nil),
				"source":          "recent_activity",
				"value_type":      "estimate",
				"as_of":           now.UTC().Format("2006-01-02T15:04:05.999999-07:00"),
				"activity_id":     item["activityId"],
				"activity_name":   item["activityName"],
			})
		}
	}
	return best
}

// collectRecoveryTime resolves a day's remaining recovery from readiness, morning readiness, then
// (for today only) recent activities. A zero now means time.Now(). It fails only when every source
// that was tried failed and none produced data.
func collectRecoveryTime(ctx context.Context, c recoveryClient, date string, now time.Time) (map[string]any, error) {
	var srcErrs []map[string]string
	safe := func(source string, fn func() (any, error)) any {
		v, err := fn()
		if err != nil {
			srcErrs = append(srcErrs, map[string]string{"source": source, "error": err.Error()})
			return nil
		}
		return v
	}
	finish := func(curated map[string]any, withErrors bool) map[string]any {
		if _, ok := curated["date"]; !ok {
			curated["date"] = date
		}
		if withErrors && len(srcErrs) > 0 {
			curated["source_errors"] = srcErrs
		}
		return curated
	}

	readiness := safe("get_training_readiness", func() (any, error) { return c.GetTrainingReadiness(ctx, date) })
	if curated := recoveryFromReadiness(readiness, "training_readiness"); curated != nil {
		return finish(curated, false), nil
	}
	morning := safe("get_morning_training_readiness", func() (any, error) { return c.GetMorningTrainingReadiness(ctx, date) })
	if curated := recoveryFromReadiness(morning, "morning_training_readiness"); curated != nil {
		return finish(curated, true), nil
	}

	if now.IsZero() {
		now = time.Now()
	}
	// A current estimate from recent activities cannot establish the recovery clock on another day.
	isToday := date == now.Local().Format(time.DateOnly)
	if isToday {
		if curated := recoveryFromRecentActivities(ctx, c, now, &srcErrs); curated != nil {
			return finish(curated, true), nil
		}
	}
	if len(srcErrs) > 0 {
		parts := make([]string, len(srcErrs))
		for i, e := range srcErrs {
			parts[i] = e["source"] + ": " + e["error"]
		}
		return nil, errors.New(strings.Join(parts, "; "))
	}

	msg := "No Training Readiness recovery snapshot found for this date. The activity-based recovery estimate is only available for today."
	if isToday {
		msg = "No recovery time remaining found for this date. Training Readiness snapshots were empty and recent activities did not include recoveryTime. " +
			"Some devices (for example Forerunner 255) show recovery on-device without publishing a Connect Training Readiness feed."
	}
	return map[string]any{"remaining_hours": nil, "recovery_score": nil, "state": "unavailable", "date": date, "message": msg}, nil
}
