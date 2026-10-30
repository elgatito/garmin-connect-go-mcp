package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/elgatito/garmin-connect-go-mcp/internal/accounts"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHeartRateZoneValidationStopsWrite(t *testing.T) {
	var writes atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			writes.Add(1)
		}
		fmt.Fprint(w, `[{"sport":"DEFAULT","maxHeartRateUsed":180,"zone1Floor":80,"zone2Floor":100,"zone3Floor":120,"zone4Floor":140,"zone5Floor":160}]`)
	}))
	defer fake.Close()
	store := accounts.Store{Base: t.TempDir()}
	if err := store.SaveTokens("me@x", garmin.Tokens{DIToken: "token"}); err != nil {
		t.Fatal(err)
	}
	srv, err := New(Options{Store: store, Version: "test", Endpoints: func(string) garmin.Endpoints { return garmin.Endpoints{ConnectAPI: fake.URL} }})
	if err != nil {
		t.Fatal(err)
	}
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
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "set_heart_rate_zones", Arguments: map[string]any{"email": "me@x", "sport": "default", "max_hr": 100}})
	if err != nil || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "boundaries must not exceed max_hr") || writes.Load() != 0 {
		t.Fatal(result, err, writes.Load())
	}
}
