package server

import (
	"context"
)

// Port of the workout curation, paging and preview helpers of garmin_mcp/workouts.py.

// workoutsPageSize: Garmin's get_workouts returns a single page, so one call drops the rest of a larger library.
const workoutsPageSize = 100

// workoutReader is the part of *garmin.Client the preview and paging helpers use.
type workoutReader interface {
	GetWorkoutByID(ctx context.Context, id int64) (any, error)
	GetWorkouts(ctx context.Context, start, limit int) (any, error)
}

// getAllWorkouts pages through get_workouts until a short page and returns every workout.
func getAllWorkouts(ctx context.Context, r workoutReader) ([]map[string]any, error) {
	var workouts []map[string]any
	seen := map[string]bool{}
	// Garmin's start is 1-based (0 is treated as 1), so start=0 then start=100 would overlap by one workout.
	for start := 1; ; start += workoutsPageSize {
		v, err := r.GetWorkouts(ctx, start, workoutsPageSize)
		if err != nil {
			return nil, err
		}
		page, ok := v.([]any)
		if !ok {
			break
		}
		// Skipping workouts already seen keeps an overlapping page from duplicating entries, and stops the
		// loop if Garmin ignores the offset.
		var fresh []map[string]any
		for _, e := range page {
			if w, ok := e.(map[string]any); ok && !seen[pyReprValue(w["workoutId"])] {
				fresh = append(fresh, w)
			}
		}
		if len(fresh) == 0 {
			break
		}
		for _, w := range fresh {
			workouts = append(workouts, w)
			seen[pyReprValue(w["workoutId"])] = true
		}
		if len(page) < workoutsPageSize {
			break
		}
	}
	return workouts, nil
}

// workoutPreview is a compact preview of a workout for the delete confirmation gate.
func workoutPreview(ctx context.Context, r workoutReader, id int64) map[string]any {
	preview := func(w map[string]any) map[string]any {
		s := curateWorkoutSummary(w)
		return dropNil(map[string]any{
			"workout_id": id, "name": s["name"], "sport": s["sport"], "provider": s["provider"], "updated_date": s["updated_date"],
		})
	}
	if v, err := r.GetWorkoutByID(ctx, id); err == nil {
		if w := asMap(v); len(w) > 0 {
			return preview(w)
		}
	}
	items, err := getAllWorkouts(ctx, r)
	if err != nil {
		return map[string]any{"workout_id": id}
	}
	for _, item := range items {
		if n, ok := asNumber(item["workoutId"]); ok && n == float64(id) {
			return preview(item)
		}
	}
	return map[string]any{"workout_id": id}
}

func curateWorkoutSummary(w map[string]any) map[string]any {
	summary := map[string]any{
		"id":           w["workoutId"],
		"name":         w["workoutName"],
		"sport":        asMap(w["sportType"])["sportTypeKey"],
		"provider":     w["workoutProvider"],
		"created_date": w["createdDate"],
		"updated_date": w["updatedDate"],
	}
	if !falsy(w["description"]) {
		summary["description"] = w["description"]
	}
	duration := w["estimatedDurationInSecs"]
	if duration == nil {
		duration = w["estimatedDuration"]
	}
	if duration != nil {
		summary["estimated_duration_seconds"] = duration
	}
	distance := w["estimatedDistanceInMeters"]
	if distance == nil {
		distance = w["estimatedDistance"]
	}
	if distance != nil {
		summary["estimated_distance_meters"] = distance
	}
	return dropNil(summary)
}

// curateStepTarget curates a workout target block, handling Garmin null target payloads safely.
func curateStepTarget(curated, step map[string]any, targetField, v1, v2, zone, unit, prefix string) {
	key := asMap(step[targetField])["workoutTargetTypeKey"]
	if falsy(key) || key == "no.target" {
		return
	}
	curated[prefix+"target_type"] = key
	if step[v1] != nil {
		curated[prefix+"target_value_low"] = step[v1]
	}
	if step[v2] != nil {
		curated[prefix+"target_value_high"] = step[v2]
	}
	if step[zone] != nil {
		curated[prefix+"target_zone"] = step[zone]
	}
	// Without the unit, a power range in % FTP reads the same as one in watts.
	if k := asMap(step[unit])["unitKey"]; !falsy(k) {
		curated[prefix+"target_value_unit"] = k
	} else if key == "pace.zone" && (step[v1] != nil || step[v2] != nil) {
		curated[prefix+"target_value_unit"] = "meters_per_second"
	}
}

func curateWorkoutStep(step map[string]any) map[string]any {
	curated := map[string]any{
		"order": step["stepOrder"],
		"type":  asMap(step["stepType"])["stepTypeKey"], // warmup, interval, cooldown, rest, recover
	}
	if !falsy(step["description"]) {
		curated["description"] = step["description"]
	}
	if k := asMap(step["endCondition"])["conditionTypeKey"]; !falsy(k) {
		curated["end_condition"] = k
	}
	if !falsy(step["endConditionValue"]) {
		// Meaning depends on the condition type (seconds for time, meters for distance).
		curated["end_condition_value"] = step["endConditionValue"]
	}
	curateStepTarget(curated, step, "targetType", "targetValueOne", "targetValueTwo", "zoneNumber", "targetValueUnit", "")
	// Any sport can have one, e.g. swim pace or cadence alongside bike power.
	curateStepTarget(curated, step, "secondaryTargetType", "secondaryTargetValueOne", "secondaryTargetValueTwo",
		"secondaryZoneNumber", "secondaryTargetValueUnit", "secondary_")
	// Swim stroke / equipment / drill info and strength exercise info.
	for _, f := range [][3]string{{"strokeType", "strokeTypeKey", "stroke_type"}, {"equipmentType", "equipmentTypeKey", "equipment_type"}, {"drillType", "drillTypeKey", "drill_type"}} {
		if k := asMap(step[f[0]])[f[1]]; !falsy(k) {
			curated[f[2]] = k
		}
	}
	if !falsy(step["category"]) {
		curated["category"] = step["category"]
	}
	if !falsy(step["exerciseName"]) {
		curated["exercise_name"] = step["exerciseName"]
	}
	if step["weightValue"] != nil {
		curated["weight_value"] = step["weightValue"]
		if k := asMap(step["weightUnit"])["unitKey"]; !falsy(k) {
			curated["weight_unit"] = k
		}
	}
	if step["type"] == "RepeatGroupDTO" {
		curated["repeat_count"] = step["numberOfIterations"]
		if nested := asList(step["workoutSteps"]); len(nested) > 0 {
			curated["steps"] = curateSteps(nested)
			curated["step_count"] = len(nested)
		}
	}
	return dropNil(curated)
}

func curateSteps(steps []any) []any {
	out := make([]any, len(steps))
	for i, s := range steps {
		out[i] = curateWorkoutStep(asMap(s))
	}
	return out
}

func curateWorkoutSegment(seg map[string]any) map[string]any {
	curated := map[string]any{"order": seg["segmentOrder"], "sport": asMap(seg["sportType"])["sportTypeKey"]}
	if !falsy(seg["estimatedDurationInSecs"]) {
		curated["estimated_duration_seconds"] = seg["estimatedDurationInSecs"]
	}
	if !falsy(seg["estimatedDistanceInMeters"]) {
		curated["estimated_distance_meters"] = seg["estimatedDistanceInMeters"]
	}
	if steps := asList(seg["workoutSteps"]); len(steps) > 0 {
		curated["steps"] = curateSteps(steps)
		curated["step_count"] = len(steps)
	}
	return dropNil(curated)
}

// curateWorkoutDetails handles both regular workouts and training plan (fbt-adaptive) workouts, which use slightly
// different field names.
func curateWorkoutDetails(w map[string]any) map[string]any {
	details := map[string]any{
		"id":           w["workoutId"],
		"uuid":         w["workoutUuid"],
		"name":         w["workoutName"],
		"sport":        asMap(w["sportType"])["sportTypeKey"],
		"provider":     w["workoutProvider"],
		"created_date": w["createdDate"],
		"updated_date": w["updatedDate"],
	}
	if !falsy(w["description"]) {
		details["description"] = w["description"]
	}
	if d := firstTruthy(w["estimatedDuration"], w["estimatedDurationInSecs"]); !falsy(d) {
		details["estimated_duration_seconds"] = d
	}
	if d := firstTruthy(w["estimatedDistance"], w["estimatedDistanceInMeters"]); !falsy(d) {
		details["estimated_distance_meters"] = d
	}
	if !falsy(w["avgTrainingSpeed"]) {
		details["avg_training_speed_mps"] = w["avgTrainingSpeed"]
	}
	if !falsy(w["poolLength"]) {
		details["pool_length"] = w["poolLength"]
		if k := asMap(w["poolLengthUnit"])["unitKey"]; !falsy(k) {
			details["pool_length_unit"] = k
		}
	}
	for _, f := range [][2]string{{"workoutPhrase", "workout_type"}, {"trainingEffectLabel", "training_effect_label"}, {"estimatedTrainingEffect", "estimated_training_effect"}} {
		if !falsy(w[f[0]]) {
			details[f[1]] = w[f[0]]
		}
	}
	if segs := asList(w["workoutSegments"]); len(segs) > 0 {
		out := make([]any, len(segs))
		for i, s := range segs {
			out[i] = curateWorkoutSegment(asMap(s))
		}
		details["segments"] = out
		details["segment_count"] = len(segs)
	}
	return dropNil(details)
}

// curateScheduledWorkout extracts the essentials of a scheduled workout from the GraphQL response.
func curateScheduledWorkout(s map[string]any) map[string]any {
	completed := s["associatedActivityId"] != nil
	summary := map[string]any{
		"date": s["scheduleDate"],
		// Calendar-entry id (distinct from workout_id). Pass it to unschedule_workout to remove the entry.
		"scheduled_workout_id": s["scheduledWorkoutId"],
		"workout_uuid":         s["workoutUuid"],
		"workout_id":           s["workoutId"],
		"training_plan_id":     s["trainingPlanId"],
		"fbt_adaptive_plan_id": s["fbtAdaptivePlanId"],
		"tp_type":              s["tpType"],
		"name":                 s["workoutName"],
		"sport":                s["workoutType"],
		"completed":            completed,
	}
	if !falsy(s["tpPlanName"]) {
		summary["training_plan"] = s["tpPlanName"]
	}
	// The workout's intent from Garmin Coach, e.g. "AEROBIC_LOW_SHORTAGE_BASE", "ANAEROBIC_SPEED", "LONG_WORKOUT".
	if !falsy(s["workoutPhrase"]) {
		summary["workout_type"] = s["workoutPhrase"]
	}
	if !falsy(s["isRestDay"]) {
		summary["is_rest_day"] = true
	}
	if !falsy(s["race"]) {
		summary["is_race_day"] = true
	}
	if !falsy(s["estimatedDurationInSecs"]) {
		summary["estimated_duration_seconds"] = s["estimatedDurationInSecs"]
	}
	if !falsy(s["estimatedDistanceInMeters"]) {
		summary["estimated_distance_meters"] = s["estimatedDistanceInMeters"]
	}
	if completed {
		summary["activity_id"] = s["associatedActivityId"]
	}
	return dropNil(summary)
}
