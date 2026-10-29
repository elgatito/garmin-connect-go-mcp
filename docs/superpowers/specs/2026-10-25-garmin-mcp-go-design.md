# Garmin Connect MCP server in Go: design

Date: 2026-10-25
Repo: `github.com/elgatito/garmin-connect-go-mcp` (Apache-2.0)

## Intent

Port the Python MCP server [taxuspt/garmin_mcp](https://github.com/taxuspt/garmin_mcp) to Go. It should ship as a single
self-contained binary that people download for their platform and run locally. The main difference from the original
is that one running server serves **several Garmin accounts at the same time**. Every tool takes the account's `email`
and uses that account's saved session.

Why: the original binds one process to one account and needs a Python toolchain (`uvx`). People with more than one
account (family members, a coach with athletes, a test account) have to run one server per account.

## Reference sources (port from these exact versions)

| What | Where | Pinned commit |
|---|---|---|
| MCP server (tools, curation, profiles, annotations, write errors) | `github.com/taxuspt/garmin_mcp` `src/garmin_mcp/` | `a2ca5fcac34524b83814fe2fcf3d479f8a75983f` |
| Garmin API client and login engine | `github.com/cyberjunky/python-garminconnect` tag `0.3.2`: `garminconnect/client.py` (auth), `garminconnect/__init__.py` (endpoints), `garminconnect/workout.py` | `30d90410947d541ecf3d767be11213f5f8645f63` |
| Test fixtures | garmin_mcp `tests/fixtures/garmin_responses.py`, `tests/integration/*.py`, `tests/unit/*.py` | same as server |

When this document and the Python source disagree about a tool's behaviour, the Python source wins. This document
only records where the Go port deliberately differs.

## Goals

1. **Tool parity:** all 157 tools of the original, with the same names, argument names, defaults, descriptions and
   result shapes (JSON). Every tool also takes a required `email` string argument.
2. **Resource parity:** the 5 workout template resources (`workout://templates/*`, `workout://reference/structure`).
3. **Multi-account:** one server process serves any number of accounts at the same time. Each account has its own folder.
4. **Terminal-only login:** email, password and MFA are entered in a terminal, never through the AI.
5. **Release binaries:** for Windows x64/arm64, Linux x64/arm64 and macOS x64/arm64, attached to a GitHub Release on
   each version tag.
6. **Robust login:** Garmin's current SSO flow (mobile sign-in → service ticket → DI OAuth2 token with refresh), with
   connection-level (TLS) impersonation through `utls` to get past Cloudflare.

## Non-goals

- HTTP/SSE transports, the HTTP bearer token, Docker, Kubernetes probes. stdio only.
- `.dxt` / `.mcpb` Claude Desktop extension packaging (could come later).
- Login from inside the chat (no `login` / `submit_mfa` tools).
- `GARMIN_EMAIL`/`GARMIN_PASSWORD`(`_FILE`), `GARMINTOKENS`, `GARMINTOKENS_BASE64`, `GARMIN_IS_CN` environment variables.
- The JWT_WEB cookie fallback from python-garminconnect. The library never persists it, so a terminal login could never
  hand it to the server.
- Code signing / notarization of macOS and Windows binaries.

## Anti-goals (a failure even if it technically works)

- One account's request ever using another account's tokens.
- A tool argument (`email`) that can make the server read or write outside its accounts folder.
- A token refresh that loses the rotated refresh token, which silently logs the user out (non-atomic write, or a write
  that fails without saying so).
- Credentials or tokens in logs, tool results or error messages.
- A binary that needs CGO, a runtime or extra files to run.

## User-facing behaviour

### Binary and subcommands

Binary name: `garmin-mcp`.

| Invocation | Behaviour |
|---|---|
| `garmin-mcp` with stdin **and** stdout a terminal, no accounts yet | Prints a one-line welcome, then runs the `auth` wizard. |
| `garmin-mcp` with stdin and stdout a terminal, accounts exist | Prints the configured accounts and the usage text. Does not start the server. |
| `garmin-mcp` with stdin or stdout not a terminal (started by an MCP client) | Runs the MCP server over stdio. |
| `garmin-mcp serve` | Runs the MCP server over stdio, whatever stdin is. |
| `garmin-mcp auth [--email E] [--cn] [--force]` | Login wizard. Prompts for email (unless `--email`), password (hidden input), and the MFA code if Garmin asks for one. Saves the account. If the account already has working tokens and `--force` is not set, says so and exits 0. `--cn` uses Garmin Connect China (`garmin.cn`). |
| `garmin-mcp accounts` | Lists accounts: email, domain, display name, token status (`ok` / `expired` / `invalid`). Checks token status offline from the JWT `exp`; no network call. |
| `garmin-mcp remove <email>` | Deletes that account's folder after a `y/N` confirmation (`--yes` skips the prompt). |
| `garmin-mcp version` / `--version` | Prints the version set at build time (`-X main.version`). |

After a successful login, `auth` checks the new tokens by fetching the social profile. It then prints
`Logged in as <fullName> (<email>)` and the account folder path.

### Accounts folder

- Base directory: `$GARMIN_MCP_HOME` if set, otherwise `<user home>/.garmin-mcp`. The home comes from `os.UserHomeDir`,
  so `%USERPROFILE%` is used on Windows.
- Layout: `<base>/accounts/<email>/tokens.json` and `<base>/accounts/<email>/settings.json`.
- `<email>` is the normalized email: trimmed and lowercased. Validation, applied to **every** email from a tool argument
  or the CLI before it touches the filesystem: must match `^[^@\s/\\:*?"<>|]+@[^@\s/\\:*?"<>|]+$`, must not start with
  `.`, and must not contain `..`. On failure: `invalid email "<value>"`.
- Permissions: account dirs `0700`, files `0600` (best effort on Windows, where chmod is a no-op).
- `tokens.json`: `{"di_token": "...", "di_refresh_token": "...", "di_client_id": "..."}`. These are the same keys as
  python-garminconnect's `garmin_tokens.json`, so a Python token file can be copied in by hand.
- `settings.json`: `{"domain": "garmin.com" | "garmin.cn", "fit_download_dir": "<path or empty>"}`.
- All writes are atomic: write a temp file in the same dir, then rename over the target.

### Server behaviour

- Every tool's input schema has `email` (string, required, description: `Email of the Garmin Connect account to use.
  Call list_accounts to see configured accounts.`) as its first property, followed by the original arguments.
- **Extra tool (my call):** `list_accounts`, with no arguments. It returns
  `{"accounts": [{"email", "domain", "display_name", "token_status"}]}`. Without it the AI would have to ask the user for
  emails. It's the only tool without `email`, so the server has 158 tools in total. It's included in every profile.
- Accounts are loaded lazily on first use and cached in memory, one `garmin.Client` per email. Calls for different
  accounts run in parallel. Calls for the same account share the client, and token refresh is serialized per account.
- Unknown email → tool error result: `No Garmin account "<email>" is set up. Ask the user to run "garmin-mcp auth --email
  <email>" in a terminal. Configured accounts: <comma list or "none">.`
- Refresh token rejected → tool error result: `The Garmin session for "<email>" has expired. Ask the user to run
  "garmin-mcp auth --email <email> --force" in a terminal.`
- A refreshed token pair is written back to that account's `tokens.json` before the API call proceeds. If the write
  fails, the call still proceeds, and the error goes to stderr with the email and path (never the token).
- **Two processes, one account** (for example Claude Desktop and Claude Code both running): before refreshing, the
  client re-reads `tokens.json`. If the file holds a different access token that isn't about to expire, it uses that
  token and skips the refresh.
- Logging goes to stderr only. stdout belongs to the MCP protocol.

### Kept configuration (environment variables)

| Var | Meaning |
|---|---|
| `GARMIN_TOOL_PROFILE` | Same profiles as the original (`minimal`, `health`, `activities`, `workouts`, `training`, `nutrition`, `all`, comma-combinable). Same membership lists as `tool_profiles.py`, plus `list_accounts` in every profile. |
| `GARMIN_ENABLED_TOOLS` / `GARMIN_DISABLED_TOOLS` | Same semantics as the original (see its README "Tool Filtering"), including warnings on stderr for unknown names and startup failure for unknown profiles. |
| `GARMIN_FIT_DOWNLOAD_DIR` | Default download dir, overriding the per-account `fit_download_dir`. |
| `GARMIN_MCP_CALL_TIMEOUT` | Per-tool-call timeout in seconds, default `90`. `<=0` disables it. Same messages as the original. |
| `GARMIN_MCP_HOME` | Base dir override (new). |

`set_fit_download_dir(email, path)` stores the dir in that account's `settings.json`. Download dir precedence:
`output_dir` argument > `GARMIN_FIT_DOWNLOAD_DIR` > account setting > `needs_setup` result (same as the original).

Tool annotations (read-only / additive / destructive / idempotent hints) and structured write-error results
(`stage`, `upstream_status`, `upstream_body`, `error_type`, `write_committed`, `correlation_id`) are ported unchanged
from `tool_annotations.py` and `write_errors.py`. `list_accounts` is read-only.

## Login engine

Port `garminconnect/client.py` (0.3.2) to Go package `internal/garmin`. Constants (client IDs, service URLs, user
agents, native headers, DI client ID list, delays) are copied verbatim from that file.

Strategy chain, tried in order. A wrong-password error or an MFA request stops the chain. Any other failure (429, transport error,
non-JSON/HTML challenge) moves on to the next strategy.

1. `mobile+utls`: the iOS mobile flow (`POST sso.<domain>/mobile/api/login`) over a utls connection, rotating
   through the client hello profiles `HelloIOS_Auto`, `HelloSafari_Auto`, `HelloChrome_120` (the Go equivalents of
   `safari_ios`, `safari`, `chrome120`).
2. `mobile+plain`: the same flow over Go's standard TLS.
3. `widget+utls`: the SSO embed widget HTML form flow (CSRF scraped with the same regexes), `HelloChrome_Auto`.
4. `portal+utls`: the portal flow (`/portal/api/login`) with the 10–20 s anti-WAF delay, rotating `HelloSafari_Auto`,
   `HelloIOS_Auto`, `HelloChrome_120`, `HelloEdge_Auto`, `HelloChrome_Auto`.
5. `portal+plain`.

MFA: the `auth` CLI prompts for the code in the terminal, then verifies it on the flow's endpoint with the alternate
endpoint as a fallback, exactly as `_complete_mfa` / `_complete_mfa_widget` do. Ticket → DI token exchange tries each
DI client ID in order. Refresh uses `grant_type=refresh_token`. A token counts as expiring when `now > exp - 900s`.

API requests: `https://connectapi.<domain>/<path>`, `Authorization: Bearer <di_token>` plus the native headers,
15 s default timeout. On 401, refresh once and retry. 204 → `{}`. Status ≥ 400 → `APIError{Status, Message}`, with
the message extracted the same way as `_run_request`. API calls use the plain Go TLS transport (python also uses plain
`requests` for API calls). utls is only used for SSO.

utls needs HTTP/2 when the client hello offers `h2`. The transport picks HTTP/2 (`golang.org/x/net/http2`) or HTTP/1.1
based on the negotiated ALPN.

## Tech stack (my calls)

- Go: the version in `go.mod` is the toolchain used to build (1.27 at time of writing).
- `github.com/modelcontextprotocol/go-sdk` (MCP server, stdio, in-memory transport for tests).
- `github.com/refraction-networking/utls` (TLS fingerprints).
- `golang.org/x/net/http2` (HTTP/2 over utls connections).
- `golang.org/x/term` (terminal detection, hidden password input).
- `github.com/muktihari/fit` for FIT decoding. It replaces `fitparse`, and it was chosen over `tormoder/fit` (which I
  mentioned earlier in chat) because it decodes into generic messages with profile field names. The "return every
  message and field" tools need that.
- Everything else uses the standard library. `CGO_ENABLED=0`.

## CI and releases

- `.github/workflows/ci.yml`, on push and PR: `go vet ./...`, `go test -race ./...`, and a cross-compile of all six
  targets (catches platform-specific build breaks).
- `.github/workflows/release.yml`, on tag `v*`: builds the six targets with `CGO_ENABLED=0 go build -trimpath
  -ldflags "-s -w -X main.version=<tag>"`. Archives are `garmin-mcp_<tag>_<os>_<arch>.tar.gz` (`.zip` for Windows),
  each holding the binary, README and LICENSE, plus `checksums.txt` (sha256). They're published with `gh release create`.
  No goreleaser.
- Targets: `windows/amd64`, `windows/arm64`, `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`.

## Testing

- No live Garmin calls in CI. API and SSO tests use `httptest` servers. The client's base URLs can be overridden for
  tests: the client takes an `Endpoints` struct.
- Tool tests run through the MCP SDK's in-memory transport against an `httptest` Garmin, with fixtures ported from
  `tests/fixtures/garmin_responses.py`. Each tool module's Go test covers the cases in the matching Python
  `tests/integration/test_<module>_tools.py` and the relevant `tests/unit/*` files.
- A registry test checks that exactly 158 tools register, every tool except `list_accounts` requires `email`, every
  profile name exists, and every tool has an annotation.
- Manual live check before the first release: `garmin-mcp auth` against a real account (with and without MFA), then a
  handful of tools through Claude Desktop.

## Docs

The README covers install per OS (including the macOS quarantine note: `xattr -d com.apple.quarantine garmin-mcp`), first
login, adding accounts, Claude Desktop / Claude Code config snippets, env vars, the tool list by area, and the
differences from the Python original.

## Left to the builder

Internal package layout beyond what the plan fixes, Go naming inside packages, and the exact wording of CLI help text.
