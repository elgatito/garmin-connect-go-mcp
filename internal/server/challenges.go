package server

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

func shortDate(v any) any {
	str, ok := v.(string)
	if !ok || str == "" {
		return nil
	}
	return strings.SplitN(str, "T", 2)[0]
}
func badgeValue(v any, unit int) any {
	n, ok := v.(float64)
	if !ok {
		return nil
	}
	switch unit {
	case 1:
		if n >= 1000 {
			return fmt.Sprintf("%.2f km", n/1000)
		}
		return fmt.Sprintf("%.0f m", n)
	case 2:
		return fmt.Sprintf("%.0f m", n)
	case 3, 5:
		raw := strconv.FormatInt(int64(n), 10)
		sign := ""
		if strings.HasPrefix(raw, "-") {
			sign = "-"
			raw = raw[1:]
		}
		for i := len(raw) - 3; i > 0; i -= 3 {
			raw = raw[:i] + "," + raw[i:]
		}
		return sign + raw
	case 7:
		return predictionTime(n)
	default:
		return fmt.Sprint(v)
	}
}
func badgeChallenge(raw map[string]any) map[string]any {
	category, _ := raw["challengeCategoryId"].(float64)
	status, _ := raw["badgeChallengeStatusId"].(float64)
	categories := map[int]string{1: "Running", 2: "Cycling", 3: "Fitness", 4: "Steps", 5: "Walking", 6: "Yoga/Mindfulness", 9: "Multi-Activity"}
	statuses := map[int]string{1: "Not Started", 2: "In Progress", 3: "Completed", 4: "Ended"}
	cat := categories[int(category)]
	if cat == "" {
		cat = fmt.Sprintf("category_%v", raw["challengeCategoryId"])
	}
	state := statuses[int(status)]
	if state == "" {
		state = fmt.Sprintf("status_%v", raw["badgeChallengeStatusId"])
	}
	joined := raw["userJoined"]
	if joined == nil {
		joined = false
	}
	out := map[string]any{"name": raw["badgeChallengeName"], "uuid": raw["uuid"], "category": cat, "status": state, "points": raw["badgePoints"], "start_date": shortDate(raw["startDate"]), "end_date": shortDate(raw["endDate"]), "joined": joined}
	target, ok := raw["badgeTargetValue"].(float64)
	if ok && target > 0 {
		unit, _ := raw["badgeUnitId"].(float64)
		progress := raw["badgeProgressValue"]
		out["progress"] = badgeValue(progress, int(unit))
		out["target"] = badgeValue(target, int(unit))
		var percent any
		if n, ok := progress.(float64); ok {
			pct := n / target * 100
			if pct > 100 {
				pct = 100
			}
			percent = fmt.Sprintf("%.1f%%", pct)
		}
		out["progress_percent"] = percent
	}
	if truthy(raw["badgeEarnedDate"]) {
		out["earned_date"] = shortDate(raw["badgeEarnedDate"])
	}
	return out
}
func registerChallenges(srv *mcp.Server, m *manager) {
	for _, item := range []struct {
		name, path, empty, label, sortKey string
		reverse, joinable                 bool
	}{{"get_available_badge_challenges", "available", "No available badge challenges found.", "available badge challenges", "start_date", false, true}, {"get_badge_challenges", "completed", "No badge challenges found.", "badge challenges", "start_date", true, false}, {"get_non_completed_badge_challenges", "non-completed", "No in-progress badge challenges found.", "in-progress badge challenges", "end_date", false, false}} {
		item := item
		addTool(srv, m, item.name, description(item.name), map[string]any{"start": map[string]any{"type": "integer", "default": 1}, "limit": map[string]any{"type": "integer", "default": 20}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
			start, err := intArg(args, "start", 1)
			if err != nil || start < 0 {
				return nil, fmt.Errorf("Error retrieving %s: start must be a non-negative integer", item.label)
			}
			limit, err := intArg(args, "limit", 20)
			if err != nil || limit < 1 {
				return nil, fmt.Errorf("Error retrieving %s: limit must be a positive integer", item.label)
			}
			if limit > 100 {
				limit = 100
			}
			v, err := s.Client.GetJSON(ctx, "/badgechallenge-service/badgeChallenge/"+item.path, url.Values{"start": {strconv.Itoa(start)}, "limit": {strconv.Itoa(limit)}})
			if err != nil {
				return nil, fmt.Errorf("Error retrieving %s: %w", item.label, err)
			}
			if empty(v) {
				return item.empty, nil
			}
			list, ok := v.([]any)
			if !ok {
				return nil, fmt.Errorf("Error retrieving %s: unexpected response", item.label)
			}
			rows := []map[string]any{}
			for _, v := range list {
				raw, ok := v.(map[string]any)
				if !ok {
					continue
				}
				out := badgeChallenge(raw)
				if item.joinable {
					joinable, exists := raw["joinable"]
					if !exists {
						joinable = true
					}
					out["joinable"] = joinable
				}
				rows = append(rows, out)
			}
			sort.SliceStable(rows, func(i, j int) bool {
				a, _ := rows[i][item.sortKey].(string)
				b, _ := rows[j][item.sortKey].(string)
				if item.reverse {
					return a > b
				}
				return a < b
			})
			return map[string]any{"total": len(rows), "challenges": rows}, nil
		})
	}
}
