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

// Copied verbatim from python-garminconnect 0.3.2 client.py.
const (
	nativeAPIUserAgent     = "GCM-Android-5.23"
	nativeXGarminUserAgent = "com.garmin.android.apps.connectmobile/5.23; ; Google/sdk_gphone64_arm64/google; Android/33; Dalvik/2.1.0"
	diTokenURL             = "https://diauth.garmin.com/di-oauth2-service/oauth/token"
	defaultRequestTimeout  = 15 * time.Second
	refreshRequestTimeout  = 30 * time.Second
	maxErrorText           = 500
)

// ErrSessionExpired means the refresh token was rejected; the user has to log in again.
var ErrSessionExpired = errors.New("garmin session expired")

type Endpoints struct{ SSO, Connect, ConnectAPI, DIToken string }

func DefaultEndpoints(domain string) Endpoints {
	return Endpoints{
		SSO:        "https://sso." + domain,
		Connect:    "https://connect." + domain,
		ConnectAPI: "https://connectapi." + domain,
		DIToken:    diTokenURL,
	}
}

type TokenStore interface {
	Load() (Tokens, error)
	Save(Tokens) error
}

type Req struct {
	Method, Path string
	Query        url.Values
	JSON         any
	Body         []byte
	ContentType  string
	Header       http.Header
	Timeout      time.Duration
}

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

type Client struct {
	ep Endpoints
	ts TokenStore
	hc *http.Client

	refreshMu sync.Mutex // single-flight refresh; may be held across the diauth call

	mu      sync.Mutex // guards tok and profile; never held across network calls
	tok     Tokens
	profile *Profile
}

func NewClient(ep Endpoints, ts TokenStore, hc *http.Client) (*Client, error) {
	tok, err := ts.Load()
	if err != nil {
		return nil, err
	}
	if hc == nil {
		hc = &http.Client{}
	}
	return &Client{ep: ep, ts: ts, hc: hc, tok: tok}, nil
}

func (c *Client) Domain() string {
	u, err := url.Parse(c.ep.Connect)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Host, "connect.")
}

func (c *Client) tokens() Tokens {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tok
}

func (c *Client) setTokens(t Tokens) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tok = t
}

func nativeHeaders() http.Header {
	return http.Header{
		"User-Agent":                  {nativeAPIUserAgent},
		"X-Garmin-User-Agent":         {nativeXGarminUserAgent},
		"X-Garmin-Paired-App-Version": {"10861"},
		"X-Garmin-Client-Platform":    {"Android"},
		"X-App-Ver":                   {"10861"},
		"X-Lang":                      {"en"},
		"X-GCExperience":              {"GC5"}, // non-canonical key on purpose: sent exactly as Python sends it
		"Accept-Language":             {"en-US,en;q=0.9"},
	}
}

func (c *Client) Do(ctx context.Context, r Req) ([]byte, error) {
	tok := c.tokens()
	if tok.ExpiresSoon(time.Now()) {
		if err := c.refresh(ctx, tok.DIToken); err != nil {
			return nil, err
		}
	}
	status, body, used, err := c.send(ctx, r)
	if err == nil && status == http.StatusUnauthorized {
		if err := c.refresh(ctx, used); err != nil {
			return nil, err
		}
		status, body, _, err = c.send(ctx, r)
	}
	if err != nil {
		return nil, err
	}
	if status == http.StatusNoContent {
		return []byte("{}"), nil
	}
	if status >= 400 {
		return nil, &APIError{Status: status, Message: errorMessage(body)}
	}
	return body, nil
}

// send performs one attempt and returns the status, body and the access token it used.
func (c *Client) send(ctx context.Context, r Req) (int, []byte, string, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	u := c.ep.ConnectAPI + "/" + strings.TrimLeft(r.Path, "/")
	if len(r.Query) > 0 {
		u += "?" + r.Query.Encode()
	}
	body, ctype := r.Body, r.ContentType
	if r.JSON != nil {
		b, err := json.Marshal(r.JSON)
		if err != nil {
			return 0, nil, "", err
		}
		body, ctype = b, "application/json"
	}
	method := r.Method
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return 0, nil, "", err
	}
	tok := c.tokens().DIToken
	req.Header = nativeHeaders()
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	for k, v := range r.Header {
		req.Header[http.CanonicalHeaderKey(k)] = v
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, tok, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, tok, err
	}
	return resp.StatusCode, b, tok, nil
}

// errorMessage mirrors the message extraction in python-garminconnect's _run_request.
func errorMessage(body []byte) string {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		if len(body) < maxErrorText {
			return string(body)
		}
		return ""
	}
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	for _, k := range []string{"message", "content"} {
		if s := pyStr(m[k]); s != "" {
			return s
		}
	}
	if dir, ok := m["detailedImportResult"].(map[string]any); ok {
		if f, ok := dir["failures"].([]any); ok && len(f) > 0 {
			if f0, ok := f[0].(map[string]any); ok {
				if msgs, ok := f0["messages"].([]any); ok && len(msgs) > 0 {
					if s := pyStr(msgs[0]); s != "" {
						return s
					}
				}
			}
		}
	}
	return string(body)
}

// pyStr renders a truthy JSON value as text; falsy values (null, "", 0, false, empty containers) give "".
func pyStr(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if !x {
			return ""
		}
	case float64:
		if x == 0 {
			return ""
		}
	case []any:
		if len(x) == 0 {
			return ""
		}
	case map[string]any:
		if len(x) == 0 {
			return ""
		}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// refresh renews the access token unless another caller already replaced stale.
func (c *Client) refresh(ctx context.Context, stale string) error {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()

	cur := c.tokens()
	if cur.DIToken != stale {
		return nil
	}
	// Another process sharing the token file may have refreshed already.
	// Its pair wins even when expiring: our refresh token may already be rotated away.
	if st, err := c.ts.Load(); err == nil && st.DIToken != "" && st.DIToken != cur.DIToken {
		c.setTokens(st)
		if !st.ExpiresSoon(time.Now()) {
			return nil
		}
		cur = st
	}
	if cur.DIRefreshToken == "" || cur.DIClientID == "" {
		return fmt.Errorf("%w: no refresh token available", ErrSessionExpired)
	}

	// Detached from the caller: a cancel after diauth rotates the grant would lose the new refresh token.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshRequestTimeout)
	defer cancel()
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {cur.DIClientID},
		"refresh_token": {cur.DIRefreshToken},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.ep.DIToken, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header = nativeHeaders()
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(cur.DIClientID+":")))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("garmin token refresh: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("garmin token refresh: %w", err)
	}
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return fmt.Errorf("%w: token refresh rejected with status %d", ErrSessionExpired, resp.StatusCode)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("garmin token refresh failed with status %d", resp.StatusCode)
	}
	var data struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(b, &data); err != nil || data.AccessToken == "" {
		return errors.New("garmin token refresh: response has no access_token")
	}
	next := cur
	next.DIToken = data.AccessToken
	if data.RefreshToken != "" {
		next.DIRefreshToken = data.RefreshToken
	}
	if id := clientIDFromJWT(next.DIToken); id != "" {
		next.DIClientID = id
	}
	c.setTokens(next)
	if err := c.ts.Save(next); err != nil {
		fmt.Fprintf(os.Stderr, "garmin: saving refreshed tokens failed: %v\n", err)
	}
	return nil
}

func (c *Client) DoJSON(ctx context.Context, r Req) (any, error) {
	b, err := c.Do(ctx, r)
	if err != nil || len(bytes.TrimSpace(b)) == 0 {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("invalid JSON from %s: %w", r.Path, err)
	}
	return v, nil
}

func (c *Client) GetJSON(ctx context.Context, path string, q url.Values) (any, error) {
	return c.DoJSON(ctx, Req{Method: http.MethodGet, Path: path, Query: q})
}

func (c *Client) Download(ctx context.Context, path string, q url.Values) ([]byte, error) {
	return c.Do(ctx, Req{Method: http.MethodGet, Path: path, Query: q, Header: http.Header{"Accept": {"*/*"}}})
}

func (c *Client) Upload(ctx context.Context, path, field, filename string, data []byte) (any, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		return nil, err
	}
	fw.Write(data)
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return c.DoJSON(ctx, Req{Method: http.MethodPost, Path: path, Body: buf.Bytes(), ContentType: mw.FormDataContentType()})
}
