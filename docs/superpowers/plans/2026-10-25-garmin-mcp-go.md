# Garmin Connect MCP (Go) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A single static Go binary, `garmin-mcp`, that serves all 157 tools of `taxuspt/garmin_mcp` (plus `list_accounts`) over stdio MCP, for any number of Garmin accounts at once. Accounts are logged in through a terminal wizard.

**Architecture:** `internal/garmin` is a port of python-garminconnect 0.3.2. It holds the SSO login chain (utls for TLS fingerprints), the DI token refresh, and the API methods. `internal/accounts` stores one folder per email. `internal/server` builds the MCP server: a generic `addTool` injects the `email` argument, resolves it to a cached per-account `garmin.Client`, and applies filtering, annotations and the call timeout. `cmd/garmin-mcp` dispatches the subcommands (`serve`, `auth`, `accounts`, `remove`, `version`).

**Tech Stack:** Go 1.27, `github.com/modelcontextprotocol/go-sdk` v1.8.0, `github.com/refraction-networking/utls` v1.8.2, `golang.org/x/net/http2`, `golang.org/x/term`, `github.com/muktihari/fit` v0.28.5. Everything else uses the standard library.

**Spec:** `docs/superpowers/specs/2026-10-25-garmin-mcp-go-design.md`

**Python reference (read-only, used as the porting spec):** clone into a scratch dir, never into the repo:
```bash
git clone https://github.com/taxuspt/garmin_mcp $REF/garmin_mcp && git -C $REF/garmin_mcp checkout a2ca5fcac34524b83814fe2fcf3d479f8a75983f
git clone --branch 0.3.2 https://github.com/cyberjunky/python-garminconnect $REF/python-garminconnect
```
Below, `PY/` means `$REF/garmin_mcp/src/garmin_mcp/`, `PYT/` means `$REF/garmin_mcp/tests/`, and `GC/` means `$REF/python-garminconnect/garminconnect/`.

## Global Constraints

- Module path `github.com/elgatito/garmin-connect-go-mcp`. Binary `garmin-mcp`. `CGO_ENABLED=0` must build.
- Release targets, exactly: `windows/amd64`, `windows/arm64`, `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`.
- Tool names, argument names, defaults, descriptions and result JSON shapes are copied from `PY/`. Where this plan and the Python disagree, the Python wins (spec §Reference sources).
- Every tool except `list_accounts` has a required `email` string argument. Its description is exactly: `Email of the Garmin Connect account to use. Call list_accounts to see configured accounts.`
- Email validation (normalize = trim + lowercase, then): regex `^[^@\s/\\:*?"<>|]+@[^@\s/\\:*?"<>|]+$`, must not start with `.`, must not contain `..`. Error text: `invalid email "<value>"`.
- Account files: `<base>/accounts/<email>/tokens.json` (keys `di_token`, `di_refresh_token`, `di_client_id`), `settings.json` (keys `domain`, `fit_download_dir`). Dirs `0700`, files `0600`. All writes go to a temp file and are then renamed into place.
- `<base>` = `$GARMIN_MCP_HOME`, else `os.UserHomeDir()/.garmin-mcp`.
- Exact error copy:
  - unknown account: `No Garmin account "<email>" is set up. Ask the user to run "garmin-mcp auth --email <email>" in a terminal. Configured accounts: <a, b | none>.`
  - expired session: `The Garmin session for "<email>" has expired. Ask the user to run "garmin-mcp auth --email <email> --force" in a terminal.`
- Token counts as expiring when `now > exp - 900s`. API timeout 15 s. Tool call timeout `GARMIN_MCP_CALL_TIMEOUT`, default 90 s, `<=0` disables it.
- Logs go to stderr only. Tokens and passwords never appear in logs, errors or tool results.
- Tool results: a string is returned as-is. Anything else is JSON with 2-space indent and HTML escaping off. A handler error becomes `IsError: true` with `err.Error()` as the text. Handlers build the same message prefixes the Python returns (e.g. `Error retrieving stats: ...`).
- Decode Garmin JSON into `any` (`map[string]any` / `[]any` / `float64`). Don't define per-endpoint structs unless curation logic needs typed access.
- No live Garmin calls in tests. Tests use `httptest`.

## Review Focus

1. **Two processes share one account** (Claude Desktop and Claude Code both running). When one refreshes and rotates the refresh token, the other must pick up the new tokens from `tokens.json`, not refresh with a dead token and log the user out. Pinned by `TestRefreshUsesNewerTokensFromStore` (Task 3).
2. **Parallel tool calls on one account when the token is about to expire.** Exactly one refresh request must go out, and all calls must succeed. Pinned by `TestConcurrentCallsRefreshOnce` (Task 3, run with `-race`).
3. **Hostile or odd `email` values from the AI** (`../x@y`, `a@b/../../etc`, `Bob@Example.COM `). Traversal must be rejected before any file access, and case and space variants must map to the same account. Pinned by `TestNormalize` (Task 2) and `TestToolRejectsBadEmail` (Task 8).
4. **Account folder with a missing or corrupt `tokens.json`** (interrupted login, hand edit). The tool must return the "run auth --force" message, not panic or show a raw JSON error. Pinned by `TestCorruptTokensMessage` (Task 8).
5. **Garmin answers an API call with HTML or an empty body** (Cloudflare challenge, maintenance). The error must name the HTTP status. Pinned by `TestNonJSONErrorBody` (Task 3).

---

## File Structure

```
go.mod, go.sum
cmd/garmin-mcp/main.go          run(): subcommand dispatch, tty rule, version
cmd/garmin-mcp/auth.go          auth wizard, accounts list, remove
cmd/garmin-mcp/main_test.go
internal/accounts/store.go      Store, Settings, Normalize, ErrNoAccount
internal/accounts/store_test.go
internal/garmin/tokens.go       Tokens, JWT exp parsing
internal/garmin/client.go       Endpoints, Client, Req, Do/DoJSON/GetJSON/Download/Upload, APIError, refresh
internal/garmin/profile.go      Profile (socialProfile + user settings), cached
internal/garmin/transport.go    utls RoundTripper (h2 / http1.1 by ALPN)
internal/garmin/sso.go          Login chain, mobile flow, MFA, DI exchange, constants
internal/garmin/sso_web.go      widget + portal flows
internal/garmin/api_<area>.go   ported GC/__init__.py methods, one file per tool area
internal/garmin/*_test.go
internal/server/server.go       Options, New, manager, Session, list_accounts, resources
internal/server/registry.go     Account, addTool, result rendering, timeout, filter
internal/server/profiles.go     ported PY/tool_profiles.py
internal/server/annotations.go  ported PY/tool_annotations.py
internal/server/writeerr.go     ported PY/write_errors.py
internal/server/templates.go    ported PY/workout_templates.py (5 resources)
internal/server/tools_<area>.go one file per PY module
internal/server/harness_test.go fake Garmin + in-memory MCP session helpers
internal/server/testdata/fixtures.json   exported from PYT/fixtures/garmin_responses.py
internal/server/testdata/*.fit           sample FIT files (Task 15)
.github/workflows/ci.yml, .github/workflows/release.yml
README.md
```

---

### Task 1: Scaffold, version command, CI and release workflows

**Files:**
- Create: `go.mod`, `cmd/garmin-mcp/main.go`, `cmd/garmin-mcp/main_test.go`, `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `.gitignore` (`/dist/`, `/garmin-mcp`, `/garmin-mcp.exe`)

**Interfaces:**
- Produces: `var version = "dev"` in package main. `func run(ctx context.Context, args []string, e env) int`. `type env struct { Stdin io.Reader; Stdout, Stderr io.Writer; Getenv func(string) string; IsTerminal func() bool; ReadPassword func() (string, error) }`. `func defaultEnv() env` uses `golang.org/x/term` (`term.IsTerminal` on stdin **and** stdout fds; `term.ReadPassword(stdin fd)`). Later tasks add fields to `env`.

- [ ] **Step 1: Write the failing test**

```go
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
	if !strings.Contains(errOut.String(), "unknown command \"bogus\"") { t.Fatal(errOut.String()) }
}
```

- [ ] **Step 2: Run** `go test ./cmd/...`. Expected: FAIL (no `run`).
- [ ] **Step 3: Implement** `go mod init github.com/elgatito/garmin-connect-go-mcp`, then `main()` → `os.Exit(run(ctx, os.Args[1:], defaultEnv()))` with `signal.NotifyContext` for SIGINT/SIGTERM. `run` handles `version`/`--version` and unknown commands for now (exit 2, usage to stderr).
- [ ] **Step 4: Write `ci.yml`.** On `push` and `pull_request`: `actions/checkout@v4`, `actions/setup-go@v5` with `go-version-file: go.mod`, `go vet ./...`, `go test -race ./...`. Then a loop over the six targets running `CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -o /dev/null ./cmd/garmin-mcp`.
- [ ] **Step 5: Write `release.yml`.** On `push: tags: ['v*']`, `permissions: contents: write`. The same setup steps, then a bash loop over the six targets. Each builds `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${GITHUB_REF_NAME}" -o dist/stage/garmin-mcp[.exe] ./cmd/garmin-mcp`, copies `README.md` and `LICENSE` in, and packs `dist/garmin-mcp_${GITHUB_REF_NAME}_${os}_${arch}.tar.gz` (`.zip` for windows, via `zip -j`). Then `cd dist && sha256sum *.tar.gz *.zip > checksums.txt`, then `gh release create "$GITHUB_REF_NAME" dist/*.tar.gz dist/*.zip dist/checksums.txt --generate-notes` with `GH_TOKEN: ${{ github.token }}`.
- [ ] **Step 6: Verify:** `go test ./... && for t in windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do CGO_ENABLED=0 GOOS=${t%/*} GOARCH=${t#*/} go build -o /dev/null ./cmd/garmin-mcp || exit 1; done`. Expected: PASS, no build errors.
- [ ] **Step 7: Commit** `feat: scaffold garmin-mcp binary with CI and release workflows`.

---

### Task 2: Account store

**Files:**
- Create: `internal/accounts/store.go`, `internal/accounts/store_test.go`, `internal/garmin/tokens.go` (only the `Tokens` struct for now)

**Interfaces:**
- Produces (package `garmin`): `type Tokens struct { DIToken string \`json:"di_token"\`; DIRefreshToken string \`json:"di_refresh_token"\`; DIClientID string \`json:"di_client_id"\` }`.
- Produces (package `accounts`):
  - `type Settings struct { Domain string \`json:"domain"\`; FitDownloadDir string \`json:"fit_download_dir"\` }`
  - `var ErrNoAccount = errors.New("account not found")`
  - `func DefaultBase(getenv func(string) string) (string, error)`
  - `func Normalize(email string) (string, error)`
  - `type Store struct { Base string }`
  - `func (s Store) Dir(email string) (string, error)` returns the normalized path. It doesn't create anything.
  - `func (s Store) LoadTokens(email string) (garmin.Tokens, error)`. A missing dir or file gives an error wrapping `ErrNoAccount`. Corrupt JSON or an empty `di_token` gives an error that does **not** wrap `ErrNoAccount`.
  - `func (s Store) SaveTokens(email string, t garmin.Tokens) error` creates the dir.
  - `func (s Store) LoadSettings(email string) (Settings, error)`. A missing file gives `Settings{Domain: "garmin.com"}`.
  - `func (s Store) SaveSettings(email string, st Settings) error`
  - `func (s Store) List() ([]string, error)`: sorted emails whose dir holds `tokens.json`. A missing base gives an empty list.
  - `func (s Store) Remove(email string) error`

- [ ] **Step 1: Write the failing tests**

```go
func TestNormalize(t *testing.T) {
	ok := map[string]string{" Bob@Example.COM ": "bob@example.com", "a+b@x.io": "a+b@x.io"}
	for in, want := range ok {
		got, err := Normalize(in)
		if err != nil || got != want { t.Errorf("%q: got %q %v", in, got, err) }
	}
	for _, bad := range []string{"", "nobody", "../x@y", "a@b/../../etc", `a\b@c`, ".a@b", "a..b@c", "a@b@c", "a b@c", "a@b:c"} {
		if _, err := Normalize(bad); err == nil || err.Error() != fmt.Sprintf("invalid email %q", bad) {
			t.Errorf("%q: err=%v", bad, err)
		}
	}
}
func TestTokensRoundTripAndPerms(t *testing.T)   // save → load equal; on non-windows dir mode 0700, file 0600; no *.tmp left in dir
func TestLoadTokensMissingIsErrNoAccount(t *testing.T)   // errors.Is(err, ErrNoAccount)
func TestLoadTokensCorruptIsNotErrNoAccount(t *testing.T) // "{" and `{"di_token":""}` → err != nil && !errors.Is(err, ErrNoAccount)
func TestSettingsDefault(t *testing.T)            // LoadSettings on fresh account → Domain "garmin.com"
func TestListAndRemove(t *testing.T)              // save B@x.com, a@x.com, plus a dir without tokens.json → List == [a@x.com b@x.com]; Remove("A@X.com") → List == [b@x.com]
func TestDefaultBase(t *testing.T)                // getenv GARMIN_MCP_HOME=/tmp/z → "/tmp/z"; unset → filepath.Join(home, ".garmin-mcp")
```

- [ ] **Step 2: Run** `go test ./internal/accounts/`. Expected: FAIL (undefined).
- [ ] **Step 3: Implement** `store.go`. Atomic write helper: `os.CreateTemp(dir, ".tmp-*")`, write, `Chmod(0600)`, `Close`, `os.Rename`. Remove the temp file on any error.
- [ ] **Step 4: Run** `go test ./internal/accounts/`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(accounts): per-email account store`.

---

### Task 3: Garmin API client (tokens, requests, refresh)

**Files:**
- Modify: `internal/garmin/tokens.go`
- Create: `internal/garmin/client.go`, `internal/garmin/profile.go`, `internal/garmin/client_test.go`, `internal/garmin/testing_test.go` (helper `fakeJWT(exp time.Time, clientID string) string`, an unsigned JWT with an `exp`/`client_id` payload)

**Interfaces:**
- Consumes: `garmin.Tokens` (Task 2).
- Produces:
  - `func (t Tokens) ExpiresSoon(now time.Time) bool`: true when `now > exp-900s`. False when there's no parseable `exp` (as in Python).
  - `func (t Tokens) Expiry() (time.Time, bool)`
  - `type Endpoints struct { SSO, Connect, ConnectAPI, DIToken string }`. `func DefaultEndpoints(domain string) Endpoints` → `https://sso.<d>`, `https://connect.<d>`, `https://connectapi.<d>`, and `DIToken` fixed at `https://diauth.garmin.com/di-oauth2-service/oauth/token` for every domain (as in GC/client.py).
  - `type TokenStore interface { Load() (Tokens, error); Save(Tokens) error }`
  - `func NewClient(ep Endpoints, ts TokenStore, hc *http.Client) (*Client, error)`: loads tokens from `ts`. If `hc` is nil, it uses an `http.Client` with the standard transport.
  - `type Req struct { Method, Path string; Query url.Values; JSON any; Body []byte; ContentType string; Header http.Header; Timeout time.Duration }`
  - `func (c *Client) Do(ctx context.Context, r Req) ([]byte, error)`: 204 gives `[]byte("{}")`.
  - `func (c *Client) DoJSON(ctx context.Context, r Req) (any, error)`: an empty body gives `nil`.
  - `func (c *Client) GetJSON(ctx context.Context, path string, q url.Values) (any, error)`
  - `func (c *Client) Download(ctx context.Context, path string, q url.Values) ([]byte, error)`: sets `Accept: */*`.
  - `func (c *Client) Upload(ctx context.Context, path, field, filename string, data []byte) (any, error)`: multipart POST.
  - `func (c *Client) Domain() string`: derived from `Endpoints.Connect`'s host minus `connect.`.
  - `type APIError struct { Status int; Message string }`. `Error()` gives `API Error <status>` or `API Error <status> - <message>`. The message is extracted exactly like `_run_request` in GC/client.py: `message` / `content` / `detailedImportResult.failures[0].messages[0]`, else the whole JSON. For a non-JSON body under 500 chars, the body text.
  - `var ErrSessionExpired = errors.New("garmin session expired")`: returned (wrapped) when refresh gets a 4xx.
  - `func (c *Client) Profile(ctx context.Context) (Profile, error)`: `type Profile struct { DisplayName, FullName, UnitSystem string }`. Cached after the first success. Sources: `/userprofile-service/socialProfile` (`displayName`, `fullName`) and the user-settings URL from GC/__init__.py (`userData.measurementSystem`).
- Request rules (port of `_run_request` / `get_api_headers` / `_refresh_di_token`): URL `ConnectAPI + "/" + strings.TrimLeft(path, "/")`. Headers are the native header set from GC/client.py `_native_headers` plus `Authorization: Bearer`, `Accept: application/json`, with `Req.Header` merged on top. Before the request, if `ExpiresSoon`, call `refresh()`. On 401, call `refresh()` once and retry. `refresh()` holds a per-client mutex. After taking the lock, it calls `ts.Load()`. If the stored `DIToken` differs from the in-memory one and isn't expiring, it adopts that and returns. Otherwise it POSTs `grant_type=refresh_token` to `DIToken` (Basic auth `client_id:`), adopts the new pair (keeping the old refresh token if none is returned), and calls `ts.Save`. A save error is logged to stderr as `garmin: saving refreshed tokens failed: <err>` (no token values) and the call goes on.

- [ ] **Step 1: Write the failing tests** (an `httptest.Server` plays connectapi and diauth, and a `memStore` implements `TokenStore`)

```go
func TestExpiresSoon(t *testing.T)               // exp=now+10m → true; now+20m → false; "garbage" → false
func TestGetJSONSendsBearerAndNativeHeaders(t *testing.T) // server asserts Authorization "Bearer <tok>", X-Garmin-Client-Platform "Android"; returns {"a":1} → map a=1
func TestNoContentIsEmptyObject(t *testing.T)     // 204 → Do returns "{}"
func TestAPIErrorMessage(t *testing.T)            // 400 {"message":"bad date"} → err.Error()=="API Error 400 - bad date"; errors.As(*APIError) Status 400
func TestNonJSONErrorBody(t *testing.T)           // 503 "<html>busy</html>" → "API Error 503 - <html>busy</html>"; 502 empty body → "API Error 502"
func TestRefreshOn401(t *testing.T)               // first call 401, refresh returns new token, retry with new bearer succeeds; memStore.saved == new tokens
func TestRefreshWhenExpiring(t *testing.T)        // token exp now+1m → refresh before request; request carries new token
func TestRefreshUsesNewerTokensFromStore(t *testing.T) // in-memory token expiring; store already holds a different fresh token → no POST to diauth, request uses stored token
func TestConcurrentCallsRefreshOnce(t *testing.T) // expiring token, 10 goroutines GetJSON → diauth hit exactly 1 time, all succeed
func TestRefreshRejectedIsSessionExpired(t *testing.T) // diauth 400 → errors.Is(err, ErrSessionExpired)
func TestProfileCached(t *testing.T)              // two Profile() calls → socialProfile requested once; fields mapped
```

- [ ] **Step 2: Run** `go test -race ./internal/garmin/`. Expected: FAIL.
- [ ] **Step 3: Implement** `tokens.go`, `client.go` and `profile.go` per the Interfaces block.
- [ ] **Step 4: Run** `go test -race ./internal/garmin/`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(garmin): API client with DI token refresh`.

---

### Task 4: utls transport

**Files:**
- Create: `internal/garmin/transport.go`, `internal/garmin/transport_test.go`
- Modify: `go.mod` (add `github.com/refraction-networking/utls@v1.8.2`, `golang.org/x/net`)

**Interfaces:**
- Produces: `func newUTLSClient(hello utls.ClientHelloID, roots *x509.CertPool, jar http.CookieJar) *http.Client`. Its `http.RoundTripper` dials TCP, runs `utls.UClient(conn, &utls.Config{ServerName: host, RootCAs: roots}, hello)` with `HandshakeContext`, and then picks by `ConnectionState().NegotiatedProtocol`: `"h2"` goes through `(&http2.Transport{}).NewClientConn(uconn)`, anything else writes HTTP/1.1 over the conn (`req.Write` + `http.ReadResponse`, `Connection: close`). No connection pooling: SSO login makes a handful of requests. `roots == nil` means system roots.

- [ ] **Step 1: Write the failing tests**

```go
func TestUTLSHTTP2(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, r.Proto) }))
	srv.EnableHTTP2 = true
	srv.StartTLS(); defer srv.Close()
	pool := x509.NewCertPool(); pool.AddCert(srv.Certificate())
	resp, err := newUTLSClient(utls.HelloChrome_Auto, pool, nil).Get(srv.URL)
	// expect err==nil, body "HTTP/2.0"
}
func TestUTLSHTTP1(t *testing.T) // httptest.NewTLSServer (no h2) + HelloIOS_Auto → body "HTTP/1.1"
func TestUTLSCookieJar(t *testing.T) // server sets cookie on /a, /b echoes it; same client with cookiejar → echoed
```

Note: httptest certs are for `127.0.0.1` / `example.com`. Set `ServerName` from the URL host (IP), which Go's verifier accepts against the IP SAN.

- [ ] **Step 2: Run** `go test ./internal/garmin/ -run UTLS`. Expected: FAIL.
- [ ] **Step 3: Implement** `transport.go`.
- [ ] **Step 4: Run** `go test -race ./internal/garmin/ -run UTLS`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(garmin): utls transport with h2/http1.1 by ALPN`.

---

### Task 5: SSO login — mobile flow, MFA, DI exchange, strategy chain

**Files:**
- Create: `internal/garmin/sso.go`, `internal/garmin/sso_test.go`

**Interfaces:**
- Consumes: `Endpoints`, `Tokens`, `newUTLSClient` (Tasks 3–4).
- Produces:
  - `type MFAPrompt func(ctx context.Context) (string, error)`
  - `type LoginOptions struct { Strategies []string; RootCAs *x509.CertPool; Sleep func(time.Duration); Log io.Writer }`. `Strategies` nil means all in chain order. `Sleep` nil means `time.Sleep`. `Log` nil means `io.Discard`.
  - `func Login(ctx context.Context, ep Endpoints, email, password string, mfa MFAPrompt, opt *LoginOptions) (Tokens, error)`
  - `var ErrInvalidCredentials = errors.New("invalid username or password")`, `var ErrTooManyRequests = errors.New("rate limited by Garmin (429)")`
  - Strategy names: `mobile+utls`, `mobile+plain` (this task), `widget+utls`, `portal+utls`, `portal+plain` (Task 6 registers them in the same ordered table).
- Port from GC/client.py: constants (`IOS_SSO_CLIENT_ID`, `IOS_SERVICE_URL`, `IOS_LOGIN_UA`, `PORTAL_*`, `DI_TOKEN_URL`, `DI_GRANT_TYPE`, `DI_CLIENT_IDS`, the native header values) verbatim. `login()` chain semantics: `ErrInvalidCredentials` and an MFA request stop the chain, every other error moves on. If every strategy got a 429, return `ErrTooManyRequests`. Otherwise return `all login strategies failed: <last err>`. `_do_mobile_login`, `_complete_mfa` (flow endpoint, then the alternate endpoint), `_exchange_service_ticket` (DI client IDs in order; `di_client_id` comes from the JWT's `client_id` claim when present). `mobile+utls` rotates `HelloIOS_Auto`, `HelloSafari_Auto`, `HelloChrome_120`. The DI token POST uses the plain TLS client. Python uses curl_cffi `chrome` there; use `newUTLSClient(HelloChrome_Auto)` with a fallback to plain on a transport error.
- `mfa` nil while Garmin asks for MFA → error `MFA required but no MFA prompt available`.

- [ ] **Step 1: Write the failing tests.** A fake SSO+diauth `httptest.NewServer`. Endpoints point at it. `Strategies: []string{"mobile+plain"}` unless stated.

```go
func TestMobileLoginSuccess(t *testing.T)      // POST /mobile/api/login (assert query clientId=GCM_IOS_DARK, JSON username/password) → {"responseStatus":{"type":"SUCCESSFUL"},"serviceTicketId":"ST-1"}; diauth form has service_ticket=ST-1, grant_type=DI_GRANT_TYPE → tokens returned; DIClientID from JWT claim
func TestMobileLoginInvalidPassword(t *testing.T) // type INVALID_USERNAME_PASSWORD → errors.Is(err, ErrInvalidCredentials); chain with two strategies stops after first (second handler never hit)
func TestMobileLoginMFA(t *testing.T)          // MFA_REQUIRED (customerMfaInfo.mfaLastMethodUsed "email") → prompt called once, POST /mobile/api/mfa/verifyCode body has mfaVerificationCode "123456", mfaMethod "email" → SUCCESSFUL → tokens
func TestMFAFallsBackToPortalEndpoint(t *testing.T) // mobile verifyCode returns 429 → /portal/api/mfa/verifyCode tried and succeeds
func TestDIExchangeTriesClientIDs(t *testing.T) // diauth 400 for first client id, 200 for second → success, two POSTs
func TestChainMovesOnAfter429(t *testing.T)    // Strategies {"mobile+utls","mobile+plain"}; ep.SSO points at an httptest TLS server (RootCAs set) whose login handler returns 429 on the first request and SUCCESSFUL on the second → tokens returned, two login requests seen
func TestAllRateLimited(t *testing.T)          // all selected strategies 429 → errors.Is(err, ErrTooManyRequests)
func TestMFANoPrompt(t *testing.T)             // mfa=nil + MFA_REQUIRED → error text above
```

- [ ] **Step 2: Run** `go test ./internal/garmin/ -run 'Login|MFA|DIExchange|Chain|RateLimited'`. Expected: FAIL.
- [ ] **Step 3: Implement** `sso.go`. Keep the strategy table as `[]struct{ name string; run func(...) (Tokens, error) }` so Task 6 can append to it. A strategy's endpoint base comes from `ep.SSO`, so tests can point both utls and plain strategies at fakes.
- [ ] **Step 4: Run** the same command. Expected: PASS.
- [ ] **Step 5: Commit** `feat(garmin): SSO mobile login with MFA and DI token exchange`.

---

### Task 6: SSO fallback strategies — widget and portal

**Files:**
- Create: `internal/garmin/sso_web.go`, `internal/garmin/sso_web_test.go`
- Modify: `internal/garmin/sso.go` (append strategies to the table)

**Interfaces:**
- Consumes: the strategy table, `LoginOptions.Sleep`, `newUTLSClient` (Tasks 4–5).
- Produces: strategies `widget+utls` (`HelloChrome_Auto`), `portal+utls` (rotating `HelloSafari_Auto`, `HelloIOS_Auto`, `HelloChrome_120`, `HelloEdge_Auto`, `HelloChrome_Auto`) and `portal+plain`. They're ports of `_widget_web_login`, `_complete_mfa_widget`, `_do_portal_web_login` and the portal branch of `_complete_mfa`. The same `_CSRF_RE` / `_TITLE_RE` regexes are used. Delays (`LOGIN_DELAY_*` 10–20 s portal, `WIDGET_DELAY_*` 3–8 s) go through `opt.Sleep`. The browser User-Agent is the static `DESKTOP_USER_AGENT` (python's `ua_generator` is optional; skip it).

- [ ] **Step 1: Write the failing tests** (fake SSO serving the embed/sign-in HTML with `name="_csrf" value="tok"` and the expected redirects/JSON, `Sleep` recorded rather than slept)

```go
func TestWidgetLoginSuccess(t *testing.T)   // GET embed → GET signin (csrf) → POST signin with _csrf=tok, username, password → page title "Success" with embedded ticket → DI exchange → tokens
func TestWidgetLoginMFA(t *testing.T)       // title "MFA" path → prompt → verifyMFA post → success
func TestPortalLoginSuccess(t *testing.T)   // GET /portal/sso/en-US/sign-in, recorded Sleep in [10s,20s], POST /portal/api/login → SUCCESSFUL → tokens
func TestPortalInvalidPassword(t *testing.T) // → ErrInvalidCredentials
func TestDefaultChainOrder(t *testing.T)    // names of default table == []string{"mobile+utls","mobile+plain","widget+utls","portal+utls","portal+plain"}
```

- [ ] **Step 2: Run** `go test ./internal/garmin/ -run 'Widget|Portal|ChainOrder'`. Expected: FAIL.
- [ ] **Step 3: Implement** `sso_web.go`. Copy the request sequence, params and response checks of each Python function step by step.
- [ ] **Step 4: Run** `go test -race ./internal/garmin/`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(garmin): widget and portal SSO fallback strategies`.

---

### Task 7: CLI — auth wizard, accounts, remove, tty dispatch

**Files:**
- Create: `cmd/garmin-mcp/auth.go`
- Modify: `cmd/garmin-mcp/main.go`, `cmd/garmin-mcp/main_test.go`

**Interfaces:**
- Consumes: `accounts.Store`, `accounts.DefaultBase`, `accounts.Normalize`, `garmin.Login`, `garmin.LoginOptions`, `garmin.NewClient`, `(*Client).Profile`, `garmin.DefaultEndpoints`, `Tokens.Expiry` (Tasks 2–6).
- Produces: new `env` fields `Endpoints func(domain string) garmin.Endpoints` (default `garmin.DefaultEndpoints`) and `Login *garmin.LoginOptions`. Also `func serve(ctx context.Context, e env, st accounts.Store) int`, a stub returning 0 that Task 8 replaces.
- Behaviour (exact rules in spec §Binary and subcommands):
  - No args: terminal and no accounts → print `Welcome to garmin-mcp. Let's connect your first Garmin account.` and run `auth`. Terminal with accounts → print the `accounts` output and the usage, exit 0. Not a terminal → `serve`.
  - `auth [--email E] [--cn] [--force]`: reads the email line from `Stdin` if not given, then `ReadPassword`. The MFA prompt prints `Enter the MFA code Garmin sent you: ` and reads a line from `Stdin`. If tokens exist, aren't expiring and `--force` isn't set → `Account <email> already has a valid session. Use --force to log in again.`, exit 0. On success → save tokens, save settings (`domain` = `garmin.cn` if `--cn`, keeping any existing `fit_download_dir`), fetch `Profile`, then print `Logged in as <FullName> (<email>)` and `Saved to <dir>`. Exit 1 on failure, with `Login failed: <err>` on stderr.
  - `accounts`: one line per account: `<email>  <domain>  <status>`, where status is `ok` / `expired` (refresh may still work) / `invalid` (unreadable tokens). With none: `No accounts. Run "garmin-mcp auth" to add one.`
  - `remove <email> [--yes]`: prompts `Remove <email>? [y/N] ` unless `--yes`. Unknown email → stderr `No account <email>`, exit 1.

- [ ] **Step 1: Write the failing tests** (temp `GARMIN_MCP_HOME`, fake SSO+diauth+connectapi `httptest` server, `Login: &garmin.LoginOptions{Strategies: []string{"mobile+plain"}}`)

```go
func TestFirstRunStartsAuth(t *testing.T)       // IsTerminal true, no accounts, Stdin "Me@X.com\n", ReadPassword "pw" → exit 0; output contains "Welcome to garmin-mcp" and "Logged in as Test User (me@x.com)"; tokens.json exists under accounts/me@x.com
func TestAuthWithMFA(t *testing.T)              // fake returns MFA_REQUIRED; Stdin "me@x.com\n123456\n" → success; verifyCode received 123456
func TestAuthCNSavesDomain(t *testing.T)        // --cn → settings.json domain "garmin.cn"; Endpoints called with "garmin.cn"
func TestAuthExistingValidSkips(t *testing.T)   // pre-saved fresh token → message, no SSO request
func TestAuthBadPassword(t *testing.T)          // → exit 1, stderr "Login failed: invalid username or password"
func TestNoArgsWithAccountsPrintsList(t *testing.T)
func TestNoArgsNotTerminalServes(t *testing.T)  // IsTerminal false → serve stub called (exit 0, no prompts read)
func TestAccountsStatus(t *testing.T)           // ok / expired / invalid rows
func TestRemove(t *testing.T)                   // "y\n" removes; "n\n" keeps; --yes skips prompt; unknown → exit 1
```

- [ ] **Step 2: Run** `go test ./cmd/...`. Expected: FAIL.
- [ ] **Step 3: Implement** `auth.go` and the dispatch in `main.go` with the `flag` package (one `flag.FlagSet` per subcommand).
- [ ] **Step 4: Run** `go test -race ./cmd/...`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(cli): auth wizard, accounts and remove commands`.

---

### Task 8: MCP server core and user_profile tools

**Files:**
- Create: `internal/server/server.go`, `registry.go`, `profiles.go`, `annotations.go`, `writeerr.go`, `templates.go`, `tools_user_profile.go`, `harness_test.go`, `server_test.go`, `tools_user_profile_test.go`, `testdata/fixtures.json`
- Modify: `cmd/garmin-mcp/main.go` (the real `serve`), `go.mod` (go-sdk v1.8.0)

**Interfaces:**
- Consumes: `accounts.*`, `garmin.NewClient`, `garmin.Client`, `garmin.ErrSessionExpired`, `garmin.APIError`, `garmin.DefaultEndpoints`.
- Produces:
  - `type Options struct { Store accounts.Store; Endpoints func(domain string) garmin.Endpoints; Getenv func(string) string; Version string; Log io.Writer }`
  - `func New(opt Options) (*mcp.Server, error)`: an error for an unknown `GARMIN_TOOL_PROFILE` (message as in PY/tool_profiles.py). `serve` runs `srv.Run(ctx, &mcp.StdioTransport{})`.
  - `type Account struct { Email string \`json:"email" jsonschema:"Email of the Garmin Connect account to use. Call list_accounts to see configured accounts."\` }` with method `func (a Account) account() string`. Every tool input struct embeds `Account` as its **first** field. The schema inference flattens it, so `email` is first and required.
  - `type Session struct { Email string; Client *garmin.Client; Store accounts.Store }` with `func (s *Session) Settings() (accounts.Settings, error)`
  - `func addTool[In interface{ account() string }](r *registry, name, description string, h func(ctx context.Context, s *Session, in In) (any, error))`: normalizes and validates the email (the error text is the `Normalize` error), looks up the manager, applies the call timeout (`context.WithTimeout`; on deadline it returns the timeout message from PY/__init__.py `_GarminProxy`), calls `h`, and renders the result per Global Constraints. It skips registration when the filter rejects `name`, and records unannotated names.
  - `func addPlainTool[In any](r *registry, name, description string, h func(ctx context.Context, in In) (any, error))` for `list_accounts`.
  - `type writeFailure struct{ payload map[string]any }` implements `error`. The registry renders it as JSON with `IsError: true`. `func newWriteTracker(tool string, log io.Writer) *writeTracker` with `Sent()`, `Fail(err error) error`, a port of `WriteTracker` (stages, `write_committed` true/false/"unknown", `correlation_id` = random hex, logged with the error to `Log`). `upstream_status`/`upstream_body` come from `*garmin.APIError` (body truncated to 500 chars).
  - Manager: `func (m *manager) session(email string) (*Session, error)`. A cached `*garmin.Client` per normalized email, created under a mutex. The `TokenStore` adapter writes via `Store.SaveTokens`. Errors: `errors.Is(err, accounts.ErrNoAccount)` → the unknown-account copy (listing `Store.List()`). Corrupt tokens or `garmin.ErrSessionExpired` → the expired-session copy. The registry also maps `ErrSessionExpired` returned from a handler to that copy.
  - `list_accounts`: `{"accounts":[{"email","domain","display_name","token_status"}]}`. `display_name` comes from a cached `Profile` if that account's client already exists, otherwise `""` (no network). `token_status` uses the same rule as the CLI `accounts`.
  - Resources: the 5 `workout://...` URIs with their JSON bodies and names copied from PY/workout_templates.py, `MIMEType: "application/json"`.
  - `profiles.go`: the profile sets copied from PY/tool_profiles.py. `annotations.go`: the sets from PY/tool_annotations.py, plus `list_accounts` in the read-only set.
- Test harness (`harness_test.go`), used by every later tool task:
  - `type fakeGarmin struct { *httptest.Server; routes map[string]any; calls []recordedCall }` with `func newFakeGarmin(t *testing.T) *fakeGarmin` and `func (f *fakeGarmin) on(methodAndPath string, body any)`. The key is `"GET /usersummary-service/usersummary/daily/testuser"` (path only, no query). `body` is any JSON-marshalable value, `rawBody([]byte)`, or `status(code, body)`. Unknown routes return 404 `{"message":"no route"}`. `recordedCall{Method, Path string; Query url.Values; Body []byte}`. `/userprofile-service/socialProfile` → `{"displayName":"testuser","fullName":"Test User"}` by default.
  - `func newSession(t *testing.T, f *fakeGarmin, env map[string]string) *mcp.ClientSession`: temp store, one account `me@x.com` with a fresh `fakeJWT`, `Options.Endpoints` pointing at `f`, connected through `mcp.NewInMemoryTransports()`.
  - `func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (text string, isErr bool)`. `args` without `email` gets `"email": "me@x.com"` added.
  - `func fixture(t *testing.T, name string) any` reads `testdata/fixtures.json[name]`.
- `testdata/fixtures.json`: generated once (commit the output, not the script):
  ```bash
  cd $REF/garmin_mcp && python3 -c "import json; from tests.fixtures import garmin_responses as g; print(json.dumps({k: getattr(g,k) for k in dir(g) if k.startswith('MOCK_')}, indent=1, sort_keys=True))" > $REPO/internal/server/testdata/fixtures.json
  ```
- user_profile tools (6, PY/user_profile.py): `get_full_name`, `get_unit_system`, `get_user_profile`, `get_userprofile_settings`, `get_heart_rate_zones`, `set_heart_rate_zones`. Library methods they use go in `internal/garmin/api_user.go`, named in Go style after the GC method (e.g. `GetUserProfile(ctx) (any, error)`).

- [ ] **Step 1: Write the failing tests** (`server_test.go`)

```go
func TestEveryToolRequiresEmail(t *testing.T)   // ListTools: every tool except list_accounts has "email" in required and Properties["email"].Description == exact copy
func TestEveryToolAnnotated(t *testing.T)       // every listed tool has non-nil Annotations
func TestUnknownAccountMessage(t *testing.T)    // email "nobody@x.com" → isErr, text == exact unknown-account copy with "Configured accounts: me@x.com."
func TestToolRejectsBadEmail(t *testing.T)      // email "../../etc@x" → isErr, text `invalid email "../../etc@x"`; no file created outside temp base
func TestEmailCaseInsensitive(t *testing.T)     // "ME@X.COM" works like me@x.com
func TestCorruptTokensMessage(t *testing.T)     // write "{" to tokens.json → expired-session copy
func TestExpiredRefreshMessage(t *testing.T)    // token expiring, fake diauth 400 → expired-session copy
func TestTwoAccountsIsolated(t *testing.T)      // accounts a@x.com, b@x.com with different tokens; call get_full_name for each → fake saw matching bearer per call
func TestListAccounts(t *testing.T)             // → JSON accounts with me@x.com, domain garmin.com, token_status ok
func TestProfileFiltering(t *testing.T)         // GARMIN_TOOL_PROFILE=minimal → tool set == MINIMAL set ∪ {list_accounts} (for the tools registered so far); GARMIN_ENABLED_TOOLS / GARMIN_DISABLED_TOOLS cases ported from PYT/unit/test_tool_filter.py and test_tool_profiles.py
func TestUnknownProfileFails(t *testing.T)      // New returns error
func TestCallTimeout(t *testing.T)              // GARMIN_MCP_CALL_TIMEOUT=0.2, fake handler sleeps 1s → isErr, timeout message
func TestWriteFailurePayload(t *testing.T)      // cases from PYT/unit/test_write_errors.py
func TestWorkoutResources(t *testing.T)         // ListResources has the 5 URIs; ReadResource simple-run → JSON with workoutName "Simple Run"
```
And `tools_user_profile_test.go`: the user_profile test cases from `PYT/integration/test_other_modules_tools.py` (and any `PYT/unit` file that names these tools), one Go test per Python test, same fixtures and assertions.

- [ ] **Step 2: Run** `go test ./internal/server/`. Expected: FAIL.
- [ ] **Step 3: Implement** server core and user_profile tools. Wire the real `serve` in `cmd/garmin-mcp`: `New(Options{Store, Endpoints: e.Endpoints, Getenv: e.Getenv, Version: version, Log: e.Stderr})`. On error, print it and exit 1.
- [ ] **Step 4: Run** `go test -race ./...`. Expected: PASS.
- [ ] **Step 5: Smoke test** `printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}' | GARMIN_MCP_HOME=$(mktemp -d) go run ./cmd/garmin-mcp serve`. Expected: one JSON-RPC result line naming server `garmin-mcp`.
- [ ] **Step 6: Commit** `feat(server): multi-account MCP server core with user profile tools`.

---

### Tool porting tasks (9–15): common recipe

Each task ports one or more Python modules. **For every tool:** read the Python tool function in `PY/<module>.py` and every GC method it calls in `GC/__init__.py` (and `GC/workout.py` where used). Then:

1. Port the GC methods it uses into `internal/garmin/api_<area>.go` as `(c *Client) <GoName>(ctx, ...) (any, error)`, or `([]byte, error)` for downloads. Keep the same URL, query params, body and date validation (`_validate_date_format` etc. → shared helpers in `internal/garmin/validate.go`, created in the first task that needs them). A method used by more than one area lives in the file of the first task that ports it.
2. Port the tool into `internal/server/tools_<area>.go` with an input struct embedding `Account`. Field names are the Python argument names as `json` tags. Python defaults are applied in the handler when the field is zero or `nil`; use pointer fields where zero is a valid value distinct from "not given". `jsonschema` descriptions come from the Python docstring's Args section, and the tool description is the docstring summary. The result-curation logic follows the Python line by line.
3. Port the tests: one Go test per Python test function in the listed test files. Name it `Test<PythonNameInCamelCase>`, use the same fixtures (via `fixture()`), the same `fakeGarmin` routes the GC method would hit, and the same assertions on the result JSON or text.

Each task's steps are:
- [ ] Write the ported tests → `go test ./internal/server/ -run '<Area>'` → FAIL.
- [ ] Port the GC methods and tools.
- [ ] `go test -race ./...` → PASS. Add the area's tools to `TestEveryToolAnnotated`'s coverage (it reads ListTools, so nothing to change if annotations were copied completely).
- [ ] Commit `feat(tools): port <area> tools`.

### Task 9: health & wellness (33 tools)

**Files:** `internal/garmin/api_health.go`, `internal/garmin/validate.go`, `internal/server/tools_health.go`, `internal/server/tools_health_test.go`
**Source:** `PY/health_wellness.py`. Tests: `PYT/integration/test_health_wellness_tools.py`, `PYT/unit/test_recovery_time.py`.
**Tools:** get_stats get_stats_range get_energy_balance get_user_summary get_body_composition get_stats_and_body get_steps_data get_daily_steps get_training_readiness get_body_battery get_body_battery_events get_blood_pressure get_floors get_rhr_day get_heart_rates get_heart_rates_summary get_hydration_data get_sleep_data get_sleep_summary get_sleep_summary_range get_stress_data get_stress_summary get_respiration_data get_respiration_summary get_spo2_data get_all_day_stress get_all_day_events get_lifestyle_logging_data get_weekly_steps get_weekly_stress get_weekly_intensity_minutes get_morning_training_readiness get_recovery_time_remaining

### Task 10: activities and gear (23 + 3 tools)

**Files:** `internal/garmin/api_activities.go`, `internal/server/tools_activities.go`, `tools_gear.go`, tests alongside
**Source:** `PY/activity_management.py`, `PY/gear_management.py`. Tests: `PYT/integration/test_activity_management_tools.py`, the gear cases in `test_other_modules_tools.py`, `PYT/unit/test_delete_activity.py`, `test_training_effect.py` (activity parts).
**Tools:** get_activities_by_date get_activities_fordate get_activity set_activity_name set_activity_type set_activity_description set_activity_event_type set_perceived_effort set_activity_feel get_activity_splits get_activity_typed_splits get_activity_split_summaries get_activity_weather get_activity_hr_in_timezones get_activity_power_in_timezones get_activity_gear get_activity_exercise_sets set_activity_exercise_sets count_activities get_activities create_manual_activity delete_activity get_activity_types · get_gear add_gear_to_activity remove_gear_from_activity
**Must keep:** the `delete_activity` two-call confirm gate (preview, then `confirm=true`), with the deletion logged to stderr. Writes use `writeTracker`.

### Task 11: training (18 tools)

**Files:** `internal/garmin/api_training.go`, `internal/server/tools_training.go`, test
**Source:** `PY/training.py`. Tests: `PYT/integration/test_training_tools.py`, `PYT/unit/test_training_effect.py`, `test_training_vo2.py`, `test_training_activity_types.py`, `test_training_device_selection.py`.
**Tools:** get_progress_summary_between_dates get_hill_score get_endurance_score get_training_effect get_hrv_data get_fitnessage_data get_training_status get_cycling_ftp get_lactate_threshold request_reload get_training_load_trend get_training_load_balance get_hrv_trend get_vo2max_trend get_respiration_trend get_running_tolerance get_running_tolerance_trend get_acclimation

### Task 12: workouts, calendar, workout builders (15 + 1 + 6 tools)

**Files:** `internal/garmin/api_workouts.go`, `internal/server/tools_workouts.go`, `tools_calendar.go`, `tools_workout_builders.go`, tests
**Source:** `PY/workouts.py`, `PY/calendar_events.py`, `PY/workout_builders.py`, `GC/workout.py`. Tests: `PYT/integration/test_workouts_tools.py`, `test_calendar_events_tools.py`, `test_workout_builders_tools.py`, `PYT/unit/test_workout_builders.py`, `test_workout_preview.py`, `test_workout_templates.py`, `test_tool_input_schemas.py` (workout parts).
**Tools:** get_workouts get_workout_by_id download_workout upload_workout upload_workouts delete_workout delete_workouts get_scheduled_workouts get_garmin_coach_workouts get_training_plan_workouts schedule_workout schedule_workouts reschedule_workout unschedule_workout unschedule_workouts · get_calendar_events · create_walk_run_workout create_run_workout create_run_interval_workout create_z2_walk_workout create_strength_workout schedule_week
**Must keep:** the end-condition / target-type validation and the nested `targetValueOne/Two` repair-or-reject logic described in the Python README and `PY/workouts.py`.

### Task 13: nutrition (15 tools)

**Files:** `internal/garmin/api_nutrition.go`, `internal/server/tools_nutrition.go`, test
**Source:** `PY/nutrition.py`. Tests: `PYT/integration/test_nutrition_tools.py`, `PYT/unit/test_nutrition_target_date.py`.
**Tools:** get_nutrition_daily_food_log get_nutrition_summary_between_dates get_nutrition_daily_meals get_nutrition_daily_settings set_nutrition_daily_settings search_foods get_custom_foods get_custom_food_serving_units create_custom_food update_custom_food delete_custom_food log_custom_food log_food delete_food_log upsert_and_log

### Task 14: challenges, devices, weight, data management, women's health, courses (9 + 7 + 5 + 3 + 3 + 5 tools)

**Files:** `internal/garmin/api_misc.go`, `internal/server/tools_challenges.go`, `tools_devices.go`, `tools_weight.go`, `tools_data.go`, `tools_womens_health.go`, `tools_courses.go`, tests
**Source:** the matching `PY/*.py`. Tests: `PYT/integration/test_challenges_tools.py`, `test_courses_tools.py`, the remaining cases of `test_other_modules_tools.py`, `PYT/unit/test_goals.py`, `test_course_preview.py`.
**Tools:** get_goals get_personal_record get_earned_badges get_adhoc_challenges get_available_badge_challenges get_badge_challenges get_non_completed_badge_challenges get_race_predictions get_inprogress_virtual_challenges · get_devices get_device_last_used get_device_settings get_primary_training_device get_device_solar_data get_device_alarms push_workout_to_device · get_weigh_ins get_daily_weigh_ins delete_weigh_ins add_weigh_in add_weigh_in_with_timestamps · add_body_composition set_blood_pressure add_hydration_data · get_pregnancy_summary get_menstrual_data_for_date get_menstrual_calendar_data · get_courses get_course_details download_course_gpx upload_course delete_course
**Note:** course URLs use `https://connect.<Session.Client.Domain()>/modern/course/<id>`.

### Task 15: activity analysis and FIT files (5 tools)

**Files:** `internal/server/tools_activity_analysis.go`, `internal/server/fit.go`, tests, `internal/server/testdata/*.fit`. Modify `go.mod` (add `github.com/muktihari/fit@v0.28.5`).
**Source:** `PY/activity_analysis.py`. Tests: `PYT/integration/test_activity_analysis_tools.py`.
**Tools:** get_activity_fit_messages get_activity_fit_data get_power_duration_curve download_activity_file set_fit_download_dir
**Interfaces:** `func decodeFIT(raw []byte) ([]fitMessage, error)` with `type fitMessage struct { Name string; Fields []fitField }` and `type fitField struct { Name string; Value any; Units string }`. The ZIP extraction ports `_extract_fit_bytes`: Garmin's original-file download is a ZIP holding the `.fit`. Message names come from `typedef.MesgNum.String()` lowercased to fitparse's snake_case (`file_id`, `record`, `session`, `lap`...). Unknown or manufacturer messages are `unknown_<num>`, and unknown fields are `unknown_<num>`, matching fitparse. Values are scaled/offset per profile (`Field.Value` with `Scale`/`Offset` applied), and `date_time` fields become ISO-8601 UTC strings as in `PY` `_json_value`.
**Test data:** copy `testdata/from_official_sdk/Activity.fit` and `ActivityDevFields.fit` from the `muktihari/fit` v0.28.5 module into `internal/server/testdata/`, along with `LICENSE-FIT-SDK` (FIT SDK license note), plus any FIT fixture the Python tests build inline.
**Download dir:** precedence `output_dir` > `GARMIN_FIT_DOWNLOAD_DIR` > `Session.Settings().FitDownloadDir` > `{"status":"needs_setup", ...}` (copy from Python). `set_fit_download_dir` saves to the account's `settings.json` via `Store.SaveSettings`.
**Extra test:** `TestFitMessageNamesMatchFitparse`: for `Activity.fit`, the message counts by name include `file_id`, `record`, `session`, `lap`, `event`, `device_info`. **Must keep:** the `message_offset >= 0` and `1 <= message_limit <= 5000` validation copy.

---

### Task 16: Parity check, README, live smoke checklist

**Files:**
- Create: `internal/server/parity_test.go`
- Modify: `README.md`

**Interfaces:**
- Consumes: the full server from Tasks 8–15.

- [ ] **Step 1: Write the failing test**

```go
// pythonTools is the 157-name list from the spec (copied from PY @ a2ca5fc).
var pythonTools = strings.Fields(`get_activity_fit_messages get_activity_fit_data ... unschedule_workouts`) // all 157, alphabetical
func TestToolParity(t *testing.T) {
	// ListTools with no env filters → names == pythonTools ∪ {"list_accounts"}; len == 158
}
func TestProfilesReferenceRealTools(t *testing.T) // every name in every profile is registered (port of PYT/unit/test_tool_profiles.py membership check)
```
Generate the list with `grep -A3 "@app.tool" $REF/garmin_mcp/src/garmin_mcp/*.py | grep -o "def [a-z_0-9]*" | sed 's/def //' | sort`. Expected: 157 names.

- [ ] **Step 2: Run** `go test ./internal/server/ -run 'Parity|ProfilesReference'`. Expected: PASS if Tasks 8–15 are complete. Otherwise the diff names the missing tools; port them in their area task, then re-run.
- [ ] **Step 3: Write README.md.** Sections: what it is; install (download from Releases, unpack, macOS `xattr -d com.apple.quarantine garmin-mcp`, Windows SmartScreen "More info → Run anyway"); first login (`garmin-mcp` / `garmin-mcp auth`, MFA, `--cn`); more accounts (`garmin-mcp auth` again, `accounts`, `remove`); client config for Claude Desktop (`{"mcpServers":{"garmin":{"command":"/path/to/garmin-mcp"}}}`) and Claude Code (`claude mcp add garmin /path/to/garmin-mcp`); asking for data ("use my account me@x.com"); env vars table from the spec; the tool list by area; differences from the Python original (multi-account, `email` argument, `list_accounts`, stdio only, no env credentials); building from source; releasing (`git tag vX.Y.Z && git push --tags`).
- [ ] **Step 4: Verify the whole thing:** `go vet ./... && go test -race ./...` plus the six-target build loop from Task 1. Expected: all PASS.
- [ ] **Step 5: Live smoke checklist** (manual, by the human, before tagging `v0.1.0`; record the results in the PR description):
  1. `garmin-mcp auth` with a real non-MFA account → `Logged in as ...`.
  2. `garmin-mcp auth` with an MFA account → code prompt → success.
  3. `garmin-mcp accounts` lists both as `ok`.
  4. In Claude Desktop: `list_accounts`, `get_stats` for each account on today's date, `get_activities` limit 3, `download_activity_file` → a file appears.
  5. Edit `tokens.json` `di_token` to an expired JWT → the next tool call refreshes and the file changes.
- [ ] **Step 6: Commit** `docs: README and tool parity test`.
