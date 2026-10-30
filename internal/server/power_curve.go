package server

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"math"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type powerRecord struct {
	power     float64
	valid     bool
	timestamp string
}

func fitNumber(v any) (float64, bool) {
	if v == nil {
		return 0, false
	}
	x := reflect.ValueOf(v)
	switch x.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(x.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(x.Uint()), true
	case reflect.Float32, reflect.Float64:
		return x.Float(), true
	}
	return 0, false
}
func powerCurve(records []powerRecord) map[string]int {
	if len(records) == 0 {
		return nil
	}
	times := make([]time.Time, len(records))
	validTimes := true
	hasPower := false
	for i, r := range records {
		hasPower = hasPower || r.valid
		var err error
		times[i], err = time.Parse("2006-01-02T15:04:05", r.timestamp)
		if err != nil {
			validTimes = false
		}
	}
	if !hasPower {
		return nil
	}
	gaps := make([]int, len(records))
	for i := range gaps {
		gaps[i] = 1
	}
	if validTimes {
		for i := 0; i < len(records)-1; i++ {
			gap := int(times[i+1].Sub(times[i]).Seconds())
			if gap < 0 {
				validTimes = false
				break
			}
			gaps[i] = gap
		}
		if validTimes && len(records) > 1 {
			gap := gaps[len(records)-2]
			if gap > 10 {
				gap = 1
			}
			gaps[len(records)-1] = gap
		}
	}
	if !validTimes {
		for i := range gaps {
			gaps[i] = 1
		}
	}
	values := []float64{}
	for i, r := range records {
		gap := gaps[i]
		held := gap
		if gap > 10 {
			held = 1
		}
		p := r.power
		if !r.valid {
			p = 0
		}
		for j := 0; j < held; j++ {
			values = append(values, p)
		}
		for j := 0; j < min(gap-held, 3600); j++ {
			values = append(values, 0)
		}
	}
	prefix := make([]float64, len(values)+1)
	for i, v := range values {
		prefix[i+1] = prefix[i] + v
	}
	out := map[string]int{}
	for i, duration := range []int{5, 30, 60, 300, 600, 1200, 3600} {
		if len(values) < duration {
			continue
		}
		best := float64(0)
		for j := 0; j+duration <= len(values); j++ {
			sum := prefix[j+duration] - prefix[j]
			if sum > best {
				best = sum
			}
		}
		if best > 0 {
			label := []string{"5s", "30s", "1min", "5min", "10min", "20min", "60min"}[i]
			out[label] = int(math.RoundToEven(best / float64(duration)))
		}
	}
	return out
}
func registerPowerCurve(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_power_duration_curve", description("get_power_duration_curve"), map[string]any{"num_activities": map[string]any{"type": "integer", "default": 20}, "activity_type": map[string]any{"type": "string", "default": "cycling"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		n, err := intArg(args, "num_activities", 20)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("Error computing power duration curve: num_activities must be positive")
		}
		if n > 50 {
			n = 50
		}
		kind := "cycling"
		if v, ok := args["activity_type"].(string); ok {
			kind = v
		}
		requested := strings.ToLower(kind)
		v, err := s.Client.GetJSON(ctx, "/activitylist-service/activities/search/activities", url.Values{"start": {"0"}, "limit": {strconv.Itoa(n)}, "activityType": {requested}})
		if err != nil {
			return nil, fmt.Errorf("Error computing power duration curve: %w", err)
		}
		rows, _ := v.([]any)
		if len(rows) == 0 {
			return "No activities found.", nil
		}
		filtered := []map[string]any{}
		for _, v := range rows {
			a, ok := v.(map[string]any)
			if !ok {
				continue
			}
			typ, _ := a["activityType"].(map[string]any)
			key := strings.ToLower(fmt.Sprint(typ["typeKey"]))
			parent := fmt.Sprint(typ["parentTypeId"])
			if strings.Contains(key, requested) || requested == parent || (requested == "cycling" && (parent == "2" || strings.Contains(key, "biking"))) {
				filtered = append(filtered, a)
			}
		}
		if len(filtered) == 0 {
			return fmt.Sprintf("No %s activities found in the last %d activities.", kind, n), nil
		}
		bests := map[string]any{}
		processed, skipped := 0, 0
		for _, a := range filtered {
			id, err := activityID(a["activityId"])
			if err != nil {
				continue
			}
			data, err := s.Client.Download(ctx, fmt.Sprintf("/download-service/files/activity/%d", id), nil)
			if err != nil {
				skipped++
				continue
			}
			if len(data) == 0 {
				continue
			}
			messages, err := decodeFIT(data)
			if err != nil {
				skipped++
				continue
			}
			records := []powerRecord{}
			for _, message := range messages {
				if message.Name != "record" {
					continue
				}
				var record powerRecord
				for _, field := range message.Fields {
					if field.Name == "power" {
						record.power, record.valid = fitNumber(field.Value)
					}
					if field.Name == "timestamp" {
						record.timestamp, _ = field.Value.(string)
					}
				}
				records = append(records, record)
			}
			for label, watts := range powerCurve(records) {
				previous, _ := bests[label].(map[string]any)
				if previous == nil || watts > previous["watts"].(int) {
					date, _ := a["startTimeLocal"].(string)
					if len(date) > 10 {
						date = date[:10]
					}
					bests[label] = map[string]any{"watts": watts, "activity_id": id, "date": date}
				}
			}
			processed++
		}
		if len(bests) == 0 {
			return fmt.Sprintf("No power data found across %d activities.", processed), nil
		}
		var ftp any
		if entry, ok := bests["20min"].(map[string]any); ok {
			ftp = int(math.RoundToEven(float64(entry["watts"].(int)) * 0.95))
		}
		return map[string]any{"activities_analyzed": processed, "activities_skipped": skipped, "ftp_estimate_w": ftp, "ftp_note": "Estimated as 95% of 20-minute best power", "season_bests": bests}, nil
	})
}
