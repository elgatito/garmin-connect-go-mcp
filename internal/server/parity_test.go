package server

import (
	"sort"
	"strings"
	"testing"
)

// pythonTools is the complete tool set of taxuspt/garmin_mcp at a2ca5fc, in
// alphabetical order, generated from its @app.tool declarations:
// grep -A3 "@app.tool" $REF/garmin_mcp/src/garmin_mcp/*.py |
// grep -o "def [a-z_0-9]*" | sed 's/def //' | sort
var pythonTools = strings.Fields(`
add_body_composition
add_gear_to_activity
add_hydration_data
add_weigh_in
add_weigh_in_with_timestamps
count_activities
create_custom_food
create_manual_activity
create_run_interval_workout
create_run_workout
create_strength_workout
create_walk_run_workout
create_z2_walk_workout
delete_activity
delete_course
delete_custom_food
delete_food_log
delete_weigh_ins
delete_workout
delete_workouts
download_activity_file
download_course_gpx
download_workout
get_acclimation
get_activities
get_activities_by_date
get_activities_fordate
get_activity
get_activity_exercise_sets
get_activity_fit_data
get_activity_fit_messages
get_activity_gear
get_activity_hr_in_timezones
get_activity_power_in_timezones
get_activity_split_summaries
get_activity_splits
get_activity_typed_splits
get_activity_types
get_activity_weather
get_adhoc_challenges
get_all_day_events
get_all_day_stress
get_available_badge_challenges
get_badge_challenges
get_blood_pressure
get_body_battery
get_body_battery_events
get_body_composition
get_calendar_events
get_course_details
get_courses
get_custom_food_serving_units
get_custom_foods
get_cycling_ftp
get_daily_steps
get_daily_weigh_ins
get_device_alarms
get_device_last_used
get_device_settings
get_device_solar_data
get_devices
get_earned_badges
get_endurance_score
get_energy_balance
get_fitnessage_data
get_floors
get_full_name
get_garmin_coach_workouts
get_gear
get_goals
get_heart_rate_zones
get_heart_rates
get_heart_rates_summary
get_hill_score
get_hrv_data
get_hrv_trend
get_hydration_data
get_inprogress_virtual_challenges
get_lactate_threshold
get_lifestyle_logging_data
get_menstrual_calendar_data
get_menstrual_data_for_date
get_morning_training_readiness
get_non_completed_badge_challenges
get_nutrition_daily_food_log
get_nutrition_daily_meals
get_nutrition_daily_settings
get_nutrition_summary_between_dates
get_personal_record
get_power_duration_curve
get_pregnancy_summary
get_primary_training_device
get_progress_summary_between_dates
get_race_predictions
get_recovery_time_remaining
get_respiration_data
get_respiration_summary
get_respiration_trend
get_rhr_day
get_running_tolerance
get_running_tolerance_trend
get_scheduled_workouts
get_sleep_data
get_sleep_summary
get_sleep_summary_range
get_spo2_data
get_stats
get_stats_and_body
get_stats_range
get_steps_data
get_stress_data
get_stress_summary
get_training_effect
get_training_load_balance
get_training_load_trend
get_training_plan_workouts
get_training_readiness
get_training_status
get_unit_system
get_user_profile
get_user_summary
get_userprofile_settings
get_vo2max_trend
get_weekly_intensity_minutes
get_weekly_steps
get_weekly_stress
get_weigh_ins
get_workout_by_id
get_workouts
log_custom_food
log_food
push_workout_to_device
remove_gear_from_activity
request_reload
reschedule_workout
schedule_week
schedule_workout
schedule_workouts
search_foods
set_activity_description
set_activity_event_type
set_activity_exercise_sets
set_activity_feel
set_activity_name
set_activity_type
set_blood_pressure
set_fit_download_dir
set_heart_rate_zones
set_nutrition_daily_settings
set_perceived_effort
unschedule_workout
unschedule_workouts
update_custom_food
upload_course
upload_workout
upload_workouts
upsert_and_log
`)

// TestToolParity pins the Go server to the Python tool surface: every Python
// tool plus list_accounts, and nothing else.
func TestToolParity(t *testing.T) {
	if len(pythonTools) != 157 {
		t.Fatalf("pythonTools has %d names, want 157", len(pythonTools))
	}
	got := callToolNames(t, newSession(t, newFakeGarmin(t), nil))

	want := map[string]bool{"list_accounts": true}
	for _, name := range pythonTools {
		want[name] = true
	}

	var missing, extra []string
	for name := range want {
		if !got[name] {
			missing = append(missing, name)
		}
	}
	for name := range got {
		if !want[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("missing tools: %s", strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		t.Errorf("unexpected tools: %s", strings.Join(extra, ", "))
	}
	if len(got) != 158 {
		t.Errorf("ListTools returned %d tools, want 158", len(got))
	}
}

func TestListAccountsIsLocalReadOnly(t *testing.T) {
	cs := newSession(t, newFakeGarmin(t), nil)
	for _, tool := range listToolObjects(t, cs) {
		if tool.Name != "list_accounts" {
			continue
		}
		a := tool.Annotations
		if a == nil || a.ReadOnlyHint != true || a.OpenWorldHint == nil || *a.OpenWorldHint {
			t.Fatalf("annotations = %+v", a)
		}
		return
	}
	t.Fatal("list_accounts missing")
}

// TestProfilesReferenceRealTools ports test_tool_profiles.py's membership
// check: every name listed by a profile must be a registered tool.
func TestProfilesReferenceRealTools(t *testing.T) {
	registered := callToolNames(t, newSession(t, newFakeGarmin(t), nil))
	for profile, tools := range toolProfiles {
		if tools == nil {
			continue
		}
		var unknown []string
		for name := range tools {
			if !registered[name] {
				unknown = append(unknown, name)
			}
		}
		sort.Strings(unknown)
		if len(unknown) > 0 {
			t.Errorf("profile %q lists unknown tools: %s", profile, strings.Join(unknown, ", "))
		}
	}
}
