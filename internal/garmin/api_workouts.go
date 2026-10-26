package garmin

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

const (
	workoutServicePath  = "/workout-service"
	workoutListPath     = workoutServicePath + "/workouts"
	workoutSchedulePath = workoutServicePath + "/schedule"
	calendarServicePath = "/calendar-service"
	graphqlPath         = "graphql-gateway/graphql"
	workoutIDParam      = "workout_id"
)

// workoutPath builds /workout-service/workout/{id}, validating Garmin's
// positive-integer ID.
func workoutPath(id int64, suffix string) (string, error) {
	if id <= 0 {
		return "", fmt.Errorf("%s must be a positive integer, got: %d", workoutIDParam, id)
	}
	return workoutServicePath + "/workout/" + strconv.FormatInt(id, 10) + suffix, nil
}

func scheduleEntryPath(id int64) (string, error) {
	if id <= 0 {
		return "", fmt.Errorf("scheduled_workout_id must be a positive integer, got: %d", id)
	}
	return workoutSchedulePath + "/" + strconv.FormatInt(id, 10), nil
}

// GetWorkouts returns one page of the workout library.
func (c *Client) GetWorkouts(ctx context.Context, start, limit int) (any, error) {
	if _, err := ValidateNonNegativeInteger(start, "start"); err != nil {
		return nil, err
	}
	if _, err := ValidatePositiveInteger(limit, "limit"); err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, workoutListPath, url.Values{
		"start": {strconv.Itoa(start)},
		"limit": {strconv.Itoa(limit)},
	})
}

// GetWorkoutByID returns one workout from the library.
func (c *Client) GetWorkoutByID(ctx context.Context, id int64) (any, error) {
	path, err := workoutPath(id, "")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, path, nil)
}

// GetWorkoutByUUID returns an adaptive training-plan workout.
func (c *Client) GetWorkoutByUUID(ctx context.Context, uuid string) (any, error) {
	return c.GetJSON(ctx, workoutServicePath+"/fbt-adaptive/"+uuid, nil)
}

// DownloadWorkout returns the workout as FIT bytes.
func (c *Client) DownloadWorkout(ctx context.Context, id int64) ([]byte, error) {
	if id <= 0 {
		return nil, fmt.Errorf("%s must be a positive integer, got: %d", workoutIDParam, id)
	}
	return c.Download(ctx, workoutServicePath+"/workout/FIT/"+strconv.FormatInt(id, 10), nil)
}

// UploadWorkout posts a structured workout DTO.
func (c *Client) UploadWorkout(ctx context.Context, payload any) (any, error) {
	return c.DoJSON(ctx, Req{Method: http.MethodPost, Path: workoutServicePath + "/workout", JSON: payload})
}

// DeleteWorkout permanently deletes a workout template.
func (c *Client) DeleteWorkout(ctx context.Context, id int64) (any, error) {
	path, err := workoutPath(id, "")
	if err != nil {
		return nil, err
	}
	return c.DoJSON(ctx, Req{Method: http.MethodDelete, Path: path})
}

// GetScheduledWorkouts returns the calendar feed for one month. Garmin expects
// a zero-indexed month, while callers pass the normal 1-indexed month.
func (c *Client) GetScheduledWorkouts(ctx context.Context, year, month int) (any, error) {
	if _, err := ValidatePositiveInteger(year, "year"); err != nil {
		return nil, err
	}
	if year < 2000 {
		return nil, fmt.Errorf("year must be 2000 or later, got: %d", year)
	}
	if month < 1 || month > 12 {
		return nil, fmt.Errorf("month must be between 1 and 12, got: %d", month)
	}
	path := fmt.Sprintf("%s/year/%d/month/%d", calendarServicePath, year, month-1)
	return c.GetJSON(ctx, path, nil)
}

// GetScheduledWorkoutByID returns one calendar entry.
func (c *Client) GetScheduledWorkoutByID(ctx context.Context, id int64) (any, error) {
	path, err := scheduleEntryPath(id)
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, path, nil)
}

// ScheduleWorkout adds a library workout to the calendar.
func (c *Client) ScheduleWorkout(ctx context.Context, id int64, date string) (any, error) {
	path, err := scheduleEntryPath(id)
	if err != nil {
		return nil, err
	}
	d, err := ValidateDate(date, "date")
	if err != nil {
		return nil, err
	}
	return c.DoJSON(ctx, Req{Method: http.MethodPost, Path: path, JSON: map[string]any{"date": d}})
}

// UnscheduleWorkout removes a calendar entry without deleting the template.
func (c *Client) UnscheduleWorkout(ctx context.Context, id int64) (any, error) {
	path, err := scheduleEntryPath(id)
	if err != nil {
		return nil, err
	}
	return c.DoJSON(ctx, Req{Method: http.MethodDelete, Path: path})
}

// RescheduleWorkout moves a calendar entry in place.
func (c *Client) RescheduleWorkout(ctx context.Context, id int64, payload map[string]any) (any, error) {
	path, err := scheduleEntryPath(id)
	if err != nil {
		return nil, err
	}
	return c.DoJSON(ctx, Req{Method: http.MethodPut, Path: path, JSON: payload})
}

// QueryGraphQL posts a Garmin GraphQL request body.
func (c *Client) QueryGraphQL(ctx context.Context, query map[string]any) (any, error) {
	return c.DoJSON(ctx, Req{Method: http.MethodPost, Path: graphqlPath, JSON: query})
}

// GetCalendarEventDetail returns the per-event detail payload. path is one of
// /calendar-service/event/{id} or /calendar-service/event/{uuid}/shareable.
func (c *Client) GetCalendarEventDetail(ctx context.Context, path string) (any, error) {
	return c.GetJSON(ctx, path, nil)
}
