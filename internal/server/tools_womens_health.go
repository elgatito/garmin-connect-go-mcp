package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Port of garmin_mcp/womens_health.py.

// menstrualCalendarMaxDays is Garmin's server-side limit for one calendar request.
const menstrualCalendarMaxDays = 92

type menstrualDateIn struct {
	Account
	Date string `json:"date" jsonschema:"Date in YYYY-MM-DD format"`
}

type menstrualRangeIn struct {
	Account
	StartDate string `json:"start_date" jsonschema:"Start date in YYYY-MM-DD format"`
	EndDate   string `json:"end_date" jsonschema:"End date in YYYY-MM-DD format"`
}

const menstrualCalendarDesc = `Get menstrual calendar data between specified dates

Automatically chunks requests longer than 92 days, Garmin's
server-side limit, and stitches the responses together.
Identical records spanning a chunk boundary are returned once for list
and dict-of-list responses. Other payload shapes are returned per chunk.`

func registerWomensHealthTools(r *registry) {
	addTool(r, "get_pregnancy_summary", "Get pregnancy summary data", func(ctx context.Context, s *Session, _ noArgsIn) (any, error) {
		summary, err := s.Client.GetPregnancySummary(ctx)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving pregnancy summary: %w", err)
		}
		if falsy(summary) {
			return "No pregnancy summary data found.", nil
		}
		return summary, nil
	})
	addTool(r, "get_menstrual_data_for_date", "Get menstrual data for a specific date", func(ctx context.Context, s *Session, in menstrualDateIn) (any, error) {
		data, err := s.Client.GetMenstrualDataForDate(ctx, in.Date)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving menstrual data: %w", err)
		}
		if falsy(data) {
			return fmt.Sprintf("No menstrual data found for %s.", in.Date), nil
		}
		return data, nil
	})
	addTool(r, "get_menstrual_calendar_data", menstrualCalendarDesc, getMenstrualCalendarData)
}

// uniqueItems keeps identical JSON records once, preserving their first occurrence.
func uniqueItems(items []any) []any {
	seen := map[string]bool{}
	out := []any{}
	for _, it := range items {
		b, _ := json.Marshal(it) // map keys are sorted
		if k := string(b); !seen[k] {
			seen[k] = true
			out = append(out, it)
		}
	}
	return out
}

// stitchMenstrualChunks combines list-shaped responses; other payloads are retained grouped by chunk.
func stitchMenstrualChunks(chunks []any) any {
	if len(chunks) == 1 {
		return chunks[0]
	}
	allLists, allListDicts := true, true
	for _, c := range chunks {
		if _, ok := c.([]any); !ok {
			allLists = false
		}
		m, ok := c.(map[string]any)
		if !ok {
			allListDicts = false
			continue
		}
		for _, v := range m {
			if _, isList := v.([]any); !isList {
				allListDicts = false
			}
		}
	}
	switch {
	case allLists:
		var all []any
		for _, c := range chunks {
			all = append(all, asList(c)...)
		}
		return uniqueItems(all)
	case allListDicts:
		merged := map[string][]any{}
		for _, c := range chunks {
			for k, v := range asMap(c) {
				merged[k] = append(merged[k], asList(v)...)
			}
		}
		out := map[string]any{}
		for k, v := range merged {
			out[k] = uniqueItems(v)
		}
		return out
	}
	return chunks
}

func getMenstrualCalendarData(ctx context.Context, s *Session, in menstrualRangeIn) (any, error) {
	fail := func(err error) (any, error) {
		return nil, fmt.Errorf("Error retrieving menstrual calendar data: %w", err)
	}
	start, err := parseISODate(in.StartDate)
	if err != nil {
		return fail(err)
	}
	end, err := parseISODate(in.EndDate)
	if err != nil {
		return fail(err)
	}
	if end.Before(start) {
		return fmt.Sprintf("end_date %s is before start_date %s.", in.EndDate, in.StartDate), nil
	}
	var chunks []any
	for cursor := start; !cursor.After(end); {
		windowEnd := cursor.AddDate(0, 0, menstrualCalendarMaxDays-1)
		if windowEnd.After(end) {
			windowEnd = end
		}
		data, err := s.Client.GetMenstrualCalendarData(ctx, cursor.Format(time.DateOnly), windowEnd.Format(time.DateOnly))
		if err != nil {
			return fail(err)
		}
		if !falsy(data) {
			chunks = append(chunks, data)
		}
		cursor = windowEnd.AddDate(0, 0, 1)
	}
	if len(chunks) == 0 {
		return fmt.Sprintf("No menstrual calendar data found between %s and %s.", in.StartDate, in.EndDate), nil
	}
	return stitchMenstrualChunks(chunks), nil
}
