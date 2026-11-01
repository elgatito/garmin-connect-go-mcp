package server

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/http"
	"regexp"
	"strings"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

// Port of garmin_mcp/user_profile.py.

const heartRateZonesPath = "/biometric-service/heartRateZones"

var heartRateZoneMethods = map[string]string{
	"HR_MAX": "HR_MAX", "MAX_HR": "HR_MAX", "MAXHR": "HR_MAX", "%MAX_HR": "HR_MAX",
	"HR_RESERVE": "HR_RESERVE", "HRR": "HR_RESERVE", "KARVONEN": "HR_RESERVE", "%HRR": "HR_RESERVE",
	"LACTATE_THRESHOLD": "LACTATE_THRESHOLD", "LTHR": "LACTATE_THRESHOLD", "%LTHR": "LACTATE_THRESHOLD",
	// Garmin does not persist a distinct custom-method value. The setter uses this local sentinel,
	// sends HR_MAX, and relies on the explicit BPM floors.
	"CUSTOM": "CUSTOM_BPM", "CUSTOM_BPM": "CUSTOM_BPM", "BPM": "CUSTOM_BPM",
}

var sportKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// valueError marks the errors Python raises as ValueError (reported as invalid settings before the write).
type valueError string

func (e valueError) Error() string { return string(e) }

type noArgs struct{ Account }

type heartRateZonesIn struct {
	Account
	Sport *string `json:"sport,omitempty" jsonschema:"Optional Garmin sport key (for example default, running, or cycling). \"generic\" is accepted as an alias for DEFAULT."`
}

type setHeartRateZonesIn struct {
	Account
	Sport              string  `json:"sport" jsonschema:"Garmin sport key, e.g. default/generic, running, or cycling."`
	MaxHR              *int    `json:"max_hr,omitempty" jsonschema:"Maximum heart rate in BPM."`
	RestingHR          *int    `json:"resting_hr,omitempty" jsonschema:"Resting heart rate in BPM. Supplying it disables Garmin's resting-HR auto-update for this zone profile."`
	LactateThresholdHR *int    `json:"lactate_threshold_hr,omitempty" jsonschema:"Lactate-threshold heart rate (LTHR) in BPM."`
	CalculationMethod  *string `json:"calculation_method,omitempty" jsonschema:"max_hr, hrr/karvonen, lthr, or custom_bpm."`
	ZoneBoundaries     []int   `json:"zone_boundaries,omitempty" jsonschema:"Five strictly increasing BPM floors [Z1, Z2, Z3, Z4, Z5]. Every floor must be at most max_hr."`
}

func registerUserProfileTools(r *registry) {
	addTool(r, "get_full_name", "Get user's full name from profile", func(ctx context.Context, s *Session, _ noArgs) (any, error) {
		name, err := s.Client.GetFullName(ctx)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving user's full name: %w", err)
		}
		return map[string]any{"full_name": name}, nil
	})

	addTool(r, "get_unit_system", "Get user's preferred unit system from profile", func(ctx context.Context, s *Session, _ noArgs) (any, error) {
		unit, err := s.Client.GetUnitSystem(ctx)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving unit system: %w", err)
		}
		return map[string]any{"unit_system": unit}, nil
	})

	addTool(r, "get_user_profile", "Get user profile information", func(ctx context.Context, s *Session, _ noArgs) (any, error) {
		profile, err := s.Client.GetUserProfile(ctx)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving user profile: %w", err)
		}
		if falsy(profile) {
			return "No user profile information found.", nil
		}
		return profile, nil
	})

	addTool(r, "get_userprofile_settings", "Get user profile settings", func(ctx context.Context, s *Session, _ noArgs) (any, error) {
		settings, err := s.Client.GetUserprofileSettings(ctx)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving user profile settings: %w", err)
		}
		if falsy(settings) {
			return "No user profile settings found.", nil
		}
		return settings, nil
	})

	addTool(r, "get_heart_rate_zones", `Get the user's saved heart-rate training-zone configuration.

Garmin stores a generic DEFAULT profile plus optional sport-specific
overrides such as RUNNING and CYCLING. With no sport, this returns every
saved profile; pass a sport key to return just that profile.`, getHeartRateZones)

	addTool(r, "set_heart_rate_zones", `Set an account-level heart-rate training-zone configuration for one sport.

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
the submitted_configuration. Re-read the profile before another update.`, setHeartRateZones)
}

func normalizeHRZoneSport(sport string) (string, error) {
	n := strings.NewReplacer("-", "_", " ", "_").Replace(strings.ToUpper(strings.TrimSpace(sport)))
	if n == "GENERIC" {
		n = "DEFAULT"
	}
	if !sportKeyRe.MatchString(n) {
		return "", valueError("sport must be a non-empty Garmin sport key such as DEFAULT, RUNNING, or CYCLING")
	}
	return n, nil
}

func normalizeHRZoneMethod(method string) (string, error) {
	n := strings.NewReplacer("-", "_", " ", "_").Replace(strings.ToUpper(strings.TrimSpace(method)))
	if m, ok := heartRateZoneMethods[n]; ok {
		return m, nil
	}
	return "", valueError("calculation_method must be one of max_hr, hrr/karvonen, lthr, or custom_bpm")
}

// bpm returns v as a whole number of beats; ok is false for anything that isn't an integer.
func bpm(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok && f == math.Trunc(f)
}

func validBPM(v any) bool {
	f, ok := bpm(v)
	return ok && f > 0 && f <= 300
}

// validateHeartRateZoneConfig checks the merged configuration before the account-level write.
func validateHeartRateZoneConfig(c map[string]any) error {
	for _, f := range [][2]string{{"maxHeartRateUsed", "max_hr"}, {"restingHeartRateUsed", "resting_hr"}, {"lactateThresholdHeartRateUsed", "lactate_threshold_hr"}} {
		if v := c[f[0]]; v != nil && !validBPM(v) {
			return valueError(f[1] + " must be an integer BPM value from 1 to 300")
		}
	}
	if !validBPM(c["maxHeartRateUsed"]) {
		return valueError("the current or supplied max_hr must be an integer BPM value from 1 to 300")
	}
	maxHR, _ := bpm(c["maxHeartRateUsed"])
	if v, ok := bpm(c["restingHeartRateUsed"]); ok && v >= maxHR {
		return valueError("resting_hr must be lower than max_hr")
	}
	if v, ok := bpm(c["lactateThresholdHeartRateUsed"]); ok && v > maxHR {
		return valueError("lactate_threshold_hr must not exceed max_hr")
	}
	var floors []float64
	for z := 1; z <= 5; z++ {
		v := c[fmt.Sprintf("zone%dFloor", z)]
		if !validBPM(v) {
			return valueError("all five zone boundaries must be integer BPM values from 1 to 300")
		}
		f, _ := bpm(v)
		floors = append(floors, f)
	}
	for i := 1; i < len(floors); i++ {
		if floors[i-1] >= floors[i] {
			return valueError("zone boundaries must be strictly monotonically increasing")
		}
	}
	if floors[4] > maxHR {
		return valueError("zone boundaries must not exceed max_hr")
	}
	return nil
}

// heartRateZoneConfigs reads the saved per-sport zone configurations.
func heartRateZoneConfigs(ctx context.Context, s *Session) ([]any, error) {
	v, err := s.Client.GetJSON(ctx, heartRateZonesPath, nil)
	if err != nil {
		return nil, err
	}
	zones, ok := v.([]any)
	if !ok {
		return nil, valueError("Garmin returned an unexpected heart-rate-zone response")
	}
	return zones, nil
}

func zoneForSport(zones []any, sport string) map[string]any {
	for _, z := range zones {
		if m, ok := z.(map[string]any); ok && m["sport"] == sport {
			return m
		}
	}
	return nil
}

func getHeartRateZones(ctx context.Context, s *Session, in heartRateZonesIn) (any, error) {
	zones, err := heartRateZoneConfigs(ctx, s)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving heart-rate zones: %w", err)
	}
	if in.Sport == nil {
		if len(zones) == 0 {
			return "No configured heart-rate zones found.", nil
		}
		return zones, nil
	}
	key, err := normalizeHRZoneSport(*in.Sport)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving heart-rate zones: %w", err)
	}
	configured := zoneForSport(zones, key)
	if configured == nil {
		return fmt.Sprintf("No configured heart-rate zones found for sport %s.", key), nil
	}
	return configured, nil
}

func setHeartRateZones(ctx context.Context, s *Session, in setHeartRateZonesIn) (any, error) {
	if in.MaxHR == nil && in.RestingHR == nil && in.LactateThresholdHR == nil && in.CalculationMethod == nil && in.ZoneBoundaries == nil {
		return "No fields to update — supply at least one of max_hr, resting_hr, lactate_threshold_hr, calculation_method, or zone_boundaries.", nil
	}
	tr := newWriteTracker("set_heart_rate_zones", "Error updating heart-rate zones", s.Log)
	fail := func(err error) error {
		var ve valueError
		if errors.As(err, &ve) && !tr.sent {
			return fmt.Errorf("Invalid heart-rate zone settings: %w", err)
		}
		return tr.Fail(err)
	}

	sportKey, err := normalizeHRZoneSport(in.Sport)
	if err != nil {
		return nil, fail(err)
	}
	zones, err := heartRateZoneConfigs(ctx, s)
	if err != nil {
		return nil, fail(err)
	}
	current := zoneForSport(zones, sportKey)
	if current == nil {
		def := zoneForSport(zones, "DEFAULT")
		if def == nil {
			return fmt.Sprintf("Could not read current heart-rate zones for %s, and no DEFAULT profile is available to inherit — cannot apply update.", sportKey), nil
		}
		current = maps.Clone(def)
		current["sport"] = sportKey
	} else {
		current = maps.Clone(current)
	}

	if in.MaxHR != nil {
		current["maxHeartRateUsed"] = float64(*in.MaxHR)
	}
	if in.RestingHR != nil {
		current["restingHeartRateUsed"] = float64(*in.RestingHR)
		current["restingHrAutoUpdateUsed"] = false
	}
	if in.LactateThresholdHR != nil {
		current["lactateThresholdHeartRateUsed"] = float64(*in.LactateThresholdHR)
	}
	method := ""
	if in.CalculationMethod != nil {
		if method, err = normalizeHRZoneMethod(*in.CalculationMethod); err != nil {
			return nil, fail(err)
		}
		current["trainingMethod"] = method
		if method == "CUSTOM_BPM" {
			current["trainingMethod"] = "HR_MAX"
		}
	}
	if in.ZoneBoundaries != nil {
		if len(in.ZoneBoundaries) != 5 {
			return nil, fail(valueError("zone_boundaries must contain exactly five BPM floors"))
		}
		for i, floor := range in.ZoneBoundaries {
			current[fmt.Sprintf("zone%dFloor", i+1)] = float64(floor)
		}
	}
	if in.ZoneBoundaries != nil && method != "CUSTOM_BPM" {
		return nil, fail(valueError("manual zone_boundaries require calculation_method='custom_bpm'"))
	}
	if method == "CUSTOM_BPM" && in.ZoneBoundaries == nil {
		return nil, fail(valueError("calculation_method='custom_bpm' requires all five zone_boundaries"))
	}

	current["changeState"] = "CHANGED"
	if err := validateHeartRateZoneConfig(current); err != nil {
		return nil, fail(err)
	}

	// Garmin accepts an array of changed sport profiles and answers 204 No Content.
	tr.Sent()
	if _, err := s.Client.Do(ctx, garmin.Req{Method: http.MethodPut, Path: heartRateZonesPath, JSON: []any{current}}); err != nil {
		return nil, fail(err)
	}
	tr.Committed()

	confirmedZones, err := heartRateZoneConfigs(ctx, s)
	var confirmed map[string]any
	if err == nil {
		if confirmed = zoneForSport(confirmedZones, sportKey); confirmed == nil {
			err = fmt.Errorf("No %s heart-rate zone profile was present on read-back", sportKey)
		}
	}
	if err != nil {
		return map[string]any{
			"status":                  "success",
			"write_committed":         true,
			"message":                 "Garmin accepted the update, but the saved profile could not be confirmed. Re-read the profile before another update.",
			"confirmation_error":      err.Error(),
			"submitted_configuration": current,
		}, nil
	}
	return confirmed, nil
}
