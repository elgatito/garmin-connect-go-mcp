package garmin

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

type Tokens struct {
	DIToken        string `json:"di_token"`
	DIRefreshToken string `json:"di_refresh_token"`
	DIClientID     string `json:"di_client_id"`
}

func (t Tokens) Expiry() (time.Time, bool) {
	parts := strings.Split(t.DIToken, ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		Exp *int64 `json:"exp"`
	}
	if json.Unmarshal(b, &claims) != nil || claims.Exp == nil {
		return time.Time{}, false
	}
	return time.Unix(*claims.Exp, 0), true
}
func (t Tokens) ExpiresSoon(now time.Time) bool {
	exp, ok := t.Expiry()
	return ok && now.After(exp.Add(-15*time.Minute))
}
