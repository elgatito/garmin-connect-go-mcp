# garmin-connect-go-mcp
MCP for Garmin Connect written in Go

`garmin-mcp` is a single self-contained binary that gives an AI assistant (Claude Desktop, Claude Code, any MCP
client) access to your Garmin Connect data over stdio. It is a Go port of
[taxuspt/garmin_mcp](https://github.com/taxuspt/garmin_mcp). One running server handles **several Garmin accounts at
once**: every tool takes the account's `email`. No Python, no runtime, no CGO.

## Install

Download the latest build for your platform and unpack it:

| Platform | Download | Claude Desktop extension |
|----------|----------|----------|
| Windows x64 | [garmin-mcp_windows_amd64.zip](https://github.com/elgatito/garmin-connect-go-mcp/releases/latest/download/garmin-mcp_windows_amd64.zip) | [garmin-mcp_windows_amd64.mcpb](https://github.com/elgatito/garmin-connect-go-mcp/releases/latest/download/garmin-mcp_windows_amd64.mcpb) |
| Linux arm64 | [garmin-mcp_linux_arm64.tar.gz](https://github.com/elgatito/garmin-connect-go-mcp/releases/latest/download/garmin-mcp_linux_arm64.tar.gz) | — |
| macOS Intel | [garmin-mcp_darwin_amd64.tar.gz](https://github.com/elgatito/garmin-connect-go-mcp/releases/latest/download/garmin-mcp_darwin_amd64.tar.gz) | [garmin-mcp_darwin_amd64.mcpb](https://github.com/elgatito/garmin-connect-go-mcp/releases/latest/download/garmin-mcp_darwin_amd64.mcpb) |
| macOS Apple Silicon | [garmin-mcp_darwin_arm64.tar.gz](https://github.com/elgatito/garmin-connect-go-mcp/releases/latest/download/garmin-mcp_darwin_arm64.tar.gz) | [garmin-mcp_darwin_arm64.mcpb](https://github.com/elgatito/garmin-connect-go-mcp/releases/latest/download/garmin-mcp_darwin_arm64.mcpb) |

Older versions are on the [Releases](https://github.com/elgatito/garmin-connect-go-mcp/releases) page. Each archive
holds `garmin-mcp` (`garmin-mcp.exe` on Windows), this README and the LICENSE;
[checksums.txt](https://github.com/elgatito/garmin-connect-go-mcp/releases/latest/download/checksums.txt) has the
sha256 sums.

- **macOS:** the binary is not signed or notarized. Remove the quarantine flag once:
  `xattr -d com.apple.quarantine garmin-mcp`
- **Windows:** SmartScreen may warn about an unknown publisher. Click "More info", then "Run anyway".
- **Linux/macOS:** `chmod +x garmin-mcp` if needed, and put it somewhere stable (for example `~/bin`).

## First login

Run `garmin-mcp` (or `garmin-mcp auth`) in a terminal. With no accounts yet it asks for your Garmin email, your
password (hidden input) and, if Garmin asks for one, the MFA code. Then it prints `Logged in as <name> (<email>)`.
Your password and the MFA code are typed in the terminal only, never through the AI, and the password is not saved.

```
garmin-mcp auth [--email E] [--cn] [--force]
```

- `--email E` skips the email prompt.
- `--cn` logs in to Garmin Connect China (`garmin.cn`). An existing account keeps its saved region, so `--force`
  without `--cn` stays on `garmin.cn` for a China account. To switch region, `garmin-mcp remove <email>` and run
  `auth` again.
- `--force` logs in again even if the saved session is still valid. An expired or soon-to-expire session is replaced
  without it; the server's "session expired" message suggests `--force`, which is harmless in that case.

## More accounts

Run `garmin-mcp auth` again for each additional account. Other commands:

- `garmin-mcp accounts` lists saved accounts with their token status (`ok`, `expired`, `invalid`; checked offline).
- `garmin-mcp remove <email> [--yes]` deletes an account's saved data after a confirmation.
- `garmin-mcp serve` runs the server over stdio explicitly; `garmin-mcp version` prints the version.

Started by an MCP client (stdin or stdout is not a terminal), `garmin-mcp` runs the server by itself. Run with no
arguments in a terminal and with accounts already saved, it only lists them and prints the usage; it does not serve.

## Client configuration

`garmin-mcp` is a standard stdio MCP server: the command is the full path to the binary, with no arguments. Log in
with `garmin-mcp auth` first (see [First login](#first-login)). On Windows use the full path to `garmin-mcp.exe`
(in JSON, write backslashes as `\\` or use forward slashes). [Environment variables](#environment-variables) go in the
client's `env` block.

### Claude Desktop

**Extension (easiest):** download the `.mcpb` file for your platform from the [Install](#install) table and
double-click it, or drag it into Claude Desktop's Settings > Extensions. The settings let you pick a
[tool profile](#environment-variables). The extension bundles its own copy of the binary, so you still need the
archive from the table to run `garmin-mcp auth` in a terminal; both use the same saved accounts.

**Manual:** add to `claude_desktop_config.json` (Settings > Developer > Edit Config) and restart Claude Desktop:

```json
{"mcpServers": {"garmin": {"command": "/path/to/garmin-mcp"}}}
```

### Claude Code

```
claude mcp add --scope user garmin /path/to/garmin-mcp
```

### Cursor, Windsurf, Gemini CLI and other `mcpServers` clients

Add the same block as for Claude Desktop to the client's config file:

| Client | Config file |
|---|---|
| Cursor | `~/.cursor/mcp.json` (all projects) or `.cursor/mcp.json` (one project) |
| Windsurf | `~/.codeium/windsurf/mcp_config.json` |
| Gemini CLI | `~/.gemini/settings.json`, or run `gemini mcp add garmin /path/to/garmin-mcp` |

```json
{"mcpServers": {"garmin": {"command": "/path/to/garmin-mcp"}}}
```

### VS Code (GitHub Copilot agent mode)

Run **MCP: Open User Configuration** (or create `.vscode/mcp.json` for one workspace) and add:

```json
{"servers": {"garmin": {"type": "stdio", "command": "/path/to/garmin-mcp"}}}
```

### OpenAI Codex CLI

```
codex mcp add garmin -- /path/to/garmin-mcp
```

or in `~/.codex/config.toml`:

```toml
[mcp_servers.garmin]
command = "/path/to/garmin-mcp"
```

## Asking for data

Always say which account: "use my account me@example.com and show yesterday's sleep". If you are not sure which
accounts are set up, ask the assistant to call `list_accounts`. An unknown account gets an error telling you to run
`garmin-mcp auth --email <email>`; an expired session tells you to run it with `--force`.

## Where data lives

`~/.garmin-mcp/accounts/<email>/` (`%USERPROFILE%\.garmin-mcp\...` on Windows), or under `$GARMIN_MCP_HOME` if set.
Each account folder has `tokens.json` (`di_token`, `di_refresh_token`, `di_client_id`) and `settings.json`
(`domain`, `fit_download_dir`). Folders are created `0700` and files `0600` (best effort on Windows). Writes are
atomic. **The tokens are bearer credentials**: anyone with the file can use your Garmin account. Keep the folder out of
synced or backed-up shared folders (Dropbox, iCloud, OneDrive, git).

## Log file

The server appends to `~/.garmin-mcp/garmin-mcp.log` (or `GARMIN_MCP_LOG_FILE`): one line per tool call with the
tool name, its arguments, how long it took and whether it succeeded (with the error text if not), plus start/stop,
warnings, deletions and failed writes. Each line starts with the time and the process id, so you can tell apart
several clients sharing the file. Results themselves are not logged, only their size. When the file is over 10 MB at
startup it is renamed to `garmin-mcp.log.1` (replacing the previous one). The same lines also go to stderr, which
Claude Desktop keeps in its own `mcp-server-*.log`. Tool arguments can include personal data (weights, food entries),
so the file is created `0600`, like the tokens.

## Environment variables

| Variable | Meaning |
|---|---|
| `GARMIN_TOOL_PROFILE` | Expose only a subset of tools. Profiles: `minimal`, `health`, `activities`, `workouts`, `training`, `nutrition`, `all` (default). Comma-separate to combine. `list_accounts` and the core identity tools are in every profile. Unknown names stop the server at startup. |
| `GARMIN_ENABLED_TOOLS` | Comma-separated tool names. With `GARMIN_TOOL_PROFILE` set, they are added to the profile (and `GARMIN_DISABLED_TOOLS` still removes tools from the result). Without a profile it is an exclusive allowlist: only these tools are exposed (`list_accounts` only if you list it) and `GARMIN_DISABLED_TOOLS` is ignored. |
| `GARMIN_DISABLED_TOOLS` | Comma-separated tool names to remove. Unknown names are warned about on stderr. |
| `GARMIN_FIT_DOWNLOAD_DIR` | Default directory for downloaded FIT files and GPX courses (created if missing, mode 0755); overrides the per-account setting from `set_fit_download_dir`. An explicit `output_dir` argument wins over both. |
| `GARMIN_MCP_CALL_TIMEOUT` | Per-tool-call timeout in seconds. Default `90`; `0` or negative disables it. |
| `GARMIN_MCP_HOME` | Base folder for account data (default `~/.garmin-mcp`). |
| `GARMIN_MCP_LOG_FILE` | Server log file (default `garmin-mcp.log` in the base folder). |

## Tools

158 tools: the 157 of the original plus `list_accounts`. Every tool except `list_accounts` takes a required `email`.

| Area | Tools |
|---|---|
| Health and wellness (sleep, HRV, stress, body battery, steps, SpO2, ...) | 33 |
| Activity management (list, fetch, upload, download, edit, delete) | 23 |
| Training (status, readiness, VO2, race predictions, ...) | 18 |
| Nutrition (food log, custom foods, targets) | 15 |
| Workouts (list, upload, schedule, delete) | 15 |
| Challenges, goals and badges | 9 |
| Devices | 7 |
| User profile | 6 |
| Workout builders | 6 |
| Activity analysis (FIT messages, FIT data, ride analysis) | 5 |
| Courses | 5 |
| Weight management | 5 |
| Data management | 3 |
| Gear management | 3 |
| Women's health | 3 |
| Calendar events | 1 |
| Accounts (`list_accounts`) | 1 |

The five workout template resources (`workout://templates/*`, `workout://reference/structure`) are included. Tool
names, argument names and descriptions match the Python original, so its documentation applies.

## Differences from the Python original

- Multi-account: every tool takes a required `email` argument, and there is an extra `list_accounts` tool.
- stdio only: no HTTP/SSE transport, bearer token or Docker. Claude Desktop gets a `.mcpb` bundle (the format formerly called `.dxt`).
- Login happens in a terminal (`garmin-mcp auth`); there are no login tools and no `GARMIN_EMAIL`,
  `GARMIN_PASSWORD`, `GARMINTOKENS` or `GARMIN_IS_CN` environment variables.
- The FIT download directory is per account (`set_fit_download_dir` writes to that account's `settings.json` and creates the directory, mode 0755).
- Key order in JSON results may differ from the Python original (map-based results are sorted alphabetically; results built from typed structs keep field order), and whole-number floats print as integers (`56`, not `56.0`).
- Error texts differ: tool error messages after the `Error ...:` prefix come from Go (for example `API Error 403 - ...`), and session problems tell the user to run `garmin-mcp auth`.
- Tool annotations, structured write errors and profiles are ported unchanged.

## Security notes

- Write tools exist (they can delete activities, upload workouts, log food and weight, ...). Restrict what the
  assistant can do with `GARMIN_TOOL_PROFILE` (for example `minimal`, `health`) or `GARMIN_DISABLED_TOOLS`.
- `download_course_gpx` and `download_activity_file` write to, and `upload_course` reads from, file paths that the
  model supplies. Keep a client that asks for approval of tool calls, or disable those tools.
- Tokens and passwords are never logged or returned in tool results. Logs go to stderr; stdout is the protocol.

## Building from source

```
make build   # or: go build -trimpath -o garmin-mcp ./cmd/garmin-mcp
make check   # lint, race tests, cross-compile (what CI runs)
```

Needs the Go version in `go.mod`. `make help` lists the build, test and release targets.

## Releasing

`git tag vX.Y.Z && git push --tags`. The Release workflow runs `make dist`, which builds the four archives, the
three `.mcpb` bundles and `checksums.txt`, and publishes a GitHub Release. Do the checklist below first.

### Before tagging a release

Manual live checks (no live Garmin calls run in CI); record the results in the PR description.

1. `garmin-mcp auth` with a real non-MFA account prints `Logged in as ...`; note which login strategy succeeded in the stderr log.
2. `garmin-mcp auth` with an MFA account asks for the code and succeeds; note which login strategy succeeded in the stderr log.
3. `garmin-mcp accounts` lists both as `ok`.
4. In Claude Desktop: `list_accounts`, `get_stats` for each account on today's date, `get_activities` with limit 3,
   and `download_activity_file` (a file appears).
5. Edit `tokens.json` and set `di_token` to an expired JWT. The next tool call refreshes it and the file changes.
