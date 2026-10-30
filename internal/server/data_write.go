package server

import (
	"context"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"math"
	"net/http"
	"time"
)

func registerDataWrites(srv *mcp.Server, m *manager) {
	addTool(srv, m, "set_blood_pressure", description("set_blood_pressure"), map[string]any{"systolic": map[string]any{"type": "integer"}, "diastolic": map[string]any{"type": "integer"}, "pulse": map[string]any{"type": "integer"}, "notes": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		values := map[string]int{}
		for _, field := range []struct {
			name   string
			lo, hi int
		}{{"systolic", 70, 260}, {"diastolic", 40, 150}, {"pulse", 20, 250}} {
			v, err := intArg(args, field.name, 0)
			if err != nil || v < field.lo || v > field.hi {
				return nil, fmt.Errorf("Error setting blood pressure values: %s must be an int in [%d, %d]", field.name, field.lo, field.hi)
			}
			values[field.name] = v
		}
		now := time.Now()
		notes := args["notes"]
		payload := map[string]any{"measurementTimestampLocal": now.Format("2006-01-02T15:04:05.000"), "measurementTimestampGMT": now.UTC().Format("2006-01-02T15:04:05.000"), "systolic": values["systolic"], "diastolic": values["diastolic"], "pulse": values["pulse"], "sourceType": "MANUAL", "notes": notes}
		tracker := newWriteTracker("set_blood_pressure", "Error setting blood pressure values", m.opt.Log)
		tracker.Sent()
		v, err := s.Client.DoJSON(ctx, garmin.Req{Method: http.MethodPost, Path: "/bloodpressure-service/bloodpressure", JSON: payload})
		if err != nil {
			return nil, tracker.Fail(err)
		}
		tracker.Committed()
		return v, nil
	}, "systolic", "diastolic", "pulse")
	addTool(srv, m, "add_hydration_data", description("add_hydration_data"), map[string]any{"value_in_ml": map[string]any{"type": "integer"}, "cdate": map[string]any{"type": "string"}, "timestamp": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		if _, exists := args["value_in_ml"]; !exists {
			return nil, fmt.Errorf("Error adding hydration data: value_in_ml is required")
		}
		value, err := intArg(args, "value_in_ml", 0)
		if err != nil || math.Abs(float64(value)) > 10000 {
			return nil, fmt.Errorf("Error adding hydration data: value_in_ml must be within -10000 to 10000 ml")
		}
		date, err := dateArgument(args, "cdate")
		if err != nil {
			return nil, fmt.Errorf("Error adding hydration data: %w", err)
		}
		stamp, ok := args["timestamp"].(string)
		if !ok {
			return nil, fmt.Errorf("Error adding hydration data: timestamp is required")
		}
		var ts time.Time
		ts, err = time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			ts, err = time.ParseInLocation("2006-01-02T15:04:05.999999999", stamp, time.Local)
		}
		if err != nil {
			return nil, fmt.Errorf("Error adding hydration data: invalid timestamp format (expected ISO 8601)")
		}
		if ts.Format("2006-01-02") != date {
			return nil, fmt.Errorf("Error adding hydration data: timestamp date (%s) doesn't match cdate (%s)", ts.Format("2006-01-02"), date)
		}
		payload := map[string]any{"calendarDate": date, "timestampLocal": ts.Format("2006-01-02T15:04:05.000"), "valueInML": value}
		tracker := newWriteTracker("add_hydration_data", "Error adding hydration data", m.opt.Log)
		tracker.Sent()
		v, err := s.Client.DoJSON(ctx, garmin.Req{Method: http.MethodPut, Path: "/usersummary-service/usersummary/hydration/log", JSON: payload})
		if err != nil {
			return nil, tracker.Fail(err)
		}
		tracker.Committed()
		return v, nil
	}, "value_in_ml", "cdate", "timestamp")
}
