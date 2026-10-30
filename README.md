# Garmin Connect MCP server

A Go, stdio-based Model Context Protocol server for Garmin Connect. It provides health, activity, training, workout, nutrition, device, and FIT analysis tools. Multiple Garmin accounts can share one server; every Garmin tool takes an `email` argument identifying the account.

## Install

Download the archive for your operating system and architecture from [Releases](https://github.com/elgatito/garmin-connect-go-mcp/releases), unpack it, and put `garmin-mcp` somewhere convenient. The release archives contain checksums and the license. On macOS, if Gatekeeper blocks the downloaded binary, run `xattr -d com.apple.quarantine garmin-mcp`. On Windows, SmartScreen may require **More info → Run anyway**.

## Connect accounts

Run `garmin-mcp` in a terminal for the first-login wizard, or run `garmin-mcp auth`. Enter your Garmin email and password; if prompted, enter your MFA code. Use `garmin-mcp auth --cn` for Garmin China. Credentials are entered interactively, not through environment variables.

Run `garmin-mcp auth` again for another account, `garmin-mcp accounts` to list accounts and token status, and `garmin-mcp remove` to remove one. Use `garmin-mcp auth --email you@example.com --force` to replace an expired or invalid session. Account tokens are stored under `~/.garmin-mcp/accounts/` by default; treat them as secrets.

## Connect an MCP client

Claude Desktop configuration:

```json
{
  "mcpServers": {
    "garmin": { "command": "/path/to/garmin-mcp" }
  }
}
```

Claude Code:

```sh
claude mcp add garmin /path/to/garmin-mcp
```

Restart the client after configuring it. Ask, for example, “Use my account me@example.com to show my Garmin stats for today.” `list_accounts` shows which accounts are available. The executable starts the server automatically when stdin is not a terminal; `garmin-mcp serve` starts it explicitly. Protocol output is written to stdout and diagnostics to stderr.

## Configuration

| Environment variable | Purpose |
| --- | --- |
| `GARMIN_MCP_HOME` | Account storage directory; defaults to `~/.garmin-mcp`. |
| `GARMIN_TOOL_PROFILE` | Comma-separated profiles: `minimal`, `health`, `activities`, `workouts`, `training`, `nutrition`, or `all`. |
| `GARMIN_ENABLED_TOOLS` | Comma-separated tool names: an allowlist without a profile, or additions to the selected profile. |
| `GARMIN_DISABLED_TOOLS` | Comma-separated tool names to disable; ignored when an allowlist is used without a profile. |
| `GARMIN_FIT_DOWNLOAD_DIR` | Default FIT download directory, overriding the account setting. |
| `GARMIN_MCP_CALL_TIMEOUT` | Per-tool timeout in seconds; defaults to 90. Values at or below zero disable the timeout. |

For downloads, an explicit `output_dir` takes precedence over `GARMIN_FIT_DOWNLOAD_DIR`, which takes precedence over the per-account directory set with `set_fit_download_dir`.

## Tools and resources

There are 158 tools, including `list_accounts`. The following tools are available by area; profiles and enabled/disabled filters can restrict the set exposed to a client.

**Health and wellness:** `get_all_day_events`, `get_all_day_stress`, `get_blood_pressure`, `get_body_battery`, `get_body_battery_events`, `get_body_composition`, `get_daily_steps`, `get_energy_balance`, `get_floors`, `get_heart_rates`, `get_heart_rates_summary`, `get_hydration_data`, `get_lifestyle_logging_data`, `get_morning_training_readiness`, `get_recovery_time_remaining`, `get_respiration_data`, `get_respiration_summary`, `get_rhr_day`, `get_sleep_data`, `get_sleep_summary`, `get_sleep_summary_range`, `get_spo2_data`, `get_stats`, `get_stats_and_body`, `get_stats_range`, `get_steps_data`, `get_stress_data`, `get_stress_summary`, `get_training_readiness`, `get_user_summary`, `get_weekly_intensity_minutes`, `get_weekly_steps`, `get_weekly_stress`.

**Activities:** `count_activities`, `create_manual_activity`, `delete_activity`, `get_activities`, `get_activities_by_date`, `get_activities_fordate`, `get_activity`, `get_activity_exercise_sets`, `get_activity_gear`, `get_activity_hr_in_timezones`, `get_activity_power_in_timezones`, `get_activity_split_summaries`, `get_activity_splits`, `get_activity_typed_splits`, `get_activity_types`, `get_activity_weather`, `set_activity_description`, `set_activity_event_type`, `set_activity_exercise_sets`, `set_activity_feel`, `set_activity_name`, `set_activity_type`, `set_perceived_effort`.

**Gear:** `add_gear_to_activity`, `get_gear`, `remove_gear_from_activity`.

**Training:** `get_acclimation`, `get_cycling_ftp`, `get_endurance_score`, `get_fitnessage_data`, `get_hill_score`, `get_hrv_data`, `get_hrv_trend`, `get_lactate_threshold`, `get_progress_summary_between_dates`, `get_respiration_trend`, `get_running_tolerance`, `get_running_tolerance_trend`, `get_training_effect`, `get_training_load_balance`, `get_training_load_trend`, `get_training_status`, `get_vo2max_trend`, `request_reload`.

**Workouts:** `delete_workout`, `delete_workouts`, `download_workout`, `get_garmin_coach_workouts`, `get_scheduled_workouts`, `get_training_plan_workouts`, `get_workout_by_id`, `get_workouts`, `reschedule_workout`, `schedule_workout`, `schedule_workouts`, `unschedule_workout`, `unschedule_workouts`, `upload_workout`, `upload_workouts`.

**Calendar:** `get_calendar_events`.

**Workout builders:** `create_run_interval_workout`, `create_run_workout`, `create_strength_workout`, `create_walk_run_workout`, `create_z2_walk_workout`, `schedule_week`.

**Nutrition:** `create_custom_food`, `delete_custom_food`, `delete_food_log`, `get_custom_food_serving_units`, `get_custom_foods`, `get_nutrition_daily_food_log`, `get_nutrition_daily_meals`, `get_nutrition_daily_settings`, `get_nutrition_summary_between_dates`, `log_custom_food`, `log_food`, `search_foods`, `set_nutrition_daily_settings`, `update_custom_food`, `upsert_and_log`.

**Challenges:** `get_adhoc_challenges`, `get_available_badge_challenges`, `get_badge_challenges`, `get_earned_badges`, `get_goals`, `get_inprogress_virtual_challenges`, `get_non_completed_badge_challenges`, `get_personal_record`, `get_race_predictions`.

**Devices:** `get_device_alarms`, `get_device_last_used`, `get_device_settings`, `get_device_solar_data`, `get_devices`, `get_primary_training_device`, `push_workout_to_device`.

**Weight and body composition:** `add_weigh_in`, `add_weigh_in_with_timestamps`, `delete_weigh_ins`, `get_daily_weigh_ins`, `get_weigh_ins`.

**Data management:** `add_body_composition`, `add_hydration_data`, `set_blood_pressure`.

**Women’s health:** `get_menstrual_calendar_data`, `get_menstrual_data_for_date`, `get_pregnancy_summary`.

**Courses:** `delete_course`, `download_course_gpx`, `get_course_details`, `get_courses`, `upload_course`.

**FIT files and activity analysis:** `download_activity_file`, `get_activity_fit_data`, `get_activity_fit_messages`, `get_power_duration_curve`, `set_fit_download_dir`.

**Profile:** `get_full_name`, `get_heart_rate_zones`, `get_unit_system`, `get_user_profile`, `get_userprofile_settings`, `set_heart_rate_zones`.

The server also exposes workout template resources and a workout-structure reference under `workout://`.

Compared with the Python server, this version supports multiple accounts, requires an `email` on Garmin tools, adds `list_accounts`, uses stdio transport only, and does not accept credentials or tokens through environment variables.

## Build and test

Install the Go version specified in `go.mod` and GNU Make, then run:

```sh
make build       # Build the executable in the project root
make check       # Run lint, race tests, and the six release build checks
```

Run `make help` to see the individual commands. For example, `make run ARGS="auth"` starts the login wizard, and `make clean` removes the executable built for the current platform.

On pushes and pull requests, GitHub Actions runs lint and tests, then uploads Windows amd64 and Linux amd64 binaries as workflow artifacts.

To release a new version after the manual live smoke checklist in the implementation plan, create and push a tag: `git tag vX.Y.Z && git push --tags`. The release workflow builds binaries for Linux, macOS, and Windows on amd64 and arm64.
