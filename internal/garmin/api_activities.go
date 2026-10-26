package garmin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Port of the python-garminconnect activity and gear methods (GC/__init__.py).

const (
	activitiesSearchPath = "/activitylist-service/activities/search/activities"
	activitiesCountPath  = "/activitylist-service/activities/count"
	activityForDatePath  = "/mobile-gateway/heartRate/forDate"
	activityPath         = "/activity-service/activity"
	activityTypesPath    = "/activity-service/activity/activityTypes"
	gearFilterPath       = "/gear-service/gear/filterGear"
	gearBasePath         = "/gear-service/gear"
	deviceLastUsedPath   = "/device-service/deviceservice/mylastused"
)

// pathSeg escapes a caller-supplied string for use as ONE path segment ("/", "?", "#" and ".." cannot
// change the request's path). Use it for every free-text value interpolated into an API path.
func pathSeg(s string) string {
	if s == "." || s == ".." { // dot segments are resolved by servers even when nothing else is special
		return strings.ReplaceAll(s, ".", "%2E")
	}
	return url.PathEscape(s)
}

// validActivityID is str(_validate_positive_integer(int(activity_id), "activity_id")).
func validActivityID(id int64) (string, error) {
	if id <= 0 {
		return "", fmt.Errorf("activity_id must be a positive integer, got: %d", id)
	}
	return strconv.FormatInt(id, 10), nil
}

// getActivityPart GETs /activity-service/activity/<id><suffix> after validating the id.
func (c *Client) getActivityPart(ctx context.Context, id int64, suffix string, q url.Values) (any, error) {
	s, err := validActivityID(id)
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, activityPath+"/"+s+suffix, q)
}

func (c *Client) GetActivity(ctx context.Context, id int64) (any, error) {
	return c.getActivityPart(ctx, id, "", nil)
}
func (c *Client) GetActivitySplits(ctx context.Context, id int64) (any, error) {
	return c.getActivityPart(ctx, id, "/splits", nil)
}
func (c *Client) GetActivityTypedSplits(ctx context.Context, id int64) (any, error) {
	return c.getActivityPart(ctx, id, "/typedsplits", nil)
}
func (c *Client) GetActivitySplitSummaries(ctx context.Context, id int64) (any, error) {
	return c.getActivityPart(ctx, id, "/split_summaries", nil)
}
func (c *Client) GetActivityWeather(ctx context.Context, id int64) (any, error) {
	return c.getActivityPart(ctx, id, "/weather", nil)
}
func (c *Client) GetActivityHRInTimezones(ctx context.Context, id int64) (any, error) {
	return c.getActivityPart(ctx, id, "/hrTimeInZones", nil)
}
func (c *Client) GetActivityPowerInTimezones(ctx context.Context, id int64) (any, error) {
	return c.getActivityPart(ctx, id, "/powerTimeInZones", nil)
}
func (c *Client) GetActivityExerciseSets(ctx context.Context, id int64) (any, error) {
	return c.getActivityPart(ctx, id, "/exerciseSets", nil)
}

// GetActivityGear returns the gear used for an activity.
func (c *Client) GetActivityGear(ctx context.Context, id int64) (any, error) {
	s, err := validActivityID(id)
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, gearFilterPath, url.Values{"activityId": {s}})
}

// GetActivitiesForDate is get_activities_fordate.
func (c *Client) GetActivitiesForDate(ctx context.Context, fordate string) (any, error) {
	return c.getByDate(ctx, activityForDatePath, "fordate", fordate)
}

// CountActivities returns the account's total activity count (the totalCount value).
func (c *Client) CountActivities(ctx context.Context) (any, error) {
	v, err := c.GetJSON(ctx, activitiesCountPath, nil)
	if err != nil {
		return nil, err
	}
	m, _ := v.(map[string]any)
	if _, ok := m["totalCount"]; !ok || len(m) == 0 {
		return nil, errors.New("No activities count data received")
	}
	return m["totalCount"], nil
}

// SearchActivities is the date-ranged activity search (connectapi on garmin_connect_activities); the tool pages it itself
// because get_activities_by_date in the library fetches every page.
func (c *Client) SearchActivities(ctx context.Context, startDate, endDate, activityType string, start, limit int) (any, error) {
	q := url.Values{"startDate": {startDate}, "endDate": {endDate}, "start": {strconv.Itoa(start)}, "limit": {strconv.Itoa(limit)}}
	if activityType != "" {
		q.Set("activityType", activityType)
	}
	return c.GetJSON(ctx, activitiesSearchPath, q)
}

// GetActivityEventTypes lists the event types an activity can be tagged with.
func (c *Client) GetActivityEventTypes(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, activityPath+"/eventTypes", nil)
}

// GetGearV2List is Garmin's v2 gear list, the only one that carries notes.
func (c *Client) GetGearV2List(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, gearBasePath+"/v2/list", nil)
}

func (c *Client) GetActivityTypes(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, activityTypesPath, nil)
}

// UpdateActivity PUTs a partial ActivityDTO keyed by activityId (set_activity_name and the tool-level updates).
func (c *Client) UpdateActivity(ctx context.Context, id int64, fields map[string]any) (any, error) {
	body := map[string]any{"activityId": id}
	for k, v := range fields {
		body[k] = v
	}
	return c.DoJSON(ctx, Req{Method: http.MethodPut, Path: activityPath + "/" + strconv.FormatInt(id, 10), JSON: body})
}

func (c *Client) SetActivityName(ctx context.Context, id int64, title string) (any, error) {
	return c.UpdateActivity(ctx, id, map[string]any{"activityName": title})
}

func (c *Client) SetActivityType(ctx context.Context, id int64, typeID any, typeKey string, parentTypeID any) (any, error) {
	return c.UpdateActivity(ctx, id, map[string]any{"activityTypeDTO": map[string]any{
		"typeId": typeID, "typeKey": typeKey, "parentTypeId": parentTypeID,
	}})
}

// SetActivityExerciseSets replaces an activity's exercise sets.
func (c *Client) SetActivityExerciseSets(ctx context.Context, id int64, sets any) (any, error) {
	return c.DoJSON(ctx, Req{
		Method: http.MethodPut, Path: activityPath + "/" + strconv.FormatInt(id, 10) + "/exerciseSets",
		JSON: map[string]any{"activityId": id, "exerciseSets": sets},
	})
}

// CreateManualActivity creates a private activity from a few basic parameters.
func (c *Client) CreateManualActivity(ctx context.Context, startDatetime, timeZone, typeKey string, distanceKm float64, durationMin int, name string) (any, error) {
	return c.DoJSON(ctx, Req{Method: http.MethodPost, Path: activityPath, JSON: map[string]any{
		"activityTypeDTO":      map[string]any{"typeKey": typeKey},
		"accessControlRuleDTO": map[string]any{"typeId": 2, "typeKey": "private"},
		"timeZoneUnitDTO":      map[string]any{"unitKey": timeZone},
		"activityName":         name,
		"metadataDTO":          map[string]any{"autoCalcCalories": true},
		"summaryDTO":           map[string]any{"startTimeLocal": startDatetime, "distance": distanceKm * 1000, "duration": durationMin * 60},
	}})
}

func (c *Client) DeleteActivity(ctx context.Context, id int64) error {
	s, err := validActivityID(id)
	if err != nil {
		return err
	}
	_, err = c.Do(ctx, Req{Method: http.MethodDelete, Path: activityPath + "/" + s})
	return err
}

// GetDeviceLastUsed returns the last-used device (its userProfileNumber identifies the account for gear).
func (c *Client) GetDeviceLastUsed(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, deviceLastUsedPath, nil)
}

func (c *Client) GetGear(ctx context.Context, userProfileNumber string) (any, error) {
	return c.GetJSON(ctx, gearFilterPath, url.Values{"userProfilePk": {userProfileNumber}})
}

// GetGearStats returns {} for gear Garmin no longer knows (404).
func (c *Client) GetGearStats(ctx context.Context, gearUUID string) (any, error) {
	v, err := c.GetJSON(ctx, gearBasePath+"/stats/"+pathSeg(gearUUID), nil)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		return map[string]any{}, nil
	}
	return v, err
}

func (c *Client) GetGearDefaults(ctx context.Context, userProfileNumber string) (any, error) {
	return c.GetJSON(ctx, gearBasePath+"/user/"+pathSeg(userProfileNumber)+"/activityTypes", nil)
}

// linkGear PUTs link/unlink; a 404 becomes GC's "gear not found" error.
func (c *Client) linkGear(ctx context.Context, verb, preposition, gearUUID string, id int64) (any, error) {
	s, err := validActivityID(id)
	if err != nil {
		return nil, err
	}
	v, err := c.DoJSON(ctx, Req{Method: http.MethodPut, Path: gearBasePath + "/" + verb + "/" + pathSeg(gearUUID) + "/activity/" + s})
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		return nil, fmt.Errorf("Cannot %s gear %s %s activity %s: gear not found (likely retired/removed)",
			map[string]string{"link": "add", "unlink": "remove"}[verb], gearUUID, preposition, s)
	}
	return v, err
}

func (c *Client) AddGearToActivity(ctx context.Context, gearUUID string, id int64) (any, error) {
	return c.linkGear(ctx, "link", "to", gearUUID, id)
}

func (c *Client) RemoveGearFromActivity(ctx context.Context, gearUUID string, id int64) (any, error) {
	return c.linkGear(ctx, "unlink", "from", gearUUID, id)
}
