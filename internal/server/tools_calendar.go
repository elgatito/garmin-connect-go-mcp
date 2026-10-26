package server

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

// Port of garmin_mcp/calendar_events.py.

type calendarEventsIn struct {
	Account
	StartDate string `json:"start_date" jsonschema:"Start date in YYYY-MM-DD format"`
	EndDate   string `json:"end_date" jsonschema:"End date in YYYY-MM-DD format"`
}

// Garmin's calendar-service sends completionTarget.unit as one of these (Connect's custom-event picker).
// Distances are always reported in meters; unknown units become null.
var distanceUnitToMeters = map[string]float64{"meter": 1.0, "kilometer": 1000.0, "yard": 0.9144, "mile": 1609.344}

var calendarDateRe = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}\n?$`)

func registerCalendarTools(r *registry) {
	addTool(r, "get_calendar_events", getCalendarEventsDesc, getCalendarEventsTool)
}

// validateCalendarDate is calendar_events._validate_date. Python's `$` lets one trailing newline through the
// pattern, so that case fails in fromisoformat with its own message.
func validateCalendarDate(value, field string) (time.Time, error) {
	if !calendarDateRe.MatchString(value) {
		return time.Time{}, fmt.Errorf("Invalid %s '%s': expected YYYY-MM-DD", field, value)
	}
	if strings.HasSuffix(value, "\n") {
		return time.Time{}, fmt.Errorf("Invalid isoformat string: %s", pyRepr(value))
	}
	return parseISODate(value)
}

func cleanStr(v any) any {
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.TrimSpace(s)
}

// targetDistanceMeters is the event's distance goal when it is expressed as a distance.
func targetDistanceMeters(item map[string]any) any {
	target := asMap(item["completionTarget"])
	if target["unitType"] != "distance" {
		return nil
	}
	value, ok := asNumber(target["value"])
	unit, _ := target["unit"].(string)
	scale, known := distanceUnitToMeters[unit]
	if !ok || !known {
		return nil
	}
	return value * scale
}

// goalTimeSeconds is the user's custom event goal when Garmin stores it as time.
func goalTimeSeconds(detail map[string]any) any {
	goal := asMap(asMap(detail["eventCustomization"])["customGoal"])
	value, ok := asNumber(goal["value"])
	if goal["unitType"] != "time" || goal["unit"] != "second" || !ok {
		return nil
	}
	return value
}

// fetchEventDetail fetches the event detail payload, which contains the custom goal. Failures give an empty detail,
// so event-detail availability never makes the calendar list unusable.
func fetchEventDetail(ctx context.Context, c *garmin.Client, item map[string]any) map[string]any {
	var id string
	shareable := false
	switch x := item["id"].(type) {
	case string:
		id = x
	case float64:
		if x == float64(int64(x)) {
			id = idString(x)
		}
	}
	if _, isStr := item["id"].(string); id == "" && !isStr {
		uuid, _ := cleanStr(item["shareableEventUuid"]).(string)
		if uuid == "" {
			return map[string]any{}
		}
		id, shareable = uuid, true
	}
	v, err := c.GetCalendarEvent(ctx, id, shareable)
	if err != nil {
		return map[string]any{}
	}
	if m := asMap(v); m != nil {
		return m
	}
	return map[string]any{}
}

func curateEvent(item, detail map[string]any) map[string]any {
	eventTime := asMap(item["eventTimeLocal"])
	return map[string]any{
		"title": item["title"], "date": item["date"],
		"is_race": !falsy(item["isRace"]), "primary_event": !falsy(item["primaryEvent"]), "subscribed": !falsy(item["subscribed"]),
		"distance_meters": targetDistanceMeters(item), "goal_time_seconds": goalTimeSeconds(detail),
		"start_time_local": eventTime["startTimeHhMm"], "time_zone": eventTime["timeZoneId"],
		"location": cleanStr(item["location"]), "url": item["url"], "event_uuid": item["shareableEventUuid"],
	}
}

func getCalendarEventsTool(ctx context.Context, s *Session, in calendarEventsIn) (any, error) {
	start, err := validateCalendarDate(in.StartDate, "start_date")
	if err != nil {
		return nil, err
	}
	end, err := validateCalendarDate(in.EndDate, "end_date")
	if err != nil {
		return nil, err
	}
	if start.After(end) {
		return nil, errors.New("start_date must be on or before end_date.")
	}
	events := []map[string]any{}
	seen := map[string]bool{}
	for y, m := start.Year(), int(start.Month()); y*12+m <= end.Year()*12+int(end.Month()); y, m = y+(m/12), m%12+1 {
		if y < 2000 { // ValueError in the library, reported without the "Error retrieving" prefix
			return nil, fmt.Errorf("year must be 2000 or later, got: %d", y)
		}
		data, err := s.Client.GetScheduledWorkouts(ctx, y, m)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving calendar events: %w", err)
		}
		// Despite its name, the library's get_scheduled_workouts returns the whole calendar feed for a month.
		for _, it := range asList(asMap(data)["calendarItems"]) {
			item, ok := it.(map[string]any)
			if !ok || item["itemType"] != "event" {
				continue
			}
			// The first and last month of the range extend past it.
			day, ok := item["date"].(string)
			if !ok || day < in.StartDate || day > in.EndDate {
				continue
			}
			// Multi-month responses can repeat an event at the seams.
			key := pyReprValue(item["shareableEventUuid"]) + "|" + pyReprValue(item["title"]) + "|" + day
			if seen[key] {
				continue
			}
			seen[key] = true
			events = append(events, curateEvent(item, fetchEventDetail(ctx, s.Client, item)))
		}
	}
	sort.SliceStable(events, func(i, j int) bool {
		di, dj := pyStr(events[i]["date"]), pyStr(events[j]["date"])
		if di != dj {
			return di < dj
		}
		return titleKey(events[i]["title"]) < titleKey(events[j]["title"])
	})
	list := make([]any, len(events))
	for i, e := range events {
		list[i] = e
	}
	return map[string]any{"count": len(events), "date_range": map[string]any{"start": in.StartDate, "end": in.EndDate}, "events": list}, nil
}

func titleKey(v any) string {
	if falsy(v) {
		return ""
	}
	return pyStr(v)
}
