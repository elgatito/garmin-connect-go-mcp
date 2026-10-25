package garmin

// Tokens is the persisted login state; the JSON keys match python-garminconnect's garmin_tokens.json.
type Tokens struct {
	DIToken        string `json:"di_token"`
	DIRefreshToken string `json:"di_refresh_token"`
	DIClientID     string `json:"di_client_id"`
}
