package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

// Port of garmin_mcp/challenges.py.

// Badge category mappings. This list is estimated based on real data, the mapping might not be 100% accurate.
var badgeCategoryMapping = map[int]string{1: "Activity", 2: "Running", 3: "Cycling", 4: "Challenge", 5: "Steps", 9: "Diving"}

var badgeDifficultyMapping = map[int]string{1: "Easy", 2: "Medium", 3: "Hard"}

// badgeUnitMapping: unitId -> how progress/target values are formatted.
var badgeUnitMapping = map[int]string{1: "distance", 2: "elevation", 3: "count", 5: "count", 7: "time"}

var challengeCategoryMapping = map[int]string{1: "Running", 2: "Cycling", 3: "Fitness", 4: "Steps", 5: "Walking", 6: "Yoga/Mindfulness", 9: "Multi-Activity"}

var challengeStatusMapping = map[int]string{1: "Not Started", 2: "In Progress", 3: "Completed", 4: "Ended"}

var adhocActivityTypeMapping = map[int]string{1: "Running", 2: "Cycling", 3: "Swimming", 4: "Steps", 5: "Walking"}

type prType struct{ name, valueType string }

// Personal record types. This list is estimated based on real data, the type mapping might not be 100% accurate.
var prTypeMapping = map[int]prType{
	1: {"Fastest 1K", "time"}, 2: {"Fastest Mile", "time"}, 3: {"Fastest 5K", "time"}, 4: {"Fastest 10K", "time"},
	5: {"Fastest Half Marathon", "time"}, 6: {"Fastest Marathon", "time"}, 7: {"Longest Run", "distance"},
	8: {"Longest Ride", "distance"}, 9: {"Most Elevation Gain Cycling", "elevation"}, 10: {"Fastest 100K Cycling", "time"},
	11: {"Fastest 40K Cycling", "time"}, 12: {"Most Steps Day", "count"}, 13: {"Most Steps Week", "count"},
	14: {"Most Steps Month", "count"}, 15: {"Longest Daily Goal Streak", "days"}, 16: {"Longest Weekly Goal Streak", "days"},
	17: {"Longest Pool Swim", "distance"}, 18: {"Fastest 100m Pool Swim", "time"}, 19: {"Fastest 400m Pool Swim", "time"},
	20: {"Fastest 500m Pool Swim", "time"}, 21: {"Fastest 800m Pool Swim", "time"}, 22: {"Fastest 1500m Pool Swim", "time"},
	23: {"Fastest 1 Mile Pool Swim", "time"},
}

// idKey is a JSON number used as a dict key: 3.0 and 3 are the same key in Python.
func idKey(v any) (int, bool) {
	f, ok := asNumber(v)
	if !ok || f != math.Trunc(f) {
		return 0, false
	}
	return int(f), true
}

// lookupOr is MAPPING.get(id, f"{prefix}_{id}").
func lookupOr(m map[int]string, id any, prefix string) string {
	if k, ok := idKey(id); ok {
		if s, found := m[k]; found {
			return s
		}
	}
	return prefix + "_" + idString(id)
}

// formatTime is _format_time: seconds as H:MM:SS or M:SS (nil for a missing value).
func formatTime(v any) any {
	f, ok := asNumber(v)
	if !ok {
		return nil
	}
	total := int(f)
	h, m, s := total/3600, total%3600/60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func formatDistance(m float64) string {
	if m >= 1000 {
		return fmt.Sprintf("%.2f km", m/1000)
	}
	return fmt.Sprintf("%.0f m", m)
}

// commas is f"{n:,}".
func commas(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		s = "-" + s
	}
	return s
}

// strOf is str(value) for a number.
func strOf(v any) string {
	if f, ok := asNumber(v); ok {
		return garmin.PyFloatRepr(f)
	}
	return pyStr(v)
}

// formatValue formats a value by its kind ("time", "distance", "elevation", "count", "days"); other kinds are str(value).
func formatValue(v any, kind string) any {
	f, ok := asNumber(v)
	if v == nil || !ok {
		if v == nil {
			return nil
		}
		return pyStr(v)
	}
	switch kind {
	case "time":
		return formatTime(f)
	case "distance":
		return formatDistance(f)
	case "elevation":
		return fmt.Sprintf("%.0f m", f)
	case "count":
		return commas(int(f))
	case "days":
		return fmt.Sprintf("%d days", int(f))
	}
	return strOf(f)
}

// formatBadgeValue is _format_badge_value.
func formatBadgeValue(v, unitID any) any {
	if v == nil {
		return nil
	}
	if k, ok := idKey(unitID); ok {
		if kind, found := badgeUnitMapping[k]; found && kind != "days" {
			return formatValue(v, kind)
		}
	}
	return strOf(v)
}

// progressPercent is _calculate_progress_percent.
func progressPercent(progress, target any) any {
	p, ok1 := asNumber(progress)
	t, ok2 := asNumber(target)
	if !ok1 || !ok2 || t == 0 {
		return nil
	}
	return fmt.Sprintf("%.1f%%", math.Min(p/t*100, 100))
}

// formatTimestamp is _format_timestamp: the UTC date of a GMT millisecond timestamp.
func formatTimestamp(v any) any {
	f, ok := asNumber(v)
	if !ok {
		return nil
	}
	sec := f / 1000
	return time.Unix(int64(math.Floor(sec)), 0).UTC().Format(time.DateOnly)
}

// firstNonNil is _first_non_none.
func firstNonNil(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v := m[k]; v != nil {
			return v
		}
	}
	return nil
}

func strKey(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// sortByKey sorts curated entries stably by a string field ("" when missing), like list.sort(key=...).
func sortByKey(list []map[string]any, key string, reverse bool) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := strKey(list[i], key), strKey(list[j], key)
		if reverse {
			return a > b
		}
		return a < b
	})
}

func positiveNumber(v any) (float64, bool) {
	f, ok := asNumber(v)
	return f, ok && f > 0
}

// curateBadgeChallenge is _curate_badge_challenge.
func curateBadgeChallenge(c map[string]any) map[string]any {
	curated := map[string]any{
		"name":       c["badgeChallengeName"],
		"uuid":       c["uuid"],
		"category":   lookupOr(challengeCategoryMapping, c["challengeCategoryId"], "category"),
		"status":     lookupOr(challengeStatusMapping, c["badgeChallengeStatusId"], "status"),
		"points":     c["badgePoints"],
		"start_date": parseISODateOnly(c["startDate"]),
		"end_date":   parseISODateOnly(c["endDate"]),
		"joined":     getDefault(c, "userJoined", false),
	}
	if _, ok := positiveNumber(c["badgeTargetValue"]); ok {
		curated["progress"] = formatBadgeValue(c["badgeProgressValue"], c["badgeUnitId"])
		curated["target"] = formatBadgeValue(c["badgeTargetValue"], c["badgeUnitId"])
		curated["progress_percent"] = progressPercent(c["badgeProgressValue"], c["badgeTargetValue"])
	}
	if !falsy(c["badgeEarnedDate"]) {
		curated["earned_date"] = parseISODateOnly(c["badgeEarnedDate"])
	}
	return curated
}

// ── goals ──────────────────────────────────────────────────────────────────

// goalsPageSize and goalsMaxPages: “start“ is 1-based (start=0 returns [] even when goals exist, which is why the
// library's own get_goals() misses them) and goal-service pages by 100.
const (
	goalsPageSize = 100
	goalsMaxPages = 20
)

// asGoalList is _as_goal_list: Connect goal payloads as a list of dicts.
func asGoalList(payload any) []map[string]any {
	dicts := func(l []any) []map[string]any {
		out := []map[string]any{}
		for _, it := range l {
			if m, ok := it.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	switch p := payload.(type) {
	case []any:
		return dicts(p)
	case map[string]any:
		for _, key := range []string{"goals", "items", "data"} {
			if nested, ok := p[key].([]any); ok {
				return dicts(nested)
			}
		}
		for _, key := range []string{"name", "goalType", "id", "type"} {
			if _, ok := p[key]; ok {
				return []map[string]any{p}
			}
		}
	}
	return []map[string]any{}
}

// isConnectUIGoal is _is_connect_ui_goal: the current Connect Goals UI shape (distance/time accumulation).
func isConnectUIGoal(g map[string]any) bool {
	_, hasName := g["name"]
	_, isDict := g["progress"].(map[string]any)
	_, hasDistance := g["distanceInMeters"]
	_, hasType := g["type"]
	_, hasGoalType := g["goalType"]
	return hasName && (isDict || hasDistance || (hasType && !hasGoalType))
}

// curateConnectUIGoal is _curate_connect_ui_goal: progress/remaining flattened into LLM-friendly fields.
func curateConnectUIGoal(g map[string]any) map[string]any {
	progress, remaining, overage := asMap(g["progress"]), asMap(g["remaining"]), asMap(g["overage"])
	return dropNil(map[string]any{
		"id": g["id"], "name": g["name"], "type": g["type"], "activity_type": g["activityType"], "period": g["period"],
		"privacy": g["privacy"], "start_date": g["startDate"], "end_date": g["endDate"], "active": g["active"], "completed": g["completed"],
		"target_distance_meters": g["distanceInMeters"], "target_duration_seconds": g["durationInSeconds"],
		"target_calories": g["caloriesInKiloCalories"], "target_activities": g["numberOfActivities"],
		"progress_percent": progress["percent"], "progress_distance_meters": progress["distanceInMeters"],
		"progress_duration_seconds": progress["durationInSeconds"], "progress_calories": progress["caloriesInKiloCalories"],
		"progress_activities": progress["numberOfActivities"], "progress_days": progress["days"],
		"remaining_percent": remaining["percent"], "remaining_distance_meters": remaining["distanceInMeters"],
		"remaining_duration_seconds": remaining["durationInSeconds"], "remaining_calories": remaining["caloriesInKiloCalories"],
		"remaining_activities": remaining["numberOfActivities"], "remaining_days": remaining["days"],
		"overage_percent": overage["percent"],
	})
}

// goalsClient is what collectGoals needs of the Garmin client.
type goalsClient interface {
	GoalsPage(ctx context.Context, status string, start int) (any, error) // the Goals page request
	GetGoals(ctx context.Context, status string) (any, error)             // the library's own get_goals
}

type garminGoals struct{ c *garmin.Client }

func (g garminGoals) GoalsPage(ctx context.Context, status string, start int) (any, error) {
	return g.c.GoalsPage(ctx, status, start, goalsPageSize)
}
func (g garminGoals) GetGoals(ctx context.Context, status string) (any, error) {
	return g.c.GetGoals(ctx, status)
}

// goalMarker identifies a goal for de-duplication: its id, or its whole content when it has none.
func goalMarker(g map[string]any) string {
	if id := g["id"]; id != nil {
		b, _ := json.Marshal(id)
		return "id:" + string(b)
	}
	b, _ := json.Marshal(g) // map keys are sorted
	return "json:" + string(b)
}

// fetchConnectGoals is _fetch_connect_goals: every goal for status with 1-based pagination.
func fetchConnectGoals(ctx context.Context, c goalsClient, status string) ([]map[string]any, error) {
	var goals []map[string]any
	seen := map[string]bool{}
	start := 1
	for range goalsMaxPages {
		raw, err := c.GoalsPage(ctx, status, start)
		if err != nil {
			return nil, err
		}
		page := asGoalList(raw)
		added := 0
		for _, g := range page {
			if m := goalMarker(g); !seen[m] {
				seen[m] = true
				goals = append(goals, g)
				added++
			}
		}
		// Stop on a short page, or if the server ignored ``start`` and handed back goals we already have.
		if len(page) < goalsPageSize || added == 0 {
			break
		}
		start += goalsPageSize
	}
	return goals, nil
}

// collectGoals is _collect_goals: read goals like Connect's Goals page; fall back to the library call.
func collectGoals(ctx context.Context, c goalsClient, status string) ([]any, error) {
	if goals, err := fetchConnectGoals(ctx, c, status); err == nil && len(goals) > 0 {
		out := make([]any, len(goals))
		for i, g := range goals {
			if isConnectUIGoal(g) {
				out[i] = curateConnectUIGoal(g)
			} else {
				out[i] = g
			}
		}
		return out, nil
	}
	legacy, err := c.GetGoals(ctx, status)
	if err != nil {
		return nil, err
	}
	if l := asGoalList(legacy); len(l) > 0 {
		out := make([]any, len(l))
		for i, g := range l {
			out[i] = g
		}
		return out, nil
	}
	if !falsy(legacy) {
		return []any{legacy}, nil
	}
	return []any{}, nil
}

// ── tools ──────────────────────────────────────────────────────────────────

type goalsIn struct {
	Account
	GoalType string `json:"goal_type,omitempty" jsonschema:"Type of goals to retrieve. Options: \"active\", \"future\", or \"past\""`
}

type pagedIn struct {
	Account
	Start *int `json:"start,omitempty" jsonschema:"Starting index for pagination (starts at 1)"`
	Limit *int `json:"limit,omitempty" jsonschema:"Maximum number of challenges to return (default 20, max 100)"`
}

type adhocIn struct {
	Account
	Start *int `json:"start,omitempty" jsonschema:"Starting index for pagination (default 0)"`
	Limit *int `json:"limit,omitempty" jsonschema:"Maximum number of challenges to return (default 20, max 100)"`
}

type virtualIn struct {
	Account
	Start *int `json:"start,omitempty" jsonschema:"Starting index for pagination (default 1, must be >= 1; garminconnect 0.3.2 rejects 0 for this endpoint)"`
	Limit *int `json:"limit,omitempty" jsonschema:"Maximum number of challenges to return (default 20, max 100)"`
}

func intOr(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

// page returns the start and limit of a paged tool: Python's `start`, `min(limit, 100)`.
func page(start, limit *int, defStart int) (int, int) {
	return intOr(start, defStart), min(intOr(limit, 20), 100)
}

const (
	getGoalsDesc = `Get Garmin Connect goals (active, future, or past).

Reads goals the way Garmin Connect's Goals page does, so named
distance/time targets (e.g. a monthly cycling mileage goal) are
included with their progress and remaining amounts. Falls back to
python-garminconnect's get_goals() if that request returns nothing.`
	adhocDesc = `Get user-created social/group challenges (e.g., step competitions with friends)

Returns challenges created by users to compete with connections. These are
different from official Garmin badge challenges.`
	availableDesc = `Get official Garmin badge challenges available to join

Returns monthly/seasonal challenges from Garmin that the user can join.
These challenges award badges and points upon completion.`
	completedDesc = `Get badge challenges the user has completed.

Returns the completed badge-challenge history and earned dates. For
challenges still in progress, use get_non_completed_badge_challenges.`
	nonCompletedDesc = `Get badge challenges currently in progress (not yet completed)

Returns active challenges the user has joined but hasn't completed yet.
Useful for tracking current progress toward badge goals.`
	racePredictionsDesc = `Get predicted race times based on current fitness level

Returns Garmin's predictions for 5K, 10K, half marathon, and marathon
finish times based on the user's recent training data and VO2 max.`
	virtualDesc = `Get in-progress virtual challenges/expeditions

Returns virtual challenges (like walking expeditions on famous trails)
that the user is currently participating in.`
)

func registerChallengeTools(r *registry) {
	addTool(r, "get_goals", getGoalsDesc, getGoals)
	addTool(r, "get_personal_record", "Get personal records for user", getPersonalRecord)
	addTool(r, "get_earned_badges", "Get earned badges for user", getEarnedBadges)
	addTool(r, "get_adhoc_challenges", adhocDesc, getAdhocChallenges)
	addTool(r, "get_available_badge_challenges", availableDesc, func(ctx context.Context, s *Session, in pagedIn) (any, error) {
		return badgeChallengeList(ctx, in, s.Client.GetAvailableBadgeChallenges, "No available badge challenges found.",
			"Error retrieving available badge challenges", "start_date", false, true)
	})
	addTool(r, "get_badge_challenges", completedDesc, func(ctx context.Context, s *Session, in pagedIn) (any, error) {
		return badgeChallengeList(ctx, in, s.Client.GetBadgeChallenges, "No badge challenges found.",
			"Error retrieving badge challenges", "start_date", true, false)
	})
	addTool(r, "get_non_completed_badge_challenges", nonCompletedDesc, func(ctx context.Context, s *Session, in pagedIn) (any, error) {
		return badgeChallengeList(ctx, in, s.Client.GetNonCompletedBadgeChallenges, "No in-progress badge challenges found.",
			"Error retrieving in-progress badge challenges", "end_date", false, false)
	})
	addTool(r, "get_race_predictions", racePredictionsDesc, getRacePredictions)
	addTool(r, "get_inprogress_virtual_challenges", virtualDesc, getInprogressVirtualChallenges)
}

func getGoals(ctx context.Context, s *Session, in goalsIn) (any, error) {
	goalType := strings.ToLower(strings.TrimSpace(firstTruthy(in.GoalType, "active").(string)))
	switch goalType {
	case "active", "future", "past":
	default:
		return fmt.Sprintf("Invalid goal_type %s. Options: active, future, past.", pyRepr(goalType)), nil
	}
	goals, err := collectGoals(ctx, garminGoals{s.Client}, goalType)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving %s goals: %w", goalType, err)
	}
	if len(goals) == 0 {
		return fmt.Sprintf("No %s goals found.", goalType), nil
	}
	return goals, nil
}

func getPersonalRecord(ctx context.Context, s *Session, _ noArgsIn) (any, error) {
	fail := func(err error) (any, error) { return nil, fmt.Errorf("Error retrieving personal records: %w", err) }
	records, err := s.Client.GetPersonalRecord(ctx)
	if err != nil {
		return fail(err)
	}
	if falsy(records) {
		return "No personal records found.", nil
	}
	var curated []map[string]any
	for _, rec := range asList(records) {
		record := asMap(rec)
		name, kind := "Unknown Record (typeId="+idString(record["typeId"])+")", "unknown"
		if k, ok := idKey(record["typeId"]); ok {
			if pr, found := prTypeMapping[k]; found {
				name, kind = pr.name, pr.valueType
			}
		}
		raw := record["value"]
		// The local date the record was set; the GMT timestamp's UTC date is only a fallback, as it can be a day off.
		c := map[string]any{
			"record_type": name, "type_id": record["typeId"], "value": formatValue(raw, kind), "raw_value": raw,
			"date": firstTruthy(parseISODateOnly(record["prStartTimeLocalFormatted"]), formatTimestamp(record["prStartTimeGmt"])),
		}
		if !falsy(record["activityId"]) {
			c["activity_id"] = record["activityId"]
		}
		curated = append(curated, c)
	}
	sort.SliceStable(curated, func(i, j int) bool {
		a, _ := asNumber(curated[i]["type_id"])
		b, _ := asNumber(curated[j]["type_id"])
		return a < b
	})
	return curated, nil
}

func getEarnedBadges(ctx context.Context, s *Session, _ noArgsIn) (any, error) {
	badges, err := s.Client.GetEarnedBadges(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving earned badges: %w", err)
	}
	if falsy(badges) {
		return "No earned badges found.", nil
	}
	curated := []map[string]any{}
	for _, b := range asList(badges) {
		badge := asMap(b)
		progress, target := badge["badgeProgressValue"], badge["badgeTargetValue"]
		c := map[string]any{
			"name":        badge["badgeName"],
			"category":    lookupOr(badgeCategoryMapping, badge["badgeCategoryId"], "category"),
			"difficulty":  lookupOr(badgeDifficultyMapping, badge["badgeDifficultyId"], "level"),
			"points":      badge["badgePoints"],
			"earned_date": parseISODateOnly(badge["badgeEarnedDate"]),
		}
		if target != nil && progress != nil {
			c["progress"] = formatBadgeValue(progress, badge["badgeUnitId"])
			c["target"] = formatBadgeValue(target, badge["badgeUnitId"])
		}
		start, end := parseISODateOnly(badge["badgeStartDate"]), parseISODateOnly(badge["badgeEndDate"])
		if !falsy(start) && !falsy(end) {
			c["challenge_period"] = fmt.Sprintf("%s to %s", start, end)
		}
		if badge["badgeAssocType"] == "activityId" && !falsy(badge["badgeAssocDataId"]) {
			c["activity_id"] = badge["badgeAssocDataId"]
		}
		if !falsy(badge["badgeSeriesId"]) {
			c["series_id"] = badge["badgeSeriesId"]
		}
		curated = append(curated, c)
	}
	sortByKey(curated, "earned_date", true) // most recent first
	return map[string]any{"total_badges": len(curated), "badges": curated}, nil
}

func getAdhocChallenges(ctx context.Context, s *Session, in adhocIn) (any, error) {
	start, limit := page(in.Start, in.Limit, 0)
	challenges, err := s.Client.GetAdhocChallenges(ctx, start, limit)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving adhoc challenges: %w", err)
	}
	if falsy(challenges) {
		return "No adhoc challenges found.", nil
	}
	curated := []map[string]any{}
	for _, c := range asList(challenges) {
		ch := asMap(c)
		curated = append(curated, map[string]any{
			"name": ch["adHocChallengeName"], "description": ch["adHocChallengeDesc"], "uuid": ch["uuid"],
			"activity_type": lookupOr(adhocActivityTypeMapping, ch["socialChallengeActivityTypeId"], "type"),
			"status":        lookupOr(challengeStatusMapping, ch["socialChallengeStatusId"], "status"),
			"start_date":    parseISODateOnly(ch["startDate"]), "end_date": parseISODateOnly(ch["endDate"]),
			"your_ranking": ch["userRanking"], "player_count": ch["playerCount"],
		})
	}
	sortByKey(curated, "start_date", true) // most recent first
	return map[string]any{"total": len(curated), "challenges": curated}, nil
}

// badgeChallengeList is the shared body of the three badge challenge list tools.
func badgeChallengeList(ctx context.Context, in pagedIn, get func(ctx context.Context, start, limit int) (any, error),
	empty, errPrefix, sortField string, reverse, joinable bool) (any, error) {
	start, limit := page(in.Start, in.Limit, 1)
	challenges, err := get(ctx, start, limit)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}
	if falsy(challenges) {
		return empty, nil
	}
	curated := []map[string]any{}
	for _, c := range asList(challenges) {
		ch := asMap(c)
		cur := curateBadgeChallenge(ch)
		if joinable {
			cur["joinable"] = getDefault(ch, "joinable", true)
		}
		curated = append(curated, cur)
	}
	sortByKey(curated, sortField, reverse)
	return map[string]any{"total": len(curated), "challenges": curated}, nil
}

func getRacePredictions(ctx context.Context, s *Session, _ noArgsIn) (any, error) {
	predictions, err := s.Client.GetLatestRacePredictions(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving race predictions: %w", err)
	}
	if falsy(predictions) {
		return "No race predictions found.", nil
	}
	p := asMap(predictions)
	entry := func(key string) map[string]any {
		return map[string]any{"time": formatTime(p[key]), "time_seconds": p[key]}
	}
	return map[string]any{"prediction_date": p["calendarDate"], "predictions": map[string]any{
		"5K": entry("time5K"), "10K": entry("time10K"), "half_marathon": entry("timeHalfMarathon"), "marathon": entry("timeMarathon"),
	}}, nil
}

func getInprogressVirtualChallenges(ctx context.Context, s *Session, in virtualIn) (any, error) {
	start, limit := page(in.Start, in.Limit, 1)
	challenges, err := s.Client.GetInprogressVirtualChallenges(ctx, start, limit)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving in-progress virtual challenges: %w", err)
	}
	if falsy(challenges) {
		return "No in-progress virtual challenges found.", nil
	}
	var list []any
	if m, ok := challenges.(map[string]any); ok {
		list = []any{m}
		if nested, found := m["challenges"]; found {
			list = asList(nested) // challenges.get("challenges", [challenges])
		}
	} else {
		list = asList(challenges)
	}
	curated := []map[string]any{}
	for _, c := range list {
		ch := asMap(c)
		cur := map[string]any{
			"name":       firstTruthy(firstTruthy(ch["badgeChallengeName"], ch["name"]), ch["challengeName"]),
			"uuid":       ch["uuid"],
			"start_date": parseISODateOnly(ch["startDate"]),
			"end_date":   parseISODateOnly(ch["endDate"]),
		}
		progress := firstNonNil(ch, "badgeProgressValue", "progress", "progressValue")
		target := firstNonNil(ch, "badgeTargetValue", "target", "targetValue")
		pf, pok := asNumber(progress)
		if _, tok := positiveNumber(target); pok && tok {
			tf, _ := asNumber(target)
			unit := ch["badgeUnitId"]
			if k, isNum := idKey(unit); unit == nil || (isNum && k == 1) {
				// Preserve the existing output contract for distance challenges and legacy payloads without unit metadata.
				cur["progress_meters"], cur["target_meters"] = progress, target
				cur["progress_km"], cur["target_km"] = fmt.Sprintf("%.2f km", pf/1000), fmt.Sprintf("%.2f km", tf/1000)
			} else {
				cur["progress"], cur["target"] = formatBadgeValue(progress, unit), formatBadgeValue(target, unit)
			}
			cur["progress_percent"] = progressPercent(progress, target)
		}
		curated = append(curated, cur)
	}
	return map[string]any{"total": len(curated), "challenges": curated}, nil
}
