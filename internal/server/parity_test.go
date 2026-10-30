package server

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/elgatito/garmin-connect-go-mcp/internal/accounts"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestToolParity(t *testing.T) {
	var expected []string
	b, err := assets.ReadFile("tool_names.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &expected); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	srv, err := New(Options{Store: accounts.Store{Base: t.TempDir()}, Version: "test", Getenv: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
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
	actual := map[string]bool{}
	cursor := ""
	for {
		page, err := cs.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, tool := range page.Tools {
			actual[tool.Name] = true
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	missing := []string{}
	for _, name := range expected {
		if !actual[name] {
			missing = append(missing, name)
		}
		delete(actual, name)
	}
	extra := []string{}
	for name := range actual {
		extra = append(extra, name)
	}
	sort.Strings(extra)
	if len(expected) != 158 || len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("expected %d tools; missing %d: %v; extra: %v", len(expected), len(missing), missing, extra)
	}
}

func TestProfilesReferenceRealTools(t *testing.T) {
	var profiles map[string][]string
	b, err := assets.ReadFile("profiles.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &profiles); err != nil {
		t.Fatal(err)
	}
	b, err = assets.ReadFile("tool_names.json")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	if err = json.Unmarshal(b, &names); err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, name := range names {
		known[name] = true
	}
	for profile, tools := range profiles {
		for _, tool := range tools {
			if !known[tool] {
				t.Errorf("profile %s references unknown tool %s", profile, tool)
			}
		}
	}
}
func TestEveryToolAnnotated(t *testing.T) {
	ctx := context.Background()
	srv, err := New(Options{Store: accounts.Store{Base: t.TempDir()}, Version: "test", Getenv: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
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
	cursor := ""
	for {
		page, err := cs.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, tool := range page.Tools {
			if tool.Annotations == nil || tool.Annotations.DestructiveHint == nil || tool.Annotations.OpenWorldHint == nil {
				t.Errorf("missing annotation on %s", tool.Name)
			}
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
}

func TestEveryToolRequiresEmail(t *testing.T) {
	ctx := context.Background()
	srv, err := New(Options{Store: accounts.Store{Base: t.TempDir()}, Version: "test", Getenv: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
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
	cursor := ""
	for {
		page, err := cs.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, tool := range page.Tools {
			if tool.Name == "list_accounts" {
				continue
			}
			schema := asMap(tool.InputSchema)
			props := asMap(schema["properties"])
			email := asMap(props["email"])
			if email["type"] != "string" || email["description"] != emailDescription {
				t.Errorf("%s has incorrect email schema: %v", tool.Name, email)
			}
			required := false
			for _, v := range slice(schema["required"]) {
				if v == "email" {
					required = true
				}
			}
			if !required {
				t.Errorf("%s does not require email", tool.Name)
			}
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
}

func TestEmailIsFirstSchemaProperty(t *testing.T) {
	properties := orderedProperties{"activity_id": map[string]any{"type": "integer"}, "email": map[string]any{"type": "string"}}
	encoded, err := json.Marshal(properties)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(encoded), `{"email":`) {
		t.Fatal(string(encoded))
	}
}
