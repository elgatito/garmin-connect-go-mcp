# garmin-connect-go-mcp

An MCP (Model Context Protocol) server for Garmin Connect, written in Go. It
exposes your Garmin fitness and health data to Claude and other MCP clients as
a single static binary, with no Python runtime.

This is a port of [taxuspt/garmin_mcp](https://github.com/Taxuspt/garmin_mcp)
at `a2ca5fc`: the same 157 tools, argument names, defaults and result shapes,
plus a few additions for running several Garmin accounts from one server
(see [Differences from the Python original](#differences-from-the-python-original)).

The server speaks MCP over stdio. Logs go to stderr; stdout carries only the
protocol. Tokens and passwords never appear in logs, errors or tool results.

## Install

1. Download the archive for your OS and architecture from the
   [Releases page](https://github.com/elgatito/garmin-connect-go-mcp/releases):
   `garmin-mcp_vX.Y.Z_linux_amd64.tar.gz`, `..._darwin_arm64.tar.gz`,
   `..._windows_amd64.zip`, etc.
2. Unpack it. Each archive contains the `garmin-mcp` binary (or
   `garmin-mcp.exe` on Windows), `README.md` and `LICENSE`.
3. Move the binary somewhere convenient and note its full path. You can put it
   on `PATH` or use the absolute path in your client configuration.

**macOS:** a downloaded binary is quarantined by Gatekeeper. Remove the
attribute once:

```bash
xattr -d com.apple.quarantine garmin-mcp
```

If macOS still refuses to run it, allow it in **System Settings → Privacy &
Security**.

**Windows:** the binary is unsigned, so SmartScreen may show "Windows
protected your PC". Click **More info → Run anyway**.

## First login

Authenticate once in a terminal before adding the server to a client:

```bash
garmin-mcp auth
```

You will be prompted for your email and password (input is hidden). If the
account has MFA enabled, Garmin sends a code and the CLI asks for it. On success
it prints the account name and the directory the tokens were saved to, for
example:

```text
Logged in as Ada Lovelace (ada@example.com)
Saved to /home/ada/.garmin-mcp/accounts/ada@example.com
```

Running `garmin-mcp` with no arguments in a terminal does the same when no
account exists yet. Tokens are refreshed automatically; when a refresh token is
rejected, re-authenticate with:

```bash
garmin-mcp auth --email ada@example.com --force
```

For Garmin Connect China (garmin.cn), add `--cn`:

```bash
garmin-mcp auth --cn
```

The domain is stored per account, so international and Chinese accounts can
coexist.

## More accounts

One server process serves any number of accounts. Add another with:

```bash
garmin-mcp auth --email bob@example.com --cn
```

List them and their token status at any time:

```bash
garmin-mcp accounts
```

```text
ada@example.com  garmin.com  ok
bob@example.com  garmin.cn  ok
```

The status is `ok`, `expired` (the token is expired or about to expire and will
be refreshed on the next call) or `invalid` (run `auth --force`). Remove an
account with:

```bash
garmin-mcp remove bob@example.com
```

`remove` asks for confirmation; pass `--yes` to skip it. It deletes that
account's folder, including its tokens.

## Client configuration

### Claude Desktop

Add the server to `claude_desktop_config.json`:

- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`

```json
{
  "mcpServers": {
    "garmin": {
      "command": "/path/to/garmin-mcp"
    }
  }
}
```

Use the absolute path to the binary (`C:\\path\\to\\garmin-mcp.exe` on Windows).
No credentials go in this file: the server reads the accounts saved by `auth`.
Restart Claude Desktop after saving.

### Claude Code

```bash
claude mcp add garmin /path/to/garmin-mcp
```

### Asking for data

Every account-scoped tool requires the account's email, so tell the client which
account to use. `list_accounts` shows the configured addresses:

> Use my account me@example.com. How did I sleep last night?

> List my recent activities for bob@example.com, limit 3.

> Use ada@example.com and show my stats for today.

A tool call without `email` fails validation, so the client needs an address; it
can get one from `list_accounts` or ask you.

## Environment variables

| Variable | Meaning | Default |
|---|---|---|
| `GARMIN_MCP_HOME` | Base directory for accounts (`accounts/<email>/tokens.json` and `settings.json`). | `~/.garmin-mcp` |
| `GARMIN_TOOL_PROFILE` | Built-in subset of tools by area: `minimal`, `health`, `activities`, `workouts`, `training`, `nutrition`, `all`. Combine with commas, e.g. `health,training`. | `all` |
| `GARMIN_ENABLED_TOOLS` | Comma-separated allowlist. On its own, only these tools register; with a profile, they are added to it. | unset |
| `GARMIN_DISABLED_TOOLS` | Comma-separated denylist. Ignored when an allowlist is set without a profile; with a profile, it removes tools from the profile. | unset |
| `GARMIN_FIT_DOWNLOAD_DIR` | Default directory for downloaded activity files and course GPX files. For activities it overrides the per-account `set_fit_download_dir` setting. | unset |
| `GARMIN_MCP_CALL_TIMEOUT` | Per-tool-call timeout in seconds for Garmin requests. `0` or a negative value disables it. | `90` |

Tool and profile names are case-insensitive. Unknown names in the filter
variables are ignored with a warning on stderr; an unknown profile stops the
server at startup with the list of known profiles. Every profile also includes
the identity and unit tools (`get_full_name`, `get_heart_rate_zones`,
`get_unit_system`, `get_user_profile`, `get_userprofile_settings`) and
`list_accounts`.

Download directory precedence is: `output_dir` argument >
`GARMIN_FIT_DOWNLOAD_DIR` > account's `set_fit_download_dir` setting > a
`needs_setup` result asking the client where to save.

## Tools

158 tools: the 157 from the Python original plus `list_accounts`.

### Account (1)

`list_accounts`

### User profile and settings (6)

`get_full_name`, `get_heart_rate_zones`, `get_unit_system`,
`get_user_profile`, `get_userprofile_settings`, `set_heart_rate_zones`

### Health and wellness (33)

`get_all_day_events`, `get_all_day_stress`, `get_body_battery`,
`get_body_battery_events`, `get_blood_pressure`, `get_body_composition`,
`get_daily_steps`, `get_energy_balance`, `get_floors`, `get_heart_rates`,
`get_heart_rates_summary`, `get_hydration_data`, `get_lifestyle_logging_data`,
`get_morning_training_readiness`, `get_recovery_time_remaining`,
`get_respiration_data`, `get_respiration_summary`, `get_rhr_day`,
`get_sleep_data`, `get_sleep_summary`, `get_sleep_summary_range`,
`get_spo2_data`, `get_stats`, `get_stats_and_body`, `get_stats_range`,
`get_steps_data`, `get_stress_data`, `get_stress_summary`,
`get_training_readiness`, `get_user_summary`, `get_weekly_intensity_minutes`,
`get_weekly_steps`, `get_weekly_stress`

### Activities and gear (26)

`add_gear_to_activity`, `count_activities`, `create_manual_activity`,
`delete_activity`, `get_activities`, `get_activities_by_date`,
`get_activities_fordate`, `get_activity`, `get_activity_exercise_sets`,
`get_activity_gear`, `get_activity_hr_in_timezones`,
`get_activity_power_in_timezones`, `get_activity_split_summaries`,
`get_activity_splits`, `get_activity_typed_splits`, `get_activity_types`,
`get_activity_weather`, `get_gear`, `remove_gear_from_activity`,
`set_activity_description`, `set_activity_event_type`,
`set_activity_exercise_sets`, `set_activity_feel`, `set_activity_name`,
`set_activity_type`, `set_perceived_effort`

### Training and performance (18)

`get_acclimation`, `get_cycling_ftp`, `get_endurance_score`,
`get_fitnessage_data`, `get_hill_score`, `get_hrv_data`, `get_hrv_trend`,
`get_lactate_threshold`, `get_progress_summary_between_dates`,
`get_respiration_trend`, `get_running_tolerance`, `get_running_tolerance_trend`,
`get_training_effect`, `get_training_load_balance`, `get_training_load_trend`,
`get_training_status`, `get_vo2max_trend`, `request_reload`

### Workouts and calendar (22)

`create_run_interval_workout`, `create_run_workout`,
`create_strength_workout`, `create_walk_run_workout`,
`create_z2_walk_workout`, `delete_workout`, `delete_workouts`,
`download_workout`, `get_calendar_events`, `get_garmin_coach_workouts`,
`get_scheduled_workouts`, `get_training_plan_workouts`, `get_workout_by_id`,
`get_workouts`, `reschedule_workout`, `schedule_week`, `schedule_workout`,
`schedule_workouts`, `unschedule_workout`, `unschedule_workouts`,
`upload_workout`, `upload_workouts`

### Nutrition (15)

`create_custom_food`, `delete_custom_food`, `delete_food_log`,
`get_custom_food_serving_units`, `get_custom_foods`,
`get_nutrition_daily_food_log`, `get_nutrition_daily_meals`,
`get_nutrition_daily_settings`, `get_nutrition_summary_between_dates`,
`log_custom_food`, `log_food`, `search_foods`, `set_nutrition_daily_settings`,
`update_custom_food`, `upsert_and_log`

### Challenges, badges, goals and records (9)

`get_adhoc_challenges`, `get_available_badge_challenges`, `get_badge_challenges`,
`get_earned_badges`, `get_goals`, `get_inprogress_virtual_challenges`,
`get_non_completed_badge_challenges`, `get_personal_record`,
`get_race_predictions`

### Devices (7)

`get_device_alarms`, `get_device_last_used`, `get_device_settings`,
`get_device_solar_data`, `get_devices`, `get_primary_training_device`,
`push_workout_to_device`

### Weight (5)

`add_weigh_in`, `add_weigh_in_with_timestamps`, `delete_weigh_ins`,
`get_daily_weigh_ins`, `get_weigh_ins`

### Body composition, hydration and blood pressure (3)

`add_body_composition`, `add_hydration_data`, `set_blood_pressure`

### Women's health (3)

`get_menstrual_calendar_data`, `get_menstrual_data_for_date`,
`get_pregnancy_summary`

### Courses (5)

`delete_course`, `download_course_gpx`, `get_course_details`, `get_courses`,
`upload_course`

### FIT files and activity analysis (5)

`download_activity_file`, `get_activity_fit_data`, `get_activity_fit_messages`,
`get_power_duration_curve`, `set_fit_download_dir`

## Differences from the Python original

- **Many accounts in one process.** A single server serves every account saved
  with `auth`. The Python original bound one process to one account and needed
  one server instance and one token directory per account.
- **`email` argument on every account-scoped tool.** It is required and names
  the account to use. `list_accounts` reports the configured addresses, their
  domain and token status.
- **`list_accounts` is new.** The other 157 tool names, argument names,
  defaults and result shapes are unchanged.
- **stdio only.** There are no HTTP, SSE or `.dxt` transports; clients launch
  the binary directly.
- **No credentials in environment variables.** `GARMIN_EMAIL`,
  `GARMIN_PASSWORD` (`_FILE`), `GARMINTOKENS`, `GARMINTOKENS_BASE64` and
  `GARMIN_IS_CN` are not read. Log in once with `garmin-mcp auth` (use `--cn`
  for garmin.cn); tokens live under `$GARMIN_MCP_HOME` (default
  `~/.garmin-mcp`).
- **New CLI.** `garmin-mcp auth|accounts|remove|version|serve` replaces
  `garmin-mcp-auth`; `--force` replaces `--force-reauth`. There is no
  `--verify`, `--diagnose` or `--token-path`; set `GARMIN_MCP_HOME` to change
  the store location.

## Building from source

Requires Go 1.27 or newer.

```bash
git clone https://github.com/elgatito/garmin-connect-go-mcp.git
cd garmin-connect-go-mcp
CGO_ENABLED=0 go build -trimpath -o garmin-mcp ./cmd/garmin-mcp
```

Run the tests (no live Garmin calls; the suite uses fake HTTP servers):

```bash
go vet ./...
go test -race ./...
```

Cross-compile for any release target, for example:

```bash
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o garmin-mcp ./cmd/garmin-mcp
```

The six supported targets are `windows/amd64`, `windows/arm64`, `linux/amd64`,
`linux/arm64`, `darwin/amd64` and `darwin/arm64`.

## Releasing

Tag and push:

```bash
git tag vX.Y.Z
git push --tags
```

The release workflow builds the six targets with `CGO_ENABLED=0`, stamps the
version into the binary, packs a `.tar.gz` (`.zip` on Windows) per target with
`README.md` and `LICENSE`, writes `checksums.txt`, and publishes a GitHub
release.

## Live smoke checklist

Manual, by a human with real Garmin accounts, before tagging `v0.1.0`. Record
the results in the PR description. Do not check these off from CI or a fake
server.

- [ ] `garmin-mcp auth` with a real non-MFA account → `Logged in as ...`.
- [ ] `garmin-mcp auth` with an MFA account → code prompt → success.
- [ ] `garmin-mcp accounts` lists both accounts as `ok`.
- [ ] In Claude Desktop: `list_accounts`, then `get_stats` for each account on
      today's date, `get_activities` with limit 3, and
      `download_activity_file` → a file appears on disk.
- [ ] Edit `tokens.json` and replace `di_token` with an expired JWT → the next
      tool call refreshes it and the file changes.
