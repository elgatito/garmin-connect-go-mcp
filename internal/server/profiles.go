// Copied from garmin_mcp/tool_profiles.py, plus list_accounts in the core set (so every profile has it).

package server

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// coreTools: identity and unit settings that every profile includes.
var coreTools = []string{
	"get_full_name",
	"get_heart_rate_zones",
	"get_unit_system",
	"get_user_profile",
	"get_userprofile_settings",
	"list_accounts",
}

// toolProfiles maps GARMIN_TOOL_PROFILE names to their tools; nil means every tool.
var toolProfiles = map[string][]string{
	"all": nil,
	"minimal": append(slices.Clone(coreTools),
		"get_activities",
		"get_activities_by_date",
		"get_activity",
		"get_activity_splits",
		"get_body_battery",
		"get_daily_steps",
		"get_heart_rates_summary",
		"get_hrv_data",
		"get_sleep_data",
		"get_sleep_summary",
		"get_stats",
		"get_stress_summary",
		"get_training_readiness",
		"get_training_status",
		"get_user_summary",
	),
	"health": append(slices.Clone(coreTools),
		"add_body_composition",
		"add_hydration_data",
		"add_weigh_in",
		"add_weigh_in_with_timestamps",
		"delete_weigh_ins",
		"get_all_day_events",
		"get_all_day_stress",
		"get_blood_pressure",
		"get_body_battery",
		"get_body_battery_events",
		"get_body_composition",
		"get_daily_steps",
		"get_daily_weigh_ins",
		"get_energy_balance",
		"get_fitnessage_data",
		"get_floors",
		"get_heart_rates",
		"get_heart_rates_summary",
		"get_hrv_data",
		"get_hrv_trend",
		"get_hydration_data",
		"get_lifestyle_logging_data",
		"get_menstrual_calendar_data",
		"get_menstrual_data_for_date",
		"get_morning_training_readiness",
		"get_pregnancy_summary",
		"get_recovery_time_remaining",
		"get_respiration_data",
		"get_respiration_summary",
		"get_respiration_trend",
		"get_rhr_day",
		"get_sleep_data",
		"get_sleep_summary",
		"get_sleep_summary_range",
		"get_spo2_data",
		"get_stats",
		"get_stats_and_body",
		"get_stats_range",
		"get_steps_data",
		"get_stress_data",
		"get_stress_summary",
		"get_training_readiness",
		"get_user_summary",
		"get_weekly_intensity_minutes",
		"get_weekly_steps",
		"get_weekly_stress",
		"get_weigh_ins",
		"set_blood_pressure",
	),
	"activities": append(slices.Clone(coreTools),
		"add_gear_to_activity",
		"count_activities",
		"create_manual_activity",
		"delete_activity",
		"delete_course",
		"download_activity_file",
		"download_course_gpx",
		"get_activities",
		"get_activities_by_date",
		"get_activities_fordate",
		"get_activity",
		"get_activity_exercise_sets",
		"get_activity_fit_data",
		"get_activity_fit_messages",
		"get_activity_gear",
		"get_activity_hr_in_timezones",
		"get_activity_power_in_timezones",
		"get_activity_split_summaries",
		"get_activity_splits",
		"get_activity_typed_splits",
		"get_activity_types",
		"get_activity_weather",
		"get_course_details",
		"get_courses",
		"get_gear",
		"get_power_duration_curve",
		"remove_gear_from_activity",
		"set_activity_description",
		"set_activity_event_type",
		"set_activity_exercise_sets",
		"set_activity_feel",
		"set_activity_name",
		"set_activity_type",
		"set_fit_download_dir",
		"set_perceived_effort",
		"upload_course",
	),
	"workouts": append(slices.Clone(coreTools),
		"create_run_interval_workout",
		"create_run_workout",
		"create_strength_workout",
		"create_walk_run_workout",
		"create_z2_walk_workout",
		"delete_workout",
		"delete_workouts",
		"download_workout",
		"get_calendar_events",
		"get_garmin_coach_workouts",
		"get_scheduled_workouts",
		"get_training_plan_workouts",
		"get_workout_by_id",
		"get_workouts",
		"schedule_week",
		"schedule_workout",
		"schedule_workouts",
		"unschedule_workout",
		"unschedule_workouts",
		"upload_workout",
		"upload_workouts",
	),
	"training": append(slices.Clone(coreTools),
		"get_acclimation",
		"get_adhoc_challenges",
		"get_available_badge_challenges",
		"get_badge_challenges",
		"get_cycling_ftp",
		"get_earned_badges",
		"get_endurance_score",
		"get_fitnessage_data",
		"get_goals",
		"get_hill_score",
		"get_hrv_data",
		"get_hrv_trend",
		"get_inprogress_virtual_challenges",
		"get_lactate_threshold",
		"get_morning_training_readiness",
		"get_non_completed_badge_challenges",
		"get_personal_record",
		"get_power_duration_curve",
		"get_primary_training_device",
		"get_progress_summary_between_dates",
		"get_race_predictions",
		"get_recovery_time_remaining",
		"get_respiration_trend",
		"get_running_tolerance",
		"get_running_tolerance_trend",
		"get_training_effect",
		"get_training_load_balance",
		"get_training_load_trend",
		"get_training_readiness",
		"get_training_status",
		"get_vo2max_trend",
		"get_weekly_intensity_minutes",
		"request_reload",
	),
	"nutrition": append(slices.Clone(coreTools),
		"add_hydration_data",
		"create_custom_food",
		"delete_custom_food",
		"delete_food_log",
		"get_custom_food_serving_units",
		"get_custom_foods",
		"get_energy_balance",
		"get_hydration_data",
		"get_nutrition_daily_food_log",
		"get_nutrition_daily_meals",
		"get_nutrition_daily_settings",
		"get_nutrition_summary_between_dates",
		"log_custom_food",
		"log_food",
		"search_foods",
		"set_nutrition_daily_settings",
		"update_custom_food",
		"upsert_and_log",
	),
}

func toSet(names []string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// resolveToolProfile turns GARMIN_TOOL_PROFILE into the selected tool set; nil means every tool.
// Port of resolve_tool_profile: comma-separated union, case-insensitive, "all" or blank selects everything.
func resolveToolProfile(value string) (map[string]bool, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	names := parseNames(value)
	if len(names) == 0 {
		return nil, errors.New("Invalid GARMIN_TOOL_PROFILE: expected at least one profile name")
	}
	var unknown []string
	for _, n := range names {
		if _, ok := toolProfiles[n]; !ok {
			unknown = append(unknown, fmt.Sprintf("'%s'", n))
		}
	}
	if len(unknown) > 0 {
		known := make([]string, 0, len(toolProfiles))
		for k := range toolProfiles {
			known = append(known, k)
		}
		sort.Strings(known)
		return nil, fmt.Errorf("Unknown GARMIN_TOOL_PROFILE %s. Known profiles: %s", strings.Join(unknown, ", "), strings.Join(known, ", "))
	}
	if slices.Contains(names, "all") {
		return nil, nil
	}
	set := map[string]bool{}
	for _, n := range names {
		for _, tool := range toolProfiles[n] {
			set[tool] = true
		}
	}
	return set, nil
}

// parseNames splits a comma list into trimmed, lowercased, non-empty names.
func parseNames(value string) []string {
	var out []string
	for _, n := range strings.Split(value, ",") {
		if n = strings.ToLower(strings.TrimSpace(n)); n != "" {
			out = append(out, n)
		}
	}
	return out
}
