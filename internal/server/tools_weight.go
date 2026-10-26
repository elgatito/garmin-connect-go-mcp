package server

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Port of garmin_mcp/weight_management.py.

type weighInsIn struct {
	Account
	StartDate string `json:"start_date" jsonschema:"Start date in YYYY-MM-DD format"`
	EndDate   string `json:"end_date" jsonschema:"End date in YYYY-MM-DD format"`
}

type dailyWeighInsIn struct {
	Account
	Date string `json:"date" jsonschema:"Date in YYYY-MM-DD format"`
}

type deleteWeighInsIn struct {
	Account
	Date      string `json:"date" jsonschema:"Date in YYYY-MM-DD format"`
	DeleteAll *bool  `json:"delete_all,omitempty" jsonschema:"Whether to delete all measurements for the day"`
	Confirm   bool   `json:"confirm,omitempty" jsonschema:"Set true after reviewing the preview to perform the deletion"`
}

type addWeighInIn struct {
	Account
	Weight  float64 `json:"weight" jsonschema:"Weight value"`
	UnitKey *string `json:"unit_key,omitempty" jsonschema:"Unit of weight ('kg' or 'lbs'; 'lb' is accepted as an alias)"`
}

type addWeighInTimestampsIn struct {
	Account
	Weight        float64 `json:"weight" jsonschema:"Weight value"`
	UnitKey       *string `json:"unit_key,omitempty" jsonschema:"Unit of weight ('kg' or 'lbs'; 'lb' is accepted as an alias)"`
	DateTimestamp *string `json:"date_timestamp,omitempty" jsonschema:"Local timestamp in format YYYY-MM-DDThh:mm:ss (required if gmt_timestamp is given -- it sets the day the weigh-in is recorded on)"`
	GmtTimestamp  *string `json:"gmt_timestamp,omitempty" jsonschema:"GMT timestamp in format YYYY-MM-DDThh:mm:ss"`
}

const (
	deleteWeighInsDesc = `Delete weight measurements for a specific date

Permanently removes the selected day's weigh-ins. This cannot be undone.
Call once with confirm=false (the default) to preview the measurements,
then call again with confirm=true to delete them.`
	addWeighInTimestampsDesc = `Add a new weight measurement with specific timestamps

If both timestamps are omitted, the current time is used. If only
date_timestamp is given, the GMT timestamp is derived from it.`
)

func registerWeightTools(r *registry) {
	addTool(r, "get_weigh_ins", "Get weight measurements between specified dates", getWeighIns)
	addTool(r, "get_daily_weigh_ins", "Get weight measurements for a specific date", getDailyWeighIns)
	addTool(r, "delete_weigh_ins", deleteWeighInsDesc, deleteWeighIns)
	addTool(r, "add_weigh_in", "Add a new weight measurement", addWeighIn)
	addTool(r, "add_weigh_in_with_timestamps", addWeighInTimestampsDesc, addWeighInWithTimestamps)
}

// weightKg is round(w.get("weight", 0) / 1000, 2) if w.get("weight") else None.
func weightKg(w map[string]any) any {
	if f, ok := asNumber(w["weight"]); ok && f != 0 {
		return round(f/1000, 2)
	}
	return nil
}

func curateWeighIn(w map[string]any) map[string]any {
	return dropNil(map[string]any{
		"weight_grams": w["weight"], "weight_kg": weightKg(w), "bmi": w["bmi"], "body_fat_percent": w["bodyFat"],
		"body_water_percent": w["bodyWater"], "bone_mass_grams": w["boneMass"], "muscle_mass_grams": w["muscleMass"],
		"source_type": w["sourceType"], "timestamp_gmt": w["timestampGMT"],
	})
}

func addAverage(curated map[string]any, data map[string]any) {
	if w, ok := asNumber(asMap(data["totalAverage"])["weight"]); ok && w != 0 {
		curated["average_weight_grams"], curated["average_weight_kg"] = w, round(w/1000, 2)
	}
}

func getWeighIns(ctx context.Context, s *Session, in weighInsIn) (any, error) {
	data, err := s.Client.GetWeighIns(ctx, in.StartDate, in.EndDate)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving weight measurements: %w", err)
	}
	none := fmt.Sprintf("No weight measurements found between %s and %s.", in.StartDate, in.EndDate)
	d := asMap(data)
	summaries := asList(d["dailyWeightSummaries"])
	if falsy(data) || len(summaries) == 0 {
		return none, nil
	}
	var all []any
	for _, day := range summaries {
		all = append(all, asList(asMap(day)["allWeightMetrics"])...)
	}
	measurements := []map[string]any{}
	for _, w := range all {
		wm := asMap(w)
		m := curateWeighIn(wm)
		if c, ok := wm["calendarDate"]; ok && c != nil {
			m["date"] = c
		}
		measurements = append(measurements, m)
	}
	sortByKey(measurements, "date", true) // most recent first
	curated := map[string]any{"date_range": map[string]any{"start": in.StartDate, "end": in.EndDate}, "measurement_count": len(all),
		"days_with_data": len(summaries), "measurements": measurements}
	addAverage(curated, d)
	return curated, nil
}

func getDailyWeighIns(ctx context.Context, s *Session, in dailyWeighInsIn) (any, error) {
	data, err := s.Client.GetDailyWeighIns(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving daily weight measurements: %w", err)
	}
	d := asMap(data)
	list := asList(d["dateWeightList"])
	if falsy(data) || len(list) == 0 {
		return fmt.Sprintf("No weight measurements found for %s.", in.Date), nil
	}
	measurements := []map[string]any{}
	for _, w := range list {
		measurements = append(measurements, curateWeighIn(asMap(w)))
	}
	curated := map[string]any{"date": in.Date, "measurement_count": len(list), "measurements": measurements}
	addAverage(curated, d)
	return curated, nil
}

func deleteWeighIns(ctx context.Context, s *Session, in deleteWeighInsIn) (any, error) {
	fail := func(err error) (any, error) { return nil, fmt.Errorf("Error deleting weight measurements: %w", err) }
	deleteAll := in.DeleteAll == nil || *in.DeleteAll
	nothing := func(msg string) map[string]any {
		return map[string]any{"status": "nothing_deleted", "date": in.Date, "deleted_count": 0, "message": msg}
	}
	if !in.Confirm {
		data, err := s.Client.GetDailyWeighIns(ctx, in.Date)
		if err != nil {
			return fail(err)
		}
		measurements := asList(asMap(data)["dateWeightList"])
		if len(measurements) == 0 {
			return nothing(fmt.Sprintf("No weight measurements found for %s; nothing was deleted", in.Date)), nil
		}
		if len(measurements) > 1 && !deleteAll {
			return nothing(fmt.Sprintf("Nothing was deleted for %s: deleting multiple measurements requires delete_all=true", in.Date)), nil
		}
		preview := []map[string]any{}
		for _, m := range measurements {
			mm := asMap(m)
			item := map[string]any{"id": mm["samplePk"], "source_type": mm["sourceType"], "timestamp_gmt": mm["timestampGMT"]}
			if w, ok := asNumber(mm["weight"]); ok {
				item["weight_kg"] = round(w/1000, 2)
			}
			preview = append(preview, dropNil(item))
		}
		return map[string]any{"status": "needs_confirmation", "date": in.Date, "delete_all": deleteAll, "measurement_count": len(preview),
			"measurements": preview, "message": "This permanently deletes the listed weight measurements and cannot be undone. Call again with confirm=true to proceed."}, nil
	}
	n, deleted, err := s.Client.DeleteWeighIns(ctx, in.Date, deleteAll)
	if err != nil {
		return fail(err)
	}
	if !deleted {
		if deleteAll {
			return nothing(fmt.Sprintf("No weight measurements found for %s; nothing was deleted", in.Date)), nil
		}
		return nothing(fmt.Sprintf("Nothing was deleted for %s: the day has either no weight measurements or more than one, and deleting more than one requires delete_all=true", in.Date)), nil
	}
	return map[string]any{"status": "success", "date": in.Date, "deleted_count": n, "message": "Weight measurements deleted for " + in.Date}, nil
}

// normalizeUnit is unit_key.strip().lower() with the 'lb' alias.
func normalizeUnit(p *string) string {
	u := "kg"
	if p != nil {
		u = strings.ToLower(strings.TrimSpace(*p))
	}
	if u == "lb" {
		return "lbs"
	}
	return u
}

func addWeighIn(ctx context.Context, s *Session, in addWeighInIn) (any, error) {
	unit := normalizeUnit(in.UnitKey)
	if _, err := s.Client.AddWeighIn(ctx, in.Weight, unit); err != nil {
		return nil, fmt.Errorf("Error adding weight measurement: %w", err)
	}
	return map[string]any{"status": "success", "weight": in.Weight, "unit": unit, "message": "Weight measurement added successfully"}, nil
}

func addWeighInWithTimestamps(ctx context.Context, s *Session, in addWeighInTimestampsIn) (any, error) {
	unit := normalizeUnit(in.UnitKey)
	var local, gmt string
	if in.DateTimestamp != nil {
		local = *in.DateTimestamp
	}
	if in.GmtTimestamp != nil {
		gmt = *in.GmtTimestamp
	}
	switch {
	case local == "" && gmt == "":
		now := time.Now()
		local, gmt = now.Format("2006-01-02T15:04:05"), now.UTC().Format("2006-01-02T15:04:05")
	case local == "":
		// garminconnect would fill the local timestamp with "now" rather than derive it from gmt_timestamp, recording the wrong day.
		return "date_timestamp is required when gmt_timestamp is given: the local timestamp sets the day the weigh-in is recorded on.", nil
	}
	// garminconnect derives the GMT timestamp from dateTimestamp when empty.
	if _, err := s.Client.AddWeighInWithTimestamps(ctx, in.Weight, unit, local, gmt); err != nil {
		return nil, fmt.Errorf("Error adding weight measurement with timestamps: %w", err)
	}
	var gmtOut any
	if gmt != "" {
		gmtOut = gmt
	}
	return map[string]any{"status": "success", "weight": in.Weight, "unit": unit, "timestamp_local": local, "timestamp_gmt": gmtOut,
		"message": "Weight measurement added successfully"}, nil
}
