package server

import (
	"fmt"
	"strings"
	"testing"
)

const (
	challengesGoalsPath        = "/goal-service/goal/goals"
	challengesPRPath           = "/personalrecord-service/personalrecord/prs/testuser"
	challengesEarnedPath       = "/badge-service/badge/earned"
	challengesAdhocPath        = "/adhocchallenge-service/adHocChallenge/historical"
	challengesCompletedPath    = "/badgechallenge-service/badgeChallenge/completed"
	challengesAvailablePath    = "/badgechallenge-service/badgeChallenge/available"
	challengesNonCompletedPath = "/badgechallenge-service/badgeChallenge/non-completed"
	challengesVirtualPath      = "/badgechallenge-service/virtualChallenge/inProgress"
	challengesPredictionsPath  = "/metrics-service/metrics/racepredictions/latest/testuser"
)

func goalsList(t *testing.T) []any {
	t.Helper()
	return asList(asMap(fixture(t, "MOCK_GOALS"))["goals"])
}

func connectUIGoal() map[string]any {
	return map[string]any{
		"id":               1.0,
		"name":             "GCC 2026",
		"type":             "distance_accumulation",
		"distanceInMeters": 160934.0,
		"startDate":        "2026-09-01",
		"endDate":          "2026-09-30",
		"activityType":     "cycling",
		"period":           "one_month",
		"progress":         map[string]any{"percent": 17.0, "distanceInMeters": 28089.0},
		"remaining":        map[string]any{"percent": 83.0, "days": 13.0, "distanceInMeters": 132845.0},
		"active":           true,
		"completed":        false,
	}
}

func TestCompletedBadgeChallengeDescriptionMatchesLibraryEndpoint(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+challengesCompletedPath, []any{})
	cs := newSession(t, f, nil)

	if _, isErr := callTool(t, cs, "get_badge_challenges", nil); isErr {
		t.Fatal("get_badge_challenges returned an error")
	}
	calls := f.callsMatching("GET", challengesCompletedPath)
	if len(calls) != 1 {
		t.Fatalf("completed endpoint calls = %d, want 1", len(calls))
	}
	if calls[0].Query.Get("start") != "1" || calls[0].Query.Get("limit") != "20" {
		t.Fatalf("query = %v", calls[0].Query)
	}

	description := ""
	for _, tool := range listToolObjects(t, cs) {
		if tool.Name == "get_badge_challenges" {
			description = tool.Description
		}
	}
	if strings.Contains(description, "completed and in-progress") {
		t.Fatalf("description = %q", description)
	}
	if !strings.Contains(description, "get_non_completed_badge_challenges") {
		t.Fatalf("description = %q", description)
	}
}

func TestGetGoalsActive(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+challengesGoalsPath, sequence{rawBody("[]"), goalsList(t), rawBody("[]")})
	cs := newSession(t, f, nil)

	if _, isErr := callTool(t, cs, "get_goals", map[string]any{"goal_type": "active"}); isErr {
		t.Fatal("get_goals returned an error")
	}
	calls := f.callsMatching("GET", challengesGoalsPath)
	if len(calls) < 2 {
		t.Fatalf("calls = %d", len(calls))
	}
	if calls[len(calls)-1].Query.Get("status") != "active" {
		t.Fatalf("status = %q", calls[len(calls)-1].Query.Get("status"))
	}
	if calls[1].Query.Get("start") != "0" {
		t.Fatalf("legacy start = %q, want 0", calls[1].Query.Get("start"))
	}
}

func TestGetGoalsDefault(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+challengesGoalsPath, sequence{rawBody("[]"), goalsList(t), rawBody("[]")})
	cs := newSession(t, f, nil)

	if _, isErr := callTool(t, cs, "get_goals", nil); isErr {
		t.Fatal("get_goals returned an error")
	}
	calls := f.callsMatching("GET", challengesGoalsPath)
	last := calls[len(calls)-1]
	if last.Query.Get("status") != "active" {
		t.Fatalf("status = %q, want active", last.Query.Get("status"))
	}
}

func TestGetGoalsFuture(t *testing.T) {
	f := newFakeGarmin(t)
	future := []any{map[string]any{"goalType": "STEPS", "goalValue": 10000.0, "startDate": "2024-02-01"}}
	f.on("GET "+challengesGoalsPath, sequence{rawBody("[]"), future, rawBody("[]")})
	cs := newSession(t, f, nil)

	if _, isErr := callTool(t, cs, "get_goals", map[string]any{"goal_type": "future"}); isErr {
		t.Fatal("get_goals returned an error")
	}
	calls := f.callsMatching("GET", challengesGoalsPath)
	last := calls[len(calls)-1]
	if last.Query.Get("status") != "future" {
		t.Fatalf("status = %q, want future", last.Query.Get("status"))
	}
}

func TestGetGoalsReturnsConnectUICyclingGoal(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+challengesGoalsPath, []any{connectUIGoal()})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_goals", map[string]any{"goal_type": "active"})
	var data []map[string]any
	decodeJSON(t, text, &data)
	if len(data) != 1 {
		t.Fatalf("goals = %d", len(data))
	}
	if data[0]["name"] != "GCC 2026" || data[0]["activity_type"] != "cycling" {
		t.Fatalf("goal = %v", data[0])
	}
	if data[0]["progress_percent"] != 17.0 {
		t.Fatalf("progress_percent = %v", data[0]["progress_percent"])
	}
	if calls := f.callsMatching("GET", challengesGoalsPath); len(calls) != 1 {
		t.Fatalf("calls = %d, want no legacy call", len(calls))
	}
}

func TestGetPersonalRecordTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+challengesPRPath, fixture(t, "MOCK_PERSONAL_RECORD"))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_personal_record", nil)
	var data []any
	decodeJSON(t, text, &data)
	want := []any{
		map[string]any{
			"record_type": "Fastest 5K",
			"type_id":     3.0,
			"value":       "20:00",
			"raw_value":   1200.0,
			"date":        "2024-01-14",
			"activity_id": 12345678901.0,
		},
		map[string]any{
			"record_type": "Most Steps Day",
			"type_id":     12.0,
			"value":       "25,000",
			"raw_value":   25000.0,
			"date":        "2024-01-10",
		},
	}
	sameJSON(t, data, want)
}

func TestGetPersonalRecordDateFallsBackToGmtInUtc(t *testing.T) {
	f := newFakeGarmin(t)
	record := asMap(fixture(t, "MOCK_PERSONAL_RECORD").([]any)[0])
	delete(record, "prStartTimeLocalFormatted")
	f.on("GET "+challengesPRPath, []any{record})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_personal_record", nil)
	var data []map[string]any
	decodeJSON(t, text, &data)
	if len(data) != 1 || data[0]["date"] != "2024-01-15" {
		t.Fatalf("data = %v", data)
	}
}

func TestGetEarnedBadgesTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+challengesEarnedPath, fixture(t, "MOCK_BADGES"))
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_earned_badges", nil)
	if isErr {
		t.Fatalf("error: %s", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	if data["total_badges"] != 1.0 {
		t.Fatalf("data = %v", data)
	}
	if calls := f.callsMatching("GET", challengesEarnedPath); len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
}

func TestGetAdhocChallengesDefault(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+challengesAdhocPath, []any{map[string]any{
		"socialChallengeStatusId":       2,
		"socialChallengeActivityTypeId": 4,
		"adHocChallengeName":            "January Step Challenge",
		"adHocChallengeDesc":            "Steps Challenge",
		"uuid":                          "ABC123",
		"startDate":                     "2024-01-01T00:00:00.0",
		"endDate":                       "2024-01-31T23:59:59.0",
		"userRanking":                   1,
		"playerCount":                   5,
	}})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_adhoc_challenges", nil)
	var data map[string]any
	decodeJSON(t, text, &data)
	challenges := asList(data["challenges"])
	if len(challenges) != 1 {
		t.Fatalf("challenges = %v", challenges)
	}
	got := asMap(challenges[0])
	if got["activity_type"] != "Steps" || got["status"] != "In Progress" {
		t.Fatalf("challenge = %v", got)
	}
	if got["start_date"] != "2024-01-01" || got["end_date"] != "2024-01-31" {
		t.Fatalf("dates = %v", got)
	}
	calls := f.callsMatching("GET", challengesAdhocPath)
	if calls[0].Query.Get("start") != "0" || calls[0].Query.Get("limit") != "20" {
		t.Fatalf("query = %v", calls[0].Query)
	}
}

func TestGetAdhocChallengesCustomParams(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+challengesAdhocPath, []any{})
	cs := newSession(t, f, nil)

	callTool(t, cs, "get_adhoc_challenges", map[string]any{"start": 10, "limit": 50})
	calls := f.callsMatching("GET", challengesAdhocPath)
	if calls[0].Query.Get("start") != "10" || calls[0].Query.Get("limit") != "50" {
		t.Fatalf("query = %v", calls[0].Query)
	}
}

func TestGetAvailableBadgeChallengesTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+challengesAvailablePath, []any{map[string]any{
		"uuid":                   "ABC123",
		"badgeChallengeName":     "Marathon Challenge",
		"challengeCategoryId":    1,
		"badgeChallengeStatusId": 2,
		"startDate":              "2024-01-01T00:00:00.0",
		"endDate":                "2024-01-31T23:59:59.0",
		"badgePoints":            4,
		"badgeUnitId":            1,
		"badgeProgressValue":     nil,
		"badgeTargetValue":       42195.0,
		"userJoined":             false,
		"joinable":               true,
	}})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_available_badge_challenges", nil)
	var data map[string]any
	decodeJSON(t, text, &data)
	ch := asMap(asList(data["challenges"])[0])
	if ch["name"] != "Marathon Challenge" || ch["joinable"] != true {
		t.Fatalf("challenge = %v", ch)
	}
	if ch["target"] != "42.20 km" {
		t.Fatalf("target = %v", ch["target"])
	}
	calls := f.callsMatching("GET", challengesAvailablePath)
	if calls[0].Query.Get("start") != "1" || calls[0].Query.Get("limit") != "20" {
		t.Fatalf("query = %v", calls[0].Query)
	}
}

func TestGetBadgeChallengesTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+challengesCompletedPath, []any{fixture(t, "MOCK_BADGES").([]any)[0]})
	cs := newSession(t, f, nil)

	if _, isErr := callTool(t, cs, "get_badge_challenges", map[string]any{"start": 1, "limit": 50}); isErr {
		t.Fatal("get_badge_challenges returned an error")
	}
	calls := f.callsMatching("GET", challengesCompletedPath)
	if calls[0].Query.Get("start") != "1" || calls[0].Query.Get("limit") != "50" {
		t.Fatalf("query = %v", calls[0].Query)
	}
}

func TestGetNonCompletedBadgeChallengesTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+challengesNonCompletedPath, []any{map[string]any{
		"uuid":                   "DEF456",
		"badgeChallengeName":     "Ultra Marathon Challenge",
		"challengeCategoryId":    1,
		"badgeChallengeStatusId": 2,
		"startDate":              "2024-01-01T00:00:00.0",
		"endDate":                "2024-01-31T23:59:59.0",
		"badgePoints":            4,
		"badgeUnitId":            1,
		"badgeProgressValue":     21000.0,
		"badgeTargetValue":       50000.0,
		"badgeEarnedDate":        nil,
		"userJoined":             true,
	}})
	cs := newSession(t, f, nil)

	if _, isErr := callTool(t, cs, "get_non_completed_badge_challenges", nil); isErr {
		t.Fatal("get_non_completed_badge_challenges returned an error")
	}
	calls := f.callsMatching("GET", challengesNonCompletedPath)
	if calls[0].Query.Get("start") != "1" || calls[0].Query.Get("limit") != "20" {
		t.Fatalf("query = %v", calls[0].Query)
	}
}

func TestGetRacePredictionsTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+challengesPredictionsPath, map[string]any{
		"5K":           map[string]any{"time": 1200, "unit": "seconds"},
		"10K":          map[string]any{"time": 2520, "unit": "seconds"},
		"halfMarathon": map[string]any{"time": 5400, "unit": "seconds"},
		"marathon":     map[string]any{"time": 11400, "unit": "seconds"},
	})
	cs := newSession(t, f, nil)

	text, isErr := callTool(t, cs, "get_race_predictions", nil)
	if isErr {
		t.Fatalf("error: %s", text)
	}
	var data map[string]any
	decodeJSON(t, text, &data)
	predictions := asMap(data["predictions"])
	if _, ok := predictions["5K"]; !ok {
		t.Fatalf("predictions = %v", predictions)
	}
}

func TestGetInprogressVirtualChallengesTool(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+challengesVirtualPath, []any{map[string]any{
		"uuid":               "GHI789",
		"badgeChallengeName": "Camino de Santiago",
		"startDate":          nil,
		"endDate":            nil,
		"badgeUnitId":        1,
		"badgeProgressValue": 159.0,
		"badgeTargetValue":   784000.0,
		"userJoined":         true,
	}})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_inprogress_virtual_challenges", nil)
	var data any
	decodeJSON(t, text, &data)
	want := map[string]any{
		"total": 1.0,
		"challenges": []any{map[string]any{
			"name":             "Camino de Santiago",
			"uuid":             "GHI789",
			"start_date":       nil,
			"end_date":         nil,
			"progress_meters":  159.0,
			"target_meters":    784000.0,
			"progress_km":      "0.16 km",
			"target_km":        "784.00 km",
			"progress_percent": "0.0%",
		}},
	}
	sameJSON(t, data, want)
	calls := f.callsMatching("GET", challengesVirtualPath)
	if calls[0].Query.Get("start") != "1" || calls[0].Query.Get("limit") != "20" {
		t.Fatalf("query = %v", calls[0].Query)
	}
}

func TestGetInprogressVirtualChallengesLegacyFields(t *testing.T) {
	cases := []struct {
		name             string
		legacy           map[string]any
		expectedName     string
		expectedProgress float64
		expectedPercent  string
	}{
		{
			name:             "legacy names",
			legacy:           map[string]any{"name": "Legacy Expedition", "progress": 28000.0, "target": 42195.0},
			expectedName:     "Legacy Expedition",
			expectedProgress: 28000.0,
			expectedPercent:  "66.4%",
		},
		{
			name:             "alternate legacy names",
			legacy:           map[string]any{"challengeName": "Alternate Legacy Expedition", "progressValue": 0.0, "targetValue": 42195.0},
			expectedName:     "Alternate Legacy Expedition",
			expectedProgress: 0.0,
			expectedPercent:  "0.0%",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGarmin(t)
			challenge := map[string]any{
				"uuid":      "LEGACY123",
				"startDate": "2024-01-01T00:00:00.0",
				"endDate":   "2024-01-31T23:59:59.0",
			}
			for k, v := range tc.legacy {
				challenge[k] = v
			}
			f.on("GET "+challengesVirtualPath, []any{challenge})
			cs := newSession(t, f, nil)

			text, _ := callTool(t, cs, "get_inprogress_virtual_challenges", nil)
			var data map[string]any
			decodeJSON(t, text, &data)
			got := asMap(asList(data["challenges"])[0])
			want := map[string]any{
				"name":             tc.expectedName,
				"uuid":             "LEGACY123",
				"start_date":       "2024-01-01",
				"end_date":         "2024-01-31",
				"progress_meters":  tc.expectedProgress,
				"target_meters":    42195.0,
				"progress_km":      fmt.Sprintf("%.2f km", tc.expectedProgress/1000),
				"target_km":        "42.20 km",
				"progress_percent": tc.expectedPercent,
			}
			sameJSON(t, got, want)
		})
	}
}

func TestGetInprogressVirtualChallengesNonDistanceUnits(t *testing.T) {
	cases := []struct {
		name              string
		unitID            float64
		progress          float64
		target            float64
		formattedProgress string
		formattedTarget   string
	}{
		{"elevation", 2, 120.0, 1000.0, "120 m", "1000 m"},
		{"count", 5, 120000.0, 200000.0, "120,000", "200,000"},
		{"time", 7, 3600.0, 7200.0, "1:00:00", "2:00:00"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGarmin(t)
			f.on("GET "+challengesVirtualPath, []any{map[string]any{
				"uuid":               "NON_DISTANCE",
				"badgeChallengeName": "Non-distance Expedition",
				"badgeUnitId":        tc.unitID,
				"badgeProgressValue": tc.progress,
				"badgeTargetValue":   tc.target,
			}})
			cs := newSession(t, f, nil)

			text, _ := callTool(t, cs, "get_inprogress_virtual_challenges", nil)
			var data map[string]any
			decodeJSON(t, text, &data)
			challenge := asMap(asList(data["challenges"])[0])
			if challenge["progress"] != tc.formattedProgress || challenge["target"] != tc.formattedTarget {
				t.Fatalf("challenge = %v", challenge)
			}
			for _, key := range []string{"progress_meters", "target_meters", "progress_km", "target_km"} {
				if _, ok := challenge[key]; ok {
					t.Fatalf("%s present: %v", key, challenge)
				}
			}
		})
	}
}

func TestGetInprogressVirtualChallengesSkipsZeroTarget(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+challengesVirtualPath, []any{map[string]any{
		"uuid":               "ZERO_TARGET",
		"badgeChallengeName": "",
		"name":               "Fallback Name",
		"badgeUnitId":        1,
		"badgeProgressValue": 0.0,
		"badgeTargetValue":   0.0,
	}})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_inprogress_virtual_challenges", nil)
	var data map[string]any
	decodeJSON(t, text, &data)
	challenge := asMap(asList(data["challenges"])[0])
	if challenge["name"] != "Fallback Name" {
		t.Fatalf("challenge = %v", challenge)
	}
	if len(challenge) != 4 {
		t.Fatalf("challenge keys = %v", challenge)
	}
}

func TestGetGoalsNoData(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+challengesGoalsPath, sequence{rawBody("[]"), rawBody("[]")})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_goals", map[string]any{"goal_type": "active"})
	if !strings.Contains(text, "No active goals found.") {
		t.Fatalf("text = %q", text)
	}
}

func TestGetPersonalRecordException(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+challengesPRPath, status(500, nil))
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_personal_record", nil)
	if !strings.Contains(text, "Error retrieving personal records") {
		t.Fatalf("text = %q", text)
	}
}

// --- unit tests ported from tests/unit/test_goals.py ---

func TestAsGoalListUnwrapsLegacyAndModernShapes(t *testing.T) {
	goal := connectUIGoal()
	if got := asGoalList([]any{goal}); len(got) != 1 || asMap(got[0])["name"] != "GCC 2026" {
		t.Fatalf("list = %v", got)
	}
	if got := asGoalList(map[string]any{"goals": []any{goal}}); len(got) != 1 {
		t.Fatalf("goals wrapper = %v", got)
	}
	if got := asGoalList(goal); len(got) != 1 {
		t.Fatalf("single = %v", got)
	}
	if got := asGoalList(nil); len(got) != 0 {
		t.Fatalf("nil = %v", got)
	}
	if got := asGoalList("nope"); len(got) != 0 {
		t.Fatalf("string = %v", got)
	}
}

func TestCurateConnectUIGoalFlattensProgress(t *testing.T) {
	curated := curateConnectUIGoal(connectUIGoal())
	if curated["name"] != "GCC 2026" || curated["activity_type"] != "cycling" {
		t.Fatalf("curated = %v", curated)
	}
	if curated["target_distance_meters"] != 160934.0 {
		t.Fatalf("target = %v", curated["target_distance_meters"])
	}
	if curated["progress_percent"] != 17.0 || curated["progress_distance_meters"] != 28089.0 {
		t.Fatalf("progress = %v", curated)
	}
	if curated["remaining_distance_meters"] != 132845.0 || curated["remaining_days"] != 13.0 {
		t.Fatalf("remaining = %v", curated)
	}
	if _, ok := curated["target_duration_seconds"]; ok {
		t.Fatalf("target_duration_seconds present: %v", curated)
	}
	if !isConnectUIGoal(connectUIGoal()) || isConnectUIGoal(map[string]any{"goalType": "STEPS", "goalValue": 8000}) {
		t.Fatal("isConnectUIGoal mismatch")
	}
}

func TestCollectGoalsSendsHeaderAndOneBasedStart(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET "+challengesGoalsPath, []any{connectUIGoal()})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_goals", map[string]any{"goal_type": "active"})
	var data []map[string]any
	decodeJSON(t, text, &data)
	if len(data) != 1 || data[0]["progress_percent"] != 17.0 {
		t.Fatalf("data = %v", data)
	}
	calls := f.callsMatching("GET", challengesGoalsPath)
	if calls[0].Query.Get("status") != "active" || calls[0].Query.Get("start") != "1" {
		t.Fatalf("query = %v", calls[0].Query)
	}
	if calls[0].Header.Get("Sec-Fetch-Site") != "same-origin" {
		t.Fatalf("headers = %v", calls[0].Header)
	}
	if calls[0].Query.Has("userId") {
		t.Fatalf("unexpected userId: %v", calls[0].Query)
	}
}

func TestCollectGoalsPassesStatusThrough(t *testing.T) {
	f := newFakeGarmin(t)
	past := connectUIGoal()
	past["id"] = 1.0
	past["active"] = false
	past["completed"] = true
	f.on("GET "+challengesGoalsPath, []any{past})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_goals", map[string]any{"goal_type": "past"})
	var data []map[string]any
	decodeJSON(t, text, &data)
	if len(data) != 1 || data[0]["id"] != 1.0 {
		t.Fatalf("data = %v", data)
	}
	calls := f.callsMatching("GET", challengesGoalsPath)
	if calls[0].Query.Get("status") != "past" {
		t.Fatalf("status = %q", calls[0].Query.Get("status"))
	}
}

func TestCollectGoalsPaginatesFromOne(t *testing.T) {
	f := newFakeGarmin(t)
	page := func(offset int, count int) []any {
		out := make([]any, 0, count)
		for i := 0; i < count; i++ {
			goal := connectUIGoal()
			goal["id"] = float64(offset + i)
			out = append(out, goal)
		}
		return out
	}
	f.on("GET "+challengesGoalsPath, sequence{page(0, 100), page(100, 50)})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_goals", map[string]any{"goal_type": "past"})
	var data []map[string]any
	decodeJSON(t, text, &data)
	if len(data) != 150 {
		t.Fatalf("goals = %d", len(data))
	}
	calls := f.callsMatching("GET", challengesGoalsPath)
	if len(calls) != 2 {
		t.Fatalf("calls = %d", len(calls))
	}
	if calls[0].Query.Get("start") != "1" || calls[1].Query.Get("start") != "101" {
		t.Fatalf("starts = %q, %q", calls[0].Query.Get("start"), calls[1].Query.Get("start"))
	}
}

func TestCollectGoalsStopsIfServerIgnoresStart(t *testing.T) {
	f := newFakeGarmin(t)
	full := make([]any, 0, 100)
	for i := 0; i < 100; i++ {
		goal := connectUIGoal()
		goal["id"] = float64(i)
		full = append(full, goal)
	}
	f.on("GET "+challengesGoalsPath, sequence{full, full})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_goals", map[string]any{"goal_type": "active"})
	var data []map[string]any
	decodeJSON(t, text, &data)
	if len(data) != 100 {
		t.Fatalf("goals = %d", len(data))
	}
	if calls := f.callsMatching("GET", challengesGoalsPath); len(calls) != 2 {
		t.Fatalf("calls = %d", len(calls))
	}
}

func TestCollectGoalsPassesLegacyWellnessGoalsThrough(t *testing.T) {
	f := newFakeGarmin(t)
	wellness := map[string]any{"goalType": "STEPS", "goalValue": 8000.0}
	f.on("GET "+challengesGoalsPath, []any{wellness})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_goals", map[string]any{"goal_type": "active"})
	var data []any
	decodeJSON(t, text, &data)
	sameJSON(t, data, []any{wellness})
}

func TestCollectGoalsFallsBackToLibraryWhenEmpty(t *testing.T) {
	f := newFakeGarmin(t)
	legacy := []any{map[string]any{"goalType": "STEPS", "goalValue": 8000.0}}
	f.on("GET "+challengesGoalsPath, sequence{rawBody("[]"), legacy, rawBody("[]")})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_goals", map[string]any{"goal_type": "active"})
	var data []any
	decodeJSON(t, text, &data)
	sameJSON(t, data, legacy)
	calls := f.callsMatching("GET", challengesGoalsPath)
	if calls[1].Query.Get("start") != "0" {
		t.Fatalf("legacy start = %q", calls[1].Query.Get("start"))
	}
}

func TestCollectGoalsFallsBackToLibraryOnError(t *testing.T) {
	f := newFakeGarmin(t)
	legacy := []any{map[string]any{"goalType": "STEPS", "goalValue": 8000.0}}
	f.on("GET "+challengesGoalsPath, sequence{status(500, nil), legacy, rawBody("[]")})
	cs := newSession(t, f, nil)

	text, _ := callTool(t, cs, "get_goals", map[string]any{"goal_type": "active"})
	var data []any
	decodeJSON(t, text, &data)
	sameJSON(t, data, legacy)
	if calls := f.callsMatching("GET", challengesGoalsPath); len(calls) < 2 {
		t.Fatalf("calls = %d", len(calls))
	}
}
