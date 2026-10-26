package garmin

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type nutritionCall struct {
	method, uri, body string
}

func nutritionClient(t *testing.T, body string) (*Client, *nutritionCall) {
	t.Helper()
	got := &nutritionCall{}
	c, _ := fixture(t, freshTokens("c1"), func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*got = nutritionCall{r.Method, r.RequestURI, string(b)}
		w.Write([]byte(body))
	}, nil)
	return c, got
}

func TestNutritionRequests(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name         string
		call         func(*Client) error
		method, want string
		body         string
	}{
		{"food log", func(c *Client) error { _, err := c.GetFoodLog(ctx, "2024-01-15"); return err }, "GET", "/nutrition-service/food/logs/2024-01-15", ""},
		{"range", func(c *Client) error { _, err := c.GetFoodLogRange(ctx, "2024-01-14", "2024-01-15"); return err }, "GET", "/nutrition-service/food/logs/range?endDate=2024-01-15&startDate=2024-01-14", ""},
		{"meals", func(c *Client) error { _, err := c.GetMeals(ctx, "2024-01-15"); return err }, "GET", "/nutrition-service/meals/2024-01-15", ""},
		{"settings", func(c *Client) error { _, err := c.GetNutritionSettings(ctx, "2024-01-15"); return err }, "GET", "/nutrition-service/settings/2024-01-15", ""},
		{"put settings", func(c *Client) error {
			_, err := c.PutNutritionSettings(ctx, "2024-01-15", map[string]any{"calorieGoal": 1})
			return err
		}, "PUT", "/nutrition-service/settings/2024-01-15", `{"calorieGoal":1}`},
		{"search", func(c *Client) error { _, err := c.SearchFoods(ctx, "a & b/é", 2, 5); return err }, "GET", "/nutrition-service/food/search?limit=5&searchExpression=a+%26+b%2F%C3%A9&start=2", ""},
		{"custom foods", func(c *Client) error { _, err := c.GetCustomFoods(ctx, "x", 0, 20); return err }, "GET", "/nutrition-service/customFood?includeContent=true&limit=20&searchExpression=x&start=0", ""},
		{"units", func(c *Client) error { _, err := c.GetCustomFoodServingUnits(ctx); return err }, "GET", "/nutrition-service/metadata/customFoodServingUnits", ""},
		{"put custom", func(c *Client) error { _, err := c.PutCustomFood(ctx, map[string]any{"a": 1}); return err }, "PUT", "/nutrition-service/customFood", `{"a":1}`},
		{"delete custom", func(c *Client) error { _, err := c.DeleteCustomFood(ctx, "abc"); return err }, "DELETE", "/nutrition-service/customFood/abc", ""},
		{"put logs", func(c *Client) error { _, err := c.PutFoodLogs(ctx, map[string]any{"a": 1}); return err }, "PUT", "/nutrition-service/food/logs", `{"a":1}`},
		{"quick add", func(c *Client) error { _, err := c.PutQuickAdd(ctx, map[string]any{"a": 1}); return err }, "PUT", "/nutrition-service/food/logs/quickAdd", `{"a":1}`},
		{"delete log", func(c *Client) error { _, err := c.DeleteFoodLog(ctx, "2024-01-15", "L1"); return err }, "DELETE", "/nutrition-service/food/logs/2024-01-15", `{"logIds":["L1"]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, got := nutritionClient(t, `{}`)
			if err := tc.call(c); err != nil {
				t.Fatal(err)
			}
			if got.method != tc.method || got.uri != tc.want || got.body != tc.body {
				t.Errorf("got %+v, want %s %s %s", *got, tc.method, tc.want, tc.body)
			}
		})
	}
}

// Python's .json() raises on an empty 200 body; a 204 is {} (client.py).
func TestNutritionEmptyBodyIsAParseError(t *testing.T) {
	c, _ := nutritionClient(t, ``)
	if _, err := c.PutFoodLogs(context.Background(), map[string]any{}); err == nil {
		t.Error("empty 200 body accepted")
	}
}

func TestNutritionFreeTextCannotEscapeItsPathSegment(t *testing.T) {
	const evil = "x/../../foo?a=b#frag"
	const seg = "x%2F..%2F..%2Ffoo%3Fa=b%23frag"
	ctx := context.Background()
	for prefix, call := range map[string]func(*Client) error{
		"/nutrition-service/food/logs/":  func(c *Client) error { _, err := c.GetFoodLog(ctx, evil); return err },
		"/nutrition-service/meals/":      func(c *Client) error { _, err := c.GetMeals(ctx, evil); return err },
		"/nutrition-service/settings/":   func(c *Client) error { _, err := c.GetNutritionSettings(ctx, evil); return err },
		"/nutrition-service/customFood/": func(c *Client) error { _, err := c.DeleteCustomFood(ctx, evil); return err },
		"/nutrition-service/food/logs/ ": func(c *Client) error { _, err := c.DeleteFoodLog(ctx, evil, "L"); return err },
		"/nutrition-service/settings/ ":  func(c *Client) error { _, err := c.PutNutritionSettings(ctx, evil, map[string]any{}); return err },
	} {
		prefix = strings.TrimSpace(prefix)
		c, got := nutritionClient(t, `{}`)
		if err := call(c); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(got.uri, prefix+seg) || strings.Contains(got.uri, "#") || strings.Count(got.uri, "?") != 0 {
			t.Errorf("%s: request URI %q escapes its segment", prefix, got.uri)
		}
	}
}
