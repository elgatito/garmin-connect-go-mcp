package garmin

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
)

const (
	activityServicePath    = "/activity-service/activity"
	activityTypesPath      = "/activity-service/activity/activityTypes"
	activityEventTypesPath = "/activity-service/activity/eventTypes"
	activityForDatePath    = "/mobile-gateway/heartRate/forDate"
	activitiesCountPath    = "/activitylist-service/activities/count"
	deviceLastUsedPath     = "/device-service/deviceservice/mylastused"
	gearFilterPath         = "/gear-service/gear/filterGear"
	gearBasePath           = "/gear-service/gear"
	gearV2ListPath         = "/gear-service/gear/v2/list"
	activityTypeQueryKey   = "activityType"
	activityIDParam        = "activity_id"
	userProfileQueryParam  = "userProfilePk"
)

// activityPath returns the activity-service path for id, validating it as
// Garmin's positive-integer activity ID.
func activityPath(id int64, suffix string) (string, error) {
	if id <= 0 {
		return "", fmt.Errorf("%s must be a positive integer, got: %d", activityIDParam, id)
	}
	return activityServicePath + "/" + strconv.FormatInt(id, 10) + suffix, nil
}

// numberString renders a decoded JSON number as an integer when it is whole, so
// Garmin query params do not receive float formatting.
func numberString(v any) string {
	switch x := v.(type) {
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1<<53 {
			return strconv.FormatInt(int64(x), 10)
		}
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	}
	return fmt.Sprint(v)
}

// GetActivitiesByDatePage fetches one page of activities between two dates.
func (c *Client) GetActivitiesByDatePage(ctx context.Context, startDate, endDate, activityType string, start, limit int) (any, error) {
	q := url.Values{
		"startDate": {startDate},
		"endDate":   {endDate},
		"start":     {strconv.Itoa(start)},
		"limit":     {strconv.Itoa(limit)},
	}
	if activityType != "" {
		q.Set(activityTypeQueryKey, activityType)
	}
	return c.GetJSON(ctx, activitiesSearchPath, q)
}

// GetActivitiesFordate returns the activities recorded on a date.
func (c *Client) GetActivitiesFordate(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "fordate")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, activityForDatePath+"/"+d, nil)
}

// GetActivity returns the activity summary payload.
func (c *Client) GetActivity(ctx context.Context, id int64) (any, error) {
	path, err := activityPath(id, "")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, path, nil)
}

// GetActivityTypes returns every Garmin activity type.
func (c *Client) GetActivityTypes(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, activityTypesPath, nil)
}

// GetActivityEventTypes returns every Garmin activity event type.
func (c *Client) GetActivityEventTypes(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, activityEventTypesPath, nil)
}

// PutActivityUpdate sends a partial activity DTO keyed by activityId.
func (c *Client) PutActivityUpdate(ctx context.Context, id int64, fields map[string]any) (any, error) {
	path, err := activityPath(id, "")
	if err != nil {
		return nil, err
	}
	body := make(map[string]any, len(fields)+1)
	body["activityId"] = id
	for k, v := range fields {
		body[k] = v
	}
	return c.DoJSON(ctx, Req{Method: http.MethodPut, Path: path, JSON: body})
}

// GetActivitySplits returns the lap and length splits for an activity.
func (c *Client) GetActivitySplits(ctx context.Context, id int64) (any, error) {
	return c.getActivitySub(ctx, id, "/splits")
}

// GetActivityTypedSplits returns the typed splits for an activity.
func (c *Client) GetActivityTypedSplits(ctx context.Context, id int64) (any, error) {
	return c.getActivitySub(ctx, id, "/typedsplits")
}

// GetActivitySplitSummaries returns the split summaries for an activity.
func (c *Client) GetActivitySplitSummaries(ctx context.Context, id int64) (any, error) {
	return c.getActivitySub(ctx, id, "/split_summaries")
}

// GetActivityWeather returns the weather recorded for an activity.
func (c *Client) GetActivityWeather(ctx context.Context, id int64) (any, error) {
	return c.getActivitySub(ctx, id, "/weather")
}

// GetActivityHRInTimezones returns the heart-rate time-in-zones payload.
func (c *Client) GetActivityHRInTimezones(ctx context.Context, id int64) (any, error) {
	return c.getActivitySub(ctx, id, "/hrTimeInZones")
}

// GetActivityPowerInTimezones returns the power time-in-zones payload.
func (c *Client) GetActivityPowerInTimezones(ctx context.Context, id int64) (any, error) {
	return c.getActivitySub(ctx, id, "/powerTimeInZones")
}

// GetActivityGear returns the gear linked to an activity.
func (c *Client) GetActivityGear(ctx context.Context, id int64) (any, error) {
	if _, err := activityPath(id, ""); err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, gearFilterPath, url.Values{"activityId": {strconv.FormatInt(id, 10)}})
}

// GetActivityExerciseSets returns the recorded exercise sets.
func (c *Client) GetActivityExerciseSets(ctx context.Context, id int64) (any, error) {
	return c.getActivitySub(ctx, id, "/exerciseSets")
}

func (c *Client) getActivitySub(ctx context.Context, id int64, suffix string) (any, error) {
	path, err := activityPath(id, suffix)
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, path, nil)
}

// SetActivityExerciseSets replaces all exercise sets with Garmin's wire format.
func (c *Client) SetActivityExerciseSets(ctx context.Context, id int64, sets []any) (any, error) {
	path, err := activityPath(id, "/exerciseSets")
	if err != nil {
		return nil, err
	}
	return c.DoJSON(ctx, Req{
		Method: http.MethodPut,
		Path:   path,
		JSON:   map[string]any{"activityId": id, "exerciseSets": sets},
	})
}

// CountActivities returns the account's total activity count.
func (c *Client) CountActivities(ctx context.Context) (any, error) {
	v, err := c.GetJSON(ctx, activitiesCountPath, nil)
	if err != nil {
		return nil, err
	}
	m, _ := v.(map[string]any)
	count, ok := m["totalCount"]
	if !ok {
		return nil, errors.New("No activities count data received")
	}
	return count, nil
}

// CreateManualActivity posts a manually logged activity.
func (c *Client) CreateManualActivity(ctx context.Context, startDatetime, timeZone, typeKey string, distanceKm float64, durationMin int, activityName string) (any, error) {
	return c.DoJSON(ctx, Req{
		Method: http.MethodPost,
		Path:   activityServicePath,
		JSON: map[string]any{
			"activityTypeDTO":      map[string]any{"typeKey": typeKey},
			"accessControlRuleDTO": map[string]any{"typeId": 2, "typeKey": "private"},
			"timeZoneUnitDTO":      map[string]any{"unitKey": timeZone},
			"activityName":         activityName,
			"metadataDTO":          map[string]any{"autoCalcCalories": true},
			"summaryDTO": map[string]any{
				"startTimeLocal": startDatetime,
				"distance":       distanceKm * 1000,
				"duration":       durationMin * 60,
			},
		},
	})
}

// DeleteActivity permanently deletes an activity.
func (c *Client) DeleteActivity(ctx context.Context, id int64) (any, error) {
	path, err := activityPath(id, "")
	if err != nil {
		return nil, err
	}
	return c.DoJSON(ctx, Req{Method: http.MethodDelete, Path: path})
}

// GetDeviceLastUsed returns the last used device, including the user profile
// number gear endpoints need.
func (c *Client) GetDeviceLastUsed(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, deviceLastUsedPath, nil)
}

// GetGear returns all gear registered to a user profile.
func (c *Client) GetGear(ctx context.Context, userProfileNumber any) (any, error) {
	return c.GetJSON(ctx, gearFilterPath, url.Values{
		userProfileQueryParam: {numberString(userProfileNumber)},
	})
}

// GetGearStats returns usage statistics for a gear UUID. A 404 means the gear
// is retired or removed and yields an empty payload, matching the library.
func (c *Client) GetGearStats(ctx context.Context, gearUUID string) (any, error) {
	v, err := c.GetJSON(ctx, gearBasePath+"/stats/"+gearUUID, nil)
	if err != nil {
		var api *APIError
		if errors.As(err, &api) && api.Status == http.StatusNotFound {
			return map[string]any{}, nil
		}
		return nil, err
	}
	return v, nil
}

// GetGearDefaults returns the default gear associations for a user profile.
func (c *Client) GetGearDefaults(ctx context.Context, userProfileNumber any) (any, error) {
	return c.GetJSON(ctx, gearBasePath+"/user/"+numberString(userProfileNumber)+"/activityTypes", nil)
}

// GetGearV2List returns the v2 gear list, which carries free-text notes.
func (c *Client) GetGearV2List(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, gearV2ListPath, nil)
}

// AddGearToActivity links gear to an activity.
func (c *Client) AddGearToActivity(ctx context.Context, gearUUID string, id int64) (any, error) {
	return c.gearLink(ctx, "link", gearUUID, id)
}

// RemoveGearFromActivity unlinks gear from an activity.
func (c *Client) RemoveGearFromActivity(ctx context.Context, gearUUID string, id int64) (any, error) {
	return c.gearLink(ctx, "unlink", gearUUID, id)
}

func (c *Client) gearLink(ctx context.Context, action, gearUUID string, id int64) (any, error) {
	if id <= 0 {
		return nil, fmt.Errorf("%s must be a positive integer, got: %d", activityIDParam, id)
	}
	path := gearBasePath + "/" + action + "/" + gearUUID + "/activity/" + strconv.FormatInt(id, 10)
	v, err := c.DoJSON(ctx, Req{Method: http.MethodPut, Path: path})
	if err != nil {
		var api *APIError
		if errors.As(err, &api) && api.Status == http.StatusNotFound {
			verb, preposition := "add", "to"
			if action == "unlink" {
				verb, preposition = "remove", "from"
			}
			return nil, fmt.Errorf("Cannot %s gear %s %s activity %d: gear not found (likely retired/removed)", verb, gearUUID, preposition, id)
		}
		return nil, err
	}
	return v, nil
}
