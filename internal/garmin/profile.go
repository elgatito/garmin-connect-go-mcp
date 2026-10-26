package garmin

import "context"

const (
	socialProfilePath = "/userprofile-service/socialProfile"
	userSettingsPath  = "/userprofile-service/userprofile/user-settings"
)

// Profile is the cached Garmin user profile.
type Profile struct {
	DisplayName string
	FullName    string
	UnitSystem  string
}

// Profile fetches the social profile and user settings once, then serves the
// cached copy.
func (c *Client) Profile(ctx context.Context) (Profile, error) {
	c.profileMu.Lock()
	if c.profileCached {
		p := c.profile
		c.profileMu.Unlock()
		return p, nil
	}
	c.profileMu.Unlock()

	social, err := c.socialProfile(ctx)
	if err != nil {
		return Profile{}, err
	}
	settings, err := c.userSettings(ctx)
	if err != nil {
		return Profile{}, err
	}
	userData, _ := settings["userData"].(map[string]any)
	p := Profile{
		DisplayName: stringField(social, "displayName"),
		FullName:    stringField(social, "fullName"),
		UnitSystem:  stringField(userData, "measurementSystem"),
	}

	c.profileMu.Lock()
	c.profile = p
	c.profileCached = true
	c.profileMu.Unlock()
	return p, nil
}

// CachedDisplayName returns the display name of a profile already fetched by
// Profile, GetFullName, or GetUnitSystem. It never contacts Garmin.
func (c *Client) CachedDisplayName() string {
	c.profileMu.Lock()
	defer c.profileMu.Unlock()
	if c.profileCached {
		return c.profile.DisplayName
	}
	if c.socialCached {
		return stringField(c.social, "displayName")
	}
	return ""
}

func (c *Client) socialProfile(ctx context.Context) (map[string]any, error) {
	c.profileMu.Lock()
	defer c.profileMu.Unlock()
	if c.socialCached {
		return c.social, nil
	}
	v, err := c.GetJSON(ctx, socialProfilePath, nil)
	if err != nil {
		return nil, err
	}
	m, _ := v.(map[string]any)
	c.social = m
	c.socialCached = true
	return m, nil
}

func (c *Client) userSettings(ctx context.Context) (map[string]any, error) {
	c.profileMu.Lock()
	defer c.profileMu.Unlock()
	if c.settingsCached {
		return c.settings, nil
	}
	v, err := c.GetJSON(ctx, userSettingsPath, nil)
	if err != nil {
		return nil, err
	}
	m, _ := v.(map[string]any)
	c.settings = m
	c.settingsCached = true
	return m, nil
}

func stringField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}
