package garmin

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// Port of the python-garminconnect workout, schedule, calendar and GraphQL methods (GC/__init__.py).

const (
	workoutsPath         = "/workout-service"
	workoutScheduledPath = workoutsPath + "/schedule"
	calendarPath         = "/calendar-service"
	graphQLPath          = "/graphql-gateway/graphql"
)

// positiveID is str(_validate_positive_integer(int(id), name)).
func positiveID(id int64, name string) (string, error) {
	if id <= 0 {
		return "", fmt.Errorf("%s must be a positive integer, got: %d", name, id)
	}
	return strconv.FormatInt(id, 10), nil
}

// GetWorkouts returns one page of the workout library (Garmin's start is 1-based; 0 counts as 1).
func (c *Client) GetWorkouts(ctx context.Context, start, limit int) (any, error) {
	if start < 0 {
		return nil, fmt.Errorf("start must be non-negative, got: %d", start)
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be a positive integer, got: %d", limit)
	}
	return c.GetJSON(ctx, workoutsPath+"/workouts", url.Values{"start": {strconv.Itoa(start)}, "limit": {strconv.Itoa(limit)}})
}

func (c *Client) GetWorkoutByID(ctx context.Context, id int64) (any, error) {
	s, err := positiveID(id, "workout_id")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, workoutsPath+"/workout/"+s, nil)
}

// GetFbtAdaptiveWorkout is the raw connectapi call the tool makes for a Garmin Coach workout UUID.
func (c *Client) GetFbtAdaptiveWorkout(ctx context.Context, id string) (any, error) {
	return c.GetJSON(ctx, workoutsPath+"/fbt-adaptive/"+pathSeg(id), nil)
}

func (c *Client) DeleteWorkout(ctx context.Context, id int64) (any, error) {
	s, err := positiveID(id, "workout_id")
	if err != nil {
		return nil, err
	}
	return c.DoJSON(ctx, Req{Method: http.MethodDelete, Path: workoutsPath + "/workout/" + s})
}

// DownloadWorkout returns the workout as FIT bytes; failures read "Download error: ..." like the library's wrapper.
func (c *Client) DownloadWorkout(ctx context.Context, id int64) ([]byte, error) {
	s, err := positiveID(id, "workout_id")
	if err != nil {
		return nil, err
	}
	b, err := c.Download(ctx, workoutsPath+"/workout/FIT/"+s, nil)
	if err != nil {
		return nil, fmt.Errorf("Download error: %w", err)
	}
	return b, nil
}

// UploadWorkout POSTs a workout DTO (a JSON object or array).
func (c *Client) UploadWorkout(ctx context.Context, payload any) (any, error) {
	return c.DoJSON(ctx, Req{Method: http.MethodPost, Path: workoutsPath + "/workout", JSON: payload})
}

func (c *Client) ScheduleWorkout(ctx context.Context, workoutID int64, date string) (any, error) {
	s, err := positiveID(workoutID, "workout_id")
	if err != nil {
		return nil, err
	}
	d, err := ValidateDate(date, "date_str")
	if err != nil {
		return nil, err
	}
	return c.DoJSON(ctx, Req{Method: http.MethodPost, Path: workoutScheduledPath + "/" + s, JSON: map[string]any{"date": d}})
}

func (c *Client) GetScheduledWorkoutByID(ctx context.Context, id int64) (any, error) {
	s, err := positiveID(id, "scheduled_workout_id")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, workoutScheduledPath+"/"+s, nil)
}

// UpdateScheduledWorkout PUTs a whole calendar entry back (the tool's reschedule; the library has no method for it).
func (c *Client) UpdateScheduledWorkout(ctx context.Context, id int64, entry any) (any, error) {
	return c.DoJSON(ctx, Req{Method: http.MethodPut, Path: workoutScheduledPath + "/" + strconv.FormatInt(id, 10), JSON: entry})
}

// UnscheduleWorkout removes a calendar entry without deleting the workout.
func (c *Client) UnscheduleWorkout(ctx context.Context, id int64) (any, error) {
	s, err := positiveID(id, "scheduled_workout_id")
	if err != nil {
		return nil, err
	}
	return c.DoJSON(ctx, Req{Method: http.MethodDelete, Path: workoutScheduledPath + "/" + s})
}

// GetScheduledWorkouts is the whole calendar feed for a 1-indexed month (Garmin counts months from 0).
func (c *Client) GetScheduledWorkouts(ctx context.Context, year, month int) (any, error) {
	if year <= 0 {
		return nil, fmt.Errorf("year must be a positive integer, got: %d", year)
	}
	if year < 2000 {
		return nil, fmt.Errorf("year must be 2000 or later, got: %d", year)
	}
	if month <= 0 {
		return nil, fmt.Errorf("month must be a positive integer, got: %d", month)
	}
	if month > 12 {
		return nil, fmt.Errorf("month must be between 1 and 12, got: %d", month)
	}
	return c.GetJSON(ctx, fmt.Sprintf("%s/year/%d/month/%d", calendarPath, year, month-1), nil)
}

// GetCalendarEvent fetches an event's detail by id, or by shareable UUID when shareable is set.
func (c *Client) GetCalendarEvent(ctx context.Context, id string, shareable bool) (any, error) {
	p := calendarPath + "/event/" + pathSeg(id)
	if shareable {
		p += "/shareable"
	}
	return c.GetJSON(ctx, p, nil)
}

// QueryGraphQL POSTs a GraphQL request body ({"query": ...}) and returns the parsed response.
func (c *Client) QueryGraphQL(ctx context.Context, query map[string]any) (any, error) {
	return c.DoJSON(ctx, Req{Method: http.MethodPost, Path: graphQLPath, JSON: query})
}
