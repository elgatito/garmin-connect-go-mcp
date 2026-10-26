package garmin

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

const (
	nutritionServicePath      = "/nutrition-service"
	nutritionFoodLogsPath     = nutritionServicePath + "/food/logs"
	nutritionFoodSearchPath   = nutritionServicePath + "/food/search"
	nutritionMealsPath        = nutritionServicePath + "/meals"
	nutritionSettingsPath     = nutritionServicePath + "/settings"
	nutritionCustomFoodPath   = nutritionServicePath + "/customFood"
	nutritionServingUnitsPath = nutritionServicePath + "/metadata/customFoodServingUnits"
)

// GetNutritionFoodLog returns the per-item food log for a date.
func (c *Client) GetNutritionFoodLog(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "date")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, nutritionFoodLogsPath+"/"+d, nil)
}

// GetNutritionSummary returns per-day nutrition totals for a date range.
func (c *Client) GetNutritionSummary(ctx context.Context, startDate, endDate string) (any, error) {
	return c.GetJSON(ctx, nutritionFoodLogsPath+"/range", url.Values{
		"startDate": {startDate},
		"endDate":   {endDate},
	})
}

// GetNutritionMeals returns the meal windows and totals for a date.
func (c *Client) GetNutritionMeals(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "date")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, nutritionMealsPath+"/"+d, nil)
}

// GetNutritionSettings returns the nutrition plan for a date.
func (c *Client) GetNutritionSettings(ctx context.Context, date string) (any, error) {
	d, err := ValidateDate(date, "date")
	if err != nil {
		return nil, err
	}
	return c.GetJSON(ctx, nutritionSettingsPath+"/"+d, nil)
}

// PutNutritionSettings replaces the nutrition plan for a date.
func (c *Client) PutNutritionSettings(ctx context.Context, date string, settings any) (any, error) {
	return c.DoJSON(ctx, Req{
		Method: http.MethodPut,
		Path:   nutritionSettingsPath + "/" + date,
		JSON:   settings,
	})
}

// SearchFoods searches Garmin's general food catalog.
func (c *Client) SearchFoods(ctx context.Context, query string, start, limit int) (any, error) {
	return c.GetJSON(ctx, nutritionFoodSearchPath, url.Values{
		"searchExpression": {query},
		"start":            {strconv.Itoa(start)},
		"limit":            {strconv.Itoa(limit)},
	})
}

// GetCustomFoods returns one page of the user's custom food library.
func (c *Client) GetCustomFoods(ctx context.Context, search string, start, limit int) (any, error) {
	return c.GetJSON(ctx, nutritionCustomFoodPath, url.Values{
		"searchExpression": {search},
		"start":            {strconv.Itoa(start)},
		"limit":            {strconv.Itoa(limit)},
		"includeContent":   {"true"},
	})
}

// GetCustomFoodServingUnits returns the valid serving units for custom foods.
func (c *Client) GetCustomFoodServingUnits(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, nutritionServingUnitsPath, nil)
}

// PutCustomFood creates or replaces a custom food record.
func (c *Client) PutCustomFood(ctx context.Context, payload any) (any, error) {
	return c.DoJSON(ctx, Req{Method: http.MethodPut, Path: nutritionCustomFoodPath, JSON: payload})
}

// DeleteCustomFood permanently deletes a custom food.
func (c *Client) DeleteCustomFood(ctx context.Context, foodID string) (any, error) {
	return c.DoJSON(ctx, Req{Method: http.MethodDelete, Path: nutritionCustomFoodPath + "/" + foodID})
}

// LogFood writes regular food log items.
func (c *Client) LogFood(ctx context.Context, payload any) (any, error) {
	return c.DoJSON(ctx, Req{Method: http.MethodPut, Path: nutritionFoodLogsPath, JSON: payload})
}

// LogQuickFood writes a Quick Add food entry.
func (c *Client) LogQuickFood(ctx context.Context, payload any) (any, error) {
	return c.DoJSON(ctx, Req{Method: http.MethodPut, Path: nutritionFoodLogsPath + "/quickAdd", JSON: payload})
}

// DeleteFoodLog deletes one log entry from a day's food log.
func (c *Client) DeleteFoodLog(ctx context.Context, mealDate, logID string) (any, error) {
	d, err := ValidateDate(mealDate, "meal_date")
	if err != nil {
		return nil, err
	}
	return c.DoJSON(ctx, Req{
		Method: http.MethodDelete,
		Path:   nutritionFoodLogsPath + "/" + d,
		JSON:   map[string]any{"logIds": []any{logID}},
	})
}
