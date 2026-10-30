package server

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerProfile(srv *mcp.Server, m *manager) {
	for _, item := range []struct{ name, description, key string }{{"get_full_name", "Get user's full name from profile", "full_name"}, {"get_unit_system", "Get user's preferred unit system from profile", "unit_system"}} {
		item := item
		addTool(srv, m, item.name, item.description, nil, func(ctx context.Context, s *Session, _ map[string]any) (any, error) {
			p, err := s.Client.Profile(ctx)
			if err != nil {
				label := "user's full name"
				if item.key == "unit_system" {
					label = "unit system"
				}
				return nil, fmt.Errorf("Error retrieving %s: %w", label, err)
			}
			value := p.FullName
			if item.key == "unit_system" {
				value = p.UnitSystem
			}
			return map[string]any{item.key: value}, nil
		})
	}
	for _, item := range []struct{ name, description, path string }{{"get_user_profile", "Get user profile information", "/userprofile-service/userprofile/user-settings"}, {"get_userprofile_settings", "Get user profile settings", "/userprofile-service/userprofile/settings"}} {
		item := item
		addTool(srv, m, item.name, item.description, nil, func(ctx context.Context, s *Session, _ map[string]any) (any, error) {
			v, err := s.Client.GetJSON(ctx, item.path, nil)
			label := "user profile"
			missing := "No user profile information found."
			if item.name == "get_userprofile_settings" {
				label = "user profile settings"
				missing = "No user profile settings found."
			}
			if err != nil {
				return nil, fmt.Errorf("Error retrieving %s: %w", label, err)
			}
			if empty(v) {
				return missing, nil
			}
			return v, nil
		})
	}
	addTool(srv, m, "get_heart_rate_zones", "Get the user's saved heart-rate training-zone configuration.", map[string]any{"sport": map[string]any{"type": "string", "description": "Optional Garmin sport key."}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		v, err := s.Client.GetJSON(ctx, "/biometric-service/heartRateZones", nil)
		if err != nil {
			return nil, err
		}
		zones, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("Garmin returned an unexpected heart-rate-zone response")
		}
		sport, ok := args["sport"].(string)
		if !ok {
			if len(zones) == 0 {
				return "No configured heart-rate zones found.", nil
			}
			return zones, nil
		}
		sport = strings.ToUpper(strings.TrimSpace(sport))
		sport = strings.NewReplacer("-", "_", " ", "_").Replace(sport)
		if sport == "GENERIC" {
			sport = "DEFAULT"
		}
		if !regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`).MatchString(sport) {
			return nil, fmt.Errorf("sport must be a non-empty Garmin sport key such as DEFAULT, RUNNING, or CYCLING")
		}
		for _, v := range zones {
			if z, ok := v.(map[string]any); ok && z["sport"] == sport {
				return z, nil
			}
		}
		return fmt.Sprintf("No configured heart-rate zones found for sport %s.", sport), nil
	})
}
