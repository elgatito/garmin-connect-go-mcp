package server

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/url"
)

func registerEndurance(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_endurance_score", description("get_endurance_score"), map[string]any{"start_date": map[string]any{"type": "string"}, "end_date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		start, e := dateArgument(args, "start_date")
		if e != nil {
			return nil, e
		}
		end, e := dateArgument(args, "end_date")
		if e != nil {
			return nil, e
		}
		v, e := s.Client.GetJSON(ctx, "/metrics-service/metrics/endurancescore/stats", url.Values{"startDate": {start}, "endDate": {end}, "aggregation": {"weekly"}})
		if e != nil {
			return nil, fmt.Errorf("Error retrieving endurance score data: %w", e)
		}
		data := asMap(v)
		if len(data) == 0 {
			return fmt.Sprintf("No endurance score data found between %s and %s.", start, end), nil
		}
		types, _ := s.Client.GetJSON(ctx, "/activity-service/activity/activityTypes", nil)
		names := map[string]string{}
		for _, v := range slice(types) {
			t := asMap(v)
			if t["typeId"] != nil {
				name, _ := t["typeKey"].(string)
				if name == "" {
					name = "unknown"
				}
				names[deviceID(t["typeId"])] = name
			}
		}
		contributors := func(v any) []any {
			result := []any{}
			for _, x := range slice(v) {
				c := asMap(x)
				item := map[string]any{}
				if n, ok := c["contribution"].(float64); ok && n != 0 {
					item["contribution_percent"] = rounded(n, 100)
				}
				if c["activityTypeId"] != nil {
					id := deviceID(c["activityTypeId"])
					name := names[id]
					if name == "" {
						name = "unknown_" + id
					}
					item["activity_type"] = name
					item["activity_type_id"] = c["activityTypeId"]
				} else if c["group"] != nil {
					g := deviceID(c["group"])
					name := map[string]string{"0": "running (?)", "1": "biking (?)", "8": "Other Activities"}[g]
					if name == "" {
						name = "group_" + g
					}
					item["group"] = name
				}
				result = append(result, item)
			}
			return result
		}
		score := asMap(data["enduranceScoreDTO"])
		out := map[string]any{"start_date": start, "end_date": end}
		copyPresent(out, data, map[string]string{"period_avg_score": "avg", "period_max_score": "max"})
		copyPresent(out, score, map[string]string{"current_score": "overallScore", "current_date": "calendarDate", "classification_id": "classification"})
		if score["classification"] != nil {
			id := deviceID(score["classification"])
			label := map[string]string{"1": "recreational", "2": "intermediate", "3": "trained", "4": "well_trained", "5": "expert", "6": "superior", "7": "elite"}[id]
			if label == "" {
				label = "level_" + id
			}
			out["classification"] = label
		}
		if score["classificationLowerLimitTrained"] != nil {
			thresholds := map[string]any{}
			copyPresent(thresholds, score, map[string]string{"intermediate": "classificationLowerLimitIntermediate", "trained": "classificationLowerLimitTrained", "well_trained": "classificationLowerLimitWellTrained", "expert": "classificationLowerLimitExpert", "superior": "classificationLowerLimitSuperior", "elite": "classificationLowerLimitElite"})
			out["thresholds"] = thresholds
		}
		if c := contributors(score["contributors"]); len(c) > 0 {
			out["contributors"] = c
		}
		groups := asMap(data["groupMap"])
		weeks := []any{}
		for _, date := range sortedKeys(groups) {
			week := asMap(groups[date])
			w := map[string]any{"week_start": date}
			copyPresent(w, week, map[string]string{"avg_score": "groupAverage", "max_score": "groupMax"})
			if c := contributors(week["enduranceContributorDTOList"]); len(c) > 0 {
				w["contributors"] = c
			}
			weeks = append(weeks, w)
		}
		if len(weeks) > 0 {
			out["weekly_breakdown"] = weeks
		}
		return out, nil
	}, "start_date", "end_date")
}
