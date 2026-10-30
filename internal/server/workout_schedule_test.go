package server

import (
	"context"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/accounts"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestScheduledWorkout(t *testing.T) {
	out := scheduledWorkout(map[string]any{"scheduleDate": "2026-01-01", "associatedActivityId": float64(17), "isRestDay": true})
	if out["completed"] != true || out["activity_id"] != float64(17) || out["is_rest_day"] != true {
		t.Fatal(out)
	}
}
func TestScheduleWorkoutAvoidsDuplicate(t *testing.T) {
	writes := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql-gateway/graphql" {
			writes++
			w.WriteHeader(204)
			return
		}
		fmt.Fprint(w, `{"data":{"workoutScheduleSummariesScalar":[{"workoutId":42,"scheduleDate":"2026-01-01"}]}}`)
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
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "schedule_workout", Arguments: map[string]any{"email": "me@x", "workout_id": 42, "calendar_date": "2026-01-01"}})
	if err != nil || r.IsError || writes != 0 || !strings.Contains(r.Content[0].(*mcp.TextContent).Text, `"idempotent": true`) {
		t.Fatal(r, err, writes)
	}
}
func TestWorkoutDeletePreview(t *testing.T) {
	deletes := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			deletes++
			w.WriteHeader(204)
			return
		}
		fmt.Fprint(w, `{"workoutId":42,"workoutName":"Run"}`)
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
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "delete_workout", Arguments: map[string]any{"email": "me@x", "workout_id": 42}})
	if err != nil || r.IsError || deletes != 0 || !strings.Contains(r.Content[0].(*mcp.TextContent).Text, "needs_confirmation") {
		t.Fatal(r, err, deletes)
	}
}
func TestWorkoutBatchPreviewDoesNotDelete(t *testing.T) {
	deletes := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			deletes++
			w.WriteHeader(204)
			return
		}
		fmt.Fprint(w, `{"workoutName":"Run"}`)
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
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "delete_workouts", Arguments: map[string]any{"email": "me@x", "workout_ids": []int{1, 2}}})
	if err != nil || r.IsError || deletes != 0 || !strings.Contains(r.Content[0].(*mcp.TextContent).Text, "needs_confirmation") {
		t.Fatal(r, err, deletes)
	}
}
func TestPushWorkoutUsesDefaultDevice(t *testing.T) {
	posts := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/device-service/deviceservice/mylastused":
			fmt.Fprint(w, `{"userDeviceId":77}`)
		case "/workout-service/workout/42":
			fmt.Fprint(w, `{"workoutName":"Run"}`)
		case "/device-service/devicemessage/messages":
			posts++
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
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
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "push_workout_to_device", Arguments: map[string]any{"email": "me@x", "workout_id": 42}})
	if err != nil || r.IsError || posts != 1 || !strings.Contains(r.Content[0].(*mcp.TextContent).Text, `"device_id": 77`) {
		t.Fatal(r, err, posts)
	}
}
func TestWorkoutUploadRejectsConflictingTarget(t *testing.T) {
	writes := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writes++; w.WriteHeader(204) }))
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
	workout := map[string]any{"workoutSegments": []any{map[string]any{"workoutSteps": []any{map[string]any{"targetType": map[string]any{"workoutTargetTypeId": 2, "workoutTargetTypeKey": "power.between"}}}}}}
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "upload_workout", Arguments: map[string]any{"email": "me@x", "workout_data": workout}})
	if err != nil || !r.IsError || writes != 0 {
		t.Fatal(r, err, writes)
	}
}
func TestScheduleBatchRejectsMissingDate(t *testing.T) {
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
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "schedule_workouts", Arguments: map[string]any{"email": "me@x", "schedules": []any{map[string]any{"workout_id": 42}}}})
	if err != nil || r.IsError || calls != 0 || !strings.Contains(r.Content[0].(*mcp.TextContent).Text, `"failed": 1`) {
		t.Fatal(r, err, calls)
	}
}
func TestScheduleWeekMixedEntries(t *testing.T) {
	posts := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql-gateway/graphql" {
			fmt.Fprint(w, `{"data":{"workoutScheduleSummariesScalar":[]}}`)
			return
		}
		posts++
		fmt.Fprint(w, `{"workoutScheduleId":55}`)
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
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "schedule_week", Arguments: map[string]any{"email": "me@x", "week": []any{map[string]any{"date": "bad", "workout_id": 1}, map[string]any{"date": "2026-01-01", "workout_id": 2}}}})
	if err != nil || r.IsError || posts != 1 || !strings.Contains(r.Content[0].(*mcp.TextContent).Text, `"scheduled_workout_id": 55`) {
		t.Fatal(r, err, posts)
	}
}
