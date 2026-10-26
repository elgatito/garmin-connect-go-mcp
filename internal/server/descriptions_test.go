package server

import "testing"

// Code generated from taxuspt/garmin_mcp at a2ca5fc, then checked in so drift
// fails the test. Each entry is the Python tool docstring text before "Args:",
// dedented exactly as inspect.cleandoc produces it. list_accounts is Go-only.
var pythonToolDescriptions = map[string]string{
	"add_body_composition": `Add body composition data`,
	"add_gear_to_activity": `Associate gear with an activity

Links a specific piece of gear (like shoes, bike, etc.) to an activity.`,
	"add_hydration_data": `Add hydration data`,
	"add_weigh_in":       `Add a new weight measurement`,
	"add_weigh_in_with_timestamps": `Add a new weight measurement with specific timestamps

If both timestamps are omitted, the current time is used. If only
date_timestamp is given, the GMT timestamp is derived from it.`,
	"count_activities": `Get total count of activities in the user's Garmin account

Returns the total number of activities recorded.`,
	"create_custom_food": `Create a custom food in the user's Garmin nutrition library

Creates a new food item with nutritional information per serving.
On success the response includes foodId and servingId needed for
log_custom_food. If the API returns no data (204), use
get_custom_foods(search=food_name) to retrieve those IDs.

All nutrient amounts are ABSOLUTE values per serving, not %DV.
Nutrition labels often print %DV for calcium/iron/vitamin D —
convert to absolute units before passing.`,
	"create_manual_activity": `Log a manual activity in Garmin Connect — useful for activities done without a watch.

The type_key must match a Garmin activity type. Use get_activity_types to see
the full list. Common values: yoga, strength_training, meditation, indoor_cycling,
pilates, bouldering, fitness_equipment.`,
	"create_run_interval_workout": `Create a repeat-based interval run workout and upload it to Garmin Connect.

Builds warmup -> repeats x [work interval + jog recovery] -> cooldown. Use this
instead of create_run_workout whenever the session has more than one work
interval (threshold reps, VO2 intervals, cruise intervals, strides).

Work intervals target a named heart-rate zone by default, but named zones are
usually far too wide for interval work: a threshold session at 172-180 bpm sits
inside a Z3 that may span 158-184. Pass hr_min and hr_max together to target the
exact bpm range, so the watch alerts as soon as the effort drifts off target.

Recovery steps carry no target unless recovery_hr_min/max are given -- a capped
recovery alerts constantly right after a hard rep, since heart rate lags effort.
Recovery HR bounds require recovery_seconds > 0.

Example -- 3 x 6 min at HR 172-180 with 3 min jogs, 12m warmup, 8m cooldown:
    repeats=3, rep_seconds=360, recovery_seconds=180,
    hr_min=172, hr_max=180, warmup_min=12, cooldown_min=8`,
	"create_run_workout": `Create a continuous run workout and upload it to Garmin Connect.

Builds a single uninterrupted run interval with warmup and cooldown walks.

Targets a named Garmin heart-rate zone by default. Named zones (Z1-Z5)
don't line up with every real training target -- e.g. a 136-148 bpm
Zone 2 goal straddles Garmin's Z2 (118-137) and Z3 (138-157). Pass
hr_min and hr_max together to target that exact bpm range instead;
the watch will then show "in range" only for the range you actually
want, not a whole zone that over- or under-shoots it.`,
	"create_strength_workout": `Create a strength workout and upload it to Garmin Connect.

Each exercise becomes a reps-based step; when sets > 1 the exercise is
emitted as a repeat group of that many iterations (work + rest per set),
so Garmin shows the real set count. The name is kept in the step
description; it is also sent as exerciseName, which Garmin only retains when
it matches one of its own exercise keys (e.g. "FARMERS_CARRY").`,
	"create_walk_run_workout": `Create a walk/run interval workout and upload it to Garmin Connect.

Builds the internal Garmin JSON automatically and returns the new workout ID.`,
	"create_z2_walk_workout": `Create a steady Z2 walking workout and upload it to Garmin Connect.`,
	"delete_activity": `Permanently delete an activity from Garmin Connect.

This cannot be undone. Call once with confirm=false (the default) to
preview the activity, then call again with confirm=true to delete it.
confirm=true is the scripting equivalent of a force flag.`,
	"delete_course": `Delete a course from Garmin Connect.

This cannot be undone. Call once with confirm=false (the default) to
preview the course, then call again with confirm=true to delete it.`,
	"delete_custom_food": `Delete a custom food from the user's Garmin nutrition library

Permanently removes a custom food entry — this cannot be undone and
the deleted food's name/brand/nutrition data cannot be recovered from
the API afterward. The default call previews the food without deleting
it. Review that preview, then call again with confirm=true.

Deleting a food does NOT require it to be unreferenced: existing food
log entries that already reference this food are NOT blocked or
removed — they remain in the log (their nutrition still counts toward
daily totals) but are marked inactive (foodInactive: true) and can no
longer be edited/re-logged from the library.

Use get_custom_foods to find the foodId, and match on the exact
foodId (not just a name search) — Garmin's search can return loosely
related results for a given search term, not just exact matches.`,
	"delete_food_log": `Delete a food log entry

Permanently removes a logged food item from the nutrition log.
Works for both QUICK_ADD and REGULAR_LOG entry types.
Use get_nutrition_daily_food_log to find the logId and date. The default
call previews the matching entry; call again with confirm=true to delete.`,
	"delete_weigh_ins": `Delete weight measurements for a specific date

Permanently removes the selected day's weigh-ins. This cannot be undone.
Call once with confirm=false (the default) to preview the measurements,
then call again with confirm=true to delete them.`,
	"delete_workout": `Delete a workout from Garmin Connect

Permanently removes a workout from your Garmin Connect workout library.
This cannot be undone. Call once with confirm=false (the default) to
preview the workout, then call again with confirm=true to delete it.`,
	"delete_workouts": `Delete multiple workouts from Garmin Connect in a single call

Permanently removes multiple workouts from your Garmin Connect workout library.
This cannot be undone. Call once with confirm=false (the default) to
preview the workouts, then call again with confirm=true to delete them.`,
	"download_activity_file": `Download an activity and save it to disk as a file.

Saves the activity in the requested format. Defaults to the original .fit
file; Garmin also supports gpx, tcx, and csv.

Directory resolution (first match wins):
  1. output_dir argument (one-off; not persisted)
  2. GARMIN_FIT_DOWNLOAD_DIR environment variable
  3. persisted config (set via set_fit_download_dir)
If none is configured, returns status "needs_setup" with a suggested
default (the server's current working directory). In that case, ask the
user where to save, call set_fit_download_dir(path), then call this tool
again.

Files are named "{activity_id}.{ext}" and overwrite any existing file.`,
	"download_course_gpx": `Download a Garmin Connect course as a GPX file.

The GPX is rebuilt from the course's track points (lat/lon/elevation)
and course points (as waypoints); it is not the originally uploaded file.`,
	"download_workout": `Download a workout as a FIT file

Downloads the workout in FIT format. The binary data cannot be returned
directly through the MCP interface, but this confirms the workout is available.`,
	"get_acclimation": `Get heat and altitude acclimation status for a given date.

Garmin tracks how adapted the athlete currently is to training in heat
and at altitude. heat_acclimation_percent runs 0-100 and decays without
continued exposure; use it to judge readiness for a warm-weather race.

heat_trend reports Garmin's own label (e.g. ACCLIMATIZED). The
previous_* fields hold the prior reading so direction of travel is
visible without a second call.

VO2 max is not returned here; use get_training_status or get_vo2max_trend.`,
	"get_activities": `Get activities with pagination support.

Retrieves a paginated list of activities ordered newest-first. Use this
for browsing through large activity lists when you do not need to filter
by date range, or as a complement to get_activities_by_date.

Each activity includes an event_type field. Common values: "race",
"training", "uncategorized" (no event type set by the user — common for
Peloton imports and untagged runs). Filter for races with
event_type == "race" rather than excluding "training", as many non-race
activities appear as "uncategorized" rather than "training".`,
	"get_activities_by_date": `Get activities between specified dates with pagination support.

For accounts with large activity histories, broad date ranges can return
thousands of activities in a single response. Use page and page_size to
retrieve activities in manageable chunks and avoid "result too large" errors.
Activities are ordered newest-first.

Pagination: when has_more is true the response includes next_page — pass
that value as page on the next call to retrieve the following page. Repeat
until has_more is false.

Note: total_count for a date range is not available from the Garmin API
without fetching all results. Use has_more / next_page to walk pages.

Each activity includes an event_type field with values such as:
  - "race"          — explicitly tagged as a race by the user
  - "training"      — explicitly tagged as a training activity
  - "uncategorized" — no event type set; common for Peloton imports and
                      untagged outdoor runs. Distinct from "training":
                      filter for races with event_type == "race" rather
                      than excluding "training", since many non-race
                      activities appear as "uncategorized" not "training"
  - field absent    — API returned no eventType for this activity; not
                      observed in practice in any activity back to 2012
                      (oldest activities sampled on this account)`,
	"get_activities_fordate": `Get activities for a specific date`,
	"get_activity": `Get detailed information for a single activity.

Returns a comprehensive summary including timing, distance, heart rate,
elevation, training effect, and an event_type field. Common event_type
values: "race", "training", "uncategorized" (no event type set by the
user). The field is omitted when Garmin returns no eventTypeDTO.`,
	"get_activity_exercise_sets": `Get exercise sets for strength training activities`,
	"get_activity_fit_data": `Download and parse FIT file for an activity to expose advanced cycling data.

Returns data not available through the standard REST API, including:
- DI2 / electronic shifting events with cadence at time of shift, grade at shift,
  gear combinations, shift quality classification, and terrain-grouped shift analysis
- Cycling dynamics per session and lap: platform center offset (PCO), left/right power
  balance, torque effectiveness, pedal smoothness
- Variability Index (NP / avg_power) per session and lap
- Climb detection with VAM (vertical ascent rate), avg power/cadence/HR per climb,
  and W/kg per climb (using the latest Garmin weigh-in on or before the ride,
  up to 90 days back; rider_weight_date says which weigh-in was used)
- Grade-correlated stats: avg power, cadence, HR broken down by terrain steepness
- Grade comes from the device's per-record grade field when present; otherwise
  it is derived from altitude and distance (~100 m window). grade_source
  ("device" | "derived") says which
- HR drift / cardiac drift coefficient (aerobic decoupling for rides ≥60 min)
- Temperature correlation: avg HR/power in hottest vs. coolest portions of ride
- Power Duration Curve: best mean maximal power at 5s, 30s, 1min, 5min, 10min, 20min, 60min
- Optional full per-second time series when include_records=True

Multisport files (triathlon, duathlon) contain one session per leg. For
these, the response adds multisport=true and a sessions list with every
leg; session and the record-based analyses (power curve, climbs, grade,
HR drift, temperature) describe one leg only — the cycling leg if there
is exactly one, else the longest — given by analyzed_session_index and
explained in analysis_note. Laps and records still cover all legs.

Shift quality:
- proactive: shifted at 70-100 rpm (ideal cadence range)
- reactive: shifted below 70 rpm (already grinding before shifting)
- coasting: shifted at 0 rpm (mid-stop or freewheeling)
- spun_out: shifted above 100 rpm (waited too long in easy gear)

Note: DI2 data requires Shimano Di2 / SRAM eTap. Cycling dynamics require a
compatible power meter (e.g., Garmin Rally, Favero Assioma, PowerTap P1 pedals).`,
	"get_activity_fit_messages":    "Retrieve and generically parse an activity's original FIT file.\n\nThis is the source-of-truth endpoint for original device activity data.\nIt returns every message and every field emitted by the FIT decoder\nwithout applying sport-specific curation. Messages remain in file order,\nand each field includes its decoded value plus units and definition\nnumber when available. Unknown and duplicate fields are retained.\n\nGarmin Connect edits made after upload may be stored only in Garmin's\nservice and are not necessarily written back into the original FIT file.\nThis tool intentionally does not merge those service-side edits.\n\nHigh-frequency ``record`` messages are counted but omitted by default.\nOther message types occurring more than 100 times are also inventoried\nbut omitted from an unfiltered default response. Request those types\nexplicitly with message_types. All returned messages are bounded by\nmessage_offset/message_limit (maximum 5000 per call); follow\npagination.next_offset until absent to retrieve the full selected stream.\n\nUse message_types to return only particular FIT message types while\nmessage_counts still inventories the complete file. For example, a\nstrength workout can usually be inspected efficiently with\n[\"session\", \"lap\", \"set\", \"exercise_title\"]. Message type names are\ncase-insensitive and should use FIT/fitparse snake_case names.",
	"get_activity_gear":            `Get gear data used for an activity`,
	"get_activity_hr_in_timezones": `Get heart rate data in different time zones for an activity`,
	"get_activity_power_in_timezones": `Get power distribution across training zones for an activity.

Returns time spent in each power zone with watt thresholds and duration.
Requires a power meter. Zones are based on the athlete's FTP configured in Garmin Connect.`,
	"get_activity_split_summaries": `Get split summaries for an activity`,
	"get_activity_splits": `Get splits for an activity

Lap and nested length start_time_gmt timestamps are GMT (UTC).`,
	"get_activity_typed_splits": `Get typed splits for an activity`,
	"get_activity_types": `Get all available activity types

Returns a list of all activity types supported by Garmin Connect,
useful for filtering activities by type.`,
	"get_activity_weather": `Get weather data for an activity.

Garmin's weather endpoint returns temperatures in Fahrenheit (from the
weather-station source) with no unit indicator, regardless of account
settings. This tool converts them to the account's display unit: metric
accounts get Celsius, statute_us accounts keep Fahrenheit. The
temperature_unit field ("F" or "C") states which unit was returned.

Wind speed, unlike temperature, is already returned in the account's
display unit (km/h for metric, mph for statute_us), so it is passed
through unconverted and labeled via the wind_speed_unit field.`,
	"get_adhoc_challenges": `Get user-created social/group challenges (e.g., step competitions with friends)

Returns challenges created by users to compete with connections. These are
different from official Garmin badge challenges.`,
	"get_all_day_events": `Get daily wellness events data`,
	"get_all_day_stress": `Get all-day stress data`,
	"get_available_badge_challenges": `Get official Garmin badge challenges available to join

Returns monthly/seasonal challenges from Garmin that the user can join.
These challenges award badges and points upon completion.`,
	"get_badge_challenges": `Get badge challenges the user has completed.

Returns the completed badge-challenge history and earned dates. For
challenges still in progress, use get_non_completed_badge_challenges.`,
	"get_blood_pressure":      `Get blood pressure data`,
	"get_body_battery":        `Get body battery data with events`,
	"get_body_battery_events": `Get body battery events data`,
	"get_body_composition":    `Get body composition data for a single date or date range`,
	"get_calendar_events": `Get races and events on the Garmin Connect calendar between two dates

Returns calendar entries the user added or subscribed to in Garmin
Connect, such as upcoming races. Use this to answer questions about
which events or races are scheduled.

These entries are not returned by get_scheduled_workouts, which covers
only workouts, nor by get_goals. This is the only tool that exposes
them.

Each event reports its distance in meters when Garmin stores one,
the user's custom goal time in seconds when set, the local start
time when the organiser published it, and two flags: is_race marks
the entry as a race rather than a general event, and primary_event
marks the goal race that an active training plan targets. Goal time
comes from a per-event detail request.`,
	"get_course_details": `Get full details of a Garmin Connect course by ID.

Returns course metadata, elevation gain/loss, total distance,
and all custom course waypoints (shops, water, food, campgrounds, hazards).`,
	"get_courses": `List all courses saved on Garmin Connect.

Returns a curated list of courses with id, name, distance, activity type
and creation date.`,
	"get_custom_food_serving_units": `Get available serving units for custom foods

Returns the list of valid serving units (e.g. G, ML, OZ)
that can be used when creating custom foods.`,
	"get_custom_foods": `Search or list user's custom foods

Returns custom foods the user has created. Use the search parameter
to find existing foods by name before creating duplicates — the
response includes foodId and servingId needed for log_custom_food.

For branded catalog foods (FatSecret), use search_foods instead.`,
	"get_cycling_ftp": `Get the latest cycling Functional Threshold Power (FTP) data.

Returns the most recent cycling FTP estimate available from Garmin.`,
	"get_daily_steps":     `Get steps data for a date range`,
	"get_daily_weigh_ins": `Get weight measurements for a specific date`,
	"get_device_alarms": `Get alarms from all Garmin devices

Returns all configured alarms with their schedules, sounds, and enabled status.`,
	"get_device_last_used": `Get information about the last used Garmin device`,
	"get_device_settings": `Get settings for a specific Garmin device

Returns device configuration including time/date format, units,
activity tracking settings, and alarm information.`,
	"get_device_solar_data": `Get solar data for a specific device

Returns a per-day summary of solar charging for devices with solar panels
(e.g., Instinct Solar, Fenix Solar): activity time gained from solar, and
the average/peak solar utilization and minutes with solar input across the
day's per-minute readings. Only applicable to solar-capable devices.`,
	"get_devices":         `Get all Garmin devices associated with the user account`,
	"get_earned_badges":   `Get earned badges for user`,
	"get_endurance_score": `Get endurance score data between dates`,
	"get_energy_balance": `Estimate a composition-derived TDEE from logged intake, Garmin's
claimed expenditure, and body-composition change over a date range.

Combines three signals Garmin exposes separately -- mean logged intake
(nutrition log), mean claimed expenditure (Garmin's total_calories from
daily stats), and a body-composition trend (weight and body-fat % from
smart-scale readings) -- into composition_derived_tdee, an estimate
independent of Garmin's calorie model:
composition_derived_tdee = mean_intake + implied_deficit, where
implied_deficit comes from the fitted rate of fat and fat-free mass
change, using the standard energy-density approximations
7700 kcal/kg fat and 1800 kcal/kg fat-free mass. These are body
composition literature estimates, not Garmin-provided or measured
values -- treat the result as a rough estimate, not a lab measurement.
difference_kcal_per_day (Garmin's claim minus this estimate) has at
least two explanations -- Garmin overestimating expenditure, or
under-logged intake, which is common -- and this tool cannot
distinguish between them.

The body-composition trend is fit by least-squares regression across
every qualifying scale reading in range, not just the first and last --
an endpoint-to-endpoint difference is fully determined by two
individual measurements and inherits all of their noise (a 0.5%
body-fat wobble, well within normal scale repeatability, can swing a
two-point estimate by over a hundred kcal/day). weight_change_kg,
fat_mass_change_kg, and lean_mass_change_kg in body_composition are
all fitted values (the fitted line's value at the last reading's day
offset minus its value at the first), not raw differences between the
observed_first_*/observed_last_* fields, which are the actual
readings for reference/sanity-checking, not what the change figures
are computed from -- fitted_first_weight_kg/fitted_last_weight_kg are
also included so fitted_last_weight_kg - fitted_first_weight_kg can be
checked against weight_change_kg directly.

implied_deficit_kcal_per_day is the slope of a single regression: for
each reading, compute -(fat_kg * 7700 + lean_kg * 1800) where
lean_kg = weight_kg - fat_kg, then fit that value against day offset.
This is mathematically identical to combining separately-fit fat and
lean slopes, but its standard error -- implied_deficit_uncertainty_kcal_per_day,
returned whenever more than 2 readings are used -- is that single
fit's own residual standard error, which is well-defined and
reproducible from the raw readings alone (fat_kg and lean_kg are both
derived from the same weight/body-fat pair per reading, so treating
their separately-fit slopes' errors as independent and combining them
after the fact, instead of fitting the combined series directly,
would not be reproducible from outside without also knowing that
assumption). Treat a deficit within roughly one standard error of
zero as indistinguishable from no change.

Below a minimum span, or when most of the fit's statistical leverage
sits in one tight cluster of readings taken within about 48 hours of
each other, the fitted trend is not reliable even with 3+ readings --
a close, low-noise pair inside that cluster makes the fit look
precise while the slope is still mostly an extrapolation from that
cluster to wherever the other reading(s) fall. When this gate fails,
body_composition is still returned (it's just the fitted line, always
checkable against the observed readings) but derived is omitted and
gate_note explains which condition failed. Prefer windows of 3+ weeks
with several readings spread across them, not clustered at one end.

Short windows are also dominated by water/glycogen shifts (which
carry little caloric weight, unlike true lean tissue), which biases
the fat-free-mass term; see assumptions.note.

Exclusions applied automatically, matching get_nutrition_summary_between_dates
and get_stats_range:
- Intake: days with no logged food (item_count 0) are excluded from
  the mean, not counted as zero. Days with a low but nonzero
  item_count remain included (excluding them requires judgment this
  tool doesn't make unilaterally) but are called out in
  low_item_count_dates so low-confidence days are visible rather than
  silently blended into the mean. A window with many such days (or
  many item_count-0 days) biases composition_derived_tdee downward --
  check days_excluded_unlogged and low_item_count_dates before
  comparing two windows against each other.
- Expenditure: days with no device data, and the current (partial)
  day, are excluded from the mean.
- Body composition: only scale readings with both weight and body-fat
  percentage are used (skips weight-only manual entries).

Needs at least 2 qualifying body-composition readings to derive a
trend, and to pass the span/leverage gate above to derive a TDEE from
it. With 0 or 1 readings, returns the intake/expenditure means and
whatever body composition data exists, but omits the derived TDEE.

Maximum range: 61 days per call.`,
	"get_fitnessage_data": `Get fitness age data`,
	"get_floors":          `Get floors climbed data`,
	"get_full_name":       `Get user's full name from profile`,
	"get_garmin_coach_workouts": `Get Garmin Coach workouts around the given date

Returns workouts from the active Garmin Coach/training plan, including
plan metadata, workout identifiers, dates, sport, duration, completion
status, rest days, race days, and workout intent when Garmin provides
them. Adaptive plans expose only Garmin's currently generated window,
typically the current week; future dates may return no workouts even
while a plan is active. The count includes rest-day entries.

Garmin's standalone Daily Suggested Workouts are generated on compatible
devices. As of July 31, 2026, no supported or known Garmin Connect
web/API endpoint, including those exposed by this project's
python-garminconnect dependency, returns the device's upcoming DSW
schedule. This tool returns Garmin Coach/training-plan workouts and does
not synthesize device-generated suggestions.

This is the preferred tool for Garmin Coach requests. The legacy
get_training_plan_workouts tool returns the same data; do not call both.

Adaptive Coach plans typically expose workout_uuid; other plan families
may expose numeric workout_id. Pass whichever identifier is present to
get_workout_by_id. Rest-day UUIDs may return minimal detail without
workout segments.`,
	"get_gear": `Get all gear registered with the user account

Returns complete gear inventory including free-text notes, usage statistics,
and default activity associations. The notes field is null when unavailable.
defaults maps each activity type (Garmin typeKey, e.g. "running") to the
names of its default gear.
A gear item's stats_error reports a failed usage-statistics request;
a null total_distance_km means Garmin did not provide a distance.
No parameters required - user profile is fetched automatically.`,
	"get_goals": `Get Garmin Connect goals (active, future, or past).

Reads goals the way Garmin Connect's Goals page does, so named
distance/time targets (e.g. a monthly cycling mileage goal) are
included with their progress and remaining amounts. Falls back to
python-garminconnect's get_goals() if that request returns nothing.`,
	"get_heart_rate_zones": `Get the user's saved heart-rate training-zone configuration.

Garmin stores a generic DEFAULT profile plus optional sport-specific
overrides such as RUNNING and CYCLING. With no sport, this returns every
saved profile; pass a sport key to return just that profile.`,
	"get_heart_rates": `Get full heart rate time-series data

Note: This returns detailed 2-minute interval data (~25KB).
For a compact summary, use get_heart_rates_summary().`,
	"get_heart_rates_summary": `Get heart rate summary with essential metrics (lightweight version)

Returns a compact summary (~500 bytes) instead of full time-series data (~25KB).
Ideal for daily health checkups and LLM integrations.`,
	"get_hill_score": `Get hill score data between dates`,
	"get_hrv_data":   `Get Heart Rate Variability (HRV) data`,
	"get_hrv_trend": `Get HRV (Heart Rate Variability) trend over a date range.

Returns daily HRV values and weekly rolling averages. Single-day HRV is too noisy
to act on — use this tool to identify baseline shifts that signal accumulated fatigue
or recovery. A drop of >10ms from the 7-day baseline warrants reducing training load.

Recommended range: 7-21 days. Maximum: 30 days.
Empty days are skipped; a failed daily request returns an error.`,
	"get_hydration_data": `Get hydration data`,
	"get_inprogress_virtual_challenges": `Get in-progress virtual challenges/expeditions

Returns virtual challenges (like walking expeditions on famous trails)
that the user is currently participating in.`,
	"get_lactate_threshold": `Get lactate threshold data

Returns lactate threshold information, which is the exercise intensity at
which lactate starts to accumulate in the blood. This is a key metric for
endurance training.`,
	"get_lifestyle_logging_data": `Get lifestyle logging data for a specific date

Returns lifestyle logging data which allows users to track behaviors
and their impact on health metrics.`,
	"get_menstrual_calendar_data": `Get menstrual calendar data between specified dates

Automatically chunks requests longer than 92 days, Garmin's
server-side limit, and stitches the responses together.
Identical records spanning a chunk boundary are returned once for list
and dict-of-list responses. Other payload shapes are returned per chunk.`,
	"get_menstrual_data_for_date": `Get menstrual data for a specific date`,
	"get_morning_training_readiness": `Get morning training readiness score

Returns the morning training readiness assessment, which evaluates
recovery status and readiness to train based on overnight metrics.
Fields match one get_training_readiness entry (the snapshot taken
right after waking).`,
	"get_non_completed_badge_challenges": `Get badge challenges currently in progress (not yet completed)

Returns active challenges the user has joined but hasn't completed yet.
Useful for tracking current progress toward badge goals.`,
	"get_nutrition_daily_food_log": `Get daily food consumption records for a date

Returns food items logged throughout the day including calories,
macronutrients, and meal associations.`,
	"get_nutrition_daily_meals": `Get daily meal summaries for a date

Returns meal-level summaries (breakfast, lunch, dinner, snacks)
with nutritional totals for each meal. Each meal includes a mealId
needed for logging food items to that meal.`,
	"get_nutrition_daily_settings": `Get nutrition plan/settings for a date

Returns the user's nutrition goals and targets including
calorie targets, macronutrient goals, and plan configuration.`,
	"get_nutrition_summary_between_dates": `Get per-day nutrition totals for every day in a date range.

Returns one lightweight entry per day (date, calories, carbs, protein,
fat, item_count) instead of the full per-item food log that
get_nutrition_daily_food_log returns for a single date. Use this for
multi-day intake analysis (e.g. mean intake for a TDEE estimate)
instead of calling get_nutrition_daily_food_log once per day.

item_count is the number of logged food items that day. A day with
item_count == 0 has no logged food at all, and a low but nonzero
item_count may mean only part of the day was logged (e.g. breakfast
only). Both cases read as low intake in the totals alone -- exclude
low-item_count days explicitly before averaging intake or deriving
TDEE; don't infer "unlogged" from a low calorie total.

Maximum range: 61 days per call (Garmin's own limit for this endpoint).`,
	"get_personal_record": `Get personal records for user`,
	"get_power_duration_curve": `Get season-best Power Duration Curve across recent activities.

Downloads FIT files for recent activities of the requested type and computes best mean maximal
power at each standard duration. Returns season bests with which activity and date
each best came from.

Durations: 5s (sprint), 30s, 1min, 5min (VO2 max proxy), 10min, 20min (FTP proxy), 60min

Use the 20-minute best × 0.95 as a strong FTP estimate without a formal test.

Warning: downloads multiple FIT files — may take 30-60 seconds for 20 activities.`,
	"get_pregnancy_summary": `Get pregnancy summary data`,
	"get_primary_training_device": `Get information about the primary training device

Returns details about the device designated as primary for training
metrics, along with other wearable devices on the account.`,
	"get_progress_summary_between_dates": `Get progress summary for a metric between dates`,
	"get_race_predictions": `Get predicted race times based on current fitness level

Returns Garmin's predictions for 5K, 10K, half marathon, and marathon
finish times based on the user's recent training data and VO2 max.`,
	"get_recovery_time_remaining": `Get remaining recovery time in hours for a day.

This is the Firstbeat recovery clock shown on-device, distinct from
Training Readiness score (missing on some devices such as Forerunner 255)
and from Training Status ACWR. Training Readiness values are snapshots:
remaining_hours and state describe the snapshot at as_of (Garmin's local
timestamp when available), not a live countdown. remaining_hours_at_snapshot
retains the same value explicitly. For today only, when snapshots are
absent, the tool estimates remaining recovery by decaying recent activity
assignments and reports the estimate's as_of time. Today uses the server's
local date. Request failures are reported as errors, or source_errors when
another source supplies usable data.`,
	"get_respiration_data": `Get full respiration time-series data

Note: This returns detailed interval data (~20KB).
For a compact summary, use get_respiration_summary().`,
	"get_respiration_summary": `Get respiration summary with essential metrics (lightweight version)

Returns a compact summary (~300 bytes) instead of full time-series data (~20KB).`,
	"get_respiration_trend": `Get overnight respiration rate trend over a date range.

Elevated resting respiration rate (compared to personal baseline) is an early
warning sign for overreaching, illness, or poor recovery. Use this alongside HRV
trend for a complete recovery picture.

Recommended range: 7-21 days. Maximum: 30 days.
Empty days are skipped; a failed daily request returns an error.`,
	"get_rhr_day":           `Get resting heart rate data`,
	"get_running_tolerance": "Get Running Tolerance for a single day.\n\nReturns Garmin's running load capacity model: how much running load the\nathlete can currently absorb (tolerance), the intensity-adjusted load\ntheir recent runs have produced (acute load), and the raw distance behind\nthat load. All three are expressed in km so they're directly comparable —\n`load_ratio` (acute_load_km / distance_km) quantifies how much intensity\nis inflating the cost of each kilometer run.",
	"get_running_tolerance_trend": `Get Running Tolerance trend over a date range.

Running Tolerance moves slowly — its value is in the trajectory, not any
single day. Returns, per period: tolerance_km (current load capacity),
acute_load_km (intensity-adjusted load), distance_km (actual distance
run), and load_ratio (acute_load_km / distance_km — how much intensity
inflates the cost of each kilometer). Weekly aggregation (default) gives
a compact multi-month view; daily gives day-to-day resolution for a
shorter window.

Recommended range: 4-12 weeks. Maximum: 90 days for daily aggregation,
366 days for weekly (this endpoint returns the whole range in one call,
so the limit protects output size, not request volume).`,
	"get_scheduled_workouts": `Get scheduled workouts between two dates with curated summary list

Returns workouts that have been scheduled on the Garmin Connect calendar,
including their scheduled dates and completion status.`,
	"get_sleep_data": `Get full sleep data with all details

Note: This returns detailed sleep data (~50KB).
For a compact summary, use get_sleep_summary().`,
	"get_sleep_summary": `Get sleep summary with only essential metrics (lightweight version)

This endpoint returns a compact summary of sleep data (~350 bytes) instead of
the full granular data (~50KB). Ideal for daily health checkups and LLM integrations
where the full time-series data would overwhelm the context window.`,
	"get_sleep_summary_range": `Get lightweight sleep summaries for every night in a date range.

Returns the same curated metrics as get_sleep_summary (sleep score, duration,
sleep stages, HRV, resting HR, etc.) for each night between start_date and
end_date, inclusive. Use this instead of calling get_sleep_summary once per
night when analyzing sleep trends over weeks or months.

Note: Garmin Connect does not expose a native range endpoint for sleep, so
this makes one request per night internally. Recommended range: up to a
few weeks for quick checks. Maximum: 90 nights per call.
Empty nights are skipped; a failed request stops the range and returns
an error identifying the failed date.`,
	"get_spo2_data": `Get SpO2 (blood oxygen) data`,
	"get_stats": `Get daily activity stats with curated essential metrics

Returns a summary of daily health and activity data including steps,
calories, heart rate, stress, body battery, and sleep metrics.

Note: bmr_calories is Garmin's bmrKilocalories field -- per Garmin's
own documentation this is RMR (BMR plus sedentary-to-light movement),
not true basal metabolic rate. For the current date, all calorie and
duration fields are a partial-day accumulator (elapsed hours so far),
not a full-day total.`,
	"get_stats_and_body": `Get stats and body composition data`,
	"get_stats_range": `Get lightweight per-day calorie and step totals for a date range.

Returns total/active/resting calories and steps for each day between
start_date and end_date, inclusive, instead of the full get_stats
payload per day. Use this for multi-day analysis (e.g. comparing
measured intake/expenditure against Garmin's calorie model) instead
of calling get_stats once per day.

resting_calories is Garmin's bmrKilocalories field -- per Garmin's
own documentation this is RMR (BMR plus sedentary-to-light movement),
not true basal metabolic rate.

is_partial is true for the current date, whose accumulator only
covers elapsed hours so far -- treating it as a full day will
overstate a mean. Days with no device data (e.g. before the account
existed, or future dates within the range) return has_data: false
and null values rather than zeros, so a missing day never silently
enters an average as zero.

Maximum range: 28 days per call (Garmin's own limit for this endpoint).`,
	"get_steps_data": `Get detailed steps data with 15-minute intervals

Note: This returns full interval data (~14KB). For a compact summary,
use get_stats() which includes total_steps.`,
	"get_stress_data": `Get full stress time-series data

Note: This returns detailed interval data (~35KB) including body battery.
For a compact summary, use get_stress_summary().`,
	"get_stress_summary": `Get stress summary with essential metrics (lightweight version)

Returns a compact summary (~400 bytes) instead of full time-series data (~35KB).
Ideal for daily health checkups and LLM integrations.`,
	"get_training_effect": `Get training effect data for a specific activity.

Uses activity details first. If Garmin returns HTTP 403 for that
endpoint (seen on valid, recent activities), falls back to activity
list search, which still includes aerobic/anaerobic training effect.`,
	"get_training_load_balance": "Get Garmin's Load Focus — the distribution of the trailing-month\ntraining load across Aerobic Low, Aerobic High, and Anaerobic intensity\nbands, plus the system's feedback phrase (e.g. AEROBIC_HIGH_SHORTAGE,\nBALANCED, ANAEROBIC_SHORTAGE).\n\nUse this to assess whether the athlete's training mix is balanced or\ndeficient in a particular intensity band. Each band reports its load\nalongside Garmin's target range; a `status` of \"below\", \"within\", or\n\"above\" is computed from the load relative to that range.\n\nGarmin reports Load Focus per device. The primary activity tracker's\nrecord is used when it can be identified; `load_device_id` and\n`load_device_name` say which device the numbers came from.",
	"get_training_load_trend":   "Get Garmin's daily acute and chronic training load over a date range.\n\nReturns, per day, Garmin's acute load (`atl`, the 7-day load), chronic load (`ctl`), their\ndifference (`tsb` = ctl - atl), and Garmin's Acute:Chronic Workload Ratio (`acwr`,\n`acwr_status`). These are Garmin's dailyTrainingLoadAcute/Chronic values, not TrainingPeaks\nCTL/ATL: they are on Garmin's weekly load scale, so `tsb` is not comparable to TrainingPeaks\nTSB and TSB form thresholds do not apply. Use `acwr`/`acwr_status` (and the optimal chronic\nload range) to judge whether load is building steadily or spiking.\n\nRecommended range: 4-8 weeks. Maximum: 90 days.\nEmpty days are skipped; a failed daily request returns an error.",
	"get_training_plan_workouts": `Compatibility alias for get_garmin_coach_workouts

Prefer get_garmin_coach_workouts for new requests. This legacy tool
returns the same Garmin Coach/training-plan data; do not call both for
one request. Adaptive plans expose only Garmin's currently generated
window, typically the current week; future dates may return no workouts
even while a plan is active.

Adaptive training plans typically expose workout_uuid; other plan
families may expose numeric workout_id. Pass whichever identifier is
present to get_workout_by_id. The returned count includes rest days.`,
	"get_training_readiness": `Get training readiness data with curated metrics

Returns training readiness score and contributing factors.`,
	"get_training_status": `Get training status with curated metrics

Returns comprehensive training status including load, VO2 max, recovery,
and training readiness indicators.`,
	"get_unit_system":          `Get user's preferred unit system from profile`,
	"get_user_profile":         `Get user profile information`,
	"get_user_summary":         `Get user summary data (compatible with garminconnect-ha)`,
	"get_userprofile_settings": `Get user profile settings`,
	"get_vo2max_trend": `Get VO2 max trend over a date range.

Returns daily VO2 max estimates from Garmin's FirstBeat algorithm. Use this to track
whether training is producing fitness gains over weeks or months. Flat or declining
VO2 max over 4+ weeks suggests insufficient training stimulus or overreaching.

Note: VO2 max estimates are smoothed and update gradually — daily changes of <0.5
are within normal noise. Focus on the 4-6 week trend direction.

Garmin records a new VO2 max value only on days with a recompute (after an
activity). Days in between carry the last known value forward and are marked
with "carried_forward": true, matching the trend chart in Garmin Connect.
Dated status values retain their measurement date. data_points counts
displayed days, including carried-forward values.

If historical values are unavailable, the current profile estimate is returned
separately and is not represented as a historical trend point.
Endpoint-specific failures remain visible in request_errors when a
fallback succeeds. Authentication, rate-limit, and connection failures
stop the request.

Recommended range: 4-12 weeks. Maximum: 90 days.`,
	"get_weekly_intensity_minutes": `Get weekly intensity minutes data aggregates

Returns weekly intensity minutes (moderate and vigorous) for the specified
number of weeks ending at end_date. total_minutes counts each vigorous
minute twice (moderate + 2 x vigorous), as Garmin does, so it is the
figure to compare against weekly_goal; moderate_minutes and
vigorous_minutes are the raw, unweighted minutes.`,
	"get_weekly_steps": `Get weekly step data aggregates

Returns weekly step totals for the specified number of weeks ending at end_date.`,
	"get_weekly_stress": `Get weekly stress data aggregates

Returns weekly stress values for the specified number of weeks ending at end_date.`,
	"get_weigh_ins": `Get weight measurements between specified dates`,
	"get_workout_by_id": `Get detailed information for a specific workout

Returns workout details including segments and step structure.

Accepts either:
- Numeric workout ID (from get_workouts, get_scheduled_workouts, or
  training-plan families that expose workout_id)
- Workout UUID (from adaptive Garmin Coach/training-plan workouts)

Rest-day UUIDs can resolve to a minimal record without a workout name
or segments.

Pace target values are speeds in meters per second. target_value_low
and target_value_high preserve Garmin's targetValueOne/targetValueTwo
order; they are not sorted by magnitude.`,
	"get_workouts": `Get all workouts with curated summary list

Returns a count and list of workout summaries with essential metadata only.
For detailed workout information including segments, use get_workout_by_id.`,
	"list_accounts": `List the configured Garmin Connect accounts.`,
	"log_custom_food": `Log a food item to a meal on a date

Adds a food entry to the nutrition log. The meal is determined
automatically by matching meal_time against each meal's
startTime/endTime window; falls back to SNACKS if no window matches.

Food sources:
  - "GARMIN" (default): user's custom food library. Use
    get_custom_foods to find food_id and serving_id.
  - "FATSECRET": branded/catalog food from FatSecret. Use
    search_foods to find food_id and serving_id. Pass the source
    value from the search_foods result (e.g. "FATSECRET").

Garmin custom food IDs are 32-char hex UUIDs; FatSecret IDs are
numeric strings (e.g. "4132350"). Passing the wrong source for a
given food_id returns a 400 from Garmin.`,
	"log_food": `Quick-add a food entry with macro values to the nutrition log

Logs food directly by name and macros without requiring a food ID.
Uses Garmin's Quick Add feature. The meal is determined automatically
by matching meal_time against each meal's startTime/endTime window;
falls back to SNACKS if no window matches.`,
	"push_workout_to_device": `Send a workout from the workout library to a Garmin device

Queues the workout in Garmin Connect's device message queue, the same
as "Send to Device" in the Garmin Connect app. The watch picks it up
at its next sync with Garmin Connect (phone app or Wi-Fi), so it does
not appear on the watch immediately. Scheduling a workout does not
need this: scheduled workouts reach the watch's calendar on their own.

Each call queues another copy, so do not push the same workout twice.`,
	"remove_gear_from_activity": `Remove gear association from an activity

Unlinks a specific piece of gear from an activity.`,
	"request_reload": `Request reload of epoch data`,
	"reschedule_workout": `Move a scheduled workout to another date on the Garmin Connect calendar

Moves the existing calendar entry in place, so its id stays the same
and no duplicate entry is created. Prefer this over unschedule +
schedule. Safe to repeat: moving an entry to the date it is already
on changes nothing.

IMPORTANT: scheduled_workout_id is the calendar-entry id, which is
different from the workout's id. Get it from get_scheduled_workouts
(the "scheduled_workout_id" field), not from get_workouts.`,
	"schedule_week": `Schedule a list of workouts for the week in a single call.

Idempotent: if a workout is already scheduled for that date, it is
reported as already scheduled and the POST is skipped (avoids
duplicating calendar entries).

Each entry is handled on its own: an invalid or failing entry is
reported with status "failed" or "error" and the rest still run.
A successful entry includes scheduled_workout_id when Garmin returns
the calendar-entry ID; pass it to unschedule_workout to remove the entry.`,
	"schedule_workout": `Schedule a workout to a specific calendar date

This adds an existing workout from your Garmin workout library
to your Garmin Connect calendar on the specified date.

Idempotent: if the workout is already scheduled for that date, this
is a no-op that reports success without creating a duplicate entry.`,
	"schedule_workouts": `Schedule multiple workouts to specific calendar dates

This adds workouts to your Garmin Connect calendar in a single call.
Each item can either reference an existing workout by ID, or provide
inline workout_data to upload-and-schedule in one step.`,
	"search_foods": `Search Garmin's general food catalog (FatSecret + Garmin custom foods)

Searches across the entire food catalog including FatSecret-sourced
branded and generic foods, not just the user's Garmin custom foods.
Use this to find branded packaged foods by name before logging them.

Returns food_id, source, name, brand, and all available servings with
macros. The source field ("FATSECRET" or "GARMIN") and food_id together
identify the right routing for log_custom_food — pass both to
log_custom_food's food_id and source parameters respectively.

For the user's own custom foods only, use get_custom_foods instead.`,
	"set_activity_description": `Set or update the free-text description (notes) of an activity.

This is the notes field shown on the activity page — useful for
recording how a session felt, kit used, conditions, niggles, etc.
Pass an empty string to clear an existing description.`,
	"set_activity_event_type": `Set the event type of an activity.

Event type categorises the activity's purpose. Valid keys:
race, recreation, specialEvent, training, transportation, touring,
geocaching, fitness, uncategorized.`,
	"set_activity_exercise_sets": `Replace the exercise sets on a strength (or other rep-counting) activity.

Writes Garmin's structured per-set data, the same grid the Garmin Connect
"Edit sets" screen saves, so the sets feed Garmin's exercise history,
volume and muscle-map charts. This OVERWRITES every existing set,
including watch-detected sets and their recorded timing; to keep some,
read them with get_activity_exercise_sets and include them in the list.

When the watch already recorded the right sets but misidentified the
exercises, pass keep_timing=True: the existing sets keep their start
times, durations and rest periods, and only the exercise, reps and
weight are replaced, in order. The number of sets must match the
activity's recorded active sets, and duration_seconds/rest_seconds are
ignored.

Exercises must use Garmin's own keys. category is an exercise category
(e.g. DEADLIFT, BENCH_PRESS, PULL_UP, SQUAT) and exercise_name an
exercise within it (e.g. BARBELL_DEADLIFT, ROMANIAN_DEADLIFT). Garmin
rejects anything else with "Invalid Category" or "Invalid Sub-Category",
and rejects the whole request, so a typo leaves the activity unchanged.
Valid keys, grouped by category, are in
https://connect.garmin.com/web-data/exercises/Exercises.json`,
	"set_activity_feel": `Set how an activity felt ('How did you feel?').

Mirrors Garmin Connect's 5-point feel rating, stored as one of:
  0   = very tired / poor
  25  = tired
  50  = normal
  75  = good
  100 = strong
Higher is better.`,
	"set_activity_name": `Set or update the name of an activity.`,
	"set_activity_type": `Change the activity type (sport) of an activity.

Useful for reclassifying a mislabelled activity, e.g. flipping a run
logged as 'trail_running' to 'running', or a 'treadmill_running' walk to
'treadmill_walking'. Call get_activity_types to see all valid type keys.`,
	"set_blood_pressure": `Set blood pressure values`,
	"set_fit_download_dir": `Set and persist the default directory for downloaded activity files.

Stores the absolute path in a small JSON config file
(~/.garminconnect_fit_config.json, overridable via GARMIN_FIT_CONFIG) so
download_activity_file can save files without asking again.`,
	"set_heart_rate_zones": `Set an account-level heart-rate training-zone configuration for one sport.

WARNING: This mutates the user's Garmin Connect account configuration.
Garmin stores zones per sport, so DEFAULT (the generic fallback),
RUNNING, CYCLING, and other sport profiles are independent. Only the
requested sport is written; omitted values are read from that sport's
current configuration and preserved.

This change affects future activity recording and zone-based training.
It does NOT retroactively re-slice already-recorded activities: their
heart-rate zone boundaries were baked in when those activities were
recorded.

The calculation method may be max_hr (% maximum heart rate), hrr or
karvonen (% heart-rate reserve), lthr (% lactate-threshold heart rate),
or custom_bpm. Manual zone_boundaries are only accepted with the
custom_bpm method. Garmin's API does not persist a separate CUSTOM enum:
direct-BPM zones are sent and read back with trainingMethod=HR_MAX while
the explicit zone floors remain authoritative.
Changing the method or reference heart rates preserves the existing
zone floors; this tool does not calculate new percentage-based floors.

The tool performs a read-modify-write and then re-fetches the requested
sport. Its result is the configuration Garmin actually saved, not the
request payload. If verification fails after Garmin accepts the write,
it reports success with write_committed=true, a confirmation_error, and
the submitted_configuration. Re-read the profile before another update.`,
	"set_nutrition_daily_settings": `Update daily nutrition goals (calorie target and macronutrient targets).

Reads the current settings for the date, applies the supplied overrides,
and writes the merged result back.  Only the fields you provide are
changed; omitted fields keep their existing values.

Macro arguments retain their existing names and are passed through to
Garmin's macroGoals without unit conversion. The endpoint's macro units
have not been independently verified. Returned goals come from Garmin's
response, or a settings read-back when the update has no response body.`,
	"set_perceived_effort": `Set the perceived effort (RPE) for an activity.

Mirrors Garmin Connect's 'Perceived Effort' rating on a 0-10 scale,
where 0 clears the rating. Internally Garmin stores this multiplied by
10 (so RPE 7 is stored as 70); this tool handles the conversion.`,
	"unschedule_workout": `Remove a scheduled workout from the Garmin Connect calendar

Deletes a calendar entry without deleting the underlying workout
template — the workout stays in your library and can be re-scheduled.

IMPORTANT: scheduled_workout_id is the calendar-entry id, which is
different from the workout's id. Get it from get_scheduled_workouts
(the "scheduled_workout_id" field), not from get_workouts.

Note: the scheduled-workouts listing is an eventually-consistent index.
If you just scheduled this workout, allow a moment before unscheduling
so the id is available.`,
	"unschedule_workouts": `Remove multiple scheduled workouts from the Garmin Connect calendar

Deletes multiple calendar entries in a single call. The underlying
workout templates are left intact in your library.

IMPORTANT: each id is a calendar-entry id (the "scheduled_workout_id"
field from get_scheduled_workouts), not a workout id.`,
	"update_custom_food": `Update an existing custom food in the user's Garmin nutrition library

Fetches the food's current record (by food_id, so renaming is safe)
before writing so that omitted optional fields (brand, carbs, protein,
fat, micros, etc.) preserve their existing values rather than being
cleared. Only the fields you explicitly pass are changed; everything
else is carried forward from the current record.

Garmin replaces the whole record on update. If the current record
cannot be read, the update is refused unless serving_unit and
number_of_units are given; the call then replaces the record in full,
and any brand or nutrient you omit is cleared.

Only future logs see the change. Each food-log entry stores its own
copy of the food's nutrition from when it was logged, so entries
already logged, and those days' summaries, keep the old values. To
correct a past day, delete_food_log each affected entry and log it
again after the update.

All nutrient amounts are ABSOLUTE values per serving, not %DV.
Nutrition labels often print %DV for calcium/iron/vitamin D —
convert to absolute units before passing.

Use get_custom_foods first to find the foodId and servingId.`,
	"upload_course": `Upload a GPX file as a Garmin Connect Course.

The course can then be loaded onto the watch (sync or "Send to Device")
and used as a navigation course or to build a PacePro strategy.`,
	"upload_workout": `Upload a workout from JSON data

Creates a new workout in Garmin Connect from structured workout data.

IMPORTANT: Step types must use Garmin's DTO format:
- Use "ExecutableStepDTO" for regular steps (warmup, interval, cooldown, recovery)
- Use "RepeatGroupDTO" for repeat/interval groups with numberOfIterations.
  Always include endCondition with conditionTypeId 7 and conditionTypeKey
  "iterations"; omitting conditionTypeId causes the API to silently corrupt
  the repeat count.
- stepOrder is one depth-first counter over the whole workout: a repeat
  group takes the next number and its nested steps continue from there
  (group 2, children 3 and 4, cooldown 5). Do not restart at 1 inside a group.

IMPORTANT: Heart rate targets come in two forms:
- Named zone (e.g. Zone 2): set targetType to "heart.rate.zone" and use "zoneNumber" (1-5).
  Do NOT put the zone number in targetValueOne.
- Custom HR range (e.g. 105-143 bpm): set targetType to "heart.rate.zone" and use
  "targetValueOne" (low bpm) / "targetValueTwo" (high bpm). Do NOT set "zoneNumber".
  This matches Garmin Connect's "Custom" heart rate target.
For non-HR targets (pace, power, cadence), use targetValueOne/targetValueTwo directly.
Pace targets are speeds in meters per second, not min/km:
5:00/km = 1000 m / 300 s = 3.333 m/s.
Target values are fields on the workout step, alongside targetType; do not put
targetValueOne, targetValueTwo, or zoneNumber inside the targetType object.
Use either zoneNumber or targetValueOne/targetValueTwo, not both. Garmin silently
discards a custom range when a named zone is also present.

Note: a safety check converts targetValueOne 1-5 to zoneNumber when zoneNumber is missing,
to catch the common mistake of putting a zone index in targetValueOne. Typical bpm values
(e.g. 105, 143) are not affected.

IMPORTANT: Target type IDs and keys must match Garmin's canonical mapping.
Garmin treats workoutTargetTypeId as authoritative, so mismatches are rejected
before upload.  Known mappings:
- workoutTargetTypeId 1  -> "no.target"
- workoutTargetTypeId 2  -> "power.zone"  (cycling power zone 1-7, use zoneNumber)
- workoutTargetTypeId 4  -> "heart.rate.zone"
- workoutTargetTypeId 6  -> "pace.zone" (running/swim) OR "power.between" (cycling)

IMPORTANT: For cycling power targets use the correct target type:
- Power zone (zone 1-7 based on FTP %): use workoutTargetTypeId 2, key "power.zone",
  and "zoneNumber" (1-7).
- Custom % FTP range (e.g. 76-81% FTP): use workoutTargetTypeId 2, key "power.zone",
  "targetValueOne"/"targetValueTwo" as percentages and
  "targetValueUnit": {"unitKey": "percent"}. Garmin Connect shows it as
  "Custom Power 76-81% FTP" with the matching watts.
- Absolute watt range (e.g. 200-250 W): use workoutTargetTypeId 6, key "power.between",
  and "targetValueOne" (low watts) / "targetValueTwo" (high watts).
Using workoutTargetTypeId 2 with key "power.between" is a silent Garmin bug: the
workout uploads but Garmin stores it as "power.zone" and the intent is lost.

Use {"workoutTargetTypeId": 4, "workoutTargetTypeKey": "heart.rate.zone"} with
targetValueOne/targetValueTwo for custom heart-rate ranges.

IMPORTANT: Sport type IDs for workouts (different from activity API!):
- 1 = running, 2 = cycling, 5 = strength_training, 6 = cardio_training, 12 = walking

IMPORTANT: End condition IDs and keys must match Garmin's canonical mapping.
Garmin treats conditionTypeId as authoritative, so mismatches such as
{"conditionTypeId": 4, "conditionTypeKey": "heart.rate"} are rejected before
upload because Garmin would interpret them as "calories". Use
{"conditionTypeId": 6, "conditionTypeKey": "heart.rate"} for heart-rate
end conditions.

**Available Templates:**
Instead of building workout JSON from scratch, you can use these MCP resources as starting points:
- workout://templates/simple-run - Basic warmup/run/cooldown structure
- workout://templates/interval-running - Interval training with repeat groups
- workout://templates/tempo-run - Tempo run with heart rate zone targets
- workout://templates/strength-circuit - Strength training with exercises, reps, rest
- workout://reference/structure - Complete JSON structure reference with all fields

Access these resources using your MCP client's resource reading capability, modify the template
as needed, and pass the resulting JSON as the workout_data parameter.

**Strength training workouts** require these additional fields on each exercise step:
- "category": exercise category (e.g. "BENCH_PRESS", "PULL_UP", "CURL", "SHOULDER_PRESS",
  "ROW", "SQUAT", "DEADLIFT", "TRICEPS_EXTENSION", "PLANK", "LUNGE", "CARDIO")
- "exerciseName": specific exercise (e.g. "BARBELL_BENCH_PRESS", "PULL_UP",
  "DUMBBELL_BICEPS_CURL", "DUMBBELL_SHOULDER_PRESS", "BENT_OVER_ROW_WITH_DUMBELL",
  "BODY_WEIGHT_DIP", "BARBELL_SQUAT", "BARBELL_DEADLIFT")
- "weightValue" (optional): weight as number (e.g. 24.0)
- "weightUnit" (optional): {"unitId": 8, "unitKey": "kilogram", "factor": 1000.0}
Use endCondition reps (conditionTypeId: 10) for exercises, rest (stepTypeId: 5) between sets.

Example strength exercise step:
{
    "type": "ExecutableStepDTO",
    "stepOrder": 1,
    "stepType": {"stepTypeId": 3, "stepTypeKey": "interval"},
    "endCondition": {"conditionTypeId": 10, "conditionTypeKey": "reps"},
    "endConditionValue": 10.0,
    "targetType": {"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target"},
    "category": "BENCH_PRESS",
    "exerciseName": "BARBELL_BENCH_PRESS",
    "weightValue": 60.0,
    "weightUnit": {"unitId": 8, "unitKey": "kilogram", "factor": 1000.0}
}

Example running workout with HR zone target:
{
    "workoutName": "My Workout",
    "sportType": {"sportTypeId": 1, "sportTypeKey": "running"},
    "workoutSegments": [{
        "segmentOrder": 1,
        "sportType": {"sportTypeId": 1, "sportTypeKey": "running"},
        "workoutSteps": [{
            "type": "ExecutableStepDTO",
            "stepOrder": 1,
            "stepType": {"stepTypeId": 3, "stepTypeKey": "interval"},
            "endCondition": {"conditionTypeId": 2, "conditionTypeKey": "time"},
            "endConditionValue": 1200.0,
            "targetType": {"workoutTargetTypeId": 4, "workoutTargetTypeKey": "heart.rate.zone"},
            "zoneNumber": 3
        }]
    }]
}`,
	"upload_workouts": `Upload multiple workouts from JSON data in a single call

Creates multiple new workouts in Garmin Connect. Each item in the list
uses the same structure as upload_workout.

IMPORTANT: Step types must use Garmin's DTO format:
- Use "ExecutableStepDTO" for regular steps (warmup, interval, cooldown, recovery)
- Use "RepeatGroupDTO" for repeat/interval groups with numberOfIterations.
  Always include endCondition with conditionTypeId 7 and conditionTypeKey
  "iterations"; omitting conditionTypeId causes the API to silently corrupt
  the repeat count.
- stepOrder is one depth-first counter over the whole workout: a repeat
  group takes the next number and its nested steps continue from there
  (group 2, children 3 and 4, cooldown 5). Do not restart at 1 inside a group.

IMPORTANT: For named heart rate zone targets, use "zoneNumber" (1-5), NOT targetValueOne/targetValueTwo.
For custom heart-rate ranges, use targetType {"workoutTargetTypeId": 4,
"workoutTargetTypeKey": "heart.rate.zone"} with targetValueOne/targetValueTwo.
Target values belong on the workout step, alongside targetType, not inside it.
For cycling power zone targets (zone-based), use workoutTargetTypeId 2, key "power.zone".
For cycling absolute watt range targets, use workoutTargetTypeId 6, key "power.between",
with targetValueOne (low watts) and targetValueTwo (high watts).
Pace targets use targetValueOne/targetValueTwo in meters per second, not
min/km: 5:00/km = 1000 m / 300 s = 3.333 m/s.
Target type IDs and keys must match Garmin's canonical mapping.

IMPORTANT: End condition IDs and keys must match Garmin's canonical mapping.
Garmin treats conditionTypeId as authoritative, so mismatches are rejected before upload.`,
	"upsert_and_log": `Find-or-create a custom food then log it in one step

Searches the user's custom food library for food_name. If found, logs
it immediately. If not found, creates it with the provided nutrition
data and then logs it. This avoids duplicate food entries and removes
the need for separate search → create → log round-trips.

Matching is by food_name alone (case-insensitive) — brand_name never
affects find-or-create lookup, so it can't cause a duplicate by missing
an existing unbranded record. If an existing match is found, it is
logged as stored, using its first serving: calories, carbs, protein,
fat, brand_name, serving_unit and number_of_units are all ignored and
the stored food is not changed; only serving_qty applies. Those values
only take effect when creating a new food. To change an existing
food's nutrition, serving size or brand, use update_custom_food.

On an error after creation, custom_food_created is true and
write_committed describes the food log, not the library food. A failed
or exhausted lookup never triggers another create automatically.`,
}

// TestToolDescriptionsMatchPython catches description drift from the pinned
// Python reference.
func TestToolDescriptionsMatchPython(t *testing.T) {
	cs := newSession(t, newFakeGarmin(t), nil)
	registered := map[string]string{}
	for _, tool := range listToolObjects(t, cs) {
		registered[tool.Name] = tool.Description
	}
	for name, want := range pythonToolDescriptions {
		got, ok := registered[name]
		if !ok {
			t.Errorf("%s is not registered", name)
			continue
		}
		if got != want {
			t.Errorf("%s description drifted from Python\ngot:  %q\nwant: %q", name, got, want)
		}
	}
	for name := range registered {
		if _, ok := pythonToolDescriptions[name]; !ok {
			t.Errorf("%s has no expected description", name)
		}
	}
	if len(pythonToolDescriptions) != 158 {
		t.Fatalf("pythonToolDescriptions has %d entries, want 158", len(pythonToolDescriptions))
	}
}
