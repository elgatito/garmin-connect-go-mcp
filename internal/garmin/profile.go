package garmin

import (
	"context"
	"errors"
)

const (
	socialProfilePath = "/userprofile-service/socialProfile"
	userSettingsPath  = "/userprofile-service/userprofile/user-settings"
)

type Profile struct {
	DisplayName, FullName, UnitSystem string
}

// Profile returns the account's profile, fetched once and then cached.
func (c *Client) Profile(ctx context.Context) (Profile, error) {
	c.mu.Lock()
	cached := c.profile
	c.mu.Unlock()
	if cached != nil {
		return *cached, nil
	}

	v, err := c.GetJSON(ctx, socialProfilePath, nil)
	if err != nil {
		return Profile{}, err
	}
	prof, ok := v.(map[string]any)
	if !ok {
		return Profile{}, errors.New("invalid profile data found")
	}
	v, err = c.GetJSON(ctx, userSettingsPath, nil)
	if err != nil {
		return Profile{}, err
	}
	settings, _ := v.(map[string]any)
	userData, ok := settings["userData"].(map[string]any)
	if !ok {
		return Profile{}, errors.New("invalid user settings found")
	}

	p := Profile{}
	p.DisplayName, _ = prof["displayName"].(string)
	p.FullName, _ = prof["fullName"].(string)
	p.UnitSystem, _ = userData["measurementSystem"].(string)
	c.mu.Lock()
	c.profile = &p
	c.mu.Unlock()
	return p, nil
}
