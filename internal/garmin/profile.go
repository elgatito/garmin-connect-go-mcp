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
	defer c.profileMu.Unlock()
	if c.profileCached {
		return c.profile, nil
	}

	v, err := c.GetJSON(ctx, socialProfilePath, nil)
	if err != nil {
		return Profile{}, err
	}
	social, _ := v.(map[string]any)
	p := Profile{
		DisplayName: stringField(social, "displayName"),
		FullName:    stringField(social, "fullName"),
	}

	v, err = c.GetJSON(ctx, userSettingsPath, nil)
	if err != nil {
		return Profile{}, err
	}
	settings, _ := v.(map[string]any)
	userData, _ := settings["userData"].(map[string]any)
	p.UnitSystem = stringField(userData, "measurementSystem")

	c.profile = p
	c.profileCached = true
	return p, nil
}

func stringField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}
