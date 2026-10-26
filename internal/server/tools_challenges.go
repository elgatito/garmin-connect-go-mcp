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
)

const (
	getGoalsDescription = `Get Garmin Connect goals (active, future, or past).

Reads goals the way Garmin Connect's Goals page does, so named
distance/time targets (e.g. a monthly cycling mileage goal) are
included with their progress and remaining amounts. Falls back to
python-garminconnect's get_goals() if that request returns nothing.`

	getPersonalRecordDescription = "Get personal records for user"

	getEarnedBadgesDescription = "Get earned badges for user"

	getAdhocChallengesDescription = `Get user-created social/group challenges (e.g., step competitions with friends)

Returns challenges created by users to compete with connections. These are
different from official Garmin badge challenges.`

	getAvailableBadgeChallengesDescription = `Get official Garmin badge challenges available to join

Returns monthly/seasonal challenges from Garmin that the user can join.
These challenges award badges and points upon completion.`

	getBadgeChallengesDescription = `Get badge challenges the user has completed.

Returns the completed badge-challenge history and earned dates. For
challenges still in progress, use get_non_completed_badge_challenges.`

	getNonCompletedBadgeChallengesDescription = `Get badge challenges currently in progress (not yet completed)

Returns active challenges the user has joined but hasn't completed yet.
Useful for tracking current progress toward badge goals.`

	getRacePredictionsDescription = `Get predicted race times based on current fitness level

Returns Garmin's predictions for 5K, 10K, half marathon, and marathon
finish times based on the user's recent training data and VO2 max.`

	getInprogressVirtualChallengesDescription = `Get in-progress virtual challenges/expeditions

Returns virtual challenges (like walking expeditions on famous trails)
that the user is currently participating in.`
)

var (
	badgeCategoryMapping = map[float64]string{
		1: "Activity", 2: "Running", 3: "Cycling", 4: "Challenge", 5: "Steps", 9: "Diving",
	}
	badgeDifficultyMapping = map[float64]string{1: "Easy", 2: "Medium", 3: "Hard"}
	badgeUnitMapping       = map[float64]string{1: "distance", 2: "elevation", 3: "count", 5: "count", 7: "time"}
	challengeCategoryMap   = map[float64]string{
		1: "Running", 2: "Cycling", 3: "Fitness", 4: "Steps", 5: "Walking", 6: "Yoga/Mindfulness", 9: "Multi-Activity",
	}
	challengeStatusMap = map[float64]string{1: "Not Started", 2: "In Progress", 3: "Completed", 4: "Ended"}
	adhocTypeMap       = map[float64]string{1: "Running", 2: "Cycling", 3: "Swimming", 4: "Steps", 5: "Walking"}
	prTypeMapping      = map[float64][2]string{
		1:  {"Fastest 1K", "time"},
		2:  {"Fastest Mile", "time"},
		3:  {"Fastest 5K", "time"},
		4:  {"Fastest 10K", "time"},
		5:  {"Fastest Half Marathon", "time"},
		6:  {"Fastest Marathon", "time"},
		7:  {"Longest Run", "distance"},
		8:  {"Longest Ride", "distance"},
		9:  {"Most Elevation Gain Cycling", "elevation"},
		10: {"Fastest 100K Cycling", "time"},
		11: {"Fastest 40K Cycling", "time"},
		12: {"Most Steps Day", "count"},
		13: {"Most Steps Week", "count"},
		14: {"Most Steps Month", "count"},
		15: {"Longest Daily Goal Streak", "days"},
		16: {"Longest Weekly Goal Streak", "days"},
		17: {"Longest Pool Swim", "distance"},
		18: {"Fastest 100m Pool Swim", "time"},
		19: {"Fastest 400m Pool Swim", "time"},
		20: {"Fastest 500m Pool Swim", "time"},
		21: {"Fastest 800m Pool Swim", "time"},
		22: {"Fastest 1500m Pool Swim", "time"},
		23: {"Fastest 1 Mile Pool Swim", "time"},
	}
)

type getGoalsInput struct {
	Account
	GoalType string `json:"goal_type,omitempty" jsonschema:"Type of goals to retrieve. Options: \"active\", \"future\", or \"past\""`
}

type noArgsInput struct {
	Account
}

type getAdhocChallengesInput struct {
	Account
	Start int  `json:"start,omitempty" jsonschema:"Starting index for pagination (default 0)"`
	Limit *int `json:"limit,omitempty" jsonschema:"Maximum number of challenges to return (default 20, max 100)"`
}

type badgeChallengesPageInput struct {
	Account
	Start *int `json:"start,omitempty" jsonschema:"Starting index for pagination (starts at 1)"`
	Limit *int `json:"limit,omitempty" jsonschema:"Maximum number of challenges to return (default 20, max 100)"`
}

func registerChallengeTools(r *registry) {
	addTool(r, "get_goals", getGoalsDescription, getGoals)
	addTool(r, "get_personal_record", getPersonalRecordDescription, getPersonalRecord)
	addTool(r, "get_earned_badges", getEarnedBadgesDescription, getEarnedBadges)
	addTool(r, "get_adhoc_challenges", getAdhocChallengesDescription, getAdhocChallenges)
	addTool(r, "get_available_badge_challenges", getAvailableBadgeChallengesDescription, getAvailableBadgeChallenges)
	addTool(r, "get_badge_challenges", getBadgeChallengesDescription, getBadgeChallenges)
	addTool(r, "get_non_completed_badge_challenges", getNonCompletedBadgeChallengesDescription, getNonCompletedBadgeChallenges)
	addTool(r, "get_race_predictions", getRacePredictionsDescription, getRacePredictions)
	addTool(r, "get_inprogress_virtual_challenges", getInprogressVirtualChallengesDescription, getInprogressVirtualChallenges)
}

func intOr(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

func stringFieldOf(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func pyScalar(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case string:
		return x
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e16 {
			return strconv.FormatFloat(x, 'f', 1, 64)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	}
	return fmt.Sprint(v)
}

func pyIntString(v any) string {
	if f, ok := numberOf(v); ok {
		return strconv.FormatInt(int64(f), 10)
	}
	if v == nil {
		return "None"
	}
	return fmt.Sprint(v)
}

func mappingOr(m map[float64]string, v any, prefix string) string {
	f, ok := numberOf(v)
	if !ok {
		return prefix + "_" + pyIntString(v)
	}
	if s, ok := m[f]; ok {
		return s
	}
	return prefix + "_" + pyIntString(v)
}

func formatTimeValue(v any) any {
	if v == nil {
		return nil
	}
	f, ok := numberOf(v)
	if !ok {
		return nil
	}
	total := int(f)
	hours := total / 3600
	minutes := (total % 3600) / 60
	secs := total % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, secs)
	}
	return fmt.Sprintf("%d:%02d", minutes, secs)
}

func formatDistanceValue(v any) any {
	if v == nil {
		return nil
	}
	f, ok := numberOf(v)
	if !ok {
		return nil
	}
	if f >= 1000 {
		return fmt.Sprintf("%.2f km", f/1000)
	}
	return fmt.Sprintf("%.0f m", f)
}

func formatThousandCount(f float64) string {
	intPart := int64(f)
	digits := strconv.FormatInt(intPart, 10)
	negative := strings.HasPrefix(digits, "-")
	if negative {
		digits = digits[1:]
	}
	var parts []string
	for len(digits) > 3 {
		parts = append([]string{digits[len(digits)-3:]}, parts...)
		digits = digits[:len(digits)-3]
	}
	parts = append([]string{digits}, parts...)
	out := strings.Join(parts, ",")
	if negative {
		return "-" + out
	}
	return out
}

func formatTimestampUTC(v any) any {
	f, ok := numberOf(v)
	if !ok || v == nil {
		return nil
	}
	return time.UnixMilli(int64(f)).UTC().Format("2006-01-02")
}

func firstNonNil(data map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := data[key]; ok && value != nil {
			return value
		}
	}
	return nil
}

func formatBadgeValue(value any, unitID any) any {
	if value == nil {
		return nil
	}
	unit, ok := numberOf(unitID)
	if !ok {
		return pyScalar(value)
	}
	valueType, ok := badgeUnitMapping[unit]
	if !ok {
		return pyScalar(value)
	}
	switch valueType {
	case "time":
		return formatTimeValue(value)
	case "distance":
		return formatDistanceValue(value)
	case "elevation":
		f, _ := numberOf(value)
		return fmt.Sprintf("%.0f m", f)
	case "count":
		f, _ := numberOf(value)
		return formatThousandCount(f)
	}
	return pyScalar(value)
}

func calculateProgressPercent(progress, target any) any {
	if progress == nil || target == nil {
		return nil
	}
	p, ok := numberOf(progress)
	if !ok {
		return nil
	}
	t, ok := numberOf(target)
	if !ok || t == 0 {
		return nil
	}
	return fmt.Sprintf("%.1f%%", math.Min(p/t*100, 100))
}

func curateBadgeChallenge(challenge map[string]any) map[string]any {
	unitID := challenge["badgeUnitId"]
	progress := challenge["badgeProgressValue"]
	target := challenge["badgeTargetValue"]

	curated := map[string]any{
		"name":       challenge["badgeChallengeName"],
		"uuid":       challenge["uuid"],
		"category":   mappingOr(challengeCategoryMap, challenge["challengeCategoryId"], "category"),
		"status":     mappingOr(challengeStatusMap, challenge["badgeChallengeStatusId"], "status"),
		"points":     challenge["badgePoints"],
		"start_date": parseISODate(challenge["startDate"]),
		"end_date":   parseISODate(challenge["endDate"]),
		"joined":     false,
	}
	if joined, ok := challenge["userJoined"]; ok {
		curated["joined"] = joined
	}
	if t, ok := numberOf(target); ok && t > 0 {
		curated["progress"] = formatBadgeValue(progress, unitID)
		curated["target"] = formatBadgeValue(target, unitID)
		curated["progress_percent"] = calculateProgressPercent(progress, target)
	}
	if earned := challenge["badgeEarnedDate"]; truthy(earned) {
		curated["earned_date"] = parseISODate(earned)
	}
	return curated
}

func formatPRValue(value any, valueType string) any {
	if value == nil {
		return nil
	}
	f, ok := numberOf(value)
	if !ok {
		return nil
	}
	switch valueType {
	case "time":
		return formatTimeValue(value)
	case "distance":
		return formatDistanceValue(value)
	case "elevation":
		return fmt.Sprintf("%.0f m", f)
	case "count":
		return formatThousandCount(f)
	case "days":
		return fmt.Sprintf("%d days", int(f))
	}
	return pyScalar(value)
}

// --- get_goals ---

func asGoalList(payload any) []any {
	switch x := payload.(type) {
	case []any:
		out := make([]any, 0, len(x))
		for _, item := range x {
			if _, ok := item.(map[string]any); ok {
				out = append(out, item)
			}
		}
		return out
	case map[string]any:
		for _, key := range []string{"goals", "items", "data"} {
			if nested, ok := x[key].([]any); ok {
				return asGoalList(nested)
			}
		}
		if _, ok := x["name"]; ok {
			return []any{x}
		}
		if _, ok := x["goalType"]; ok {
			return []any{x}
		}
		if _, ok := x["id"]; ok {
			return []any{x}
		}
		if _, ok := x["type"]; ok {
			return []any{x}
		}
	}
	return []any{}
}

func isConnectUIGoal(goal map[string]any) bool {
	if _, ok := goal["name"]; !ok {
		return false
	}
	if _, ok := goal["progress"].(map[string]any); ok {
		return true
	}
	if _, ok := goal["distanceInMeters"]; ok {
		return true
	}
	if _, hasType := goal["type"]; hasType {
		if _, hasGoalType := goal["goalType"]; !hasGoalType {
			return true
		}
	}
	return false
}

func curateConnectUIGoal(goal map[string]any) map[string]any {
	progress := asMap(goal["progress"])
	remaining := asMap(goal["remaining"])
	overage := asMap(goal["overage"])
	curated := map[string]any{
		"id":                         goal["id"],
		"name":                       goal["name"],
		"type":                       goal["type"],
		"activity_type":              goal["activityType"],
		"period":                     goal["period"],
		"privacy":                    goal["privacy"],
		"start_date":                 goal["startDate"],
		"end_date":                   goal["endDate"],
		"active":                     goal["active"],
		"completed":                  goal["completed"],
		"target_distance_meters":     goal["distanceInMeters"],
		"target_duration_seconds":    goal["durationInSeconds"],
		"target_calories":            goal["caloriesInKiloCalories"],
		"target_activities":          goal["numberOfActivities"],
		"progress_percent":           progress["percent"],
		"progress_distance_meters":   progress["distanceInMeters"],
		"progress_duration_seconds":  progress["durationInSeconds"],
		"progress_calories":          progress["caloriesInKiloCalories"],
		"progress_activities":        progress["numberOfActivities"],
		"progress_days":              progress["days"],
		"remaining_percent":          remaining["percent"],
		"remaining_distance_meters":  remaining["distanceInMeters"],
		"remaining_duration_seconds": remaining["durationInSeconds"],
		"remaining_calories":         remaining["caloriesInKiloCalories"],
		"remaining_activities":       remaining["numberOfActivities"],
		"remaining_days":             remaining["days"],
		"overage_percent":            overage["percent"],
	}
	return pruneNil(curated)
}

func goalMarker(goal map[string]any) string {
	if id, ok := goal["id"]; ok && id != nil {
		return pyScalar(id)
	}
	raw, _ := json.Marshal(goal)
	return string(raw)
}

func fetchConnectGoals(ctx context.Context, s *Session, status string) ([]any, error) {
	const pageSize = 100
	const maxPages = 20
	var goals []any
	seen := map[string]bool{}
	start := 1
	for page := 0; page < maxPages; page++ {
		v, err := s.Client.GetGoalsPage(ctx, status, start, pageSize)
		if err != nil {
			return nil, err
		}
		items := asGoalList(v)
		var fresh []any
		for _, item := range items {
			goal := asMap(item)
			marker := goalMarker(goal)
			if seen[marker] {
				continue
			}
			seen[marker] = true
			fresh = append(fresh, item)
		}
		goals = append(goals, fresh...)
		if len(items) < pageSize || len(fresh) == 0 {
			break
		}
		start += pageSize
	}
	return goals, nil
}

func collectGoals(ctx context.Context, s *Session, goalType string) ([]any, error) {
	goals, err := fetchConnectGoals(ctx, s, goalType)
	if err != nil {
		goals = nil
	}
	if len(goals) > 0 {
		out := make([]any, 0, len(goals))
		for _, item := range goals {
			goal := asMap(item)
			if isConnectUIGoal(goal) {
				out = append(out, curateConnectUIGoal(goal))
			} else {
				out = append(out, item)
			}
		}
		return out, nil
	}

	legacy, err := s.Client.GetGoals(ctx, goalType)
	if err != nil {
		return nil, err
	}
	if list := asGoalList(legacy); len(list) > 0 {
		return list, nil
	}
	if truthy(legacy) {
		return []any{legacy}, nil
	}
	return []any{}, nil
}

func getGoals(ctx context.Context, s *Session, in getGoalsInput) (any, error) {
	goalType := strings.ToLower(strings.TrimSpace(in.GoalType))
	if goalType == "" {
		goalType = "active"
	}
	if goalType != "active" && goalType != "future" && goalType != "past" {
		return fmt.Sprintf("Invalid goal_type '%s'. Options: active, future, past.", goalType), nil
	}
	goals, err := collectGoals(ctx, s, goalType)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving %s goals: %w", goalType, err)
	}
	if len(goals) == 0 {
		return fmt.Sprintf("No %s goals found.", goalType), nil
	}
	return goals, nil
}

// --- get_personal_record ---

func getPersonalRecord(ctx context.Context, s *Session, _ noArgsInput) (any, error) {
	records, err := s.Client.GetPersonalRecord(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving personal records: %w", err)
	}
	list := asList(records)
	if len(list) == 0 {
		return "No personal records found.", nil
	}
	curated := make([]map[string]any, 0, len(list))
	for _, item := range list {
		record := asMap(item)
		typeID := record["typeId"]
		info, ok := prTypeMapping[asFloat(typeID)]
		var prName, valueType string
		if ok {
			prName, valueType = info[0], info[1]
		} else {
			prName = fmt.Sprintf("Unknown Record (typeId=%s)", pyIntString(typeID))
			valueType = "unknown"
		}
		rawValue := record["value"]
		date := parseISODate(record["prStartTimeLocalFormatted"])
		if date == nil {
			date = formatTimestampUTC(record["prStartTimeGmt"])
		}
		entry := map[string]any{
			"record_type": prName,
			"type_id":     typeID,
			"value":       formatPRValue(rawValue, valueType),
			"raw_value":   rawValue,
			"date":        date,
		}
		if activityID := record["activityId"]; truthy(activityID) {
			entry["activity_id"] = activityID
		}
		curated = append(curated, entry)
	}
	sort.SliceStable(curated, func(i, j int) bool {
		return asFloat(curated[i]["type_id"]) < asFloat(curated[j]["type_id"])
	})
	return curated, nil
}

func asFloat(v any) float64 {
	f, _ := numberOf(v)
	return f
}

// --- get_earned_badges ---

func getEarnedBadges(ctx context.Context, s *Session, _ noArgsInput) (any, error) {
	badges, err := s.Client.GetEarnedBadges(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving earned badges: %w", err)
	}
	list := asList(badges)
	if len(list) == 0 {
		return "No earned badges found.", nil
	}
	curated := make([]map[string]any, 0, len(list))
	for _, item := range list {
		badge := asMap(item)
		unitID := badge["badgeUnitId"]
		progress := badge["badgeProgressValue"]
		target := badge["badgeTargetValue"]
		entry := map[string]any{
			"name":        badge["badgeName"],
			"category":    mappingOr(badgeCategoryMapping, badge["badgeCategoryId"], "category"),
			"difficulty":  mappingOr(badgeDifficultyMapping, badge["badgeDifficultyId"], "level"),
			"points":      badge["badgePoints"],
			"earned_date": parseISODate(badge["badgeEarnedDate"]),
		}
		if target != nil && progress != nil {
			entry["progress"] = formatBadgeValue(progress, unitID)
			entry["target"] = formatBadgeValue(target, unitID)
		}
		start := parseISODate(badge["badgeStartDate"])
		end := parseISODate(badge["badgeEndDate"])
		if start != nil && end != nil {
			entry["challenge_period"] = fmt.Sprintf("%v to %v", start, end)
		}
		if badge["badgeAssocType"] == "activityId" && truthy(badge["badgeAssocDataId"]) {
			entry["activity_id"] = badge["badgeAssocDataId"]
		}
		if truthy(badge["badgeSeriesId"]) {
			entry["series_id"] = badge["badgeSeriesId"]
		}
		curated = append(curated, entry)
	}
	sort.SliceStable(curated, func(i, j int) bool {
		return stringFieldOf(curated[i], "earned_date") > stringFieldOf(curated[j], "earned_date")
	})
	return map[string]any{"total_badges": len(curated), "badges": curated}, nil
}

// --- paginated challenge readers ---

func getAdhocChallenges(ctx context.Context, s *Session, in getAdhocChallengesInput) (any, error) {
	limit := min(intOr(in.Limit, 20), 100)
	challenges, err := s.Client.GetAdhocChallenges(ctx, in.Start, limit)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving adhoc challenges: %w", err)
	}
	list := asList(challenges)
	if len(list) == 0 {
		return "No adhoc challenges found.", nil
	}
	curated := make([]map[string]any, 0, len(list))
	for _, item := range list {
		challenge := asMap(item)
		curated = append(curated, map[string]any{
			"name":          challenge["adHocChallengeName"],
			"description":   challenge["adHocChallengeDesc"],
			"uuid":          challenge["uuid"],
			"activity_type": mappingOr(adhocTypeMap, challenge["socialChallengeActivityTypeId"], "type"),
			"status":        mappingOr(challengeStatusMap, challenge["socialChallengeStatusId"], "status"),
			"start_date":    parseISODate(challenge["startDate"]),
			"end_date":      parseISODate(challenge["endDate"]),
			"your_ranking":  challenge["userRanking"],
			"player_count":  challenge["playerCount"],
		})
	}
	sort.SliceStable(curated, func(i, j int) bool {
		return stringFieldOf(curated[i], "start_date") > stringFieldOf(curated[j], "start_date")
	})
	return map[string]any{"total": len(curated), "challenges": curated}, nil
}

func getAvailableBadgeChallenges(ctx context.Context, s *Session, in badgeChallengesPageInput) (any, error) {
	start, limit := intOr(in.Start, 1), min(intOr(in.Limit, 20), 100)
	challenges, err := s.Client.GetAvailableBadgeChallenges(ctx, start, limit)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving available badge challenges: %w", err)
	}
	list := asList(challenges)
	if len(list) == 0 {
		return "No available badge challenges found.", nil
	}
	curated := make([]map[string]any, 0, len(list))
	for _, item := range list {
		challenge := asMap(item)
		entry := curateBadgeChallenge(challenge)
		joined := any(true)
		if value, ok := challenge["joinable"]; ok {
			joined = value
		}
		entry["joinable"] = joined
		curated = append(curated, entry)
	}
	sort.SliceStable(curated, func(i, j int) bool {
		return stringFieldOf(curated[i], "start_date") < stringFieldOf(curated[j], "start_date")
	})
	return map[string]any{"total": len(curated), "challenges": curated}, nil
}

func getBadgeChallenges(ctx context.Context, s *Session, in badgeChallengesPageInput) (any, error) {
	start, limit := intOr(in.Start, 1), min(intOr(in.Limit, 20), 100)
	challenges, err := s.Client.GetBadgeChallenges(ctx, start, limit)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving badge challenges: %w", err)
	}
	list := asList(challenges)
	if len(list) == 0 {
		return "No badge challenges found.", nil
	}
	curated := make([]map[string]any, 0, len(list))
	for _, item := range list {
		curated = append(curated, curateBadgeChallenge(asMap(item)))
	}
	sort.SliceStable(curated, func(i, j int) bool {
		return stringFieldOf(curated[i], "start_date") > stringFieldOf(curated[j], "start_date")
	})
	return map[string]any{"total": len(curated), "challenges": curated}, nil
}

func getNonCompletedBadgeChallenges(ctx context.Context, s *Session, in badgeChallengesPageInput) (any, error) {
	start, limit := intOr(in.Start, 1), min(intOr(in.Limit, 20), 100)
	challenges, err := s.Client.GetNonCompletedBadgeChallenges(ctx, start, limit)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving in-progress badge challenges: %w", err)
	}
	list := asList(challenges)
	if len(list) == 0 {
		return "No in-progress badge challenges found.", nil
	}
	curated := make([]map[string]any, 0, len(list))
	for _, item := range list {
		curated = append(curated, curateBadgeChallenge(asMap(item)))
	}
	sort.SliceStable(curated, func(i, j int) bool {
		return stringFieldOf(curated[i], "end_date") < stringFieldOf(curated[j], "end_date")
	})
	return map[string]any{"total": len(curated), "challenges": curated}, nil
}

func getRacePredictions(ctx context.Context, s *Session, _ noArgsInput) (any, error) {
	predictions, err := s.Client.GetRacePredictions(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving race predictions: %w", err)
	}
	if !truthy(predictions) {
		return "No race predictions found.", nil
	}
	p := asMap(predictions)
	return map[string]any{
		"prediction_date": p["calendarDate"],
		"predictions": map[string]any{
			"5K": map[string]any{
				"time":         formatTimeValue(p["time5K"]),
				"time_seconds": p["time5K"],
			},
			"10K": map[string]any{
				"time":         formatTimeValue(p["time10K"]),
				"time_seconds": p["time10K"],
			},
			"half_marathon": map[string]any{
				"time":         formatTimeValue(p["timeHalfMarathon"]),
				"time_seconds": p["timeHalfMarathon"],
			},
			"marathon": map[string]any{
				"time":         formatTimeValue(p["timeMarathon"]),
				"time_seconds": p["timeMarathon"],
			},
		},
	}, nil
}

func firstTruthyOf(values ...any) any {
	for _, v := range values {
		if truthy(v) {
			return v
		}
	}
	return nil
}

func getInprogressVirtualChallenges(ctx context.Context, s *Session, in badgeChallengesPageInput) (any, error) {
	start, limit := intOr(in.Start, 1), min(intOr(in.Limit, 20), 100)
	challenges, err := s.Client.GetInprogressVirtualChallenges(ctx, start, limit)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving in-progress virtual challenges: %w", err)
	}
	if !truthy(challenges) {
		return "No in-progress virtual challenges found.", nil
	}

	var list []any
	switch value := challenges.(type) {
	case map[string]any:
		if nested, ok := value["challenges"].([]any); ok {
			list = nested
		} else {
			list = []any{value}
		}
	case []any:
		list = value
	default:
		list = nil
	}

	curated := make([]map[string]any, 0, len(list))
	for _, item := range list {
		challenge := asMap(item)
		entry := map[string]any{
			"name": firstTruthyOf(
				challenge["badgeChallengeName"],
				challenge["name"],
				challenge["challengeName"],
			),
			"uuid":       challenge["uuid"],
			"start_date": parseISODate(challenge["startDate"]),
			"end_date":   parseISODate(challenge["endDate"]),
		}
		progress := firstNonNil(challenge, "badgeProgressValue", "progress", "progressValue")
		target := firstNonNil(challenge, "badgeTargetValue", "target", "targetValue")
		if progress != nil && target != nil {
			targetValue, ok := numberOf(target)
			if ok && targetValue > 0 {
				unitID := challenge["badgeUnitId"]
				unit, unitOK := numberOf(unitID)
				if unitID == nil || (unitOK && unit == 1) {
					progressValue, _ := numberOf(progress)
					entry["progress_meters"] = progress
					entry["target_meters"] = target
					entry["progress_km"] = fmt.Sprintf("%.2f km", progressValue/1000)
					entry["target_km"] = fmt.Sprintf("%.2f km", targetValue/1000)
				} else {
					entry["progress"] = formatBadgeValue(progress, unitID)
					entry["target"] = formatBadgeValue(target, unitID)
				}
				entry["progress_percent"] = calculateProgressPercent(progress, target)
			}
		}
		curated = append(curated, entry)
	}
	return map[string]any{"total": len(curated), "challenges": curated}, nil
}
