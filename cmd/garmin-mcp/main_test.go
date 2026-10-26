package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/accounts"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

func TestVersion(t *testing.T) {
	for _, arg := range []string{"version", "--version"} {
		var out bytes.Buffer
		code := run(context.Background(), []string{arg}, env{Stdout: &out, Stderr: io.Discard})
		if code != 0 || strings.TrimSpace(out.String()) != "garmin-mcp dev" {
			t.Fatalf("%s: code=%d out=%q", arg, code, out.String())
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	var errOut bytes.Buffer
	if code := run(context.Background(), []string{"bogus"}, env{Stdout: io.Discard, Stderr: &errOut}); code != 2 {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(errOut.String(), "unknown command \"bogus\"") {
		t.Fatal(errOut.String())
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("stdin must not be read") }

func testJWT(exp time.Time) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, _ := json.Marshal(map[string]any{"exp": exp.Unix(), "client_id": "TEST"})
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + "."
}

// fakeGarmin serves the mobile login, MFA, DI token and profile endpoints.
type fakeGarmin struct {
	srv     *httptest.Server
	logins  atomic.Int32
	mfa     atomic.Bool
	invalid atomic.Bool

	mu   sync.Mutex
	code string
}

func newFakeGarmin(t *testing.T) *fakeGarmin {
	t.Helper()
	f := &fakeGarmin{}
	mux := http.NewServeMux()
	mux.HandleFunc("/mobile/api/login", func(w http.ResponseWriter, r *http.Request) {
		f.logins.Add(1)
		switch {
		case f.invalid.Load():
			writeTestJSON(t, w, map[string]any{
				"responseStatus": map[string]any{"type": "INVALID_USERNAME_PASSWORD"},
			})
		case f.mfa.Load():
			writeTestJSON(t, w, map[string]any{
				"responseStatus":  map[string]any{"type": "MFA_REQUIRED"},
				"customerMfaInfo": map[string]any{"mfaLastMethodUsed": "email"},
			})
		default:
			writeTestJSON(t, w, map[string]any{
				"responseStatus":  map[string]any{"type": "SUCCESSFUL"},
				"serviceTicketId": "ST-1",
			})
		}
	})
	mux.HandleFunc("/mobile/api/mfa/verifyCode", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Code string `json:"mfaVerificationCode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode MFA request: %v", err)
		}
		f.mu.Lock()
		f.code = body.Code
		f.mu.Unlock()
		writeTestJSON(t, w, map[string]any{
			"responseStatus":  map[string]any{"type": "SUCCESSFUL"},
			"serviceTicketId": "ST-MFA",
		})
	})
	mux.HandleFunc("/di-oauth2-service/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(t, w, map[string]any{
			"access_token":  testJWT(time.Now().Add(time.Hour)),
			"refresh_token": "RT-1",
		})
	})
	mux.HandleFunc("/userprofile-service/socialProfile", func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(t, w, map[string]any{"displayName": "testuser", "fullName": "Test User"})
	})
	mux.HandleFunc("/userprofile-service/userprofile/user-settings", func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(t, w, map[string]any{"userData": map[string]any{"measurementSystem": "metric"}})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

func (f *fakeGarmin) endpoints(domain string) garmin.Endpoints {
	return garmin.Endpoints{
		SSO:        f.srv.URL,
		Connect:    f.srv.URL,
		ConnectAPI: f.srv.URL,
		DIToken:    f.srv.URL + "/di-oauth2-service/oauth/token",
	}
}

type cli struct {
	env  env
	out  *bytes.Buffer
	err  *bytes.Buffer
	base string
}

func newCLI(t *testing.T) *cli {
	t.Helper()
	base := t.TempDir()
	c := &cli{out: &bytes.Buffer{}, err: &bytes.Buffer{}, base: base}
	c.env = env{
		Stdin:  strings.NewReader(""),
		Stdout: c.out,
		Stderr: c.err,
		Getenv: func(k string) string {
			if k == "GARMIN_MCP_HOME" {
				return base
			}
			return ""
		},
		IsTerminal: func() bool { return true },
	}
	return c
}

func (c *cli) store() accounts.Store { return accounts.Store{Base: c.base} }

func (c *cli) auth(f *fakeGarmin, stdin string) {
	c.env.Stdin = strings.NewReader(stdin)
	c.env.ReadPassword = func() (string, error) { return "pw", nil }
	c.env.Endpoints = f.endpoints
	c.env.Login = &garmin.LoginOptions{Strategies: []string{"mobile+plain"}}
}

func TestFirstRunStartsAuth(t *testing.T) {
	f := newFakeGarmin(t)
	c := newCLI(t)
	c.auth(f, "Me@X.com\n")

	if code := run(context.Background(), nil, c.env); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, c.err)
	}
	for _, want := range []string{
		"Welcome to garmin-mcp. Let's connect your first Garmin account.",
		"Logged in as Test User (me@x.com)",
		"Saved to ",
	} {
		if !strings.Contains(c.out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, c.out)
		}
	}
	if _, err := os.Stat(filepath.Join(c.base, "accounts", "me@x.com", "tokens.json")); err != nil {
		t.Fatalf("tokens not saved: %v", err)
	}
}

func TestAuthWithMFA(t *testing.T) {
	f := newFakeGarmin(t)
	f.mfa.Store(true)
	c := newCLI(t)
	c.auth(f, "me@x.com\n123456\n")

	if code := run(context.Background(), []string{"auth"}, c.env); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, c.err)
	}
	f.mu.Lock()
	code := f.code
	f.mu.Unlock()
	if code != "123456" {
		t.Errorf("verifyCode got %q, want 123456", code)
	}
	if !strings.Contains(c.out.String(), "Enter the MFA code Garmin sent you: ") {
		t.Errorf("missing MFA prompt:\n%s", c.out)
	}
	if !strings.Contains(c.out.String(), "Logged in as Test User (me@x.com)") {
		t.Errorf("missing login line:\n%s", c.out)
	}
}

func TestAuthCNSavesDomain(t *testing.T) {
	f := newFakeGarmin(t)
	c := newCLI(t)
	c.auth(f, "")
	var domains []string
	c.env.Endpoints = func(domain string) garmin.Endpoints {
		domains = append(domains, domain)
		return f.endpoints(domain)
	}
	if err := c.store().SaveSettings("me@x.com", accounts.Settings{
		Domain:         "garmin.com",
		FitDownloadDir: "/tmp/fit",
	}); err != nil {
		t.Fatal(err)
	}

	if code := run(context.Background(), []string{"auth", "--email", "me@x.com", "--cn"}, c.env); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, c.err)
	}
	settings, err := c.store().LoadSettings("me@x.com")
	if err != nil {
		t.Fatal(err)
	}
	if settings.Domain != "garmin.cn" {
		t.Errorf("domain = %q, want garmin.cn", settings.Domain)
	}
	if settings.FitDownloadDir != "/tmp/fit" {
		t.Errorf("fit_download_dir = %q, want /tmp/fit", settings.FitDownloadDir)
	}
	if len(domains) != 1 || domains[0] != "garmin.cn" {
		t.Errorf("Endpoints domains = %v, want [garmin.cn]", domains)
	}
}

func TestAuthExistingValidSkips(t *testing.T) {
	f := newFakeGarmin(t)
	c := newCLI(t)
	c.auth(f, "")
	c.env.ReadPassword = func() (string, error) {
		t.Fatal("password must not be requested for a valid session")
		return "", nil
	}
	if err := c.store().SaveTokens("me@x.com", garmin.Tokens{
		DIToken:        testJWT(time.Now().Add(time.Hour)),
		DIRefreshToken: "RT-1",
	}); err != nil {
		t.Fatal(err)
	}

	if code := run(context.Background(), []string{"auth", "--email", "me@x.com"}, c.env); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, c.err)
	}
	want := "Account me@x.com already has a valid session. Use --force to log in again.\n"
	if c.out.String() != want {
		t.Errorf("output = %q, want %q", c.out, want)
	}
	if got := f.logins.Load(); got != 0 {
		t.Errorf("login requests = %d, want 0", got)
	}
}

func TestAuthBadPassword(t *testing.T) {
	f := newFakeGarmin(t)
	f.invalid.Store(true)
	c := newCLI(t)
	c.auth(f, "")
	c.env.ReadPassword = func() (string, error) { return "s3cret-pw", nil }

	if code := run(context.Background(), []string{"auth", "--email", "me@x.com"}, c.env); code != 1 {
		t.Fatalf("code=%d, want 1", code)
	}
	if !strings.Contains(c.err.String(), "Login failed: invalid username or password") {
		t.Errorf("stderr = %q", c.err)
	}
	if strings.Contains(c.out.String()+c.err.String(), "s3cret-pw") {
		t.Errorf("password leaked into output")
	}
	if _, err := c.store().LoadTokens("me@x.com"); !errors.Is(err, accounts.ErrNoAccount) {
		t.Errorf("tokens saved after failed login: %v", err)
	}
}

func TestNoArgsWithAccountsPrintsList(t *testing.T) {
	c := newCLI(t)
	if err := c.store().SaveTokens("me@x.com", garmin.Tokens{
		DIToken: testJWT(time.Now().Add(time.Hour)),
	}); err != nil {
		t.Fatal(err)
	}

	if code := run(context.Background(), nil, c.env); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, c.err)
	}
	if want := "me@x.com  garmin.com  ok\n"; !strings.Contains(c.out.String(), want) {
		t.Errorf("output missing %q:\n%s", want, c.out)
	}
	if !strings.Contains(c.out.String(), "usage: garmin-mcp <command>") {
		t.Errorf("output missing usage:\n%s", c.out)
	}
	if strings.Contains(c.out.String(), "Email: ") {
		t.Errorf("unexpected auth prompt:\n%s", c.out)
	}
}

func TestNoArgsNotTerminalServes(t *testing.T) {
	c := newCLI(t)
	c.env.IsTerminal = func() bool { return false }
	c.env.Stdin = errorReader{}

	if code := run(context.Background(), nil, c.env); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, c.err)
	}
	if c.out.Len() != 0 || c.err.Len() != 0 {
		t.Errorf("unexpected output: stdout=%q stderr=%q", c.out, c.err)
	}
}

func TestAccountsStatus(t *testing.T) {
	c := newCLI(t)
	st := c.store()
	if err := st.SaveTokens("a@x.com", garmin.Tokens{DIToken: testJWT(time.Now().Add(time.Hour))}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSettings("a@x.com", accounts.Settings{Domain: "garmin.cn"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveTokens("b@x.com", garmin.Tokens{DIToken: testJWT(time.Now().Add(-time.Hour))}); err != nil {
		t.Fatal(err)
	}
	dir, err := st.Dir("c@x.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tokens.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}

	if code := run(context.Background(), []string{"accounts"}, c.env); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, c.err)
	}
	want := "a@x.com  garmin.cn  ok\n" +
		"b@x.com  garmin.com  expired\n" +
		"c@x.com  garmin.com  invalid\n"
	if c.out.String() != want {
		t.Errorf("output = %q, want %q", c.out, want)
	}
}

func TestRemove(t *testing.T) {
	setup := func(t *testing.T) (*cli, accounts.Store) {
		t.Helper()
		c := newCLI(t)
		st := c.store()
		if err := st.SaveTokens("me@x.com", garmin.Tokens{DIToken: testJWT(time.Now().Add(time.Hour))}); err != nil {
			t.Fatal(err)
		}
		return c, st
	}
	exists := func(t *testing.T, st accounts.Store) bool {
		t.Helper()
		dir, err := st.Dir("me@x.com")
		if err != nil {
			t.Fatal(err)
		}
		_, err = os.Stat(dir)
		return err == nil
	}

	t.Run("y removes", func(t *testing.T) {
		c, st := setup(t)
		c.env.Stdin = strings.NewReader("y\n")
		if code := run(context.Background(), []string{"remove", "me@x.com"}, c.env); code != 0 {
			t.Fatalf("code=%d stderr=%s", code, c.err)
		}
		if !strings.Contains(c.out.String(), "Remove me@x.com? [y/N] ") {
			t.Errorf("missing prompt: %q", c.out)
		}
		if exists(t, st) {
			t.Error("account still exists")
		}
	})

	t.Run("n keeps", func(t *testing.T) {
		c, st := setup(t)
		c.env.Stdin = strings.NewReader("n\n")
		if code := run(context.Background(), []string{"remove", "me@x.com"}, c.env); code != 0 {
			t.Fatalf("code=%d stderr=%s", code, c.err)
		}
		if !exists(t, st) {
			t.Error("account removed after declining")
		}
	})

	t.Run("yes flag skips prompt", func(t *testing.T) {
		c, st := setup(t)
		c.env.Stdin = errorReader{}
		if code := run(context.Background(), []string{"remove", "ME@X.COM", "--yes"}, c.env); code != 0 {
			t.Fatalf("code=%d stderr=%s", code, c.err)
		}
		if strings.Contains(c.out.String(), "Remove ") {
			t.Errorf("unexpected prompt: %q", c.out)
		}
		if exists(t, st) {
			t.Error("account still exists")
		}
	})

	t.Run("unknown", func(t *testing.T) {
		c, _ := setup(t)
		if code := run(context.Background(), []string{"remove", "nobody@x.com", "--yes"}, c.env); code != 1 {
			t.Fatalf("code=%d, want 1", code)
		}
		if got := c.err.String(); got != "No account nobody@x.com\n" {
			t.Errorf("stderr = %q", got)
		}
	})
}
