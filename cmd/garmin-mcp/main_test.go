package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
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
	r := newRig(t, "", false)
	// The SDK drops in-flight replies on stdin EOF, so keep stdin open until the reply arrives, like a real client.
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	r.env.Stdin, r.env.Stdout = inR, outW
	done := make(chan int)
	go func() { code := r.run(); outW.Close(); done <- code }()
	go inW.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}` + "\n"))
	reply, err := bufio.NewReader(outR).ReadString('\n')
	go io.Copy(io.Discard, outR)
	inW.Close()
	if code := <-done; code != 0 || err != nil {
		t.Fatalf("code=%d err=%v stderr=%q", code, err, r.errb.String())
	}
	if !strings.Contains(reply, `"serverInfo":{"name":"garmin-mcp"`) || r.errb.Len() != 0 || r.count("/mobile/api/login") != 0 {
		t.Fatalf("reply=%q stderr=%q", reply, r.errb.String())
	}
}

func TestServeWritesLogFile(t *testing.T) {
	r := newRig(t, "", false)
	if code := r.run("serve"); code != 0 { // empty stdin: starts and stops at once
		t.Fatalf("code=%d stderr=%q", code, r.errb.String())
	}
	b, err := os.ReadFile(filepath.Join(r.home, "garmin-mcp.log"))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`^\d{4}/\d\d/\d\d \d\d:\d\d:\d\d\.\d{6} pid=\d+ garmin-mcp dev started\n.* pid=\d+ garmin-mcp stopped\n$`)
	if !re.Match(b) {
		t.Fatalf("log file = %q", b)
	}
}

func TestOpenLogFileRotatesLargeLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "x.log")
	getenv := func(k string) string { return map[string]string{"GARMIN_MCP_LOG_FILE": path}[k] }
	f, err := openLogFile("unused", getenv)
	if err != nil {
		t.Fatal(err)
	}
	f.Truncate(maxLogSize + 1)
	f.Close()
	if f, err = openLogFile("unused", getenv); err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || fi.Size() != 0 {
		t.Fatalf("new log not empty: %v %v", fi, err)
	}
	if fi, err := os.Stat(path + ".1"); err != nil || fi.Size() != maxLogSize+1 {
		t.Fatalf("rotated log: %v %v", fi, err)
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

func TestAuthKeepsStoredChinaDomain(t *testing.T) {
	r := newRig(t, "", true)
	if err := r.store().SaveSettings("me@x.com", accounts.Settings{Domain: "garmin.cn"}); err != nil {
		t.Fatal(err)
	}
	r.save("me@x.com", time.Now().Add(-time.Hour))
	if code := r.run("auth", "--email", "me@x.com", "--force"); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, r.errb.String())
	}
	st, _ := r.store().LoadSettings("me@x.com")
	if st.Domain != "garmin.cn" || r.domains[0] != "garmin.cn" {
		t.Fatalf("settings=%+v domains=%v", st, r.domains)
	}
	if !strings.Contains(r.out.String(), "Using Garmin Connect China (garmin.cn)") {
		t.Fatalf("stdout = %q", r.out.String())
	}
}

func TestAuthNewAccountDefaultsToCom(t *testing.T) {
	r := newRig(t, "", true)
	if code := r.run("auth", "--email", "new@x.com"); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, r.errb.String())
	}
	st, _ := r.store().LoadSettings("new@x.com")
	if st.Domain != "garmin.com" || r.domains[0] != "garmin.com" || !strings.Contains(r.out.String(), "Using Garmin Connect (garmin.com)") {
		t.Fatalf("settings=%+v domains=%v out=%q", st, r.domains, r.out.String())
	}
}

func TestAuthUnreadableSettingsAborts(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod 000 does not block reads here")
	}
	r := newRig(t, "", true)
	if err := r.store().SaveSettings("me@x.com", accounts.Settings{Domain: "garmin.cn", FitDownloadDir: "/keep"}); err != nil {
		t.Fatal(err)
	}
	dir, _ := r.store().Dir("me@x.com")
	if err := os.Chmod(filepath.Join(dir, "settings.json"), 0); err != nil {
		t.Fatal(err)
	}
	if code := r.run("auth", "--email", "me@x.com"); code == 0 {
		t.Fatalf("code=0 stdout=%q", r.out.String())
	}
	os.Chmod(filepath.Join(dir, "settings.json"), 0o600)
	if got, err := r.store().LoadSettings("me@x.com"); err != nil || got.Domain != "garmin.cn" || got.FitDownloadDir != "/keep" {
		t.Fatalf("settings changed: %+v %v", got, err)
	}
	if !strings.Contains(r.errb.String(), "settings.json") {
		t.Fatalf("stderr = %q", r.errb.String())
	}
}

func TestInterruptDuringAuthRestoresTerminalAndExits130(t *testing.T) {
	sigs := make(chan os.Signal, 1)
	var errb bytes.Buffer
	var restored, cancelled bool
	code := -1
	sigs <- os.Interrupt
	watchInterrupt(sigs, false, func() { cancelled = true }, func() { restored = true }, &errb, func(c int) { code = c })
	if !restored || cancelled || code != 130 || errb.String() != "\nInterrupted.\n" {
		t.Fatalf("restored=%v cancelled=%v code=%d stderr=%q", restored, cancelled, code, errb.String())
	}
}

func TestInterruptDuringServeCancelsContext(t *testing.T) {
	sigs := make(chan os.Signal, 1)
	var restored, cancelled, exited bool
	sigs <- os.Interrupt
	watchInterrupt(sigs, true, func() { cancelled = true }, func() { restored = true }, io.Discard, func(int) { exited = true })
	if !cancelled || restored || exited {
		t.Fatalf("cancelled=%v restored=%v exited=%v", cancelled, restored, exited)
	}
}

func TestAuthAnnouncesLoginAndDefaultEnvLogsToStderr(t *testing.T) {
	r := newRig(t, "", true)
	if code := r.run("auth", "--email", "new@x.com"); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, r.errb.String())
	}
	if !strings.Contains(r.out.String(), "Logging in... this can take a minute if Garmin is rate limiting") {
		t.Fatalf("stdout = %q", r.out.String())
	}
	if l := defaultEnv().Login; l == nil || l.Log != os.Stderr {
		t.Fatalf("defaultEnv().Login = %+v", l)
	}
}
