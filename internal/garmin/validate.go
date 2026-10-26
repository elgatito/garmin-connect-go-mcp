package garmin

import (
	"fmt"
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
