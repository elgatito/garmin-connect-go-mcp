package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

const diPath = "/di-oauth2-service/oauth/token"

func fakeJWT(exp time.Time) string {
	p, _ := json.Marshal(map[string]any{"exp": exp.Unix(), "client_id": "CID"})
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc(p) + ".sig"
}

// rig is a fake SSO + diauth + connectapi server plus an env wired to it and to a temp GARMIN_MCP_HOME.
type rig struct {
	t         *testing.T
	home      string
	out, errb bytes.Buffer
	env       env
	mu        sync.Mutex
	hits      map[string]int
	mfa       bool
	badPass   bool
	code      string // MFA code received
	domains   []string
}

func (r *rig) count(p string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hits[p]
}

func newRig(t *testing.T, stdin string, terminal bool) *rig {
	t.Helper()
	r := &rig{t: t, home: t.TempDir(), hits: map[string]int{}}
	status := func(typ string) map[string]any {
		m := map[string]any{"responseStatus": map[string]any{"type": typ}}
		if typ == "SUCCESSFUL" {
			m["serviceTicketId"] = "ST-1"
		}
		return m
	}
	mux := http.NewServeMux()
	handle := func(p string, h func(w http.ResponseWriter, req *http.Request)) {
		mux.HandleFunc(p, func(w http.ResponseWriter, req *http.Request) {
			r.mu.Lock()
			r.hits[p]++
			r.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			h(w, req)
		})
	}
	handle("/mobile/api/login", func(w http.ResponseWriter, req *http.Request) {
		switch {
		case r.badPass:
			json.NewEncoder(w).Encode(status("INVALID_USERNAME_PASSWORD"))
		case r.mfa:
			json.NewEncoder(w).Encode(map[string]any{"responseStatus": map[string]any{"type": "MFA_REQUIRED"},
				"customerMfaInfo": map[string]any{"mfaLastMethodUsed": "email"}})
		default:
			json.NewEncoder(w).Encode(status("SUCCESSFUL"))
		}
	})
	handle("/mobile/api/mfa/verifyCode", func(w http.ResponseWriter, req *http.Request) {
		var body map[string]any
		json.NewDecoder(req.Body).Decode(&body)
		r.code, _ = body["mfaVerificationCode"].(string)
		json.NewEncoder(w).Encode(status("SUCCESSFUL"))
	})
	handle(diPath, func(w http.ResponseWriter, req *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"access_token": fakeJWT(time.Now().Add(time.Hour)), "refresh_token": "R-1"})
	})
	handle("/userprofile-service/socialProfile", func(w http.ResponseWriter, req *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"displayName": "uuid", "fullName": "Test User"})
	})
	handle("/userprofile-service/userprofile/user-settings", func(w http.ResponseWriter, req *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"userData": map[string]any{"measurementSystem": "metric"}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	r.env = env{
		Stdin:  strings.NewReader(stdin),
		Stdout: &r.out,
		Stderr: &r.errb,
		Getenv: func(k string) string {
			if k == "GARMIN_MCP_HOME" {
				return r.home
			}
			return ""
		},
		IsTerminal:   func() bool { return terminal },
		ReadPassword: func() (string, error) { return "pw", nil },
		Endpoints: func(domain string) garmin.Endpoints {
			r.domains = append(r.domains, domain)
			return garmin.Endpoints{SSO: srv.URL, Connect: srv.URL, ConnectAPI: srv.URL, DIToken: srv.URL + diPath}
		},
		Login: &garmin.LoginOptions{Strategies: []string{"mobile+plain"}},
	}
	return r
}

func (r *rig) run(args ...string) int { return run(context.Background(), args, r.env) }

func (r *rig) store() accounts.Store { return accounts.Store{Base: r.home} }

func (r *rig) save(email string, exp time.Time) {
	r.t.Helper()
	if err := r.store().SaveTokens(email, garmin.Tokens{DIToken: fakeJWT(exp), DIRefreshToken: "R", DIClientID: "CID"}); err != nil {
		r.t.Fatal(err)
	}
}

func TestFirstRunStartsAuth(t *testing.T) {
	r := newRig(t, "Me@X.com\n", true)
	if code := r.run(); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, r.errb.String())
	}
	for _, want := range []string{"Welcome to garmin-mcp", "Logged in as Test User (me@x.com)"} {
		if !strings.Contains(r.out.String(), want) {
			t.Errorf("stdout missing %q: %q", want, r.out.String())
		}
	}
	if _, err := os.Stat(filepath.Join(r.home, "accounts", "me@x.com", "tokens.json")); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.out.String()+r.errb.String(), "pw") {
		t.Error("password leaked into output")
	}
}

func TestAuthWithMFA(t *testing.T) {
	r := newRig(t, "me@x.com\n123456\n", true)
	r.mfa = true
	if code := r.run("auth"); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, r.errb.String())
	}
	if r.code != "123456" {
		t.Fatalf("verifyCode got %q", r.code)
	}
	if !strings.Contains(r.out.String(), "Enter the MFA code Garmin sent you: ") {
		t.Fatalf("stdout = %q", r.out.String())
	}
}

func TestAuthCNSavesDomain(t *testing.T) {
	r := newRig(t, "", true)
	if err := r.store().SaveSettings("me@x.com", accounts.Settings{Domain: "garmin.com", FitDownloadDir: "/fit"}); err != nil {
		t.Fatal(err)
	}
	if code := r.run("auth", "--email", "me@x.com", "--cn"); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, r.errb.String())
	}
	st, err := r.store().LoadSettings("me@x.com")
	if err != nil || st.Domain != "garmin.cn" || st.FitDownloadDir != "/fit" {
		t.Fatalf("settings=%+v err=%v", st, err)
	}
	if len(r.domains) == 0 || r.domains[0] != "garmin.cn" {
		t.Fatalf("Endpoints domains = %v", r.domains)
	}
}

func TestAuthExistingValidSkips(t *testing.T) {
	r := newRig(t, "", true)
	r.save("me@x.com", time.Now().Add(time.Hour))
	if code := r.run("auth", "--email", "me@x.com"); code != 0 {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(r.out.String(), "Account me@x.com already has a valid session. Use --force to log in again.") {
		t.Fatalf("stdout = %q", r.out.String())
	}
	if n := r.count("/mobile/api/login"); n != 0 {
		t.Fatalf("login hits = %d", n)
	}
	if code := r.run("auth", "--email", "me@x.com", "--force"); code != 0 || r.count("/mobile/api/login") != 1 {
		t.Fatalf("--force: code=%d hits=%d", code, r.count("/mobile/api/login"))
	}
}

func TestAuthBadPassword(t *testing.T) {
	r := newRig(t, "", true)
	r.badPass = true
	if code := r.run("auth", "--email", "me@x.com"); code != 1 {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(r.errb.String(), "Login failed: invalid username or password") {
		t.Fatalf("stderr = %q", r.errb.String())
	}
	if list, _ := r.store().List(); len(list) != 0 {
		t.Fatalf("account saved: %v", list)
	}
}

func TestNoArgsWithAccountsPrintsList(t *testing.T) {
	r := newRig(t, "", true)
	r.save("me@x.com", time.Now().Add(time.Hour))
	if code := r.run(); code != 0 {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(r.out.String(), "me@x.com  garmin.com  ok") || !strings.Contains(r.out.String(), "usage:") {
		t.Fatalf("stdout = %q", r.out.String())
	}
}

func TestNoArgsNotTerminalServes(t *testing.T) {
	r := newRig(t, "must not be read\n", false)
	if code := r.run(); code != 0 {
		t.Fatalf("code=%d", code)
	}
	if r.out.Len() != 0 || r.errb.Len() != 0 || r.count("/mobile/api/login") != 0 {
		t.Fatalf("out=%q err=%q", r.out.String(), r.errb.String())
	}
}

func TestAccountsStatus(t *testing.T) {
	r := newRig(t, "", true)
	if code := r.run("accounts"); code != 0 || !strings.Contains(r.out.String(), `No accounts. Run "garmin-mcp auth" to add one.`) {
		t.Fatalf("empty: code=%d out=%q", code, r.out.String())
	}
	r.out.Reset()
	r.save("a@x.com", time.Now().Add(time.Hour))
	r.save("b@x.com", time.Now().Add(-time.Hour))
	bad := filepath.Join(r.home, "accounts", "c@x.com")
	os.MkdirAll(bad, 0o700)
	os.WriteFile(filepath.Join(bad, "tokens.json"), []byte("{nope"), 0o600)
	r.store().SaveSettings("b@x.com", accounts.Settings{Domain: "garmin.cn"})
	if code := r.run("accounts"); code != 0 {
		t.Fatalf("code=%d", code)
	}
	want := "a@x.com  garmin.com  ok\nb@x.com  garmin.cn  expired\nc@x.com  garmin.com  invalid\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
}

func TestRemove(t *testing.T) {
	r := newRig(t, "n\n", true)
	r.save("me@x.com", time.Now().Add(time.Hour))
	exists := func() bool { _, err := os.Stat(filepath.Join(r.home, "accounts", "me@x.com")); return err == nil }

	if code := r.run("remove", "me@x.com"); code != 0 || !exists() {
		t.Fatalf("n: code=%d exists=%v", code, exists())
	}
	if !strings.Contains(r.out.String(), "Remove me@x.com? [y/N] ") {
		t.Fatalf("stdout = %q", r.out.String())
	}
	r.env.Stdin = strings.NewReader("y\n")
	if code := r.run("remove", "me@x.com"); code != 0 || exists() {
		t.Fatalf("y: code=%d exists=%v", code, exists())
	}
	r.save("me@x.com", time.Now().Add(time.Hour))
	r.out.Reset()
	r.env.Stdin = strings.NewReader("")
	if code := r.run("remove", "me@x.com", "--yes"); code != 0 || exists() || strings.Contains(r.out.String(), "[y/N]") {
		t.Fatalf("--yes: code=%d exists=%v out=%q", code, exists(), r.out.String())
	}
	if code := r.run("remove", "ghost@x.com", "--yes"); code != 1 || !strings.Contains(r.errb.String(), "No account ghost@x.com") {
		t.Fatalf("unknown: code=%d stderr=%q", code, r.errb.String())
	}
}
