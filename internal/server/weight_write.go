package server

import (
	"context"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"math"
	"net/http"
	"strings"
	"time"
)

func parseTimestamp(value string, loc *time.Location) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t, nil
	}
	return time.ParseInLocation("2006-01-02T15:04:05.999999999", value, loc)
}
func registerWeightWrites(srv *mcp.Server, m *manager) {
	for _, name := range []string{"add_weigh_in", "add_weigh_in_with_timestamps"} {
		name := name
		schema := map[string]any{"weight": map[string]any{"type": "number"}, "unit_key": map[string]any{"type": "string", "default": "kg"}}
		if name == "add_weigh_in_with_timestamps" {
			schema["date_timestamp"] = map[string]any{"type": "string"}
			schema["gmt_timestamp"] = map[string]any{"type": "string"}
		}
		addTool(srv, m, name, description(name), schema, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
			weight, ok := args["weight"].(float64)
			if !ok || weight <= 0 || math.IsInf(weight, 0) || math.IsNaN(weight) {
				return nil, fmt.Errorf("weight must be a positive number")
			}
			unit := "kg"
			if v, ok := args["unit_key"].(string); ok {
				unit = strings.ToLower(strings.TrimSpace(v))
			}
			if unit == "lb" {
				unit = "lbs"
			}
			if unit != "kg" && unit != "lbs" {
				return nil, fmt.Errorf("unitKey must be kg or lbs")
			}
			now := time.Now()
			local := now
			gmt := now.UTC()
			localArg, _ := args["date_timestamp"].(string)
			gmtArg, _ := args["gmt_timestamp"].(string)
			if name == "add_weigh_in_with_timestamps" {
				if localArg == "" && gmtArg != "" {
					return "date_timestamp is required when gmt_timestamp is given: the local timestamp sets the day the weigh-in is recorded on.", nil
				}
				if localArg != "" {
					var err error
					local, err = parseTimestamp(localArg, time.Local)
					if err != nil {
						return nil, fmt.Errorf("Error adding weight measurement with timestamps: %w", err)
					}
					local = local.Local()
					gmt = local.UTC()
				}
				if gmtArg != "" {
					var err error
					gmt, err = parseTimestamp(gmtArg, time.UTC)
					if err != nil {
						return nil, fmt.Errorf("Error adding weight measurement with timestamps: %w", err)
					}
					gmt = gmt.UTC()
				}
			}
			payload := map[string]any{"dateTimestamp": local.Format("2006-01-02T15:04:05.000"), "gmtTimestamp": gmt.Format("2006-01-02T15:04:05.000"), "unitKey": unit, "sourceType": "MANUAL", "value": weight}
			tracker := newWriteTracker(name, "Error adding weight measurement", m.opt.Log)
			tracker.Sent()
			_, err := s.Client.Do(ctx, garmin.Req{Method: http.MethodPost, Path: "/weight-service/user-weight", JSON: payload})
			if err != nil {
				return nil, tracker.Fail(err)
			}
			tracker.Committed()
			out := map[string]any{"status": "success", "weight": weight, "unit": unit, "message": "Weight measurement added successfully"}
			if name == "add_weigh_in_with_timestamps" {
				if localArg == "" {
					localArg = local.Format("2006-01-02T15:04:05")
					gmtArg = gmt.Format("2006-01-02T15:04:05")
				}
				out["timestamp_local"] = localArg
				if gmtArg != "" {
					out["timestamp_gmt"] = gmtArg
				} else {
					out["timestamp_gmt"] = nil
				}
			}
			return out, nil
		}, "weight")
	}
}
