package garmin

import "testing"

func TestValidateDate(t *testing.T) {
	if got, err := ValidateDate(" 2024-02-29 ", "cdate"); err != nil || got != "2024-02-29" {
		t.Errorf("leap day: %q %v", got, err)
	}
	for in, want := range map[string]string{
		"2024-1-5":   "cdate must be in format 'YYYY-MM-DD', got: 2024-1-5",
		"":           "cdate must be in format 'YYYY-MM-DD', got: ",
		"2023-02-29": "invalid cdate: time data '2023-02-29' does not match format '%Y-%m-%d'",
		"2024-13-01": "invalid cdate: time data '2024-13-01' does not match format '%Y-%m-%d'",
		"0000-01-01": "invalid cdate: time data '0000-01-01' does not match format '%Y-%m-%d'",
	} {
		if _, err := ValidateDate(in, "cdate"); err == nil || err.Error() != want {
			t.Errorf("%q: %v, want %s", in, err, want)
		}
	}
}

func TestValidatePositiveInt(t *testing.T) {
	if _, err := ValidatePositiveInt(0, "weeks"); err == nil || err.Error() != "weeks must be a positive integer, got: 0" {
		t.Error(err)
	}
	if v, err := ValidatePositiveInt(3, "weeks"); v != 3 || err != nil {
		t.Error(v, err)
	}
}
