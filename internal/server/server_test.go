package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/elgatito/garmin-connect-go-mcp/internal/accounts"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

const emailDescription = "Email of the Garmin Connect account to use. Call list_accounts to see configured accounts."

func listTools(t *testing.T, cs *mcp.ClientSession) []*mcp.Tool {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return res.Tools
}

func toolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	var names []string
	for _, tool := range listTools(t, cs) {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

func expiredCopy(email string) string {
	return fmt.Sprintf(`The Garmin session for "%s" has expired. Ask the user to run "garmin-mcp auth --email %s --force" in a terminal.`, email, email)
}

func TestEveryToolRequiresEmail(t *testing.T) {
	cs := newSession(t, newFakeGarmin(t), nil)
	for _, tool := range listTools(t, cs) {
		if tool.Name == "list_accounts" {
			continue
		}
		b, _ := json.Marshal(tool.InputSchema)
		var schema struct {
			Required   []string
			Properties map[string]struct {
				Type        any
				Description string
			}
		}
		if err := json.Unmarshal(b, &schema); err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(schema.Required, "email") {
			t.Errorf("%s: email not required: %s", tool.Name, b)
		}
		if p := schema.Properties["email"]; p.Description != emailDescription || p.Type != "string" {
			t.Errorf("%s: email property = %+v", tool.Name, p)
		}
	}
}

func TestMissingEmailIsToolError(t *testing.T) {
	cs := newSession(t, newFakeGarmin(t), nil)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_full_name", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !res.IsError || !strings.Contains(text, "email") {
		t.Fatalf("got %v %q", res.IsError, text)
	}
	if text, isErr := callTool(t, cs, "get_full_name", map[string]any{"email": "  "}); !isErr || text != `invalid email "  "` {
		t.Fatalf("blank email: %v %q", isErr, text)
	}
}

func TestEveryToolAnnotated(t *testing.T) {
	cs := newSession(t, newFakeGarmin(t), nil)
	for _, tool := range listTools(t, cs) {
		if tool.Annotations == nil {
			t.Errorf("%s has no annotations", tool.Name)
		}
	}
}

func TestAnnotations(t *testing.T) {
	sets := map[string]map[string]bool{"read-only": readOnlyTools, "additive": additiveTools, "destructive": destructiveTools}
	for a, sa := range sets {
		for b, sb := range sets {
			for name := range sa {
				if a < b && sb[name] {
					t.Errorf("%s is in both %s and %s", name, a, b)
				}
			}
		}
	}
	got := annotationsFor("get_activities")
	if !got.ReadOnlyHint || got.DestructiveHint == nil || *got.DestructiveHint || got.OpenWorldHint == nil || !*got.OpenWorldHint {
		t.Errorf("get_activities: %+v", got)
	}
	if got := annotationsFor("set_fit_download_dir"); *got.OpenWorldHint {
		t.Error("set_fit_download_dir must not be open-world")
	}
	if got := annotationsFor("schedule_workout"); got.ReadOnlyHint || *got.DestructiveHint || !got.IdempotentHint {
		t.Errorf("schedule_workout: %+v", got)
	}
	if got := annotationsFor("list_accounts"); got == nil || !got.ReadOnlyHint {
		t.Errorf("list_accounts: %+v", got)
	}
	if annotationsFor("brand_new_tool") != nil {
		t.Error("unknown tool must have no annotations")
	}
}

func TestUnknownAccountMessage(t *testing.T) {
	cs := newSession(t, newFakeGarmin(t), nil)
	text, isErr := callTool(t, cs, "get_full_name", map[string]any{"email": "nobody@x.com"})
	want := `No Garmin account "nobody@x.com" is set up. Ask the user to run "garmin-mcp auth --email nobody@x.com" in a terminal. Configured accounts: me@x.com.`
	if !isErr || text != want {
		t.Fatalf("got %v %q", isErr, text)
	}

	empty := accounts.Store{Base: t.TempDir()}
	cs = newSessionWithStore(t, newFakeGarmin(t), empty, nil)
	text, _ = callTool(t, cs, "get_full_name", nil)
	if !strings.HasSuffix(text, "Configured accounts: none.") {
		t.Fatalf("got %q", text)
	}
}

func TestToolRejectsBadEmail(t *testing.T) {
	st := newStore(t)
	parent := filepath.Dir(st.Base)
	before, _ := os.ReadDir(parent)
	cs := newSessionWithStore(t, newFakeGarmin(t), st, nil)
	for _, email := range []string{"../../etc@x", "a@b/../../etc", "../x@y"} {
		text, isErr := callTool(t, cs, "get_full_name", map[string]any{"email": email})
		if want := fmt.Sprintf("invalid email %q", email); !isErr || text != want {
			t.Errorf("%s: got %v %q", email, isErr, text)
		}
	}
	after, _ := os.ReadDir(parent)
	if len(before) != len(after) {
		t.Errorf("files appeared next to the base dir: %v -> %v", before, after)
	}
	entries, _ := os.ReadDir(filepath.Join(st.Base, "accounts"))
	if len(entries) != 1 {
		t.Errorf("accounts dir has %d entries", len(entries))
	}
}

func TestEmailCaseInsensitive(t *testing.T) {
	cs := newSession(t, newFakeGarmin(t), nil)
	text, isErr := callTool(t, cs, "get_full_name", map[string]any{"email": " ME@X.COM "})
	if isErr || !strings.Contains(text, "Test User") {
		t.Fatalf("got %v %q", isErr, text)
	}
}

func TestCorruptTokensMessage(t *testing.T) {
	st := newStore(t)
	dir, _ := st.Dir(testEmail)
	if err := os.WriteFile(filepath.Join(dir, "tokens.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	cs := newSessionWithStore(t, newFakeGarmin(t), st, nil)
	text, isErr := callTool(t, cs, "get_full_name", nil)
	if !isErr || text != expiredCopy(testEmail) {
		t.Fatalf("got %v %q", isErr, text)
	}
}

func TestExpiredRefreshMessage(t *testing.T) {
	st := accounts.Store{Base: t.TempDir()}
	tok := garmin.Tokens{DIToken: fakeJWT(time.Now().Add(time.Minute), "cid"), DIRefreshToken: "r", DIClientID: "cid"}
	if err := st.SaveTokens(testEmail, tok); err != nil {
		t.Fatal(err)
	}
	f := newFakeGarmin(t)
	f.on("POST /di-oauth2-service/oauth/token", status(400, map[string]any{"error": "invalid_grant"}))
	cs := newSessionWithStore(t, f, st, nil)
	text, isErr := callTool(t, cs, "get_full_name", nil)
	if !isErr || text != expiredCopy(testEmail) {
		t.Fatalf("got %v %q", isErr, text)
	}
}

func TestTwoAccountsIsolated(t *testing.T) {
	st := accounts.Store{Base: t.TempDir()}
	tokA, tokB := addAccount(t, st, "a@x.com"), addAccount(t, st, "b@x.com")
	f := newFakeGarmin(t)
	cs := newSessionWithStore(t, f, st, nil)
	for _, email := range []string{"a@x.com", "b@x.com", "a@x.com"} {
		if text, isErr := callTool(t, cs, "get_user_profile", map[string]any{"email": email}); isErr {
			t.Fatalf("%s: %s", email, text)
		}
	}
	var got []string
	for _, c := range f.requests("GET /userprofile-service/userprofile/user-settings") {
		got = append(got, c.Header.Get("Authorization"))
	}
	want := []string{"Bearer " + tokA, "Bearer " + tokB, "Bearer " + tokA}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bearers = %v, want %v", got, want)
	}
}

func TestListAccounts(t *testing.T) {
	cs := newSession(t, newFakeGarmin(t), nil)
	list := func() any {
		text, isErr := callTool(t, cs, "list_accounts", nil)
		if isErr {
			t.Fatal(text)
		}
		return decode(t, text)
	}
	want := map[string]any{"accounts": []any{map[string]any{"email": testEmail, "domain": "garmin.com", "display_name": "", "token_status": "ok"}}}
	if got := list(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	callTool(t, cs, "get_full_name", nil)
	want["accounts"].([]any)[0].(map[string]any)["display_name"] = "testuser"
	if got := list(); !reflect.DeepEqual(got, want) {
		t.Fatalf("after profile fetch got %v", got)
	}
}

func TestProfileFiltering(t *testing.T) {
	f := newFakeGarmin(t)
	all := toolNames(t, newSession(t, f, nil))

	var want []string
	for _, n := range all {
		if slices.Contains(toolProfiles["minimal"], n) {
			want = append(want, n)
		}
	}
	if !slices.Contains(want, "list_accounts") {
		t.Fatal("minimal profile lacks list_accounts")
	}
	if got := toolNames(t, newSession(t, f, map[string]string{"GARMIN_TOOL_PROFILE": "minimal"})); !reflect.DeepEqual(got, want) {
		t.Errorf("minimal: got %v, want %v", got, want)
	}
	got := toolNames(t, newSession(t, f, map[string]string{"GARMIN_TOOL_PROFILE": "minimal", "GARMIN_ENABLED_TOOLS": "set_heart_rate_zones", "GARMIN_DISABLED_TOOLS": "GET_FULL_NAME"}))
	if !slices.Contains(got, "set_heart_rate_zones") || slices.Contains(got, "get_full_name") {
		t.Errorf("minimal + lists: %v", got)
	}
	if got := toolNames(t, newSession(t, f, map[string]string{"GARMIN_ENABLED_TOOLS": "get_full_name"})); !reflect.DeepEqual(got, []string{"get_full_name"}) {
		t.Errorf("allowlist: %v", got)
	}

	var log bytes.Buffer
	if _, err := New(testOptions(f, newStore(t), map[string]string{"GARMIN_DISABLED_TOOLS": "get_full_name,get_typo"}, &log)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "Tool filter: warning — name(s) not found and ignored: get_typo\n") {
		t.Errorf("log = %q", log.String())
	}
}

// Ported from tests/unit/test_tool_filter.py.
func TestToolFilter(t *testing.T) {
	abcd := []string{"get_a", "get_b", "get_c", "get_d"}
	cases := []struct {
		name               string
		profile            []string // nil: every tool
		enabled, disabled  string
		register, want     []string
		unknown            []string
		profileEnvOverride string
	}{
		{name: "no filter registers all", register: abcd[:2], want: abcd[:2]},
		{name: "allowlist only registers listed", enabled: "get_a", register: abcd[:2], want: []string{"get_a"}},
		{name: "denylist skips listed", disabled: "get_b", register: abcd[:2], want: []string{"get_a"}},
		{name: "allowlist takes precedence over denylist", enabled: "get_a", disabled: "get_a", register: abcd[:2], want: []string{"get_a"}},
		{name: "matching is case insensitive", enabled: "get_a", register: []string{"GET_A"}, want: []string{"GET_A"}},
		{name: "unknown filter names flags typos", enabled: "get_a,get_typo", register: []string{"get_a"}, want: []string{"get_a"}, unknown: []string{"get_typo"}},
		{name: "blank allowlist keeps denylist behavior", enabled: "  \t", disabled: "get_b", register: abcd[:2], want: []string{"get_a"}},
		{name: "profile registers only its tools", profile: []string{"get_a"}, register: abcd[:3], want: []string{"get_a"}},
		{name: "profile plus allowlist adds tools", profile: []string{"get_a"}, enabled: "get_c", register: abcd[:3], want: []string{"get_a", "get_c"}},
		{name: "profile plus denylist removes tools", profile: []string{"get_a", "get_b"}, disabled: "get_a", register: abcd[:3], want: []string{"get_b"}},
		{name: "profile applies both lists together", profile: []string{"get_a", "get_b"}, enabled: "get_c,get_d", disabled: "get_a,get_d", register: abcd, want: []string{"get_b", "get_c"}},
		{name: "profile matching is case insensitive", profile: []string{"get_a"}, register: []string{"GET_A", "get_b"}, want: []string{"GET_A"}},
		{name: "unknown filter names with profile checks both lists", profile: []string{"get_b"}, enabled: "get_a,typo_one", disabled: "typo_two", register: abcd[:2], want: abcd[:2], unknown: []string{"typo_one", "typo_two"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, err := newToolFilter(func(k string) string {
				return map[string]string{"GARMIN_ENABLED_TOOLS": c.enabled, "GARMIN_DISABLED_TOOLS": c.disabled}[k]
			})
			if err != nil {
				t.Fatal(err)
			}
			if c.profile != nil {
				f.profile = toSet(c.profile)
			}
			var got []string
			for _, n := range c.register {
				if f.allow(n) {
					got = append(got, n)
				}
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("registered %v, want %v", got, c.want)
			}
			if u := f.unknownNames(); !reflect.DeepEqual(u, c.unknown) && len(u)+len(c.unknown) > 0 {
				t.Errorf("unknown %v, want %v", u, c.unknown)
			}
		})
	}

	_, err := newToolFilter(func(k string) string { return map[string]string{"GARMIN_ENABLED_TOOLS": ",,  ,"}[k] })
	if err == nil || err.Error() != "Invalid GARMIN_ENABLED_TOOLS: expected at least one tool name" {
		t.Errorf("malformed allowlist: %v", err)
	}
	f, _ := newToolFilter(func(k string) string { return map[string]string{"GARMIN_TOOL_PROFILE": "minimal"}[k] })
	if !reflect.DeepEqual(f.profile, toSet(toolProfiles["minimal"])) {
		t.Error("GARMIN_TOOL_PROFILE=minimal not read")
	}
	f, _ = newToolFilter(func(string) string { return "" })
	if f.profile != nil {
		t.Error("unset profile must select every tool")
	}
}

// Ported from tests/unit/test_tool_profiles.py.
func TestResolveToolProfile(t *testing.T) {
	for _, v := range []string{"all", "", "   ", "health,all"} {
		if got, err := resolveToolProfile(v); got != nil || err != nil {
			t.Errorf("%q: %v %v", v, got, err)
		}
	}
	if got, _ := resolveToolProfile(" Health "); !reflect.DeepEqual(got, toSet(toolProfiles["health"])) {
		t.Error("case-insensitive single profile")
	}
	got, _ := resolveToolProfile("training, workouts")
	if !reflect.DeepEqual(got, toSet(append(slices.Clone(toolProfiles["training"]), toolProfiles["workouts"]...))) ||
		!got["get_training_status"] || !got["create_run_workout"] || got["log_food"] {
		t.Error("union of profiles")
	}
	_, err := resolveToolProfile("helth")
	if err == nil || !strings.Contains(err.Error(), "Unknown GARMIN_TOOL_PROFILE 'helth'") ||
		!strings.Contains(err.Error(), "Known profiles: activities, all, health, minimal, nutrition, training, workouts") {
		t.Errorf("unknown profile: %v", err)
	}
	if _, err := resolveToolProfile(",, ,"); err == nil || !strings.Contains(err.Error(), "expected at least one profile name") {
		t.Errorf("separators only: %v", err)
	}
	if !slices.Contains(coreTools, "list_accounts") {
		t.Error("list_accounts must be in every profile")
	}
	for name, tools := range toolProfiles {
		if tools == nil {
			continue
		}
		set := toSet(tools)
		for _, c := range coreTools {
			if !set[c] {
				t.Errorf("profile %s lacks core tool %s", name, c)
			}
		}
	}
}

func TestUnknownProfileFails(t *testing.T) {
	_, err := New(testOptions(newFakeGarmin(t), newStore(t), map[string]string{"GARMIN_TOOL_PROFILE": "helth"}, nil))
	want := "Unknown GARMIN_TOOL_PROFILE 'helth'. Known profiles: activities, all, health, minimal, nutrition, training, workouts"
	if err == nil || err.Error() != want {
		t.Fatalf("got %v", err)
	}
}

func TestCallTimeout(t *testing.T) {
	f := newFakeGarmin(t)
	f.on("GET /userprofile-service/socialProfile", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(time.Second):
		case <-r.Context().Done():
		}
	})
	cs := newSession(t, f, map[string]string{"GARMIN_MCP_CALL_TIMEOUT": "0.2"})
	start := time.Now()
	text, isErr := callTool(t, cs, "get_full_name", nil)
	want := "Garmin request 'get_full_name' did not return within 0.2s and was abandoned. This is usually a transient stall on Garmin's side — please try again. (Adjust with GARMIN_MCP_CALL_TIMEOUT, or set it to 0 to disable the limit.)"
	if !isErr || text != want {
		t.Fatalf("got %v %q", isErr, text)
	}
	if d := time.Since(start); d > 900*time.Millisecond {
		t.Errorf("took %v", d)
	}
}

// Ported from the _resolve_call_timeout cases in tests/unit/test_garmin_proxy.py.
func TestResolveCallTimeout(t *testing.T) {
	for _, c := range []struct {
		raw  string
		want time.Duration
		log  bool
	}{{"", 90 * time.Second, false}, {"  ", 90 * time.Second, false}, {"5", 5 * time.Second, false}, {"0", 0, false}, {"-1", 0, false}, {"abc", 90 * time.Second, true}} {
		var log bytes.Buffer
		got := resolveCallTimeout(func(string) string { return c.raw }, &log)
		if got != c.want || (log.Len() > 0) != c.log {
			t.Errorf("%q: %v, log %q", c.raw, got, log.String())
		}
	}
}

// Ported from tests/unit/test_write_errors.py; Go errors stand in for the Python exception shapes.
func TestWriteFailurePayload(t *testing.T) {
	dnsErr := &url.Error{Op: "Put", URL: "https://connectapi.garmin.com/x", Err: &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "connectapi.garmin.com"}}}
	connectTimeout := &url.Error{Op: "Put", URL: "https://connectapi.garmin.com/x", Err: &net.OpError{Op: "dial", Net: "tcp", Err: context.DeadlineExceeded}}
	readTimeout := &url.Error{Op: "Put", URL: "https://connectapi.garmin.com/x", Err: context.DeadlineExceeded}

	payload := func(err error, sent, committed bool, log *bytes.Buffer) map[string]any {
		t.Helper()
		var b bytes.Buffer
		if log == nil {
			log = &b
		}
		tr := newWriteTracker("log_food", "Error logging food", log)
		if sent {
			tr.Sent()
		}
		if committed {
			tr.Committed()
		}
		var wf *writeFailure
		if !errors.As(tr.Fail(err), &wf) {
			t.Fatal("Fail must return a *writeFailure")
		}
		res := toolResult(nil, wf)
		if !res.IsError {
			t.Fatal("write failure must be an error result")
		}
		return decode(t, res.Content[0].(*mcp.TextContent).Text).(map[string]any)
	}
	type want struct {
		stage     string
		status    any // float64 or nil
		body      any
		committed any
		errType   string
		hint      string
	}
	cases := []struct {
		name      string
		err       error
		sent, com bool
		want      want
	}{
		{"garmin 401 is rejected request", &garmin.APIError{Status: 401}, true, false, want{"garmin_request", 401.0, nil, false, "GarminConnectConnectionError", ""}},
		{"garmin 400 carries upstream body", &garmin.APIError{Status: 400, Message: "Required Parameter meal id is null"}, true, false, want{"garmin_request", 400.0, "Required Parameter meal id is null", false, "", ""}},
		{"garmin 400 after send is rejected", &garmin.APIError{Status: 400, Message: "rejected"}, true, false, want{"", 400.0, "rejected", false, "", "Nothing was written"}},
		{"garmin 404 after send is rejected", &garmin.APIError{Status: 404, Message: "rejected"}, true, false, want{"", 404.0, "rejected", false, "", "Nothing was written"}},
		{"garmin 500 after send is unknown", &garmin.APIError{Status: 500, Message: "<html>Gateway Time-out</html>"}, true, false, want{"garmin_request", 500.0, nil, "unknown", "", "The request may have reached Garmin"}},
		{"garmin 502 after send is unknown", &garmin.APIError{Status: 502, Message: "<html>Gateway Time-out</html>"}, true, false, want{"garmin_request", 502.0, nil, "unknown", "", "The request may have reached Garmin"}},
		{"garmin 504 after send is unknown", fmt.Errorf("wrapped: %w", &garmin.APIError{Status: 504, Message: "<html>Gateway Time-out</html>"}), true, false, want{"garmin_request", 504.0, nil, "unknown", "", "The request may have reached Garmin"}},
		{"garmin 5xx before send is not committed", &garmin.APIError{Status: 504, Message: "Gateway Time-out"}, false, false, want{"", 504.0, nil, false, "", ""}},
		{"rate limit is 429", &garmin.APIError{Status: 429, Message: "slow down"}, true, false, want{"", 429.0, nil, false, "GarminConnectTooManyRequestsError", ""}},
		{"auth error is auth refresh", fmt.Errorf("%w: token refresh rejected with status 400", garmin.ErrSessionExpired), true, false, want{"auth_refresh", nil, nil, false, "GarminConnectAuthenticationError", ""}},
		{"dns failure never committed", dnsErr, true, false, want{"garmin_request", nil, nil, false, "ConnectionError", ""}},
		{"connect timeout never committed", connectTimeout, true, false, want{"", nil, nil, false, "ConnectTimeout", ""}},
		{"read timeout after send is unknown", readTimeout, true, false, want{"garmin_request", nil, nil, "unknown", "ReadTimeout", "duplicate"}},
		{"read timeout before send is not committed", readTimeout, false, false, want{"", nil, nil, false, "", ""}},
		{"call timeout after send is unknown", context.DeadlineExceeded, true, false, want{"", nil, nil, "unknown", "TimeoutError", ""}},
		{"non-JSON 2xx body is response parse committed", fmt.Errorf("invalid JSON from /x: %w", &json.SyntaxError{}), true, false, want{"response_parse", nil, nil, true, "JSONDecodeError", ""}},
		{"failure after commit is committed", errors.New("not JSON serializable"), true, true, want{"response_parse", nil, nil, true, "", ""}},
		{"unexpected error before send", errors.New("mealId"), false, false, want{"internal", nil, nil, false, "", ""}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := payload(c.err, c.sent, c.com, nil)
			w := c.want
			if w.stage != "" && p["stage"] != w.stage {
				t.Errorf("stage = %v", p["stage"])
			}
			if p["upstream_status"] != w.status {
				t.Errorf("upstream_status = %v", p["upstream_status"])
			}
			if w.body != nil && p["upstream_body"] != w.body {
				t.Errorf("upstream_body = %v", p["upstream_body"])
			}
			if w.status == 401.0 && p["upstream_body"] != nil {
				t.Errorf("upstream_body = %v, want null", p["upstream_body"])
			}
			if p["write_committed"] != w.committed {
				t.Errorf("write_committed = %v", p["write_committed"])
			}
			if w.errType != "" && p["error_type"] != w.errType {
				t.Errorf("error_type = %v", p["error_type"])
			}
			if hint, _ := p["hint"].(string); w.hint != "" && !strings.Contains(hint, w.hint) {
				t.Errorf("hint = %q", hint)
			}
		})
	}

	t.Run("upstream body is trimmed", func(t *testing.T) {
		p := payload(&garmin.APIError{Status: 500, Message: strings.Repeat("x", 2000)}, true, false, nil)
		if len(p["upstream_body"].(string)) != 500 {
			t.Errorf("len = %d", len(p["upstream_body"].(string)))
		}
	})
	t.Run("message keeps tool prefix", func(t *testing.T) {
		p := payload(&garmin.APIError{Status: 401}, true, false, nil)
		if p["status"] != "error" || p["tool"] != "log_food" || !strings.HasPrefix(p["message"].(string), "Error logging food: API Error 401") {
			t.Errorf("%v", p)
		}
	})
	t.Run("correlation id is logged", func(t *testing.T) {
		var log bytes.Buffer
		p := payload(readTimeout, true, false, &log)
		cid := p["correlation_id"].(string)
		if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(cid) {
			t.Errorf("correlation_id = %q", cid)
		}
		line := log.String()
		if !strings.Contains(line, cid) || !strings.Contains(line, "log_food failed") || !strings.Contains(line, "write_committed=unknown") {
			t.Errorf("log = %q", line)
		}
	})
	t.Run("correlation ids are unique", func(t *testing.T) {
		err := &garmin.APIError{Status: 401}
		if payload(err, false, false, nil)["correlation_id"] == payload(err, false, false, nil)["correlation_id"] {
			t.Error("same id twice")
		}
	})
}

func TestToolResultRendering(t *testing.T) {
	text := func(r *mcp.CallToolResult) string { return r.Content[0].(*mcp.TextContent).Text }
	if r := toolResult("plain <b>", nil); r.IsError || text(r) != "plain <b>" {
		t.Errorf("string: %q", text(r))
	}
	if r := toolResult(map[string]any{"a": "<b>&"}, nil); r.IsError || text(r) != "{\n  \"a\": \"<b>&\"\n}" {
		t.Errorf("json: %q", text(r))
	}
	if r := toolResult(nil, errors.New("boom")); !r.IsError || text(r) != "boom" {
		t.Errorf("error: %q", text(r))
	}
}

func TestWorkoutResources(t *testing.T) {
	cs := newSession(t, newFakeGarmin(t), nil)
	res, err := cs.ListResources(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var uris []string
	var strength *mcp.Resource
	for _, r := range res.Resources {
		uris = append(uris, r.URI)
		if r.MIMEType != "application/json" {
			t.Errorf("%s: mime %q", r.URI, r.MIMEType)
		}
		if r.URI == "workout://templates/strength-circuit" {
			strength = r
		}
	}
	sort.Strings(uris)
	want := []string{"workout://reference/structure", "workout://templates/interval-running", "workout://templates/simple-run", "workout://templates/strength-circuit", "workout://templates/tempo-run"}
	if !reflect.DeepEqual(uris, want) {
		t.Fatalf("uris = %v", uris)
	}

	read := func(uri string) map[string]any {
		r, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
		if err != nil {
			t.Fatal(err)
		}
		return decode(t, r.Contents[0].Text).(map[string]any)
	}
	if got := read("workout://templates/simple-run")["workoutName"]; got != "Simple Run" {
		t.Errorf("workoutName = %v", got)
	}

	// test_strength_resource_description_matches_reps_based_groups
	tmpl := read("workout://templates/strength-circuit")
	groups := tmpl["workoutSegments"].([]any)[0].(map[string]any)["workoutSteps"].([]any)[1:3]
	for i, wantReps := range []float64{10, 8} {
		g := groups[i].(map[string]any)
		if g["numberOfIterations"] != 3.0 || g["workoutSteps"].([]any)[0].(map[string]any)["endConditionValue"] != wantReps {
			t.Errorf("group %d = %v", i, g)
		}
	}
	if !strings.Contains(strength.Description, "10 reps") || !strings.Contains(strength.Description, "8 reps") || strings.Contains(strength.Description, "10min work") {
		t.Errorf("description = %q", strength.Description)
	}

	// test_swimming_reference_matches_captured_garmin_workout
	sport := fixture(t, "MOCK_SWIM_WORKOUT_DETAILS").(map[string]any)["sportType"].(map[string]any)
	ref := read("workout://reference/structure")["sportType_values"].(map[string]any)
	id := fmt.Sprint(sport["sportTypeId"])
	if ref[id].(map[string]any)["sportTypeKey"] != sport["sportTypeKey"] {
		t.Errorf("sport %s: %v vs %v", id, ref[id], sport)
	}
}

func TestHandlerPanicIsToolError(t *testing.T) {
	var log bytes.Buffer
	filter, err := newToolFilter(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	r := &registry{srv: mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil), filter: filter, log: &log}
	addPlainTool(r, "boom", "panics", func(context.Context, struct{}) (any, error) {
		var m map[string]any
		return m["x"].(string), nil // nil interface assertion panics
	})
	addPlainTool(r, "fine", "works", func(context.Context, struct{}) (any, error) { return "ok", nil })
	cs := connect(t, r.srv)
	call := func(name string) (string, bool) {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return res.Content[0].(*mcp.TextContent).Text, res.IsError
	}
	for i := 0; i < 2; i++ {
		text, isErr := call("boom")
		if !isErr || !strings.HasPrefix(text, "internal error in boom: interface conversion") || strings.Contains(text, "goroutine") {
			t.Fatalf("boom: %v %q", isErr, text)
		}
		if text, isErr := call("fine"); isErr || text != "ok" {
			t.Fatalf("fine after panic: %v %q", isErr, text)
		}
	}
	if s := log.String(); !strings.Contains(s, "internal error in boom") || !strings.Contains(s, "goroutine") {
		t.Errorf("log lacks the stack trace: %q", s)
	}
}

func TestRemovedAccountStopsWorkingAndStaysRemoved(t *testing.T) {
	f := newFakeGarmin(t)
	st := newStore(t)
	cs := newSessionWithStore(t, f, st, nil)
	callOK(t, cs, "get_full_name", nil) // caches the client
	if err := st.Remove(testEmail); err != nil {
		t.Fatal(err)
	}
	text, isErr := callTool(t, cs, "get_full_name", nil)
	if !isErr || !strings.HasPrefix(text, `No Garmin account "me@x.com" is set up.`) {
		t.Fatalf("got %v %q", isErr, text)
	}
	dir, _ := st.Dir(testEmail)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("account folder recreated: %v", err)
	}
}
