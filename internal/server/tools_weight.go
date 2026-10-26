package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

const (
	getWeighInsDescription = `Get weight measurements between specified dates`

	getDailyWeighInsDescription = `Get weight measurements for a specific date`

	deleteWeighInsDescription = `Delete weight measurements for a specific date

Permanently removes the selected day's weigh-ins. This cannot be undone.
Call once with confirm=false (the default) to preview the measurements,
then call again with confirm=true to delete them.`

	addWeighInDescription = `Add a new weight measurement`

	addWeighInWithTimestampsDescription = `Add a new weight measurement with specific timestamps

If both timestamps are omitted, the current time is used. If only
date_timestamp is given, the GMT timestamp is derived from it.`
)

type getWeighInsInput struct {
	Account
	StartDate string `json:"start_date" jsonschema:"Start date in YYYY-MM-DD format"`
	EndDate   string `json:"end_date" jsonschema:"End date in YYYY-MM-DD format"`
}

type dailyWeighInsInput struct {
	Account
	Date string `json:"date" jsonschema:"Date in YYYY-MM-DD format"`
}

type deleteWeighInsInput struct {
	Account
	Date      string `json:"date" jsonschema:"Date in YYYY-MM-DD format"`
	DeleteAll *bool  `json:"delete_all,omitempty" jsonschema:"Whether to delete all measurements for the day"`
	Confirm   bool   `json:"confirm,omitempty" jsonschema:"Set true after reviewing the preview to perform the deletion"`
}

type addWeighInInput struct {
	Account
	Weight  float64 `json:"weight" jsonschema:"Weight value"`
	UnitKey string  `json:"unit_key,omitempty" jsonschema:"Unit of weight ('kg' or 'lbs'; 'lb' is accepted as an alias)"`
}

type addWeighInWithTimestampsInput struct {
	Account
	Weight        float64 `json:"weight" jsonschema:"Weight value"`
	UnitKey       string  `json:"unit_key,omitempty" jsonschema:"Unit of weight ('kg' or 'lbs'; 'lb' is accepted as an alias)"`
	DateTimestamp string  `json:"date_timestamp,omitempty" jsonschema:"Local timestamp in format YYYY-MM-DDThh:mm:ss (required if gmt_timestamp is given -- it sets the day the weigh-in is recorded on)"`
	GmtTimestamp  string  `json:"gmt_timestamp,omitempty" jsonschema:"GMT timestamp in format YYYY-MM-DDThh:mm:ss"`
}

func registerWeightTools(r *registry) {
	addTool(r, "get_weigh_ins", getWeighInsDescription, getWeighIns)
	addTool(r, "get_daily_weigh_ins", getDailyWeighInsDescription, getDailyWeighIns)
	addTool(r, "delete_weigh_ins", deleteWeighInsDescription, deleteWeighIns)
	addTool(r, "add_weigh_in", addWeighInDescription, addWeighIn)
	addTool(r, "add_weigh_in_with_timestamps", addWeighInWithTimestampsDescription, addWeighInWithTimestamps)
}

func weighInMeasurement(weight map[string]any, includeDate bool) map[string]any {
	measurement := map[string]any{}
	if includeDate {
		measurement["date"] = weight["calendarDate"]
	}
	grams, hasGrams := numberOf(weight["weight"])
	measurement["weight_grams"] = weight["weight"]
	if hasGrams && grams != 0 {
		measurement["weight_kg"] = roundTo(grams/1000, 2)
	}
	measurement["bmi"] = weight["bmi"]
	measurement["body_fat_percent"] = weight["bodyFat"]
	measurement["body_water_percent"] = weight["bodyWater"]
	measurement["bone_mass_grams"] = weight["boneMass"]
	measurement["muscle_mass_grams"] = weight["muscleMass"]
	measurement["source_type"] = weight["sourceType"]
	measurement["timestamp_gmt"] = weight["timestampGMT"]
	return pruneNil(measurement)
}

func averageWeight(curated map[string]any, data map[string]any) {
	totalAvg := asMap(data["totalAverage"])
	if grams, ok := numberOf(totalAvg["weight"]); ok && grams != 0 {
		curated["average_weight_grams"] = totalAvg["weight"]
		curated["average_weight_kg"] = roundTo(grams/1000, 2)
	}
}

func getWeighIns(ctx context.Context, s *Session, in getWeighInsInput) (any, error) {
	dataValue, err := s.Client.GetWeighIns(ctx, in.StartDate, in.EndDate)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving weight measurements: %w", err)
	}
	noData := fmt.Sprintf("No weight measurements found between %s and %s.", in.StartDate, in.EndDate)
	if !truthy(dataValue) {
		return noData, nil
	}
	data := asMap(dataValue)
	daily := asList(data["dailyWeightSummaries"])
	if len(daily) == 0 {
		return noData, nil
	}

	var measurements []any
	for _, day := range daily {
		measurements = append(measurements, asList(asMap(day)["allWeightMetrics"])...)
	}
	curated := map[string]any{
		"date_range":        map[string]any{"start": in.StartDate, "end": in.EndDate},
		"measurement_count": len(measurements),
		"days_with_data":    len(daily),
		"measurements":      []any{},
	}
	items := make([]map[string]any, 0, len(measurements))
	for _, measurement := range measurements {
		items = append(items, weighInMeasurement(asMap(measurement), true))
	}
	sortMapsByString(items, "date", true)
	curated["measurements"] = items
	averageWeight(curated, data)
	return curated, nil
}

func getDailyWeighIns(ctx context.Context, s *Session, in dailyWeighInsInput) (any, error) {
	dataValue, err := s.Client.GetDailyWeighIns(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving daily weight measurements: %w", err)
	}
	noData := fmt.Sprintf("No weight measurements found for %s.", in.Date)
	if !truthy(dataValue) {
		return noData, nil
	}
	data := asMap(dataValue)
	weightList := asList(data["dateWeightList"])
	if len(weightList) == 0 {
		return noData, nil
	}
	curated := map[string]any{
		"date":              in.Date,
		"measurement_count": len(weightList),
		"measurements":      []any{},
	}
	items := make([]map[string]any, 0, len(weightList))
	for _, weight := range weightList {
		items = append(items, weighInMeasurement(asMap(weight), false))
	}
	curated["measurements"] = items
	averageWeight(curated, data)
	return curated, nil
}

func sortMapsByString(items []map[string]any, key string, descending bool) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0; j-- {
			a, b := stringFieldOf(items[j-1], key), stringFieldOf(items[j], key)
			if (descending && a >= b) || (!descending && a <= b) {
				break
			}
			items[j-1], items[j] = items[j], items[j-1]
		}
	}
}

func deleteWeighIns(ctx context.Context, s *Session, in deleteWeighInsInput) (any, error) {
	deleteAll := true
	if in.DeleteAll != nil {
		deleteAll = *in.DeleteAll
	}

	if !in.Confirm {
		dataValue, err := s.Client.GetDailyWeighIns(ctx, in.Date)
		if err != nil {
			return nil, fmt.Errorf("Error deleting weight measurements: %w", err)
		}
		measurements := asList(asMap(dataValue)["dateWeightList"])
		if len(measurements) == 0 || (len(measurements) > 1 && !deleteAll) {
			message := fmt.Sprintf("No weight measurements found for %s; nothing was deleted", in.Date)
			if len(measurements) > 0 {
				message = fmt.Sprintf("Nothing was deleted for %s: deleting multiple measurements requires delete_all=true", in.Date)
			}
			return map[string]any{
				"status":        "nothing_deleted",
				"date":          in.Date,
				"deleted_count": 0,
				"message":       message,
			}, nil
		}
		preview := make([]map[string]any, 0, len(measurements))
		for _, raw := range measurements {
			measurement := asMap(raw)
			item := map[string]any{
				"id":            measurement["samplePk"],
				"source_type":   measurement["sourceType"],
				"timestamp_gmt": measurement["timestampGMT"],
			}
			if grams, ok := numberOf(measurement["weight"]); ok {
				item["weight_kg"] = roundTo(grams/1000, 2)
			}
			preview = append(preview, pruneNil(item))
		}
		return map[string]any{
			"status":            "needs_confirmation",
			"date":              in.Date,
			"delete_all":        deleteAll,
			"measurement_count": len(preview),
			"measurements":      preview,
			"message":           "This permanently deletes the listed weight measurements and cannot be undone. Call again with confirm=true to proceed.",
		}, nil
	}

	// Read the day view before creating the tracker so a failed read is not
	// reported as a possibly-committed write.
	dataValue, err := s.Client.GetDailyWeighIns(ctx, in.Date)
	if err != nil {
		return nil, fmt.Errorf("Error deleting weight measurements: %w", err)
	}
	measurements := asList(asMap(dataValue)["dateWeightList"])
	if len(measurements) == 0 || (len(measurements) > 1 && !deleteAll) {
		message := fmt.Sprintf("Nothing was deleted for %s: the day has either no weight measurements or more than one, and deleting more than one requires delete_all=true", in.Date)
		if deleteAll {
			message = fmt.Sprintf("No weight measurements found for %s; nothing was deleted", in.Date)
		}
		return map[string]any{
			"status":        "nothing_deleted",
			"date":          in.Date,
			"deleted_count": 0,
			"message":       message,
		}, nil
	}

	tracker := newWriteTracker("delete_weigh_ins", s.logWriter()).
		withPrefix("Error deleting weight measurements")
	tracker.Sent()
	for _, raw := range measurements {
		if _, err := s.Client.DeleteWeighIn(ctx, in.Date, asMap(raw)["samplePk"]); err != nil {
			return nil, tracker.Fail(err)
		}
	}
	tracker.Committed()
	return map[string]any{
		"status":        "success",
		"date":          in.Date,
		"deleted_count": len(measurements),
		"message":       fmt.Sprintf("Weight measurements deleted for %s", in.Date),
	}, nil
}

func normalizedWeightUnit(unitKey string) string {
	unitKey = strings.ToLower(strings.TrimSpace(unitKey))
	if unitKey == "" {
		return "kg"
	}
	if unitKey == "lb" {
		return "lbs"
	}
	return unitKey
}

func addWeighIn(ctx context.Context, s *Session, in addWeighInInput) (any, error) {
	unitKey := normalizedWeightUnit(in.UnitKey)
	if err := garmin.ValidateWeight(in.Weight); err != nil {
		return fmt.Sprintf("Error adding weight measurement: %v", err), nil
	}
	if err := garmin.ValidateWeightUnit(unitKey); err != nil {
		return fmt.Sprintf("Error adding weight measurement: %v", err), nil
	}
	tracker := newWriteTracker("add_weigh_in", s.logWriter()).
		withPrefix("Error adding weight measurement")
	tracker.Sent()
	if _, err := s.Client.AddWeighIn(ctx, in.Weight, unitKey); err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	return map[string]any{
		"status":  "success",
		"weight":  in.Weight,
		"unit":    unitKey,
		"message": "Weight measurement added successfully",
	}, nil
}

func addWeighInWithTimestamps(ctx context.Context, s *Session, in addWeighInWithTimestampsInput) (any, error) {
	unitKey := normalizedWeightUnit(in.UnitKey)
	dateTimestamp := in.DateTimestamp
	gmtTimestamp := in.GmtTimestamp
	var outputGMT any
	if in.GmtTimestamp != "" {
		outputGMT = in.GmtTimestamp
	}
	if dateTimestamp == "" && gmtTimestamp == "" {
		now := time.Now()
		dateTimestamp = now.Format("2006-01-02T15:04:05")
		gmtTimestamp = now.UTC().Format("2006-01-02T15:04:05")
		outputGMT = gmtTimestamp
	} else if dateTimestamp == "" {
		return "date_timestamp is required when gmt_timestamp is given: the local timestamp sets the day the weigh-in is recorded on.", nil
	}
	if err := garmin.ValidateWeightUnit(unitKey); err != nil {
		return fmt.Sprintf("Error adding weight measurement with timestamps: %v", err), nil
	}
	if err := garmin.ValidateWeighInTimestamps(dateTimestamp, gmtTimestamp); err != nil {
		return fmt.Sprintf("Error adding weight measurement with timestamps: %v", err), nil
	}
	if err := garmin.ValidateWeight(in.Weight); err != nil {
		return fmt.Sprintf("Error adding weight measurement with timestamps: %v", err), nil
	}

	tracker := newWriteTracker("add_weigh_in_with_timestamps", s.logWriter()).
		withPrefix("Error adding weight measurement with timestamps")
	tracker.Sent()
	if _, err := s.Client.AddWeighInWithTimestamps(ctx, in.Weight, unitKey, dateTimestamp, gmtTimestamp); err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	return map[string]any{
		"status":          "success",
		"weight":          in.Weight,
		"unit":            unitKey,
		"timestamp_local": dateTimestamp,
		"timestamp_gmt":   outputGMT,
		"message":         "Weight measurement added successfully",
	}, nil
}
