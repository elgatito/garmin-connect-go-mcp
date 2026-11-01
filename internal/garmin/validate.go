package garmin

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Port of python-garminconnect's input validators.

var dateRe = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

// ValidateDate is _validate_date_format: trim, require YYYY-MM-DD and a real calendar date.
func ValidateDate(s, name string) (string, error) {
	s = strings.TrimSpace(s)
	if !dateRe.MatchString(s) {
		return "", fmt.Errorf("%s must be in format 'YYYY-MM-DD', got: %s", name, s)
	}
	if t, err := time.Parse(time.DateOnly, s); err != nil || t.Year() < 1 {
		return "", fmt.Errorf("invalid %s: time data '%s' does not match format '%%Y-%%m-%%d'", name, s)
	}
	return s, nil
}

// ValidatePositiveInt is _validate_positive_integer.
func ValidatePositiveInt(v int, name string) (int, error) {
	if v <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer, got: %d", name, v)
	}
	return v, nil
}
