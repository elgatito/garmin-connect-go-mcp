package server

import (
	"context"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
)

func coachData(plans []any, date string) any {
	names := []any{}
	seen := map[string]bool{}
	summaries := []any{}
	workouts := []any{}
	valid := 0
	for _, v := range plans {
		plan, ok := v.(map[string]any)
		if !ok {
			continue
		}
		valid++
		if name, ok := plan["planName"].(string); ok && name != "" && !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
		details, _ := plan["trainingPlanDetailsDTO"].(map[string]any)
		race, _ := details["athleteRace"].(map[string]any)
		summary := remapFields(plan, map[string]string{"name": "planName", "training_plan_id": "trainingPlanId", "classification": "trainingPlanClassification"})
		for k, v := range remapFields(details, map[string]string{"training_type": "trainingType", "workouts_per_week": "workoutsPerWeek", "remaining_workouts_this_week": "remainingAtpWorkoutsForWeek", "registration_date": "registrationDate", "plan_completed": "planCompleted"}) {
			summary[k] = v
		}
		for k, v := range remapFields(race, map[string]string{"race_name": "raceName", "race_day": "raceDay"}) {
			summary[k] = v
		}
		if len(summary) > 0 {
			summaries = append(summaries, summary)
		}
		rows, _ := plan["workoutScheduleSummaries"].([]any)
		for _, v := range rows {
			if row, ok := v.(map[string]any); ok {
				workouts = append(workouts, scheduledWorkout(row))
			}
		}
	}
	if valid == 0 {
		return fmt.Sprintf("No training plan workouts scheduled for %s.", date)
	}
	out := map[string]any{"date": date, "count": len(workouts), "workouts": workouts}
	if len(names) > 0 {
		out["training_plans"] = names
	}
	if len(summaries) > 0 {
		out["plans"] = summaries
	}
	return out
}
func registerCoach(srv *mcp.Server, m *manager) {
	for _, name := range []string{"get_garmin_coach_workouts", "get_training_plan_workouts"} {
		name := name
		addTool(srv, m, name, description(name), map[string]any{"calendar_date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
			label := "training plan workouts"
			if name == "get_garmin_coach_workouts" {
				label = "Garmin Coach workouts"
			}
			date, err := dateArgument(args, "calendar_date")
			if err != nil {
				return nil, fmt.Errorf("Error retrieving %s: %w", label, err)
			}
			query := fmt.Sprintf(`query{trainingPlanScalar(calendarDate:"%s", lang:"en-US", firstDayOfWeek:"monday")}`, date)
			v, err := s.Client.DoJSON(ctx, garmin.Req{Method: http.MethodPost, Path: "/graphql-gateway/graphql", JSON: map[string]any{"query": query}})
			if err != nil {
				return nil, fmt.Errorf("Error retrieving %s: %w", label, err)
			}
			data, _ := v.(map[string]any)
			obj, ok := data["data"].(map[string]any)
			if !ok {
				return "No training plan data found or error querying data.", nil
			}
			plan, ok := obj["trainingPlanScalar"].(map[string]any)
			if !ok && obj["trainingPlanScalar"] != nil {
				return "No training plan data found or error querying data.", nil
			}
			list, _ := plan["trainingPlanWorkoutScheduleDTOS"].([]any)
			if len(list) == 0 {
				return fmt.Sprintf("No training plan workouts scheduled for %s.", date), nil
			}
			return coachData(list, date), nil
		}, "calendar_date")
	}
}
