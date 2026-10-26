package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/accounts"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// rawBody is a fake route body written verbatim.
type rawBody []byte

// respStatus is a fake route response with an explicit HTTP status.
type respStatus struct {
	code int
	body any
}

// sequence returns one body per request, in order.
type sequence []any

// delay waits before writing body.
type delay struct {
	wait time.Duration
	body any
}

type recordedCall struct {
	Method string
	Path   string
	Query  url.Values
	Body   []byte
	Header http.Header
}

type fakeGarmin struct {
	*httptest.Server

	mu     sync.Mutex
	routes map[string]any
	calls  []recordedCall
}

func newFakeGarmin(t *testing.T) *fakeGarmin {
	t.Helper()
	f := &fakeGarmin{routes: map[string]any{}}
	f.on("GET /userprofile-service/socialProfile", map[string]any{
		"displayName": "testuser",
		"fullName":    "Test User",
	})
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

// on registers a route keyed by "METHOD /path"; the query string is ignored.
func (f *fakeGarmin) on(methodAndPath string, body any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[methodAndPath] = body
}

func (f *fakeGarmin) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	key := r.Method + " " + r.URL.Path

	f.mu.Lock()
	f.calls = append(f.calls, recordedCall{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.Query(),
		Body:   body,
		Header: r.Header.Clone(),
	})
	route, ok := f.routes[key]
	if seq, isSeq := route.(sequence); isSeq && len(seq) > 0 {
		route = seq[0]
		if len(seq) == 1 {
			delete(f.routes, key)
		} else {
			f.routes[key] = seq[1:]
		}
	}
	f.mu.Unlock()

	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"no route"}`))
		return
	}
	if d, isDelay := route.(delay); isDelay {
		time.Sleep(d.wait)
		route = d.body
	}
	w.Header().Set("Content-Type", "application/json")
	switch v := route.(type) {
	case respStatus:
		w.WriteHeader(v.code)
		if v.body != nil {
			_ = json.NewEncoder(w).Encode(v.body)
		}
	case rawBody:
		_, _ = w.Write(v)
	case nil:
	default:
		_ = json.NewEncoder(w).Encode(v)
	}
}

func (f *fakeGarmin) recorded() []recordedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedCall(nil), f.calls...)
}

func (f *fakeGarmin) callsMatching(method, path string) []recordedCall {
	var out []recordedCall
	for _, call := range f.recorded() {
		if call.Method == method && call.Path == path {
			out = append(out, call)
		}
	}
	return out
}

// testJWT returns a JWT that expires in one hour.
func testJWT(extra map[string]any) string {
	claims := map[string]any{
		"exp":       time.Now().Add(time.Hour).Unix(),
		"client_id": "test-client",
	}
	for k, v := range extra {
		claims[k] = v
	}
	return encodeJWT(claims)
}

// expiringJWT returns a JWT inside the 15-minute refresh window.
func expiringJWT() string {
	return encodeJWT(map[string]any{
		"exp":       time.Now().Add(time.Minute).Unix(),
		"client_id": "test-client",
	})
}

func encodeJWT(claims map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, _ := json.Marshal(claims)
	body := base64.RawURLEncoding.EncodeToString(payload)
	return header + "." + body + ".sig"
}

func newTestStore(t *testing.T) accounts.Store {
	t.Helper()
	return accounts.Store{Base: t.TempDir()}
}

func seedAccount(t *testing.T, st accounts.Store, email, token string) {
	t.Helper()
	if err := st.SaveTokens(email, garmin.Tokens{
		DIToken:        token,
		DIRefreshToken: "refresh-" + email,
		DIClientID:     "test-client",
	}); err != nil {
		t.Fatalf("seed %s: %v", email, err)
	}
}

func newSession(t *testing.T, f *fakeGarmin, env map[string]string) *mcp.ClientSession {
	t.Helper()
	st := newTestStore(t)
	seedAccount(t, st, "me@x.com", testJWT(nil))
	return newSessionWithStore(t, f, env, st)
}

func newSessionWithStore(t *testing.T, f *fakeGarmin, env map[string]string, st accounts.Store) *mcp.ClientSession {
	t.Helper()
	getenv := func(k string) string { return env[k] }
	srv, err := New(Options{
		Store: st,
		Endpoints: func(string) garmin.Endpoints {
			return garmin.Endpoints{
				SSO:        f.URL,
				Connect:    f.URL,
				ConnectAPI: f.URL,
				DIToken:    f.URL + "/diauth",
			}
		},
		Getenv:  getenv,
		Version: "test",
		Log:     io.Discard,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx, serverTransport) }()

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil).
		Connect(ctx, clientTransport, nil)
	if err != nil {
		cancel()
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		_ = cs.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	})
	return cs
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (text string, isErr bool) {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	if name != "list_accounts" {
		if _, ok := args["email"]; !ok {
			args["email"] = "me@x.com"
		}
	}
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String(), res.IsError
}

func fixture(t *testing.T, name string) any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "fixtures.json"))
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	var all map[string]any
	if err := json.Unmarshal(b, &all); err != nil {
		t.Fatalf("decode fixtures: %v", err)
	}
	v, ok := all[name]
	if !ok {
		t.Fatalf("fixture %q not found", name)
	}
	return v
}

func listToolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func callToolNames(t *testing.T, cs *mcp.ClientSession) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	for _, name := range listToolNames(t, cs) {
		names[name] = true
	}
	return names
}

func decodeJSON(t *testing.T, text string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(text), v); err != nil {
		t.Fatalf("decode %q: %v", text, err)
	}
}

// sameJSON compares two values by their canonical JSON encoding.
func sameJSON(t *testing.T, got, want any) {
	t.Helper()
	gb, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal got: %v", err)
	}
	wb, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal want: %v", err)
	}
	if string(gb) != string(wb) {
		t.Fatalf("JSON mismatch\ngot:  %s\nwant: %s", gb, wb)
	}
}

func sortedNames(names map[string]bool) []string {
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sortStrings(out)
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func assertNames(t *testing.T, got map[string]bool, want []string) {
	t.Helper()
	wantSet := map[string]bool{}
	for _, n := range want {
		wantSet[n] = true
	}
	gotList := sortedNames(got)
	wantList := sortedNames(wantSet)
	if strings.Join(gotList, ",") != strings.Join(wantList, ",") {
		t.Fatalf("tool set mismatch\ngot:  %v\nwant: %v", gotList, wantList)
	}
}
