package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/accounts"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLogFoodSelectsMeal(t *testing.T) {
	var payload map[string]any
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			fmt.Fprint(w, `{"meals":[{"mealId":5,"startTime":"12:00:00","endTime":"14:00:00"}]}`)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		w.WriteHeader(204)
	}))
	defer fake.Close()
	store := accounts.Store{Base: t.TempDir()}
	if err := store.SaveTokens("me@x", garmin.Tokens{DIToken: "token"}); err != nil {
		t.Fatal(err)
	}
	srv, _ := New(Options{Store: store, Version: "test", Endpoints: func(string) garmin.Endpoints { return garmin.Endpoints{ConnectAPI: fake.URL} }})
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "log_food", Arguments: map[string]any{"email": "me@x", "meal_date": "2026-01-01", "meal_time": "12:30:00", "name": "Oats", "calories": 100, "carbs": 20, "protein": 4, "fat": 2}})
	if err != nil || r.IsError {
		t.Fatal(r, err)
	}
	items := payload["quickAddItems"].([]any)
	entry := items[0].(map[string]any)
	if entry["mealId"] != float64(5) || entry["calories"] != "100" {
		t.Fatal(entry)
	}
}
func TestUpdateFoodPreservesOmittedFields(t *testing.T) {
	var payload map[string]any
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			fmt.Fprint(w, `{"customFoods":[{"foodMetaData":{"foodId":"abc","brandName":"Brand"},"nutritionContents":[{"servingUnit":"G","numberOfUnits":50,"protein":7}]}]}`)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		w.WriteHeader(204)
	}))
	defer fake.Close()
	store := accounts.Store{Base: t.TempDir()}
	if err := store.SaveTokens("me@x", garmin.Tokens{DIToken: "token"}); err != nil {
		t.Fatal(err)
	}
	srv, _ := New(Options{Store: store, Version: "test", Endpoints: func(string) garmin.Endpoints { return garmin.Endpoints{ConnectAPI: fake.URL} }})
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "update_custom_food", Arguments: map[string]any{"email": "me@x", "food_id": "abc", "serving_id": "s", "food_name": "Renamed", "calories": 100}})
	if err != nil || r.IsError {
		t.Fatal(r, err)
	}
	meta := payload["foodMetaData"].(map[string]any)
	nutrition := payload["nutritionContents"].([]any)[0].(map[string]any)
	if meta["brandName"] != "Brand" || nutrition["protein"] != "7" || nutrition["numberOfUnits"] != "50" {
		t.Fatal(payload)
	}
}
func TestUpsertDoesNotDuplicateUnusableFood(t *testing.T) {
	writes := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes++
			w.WriteHeader(204)
			return
		}
		fmt.Fprint(w, `{"customFoods":[{"foodMetaData":{"foodName":"Oats","foodId":"id"},"nutritionContents":[]}]}`)
	}))
	defer fake.Close()
	store := accounts.Store{Base: t.TempDir()}
	if err := store.SaveTokens("me@x", garmin.Tokens{DIToken: "token"}); err != nil {
		t.Fatal(err)
	}
	srv, _ := New(Options{Store: store, Version: "test", Endpoints: func(string) garmin.Endpoints { return garmin.Endpoints{ConnectAPI: fake.URL} }})
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "upsert_and_log", Arguments: map[string]any{"email": "me@x", "meal_date": "2026-01-01", "meal_time": "12:00:00", "food_name": "Oats", "calories": 100}})
	if err != nil || !r.IsError || writes != 0 {
		t.Fatal(r, err, writes)
	}
}
func TestWeightDeleteRequiresConfirmation(t *testing.T) {
	deletes := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			deletes++
			w.WriteHeader(204)
			return
		}
		fmt.Fprint(w, `{"dateWeightList":[{"samplePk":123,"weight":70000}]}`)
	}))
	defer fake.Close()
	store := accounts.Store{Base: t.TempDir()}
	if err := store.SaveTokens("me@x", garmin.Tokens{DIToken: "token"}); err != nil {
		t.Fatal(err)
	}
	srv, _ := New(Options{Store: store, Version: "test", Endpoints: func(string) garmin.Endpoints { return garmin.Endpoints{ConnectAPI: fake.URL} }})
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "delete_weigh_ins", Arguments: map[string]any{"email": "me@x", "date": "2026-01-01"}})
	if err != nil || r.IsError || deletes != 0 {
		t.Fatal(r, err, deletes)
	}
}
