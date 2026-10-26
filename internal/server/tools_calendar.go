package server

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const getCalendarEventsDescription = `Get races and events on the Garmin Connect calendar between two dates

Returns calendar entries the user added or subscribed to in Garmin
Connect, such as upcoming races. Use this to answer questions about
which events or races are scheduled.

These entries are not returned by get_scheduled_workouts, which covers
only workouts, nor by get_goals. This is the only tool that exposes
them.

Each event reports its distance in meters when Garmin stores one,
the user's custom goal time in seconds when set, the local start
time when the organiser published it, and two flags: is_race marks
the entry as a race rather than a general event, and primary_event
marks the goal race that an active training plan targets. Goal time
comes from a per-event detail request.`

type calendarEventsInput struct {
	Account
	StartDate string `json:"start_date" jsonschema:"Start date in YYYY-MM-DD format"`
	EndDate   string `json:"end_date" jsonschema:"End date in YYYY-MM-DD format"`
}

func registerCalendarTools(r *registry) {
	addTool(r, "get_calendar_events", getCalendarEventsDescription, getCalendarEvents)
}

var distanceUnitToMeters = map[string]float64{
	"meter": 1.0, "kilometer": 1000.0, "yard": 0.9144, "mile": 1609.344,
}

func calendarTargetDistance(item map[string]any) any {
	target := asMap(item["completionTarget"])
	if target["unitType"] != "distance" {
		return nil
	}
	value, ok := numberOf(target["value"])
	if !ok {
		return nil
	}
	unit, _ := target["unit"].(string)
	scale, ok := distanceUnitToMeters[unit]
	if !ok {
		return nil
	}
	return value * scale
}

func calendarEventPath(item map[string]any) (string, bool) {
	eventID := item["id"]
	switch x := eventID.(type) {
	case string:
		if strings.TrimSpace(x) != "" {
			return "/calendar-service/event/" + x, true
		}
	case float64:
		return "/calendar-service/event/" + strconv.FormatInt(int64(x), 10), true
	case int:
		return "/calendar-service/event/" + strconv.Itoa(x), true
	case int64:
		return "/calendar-service/event/" + strconv.FormatInt(x, 10), true
	}
	if uuid := cleanCalendarString(item["shareableEventUuid"]); uuid != nil {
		return "/calendar-service/event/" + *uuid + "/shareable", true
	}
	return "", false
}

func cleanCalendarString(value any) *string {
	s, ok := value.(string)
	if !ok {
		return nil
	}
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func calendarGoalTime(detail map[string]any) any {
	customization := asMap(detail["eventCustomization"])
	goal := asMap(customization["customGoal"])
	value, ok := numberOf(goal["value"])
	if goal["unitType"] != "time" || goal["unit"] != "second" || !ok {
		return nil
	}
	return value
}

func curateCalendarEvent(item, detail map[string]any) map[string]any {
	eventTime := asMap(item["eventTimeLocal"])
	return map[string]any{
		"title":             item["title"],
		"date":              item["date"],
		"is_race":           truthy(item["isRace"]),
		"primary_event":     truthy(item["primaryEvent"]),
		"subscribed":        truthy(item["subscribed"]),
		"distance_meters":   calendarTargetDistance(item),
		"goal_time_seconds": calendarGoalTime(detail),
		"start_time_local":  eventTime["startTimeHhMm"],
		"time_zone":         eventTime["timeZoneId"],
		"location":          cleanCalendarString(item["location"]),
		"url":               item["url"],
		"event_uuid":        item["shareableEventUuid"],
	}
}

func monthRange(start, end timeDate) []timeDate {
	var out []timeDate
	year, month := start.year, start.month
	for year < end.year || (year == end.year && month <= end.month) {
		out = append(out, timeDate{year, month})
		if month == 12 {
			year, month = year+1, 1
		} else {
			month++
		}
	}
	return out
}

type timeDate struct {
	year  int
	month int
}

func parseDateParts(value string) (timeDate, error) {
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return timeDate{}, err
	}
	return timeDate{t.Year(), int(t.Month())}, nil
}

func getCalendarEvents(ctx context.Context, s *Session, in calendarEventsInput) (any, error) {
	startDate, err := validateWorkoutDate(in.StartDate, "start_date")
	if err != nil {
		return err.Error(), nil
	}
	endDate, err := validateWorkoutDate(in.EndDate, "end_date")
	if err != nil {
		return err.Error(), nil
	}
	if startDate > endDate {
		return "start_date must be on or before end_date.", nil
	}
	start, _ := parseDateParts(startDate)
	end, _ := parseDateParts(endDate)

	events := []any{}
	seen := map[string]bool{}
	for _, ym := range monthRange(start, end) {
		payload, err := s.Client.GetScheduledWorkouts(ctx, ym.year, ym.month)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving calendar events: %w", err)
		}
		for _, itemValue := range asList(asMap(payload)["calendarItems"]) {
			item, ok := itemValue.(map[string]any)
			if !ok || item["itemType"] != "event" {
				continue
			}
			day, ok := item["date"].(string)
			if !ok || day < startDate || day > endDate {
				continue
			}
			key := fmt.Sprintf("%T:%v|%T:%v|%s", item["shareableEventUuid"], item["shareableEventUuid"], item["title"], item["title"], day)
			if seen[key] {
				continue
			}
			seen[key] = true
			detail := map[string]any{}
			if path, ok := calendarEventPath(item); ok {
				if value, err := s.Client.GetCalendarEventDetail(ctx, path); err == nil {
					detail = asMap(value)
				}
			}
			events = append(events, curateCalendarEvent(item, detail))
		}
	}
	sort.SliceStable(events, func(i, j int) bool {
		left, right := asMap(events[i]), asMap(events[j])
		leftDate, _ := left["date"].(string)
		rightDate, _ := right["date"].(string)
		if leftDate != rightDate {
			return leftDate < rightDate
		}
		leftTitle, _ := left["title"].(string)
		rightTitle, _ := right["title"].(string)
		return leftTitle < rightTitle
	})
	return map[string]any{
		"count":      len(events),
		"date_range": map[string]any{"start": startDate, "end": endDate},
		"events":     events,
	}, nil
}
