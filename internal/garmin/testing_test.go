package garmin

import (
	"encoding/base64"
	"encoding/json"
	"time"
)

// fakeJWT returns an unsigned JWT whose payload carries exp and client_id.
func fakeJWT(exp time.Time, clientID string) string {
	p, _ := json.Marshal(map[string]any{"exp": exp.Unix(), "client_id": clientID})
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc(p) + ".sig"
}
