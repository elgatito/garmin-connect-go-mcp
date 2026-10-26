package garmin

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

var dateFormatPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// ValidateDate validates a YYYY-MM-DD date string and returns it stripped.
func ValidateDate(dateStr, paramName string) (string, error) {
	dateStr = strings.TrimSpace(dateStr)
	if !dateFormatPattern.MatchString(dateStr) {
		return "", fmt.Errorf("%s must be in format 'YYYY-MM-DD', got: %s", paramName, dateStr)
	}
	if _, err := time.Parse("2006-01-02", dateStr); err != nil {
		return "", fmt.Errorf("invalid %s: %v", paramName, err)
	}
	return dateStr, nil
}

// ValidatePositiveInteger validates that a value is a positive integer.
func ValidatePositiveInteger(value int, paramName string) (int, error) {
	if value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer, got: %d", paramName, value)
	}
	return value, nil
}

// ValidateNonNegativeInteger validates that a value is a non-negative integer.
func ValidateNonNegativeInteger(value int, paramName string) (int, error) {
	if value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer, got: %d", paramName, value)
	}
	return value, nil
}

// ValidateWeightUnit validates a weigh-in unit key.
func ValidateWeightUnit(unitKey string) error {
	if unitKey != "kg" && unitKey != "lbs" {
		return fmt.Errorf("unitKey must be one of {'kg', 'lbs'}")
	}
	return nil
}

// ValidateWeight validates that a weigh-in weight is a positive finite number.
func ValidateWeight(weight float64) error {
	if math.IsNaN(weight) || math.IsInf(weight, 0) || weight <= 0 {
		return fmt.Errorf("weight must be positive, got: %v", weight)
	}
	return nil
}

// ValidateBloodPressure validates the accepted blood-pressure ranges.
func ValidateBloodPressure(systolic, diastolic, pulse int) error {
	for _, check := range []struct {
		name string
		val  int
		lo   int
		hi   int
	}{
		{"systolic", systolic, 70, 260},
		{"diastolic", diastolic, 40, 150},
		{"pulse", pulse, 20, 250},
	} {
		if check.val < check.lo || check.val > check.hi {
			return fmt.Errorf("%s must be an int in [%d, %d]", check.name, check.lo, check.hi)
		}
	}
	return nil
}

// ValidateWeighInTimestamps checks that any supplied weigh-in timestamps parse.
func ValidateWeighInTimestamps(dateTimestamp, gmtTimestamp string) error {
	_, _, err := weighInTimestamps(dateTimestamp, gmtTimestamp)
	return err
}

// ValidateHydration validates a hydration amount, date and matching timestamp.
func ValidateHydration(valueInML int, cdate, timestamp string) error {
	_, _, err := hydrationTimestamp(valueInML, cdate, timestamp)
	return err
}
