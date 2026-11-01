package garmin

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// Tokens is the persisted login state; the JSON keys match python-garminconnect's garmin_tokens.json.
type Tokens struct {
	DIToken        string `json:"di_token"`
	DIRefreshToken string `json:"di_refresh_token"`
	DIClientID     string `json:"di_client_id"`
}

// jwtClaims decodes a JWT payload without verifying it.
func jwtClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}

// Expiry returns the access token's exp claim; ok is false when there is none.
func (t Tokens) Expiry() (time.Time, bool) {
	exp, _ := jwtClaims(t.DIToken)["exp"].(float64)
	if exp == 0 {
		return time.Time{}, false
	}
	return time.Unix(int64(exp), 0), true
}

// ExpiresSoon reports whether the access token expires within 15 minutes of now.
func (t Tokens) ExpiresSoon(now time.Time) bool {
	exp, ok := t.Expiry()
	return ok && now.After(exp.Add(-900*time.Second))
}

func clientIDFromJWT(token string) string {
	if v, ok := jwtClaims(token)["client_id"].(string); ok {
		return v
	}
	return ""
}
