package garmin

import (
	"context"
	"net/http"
)

const (
	userProfileSettingsPath = "/userprofile-service/userprofile/settings"
	heartRateZonesPath      = "/biometric-service/heartRateZones"
)

// GetFullName returns the full name from the Garmin social profile.
func (c *Client) GetFullName(ctx context.Context) (any, error) {
	social, err := c.socialProfile(ctx)
	if err != nil {
		return nil, err
	}
	return stringField(social, "fullName"), nil
}

// GetUnitSystem returns the measurement system from the user settings.
func (c *Client) GetUnitSystem(ctx context.Context) (any, error) {
	settings, err := c.userSettings(ctx)
	if err != nil {
		return nil, err
	}
	userData, _ := settings["userData"].(map[string]any)
	v, ok := userData["measurementSystem"]
	if !ok {
		return nil, nil
	}
	return v, nil
}

// GetUserProfile returns the Garmin user settings payload.
func (c *Client) GetUserProfile(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, userSettingsPath, nil)
}

// GetUserProfileSettings returns the Garmin user profile settings.
func (c *Client) GetUserProfileSettings(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, userProfileSettingsPath, nil)
}

// GetHeartRateZones returns all saved heart-rate zone profiles.
func (c *Client) GetHeartRateZones(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, heartRateZonesPath, nil)
}

// SetHeartRateZones replaces the changed heart-rate zone profiles.
func (c *Client) SetHeartRateZones(ctx context.Context, zones []any) (any, error) {
	return c.DoJSON(ctx, Req{Method: http.MethodPut, Path: heartRateZonesPath, JSON: zones})
}
