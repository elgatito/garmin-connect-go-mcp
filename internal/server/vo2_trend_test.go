package server

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

// Ported from the get_vo2max_trend tests that mock a Garmin client without the max-metrics range endpoint
// (garmin_connect_metrics_url = None); the HTTP-backed client always has it, so these use a fake vo2Source.

type fakeVO2 struct {
	endpoint     metricsEndpoint
	status       func(date string) (any, error)
	maxMetrics   func(date string) (any, error)
	profile      func() (any, error)
	statusDates  []string
	maxDates     []string
	profileCalls int
}

func (f *fakeVO2) MaxMetricsEndpoint() metricsEndpoint { return f.endpoint }

func (f *fakeVO2) TrainingStatus(_ context.Context, date string) (any, error) {
	f.statusDates = append(f.statusDates, date)
	return f.status(date)
}

func (f *fakeVO2) MaxMetrics(_ context.Context, date string) (any, error) {
	f.maxDates = append(f.maxDates, date)
	if f.maxMetrics == nil {
		return nil, nil
	}
	return f.maxMetrics(date)
}

func (f *fakeVO2) UserProfile(context.Context) (any, error) {
	f.profileCalls++
	if f.profile == nil {
		return nil, nil
	}
	return f.profile()
}

func runVO2(t *testing.T, src vo2Source, start, end string) map[string]any {
	t.Helper()
	v, err := vo2maxTrend(context.Background(), src, start, end)
	if err != nil {
		t.Fatal(err)
	}
	return asMap(jsonValue(t, v))
}

func TestGetVo2maxTrendUsesDailyMaxMetricsForOlderClients(t *testing.T) {
	src := &fakeVO2{
		status: func(string) (any, error) { return map[string]any{}, nil },
		maxMetrics: func(string) (any, error) {
			return []any{map[string]any{"generic": map[string]any{"vo2MaxValue": 48.0}}}, nil
		},
	}
	data := runVO2(t, src, "2024-01-15", "2024-01-15")
	if data["latest_vo2_max"] != 48.0 || !reflect.DeepEqual(src.maxDates, []string{"2024-01-15"}) {
		t.Errorf("%v %v", data, src.maxDates)
	}
}

func TestGetVo2maxTrendHandlesEndpointExceptionAndMissingMethod(t *testing.T) {
	src := &fakeVO2{
		status:  func(string) (any, error) { return nil, errors.New("API unavailable") },
		profile: func() (any, error) { return map[string]any{"userData": map[string]any{"vo2MaxCycling": 55.0}}, nil },
	}
	data := runVO2(t, src, "2024-01-14", "2024-01-15")
	if at(data, "current_vo2_max_estimate", "sport") != "cycling" || len(src.statusDates) != 2 {
		t.Errorf("%v %v", data, src.statusDates)
	}
	if len(asList(data["request_errors"])) != 2 {
		t.Errorf("request_errors = %v", data["request_errors"])
	}
}

func TestVo2TrendPreservesUndatedSportAlongsideDatedSport(t *testing.T) {
	answers := map[string]any{
		"2024-01-13": map[string]any{"mostRecentVO2Max": map[string]any{
			"generic": map[string]any{"vo2MaxValue": 48.0},
			"cycling": map[string]any{"calendarDate": "2024-01-10", "vo2MaxValue": 55.0},
		}},
		"2024-01-14": map[string]any{"mostRecentVO2Max": map[string]any{"generic": map[string]any{"vo2MaxValue": 48.5}}},
	}
	src := &fakeVO2{status: func(d string) (any, error) { return answers[d], nil }}
	data := runVO2(t, src, "2024-01-13", "2024-01-14")
	var values []any
	for _, p := range asList(data["trend"]) {
		values = append(values, asMap(p)["vo2_max"])
	}
	if data["sport"] != "running" || !reflect.DeepEqual(values, []any{48.0, 48.5}) {
		t.Errorf("%v", data)
	}
}

func TestTrainingTrendsSurfaceUpstreamRequestFailuresVo2(t *testing.T) {
	for _, code := range []int{401, 429, 503} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			src := &fakeVO2{status: func(string) (any, error) {
				return nil, &garmin.APIError{Status: code, Message: "failed request"}
			}}
			v, err := vo2maxTrend(context.Background(), src, "2024-01-13", "2024-01-15")
			if v != nil || err == nil || !strings.HasPrefix(err.Error(), "Error retrieving") || !strings.Contains(err.Error(), strconv.Itoa(code)) {
				t.Errorf("%v %v", v, err)
			}
			if !reflect.DeepEqual(src.statusDates, []string{"2024-01-13"}) {
				t.Errorf("status dates = %v", src.statusDates)
			}
		})
	}
}
