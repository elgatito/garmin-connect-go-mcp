package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func uniqueJSON(list []any) []any {
	seen := map[string]bool{}
	out := []any{}
	for _, item := range list {
		b, _ := json.Marshal(item)
		key := string(b)
		if !seen[key] {
			seen[key] = true
			out = append(out, item)
		}
	}
	return out
}
func stitchMenstrual(chunks []any) any {
	if len(chunks) == 1 {
		return chunks[0]
	}
	lists := true
	objects := true
	for _, c := range chunks {
		if _, ok := c.([]any); !ok {
			lists = false
		}
		m, ok := c.(map[string]any)
		if !ok {
			objects = false
		} else {
			for _, v := range m {
				if _, ok := v.([]any); !ok {
					objects = false
				}
			}
		}
	}
	if lists {
		all := []any{}
		for _, c := range chunks {
			all = append(all, c.([]any)...)
		}
		return uniqueJSON(all)
	}
	if objects {
		all := map[string][]any{}
		for _, c := range chunks {
			for k, v := range c.(map[string]any) {
				all[k] = append(all[k], v.([]any)...)
			}
		}
		for k, v := range all {
			all[k] = uniqueJSON(v)
		}
		return all
	}
	return chunks
}
func registerWomensHealth(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_pregnancy_summary", description("get_pregnancy_summary"), nil, func(ctx context.Context, s *Session, _ map[string]any) (any, error) {
		v, err := s.Client.GetJSON(ctx, "/periodichealth-service/menstrualcycle/pregnancysnapshot", nil)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving pregnancy summary: %w", err)
		}
		if empty(v) {
			return "No pregnancy summary data found.", nil
		}
		return v, nil
	})
	addTool(srv, m, "get_menstrual_data_for_date", description("get_menstrual_data_for_date"), map[string]any{"date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		date, err := dateArgument(args, "date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving menstrual data: %w", err)
		}
		v, err := s.Client.GetJSON(ctx, "/periodichealth-service/menstrualcycle/dayview/"+date, nil)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving menstrual data: %w", err)
		}
		if empty(v) {
			return fmt.Sprintf("No menstrual data found for %s.", date), nil
		}
		return v, nil
	}, "date")
	addTool(srv, m, "get_menstrual_calendar_data", description("get_menstrual_calendar_data"), map[string]any{"start_date": map[string]any{"type": "string"}, "end_date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		start, err := dateArgument(args, "start_date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving menstrual calendar data: %w", err)
		}
		end, err := dateArgument(args, "end_date")
		if err != nil {
			return nil, fmt.Errorf("Error retrieving menstrual calendar data: %w", err)
		}
		a, _ := time.Parse("2006-01-02", start)
		b, _ := time.Parse("2006-01-02", end)
		if b.Before(a) {
			return fmt.Sprintf("end_date %s is before start_date %s.", end, start), nil
		}
		chunks := []any{}
		for d := a; !d.After(b); {
			last := d.AddDate(0, 0, 91)
			if last.After(b) {
				last = b
			}
			v, err := s.Client.GetJSON(ctx, "/periodichealth-service/menstrualcycle/calendar/"+d.Format("2006-01-02")+"/"+last.Format("2006-01-02"), nil)
			if err != nil {
				return nil, fmt.Errorf("Error retrieving menstrual calendar data: %w", err)
			}
			if !empty(v) {
				chunks = append(chunks, v)
			}
			d = last.AddDate(0, 0, 1)
		}
		if len(chunks) == 0 {
			return fmt.Sprintf("No menstrual calendar data found between %s and %s.", start, end), nil
		}
		return stitchMenstrual(chunks), nil
	}, "start_date", "end_date")
}
