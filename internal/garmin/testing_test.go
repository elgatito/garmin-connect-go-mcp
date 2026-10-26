package garmin

import (
	"encoding/base64"
	"encoding/json"
	"time"
)

// fakeJWT builds an unsigned JWT carrying exp/client_id, as Garmin's DI tokens do.
func fakeJWT(exp time.Time, clientID string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, _ := json.Marshal(map[string]any{"exp": exp.Unix(), "client_id": clientID})
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + "."
}
