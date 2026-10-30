package garmin

import "context"

type Profile struct{ DisplayName, FullName, UnitSystem string }

func (c *Client) Profile(ctx context.Context) (Profile, error) {
	c.profileMu.Lock()
	defer c.profileMu.Unlock()
	if c.profile != nil {
		return *c.profile, nil
	}
	social, err := c.GetJSON(ctx, "/userprofile-service/socialProfile", nil)
	if err != nil {
		return Profile{}, err
	}
	settings, err := c.GetJSON(ctx, "/userprofile-service/userprofile/user-settings", nil)
	if err != nil {
		return Profile{}, err
	}
	var p Profile
	if m, ok := social.(map[string]any); ok {
		p.DisplayName, _ = m["displayName"].(string)
		p.FullName, _ = m["fullName"].(string)
	}
	if m, ok := settings.(map[string]any); ok {
		if u, ok := m["userData"].(map[string]any); ok {
			p.UnitSystem, _ = u["measurementSystem"].(string)
		}
	}
	c.profile = &p
	return p, nil
}

func (c *Client) CachedProfile() (Profile, bool) {
	c.profileMu.Lock()
	defer c.profileMu.Unlock()
	if c.profile == nil {
		return Profile{}, false
	}
	return *c.profile, true
}
