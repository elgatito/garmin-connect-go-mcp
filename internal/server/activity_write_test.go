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
	"strings"
	"testing"
)

func TestActivityID(t *testing.T) {
	for _, v := range []any{"../../123", float64(-1), float64(1.5)} {
		if _, err := activityID(v); err == nil {
			t.Fatalf("accepted %v", v)
		}
	}
	if id, err := activityID("123"); err != nil || id != 123 {
		t.Fatal(id, err)
	}
}

func TestActivityEffortWrite(t *testing.T) {
	var payload map[string]any
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.URL.Path != "/activity-service/activity/12" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
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
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "set_perceived_effort", Arguments: map[string]any{"email": "me@x", "activity_id": 12, "rpe": 7.5}})
	if err != nil || r.IsError {
		t.Fatal(r, err)
	}
	summary, _ := payload["summaryDTO"].(map[string]any)
	if summary["directWorkoutRpe"] != float64(75) {
		t.Fatal(payload)
	}
}
func TestUnknownActivityTypeDoesNotWrite(t *testing.T) {
	writes := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			writes++
		}
		fmt.Fprint(w, `[{"typeKey":"running","typeId":1}]`)
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
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "set_activity_type", Arguments: map[string]any{"email": "me@x", "activity_id": 12, "type_key": "wrong"}})
	if err != nil || r.IsError || writes != 0 || !strings.Contains(r.Content[0].(*mcp.TextContent).Text, "Unknown activity type") {
		t.Fatal(r, err, writes)
	}
}
func TestDeleteActivityRequiresConfirmation(t *testing.T) {
	deletes := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			deletes++
			w.WriteHeader(204)
			return
		}
		fmt.Fprint(w, `{"activityId":12,"activityName":"Run"}`)
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
	for _, confirm := range []bool{false, true} {
		r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "delete_activity", Arguments: map[string]any{"email": "me@x", "activity_id": 12, "confirm": confirm}})
		if err != nil || r.IsError {
			t.Fatal(r, err)
		}
		if !confirm && deletes != 0 {
			t.Fatal("deleted before confirmation")
		}
	}
	if deletes != 1 {
		t.Fatal(deletes)
	}
}
func TestGearPathRejectsTraversal(t *testing.T) {
	calls := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(204) }))
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
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "add_gear_to_activity", Arguments: map[string]any{"email": "me@x", "activity_id": 12, "gear_uuid": "../bad"}})
	if err != nil || !r.IsError || calls != 0 {
		t.Fatal(r, err, calls)
	}
}
func TestHydrationRejectsMismatchedDate(t *testing.T) {
	calls := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(204) }))
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
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "add_hydration_data", Arguments: map[string]any{"email": "me@x", "value_in_ml": 100, "cdate": "2026-01-01", "timestamp": "2026-01-02T01:00:00"}})
	if err != nil || !r.IsError || calls != 0 {
		t.Fatal(r, err, calls)
	}
}
