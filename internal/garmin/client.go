package garmin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// ErrSessionExpired reports that Garmin rejected the DI token refresh.
var ErrSessionExpired = errors.New("garmin session expired")

const apiTimeout = 15 * time.Second

// Endpoints holds the Garmin base URLs for one account.
type Endpoints struct {
	SSO        string
	Connect    string
	ConnectAPI string
	DIToken    string
}

// DefaultEndpoints returns the public Garmin endpoints for a domain.
func DefaultEndpoints(domain string) Endpoints {
	return Endpoints{
		SSO:        "https://sso." + domain,
		Connect:    "https://connect." + domain,
		ConnectAPI: "https://connectapi." + domain,
		DIToken:    "https://diauth.garmin.com/di-oauth2-service/oauth/token",
	}
}

// TokenStore persists one account's tokens.
type TokenStore interface {
	Load() (Tokens, error)
	Save(Tokens) error
}

// Req describes one Garmin API request.
type Req struct {
	Method      string
	Path        string
	Query       url.Values
	JSON        any
	Body        []byte
	ContentType string
	Header      http.Header
	Timeout     time.Duration
}

// APIError is a non-2xx Garmin API response.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("API Error %d", e.Status)
	}
	return fmt.Sprintf("API Error %d - %s", e.Status, e.Message)
}

// Client talks to Garmin Connect with DI bearer tokens.
type Client struct {
	ep    Endpoints
	hc    *http.Client
	store TokenStore

	mu  sync.Mutex
	tok Tokens

	profileMu      sync.Mutex
	profile        Profile
	profileCached  bool
	social         map[string]any
	socialCached   bool
	settings       map[string]any
	settingsCached bool
}

// NewClient loads the account tokens and returns a client using hc, or the
// standard transport when hc is nil.
func NewClient(ep Endpoints, ts TokenStore, hc *http.Client) (*Client, error) {
	if ts == nil {
		return nil, errors.New("garmin: nil token store")
	}
	tok, err := ts.Load()
	if err != nil {
		return nil, err
	}
	if hc == nil {
		hc = &http.Client{Transport: http.DefaultTransport}
	}
	return &Client{ep: ep, hc: hc, store: ts, tok: tok}, nil
}

// Domain returns the Garmin domain derived from the Connect endpoint.
func (c *Client) Domain() string {
	u, err := url.Parse(c.ep.Connect)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Hostname(), "connect.")
}

// Do performs a request, returning the response body. A 204 yields "{}".
func (c *Client) Do(ctx context.Context, r Req) ([]byte, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = apiTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if c.expiring() {
		if err := c.refresh(ctx, false); err != nil {
			return nil, err
		}
	}
	status, body, err := c.send(ctx, r)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		if err := c.refresh(ctx, true); err != nil {
			return nil, err
		}
		status, body, err = c.send(ctx, r)
		if err != nil {
			return nil, err
		}
	}
	switch {
	case status == http.StatusNoContent:
		return []byte("{}"), nil
	case status >= 400:
		return nil, newAPIError(status, body)
	}
	return body, nil
}

// DoJSON performs a request and decodes its JSON body. An empty body yields nil.
func (c *Client) DoJSON(ctx context.Context, r Req) (any, error) {
	body, err := c.Do(ctx, r)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, fmt.Errorf("garmin: decode response: %w", err)
	}
	return v, nil
}

// GetJSON fetches a Garmin API path.
func (c *Client) GetJSON(ctx context.Context, path string, q url.Values) (any, error) {
	return c.DoJSON(ctx, Req{Method: http.MethodGet, Path: path, Query: q})
}

// Download fetches a raw Garmin API path.
func (c *Client) Download(ctx context.Context, path string, q url.Values) ([]byte, error) {
	h := http.Header{}
	h.Set("Accept", "*/*")
	return c.Do(ctx, Req{Method: http.MethodGet, Path: path, Query: q, Header: h})
}

// Upload posts data as a multipart form file.
func (c *Client) Upload(ctx context.Context, path, field, filename string, data []byte) (any, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		return nil, fmt.Errorf("garmin: build multipart form: %w", err)
	}
	if _, err := fw.Write(data); err != nil {
		return nil, fmt.Errorf("garmin: write multipart form: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("garmin: finish multipart form: %w", err)
	}
	return c.DoJSON(ctx, Req{
		Method:      http.MethodPost,
		Path:        path,
		Body:        buf.Bytes(),
		ContentType: mw.FormDataContentType(),
	})
}

func (c *Client) send(ctx context.Context, r Req) (int, []byte, error) {
	method := r.Method
	if method == "" {
		method = http.MethodGet
	}
	u := strings.TrimRight(c.ep.ConnectAPI, "/") + "/" + strings.TrimLeft(r.Path, "/")
	if len(r.Query) > 0 {
		u += "?" + r.Query.Encode()
	}

	var body io.Reader
	contentType := r.ContentType
	switch {
	case r.JSON != nil:
		b, err := json.Marshal(r.JSON)
		if err != nil {
			return 0, nil, fmt.Errorf("garmin: encode request body: %w", err)
		}
		body = bytes.NewReader(b)
		if contentType == "" {
			contentType = "application/json"
		}
	case r.Body != nil:
		body = bytes.NewReader(r.Body)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return 0, nil, fmt.Errorf("garmin: build request: %w", err)
	}
	req.Header = c.apiHeaders()
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, vs := range r.Header {
		req.Header[http.CanonicalHeaderKey(k)] = append([]string(nil), vs...)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("garmin: read response: %w", err)
	}
	return resp.StatusCode, respBody, nil
}

// refresh serializes DI token refresh per client. It first adopts newer tokens
// written by another process, then POSTs to the DI endpoint. force skips the
// "still fresh" shortcut used after a 401.
func (c *Client) refresh(ctx context.Context, force bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if stored, err := c.store.Load(); err == nil {
		if stored.DIToken != "" && stored.DIToken != c.tok.DIToken && !stored.ExpiresSoon(now) {
			c.tok = stored
			return nil
		}
	}
	if !force && !c.tok.ExpiresSoon(now) {
		return nil
	}
	if c.tok.DIRefreshToken == "" || c.tok.DIClientID == "" {
		return fmt.Errorf("%w: no DI refresh token available", ErrSessionExpired)
	}

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {c.tok.DIClientID},
		"refresh_token": {c.tok.DIRefreshToken},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.ep.DIToken, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("garmin: build token refresh request: %w", err)
	}
	req.Header = nativeHeaders()
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(c.tok.DIClientID+":")))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("garmin: token refresh failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("garmin: read token refresh response: %w", err)
	}
	switch {
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return fmt.Errorf("%w: token refresh rejected with status %d", ErrSessionExpired, resp.StatusCode)
	case resp.StatusCode >= 500:
		return fmt.Errorf("garmin: token refresh failed with status %d", resp.StatusCode)
	}

	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return fmt.Errorf("garmin: decode token refresh response: %w", err)
	}
	if out.AccessToken == "" {
		return errors.New("garmin: token refresh response has no access_token")
	}

	tok := Tokens{
		DIToken:        out.AccessToken,
		DIRefreshToken: c.tok.DIRefreshToken,
		DIClientID:     c.tok.DIClientID,
	}
	if out.RefreshToken != "" {
		tok.DIRefreshToken = out.RefreshToken
	}
	if id := tok.clientID(); id != "" {
		tok.DIClientID = id
	}
	c.tok = tok
	if err := c.store.Save(tok); err != nil {
		fmt.Fprintf(os.Stderr, "garmin: saving refreshed tokens failed: %v\n", err)
	}
	return nil
}

func (c *Client) expiring() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tok.ExpiresSoon(time.Now())
}

func (c *Client) apiHeaders() http.Header {
	h := nativeHeaders()
	c.mu.Lock()
	tok := c.tok.DIToken
	c.mu.Unlock()
	h.Set("Authorization", "Bearer "+tok)
	h.Set("Accept", "application/json")
	return h
}

func nativeHeaders() http.Header {
	h := http.Header{}
	h.Set("User-Agent", "GCM-Android-5.23")
	h.Set("X-Garmin-User-Agent", "com.garmin.android.apps.connectmobile/5.23; ; Google/sdk_gphone64_arm64/google; Android/33; Dalvik/2.1.0")
	h.Set("X-Garmin-Paired-App-Version", "10861")
	h.Set("X-Garmin-Client-Platform", "Android")
	h.Set("X-App-Ver", "10861")
	h.Set("X-Lang", "en")
	h.Set("X-GCExperience", "GC5")
	h.Set("Accept-Language", "en-US,en;q=0.9")
	return h
}

func newAPIError(status int, body []byte) *APIError {
	e := &APIError{Status: status}
	var data any
	if err := json.Unmarshal(body, &data); err != nil {
		if len(body) > 0 && len(body) < 500 {
			e.Message = string(body)
		}
		return e
	}
	if m, ok := data.(map[string]any); ok {
		if msg := nonEmpty(m["message"]); msg != "" {
			e.Message = msg
			return e
		}
		if msg := nonEmpty(m["content"]); msg != "" {
			e.Message = msg
			return e
		}
		if msg := detailedFailure(m); msg != "" {
			e.Message = msg
			return e
		}
	}
	e.Message = string(body)
	return e
}

func detailedFailure(m map[string]any) string {
	detail, _ := m["detailedImportResult"].(map[string]any)
	failures, _ := detail["failures"].([]any)
	if len(failures) == 0 {
		return ""
	}
	failure, _ := failures[0].(map[string]any)
	messages, _ := failure["messages"].([]any)
	if len(messages) == 0 {
		return ""
	}
	return nonEmpty(messages[0])
}

// nonEmpty mirrors Python truthiness for the values _run_request picks from.
func nonEmpty(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if !x {
			return ""
		}
		return "true"
	case float64:
		if x == 0 {
			return ""
		}
		return fmt.Sprint(x)
	case map[string]any:
		if len(x) == 0 {
			return ""
		}
	case []any:
		if len(x) == 0 {
			return ""
		}
	}
	return fmt.Sprint(v)
}
