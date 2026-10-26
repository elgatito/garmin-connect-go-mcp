package garmin

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// Tokens holds the Garmin Connect OAuth tokens for one account.
type Tokens struct {
	DIToken        string `json:"di_token"`
	DIRefreshToken string `json:"di_refresh_token"`
	DIClientID     string `json:"di_client_id"`
}

// Expiry returns the DIToken's JWT expiry. It reports false when there is no
// parseable exp claim.
func (t Tokens) Expiry() (time.Time, bool) {
	claims, ok := jwtPayload(t.DIToken)
	if !ok {
		return time.Time{}, false
	}
	exp, ok := claims["exp"].(float64)
	if !ok {
		if s, isString := claims["exp"].(string); isString {
			if n, err := strconv.ParseInt(s, 10, 64); err == nil {
				return time.Unix(n, 0), true
			}
		}
		return time.Time{}, false
	}
	return time.Unix(int64(exp), 0), true
}

// ExpiresSoon reports whether the token is inside its 15-minute refresh window.
func (t Tokens) ExpiresSoon(now time.Time) bool {
	exp, ok := t.Expiry()
	return ok && now.After(exp.Add(-900*time.Second))
}

// clientID returns the client_id claim, or "" when absent.
func (t Tokens) clientID() string {
	claims, ok := jwtPayload(t.DIToken)
	if !ok {
		return ""
	}
	id, _ := claims["client_id"].(string)
	return id
}

// jwtPayload decodes a JWT payload without verifying the signature.
func jwtPayload(token string) (map[string]any, bool) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 || parts[1] == "" {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return nil, false
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		return nil, false
	}
	return claims, true
}
