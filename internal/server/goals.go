package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func goalsList(v any) []map[string]any {
	if obj, ok := v.(map[string]any); ok {
		for _, key := range []string{"goals", "items", "data"} {
			if list, ok := obj[key].([]any); ok {
				v = list
				break
			}
		}
		if _, ok := v.(map[string]any); ok {
			for _, key := range []string{"name", "goalType", "id", "type"} {
				if _, exists := obj[key]; exists {
					return []map[string]any{obj}
				}
			}
			return nil
		}
	}
	list, _ := v.([]any)
	out := []map[string]any{}
	for _, v := range list {
		if m, ok := v.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
func curateGoal(goal map[string]any) map[string]any {
	progress, _ := goal["progress"].(map[string]any)
	_, hasName := goal["name"]
	_, hasProgress := goal["progress"].(map[string]any)
	_, hasDistance := goal["distanceInMeters"]
	_, hasType := goal["type"]
	_, hasLegacy := goal["goalType"]
	if !hasName || !(hasProgress || hasDistance || (hasType && !hasLegacy)) {
		return goal
	}
	out := remapFields(goal, map[string]string{"id": "id", "name": "name", "type": "type", "activity_type": "activityType", "period": "period", "privacy": "privacy", "start_date": "startDate", "end_date": "endDate", "active": "active", "completed": "completed", "target_distance_meters": "distanceInMeters", "target_duration_seconds": "durationInSeconds", "target_calories": "caloriesInKiloCalories", "target_activities": "numberOfActivities"})
	for label, data := range map[string]map[string]any{"progress": progress, "remaining": asMap(goal["remaining"]), "overage": asMap(goal["overage"])} {
		fields := map[string]string{"percent": "percent"}
		if label != "overage" {
			for k, v := range map[string]string{"distance_meters": "distanceInMeters", "duration_seconds": "durationInSeconds", "calories": "caloriesInKiloCalories", "activities": "numberOfActivities", "days": "days"} {
				fields[k] = v
			}
		}
		for key, src := range fields {
			if data[src] != nil {
				out[label+"_"+key] = data[src]
			}
		}
	}
	return out
}
func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func registerGoals(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_goals", description("get_goals"), map[string]any{"goal_type": map[string]any{"type": "string", "default": "active"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		kind, _ := args["goal_type"].(string)
		kind = strings.ToLower(strings.TrimSpace(kind))
		if kind == "" {
			kind = "active"
		}
		if kind != "active" && kind != "future" && kind != "past" {
			return fmt.Sprintf("Invalid goal_type '%s'. Options: active, future, past.", kind), nil
		}
		goals := []map[string]any{}
		seen := map[string]bool{}
		for page := 0; page < 20; page++ {
			q := url.Values{"status": {kind}, "start": {strconv.Itoa(1 + page*100)}, "limit": {"100"}, "sortOrder": {"asc"}}
			v, err := s.Client.DoJSON(ctx, garmin.Req{Path: "/goal-service/goal/goals", Query: q, Header: http.Header{"Sec-Fetch-Site": []string{"same-origin"}}})
			if err != nil {
				goals = nil
				break
			}
			entries := goalsList(v)
			added := 0
			for _, entry := range entries {
				marker := entry["id"]
				if marker == nil {
					marker = entry
				}
				raw, _ := json.Marshal(marker)
				if seen[string(raw)] {
					continue
				}
				seen[string(raw)] = true
				goals = append(goals, curateGoal(entry))
				added++
			}
			if len(entries) < 100 || added == 0 {
				break
			}
		}
		if len(goals) == 0 {
			seen = map[string]bool{}
			for start := 0; ; start += 30 {
				v, err := s.Client.GetJSON(ctx, "/goal-service/goal/goals", url.Values{"status": {kind}, "start": {strconv.Itoa(start)}, "limit": {"30"}, "sortOrder": {"asc"}})
				if err != nil {
					return nil, fmt.Errorf("Error retrieving %s goals: %w", kind, err)
				}
				entries := goalsList(v)
				added := 0
				for _, entry := range entries {
					raw, _ := json.Marshal(entry)
					if seen[string(raw)] {
						continue
					}
					seen[string(raw)] = true
					goals = append(goals, entry)
					added++
				}
				if len(entries) == 0 || added == 0 {
					break
				}
			}
		}
		if len(goals) == 0 {
			return fmt.Sprintf("No %s goals found.", kind), nil
		}
		return goals, nil
	})
}
