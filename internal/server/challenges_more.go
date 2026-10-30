package server

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/url"
	"sort"
	"strconv"
)

func earnedBadges(rows []any) map[string]any {
	out := []map[string]any{}
	for _, v := range rows {
		badge, ok := v.(map[string]any)
		if !ok {
			continue
		}
		catN, _ := badge["badgeCategoryId"].(float64)
		diffN, _ := badge["badgeDifficultyId"].(float64)
		unit, _ := badge["badgeUnitId"].(float64)
		cat := map[int]string{1: "Activity", 2: "Running", 3: "Cycling", 4: "Challenge", 5: "Steps", 9: "Diving"}[int(catN)]
		if cat == "" {
			cat = fmt.Sprintf("category_%v", badge["badgeCategoryId"])
		}
		difficulty := map[int]string{1: "Easy", 2: "Medium", 3: "Hard"}[int(diffN)]
		if difficulty == "" {
			difficulty = fmt.Sprintf("level_%v", badge["badgeDifficultyId"])
		}
		entry := map[string]any{"name": badge["badgeName"], "category": cat, "difficulty": difficulty, "points": badge["badgePoints"], "earned_date": shortDate(badge["badgeEarnedDate"])}
		if badge["badgeProgressValue"] != nil && badge["badgeTargetValue"] != nil {
			entry["progress"] = badgeValue(badge["badgeProgressValue"], int(unit))
			entry["target"] = badgeValue(badge["badgeTargetValue"], int(unit))
		}
		start, end := shortDate(badge["badgeStartDate"]), shortDate(badge["badgeEndDate"])
		if start != nil && end != nil {
			entry["challenge_period"] = fmt.Sprintf("%s to %s", start, end)
		}
		if badge["badgeAssocType"] == "activityId" && truthy(badge["badgeAssocDataId"]) {
			entry["activity_id"] = badge["badgeAssocDataId"]
		}
		if truthy(badge["badgeSeriesId"]) {
			entry["series_id"] = badge["badgeSeriesId"]
		}
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, _ := out[i]["earned_date"].(string)
		b, _ := out[j]["earned_date"].(string)
		return a > b
	})
	return map[string]any{"total_badges": len(out), "badges": out}
}
func registerChallengeMore(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_earned_badges", description("get_earned_badges"), nil, func(ctx context.Context, s *Session, _ map[string]any) (any, error) {
		v, err := s.Client.GetJSON(ctx, "/badge-service/badge/earned", nil)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving earned badges: %w", err)
		}
		if empty(v) {
			return "No earned badges found.", nil
		}
		rows, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("Error retrieving earned badges: unexpected response")
		}
		return earnedBadges(rows), nil
	})
	for _, item := range []struct {
		name, path, label, empty string
		start                    int
	}{{"get_adhoc_challenges", "/adhocchallenge-service/adHocChallenge/historical", "adhoc challenges", "No adhoc challenges found.", 0}, {"get_inprogress_virtual_challenges", "/badgechallenge-service/virtualChallenge/inProgress", "in-progress virtual challenges", "No in-progress virtual challenges found.", 1}} {
		item := item
		addTool(srv, m, item.name, description(item.name), map[string]any{"start": map[string]any{"type": "integer", "default": item.start}, "limit": map[string]any{"type": "integer", "default": 20}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
			start, err := intArg(args, "start", item.start)
			if err != nil || start < item.start {
				return nil, fmt.Errorf("Error retrieving %s: invalid start", item.label)
			}
			limit, err := intArg(args, "limit", 20)
			if err != nil || limit < 1 {
				return nil, fmt.Errorf("Error retrieving %s: invalid limit", item.label)
			}
			if limit > 100 {
				limit = 100
			}
			v, err := s.Client.GetJSON(ctx, item.path, url.Values{"start": {strconv.Itoa(start)}, "limit": {strconv.Itoa(limit)}})
			if err != nil {
				return nil, fmt.Errorf("Error retrieving %s: %w", item.label, err)
			}
			if empty(v) {
				return item.empty, nil
			}
			var rows []any
			if list, ok := v.([]any); ok {
				rows = list
			} else if obj, ok := v.(map[string]any); ok {
				rows, _ = obj["challenges"].([]any)
				if rows == nil {
					rows = []any{obj}
				}
			}
			out := []map[string]any{}
			for _, v := range rows {
				c, ok := v.(map[string]any)
				if !ok {
					continue
				}
				if item.name == "get_adhoc_challenges" {
					activity, _ := c["socialChallengeActivityTypeId"].(float64)
					status, _ := c["socialChallengeStatusId"].(float64)
					act := map[int]string{1: "Running", 2: "Cycling", 3: "Swimming", 4: "Steps", 5: "Walking"}[int(activity)]
					if act == "" {
						act = fmt.Sprintf("type_%v", c["socialChallengeActivityTypeId"])
					}
					state := map[int]string{1: "Not Started", 2: "In Progress", 3: "Completed", 4: "Ended"}[int(status)]
					if state == "" {
						state = fmt.Sprintf("status_%v", c["socialChallengeStatusId"])
					}
					out = append(out, map[string]any{"name": c["adHocChallengeName"], "description": c["adHocChallengeDesc"], "uuid": c["uuid"], "activity_type": act, "status": state, "start_date": shortDate(c["startDate"]), "end_date": shortDate(c["endDate"]), "your_ranking": c["userRanking"], "player_count": c["playerCount"]})
					continue
				}
				name := c["badgeChallengeName"]
				if !truthy(name) {
					name = c["name"]
				}
				if !truthy(name) {
					name = c["challengeName"]
				}
				entry := map[string]any{"name": name, "uuid": c["uuid"], "start_date": shortDate(c["startDate"]), "end_date": shortDate(c["endDate"])}
				first := func(keys ...string) any {
					for _, k := range keys {
						if c[k] != nil {
							return c[k]
						}
					}
					return nil
				}
				progress := first("badgeProgressValue", "progress", "progressValue")
				target := first("badgeTargetValue", "target", "targetValue")
				p, pok := progress.(float64)
				t, tok := target.(float64)
				if pok && tok && t > 0 {
					unit, hasUnit := c["badgeUnitId"].(float64)
					if !hasUnit || unit == 1 {
						entry["progress_meters"] = p
						entry["target_meters"] = t
						entry["progress_km"] = fmt.Sprintf("%.2f km", p/1000)
						entry["target_km"] = fmt.Sprintf("%.2f km", t/1000)
					} else {
						entry["progress"] = badgeValue(p, int(unit))
						entry["target"] = badgeValue(t, int(unit))
					}
					pct := p / t * 100
					if pct > 100 {
						pct = 100
					}
					entry["progress_percent"] = fmt.Sprintf("%.1f%%", pct)
				}
				out = append(out, entry)
			}
			if item.name == "get_adhoc_challenges" {
				sort.SliceStable(out, func(i, j int) bool {
					a, _ := out[i]["start_date"].(string)
					b, _ := out[j]["start_date"].(string)
					return a > b
				})
			}
			return map[string]any{"total": len(out), "challenges": out}, nil
		})
	}
}
