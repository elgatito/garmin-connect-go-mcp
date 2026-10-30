package server

import (
	"context"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"strings"
	"time"
	"unicode"
)

func titleActivity(key string) string {
	words := strings.Fields(strings.ReplaceAll(key, "_", " "))
	for i, word := range words {
		r := []rune(strings.ToLower(word))
		if len(r) > 0 {
			r[0] = unicode.ToUpper(r[0])
		}
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}
func registerManualActivity(srv *mcp.Server, m *manager) {
	schema := map[string]any{"type_key": map[string]any{"type": "string"}, "date": map[string]any{"type": "string"}, "duration_minutes": map[string]any{"type": "integer"}, "start_time": map[string]any{"type": "string", "default": "09:00"}, "activity_name": map[string]any{"type": "string", "default": ""}, "distance_km": map[string]any{"type": "number", "default": 0}, "time_zone": map[string]any{"type": "string", "default": "UTC"}}
	addTool(srv, m, "create_manual_activity", description("create_manual_activity"), schema, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		kind, _ := args["type_key"].(string)
		kind = strings.TrimSpace(kind)
		if kind == "" {
			return "Error: type_key is required", nil
		}
		minutes, err := intArg(args, "duration_minutes", 0)
		if err != nil || minutes <= 0 {
			return "Error: duration_minutes must be greater than 0", nil
		}
		distance := float64(0)
		if v, ok := args["distance_km"].(float64); ok {
			distance = v
		}
		if distance < 0 {
			return "Error: distance_km must be non-negative", nil
		}
		date, err := dateArgument(args, "date")
		if err != nil {
			return "Error: date must be a valid date in YYYY-MM-DD format", nil
		}
		clock := "09:00"
		if v, ok := args["start_time"].(string); ok {
			clock = v
		}
		parsed, err := time.Parse("15:04", clock)
		if err != nil || parsed.Format("15:04") != clock {
			return "Error: start_time must be in HH:MM (24-hour) format", nil
		}
		name, _ := args["activity_name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			name = titleActivity(kind)
		}
		zone := "UTC"
		if v, ok := args["time_zone"].(string); ok {
			zone = v
		}
		payload := map[string]any{"activityTypeDTO": map[string]any{"typeKey": kind}, "accessControlRuleDTO": map[string]any{"typeId": 2, "typeKey": "private"}, "timeZoneUnitDTO": map[string]any{"unitKey": zone}, "activityName": name, "metadataDTO": map[string]any{"autoCalcCalories": true}, "summaryDTO": map[string]any{"startTimeLocal": fmt.Sprintf("%sT%s:00.000", date, clock), "distance": distance * 1000, "duration": minutes * 60}}
		tracker := newWriteTracker("create_manual_activity", "Error creating manual activity", m.opt.Log)
		tracker.Sent()
		v, err := s.Client.DoJSON(ctx, garmin.Req{Method: http.MethodPost, Path: "/activity-service/activity", JSON: payload})
		if err != nil {
			return nil, tracker.Fail(err)
		}
		tracker.Committed()
		return map[string]any{"success": true, "activity": v}, nil
	}, "type_key", "date", "duration_minutes")
}
