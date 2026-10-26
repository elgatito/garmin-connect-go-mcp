package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Ported from tests/integration/test_challenges_tools.py. The Python tests mock the garminconnect methods;
// these run the real API methods against the fake server and assert the requests as well.

const (
	goalsRoute      = "GET /goal-service/goal/goals"
	prRoute         = "GET /personalrecord-service/personalrecord/prs/testuser"
	badgesRoute     = "GET /badge-service/badge/earned"
	adhocRoute      = "GET /adhocchallenge-service/adHocChallenge/historical"
	availableRoute  = "GET /badgechallenge-service/badgeChallenge/available"
	completedRoute  = "GET /badgechallenge-service/badgeChallenge/completed"
	nonComplRoute   = "GET /badgechallenge-service/badgeChallenge/non-completed"
	predictRoute    = "GET /metrics-service/metrics/racepredictions/latest/testuser"
	virtualRoute    = "GET /badgechallenge-service/virtualChallenge/inProgress"
	sameOriginValue = "same-origin"
)

func asSlice(v any) []any { return v.([]any) }

// goalsHandler serves goal-service the way Garmin does: Connect UI goals only with the fetch-metadata header and a
// 1-based start; the library's own request (no header, start=0) gets legacy.
func goalsHandler(ui []any, legacy any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var body any = []any{}
		if r.Header.Get("Sec-Fetch-Site") == sameOriginValue {
			var start, limit int
			fmt.Sscan(q.Get("start"), &start)
			fmt.Sscan(q.Get("limit"), &limit)
			if start >= 1 && start-1 < len(ui) {
				body = ui[start-1 : min(start-1+limit, len(ui))]
			}
		} else if q.Get("start") == "0" && legacy != nil {
			body = legacy
		}
		writeBody(w, r, http.StatusOK, body)
	}
}

// goalCalls splits recorded goal requests into those with and without the Sec-Fetch-Site header.
func goalCalls(f *fakeGarmin) (withHeader, without []recordedCall) {
	for _, c := range f.requests(goalsRoute) {
		if c.Header.Get("Sec-Fetch-Site") == sameOriginValue {
			withHeader = append(withHeader, c)
		} else {
			without = append(without, c)
		}
	}
	return
}

func legacyGoal() map[string]any {
	return map[string]any{"goalType": "STEPS", "goalValue": 8000.0}
}

func setLocal(t *testing.T, loc *time.Location) {
	t.Helper()
	old := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = old })
}

func TestCompletedBadgeChallengeDescriptionMatchesLibraryEndpoint(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(completedRoute, []any{})
	cs := newSession(t, f, nil)
	callOK(t, cs, "get_badge_challenges", nil)
	c := theCall(t, f, completedRoute)
	if c.Query.Get("start") != "1" || c.Query.Get("limit") != "20" || len(c.Query) != 2 {
		t.Errorf("query = %v", c.Query)
	}
	for _, tool := range listTools(t, cs) {
		if tool.Name != "get_badge_challenges" {
			continue
		}
		if strings.Contains(tool.Description, "completed and in-progress") || !strings.Contains(tool.Description, "get_non_completed_badge_challenges") {
			t.Errorf("description = %q", tool.Description)
		}
		return
	}
	t.Fatal("get_badge_challenges not listed")
}

func TestGetGoalsActive(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(goalsRoute, goalsHandler(nil, fixture(t, "MOCK_GOALS")))
	got := decode(t, callOK(t, newSession(t, f, nil), "get_goals", map[string]any{"goal_type": "active"}))
	want := fixture(t, "MOCK_GOALS").(map[string]any)["goals"]
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	_, legacy := goalCalls(f)
	if len(legacy) != 1 || legacy[0].Query.Get("status") != "active" {
		t.Errorf("legacy calls = %v", legacy)
	}
}

func TestGetGoalsDefault(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(goalsRoute, goalsHandler(nil, fixture(t, "MOCK_GOALS")))
	callOK(t, newSession(t, f, nil), "get_goals", nil)
	ui, legacy := goalCalls(f)
	if len(legacy) != 1 || legacy[0].Query.Get("status") != "active" || ui[0].Query.Get("status") != "active" {
		t.Errorf("ui=%v legacy=%v", ui, legacy)
	}
}

func TestGetGoalsFuture(t *testing.T) {
	future := map[string]any{"goals": []any{map[string]any{"goalType": "STEPS", "goalValue": 10000.0, "startDate": "2024-02-01"}}}
	f := newFakeGarmin(t)
	f.on(goalsRoute, goalsHandler(nil, future))
	got := decode(t, callOK(t, newSession(t, f, nil), "get_goals", map[string]any{"goal_type": " Future "}))
	if !reflect.DeepEqual(got, future["goals"]) {
		t.Errorf("got %v", got)
	}
	ui, legacy := goalCalls(f)
	if ui[0].Query.Get("status") != "future" || len(legacy) != 1 || legacy[0].Query.Get("status") != "future" {
		t.Errorf("ui=%v legacy=%v", ui, legacy)
	}
}

func connectUIGoal() map[string]any {
	return map[string]any{
		"id": 1.0, "name": "GCC 2026", "type": "distance_accumulation", "distanceInMeters": 160934.0,
		"startDate": "2026-09-01", "endDate": "2026-09-30", "activityType": "cycling", "period": "one_month",
		"progress":  map[string]any{"percent": 17.0, "distanceInMeters": 28089.0},
		"remaining": map[string]any{"percent": 83.0, "days": 13.0, "distanceInMeters": 132845.0},
		"active":    true, "completed": false,
	}
}

func TestGetGoalsReturnsConnectUiCyclingGoal(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(goalsRoute, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.Header.Get("Sec-Fetch-Site") != sameOriginValue || q.Get("start") == "0" || q.Get("status") != "active" {
			writeBody(w, r, 200, []any{})
			return
		}
		writeBody(w, r, 200, []any{connectUIGoal()})
	})
	data := asSlice(decode(t, callOK(t, newSession(t, f, nil), "get_goals", map[string]any{"goal_type": "active"})))
	g := asMap(data[0])
	if g["name"] != "GCC 2026" || g["activity_type"] != "cycling" || g["progress_percent"] != 17.0 {
		t.Errorf("goal = %v", g)
	}
	if _, legacy := goalCalls(f); len(legacy) != 0 {
		t.Errorf("legacy get_goals called: %v", legacy)
	}
}

func TestGetGoalsInvalidType(t *testing.T) {
	text := callOK(t, newSession(t, newFakeGarmin(t), nil), "get_goals", map[string]any{"goal_type": "Soon"})
	if text != "Invalid goal_type 'soon'. Options: active, future, past." {
		t.Errorf("text = %q", text)
	}
}

func TestGetPersonalRecordTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(prRoute, fixture(t, "MOCK_PERSONAL_RECORD"))
	got := decode(t, callOK(t, newSession(t, f, nil), "get_personal_record", nil))
	want := []any{
		map[string]any{"record_type": "Fastest 5K", "type_id": 3.0, "value": "20:00", "raw_value": 1200.0,
			"date": "2024-01-14", "activity_id": 12345678901.0},
		map[string]any{"record_type": "Most Steps Day", "type_id": 12.0, "value": "25,000", "raw_value": 25000.0, "date": "2024-01-10"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	theCall(t, f, prRoute)
}

func TestGetPersonalRecordDateFallsBackToGmtInUtc(t *testing.T) {
	setLocal(t, time.FixedZone("LA", -8*3600))
	rec := fixture(t, "MOCK_PERSONAL_RECORD").([]any)[0].(map[string]any)
	delete(rec, "prStartTimeLocalFormatted")
	f := newFakeGarmin(t)
	f.on(prRoute, []any{rec})
	data := asSlice(decode(t, callOK(t, newSession(t, f, nil), "get_personal_record", nil)))
	if asMap(data[0])["date"] != "2024-01-15" {
		t.Errorf("date = %v", asMap(data[0])["date"])
	}
}

func TestGetEarnedBadgesTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(badgesRoute, fixture(t, "MOCK_BADGES"))
	d := resultMap(t, callOK(t, newSession(t, f, nil), "get_earned_badges", nil))
	want := map[string]any{"name": "10K Steps - 7 Days", "category": "category_None", "difficulty": "level_None", "points": nil, "earned_date": nil}
	if d["total_badges"] != 1.0 || !reflect.DeepEqual(asList(d["badges"])[0], want) {
		t.Errorf("got %v", d)
	}
	theCall(t, f, badgesRoute)
}

func TestGetEarnedBadgesCurationAndOrder(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(badgesRoute, []any{
		map[string]any{"badgeName": "Old", "badgeCategoryId": 1.0, "badgeDifficultyId": 2.0, "badgePoints": 5.0,
			"badgeEarnedDate": "2023-03-01T10:00:00.0"},
		map[string]any{"badgeName": "New", "badgeCategoryId": 9.0, "badgeDifficultyId": 7.0, "badgePoints": 10.0,
			"badgeEarnedDate": "2024-03-31T18:55:47.534", "badgeUnitId": 1.0, "badgeProgressValue": 1500.0, "badgeTargetValue": 5000.0,
			"badgeStartDate": "2024-03-01T00:00:00.0", "badgeEndDate": "2024-03-31T00:00:00.0",
			"badgeAssocType": "activityId", "badgeAssocDataId": 99.0, "badgeSeriesId": 4.0},
	})
	d := resultMap(t, callOK(t, newSession(t, f, nil), "get_earned_badges", nil))
	want := []any{
		map[string]any{"name": "New", "category": "Diving", "difficulty": "level_7", "points": 10.0, "earned_date": "2024-03-31",
			"progress": "1.50 km", "target": "5.00 km", "challenge_period": "2024-03-01 to 2024-03-31", "activity_id": 99.0, "series_id": 4.0},
		map[string]any{"name": "Old", "category": "Activity", "difficulty": "Medium", "points": 5.0, "earned_date": "2023-03-01"},
	}
	if !reflect.DeepEqual(d["badges"], want) {
		t.Errorf("got %v", d["badges"])
	}
}

func adhocItem() map[string]any {
	return map[string]any{
		"socialChallengeStatusId": 2.0, "socialChallengeActivityTypeId": 4.0, "adHocChallengeName": "January Step Challenge",
		"adHocChallengeDesc": "Steps Challenge", "uuid": "ABC123", "startDate": "2024-01-01T00:00:00.0",
		"endDate": "2024-01-31T23:59:59.0", "userRanking": 1.0, "playerCount": 5.0,
	}
}

func TestGetAdhocChallengesDefault(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(adhocRoute, []any{adhocItem()})
	d := resultMap(t, callOK(t, newSession(t, f, nil), "get_adhoc_challenges", nil))
	want := map[string]any{"name": "January Step Challenge", "description": "Steps Challenge", "uuid": "ABC123", "activity_type": "Steps",
		"status": "In Progress", "start_date": "2024-01-01", "end_date": "2024-01-31", "your_ranking": 1.0, "player_count": 5.0}
	if d["total"] != 1.0 || !reflect.DeepEqual(asList(d["challenges"])[0], want) {
		t.Errorf("got %v", d)
	}
	c := theCall(t, f, adhocRoute)
	if c.Query.Get("start") != "0" || c.Query.Get("limit") != "20" || len(c.Query) != 2 {
		t.Errorf("query = %v", c.Query)
	}
}

func TestGetAdhocChallengesCustomParams(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(adhocRoute, []any{})
	text := callOK(t, newSession(t, f, nil), "get_adhoc_challenges", map[string]any{"start": 10, "limit": 50})
	c := theCall(t, f, adhocRoute)
	if text != "No adhoc challenges found." || c.Query.Get("start") != "10" || c.Query.Get("limit") != "50" {
		t.Errorf("text=%q query=%v", text, c.Query)
	}
}

func TestGetAdhocChallengesCapsLimitAndValidates(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(adhocRoute, []any{})
	cs := newSession(t, f, nil)
	callOK(t, cs, "get_adhoc_challenges", map[string]any{"limit": 500})
	if got := theCall(t, f, adhocRoute).Query.Get("limit"); got != "100" {
		t.Errorf("limit = %s", got)
	}
	text, isErr := callTool(t, cs, "get_adhoc_challenges", map[string]any{"start": -1})
	if !isErr || text != "Error retrieving adhoc challenges: start must be non-negative, got: -1" {
		t.Errorf("%v %q", isErr, text)
	}
	text, isErr = callTool(t, cs, "get_adhoc_challenges", map[string]any{"limit": 0})
	if !isErr || text != "Error retrieving adhoc challenges: limit must be a positive integer, got: 0" {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetAvailableBadgeChallengesTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(availableRoute, []any{map[string]any{
		"uuid": "ABC123", "badgeChallengeName": "Marathon Challenge", "challengeCategoryId": 1.0, "badgeChallengeStatusId": 2.0,
		"startDate": "2024-01-01T00:00:00.0", "endDate": "2024-01-31T23:59:59.0", "badgePoints": 4.0, "badgeUnitId": 1.0,
		"badgeProgressValue": nil, "badgeTargetValue": 42195.0, "userJoined": false, "joinable": true,
	}})
	d := resultMap(t, callOK(t, newSession(t, f, nil), "get_available_badge_challenges", nil))
	want := map[string]any{"name": "Marathon Challenge", "uuid": "ABC123", "category": "Running", "status": "In Progress", "points": 4.0,
		"start_date": "2024-01-01", "end_date": "2024-01-31", "joined": false, "progress": nil, "target": "42.20 km",
		"progress_percent": nil, "joinable": true}
	if d["total"] != 1.0 || !reflect.DeepEqual(asList(d["challenges"])[0], want) {
		t.Errorf("got %v", d)
	}
	c := theCall(t, f, availableRoute)
	if c.Query.Get("start") != "1" || c.Query.Get("limit") != "20" {
		t.Errorf("query = %v", c.Query)
	}
}

func TestGetBadgeChallengesTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(completedRoute, []any{fixture(t, "MOCK_BADGES").([]any)[0]})
	d := resultMap(t, callOK(t, newSession(t, f, nil), "get_badge_challenges", map[string]any{"start": 1, "limit": 50}))
	want := map[string]any{"name": nil, "uuid": nil, "category": "category_None", "status": "status_None", "points": nil,
		"start_date": nil, "end_date": nil, "joined": false}
	if d["total"] != 1.0 || !reflect.DeepEqual(asList(d["challenges"])[0], want) {
		t.Errorf("got %v", d)
	}
	c := theCall(t, f, completedRoute)
	if c.Query.Get("start") != "1" || c.Query.Get("limit") != "50" {
		t.Errorf("query = %v", c.Query)
	}
}

func TestGetNonCompletedBadgeChallengesTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(nonComplRoute, []any{map[string]any{
		"uuid": "DEF456", "badgeChallengeName": "Ultra Marathon Challenge", "challengeCategoryId": 1.0, "badgeChallengeStatusId": 2.0,
		"startDate": "2024-01-01T00:00:00.0", "endDate": "2024-01-31T23:59:59.0", "badgePoints": 4.0, "badgeUnitId": 1.0,
		"badgeProgressValue": 21000.0, "badgeTargetValue": 50000.0, "badgeEarnedDate": nil, "userJoined": true,
	}})
	d := resultMap(t, callOK(t, newSession(t, f, nil), "get_non_completed_badge_challenges", nil))
	want := map[string]any{"name": "Ultra Marathon Challenge", "uuid": "DEF456", "category": "Running", "status": "In Progress",
		"points": 4.0, "start_date": "2024-01-01", "end_date": "2024-01-31", "joined": true,
		"progress": "21.00 km", "target": "50.00 km", "progress_percent": "42.0%"}
	if !reflect.DeepEqual(asList(d["challenges"])[0], want) {
		t.Errorf("got %v", d)
	}
	c := theCall(t, f, nonComplRoute)
	if c.Query.Get("start") != "1" || c.Query.Get("limit") != "20" {
		t.Errorf("query = %v", c.Query)
	}
}

func TestBadgeChallengeSortOrders(t *testing.T) {
	mk := func(name, start, end string) any {
		return map[string]any{"badgeChallengeName": name, "startDate": start, "endDate": end}
	}
	items := []any{mk("b", "2024-02-01T00:00:00.0", "2024-02-28T00:00:00.0"), mk("a", "2024-01-01T00:00:00.0", "2024-03-31T00:00:00.0"),
		mk("c", "2024-03-01T00:00:00.0", "2024-03-15T00:00:00.0")}
	names := func(route, tool string) string {
		f := newFakeGarmin(t)
		f.on(route, items)
		var out []string
		for _, c := range asList(resultMap(t, callOK(t, newSession(t, f, nil), tool, nil))["challenges"]) {
			out = append(out, asMap(c)["name"].(string))
		}
		return strings.Join(out, "")
	}
	for route, tc := range map[string][2]string{
		availableRoute: {"get_available_badge_challenges", "abc"},     // start date, soonest first
		completedRoute: {"get_badge_challenges", "cba"},               // start date, most recent first
		nonComplRoute:  {"get_non_completed_badge_challenges", "bca"}, // end date, soonest first
	} {
		if got := names(route, tc[0]); got != tc[1] {
			t.Errorf("%s order = %s, want %s", tc[0], got, tc[1])
		}
	}
}

func TestGetRacePredictionsTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(predictRoute, map[string]any{
		"5K": map[string]any{"time": 1200.0, "unit": "seconds"}, "10K": map[string]any{"time": 2520.0, "unit": "seconds"},
		"halfMarathon": map[string]any{"time": 5400.0, "unit": "seconds"}, "marathon": map[string]any{"time": 11400.0, "unit": "seconds"},
	})
	d := resultMap(t, callOK(t, newSession(t, f, nil), "get_race_predictions", nil))
	nothing := map[string]any{"time": nil, "time_seconds": nil}
	want := map[string]any{"prediction_date": nil, "predictions": map[string]any{
		"5K": nothing, "10K": nothing, "half_marathon": nothing, "marathon": nothing}}
	if !reflect.DeepEqual(d, want) {
		t.Errorf("got %v", d)
	}
	theCall(t, f, predictRoute)
}

func TestGetRacePredictionsFormatsTimes(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(predictRoute, map[string]any{"calendarDate": "2024-01-15", "time5K": 1200.0, "time10K": 2519.9,
		"timeHalfMarathon": 5400.0, "timeMarathon": 11400.0})
	d := resultMap(t, callOK(t, newSession(t, f, nil), "get_race_predictions", nil))
	p := asMap(d["predictions"])
	if d["prediction_date"] != "2024-01-15" || asMap(p["5K"])["time"] != "20:00" || asMap(p["10K"])["time"] != "41:59" ||
		asMap(p["half_marathon"])["time"] != "1:30:00" || asMap(p["marathon"])["time"] != "3:10:00" || asMap(p["10K"])["time_seconds"] != 2519.9 {
		t.Errorf("got %v", d)
	}
}

func TestGetInprogressVirtualChallengesTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(virtualRoute, []any{map[string]any{
		"uuid": "GHI789", "badgeChallengeName": "Camino de Santiago", "startDate": nil, "endDate": nil, "badgeUnitId": 1.0,
		"badgeProgressValue": 159.0, "badgeTargetValue": 784000.0, "userJoined": true,
	}})
	got := decode(t, callOK(t, newSession(t, f, nil), "get_inprogress_virtual_challenges", nil))
	want := map[string]any{"total": 1.0, "challenges": []any{map[string]any{
		"name": "Camino de Santiago", "uuid": "GHI789", "start_date": nil, "end_date": nil, "progress_meters": 159.0,
		"target_meters": 784000.0, "progress_km": "0.16 km", "target_km": "784.00 km", "progress_percent": "0.0%",
	}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	c := theCall(t, f, virtualRoute)
	if c.Query.Get("start") != "1" || c.Query.Get("limit") != "20" {
		t.Errorf("query = %v", c.Query)
	}
}

func TestGetInprogressVirtualChallengesRejectsStartZero(t *testing.T) {
	f := newFakeGarmin(t)
	text, isErr := callTool(t, newSession(t, f, nil), "get_inprogress_virtual_challenges", map[string]any{"start": 0})
	if !isErr || text != "Error retrieving in-progress virtual challenges: start must be a positive integer, got: 0" || len(f.requests(virtualRoute)) != 0 {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetInprogressVirtualChallengesLegacyFields(t *testing.T) {
	for _, c := range []struct {
		name     string
		fields   map[string]any
		wantName string
		progress float64
		percent  string
		km       string
	}{
		{"name_progress_target", map[string]any{"name": "Legacy Expedition", "progress": 28000.0, "target": 42195.0}, "Legacy Expedition", 28000.0, "66.4%", "28.00 km"},
		{"challengeName_progressValue", map[string]any{"challengeName": "Alternate Legacy Expedition", "progressValue": 0.0, "targetValue": 42195.0},
			"Alternate Legacy Expedition", 0.0, "0.0%", "0.00 km"},
	} {
		t.Run(c.name, func(t *testing.T) {
			item := map[string]any{"uuid": "LEGACY123", "startDate": "2024-01-01T00:00:00.0", "endDate": "2024-01-31T23:59:59.0"}
			for k, v := range c.fields {
				item[k] = v
			}
			f := newFakeGarmin(t)
			f.on(virtualRoute, []any{item})
			d := resultMap(t, callOK(t, newSession(t, f, nil), "get_inprogress_virtual_challenges", nil))
			want := map[string]any{"name": c.wantName, "uuid": "LEGACY123", "start_date": "2024-01-01", "end_date": "2024-01-31",
				"progress_meters": c.progress, "target_meters": 42195.0, "progress_km": c.km, "target_km": "42.20 km", "progress_percent": c.percent}
			if !reflect.DeepEqual(asList(d["challenges"])[0], want) {
				t.Errorf("got %v", asList(d["challenges"])[0])
			}
		})
	}
}

func TestGetInprogressVirtualChallengesNonDistanceUnits(t *testing.T) {
	for _, c := range []struct {
		unit             float64
		progress, target float64
		fp, ft           string
	}{
		{2, 120, 1000, "120 m", "1000 m"},
		{5, 120000, 200000, "120,000", "200,000"},
		{7, 3600, 7200, "1:00:00", "2:00:00"},
	} {
		t.Run(fmt.Sprint(c.unit), func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on(virtualRoute, []any{map[string]any{"uuid": "NON_DISTANCE", "badgeChallengeName": "Non-distance Expedition",
				"badgeUnitId": c.unit, "badgeProgressValue": c.progress, "badgeTargetValue": c.target}})
			ch := asMap(asList(resultMap(t, callOK(t, newSession(t, f, nil), "get_inprogress_virtual_challenges", nil))["challenges"])[0])
			if ch["progress"] != c.fp || ch["target"] != c.ft {
				t.Errorf("got %v", ch)
			}
			for _, k := range []string{"progress_meters", "target_meters", "progress_km", "target_km"} {
				if _, ok := ch[k]; ok {
					t.Errorf("unexpected %s", k)
				}
			}
		})
	}
}

func TestGetInprogressVirtualChallengesSkipsZeroTarget(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(virtualRoute, []any{map[string]any{"uuid": "ZERO_TARGET", "badgeChallengeName": "", "name": "Fallback Name",
		"badgeUnitId": 1.0, "badgeProgressValue": 0.0, "badgeTargetValue": 0.0}})
	ch := asMap(asList(resultMap(t, callOK(t, newSession(t, f, nil), "get_inprogress_virtual_challenges", nil))["challenges"])[0])
	want := map[string]any{"name": "Fallback Name", "uuid": "ZERO_TARGET", "start_date": nil, "end_date": nil}
	if !reflect.DeepEqual(ch, want) {
		t.Errorf("got %v", ch)
	}
}

func TestGetInprogressVirtualChallengesDictPayload(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(virtualRoute, map[string]any{"challenges": []any{map[string]any{"uuid": "X", "name": "N"}}})
	d := resultMap(t, callOK(t, newSession(t, f, nil), "get_inprogress_virtual_challenges", nil))
	if d["total"] != 1.0 {
		t.Errorf("got %v", d)
	}
}

func TestGetGoalsNoData(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(goalsRoute, goalsHandler(nil, nil))
	text, isErr := callTool(t, newSession(t, f, nil), "get_goals", map[string]any{"goal_type": "active"})
	if isErr || text != "No active goals found." {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestGetPersonalRecordException(t *testing.T) {
	f := newFakeGarmin(t)
	f.on(prRoute, status(500, map[string]any{"message": "boom"}))
	text, isErr := callTool(t, newSession(t, f, nil), "get_personal_record", nil)
	if !isErr || text != "Error retrieving personal records: API Error 500 - boom" {
		t.Errorf("%v %q", isErr, text)
	}
}

// ── tests/unit/test_goals.py ───────────────────────────────────────────────

func goalsGoal() map[string]any {
	return map[string]any{
		"id": 41139679.0, "name": "GCC 2026", "type": "distance_accumulation", "distanceInMeters": 160934.0,
		"durationInSeconds": nil, "caloriesInKiloCalories": nil, "numberOfActivities": nil, "startDate": "2026-09-01",
		"activityType": "cycling", "period": "one_month", "privacy": "public",
		"progress": map[string]any{"percent": 17.0, "days": 17.0, "distanceInMeters": 28089.0, "durationInSeconds": nil,
			"caloriesInKiloCalories": nil, "numberOfActivities": nil},
		"remaining": map[string]any{"percent": 83.0, "days": 13.0, "distanceInMeters": 132845.0, "durationInSeconds": nil,
			"caloriesInKiloCalories": nil, "numberOfActivities": nil},
		"overage": map[string]any{"percent": 0.0, "days": 0.0, "distanceInMeters": 0.0},
		"endDate": "2026-09-30", "active": true, "completed": false,
	}
}

// fakeGoalsClient is _FakeClient: goals only with the header and a 1-based start.
type fakeGoalsClient struct {
	byStatus    map[string][]any
	legacy      any
	ignoreStart bool
	pageErr     error
	legacyCalls []string
	pages       []fakeGoalsPage
}

type fakeGoalsPage struct {
	status string
	start  int
}

func (c *fakeGoalsClient) GoalsPage(_ context.Context, status string, start int) (any, error) {
	c.pages = append(c.pages, fakeGoalsPage{status, start})
	if c.pageErr != nil {
		return nil, c.pageErr
	}
	goals := c.byStatus[status]
	if c.ignoreStart {
		return goals, nil
	}
	if start < 1 || start-1 >= len(goals) {
		return []any{}, nil
	}
	return goals[start-1 : min(start-1+goalsPageSize, len(goals))], nil
}

func (c *fakeGoalsClient) GetGoals(_ context.Context, status string) (any, error) {
	c.legacyCalls = append(c.legacyCalls, status)
	return c.legacy, nil
}

func TestAsGoalListUnwrapsLegacyAndModernShapes(t *testing.T) {
	g := goalsGoal()
	want := []map[string]any{g}
	for name, in := range map[string]any{"list": []any{g}, "goals": map[string]any{"goals": []any{g}}, "bare": g} {
		if got := asGoalList(in); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v", name, got)
		}
	}
	if len(asGoalList(nil)) != 0 || len(asGoalList(struct{}{})) != 0 {
		t.Error("nil and unknown shapes must give no goals")
	}
}

func TestCurateConnectUiGoalFlattensProgress(t *testing.T) {
	g := goalsGoal()
	c := curateConnectUIGoal(g)
	if c["name"] != "GCC 2026" || c["activity_type"] != "cycling" || c["target_distance_meters"] != 160934.0 ||
		c["progress_percent"] != 17.0 || c["progress_distance_meters"] != 28089.0 || c["remaining_distance_meters"] != 132845.0 ||
		c["remaining_days"] != 13.0 {
		t.Errorf("curated = %v", c)
	}
	if _, ok := c["target_duration_seconds"]; ok {
		t.Error("null fields must be dropped")
	}
	if !isConnectUIGoal(g) || isConnectUIGoal(legacyGoal()) {
		t.Error("isConnectUIGoal misclassifies")
	}
}

func TestCollectGoalsSendsHeaderAndOneBasedStart(t *testing.T) {
	// Through the real client: the request carries Sec-Fetch-Site and a 1-based start, and no userId.
	f := newFakeGarmin(t)
	f.on(goalsRoute, goalsHandler([]any{goalsGoal()}, []any{}))
	data := asSlice(decode(t, callOK(t, newSession(t, f, nil), "get_goals", nil)))
	if asMap(data[0])["name"] != "GCC 2026" || asMap(data[0])["progress_percent"] != 17.0 {
		t.Errorf("got %v", data)
	}
	ui, legacy := goalCalls(f)
	q := ui[0].Query
	if len(legacy) != 0 || len(ui) != 1 || ui[0].Path != "/goal-service/goal/goals" || q.Get("status") != "active" || q.Get("start") != "1" ||
		q.Get("limit") != "100" || q.Get("sortOrder") != "asc" || q.Has("userId") || len(q) != 4 {
		t.Errorf("ui=%v legacy=%v", ui, legacy)
	}
}

func TestCollectGoalsPassesStatusThrough(t *testing.T) {
	past := goalsGoal()
	past["id"], past["active"], past["completed"] = 1.0, false, true
	c := &fakeGoalsClient{byStatus: map[string][]any{"active": {goalsGoal()}, "past": {past}}, legacy: []any{}}
	goals, _ := collectGoals(context.Background(), c, "past")
	if len(goals) != 1 || asMap(goals[0])["id"] != 1.0 || c.pages[0].status != "past" {
		t.Errorf("goals=%v pages=%v", goals, c.pages)
	}
}

func TestCollectGoalsPaginatesFromOne(t *testing.T) {
	var many []any
	for i := range 150 {
		g := goalsGoal()
		g["id"] = float64(i)
		many = append(many, g)
	}
	c := &fakeGoalsClient{byStatus: map[string][]any{"past": many}, legacy: []any{}}
	goals, _ := collectGoals(context.Background(), c, "past")
	if len(goals) != 150 || asMap(goals[149])["id"] != 149.0 || !reflect.DeepEqual(c.pages, []fakeGoalsPage{{"past", 1}, {"past", 101}}) {
		t.Errorf("len=%d pages=%v", len(goals), c.pages)
	}
}

func TestCollectGoalsStopsIfServerIgnoresStart(t *testing.T) {
	var full []any
	for i := range 100 {
		g := goalsGoal()
		g["id"] = float64(i)
		full = append(full, g)
	}
	c := &fakeGoalsClient{byStatus: map[string][]any{"active": full}, legacy: []any{}, ignoreStart: true}
	goals, _ := collectGoals(context.Background(), c, "active")
	if len(goals) != 100 || len(c.pages) != 2 {
		t.Errorf("len=%d pages=%v", len(goals), c.pages)
	}
}

func TestCollectGoalsPassesLegacyWellnessGoalsThrough(t *testing.T) {
	c := &fakeGoalsClient{byStatus: map[string][]any{"active": {legacyGoal()}}, legacy: []any{}}
	goals, _ := collectGoals(context.Background(), c, "active")
	if !reflect.DeepEqual(goals, []any{legacyGoal()}) {
		t.Errorf("goals = %v", goals)
	}
}

func TestCollectGoalsFallsBackToLibraryWhenEmpty(t *testing.T) {
	c := &fakeGoalsClient{legacy: map[string]any{"goals": []any{legacyGoal()}}}
	goals, _ := collectGoals(context.Background(), c, "active")
	if !reflect.DeepEqual(goals, []any{legacyGoal()}) || !reflect.DeepEqual(c.legacyCalls, []string{"active"}) {
		t.Errorf("goals=%v calls=%v", goals, c.legacyCalls)
	}
}

func TestCollectGoalsFallsBackToLibraryOnError(t *testing.T) {
	c := &fakeGoalsClient{legacy: []any{legacyGoal()}, pageErr: errors.New("API Error 500")}
	goals, _ := collectGoals(context.Background(), c, "active")
	if !reflect.DeepEqual(goals, []any{legacyGoal()}) || !reflect.DeepEqual(c.legacyCalls, []string{"active"}) {
		t.Errorf("goals=%v calls=%v", goals, c.legacyCalls)
	}
}
