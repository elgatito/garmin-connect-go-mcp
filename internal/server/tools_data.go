package server

import (
	"context"
	"fmt"
	"math"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

// Port of garmin_mcp/data_management.py.

type addBodyCompositionIn struct {
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

type setBloodPressureIn struct {
	Account
	Systolic  int     `json:"systolic" jsonschema:"Systolic pressure (top number)"`
	Diastolic int     `json:"diastolic" jsonschema:"Diastolic pressure (bottom number)"`
	Pulse     int     `json:"pulse" jsonschema:"Pulse rate"`
	Notes     *string `json:"notes,omitempty" jsonschema:"Optional notes"`
}

type addHydrationIn struct {
	Account
	ValueInML int    `json:"value_in_ml" jsonschema:"Amount of liquid in milliliters"`
	CDate     string `json:"cdate" jsonschema:"Date in YYYY-MM-DD format"`
	Timestamp string `json:"timestamp" jsonschema:"Timestamp in YYYY-MM-DDThh:mm:ss.sss format"`
}

func registerDataTools(r *registry) {
	addTool(r, "add_body_composition", "Add body composition data", addBodyComposition)
	addTool(r, "set_blood_pressure", "Set blood pressure values", setBloodPressure)
	addTool(r, "add_hydration_data", "Add hydration data", addHydrationData)
}

// checkMassInKg rejects values the FIT uint16 (scaled by 100) cannot hold; 0xFFFF is the invalid-value sentinel.
func checkMassInKg(field string, v *float64) error {
	if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 || *v >= 655.35) {
		return fmt.Errorf("%s must be a finite value in kg (not grams), at least 0 and below 655.35 for FIT encoding", field)
	}
	return nil
}

func intPtrToFloat(p *int) *float64 {
	if p == nil {
		return nil
	}
	f := float64(*p)
	return &f
}

func addBodyComposition(ctx context.Context, s *Session, in addBodyCompositionIn) (any, error) {
	fail := func(err error) (any, error) { return nil, fmt.Errorf("Error adding body composition data: %w", err) }
	for _, f := range []struct {
		name string
		v    *float64
	}{{"weight", &in.Weight}, {"visceral_fat_mass", in.VisceralFatMass}, {"bone_mass", in.BoneMass}, {"muscle_mass", in.MuscleMass}} {
		if err := checkMassInKg(f.name, f.v); err != nil {
			return fail(err)
		}
	}
	result, err := s.Client.AddBodyComposition(ctx, in.Date, garmin.BodyComposition{
		Weight: in.Weight, PercentFat: in.PercentFat, PercentHydration: in.PercentHydration, VisceralFatMass: in.VisceralFatMass,
		BoneMass: in.BoneMass, MuscleMass: in.MuscleMass, BasalMet: in.BasalMet, ActiveMet: in.ActiveMet,
		PhysiqueRating: intPtrToFloat(in.PhysiqueRating), MetabolicAge: in.MetabolicAge,
		VisceralFatRating: intPtrToFloat(in.VisceralFatRating), BMI: in.BMI,
	})
	if err != nil {
		return fail(err)
	}
	return result, nil
}

func setBloodPressure(ctx context.Context, s *Session, in setBloodPressureIn) (any, error) {
	result, err := s.Client.SetBloodPressure(ctx, in.Systolic, in.Diastolic, in.Pulse, in.Notes)
	if err != nil {
		return nil, fmt.Errorf("Error setting blood pressure values: %w", err)
	}
	return result, nil
}

func addHydrationData(ctx context.Context, s *Session, in addHydrationIn) (any, error) {
	result, err := s.Client.AddHydrationData(ctx, float64(in.ValueInML), in.Timestamp, in.CDate)
	if err != nil {
		return nil, fmt.Errorf("Error adding hydration data: %w", err)
	}
	return result, nil
}
