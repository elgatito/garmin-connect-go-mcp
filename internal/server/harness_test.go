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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/elgatito/garmin-connect-go-mcp/internal/accounts"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

const testEmail = "me@x.com"

// rawBody is sent as-is with status 200.
type rawBody []byte

type statusBody struct {
	code int
	body any
}

// status answers with code and body (any route body form).
func status(code int, body any) statusBody { return statusBody{code, body} }

// sequenceBody answers successive calls with successive bodies, repeating the last one.
type sequenceBody struct {
	mu     sync.Mutex
	bodies []any
}

func sequence(bodies ...any) *sequenceBody { return &sequenceBody{bodies: bodies} }

type recordedCall struct {
	Method, Path string
	Query        url.Values
	Body         []byte
	Header       http.Header
}

// fakeGarmin is an httptest Garmin Connect API. Routes are keyed "METHOD /path" (no query).
type fakeGarmin struct {
	*httptest.Server
	mu     sync.Mutex
	routes map[string]any
	calls  []recordedCall
}

func newFakeGarmin(t *testing.T) *fakeGarmin {
	t.Helper()
	f := &fakeGarmin{routes: map[string]any{
		"GET /userprofile-service/socialProfile":             map[string]any{"displayName": "testuser", "fullName": "Test User"},
		"GET /userprofile-service/userprofile/user-settings": map[string]any{"userData": map[string]any{"measurementSystem": "metric"}},
	}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

// on sets the response for "METHOD /path". body is a JSON-marshalable value, rawBody,
// status(code, body), sequence(...), or an http.HandlerFunc.
func (f *fakeGarmin) on(methodAndPath string, body any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[methodAndPath] = body
}

// requests returns the recorded calls to methodAndPath, or every call when it is "".
func (f *fakeGarmin) requests(methodAndPath string) []recordedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []recordedCall
	for _, c := range f.calls {
		if methodAndPath == "" || c.Method+" "+c.Path == methodAndPath {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeGarmin) serve(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	key := r.Method + " " + r.URL.Path
	f.mu.Lock()
	f.calls = append(f.calls, recordedCall{r.Method, r.URL.Path, r.URL.Query(), b, r.Header.Clone()})
	body, ok := f.routes[key]
	f.mu.Unlock()
	if !ok {
		body = status(http.StatusNotFound, map[string]any{"message": "no route"})
	}
	writeBody(w, r, http.StatusOK, body)
}

func writeBody(w http.ResponseWriter, r *http.Request, code int, body any) {
	switch v := body.(type) {
	case http.HandlerFunc:
		v(w, r)
	case func(http.ResponseWriter, *http.Request):
		v(w, r)
	case statusBody:
		writeBody(w, r, v.code, v.body)
	case *sequenceBody:
		v.mu.Lock()
		next := v.bodies[0]
		if len(v.bodies) > 1 {
			v.bodies = v.bodies[1:]
		}
		v.mu.Unlock()
		writeBody(w, r, code, next)
	case rawBody:
		w.WriteHeader(code)
		w.Write(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		w.Write(b)
	}
}

func (f *fakeGarmin) endpoints(string) garmin.Endpoints {
	return garmin.Endpoints{SSO: f.URL, Connect: f.URL, ConnectAPI: f.URL, DIToken: f.URL + "/di-oauth2-service/oauth/token"}
}

// fakeJWT returns an unsigned JWT whose payload carries exp and client_id.
func fakeJWT(exp time.Time, clientID string) string {
	p, _ := json.Marshal(map[string]any{"exp": exp.Unix(), "client_id": clientID})
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc(p) + ".sig"
}

// addAccount saves a valid token pair for email and returns its access token.
func addAccount(t *testing.T, st accounts.Store, email string) string {
	t.Helper()
	tok := garmin.Tokens{DIToken: fakeJWT(time.Now().Add(time.Hour), "client-"+email), DIRefreshToken: "refresh-" + email, DIClientID: "client-" + email}
	if err := st.SaveTokens(email, tok); err != nil {
		t.Fatal(err)
	}
	return tok.DIToken
}

// newStore returns a temp store holding the account me@x.com.
func newStore(t *testing.T) accounts.Store {
	t.Helper()
	st := accounts.Store{Base: t.TempDir()}
	addAccount(t, st, testEmail)
	return st
}

func testOptions(f *fakeGarmin, st accounts.Store, env map[string]string, log io.Writer) Options {
	return Options{
		Store:     st,
		Endpoints: f.endpoints,
		Getenv:    func(k string) string { return env[k] },
		Version:   "test",
		Log:       log,
	}
}

// newSession connects an MCP client to a server backed by f and a temp store with me@x.com.
func newSession(t *testing.T, f *fakeGarmin, env map[string]string) *mcp.ClientSession {
	t.Helper()
	return newSessionWithStore(t, f, newStore(t), env)
}

func newSessionWithStore(t *testing.T, f *fakeGarmin, st accounts.Store, env map[string]string) *mcp.ClientSession {
	t.Helper()
	srv, err := New(testOptions(f, st, env, io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	return connect(t, srv)
}

func connect(t *testing.T, srv *mcp.Server) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close(); ss.Wait() })
	return cs
}

// callTool calls name with args (email defaults to me@x.com, except for list_accounts) and returns the joined text and IsError.
func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	a := map[string]any{}
	if name != "list_accounts" {
		a["email"] = testEmail
	}
	for k, v := range args {
		a[k] = v
	}
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: a})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String(), res.IsError
}

// callOK is callTool that fails the test on a tool error.
func callOK(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	text, isErr := callTool(t, cs, name, args)
	if isErr {
		t.Fatalf("%s: tool error: %s", name, text)
	}
	return text
}

// fixture returns a fresh copy of testdata/fixtures.json[name].
func fixture(t *testing.T, name string) any {
	t.Helper()
	b, err := os.ReadFile("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var all map[string]any
	if err := json.Unmarshal(b, &all); err != nil {
		t.Fatal(err)
	}
	v, ok := all[name]
	if !ok {
		t.Fatalf("no fixture %s", name)
	}
	return v
}

// decode parses a tool's JSON text result.
func decode(t *testing.T, text string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	return v
}

// jsonValue round-trips v through JSON so it compares equal to decoded results.
func jsonValue(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return decode(t, string(b))
}
