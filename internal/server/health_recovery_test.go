package server

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Ported from tests/unit/test_recovery_time.py.

func f64p(f float64) *float64 { return &f }

func TestHoursFromRecoveryMinutesAndReachedZero(t *testing.T) {
	if got := hoursFromRecoveryMinutes(1728.0, nil); got == nil || *got != 28.8 {
		t.Errorf("1728 -> %v", got)
	}
	if got := hoursFromRecoveryMinutes(106.0, "REACHED_ZERO"); got == nil || *got != 0.0 {
		t.Errorf("REACHED_ZERO -> %v", got)
	}
	if got := hoursFromRecoveryMinutes(nil, nil); got != nil {
		t.Errorf("nil -> %v", *got)
	}
	if got := hoursFromRecoveryMinutes(true, nil); got != nil {
		t.Errorf("true -> %v", *got)
	}
	if got := hoursFromRecoveryMinutes(-5.0, nil); got != nil {
		t.Errorf("negative -> %v", *got)
	}
}

func TestRecoveryStateBands(t *testing.T) {
	for _, tc := range []struct {
		hours  *float64
		phrase any
		want   string
	}{
		{f64p(0), nil, "recovered"}, {f64p(3), nil, "nearly_recovered"}, {f64p(12), nil, "recovering"},
		{f64p(36), nil, "not_recovered"}, {f64p(12), "REACHED_ZERO", "recovered"}, {nil, nil, "unknown"},
	} {
		if got := recoveryState(tc.hours, tc.phrase); got != tc.want {
			t.Errorf("recoveryState(%v, %v) = %s, want %s", tc.hours, tc.phrase, got, tc.want)
		}
	}
}

func TestRecoveryFromReadinessUsesLatestSnapshot(t *testing.T) {
	payload := []any{
		map[string]any{"calendarDate": "2026-09-12", "timestampLocal": "2026-09-12T07:00:00", "recoveryTime": 1728.0, "score": 51.0,
			"level": "MODERATE", "recoveryTimeChangePhrase": "NO_CHANGE_SLEEP"},
		map[string]any{"calendarDate": "2026-09-12", "timestampLocal": "2026-09-12T18:00:00", "recoveryTime": 106.0, "score": 72.0,
			"level": "PRODUCTIVE", "recoveryTimeChangePhrase": "NO_CHANGE_SLEEP"},
	}
	curated := recoveryFromReadiness(payload, "training_readiness")
	if curated["remaining_hours"] != 1.8 || curated["recovery_score"] != 72.0 || curated["state"] != "nearly_recovered" || curated["source"] != "training_readiness" {
		t.Errorf("curated = %v", curated)
	}
}

// fakeRecovery is the Python _FakeClient/Mock: canned answers or errors per source.
type fakeRecovery struct {
	readiness, morning, activities          any
	readinessErr, morningErr, activitiesErr error
	activityCalls                           int
}

func (f *fakeRecovery) GetTrainingReadiness(context.Context, string) (any, error) {
	return f.readiness, f.readinessErr
}

func (f *fakeRecovery) GetMorningTrainingReadiness(context.Context, string) (any, error) {
	return f.morning, f.morningErr
}

func (f *fakeRecovery) GetActivities(context.Context, int, int, string) (any, error) {
	f.activityCalls++
	return f.activities, f.activitiesErr
}

func utc(y int, mo time.Month, d, h int) time.Time { return time.Date(y, mo, d, h, 0, 0, 0, time.UTC) }

func TestCollectPrefersTrainingReadiness(t *testing.T) {
	c := &fakeRecovery{
		readiness:  map[string]any{"recoveryTime": 600.0, "score": 80.0, "calendarDate": "2026-09-12", "timestampLocal": "2026-09-12T08:00:00"},
		activities: []any{map[string]any{"recoveryTime": 9999.0}},
	}
	curated, err := collectRecoveryTime(context.Background(), c, "2026-09-12", time.Time{})
	if err != nil || curated["source"] != "training_readiness" || curated["remaining_hours"] != 10.0 || curated["recovery_score"] != 80.0 {
		t.Errorf("%v %v", curated, err)
	}
}

func TestCollectFallsBackToDecayedActivityRecovery(t *testing.T) {
	old := time.Local
	time.Local = time.UTC // "today" is judged on the server-local calendar
	t.Cleanup(func() { time.Local = old })
	now := utc(2026, 9, 12, 18)
	// Activity ended at 12:00 UTC with 8 hours (480 min) assigned -> 2 hours left.
	c := &fakeRecovery{readiness: []any{}, activities: []any{map[string]any{
		"activityId": 42.0, "activityName": "Lunch run", "beginTimestamp": float64(utc(2026, 9, 12, 11).UnixMilli()),
		"duration": 3600.0, "recoveryTime": 480.0}}}
	curated, err := collectRecoveryTime(context.Background(), c, "2026-09-12", now)
	if err != nil || curated["source"] != "recent_activity" || curated["remaining_hours"] != 2.0 || curated["activity_id"] != 42.0 ||
		curated["state"] != "nearly_recovered" || curated["as_of"] != "2026-09-12T18:00:00+00:00" || curated["date"] != "2026-09-12" {
		t.Errorf("%v %v", curated, err)
	}
}

func TestCollectUnavailableWhenNoSources(t *testing.T) {
	c := &fakeRecovery{readiness: []any{}, activities: []any{}}
	curated, err := collectRecoveryTime(context.Background(), c, "2026-09-12", time.Time{})
	if _, ok := curated["message"]; err != nil || curated["state"] != "unavailable" || curated["remaining_hours"] != nil || !ok {
		t.Errorf("%v %v", curated, err)
	}
}

func TestReadinessRecoveryExplicitlyLabelsSnapshotTime(t *testing.T) {
	curated := recoveryFromReadiness(map[string]any{"calendarDate": "2026-09-12", "timestampLocal": "2026-09-12T06:30:00", "recoveryTime": 1728.0},
		"training_readiness")
	if curated["value_type"] != "snapshot" || curated["remaining_hours_at_snapshot"] != 28.8 || curated["as_of"] != "2026-09-12T06:30:00" ||
		curated["remaining_hours"] != 28.8 {
		t.Errorf("curated = %v", curated)
	}
}

func TestCollectDoesNotLabelCurrentActivityEstimateAsPastDate(t *testing.T) {
	now := utc(2026, 10, 19, 18)
	c := &fakeRecovery{readiness: []any{}, activities: []any{map[string]any{
		"activityId": 42.0, "beginTimestamp": float64(utc(2026, 10, 19, 12).UnixMilli()), "duration": 3600.0, "recoveryTime": 1440.0}}}
	curated, err := collectRecoveryTime(context.Background(), c, "2026-09-12", now)
	msg, _ := curated["message"].(string)
	if _, ok := curated["activity_id"]; err != nil || curated["state"] != "unavailable" || !strings.Contains(msg, "only available for today") || ok {
		t.Errorf("%v %v", curated, err)
	}
	if c.activityCalls != 0 {
		t.Errorf("get_activities called %d times", c.activityCalls)
	}
}

func TestCollectSurfacesErrorsWhenAllSourcesFail(t *testing.T) {
	c := &fakeRecovery{readinessErr: errors.New("readiness unavailable"), morningErr: errors.New("morning unavailable"),
		activitiesErr: errors.New("activities unavailable")}
	// now is today so the activity source is consulted too.
	_, err := collectRecoveryTime(context.Background(), c, time.Now().Format(time.DateOnly), time.Now())
	want := "get_training_readiness: readiness unavailable; get_morning_training_readiness: morning unavailable; get_activities: activities unavailable"
	if err == nil || err.Error() != want {
		t.Errorf("err = %v, want %s", err, want)
	}
}

func TestCollectKeepsSourceErrorsWhenAnotherSourceSucceeds(t *testing.T) {
	c := &fakeRecovery{readinessErr: errors.New("readiness unavailable"), morning: map[string]any{"recoveryTime": 120.0}}
	curated, err := collectRecoveryTime(context.Background(), c, "2026-09-12", time.Time{})
	wantErrs := []map[string]string{{"source": "get_training_readiness", "error": "readiness unavailable"}}
	if err != nil || curated["remaining_hours"] != 2.0 || curated["source"] != "morning_training_readiness" ||
		!reflect.DeepEqual(curated["source_errors"], wantErrs) {
		t.Errorf("%v %v", curated, err)
	}
}

func TestCollectSurfacesActivityFailureAfterEmptyReadiness(t *testing.T) {
	c := &fakeRecovery{readiness: []any{}, activitiesErr: errors.New("activity request failed")}
	_, err := collectRecoveryTime(context.Background(), c, time.Now().Format(time.DateOnly), time.Now())
	if err == nil || !strings.Contains(err.Error(), "activity request failed") {
		t.Errorf("err = %v", err)
	}
}
