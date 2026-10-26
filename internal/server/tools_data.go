package server

import (
	"context"
	"fmt"
	"math"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

const (
	addBodyCompositionDescription = "Add body composition data"

	setBloodPressureDescription = "Set blood pressure values"

	addHydrationDataDescription = "Add hydration data"
)

type addBodyCompositionInput struct {
	Account
	Date              string   `json:"date" jsonschema:"Date in YYYY-MM-DD format"`
	Weight            float64  `json:"weight" jsonschema:"Weight in kg"`
	PercentFat        *float64 `json:"percent_fat,omitempty" jsonschema:"Body fat percentage"`
	PercentHydration  *float64 `json:"percent_hydration,omitempty" jsonschema:"Hydration percentage"`
	VisceralFatMass   *float64 `json:"visceral_fat_mass,omitempty" jsonschema:"Visceral fat mass in kg (not grams)"`
	BoneMass          *float64 `json:"bone_mass,omitempty" jsonschema:"Bone mass in kg (not grams)"`
	MuscleMass        *float64 `json:"muscle_mass,omitempty" jsonschema:"Muscle mass in kg (not grams)"`
	BasalMet          *float64 `json:"basal_met,omitempty" jsonschema:"Basal metabolic rate in kcal/day"`
	ActiveMet         *float64 `json:"active_met,omitempty" jsonschema:"Active metabolic rate in kcal/day"`
	PhysiqueRating    *int     `json:"physique_rating,omitempty" jsonschema:"Physique rating"`
	MetabolicAge      *float64 `json:"metabolic_age,omitempty" jsonschema:"Metabolic age"`
	VisceralFatRating *int     `json:"visceral_fat_rating,omitempty" jsonschema:"Visceral fat rating"`
	BMI               *float64 `json:"bmi,omitempty" jsonschema:"Body Mass Index"`
}

type setBloodPressureInput struct {
	Account
	Systolic  int     `json:"systolic" jsonschema:"Systolic pressure (top number)"`
	Diastolic int     `json:"diastolic" jsonschema:"Diastolic pressure (bottom number)"`
	Pulse     int     `json:"pulse" jsonschema:"Pulse rate"`
	Notes     *string `json:"notes,omitempty" jsonschema:"Optional notes"`
}

type addHydrationDataInput struct {
	Account
	ValueInML int    `json:"value_in_ml" jsonschema:"Amount of liquid in milliliters"`
	Cdate     string `json:"cdate" jsonschema:"Date in YYYY-MM-DD format"`
	Timestamp string `json:"timestamp" jsonschema:"Timestamp in YYYY-MM-DDThh:mm:ss.sss format"`
}

func registerDataManagementTools(r *registry) {
	addTool(r, "add_body_composition", addBodyCompositionDescription, addBodyComposition)
	addTool(r, "set_blood_pressure", setBloodPressureDescription, setBloodPressure)
	addTool(r, "add_hydration_data", addHydrationDataDescription, addHydrationData)
}

// bodyCompositionMassError rejects masses Garmin's FIT uint16 encoding cannot
// represent (0xFFFF is the invalid sentinel).
func bodyCompositionMassError(field string, value *float64) string {
	if value == nil {
		return ""
	}
	if math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || *value >= 655.35 {
		return fmt.Sprintf("%s must be a finite value in kg (not grams), at least 0 and below 655.35 for FIT encoding", field)
	}
	return ""
}

func addBodyComposition(ctx context.Context, s *Session, in addBodyCompositionInput) (any, error) {
	masses := []struct {
		field string
		value *float64
	}{
		{"weight", &in.Weight},
		{"visceral_fat_mass", in.VisceralFatMass},
		{"bone_mass", in.BoneMass},
		{"muscle_mass", in.MuscleMass},
	}
	for _, mass := range masses {
		if msg := bodyCompositionMassError(mass.field, mass.value); msg != "" {
			return fmt.Sprintf("Error adding body composition data: %s", msg), nil
		}
	}
	if _, err := garmin.ValidateDate(in.Date, "date"); err != nil {
		return fmt.Sprintf("Error adding body composition data: %v", err), nil
	}
	if err := garmin.ValidateWeight(in.Weight); err != nil {
		return fmt.Sprintf("Error adding body composition data: %v", err), nil
	}

	tracker := newWriteTracker("add_body_composition", s.logWriter()).
		withPrefix("Error adding body composition data")
	tracker.Sent()
	result, err := s.Client.AddBodyComposition(ctx, in.Date, garmin.BodyComposition{
		Weight:            in.Weight,
		PercentFat:        in.PercentFat,
		PercentHydration:  in.PercentHydration,
		VisceralFatMass:   in.VisceralFatMass,
		BoneMass:          in.BoneMass,
		MuscleMass:        in.MuscleMass,
		BasalMet:          in.BasalMet,
		ActiveMet:         in.ActiveMet,
		PhysiqueRating:    in.PhysiqueRating,
		MetabolicAge:      in.MetabolicAge,
		VisceralFatRating: in.VisceralFatRating,
		BMI:               in.BMI,
	})
	if err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	return result, nil
}

func setBloodPressure(ctx context.Context, s *Session, in setBloodPressureInput) (any, error) {
	if err := garmin.ValidateBloodPressure(in.Systolic, in.Diastolic, in.Pulse); err != nil {
		return fmt.Sprintf("Error setting blood pressure values: %v", err), nil
	}
	tracker := newWriteTracker("set_blood_pressure", s.logWriter()).
		withPrefix("Error setting blood pressure values")
	tracker.Sent()
	result, err := s.Client.SetBloodPressure(ctx, in.Systolic, in.Diastolic, in.Pulse, in.Notes)
	if err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	return result, nil
}

func addHydrationData(ctx context.Context, s *Session, in addHydrationDataInput) (any, error) {
	if err := garmin.ValidateHydration(in.ValueInML, in.Cdate, in.Timestamp); err != nil {
		return fmt.Sprintf("Error adding hydration data: %v", err), nil
	}
	tracker := newWriteTracker("add_hydration_data", s.logWriter()).
		withPrefix("Error adding hydration data")
	tracker.Sent()
	result, err := s.Client.AddHydrationData(ctx, in.ValueInML, in.Cdate, in.Timestamp)
	if err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	return result, nil
}
