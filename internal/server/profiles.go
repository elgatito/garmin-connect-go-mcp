package server

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Tool profile sets copied from the Python tool_profiles.py registry.
var coreTools = map[string]bool{
	"get_full_name":            true,
	"get_heart_rate_zones":     true,
	"get_unit_system":          true,
	"get_user_profile":         true,
	"get_userprofile_settings": true,
}

var minimalTools = union(coreTools, map[string]bool{
	"get_activities":          true,
	"get_activities_by_date":  true,
	"get_activity":            true,
	"get_activity_splits":     true,
	"get_body_battery":        true,
	"get_daily_steps":         true,
	"get_heart_rates_summary": true,
	"get_hrv_data":            true,
	"get_sleep_data":          true,
	"get_sleep_summary":       true,
	"get_stats":               true,
	"get_stress_summary":      true,
	"get_training_readiness":  true,
	"get_training_status":     true,
	"get_user_summary":        true,
})

var healthTools = union(coreTools, map[string]bool{
	"add_body_composition":           true,
	"add_hydration_data":             true,
	"add_weigh_in":                   true,
	"add_weigh_in_with_timestamps":   true,
	"delete_weigh_ins":               true,
	"get_all_day_events":             true,
	"get_all_day_stress":             true,
	"get_blood_pressure":             true,
	"get_body_battery":               true,
	"get_body_battery_events":        true,
	"get_body_composition":           true,
	"get_daily_steps":                true,
	"get_daily_weigh_ins":            true,
	"get_energy_balance":             true,
	"get_fitnessage_data":            true,
	"get_floors":                     true,
	"get_heart_rates":                true,
	"get_heart_rates_summary":        true,
	"get_hrv_data":                   true,
	"get_hrv_trend":                  true,
	"get_hydration_data":             true,
	"get_lifestyle_logging_data":     true,
	"get_menstrual_calendar_data":    true,
	"get_menstrual_data_for_date":    true,
	"get_morning_training_readiness": true,
	"get_pregnancy_summary":          true,
	"get_recovery_time_remaining":    true,
	"get_respiration_data":           true,
	"get_respiration_summary":        true,
	"get_respiration_trend":          true,
	"get_rhr_day":                    true,
	"get_sleep_data":                 true,
	"get_sleep_summary":              true,
	"get_sleep_summary_range":        true,
	"get_spo2_data":                  true,
	"get_stats":                      true,
	"get_stats_and_body":             true,
	"get_stats_range":                true,
	"get_steps_data":                 true,
	"get_stress_data":                true,
	"get_stress_summary":             true,
	"get_training_readiness":         true,
	"get_user_summary":               true,
	"get_weekly_intensity_minutes":   true,
	"get_weekly_steps":               true,
	"get_weekly_stress":              true,
	"get_weigh_ins":                  true,
	"set_blood_pressure":             true,
})

var activitiesTools = union(coreTools, map[string]bool{
	"add_gear_to_activity":            true,
	"count_activities":                true,
	"create_manual_activity":          true,
	"delete_activity":                 true,
	"delete_course":                   true,
	"download_activity_file":          true,
	"download_course_gpx":             true,
	"get_activities":                  true,
	"get_activities_by_date":          true,
	"get_activities_fordate":          true,
	"get_activity":                    true,
	"get_activity_exercise_sets":      true,
	"get_activity_fit_data":           true,
	"get_activity_fit_messages":       true,
	"get_activity_gear":               true,
	"get_activity_hr_in_timezones":    true,
	"get_activity_power_in_timezones": true,
	"get_activity_split_summaries":    true,
	"get_activity_splits":             true,
	"get_activity_typed_splits":       true,
	"get_activity_types":              true,
	"get_activity_weather":            true,
	"get_course_details":              true,
	"get_courses":                     true,
	"get_gear":                        true,
	"get_power_duration_curve":        true,
	"remove_gear_from_activity":       true,
	"set_activity_description":        true,
	"set_activity_event_type":         true,
	"set_activity_exercise_sets":      true,
	"set_activity_feel":               true,
	"set_activity_name":               true,
	"set_activity_type":               true,
	"set_fit_download_dir":            true,
	"set_perceived_effort":            true,
	"upload_course":                   true,
})

var workoutTools = union(coreTools, map[string]bool{
	"create_run_interval_workout": true,
	"create_run_workout":          true,
	"create_strength_workout":     true,
	"create_walk_run_workout":     true,
	"create_z2_walk_workout":      true,
	"delete_workout":              true,
	"delete_workouts":             true,
	"download_workout":            true,
	"get_calendar_events":         true,
	"get_garmin_coach_workouts":   true,
	"get_scheduled_workouts":      true,
	"get_training_plan_workouts":  true,
	"get_workout_by_id":           true,
	"get_workouts":                true,
	"schedule_week":               true,
	"schedule_workout":            true,
	"schedule_workouts":           true,
	"unschedule_workout":          true,
	"unschedule_workouts":         true,
	"upload_workout":              true,
	"upload_workouts":             true,
})

var trainingTools = union(coreTools, map[string]bool{
	"get_acclimation":                    true,
	"get_adhoc_challenges":               true,
	"get_available_badge_challenges":     true,
	"get_badge_challenges":               true,
	"get_cycling_ftp":                    true,
	"get_earned_badges":                  true,
	"get_endurance_score":                true,
	"get_fitnessage_data":                true,
	"get_goals":                          true,
	"get_hill_score":                     true,
	"get_hrv_data":                       true,
	"get_hrv_trend":                      true,
	"get_inprogress_virtual_challenges":  true,
	"get_lactate_threshold":              true,
	"get_morning_training_readiness":     true,
	"get_non_completed_badge_challenges": true,
	"get_personal_record":                true,
	"get_power_duration_curve":           true,
	"get_primary_training_device":        true,
	"get_progress_summary_between_dates": true,
	"get_race_predictions":               true,
	"get_recovery_time_remaining":        true,
	"get_respiration_trend":              true,
	"get_running_tolerance":              true,
	"get_running_tolerance_trend":        true,
	"get_training_effect":                true,
	"get_training_load_balance":          true,
	"get_training_load_trend":            true,
	"get_training_readiness":             true,
	"get_training_status":                true,
	"get_vo2max_trend":                   true,
	"get_weekly_intensity_minutes":       true,
	"request_reload":                     true,
})

var nutritionTools = union(coreTools, map[string]bool{
	"add_hydration_data":                  true,
	"create_custom_food":                  true,
	"delete_custom_food":                  true,
	"delete_food_log":                     true,
	"get_custom_food_serving_units":       true,
	"get_custom_foods":                    true,
	"get_energy_balance":                  true,
	"get_hydration_data":                  true,
	"get_nutrition_daily_food_log":        true,
	"get_nutrition_daily_meals":           true,
	"get_nutrition_daily_settings":        true,
	"get_nutrition_summary_between_dates": true,
	"log_custom_food":                     true,
	"log_food":                            true,
	"search_foods":                        true,
	"set_nutrition_daily_settings":        true,
	"update_custom_food":                  true,
	"upsert_and_log":                      true,
})

var toolProfiles = map[string]map[string]bool{
	"all":        nil,
	"minimal":    minimalTools,
	"health":     healthTools,
	"activities": activitiesTools,
	"workouts":   workoutTools,
	"training":   trainingTools,
	"nutrition":  nutritionTools,
}

// resolveToolProfile turns GARMIN_TOOL_PROFILE into the set of tool names it
// selects. nil means every tool.
func resolveToolProfile(value string) (map[string]bool, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	var names []string
	for _, name := range strings.Split(value, ",") {
		name = strings.ToLower(strings.TrimSpace(name))
		if name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, errors.New("Invalid GARMIN_TOOL_PROFILE: expected at least one profile name")
	}
	var unknown []string
	for _, name := range names {
		if _, ok := toolProfiles[name]; !ok {
			unknown = append(unknown, "'"+name+"'")
		}
	}
	if len(unknown) > 0 {
		keys := make([]string, 0, len(toolProfiles))
		for name := range toolProfiles {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		return nil, fmt.Errorf(
			"Unknown GARMIN_TOOL_PROFILE %s. Known profiles: %s",
			strings.Join(unknown, ", "), strings.Join(keys, ", "),
		)
	}
	for _, name := range names {
		if name == "all" {
			return nil, nil
		}
	}
	selected := make([]map[string]bool, 0, len(names))
	for _, name := range names {
		selected = append(selected, toolProfiles[name])
	}
	return union(selected...), nil
}

func init() {
	for _, set := range toolProfiles {
		if set != nil {
			set["list_accounts"] = true
		}
	}
}

func union(sets ...map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, set := range sets {
		for name := range set {
			out[name] = true
		}
	}
	return out
}
