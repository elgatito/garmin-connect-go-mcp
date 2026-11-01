package garmin

import "context"

const userprofileSettingsPath = "/userprofile-service/userprofile/settings"

// GetFullName returns the profile's full name (python-garminconnect caches it at login; we cache on first use).
func (c *Client) GetFullName(ctx context.Context) (string, error) {
	p, err := c.Profile(ctx)
	return p.FullName, err
}

// GetUnitSystem returns userData.measurementSystem from the user settings.
func (c *Client) GetUnitSystem(ctx context.Context) (string, error) {
	p, err := c.Profile(ctx)
	return p.UnitSystem, err
}

// GetUserProfile returns all user settings.
func (c *Client) GetUserProfile(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, userSettingsPath, nil)
}

// GetUserprofileSettings returns the userprofile settings.
func (c *Client) GetUserprofileSettings(ctx context.Context) (any, error) {
	return c.GetJSON(ctx, userprofileSettingsPath, nil)
}
