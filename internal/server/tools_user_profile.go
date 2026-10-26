package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const fullNameDescription = "Get user's full name from profile"

const unitSystemDescription = "Get user's preferred unit system from profile"

const userProfileDescription = "Get user profile information"

const userProfileSettingsDescription = "Get user profile settings"

const heartRateZonesDescription = `Get the user's saved heart-rate training-zone configuration.

Garmin stores a generic DEFAULT profile plus optional sport-specific
overrides such as RUNNING and CYCLING. With no sport, this returns every
saved profile; pass a sport key to return just that profile.`

const setHeartRateZonesDescription = `Set an account-level heart-rate training-zone configuration for one sport.

WARNING: This mutates the user's Garmin Connect account configuration.
Garmin stores zones per sport, so DEFAULT (the generic fallback),
RUNNING, CYCLING, and other sport profiles are independent. Only the
requested sport is written; omitted values are read from that sport's
current configuration and preserved.

This change affects future activity recording and zone-based training.
It does NOT retroactively re-slice already-recorded activities: their
heart-rate zone boundaries were baked in when those activities were
recorded.

The calculation method may be max_hr (% maximum heart rate), hrr or
karvonen (% heart-rate reserve), lthr (% lactate-threshold heart rate),
or custom_bpm. Manual zone_boundaries are only accepted with the
custom_bpm method. Garmin's API does not persist a separate CUSTOM enum:
direct-BPM zones are sent and read back with trainingMethod=HR_MAX while
the explicit zone floors remain authoritative.
Changing the method or reference heart rates preserves the existing
zone floors; this tool does not calculate new percentage-based floors.

The tool performs a read-modify-write and then re-fetches the requested
sport. Its result is the configuration Garmin actually saved, not the
request payload. If verification fails after Garmin accepts the write,
it reports success with write_committed=true, a confirmation_error, and
the submitted_configuration. Re-read the profile before another update.`

type profileInput struct {
	Account
}

type getHeartRateZonesInput struct {
	Account
	Sport *string `json:"sport,omitempty" jsonschema:"Optional Garmin sport key (for example default, running, or cycling). \"generic\" is accepted as an alias for DEFAULT."`
}

type setHeartRateZonesInput struct {
	Account
	Sport              string  `json:"sport" jsonschema:"Garmin sport key, e.g. default/generic, running, or cycling."`
	MaxHR              *int    `json:"max_hr,omitempty" jsonschema:"Maximum heart rate in BPM."`
	RestingHR          *int    `json:"resting_hr,omitempty" jsonschema:"Resting heart rate in BPM. Supplying it disables Garmin's resting-HR auto-update for this zone profile."`
	LactateThresholdHR *int    `json:"lactate_threshold_hr,omitempty" jsonschema:"Lactate-threshold heart rate (LTHR) in BPM."`
	CalculationMethod  *string `json:"calculation_method,omitempty" jsonschema:"max_hr, hrr/karvonen, lthr, or custom_bpm."`
	ZoneBoundaries     []int   `json:"zone_boundaries,omitempty" jsonschema:"Five strictly increasing BPM floors [Z1, Z2, Z3, Z4, Z5]. Every floor must be at most max_hr."`
}

func registerUserProfileTools(r *registry) {
	addTool(r, "get_full_name", fullNameDescription, getFullName)
	addTool(r, "get_unit_system", unitSystemDescription, getUnitSystem)
	addTool(r, "get_user_profile", userProfileDescription, getUserProfile)
	addTool(r, "get_userprofile_settings", userProfileSettingsDescription, getUserProfileSettings)
	addTool(r, "get_heart_rate_zones", heartRateZonesDescription, getHeartRateZones)
	addTool(r, "set_heart_rate_zones", setHeartRateZonesDescription, setHeartRateZones)
}

func getFullName(ctx context.Context, s *Session, _ profileInput) (any, error) {
	v, err := s.Client.GetFullName(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving user's full name: %w", err)
	}
	return map[string]any{"full_name": v}, nil
}

func getUnitSystem(ctx context.Context, s *Session, _ profileInput) (any, error) {
	v, err := s.Client.GetUnitSystem(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving unit system: %w", err)
	}
	return map[string]any{"unit_system": v}, nil
}

func getUserProfile(ctx context.Context, s *Session, _ profileInput) (any, error) {
	v, err := s.Client.GetUserProfile(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving user profile: %w", err)
	}
	if isEmptyValue(v) {
		return "No user profile information found.", nil
	}
	return v, nil
}

func getUserProfileSettings(ctx context.Context, s *Session, _ profileInput) (any, error) {
	v, err := s.Client.GetUserProfileSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving user profile settings: %w", err)
	}
	if isEmptyValue(v) {
		return "No user profile settings found.", nil
	}
	return v, nil
}

func getHeartRateZones(ctx context.Context, s *Session, in getHeartRateZonesInput) (any, error) {
	zones, err := s.Client.GetHeartRateZones(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving heart-rate zones: %w", err)
	}
	list, ok := zones.([]any)
	if !ok {
		return nil, errors.New("Error retrieving heart-rate zones: Garmin returned an unexpected heart-rate-zone response")
	}
	if in.Sport == nil {
		if len(list) == 0 {
			return "No configured heart-rate zones found.", nil
		}
		return list, nil
	}
	sportKey, err := normalizeHRZoneSport(*in.Sport)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving heart-rate zones: %w", err)
	}
	configured, ok := findZone(list, sportKey)
	if !ok {
		return fmt.Sprintf("No configured heart-rate zones found for sport %s.", sportKey), nil
	}
	return configured, nil
}

func setHeartRateZones(ctx context.Context, s *Session, in setHeartRateZonesInput) (any, error) {
	if in.MaxHR == nil && in.RestingHR == nil && in.LactateThresholdHR == nil &&
		in.CalculationMethod == nil && in.ZoneBoundaries == nil {
		return "No fields to update — supply at least one of max_hr, resting_hr, lactate_threshold_hr, calculation_method, or zone_boundaries.", nil
	}

	tracker := newWriteTracker("set_heart_rate_zones", s.logWriter()).
		withPrefix("Error updating heart-rate zones")

	sportKey, err := normalizeHRZoneSport(in.Sport)
	if err != nil {
		return invalidHeartRateZones(err)
	}
	zones, err := s.Client.GetHeartRateZones(ctx)
	if err != nil {
		return nil, tracker.Fail(err)
	}
	list, ok := zones.([]any)
	if !ok {
		return invalidHeartRateZones(errors.New("Garmin returned an unexpected heart-rate-zone response"))
	}

	current, ok := findZone(list, sportKey)
	if !ok {
		def, hasDefault := findZone(list, "DEFAULT")
		if !hasDefault {
			return fmt.Sprintf(
				"Could not read current heart-rate zones for %s, and no DEFAULT profile is available to inherit — cannot apply update.",
				sportKey,
			), nil
		}
		current = cloneMap(def)
		current["sport"] = sportKey
	} else {
		current = cloneMap(current)
	}

	if in.MaxHR != nil {
		current["maxHeartRateUsed"] = *in.MaxHR
	}
	if in.RestingHR != nil {
		current["restingHeartRateUsed"] = *in.RestingHR
		current["restingHrAutoUpdateUsed"] = false
	}
	if in.LactateThresholdHR != nil {
		current["lactateThresholdHeartRateUsed"] = *in.LactateThresholdHR
	}
	normalizedMethod := ""
	if in.CalculationMethod != nil {
		normalizedMethod, err = normalizeHRZoneMethod(*in.CalculationMethod)
		if err != nil {
			return invalidHeartRateZones(err)
		}
		if normalizedMethod == "CUSTOM_BPM" {
			current["trainingMethod"] = "HR_MAX"
		} else {
			current["trainingMethod"] = normalizedMethod
		}
	}
	if in.ZoneBoundaries != nil {
		if len(in.ZoneBoundaries) != 5 {
			return invalidHeartRateZones(errors.New("zone_boundaries must contain exactly five BPM floors"))
		}
		for i, floor := range in.ZoneBoundaries {
			current[fmt.Sprintf("zone%dFloor", i+1)] = floor
		}
	}
	if in.ZoneBoundaries != nil && normalizedMethod != "CUSTOM_BPM" {
		return invalidHeartRateZones(errors.New("manual zone_boundaries require calculation_method='custom_bpm'"))
	}
	if normalizedMethod == "CUSTOM_BPM" && in.ZoneBoundaries == nil {
		return invalidHeartRateZones(errors.New("calculation_method='custom_bpm' requires all five zone_boundaries"))
	}

	current["changeState"] = "CHANGED"
	if err := validateHeartRateZoneConfig(current); err != nil {
		return invalidHeartRateZones(err)
	}

	tracker.Sent()
	if _, err := s.Client.SetHeartRateZones(ctx, []any{current}); err != nil {
		return nil, tracker.Fail(err)
	}
	tracker.Committed()

	confirmedZones, err := s.Client.GetHeartRateZones(ctx)
	if err != nil {
		return confirmationFailure(err, current), nil
	}
	confirmedList, ok := confirmedZones.([]any)
	if !ok {
		return confirmationFailure(errors.New("Garmin returned an unexpected heart-rate-zone response"), current), nil
	}
	confirmed, ok := findZone(confirmedList, sportKey)
	if !ok {
		return confirmationFailure(fmt.Errorf("No %s heart-rate zone profile was present on read-back", sportKey), current), nil
	}
	return confirmed, nil
}

func invalidHeartRateZones(err error) (any, error) {
	return fmt.Sprintf("Invalid heart-rate zone settings: %v", err), nil
}

func confirmationFailure(err error, submitted map[string]any) any {
	return map[string]any{
		"status":                  "success",
		"write_committed":         true,
		"message":                 "Garmin accepted the update, but the saved profile could not be confirmed. Re-read the profile before another update.",
		"confirmation_error":      err.Error(),
		"submitted_configuration": submitted,
	}
}

func isEmptyValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case map[string]any:
		return len(x) == 0
	case []any:
		return len(x) == 0
	case bool:
		return !x
	case float64:
		return x == 0
	case int:
		return x == 0
	}
	return false
}

func findZone(zones []any, sport string) (map[string]any, bool) {
	for _, zone := range zones {
		m, ok := zone.(map[string]any)
		if !ok {
			continue
		}
		if m["sport"] == sport {
			return m, true
		}
	}
	return nil, false
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

var sportKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

func normalizeHRZoneSport(sport string) (string, error) {
	normalized := strings.ToUpper(strings.TrimSpace(sport))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	if normalized == "GENERIC" {
		normalized = "DEFAULT"
	}
	if !sportKeyPattern.MatchString(normalized) {
		return "", errors.New("sport must be a non-empty Garmin sport key such as DEFAULT, RUNNING, or CYCLING")
	}
	return normalized, nil
}

var heartRateZoneMethods = map[string]string{
	"HR_MAX":            "HR_MAX",
	"MAX_HR":            "HR_MAX",
	"MAXHR":             "HR_MAX",
	"%MAX_HR":           "HR_MAX",
	"HR_RESERVE":        "HR_RESERVE",
	"HRR":               "HR_RESERVE",
	"KARVONEN":          "HR_RESERVE",
	"%HRR":              "HR_RESERVE",
	"LACTATE_THRESHOLD": "LACTATE_THRESHOLD",
	"LTHR":              "LACTATE_THRESHOLD",
	"%LTHR":             "LACTATE_THRESHOLD",
	"CUSTOM":            "CUSTOM_BPM",
	"CUSTOM_BPM":        "CUSTOM_BPM",
	"BPM":               "CUSTOM_BPM",
}

func normalizeHRZoneMethod(method string) (string, error) {
	normalized := strings.ToUpper(strings.TrimSpace(method))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	if v, ok := heartRateZoneMethods[normalized]; ok {
		return v, nil
	}
	return "", errors.New("calculation_method must be one of max_hr, hrr/karvonen, lthr, or custom_bpm")
}

func validateHeartRateZoneConfig(config map[string]any) error {
	numericFields := []struct {
		apiName    string
		publicName string
	}{
		{"maxHeartRateUsed", "max_hr"},
		{"restingHeartRateUsed", "resting_hr"},
		{"lactateThresholdHeartRateUsed", "lactate_threshold_hr"},
	}
	for _, field := range numericFields {
		value, present := config[field.apiName]
		if !present || value == nil {
			continue
		}
		if _, ok := bpmValue(value); !ok {
			return fmt.Errorf("%s must be an integer BPM value from 1 to 300", field.publicName)
		}
	}

	maxHR, ok := bpmValue(config["maxHeartRateUsed"])
	if !ok {
		return errors.New("the current or supplied max_hr must be an integer BPM value from 1 to 300")
	}

	if value, present := config["restingHeartRateUsed"]; present && value != nil {
		resting, _ := bpmValue(value)
		if resting >= maxHR {
			return errors.New("resting_hr must be lower than max_hr")
		}
	}
	if value, present := config["lactateThresholdHeartRateUsed"]; present && value != nil {
		lactate, _ := bpmValue(value)
		if lactate > maxHR {
			return errors.New("lactate_threshold_hr must not exceed max_hr")
		}
	}

	var boundaries [5]int
	for i := range boundaries {
		value, ok := bpmValue(config[fmt.Sprintf("zone%dFloor", i+1)])
		if !ok {
			return errors.New("all five zone boundaries must be integer BPM values from 1 to 300")
		}
		boundaries[i] = value
	}
	for i := 1; i < len(boundaries); i++ {
		if boundaries[i-1] >= boundaries[i] {
			return errors.New("zone boundaries must be strictly monotonically increasing")
		}
	}
	if boundaries[len(boundaries)-1] > maxHR {
		return errors.New("zone boundaries must not exceed max_hr")
	}
	return nil
}

func bpmValue(v any) (int, bool) {
	var n int
	switch x := v.(type) {
	case int:
		n = x
	case int64:
		n = int(x)
	case float64:
		n = int(x)
		if float64(n) != x {
			return 0, false
		}
	case json.Number:
		parsed, err := x.Int64()
		if err != nil {
			return 0, false
		}
		n = int(parsed)
	default:
		return 0, false
	}
	if n <= 0 || n > 300 {
		return 0, false
	}
	return n, true
}
