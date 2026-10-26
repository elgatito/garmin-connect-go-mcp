package garmin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// Nutrition endpoints. python-garminconnect has no nutrition methods: garmin_mcp/nutrition.py calls these
// paths directly through connectapi / client.put / client.delete, so path, method and body are ported from there.

const nutritionPath = "/nutrition-service"

// nutritionDayPath is <kind>/<date> under the nutrition service. The date is free text from the caller
// (nutrition.py validates it only for logging), so it is escaped as one path segment.
func nutritionDayPath(kind, date string) string {
	return nutritionPath + "/" + kind + "/" + pathSeg(date)
}

func customFoodPath(foodID string) string { return nutritionPath + "/customFood/" + pathSeg(foodID) }

// nutritionDo is connectapi / client.<verb>(api=True): the body must be JSON (an empty 200 body is Python's
// JSONDecodeError); a 204 arrives as {}.
func (c *Client) nutritionDo(ctx context.Context, r Req) (any, error) {
	b, err := c.Do(ctx, r)
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("invalid JSON from %s: %w", r.Path, err)
	}
	return v, nil
}

func (c *Client) nutritionGet(ctx context.Context, path string, q url.Values) (any, error) {
	return c.nutritionDo(ctx, Req{Method: http.MethodGet, Path: path, Query: q})
}

func (c *Client) GetFoodLog(ctx context.Context, date string) (any, error) {
	return c.nutritionGet(ctx, nutritionDayPath("food/logs", date), nil)
}

func (c *Client) GetFoodLogRange(ctx context.Context, start, end string) (any, error) {
	return c.nutritionGet(ctx, nutritionPath+"/food/logs/range", url.Values{"startDate": {start}, "endDate": {end}})
}

func (c *Client) GetMeals(ctx context.Context, date string) (any, error) {
	return c.nutritionGet(ctx, nutritionDayPath("meals", date), nil)
}

func (c *Client) GetNutritionSettings(ctx context.Context, date string) (any, error) {
	return c.nutritionGet(ctx, nutritionDayPath("settings", date), nil)
}

// PutNutritionSettings is the full-object PUT of the day's settings.
func (c *Client) PutNutritionSettings(ctx context.Context, date string, settings any) (any, error) {
	return c.nutritionDo(ctx, Req{Method: http.MethodPut, Path: nutritionDayPath("settings", date), JSON: settings})
}

func (c *Client) SearchFoods(ctx context.Context, query string, start, limit int) (any, error) {
	return c.nutritionGet(ctx, nutritionPath+"/food/search", url.Values{
		"searchExpression": {query}, "start": {strconv.Itoa(start)}, "limit": {strconv.Itoa(limit)}})
}

func (c *Client) GetCustomFoods(ctx context.Context, search string, start, limit int) (any, error) {
	return c.nutritionGet(ctx, nutritionPath+"/customFood", url.Values{
		"searchExpression": {search}, "start": {strconv.Itoa(start)}, "limit": {strconv.Itoa(limit)}, "includeContent": {"true"}})
}

func (c *Client) GetCustomFoodServingUnits(ctx context.Context) (any, error) {
	return c.nutritionGet(ctx, nutritionPath+"/metadata/customFoodServingUnits", nil)
}

// PutCustomFood creates (no foodId in the payload) or fully replaces a custom food.
func (c *Client) PutCustomFood(ctx context.Context, payload any) (any, error) {
	return c.nutritionDo(ctx, Req{Method: http.MethodPut, Path: nutritionPath + "/customFood", JSON: payload})
}

func (c *Client) DeleteCustomFood(ctx context.Context, foodID string) (any, error) {
	return c.nutritionDo(ctx, Req{Method: http.MethodDelete, Path: customFoodPath(foodID)})
}

// PutFoodLogs logs food items (foodLogItems) to a meal.
func (c *Client) PutFoodLogs(ctx context.Context, payload any) (any, error) {
	return c.nutritionDo(ctx, Req{Method: http.MethodPut, Path: nutritionPath + "/food/logs", JSON: payload})
}

// PutQuickAdd logs quick-add items (name plus macros).
func (c *Client) PutQuickAdd(ctx context.Context, payload any) (any, error) {
	return c.nutritionDo(ctx, Req{Method: http.MethodPut, Path: nutritionPath + "/food/logs/quickAdd", JSON: payload})
}

// DeleteFoodLog removes one log entry; the id travels in the DELETE body.
func (c *Client) DeleteFoodLog(ctx context.Context, date, logID string) (any, error) {
	return c.nutritionDo(ctx, Req{Method: http.MethodDelete, Path: nutritionDayPath("food/logs", date), JSON: map[string]any{"logIds": []string{logID}}})
}
