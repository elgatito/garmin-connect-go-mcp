package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/url"
)

func hillSummary(data map[string]any, average any, start, end string) map[string]any {
	out := map[string]any{"start_date": start, "end_date": end}
	if average != nil {
		out["period_avg_score"] = average
	}
	if data["maxScore"] != nil {
		out["max_score"] = data["maxScore"]
	}
	rows, _ := data["hillScoreDTOList"].([]any)
	daily := []any{}
	var latest map[string]any
	latestDate := ""
	for _, v := range rows {
		row, ok := v.(map[string]any)
		if !ok {
			continue
		}
		daily = append(daily, map[string]any{"date": row["calendarDate"], "overall": row["overallScore"], "strength": row["strengthScore"], "endurance": row["enduranceScore"]})
		date, _ := row["calendarDate"].(string)
		if latest == nil || date > latestDate {
			latest = row
			latestDate = date
		}
	}
	out["daily_scores"] = daily
	for k, src := range map[string]string{"latest_date": "calendarDate", "latest_overall_score": "overallScore", "latest_strength_score": "strengthScore", "latest_endurance_score": "enduranceScore", "latest_classification_id": "hillScoreClassificationId"} {
		if latest[src] != nil {
			out[k] = latest[src]
		}
	}
	return out
}
func firstJSONValue(raw json.RawMessage) any {
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') || !d.More() {
		return nil
	}
	if _, err = d.Token(); err != nil {
		return nil
	}
	var value any
	if d.Decode(&value) != nil {
		return nil
	}
	return value
}
func registerHillScore(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_hill_score", description("get_hill_score"), map[string]any{"start_date": map[string]any{"type": "string"}, "end_date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		start, err := dateArgument(args, "start_date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving hill score data: %w", err)
		}
		end, err := dateArgument(args, "end_date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving hill score data: %w", err)
		}
		b, err := s.Client.Do(ctx, garmin.Req{Path: "/metrics-service/metrics/hillscore/stats", Query: url.Values{"startDate": {start}, "endDate": {end}, "aggregation": {"daily"}}})
		if err != nil {
			return nil, fmt.Errorf("Error retrieving hill score data: %w", err)
		}
		var data map[string]any
		if err = json.Unmarshal(b, &data); err != nil {
			return nil, fmt.Errorf("Error retrieving hill score data: %w", err)
		}
		if len(data) == 0 {
			return fmt.Sprintf("No hill score data found between %s and %s.", start, end), nil
		}
		var raw map[string]json.RawMessage
		json.Unmarshal(b, &raw)
		return hillSummary(data, firstJSONValue(raw["periodAvgScore"]), start, end), nil
	}, "start_date", "end_date")
}
