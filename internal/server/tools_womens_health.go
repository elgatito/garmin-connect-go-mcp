package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const (
	menstrualCalendarMaxDays = 92

	getPregnancySummaryDescription = "Get pregnancy summary data"

	getMenstrualDataForDateDescription = `Get menstrual data for a specific date`

	getMenstrualCalendarDataDescription = `Get menstrual calendar data between specified dates

Automatically chunks requests longer than 92 days, Garmin's
server-side limit, and stitches the responses together.
Identical records spanning a chunk boundary are returned once for list
and dict-of-list responses. Other payload shapes are returned per chunk.`
)

type menstrualDateInput struct {
	Account
	Date string `json:"date" jsonschema:"Date in YYYY-MM-DD format"`
}

type menstrualCalendarInput struct {
	Account
	StartDate string `json:"start_date" jsonschema:"Start date in YYYY-MM-DD format"`
	EndDate   string `json:"end_date" jsonschema:"End date in YYYY-MM-DD format"`
}

func registerWomensHealthTools(r *registry) {
	addTool(r, "get_pregnancy_summary", getPregnancySummaryDescription, getPregnancySummary)
	addTool(r, "get_menstrual_data_for_date", getMenstrualDataForDateDescription, getMenstrualDataForDate)
	addTool(r, "get_menstrual_calendar_data", getMenstrualCalendarDataDescription, getMenstrualCalendarData)
}

func getPregnancySummary(ctx context.Context, s *Session, _ noArgsInput) (any, error) {
	summary, err := s.Client.GetPregnancySummary(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving pregnancy summary: %w", err)
	}
	if !truthy(summary) {
		return "No pregnancy summary data found.", nil
	}
	return summary, nil
}

func getMenstrualDataForDate(ctx context.Context, s *Session, in menstrualDateInput) (any, error) {
	data, err := s.Client.GetMenstrualDataForDate(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving menstrual data: %w", err)
	}
	if !truthy(data) {
		return fmt.Sprintf("No menstrual data found for %s.", in.Date), nil
	}
	return data, nil
}

func uniqueItems(items []any) []any {
	seen := map[string]bool{}
	unique := make([]any, 0, len(items))
	for _, item := range items {
		raw, _ := json.Marshal(item)
		key := string(raw)
		if seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, item)
	}
	return unique
}

func stitchMenstrualChunks(chunks []any) any {
	if len(chunks) == 1 {
		return chunks[0]
	}

	allLists := true
	for _, chunk := range chunks {
		if _, ok := chunk.([]any); !ok {
			allLists = false
			break
		}
	}
	if allLists {
		var stitched []any
		for _, chunk := range chunks {
			stitched = append(stitched, chunk.([]any)...)
		}
		return uniqueItems(stitched)
	}

	allDictOfLists := true
	for _, chunk := range chunks {
		m, ok := chunk.(map[string]any)
		if !ok {
			allDictOfLists = false
			break
		}
		for _, value := range m {
			if _, ok := value.([]any); !ok {
				allDictOfLists = false
				break
			}
		}
		if !allDictOfLists {
			break
		}
	}
	if allDictOfLists {
		stitched := map[string][]any{}
		for _, chunk := range chunks {
			for key, value := range chunk.(map[string]any) {
				stitched[key] = append(stitched[key], value.([]any)...)
			}
		}
		out := map[string]any{}
		for key, items := range stitched {
			out[key] = uniqueItems(items)
		}
		return out
	}

	return chunks
}

func getMenstrualCalendarData(ctx context.Context, s *Session, in menstrualCalendarInput) (any, error) {
	start, err := time.Parse("2006-01-02", in.StartDate)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving menstrual calendar data: %w", err)
	}
	end, err := time.Parse("2006-01-02", in.EndDate)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving menstrual calendar data: %w", err)
	}
	if end.Before(start) {
		return fmt.Sprintf("end_date %s is before start_date %s.", in.EndDate, in.StartDate), nil
	}

	var chunks []any
	cursor := start
	for !cursor.After(end) {
		windowEnd := cursor.AddDate(0, 0, menstrualCalendarMaxDays-1)
		if windowEnd.After(end) {
			windowEnd = end
		}
		data, err := s.Client.GetMenstrualCalendarData(ctx, cursor.Format("2006-01-02"), windowEnd.Format("2006-01-02"))
		if err != nil {
			return nil, fmt.Errorf("Error retrieving menstrual calendar data: %w", err)
		}
		if truthy(data) {
			chunks = append(chunks, data)
		}
		cursor = windowEnd.AddDate(0, 0, 1)
	}

	if len(chunks) == 0 {
		return fmt.Sprintf("No menstrual calendar data found between %s and %s.", in.StartDate, in.EndDate), nil
	}
	return stitchMenstrualChunks(chunks), nil
}
