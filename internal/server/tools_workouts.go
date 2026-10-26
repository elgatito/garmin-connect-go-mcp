package server

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

// Port of garmin_mcp/workouts.py (15 tools). Python returns plain "Error ...: {e}" strings here (no write tracker),
// so failures are plain errors; the batch and confirm tools report per-item JSON as Python does.

type emptyIn struct{ Account }

type getWorkoutByIDIn struct {
	Account
	WorkoutID any  `json:"workout_id" jsonschema:"Workout ID (numeric) or UUID (for training plan workouts)"`
	Raw       bool `json:"raw,omitempty" jsonschema:"Return Garmin's full workout JSON (the DTO format upload_workout describes) instead of the simplified summary, which leaves fields out."`
}

type workoutIDIn struct {
	Account
	WorkoutID int64 `json:"workout_id" jsonschema:"ID of the workout to download"`
}

type uploadWorkoutIn struct {
	Account
	WorkoutData map[string]any `json:"workout_data" jsonschema:"Dictionary containing workout structure (name, sport type, segments, etc.)"`
}

type uploadWorkoutsIn struct {
	Account
	Workouts []map[string]any `json:"workouts" jsonschema:"List of workout dictionaries, each containing workout structure (name, sport type, segments, etc.) — same format as upload_workout."`
}

type deleteWorkoutIn struct {
	Account
	WorkoutID int64 `json:"workout_id" jsonschema:"ID of the workout to delete (get IDs from get_workouts)"`
	Confirm   bool  `json:"confirm,omitempty" jsonschema:"Must be true to actually delete. Default false returns a preview."`
}

type deleteWorkoutsIn struct {
	Account
	WorkoutIDs []int64 `json:"workout_ids" jsonschema:"List of workout IDs to delete (get IDs from get_workouts)"`
	Confirm    bool    `json:"confirm,omitempty" jsonschema:"Must be true to actually delete. Default false returns a preview."`
}

type dateRangeIn struct {
	Account
	StartDate string `json:"start_date" jsonschema:"Start date in YYYY-MM-DD format"`
	EndDate   string `json:"end_date" jsonschema:"End date in YYYY-MM-DD format"`
}

type coachWorkoutsIn struct {
	Account
	CalendarDate string `json:"calendar_date" jsonschema:"Reference date in YYYY-MM-DD format (returns week's workouts)"`
}

type scheduleWorkoutIn struct {
	Account
	WorkoutID    int64  `json:"workout_id" jsonschema:"ID of the workout to schedule (get IDs from get_workouts)"`
	CalendarDate string `json:"calendar_date" jsonschema:"Date to schedule the workout in YYYY-MM-DD format"`
}

type scheduleWorkoutsIn struct {
	Account
	Schedules []map[string]any `json:"schedules" jsonschema:"List of workout schedules, each with: calendar_date (str): Date to schedule the workout in YYYY-MM-DD format (required); workout_id (int): ID of an existing workout to schedule (required unless workout_data is provided); workout_data (dict): Inline workout JSON to upload first, then schedule (optional). When provided, workout_id is not required. Uses the same structure and target-value rules as upload_workout; pace targets are in m/s, not min/km (5:00/km = 3.333 m/s)."`
}

type rescheduleWorkoutIn struct {
	Account
	ScheduledWorkoutID int64  `json:"scheduled_workout_id" jsonschema:"Calendar-entry id from get_scheduled_workouts"`
	CalendarDate       string `json:"calendar_date" jsonschema:"New date in YYYY-MM-DD format"`
}

type unscheduleWorkoutIn struct {
	Account
	ScheduledWorkoutID int64 `json:"scheduled_workout_id" jsonschema:"Calendar-entry id from get_scheduled_workouts"`
}

type unscheduleWorkoutsIn struct {
	Account
	ScheduledWorkoutIDs []int64 `json:"scheduled_workout_ids" jsonschema:"List of calendar-entry ids from get_scheduled_workouts"`
}

func registerWorkoutTools(r *registry) {
	addTool(r, "get_workouts", getWorkoutsDesc, getWorkoutsTool)
	addTool(r, "get_workout_by_id", getWorkoutByIdDesc, getWorkoutByIDTool)
	addTool(r, "download_workout", downloadWorkoutDesc, downloadWorkoutTool)
	addTool(r, "upload_workout", uploadWorkoutDesc, uploadWorkoutTool)
	addTool(r, "upload_workouts", uploadWorkoutsDesc, uploadWorkoutsTool)
	addTool(r, "delete_workout", deleteWorkoutDesc, deleteWorkoutTool)
	addTool(r, "delete_workouts", deleteWorkoutsDesc, deleteWorkoutsTool)
	addTool(r, "get_scheduled_workouts", getScheduledWorkoutsDesc, getScheduledWorkoutsTool)
	addTool(r, "get_garmin_coach_workouts", getGarminCoachWorkoutsDesc, func(ctx context.Context, s *Session, in coachWorkoutsIn) (any, error) {
		v, err := garminCoachWorkouts(ctx, s.Client, in.CalendarDate)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving Garmin Coach workouts: %w", err)
		}
		return v, nil
	})
	addTool(r, "get_training_plan_workouts", getTrainingPlanWorkoutsDesc, func(ctx context.Context, s *Session, in coachWorkoutsIn) (any, error) {
		v, err := garminCoachWorkouts(ctx, s.Client, in.CalendarDate)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving training plan workouts: %w", err)
		}
		return v, nil
	})
	addTool(r, "schedule_workout", scheduleWorkoutDesc, scheduleWorkoutTool)
	addTool(r, "schedule_workouts", scheduleWorkoutsDesc, scheduleWorkoutsTool)
	addTool(r, "reschedule_workout", rescheduleWorkoutDesc, rescheduleWorkoutTool)
	addTool(r, "unschedule_workout", unscheduleWorkoutDesc, unscheduleWorkoutTool)
	addTool(r, "unschedule_workouts", unscheduleWorkoutsDesc, unscheduleWorkoutsTool)
}

// ── helpers shared with the workout builders ──────────────────────────────────

// batchResult is the {"total", "succeeded", "failed", "results"} summary of the batch tools.
func batchResult(results []any) map[string]any {
	succeeded := 0
	for _, r := range results {
		if asMap(r)["status"] == "success" {
			succeeded++
		}
	}
	return map[string]any{"total": len(results), "succeeded": succeeded, "failed": len(results) - succeeded, "results": results}
}

func scheduleQuery(start, end string) map[string]any {
	return map[string]any{"query": fmt.Sprintf(`query{workoutScheduleSummariesScalar(startDate:"%s", endDate:"%s")}`, start, end)}
}

// isAlreadyScheduled reports whether workoutID is already on calendarDate. The schedule endpoint is not idempotent, so
// schedule_* check first; a failing check falls through to the POST.
func isAlreadyScheduled(ctx context.Context, c *garmin.Client, workoutID int64, calendarDate string) bool {
	if _, err := validateWorkoutDate(calendarDate, "calendar_date"); err != nil {
		return false
	}
	result, err := c.QueryGraphQL(ctx, scheduleQuery(calendarDate, calendarDate))
	if err != nil {
		return false
	}
	// GraphQL returns {"data": null} on error.
	for _, e := range asList(asMap(asMap(result)["data"])["workoutScheduleSummariesScalar"]) {
		entry, ok := e.(map[string]any)
		if !ok {
			return false
		}
		if n, ok := asNumber(entry["workoutId"]); ok && n == float64(workoutID) && entry["scheduleDate"] == calendarDate {
			return true
		}
	}
	return false
}

// uploadSummary is the curated upload response: status/workout_id/name/message, without nulls.
func uploadSummary(result any) any {
	m, ok := result.(map[string]any)
	if !ok {
		return result
	}
	return dropNil(map[string]any{"status": "success", "workout_id": m["workoutId"], "name": m["workoutName"], "message": "Workout uploaded successfully"})
}

// prepareAndUpload repairs, validates and uploads a workout DTO.
func prepareAndUpload(ctx context.Context, c *garmin.Client, w map[string]any) (any, error) {
	if err := prepareWorkoutForUpload(w); err != nil {
		return nil, err
	}
	return c.UploadWorkout(ctx, w)
}

// ── read tools ────────────────────────────────────────────────────────────────

func getWorkoutsTool(ctx context.Context, s *Session, _ emptyIn) (any, error) {
	workouts, err := getAllWorkouts(ctx, s.Client)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving workouts: %w", err)
	}
	if len(workouts) == 0 {
		return "No workouts found.", nil
	}
	list := make([]any, len(workouts))
	for i, w := range workouts {
		list[i] = curateWorkoutSummary(w)
	}
	return map[string]any{"count": len(workouts), "workouts": list}, nil
}

func getWorkoutByIDTool(ctx context.Context, s *Session, in getWorkoutByIDIn) (any, error) {
	v, err := func() (any, error) {
		idStr := workoutIDString(in.WorkoutID)
		if strings.Contains(idStr, "-") { // a UUID: Garmin Coach / training plan workout
			return s.Client.GetFbtAdaptiveWorkout(ctx, idStr)
		}
		n, perr := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64)
		if perr != nil {
			return nil, fmt.Errorf("invalid literal for int() with base 10: %s", pyRepr(idStr))
		}
		return s.Client.GetWorkoutByID(ctx, n)
	}()
	if err != nil {
		return nil, fmt.Errorf("Error retrieving workout: %w", err)
	}
	if falsy(v) {
		return fmt.Sprintf("No workout found with ID %s.", workoutIDString(in.WorkoutID)), nil
	}
	if in.Raw {
		return v, nil
	}
	return curateWorkoutDetails(asMap(v)), nil
}

// workoutIDString is str(workout_id) for the Union[int, str] argument.
func workoutIDString(v any) string {
	if f, ok := v.(float64); ok {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return pyStrValue(v)
}

func downloadWorkoutTool(ctx context.Context, s *Session, in workoutIDIn) (any, error) {
	b, err := s.Client.DownloadWorkout(ctx, in.WorkoutID)
	if err != nil {
		return nil, fmt.Errorf("Error downloading workout: %w", err)
	}
	if len(b) == 0 {
		return fmt.Sprintf("No workout data found for workout with ID %d.", in.WorkoutID), nil
	}
	return map[string]any{
		"workout_id": in.WorkoutID, "format": "FIT", "size_bytes": len(b),
		"message": "Workout data is available in FIT format. Use Garmin Connect API to save to file.",
	}, nil
}

func getScheduledWorkoutsTool(ctx context.Context, s *Session, in dateRangeIn) (any, error) {
	v, err := func() (any, error) {
		if _, err := validateWorkoutDate(in.StartDate, "start_date"); err != nil {
			return nil, err
		}
		if _, err := validateWorkoutDate(in.EndDate, "end_date"); err != nil {
			return nil, err
		}
		result, err := s.Client.QueryGraphQL(ctx, scheduleQuery(in.StartDate, in.EndDate))
		if err != nil {
			return nil, err
		}
		m := asMap(result)
		if _, has := m["data"]; falsy(result) || !has {
			return "No scheduled workouts found or error querying data.", nil
		}
		scheduled := asList(asMap(m["data"])["workoutScheduleSummariesScalar"])
		if len(scheduled) == 0 {
			return fmt.Sprintf("No workouts scheduled between %s and %s.", in.StartDate, in.EndDate), nil
		}
		list := make([]any, len(scheduled))
		for i, e := range scheduled {
			list[i] = curateScheduledWorkout(asMap(e))
		}
		return map[string]any{
			"count": len(scheduled), "date_range": map[string]any{"start": in.StartDate, "end": in.EndDate}, "scheduled_workouts": list,
		}, nil
	}()
	if err != nil {
		return nil, fmt.Errorf("Error retrieving scheduled workouts: %w", err)
	}
	return v, nil
}

// garminCoachWorkouts returns the curated workouts of the active Garmin Coach/training plan.
func garminCoachWorkouts(ctx context.Context, c *garmin.Client, calendarDate string) (any, error) {
	const none = "No training plan data found or error querying data."
	if _, err := validateWorkoutDate(calendarDate, "calendar_date"); err != nil {
		return nil, err
	}
	result, err := c.QueryGraphQL(ctx, map[string]any{"query": fmt.Sprintf(
		`query{trainingPlanScalar(calendarDate:"%s", lang:"en-US", firstDayOfWeek:"monday")}`, calendarDate)})
	if err != nil {
		return nil, err
	}
	data, ok := asMap(result)["data"].(map[string]any)
	if !ok {
		return none, nil
	}
	planData := data["trainingPlanScalar"]
	if falsy(planData) {
		planData = map[string]any{}
	}
	pd, ok := planData.(map[string]any)
	if !ok {
		return none, nil
	}
	noPlans := fmt.Sprintf("No training plan workouts scheduled for %s.", calendarDate)
	trainingPlans, ok := pd["trainingPlanWorkoutScheduleDTOS"].([]any)
	if !ok || len(trainingPlans) == 0 {
		return noPlans, nil
	}
	allWorkouts := []any{}
	var planNames []any
	plans := []any{}
	valid := 0
	for _, p := range trainingPlans {
		plan, ok := p.(map[string]any)
		if !ok {
			continue
		}
		valid++
		name := plan["planName"]
		if !falsy(name) {
			dup := false
			for _, n := range planNames {
				dup = dup || reflect.DeepEqual(n, name)
			}
			if !dup {
				planNames = append(planNames, name)
			}
		}
		details := asMap(plan["trainingPlanDetailsDTO"])
		race := asMap(details["athleteRace"])
		summary := dropNil(map[string]any{
			"name": name, "training_plan_id": plan["trainingPlanId"], "classification": plan["trainingPlanClassification"],
			"training_type": details["trainingType"], "race_name": race["raceName"], "race_day": race["raceDay"],
			"workouts_per_week": details["workoutsPerWeek"], "remaining_workouts_this_week": details["remainingAtpWorkoutsForWeek"],
			"registration_date": details["registrationDate"], "plan_completed": details["planCompleted"],
		})
		if len(summary) > 0 {
			plans = append(plans, summary)
		}
		summaries, ok := firstTruthy(plan["workoutScheduleSummaries"], []any{}).([]any)
		if !ok {
			continue
		}
		for _, w := range summaries {
			if m, ok := w.(map[string]any); ok {
				allWorkouts = append(allWorkouts, curateScheduledWorkout(m))
			}
		}
	}
	if valid == 0 {
		return noPlans, nil
	}
	out := map[string]any{"date": calendarDate, "count": len(allWorkouts), "workouts": allWorkouts}
	if len(planNames) > 0 {
		out["training_plans"] = planNames
	}
	if len(plans) > 0 {
		out["plans"] = plans
	}
	return out, nil
}

// ── upload / delete ───────────────────────────────────────────────────────────

func uploadWorkoutTool(ctx context.Context, s *Session, in uploadWorkoutIn) (any, error) {
	result, err := prepareAndUpload(ctx, s.Client, in.WorkoutData)
	if err != nil {
		return nil, fmt.Errorf("Error uploading workout: %w", err)
	}
	return uploadSummary(result), nil
}

func uploadWorkoutsTool(ctx context.Context, s *Session, in uploadWorkoutsIn) (any, error) {
	results := []any{}
	for _, w := range in.Workouts {
		result, err := prepareAndUpload(ctx, s.Client, w)
		switch {
		case err != nil:
			results = append(results, map[string]any{"status": "error", "name": w["workoutName"], "message": "Error uploading workout: " + err.Error()})
		case asMap(result) != nil:
			results = append(results, uploadSummary(result))
		default:
			results = append(results, map[string]any{"status": "success", "message": "Workout uploaded successfully"})
		}
	}
	return batchResult(results), nil
}

func logDeleted(s *Session, tool string, id int64, preview map[string]any) {
	fmt.Fprintf(s.Log, "%s: deleted workout %d name=%s at %s\n", tool, id, pyReprValue(preview["name"]),
		time.Now().UTC().Format("2006-01-02T15:04:05.000000-07:00"))
}

func deleteWorkoutTool(ctx context.Context, s *Session, in deleteWorkoutIn) (any, error) {
	preview := workoutPreview(ctx, s.Client, in.WorkoutID)
	if !in.Confirm {
		return map[string]any{
			"status": "needs_confirmation", "workout": preview,
			"message": "This permanently deletes the workout from Garmin Connect and cannot be undone. Call again with confirm=true to proceed.",
		}, nil
	}
	if _, err := s.Client.DeleteWorkout(ctx, in.WorkoutID); err != nil {
		return map[string]any{"status": "failed", "workout_id": in.WorkoutID, "message": "Failed to delete workout: " + err.Error()}, nil
	}
	logDeleted(s, "delete_workout", in.WorkoutID, preview)
	return map[string]any{
		"status": "success", "workout_id": in.WorkoutID, "workout": preview,
		"message": fmt.Sprintf("Workout %d deleted successfully", in.WorkoutID),
	}, nil
}

func deleteWorkoutsTool(ctx context.Context, s *Session, in deleteWorkoutsIn) (any, error) {
	previews := make([]any, len(in.WorkoutIDs))
	for i, id := range in.WorkoutIDs {
		previews[i] = workoutPreview(ctx, s.Client, id)
	}
	if !in.Confirm {
		return map[string]any{
			"status": "needs_confirmation", "total": len(in.WorkoutIDs), "workouts": previews,
			"message": "This permanently deletes these workouts from Garmin Connect and cannot be undone. Call again with confirm=true to proceed.",
		}, nil
	}
	results := []any{}
	for i, id := range in.WorkoutIDs {
		preview := asMap(previews[i])
		if _, err := s.Client.DeleteWorkout(ctx, id); err != nil {
			results = append(results, map[string]any{"status": "error", "workout_id": id, "message": "Error deleting workout: " + err.Error()})
			continue
		}
		logDeleted(s, "delete_workouts", id, preview)
		results = append(results, map[string]any{
			"status": "success", "workout_id": id, "workout": preview, "message": fmt.Sprintf("Workout %d deleted successfully", id),
		})
	}
	return batchResult(results), nil
}

// ── schedule ──────────────────────────────────────────────────────────────────

func scheduleWorkoutTool(ctx context.Context, s *Session, in scheduleWorkoutIn) (any, error) {
	if _, err := validateWorkoutDate(in.CalendarDate, "calendar_date"); err != nil {
		return map[string]any{"status": "failed", "workout_id": in.WorkoutID, "scheduled_date": in.CalendarDate, "message": err.Error()}, nil
	}
	if isAlreadyScheduled(ctx, s.Client, in.WorkoutID, in.CalendarDate) {
		return map[string]any{
			"status": "success", "workout_id": in.WorkoutID, "scheduled_date": in.CalendarDate, "idempotent": true,
			"message": fmt.Sprintf("Workout %d already scheduled for %s — no action taken", in.WorkoutID, in.CalendarDate),
		}, nil
	}
	// Any 2xx is success: the client turns >= 400 into an error.
	if _, err := s.Client.ScheduleWorkout(ctx, in.WorkoutID, in.CalendarDate); err != nil {
		return nil, fmt.Errorf("Error scheduling workout: %w", err)
	}
	return map[string]any{
		"status": "success", "workout_id": in.WorkoutID, "scheduled_date": in.CalendarDate,
		"message": fmt.Sprintf("Successfully scheduled workout %d for %s", in.WorkoutID, in.CalendarDate),
	}, nil
}

func scheduleWorkoutsTool(ctx context.Context, s *Session, in scheduleWorkoutsIn) (any, error) {
	results := []any{}
	for _, item := range in.Schedules {
		results = append(results, scheduleItem(ctx, s.Client, item))
	}
	return batchResult(results), nil
}

func scheduleItem(ctx context.Context, c *garmin.Client, item map[string]any) map[string]any {
	workoutID, calendarDate, workoutData := item["workout_id"], item["calendar_date"], item["workout_data"]
	failed := func(id any, msg string) map[string]any {
		return map[string]any{"status": "failed", "workout_id": id, "scheduled_date": calendarDate, "message": msg}
	}
	if calendarDate == nil {
		return failed(workoutID, "Missing required field: calendar_date")
	}
	if _, err := validateWorkoutDate(calendarDate, "calendar_date"); err != nil {
		return failed(workoutID, err.Error())
	}
	date := pyStr(calendarDate) // validateWorkoutDate accepted it, so it is a string
	if workoutID == nil && workoutData == nil {
		return failed(nil, "Missing required fields: provide either workout_id or workout_data")
	}
	errEntry := func(err error) map[string]any {
		return map[string]any{"status": "error", "workout_id": workoutID, "scheduled_date": calendarDate, "message": "Error scheduling workout: " + err.Error()}
	}
	var workoutName any
	if workoutData != nil {
		w, ok := workoutData.(map[string]any)
		if !ok {
			return errEntry(fmt.Errorf("'%s' object has no attribute 'get'", pyTypeName(workoutData)))
		}
		up, err := prepareAndUpload(ctx, c, w)
		if err != nil {
			return errEntry(err)
		}
		um := asMap(up)
		if um == nil || um["workoutId"] == nil {
			return map[string]any{"status": "failed", "scheduled_date": calendarDate, "message": "Upload succeeded but no workout_id returned"}
		}
		workoutID, workoutName = um["workoutId"], um["workoutName"]
	}
	// Garmin reports workoutId as an int; a string id would never match in the pre-check and get double-booked.
	id, err := pyInt(workoutID)
	if err != nil {
		return errEntry(err)
	}
	workoutID = id
	named := func(m map[string]any) map[string]any {
		if !falsy(workoutName) {
			m["workout_name"] = workoutName
		}
		return m
	}
	if isAlreadyScheduled(ctx, c, id, date) {
		return named(map[string]any{
			"status": "success", "workout_id": id, "scheduled_date": date, "idempotent": true,
			"message": fmt.Sprintf("Workout %d already scheduled for %s — no action taken", id, date),
		})
	}
	if _, err := c.ScheduleWorkout(ctx, id, date); err != nil {
		return errEntry(err)
	}
	return named(map[string]any{
		"status": "success", "workout_id": id, "scheduled_date": date,
		"message": fmt.Sprintf("Successfully scheduled workout %d for %s", id, date),
	})
}

func rescheduleWorkoutTool(ctx context.Context, s *Session, in rescheduleWorkoutIn) (any, error) {
	id := in.ScheduledWorkoutID
	if _, err := validateWorkoutDate(in.CalendarDate, "calendar_date"); err != nil {
		return map[string]any{"status": "failed", "scheduled_workout_id": id, "scheduled_date": in.CalendarDate, "message": err.Error()}, nil
	}
	failed := func(err error) (any, error) {
		return map[string]any{
			"status": "failed", "scheduled_workout_id": id, "scheduled_date": in.CalendarDate,
			"message": "Failed to reschedule workout: " + err.Error(),
		}, nil
	}
	// Garmin's schedule PUT takes the whole entry back with the new date, and only the workout reference.
	v, err := s.Client.GetScheduledWorkoutByID(ctx, id)
	if err != nil {
		return failed(err)
	}
	if falsy(v) {
		return map[string]any{"status": "failed", "scheduled_workout_id": id, "message": fmt.Sprintf("Scheduled workout %d not found", id)}, nil
	}
	current, ok := v.(map[string]any)
	if !ok {
		return failed(fmt.Errorf("'%s' object has no attribute 'get'", pyTypeName(v)))
	}
	workout := asMap(current["workout"])
	payload := make(map[string]any, len(current)+2)
	for k, val := range current {
		payload[k] = val
	}
	payload["calendarDate"] = in.CalendarDate
	payload["workout"] = nil
	if len(workout) > 0 {
		payload["workout"] = map[string]any{"workoutId": workout["workoutId"]}
	}
	if _, err := s.Client.UpdateScheduledWorkout(ctx, id, payload); err != nil {
		return failed(err)
	}
	return map[string]any{
		"status": "success", "scheduled_workout_id": id, "workout_id": workout["workoutId"],
		"previous_date": current["calendarDate"], "scheduled_date": in.CalendarDate,
		"message": fmt.Sprintf("Moved scheduled workout %d to %s", id, in.CalendarDate),
	}, nil
}

func unscheduleWorkoutTool(ctx context.Context, s *Session, in unscheduleWorkoutIn) (any, error) {
	id := in.ScheduledWorkoutID
	if _, err := s.Client.UnscheduleWorkout(ctx, id); err != nil {
		return map[string]any{"status": "failed", "scheduled_workout_id": id, "message": "Failed to unschedule workout: " + err.Error()}, nil
	}
	return map[string]any{
		"status": "success", "scheduled_workout_id": id, "message": fmt.Sprintf("Scheduled workout %d removed from calendar", id),
	}, nil
}

func unscheduleWorkoutsTool(ctx context.Context, s *Session, in unscheduleWorkoutsIn) (any, error) {
	results := []any{}
	for _, id := range in.ScheduledWorkoutIDs {
		if _, err := s.Client.UnscheduleWorkout(ctx, id); err != nil {
			results = append(results, map[string]any{"status": "error", "scheduled_workout_id": id, "message": "Error unscheduling workout: " + err.Error()})
			continue
		}
		results = append(results, map[string]any{
			"status": "success", "scheduled_workout_id": id, "message": fmt.Sprintf("Scheduled workout %d removed from calendar", id),
		})
	}
	return batchResult(results), nil
}
